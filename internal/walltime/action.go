package walltime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// actionStateFile is where `wall begin` leaves what `wall end` needs. It is
// not a cache: it is the only link between two Actions steps, so its absence
// at end time is a terminal wrapper error rather than a reason to guess an
// envelope.
const actionStateFile = "action-state.json"

// ActionState is the handoff between the two halves of the action envelope.
type ActionState struct {
	Schema       string              `json:"schema"`
	Dir          string              `json:"dir"`
	Run          RunIdentity         `json:"run"`
	Containment  ContainmentIdentity `json:"containment"`
	PeerControl  string              `json:"peer_control"`
	TraceControl string              `json:"trace_control"`
	// PeerPID and TracePID are the detached observers' process ids, and
	// PeerStart and TraceStart are those processes' START IDENTITIES.
	//
	// `wall begin` and `wall end` are two different steps, so the handles the
	// closing step reconstructs have no cmd. Without the pids, a lifecycle
	// that could not be completed there had nothing to kill: the observers
	// would outlive the action they were bracketing.
	//
	// The start identities are what make those numbers safe to use. A pid is
	// reused, and between the two steps it may have come to name the runner's
	// own work — so the closing step would signal a stranger, and would read
	// "that pid is gone" as "the observer exited". Recording the identity the
	// launching step read turns the pair into something that can be checked:
	// either this is still our observer, or there is nothing of ours here.
	PeerPID    int    `json:"peer_pid,omitempty"`
	TracePID   int    `json:"trace_pid,omitempty"`
	PeerStart  string `json:"peer_start,omitempty"`
	TraceStart string `json:"trace_start,omitempty"`
	// Root is the process that OPENED the envelope, retained as provenance
	// rather than as the action's measured root.
	//
	// It was carried so the closing step could copy it into its own records,
	// which made those records claim a process that had already exited as the
	// thing they had just observed. `wall begin` returns after writing this
	// handoff; the setup, bucket and closing steps are sibling step processes
	// that join the same containment. No process spans an action, so no record
	// may say one did — the containment spans it, and each record now names
	// the wrapper that actually took its reading.
	Root     ProcIdentity `json:"root"`
	Deadline string       `json:"deadline"`
	// StartedAt is the AT_start reading, repeated here only so a human reading
	// the file can find the record; the RECORD is the evidence.
	StartedAt Instant `json:"started_at"`

	// SignerDelegate is the delegate PRIVATE key this step minted, returned to
	// the caller in memory and never serialized.
	//
	// It has to reach the measured step — that is where the script and
	// invocation producers, and the action-owned children, mint the keys it
	// authorizes — and it must reach nothing else. Writing it into the
	// evidence directory put it where the measured script could read it and
	// where every observer is told to look; the caller places it in the
	// measured step's ENVIRONMENT instead, where the observer scrub removes it
	// and `sudo` strips it from the measured child.
	SignerDelegate string `json:"-"`
}

// retainActionProcessTree writes one process-tree record for the ACTION
// envelope, whose measured root is this wrapper process.
//
// The action level had no such record at all. Its root is not a spawned child
// but the wrapper itself — it joins the containment it created and everything
// the action owns is started underneath — so the identity read here is the one
// the whole envelope is accounted to.
func retainActionProcessTree(w *Writer, run RunIdentity, clock Clock, cont Containment, boundary string) {
	retainActionProcessTreeFor(w, run, clock, cont, boundary, actionRootIdentity(os.Getpid()))
}

// actionRootIdentity reads one process's identity. At the action level it is
// the identity of the wrapper that TOOK a reading, not of a root that spans
// the envelope: no process spans an action, and a record that named one was
// naming a process which had already exited.
func actionRootIdentity(pid int) ProcIdentity {
	parent := processParentOf(pid)
	if parent <= 0 {
		parent = os.Getppid()
	}
	id := ProcIdentity{
		PID: pid, PGID: processGroupOf(pid), StartID: processStartID(pid),
		SessionID: processSessionOf(pid), ParentPID: parent, UID: processUIDOf(pid),
	}
	id.GID, id.Groups = processGroupsOf(pid)
	return id
}

// retainActionProcessTreeFor writes one action process-tree record for a GIVEN
// observing identity.
//
// Each action record names the wrapper that took its reading. `wall begin`,
// the setup step, the bucket step and `wall end` are separate step processes
// that each join the same containment, so the honest subject of an action
// record is "this wrapper observed this membership at this instant". What
// spans the action is the containment, and the membership reads are the
// evidence for it.
func retainActionProcessTreeFor(w *Writer, run RunIdentity, clock Clock, cont Containment, boundary string, proc ProcIdentity) {
	rec := Record{
		Kind: "process_tree", Boundary: boundary,
		Role: RolePhysicalAction, Level: LevelAction,
		Source: SourceProcessLifecycle, Run: run,
		Containment: cont.Identity(), Instant: clock.Now(),
		Proc: proc,
	}
	ev, _, err := cont.Observe(string(ProducerPhysical))
	if err != nil {
		rec.Note = "cgroup.procs unreadable: " + err.Error()
	} else {
		rec.RawEventID, rec.RawEventDigest, rec.RawEventBytes = ev.ID, ev.Digest, ev.Bytes
		rec.RawProcs, rec.RawProcsBytes, rec.RawProcsDigest = ev.Procs, ev.ProcsBytes, ev.ProcsDigest
		rec.Note = fmt.Sprintf("cgroup.procs members at %s: %d", boundary, len(ev.Procs))
	}
	_, _ = w.Append(rec)
}

// observerTeardownNote says what was actually established about the observers,
// rather than asserting it.
//
// The closing record used to state "observers reaped" unconditionally. It was
// written whether or not either observer had exited — and for a detached
// observer nothing had even looked. A note is read as a finding by anyone
// auditing the ledger, so it may only say what this step proved.
func observerTeardownNote(peer, trace *observerProc) string {
	for _, o := range []*observerProc{peer, trace} {
		switch {
		case o.pid <= 0:
			// An older handoff, or one written before the pids were retained.
			// "Could not be confirmed" is the whole truth about it.
			return fmt.Sprintf("the %s observer's process identity was not recorded by the opening step, so its exit could not be confirmed here; ", o.producer)
		case o.stillRunning():
			return fmt.Sprintf("the %s observer (pid %d) had NOT exited when this reading was taken; ", o.producer, o.pid)
		}
	}
	return "both observers exited and were reaped, "
}

// BeginAction opens the complete physical action envelope: it records AT_start
// as its first owned operation, creates the action containment before any
// action-owned child can exist, and brings up the independent CPA peer and VTA
// collector — each in its own process, each with its own key.
//
// It deliberately does the expensive part (containment, two observer processes,
// two admission handshakes) INSIDE the envelope. That work is real action time;
// hiding it outside AT_start would make A smaller than the action actually was.
func BeginAction(dir string, run RunIdentity, timeout time.Duration) (*ActionState, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	// AT_start before ANYTHING else this process owns — before the records
	// directory, the signing key and the writer. Those are action-owned work,
	// and an envelope opened after them would report an action shorter than
	// the one that ran.
	//
	// The wrapper cannot read a clock before it exists, so under measurement
	// it is installed by the CALLER, before the measured action starts, and
	// `wall begin` is the action's first owned step. That ordering is the
	// control: there is no action-owned work left to precede this reading.
	//
	// The verifier REPORTS whatever still precedes it, from the GitHub step
	// attempt, and scores none of it — A_GH is frozen as a diagnostic that
	// never enters a success calculation, and eligibility is one.
	clock := NewSystemClock()
	start := clock.Now()
	probe(atStartReading, dir)

	// The operations BEFORE a writer exists still have to leave a record.
	//
	// A_start has already been read, so from here the action has begun; a
	// failure that returned silently would be indistinguishable from an action
	// that never started, which is exactly the retention the contract forbids
	// losing. Each of these is retained on a best-effort basis — the retention
	// path mints its own key and writer, so it can record a failure of the key
	// or the writer as long as the directory is writable, and when even that
	// is impossible the error is still returned rather than swallowed.
	preWriter := func(what string, err error) error {
		retainActionTerminal(dir, run, TerminalWrapperError, what+": "+err.Error())
		return fmt.Errorf("walltime: %s: %w", what, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// Nothing can be retained into a directory that does not exist, and
		// saying so is more useful than a bare mkdir error.
		return nil, fmt.Errorf("walltime: create the records directory (no terminal record can be retained without it): %w", err)
	}
	// THE EVIDENCE DIRECTORY IS APPEND-ONLY FOR THE SCRIPT ACCOUNT, AND THE
	// WRAPPER DOES NOT CHOWN IT.
	//
	// The measured bucket script runs as its own account and must create one
	// invocation spec per call here. It cannot be given general write access —
	// it would then be able to rewrite the evidence being attested — so what
	// it gets is CREATE and nothing else: group write and search, setgid so
	// new files carry the directory's group, and the STICKY bit so a file may
	// be removed or renamed only by its owner. The wrapper's ledgers stay 0644
	// and wrapper-owned.
	//
	// The group is INHERITED, not assigned. This used to call
	// `os.Chown(dir, -1, scriptGID)`, which cannot work: on Linux an
	// unprivileged owner may change a file's group only to a group it is
	// itself in, and the wrapper is deliberately not in the measured script's
	// group — putting it there would destroy the separation the chown exists
	// to serve. A setgid PARENT makes the kernel do it instead, so the caller
	// prepares the parent once, privileged, and the wrapper only sets the mode
	// on a directory it owns, which is always permitted.
	//
	// And it is VERIFIED rather than assumed: if a script account is declared
	// and the directory did not come out owned by that account's group, the
	// envelope refuses here instead of opening a run whose measured script
	// cannot write its first spec file.
	if err := prepareEvidenceDir(dir); err != nil {
		return nil, preWriter("prepare the evidence directory for the script account", err)
	}
	if err := probeErr(atRecordsDir, dir); err != nil {
		return nil, preWriter("prepare the records directory", err)
	}
	key, err := NewSigningKey()
	if err != nil {
		return nil, preWriter("mint the action signing key", err)
	}
	w, err := NewWriter(filepath.Join(dir, streamName(ProducerPhysical, LevelAction, 0)), ProducerPhysical, ProducerID(ProducerPhysical), key)
	if err != nil {
		return nil, preWriter("open the action record stream", err)
	}
	defer w.Close()
	runKey, err := RunKeyFromEnv()
	if err != nil {
		return nil, preWriter("read the run key", err)
	}

	// From here on every failure is RETAINED. A bootstrap that dies without a
	// record is indistinguishable from an action that never started, and the
	// contract is explicit that a failed setup stays in the ledger with its
	// reason rather than disappearing.
	// THE ROLLBACK GUARD.
	//
	// Everything this function starts before the state handoff exists is owned
	// by this function and by nobody else: the two DETACHED observers and the
	// action containment. Once the handoff is written, EndAction owns them.
	// In between there is a window — signing and writing the roster,
	// serialising and writing the state — where a failure used to return with
	// both observers still running and the containment still present, and
	// nothing downstream had a handle to either. They would watch the
	// containment for their whole timeout while the action reported that its
	// lifecycle never opened.
	//
	// rollback is armed as each resource is created and disarmed exactly once,
	// at the successful handoff. Its outcome is retained in the terminal
	// record: a cleanup that could not complete is a fact about the run, not a
	// detail to swallow.
	var rollback []func() string
	fail := func(reason string, cause error) (*ActionState, error) {
		var cleanup []string
		for i := len(rollback) - 1; i >= 0; i-- {
			if note := rollback[i](); note != "" {
				cleanup = append(cleanup, note)
			}
		}
		detail := reason + ": " + cause.Error()
		if len(cleanup) > 0 {
			detail += " [rollback: " + strings.Join(cleanup, "; ") + "]"
		}
		_, _ = w.Append(Record{
			Kind: "terminal", Role: RolePhysicalAction, Level: LevelAction,
			Source: SourceWrapper, Run: run, Instant: clock.Now(),
			Terminal: TerminalWrapperError, Reason: detail,
		})
		return nil, fmt.Errorf("walltime: %s: %w", reason, cause)
	}

	cont, err := NewContainmentAt(LevelAction, containmentName(ExecOptions{Level: LevelAction, Run: run}), nil)
	if err != nil {
		return fail("create the action containment", err)
	}
	rollback = append(rollback, func() string {
		if err := cont.Destroy(); err != nil {
			return "the action containment could not be destroyed: " + err.Error()
		}
		return "the action containment was destroyed"
	})
	if _, err := w.Append(Record{
		Kind: "boundary", Role: RolePhysicalAction, Level: LevelAction, Boundary: "start",
		Source: SourceWrapper, Run: run, Containment: cont.Identity(), Instant: start,
	}); err != nil {
		// THROUGH THE GUARD, like every other failure after the containment
		// exists. This returned directly, so the one failure that happens
		// immediately after the rollback is armed was the one failure that
		// bypassed it: the containment stayed, and nothing recorded that it
		// had. A guard with a hole next to where it is armed is not a guard.
		return fail("record the action start boundary", err)
	}

	deadline := time.Now().Add(timeout)
	opt := ExecOptions{Level: LevelAction, Dir: dir, Run: run}
	peer, err := startObserver(ProducerPeer, opt, cont.Identity(), deadline, true)
	if err != nil {
		return fail("start the containment peer", err)
	}
	rollback = append(rollback, func() string { return endObserver(peer) })
	trace, err := startObserver(ProducerTrace, opt, cont.Identity(), deadline, true)
	if err != nil {
		return fail("start the trace collector", err)
	}
	rollback = append(rollback, func() string { return endObserver(trace) })
	// BOTH detached observers end when either admission fails. Abandoning only
	// the other one left the one that failed running — and these are detached,
	// so nothing downstream inherits a handle to it: it would watch the action
	// containment for its whole timeout while the action reports that its
	// lifecycle never opened.
	if err := peer.admit(deadline); err != nil {
		return fail("admit the containment peer", err)
	}
	if err := trace.admit(deadline); err != nil {
		return fail("admit the trace collector", err)
	}

	// THE ACTION ROOT JOINS ITS OWN CONTAINMENT FIRST.
	//
	// The start snapshot named this wrapper as the action's measured root
	// while the containment was still empty, because nothing had been admitted
	// to it — a record asserting a root that its own membership read did not
	// contain. Joining here makes the read observe the process the record
	// names, and it is the same join every action-owned child inherits.
	if err := joinContainment(cont.Identity(), os.Getpid()); err != nil {
		return fail("admit the action root to its containment", err)
	}

	// THE ACTION'S OWN PROCESS-TREE EVIDENCE.
	//
	// The verifier requires it for every cgroup envelope, and the action
	// lifecycle emitted none — so once the containment model is scorable at
	// all, every action envelope would be WT-033 ineligible for want of a
	// record nothing wrote. The action's measured root is this wrapper
	// process, which joins the containment it created, so its identity is read
	// here while it plainly exists.
	retainActionProcessTree(w, run, clock, cont, "start")

	// The roster is sealed HERE, inside the envelope and before the measured
	// script exists, and it is signed with a key delivered only to this step.
	// That ordering is the control: after this point the set of keys that may
	// sign action-level evidence is fixed, and the step that runs the measured
	// work cannot extend it.
	roster := Roster{Kind: RosterKind, Run: run, Entries: []RosterEntry{
		{Producer: ProducerPhysical, Level: LevelAction, PublicKey: PublicKeyOf(key), Binary: SelfDigest()},
		{Producer: ProducerPeer, Level: LevelAction, PublicKey: peer.pub, Binary: SelfDigest()},
		{Producer: ProducerTrace, Level: LevelAction, PublicKey: trace.pub, Binary: SelfDigest()},
	}}
	if runKey != nil {
		if err := roster.Sign(run.CampaignID, runKey); err != nil {
			return fail("sign the signer roster", err)
		}
	}
	if err := WriteRoster(dir, roster); err != nil {
		return fail("write the signer roster", err)
	}
	// AND THE SIGNER DELEGATION, opened in the same step and with the same
	// key, for the same reason the roster is sealed here.
	//
	// The script and invocation producers mint their keys during the MEASURED
	// step, when no holder of the run key is left to vouch for them — so every
	// lower key was registered unauthorized and the verifier reported every
	// row ineligible for want of a capability nobody could hold. The delegate
	// is that capability, bound to this run and to the lower levels only, so
	// what it can do is exactly what the wrapper chain must do and nothing the
	// roster reserves to this step.
	delegate, err := OpenSignerDelegation(dir, run, runKey)
	if err != nil {
		return fail("open the signer delegation", err)
	}

	st := &ActionState{
		Schema: SchemaVersion, Dir: dir, Run: run, Containment: cont.Identity(),
		PeerControl: peer.ctl.base, TraceControl: trace.ctl.base,
		PeerPID: peer.pid, TracePID: trace.pid,
		PeerStart: peer.start, TraceStart: trace.start,
		Root:     actionRootIdentity(os.Getpid()),
		Deadline: deadline.UTC().Format(time.RFC3339Nano), StartedAt: start,
		// In memory only: `json:"-"` keeps it out of the file written below,
		// which the measured script can read the directory of.
		SignerDelegate: delegate,
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fail("serialise the action state", err)
	}
	if err := os.WriteFile(filepath.Join(dir, actionStateFile), b, 0o644); err != nil {
		return fail("write the action state handoff", err)
	}
	// DISARMED. The handoff exists, so EndAction owns the observers and the
	// containment from here.
	rollback = nil
	return st, nil
}

// endObserver terminates one observer this function started and PROVES it is
// gone, returning what it established.
//
// abandon kills and waits, so a handle that still owns its command is reaped
// here rather than left as a process nobody will collect. The note is retained
// because "the cleanup ran" and "the cleanup worked" are different facts, and
// only the second one may be asserted.
func endObserver(o *observerProc) string {
	// A REFUSAL IS PART OF WHAT HAPPENED. When no stable handle for the
	// observer was available, abandon signals nothing at all rather than
	// guessing at a pid, and the note has to say so — "the cleanup ran" and
	// "the cleanup was declined for safety" are different facts, and a reader
	// deciding whether anything is still watching the containment needs the
	// second one.
	refused := o.abandon()
	if o.stillRunning() {
		if refused != nil {
			return fmt.Sprintf("the %s observer (pid %d) is still running and was NOT signalled: %s", o.producer, o.pid, refused)
		}
		return fmt.Sprintf("the %s observer (pid %d) was signalled but had NOT exited", o.producer, o.pid)
	}
	if refused != nil {
		return fmt.Sprintf("the %s observer is gone, though it was not signalled: %s", o.producer, refused)
	}
	return fmt.Sprintf("the %s observer exited and was reaped", o.producer)
}

// RunInAction runs one action-owned command INSIDE the action containment,
// without giving it an envelope of its own.
//
// It is what a per-bucket setup command needs: that work is real action time
// and it must be inside the containment lifecycle the peer and the collector
// are bracketing, but it is not the bucket script and giving it a second
// script envelope would misname it. Its duration lands in the physical action
// prologue, where the Aeta registry forecasts it.
func RunInAction(dir string, argv []string, cwd string, stdout, stderr *os.File) (int, error) {
	return RunInActionWith(RunInActionOptions{Dir: dir, Argv: argv, Cwd: cwd, Stdout: stdout, Stderr: stderr})
}

// RunInActionOptions is one action-owned command and whether it CONTINUES THE
// WRAPPER CHAIN.
//
// That distinction is a capability boundary, not a convenience. Two very
// different commands run through this path: the bucket command, which is this
// tool's own wrapper starting the measured script and therefore needs the
// wall-time capabilities to do it, and the consumer-supplied SETUP command,
// which is somebody else's code and needs none of them.
type RunInActionOptions struct {
	Dir            string
	Argv           []string
	Cwd            string
	Stdout, Stderr *os.File
	// WrapperChain says this child is the wrapper chain continuing. It is
	// false by default, so a caller that does not think about it gets the
	// scrubbed environment.
	WrapperChain bool
}

// RunInActionWith is RunInAction with the wrapper-chain distinction made
// explicit.
func RunInActionWith(o RunInActionOptions) (int, error) {
	dir, argv, cwd, stdout, stderr := o.Dir, o.Argv, o.Cwd, o.Stdout, o.Stderr
	if len(argv) == 0 {
		return 1, fmt.Errorf("walltime: no command to run")
	}
	st, err := LoadActionState(dir)
	if err != nil {
		return 1, err
	}
	// Join BEFORE spawning. The child then inherits the containment, and so
	// does everything the child starts — which is the whole mechanism: a
	// process can move itself, and a child cannot retroactively admit the
	// parent that spawned it. Anything an already-running sibling did is
	// outside the lifecycle the peer and the trace observe, permanently.
	if err := joinContainment(st.Containment, os.Getpid()); err != nil {
		return 1, fmt.Errorf("walltime: join action containment: %w", err)
	}
	probe(atContainmentJoin, dir)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// THE CAPABILITIES DO NOT TRAVEL INTO CONSUMER-CONTROLLED CODE.
	//
	// `Cmd.Env` was nil here, which makes Go pass this process's whole
	// environment through — and the consumer-supplied setup command runs on
	// exactly this path. The signer delegate reached it, so caller-controlled
	// code ran holding the capability that decides which keys may attest a
	// run's evidence. The account selectors reached it too, and this path
	// performs no credential drop.
	//
	// The bucket command is the wrapper chain continuing and does need them:
	// the script-level wrapper it starts registers its own producers and its
	// controller registers the invocation ones. That case says so.
	if !o.WrapperChain {
		cmd.Env = scrubSecrets(nil)
	}
	// EVERY ACTION-OWNED CHILD is retained, because the contract requires
	// containment proof before every one of them and RunInAction may be called
	// more than once. The record is appended to the action stream with the
	// child's own identity, read while it is running.
	// THE PROOF IS COMMITTED BEFORE THE CHILD EXISTS.
	//
	// The child used to be started first and its record written afterwards, so
	// nothing committed preceded the execution the contract asks for
	// containment proof BEFORE. Every failure along the way — key, writer,
	// registration, observation, append — was also discarded, so an action
	// child could run with no retained proof at all and nothing said so.
	//
	// This writes the pre-spawn reading, and refuses to start the child if it
	// cannot: an action-owned child that cannot be accounted for is a child
	// that must not run, which is the same rule the measured admission
	// protocol applies one level down.
	child, err := openActionChild(dir, st, argv)
	if err != nil {
		return 1, fmt.Errorf("walltime: retain the containment proof before an action-owned child: %w", err)
	}
	if err := cmd.Start(); err != nil {
		child.close()
		return 1, err
	}
	// AND THE IDENTITY, once the child exists. Two records, because they state
	// two different things: what the containment held before the child, and
	// which process the wrapper then started inside it.
	if err := child.observe(cmd.Process.Pid, argv); err != nil {
		child.close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return 1, fmt.Errorf("walltime: retain the identity of an action-owned child: %w", err)
	}
	if err := child.close(); err != nil {
		// THE CHILD IS ALREADY RUNNING. Returning here left a started process
		// unreaped whenever the ledger close reported a real error — the one
		// post-Start path that did not terminate and wait. A lifecycle that
		// cannot be recorded still has to end.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return 1, fmt.Errorf("walltime: close the action-child ledger: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		if cmd.ProcessState != nil {
			return cmd.ProcessState.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

// actionChild is one action-owned child's ledger: its own stream, its own
// registered signer and its own record chain.
type actionChild struct {
	w    *Writer
	st   *ActionState
	cont Containment
	seq  int
}

// openActionChild opens that ledger and writes the containment proof that must
// precede the child.
//
// THE SIDE STREAM IS ITS OWN CHAIN, AND MUST BE ITS OWN STREAM IDENTITY.
//
// It used to be written to `physical_wrapper.action-child.jsonl` with the same
// producer, level and default Seqno 0 as the main action ledger, while
// starting its own record sequence and previous-hash chain. `ReadDir` reads
// every stream in the directory and groups records by producer/level/Seqno, so
// the two files were merged into one group whose second half chained to
// nothing — a terminal WT-002 on every action that ran a setup command. The
// sequence number is what distinguishes them: the action envelope's own ledger
// is 0 and each action-owned child takes the next, so the two are different
// streams to the reader as well as to the writer.
func openActionChild(dir string, st *ActionState, argv []string) (*actionChild, error) {
	seq := actionChildSeq(dir) + 1
	key, err := NewSigningKey()
	if err != nil {
		return nil, err
	}
	// REGISTERED, or the record is signed by a key the closed signer set
	// cannot attribute. A side stream that minted its own key and never
	// declared it left the one record proving an action-owned child existed as
	// the one record nobody could attribute.
	if err := RegisterKeyFor(dir, KeyLogEntry{
		Producer: ProducerPhysical, Level: LevelAction, Seq: seq,
		PublicKey: PublicKeyOf(key), Binary: SelfDigest(),
	}, st.Run); err != nil {
		return nil, err
	}
	w, err := NewWriter(filepath.Join(dir, streamName(ProducerPhysical, LevelAction, seq)), ProducerPhysical, ProducerID(ProducerPhysical), key)
	if err != nil {
		return nil, err
	}
	cont, err := AttachContainment(st.Containment)
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	c := &actionChild{w: w, st: st, cont: cont, seq: seq}
	if err := c.append("before", 0, "action-owned child about to start: "+strings.Join(argv, " ")); err != nil {
		_ = w.Close()
		return nil, err
	}
	return c, nil
}

// observe records the child's identity while it is running.
func (c *actionChild) observe(pid int, argv []string) error {
	return c.append("observed", pid, "action-owned child: "+strings.Join(argv, " "))
}

func (c *actionChild) close() error { return c.w.Close() }

// append writes one action-child record with the containment proof the
// contract asks for: the exact kernel bytes, read while the claim is true.
func (c *actionChild) append(boundary string, pid int, note string) error {
	rec := Record{
		Kind: "action_child", Boundary: boundary,
		Role: RolePhysicalAction, Level: LevelAction, Seqno: c.seq,
		Source: SourceProcessLifecycle, Run: c.st.Run, Containment: c.st.Containment,
		Instant: NewSystemClock().Now(), Note: note,
	}
	if pid > 0 {
		proc := ProcIdentity{
			PID: pid, PGID: processGroupOf(pid), StartID: processStartID(pid),
			SessionID: processSessionOf(pid), ParentPID: os.Getpid(),
			UID: processUIDOf(pid),
		}
		proc.GID, proc.Groups = processGroupsOf(pid)
		rec.Proc = proc
	}
	ev, _, err := c.cont.Observe(string(ProducerPhysical))
	if err != nil {
		return fmt.Errorf("read the action containment: %w", err)
	}
	rec.RawEventID, rec.RawEventDigest, rec.RawEventBytes = ev.ID, ev.Digest, ev.Bytes
	rec.RawProcs, rec.RawProcsBytes, rec.RawProcsDigest = ev.Procs, ev.ProcsBytes, ev.ProcsDigest
	rec.Note += fmt.Sprintf("; cgroup.procs members at this reading: %d", len(ev.Procs))
	_, err = c.w.Append(rec)
	return err
}

// actionChildSeq numbers each action-owned child, so two of them do not claim
// one producer role.
func actionChildSeq(dir string) int {
	logged, _, err := ReadKeyLog(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range logged {
		if e.Level == LevelAction {
			n++
		}
	}
	return n
}

// retainActionTerminal appends a terminal record to the action stream on a
// best-effort basis. It is best-effort because the alternative — refusing to
// report a failure because reporting it also failed — retains nothing at all.
func retainActionTerminal(dir string, run RunIdentity, state, reason string) {
	key, err := NewSigningKey()
	if err != nil {
		return
	}
	w, err := NewWriter(filepath.Join(dir, streamName(ProducerPhysical, LevelAction, 0)), ProducerPhysical, ProducerID(ProducerPhysical), key)
	if err != nil {
		return
	}
	defer w.Close()
	_ = RegisterKey(dir, KeyLogEntry{
		Producer: ProducerPhysical, Level: LevelAction,
		PublicKey: PublicKeyOf(key), Binary: SelfDigest(),
	})
	_, _ = w.Append(Record{
		Kind: "terminal", Role: RolePhysicalAction, Level: LevelAction,
		Source: SourceWrapper, Run: run, Instant: NewSystemClock().Now(),
		Terminal: state, Reason: reason,
	})
}

// LoadActionState reads the handoff `wall begin` left behind.
func LoadActionState(dir string) (*ActionState, error) {
	b, err := os.ReadFile(filepath.Join(dir, actionStateFile))
	if err != nil {
		return nil, fmt.Errorf("walltime: no action envelope in %s: %w", dir, err)
	}
	var st ActionState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("walltime: action state: %w", err)
	}
	if st.Schema != SchemaVersion {
		return nil, fmt.Errorf("walltime: action state schema %q, want %q", st.Schema, SchemaVersion)
	}
	return &st, nil
}

// EndAction closes the envelope: it waits for the action containment to be
// verified empty, lets the collector close and then the peer, and records
// AT_end after the final epilogue.
//
// terminal is the outcome the action itself reached (a failed bucket is still
// a complete measurement of a failed bucket); an unclosable containment
// overrides it, because an escaped descendant means the envelope did not end
// where the record would claim.
func EndAction(dir string, terminal, reason string) (*ActionState, error) {
	st, err := LoadActionState(dir)
	if err != nil {
		return nil, err
	}
	deadline, err := time.Parse(time.RFC3339Nano, st.Deadline)
	if err != nil || time.Now().After(deadline) {
		deadline = time.Now().Add(2 * time.Minute)
	}
	clock := NewSystemClock()
	key, err := NewSigningKey()
	if err != nil {
		return st, err
	}
	w, err := NewWriter(filepath.Join(dir, streamName(ProducerPhysical, LevelAction, 0)), ProducerPhysical, ProducerID(ProducerPhysical), key)
	if err != nil {
		return st, err
	}
	defer w.Close()
	if err := RegisterKey(dir, KeyLogEntry{
		Producer: ProducerPhysical, Level: LevelAction,
		PublicKey: PublicKeyOf(key), Binary: SelfDigest(),
	}); err != nil {
		return st, err
	}

	cont, err := AttachContainment(st.Containment)
	if err != nil {
		return st, err
	}
	if terminal == "" {
		terminal = TerminalPassed
	}
	// THE CLOSER STANDS OUTSIDE THE CONTAINMENT IT DRAINS.
	//
	// This step used to join the action containment before reading it, so that
	// its reading would be taken from inside the lifecycle it closes. That is
	// a contradiction: `enforceContainmentEmpty` waits for the containment to
	// become empty and then SIGKILLs every member, and a closer that has made
	// itself a member can never see it empty — at the deadline it kills
	// itself, and the drained read, the end boundary, the seal and the cleanup
	// it claims to perform never happen.
	//
	// So the closer does not join, and the verifier requires exactly that: the
	// closing readings must be taken by a process the membership does NOT
	// contain, because a drain measured by one of its own members measures
	// nothing.
	retainActionProcessTree(w, st.Run, clock, cont, "observed")

	if reaped, emptyErr := enforceContainmentEmpty(cont, deadline); emptyErr != nil {
		// Same rule as the exec path: a cancelled action whose containment the
		// wrapper itself killed and then verified empty is cancelled, not
		// escaped. Anything else — including a kill that did not empty it — is
		// terminal crash_unclosed.
		if terminal == TerminalCancelled && reaped {
			reason = joinReason(reason, "the containment was verified empty after the cancellation kill: "+emptyErr.Error())
		} else {
			terminal, reason = TerminalCrashUnclosed, emptyErr.Error()
		}
	}
	// Reconstructed WITH the process identities the opening step recorded — the
	// pid AND the start identity — so a close that cannot be completed can end
	// the observer rather than leaving it to watch a containment that no
	// longer exists, and so it can only ever end THAT process.
	//
	// AND WITH THE LIVE HANDLE where this is still the process that launched
	// them. The state can only carry numbers; the registry can return the OS
	// handle itself, which proves ownership rather than arguing for it from a
	// pid that the kernel is entitled to reuse.
	trace := &observerProc{producer: ProducerTrace, ctl: control{base: st.TraceControl},
		pid: st.TracePID, start: st.TraceStart, proc: recallObserver(st.TracePID),
		stream: filepath.Join(dir, streamName(ProducerTrace, LevelAction, 0))}
	peer := &observerProc{producer: ProducerPeer, ctl: control{base: st.PeerControl},
		pid: st.PeerPID, start: st.PeerStart, proc: recallObserver(st.PeerPID),
		stream: filepath.Join(dir, streamName(ProducerPeer, LevelAction, 0))}
	// EACH OBSERVER GETS ITS OWN BOUNDED SHARE OF THE CLOSING BUDGET.
	//
	// Both teardowns used the one deadline, so the first could spend all of
	// it: the second was then asked to close with nothing left, its closing
	// record was never collected, and the envelope went terminal for a missing
	// endpoint belonging to the observer that had not been given a chance.
	// Bounding each separately reports a stuck observer as itself and still
	// proves the other. The overall deadline continues to bound the whole
	// close — this only stops one step from consuming it.
	for _, o := range []*observerProc{trace, peer} {
		if err := o.close(observerCloseBy(deadline)); err != nil && terminal == TerminalPassed {
			terminal, reason = TerminalWrapperError, err.Error()
		}
	}

	// The DRAINED read, after the containment empties.
	retainActionProcessTree(w, st.Run, clock, cont, "end")

	// The epilogue — destroying the containment and removing the handoff — runs
	// BEFORE the closing reading, because it is action-owned work and the
	// contract puts AT_end after the final epilogue, not before it. What
	// remains outside is one record write: the ledger closing itself.
	_ = cont.Destroy()
	_ = os.Remove(filepath.Join(dir, actionStateFile))
	probe(atEndReading, dir)
	if _, err := w.Append(Record{
		Kind: "boundary", Role: RolePhysicalAction, Level: LevelAction, Boundary: "end",
		Source: SourceWrapper, Run: st.Run, Containment: st.Containment,
		Instant: clock.Now(), Terminal: terminal, Reason: reason,
		Note: observerTeardownNote(peer, trace) + "containment destroyed and the action-state handoff removed before this reading; only this record's own write and the ledger seal follow it",
	}); err != nil {
		return st, err
	}
	// The seal comes AFTER the closing record so it covers that record too.
	// Both are the ledger closing itself rather than action work: no measured
	// process exists at this point, and a seal that omitted AT_end would leave
	// the one record that defines the end of A unfixed.
	if err := w.Close(); err != nil {
		return st, err
	}
	if err := sealDirectory(dir, st.Run); err != nil {
		return st, err
	}
	return st, nil
}

// sealDirectory writes the closing attestation over every stream and the key
// log. An unsigned seal is still written: it fixes the bytes for a reader, and
// the verifier reports the missing signature rather than silently accepting
// it.
func sealDirectory(dir string, run RunIdentity) error {
	runKey, err := RunKeyFromEnv()
	if err != nil {
		return err
	}
	streams, err := SealStreams(dir)
	if err != nil {
		return err
	}
	_, keyLog, err := ReadKeyLog(dir)
	if err != nil {
		return err
	}
	rosterDigest := Digest("")
	if r, err := ReadRoster(dir); err == nil {
		if d, err := r.DigestOf(); err == nil {
			rosterDigest = d
		}
	}
	seal := Seal{Kind: SealKind, Run: run, RosterDigest: rosterDigest, KeyLogDigest: keyLog, Streams: streams}
	if runKey != nil {
		if err := seal.Sign(run.CampaignID, runKey); err != nil {
			return err
		}
	}
	return WriteSeal(dir, seal)
}
