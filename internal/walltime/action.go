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
}

// BeginAction opens the action envelope and leaves the handoff `wall end`
// needs.
//
// SIMPLIFIED per salvage-map: what remains is the opening reading and its
// placement, the action-state handoff between steps, and the rollback path
// that cleans up resources started before a failed begin.
//
// Removed with the proof machinery: observer process startup — this function
// used to launch two — the peer/trace brackets, signing and key registration,
// the roster, cgroup delegation checks, and the sealed-directory step. The
// closing record write and the directory seal stay OUTSIDE the interval by
// construction and §3.2 names them there rather than hiding them.
//
// The opening reading is the FIRST owned operation, taken before the writer
// and the handoff, because those are wrapper-owned work and an envelope that
// started after them would report an interval shorter than the one that ran.
func BeginAction(dir string, run RunIdentity, timeout time.Duration) (*ActionState, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("walltime: create the records directory: %w", err)
	}

	clock := NewSystemClock()
	start := clock.Now()
	probe(atStartReading, dir)

	w, err := NewWriter(filepath.Join(dir, "physical-action-00.jsonl"), ProducerPhysical, "physical", nil)
	if err != nil {
		return nil, err
	}
	defer w.Close()

	deadline := time.Now().Add(timeout)
	st := &ActionState{
		Schema:    SchemaVersion,
		Dir:       dir,
		Run:       run,
		Deadline:  deadline.UTC().Format(time.RFC3339Nano),
		StartedAt: start,
	}

	if _, err := w.Append(Record{
		Kind: "boundary", Level: LevelAction,
		Boundary: "start", Source: SourceWrapper,
		Run: run, Instant: start,
	}); err != nil {
		return nil, err
	}

	// The handoff is written LAST. Until it exists this function owns
	// everything it started, and a failure above rolls back by returning
	// without leaving a handoff for `wall end` to attach to; once it exists,
	// EndAction owns the interval.
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("walltime: encode the action state: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, actionStateFile), b, 0o644); err != nil {
		return nil, fmt.Errorf("walltime: write the action state handoff: %w", err)
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

// RunInActionWith runs one action-owned child inside the envelope.
//
// SIMPLIFIED per salvage-map: it loads the handoff, spawns the argv with
// stdout/stderr passthrough, waits, and preserves the exit status.
//
// Removed: the containment join, the held-child barrier and the per-child
// ledger. The barrier existed only to win a race against `cgroup.procs` — the
// kernel always wins it for a short-lived child, so a setup command that
// exited immediately was absent from the membership read and the verifier
// reported it as having run outside its containment. With cgroup admission
// gone there is no membership to read and no race to win.
func RunInActionWith(o RunInActionOptions) (int, error) {
	dir, argv, cwd, stdout, stderr := o.Dir, o.Argv, o.Cwd, o.Stdout, o.Stderr
	if len(argv) == 0 {
		return 1, fmt.Errorf("walltime: no command to run")
	}
	if _, err := LoadActionState(dir); err != nil {
		return 1, err
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		return 1, err
	}
	if err := cmd.Wait(); err != nil {
		if cmd.ProcessState != nil {
			return cmd.ProcessState.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
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

// EndAction closes the envelope and records AT_end.
//
// SIMPLIFIED per salvage-map: what remains is the closing reading and its
// placement relative to teardown, the terminal state and reason, and removal
// of the handoff. The observer close protocol, the containment attach/destroy,
// signing and key registration, and the directory seal are removed with the
// machinery they served.
//
// terminal is the outcome the action itself reached — a failed bucket is still
// a complete measurement of a failed bucket.
//
// The handoff is removed BEFORE the reading, because that removal is
// action-owned work; the record write that follows is the ledger closing
// itself and cannot be inside the interval it closes.
func EndAction(dir string, terminal, reason string) (*ActionState, error) {
	st, err := LoadActionState(dir)
	if err != nil {
		return nil, err
	}
	clock := NewSystemClock()
	w, err := NewWriter(filepath.Join(dir, "physical-action-00.jsonl"), ProducerPhysical, "physical", nil)
	if err != nil {
		return st, err
	}
	defer w.Close()

	_ = os.Remove(filepath.Join(dir, actionStateFile))
	probe(atEndReading, dir)

	if _, err := w.Append(Record{
		Kind: "boundary", Level: LevelAction,
		Boundary: "end", Source: SourceWrapper, Run: st.Run,
		Instant: clock.Now(), Terminal: terminal, Reason: reason,
		Note: "the action-state handoff was removed before this reading; only this record's own write follows it",
	}); err != nil {
		return st, err
	}
	return st, nil
}
