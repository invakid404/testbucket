package gorunner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/invakid404/testbucket/internal/runner"
)

// defaultExcludedModules is the module set this adapter sweeps by default —
// empty, so out of the box every module under the repo root is in scope and
// nothing is silently dropped.
//
// A consumer repo scopes the set with --exclude-module (repeatable, glob):
// pinned or vendored sub-modules, cgo modules kept out of the pure-Go lane, and
// anything the integration suite already owns. The exclusion set is a scoping
// knob, not an escape hatch — anything inside it must be scheduled, and the set
// itself is echoed in the plan output. See excluded for the glob semantics
// (whole path elements, at any depth).
var defaultExcludedModules []string

// toolchain runs `go` subprocesses, giving EACH ONE its own deadline derived
// from the caller's context.
//
// The deadline is per subprocess, not per discovery pass, because that is what
// --toolchain-timeout promises and what actually protects the job: `plan` runs
// `go work edit`, one `go list` per module and one `go test -list` per
// name-sliced package, all sequentially. A single shared context.WithTimeout
// would turn the flag into a budget for the whole sweep, so a slow-but-healthy
// `go list` could consume it and make a later `go test -list` fail the instant
// it started — a false failure charged to the wrong command. The caller's
// context is still honoured on top of the per-command deadline, so cancelling a
// plan cancels the in-flight subprocess.
type toolchain struct {
	// timeout bounds each subprocess. Zero disables the per-command deadline.
	timeout time.Duration
}

// context returns a FRESH per-command deadline derived from the caller's ctx.
func (t toolchain) context(ctx context.Context) (context.Context, context.CancelFunc) {
	if t.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, t.timeout)
}

// run executes one `go` invocation under its own deadline and returns its
// stdout. A deadline hit is reported as such, naming the flag, so a timeout
// never reads as a broken repository.
func (t toolchain) run(ctx context.Context, dir string, extraEnv []string, args ...string) ([]byte, error) {
	cctx, cancel := t.context(ctx)
	defer cancel()

	cmd := exec.CommandContext(cctx, "go", args...)
	cmd.Dir = dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if cctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			return nil, fmt.Errorf("go %s timed out after %s (raise or disable --toolchain-timeout)",
				strings.Join(args, " "), t.timeout)
		}
		return nil, fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// validateToolchainTimeout keeps ZERO as the only way to opt out of the
// subprocess deadline. A negative duration parses fine and would previously
// have been treated as "disabled", so a typo (-10m for 10m) silently removed
// the hang protection the deadline exists to provide.
func validateToolchainTimeout(timeout time.Duration) error {
	if timeout < 0 {
		return fmt.Errorf("--toolchain-timeout must be >= 0 (0 disables the deadline), got %v", timeout)
	}
	return nil
}

// newToolchain builds the subprocess runner, rejecting a timeout the flag does
// not allow. It re-validates rather than trusting its caller: the check is the
// whole point, so it must not be possible to build an unbounded runner by
// reaching this down a path that forgot to validate.
func newToolchain(timeout time.Duration) (toolchain, error) {
	if err := validateToolchainTimeout(timeout); err != nil {
		return toolchain{}, err
	}
	return toolchain{timeout: timeout}, nil
}

type moduleSpec struct {
	Dir    string // repo-relative; "." for the root module
	Mode   string // runner.ModeWork | runner.ModeOff
	Atomic bool
}

// findRepoRoot walks up from dir looking for the workspace file, then the git
// dir. Everything this tool emits is expressed relative to that root.
func findRepoRoot(dir string) (string, error) {
	cur, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.work")); err == nil {
			return cur, nil
		}
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("no go.work or .git found above %s", dir)
		}
		cur = parent
	}
}

// workspaceMembers reads go.work through `go work edit -json` rather than
// parsing it: the toolchain is the only correct parser of the directive it
// wraps.
func workspaceMembers(ctx context.Context, tc toolchain, repoRoot string) (map[string]bool, error) {
	workFile := filepath.Join(repoRoot, "go.work")
	if _, err := os.Stat(workFile); err != nil {
		// Any stat failure other than absence — a permission problem, an I/O
		// error — must NOT be read as "there is no workspace". Doing so would
		// flip every module to GOWORK=off and pack each as a whole-module atom,
		// silently rescheduling the entire tree. There is no coverage-gate
		// backstop for this: it changes discovery before the final plan is ever
		// checked.
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat go.work in %s: %w", repoRoot, err)
		}
		// Stat FOLLOWS symlinks, so ENOENT here is ambiguous: either there is no
		// directory entry at all, or there is one that points at nothing. Only
		// the first is a repo that legitimately has no workspace; the second is
		// a broken workspace and must be loud. Lstat answers the question by not
		// following the link.
		if _, lerr := os.Lstat(workFile); lerr == nil {
			return nil, fmt.Errorf("go.work in %s is a dangling symlink: %w", repoRoot, err)
		} else if !os.IsNotExist(lerr) {
			return nil, fmt.Errorf("lstat go.work in %s: %w", repoRoot, lerr)
		}
		// No directory entry at all: a repo with no workspace file is a
		// legitimate shape, and every module then resolves standalone.
		return map[string]bool{}, nil
	}
	out, err := tc.run(ctx, repoRoot, nil, "work", "edit", "-json")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Use []struct {
			DiskPath string `json:"DiskPath"`
		} `json:"Use"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("parse go work edit -json: %w", err)
	}
	members := map[string]bool{}
	for _, u := range doc.Use {
		members[path.Clean(filepath.ToSlash(u.DiskPath))] = true
	}
	return members, nil
}

// discoverModules finds every go.mod under repoRoot that the module set
// includes, and tags each with the resolution mode its packages must run in.
func discoverModules(ctx context.Context, tc toolchain, repoRoot string, excludes []string) ([]moduleSpec, error) {
	members, err := workspaceMembers(ctx, tc, repoRoot)
	if err != nil {
		return nil, err
	}
	var mods []moduleSpec
	err = filepath.WalkDir(repoRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// testdata is skipped for the same reason the Go toolchain ignores
			// it: a go.mod fixture there is data, not a module of this repo.
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, filepath.Dir(p))
		if err != nil {
			return err
		}
		rel = path.Clean(filepath.ToSlash(rel))
		if excluded(rel, excludes) {
			return nil
		}
		spec := moduleSpec{Dir: rel, Mode: runner.ModeOff, Atomic: true}
		if members[rel] {
			spec.Mode = runner.ModeWork
			spec.Atomic = false
		}
		mods = append(mods, spec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].Dir < mods[j].Dir })
	return mods, nil
}

// excluded matches a repo-relative dir against the exclusion patterns,
// including anything nested beneath an excluded module at ANY depth.
//
// The pattern is tested against the dir and against every ancestor of it, which
// is the only formulation that works for a glob: `*` does not cross `/`, so a
// module two levels under an excluded adapter must still be caught. Matching
// ancestors cannot over-exclude: a pattern only ever matches a COMPLETE path
// element sequence.
func excluded(rel string, patterns []string) bool {
	for _, pat := range patterns {
		pat = strings.TrimSpace(strings.TrimSuffix(pat, "/"))
		if pat == "" {
			continue
		}
		// Normalise the pattern the same way rel is normalised, or a perfectly
		// reasonable `--exclude-module ./nativeserve` matches no cleaned dir and
		// the exclusion silently does nothing.
		pat = path.Clean(pat)
		for cur := path.Clean(rel); ; cur = path.Dir(cur) {
			if ok, _ := path.Match(pat, cur); ok {
				return true
			}
			if !strings.Contains(cur, "/") {
				break
			}
		}
	}
	return false
}

// atomKeyFor is the co-scheduling key the Go adapter stamps onto a discovered
// target: the module directory for a target that must be packed whole (GOWORK=off
// or otherwise atomic), and empty for a workspace target that mixes freely. It
// is the ONE place the Go module rule is turned into the neutral Atom the core
// groups by.
func atomKeyFor(mode, module string, atomic bool) string {
	if atomic || mode == runner.ModeOff {
		return module
	}
	return ""
}

// listPackages resolves the live package set — the authority on what must run.
// A package with no _test.go files is reported with HasTests=false: it is not
// bucketed (running it is a no-op) but the moment it gains a test file the next
// `go list` schedules it, with no store update needed.
func listPackages(ctx context.Context, tc toolchain, repoRoot string, mods []moduleSpec) ([]runner.LivePackage, error) {
	var out []runner.LivePackage
	seen := map[string]bool{}
	for _, m := range mods {
		const format = "{{.ImportPath}}\t{{.Dir}}\t{{len .TestGoFiles}}\t{{len .XTestGoFiles}}"
		var env []string
		if m.Mode == runner.ModeOff {
			env = []string{"GOWORK=off"}
		}
		stdout, err := tc.run(ctx, filepath.Join(repoRoot, filepath.FromSlash(m.Dir)), env, "list", "-f", format, "./...")
		if err != nil {
			return nil, fmt.Errorf("module %s (mode=%s): %w", m.Dir, m.Mode, err)
		}
		for _, line := range strings.Split(string(stdout), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) < 4 {
				return nil, fmt.Errorf("go list in %s: unexpected line %q", m.Dir, line)
			}
			nTest, _ := strconv.Atoi(fields[2])
			nXTest, _ := strconv.Atoi(fields[3])
			dir, err := filepath.Rel(repoRoot, fields[1])
			if err != nil {
				return nil, fmt.Errorf("relativize %s: %w", fields[1], err)
			}
			p := runner.LivePackage{
				ID:       fields[0],
				Atom:     atomKeyFor(m.Mode, m.Dir, m.Atomic),
				HasTests: nTest+nXTest > 0,
				Dir:      path.Clean(filepath.ToSlash(dir)),
				Module:   m.Dir,
				Mode:     m.Mode,
			}
			// A package reachable from two modules (e.g. through a replace) must
			// be scheduled once, not twice.
			if seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// runnablePrefixes are the top-level name prefixes that `go test -run` actually
// selects: tests, examples and fuzz targets. Benchmarks are deliberately absent
// — `-run` does not select them (`-bench` does), so putting a Benchmark name
// into a slice's alternation would cover nothing while claiming a weight the
// slicer would then balance around.
var runnablePrefixes = []string{"Test", "Example", "Fuzz"}

// isRunnable reports whether a name listed by `go test -list` is something the
// emitted `-run` alternation can actually select.
func isRunnable(name string) bool {
	for _, pre := range runnablePrefixes {
		if strings.HasPrefix(name, pre) {
			return true
		}
	}
	return false
}

// listRunnableNames enumerates a package's complete top-level RUNNABLE set —
// every name the emitted `-run '^(...)$'` would select. It is only ever called
// for packages the store has flagged split=run — at most a couple — because it
// compiles the test binary.
//
// The universe must be enumerated with the SAME selection semantics as the
// invocation it feeds, or the slices are complete against a set narrower than
// the one that runs. `go test -run` selects tests, examples AND fuzz targets;
// listing only `^Test` would leave a package's ExampleXxx in no slice at all.
// So the list is taken wide (`-list '.*'`) and narrowed here by the documented
// `-run` rule. Using the toolchain instead of grepping respects build tags and
// lists only examples the test binary actually registers.
func listRunnableNames(ctx context.Context, tc toolchain, repoRoot string, p runner.LivePackage) ([]string, error) {
	target := p.ID
	dir := repoRoot
	var env []string
	if p.Mode == runner.ModeOff {
		dir = filepath.Join(repoRoot, filepath.FromSlash(p.Module))
		env = []string{"GOWORK=off"}
		target = pattern(p)
	}
	stdout, err := tc.run(ctx, dir, env, "test", "-list", ".*", target)
	if err != nil {
		return nil, fmt.Errorf("package %s: %w", p.ID, err)
	}
	var names []string
	for _, line := range strings.Split(string(stdout), "\n") {
		line = strings.TrimSpace(line)
		// -list prints one name per line, then a trailing "ok <pkg> <t>".
		if line == "" || strings.ContainsAny(line, " \t") || !isRunnable(line) {
			continue
		}
		names = append(names, line)
	}
	sort.Strings(names)
	return names, nil
}

// liveJSON is one entry of a --live file. The --live schema is a GO-ADAPTER
// concern (the neutral runner.LivePackage type stays clean), and this struct
// accepts BOTH schemas so a file written by any version of the tool still loads:
//
//   - the current neutral schema: "id" + "atom";
//   - the historical schema this tool was extracted from: "import_path" +
//     "atomic" (a bool).
//
// The backing fields (dir/module/mode/has_tests) carry the same tag in both, so
// they need no aliasing. Decoding straight into runner.LivePackage would have
// SILENTLY IGNORED the historical tags, leaving every identity empty — which at
// plan time collides all packages and at ingest time prunes the whole store.
type liveJSON struct {
	// current neutral schema
	ID   string `json:"id"`
	Atom string `json:"atom"`
	// historical Go-adapter schema
	ImportPath string `json:"import_path"`
	Atomic     bool   `json:"atomic"`
	// backing fields (identical tags in both schemas)
	HasTests bool   `json:"has_tests"`
	Dir      string `json:"dir"`
	Module   string `json:"module"`
	Mode     string `json:"mode"`
}

// LoadLivePackages reads a live set from a JSON file instead of shelling out to
// the toolchain. It exists so `plan` can be run — and reviewed — against a
// recorded tree with no build, which is also how the tests drive it.
//
// It accepts both the current and the historical --live schema, normalises to
// the neutral runner.LivePackage, and REJECTS any entry with no resolvable
// identity before returning — never proceeding with an empty package id, which
// would break coverage and silently prune the timing store.
func LoadLivePackages(path string) ([]runner.LivePackage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read live set: %w", err)
	}
	var raw []liveJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse live set %s: %w", path, err)
	}
	live := make([]runner.LivePackage, 0, len(raw))
	for i, e := range raw {
		mode := e.Mode
		if mode == "" {
			mode = runner.ModeWork
		}
		module := e.Module
		if module == "" {
			module = "."
		}
		// Identity: the neutral id, falling back to the historical import_path.
		// An entry with neither is malformed — proceeding would give it an EMPTY
		// identity that collides with every other empty one and, at ingest,
		// prunes the whole store. Refuse loudly instead of dropping data.
		id := e.ID
		if id == "" {
			id = e.ImportPath
		}
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("live set %s: entry %d has no identity (neither %q nor %q); refusing to proceed with an empty package id", path, i, "id", "import_path")
		}
		// Co-scheduling key: the neutral atom, else derived from the historical
		// atomic bool / GOWORK=off mode.
		atom := e.Atom
		if atom == "" && (e.Atomic || mode == runner.ModeOff) {
			atom = module
		}
		live = append(live, runner.LivePackage{
			ID:       id,
			Atom:     atom,
			HasTests: e.HasTests,
			Dir:      e.Dir,
			Module:   module,
			Mode:     mode,
		})
	}
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })
	return live, nil
}

// pattern renders the package pattern to pass to `go test`, relative to the
// module directory the invocation runs from.
func pattern(p runner.LivePackage) string {
	rel := relDir(p.Module, p.Dir)
	if rel == "." {
		return "."
	}
	return "./" + rel
}

func relDir(moduleDir, pkgDir string) string {
	moduleDir = path.Clean(moduleDir)
	pkgDir = path.Clean(pkgDir)
	if moduleDir == pkgDir {
		return "."
	}
	if moduleDir == "." {
		return pkgDir
	}
	if strings.HasPrefix(pkgDir, moduleDir+"/") {
		return strings.TrimPrefix(pkgDir, moduleDir+"/")
	}
	return pkgDir
}
