package runner

import (
	"context"
	"io"
)

// Rendered is what an adapter turns one bucket into: the concrete commands the
// CI job runs, plus the flags the matrix needs.
type Rendered struct {
	// NeedsNode is true when any invocation in the bucket needs Node set up.
	// The matrix carries it as `needs_node`; it is the adapter's own output
	// flag, driven by the adapter's configuration (see gorunner Options).
	NeedsNode bool
	// Invocations is the structured form (dir, env, args) — the audit-able
	// record of what a bucket runs.
	Invocations []Invocation
	// Script is the shell the CI job executes: the invocations wired together
	// with `set -euo pipefail`, per-command tees and status propagation.
	Script string
}

// Runner is the minimal seam the core needs a framework adapter to fill. It is
// deliberately small and deliberately NEUTRAL: no method takes a
// framework-specific argument. A runner's own run configuration — its flags,
// timeout, sweep count, and any setup detection — lives inside the adapter
// (supplied when it is constructed), never on the core or in this interface, so
// a non-Go adapter can be built without touching internal/core.
//
// gorunner is adapter #1 (Go's `go test`). A second adapter implements the same
// six methods against the same value types; nothing in internal/core changes
// when it drops in. See README.md, "Adding a framework adapter".
type Runner interface {
	// Discover enumerates the live test targets — for Go, `go list ./...` over
	// the module set. The authority on what must run. It honours the context's
	// cancellation and deadline.
	Discover(ctx context.Context) ([]LivePackage, error)

	// Runnables enumerates the selectable top-level runnable names inside one
	// target — for Go, `go test -list` — used to name-slice a whale. Called at
	// most for the few targets the store has flagged split=run. It honours the
	// context's cancellation and deadline.
	Runnables(ctx context.Context, p LivePackage) ([]string, error)

	// ParseTimings folds one or more of a run's timing streams — for Go,
	// `go test -json` — into a RunSummary the store ingests and the audit checks
	// against a plan.
	ParseTimings(readers ...io.Reader) (*RunSummary, error)

	// Render turns one planned bucket into the concrete invocations and shell
	// script the CI job runs, using the adapter's own render configuration.
	// Pure: no toolchain, no I/O.
	Render(b Bucket) Rendered

	// ValidateUnit is the command-grammar half of the never-drop gate: it checks
	// that a final unit renders to a command that actually runs what the unit
	// claims (a stray name filter, a zero sweep, a mismatched resolution
	// envelope). The core owns the structural half (every target in exactly one
	// bucket, shard-group completeness, the aggregate sweep); this owns the
	// per-command half, because only the adapter knows what its own command
	// grammar means. baseCount is the neutral sweep base the core is planning at,
	// so a per-unit "weakens the sweep" check stays consistent with the core's
	// aggregate check. It returns one message per defect, empty when the unit is
	// well-formed.
	ValidateUnit(u Unit, live map[string]LivePackage, baseCount int) []string

	// CanonicalToken renders the opaque comparability token weights are measured
	// within (for Go, "-race -count=100"). The core treats it as an opaque key:
	// the store cold-starts when it changes, which is the guard against the
	// "renamed job -> silently bad split" trap. The adapter builds it from its
	// own configuration, so its inputs never leak into this interface.
	CanonicalToken() string
}
