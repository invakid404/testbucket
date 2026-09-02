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
	Deadline   string `json:"deadline"`
	// StartedAt is the AT_start reading, repeated here only so a human reading
	// the file can find the record; the RECORD is the evidence.
	StartedAt Instant `json:"started_at"`
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

	cont, err := NewContainment(containmentName(ExecOptions{Level: LevelAction, Run: run}), nil)
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

	st := &ActionState{
		Schema: SchemaVersion, Dir: dir, Run: run, Containment: cont.Identity(),
		PeerControl: peer.ctl.base, TraceControl: trace.ctl.base,
		PeerPID: peer.pid, TracePID: trace.pid,
		PeerStart: peer.start, TraceStart: trace.start,
		Deadline: deadline.UTC().Format(time.RFC3339Nano), StartedAt: start,
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
	o.abandon()
	if o.stillRunning() {
		return fmt.Sprintf("the %s observer (pid %d) was signalled but had NOT exited", o.producer, o.pid)
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
	if err := cmd.Run(); err != nil {
		if cmd.ProcessState != nil {
			return cmd.ProcessState.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
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
	trace := &observerProc{producer: ProducerTrace, ctl: control{base: st.TraceControl},
		pid: st.TracePID, start: st.TraceStart,
		stream: filepath.Join(dir, streamName(ProducerTrace, LevelAction, 0))}
	peer := &observerProc{producer: ProducerPeer, ctl: control{base: st.PeerControl},
		pid: st.PeerPID, start: st.PeerStart,
		stream: filepath.Join(dir, streamName(ProducerPeer, LevelAction, 0))}
	if err := trace.close(deadline); err != nil && terminal == TerminalPassed {
		terminal, reason = TerminalWrapperError, err.Error()
	}
	if err := peer.close(deadline); err != nil && terminal == TerminalPassed {
		terminal, reason = TerminalWrapperError, err.Error()
	}

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
