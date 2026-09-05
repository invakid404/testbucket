package core

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/runner/gorunner"
	"github.com/invakid404/testbucket/internal/runner/vitestrunner"
)

// The wall-time work adds a whole measurement subsystem, and the thing most
// likely to break quietly is not the new code — it is the two consumer
// contracts that were already load-bearing. These tests pin both, plus the
// neutrality of the Go adapter, so a future change to the shared core or to
// the actions has to break a test rather than a consumer.

// TestGoAdapterRendersTheBamlRestContract pins the Go consumer's contract:
// K=6, `-race -count=100`, count shards that add back up to the sweep, and a
// script whose bytes do not mention the wall-time wrapper anywhere.
func TestGoAdapterRendersTheBamlRestContract(t *testing.T) {
	r := mustGoRunner(gorunner.Options{
		Dir: ".", Race: true, Count: 100, Timeout: "20m",
	})
	if got, want := r.CanonicalToken(), "-race -count=100"; got != want {
		t.Errorf("canonical token = %q, want %q", got, want)
	}
	b := runner.Bucket{Index: 0, Units: []runner.Unit{
		{ID: "example.com/m/pkg", Kind: runner.KindPackage, Count: 100,
			Packages: []runner.LivePackage{{ID: "example.com/m/pkg", Dir: "pkg", Module: ".", Mode: runner.ModeWork, HasTests: true}},
			Module:   ".", Mode: runner.ModeWork},
		{ID: "example.com/m/whale#shard1of2", Kind: runner.KindCountShard, Count: 50, Shard: 1, Shards: 2,
			Packages: []runner.LivePackage{{ID: "example.com/m/whale", Dir: "whale", Module: ".", Mode: runner.ModeWork, HasTests: true}},
			Module:   ".", Mode: runner.ModeWork},
	}}
	out := r.Render(b)
	for _, want := range []string{"go test", "-race", "-count=100", "-count=50", "-timeout 20m", "-p=1"} {
		if !strings.Contains(out.Script, want) {
			t.Errorf("the Go script lost %q:\n%s", want, out.Script)
		}
	}
	// Wall-time measurement is Vitest-only. The Go adapter has no records
	// directory, no spec file and no wrapper — by construction, since its
	// Options struct has no such field, and here in its output too.
	for _, forbidden := range []string{"testbucket wall", "spec-", "--level invocation"} {
		if strings.Contains(out.Script, forbidden) {
			t.Errorf("the Go script mentions the wall-time wrapper (%q):\n%s", forbidden, out.Script)
		}
	}
	if out.NeedsNode {
		t.Errorf("a Go bucket with no node prefixes must not request Node")
	}
}

// TestGoEventsAndAuditAreUnchanged proves the Go event schema and the audit
// still agree after the store refactor: a `go test -json` stream parses, and
// the audit accepts exactly the coverage the plan scheduled.
func TestGoEventsAndAuditAreUnchanged(t *testing.T) {
	events := strings.Join([]string{
		`{"Action":"run","Package":"example.com/m/pkg","Test":"TestA"}`,
		`{"Action":"pass","Package":"example.com/m/pkg","Test":"TestA","Elapsed":0.5}`,
		`{"Action":"pass","Package":"example.com/m/pkg","Elapsed":1.25}`,
	}, "\n")
	sum, err := parseEvents(strings.NewReader(events))
	if err != nil {
		t.Fatalf("parseEvents: %v", err)
	}
	if len(sum.PackageSeconds) != 1 || sum.PackageSeconds["example.com/m/pkg"] != 1.25 {
		t.Fatalf("parsed package seconds = %v, want one target at 1.25 s", sum.PackageSeconds)
	}
	if got := sum.TestSeconds["example.com/m/pkg"]["TestA"]; got != 0.5 {
		t.Errorf("per-test weight = %v, want 0.5 s", got)
	}
	planned := &PlannedCoverage{
		Invocations: map[string]int{"example.com/m/pkg": 1},
		Runnables:   map[string][]string{},
		Units:       1,
	}
	var out strings.Builder
	if err := AuditCoverage(&out, planned, sum); err != nil {
		t.Errorf("the audit rejected a run that covered exactly what was planned: %v\n%s", err, out.String())
	}
}

// TestMandelVitestContract pins the Vitest consumer's contract: count=1,
// serial files, and one whole-file invocation per bucket. It also pins the
// opt-in: with no records directory the rendered bytes are the v0.2.2 bytes.
func TestMandelVitestContract(t *testing.T) {
	plain, err := vitestrunner.New(vitestrunner.Options{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	b := runner.Bucket{Index: 0, Units: []runner.Unit{
		{ID: "src/a.spec.ts", Kind: runner.KindPackage, Count: 1,
			Packages: []runner.LivePackage{{ID: "src/a.spec.ts", HasTests: true}}},
	}}
	out := plain.Render(b)
	if !strings.Contains(out.Script, "--no-file-parallelism") {
		t.Errorf("the Vitest script is not serial:\n%s", out.Script)
	}
	if !out.NeedsNode {
		t.Errorf("a Vitest bucket must request Node")
	}
	if strings.Contains(out.Script, "testbucket wall") {
		t.Errorf("wall-time measurement leaked into the default render:\n%s", out.Script)
	}
	if got := plain.CanonicalToken(); got != "vitest" {
		t.Errorf("canonical token = %q, want %q", got, "vitest")
	}
	// Count is the sharp one: the adapter's per-unit grammar check refuses a
	// sweep it cannot represent, which is what stops an impossible
	// count-shard policy reaching the store.
	live := map[string]runner.LivePackage{"src/a.spec.ts": {ID: "src/a.spec.ts", HasTests: true}}
	if msgs := plain.ValidateUnit(b.Units[0], live, 1); len(msgs) != 0 {
		t.Errorf("a count=1 whole-file unit was rejected: %v", msgs)
	}
	sharded := runner.Unit{ID: "src/a.spec.ts#shard1of2", Kind: runner.KindCountShard, Count: 50, Shard: 1, Shards: 2,
		Packages: []runner.LivePackage{{ID: "src/a.spec.ts", HasTests: true}}}
	if msgs := plain.ValidateUnit(sharded, live, 1); len(msgs) == 0 {
		t.Errorf("a count-sharded Vitest unit was accepted; Vitest runs each test once")
	}

	// And with a records directory, every invocation goes through the wrapper
	// — the same bucket, the same units, a different script.
	measured, err := vitestrunner.New(vitestrunner.Options{Root: ".", WallDir: "/var/tb/wall"})
	if err != nil {
		t.Fatal(err)
	}
	wall := measured.Render(b)
	for _, want := range []string{"testbucket wall exec", "--level invocation", "/var/tb/wall/spec-0-00.json"} {
		if !strings.Contains(wall.Script, want) {
			t.Errorf("the measured script lost %q:\n%s", want, wall.Script)
		}
	}
	// The INVOCATIONS are identical either way: measurement wraps the command,
	// it does not change which tests run.
	if len(wall.Invocations) != len(out.Invocations) {
		t.Fatalf("measurement changed the invocation count: %d vs %d", len(wall.Invocations), len(out.Invocations))
	}
	for i := range out.Invocations {
		if strings.Join(wall.Invocations[i].Args, " ") != strings.Join(out.Invocations[i].Args, " ") {
			t.Errorf("measurement changed invocation %d:\n  %v\n  %v", i, out.Invocations[i].Args, wall.Invocations[i].Args)
		}
	}
}

// TestMatrixSemanticsAreUnchanged pins what the consumers' workflows read: a
// numeric, one-decimal est_seconds, and the needs_node flag.
func TestMatrixSemanticsAreUnchanged(t *testing.T) {
	rnr, err := vitestrunner.New(vitestrunner.Options{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	live := []runner.LivePackage{
		{ID: "src/a.spec.ts", HasTests: true},
		{ID: "src/b.spec.ts", HasTests: true},
	}
	st := NewStore("vitest")
	st.Units["src/a.spec.ts"] = &UnitStat{Seconds: 12.34, Samples: 3}
	st.Units["src/b.spec.ts"] = &UnitStat{Seconds: 7.89, Samples: 3}
	doc, err := BuildPlan(context.Background(), rnr, st, "", PlanOptions{
		K: 2, StorePath: "test-timings.json", Count: 1, StaleAfter: 14 * 24 * time.Hour,
		Now: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC), Live: live, Token: "vitest",
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	matrix, err := doc.MatrixJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(matrix)
	for _, want := range []string{`"est_seconds":12.3`, `"est_seconds":7.9`, `"needs_node":true`} {
		if !strings.Contains(s, want) {
			t.Errorf("the matrix lost %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"est_seconds":"`) {
		t.Errorf("est_seconds became a string; consumers read it as a number:\n%s", s)
	}
}

// TestParseStoreMatchesLoadStore pins the refactor that let a replay read the
// store from frozen bytes: the two paths must produce the same store and the
// same not-an-error reason, or a replayed plan would differ from the live one
// for a reason nobody bound.
func TestParseStoreMatchesLoadStore(t *testing.T) {
	const body = `{"schema":1,"flags":"vitest","units":{"a":{"seconds":3,"samples":2}}}`
	dir := t.TempDir()
	path := dir + "/test-timings.json"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	fromFile, reasonFile, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	fromBytes, reasonBytes, err := ParseStore([]byte(body), path)
	if err != nil {
		t.Fatal(err)
	}
	if reasonFile != reasonBytes {
		t.Errorf("reasons differ: %q vs %q", reasonFile, reasonBytes)
	}
	if fromFile.Flags != fromBytes.Flags || len(fromFile.Units) != len(fromBytes.Units) {
		t.Errorf("stores differ: %+v vs %+v", fromFile, fromBytes)
	}
	// A wrong schema is a REASON, not an error, on both paths: that is what
	// makes a schema bump a loud cold start instead of a failed plan.
	if _, reason, err := ParseStore([]byte(`{"schema":99}`), "x"); err != nil || reason == "" {
		t.Errorf("a future schema gave err=%v reason=%q; want a reason and no error", err, reason)
	}
	if _, reason, err := ParseStore(nil, "x"); err != nil || reason == "" {
		t.Errorf("an empty store gave err=%v reason=%q; want a reason and no error", err, reason)
	}
}
