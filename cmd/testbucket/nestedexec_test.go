package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/invakid404/testbucket/internal/walltime"
)

// requireDelegatedRoot skips unless this host really has a usable delegated
// cgroup-v2 subtree, and returns it.
func requireDelegatedRoot(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("cgroup-v2 containment is a Linux property")
	}
	root := strings.TrimSpace(os.Getenv(walltime.CgroupRootEnv))
	if root == "" {
		t.Skipf("%s is unset: no delegated subtree to exercise the production path against", walltime.CgroupRootEnv)
	}
	if err := walltime.CheckCgroupDelegation(root); err != nil {
		t.Skipf("%s is not a usable delegation: %v", root, err)
	}
	return root
}

// containmentsUnder lists every testbucket containment directory beneath root,
// at any depth: a nested level lives inside its parent.
func containmentsUnder(t *testing.T, root, prefix string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A containment destroyed while the walk is running is not a leak.
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() && path != root && strings.HasPrefix(d.Name(), prefix) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// buildCLI builds the shipped command under test.
func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "testbucket")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build the CLI under test: %v\n%s", err, out)
	}
	return bin
}

// THE NESTED PRODUCTION EXECUTION, ON A REAL CGROUP-V2 KERNEL.
//
// The admission used to FREEZE the containment before `cmd.Start`, so the
// child was created stopped and the membership read could not race it. On a
// real kernel that deadlocks: Go's `Start` waits for the child to report over
// its close-on-exec error pipe, a child born frozen never reaches that point,
// and the parent is blocked inside `Start` and can never thaw. `wall exec`
// hung with the child parked in `do_freezer_trap`, and no cancellation could
// repair the ordering.
//
// The `wall begin` / `wall run` / `wall end` lifecycle never exercised it,
// because none of those spawns a measured child through `runChild`. This does:
// it runs the real nested `wall exec` inside a real delegated cgroup and
// requires bounded completion and cleanup on success, on failure and on
// cancellation.
func TestNestedWallExecCompletesAndCleansUpInARealDelegatedCgroup(t *testing.T) {
	root := requireDelegatedRoot(t)
	bin := buildCLI(t)
	dir := filepath.Join(t.TempDir(), "wall")

	cli := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	// A per-run bucket id, so this test's containments are distinguishable
	// from any other run's under the shared delegated root.
	actionTag := fmt.Sprintf("nested-%d", os.Getpid())
	if out, err := cli("wall", "begin", "--dir", dir, "--bucket-id", actionTag,
		"--campaign-id", "nested", "--run-id", "1", "--attempt-id", "1",
		"--repository", "invakid404/testbucket", "--workflow-run", "1",
		"--job", "nested", "--step", "run-bucket", "--step-attempt", "1"); err != nil {
		t.Fatalf("the action envelope would not open: %v\n%s", err, out)
	}
	t.Cleanup(func() { _, _ = cli("wall", "end", "--dir", dir, "--terminal", "failed", "--reason", "test cleanup") })

	// BOUNDED. A deadlock is the defect under test, so every case runs under a
	// deadline and a case that reaches it fails rather than hanging the suite.
	const bound = 90 * time.Second
	execCase := func(seq, desc string, argv ...string) (int, string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), bound)
		defer cancel()
		args := append([]string{"wall", "exec", "--dir", dir, "--level", "script",
			"--seq", seq, "--desc", desc, "--"}, argv...)
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = os.Environ()
		// WaitDelay so the deadline actually returns. A wrapper blocked in
		// `Start` with a frozen child still holds the output pipes, and
		// CombinedOutput would wait on those for ever after the kill — the
		// test would hang instead of reporting the deadlock it exists to find.
		cmd.WaitDelay = 15 * time.Second
		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("nested `wall exec` did not complete within %s — this is the freeze/Start deadlock:\n%s", bound, out)
		}
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		} else if err != nil {
			t.Fatalf("nested `wall exec` produced no process state: %v\n%s", err, out)
		}
		return code, string(out)
	}
	// SCOPED TO THIS TEST'S OWN ACTION. Another run's leftovers under the
	// shared delegated root are not this measurement's leak, and counting them
	// would make the result depend on what ran before.
	mine := containmentsUnder(t, root, "tb-action-"+actionTag)
	if len(mine) != 1 {
		t.Fatalf("expected exactly one action containment for this test, found %v", mine)
	}
	action := mine[0]
	// The action containment is open for the whole test, so only the nested
	// levels are leak candidates.
	requireNoNestedLeak := func(what string) {
		t.Helper()
		// A wrapper reaps its observers as it closes; give the exit a moment
		// to become observable before calling it a leak.
		deadline := time.Now().Add(20 * time.Second)
		for {
			left := append(containmentsUnder(t, action, "tb-script-"), containmentsUnder(t, action, "tb-invocation-")...)
			if len(left) == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("after %s the containment(s) %v were left behind; a level that outlives its wrapper is measured by nobody", what, left)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	t.Run("success", func(t *testing.T) {
		code, out := execCase("1", "success", "/bin/sh", "-c", "exit 0")
		if code != 0 {
			t.Fatalf("a nested measured child that succeeds reported %d:\n%s", code, out)
		}
		requireNoNestedLeak("a successful nested execution")
	})

	t.Run("failure", func(t *testing.T) {
		code, out := execCase("2", "failure", "/bin/sh", "-c", "exit 7")
		if code != 7 {
			t.Fatalf("a nested measured child that exits 7 reported %d:\n%s", code, out)
		}
		requireNoNestedLeak("a failed nested execution")
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), bound)
		defer cancel()
		// THE MEASURED CHILD SAYS WHEN IT IS RUNNING.
		//
		// Waiting for a containment directory to appear is not the same
		// question: another run's leftovers answer it, and the signal then
		// arrives while this wrapper is still building its level — which
		// measures the setup path rather than cancellation of a running
		// measurement. The child itself is the only witness that cannot be
		// confused with somebody else's.
		ready := filepath.Join(t.TempDir(), "running")
		cmd := exec.Command(bin, "wall", "exec", "--dir", dir, "--level", "script",
			"--seq", "3", "--desc", "cancelled", "--",
			"/bin/sh", "-c", "touch "+ready+"; sleep 60")
		cmd.Env = os.Environ()
		if err := cmd.Start(); err != nil {
			t.Fatalf("start the nested execution to cancel: %v", err)
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			if time.Now().After(deadline) {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal("the measured child never started, so the wrapper never got as far as running it")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatalf("signal the wrapper: %v", err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			t.Fatalf("a cancelled nested `wall exec` did not exit within %s; cancellation cannot repair a wrapper blocked in Start", bound)
		}
		// The measured child outlives nothing: the wrapper kills the whole
		// containment and waits for it before it returns.
		requireNoNestedLeak("a cancelled nested execution")
	})

	// AND THE ENVELOPE STILL CLOSES, leaving no containment at all.
	if out, err := cli("wall", "end", "--dir", dir, "--terminal", "passed"); err != nil {
		t.Fatalf("the action envelope would not close after the nested executions: %v\n%s", err, out)
	}
	if left := containmentsUnder(t, root, "tb-action-"+actionTag); len(left) > 0 {
		t.Errorf("after the envelope closed this test's containment(s) %v remain under %s", left, root)
	}
}

// A held child that is never released must NOT run the command.
//
// The barrier replaces a freeze, and it has to fail closed the same way the
// freeze did: a wrapper that dies before it can retain the containment proof
// leaves the pipe's write end closed, and the child must read that EOF as
// "this command must not run" rather than as permission to proceed.
func TestAHeldChildWhoseWrapperNeverReleasesItRunsNothing(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "it-ran")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "--", "/bin/sh", "-c", fmt.Sprintf("touch %q", marker))
	cmd.Env = append(os.Environ(), cliHoldEnv+"=1")
	cmd.ExtraFiles = []*os.File{r}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	// The wrapper dies without releasing: the write end closes and the child
	// sees EOF.
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Error("a held child whose barrier was never released exited successfully; it must refuse to run the command")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a held child whose barrier was never released ran the command anyway")
	}
}
