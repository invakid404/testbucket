package walltime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
	// ObserverCloseGrace bounds the wait for ONE observer to stop and exit,
	// so a single stuck observer cannot spend the whole closing budget.
	//
	// The closer tears down the trace collector and then the containment peer
	// against one shared deadline, and the first one took all of it: the
	// second was then asked to close with no time left, its closing record was
	// never collected, and the envelope was terminal for a missing endpoint
	// that had nothing to do with it. Bounding each teardown separately means
	// a stuck observer is reported as a stuck observer while the other is
	// still proved. The overall deadline still bounds the whole close; this
	// only stops one step consuming it.
	ObserverCloseGrace = 15 * time.Second
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
	cancellationGrace  = CancellationGrace
	reapGrace          = ReapGrace
	observerCloseGrace = ObserverCloseGrace
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

// Exec measures one owned child and returns its exit code.
//
// SIMPLIFIED per salvage-map: what remains is the observable itself, and only
// that. The two readings and their ordering, concrete argv spawn with
// stdout/stderr passthrough, exit-status preservation, cancellation
// forwarding, root wait and reap of the one child this process parented,
// same-PGID signal with bounded TERM->KILL escalation, and group drain before
// the closing read.
//
// Removed with the proof machinery: the three-ledger split, signer and key
// handling, observer startup and admission handshakes, cgroup admission and
// freeze/thaw, and peer/trace record emission. Those served the hostile-runner
// model contract §0.1 places out of scope, and were roughly two-thirds of this
// function.
//
// The interval is bracketed so that:
//
//	start <= spawn <= child exit <= root reap <= group drain <= end
//
// The opening reading is this function's FIRST owned operation, taken before
// the writer, the spec digests and the spawn, because all of those are
// wrapper-owned work and an envelope that started after them would report an
// interval shorter than the one that ran. The closing reading is taken only
// after the group is confirmed empty; what remains outside is one record
// write, which is the ledger closing itself and cannot be inside the interval
// it closes.
func Exec(opt ExecOptions) (int, error) {
	if len(opt.Argv) == 0 {
		return 1, fmt.Errorf("walltime: no command to run")
	}
	if opt.Timeout <= 0 {
		opt.Timeout = DefaultTimeout
	}

	clock := NewSystemClock()
	start := clock.Now()
	probe(atStartReading, opt.Dir)

	w, err := NewWriter(filepath.Join(opt.Dir, execStreamName(opt)), ProducerPhysical, "physical")
	if err != nil {
		return 1, err
	}
	defer w.Close()

	// THE EXECUTED ABSOLUTE DIRECTORY, not the relative string the plan
	// rendered: §13.1's cwd identity is the directory the command ran in, and
	// the child below is given the same resolved value so the record and the
	// execution cannot describe different directories.
	opt.Cwd = AbsCwd(opt.Cwd)
	spec := &SpecIdentity{
		ArgvDigest:     mustDigest(opt.Argv),
		Cwd:            opt.Cwd,
		SelectorDigest: mustDigest(opt.Selector),
		UnitDigest:     opt.UnitDigest,
		AtomDigest:     opt.AtomDigest,
		Desc:           opt.Desc,
	}

	if _, err := w.Append(Record{
		Kind: "boundary", Level: opt.Level,
		Boundary: "start", Source: SourceWrapper, Seqno: opt.Seq,
		Run: opt.Run, Instant: start, Spec: spec,
	}); err != nil {
		return 1, err
	}

	deadline := time.Now().Add(opt.Timeout)
	code, proc, termState, reason := runOwnedChild(opt, deadline, clock)

	probe(atEndReading, opt.Dir)
	if _, err := w.Append(Record{
		Kind: "boundary", Level: opt.Level,
		Boundary: "end", Source: SourceWrapper, Seqno: opt.Seq, Run: opt.Run,
		Instant: clock.Now(), Spec: spec,
		Proc: procOrZero(proc), Terminal: termState, Reason: reason,
		Note: "root child reaped and the process group drained before this reading; only this record's own write follows it",
	}); err != nil {
		return code, err
	}
	return code, nil
}

// execStreamName names the one stream this wrapper writes. The three-ledger
// fan-out is gone, so there is exactly one producer and the name no longer
// has to disambiguate between them.
func execStreamName(opt ExecOptions) string {
	return fmt.Sprintf("physical-%s-%02d.jsonl", sanitize(string(opt.Level)), opt.Seq)
}

// runOwnedChild spawns the argv in its OWN process group, forwards
// cancellation, then performs contract §3.3's three teardown steps through
// DrainGroup: wait and reap the root child, signal the group by negative PGID
// with bounded escalation, and drain it.
//
// The reap runs concurrently with the escalation inside DrainGroup, because
// sequencing them either way deadlocks: a killed root child is a zombie that
// keeps the PGID alive, but a child ignoring TERM does not exit until KILL.
func runOwnedChild(opt ExecOptions, deadline time.Time, clock Clock) (code int, proc *ProcIdentity, termState, reason string) {
	cmd := exec.Command(opt.Argv[0], opt.Argv[1:]...)
	cmd.Dir = opt.Cwd
	// Passthrough, not capture: the measured command's output is the job log's,
	// and buffering it here would change what a consumer sees.
	//
	// The CONCRETE *os.File values are tested, not the assigned struct fields:
	// a nil *os.File stored in an io.Writer is a NON-nil interface holding a
	// nil pointer, so `cmd.Stdout == nil` would be false and the child's
	// output would go nowhere. This was measured the hard way — a wrapper that
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
	cmd.Env = os.Environ()
	ownProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return 1, nil, TerminalWrapperError, "start the child: " + err.Error()
	}

	// THE PROCESS IDENTITY IS RECORDED, not merely claimed.
	//
	// The closing record asserted in its note that the root had been reaped and
	// the group drained, while carrying no `proc` object at all: the named
	// result was declared and never assigned. QC7a requires a process-group
	// identity, so a genuinely emitted record could not satisfy the very check
	// it was written for.
	//
	// It is read HERE, while the child is alive, because that is the only time
	// it exists: after cmd.Wait reaps it there is no process left to ask.
	pgid, pgidErr := childProcessGroup(cmd)
	identity := &ProcIdentity{
		PID:       cmd.Process.Pid,
		PGID:      pgid,
		StartID:   processStartID(cmd.Process.Pid),
		ParentPID: os.Getpid(),
		UID:       os.Getuid(),
		GID:       os.Getgid(),
	}
	if pgidErr != nil {
		// A platform that cannot report the group is a stated limitation, not
		// a reason to record nothing: the pid and start identity are still
		// facts, and the absent pgid is what QC7a will read.
		identity.PGID = 0
	}
	proc = identity

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	// CANCELLATION FORWARDING is retained: a signal this process receives is
	// the operator or the runner asking the whole run to stop, and it has to
	// reach the child. Without it a TERM-ignoring child hangs the wrapper
	// until the deadline, which is exactly the shape the retained
	// cancellation regression drives.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)

	// Whether cmd.Wait has ALREADY returned decides two later things: the reap
	// the drain performs must not wait a second time on a channel that will
	// never deliver again, and the escape probe is only askable once the root
	// is out of its own group.
	rootReaped, rootWait := false, error(nil)

	termState, reason = TerminalPassed, ""
	select {
	case err := <-waitErr:
		rootReaped, rootWait = true, err
		code = exitCodeOf(err)
		if code != 0 {
			termState = TerminalFailed
			if err != nil {
				reason = err.Error()
			}
		}
		if ws, ok := exitStatusOf(cmd); ok && ws.Signaled() {
			identity.Signal = ws.Signal().String()
			termState, reason = TerminalSignalled, "child signalled with "+identity.Signal
		}
	case sig := <-sigs:
		code, termState = 1, TerminalCancelled
		reason = "cancelled by " + sig.String()
	case <-time.After(time.Until(deadline)):
		// The deadline is a real endpoint, not a suggestion: a child that
		// never exits must not hang the wrapper with no record.
		code, termState, reason = 1, TerminalCancelled, "the cancellation deadline passed"
	}

	// THE ROOT IS REAPED ON EVERY RETURN PATH.
	//
	// The teardown below is guarded on a usable PGID, and everything —
	// including the root reap — used to sit inside that guard. So a platform
	// or a moment where `childProcessGroup` failed, and every cancellation or
	// deadline return, left the process this wrapper had parented unwaited:
	// a zombie held by this process, while the record it then wrote said the
	// root had been reaped. The claim in the note has to be true on the path
	// that writes it.
	//
	// A kill is what makes the wait terminate on the cancelled paths: the
	// child is not going to exit on its own, and waiting for it without one
	// would hang the wrapper the deadline exists to protect.
	defer func() {
		if rootReaped {
			return
		}
		_ = cmd.Process.Kill()
		select {
		case err := <-waitErr:
			rootReaped, rootWait = true, err
		case <-time.After(reapGrace):
			// Reported, never silently absorbed: an unreaped root is exactly
			// the state QC7a and the escape rule are looking for.
			identity.ExitKind = TerminalCrashUnclosed
			reason = joinReason(reason, "the root was not reaped within "+reapGrace.String()+" of the wrapper's own kill")
		}
	}()

	// Teardown, and only then may the caller take the closing reading.
	if pgidErr == nil && pgid > 1 {
		// The graces come from the FROZEN CANCELLATION POLICY, not from the
		// drain's own defaults. CancellationPolicyID is derived from these
		// same two values, so a manifest cannot declare a policy the wrapper
		// does not implement — and a test that shortens the policy has to
		// actually shorten the escalation, which is what the retained
		// cancellation regressions check.
		// The ESCAPE PROBE is taken BEFORE the teardown signal, because the
		// signal would mask exactly what it asks. In the exited paths the root
		// has already been reaped and is out of its own group, so a group that
		// is still populated after a bounded settle holds a descendant that
		// outlived the process it belonged to. In the cancelled paths the
		// wrapper is itself about to kill the group, so members draining a
		// moment later are the tail of that kill and calling them an escape
		// would report a defect the wrapper caused — hiding the cancellation
		// that is the real terminal state. Not asking the question there is
		// the distinction, not an omission.
		escaped := rootReaped && groupProbeSupported() && !groupEmptyWithin(pgid, cancellationGrace)

		out, err := DrainGroup(DrainRequest{
			PGID: pgid, TermGrace: cancellationGrace, KillGrace: reapGrace,
			ImmediateKill: escaped,
			ReapRoot: func() error {
				// Reaping twice is not possible: when the select already took
				// the wait result, the reap is done and its outcome is
				// replayed rather than waited for again.
				if rootReaped {
					return rootWait
				}
				return <-waitErr
			},
		})
		if err != nil {
			reason = joinReason(reason, err.Error())
			if termState == TerminalPassed {
				termState = TerminalWrapperError
			}
		}
		switch {
		case escaped:
			// An escape is TERMINAL and is never rounded down to a finished
			// run: the envelope did not end where the closing record would
			// claim. What the forced kill changes is that the descendant no
			// longer survives the wrapper that was supposed to contain it —
			// so the reason names the escape AND what the reap achieved,
			// because "it escaped" and "it escaped and is still running" call
			// for different responses from whoever reads the receipt.
			termState = TerminalCrashUnclosed
			reason = joinReason(reason, "a descendant outlived its root: the process group was still populated after the root was reaped, and was then killed")
			if !out.GroupEmpty {
				reason = joinReason(reason, "the killed group was STILL not confirmed empty after "+reapGrace.String())
			}
		case out.Escalated:
			// A cancelled run that had to be KILLED and one that stopped when
			// asked are different facts, and the row keeps which happened.
			reason = joinReason(reason, "the process group was killed after the "+cancellationGrace.String()+" grace")
		}
		if lim := out.Limitation(); lim != "" {
			reason = joinReason(reason, lim)
		}
	}
	identity.ExitCode = code
	if identity.ExitKind == "" {
		identity.ExitKind = termState
	}
	return code, proc, termState, reason
}

// exitCodeOf preserves the child's exit status, which is the whole point of
// wrapping it: a measured run that lost its status would report a pass.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
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

// abandonReason renders a refusal to signal as text for the record. An
// observer that could not be ended safely is part of what happened, so it
// travels with the terminal reason rather than being dropped on the floor.
func abandonReason(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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

// membershipSnapshot returns the containment membership at close. The LIST is
// retained, not a count: "0 members" and "these are the members" answer
// different questions, and only the second one lets a reader check the first.

// credentialDropWait bounds how long the wrapper waits for the drop to become
// observable. It is a spawn-and-exec of one program; a second is generous, and
// the wait ends the moment the credential arrives.
const credentialDropWait = time.Second

func membershipSnapshot(cont Containment) ([]int, string) {
	pids, err := cont.Procs()
	if err != nil {
		return nil, "cgroup.procs unreadable: " + err.Error()
	}
	return pids, fmt.Sprintf("cgroup.procs members at close: %d", len(pids))
}

// THE PRIVATE SIGNING CAPABILITIES ARE GONE, and with them the observers.
//
// TB_WALL_AUTHORITY_KEY, _VERIFIER_KEY, _REPLAY_KEY, _BUILDER_KEY and
// _RUNNER_KEY declared the keys that approved Stage-1 inputs, signed verdicts,
// signed replay and build attestations, and let a fleet attest a host image.
// ObserverLauncher re-executed this binary as an independent containment peer
// and trace collector, handing each a signing key on descriptor 3. None of it
// had a production caller, and all of it belongs to the threat model the
// practical contract replaces with §3's trusted-CI boundary.
//
// The GitHub file-command scrubbing below STAYS, and it is not part of that
// machinery: it is least privilege for consumer-supplied code. `wall run`
// executes a setup command somebody else wrote, and a child that inherits
// $GITHUB_OUTPUT or $GITHUB_ENV can rewrite the measured step's outputs and
// the job's environment. That is true whether or not anything is signed.

// GitHubFileCommandEnv names the writable file channels an Actions step is
// handed. They are not secrets; they are something worse to inherit — paths to
// files a later step's capability is DELIVERED THROUGH.
//
// `wall begin` mints the signer delegate, returns it on stdout, and the
// composite step then appends it to $GITHUB_OUTPUT so exactly the measured
// step can name it. The two action observers are started BEFORE that append
// and are deliberately detached, so they outlive the step — and they were
// inheriting $GITHUB_OUTPUT. Scrubbing the delegate VALUE out of their
// environment while leaving them holding the path of the file it is about to
// be written to secures nothing: each observer kept same-uid read access to
// the exact channel, and an observer that can obtain the delegate can
// authorize a lower signer and vouch for itself, which is the one thing the
// delegation scope exists to prevent.
//
// So the channels go too. The handoff itself is untouched: the append happens
// in the composite step's OWN shell, which is not a scrubbed child, so the
// step output still reaches the measured step by the same narrow route.
// It is the CURRENT official set, not a plausible one. actions/runner v2.337.0
// (commit 397b032cbf865e9c3ddfab89d533ec19325e1273) exports `artifacts` and
// `artifacts_list` as GITHUB_ARTIFACTS and GITHUB_ARTIFACTS_LIST, and its
// FileCommandManager gives EVERY extension one shared GUID suffix — including
// `set_output_`. So an observer holding either artifacts path holds the suffix
// that names the output file, whether or not artifact processing is enabled.
// Listing the five obvious names and stopping was a defence against the
// channel one happens to think of.
var GitHubFileCommandEnv = []string{
	"GITHUB_OUTPUT",
	"GITHUB_ENV",
	"GITHUB_PATH",
	"GITHUB_STEP_SUMMARY",
	"GITHUB_STATE",
	"GITHUB_ARTIFACTS",
	"GITHUB_ARTIFACTS_LIST",
}

// scrubFileCommandSiblings removes any OTHER variable whose value is a file in
// the same directory as one of the named channels.
//
// The runner puts every file command in one directory and gives them one
// shared suffix, so a sibling path is the whole channel: from
// `.../_runner_file_commands/artifacts_<suffix>` the output file is
// `set_output_<suffix>` in the same directory. Enumerating names cannot keep
// up with a runner that adds an extension — v2.337.0 added two since this
// denylist was written — so the SHAPE is refused as well as the names.
//
// It is deliberately narrow: only variables whose value is a path in a
// directory some named channel also lives in. GITHUB_WORKSPACE,
// GITHUB_EVENT_PATH and the rest of the run identity live elsewhere and are
// untouched, and with no channel present it removes nothing at all.
func scrubFileCommandSiblings(env []string) []string {
	dirs := map[string]bool{}
	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		if !slices.Contains(GitHubFileCommandEnv, name) {
			continue
		}
		if d := filepath.Dir(value); filepath.IsAbs(value) && d != "." && d != string(filepath.Separator) {
			dirs[d] = true
		}
	}
	if len(dirs) == 0 {
		return env
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		if !slices.Contains(GitHubFileCommandEnv, name) && filepath.IsAbs(value) && dirs[filepath.Dir(value)] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// reapExitedChild collects an exited child without blocking, and reports
// whether this pid is now gone because of it.
//
// WNOHANG means a live child costs one syscall and answers false; a pid that
// is not our child answers ECHILD and also false, which leaves the callers
// that rely on init having reaped it exactly as they were.
func reapExitedChild(pid int) bool {
	var status syscall.WaitStatus
	got, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	if err != nil || got != pid {
		return false
	}
	// REAPED, SO THE PID IS THE KERNEL'S AGAIN. The retained handle must not
	// outlive the process it names, or the registry would go on vouching for a
	// number that has been released for reuse.
	forgetObserver(pid)
	return true
}

// terminalExec retains a wrapper-level failure with its reason and no
// duration.
func terminalExec(w *Writer, opt ExecOptions, spec *SpecIdentity, start Instant, clock Clock, state, reason string) error {
	_, _ = w.Append(Record{
		Kind: "terminal", Level: opt.Level,
		Source: SourceWrapper, Seqno: opt.Seq, Run: opt.Run, Instant: clock.Now(),
		Spec: spec, Terminal: state, Reason: reason,
	})
	return fmt.Errorf("walltime: %s", reason)
}

// scriptHandoffPath is where a script wrapper leaves its containment identity
// for the invocation wrappers its own script starts.
func scriptHandoffPath(dir string) string { return filepath.Join(dir, "script-containment.json") }

// ScriptHandoffKind identifies the handoff document.
const ScriptHandoffKind = "tb.walltime.script-handoff/v1"

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

func mustDigest(v any) Digest {
	d, err := DigestJSON(v)
	if err != nil {
		return ""
	}
	return d
}

// observerCloseBy is one observer's share of the closing budget: a bounded
// grace, never beyond the envelope's own deadline.
func observerCloseBy(deadline time.Time) time.Time {
	grace := time.Now().Add(observerCloseGrace)
	if grace.After(deadline) {
		return deadline
	}
	return grace
}

// THE OBSERVER REGISTRY: process handles this process is still holding.
//
// A detached observer is reconstructed by a LATER STEP from the action state,
// which can only carry numbers and strings. When that step runs in the same
// process that launched the observer — a single-step action, and every test —
// the live OS handle is still here, and it is a better answer than any pair of
// numbers: an unreaped child holds its pid, so the handle proves ownership
// instead of arguing for it.
//
// Across processes there is nothing to recover and the reconstruction falls
// back to the (pid, start) pair, which is why that pair is still recorded.
var observerHandles sync.Map // pid -> *os.Process

func rememberObserver(p *os.Process) {
	if p != nil {
		observerHandles.Store(p.Pid, p)
	}
}

// recallObserver returns the retained handle for a pid, if this process is the
// one that launched it and has not forgotten it.
func recallObserver(pid int) *os.Process {
	if pid <= 0 {
		return nil
	}
	if v, ok := observerHandles.Load(pid); ok {
		if p, ok := v.(*os.Process); ok {
			return p
		}
	}
	return nil
}

// forgetObserver drops a handle once its process has been reaped, so the
// registry cannot hand out ownership of a pid the kernel is free to reuse.
func forgetObserver(pid int) {
	observerHandles.Delete(pid)
}

// forgetObserverHandle drops a pid's entry only when the registry still holds
// THIS handle for it, so a stale object cannot evict the entry belonging to a
// different process that was registered at a recycled number.
func forgetObserverHandle(pid int, p *os.Process) {
	observerHandles.CompareAndDelete(pid, p)
}

// errNoChildProcess is returned when a process-group question is asked before
// the child exists.
var errNoChildProcess = errors.New("walltime: no child process")

// procOrZero renders an absent process identity as the zero value, so a
// terminal record can still be written when the spawn itself failed.
func procOrZero(p *ProcIdentity) ProcIdentity {
	if p == nil {
		return ProcIdentity{}
	}
	return *p
}

// exitStatusOf reads the child's wait status, once cmd.Wait has returned. It
// is a helper rather than an inline type assertion so the caller cannot read
// cmd.ProcessState on a path where the wait goroutine may still be writing it.
func exitStatusOf(cmd *exec.Cmd) (syscall.WaitStatus, bool) {
	if cmd.ProcessState == nil {
		return 0, false
	}
	ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	return ws, ok
}

// AbsCwd resolves a working directory to the ABSOLUTE, cleaned path §13.1
// makes the invocation's cwd identity.
//
// The plan renders `dir` relative to the repo root, and every side used to
// hash that relative string: the plan job, the bucket runner and the record
// job could each resolve "." under a different absolute root and QC7a would
// still pass, because it was comparing two copies of the same relative text.
// Resolving here means the digest is of the directory a command actually ran
// in — the jobs share a workspace path, so agreeing on it is a real check
// rather than a tautology, and disagreeing is now visible.
//
// An unresolvable path yields the cleaned input rather than an error: a cwd
// that cannot be made absolute is a finding for the checks downstream, not a
// reason for this helper to have no answer.
func AbsCwd(dir string) string {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	return filepath.Clean(abs)
}
