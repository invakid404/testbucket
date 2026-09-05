// Package vitestrunner is testbucket's Vitest adapter — adapter #2 for the
// runner.Runner seam, alongside the Go adapter (internal/runner/gorunner). It
// is the only package that knows about Vitest, `vitest list`, and the Vitest
// JSON reporter, and it owns its own run configuration (the invocation, the
// project root, the event-capture directory) so none of that leaks across the
// neutral seam into internal/core.
//
// A Vitest spec file is the unit of scheduling, as a Go package is for the Go
// adapter, and a whale file is name-sliced across buckets by test name:
//
//   - Discover: `vitest list --filesOnly --json` (glob) -> one LivePackage per
//     test file. `--filesOnly` resolves each project's include/exclude by GLOB
//     and returns the file specifiers WITHOUT importing or collecting a line of
//     test code (the CLI surface of Vitest's globTestSpecifications()). The
//     full-collection `vitest list --json` imports the whole module graph and can
//     DEADLOCK on a multi-project config; glob discovery is immune, so it is the
//     default. `--vitest-discovery=list` opts back into the importing path.
//   - Runnables: for a whale being name-sliced, `vitest list <file> --json
//     [--project <the file's project>]` -> that file's `" > "`-joined test names.
//     The file positional scopes the (necessarily importing) collection to the one
//     spec, not the whole project (201s -> 30s on a 1,398-file project); --project
//     keeps it from inheriting a sibling project's collection deadlock. The file
//     leads because `--json` takes an optional path value, so a file after it is
//     swallowed as an output file. The file->project map comes from the same
//     deadlock-safe glob. A brand-new test in the whale is therefore seen live, so
//     the never-drop gate stays sound under glob default.
//   - ParseTimings: the Vitest JSON reporter -> per-file wall-time weights plus
//     per-test durations for name-slicing;
//   - Render: a bucket -> `vitest run <files>` for whole files, plus one
//     `vitest run -t '<robust regex>' <file>` per name slice (files serial by
//     default, so a bucket's wall time is the sum the balancer partitioned;
//     see #22 / fileParallelism to bound intra-bucket concurrency instead).
//
// The correctness subtlety name-slicing turns on is that a Vitest test's name
// takes three divergent forms — `vitest list`'s `" > "`-joined path, the
// reporter's space-joined fullName, and the string -t matches — so this adapter
// keys everything on the first, renders a -t robust to the ambiguity between
// them (names.go), and refuses to slice a file whose names collide under that
// projection. A count-shard is still refused: there is no repeat sweep to divide.
//
// internal/core is UNCHANGED by this package: it is a second implementation of
// the same six-method interface against the same value types.
package vitestrunner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
	// observed is the discovery invocation this runner actually issued, kept
	// so the bundle records the command that ran rather than one the caller
	// reconstructs from the same flags a second time.
	observed *DiscoveryProvenance
	tool     nodetool // the base vitest command; bounds discovery and Runnables
	// discoveryMode selects the built-in discovery invocation (glob | list). It
	// is ignored when discoveryCmd is set.
	discoveryMode string
	// discoveryCmd, when non-empty, is run VERBATIM for discovery (it owns its
	// own subcommand and flags) instead of appending to the base command — the
	// escape hatch for a consumer whose wrapper already answers discovery.
	discoveryCmd []string
	root         string // absolute project dir, for subprocesses and id relativisation
	render       renderConfig
	// frozen, when set, replaces every discovery/listing subprocess with the
	// byte-exact snapshots a Stage-1 planning-input bundle bound. See
	// FrozenInputs.
	frozen *FrozenInputs
	// projectByFile maps a file id to its Vitest project name, resolved once
	// (lazily) from the deadlock-safe glob so Runnables can scope its importing
	// `vitest list` to a single project. nil until first resolved; "" for a file
	// in a single-project config (no --project scoping needed). Runnables is
	// called sequentially by the core, so no lock guards this.
	projectByFile map[string]string
	// runnablePaths is the resolved executable each runnable listing actually
	// ran, kept so the bundle's closure binds the binary that took the
	// snapshot rather than whatever the name resolves to afterwards.
	runnablePaths []ResolvedProgram
}

// ResolvedProgram is a program name and the executable path it resolved to
// when it ran.
type ResolvedProgram struct {
	Name string
	Path string
}

// RunnablePaths reports the executables the runnable listings ran.
func (r *Runner) RunnablePaths() []ResolvedProgram { return r.runnablePaths }

func appendPath(list []ResolvedProgram, p ResolvedProgram) []ResolvedProgram {
	for _, e := range list {
		if e.Name == p.Name && e.Path == p.Path {
			return list
		}
	}
	return append(list, p)
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
	// FileParallelism bounds intra-bucket file concurrency (#22). 0 or 1 keeps
	// Vitest serial (--no-file-parallelism), the sum-of-weights model the balancer
	// packs to; a value >1 renders --maxWorkers=N instead.
	FileParallelism int
	// WallDir, when set, renders each invocation under `testbucket wall exec`
	// with that records directory. Empty — the default — renders the v0.2.2
	// bytes unchanged.
	WallDir string
	// Env is the exact environment every subprocess this runner spawns is
	// given, as KEY=VALUE. nil inherits the ambient environment, which is what
	// a caller that is not binding a plan wants; `wall bundle` supplies the
	// same set it retains, so the recorded environment is the one that ran.
	Env []string
	// Frozen, when set, makes discovery and runnable listing read BOUND BYTES
	// instead of running Vitest. It is how a plan is replayed from a frozen
	// input bundle: the same parsers run over the same bytes, so the plan is a
	// function of its recorded inputs rather than of whatever the tree and the
	// clock happened to say at the time.
	Frozen *FrozenInputs
}

// FrozenInputs is the byte-exact discovery and runnable-listing evidence a
// planning-input bundle carries.
//
// A missing entry is a LOUD ERROR, never a fall back to running Vitest: the
// whole point of a frozen plan is that no unbound input can reach it, and a
// silent live listing is exactly the unbound input that would.
type FrozenInputs struct {
	// Discovery is the raw discovery JSON, exactly as the recorded subprocess
	// printed it.
	Discovery []byte
	// Runnables maps a file id to the raw `vitest list --json` bytes for that
	// file. Only name-sliced targets need an entry.
	Runnables map[string][]byte
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
		tool:          nodetool{command: cmd, timeout: discTimeout, env: opt.Env},
		discoveryMode: mode,
		discoveryCmd:  append([]string(nil), opt.DiscoveryCommand...),
		frozen:        opt.Frozen,
		root:          abs,
		render: renderConfig{
			command:         cmd,
			rootRel:         rootRel,
			eventsDir:       opt.EventsDir,
			fileParallelism: opt.FileParallelism,
			wallDir:         opt.WallDir,
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

// Runnables lists one file's test names — the universe the planner slices a
// whale over and the never-drop gate checks the slices against. It refuses a
// file whose names cannot be told apart by a -t filter (see runnableNames).
//
// It cannot use glob discovery (that returns no test names) so it must import;
// but it imports as LITTLE as possible. Two scopes stack:
//
//   - the file id is passed as a positional FILE FILTER (`./`-prefixed so a
//     root-level id starting with '-' is not misread as an option), so Vitest
//     collects only that one spec instead of the whole project — on a 1,398-file
//     project that is the difference between importing every module to read one
//     file's names (201s measured) and importing one (30s). runnableNames still
//     filters to the file's exact rows, so a substring over-match cannot add a
//     sibling's names.
//   - `--project <name>` scopes to the file's OWN Vitest project, so a sibling
//     project's collection deadlock — the exact hang glob discovery exists to
//     avoid — cannot reach this call. The file->project map is resolved once from
//     the same deadlock-safe glob. A single-project config has no project name, so
//     `--project` is omitted; there is no sibling to deadlock on there.
//
// The file id MUST lead, before --json. Vitest's `--json` takes an OPTIONAL value
// (`--json [true|path]`): a file token immediately AFTER --json is swallowed as an
// output PATH, so `list --json <file>` writes the JSON report INTO <file> — it
// clobbers the test and prints nothing. Leading with the positional keeps --json
// bare (`list <file> --json [--project p]`), which is also the form that actually
// scopes; `list --json ... <file>` only survives when --project sits between them.
func (r *Runner) Runnables(ctx context.Context, p runner.LivePackage) ([]string, error) {
	if r.frozen != nil {
		raw, ok := r.frozen.Runnables[p.ID]
		if !ok {
			// Refusing here is the point: falling back to a live listing would
			// let an input the bundle never bound decide which tests a slice
			// selects.
			return nil, fmt.Errorf("vitest runnables: %s has no frozen listing in the planning-input bundle; a live listing is an unbound input", p.ID)
		}
		return runnableNames(r.root, p.ID, raw)
	}
	project, err := r.projectFor(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out, err := r.tool.run(ctx, r.root, runnablesArgs(project, p.ID)...)
	if err != nil {
		return nil, r.runnablesError(p.ID, project, err)
	}
	return runnableNames(r.root, p.ID, out)
}

// CaptureDiscovery returns the RAW discovery bytes, exactly as the subprocess
// printed them. It exists so a planning-input bundle can freeze the evidence
// rather than a parsed summary of it: the bundle's promise is that a replay
// runs the same parser over the same bytes, and that is only checkable if the
// bytes are what was kept.
//
// A runner already reading frozen inputs refuses: capturing from a capture
// would record the snapshot as if it were a fresh observation.
func (r *Runner) CaptureDiscovery(ctx context.Context) ([]byte, error) {
	if r.frozen != nil {
		return nil, fmt.Errorf("vitest: this runner is replaying a frozen bundle; there is nothing live to capture")
	}
	return r.runDiscovery(ctx)
}

// CaptureRunnables returns the raw `vitest list` bytes for one file, for the
// same reason.
//
// discovery is the ALREADY CAPTURED discovery snapshot. It is passed in rather
// than resolved so that scoping this listing to the file's own project reuses
// the one bound observation instead of taking a second, unbound one that could
// disagree with it.
// It returns the EXACT argv it ran alongside the bytes. The bundle records
// that argv as the listing's provenance, and a provenance record that names a
// command nobody ran is worse than none: a replay would reproduce it, get
// different bytes, and have no way to see why.
func (r *Runner) CaptureRunnables(ctx context.Context, fileID string, discovery []byte) ([]byte, []string, error) {
	if r.frozen != nil {
		return nil, nil, fmt.Errorf("vitest: this runner is replaying a frozen bundle; there is nothing live to capture")
	}
	if r.projectByFile == nil && len(discovery) > 0 {
		m, err := parseProjects(r.root, discovery)
		if err != nil {
			return nil, nil, err
		}
		r.projectByFile = m
	}
	project, err := r.projectFor(ctx, fileID)
	if err != nil {
		return nil, nil, err
	}
	args := runnablesArgs(project, fileID)
	var prov ExecProvenance
	out, err := r.tool.runWith(ctx, r.root, &prov, args...)
	if prov.Path != "" && len(prov.Argv) > 0 {
		r.runnablePaths = appendPath(r.runnablePaths, ResolvedProgram{Name: prov.Argv[0], Path: prov.Path})
	}
	if err != nil {
		return nil, nil, r.runnablesError(fileID, project, err)
	}
	return out, prov.Argv, nil
}

// runnablesArgs builds the `vitest list` invocation for one file's names:
// `list <file> --json [--project <p>]`. The file id LEADS as a positional filter,
// which both scopes collection to that one spec and keeps it clear of `--json`'s
// optional value — a file token immediately after --json is swallowed as an output
// path (`--json [true|path]`), clobbering the file. --project is appended only for
// a multi-project file. It is a pure function of (project, fileID) so the arg order
// — the clobber-and-scope invariant — is unit-tested offline, no subprocess.
func runnablesArgs(project, fileID string) []string {
	args := []string{"list", filterPathArg(fileID), "--json"}
	if project != "" {
		args = append(args, "--project", project)
	}
	return args
}

// filterPathArg makes a root-relative file id safe to pass as a Vitest positional
// filter. Vitest/CAC reads a positional that begins with '-' as an OPTION, so a
// root-level spec whose id starts with '-' (e.g. `--odd.spec.ts`) fails the whole
// list with "Unknown option --odd" — a valid file the old whole-project path
// handled fine. Prefixing `./` turns the id into an unambiguous path token that
// Vitest resolves against the root and matches the SAME file; verified to keep
// normal ids scoping too, so it is applied uniformly. An id that is already an
// explicit path (relative `.`/`..`, or absolute on EITHER path flavour) is left
// as-is: it cannot be read as an option, and rerooting an absolute path would
// break the match.
//
// The absolute test has to cover the platform's own notion of absolute, not just
// a leading '/'. relID deliberately KEEPS an absolute path when filepath.Rel
// fails, which is exactly what happens for a discovered file on a different
// Windows volume: the id stays `D:/tests/a.vtest.ts`. That has no leading '.' or
// '/', so a naive check would hand Vitest `./D:/tests/a.vtest.ts` and reroot the
// file out of existence. filepath.IsAbs on the platform-native form catches it
// (true for `D:\tests\a.vtest.ts` on Windows). The leading-'/' check is kept
// alongside it because a POSIX-style absolute id is NOT filepath.IsAbs on
// Windows, and such an id can reach a Windows run through a live set recorded
// elsewhere.
//
// On POSIX the same `D:/tests/a.vtest.ts` is a genuinely RELATIVE path (a
// directory literally named `D:`), so `./` is the correct answer there — the
// behaviour differs by platform because the meaning does.
func filterPathArg(fileID string) string {
	if strings.HasPrefix(fileID, ".") || strings.HasPrefix(fileID, "/") ||
		filepath.IsAbs(filepath.FromSlash(fileID)) {
		return fileID
	}
	return "./" + fileID
}

// projectFor resolves the Vitest project a file belongs to, lazily building the
// file->project map from a deadlock-safe glob and caching it. An empty string
// means a single-project config (no scoping needed).
func (r *Runner) projectFor(ctx context.Context, fileID string) (string, error) {
	if r.projectByFile == nil {
		if r.frozen != nil {
			// A frozen runner has bound discovery bytes; running a second
			// listing to answer the same question would be exactly the
			// unbound input the bundle exists to close.
			m, err := parseProjects(r.root, r.frozen.Discovery)
			if err != nil {
				return "", err
			}
			r.projectByFile = m
			return r.projectByFile[fileID], nil
		}
		out, err := r.tool.run(ctx, r.root, "list", "--filesOnly", "--json")
		if err != nil {
			return "", r.discoveryError(err)
		}
		m, err := parseProjects(r.root, out)
		if err != nil {
			return "", err
		}
		r.projectByFile = m
	}
	return r.projectByFile[fileID], nil
}

// runnablesError enriches a name-listing failure. A deadline hit here means that
// one file could not finish importing (a genuine hang in the code under test, not
// a sibling — the list is scoped to this file and its project), so the hint points
// at the file, not at glob mode.
func (r *Runner) runnablesError(fileID, project string, err error) error {
	var te *timeoutError
	if !errors.As(err, &te) {
		return err
	}
	scope := "single-project config"
	if project != "" {
		scope = fmt.Sprintf("project %q", project)
	}
	return fmt.Errorf(
		"listing test names for name-slice target %s timed out (%s): importing that file did not finish — "+
			"a module it imports may hang on import, or raise --discovery-timeout / TB_DISCOVERY_TIMEOUT if it is legitimately slow: %w",
		fileID, scope, err)
}

// CanonicalToken is the opaque comparability token weights are measured within.
// Vitest runs each test once under a fixed config, so the token is stable; the
// core treats it as a key and cold-starts the store if it ever changes.
func (r *Runner) CanonicalToken() string { return "vitest" }
