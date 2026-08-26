package vitestrunner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// nodetool runs Vitest CLI subprocesses, giving EACH ONE its own deadline
// derived from the caller's context — the same discipline the Go adapter's
// toolchain uses, and for the same reason: `plan` runs `vitest list` once and
// `vitest list <file>` per name-sliced target, and a single shared budget would
// let a slow-but-healthy call starve a later one. The caller's context is
// honoured on top of the per-command deadline, so cancelling a plan cancels the
// in-flight subprocess.
type nodetool struct {
	// command is how Vitest is invoked, e.g. ["npx", "vitest"]. The first
	// element is the program; the rest are leading args.
	command []string
	// timeout bounds each subprocess. Zero disables the per-command deadline.
	timeout time.Duration
}

// timeoutError is returned when a subprocess hit ITS OWN deadline (not the
// caller's cancellation). It is a distinct type so a caller — discovery, most of
// all — can recognise a deadline hit with errors.As and add domain guidance (a
// deadlocked `vitest list` collection reads as a hang, not a broken project).
type timeoutError struct {
	program string
	args    []string
	after   time.Duration
}

func (e *timeoutError) Error() string {
	return fmt.Sprintf("%s %s timed out after %s", e.program, strings.Join(e.args, " "), e.after)
}

func (t nodetool) context(ctx context.Context) (context.Context, context.CancelFunc) {
	if t.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, t.timeout)
}

// run executes one Vitest invocation from dir, under its own deadline, and
// returns its stdout. A deadline hit is reported as such so a timeout never
// reads as a broken project.
func (t nodetool) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if len(t.command) == 0 {
		return nil, fmt.Errorf("vitestrunner: no vitest command configured")
	}
	cctx, cancel := t.context(ctx)
	defer cancel()

	full := append(append([]string(nil), t.command[1:]...), args...)
	cmd := exec.CommandContext(cctx, t.command[0], full...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the child in its OWN process group. `vitest` is `npx` -> `node` ->
	// worker processes; a deadlocked collection is the workers, not npx. The
	// default context cancellation kills only the direct child (npx), leaving the
	// node workers alive and STILL HOLDING the stdout pipe — so cmd.Wait blocks on
	// the pipe forever and the deadline never actually fires (the exact hang #25
	// is about). Killing the whole group on cancel reaps the workers too; WaitDelay
	// is a belt-and-suspenders backstop that force-closes the pipes and returns
	// even if some descendant escapes the group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Negative pid => the whole process group (the child is its leader).
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return err
		}
		return os.ErrProcessDone
	}
	cmd.WaitDelay = 10 * time.Second

	if err := cmd.Run(); err != nil {
		if cctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			return nil, &timeoutError{program: t.command[0], args: full, after: t.timeout}
		}
		return nil, fmt.Errorf("%s %s: %w: %s", t.command[0], strings.Join(full, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
