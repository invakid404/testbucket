package vitestrunner

// Exact whole-file Vitest paths (#33).
//
// Two Mandel pairs share a path suffix:
//
//	lib/keto/organizations.test.ts        shared/f/lib/keto/organizations.test.ts
//	lib/attribution/resolveSupplierUserAttribution.test.ts
//	                          shared/f/lib/attribution/resolveSupplierUserAttribution.test.ts
//
// Vitest matches a positional file filter with `testFile.includes(filter)`
// against the ROOT-RELATIVE path, so the short id of each pair also selects its
// `shared/f/` mate. Split across buckets, the mate then runs — and reports —
// twice: once for its own bucket, once as an unplanned passenger of the short
// file's bucket.
//
// These tests pin both halves of the fix offline, with no Node runtime:
//
//   - the renderer emits every whole-file positional as a `./` PATH TOKEN;
//   - discovery co-schedules the pairs, so each planned file runs exactly once
//     and the coverage audit passes — while the pre-fix (v0.2.1) shape runs an
//     unplanned suffix match and the audit still FAILS on it, fail-closed.
//
// The Vitest side is simulated by filterSelects, a transcription of
// TestProject.filterFiles — byte-identical in vitest 4.1.11 (this repo's fixture
// pin) and 4.1.10 (the Mandel consumer's pin). integration_test.go carries the
// same contract against the real 4.1.11 the fixture installs.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
)

// The four real Mandel paths of the two proven suffix-sharing pairs, plus one
// control file that collides with nothing.
const (
	ketoShort  = "lib/keto/organizations.test.ts"
	ketoShared = "shared/f/lib/keto/organizations.test.ts"
	attrShort  = "lib/attribution/resolveSupplierUserAttribution.test.ts"
	attrShared = "shared/f/lib/attribution/resolveSupplierUserAttribution.test.ts"
	loneFile   = "lib/billing/invoices.test.ts"
)

const suffixRoot = "/repo"

func suffixUniverse() []string {
	return []string{ketoShort, ketoShared, attrShort, attrShared, loneFile}
}

// suffixListJSON is the `vitest list --filesOnly --json` capture discovery reads.
func suffixListJSON() []byte {
	var rows []string
	for _, f := range suffixUniverse() {
		rows = append(rows, fmt.Sprintf(`{"file":%q,"projectName":"unit"}`, suffixRoot+"/"+f))
	}
	return []byte("[" + strings.Join(rows, ",") + "]")
}

// normalizeFilter mirrors Vitest's `relative(dir, f)`: every spelling of one file
// — bare `x`, `./x`, `<root>/x` — collapses to the same root-relative id before
// the substring test, which is exactly why no spelling is more selective than
// another. Only the two relative forms are ever emitted, so stripping `./` is
// the whole of it here.
func normalizeFilter(f string) string {
	return strings.TrimPrefix(strings.TrimPrefix(f, suffixRoot+"/"), "./")
}

// vitestSelect returns the files Vitest would actually run for one invocation's
// positional filters, over the given universe of root-relative ids.
func vitestSelect(filters, universe []string) []string {
	if len(filters) == 0 {
		return append([]string(nil), universe...)
	}
	var out []string
	for _, cand := range universe {
		for _, f := range filters {
			if filterSelects(normalizeFilter(f), cand) {
				out = append(out, cand)
				break
			}
		}
	}
	return out
}

// filePositionals extracts the file filters of a rendered invocation the way a
// shell hands them to Vitest: everything after `run` that is not a flag, and not
// a flag's value.
func filePositionals(t *testing.T, args []string) []string {
	t.Helper()
	i := 0
	for ; i < len(args) && args[i] != "run"; i++ {
	}
	if i == len(args) {
		t.Fatalf("invocation is not a `vitest run`: %v", args)
	}
	var out []string
	for i++; i < len(args); i++ {
		switch {
		case args[i] == "-t":
			i++ // the pattern is its value, not a file
		case strings.HasPrefix(args[i], "-"):
		default:
			out = append(out, args[i])
		}
	}
	return out
}

// TestRenderWholeFilePositionalsArePathTokens pins the renderer contract for the
// four real Mandel paths: one invocation per one-file bucket, whose sole file
// positional is `./` + the canonical id — never the bare id, which Vitest/CAC
// could read as an option.
func TestRenderWholeFilePositionalsArePathTokens(t *testing.T) {
	r := mustNew(t, Options{Root: suffixRoot})
	for i, file := range []string{ketoShort, ketoShared, attrShort, attrShared} {
		got := r.Render(runner.Bucket{Index: i, Units: []runner.Unit{wholeFileUnit(file, 1)}})
		if len(got.Invocations) != 1 {
			t.Fatalf("%s: got %d invocations, want 1", file, len(got.Invocations))
		}
		inv := got.Invocations[0]
		pos := filePositionals(t, inv.Args)
		if len(pos) != 1 || pos[0] != "./"+file {
			t.Errorf("%s: file positionals = %v, want exactly [./%s]", file, pos, file)
		}
		// The plan-facing identity stays the canonical id: it is the store key and
		// the id the reporter reports back.
		if inv.Desc != file {
			t.Errorf("%s: Desc = %q, want the canonical id", file, inv.Desc)
		}
	}
}

// TestPositionalFilterCannotSeparateASuffixPair is the negative half of the
// defect, stated as a property: for a suffix-sharing pair NO spelling of the
// short id — the v0.2.1 bare id, the v0.2.2 `./` path token, or an absolute path
// — selects only the short file. That is why exactness has to come from
// scheduling (assignFilterAtoms) and not from argument syntax, and it is what
// stops the `./` rewrite from being mistaken for the whole fix.
func TestPositionalFilterCannotSeparateASuffixPair(t *testing.T) {
	universe := suffixUniverse()
	for _, pair := range [][2]string{{ketoShort, ketoShared}, {attrShort, attrShared}} {
		short, shared := pair[0], pair[1]
		for _, spelling := range []string{short, "./" + short, suffixRoot + "/" + short} {
			got := vitestSelect([]string{spelling}, universe)
			if len(got) != 2 || got[0] != short || got[1] != shared {
				t.Errorf("filter %q selected %v, want the unavoidable over-match [%s %s]",
					spelling, got, short, shared)
			}
		}
		// The LONG id is unambiguous in the other direction: nothing contains it.
		if got := vitestSelect([]string{"./" + shared}, universe); len(got) != 1 || got[0] != shared {
			t.Errorf("filter ./%s selected %v, want only itself", shared, got)
		}
	}
	if got := vitestSelect([]string{"./" + loneFile}, universe); len(got) != 1 || got[0] != loneFile {
		t.Errorf("a non-colliding file over-matched: %v", got)
	}
}

// TestDiscoveryCoSchedulesFilterCollisions proves the discovery half: each
// suffix-sharing pair gets one shared Atom (keyed by the ambiguous filter), the
// two pairs get DIFFERENT atoms, and a file nothing collides with keeps mixing
// freely.
func TestDiscoveryCoSchedulesFilterCollisions(t *testing.T) {
	live, err := parseList(suffixRoot, suffixListJSON())
	if err != nil {
		t.Fatalf("parseList: %v", err)
	}
	atom := map[string]string{}
	for _, p := range live {
		atom[p.ID] = p.Atom
	}
	if len(atom) != len(suffixUniverse()) {
		t.Fatalf("discovered %d files, want %d: %v", len(atom), len(suffixUniverse()), atom)
	}
	if atom[ketoShort] == "" || atom[ketoShort] != atom[ketoShared] {
		t.Errorf("keto pair not co-scheduled: %q vs %q", atom[ketoShort], atom[ketoShared])
	}
	if atom[attrShort] == "" || atom[attrShort] != atom[attrShared] {
		t.Errorf("attribution pair not co-scheduled: %q vs %q", atom[attrShort], atom[attrShared])
	}
	if atom[ketoShort] == atom[attrShort] {
		t.Errorf("the two pairs share one atom %q; they are independent groups", atom[ketoShort])
	}
	// The key is the ambiguous filter itself, so the plan reads meaningfully.
	if want := filterAtomPrefix + ketoShort; atom[ketoShort] != want {
		t.Errorf("keto atom = %q, want %q", atom[ketoShort], want)
	}
	if atom[loneFile] != "" {
		t.Errorf("%s was pinned to atom %q; a file nothing collides with must mix freely",
			loneFile, atom[loneFile])
	}
}

// TestLoadLivePackagesCoSchedulesBothShapes pins the fix on BOTH offline live-set
// shapes LoadLivePackages accepts. The `vitest list` shape routes through
// parseList; the neutral LivePackage shape does not, and before this was closed a
// recorded/exported live set planned the colliding files into separate buckets
// and double-ran them. A caller that already rides the whole group in one
// invocation keeps its own key.
func TestLoadLivePackagesCoSchedulesBothShapes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	listShape := write("list.json", string(suffixListJSON()))
	var neutral []string
	for _, f := range suffixUniverse() {
		neutral = append(neutral, fmt.Sprintf(`{"id":%q,"has_tests":true}`, f))
	}
	neutralShape := write("neutral.json", "["+strings.Join(neutral, ",")+"]")

	for _, tc := range []struct{ name, path string }{
		{"vitest-list shape", listShape},
		{"neutral LivePackage shape", neutralShape},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live, err := LoadLivePackages(suffixRoot, tc.path)
			if err != nil {
				t.Fatalf("LoadLivePackages: %v", err)
			}
			atom := map[string]string{}
			for _, p := range live {
				atom[p.ID] = p.Atom
			}
			if atom[ketoShort] == "" || atom[ketoShort] != atom[ketoShared] {
				t.Errorf("keto pair not co-scheduled: %q vs %q", atom[ketoShort], atom[ketoShared])
			}
			if atom[attrShort] == "" || atom[attrShort] != atom[attrShared] {
				t.Errorf("attribution pair not co-scheduled: %q vs %q", atom[attrShort], atom[attrShared])
			}
			if atom[loneFile] != "" {
				t.Errorf("%s was pinned to atom %q", loneFile, atom[loneFile])
			}
		})
	}

	// Build a neutral live set with caller-supplied atoms.
	neutralWithAtoms := func(name string, atomOf map[string]string) []runner.LivePackage {
		t.Helper()
		var rows []string
		for _, f := range suffixUniverse() {
			rows = append(rows, fmt.Sprintf(`{"id":%q,"atom":%q,"has_tests":true}`, f, atomOf[f]))
		}
		live, err := LoadLivePackages(suffixRoot, write(name, "["+strings.Join(rows, ",")+"]"))
		if err != nil {
			t.Fatalf("LoadLivePackages: %v", err)
		}
		return live
	}
	atomsOf := func(live []runner.LivePackage) map[string]string {
		out := map[string]string{}
		for _, p := range live {
			out[p.ID] = p.Atom
		}
		return out
	}

	// A caller that already co-schedules the WHOLE collision group keeps its key.
	t.Run("caller already rides the whole group", func(t *testing.T) {
		got := atomsOf(neutralWithAtoms("preset.json", map[string]string{
			ketoShort: "caller-group", ketoShared: "caller-group",
		}))
		for _, id := range []string{ketoShort, ketoShared} {
			if got[id] != "caller-group" {
				t.Errorf("%s: caller atom overwritten with %q; the group already rode together", id, got[id])
			}
		}
	})

	// The overlap case. A caller atom spans ketoShort AND an unrelated file, while
	// ketoShared has none. Rewriting only the colliding pair would strand loneFile
	// alone on "caller" — two groups where the caller declared one, and a core
	// free to put them in different invocations. All three must end up on ONE key,
	// and since exactly one caller key is present it is the one that survives.
	t.Run("collision overlaps part of a caller atom", func(t *testing.T) {
		got := atomsOf(neutralWithAtoms("overlap.json", map[string]string{
			ketoShort: "caller-group", loneFile: "caller-group",
		}))
		for _, id := range []string{ketoShort, ketoShared, loneFile} {
			if got[id] != "caller-group" {
				t.Errorf("%s has atom %q, want the whole union on \"caller-group\" (got %v)", id, got[id], got)
			}
		}
		// The other pair is untouched and keeps its own filter key.
		if got[attrShort] == "" || got[attrShort] != got[attrShared] {
			t.Errorf("attribution pair not co-scheduled: %q vs %q", got[attrShort], got[attrShared])
		}
		if got[attrShort] == got[ketoShort] {
			t.Errorf("the attribution pair was fused into the caller group: %v", got)
		}
	})

	// Two DISTINCT caller atoms fused by a collision cannot both survive, so the
	// component takes the deterministic filter key. The load-bearing part is that
	// the OTHER members of the losing caller groups come along: here "group-a"
	// also holds loneFile, which collides with nothing. Keying the colliding pair
	// alone would strand loneFile by itself on "group-a" — one caller group turned
	// into two, which is precisely the contract Atom exists to state. Only the
	// union over atom membership pulls it in.
	t.Run("collision fuses two caller atoms and carries their other members", func(t *testing.T) {
		got := atomsOf(neutralWithAtoms("fuse.json", map[string]string{
			ketoShort: "group-a", loneFile: "group-a", ketoShared: "group-b",
		}))
		// The key is the smallest id in the WHOLE union, which now includes the
		// carried-in loneFile ("lib/b..." sorts before "lib/k...").
		want := filterAtomPrefix + loneFile
		for _, id := range []string{ketoShort, ketoShared, loneFile} {
			if got[id] != want {
				t.Errorf("%s has atom %q, want the deterministic %q shared by the whole union (got %v)",
					id, got[id], want, got)
			}
		}
		// The untouched pair keeps its own key and is not swept in.
		if got[attrShort] == "" || got[attrShort] != got[attrShared] {
			t.Errorf("attribution pair not co-scheduled: %q vs %q", got[attrShort], got[attrShared])
		}
		if got[attrShort] == want {
			t.Errorf("the attribution pair was fused into the keto union: %v", got)
		}
	})
}

// TestFilterPathArgLeavesExplicitPathsAlone covers the argument-shaping helper the
// whole-file renderer now applies to EVERY positional, including the case relID
// deliberately produces when filepath.Rel fails: a retained absolute path.
//
// The cross-volume Windows id `D:/tests/a.vtest.ts` is the one that bites. It has
// no leading '.' or '/', so a naive check prepends `./` and reroots the file out
// of existence. The expectation is platform-dependent because the MEANING is: on
// Windows that id is absolute and must be passed through; on POSIX it is a real
// relative path under a directory named `D:`, where `./` is correct.
func TestFilterPathArgLeavesExplicitPathsAlone(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		{"tests/a.vtest.ts", "./tests/a.vtest.ts"},
		{"--odd.vtest.ts", "./--odd.vtest.ts"},       // the '-'-leading id this helper exists for
		{"./tests/a.vtest.ts", "./tests/a.vtest.ts"}, // already explicit
		{"../sibling/a.vtest.ts", "../sibling/a.vtest.ts"},
		{"/abs/tests/a.vtest.ts", "/abs/tests/a.vtest.ts"}, // POSIX absolute, on every platform
	} {
		if got := filterPathArg(tc.id); got != tc.want {
			t.Errorf("filterPathArg(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}

	const volume = "D:/tests/a.vtest.ts"
	want := "./" + volume // POSIX: a directory literally named "D:"
	if runtime.GOOS == "windows" {
		want = volume // Windows: absolute on another volume; rerooting would lose the file
	}
	if got := filterPathArg(volume); got != want {
		t.Errorf("filterPathArg(%q) = %q, want %q on %s", volume, got, want, runtime.GOOS)
	}

	// The property behind the table: an id the PLATFORM calls absolute is never
	// rewritten, on whatever platform the test runs.
	for _, id := range []string{"/abs/a.vtest.ts", volume, "tests/a.vtest.ts"} {
		if filepath.IsAbs(filepath.FromSlash(id)) && filterPathArg(id) != id {
			t.Errorf("platform-absolute %q was rewritten to %q", id, filterPathArg(id))
		}
	}
}

// TestFilterFoldIsConservativeAcrossLocales pins every place Vitest's case fold
// disagrees with Go's per-rune one — the locale-sensitive dotted/dotless I and the
// context-sensitive final Greek sigma — and the direction this adapter may never
// get wrong.
//
// Vitest folds with toLocaleLowerCase(). Measured against Node:
//
//	          root locale     tr / az locale   Go strings.ToLower
//	'I'   ->  "i"             "ı"              "i"
//	'İ'   ->  "i" + U+0307    "i"              "i"
//	'ı'   ->  "ı"             "ı"              "ı"
//	"AΣ"  ->  "aς"            "aς"             "aσ"
//
// So on a Turkish-locale runner the filter `FILE.test.ts` folds to
// `fıle.test.ts` and genuinely selects `shared/fıle.test.ts` — a collision plain
// strings.ToLower misses. A MISS is the dangerous direction: the pair is left
// un-atomized, split across buckets, and the long file double-runs. An EXTRA
// collision only co-schedules two files that did not have to ride together.
func TestFilterFoldIsConservativeAcrossLocales(t *testing.T) {
	// The whole dotted/dotless-I family must fold together, so no locale's fold
	// can separate two ids this one joins.
	family := []string{"I", "i", "\u0130", "\u0131"} // I, i, İ, ı
	want := filterFold(family[0])
	for _, r := range family[1:] {
		if got := filterFold(r); got != want {
			t.Errorf("filterFold(%q) = %q, want the whole I family folded to %q", r, got, want)
		}
	}
	// A root-locale fold of 'İ' leaves a combining dot behind; it must not make
	// two otherwise-equal ids look different.
	if a, b := filterFold("i\u0307x"), filterFold("ix"); a != b {
		t.Errorf("filterFold kept a combining dot above: %q vs %q", a, b)
	}

	// Final sigma. CONTEXT-sensitive, not locale-sensitive: a \u03a3 that ends a word
	// folds to \u03c2 in EVERY locale, which Go's per-rune table never produces.
	if a, b := filterFold("\u03a3"), filterFold("\u03c2"); a != b {
		t.Errorf("filterFold: \u03a3 folds to %q but \u03c2 folds to %q; they must collapse together", a, b)
	}
	if got, want := filterFold("\u03c2"), string(sigma); got != want {
		t.Errorf("filterFold(final sigma) = %q, want %q", got, want)
	}

	// Lithuanian accented i. `lt` lower-cases the UPPERCASE forms to a DECOMPOSED
	// i + U+0307 + mark, while Go gives the precomposed rune, so a path carrying
	// either spelling must reach the same fold.
	for _, pair := range [][2]string{
		{"\u00cc", "i\u0307\u0300"}, // Ì  vs  i + dot above + grave
		{"\u00cd", "i\u0307\u0301"}, // Í  vs  i + dot above + acute
		{"\u0128", "i\u0307\u0303"}, // Ĩ  vs  i + dot above + tilde
	} {
		if a, b := filterFold(pair[0]), filterFold(pair[1]); a != b {
			t.Errorf("filterFold(%q) = %q but filterFold(%q) = %q; a Lithuanian fold emits the "+
				"decomposed form, so the two spellings must collapse together", pair[0], a, pair[1], b)
		}
	}
	if !filterSelects("\u00cc.test.ts", "x/i\u0307\u0300.test.ts") {
		t.Error("filterSelects missed a Lithuanian precomposed-vs-decomposed collision")
	}

	// The two reported cases, end to end through the collision predicate.
	const (
		upper = "FILE.test.ts"             // folds to "fıle.test.ts" under tr
		mate  = "shared/f\u0131le.test.ts" // literal dotless i on disk
		unrel = "shared/other.test.ts"

		sigUpper = "dir/A\u03a3/foo.test.ts"        // Vitest folds to dir/a\u03c2/...
		sigMate  = "shared/dir/a\u03c2/foo.test.ts" // literal final sigma on disk
		sigUnrel = "shared/dir/a\u03b4/foo.test.ts" // delta: unrelated
	)
	if !filterSelects(upper, mate) {
		t.Errorf("filterSelects(%q, %q) = false; a Turkish-locale Vitest DOES select the mate, "+
			"so the predicate is not a conservative superset and the pair would be split", upper, mate)
	}
	if filterSelects(upper, unrel) {
		t.Errorf("filterSelects(%q, %q) = true; the fold over-reached beyond the I family", upper, unrel)
	}
	if !filterSelects(sigUpper, sigMate) {
		t.Errorf("filterSelects(%q, %q) = false; Vitest folds the word-final \u03a3 to \u03c2 in every "+
			"locale and DOES select the mate, so the pair would be split and double-run", sigUpper, sigMate)
	}
	if filterSelects(sigUpper, sigUnrel) {
		t.Errorf("filterSelects(%q, %q) = true; the fold over-reached beyond the sigma pair", sigUpper, sigUnrel)
	}

	// ASCII ids keep folding exactly as before — the family fold must not disturb
	// the common path.
	for _, id := range []string{"lib/keto/organizations.test.ts", "Tests/FAST.spec.ts", "--odd.spec.ts"} {
		if got, want := filterFold(id), strings.ToLower(id); got != want {
			t.Errorf("filterFold(%q) = %q, want the plain lower-case %q for an ASCII id", id, got, want)
		}
	}
}

// TestDiscoveryCoSchedulesSpecialFoldCollision drives the same cases through
// discovery: each pair must come back co-scheduled, so the planner can never
// split it and double-run the long file.
func TestDiscoveryCoSchedulesSpecialFoldCollision(t *testing.T) {
	for _, tc := range []struct {
		name, short, mate, unrelated, why string
	}{
		{
			name:  "dotted/dotless I (locale-sensitive)",
			short: "FILE.test.ts", mate: "shared/f\u0131le.test.ts", unrelated: "shared/other.test.ts",
			why: "a Turkish-locale runner folds the filter to f\u0131le.test.ts",
		},
		{
			name:  "final Greek sigma (context-sensitive)",
			short: "dir/A\u03a3/foo.test.ts", mate: "shared/dir/a\u03c2/foo.test.ts", unrelated: "shared/dir/a\u03b4/foo.test.ts",
			why: "every locale folds the word-final \u03a3 to \u03c2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rows []string
			for _, f := range []string{tc.short, tc.mate, tc.unrelated} {
				rows = append(rows, fmt.Sprintf(`{"file":%q,"projectName":"unit"}`, suffixRoot+"/"+f))
			}
			live, err := parseList(suffixRoot, []byte("["+strings.Join(rows, ",")+"]"))
			if err != nil {
				t.Fatalf("parseList: %v", err)
			}
			atom := map[string]string{}
			for _, p := range live {
				atom[p.ID] = p.Atom
			}
			if atom[tc.short] == "" || atom[tc.short] != atom[tc.mate] {
				t.Errorf("colliding pair not co-scheduled: %q vs %q (atoms=%v); %s, so %s would run twice",
					atom[tc.short], atom[tc.mate], atom, tc.why, tc.mate)
			}
			if atom[tc.unrelated] != "" {
				t.Errorf("%s was pinned to atom %q; it collides with nothing", tc.unrelated, atom[tc.unrelated])
			}
		})
	}
}

// TestDiscoveryCoSchedulesChildProjectRootCollision covers the case a comparison
// at the WORKSPACE root cannot see.
//
// Vitest evaluates a positional once per project, against that project's own
// `config.dir || config.root`. With a project rooted at `projects/unit`, the ids
//
//	projects/unit/lib/keto/organizations.test.ts
//	projects/unit/shared/f/lib/keto/organizations.test.ts
//
// share no substring as this adapter stores them, yet Vitest compares them as
// `lib/keto/…` and `shared/f/lib/keto/…` — where the short one IS contained.
// Measured against a real Vitest in TestVitestChildProjectRootPairsRunExactlyOnce.
// Missing it leaves the pair un-atomized, split, and double-running.
func TestDiscoveryCoSchedulesChildProjectRootCollision(t *testing.T) {
	const prefix = "projects/unit/"
	var rows []string
	all := []string{
		prefix + ketoShort, prefix + ketoShared,
		prefix + attrShort, prefix + attrShared,
		prefix + loneFile, // collides with nothing at any depth
	}
	for _, f := range all {
		rows = append(rows, fmt.Sprintf(`{"file":%q,"projectName":"unit"}`, suffixRoot+"/"+f))
	}
	live, err := parseList(suffixRoot, []byte("["+strings.Join(rows, ",")+"]"))
	if err != nil {
		t.Fatalf("parseList: %v", err)
	}
	atom := map[string]string{}
	for _, p := range live {
		atom[p.ID] = p.Atom
	}
	for _, pair := range [][2]string{
		{prefix + ketoShort, prefix + ketoShared},
		{prefix + attrShort, prefix + attrShared},
	} {
		short, shared := pair[0], pair[1]
		if atom[short] == "" || atom[short] != atom[shared] {
			t.Errorf("child-root pair not co-scheduled: %q vs %q (atoms=%v); Vitest compares these "+
				"project-relative, where the short id IS contained, so %s would run twice",
				atom[short], atom[shared], atom, shared)
		}
	}
	if atom[prefix+ketoShort] == atom[prefix+attrShort] {
		t.Errorf("the two pairs were fused into one atom %q; they collide with each other at no depth",
			atom[prefix+ketoShort])
	}
	if atom[prefix+loneFile] != "" {
		t.Errorf("%s was pinned to atom %q; it collides with nothing", prefix+loneFile, atom[prefix+loneFile])
	}
}

// TestCollisionWalkStopsAtDivergingRoots pins the bound on the depth walk: it may
// only strip directories the two ids SHARE. A directory that is an ancestor of
// just one file cannot produce a Vitest match — `relative(dir, other)` then starts
// with `..`, which a project-relative path never contains — so stripping past a
// divergence would invent collisions and needlessly co-schedule unrelated files.
func TestCollisionWalkStopsAtDivergingRoots(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
		why  string
	}{
		{"lib/keto/organizations.test.ts", "shared/f/lib/keto/organizations.test.ts", true,
			"plain workspace-root containment"},
		{"projects/unit/lib/keto/x.test.ts", "projects/unit/shared/f/lib/keto/x.test.ts", true,
			"visible only after stripping the two shared segments"},
		{"projects/unit/lib/keto/x.test.ts", "other/unit/shared/f/lib/keto/x.test.ts", false,
			"first segments differ: no directory is a common ancestor"},
		{"projects/unit/lib/keto/x.test.ts", "projects/e2e/shared/f/lib/keto/x.test.ts", false,
			"second segments differ: `projects` is the deepest common ancestor and does not match"},
		{"a/one.test.ts", "b/two.test.ts", false, "unrelated"},
	} {
		if got := collidesUnderSomeProjectRoot(tc.a, tc.b); got != tc.want {
			t.Errorf("collidesUnderSomeProjectRoot(%q, %q) = %v, want %v (%s)", tc.a, tc.b, got, tc.want, tc.why)
		}
	}
}

// TestSuffixPairsRunExactlyOnceAndAuditPasses is the behavioural regression, run
// end to end offline: discover -> plan (K=4) -> render -> what Vitest would
// really execute -> the real JSON-reporter ingest -> the real coverage audit.
//
// Fixed shape: each planned file executes exactly once and the audit PASSES.
// v0.2.1 shape (the same core, the same renderer, but discovery NOT marking the
// collisions): a `shared/f/` mate runs as an unplanned passenger, reports twice,
// and the audit still FAILS on it — the oracle stays fail-closed.
func TestSuffixPairsRunExactlyOnceAndAuditPasses(t *testing.T) {
	universe := suffixUniverse()

	fixed, err := parseList(suffixRoot, suffixListJSON())
	if err != nil {
		t.Fatalf("parseList: %v", err)
	}
	// v0.2.1 discovery: the same live set with no co-scheduling at all.
	var pre []runner.LivePackage
	for _, p := range fixed {
		pre = append(pre, runner.LivePackage{ID: p.ID, HasTests: true})
	}

	t.Run("fixed", func(t *testing.T) {
		counts, sum, planned := planAndSimulate(t, fixed, universe)
		for _, f := range universe {
			if counts[f] != 1 {
				t.Errorf("%s executed %d time(s), want exactly 1 (counts=%v)", f, counts[f], counts)
			}
		}
		if err := core.AuditCoverage(io.Discard, planned, sum); err != nil {
			t.Fatalf("audit rejected a run that executed every planned file exactly once: %v", err)
		}
	})

	t.Run("v0.2.1", func(t *testing.T) {
		counts, sum, planned := planAndSimulate(t, pre, universe)
		// EACH shared mate must be an unplanned passenger, asserted per pair. An
		// "either one of them" check would let an implementation that doubled only
		// ONE pair pass as a reproduction of a two-pair defect.
		for _, pair := range [][2]string{{ketoShort, ketoShared}, {attrShort, attrShared}} {
			short, shared := pair[0], pair[1]
			if counts[shared] != 2 {
				t.Errorf("%s executed %d time(s), want exactly 2 under v0.2.1: its own bucket plus the unplanned suffix match from %s (counts=%v)",
					shared, counts[shared], short, counts)
			}
			if counts[short] != 1 {
				t.Errorf("%s executed %d time(s), want 1; only the SHARED mate is over-matched (counts=%v)",
					short, counts[short], counts)
			}
		}
		if counts[loneFile] != 1 {
			t.Errorf("%s executed %d time(s), want 1; it collides with nothing (counts=%v)", loneFile, counts[loneFile], counts)
		}
		err := core.AuditCoverage(io.Discard, planned, sum)
		if err == nil {
			t.Fatal("the audit PASSED a run with an unplanned suffix match; the oracle is not fail-closed")
		}
		if !strings.Contains(err.Error(), "reported more invocations than planned") {
			t.Errorf("audit failed for the wrong reason: %v", err)
		}
	})
}

// planAndSimulate runs the real cold plan over a live set, then works out what
// Vitest would actually execute for every rendered invocation and folds those
// reports through the real ingest.
func planAndSimulate(t *testing.T, live []runner.LivePackage, universe []string) (map[string]int, *runner.RunSummary, *core.PlannedCoverage) {
	t.Helper()
	r := mustNew(t, Options{Root: suffixRoot})
	doc, err := core.BuildPlan(t.Context(), r, nil, "cold", core.PlanOptions{
		K: 4, Count: 1, Live: live, Token: r.CanonicalToken(),
		Now: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	counts := map[string]int{}
	var readers []io.Reader
	for _, b := range doc.Buckets {
		for _, inv := range b.Invocations {
			for _, a := range inv.Args {
				if a == "-t" {
					t.Fatalf("bucket %d emitted a name slice; this fixture must plan whole files only: %v", b.Index, inv.Args)
				}
			}
			executed := vitestSelect(filePositionals(t, inv.Args), universe)
			for _, f := range executed {
				counts[f]++
			}
			readers = append(readers, strings.NewReader(reportJSON(executed)))
		}
	}
	sum, err := r.ParseTimings(readers...)
	if err != nil {
		t.Fatalf("ParseTimings: %v", err)
	}
	return counts, sum, plannedFrom(t, doc)
}

// reportJSON synthesises one invocation's `--reporter=json` document: every file
// Vitest actually ran reports one passing test, exactly as a real run would.
func reportJSON(files []string) string {
	var rows []string
	for _, f := range files {
		rows = append(rows, fmt.Sprintf(
			`{"name":%q,"status":"passed","startTime":0,"endTime":1000,"assertionResults":[`+
				`{"title":"case","ancestorTitles":[],"fullName":"case","status":"passed","duration":10}]}`,
			suffixRoot+"/"+f))
	}
	return `{"testResults":[` + strings.Join(rows, ",") + `]}`
}

// plannedFrom round-trips the plan through the on-disk artifact, the way the
// record job loads the uploaded --shard-plan.
func plannedFrom(t *testing.T, doc *core.PlanDocument) *core.PlannedCoverage {
	t.Helper()
	blob, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "shard-plan.json")
	if err := os.WriteFile(p, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	planned, err := core.LoadPlannedCoverage(p)
	if err != nil {
		t.Fatal(err)
	}
	return planned
}
