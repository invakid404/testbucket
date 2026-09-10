package walltime

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

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
	r := closingRecord(t, dir)
	return r.Terminal, r.Reason
}

// closingRecord returns the physical closing boundary, so a test can assert on
// the process identity it carries and not only on the terminal string.
func closingRecord(t *testing.T, dir string) Record {
	t.Helper()
	recs, err := ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, r := range recs {
		if r.Producer == ProducerPhysical && r.Kind == "boundary" && r.Boundary == "end" {
			return r
		}
	}
	t.Fatal("no physical closing record was retained")
	return Record{}
}

// assertTheReapWasNotReported checks the shape of the reap bookkeeping defect:
// the drain reaped the root, nothing wrote that back, and the deferred
// fallback then killed an already-reaped process, waited out ReapGrace on a
// channel that could not deliver again, and stamped a false crash.
//
// The elapsed bound is part of the assertion, not decoration. The wrong
// behaviour was CORRECT-LOOKING apart from costing ten seconds — a reader of
// the record saw `crash_unclosed` and believed it.
func assertTheReapWasNotReported(t *testing.T, dir string, elapsed, bound time.Duration) {
	t.Helper()
	r := closingRecord(t, dir)
	if r.Proc.ExitKind == TerminalCrashUnclosed {
		t.Errorf("proc.exit_kind is %q on a run whose root the drain reaped; reason was %q",
			r.Proc.ExitKind, r.Reason)
	}
	if strings.Contains(r.Reason, "was not reaped") {
		t.Errorf("the closing reason claims the root was not reaped: %q", r.Reason)
	}
	if elapsed > bound {
		t.Errorf("the wrapper took %s; a reaped root must not cost the %s reap grace as well",
			elapsed, bound)
	}
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
			Run: RunIdentity{BucketID: "b1", RunID: "run-1"},
			// trap '' TERM makes the shell ignore SIGTERM outright.
			Argv:    []string{"sh", "-c", "trap '' TERM; while :; do sleep 0.05; done"},
			Timeout: 30 * time.Second,
		})
		if err != nil {
			t.Errorf("Exec: %v", err)
		}
	}()
	started := time.Now()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the wrapper never returned: a TERM-ignoring root still hangs it")
	}
	// The policy here is a 300ms TERM grace and a 5s reap grace. A run that
	// pays the reap grace a SECOND time, for a root the drain already reaped,
	// cannot fit in three seconds.
	assertTheReapWasNotReported(t, dir, time.Since(started), 3*time.Second)

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
			Run:     RunIdentity{BucketID: "b1", RunID: "run-1"},
			Argv:    []string{"sh", "-c", "trap '' TERM; while :; do sleep 0.05; done"},
			Timeout: 700 * time.Millisecond,
		}); err != nil {
			t.Errorf("Exec: %v", err)
		}
	}()
	started := time.Now()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the wrapper never returned: the cancellation deadline is not enforced")
	}
	// Same bound, same reason: the deadline path is the other route that never
	// took the wait result itself, so it is the other one that paid the reap
	// grace twice and reported a crash that had not happened.
	assertTheReapWasNotReported(t, dir, time.Since(started), 3*time.Second)

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
		Run:     RunIdentity{BucketID: "b1", RunID: "run-1"},
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

// TestTheEscalationPolicyRunsOnTheGroupTheRunnerOwns replaces a test that
// drove a REMOVED code path.
//
// The escalation used to be asserted through `awaitChild(Containment, …)`
// against a recording double that counted signals. `awaitChild` had no
// production caller — the shipped runner reaps its root and calls DrainGroup —
// so the sequence was proved on one implementation while another one ran. The
// double also made "the containment was signalled" checkable without any
// process existing, which is exactly the confidence the removal was about.
//
// This drives DrainGroup over a REAL process group and asserts what §3.3
// fixes: a group that stops when asked is not escalated, the root is reaped and
// the group confirmed empty before the drain returns, and an escape is killed
// at once with no cooperative grace to serve.
//
// The TERM-IGNORING escalation is deliberately not re-asserted here.
// TestATermIgnoringRootIsKilledAtTheGrace above already drives it end to end
// through the shipped Exec path, which is where it matters; reproducing a
// stubborn child in isolation depends on the host's /bin/sh honouring
// `trap \'\' TERM`, and this one does not.
func TestTheEscalationPolicyRunsOnTheGroupTheRunnerOwns(t *testing.T) {
	// startGroup launches one shell in its own process group and returns the
	// pgid plus a reap function, which is what the exec path hands DrainGroup.
	startGroup := func(t *testing.T, script string) (int, func() error) {
		t.Helper()
		cmd := exec.Command("sh", "-c", script)
		ownProcessGroup(cmd)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		pgid, err := childProcessGroup(cmd)
		if err != nil || pgid <= 1 {
			t.Fatalf("child process group = %d, %v", pgid, err)
		}
		return pgid, cmd.Wait
	}

	t.Run("a group that stops when asked is not killed", func(t *testing.T) {
		// Default TERM disposition: the shell dies on the first signal.
		pgid, reap := startGroup(t, "while :; do sleep 0.05; done")
		out, err := DrainGroup(DrainRequest{
			PGID: pgid, TermGrace: 5 * time.Second, KillGrace: 5 * time.Second,
			ReapRoot: reap,
		})
		if err != nil {
			t.Fatalf("DrainGroup: %v", err)
		}
		if !out.Signalled {
			t.Error("the group was never signalled")
		}
		if out.Escalated {
			t.Error("a group that stopped on TERM was escalated to KILL")
		}
		if !out.Reaped {
			t.Error("the root was not reaped")
		}
	})

	t.Run("the root is reaped and the group confirmed empty before returning", func(t *testing.T) {
		// The ORDERING is the measurable content of §3.3: the caller must not
		// take its closing reading while any member remains, so the drain does
		// not return until the root is reaped AND the group reads empty.
		pgid, reap := startGroup(t, "while :; do sleep 0.05; done")
		out, err := DrainGroup(DrainRequest{
			PGID: pgid, TermGrace: 5 * time.Second, KillGrace: 5 * time.Second,
			ReapRoot: reap,
		})
		if err != nil {
			t.Fatalf("DrainGroup: %v", err)
		}
		if !out.Reaped {
			t.Error("the drain returned without reaping the root")
		}
		if out.Probeable && !out.GroupEmpty {
			t.Error("the drain returned on a probeable platform without confirming an empty group")
		}
		if !out.Probeable && out.Limitation() == "" {
			t.Error("a platform that cannot probe must state the limitation rather than imply an empty group")
		}
	})

	t.Run("an escape is killed at once, with no grace to wait out", func(t *testing.T) {
		// ImmediateKill is the ESCAPE path: the root has already been reaped,
		// so there is nothing left to ask cooperatively.
		pgid, reap := startGroup(t, "trap '' TERM; while :; do sleep 0.05; done")
		start := time.Now()
		out, err := DrainGroup(DrainRequest{
			PGID: pgid, TermGrace: 10 * time.Second, KillGrace: 5 * time.Second,
			ReapRoot: reap, ImmediateKill: true,
		})
		if err != nil {
			t.Fatalf("DrainGroup: %v", err)
		}
		if !out.Escalated {
			t.Error("an escape was not killed")
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("an immediate kill waited %s; it must not serve the TERM grace", elapsed)
		}
	})
}

// TestTheCancellationPolicyIsStatedWhereItIsImplemented replaces an assertion
// over a REMOVED exported string.
//
// `CancellationPolicyID` restated the policy as "the frozen policy Stage 1
// declares", derived from the constants so a manifest could not declare
// something the wrapper did not implement. There is no manifest and nothing
// read the string. What still has to be true is that the two numbers the drain
// actually uses are the two the package documents.
func TestTheCancellationPolicyIsStatedWhereItIsImplemented(t *testing.T) {
	b, err := os.ReadFile("exec.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{"SIGTERM", "SIGKILL", "CancellationGrace", "ReapGrace", "reaped"} {
		if !strings.Contains(src, want) {
			t.Errorf("the stated policy does not name %q", want)
		}
	}
	// And the variables the drain reads ARE the declared constants, so a test
	// shortening the policy cannot be mistaken for the shipped one.
	if cancellationGrace != CancellationGrace {
		t.Errorf("cancellationGrace = %s, want the declared %s", cancellationGrace, CancellationGrace)
	}
	if reapGrace != ReapGrace {
		t.Errorf("reapGrace = %s, want the declared %s", reapGrace, ReapGrace)
	}
}
