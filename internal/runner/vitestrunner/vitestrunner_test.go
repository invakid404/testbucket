package vitestrunner

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/invakid404/testbucket/internal/runner"
)

func mustNew(t *testing.T, opt Options) *Runner {
	t.Helper()
	r, err := New(opt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

// A `vitest list --json` fixture with absolute paths under a known root, so the
// relativised ids are deterministic and machine-independent.
const listFixture = `[
  {"name":"slow io","file":"/repo/tests/slow.spec.ts"},
  {"name":"slow calc","file":"/repo/tests/slow.spec.ts"},
  {"name":"fast add","file":"/repo/tests/fast.spec.ts"},
  {"name":"tiny","file":"/repo/tests/tiny.spec.ts"}
]`

func TestParseListGroupsByFile(t *testing.T) {
	live, err := parseList("/repo", []byte(listFixture))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tests/fast.spec.ts", "tests/slow.spec.ts", "tests/tiny.spec.ts"}
	if len(live) != len(want) {
		t.Fatalf("got %d files, want %d: %+v", len(live), len(want), live)
	}
	for i, w := range want {
		if live[i].ID != w {
			t.Errorf("file %d id=%q, want %q", i, live[i].ID, w)
		}
		if !live[i].HasTests {
			t.Errorf("file %s not marked HasTests", live[i].ID)
		}
		if live[i].Atom != "" {
			t.Errorf("file %s got Atom=%q, want empty (files mix freely)", live[i].ID, live[i].Atom)
		}
	}
}

// The glob discovery shape (`vitest list --filesOnly --json`): rows carry a
// file (and an ignored projectName) but NO name. parseList must reduce it to the
// same live set as the full-collection shape, since discovery only ever needs
// files.
const globFixture = `[
  {"file":"/repo/tests/slow.spec.ts","projectName":"a"},
  {"file":"/repo/tests/fast.spec.ts","projectName":"a"},
  {"file":"/repo/tests/tiny.spec.ts","projectName":"b"}
]`

func TestParseListAcceptsGlobFilesOnlyShape(t *testing.T) {
	live, err := parseList("/repo", []byte(globFixture))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tests/fast.spec.ts", "tests/slow.spec.ts", "tests/tiny.spec.ts"}
	if len(live) != len(want) {
		t.Fatalf("got %d files, want %d: %+v", len(live), len(want), live)
	}
	for i, w := range want {
		if live[i].ID != w {
			t.Errorf("file %d id=%q, want %q", i, live[i].ID, w)
		}
		if !live[i].HasTests {
			t.Errorf("file %s not marked HasTests", live[i].ID)
		}
	}
	// A row with no file is still refused in the glob shape — a filesOnly document
	// that dropped a path would silently lose a test file.
	if _, err := parseList("/repo", []byte(`[{"projectName":"a"}]`)); err == nil {
		t.Error("a filesOnly row with no file was accepted")
	}
}

// TestParseProjects builds the file->project routing table Runnables uses to
// scope its importing list. A single-project row (no projectName) maps to "" —
// Runnables reads that as "no --project needed".
func TestParseProjects(t *testing.T) {
	m, err := parseProjects("/repo", []byte(globFixture))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"tests/slow.spec.ts": "a",
		"tests/fast.spec.ts": "a",
		"tests/tiny.spec.ts": "b",
	}
	for id, proj := range want {
		if m[id] != proj {
			t.Errorf("%s -> %q, want %q", id, m[id], proj)
		}
	}
	// Single-project shape (no projectName) maps every file to "".
	single, err := parseProjects("/repo", []byte(`[{"file":"/repo/a.spec.ts"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := single["a.spec.ts"]; !ok || v != "" {
		t.Errorf("single-project file mapped to %q (ok=%v), want \"\"", v, ok)
	}
}

func TestRunnableNamesFiltersByFile(t *testing.T) {
	names, err := runnableNames("/repo", "tests/slow.spec.ts", []byte(listFixture))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names, ","); got != "slow calc,slow io" {
		t.Errorf("slow file names = %q, want the two slow tests sorted", got)
	}
}

// TestRunnableNamesKeepsEmptyTitle is the P1 regression guard: Vitest accepts
// `test("", ...)` and reports it as `"name":""`, a LEGAL runnable. It must stay
// in the slice universe — dropping it lets a whale be sliced over an incomplete
// universe and silently loses that test. A row with the name field ABSENT (not
// merely empty) is a different thing — a truncated capture — and is refused.
func TestRunnableNamesKeepsEmptyTitle(t *testing.T) {
	const withEmpty = `[
      {"name":"","file":"/repo/tests/whale.spec.ts"},
      {"name":"named one","file":"/repo/tests/whale.spec.ts"},
      {"name":"named two","file":"/repo/tests/whale.spec.ts"}
    ]`
	names, err := runnableNames("/repo", "tests/whale.spec.ts", []byte(withEmpty))
	if err != nil {
		t.Fatalf("runnableNames: %v", err)
	}
	if got := strings.Join(names, "|"); got != "|named one|named two" {
		t.Errorf("names = %q, want the empty title kept (sorts first): \"|named one|named two\"", got)
	}
	// A row with NO name field at all is a truncated/reshaped capture — refuse it
	// loudly rather than drop a test.
	const absentName = `[{"file":"/repo/tests/whale.spec.ts"}]`
	if _, err := runnableNames("/repo", "tests/whale.spec.ts", []byte(absentName)); err == nil {
		t.Error("a row with an absent name field was accepted; a truncated capture must be refused")
	}
}

// A Vitest JSON-reporter fixture: three passing files with distinct walls, one
// failed file, one file with no tests.
const reportFixture = `{
  "testResults": [
    {"name":"/repo/tests/slow.spec.ts","status":"passed","startTime":1000,"endTime":1300,
     "assertionResults":[
       {"title":"slow io","ancestorTitles":[],"fullName":"slow io","status":"passed","duration":180},
       {"title":"inner","ancestorTitles":["group"],"fullName":"group inner","status":"passed","duration":120},
       {"title":"skipped one","ancestorTitles":[],"fullName":"skipped one","status":"skipped","duration":null}]},
    {"name":"/repo/tests/fast.spec.ts","status":"passed","startTime":2000,"endTime":2050,
     "assertionResults":[{"title":"fast add","ancestorTitles":[],"fullName":"fast add","status":"passed","duration":50}]},
    {"name":"/repo/tests/tiny.spec.ts","status":"passed","startTime":3000,"endTime":3010,
     "assertionResults":[{"title":"tiny","ancestorTitles":[],"fullName":"tiny","status":"passed","duration":10}]},
    {"name":"/repo/tests/broken.spec.ts","status":"failed","startTime":4000,"endTime":9000,
     "assertionResults":[{"title":"boom","ancestorTitles":[],"fullName":"boom","status":"failed","duration":5000}]},
    {"name":"/repo/tests/empty.spec.ts","status":"passed","startTime":5000,"endTime":5001,
     "assertionResults":[]}
  ]
}`

func TestParseTimingsFromReporter(t *testing.T) {
	r := mustNew(t, Options{Root: "/repo"})
	sum, err := r.ParseTimings(strings.NewReader(reportFixture))
	if err != nil {
		t.Fatalf("ParseTimings: %v", err)
	}
	// Weights are file WALL times, in seconds, keyed by root-relative id.
	cases := map[string]float64{
		"tests/slow.spec.ts": 0.300,
		"tests/fast.spec.ts": 0.050,
		"tests/tiny.spec.ts": 0.010,
	}
	for id, want := range cases {
		if got := sum.PackageSeconds[id]; got < want-1e-9 || got > want+1e-9 {
			t.Errorf("%s = %vs, want %vs (endTime-startTime)", id, got, want)
		}
		if sum.PackageRuns[id] != 1 {
			t.Errorf("%s ran %d times, want 1", id, sum.PackageRuns[id])
		}
	}
	if !sum.Failed["tests/broken.spec.ts"] {
		t.Error("the failed file was not recorded as failed")
	}
	if _, ok := sum.PackageSeconds["tests/broken.spec.ts"]; ok {
		t.Error("a failed file contributed a weight")
	}
	if !sum.NoTests["tests/empty.spec.ts"] {
		t.Error("the empty file was not recorded as no-tests")
	}
	// Per-test weights ARE recorded now (name-slicing). Keys are the `" > "`-joined
	// task path (ancestor describes + title), the same identity `vitest list`
	// reports — a nested test is keyed "group > inner", not the reporter's
	// space-joined "group inner".
	slow := sum.TestSeconds["tests/slow.spec.ts"]
	if got := slow["slow io"]; got < 0.18-1e-9 || got > 0.18+1e-9 {
		t.Errorf("slow io per-test weight = %v, want 0.18", got)
	}
	if got := slow["group > inner"]; got < 0.12-1e-9 || got > 0.12+1e-9 {
		t.Errorf("nested test weight = %v under key %q, want 0.12 under \"group > inner\"", got, "group > inner")
	}
	// A skipped test has a null duration and contributes no weight (it is still in
	// the Runnables universe, so it is never dropped — just unweighed).
	if _, ok := slow["skipped one"]; ok {
		t.Errorf("a skipped test was weighed: %v", slow)
	}
	// A failed file contributes no per-test weights either.
	if _, ok := sum.TestSeconds["tests/broken.spec.ts"]; ok {
		t.Error("a failed file contributed per-test weights")
	}
	// A stream with nothing usable is a broken capture, not a silent success.
	if _, err := r.ParseTimings(strings.NewReader(`{"testResults":[]}`)); err == nil {
		t.Error("an empty report was accepted")
	}
}

// TestParseTimingsSkipsAmbiguousFile proves a file whose test names collide under
// the space-joined form -t matches is DEMOTED at ingest: its per-test rows are
// dropped and the unsliceable signal is raised, so the planner never flags it for
// a slice it could not run cleanly. The file's wall time is still recorded (it
// still runs, whole).
func TestParseTimingsSkipsAmbiguousFile(t *testing.T) {
	r := mustNew(t, Options{Root: "/repo"})
	// Two DISTINCT `" > "` ids that collapse to the same space-form the reporter
	// (and -t) produce: `describe("a") test("b c")` -> id "a > b c" and
	// `describe("a b") test("c")` -> id "a b > c", both fullName "a b c". Their -t
	// patterns would each match the other's test, so the file cannot be sliced.
	// (Two IDENTICAL ids — a genuine duplicate — are NOT this case; they share a
	// slice and stay sliceable.)
	const ambig = `{"testResults":[
      {"name":"/repo/tests/ambig.spec.ts","status":"passed","startTime":0,"endTime":100,
       "assertionResults":[
         {"title":"b c","ancestorTitles":["a"],"fullName":"a b c","status":"passed","duration":40},
         {"title":"c","ancestorTitles":["a b"],"fullName":"a b c","status":"passed","duration":60}]}]}`
	sum, err := r.ParseTimings(strings.NewReader(ambig))
	if err != nil {
		t.Fatalf("ParseTimings: %v", err)
	}
	if got := sum.PackageSeconds["tests/ambig.spec.ts"]; got < 0.1-1e-9 || got > 0.1+1e-9 {
		t.Errorf("ambiguous file lost its wall time (%v); it must still run whole", got)
	}
	if _, ok := sum.TestSeconds["tests/ambig.spec.ts"]; ok {
		t.Errorf("per-test rows recorded for an ambiguous file (%v); it must be demoted to whole-file", sum.TestSeconds)
	}
	if !sum.Unsliceable["tests/ambig.spec.ts"] {
		t.Error("the unsliceable signal was not raised for the ambiguous file")
	}

	// A genuine DUPLICATE (two tests with the identical full name) is NOT
	// ambiguous — it shares one slice and stays sliceable, so per-test rows ARE
	// recorded and the file is not flagged.
	const dup = `{"testResults":[
      {"name":"/repo/tests/dup.spec.ts","status":"passed","startTime":0,"endTime":50,
       "assertionResults":[
         {"title":"same","ancestorTitles":[],"fullName":"same","status":"passed","duration":10},
         {"title":"same","ancestorTitles":[],"fullName":"same","status":"passed","duration":20},
         {"title":"other","ancestorTitles":[],"fullName":"other","status":"passed","duration":15}]}]}`
	sum2, err := r.ParseTimings(strings.NewReader(dup))
	if err != nil {
		t.Fatalf("ParseTimings(dup): %v", err)
	}
	if sum2.Unsliceable["tests/dup.spec.ts"] {
		t.Error("a genuine duplicate name was wrongly flagged unsliceable")
	}
	if rows := sum2.TestSeconds["tests/dup.spec.ts"]; len(rows) != 2 {
		t.Errorf("duplicate-name file recorded %d per-test rows, want 2 (the dup collapses to one key)", len(rows))
	}
}

func wholeFileUnit(id string, secs float64) runner.Unit {
	return runner.Unit{
		ID: id, Kind: runner.KindPackage, Seconds: secs, Count: 1,
		Packages: []runner.LivePackage{{ID: id, HasTests: true}},
	}
}

func TestRenderWholeFilesSerially(t *testing.T) {
	r := mustNew(t, Options{Root: "frontend"})
	b := runner.Bucket{Index: 0, Units: []runner.Unit{
		wholeFileUnit("tests/b.spec.ts", 1),
		wholeFileUnit("tests/a.spec.ts", 2),
	}}
	got := r.Render(b)

	if !got.NeedsNode {
		t.Error("a Vitest bucket must set NeedsNode")
	}
	if len(got.Invocations) != 1 {
		t.Fatalf("got %d invocations, want the whole files merged into 1", len(got.Invocations))
	}
	args := strings.Join(got.Invocations[0].Args, " ")
	for _, want := range []string{"npx vitest run", "--no-file-parallelism", "tests/a.spec.ts", "tests/b.spec.ts"} {
		if !strings.Contains(args, want) {
			t.Errorf("invocation args miss %q: %s", want, args)
		}
	}
	// Files are sorted, so the command is a pure function of the bucket.
	if !strings.Contains(args, "tests/a.spec.ts tests/b.spec.ts") {
		t.Errorf("files are not sorted: %s", args)
	}
	if !strings.HasPrefix(got.Script, "set -euo pipefail\n( cd frontend && npx vitest run") {
		t.Errorf("script does not cd into the project and fail fast:\n%s", got.Script)
	}
	if strings.Contains(got.Script, "outputFile") {
		t.Errorf("no events dir was set, but the script captures events:\n%s", got.Script)
	}
}

func sliceUnit(file string, names []string) runner.Unit {
	return runner.Unit{
		ID: file + "[" + strings.Join(names, "|") + "]", Kind: runner.KindRunSlice, Count: 1,
		Run:      names,
		Packages: []runner.LivePackage{{ID: file, HasTests: true}},
	}
}

// TestRenderRunSlice proves a name slice renders to its own `vitest run -t` call
// over exactly its one file, with the robust anchored pattern.
func TestRenderRunSlice(t *testing.T) {
	r := mustNew(t, Options{Root: "frontend"})
	b := runner.Bucket{Index: 1, Units: []runner.Unit{
		sliceUnit("tests/whale.spec.ts", []string{"outer > inner a", "flat one"}),
		wholeFileUnit("tests/small.spec.ts", 1),
	}}
	got := r.Render(b)
	if len(got.Invocations) != 2 {
		t.Fatalf("got %d invocations, want a whole-file merge + one slice call", len(got.Invocations))
	}
	// Find the slice invocation (the one carrying -t).
	var sliceArgs string
	for _, inv := range got.Invocations {
		joined := strings.Join(inv.Args, "\x00")
		if strings.Contains(joined, "-t") {
			sliceArgs = joined
		}
	}
	if sliceArgs == "" {
		t.Fatalf("no slice invocation carried -t: %+v", got.Invocations)
	}
	// The robust, anchored pattern (separators -> (?: > | ), sorted names).
	if !strings.Contains(sliceArgs, `^(flat one|outer(?: > | )inner a)$`) {
		t.Errorf("slice -t pattern is not the robust anchored form: %s", strings.ReplaceAll(sliceArgs, "\x00", " "))
	}
	// The slice runs its ONE file, not the other bucket file.
	if !strings.Contains(sliceArgs, "tests/whale.spec.ts") || strings.Contains(sliceArgs, "tests/small.spec.ts\x00") {
		t.Errorf("slice invocation did not target exactly its own file: %s", strings.ReplaceAll(sliceArgs, "\x00", " "))
	}
}

// TestRenderFileParallelism proves #22: default is serial (--no-file-parallelism),
// a bound >1 renders --maxWorkers=N and drops the serial flag.
func TestRenderFileParallelism(t *testing.T) {
	serial := mustNew(t, Options{Root: "."})
	got := serial.Render(runner.Bucket{Units: []runner.Unit{wholeFileUnit("a.spec.ts", 1)}})
	args := strings.Join(got.Invocations[0].Args, " ")
	if !strings.Contains(args, "--no-file-parallelism") || strings.Contains(args, "--maxWorkers") {
		t.Errorf("default render must stay serial: %s", args)
	}

	par := mustNew(t, Options{Root: ".", FileParallelism: 4})
	got = par.Render(runner.Bucket{Units: []runner.Unit{wholeFileUnit("a.spec.ts", 1)}})
	args = strings.Join(got.Invocations[0].Args, " ")
	if !strings.Contains(args, "--maxWorkers=4") || strings.Contains(args, "--no-file-parallelism") {
		t.Errorf("FileParallelism=4 must render --maxWorkers=4 and no serial flag: %s", args)
	}
}

func TestRunnableNamesRejectsAmbiguous(t *testing.T) {
	// Two rows for one file whose names collapse to the same space-form: a -t
	// filter cannot separate them, so the whole file is refused for slicing.
	const list = `[
      {"name":"a > b","file":"/repo/tests/x.spec.ts"},
      {"name":"a b","file":"/repo/tests/x.spec.ts"}
    ]`
	if _, err := runnableNames("/repo", "tests/x.spec.ts", []byte(list)); err == nil {
		t.Fatal("ambiguous names were accepted; a slice could run a test twice")
	} else if !strings.Contains(err.Error(), "collide") {
		t.Errorf("error does not explain the collision: %v", err)
	}
	// Distinct nested names are fine.
	const ok = `[
      {"name":"group > a","file":"/repo/tests/x.spec.ts"},
      {"name":"group > b","file":"/repo/tests/x.spec.ts"}
    ]`
	names, err := runnableNames("/repo", "tests/x.spec.ts", []byte(ok))
	if err != nil {
		t.Fatalf("distinct nested names rejected: %v", err)
	}
	if strings.Join(names, ",") != "group > a,group > b" {
		t.Errorf("names = %v, want the two nested names in `>`-form", names)
	}
}

func TestRenderCapturesEventsWhenConfigured(t *testing.T) {
	r := mustNew(t, Options{Root: ".", EventsDir: "/tmp/ev"})
	got := r.Render(runner.Bucket{Index: 3, Units: []runner.Unit{wholeFileUnit("a.spec.ts", 1)}})
	if !strings.Contains(got.Script, "--reporter=json") || !strings.Contains(got.Script, "--outputFile.json=/tmp/ev/bucket-3-00.json") {
		t.Errorf("events capture not wired into the script:\n%s", got.Script)
	}
	if !strings.Contains(got.Script, "--reporter=default") {
		t.Errorf("the human-readable reporter was dropped:\n%s", got.Script)
	}
}

func TestValidateUnit(t *testing.T) {
	live := map[string]runner.LivePackage{
		"tests/a.spec.ts": {ID: "tests/a.spec.ts", HasTests: true},
		"tests/notest.ts": {ID: "tests/notest.ts", HasTests: false},
	}
	r := mustNew(t, Options{Root: "."})

	// Healthy whole-file unit.
	if d := r.ValidateUnit(wholeFileUnit("tests/a.spec.ts", 1), live, 1); len(d) != 0 {
		t.Errorf("a well-formed whole-file unit was rejected: %v", d)
	}
	// Healthy run-slice unit — including a name with a literal separator and a
	// regex metacharacter, both of which the renderer handles.
	okSlice := runner.Unit{
		ID: "tests/a.spec.ts[group > inner|has (p)]", Kind: runner.KindRunSlice, Count: 1,
		Run:      []string{"group > inner", "has (p)"},
		Packages: []runner.LivePackage{live["tests/a.spec.ts"]},
	}
	if d := r.ValidateUnit(okSlice, live, 1); len(d) != 0 {
		t.Errorf("a well-formed run-slice was rejected: %v", d)
	}

	bad := []struct {
		name string
		u    runner.Unit
		want string
	}{
		{"count-shard", runner.Unit{ID: "x#shard1", Kind: runner.KindCountShard, Count: 1, Shard: 1, Shards: 2, Packages: []runner.LivePackage{live["tests/a.spec.ts"]}}, "count-shard"},
		{"name filter on whole file", runner.Unit{ID: "tests/a.spec.ts", Kind: runner.KindPackage, Count: 1, Run: []string{"t"}, Packages: []runner.LivePackage{live["tests/a.spec.ts"]}}, "name filter"},
		{"empty run-slice", runner.Unit{ID: "x[]", Kind: runner.KindRunSlice, Count: 1, Run: nil, Packages: []runner.LivePackage{live["tests/a.spec.ts"]}}, "empty name set"},
		{"run-slice over two files", runner.Unit{ID: "x[t]", Kind: runner.KindRunSlice, Count: 1, Run: []string{"t"}, Packages: []runner.LivePackage{live["tests/a.spec.ts"], live["tests/notest.ts"]}}, "must cover exactly 1"},
		{"run-slice colliding names", runner.Unit{ID: "x[a > b|a b]", Kind: runner.KindRunSlice, Count: 1, Run: []string{"a > b", "a b"}, Packages: []runner.LivePackage{live["tests/a.spec.ts"]}}, "collide"},
		{"missing file", wholeFileUnit("tests/ghost.ts", 1), "not in the live test set"},
		{"no-test file", wholeFileUnit("tests/notest.ts", 1), "has no tests"},
		{"zero sweep", runner.Unit{ID: "tests/a.spec.ts", Kind: runner.KindPackage, Count: 0, Packages: []runner.LivePackage{live["tests/a.spec.ts"]}}, "runs each file exactly once"},
		{"repeat sweep count=2", runner.Unit{ID: "tests/a.spec.ts", Kind: runner.KindPackage, Count: 2, Packages: []runner.LivePackage{live["tests/a.spec.ts"]}}, "runs each file exactly once"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			d := r.ValidateUnit(tc.u, live, 1)
			if len(d) == 0 {
				t.Fatalf("a %s unit was accepted", tc.name)
			}
			if !strings.Contains(strings.Join(d, "\n"), tc.want) {
				t.Errorf("defects miss %q: %v", tc.want, d)
			}
		})
	}
}

func TestDiscoveryInvocationSelection(t *testing.T) {
	// glob is the DEFAULT: `list --filesOnly --json` (resolves files without
	// importing them).
	glob := mustNew(t, Options{Root: ".", Command: []string{"npx", "vitest"}})
	if tool, args := glob.discoveryInvocation(); strings.Join(tool.command, " ") != "npx vitest" ||
		strings.Join(args, " ") != "list --filesOnly --json" {
		t.Errorf("default discovery = %q %q, want the base command + `list --filesOnly --json`", tool.command, args)
	}
	// Explicit glob is identical.
	if _, args := mustNew(t, Options{Root: ".", DiscoveryMode: "glob"}).discoveryInvocation(); strings.Join(args, " ") != "list --filesOnly --json" {
		t.Errorf("glob discovery args = %q, want `list --filesOnly --json`", args)
	}
	// list opts back into the importing full-collection path.
	if _, args := mustNew(t, Options{Root: ".", DiscoveryMode: "list"}).discoveryInvocation(); strings.Join(args, " ") != "list --json" {
		t.Errorf("list discovery args = %q, want `list --json`", args)
	}
	// A verbatim discovery command OWNS its subcommand: it is run as-is with NO
	// appended args, and it overrides the mode.
	verbatim := mustNew(t, Options{
		Root:             ".",
		Command:          []string{"npx", "vitest"},
		DiscoveryMode:    "list",
		DiscoveryCommand: []string{"pnpm", "exec", "tb-discover"},
	})
	tool, args := verbatim.discoveryInvocation()
	if strings.Join(tool.command, " ") != "pnpm exec tb-discover" {
		t.Errorf("verbatim discovery command = %q, want the override run as-is", tool.command)
	}
	if len(args) != 0 {
		t.Errorf("verbatim discovery appended %q; a command that owns its subcommand must get nothing appended", args)
	}
}

// TestRunnablesArgsFileLeadsJSON pins the Runnables invocation offline (no Node).
// Three invariants the real-Vitest tests measure, guarded on every CI run even
// without Vitest installed:
//   - the file id leads --json — Vitest's `--json [true|path]` swallows a file
//     token immediately after it as an OUTPUT PATH, so `list --json <file>` writes
//     the report INTO the test file (clobbering it) and prints nothing;
//   - the file filter is present — a missing one un-scopes the list back to the
//     whole 201s project import;
//   - the positional cannot be read as an OPTION — a root-level id starting with
//     '-' (e.g. `--odd.spec.ts`) is `./`-prefixed, or CAC fails with "Unknown
//     option". Normal ids are `./`-prefixed too (verified to still scope).
func TestRunnablesArgsFileLeadsJSON(t *testing.T) {
	// Single-project (no project name): `list ./<file> --json`.
	single := runnablesArgs("", "tests/whale.spec.ts")
	if strings.Join(single, " ") != "list ./tests/whale.spec.ts --json" {
		t.Errorf("single-project args = %q, want `list ./<file> --json`", single)
	}
	// Multi-project: --project is appended AFTER; the file still leads --json.
	multi := runnablesArgs("unit", "pkg-a/ok.vtest.ts")
	if strings.Join(multi, " ") != "list ./pkg-a/ok.vtest.ts --json --project unit" {
		t.Errorf("multi-project args = %q, want `list ./<file> --json --project <p>`", multi)
	}
	// A root-level id that starts with '-' must be neutralised into a path token.
	dash := runnablesArgs("", "--odd.spec.ts")
	if strings.Join(dash, " ") != "list ./--odd.spec.ts --json" {
		t.Errorf("dash-leading args = %q, want the id `./`-prefixed so it is not read as an option", dash)
	}

	for _, tc := range []struct {
		name string
		args []string
	}{{"single", single}, {"multi", multi}, {"dash", dash}} {
		// The positional filter is arg index 1 (`list` is 0). It must exist, must
		// carry the file id, must not be readable as an option, and must precede
		// --json.
		if len(tc.args) < 2 || tc.args[0] != "list" {
			t.Fatalf("%s: args %q do not start with `list <filter>`", tc.name, tc.args)
		}
		filter := tc.args[1]
		if strings.HasPrefix(filter, "-") {
			t.Errorf("%s: positional filter %q begins with '-' — Vitest/CAC would read it as an option", tc.name, filter)
		}
		ji := indexOfArg(tc.args, "--json")
		if ji <= 1 {
			t.Errorf("%s: file filter at 1 does not lead --json at %d in %q — Vitest would swallow it as --json's output path and clobber the file", tc.name, ji, tc.args)
		}
	}
}

func indexOfArg(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func TestDiscoveryConfigValidation(t *testing.T) {
	// An unknown discovery mode fails loudly at construction.
	if _, err := New(Options{Root: ".", DiscoveryMode: "collect"}); err == nil {
		t.Error("an unknown discovery mode was accepted")
	}
	// A negative discovery timeout is refused (0 is the disable sentinel).
	if _, err := New(Options{Root: ".", DiscoveryTimeout: -time.Second}); err == nil {
		t.Error("a negative discovery timeout was accepted")
	}
	// DiscoveryTimeout takes precedence over the general Timeout for the discovery
	// subprocess; when it is unset, Timeout is the fallback.
	r := mustNew(t, Options{Root: ".", Timeout: 9 * time.Second, DiscoveryTimeout: 3 * time.Second})
	if tool, _ := r.discoveryInvocation(); tool.timeout != 3*time.Second {
		t.Errorf("discovery deadline = %s, want the DiscoveryTimeout (3s) to win over Timeout", tool.timeout)
	}
	fallback := mustNew(t, Options{Root: ".", Timeout: 9 * time.Second})
	if tool, _ := fallback.discoveryInvocation(); tool.timeout != 9*time.Second {
		t.Errorf("discovery deadline = %s, want the fallback to Timeout (9s) when DiscoveryTimeout is unset", tool.timeout)
	}
}

func TestCanonicalTokenStable(t *testing.T) {
	a := mustNew(t, Options{Root: "."}).CanonicalToken()
	b := mustNew(t, Options{Root: "other"}).CanonicalToken()
	if a == "" || a != b {
		t.Errorf("token should be a stable non-empty constant, got %q and %q", a, b)
	}
}

func TestLoadLivePackagesBothShapesAndRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := dir + "/" + name
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// vitest-list shape
	lp := write("list.json", `[{"name":"t","file":"`+dir+`/tests/a.spec.ts"}]`)
	got, err := LoadLivePackages(dir, lp)
	if err != nil || len(got) != 1 || got[0].ID != "tests/a.spec.ts" {
		t.Fatalf("list-shape load = %+v, err %v", got, err)
	}
	// neutral shape
	np := write("neutral.json", `[{"id":"tests/b.spec.ts","has_tests":true}]`)
	got, err = LoadLivePackages(dir, np)
	if err != nil || len(got) != 1 || got[0].ID != "tests/b.spec.ts" {
		t.Fatalf("neutral-shape load = %+v, err %v", got, err)
	}
	// empty identity in the neutral shape
	ep := write("empty.json", `[{"has_tests":true}]`)
	if _, err := LoadLivePackages(dir, ep); err == nil {
		t.Error("an entry with no id was accepted")
	}
}

func TestParseListRejectsMalformedRows(t *testing.T) {
	// A row that exists but carries no file is a truncated capture or a reporter
	// schema rename (name kept, file renamed). Dropping it would silently lose a
	// test, so the whole discovery document is refused.
	bad := map[string]string{
		"one row, no file":      `[{"name":"orphan"}]`,
		"mixed valid + no-file": `[{"name":"ok","file":"/repo/tests/a.spec.ts"},{"name":"orphan"}]`,
		"all rows lack file":    `[{"name":"a"},{"name":"b"}]`,
		"whitespace file":       `[{"name":"a","file":"  "}]`,
	}
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := parseList("/repo", []byte(body)); err == nil {
				t.Fatal("a malformed vitest list was accepted (a test would be silently dropped)")
			}
		})
	}
	// An EMPTY array is a project with genuinely no tests — legitimate, not
	// malformed, and must not error.
	live, err := parseList("/repo", []byte(`[]`))
	if err != nil || len(live) != 0 {
		t.Errorf("empty list = %v, err %v; want no error and no files", live, err)
	}
}

func TestLoadLivePackagesRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := dir + "/" + name
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// A mixed vitest-list document routes through parseList, which now rejects
	// the orphan row rather than dropping it.
	mixed := write("mixed.json", `[{"name":"ok","file":"`+dir+`/tests/a.spec.ts"},{"name":"orphan"}]`)
	if _, err := LoadLivePackages(dir, mixed); err == nil {
		t.Error("a mixed document silently dropped its malformed row")
	}
	// A whitespace-only neutral identity is refused too.
	ws := write("ws.json", `[{"id":"   ","has_tests":true}]`)
	if _, err := LoadLivePackages(dir, ws); err == nil {
		t.Error("a whitespace-only id was accepted")
	}
}
