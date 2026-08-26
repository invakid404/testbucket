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

func TestRunnableNamesFiltersByFile(t *testing.T) {
	names, err := runnableNames("/repo", "tests/slow.spec.ts", []byte(listFixture))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names, ","); got != "slow calc,slow io" {
		t.Errorf("slow file names = %q, want the two slow tests sorted", got)
	}
}

// A Vitest JSON-reporter fixture: three passing files with distinct walls, one
// failed file, one file with no tests.
const reportFixture = `{
  "testResults": [
    {"name":"/repo/tests/slow.spec.ts","status":"passed","startTime":1000,"endTime":1300,
     "assertionResults":[{"title":"slow io","status":"passed","duration":180},{"title":"slow calc","status":"passed","duration":120}]},
    {"name":"/repo/tests/fast.spec.ts","status":"passed","startTime":2000,"endTime":2050,
     "assertionResults":[{"title":"fast add","status":"passed","duration":50}]},
    {"name":"/repo/tests/tiny.spec.ts","status":"passed","startTime":3000,"endTime":3010,
     "assertionResults":[{"title":"tiny","status":"passed","duration":10}]},
    {"name":"/repo/tests/broken.spec.ts","status":"failed","startTime":4000,"endTime":9000,
     "assertionResults":[{"title":"boom","status":"failed","duration":5000}]},
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
	// File granularity: NO per-test weights are recorded (name-slicing is a
	// documented follow-up).
	if len(sum.TestSeconds) != 0 {
		t.Errorf("per-test weights were recorded (%v); the file-granularity adapter must not", sum.TestSeconds)
	}
	// A stream with nothing usable is a broken capture, not a silent success.
	if _, err := r.ParseTimings(strings.NewReader(`{"testResults":[]}`)); err == nil {
		t.Error("an empty report was accepted")
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

	bad := []struct {
		name string
		u    runner.Unit
		want string
	}{
		{"count-shard", runner.Unit{ID: "x#shard1", Kind: runner.KindCountShard, Count: 1, Shard: 1, Shards: 2, Packages: []runner.LivePackage{live["tests/a.spec.ts"]}}, "count-shard"},
		{"run-slice", runner.Unit{ID: "x[t]", Kind: runner.KindRunSlice, Count: 1, Run: []string{"t"}, Packages: []runner.LivePackage{live["tests/a.spec.ts"]}}, "run-slice"},
		{"name filter on whole file", runner.Unit{ID: "tests/a.spec.ts", Kind: runner.KindPackage, Count: 1, Run: []string{"t"}, Packages: []runner.LivePackage{live["tests/a.spec.ts"]}}, "name filter"},
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
