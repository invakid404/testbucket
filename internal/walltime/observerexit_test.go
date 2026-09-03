package walltime

import (
	"os/exec"
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
	o := &observerProc{producer: ProducerTrace, pid: pid, start: processStartID(pid)}

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
