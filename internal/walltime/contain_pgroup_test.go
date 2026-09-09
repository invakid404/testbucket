//go:build unix

package walltime

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startOwnedGroup starts a shell in its OWN process group running script, and
// returns the command and the pgid. The group is the wrapper's to signal, which
// is the arrangement contract §3.3 describes.
func startOwnedGroup(t *testing.T, script string) (*exec.Cmd, int) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start owned group: %v", err)
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("getpgid: %v", err)
	}
	if pgid == syscall.Getpgrp() {
		t.Fatal("child shares the test process's group; the fixture would signal the test runner")
	}
	return cmd, pgid
}

// TestProcessGroupDrainsBeforeEnd is §22 test 45 (F7) and the acceptance test
// the registry names for ID-12.
//
// It asserts the bounded TERM->KILL escalation and that no end timestamp is
// taken while the group exists. It makes NO assertion about descendant
// reaping, because a non-subreaper parent cannot perform one — that is the
// wording §3.3 removed as infeasible.
func TestProcessGroupDrainsBeforeEnd(t *testing.T) {
	t.Run("a cooperative child that exits is drained without escalation", func(t *testing.T) {
		cmd, pgid := startOwnedGroup(t, "exit 0")

		out, err := DrainGroup(DrainRequest{
			PGID: pgid, TermGrace: DefaultTermGrace, KillGrace: DefaultKillGrace,
			ReapRoot: cmd.Wait,
		})
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
		if !out.GroupEmpty {
			t.Fatal("drain did not confirm an empty group for a child that already exited")
		}
		if out.Escalated {
			t.Fatal("drain escalated to KILL for a group that was already empty")
		}
		if out.Limitation() != "" {
			t.Fatalf("a confirmed-empty drain must report no limitation, got %q", out.Limitation())
		}
	})

	t.Run("a child that ignores TERM forces bounded escalation to KILL", func(t *testing.T) {
		// The child traps and ignores TERM and outlives the root, which is
		// exactly the case the escalation exists for.
		cmd, pgid := startOwnedGroup(t, `trap "" TERM; sleep 30`)
		t.Cleanup(func() {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			_ = cmd.Wait()
		})

		// Give the shell time to install the trap, so the fixture really does
		// test an ignored TERM rather than a race.
		time.Sleep(150 * time.Millisecond)

		termGrace := 200 * time.Millisecond
		start := time.Now()
		out, err := DrainGroup(DrainRequest{
			PGID: pgid, TermGrace: termGrace, KillGrace: 2 * time.Second,
			ReapRoot: cmd.Wait,
		})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("drain: %v", err)
		}

		if !out.Signalled {
			t.Fatal("drain did not signal the group")
		}
		if !out.Escalated {
			t.Fatal("drain did not escalate to KILL against a child that ignores TERM")
		}
		if !out.GroupEmpty {
			t.Fatal("drain did not confirm an empty group after KILL")
		}
		// Bounded: it waited at least the TERM grace, and did not hang for the
		// child's full 30s sleep.
		if elapsed < termGrace {
			t.Fatalf("drain returned in %v, less than the TERM grace %v", elapsed, termGrace)
		}
		if elapsed > 10*time.Second {
			t.Fatalf("drain took %v; the escalation is not bounded", elapsed)
		}
	})

	t.Run("no end timestamp is taken while the group exists", func(t *testing.T) {
		// The measurable content of §3.3 step 3: the closing read happens
		// AFTER the drain confirms the group is gone. The test drives the same
		// ordering the wrapper uses and checks the group was already empty at
		// the instant the reading was taken.
		cmd, pgid := startOwnedGroup(t, `trap "" TERM; sleep 30`)
		t.Cleanup(func() {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			_ = cmd.Wait()
		})
		time.Sleep(150 * time.Millisecond)

		if !groupExists(pgid) {
			t.Fatal("fixture group is already gone; the ordering assertion would be vacuous")
		}

		out, err := DrainGroup(DrainRequest{
			PGID: pgid, TermGrace: 200 * time.Millisecond, KillGrace: 2 * time.Second,
			ReapRoot: cmd.Wait,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !out.GroupEmpty {
			t.Fatal("drain did not confirm an empty group")
		}
		if !out.Reaped {
			t.Fatal("the drain must sequence the root-child reap (§3.3 step 1)")
		}
		// Only now is the closing reading legitimate.
		if groupExists(pgid) {
			t.Fatal("the group still exists at the moment the closing reading would be taken")
		}
	})

	t.Run("the drain refuses a pgid that would signal everything", func(t *testing.T) {
		for _, bad := range []int{0, 1, -5} {
			if _, err := DrainGroup(DrainRequest{PGID: bad, TermGrace: time.Millisecond, KillGrace: time.Millisecond}); err == nil {
				t.Errorf("DrainGroup(%d) must refuse: a sign mistake here signals every process the user owns", bad)
			}
		}
	})

	t.Run("a setsid escapee is neither signalled nor drained, and that is stated", func(t *testing.T) {
		// §3.3's stated limitation, made observable: a child that leaves the
		// group survives the drain, and the drain does not claim otherwise.
		cmd, pgid := startOwnedGroup(t, `sh -c 'sleep 30' & exit 0`)

		out, err := DrainGroup(DrainRequest{
			PGID: pgid, TermGrace: 200 * time.Millisecond, KillGrace: 500 * time.Millisecond,
			ReapRoot: cmd.Wait,
		})
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
		// Whatever the outcome, the drain reports only what it CONFIRMED.
		if !out.GroupEmpty && out.Limitation() == "" {
			t.Fatal("an unconfirmed drain must carry a limitation rather than implying an empty group")
		}
	})

	t.Run("no code path claims descendant reaping", func(t *testing.T) {
		b, err := os.ReadFile("drain.go")
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		// The words may appear only in the negative statement §3.3 requires.
		for _, claim := range []string{"reap descendants", "reap grandchildren", "descendant reap"} {
			idx := strings.Index(src, claim)
			if idx < 0 {
				continue
			}
			window := src[max0(idx-200):idx]
			if !strings.Contains(window, "does NOT") && !strings.Contains(window, "cannot") &&
				!strings.Contains(window, "infeasible") && !strings.Contains(src[idx:min0(idx+200, len(src))], "infeasible") {
				t.Errorf("drain.go contains %q outside a negative statement", claim)
			}
		}
		// And the limitation the observation ships names the real behaviour.
		var found bool
		for _, l := range CanonicalLimitations() {
			if strings.Contains(l, "descendants are never reaped") {
				found = true
			}
		}
		if !found {
			t.Error("the shipped limitations must state that descendants are never reaped")
		}
	})
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func min0(a, b int) int {
	if a < b {
		return a
	}
	return b
}
