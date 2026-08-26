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
// (`vitest list --filesOnly --json`) carries {file} only (and an ignored
// projectName). Both are decoded here; discovery reads File, and Runnables reads
// Name. A missing File is the loud-refusal case (see parseList).
type listEntry struct {
	Name string `json:"name"`
	File string `json:"file"`
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
// path. Atom is left empty — Vitest spec files mix freely across a single
// project for best balance; a workspace that must co-schedule a project's files
// would set Atom to the project name.
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
			return nil, fmt.Errorf("vitest list row %d has no file (name=%q); refusing to drop a test — the capture is truncated or the reporter schema changed", i, r.Name)
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
	return live, nil
}

// runnableNames extracts the test names of ONE file from a `vitest list --json`
// output, in sorted order — the universe the never-drop gate checks a file's
// name slices against, and what an emitted `-t '^(a|b)$'` selects.
func runnableNames(root, file string, data []byte) ([]string, error) {
	var rows []listEntry
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parse vitest list --json: %w", err)
	}
	seen := map[string]bool{}
	var names []string
	for _, r := range rows {
		if relID(root, r.File) != file || r.Name == "" || seen[r.Name] {
			continue
		}
		seen[r.Name] = true
		names = append(names, r.Name)
	}
	sort.Strings(names)
	return names, nil
}

// discover runs the configured discovery command and reduces its JSON to the
// live target set. The default is glob (`vitest list --filesOnly --json`), which
// resolves files without importing them; `list` opts into the importing
// full-collection path, and a verbatim DiscoveryCommand overrides both.
func (r *Runner) discover(ctx context.Context) ([]runner.LivePackage, error) {
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
