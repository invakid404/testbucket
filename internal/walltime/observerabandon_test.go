package walltime

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
// retainOwnership mirrors what production does with an observer it launched:
// the OS handle is remembered at launch and recovered by the later step
// through the registry. A test that means "this observer is ours" has to say
// so the same way, because ownership is now the thing that authorizes a
// signal — a bare pid authorizes nothing on any platform.
func retainOwnership(t *testing.T, cmd *exec.Cmd) *os.Process {
	t.Helper()
	rememberObserver(cmd.Process)
	t.Cleanup(func() { forgetObserver(cmd.Process.Pid) })
	p := recallObserver(cmd.Process.Pid)
	if p == nil {
		t.Fatal("the observer registry did not return the handle it was just given")
	}
	return p
}

func diedFrom(t *testing.T, cmd *exec.Cmd) (syscall.Signal, bool) {
	t.Helper()
	return diedFromWithin(t, cmd, 5*time.Second)
}

// diedFromWithin is diedFrom with an explicit bound, for the case that asserts
// a process was NOT killed and does not want to wait five seconds to say so.
func diedFromWithin(t *testing.T, cmd *exec.Cmd, within time.Duration) (syscall.Signal, bool) {
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
	case <-time.After(within):
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

	// The handle EndAction reconstructs: the pid AND the start identity the
	// opening step recorded, no command. The identity is not decoration — it
	// is what entitles this handle to signal the number at all, and production
	// carries it through the action state for exactly that reason.
	o := &observerProc{producer: ProducerPeer, pid: pid, start: processStartID(pid),
		proc: retainOwnership(t, cmd)}
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

// TestADetachedCloseProvesTheObserverExited is the F4 regression for the
// completion path.
//
// The reconstructed handle EndAction builds has no *exec.Cmd, and close() used
// to return the moment it saw an end record — so the closing step wrote
// "observers reaped" while a detached observer was still running. The contract
// makes failure to reap after the containment signal terminal; it is not
// something a note may assert on the strength of a record the observer wrote
// before exiting.
func TestADetachedCloseProvesTheObserverExited(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	dir := t.TempDir()
	o := &observerProc{
		producer: ProducerPeer,
		pid:      cmd.Process.Pid,
		start:    processStartID(cmd.Process.Pid),
		proc:     retainOwnership(t, cmd),
		ctl:      control{base: filepath.Join(dir, "ctl")},
		stream:   closedStream(t, dir),
	}
	// The end record is already on disk and the process is very much alive.
	err := o.close(time.Now().Add(300 * time.Millisecond))
	if err == nil {
		t.Fatal("close() reported a completed lifecycle while the detached observer was still running")
	}
	if !strings.Contains(err.Error(), "had not exited") {
		t.Errorf("error %q does not say the observer had not exited", err)
	}
	// And it is not a false alarm: once the process is gone, close returns.
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	if err := o.close(time.Now().Add(5 * time.Second)); err != nil {
		t.Errorf("close() refused a lifecycle whose observer had exited: %v", err)
	}
}

// TestADetachedKillRequiresTheStartIdentity is the F4 regression for the
// cancellation path.
//
// Between `wall begin` and `wall end` the kernel may have reused the pid, so a
// bare number can name an unrelated process — the runner's own work, for
// instance. abandon() signalled it anyway. It now signals only what it can
// still identify as the process the opening step launched.
func TestADetachedKillRequiresTheStartIdentity(t *testing.T) {
	// Two separate processes: each cmd may be waited on exactly once, and the
	// two halves of this test have to make opposite assertions about theirs.
	bystander := startSleeper(t)
	observer := startSleeper(t)

	// A handle whose recorded identity is NOT the one now at that pid — the
	// shape a reused pid produces between `wall begin` and `wall end`.
	stranger := &observerProc{producer: ProducerPeer, pid: bystander.Process.Pid, start: "999999999999"}
	stranger.abandon()
	// WAITED FOR, not probed. A killed child of this test stays visible as a
	// zombie until it is reaped, and signal 0 succeeds on a zombie — so a
	// probe would report "still there" for a process abandon() had just
	// killed, and would pass against the very defect this asserts.
	if sig, died := diedFromWithin(t, bystander, 400*time.Millisecond); died {
		t.Fatalf("abandon() killed a process at a REUSED pid (signal %v); a wrapper must not terminate work it cannot identify as its own", sig)
	}

	// And the handle that CAN identify its observer still ends it — on every
	// platform, because it holds the OS handle for a child it has not reaped
	// rather than a number it hopes is still the same process.
	ours := &observerProc{producer: ProducerPeer, pid: observer.Process.Pid,
		start: processStartID(observer.Process.Pid), proc: retainOwnership(t, observer)}
	ours.abandon()
	sig, died := diedFrom(t, observer)
	if !died {
		t.Fatal("abandon() left the observer it could identify running")
	}
	if sig != syscall.SIGKILL {
		t.Errorf("the observer died of %v, want SIGKILL", sig)
	}
}

// startSleeper starts a long-lived process the test owns and cleans up.
func startSleeper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd
}

// TestTheActionStateCarriesBothObserverStartIdentities: the closing step can
// only check an identity the opening step wrote down.
func TestTheActionStateCarriesBothObserverStartIdentities(t *testing.T) {
	st := ActionState{PeerPID: 111, TracePID: 222, PeerStart: "12345", TraceStart: "67890"}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"peer_pid", "trace_pid", "peer_start", "trace_start"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("the action state does not serialise %q, so the closing step must trust a bare number: %s", key, b)
		}
	}
	var got ActionState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.PeerStart != "12345" || got.TraceStart != "67890" {
		t.Errorf("start identities round-tripped as peer=%q trace=%q", got.PeerStart, got.TraceStart)
	}
}

// TestTheClosingNoteOnlyClaimsWhatWasProved: the note is read as a finding by
// anyone auditing the ledger, and it used to assert a reap unconditionally.
func TestTheClosingNoteOnlyClaimsWhatWasProved(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	live := &observerProc{producer: ProducerPeer, pid: cmd.Process.Pid, start: processStartID(cmd.Process.Pid)}
	gone := &observerProc{producer: ProducerTrace, pid: -1}

	if note := observerTeardownNote(live, gone); !strings.Contains(note, "had NOT exited") {
		t.Errorf("the closing note claims %q while an observer was still running", note)
	}
	if note := observerTeardownNote(gone, gone); !strings.Contains(note, "not recorded") {
		t.Errorf("the closing note claims %q for observers whose identity was never recorded", note)
	}
	exited := &observerProc{producer: ProducerPeer, pid: 1 << 30, start: "never"}
	if note := observerTeardownNote(exited, exited); !strings.Contains(note, "exited and were reaped") {
		t.Errorf("the closing note does not report a proved reap: %q", note)
	}
}

// closedStream writes a signed stream whose only record is the closing
// boundary, which is exactly what a detached observer leaves behind.
func closedStream(t *testing.T, dir string) string {
	t.Helper()
	stream := filepath.Join(dir, streamName(ProducerPeer, LevelAction, 0))
	key, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWriter(stream, ProducerPeer, "detached#1", key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(Record{
		Kind: "boundary", Boundary: "end", Producer: ProducerPeer,
		Level: LevelAction, Source: SourceContainment,
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return stream
}
