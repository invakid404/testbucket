package vitestrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
)

// listEntry is one row of Vitest discovery JSON. The full-collection shape
// (`vitest list --json`) carries a per-test {name, file}; the glob shape
// (`vitest list --filesOnly --json`) carries {file, projectName}. All three are
// decoded here; discovery reads File, Runnables reads Name, and the file->project
// map (parseProjects) reads ProjectName. A missing File is the loud-refusal case
// (see parseList).
type listEntry struct {
	// Name is a pointer so an ABSENT name field (nil — a truncated/reshaped row)
	// is distinguishable from a PRESENT-but-empty title (""). Vitest accepts
	// `test("", ...)` and `vitest list --json` reports it as `"name":""`, a LEGAL
	// runnable that must NOT be dropped from the slice universe (dropping it lets
	// the gate pass over an incomplete universe and silently lose that test).
	Name *string `json:"name"`
	File string  `json:"file"`
	// ProjectName is the Vitest project a file belongs to in a multi-project
	// config (empty for a single-project config). It lets Runnables scope its
	// importing `vitest list` to one project, so a sibling project's collection
	// deadlock cannot reach it — the whole reason glob discovery exists.
	ProjectName string `json:"projectName,omitempty"`
}

// quoteName renders a row's name for an error message, distinguishing an absent
// field (<absent>) from a present-but-empty title ("").
func quoteName(name *string) string {
	if name == nil {
		return "<absent>"
	}
	return fmt.Sprintf("%q", *name)
}

// relID turns an absolute test-file path into the neutral, machine-independent
// identity used as the store key: the path relative to the Vitest root, in
// slash form. A path already outside the root (or already relative) is returned
// slash-cleaned.
func relID(root, file string) string {
	if !filepath.IsAbs(file) {
		return filepath.ToSlash(filepath.Clean(file))
	}
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(file))
	}
	return filepath.ToSlash(rel)
}

// parseList reduces Vitest discovery JSON (either the glob `[{file}]` or the
// full-collection `[{name,file}]` shape — both are handled, File is all this
// reads) to the live target set: one LivePackage per test FILE (the Vitest unit
// of scheduling, as a Go package is the Go unit), keyed by its root-relative
// path. Atom is left empty for the common case — Vitest spec files mix freely
// across a single project for best balance — and is set only by
// assignFilterAtoms, for the files whose ids Vitest's positional FILE filters
// cannot tell apart (one id being a substring of another). Those must ride in
// one invocation or the shorter filter runs its mate a second time; see
// collide.go.
func parseList(root string, data []byte) ([]runner.LivePackage, error) {
	var rows []listEntry
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parse vitest list --json: %w", err)
	}
	seen := map[string]bool{}
	var live []runner.LivePackage
	for i, r := range rows {
		// A row that exists but carries no file is a truncated capture or a
		// reporter-schema change (name kept, file renamed). Dropping it would
		// silently lose a test, and an all-rows-lack-file document would yield an
		// empty authoritative live set — a vacuously-green coverage gate over a
		// plan with no tests. Refuse loudly instead; never continue past it. (An
		// empty array — a project with genuinely no tests — has no rows to
		// reject and returns an empty set, which is legitimate.)
		if strings.TrimSpace(r.File) == "" {
			return nil, fmt.Errorf("vitest list row %d has no file (name=%s); refusing to drop a test — the capture is truncated or the reporter schema changed", i, quoteName(r.Name))
		}
		id := relID(root, r.File)
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("vitest list row %d (file %q) has no resolvable identity", i, r.File)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		live = append(live, runner.LivePackage{ID: id, HasTests: true})
	}
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })
	assignFilterAtoms(live)
	return live, nil
}

// runnableNames extracts the test names of ONE file from a `vitest list --json`
// output, in sorted order — the universe the never-drop gate checks a file's
// name slices against, and what an emitted robust -t selects. Each name is the
// `" > "`-joined task path, the same identity `ingest` records.
//
// It refuses (loudly) a file whose names collide under the space-joined form -t
// actually matches: two such names cannot be placed in different slices without
// a -t running one in the other's place. This is the plan-time backstop for a
// name added since the last record — the steady-state case is already demoted at
// ingest, so refusing here is rare, and refusing beats emitting a slice that
// would double-run a test.
func runnableNames(root, file string, data []byte) ([]string, error) {
	var rows []listEntry
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parse vitest list --json: %w", err)
	}
	seen := map[string]bool{}
	var names []string
	for _, r := range rows {
		if relID(root, r.File) != file {
			continue
		}
		// A row for this file with NO name field is a truncated/reshaped capture,
		// not a real test — refuse loudly rather than drop it. But a PRESENT-but-
		// empty name is the legal `test("")` runnable and MUST be kept: dropping it
		// here would slice the whale over an incomplete universe and silently lose
		// that test (the gate only checks the names it is given). The `^()$` the
		// renderer emits matches it exactly.
		if r.Name == nil {
			return nil, fmt.Errorf(
				"vitest list row for %s has no name field; refusing to drop a test — the capture is truncated or the reporter schema changed", file)
		}
		n := *r.Name
		if seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	sort.Strings(names)
	if dupes := ambiguous(names); len(dupes) > 0 {
		return nil, fmt.Errorf(
			"cannot safely name-slice %s: its test names collide under the space-joined form Vitest's -t matches (%s) — "+
				"a name filter cannot tell them apart, so a slice would run a test twice; rename them or the file runs whole",
			file, strings.Join(dupes, ", "))
	}
	return names, nil
}

// parseProjects reduces glob discovery JSON to a file-id -> project-name map,
// the routing table Runnables uses to scope its importing `vitest list` to one
// project. A row with no projectName (single-project config) maps to "", which
// Runnables reads as "no --project scoping needed". A row with no file is skipped
// rather than refused: this map is only a routing hint (parseList already refused
// a fileless discovery), so a stray row costs a scoping decision, not coverage.
func parseProjects(root string, data []byte) (map[string]string, error) {
	var rows []listEntry
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parse vitest list --filesOnly --json: %w", err)
	}
	m := map[string]string{}
	for _, r := range rows {
		if strings.TrimSpace(r.File) == "" {
			continue
		}
		id := relID(root, r.File)
		if strings.TrimSpace(id) == "" {
			continue
		}
		// First writer wins so the map is a pure function of the input; every row
		// for one file carries the same projectName anyway.
		if _, ok := m[id]; !ok {
			m[id] = r.ProjectName
		}
	}
	return m, nil
}

// discover runs the configured discovery command and reduces its JSON to the
// live target set. The default is glob (`vitest list --filesOnly --json`), which
// resolves files without importing them; `list` opts into the importing
// full-collection path, and a verbatim DiscoveryCommand overrides both.
func (r *Runner) discover(ctx context.Context) ([]runner.LivePackage, error) {
	// A frozen bundle supplies the discovery BYTES; the parser below is the
	// same one the live path uses, so a replay differs from the original run
	// in where the bytes came from and in nothing else.
	if r.frozen != nil {
		return parseList(r.root, r.frozen.Discovery)
	}
	out, err := r.runDiscovery(ctx)
	if err != nil {
		return nil, err
	}
	return parseList(r.root, out)
}

// runDiscovery issues the discovery subprocess and returns its raw JSON, mapping
// a deadline hit into an actionable discovery error.
func (r *Runner) runDiscovery(ctx context.Context) ([]byte, error) {
	vt, args := r.discoveryInvocation()
	out, err := vt.run(ctx, r.root, args...)
	if err != nil {
		return nil, r.discoveryError(err)
	}
	return out, nil
}

// discoveryInvocation resolves the discovery subprocess: a verbatim
// DiscoveryCommand (which owns its subcommand, so nothing is appended) when set,
// otherwise the base vitest command plus the mode's flags — glob
// (`list --filesOnly --json`, no import) by default, `list --json` (full
// collection) under discoveryList. It is a pure function of the Runner's config,
// so the selection is unit-tested without spawning a process.
func (r *Runner) discoveryInvocation() (nodetool, []string) {
	if len(r.discoveryCmd) > 0 {
		return nodetool{command: r.discoveryCmd, timeout: r.tool.timeout}, nil
	}
	if r.discoveryMode == discoveryList {
		return r.tool, []string{"list", "--json"}
	}
	return r.tool, []string{"list", "--filesOnly", "--json"}
}

// discoveryError enriches a discovery-subprocess failure. A deadline hit is the
// important one: with the default glob mode it means discovery genuinely
// out-ran its budget, but under `--vitest-discovery=list` it is very likely the
// multi-project collection deadlock — so the message names the fix (glob mode)
// and the knob (--discovery-timeout / TB_DISCOVERY_TIMEOUT) rather than reading
// as a broken project.
func (r *Runner) discoveryError(err error) error {
	var te *timeoutError
	if !errors.As(err, &te) {
		return err
	}
	hint := "raise --discovery-timeout / TB_DISCOVERY_TIMEOUT if discovery is legitimately slow"
	if r.discoveryMode == discoveryList && len(r.discoveryCmd) == 0 {
		hint = "Vitest's multi-project `vitest list` collection can deadlock; the default glob discovery (`vitest list --filesOnly`) resolves files without importing them and avoids this — or " + hint
	}
	return fmt.Errorf("vitest discovery failed: %w. %s", err, hint)
}

// LoadLivePackages reads a live set from a JSON file — the offline path, so
// `plan` can run against a recorded tree with no Vitest install, and how the
// tests drive discovery. It accepts either a `vitest list --json` array
// ([{name,file}]) or a neutral LivePackage array ([{id,...}]); an entry with no
// resolvable identity is a loud error, never a silently-empty id.
func LoadLivePackages(root, path string) ([]runner.LivePackage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read live set: %w", err)
	}
	abs, err := resolveRootLoose(root)
	if err != nil {
		return nil, err
	}
	// Try the vitest-list shape first (has a "file" per row); fall back to the
	// neutral LivePackage shape.
	var rows []listEntry
	if err := json.Unmarshal(data, &rows); err == nil && looksLikeList(rows) {
		return parseList(abs, data)
	}
	var live []runner.LivePackage
	if err := json.Unmarshal(data, &live); err != nil {
		return nil, fmt.Errorf("parse live set %s: %w", path, err)
	}
	for i := range live {
		if strings.TrimSpace(live[i].ID) == "" {
			return nil, fmt.Errorf("live set %s: entry %d has no identity (\"id\"); refusing an empty or whitespace-only package id", path, i)
		}
	}
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })
	// The neutral shape skips parseList, so it must co-schedule filter collisions
	// here too. Without this an offline live set — a recorded tree, a fixture, a
	// consumer's exported package list — would plan the colliding files into
	// separate buckets and double-run them, exactly the defect the list-shaped
	// path is protected from.
	assignFilterAtoms(live)
	return live, nil
}

func looksLikeList(rows []listEntry) bool {
	for _, r := range rows {
		if r.File != "" {
			return true
		}
	}
	return false
}

// resolveRootLoose resolves a root for offline loading without requiring the
// directory to exist (the fixture may describe a tree that is not checked out).
func resolveRootLoose(root string) (string, error) {
	if root == "" {
		root = "."
	}
	return filepath.Abs(root)
}
