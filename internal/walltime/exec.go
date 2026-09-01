package walltime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// DefaultTimeout bounds every wait this package performs: the observer
// handshakes and the wait for verified-empty containment. It is generous
// because a slow runner is not a defect, and it is FINITE because a lifecycle
// that never closes must become a terminal record rather than a hung job.
const DefaultTimeout = 30 * time.Minute

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

	// Parent, when set, is a containment this wrapper joins before creating
	// its own: a script wrapper joins the action containment so its work is
	// inside A, and an invocation wrapper joins the script containment.
	Parent *ContainmentIdentity

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
	clock := NewSystemClock()
	key, err := NewSigningKey()
	if err != nil {
		return 1, err
	}
	w, err := NewWriter(filepath.Join(opt.Dir, streamName(ProducerPhysical, opt.Level, opt.Seq)), ProducerPhysical, ProducerID(ProducerPhysical), key)
	if err != nil {
		return 1, err
	}
	defer w.Close()

	spec := &SpecIdentity{
		ArgvDigest:     mustDigest(opt.Argv),
		Cwd:            opt.Cwd,
		SelectorDigest: mustDigest(opt.Selector),
		UnitDigest:     opt.UnitDigest,
		AtomDigest:     opt.AtomDigest,
		Desc:           opt.Desc,
	}

	// AT_start / VB_start / V_start: the wrapper's FIRST owned operation. Every
	// containment, peer and observer cost after this point is inside the
	// physical envelope, which is where the contract puts it.
	start := clock.Now()

	// Joining the parent containment before doing anything else is what makes
	// this wrapper's own work — not just its child's — part of the enclosing
	// envelope's containment lifecycle.
	if opt.Parent != nil {
		if err := joinContainment(*opt.Parent, os.Getpid()); err != nil {
			return 1, terminalExec(w, opt, spec, start, clock, TerminalWrapperError, "join parent containment: "+err.Error())
		}
	}

	cont, err := NewContainment(containmentName(opt))
	if err != nil {
		return 1, terminalExec(w, opt, spec, start, clock, TerminalWrapperError, "create containment: "+err.Error())
	}
	defer cont.Destroy()

	if _, err := w.Append(Record{
		Kind: "boundary", Role: roleOrPanic(ProducerPhysical, opt.Level), Level: opt.Level,
		Boundary: "start", Source: SourceWrapper, Seqno: opt.Seq,
		Run: opt.Run, Containment: cont.Identity(), Instant: start, Spec: spec,
	}); err != nil {
		return 1, err
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
	if err := peer.admit(deadline); err != nil {
		trace.abandon()
		return 1, terminalExec(w, opt, spec, start, clock, TerminalWrapperError, err.Error())
	}
	if err := trace.admit(deadline); err != nil {
		peer.close(deadline)
		return 1, terminalExec(w, opt, spec, start, clock, TerminalWrapperError, err.Error())
	}

	code, proc, termState, reason := runChild(opt, cont)

	// The wrapper's own view of emptiness is physical suffix work; the
	// observers still take their own reads, and the VERIFIER decides closure.
	emptyErr := waitContainmentEmpty(cont, deadline)

	// Trace closes first, then the peer: the resulting endpoint order is the
	// contract's, and it is established by the protocol rather than asserted
	// after the fact.
	traceErr := trace.close(deadline)
	peerErr := peer.close(deadline)

	if _, err := w.Append(Record{
		Kind: "process_tree", Role: roleOrPanic(ProducerPhysical, opt.Level), Level: opt.Level,
		Source: SourceProcessLifecycle, Seqno: opt.Seq, Run: opt.Run,
		Containment: cont.Identity(), Proc: proc, Instant: clock.Now(),
		Note: membershipNote(cont),
	}); err != nil {
		return code, err
	}

	if emptyErr != nil {
		termState, reason = TerminalCrashUnclosed, emptyErr.Error()
	}
	for _, e := range []error{traceErr, peerErr} {
		if e != nil && termState == TerminalPassed {
			termState, reason = TerminalWrapperError, e.Error()
		}
	}

	if _, err := w.Append(Record{
		Kind: "boundary", Role: roleOrPanic(ProducerPhysical, opt.Level), Level: opt.Level,
		Boundary: "end", Source: SourceWrapper, Seqno: opt.Seq, Run: opt.Run,
		Containment: cont.Identity(), Instant: clock.Now(), Spec: spec,
		Proc: proc, Terminal: termState, Reason: reason,
	}); err != nil {
		return code, err
	}
	return code, nil
}

// runChild starts the command INSIDE the containment and waits for it. The
// child is created in the containment (never moved into it afterwards), so
// there is no window in which action-owned work exists outside the lifecycle
// the peer and trace are bracketing.
func runChild(opt ExecOptions, cont Containment) (int, ProcIdentity, string, string) {
	cmd := exec.Command(opt.Argv[0], opt.Argv[1:]...)
	cmd.Dir = opt.Cwd
	cmd.Stdin, cmd.Stdout, cmd.Stderr = opt.Stdin, opt.Stdout, opt.Stderr
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
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
		return 1, ProcIdentity{PID: cmd.Process.Pid}, TerminalSpawnError, "admit child: " + err.Error()
	}

	cancelled := ""
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	for waitErr == nil {
		select {
		case s := <-sigs:
			cancelled = s.String()
			_ = cont.Signal(syscall.SIGTERM)
		case waitErr = <-done:
			if waitErr == nil {
				waitErr = errNoError
			}
		}
	}
	if waitErr == errNoError {
		waitErr = nil
	}

	proc := ProcIdentity{
		PID:       cmd.Process.Pid,
		PGID:      processGroupOf(cmd.Process.Pid),
		StartID:   processStartID(cmd.Process.Pid),
		ParentPID: os.Getpid(),
	}
	code := 0
	state := TerminalPassed
	reason := ""
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
	}
	if waitErr != nil && state == TerminalPassed {
		state, reason = TerminalWrapperError, waitErr.Error()
	}
	return code, proc, state, reason
}

// errNoError distinguishes "Wait returned nil" from "Wait has not returned" in
// the select above without a second channel.
var errNoError = fmt.Errorf("ok")

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

func membershipNote(cont Containment) string {
	pids, err := cont.Procs()
	if err != nil {
		return "cgroup.procs unreadable: " + err.Error()
	}
	return fmt.Sprintf("cgroup.procs members at close: %d", len(pids))
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
	ctl      control
	stream   string
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
		"--key", EncodeKey(key),
		"--timeout", time.Until(deadline).String(),
	}
	cmd, err := ObserverLauncher(args)
	if err != nil {
		return nil, err
	}
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
		return nil, err
	}
	logf.Close()
	return &observerProc{producer: p, cmd: cmd, ctl: control{base: base}, stream: filepath.Join(opt.Dir, streamName(p, opt.Level, opt.Seq))}, nil
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
