// Package gorunner is testbucket's Go adapter — adapter #1 for the
// runner.Runner seam. It is the only package that knows about `go`, `go test`,
// `go list`, GOWORK and the `go test -json` event stream, and it owns its own
// run configuration (race, sweep count, timeout, node detection) so none of
// that leaks across the neutral seam into internal/core.
//
// Everything it does falls into the four jobs the seam names:
//
//   - Discover: `go list ./...` over the module set -> the live target set;
//   - Runnables: `go test -list` -> a target's selectable runnable names;
//   - ParseTimings: `go test -json` -> a RunSummary of per-target/per-runnable
//     elapsed times;
//   - Render: a planned bucket -> the concrete `go test` commands;
//
// plus the two small Go-specific facts the core needs from any adapter:
// ValidateUnit (does this unit's rendered command run what it claims?) and
// CanonicalToken (the opaque comparability token weights are measured within).
//
// A second adapter (e.g. Vitest) is a sibling package implementing the same
// interface; nothing in internal/core changes when it drops in.
package gorunner

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/invakid404/testbucket/internal/runner"
)

// Runner is the Go adapter. It carries the toolchain deadline, the module-set
// exclusions, the repo root the `go` subprocesses run under, and its own render
// configuration.
type Runner struct {
	tc       toolchain
	excludes []string
	repoRoot string
	rootErr  error
	render   renderConfig
}

// Options configures the Go adapter. Discovery/toolchain settings and the run
// configuration (what the emitted `go test` commands look like) both live here,
// because both are the adapter's business.
type Options struct {
	// ToolchainTimeout bounds each `go` subprocess; 0 disables the deadline.
	ToolchainTimeout time.Duration
	// Excludes is the module-set exclusion list; nil uses the (empty) defaults.
	Excludes []string
	// Dir is where discovery starts walking up from for the repo root; ""
	// means the process working directory.
	Dir string

	// --- run configuration for the emitted `go test` commands ---

	// Race renders the data-race detector envelope (-race) and is part of the
	// comparability token.
	Race bool
	// Count is the flake-sweep base count (go test -count); count-shards divide
	// it, and it is part of the comparability token.
	Count int
	// Timeout is spliced verbatim into each invocation (a Go duration string).
	Timeout string
	// EventsDir, when set, makes each invocation emit its `go test -json` stream
	// and tee it into this directory for a later ingest.
	EventsDir string
	// NodePrefixes are target-dir prefixes whose buckets need Node set up. Empty
	// by default — a consumer opts in explicitly.
	NodePrefixes []string
}

// New builds the Go adapter, resolving the repo root once. A missing root is
// not fatal here — the --live paths and ParseTimings do not need it — but every
// method that runs the toolchain surfaces it.
func New(opt Options) (*Runner, error) {
	tc, err := newToolchain(opt.ToolchainTimeout)
	if err != nil {
		return nil, err
	}
	if err := validateTestTimeout(opt.Timeout); err != nil {
		return nil, err
	}
	ex := opt.Excludes
	if len(ex) == 0 {
		ex = defaultExcludedModules
	}
	dir := opt.Dir
	if dir == "" {
		dir = "."
	}
	root, rootErr := findRepoRoot(dir)
	return &Runner{
		tc:       tc,
		excludes: ex,
		repoRoot: root,
		rootErr:  rootErr,
		render: renderConfig{
			race:         opt.Race,
			count:        opt.Count,
			timeout:      opt.Timeout,
			eventsDir:    opt.EventsDir,
			nodePrefixes: opt.NodePrefixes,
		},
	}, nil
}

// validateTestTimeout rejects a --timeout the emitted invocations could not use.
// The value is spliced verbatim into every command, so an unparsable one would
// fail every bucket of the matrix at once, far from where the typo is — this
// catches it at construction instead.
func validateTestTimeout(timeout string) error {
	if timeout == "" {
		return nil
	}
	d, err := time.ParseDuration(timeout)
	if err != nil {
		return fmt.Errorf("--timeout %q is not a Go duration: %w", timeout, err)
	}
	if d < 0 {
		return fmt.Errorf("--timeout must be >= 0, got %v", d)
	}
	return nil
}

// compile-time proof the adapter satisfies the seam.
var _ runner.Runner = (*Runner)(nil)

// RepoRoot reports the resolved repo root and any error from resolving it, so
// the CLI can decide whether toolchain-backed discovery is possible.
func (r *Runner) RepoRoot() (string, error) { return r.repoRoot, r.rootErr }

// Discover enumerates the live target set via `go list ./...` over the module
// set, under the caller's context.
func (r *Runner) Discover(ctx context.Context) ([]runner.LivePackage, error) {
	if r.rootErr != nil {
		return nil, r.rootErr
	}
	mods, err := discoverModules(ctx, r.tc, r.repoRoot, r.excludes)
	if err != nil {
		return nil, err
	}
	return listPackages(ctx, r.tc, r.repoRoot, mods)
}

// Runnables resolves a target's complete top-level runnable set via
// `go test -list`, under the caller's context. A target flagged split=run
// cannot be sliced without a repo root, so the failure is surfaced with its
// real cause rather than a confusing toolchain error.
func (r *Runner) Runnables(ctx context.Context, p runner.LivePackage) ([]string, error) {
	if r.rootErr != nil {
		return nil, fmt.Errorf("cannot resolve the runnable set for %s (flagged split=run): no repo root: %w", p.ID, r.rootErr)
	}
	return listRunnableNames(ctx, r.tc, r.repoRoot, p)
}

// ParseTimings folds one or more `go test -json` streams into a RunSummary.
func (r *Runner) ParseTimings(readers ...io.Reader) (*runner.RunSummary, error) {
	return parseEvents(readers...)
}

// Render turns one planned bucket into the concrete `go test` invocations and
// shell script the CI job runs, using this adapter's render configuration.
func (r *Runner) Render(b runner.Bucket) runner.Rendered {
	return renderBucket(b, r.render)
}

// ValidateUnit is the command-grammar half of the never-drop gate for the Go
// runner. It checks each unit against the sweep base the core is planning at.
func (r *Runner) ValidateUnit(u runner.Unit, live map[string]runner.LivePackage, baseCount int) []string {
	return validateUnitGrammar(u, live, baseCount)
}

// CanonicalToken renders the opaque comparability token weights are measured
// within, from this adapter's own race + count.
func (r *Runner) CanonicalToken() string {
	return canonicalFlags(r.render.race, r.render.count)
}
