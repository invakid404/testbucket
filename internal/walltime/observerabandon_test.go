package walltime

import (
	"encoding/json"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// diedFrom waits for a process this test started and reports the signal that
// ended it, or an empty state if it is still running after the deadline.
//
// It WAITS rather than probing with signal 0. A killed child of this process
// stays visible as a zombie until someone reaps it, and signal 0 succeeds on a
// zombie — so a probe would report "alive" for a process that is already dead
// and would pass against an abandon() that did nothing at all only by luck of
// timing. Waiting distinguishes the two.
func diedFrom(t *testing.T, cmd *exec.Cmd) (syscall.Signal, bool) {
	t.Helper()
	done := make(chan *os.ProcessState, 1)
	go func() {
		st, err := cmd.Process.Wait()
		if err == nil {
			done <- st
		}
		close(done)
	}()
	select {
	case st := <-done:
		if st == nil {
			return 0, false
		}
		ws, ok := st.Sys().(syscall.WaitStatus)
		if !ok || !ws.Signaled() {
			return 0, false
		}
		return ws.Signal(), true
	case <-time.After(5 * time.Second):
		return 0, false
	}
}

// TestADetachedObserverIsStillKillableWithoutItsCommand is the F1 regression
// for the reconstructed handle.
//
// A detached ACTION observer outlives the step that started it, so `EndAction`
// rebuilds its handle from the action state and has no `*exec.Cmd`. `abandon`
// only ever killed through `cmd`, so for exactly the handle that matters — the
// one belonging to a lifecycle nobody can complete — it was a no-op: the
// observer went on watching the containment for its whole timeout, writing
// records over whatever ran next, and the one process that knew its address
// had thrown the address away.
func TestADetachedObserverIsStillKillableWithoutItsCommand(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	_ = pid
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	// The handle EndAction reconstructs: an identity and a pid, no command.
	o := &observerProc{producer: ProducerPeer, pid: pid}
	o.abandon()

	sig, killed := diedFrom(t, cmd)
	if !killed {
		t.Fatal("abandon() left a detached observer running; a lifecycle that cannot be completed must still end the observation")
	}
	if sig != syscall.SIGKILL {
		t.Errorf("the detached observer died of %v, want SIGKILL", sig)
	}
}

// TestAbandonIsSafeWithNothingToKill: a handle that never started anything is
// abandoned all the time — both admit branches now abandon both observers,
// including one that was never launched. Killing pid 0 or a negative pid would
// signal a process group.
func TestAbandonIsSafeWithNothingToKill(t *testing.T) {
	for _, o := range []*observerProc{
		{producer: ProducerPeer},
		{producer: ProducerTrace, pid: 0},
		{producer: ProducerPeer, pid: -1},
	} {
		o.abandon() // must not panic, and must not signal a group
	}
}

// TestTheActionStateCarriesBothObserverPIDs is the other half of F1: the
// reconstructed handle can only kill what the state remembered.
//
// `EndAction` runs in a later step, in a different process. If the observer
// pids are not written into the action state, the rebuilt handles have pid 0
// and abandoning them does nothing at all — which is the same hole one process
// further along.
func TestTheActionStateCarriesBothObserverPIDs(t *testing.T) {
	st := ActionState{PeerPID: 111, TracePID: 222}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var got ActionState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.PeerPID != 111 || got.TracePID != 222 {
		t.Errorf("the action state round-tripped peer=%d trace=%d, want 111/222; a later step cannot end an observation whose address it was never told",
			got.PeerPID, got.TracePID)
	}
	// Both fields must actually be serialised, or the later step reads zeros.
	for _, key := range []string{"peer_pid", "trace_pid"} {
		var raw map[string]any
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatal(err)
		}
		if _, ok := raw[key]; !ok {
			t.Errorf("the action state does not serialise %q: %s", key, b)
		}
	}
}
