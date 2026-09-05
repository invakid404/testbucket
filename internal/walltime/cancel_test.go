package walltime

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// recordingContainment records the signals the policy sends it and nothing
// else. It exists so the escalation sequence can be asserted directly rather
// than inferred from whether a real process happened to die.
type recordingContainment struct {
	mu   sync.Mutex
	sent []syscall.Signal
}

func (c *recordingContainment) Identity() ContainmentIdentity { return ContainmentIdentity{ID: "test"} }
func (c *recordingContainment) Admit(int) error               { return nil }

// Freeze is a no-op for the double: these tests exercise cancellation, and the
// freeze protocol belongs to the admission read.
func (c *recordingContainment) Freeze(bool) error     { return nil }
func (c *recordingContainment) Procs() ([]int, error) { return nil, nil }
func (c *recordingContainment) Observe(string) (RawEvent, bool, error) {
	return RawEvent{}, false, nil
}
func (c *recordingContainment) Signal(sig syscall.Signal) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, sig)
	return nil
}
func (c *recordingContainment) Destroy() error { return nil }
func (c *recordingContainment) signals() []syscall.Signal {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]syscall.Signal(nil), c.sent...)
}

// TestTheCancellationPolicyEscalatesExactlyOnce drives the policy directly.
//
// The three endings are asserted separately because they are separately
// wrong-able: an exit needs no signal at all, a cancellation that stops in the
// grace must NOT be killed, and one that does not must be killed and then
// reported unreaped rather than waited on forever.
func TestTheCancellationPolicyEscalatesExactlyOnce(t *testing.T) {
	t.Run("a child that exits is never signalled", func(t *testing.T) {
		withShortCancellationPolicy(t, 50*time.Millisecond, 50*time.Millisecond)
		cont := &recordingContainment{}
		done := make(chan error, 1)
		done <- nil
		cancelled, escalation, err := awaitChild(cont, make(chan os.Signal), done, time.Now().Add(time.Minute))
		if cancelled != "" || escalation != "" || err != nil {
			t.Fatalf("a clean exit reported cancelled=%q escalation=%q err=%v", cancelled, escalation, err)
		}
		if got := cont.signals(); len(got) != 0 {
			t.Errorf("a clean exit signalled the containment: %v", got)
		}
	})

	t.Run("a cancellation that stops inside the grace is not killed", func(t *testing.T) {
		withShortCancellationPolicy(t, 2*time.Second, time.Second)
		cont := &recordingContainment{}
		sigs := make(chan os.Signal, 1)
		done := make(chan error, 1)
		sigs <- syscall.SIGINT
		go func() {
			time.Sleep(50 * time.Millisecond)
			done <- nil
		}()
		cancelled, escalation, err := awaitChild(cont, sigs, done, time.Now().Add(time.Minute))
		if cancelled == "" {
			t.Error("the cancellation was not recorded")
		}
		if escalation != "" {
			t.Errorf("a child that stopped when asked was escalated: %q", escalation)
		}
		if err != nil {
			t.Errorf("err = %v", err)
		}
		if got := cont.signals(); len(got) != 1 || got[0] != syscall.SIGTERM {
			t.Errorf("signals = %v, want exactly one SIGTERM", got)
		}
	})

	t.Run("a child that ignores TERM is killed and then reported unreaped", func(t *testing.T) {
		withShortCancellationPolicy(t, 50*time.Millisecond, 50*time.Millisecond)
		cont := &recordingContainment{}
		sigs := make(chan os.Signal, 1)
		sigs <- syscall.SIGTERM
		// A wait that never returns: the shape that used to hang the wrapper.
		cancelled, escalation, err := awaitChild(cont, sigs, make(chan error), time.Now().Add(time.Minute))
		if cancelled == "" {
			t.Error("the cancellation was not recorded")
		}
		if !strings.Contains(escalation, "was killed") {
			t.Errorf("escalation %q does not record the kill", escalation)
		}
		if err != errUnreaped {
			t.Errorf("err = %v, want errUnreaped", err)
		}
		want := []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}
		if got := cont.signals(); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("signals = %v, want %v", got, want)
		}
	})

	t.Run("the deadline escalates on its own", func(t *testing.T) {
		withShortCancellationPolicy(t, 50*time.Millisecond, 50*time.Millisecond)
		cont := &recordingContainment{}
		_, escalation, err := awaitChild(cont, make(chan os.Signal), make(chan error), time.Now().Add(20*time.Millisecond))
		if !strings.Contains(escalation, "cancellation deadline") {
			t.Errorf("escalation %q does not name the deadline", escalation)
		}
		if err != errUnreaped {
			t.Errorf("err = %v, want errUnreaped", err)
		}
		want := []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}
		if got := cont.signals(); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("signals = %v, want %v", got, want)
		}
	})
}

// withShortCancellationPolicy shortens the frozen bounds for the duration of
// one test. Production never assigns them; a test that waited out the real
// thirty-second grace would be a thirty-second test.
func withShortCancellationPolicy(t *testing.T, grace, reap time.Duration) {
	t.Helper()
	oldGrace, oldReap := cancellationGrace, reapGrace
	cancellationGrace, reapGrace = grace, reap
	t.Cleanup(func() { cancellationGrace, reapGrace = oldGrace, oldReap })
}

// terminalOf returns the terminal state and reason the physical wrapper
// recorded for a run.
func terminalOf(t *testing.T, dir string) (string, string) {
	t.Helper()
	recs, err := ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, r := range recs {
		if r.Producer == ProducerPhysical && r.Kind == "boundary" && r.Boundary == "end" {
			return r.Terminal, r.Reason
		}
	}
	t.Fatal("no physical closing record was retained")
	return "", ""
}

// TestATermIgnoringRootIsKilledAtTheGrace is the F4 regression for the
// escalation half.
//
// The child traps SIGTERM and keeps running. The wrapper used to send TERM to
// the containment and then wait on `cmd.Wait` with no timer and no second
// step, so this exact shape hung the wrapper — and therefore the job —
// forever. The frozen policy now bounds that wait and escalates to a
// whole-containment SIGKILL, which nothing survives.
func TestATermIgnoringRootIsKilledAtTheGrace(t *testing.T) {
	withShortCancellationPolicy(t, 300*time.Millisecond, 5*time.Second)
	dir := t.TempDir()

	// The wrapper cancels on a signal it receives itself, so the test sends
	// one to this process once the hostile child is certainly running.
	go func() {
		time.Sleep(400 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := Exec(ExecOptions{
			Level: LevelInvocation, Dir: dir, Cwd: dir,
			Run: RunIdentity{BucketID: "b1", Stage2: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
			// trap '' TERM makes the shell ignore SIGTERM outright.
			Argv:    []string{"sh", "-c", "trap '' TERM; while :; do sleep 0.05; done"},
			Timeout: 30 * time.Second,
		})
		if err != nil {
			t.Errorf("Exec: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the wrapper never returned: a TERM-ignoring root still hangs it")
	}

	terminal, reason := terminalOf(t, dir)
	if terminal != TerminalCancelled {
		t.Errorf("terminal = %q, want %q", terminal, TerminalCancelled)
	}
	// The escalation is RETAINED. A cancelled run that had to be killed and a
	// cancelled run that stopped when asked are different facts.
	if !strings.Contains(reason, "was killed") {
		t.Errorf("reason %q does not retain the escalation", reason)
	}
	// And it is never scored: a cancelled row is retained, not a measurement.
	v, err := VerifyDir(VerifyOptions{Dir: dir})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	if v.Eligible {
		t.Error("a cancelled run was scored")
	}
}

// TestTheDeadlineIsAnEndpointNotASuggestion: with no signal at all, a child
// that never exits must still end the wrapper. The deadline never even reached
// the wait loop before, so this shape hung with no record.
func TestTheDeadlineIsAnEndpointNotASuggestion(t *testing.T) {
	withShortCancellationPolicy(t, 200*time.Millisecond, 5*time.Second)
	dir := t.TempDir()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := Exec(ExecOptions{
			Level: LevelInvocation, Dir: dir, Cwd: dir,
			Run:     RunIdentity{BucketID: "b1", Stage2: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
			Argv:    []string{"sh", "-c", "trap '' TERM; while :; do sleep 0.05; done"},
			Timeout: 700 * time.Millisecond,
		}); err != nil {
			t.Errorf("Exec: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the wrapper never returned: the cancellation deadline is not enforced")
	}
	terminal, reason := terminalOf(t, dir)
	if terminal != TerminalCancelled {
		t.Errorf("terminal = %q, want %q", terminal, TerminalCancelled)
	}
	if !strings.Contains(reason, "cancellation deadline") {
		t.Errorf("reason %q does not name the deadline", reason)
	}
}

// TestADetachedDescendantIsKilledAndReaped is the F4 regression for the reap
// half.
//
// The root exits immediately and leaves a descendant behind. The wrapper used
// to observe the non-empty containment, write `crash_unclosed` and RETURN,
// leaving the descendant running on the runner for whatever came next. The
// escape is still terminal — that is the contract — but the descendant must
// not survive the wrapper that was supposed to contain it.
func TestADetachedDescendantIsKilledAndReaped(t *testing.T) {
	withShortCancellationPolicy(t, 200*time.Millisecond, 5*time.Second)
	dir := t.TempDir()
	marker := dir + "/still-alive"
	// The descendant outlives its root and keeps touching a file. If the
	// wrapper only labelled the escape, the file's timestamp would keep
	// advancing after Exec returned.
	script := "( while :; do : > " + marker + "; sleep 0.05; done ) & exit 0"
	if _, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: dir, Cwd: dir,
		Run:     RunIdentity{BucketID: "b1", Stage2: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
		Argv:    []string{"sh", "-c", script},
		Timeout: 2 * time.Second,
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	terminal, reason := terminalOf(t, dir)
	if terminal != TerminalCrashUnclosed {
		t.Fatalf("terminal = %q, want %q; an escape is never rounded down to a finished run", terminal, TerminalCrashUnclosed)
	}
	if !strings.Contains(reason, "killed") {
		t.Errorf("reason %q does not record the forced reap", reason)
	}

	// The descendant is gone: the marker stops moving.
	before, err := os.Stat(marker)
	if err != nil {
		t.Skipf("the descendant never reached the marker on this host: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	after, err := os.Stat(marker)
	if err != nil {
		t.Fatalf("stat marker: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("the detached descendant is still running after the wrapper returned; the escape was labelled, not reaped")
	}

	v, err := VerifyDir(VerifyOptions{Dir: dir})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	if v.Eligible {
		t.Error("a run with an escaped descendant was scored")
	}
}

// TestTheDeclaredCancellationPolicyIsTheImplementedOne: Stage 1 declares the
// policy as an instrumentation identity, and a manifest that declared bounds
// the wrapper does not implement would be authorising behaviour nobody wrote.
func TestTheDeclaredCancellationPolicyIsTheImplementedOne(t *testing.T) {
	for _, want := range []string{CancellationGrace.String(), ReapGrace.String(), "SIGTERM", "SIGKILL", "reaped"} {
		if !strings.Contains(CancellationPolicyID, want) {
			t.Errorf("the declared policy %q does not name %q", CancellationPolicyID, want)
		}
	}
}
