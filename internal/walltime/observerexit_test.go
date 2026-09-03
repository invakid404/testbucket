package walltime

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// AN OBSERVER THAT HAS EXITED IS NOT STILL RUNNING, even when nothing has
// reaped it yet.
//
// A detached action observer is reconstructed in a later step from the action
// state, so the handle has a pid and no cmd, and nothing has called Wait on
// it. On Linux a child in exactly that position stays a zombie: it has exited,
// but the process table still holds an entry for it, and `kill(pid, 0)`
// SUCCEEDS. Reading that success as "still running" made the close wait spend
// its entire budget on a process that had already finished — the wrapper
// declared the lifecycle incomplete after twenty seconds because it could not
// see an exit that had already happened.
//
// Exit proof therefore has to be collected, not inferred from existence.
func TestAnExitedObserverIsNotWaitedFor(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid

	// The handle a later step reconstructs: a pid and its start identity, no
	// cmd, and deliberately no Wait — this process has NOT reaped it.
	o := &observerProc{producer: ProducerTrace, pid: pid, start: processStartID(pid),
		proc: retainOwnership(t, cmd)}

	// Give the child time to actually finish. It is not reaped here, so on
	// Linux it is now a zombie and on any platform it is an exited child of
	// this process.
	deadline := time.Now().Add(10 * time.Second)
	for cmd.ProcessState == nil && time.Now().Before(deadline) {
		if !o.stillRunning() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if o.stillRunning() {
		t.Fatal("an exited, unreaped observer reported itself still running; existence was mistaken for liveness")
	}

	// And the wait that depends on it returns AT ONCE rather than burning its
	// budget. The generous deadline is the point: a correct awaitExit never
	// reaches it, so a slow machine cannot make this test flake, while the
	// defect it guards fails it by construction.
	start := time.Now()
	if err := o.awaitExit(time.Now().Add(20 * time.Second)); err != nil {
		t.Fatalf("awaitExit on an exited observer: %v", err)
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Errorf("awaitExit spent %s confirming an exit that had already happened", waited)
	}
}

// EACH OBSERVER GETS ITS OWN CLOSE BUDGET.
//
// The action closes a trace collector and a containment peer, and both have to
// produce a closing record for the lifecycle to be complete. Closing them
// against ONE shared deadline let the first observer spend the whole window:
// the second was then asked to close by a deadline that had already passed, so
// its proof was never waited for and the action was recorded as incomplete
// because of how the wait was budgeted rather than because of anything the
// observer did.
func TestEveryObserverGetsItsOwnCloseBudget(t *testing.T) {
	t.Parallel()

	// The action's own deadline, far enough away that it does not bind.
	shared := time.Now().Add(10 * time.Minute)

	first := observerCloseBy(shared)
	if budget := time.Until(first); budget < ObserverCloseGrace/2 || budget > ObserverCloseGrace+time.Minute {
		t.Fatalf("the first observer's budget was %s, want about %s", budget, ObserverCloseGrace)
	}

	// The second observer is budgeted AFTER the first has had its turn. Its
	// budget is measured from that moment, so a slow first close does not
	// leave the second with nothing.
	second := observerCloseBy(shared)
	if budget := time.Until(second); budget < ObserverCloseGrace/2 {
		t.Errorf("the second observer got %s, want its own %s: one observer's close consumed the other's proof",
			budget, ObserverCloseGrace)
	}

	// THE ACTION'S DEADLINE IS STILL AN UPPER BOUND. The per-observer grace
	// extends nothing: when the action ends sooner than the grace, the close
	// ends with the action.
	near := time.Now().Add(time.Second)
	if got := observerCloseBy(near); !got.Equal(near) {
		t.Errorf("close budget %v ran past the action deadline %v", got, near)
	}
}

// A HANDLE THAT DOES NOT OWN THE PID DOES NOT REAP IT.
//
// Reaping is an action, not an observation: `Wait4` consumes an exited child's
// status and takes it away from whoever was actually waiting for it. The
// wrapper used to reap FIRST and compare the start identity afterwards, so a
// handle rebuilt in a later step — holding a pid the kernel had since handed
// to unrelated work — collected that unrelated child's exit status on its way
// to concluding it owned nothing at all. The real owner's `Wait` then failed
// with ECHILD.
//
// This is the candidate's own version of the external control that found it.
func TestAWrongIdentityHandleDoesNotReapAnUnrelatedExitedChild(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Let it exit and stay unreaped, so there is a status available to steal.
	// A deliberately wrong start identity models the reused pid.
	time.Sleep(250 * time.Millisecond)
	o := &observerProc{producer: ProducerPeer, pid: cmd.Process.Pid, start: "not-the-process-at-this-pid"}

	if o.stillRunning() {
		t.Fatal("a process with the wrong start identity was reported as our observer")
	}
	if o.ownsPID() {
		t.Error("a handle whose start identity does not match claimed the pid")
	}

	// THE REAL OWNER MUST STILL BE ABLE TO COLLECT ITS CHILD. An ECHILD here
	// means the handle acted on the number before authenticating it.
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wrong-identity observer handle reaped an unrelated child: %v", err)
	}
}

// A DETACHED HANDLE WITH NO IDENTITY FAILS CLOSED.
//
// On a platform that can identify processes, a reconstructed handle holding
// only a pid has lost its identity rather than never having had one. It may
// not signal that number — the process there may be the runner's own work —
// and it may not report an exit it cannot see. Both refusals leave the
// lifecycle incomplete, which is the honest record; the observer's own timeout
// still ends it.
func TestADetachedHandleWithoutAnIdentityWillNotActOnItsPID(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	// The handle a later step rebuilds when the identity was never recorded.
	o := &observerProc{producer: ProducerTrace, pid: cmd.Process.Pid}

	if o.ownsPID() {
		t.Error("a detached handle with no start identity claimed authority over its pid")
	}

	o.abandon()
	if _, killed := diedFrom(t, cmd); killed {
		t.Error("abandon() signalled a pid it could not authenticate; an unrelated process at a reused number would have been killed")
	}

	// And it does not pass off existence as an exit proof.
	if err := o.awaitExit(time.Now().Add(time.Second)); err == nil {
		t.Error("awaitExit reported an exit for a pid it could not authenticate; the lifecycle must stay incomplete")
	}
}

// SIGNALLING GOES THROUGH A HANDLE, NOT A NUMBER, WHERE THE PLATFORM HAS ONE.
//
// `ownsPID` answering yes and the signal landing are two separate moments. In
// between, the process may exit and the kernel may give its pid to something
// else — so a check-then-kill can verify one process and signal another. The
// window is small; killing the runner's unrelated work is not a small
// consequence.
//
// pidfd_open pins the process before the identity is re-read, so a pid that
// changed hands is detected instead of signalled. This exercises that path
// directly, including its refusal.
func TestSignallingPrefersAPinnedHandleOverAPID(t *testing.T) {
	if !signalByIdentity(os.Getpid(), processStartID(os.Getpid()), syscall.Signal(0)) {
		t.Skip("no pinned-handle signalling on this platform or kernel")
	}
	t.Parallel()

	// A MATCHING IDENTITY IS SIGNALLED.
	victim := exec.Command("sleep", "30")
	if err := victim.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = victim.Process.Kill(); _, _ = victim.Process.Wait() })
	if !signalByIdentity(victim.Process.Pid, processStartID(victim.Process.Pid), syscall.SIGKILL) {
		t.Fatal("a pinned handle with a matching identity did not deliver the signal")
	}
	if _, died := diedFromWithin(t, victim, 2*time.Second); !died {
		t.Error("the signal was reported delivered but the process survived")
	}

	// A MISMATCHED IDENTITY IS REFUSED, after the pin rather than before it.
	bystander := exec.Command("sleep", "30")
	if err := bystander.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bystander.Process.Kill(); _, _ = bystander.Process.Wait() })
	if signalByIdentity(bystander.Process.Pid, "999999999999", syscall.SIGKILL) {
		t.Fatal("a pinned handle signalled a process whose identity did not match")
	}
	if _, died := diedFromWithin(t, bystander, 400*time.Millisecond); died {
		t.Error("the refused signal was delivered anyway")
	}
}

// THE REGISTRY STOPS VOUCHING ONCE THE PROCESS IS REAPED.
//
// A retained handle is ownership only while the child is unreaped: that is
// exactly what stops the kernel reusing its pid. The moment the wrapper reaps
// it, the number is the kernel's again — and a registry still holding the
// handle would go on answering "ours" for a pid that may now belong to
// anything, handing back the bare-pid authority this whole rule removes.
func TestTheRegistryReleasesAPIDOnceItIsReaped(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	rememberObserver(cmd.Process)
	t.Cleanup(func() { forgetObserver(pid) })

	o := &observerProc{producer: ProducerPeer, pid: pid, proc: recallObserver(pid)}
	if !o.ownsPID() {
		t.Fatal("a retained handle for an unreaped child was not treated as ownership")
	}

	// Let it exit, then collect it the way the close path does.
	deadline := time.Now().Add(10 * time.Second)
	for o.stillRunning() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if p := recallObserver(pid); p != nil {
		t.Error("the registry still vouches for a pid that has been reaped and released for reuse")
	}
	bare := &observerProc{producer: ProducerPeer, pid: pid, proc: recallObserver(pid)}
	if bare.ownsPID() {
		t.Error("a reaped pid still claimed ownership; the number is the kernel's again")
	}
}
