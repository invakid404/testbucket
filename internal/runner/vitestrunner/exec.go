package vitestrunner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
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
	if err := cmd.Run(); err != nil {
		if cctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			return nil, fmt.Errorf("%s %s timed out after %s (raise or disable the vitest timeout)",
				t.command[0], strings.Join(full, " "), t.timeout)
		}
		return nil, fmt.Errorf("%s %s: %w: %s", t.command[0], strings.Join(full, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
