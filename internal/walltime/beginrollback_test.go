package walltime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestBeginActionCleansUpEverythingItStarted is the F6 regression.
//
// BeginAction starts two DETACHED observers, admits them, then signs and
// writes the roster and the action state. A failure in that last stretch
// returned an error with both observers still running and the containment
// still present — and because they are detached, nothing downstream inherited
// a handle to either. They would watch the action containment for their whole
// timeout while the action reported that its lifecycle never opened.
//
// The failure is forced the way the contract's own ordering makes possible:
// WriteRoster uses O_EXCL, so a pre-existing roster path fails only AFTER both
// observers have started and completed their admission handshakes.
func TestBeginActionCleansUpEverythingItStarted(t *testing.T) {
	t.Setenv(RunKeyEnv, EncodeKey(mustSigningKey()))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, rosterFile), []byte("collision"), 0o644); err != nil {
		t.Fatal(err)
	}

	original := ObserverLauncher
	var launched []*exec.Cmd
	ObserverLauncher = func(args []string) (*exec.Cmd, error) {
		cmd, err := original(args)
		if err == nil {
			launched = append(launched, cmd)
		}
		return cmd, err
	}
	t.Cleanup(func() {
		ObserverLauncher = original
		for _, cmd := range launched {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
			}
		}
	})

	run := RunIdentity{CampaignID: "ewj2", RunID: "post-admit-failure", BucketID: "bucket-0", Stage2: "sha256:stage2"}
	if _, err := BeginAction(dir, run, 30*time.Second); err == nil {
		t.Fatal("BeginAction succeeded despite a roster path it could not write")
	}
	if len(launched) != 2 {
		t.Fatalf("the failure happened before the boundary this test is about: %d observer(s) launched, want 2", len(launched))
	}
	// Both are gone AND reaped. Signal 0 succeeds on a zombie, so a handle
	// that was killed but never waited on would still answer here — which is
	// precisely why abandon waits.
	for i, cmd := range launched {
		if cmd.Process == nil {
			t.Fatalf("observer %d has no process handle", i)
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
			t.Errorf("detached observer %d is still running after BeginAction returned failure", i)
		}
	}

	// And the rollback is RETAINED, because "the cleanup ran" is a fact about
	// the run rather than a detail to swallow.
	recs, err := ReadRecords(filepath.Join(dir, streamName(ProducerPhysical, LevelAction, 0)))
	if err != nil {
		t.Fatal(err)
	}
	var terminal string
	for _, r := range recs {
		if r.Kind == "terminal" {
			terminal = r.Reason
		}
	}
	if terminal == "" {
		t.Fatal("the failure left no terminal record")
	}
	if !strings.Contains(terminal, "rollback:") {
		t.Errorf("the terminal record does not say what the rollback did: %q", terminal)
	}
	for _, want := range []string{"containment_peer observer exited and was reaped", "trace_collector observer exited and was reaped", "the action containment was destroyed"} {
		if !strings.Contains(terminal, want) {
			t.Errorf("the terminal record does not record %q: %q", want, terminal)
		}
	}
}

// TestASuccessfulBeginDisarmsTheRollback: the guard exists for the window
// before the handoff. Once the state file is written EndAction owns the
// observers, and a rollback that stayed armed would tear down a live envelope.
func TestASuccessfulBeginDisarmsTheRollback(t *testing.T) {
	t.Setenv(RunKeyEnv, EncodeKey(mustSigningKey()))
	dir := t.TempDir()
	st, err := BeginAction(dir, RunIdentity{CampaignID: "ewj2", RunID: "r1", BucketID: "b0"}, 30*time.Second)
	if err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	t.Cleanup(func() { _, _ = EndAction(dir, TerminalPassed, "") })
	if st.PeerPID == 0 || st.TracePID == 0 {
		t.Fatalf("the handoff carries no observer pids: %+v", st)
	}
	// The observers the handoff names are still there for EndAction to close.
	for _, pid := range []int{st.PeerPID, st.TracePID} {
		if err := syscall.Kill(pid, syscall.Signal(0)); err != nil {
			t.Errorf("a successful BeginAction left observer %d already gone: %v", pid, err)
		}
	}
}
