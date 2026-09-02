package walltime

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// DefaultTimeout bounds every wait this package performs: the observer
// handshakes and the wait for verified-empty containment. It is generous
// because a slow runner is not a defect, and it is FINITE because a lifecycle
// that never closes must become a terminal record rather than a hung job.
const DefaultTimeout = 30 * time.Minute

// The frozen cancellation policy. Stage 1 declares it as an instrumentation
// identity, and both arms of a pair must run the same one, so the numbers live
// here as constants rather than as knobs a caller could pass.
//
// A cancelled containment gets exactly one bounded chance to exit on its own,
// then the whole containment is killed and reaped. The wrapper used to send
// SIGTERM and then wait for `cmd.Wait` forever: a root that ignores TERM hung
// the job indefinitely, and a detached descendant that outlived its root was
// LABELLED `crash_unclosed` and left running. Both are the same defect — the
// wrapper had no bound and no second step — and the contract asks for the
// bound, the escalation and the reap by name.
const (
	// CancellationGrace is how long the containment has to exit after the
	// whole-containment TERM before the KILL. It is generous, because a
	// worker flushing a report is not a defect, and FINITE, because a
	// lifecycle that never closes must become a terminal record rather than a
	// hung job.
	CancellationGrace = 30 * time.Second
	// ReapGrace bounds the wait AFTER the whole-containment KILL. Nothing
	// survives SIGKILL, so exceeding this means the wrapper could not reap
	// what it killed, which is itself terminal and must be recorded rather
	// than waited on.
	ReapGrace = 10 * time.Second
)

// CancellationPolicyID is the frozen policy Stage 1 declares. It is derived
// from the constants above rather than written out beside them, so a manifest
// cannot declare a policy the wrapper does not implement.
var CancellationPolicyID = fmt.Sprintf(
	"whole-containment SIGTERM on signal or deadline; SIGKILL after a %s grace; verified empty and reaped within %s; the incomplete receipt is retained",
	CancellationGrace, ReapGrace)

// The same two values as variables, so a test can shorten the policy without
// waiting out a real cancellation. Production never assigns them; every
// assignment in the tree is in a _test file, exactly as the probe hooks below
// are.
var (
	cancellationGrace = CancellationGrace
	reapGrace         = ReapGrace
)

// ExecOptions describes one physical wrapper: the exact command it starts, the
// level it measures, and the campaign identity it records.
type ExecOptions struct {
	Level Level
	// Seq is the stable ordinal of an invocation inside its bucket script.
	Seq int
	// Dir is the records directory; every stream, control file and receipt
	// for this bucket lives there.
	Dir string
	Run RunIdentity

	// Argv is the command, already split. It is executed directly — there is
	// no shell between the plan and the process, so a file name containing a
	// space or a dash cannot be re-parsed into different work than the plan
	// bound.
	Argv []string
	Cwd  string
	// Selector is the test selection this invocation applies (the file list
	// and any name filter). It is digest-bound so "this V measured that
	// invocation" is checkable.
	Selector   []string
	Desc       string
	UnitDigest Digest
	AtomDigest Digest

	// Parent, when set, is the ENCLOSING containment: this wrapper's own
	// containment is created inside it, so a nested envelope's processes stay
	// inside the lifecycle that is supposed to contain them.
	Parent *ContainmentIdentity
	// JoinParent additionally moves THIS process into the parent containment
	// before it does any work. It is true for a script wrapper, which an
	// Actions step starts fresh from outside the action containment, and false
	// for an invocation wrapper, which is already inside the script
	// containment by inheritance and would be moved OUT by joining.
	JoinParent bool

	Timeout time.Duration
	Stdin   *os.File
	Stdout  *os.File
	Stderr  *os.File
}

// Exec runs one command under a complete physical envelope with an independent
// containment peer and an independent trace collector, and returns the child's
// exit code.
//
// The ordering the verifier requires is produced here by construction:
//
//	AT_start <= CPA_start <= VTA_start <= VTA_end <= CPA_end <= AT_end
//
// The wrapper never hands an endpoint to an observer and never takes one back:
// each of the three producers reads its own clock and its own raw containment
// state, in its own process, with its own signing key.
func Exec(opt ExecOptions) (int, error) {
	if len(opt.Argv) == 0 {
		return 1, fmt.Errorf("walltime: no command to run")
	}
	if opt.Timeout <= 0 {
		opt.Timeout = DefaultTimeout
	}

	// AT_start / VB_start / V_start is the wrapper's FIRST owned operation, and
	// that is meant literally: the reading is taken before the signing key, the
	// writer, the records directory, the spec digests and the containment,
	// because all of those are wrapper-owned work and an envelope that started
	// after them would report an action shorter than the one that ran.
	clock := NewSystemClock()
	start := clock.Now()
	probe(atStartReading, opt.Dir)

	key, err := NewSigningKey()
	if err != nil {
		return 1, err
	}
	w, err := NewWriter(filepath.Join(opt.Dir, streamName(ProducerPhysical, opt.Level, opt.Seq)), ProducerPhysical, ProducerID(ProducerPhysical), key)
	if err != nil {
		return 1, err
	}
	defer w.Close()
	// A script- or invocation-level wrapper is started BY the measured step,
	// so its key cannot be in a roster sealed before that step ran. What can
	// be fixed is that the set is CLOSED at AT_end: every key registers here,
	// `wall end` seals the log, and a key that was never registered — or one
	// appended afterwards — is not a signer this measurement admitted.
	if err := RegisterKey(opt.Dir, KeyLogEntry{
		Producer: ProducerPhysical, Level: opt.Level, Seq: opt.Seq,
		PublicKey: PublicKeyOf(key), Binary: SelfDigest(),
	}); err != nil {
		return 1, err
	}

	spec := &SpecIdentity{
		ArgvDigest:     mustDigest(opt.Argv),
		Cwd:            opt.Cwd,
		SelectorDigest: mustDigest(opt.Selector),
		UnitDigest:     opt.UnitDigest,
		AtomDigest:     opt.AtomDigest,
		Desc:           opt.Desc,
	}

	// Joining the parent containment before doing anything else is what makes
	// this wrapper's own work — not just its child's — part of the enclosing
	// envelope's containment lifecycle.
	//
	// JoinParent is separate from Parent because they are different questions.
	// A script wrapper is a fresh process started by an Actions step, so it has
	// to join. An invocation wrapper is already inside the script containment
	// by inheritance, and joining would MOVE it out — a process belongs to
	// exactly one cgroup — taking the invocation's work out of the script
	// lifecycle that is supposed to contain it.
	if opt.Parent != nil && opt.JoinParent {
		if err := joinContainment(*opt.Parent, os.Getpid()); err != nil {
			return 1, terminalExec(w, opt, spec, start, clock, TerminalWrapperError, "join parent containment: "+err.Error())
		}
	}

	cont, err := NewContainment(containmentName(opt), opt.Parent)
	if err != nil {
		return 1, terminalExec(w, opt, spec, start, clock, TerminalWrapperError, "create containment: "+err.Error())
	}
	// Cleanup is EXPLICIT rather than deferred, because a defer would run
	// after the closing record and put real wrapper-owned work outside the
	// envelope it belongs to. destroyed guards the error paths below, which
	// still need it.
	destroyed := false
	destroy := func() {
		if !destroyed {
			destroyed = true
			_ = cont.Destroy()
		}
	}
	defer destroy()

	if _, err := w.Append(Record{
		Kind: "boundary", Role: roleOrPanic(ProducerPhysical, opt.Level), Level: opt.Level,
		Boundary: "start", Source: SourceWrapper, Seqno: opt.Seq,
		Run: opt.Run, Containment: cont.Identity(), Instant: start, Spec: spec,
	}); err != nil {
		return 1, err
	}
	if opt.Level == LevelScript {
		// The invocation wrappers the script is about to start are separate
		// processes; this is how they find the containment they must nest
		// inside. It is removed when the script closes, so a later run cannot
		// nest under a containment that no longer exists.
		if err := writeContainmentHandoff(opt.Dir, cont.Identity()); err != nil {
			return 1, terminalExec(w, opt, spec, start, clock, TerminalWrapperError, err.Error())
		}
	}

	deadline := time.Now().Add(opt.Timeout)
	peer, err := startObserver(ProducerPeer, opt, cont.Identity(), deadline, false)
	if err != nil {
		return 1, terminalExec(w, opt, spec, start, clock, TerminalWrapperError, "start containment peer: "+err.Error())
	}
	trace, err := startObserver(ProducerTrace, opt, cont.Identity(), deadline, false)
	if err != nil {
		peer.abandon()
		return 1, terminalExec(w, opt, spec, start, clock, TerminalWrapperError, "start trace collector: "+err.Error())
	}

	// No child may start before the peer's admission receipt exists, and the
	// trace admits after the peer so that the peer brackets it. Both are
	// verified by reading the observers' OWN records, not by trusting a return
	// code.
	// BOTH observers end when either admission fails. Abandoning only the
	// other one left the observer whose admission failed running: it is
	// already started, it is watching the containment, and the wrapper is
	// about to return a terminal record saying the lifecycle never opened.
	if err := peer.admit(deadline); err != nil {
		peer.abandon()
		trace.abandon()
		return 1, terminalExec(w, opt, spec, start, clock, TerminalWrapperError, err.Error())
	}
	if err := trace.admit(deadline); err != nil {
		trace.abandon()
		// The peer admitted, so it is given the chance to close cleanly and
		// leave a closing record; abandon is the fallback when it will not.
		if closeErr := peer.close(deadline); closeErr != nil {
			peer.abandon()
		}
		return 1, terminalExec(w, opt, spec, start, clock, TerminalWrapperError, err.Error())
	}

	code, proc, termState, reason := runChild(opt, cont, deadline)

	// The wrapper's own view of emptiness is physical suffix work; the
	// observers still take their own reads, and the VERIFIER decides closure.
	// Anything still alive is killed and reaped here rather than merely
	// labelled, because the wrapper is the last thing that can reach it.
	emptyReaped, emptyErr := enforceContainmentEmpty(cont, deadline)

	// Trace closes first, then the peer: the resulting endpoint order is the
	// contract's, and it is established by the protocol rather than asserted
	// after the fact.
	traceErr := trace.close(deadline)
	peerErr := peer.close(deadline)

	members, note := membershipSnapshot(cont)
	if _, err := w.Append(Record{
		Kind: "process_tree", Role: roleOrPanic(ProducerPhysical, opt.Level), Level: opt.Level,
		Source: SourceProcessLifecycle, Seqno: opt.Seq, Run: opt.Run,
		Containment: cont.Identity(), Proc: proc, Instant: clock.Now(),
		RawProcs: members, Note: note,
	}); err != nil {
		return code, err
	}

	if emptyErr != nil {
		// An ESCAPE is a descendant that outlived a run which ended on its own
		// terms. After a cancellation the wrapper has just killed the whole
		// containment itself, so members still draining a moment later are the
		// tail of that kill — and calling it `crash_unclosed` would report an
		// escape the wrapper caused, hiding the cancellation that is the real
		// terminal state. The distinction is exactly whether the containment
		// was successfully reaped.
		if termState == TerminalCancelled && emptyReaped {
			reason = joinReason(reason, "the containment was verified empty after the cancellation kill: "+emptyErr.Error())
		} else {
			termState, reason = TerminalCrashUnclosed, emptyErr.Error()
		}
	}
	for _, e := range []error{traceErr, peerErr} {
		if e != nil && termState == TerminalPassed {
			termState, reason = TerminalWrapperError, e.Error()
		}
	}

	if opt.Level == LevelScript {
		// The handoff is script-owned work; removing it here rather than in a
		// defer keeps it inside the envelope.
		_ = os.Remove(scriptHandoffPath(opt.Dir))
	}
	destroy()
	probe(atEndReading, opt.Dir)

	// Only now, with the observers reaped, the containment destroyed and every
	// other record flushed, is the closing reading taken. What remains outside
	// is exactly one record write, which is the ledger closing itself and
	// cannot be inside the interval it closes; the note says so rather than
	// leaving a reader to assume otherwise.
	if _, err := w.Append(Record{
		Kind: "boundary", Role: roleOrPanic(ProducerPhysical, opt.Level), Level: opt.Level,
		Boundary: "end", Source: SourceWrapper, Seqno: opt.Seq, Run: opt.Run,
		Containment: cont.Identity(), Instant: clock.Now(), Spec: spec,
		Proc: proc, Terminal: termState, Reason: reason,
		Note: "observers reaped, containment destroyed and all other records flushed before this reading; only this record's own write follows it",
	}); err != nil {
		return code, err
	}
	return code, nil
}

// runChild starts the command INSIDE the containment and waits for it. The
// child is created in the containment (never moved into it afterwards), so
// there is no window in which action-owned work exists outside the lifecycle
// the peer and trace are bracketing.
func runChild(opt ExecOptions, cont Containment, deadline time.Time) (int, ProcIdentity, string, string) {
	cmd := exec.Command(opt.Argv[0], opt.Argv[1:]...)
	cmd.Dir = opt.Cwd
	// Assign the concrete *os.File values, not the struct fields directly: a
	// nil *os.File stored in an io.Writer is a NON-nil interface holding a nil
	// pointer, so `cmd.Stdout == nil` would be false and the child's output
	// would go nowhere. The tests measured this the hard way — a wrapper that
	// swallows the test log is worse than no wrapper.
	stdout, stderr := opt.Stdout, opt.Stderr
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if opt.Stdin != nil {
		cmd.Stdin = opt.Stdin
	}
	attr, cleanup, err := containmentSysProc(cont)
	if err != nil {
		return 1, ProcIdentity{}, TerminalSpawnError, err.Error()
	}
	defer cleanup()
	cmd.SysProcAttr = attr

	// Cancellation must reach the whole containment, be waited for, and be
	// RETAINED — never converted into a shorter successful measurement.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)

	if err := cmd.Start(); err != nil {
		return 1, ProcIdentity{}, TerminalSpawnError, err.Error()
	}
	if err := postSpawnAdmit(cont, cmd.Process.Pid); err != nil {
		// The child is running but is not in the containment, so nothing
		// downstream can account for it — and returning here abandoned it
		// with no handle for anyone else to reap. A child that cannot be
		// admitted is a child that must not run.
		pid := cmd.Process.Pid
		err = reapStarted(ProducerPhysical, cmd, fmt.Errorf("admit child: %w", err))
		return 1, ProcIdentity{PID: pid}, TerminalSpawnError, err.Error()
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	cancelled, escalation, waitErr := awaitChild(cont, sigs, done, deadline)

	proc := ProcIdentity{
		PID:       cmd.Process.Pid,
		PGID:      processGroupOf(cmd.Process.Pid),
		StartID:   processStartID(cmd.Process.Pid),
		ParentPID: os.Getpid(),
	}
	code := 0
	state := TerminalPassed
	reason := ""
	if waitErr == errUnreaped {
		// cmd.Wait has NOT returned, so cmd.ProcessState is still being
		// written by the goroutine waiting on it and must not be read here.
		// The envelope did not end where a closing record would claim it did,
		// which is exactly what crash_unclosed means.
		proc.ExitKind = TerminalCrashUnclosed
		reason = joinReason(escalation, "the root was not reaped within "+reapGrace.String()+" of the whole-containment KILL")
		if cancelled != "" {
			reason = joinReason("wrapper received "+cancelled, reason)
		}
		return 1, proc, TerminalCrashUnclosed, reason
	}
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			proc.Signal = ws.Signal().String()
			state, reason = TerminalSignalled, "child signalled with "+proc.Signal
		}
	}
	proc.ExitCode = code
	if code != 0 && state == TerminalPassed {
		state, reason = TerminalFailed, fmt.Sprintf("child exited %d", code)
	}
	proc.ExitKind = state
	if cancelled != "" {
		state, reason = TerminalCancelled, "wrapper received "+cancelled
		if escalation != "" {
			// The escalation is RETAINED, not summarised away. "cancelled"
			// and "cancelled after the containment had to be killed" are
			// different facts about the same run, and only the second one
			// says the workload did not stop when it was asked to.
			reason += "; " + escalation
		}
	} else if escalation != "" {
		state, reason = TerminalCancelled, escalation
	}
	if waitErr != nil && state == TerminalPassed {
		state, reason = TerminalWrapperError, waitErr.Error()
	}
	return code, proc, state, reason
}

// awaitChild waits for the child under the frozen bounded cancellation policy.
//
// There are three sources of an ending and they are deliberately not
// interchangeable: the child exiting, a signal reaching the wrapper, and the
// deadline passing. The last two both mean "stop", and both used to mean
// "send SIGTERM and then wait forever" — the deadline did not even reach here,
// so a child that simply never exited hung the wrapper with no record.
//
// It returns what cancelled the run, what escalation was needed, and the
// wait error (errUnreaped when the root outlived a whole-containment KILL).
func awaitChild(cont Containment, sigs <-chan os.Signal, done <-chan error, deadline time.Time) (cancelled, escalation string, waitErr error) {
	// Phase 0: ordinary running. The deadline is a real endpoint, not a
	// suggestion: the contract makes a cancellation timeout terminal.
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case err := <-done:
		return "", "", err
	case s := <-sigs:
		cancelled = s.String()
	case <-timer.C:
		escalation = "the run did not finish before its cancellation deadline"
	}

	// Phase 1: the bounded grace. TERM reaches the WHOLE containment, not just
	// the root: a root that exits while its workers keep running has not
	// stopped, and the containment is the only thing that names all of them.
	_ = cont.Signal(syscall.SIGTERM)
	grace := cancellationGrace
	if until := time.Until(deadline); until > 0 && until < grace {
		grace = until
	}
	graceTimer := time.NewTimer(grace)
	defer graceTimer.Stop()
	select {
	case err := <-done:
		return cancelled, escalation, err
	case <-graceTimer.C:
	}

	// Phase 2: escalation. Nothing survives a whole-containment SIGKILL, and
	// on Linux cgroup.kill makes it atomic — there is no window for a
	// descendant to fork out from under the enumeration.
	_ = cont.Signal(syscall.SIGKILL)
	escalation = joinReason(escalation, "the containment did not exit within "+grace.String()+" of SIGTERM and was killed")
	reapTimer := time.NewTimer(reapGrace)
	defer reapTimer.Stop()
	select {
	case err := <-done:
		return cancelled, escalation, err
	case <-reapTimer.C:
		return cancelled, escalation, errUnreaped
	}
}

func joinReason(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "; " + b
	}
}

// atStartReading and atEndReading are called at the exact moments the physical
// envelope opens and closes. They are nil in production and cost one nil check.
//
// They exist because the property that matters here — that the reading is
// taken BEFORE the records directory, the signing key and the writer, and
// AFTER the containment is destroyed and the handoff removed — cannot be
// tested from timings. The setup costs tens of microseconds, so any threshold
// small enough to catch a regression would be flaky. A probe that inspects the
// observable state at the instant of the reading catches it exactly: if the
// reading moves after the setup, the directory already exists when the probe
// fires.
var (
	atStartReading func(dir string)
	atEndReading   func(dir string)
	// atContainmentJoin fires after this process has joined the enclosing
	// containment and before it spawns anything, so a test can prove the
	// ordering the inheritance depends on.
	atContainmentJoin func(dir string)
	// atRecordsDir fires after the records directory exists and before the
	// signing key, so a test can INJECT a failure into the window between
	// AT_start and the first writer. That window is the one place a bootstrap
	// failure could previously return with nothing in the ledger, and its
	// retention cannot be proved by any real error a test can provoke there.
	atRecordsDir func(dir string) error
)

func probe(hook func(string), dir string) {
	if hook != nil {
		hook(dir)
	}
}

// probeErr is probe for a hook that can fail, used to inject a pre-writer
// bootstrap failure. It returns nil in production, where the hook is nil.
func probeErr(hook func(string) error, dir string) error {
	if hook == nil {
		return nil
	}
	return hook(dir)
}

// errUnreaped is returned when the root outlived a whole-containment SIGKILL
// long enough for the bounded reap window to expire. It is a distinct value
// because "the child failed" and "the wrapper could not reap the child" are
// different terminal states.
var errUnreaped = fmt.Errorf("the root was not reaped after the whole containment was killed")

// enforceContainmentEmpty waits for the containment to drain and, if it does
// not, KILLS AND REAPS what is left.
//
// The escape is still terminal — a descendant that outlived its root means the
// envelope did not end where the closing record would claim, and no amount of
// cleanup changes that. What changes is that the descendant no longer survives
// the wrapper. Callers used to take `waitContainmentEmpty`'s error, write
// `crash_unclosed` and return, leaving the process running on the runner for
// whatever came next; the contract asks for a guaranteed reap, and recording
// an escape is not reaping it.
//
// The returned error is the retained reason: it names the escape AND what the
// forced reap achieved, because "it escaped" and "it escaped and is still
// running" call for different responses from whoever reads the receipt.
func enforceContainmentEmpty(cont Containment, deadline time.Time) (reaped bool, err error) {
	escape := waitContainmentEmpty(cont, deadline)
	if escape == nil {
		return true, nil
	}
	if killErr := cont.Signal(syscall.SIGKILL); killErr != nil {
		return false, fmt.Errorf("%w; the whole-containment KILL failed: %v", escape, killErr)
	}
	if waitErr := waitContainmentEmpty(cont, time.Now().Add(reapGrace)); waitErr != nil {
		return false, fmt.Errorf("%w; the containment was killed and was STILL not empty after %s: %v", escape, reapGrace, waitErr)
	}
	// Reaped, but it should not have needed reaping. The caller decides what
	// that means: after a cancellation the wrapper itself killed the
	// containment, so members still draining are the END of that cancellation
	// rather than an escape from a completed run.
	return true, fmt.Errorf("%w; the whole containment was then killed and verified empty", escape)
}

// waitContainmentEmpty is the wrapper's own post-exit verification. A root
// that returned while a descendant is still alive is an escape, and an escape
// is terminal — it is never rounded down to "the tests finished".
func waitContainmentEmpty(cont Containment, deadline time.Time) error {
	for {
		_, populated, err := cont.Observe("physical")
		if err != nil {
			return fmt.Errorf("containment read: %w", err)
		}
		if !populated {
			return nil
		}
		if time.Now().After(deadline) {
			pids, _ := cont.Procs()
			return fmt.Errorf("containment %s still populated after root exit (%d member(s))", cont.Identity().ID, len(pids))
		}
		time.Sleep(pollInterval)
	}
}

// membershipSnapshot returns the containment membership at close. The LIST is
// retained, not a count: "0 members" and "these are the members" answer
// different questions, and only the second one lets a reader check the first.
func membershipSnapshot(cont Containment) ([]int, string) {
	pids, err := cont.Procs()
	if err != nil {
		return nil, "cgroup.procs unreadable: " + err.Error()
	}
	return pids, fmt.Sprintf("cgroup.procs members at close: %d", len(pids))
}

// ObserverLauncher builds the command that runs one independent observer. It
// is a variable so a test can point it at a helper process; production always
// re-executes THIS binary, which is what makes the observer's delivery
// identity the same bytes Stage 1 bound.
var ObserverLauncher = func(args []string) (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return exec.Command(self, append([]string{"wall", "observe"}, args...)...), nil
}

// observerProc is the wrapper's handle on one independent observer process.
type observerProc struct {
	producer Producer
	cmd      *exec.Cmd
	// pid is the observer's process id, retained SEPARATELY from cmd.
	//
	// A detached action observer outlives the step that started it, so the
	// later step reconstructs this handle from the action state and has no
	// cmd. Without a pid there was then nothing to kill: a lifecycle that
	// could not be completed left a live observer watching the containment,
	// and the one process able to reach it had thrown away its address.
	pid    int
	ctl    control
	stream string
	// pub is the observer's signing identity, kept so the wrapper can declare
	// it in the roster or register it in the key log. The PRIVATE half never
	// comes back here — it goes down a pipe into the child and nowhere else.
	pub string
}

// startObserver spawns a peer or trace collector as a SEPARATE PROCESS with
// its own signing key. Independence is structural: the observer never sees the
// wrapper's records and the wrapper never sees the observer's clock.
func startObserver(p Producer, opt ExecOptions, ident ContainmentIdentity, deadline time.Time, detached bool) (*observerProc, error) {
	key, err := NewSigningKey()
	if err != nil {
		return nil, err
	}
	identJSON, err := json.Marshal(ident)
	if err != nil {
		return nil, err
	}
	runJSON, err := json.Marshal(opt.Run)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(opt.Dir, fmt.Sprintf("ctl.%s.%s.%03d", opt.Level, p, opt.Seq))
	args := []string{
		"--producer", string(p),
		"--level", string(opt.Level),
		"--seq", fmt.Sprint(opt.Seq),
		"--dir", opt.Dir,
		"--control", base,
		"--containment", string(identJSON),
		"--run", string(runJSON),
		// The key travels down an inherited pipe, NOT in argv. Argv is
		// world-readable in the process table, so a key passed that way is
		// readable by every process on the runner — not merely by the measured
		// script, but by anything sharing the machine. A signer set nobody
		// else can read is the point of having one.
		"--key-fd", fmt.Sprint(observerKeyFD),
		"--timeout", time.Until(deadline).String(),
	}
	cmd, err := ObserverLauncher(args)
	if err != nil {
		return nil, err
	}
	keyR, keyW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer keyR.Close()
	cmd.ExtraFiles = append(cmd.ExtraFiles, keyR)
	// The observer's environment is SCRUBBED of every wall-time secret.
	//
	// A nil Cmd.Env hands the child this process's whole environment, and
	// `wall begin` holds the run key while it launches these two. The action
	// level then detaches them so they outlive the opening step and span the
	// measured work — so an inherited key would sit in a live process for the
	// entire window the measured script runs in, readable by any same-uid
	// workload through /proc/<pid>/environ.
	//
	// That would hand a hostile workload the ability to sign replacement
	// rosters and closing seals: not an ineligible row, but a forged
	// attestation that verifies. The observer needs none of these values — its
	// own signing key arrives on an inherited descriptor — so it gets none.
	cmd.Env = scrubSecrets(cmd.Env)
	if detached {
		// The action-level observers outlive the step that started them:
		// `wall begin` and `wall end` are two different Actions steps, so the
		// peer and the collector are given their own session.
		cmd.SysProcAttr = detachedSysProc()
	}
	logPath := filepath.Join(opt.Dir, fmt.Sprintf("observer.%s.%s.%03d.log", opt.Level, p, opt.Seq))
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		logf.Close()
		keyW.Close()
		return nil, err
	}
	logf.Close()
	// EVERY failure from here on has already started a child.
	//
	// Returning (nil, err) hands the caller no handle, so nothing else can
	// reap it — and the process is a real observer that will sit watching the
	// containment for its whole timeout, writing records for a launch that was
	// refused. It contaminates exactly what it was meant to measure: the
	// lifecycle boundaries and the timing of whatever runs next.
	//
	// reapStarted kills and waits before the error leaves this function, so a
	// refused launch leaves nothing behind. It is the same rule the wrapper
	// applies to its measured child; an observer is not exempt because it is
	// ours.
	fail := func(err error) (*observerProc, error) {
		return nil, reapStarted(p, cmd, err)
	}
	// Write and close AFTER Start: the child holds the read end, so the write
	// end must be closed here or the child's read never sees EOF.
	_, writeErr := io.WriteString(keyW, EncodeKey(key))
	closeErr := keyW.Close()
	if writeErr != nil {
		return fail(fmt.Errorf("walltime: hand the %s its key: %w", p, writeErr))
	}
	if closeErr != nil {
		return fail(fmt.Errorf("walltime: hand the %s its key: %w", p, closeErr))
	}
	if opt.Level != LevelAction {
		// Action-level observers are declared in the roster instead; anything
		// below it registers, for the same reason the physical wrapper does.
		if err := RegisterKey(opt.Dir, KeyLogEntry{
			Producer: p, Level: opt.Level, Seq: opt.Seq,
			PublicKey: PublicKeyOf(key), Binary: SelfDigest(),
		}); err != nil {
			return fail(err)
		}
	}
	return &observerProc{
		producer: p, cmd: cmd, pid: cmd.Process.Pid, ctl: control{base: base}, pub: PublicKeyOf(key),
		stream: filepath.Join(opt.Dir, streamName(p, opt.Level, opt.Seq)),
	}, nil
}

// reapStarted terminates and waits for an observer this function started but
// is about to abandon, and folds the outcome into the error it returns.
//
// It KILLs rather than asking politely: the child is an observer whose launch
// has already failed, there is nothing for it to finish, and the caller is
// blocked until it is gone. The wait is what makes it a reap rather than a
// signal — an unwaited child is a zombie, and the contract's rule is
// terminate AND reap.
//
// A failure to reap is folded into the returned error rather than replacing
// it: the original cause is why the launch was abandoned, and "we also could
// not clean it up" is a second fact about the same event, not a substitute
// for the first.
func reapStarted(p Producer, cmd *exec.Cmd, cause error) error {
	if cmd.Process == nil {
		return cause
	}
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("%w; the already-started %s could not be killed: %v", cause, p, err)
	}
	if _, err := cmd.Process.Wait(); err != nil {
		return fmt.Errorf("%w; the already-started %s was killed but not reaped: %v", cause, p, err)
	}
	return cause
}

// observerKeyFD is the descriptor an observer reads its signing key from. It
// is 3 because Go places ExtraFiles immediately after stdin/stdout/stderr.
const observerKeyFD = 3

// The private signing capabilities this system reads from the environment.
//
// They are declared HERE, together, and every command that needs one refers to
// the constant rather than writing the name out again. That is not tidiness:
// the scrub list below is built from exactly these names, so a capability
// cannot be introduced somewhere else and then be missing from it. The builder
// key was introduced in another package and forgotten by the denylist, which
// is precisely the failure this arrangement removes — and
// TestEveryPrivateKeyEnvironmentVariableIsScrubbed scans the whole repository
// for `TB_WALL_*_KEY` so a name declared anywhere else is still caught.
const (
	// AuthorityKeyEnv approves Stage-1 inputs.
	AuthorityKeyEnv = "TB_WALL_AUTHORITY_KEY"
	// VerifierKeyEnv signs a verifier verdict.
	VerifierKeyEnv = "TB_WALL_VERIFIER_KEY"
	// ReplayKeyEnv signs an independent replay attestation.
	ReplayKeyEnv = "TB_WALL_REPLAY_KEY"
	// BuilderKeyEnv signs a build attestation.
	BuilderKeyEnv = "TB_WALL_BUILDER_KEY"
)

// WallTimeSecretEnv is every environment variable this package treats as a
// secret. A process that does not need one must not carry one: these are
// capabilities, and a capability that outlives the step it was granted to is
// no longer scoped to that step.
var WallTimeSecretEnv = []string{
	RunKeyEnv,       // signs the roster and the closing seal
	AuthorityKeyEnv, // approves Stage-1 inputs
	VerifierKeyEnv,  // signs a verifier verdict
	ReplayKeyEnv,    // signs an independent replay attestation
	BuilderKeyEnv,   // signs a build attestation
}

// scrubSecrets removes every wall-time secret from an environment.
//
// A nil env means "inherit", which is what exec.Cmd does by default, so nil is
// resolved to the current environment HERE rather than left to the child. It
// scrubs whatever the launcher supplied rather than rebuilding from
// os.Environ(), so a caller that deliberately set something on the command —
// a test harness selecting its dispatch mode, say — keeps it.
//
// It is a denylist rather than an allowlist because an observer legitimately
// needs the ambient environment — PATH, HOME, the cgroup root, the runner's
// own variables — and an allowlist would silently break on the first runner
// that requires something nobody enumerated. What must not travel is
// enumerable and short.
func scrubSecrets(env []string) []string {
	if env == nil {
		env = os.Environ()
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(WallTimeSecretEnv, name) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// ReadKeyFD reads a signing key handed down an inherited descriptor.
func ReadKeyFD(fd int) (ed25519.PrivateKey, error) {
	f := os.NewFile(uintptr(fd), "walltime-key")
	if f == nil {
		return nil, fmt.Errorf("walltime: no key on fd %d", fd)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("walltime: read key from fd %d: %w", fd, err)
	}
	return DecodeKey(strings.TrimSpace(string(b)))
}

// admit releases the observer's admission read and waits for the RECORD it
// wrote — not for an exit code. The wrapper proceeds only once the receipt
// exists on disk.
func (o *observerProc) admit(deadline time.Time) error {
	if err := o.ctl.signal(phaseAdmit); err != nil {
		return fmt.Errorf("signal %s admit: %w", o.producer, err)
	}
	return o.awaitBoundary("start", deadline)
}

// close releases the observer's verified-empty read, waits for its closing
// record, and reaps the process.
func (o *observerProc) close(deadline time.Time) error {
	if err := o.ctl.signal(phaseClose); err != nil {
		// The observer never learned to stop. Returning here left it running
		// for its whole timeout with the caller holding an error and no
		// intention of trying again.
		o.abandon()
		return fmt.Errorf("signal %s close: %w", o.producer, err)
	}
	if err := o.awaitBoundary("end", deadline); err != nil {
		o.abandon()
		return err
	}
	if o.cmd == nil {
		// A detached observer is reaped by init; its closing RECORD is the
		// receipt that matters, and it is already on disk.
		return nil
	}
	return o.cmd.Wait()
}

// abandon kills an observer whose lifecycle cannot be completed. Its partial
// stream stays on disk: a truncated observation is evidence, and deleting it
// would turn an ineligible row into a missing one.
func (o *observerProc) abandon() {
	if o.cmd != nil && o.cmd.Process != nil {
		_ = o.cmd.Process.Kill()
		_ = o.cmd.Wait()
		return
	}
	// A DETACHED observer reconstructed from the action state has no cmd, and
	// a lifecycle that cannot be completed still has to end. It is not this
	// process's child any more, so there is nothing to reap — but it is still
	// killable, and leaving it watching the containment would let a refused
	// lifecycle keep writing records over whatever runs next.
	if o.pid > 0 {
		_ = syscall.Kill(o.pid, syscall.SIGKILL)
	}
}

func (o *observerProc) awaitBoundary(boundary string, deadline time.Time) error {
	for {
		recs, err := ReadRecords(o.stream)
		if err == nil {
			for _, r := range recs {
				if r.Kind == "boundary" && r.Boundary == boundary {
					return nil
				}
				if r.Kind == "terminal" {
					return fmt.Errorf("%s terminal before %s: %s", o.producer, boundary, r.Reason)
				}
			}
		}
		if o.cmd != nil && o.cmd.ProcessState != nil {
			return fmt.Errorf("%s exited before writing its %s record", o.producer, boundary)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for the %s %s record", o.producer, boundary)
		}
		time.Sleep(pollInterval)
	}
}

// terminalExec retains a wrapper-level failure with its reason and no
// duration.
func terminalExec(w *Writer, opt ExecOptions, spec *SpecIdentity, start Instant, clock Clock, state, reason string) error {
	_, _ = w.Append(Record{
		Kind: "terminal", Role: roleOrPanic(ProducerPhysical, opt.Level), Level: opt.Level,
		Source: SourceWrapper, Seqno: opt.Seq, Run: opt.Run, Instant: clock.Now(),
		Spec: spec, Terminal: state, Reason: reason,
	})
	return fmt.Errorf("walltime: %s", reason)
}

// scriptHandoffPath is where a script wrapper leaves its containment identity
// for the invocation wrappers its own script starts.
func scriptHandoffPath(dir string) string { return filepath.Join(dir, "script-containment.json") }

func writeContainmentHandoff(dir string, ident ContainmentIdentity) error {
	b, err := json.MarshalIndent(ident, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(scriptHandoffPath(dir), b, 0o644); err != nil {
		return fmt.Errorf("write the script containment handoff: %w", err)
	}
	return nil
}

// ScriptContainment reads that handoff. A missing one is not an error: an
// invocation run outside a measured script simply has no enclosing script
// containment.
func ScriptContainment(dir string) (*ContainmentIdentity, bool) {
	b, err := os.ReadFile(scriptHandoffPath(dir))
	if err != nil {
		return nil, false
	}
	var ident ContainmentIdentity
	if err := json.Unmarshal(b, &ident); err != nil {
		return nil, false
	}
	return &ident, true
}

func containmentName(opt ExecOptions) string {
	base := fmt.Sprintf("tb-%s", opt.Level)
	if opt.Run.BucketID != "" {
		base += "-" + sanitize(opt.Run.BucketID)
	}
	if opt.Level == LevelInvocation {
		base += fmt.Sprintf("-%03d", opt.Seq)
	}
	return base + fmt.Sprintf("-%d", os.Getpid())
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, s)
}

func roleOrPanic(p Producer, l Level) Role {
	r, err := RoleFor(p, l)
	if err != nil {
		panic(err)
	}
	return r
}

func mustDigest(v any) Digest {
	d, err := DigestJSON(v)
	if err != nil {
		return ""
	}
	return d
}
