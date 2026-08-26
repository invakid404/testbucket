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
//   - Discover: `vitest list --filesOnly --json` -> one LivePackage per test
//     file. `--filesOnly` resolves each project's include/exclude by GLOB and
//     returns the file specifiers WITHOUT importing or collecting a line of test
//     code (it is the CLI surface of Vitest's globTestSpecifications()). The
//     older full-collection `vitest list --json` imports the whole module graph
//     and can DEADLOCK on a multi-project config; glob discovery is immune, so
//     it is the default. `--vitest-discovery=list` opts back into the importing
//     path (only its per-test names, unused by file-granularity bucketing today,
//     require collection);
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

// Discovery modes. glob is the default: it resolves test files without importing
// them, so it cannot inherit `vitest list`'s multi-project collection deadlock.
const (
	discoveryGlob = "glob" // `vitest list --filesOnly --json`
	discoveryList = "list" // `vitest list --json` (imports the module graph)
)

// Runner is the Vitest adapter.
type Runner struct {
	tool nodetool // the base vitest command; bounds discovery and Runnables
	// discoveryMode selects the built-in discovery invocation (glob | list). It
	// is ignored when discoveryCmd is set.
	discoveryMode string
	// discoveryCmd, when non-empty, is run VERBATIM for discovery (it owns its
	// own subcommand and flags) instead of appending to the base command — the
	// escape hatch for a consumer whose wrapper already answers discovery.
	discoveryCmd []string
	root         string // absolute project dir, for subprocesses and id relativisation
	render       renderConfig
}

// Options configures the Vitest adapter.
type Options struct {
	// Root is the Vitest project directory (where package.json / the config
	// live); "" means the process working directory. It is used relative for the
	// emitted script's `cd` and absolute for subprocesses / id relativisation.
	Root string
	// Command is how Vitest is invoked; nil defaults to ["npx", "vitest"]. It is
	// the RUN command and the base for built-in discovery, which appends
	// `list --filesOnly --json` (glob) or `list --json` (list).
	Command []string
	// DiscoveryMode selects the built-in discovery invocation: "glob" (default,
	// `vitest list --filesOnly`, no collection) or "list" (`vitest list --json`,
	// imports the graph). "" means glob.
	DiscoveryMode string
	// DiscoveryCommand, when non-empty, is run VERBATIM for discovery (program +
	// its own args, nothing appended) and must print the same [{file}] /
	// [{name,file}] JSON on stdout. It lets a wrapper that already owns its
	// subcommand serve discovery; DiscoveryMode is then ignored.
	DiscoveryCommand []string
	// Timeout bounds each Vitest subprocess; 0 disables the deadline. It is the
	// general per-command deadline; DiscoveryTimeout overrides it for discovery.
	Timeout time.Duration
	// DiscoveryTimeout bounds the discovery subprocess specifically; 0 falls back
	// to Timeout. It is separate so a stalled discovery fails fast even when the
	// run budget is generous — a deadlocked `vitest list` must not hang for the
	// whole job (the 14-minute silent hang this adapter is hardened against).
	DiscoveryTimeout time.Duration
	// EventsDir, when set, makes Render write each bucket's JSON report there for
	// a later ingest.
	EventsDir string
}

// New builds the Vitest adapter. The root is resolved to an absolute path but
// not required to exist here: the offline methods (Render, ParseTimings,
// ValidateUnit, CanonicalToken, LoadLivePackages) never touch the toolchain,
// and Discover / Runnables surface any problem when they run `vitest`.
func New(opt Options) (*Runner, error) {
	if err := validateTimeout("vitest timeout", opt.Timeout); err != nil {
		return nil, err
	}
	if err := validateTimeout("vitest discovery timeout", opt.DiscoveryTimeout); err != nil {
		return nil, err
	}
	mode, err := normalizeDiscoveryMode(opt.DiscoveryMode)
	if err != nil {
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
	// Discovery gets its own deadline: DiscoveryTimeout when set, else the general
	// Timeout. Discovery (and Runnables) are the ONLY subprocesses this adapter
	// runs — Render just emits a script CI executes — so the base tool's deadline
	// IS the discovery deadline.
	discTimeout := opt.DiscoveryTimeout
	if discTimeout == 0 {
		discTimeout = opt.Timeout
	}
	return &Runner{
		tool:          nodetool{command: cmd, timeout: discTimeout},
		discoveryMode: mode,
		discoveryCmd:  append([]string(nil), opt.DiscoveryCommand...),
		root:          abs,
		render: renderConfig{
			command:   cmd,
			rootRel:   rootRel,
			eventsDir: opt.EventsDir,
		},
	}, nil
}

func validateTimeout(label string, timeout time.Duration) error {
	if timeout < 0 {
		return fmt.Errorf("%s must be >= 0 (0 disables the deadline), got %v", label, timeout)
	}
	return nil
}

// normalizeDiscoveryMode defaults "" to glob and rejects anything but the two
// known modes, so a typo in --vitest-discovery fails loudly at construction
// rather than silently discovering nothing.
func normalizeDiscoveryMode(mode string) (string, error) {
	switch mode {
	case "":
		return discoveryGlob, nil
	case discoveryGlob, discoveryList:
		return mode, nil
	default:
		return "", fmt.Errorf("unknown vitest discovery mode %q (want %q or %q)", mode, discoveryGlob, discoveryList)
	}
}

// compile-time proof the adapter satisfies the seam.
var _ runner.Runner = (*Runner)(nil)

// Discover enumerates the live test files by GLOB (`vitest list --filesOnly
// --json`) — files resolved from each project's include/exclude without
// importing them, so multi-project collection cannot deadlock discovery. See
// discover for the mode/override/timeout handling.
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
