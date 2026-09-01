package walltime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	Deadline     string              `json:"deadline"`
	// StartedAt is the AT_start reading, repeated here only so a human reading
	// the file can find the record; the RECORD is the evidence.
	StartedAt Instant `json:"started_at"`
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
	// `wall begin` is the action's first owned step. What remains before this
	// reading is the runner's own step startup, which the verifier bounds
	// against A_GH's one-second resolution rather than merely reporting: an
	// unbounded prefix would mean A measured a different product.
	clock := NewSystemClock()
	start := clock.Now()
	probe(atStartReading, dir)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	key, err := NewSigningKey()
	if err != nil {
		return nil, err
	}
	w, err := NewWriter(filepath.Join(dir, streamName(ProducerPhysical, LevelAction, 0)), ProducerPhysical, ProducerID(ProducerPhysical), key)
	if err != nil {
		return nil, err
	}
	defer w.Close()
	runKey, err := RunKeyFromEnv()
	if err != nil {
		return nil, err
	}

	// From here on every failure is RETAINED. A bootstrap that dies without a
	// record is indistinguishable from an action that never started, and the
	// contract is explicit that a failed setup stays in the ledger with its
	// reason rather than disappearing.
	fail := func(reason string, cause error) (*ActionState, error) {
		_, _ = w.Append(Record{
			Kind: "terminal", Role: RolePhysicalAction, Level: LevelAction,
			Source: SourceWrapper, Run: run, Instant: clock.Now(),
			Terminal: TerminalWrapperError, Reason: reason + ": " + cause.Error(),
		})
		return nil, fmt.Errorf("walltime: %s: %w", reason, cause)
	}

	cont, err := NewContainment(containmentName(ExecOptions{Level: LevelAction, Run: run}), nil)
	if err != nil {
		return fail("create the action containment", err)
	}
	if _, err := w.Append(Record{
		Kind: "boundary", Role: RolePhysicalAction, Level: LevelAction, Boundary: "start",
		Source: SourceWrapper, Run: run, Containment: cont.Identity(), Instant: start,
	}); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	opt := ExecOptions{Level: LevelAction, Dir: dir, Run: run}
	peer, err := startObserver(ProducerPeer, opt, cont.Identity(), deadline, true)
	if err != nil {
		return fail("start the containment peer", err)
	}
	trace, err := startObserver(ProducerTrace, opt, cont.Identity(), deadline, true)
	if err != nil {
		peer.abandon()
		return fail("start the trace collector", err)
	}
	if err := peer.admit(deadline); err != nil {
		trace.abandon()
		return fail("admit the containment peer", err)
	}
	if err := trace.admit(deadline); err != nil {
		peer.abandon()
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
		Deadline: deadline.UTC().Format(time.RFC3339Nano), StartedAt: start,
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fail("serialise the action state", err)
	}
	if err := os.WriteFile(filepath.Join(dir, actionStateFile), b, 0o644); err != nil {
		return fail("write the action state handoff", err)
	}
	return st, nil
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
	if emptyErr := waitContainmentEmpty(cont, deadline); emptyErr != nil {
		terminal, reason = TerminalCrashUnclosed, emptyErr.Error()
	}
	trace := &observerProc{producer: ProducerTrace, ctl: control{base: st.TraceControl},
		stream: filepath.Join(dir, streamName(ProducerTrace, LevelAction, 0))}
	peer := &observerProc{producer: ProducerPeer, ctl: control{base: st.PeerControl},
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
		Note: "observers reaped, containment destroyed and the action-state handoff removed before this reading; only this record's own write and the ledger seal follow it",
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
