package walltime

import (
	"encoding/json"
	"fmt"
	"os"
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	clock := NewSystemClock()
	key, err := NewSigningKey()
	if err != nil {
		return nil, err
	}
	w, err := NewWriter(filepath.Join(dir, streamName(ProducerPhysical, LevelAction, 0)), ProducerPhysical, ProducerID(ProducerPhysical), key)
	if err != nil {
		return nil, err
	}
	defer w.Close()

	start := clock.Now()
	cont, err := NewContainment(containmentName(ExecOptions{Level: LevelAction, Run: run}))
	if err != nil {
		return nil, err
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
		return nil, err
	}
	trace, err := startObserver(ProducerTrace, opt, cont.Identity(), deadline, true)
	if err != nil {
		peer.abandon()
		return nil, err
	}
	if err := peer.admit(deadline); err != nil {
		trace.abandon()
		return nil, err
	}
	if err := trace.admit(deadline); err != nil {
		peer.abandon()
		return nil, err
	}

	st := &ActionState{
		Schema: SchemaVersion, Dir: dir, Run: run, Containment: cont.Identity(),
		PeerControl: peer.ctl.base, TraceControl: trace.ctl.base,
		Deadline: deadline.UTC().Format(time.RFC3339Nano), StartedAt: start,
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, actionStateFile), b, 0o644); err != nil {
		return nil, err
	}
	return st, nil
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
	if _, err := w.Append(Record{
		Kind: "boundary", Role: RolePhysicalAction, Level: LevelAction, Boundary: "end",
		Source: SourceWrapper, Run: st.Run, Containment: st.Containment,
		Instant: clock.Now(), Terminal: terminal, Reason: reason,
	}); err != nil {
		return st, err
	}
	_ = cont.Destroy()
	return st, nil
}
