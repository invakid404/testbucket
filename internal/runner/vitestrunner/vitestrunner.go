// Package vitestrunner is testbucket's Vitest adapter — adapter #2 for the
// runner.Runner seam, alongside the Go adapter (internal/runner/gorunner). It
// is the only package that knows about Vitest, `vitest list`, and the Vitest
// JSON reporter, and it owns its own run configuration (the invocation, the
// project root, the event-capture directory) so none of that leaks across the
// neutral seam into internal/core.
//
// It buckets at FILE granularity — a Vitest spec file is the unit of
// scheduling, as a Go package is for the Go adapter:
//
//   - Discover: `vitest list --json` -> one LivePackage per test file;
//   - ParseTimings: the Vitest JSON reporter -> per-file wall-time weights;
//   - Render: a bucket -> `vitest run <the bucket's files>` (files serial, so a
//     bucket's wall time is the sum the balancer partitioned).
//
// Name-slicing a whale spec by test name is a documented follow-up (it needs
// -t regex escaping plus duplicate-title / nested-describe handling); until
// then ValidateUnit refuses run-slice and count-shard units so the never-drop
// gate stays a real backstop.
//
// internal/core is UNCHANGED by this package: it is a second implementation of
// the same six-method interface against the same value types.
package vitestrunner

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/invakid404/testbucket/internal/runner"
)

// Runner is the Vitest adapter.
type Runner struct {
	tool   nodetool
	root   string // absolute project dir, for subprocesses and id relativisation
	render renderConfig
}

// Options configures the Vitest adapter.
type Options struct {
	// Root is the Vitest project directory (where package.json / the config
	// live); "" means the process working directory. It is used relative for the
	// emitted script's `cd` and absolute for subprocesses / id relativisation.
	Root string
	// Command is how Vitest is invoked; nil defaults to ["npx", "vitest"].
	Command []string
	// Timeout bounds each Vitest subprocess; 0 disables the deadline.
	Timeout time.Duration
	// EventsDir, when set, makes Render write each bucket's JSON report there for
	// a later ingest.
	EventsDir string
}

// New builds the Vitest adapter. The root is resolved to an absolute path but
// not required to exist here: the offline methods (Render, ParseTimings,
// ValidateUnit, CanonicalToken, LoadLivePackages) never touch the toolchain,
// and Discover / Runnables surface any problem when they run `vitest`.
func New(opt Options) (*Runner, error) {
	if err := validateTimeout(opt.Timeout); err != nil {
		return nil, err
	}
	cmd := opt.Command
	if len(cmd) == 0 {
		cmd = []string{"npx", "vitest"}
	}
	rootRel := opt.Root
	if rootRel == "" {
		rootRel = "."
	}
	abs, err := filepath.Abs(rootRel)
	if err != nil {
		return nil, err
	}
	return &Runner{
		tool: nodetool{command: cmd, timeout: opt.Timeout},
		root: abs,
		render: renderConfig{
			command:   cmd,
			rootRel:   rootRel,
			eventsDir: opt.EventsDir,
		},
	}, nil
}

func validateTimeout(timeout time.Duration) error {
	if timeout < 0 {
		return fmt.Errorf("vitest timeout must be >= 0 (0 disables the deadline), got %v", timeout)
	}
	return nil
}

// compile-time proof the adapter satisfies the seam.
var _ runner.Runner = (*Runner)(nil)

// Discover enumerates the live test files via `vitest list --json`.
func (r *Runner) Discover(ctx context.Context) ([]runner.LivePackage, error) {
	return r.discover(ctx)
}

// Runnables lists one file's test names via `vitest list --json`. The Vitest
// split model does not name-slice yet, so this is unused by planning today, but
// the method is implemented so a future name-slicing pass has its data source.
func (r *Runner) Runnables(ctx context.Context, p runner.LivePackage) ([]string, error) {
	out, err := r.tool.run(ctx, r.root, "list", "--json")
	if err != nil {
		return nil, err
	}
	return runnableNames(r.root, p.ID, out)
}

// CanonicalToken is the opaque comparability token weights are measured within.
// Vitest runs each test once under a fixed config, so the token is stable; the
// core treats it as a key and cold-starts the store if it ever changes.
func (r *Runner) CanonicalToken() string { return "vitest" }
