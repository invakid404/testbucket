package vitestrunner_test

// End-to-end proof that the Vitest adapter drives the SAME core the Go adapter
// does: discover a real sample project, run the tool's own emitted commands,
// ingest the captured timings, and re-plan into a time-balanced split whose
// coverage gate is green and whose run the audit oracle accepts.
//
// It is gated on a real Vitest install (Node + the fixture's node_modules) and
// skipped otherwise, so the offline unit tests remain the always-on coverage.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/runner/vitestrunner"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "vitest-sample"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("no npx on PATH")
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules")); err != nil {
		t.Skip("vitest sample not installed (run `npm install` in testdata/vitest-sample)")
	}
	return root
}

// multiprojectRoot points at the deadlock fixture nested under the sample. It
// reuses the sample's single `node_modules` (node resolves vitest by walking up
// from the fixture dir), so no second install is needed, and it is gated on the
// same real Vitest install as fixtureRoot.
func multiprojectRoot(t *testing.T) string {
	t.Helper()
	sample, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "vitest-sample"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("no npx on PATH")
	}
	if _, err := os.Stat(filepath.Join(sample, "node_modules")); err != nil {
		t.Skip("vitest sample not installed (run `npm install` in testdata/vitest-sample)")
	}
	return filepath.Join(sample, "multiproject")
}

func planOpts(live []runner.LivePackage, token string) core.PlanOptions {
	return core.PlanOptions{K: 2, Count: 1, Live: live, Token: token, Now: time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)}
}

func TestVitestPipelineEndToEnd(t *testing.T) {
	root := fixtureRoot(t)
	ctx := context.Background()
	events := t.TempDir()

	rnr, err := vitestrunner.New(vitestrunner.Options{Root: root, EventsDir: events, Timeout: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Discover the real project. Prerequisites (npx + node_modules) already
	//    passed in fixtureRoot, so a Discover failure here is a PRODUCT
	//    regression (a vitest that exits non-zero, a bad list flag, a reporter
	//    change), not a missing environment — it must fail, never skip green.
	live, err := rnr.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover failed against an installed Vitest: %v", err)
	}
	wantIDs := []string{"tests/fast.spec.ts", "tests/medium.spec.ts", "tests/slow.spec.ts", "tests/tiny.spec.ts"}
	gotIDs := make([]string, 0, len(live))
	for _, p := range live {
		gotIDs = append(gotIDs, p.ID)
	}
	if strings.Join(gotIDs, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("discovered %v, want exactly the four fixture specs %v", gotIDs, wantIDs)
	}
	const slowID = "tests/slow.spec.ts"

	// 2. Plan cold — the gate must pass on a purely-Vitest tree.
	cold, err := core.BuildPlan(ctx, rnr, nil, "cold", planOpts(live, rnr.CanonicalToken()))
	if err != nil {
		t.Fatalf("cold plan failed the coverage gate: %v", err)
	}
	if len(cold.Buckets) != 2 {
		t.Fatalf("got %d buckets, want K=2", len(cold.Buckets))
	}

	// 3. Run each bucket's OWN emitted script (cwd is irrelevant — the script cds
	//    into the absolute project root), capturing JSON reports into events/.
	for _, b := range cold.Buckets {
		if len(strings.Split(strings.TrimSpace(b.Script), "\n")) < 2 {
			continue // empty bucket
		}
		cmd := exec.CommandContext(ctx, "bash", "-c", b.Script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bucket %d script failed: %v\n%s", b.Index, err, out)
		}
	}

	// 4. Ingest the captured timings.
	files, _ := filepath.Glob(filepath.Join(events, "*.json"))
	if len(files) == 0 {
		t.Fatal("no JSON reports were captured")
	}
	sum, err := parseAll(t, rnr, files)
	if err != nil {
		t.Fatalf("ParseTimings: %v", err)
	}

	// 5. Audit the run against the plan it came from.
	planned := loadPlanned(t, cold)
	if err := core.AuditCoverage(os.Stderr, planned, sum); err != nil {
		t.Fatalf("audit rejected a complete Vitest run: %v", err)
	}

	// 6. Record, then re-plan warm and assert the split is now time-balanced:
	//    the slow spec is heavy enough to be isolated into the makespan bucket.
	st := core.NewStore(rnr.CanonicalToken())
	rep, err := core.ApplyIngest(st, sum, core.IngestOptions{
		Alpha: 1, Count: 1, WhaleK: 2, MinShardSeconds: 30, Token: rnr.CanonicalToken(),
		Now: time.Now(), Live: live, LiveAuthoritative: true,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	t.Logf("ingested %d file(s), total measured %.3fs", len(rep.New)+len(rep.Updated), rep.TotalSeconds)

	warm, err := core.BuildPlan(ctx, rnr, st, "", planOpts(live, rnr.CanonicalToken()))
	if err != nil {
		t.Fatalf("warm plan failed the gate: %v", err)
	}
	if warm.Summary.ColdStart {
		t.Errorf("the plan after a record still reports a cold start: %q", warm.Summary.ColdStartReason)
	}

	// Every file scheduled exactly once.
	scheduled := map[string]int{}
	heaviest := warm.Buckets[0]
	for _, b := range warm.Buckets {
		if b.Seconds > heaviest.Seconds {
			heaviest = b
		}
		for _, u := range b.Units {
			for _, id := range u.Packages {
				scheduled[id]++
			}
		}
	}
	for _, p := range live {
		if scheduled[p.ID] != 1 {
			t.Errorf("%s scheduled %d times, want 1", p.ID, scheduled[p.ID])
		}
	}
	// The slow spec drove the balance: measured timings must ISOLATE it — its
	// bucket holds it and NOTHING ELSE (the other three specs pack into the other
	// bucket). Membership in the heaviest bucket is not enough; isolation is the
	// advertised end-state.
	var slowBucket *core.PlanBucket
	for i := range warm.Buckets {
		for _, u := range warm.Buckets[i].Units {
			for _, id := range u.Packages {
				if id == slowID {
					slowBucket = &warm.Buckets[i]
				}
			}
		}
	}
	if slowBucket == nil {
		t.Fatalf("the slow spec %q is in no bucket", slowID)
	}
	var covered []string
	for _, u := range slowBucket.Units {
		covered = append(covered, u.Packages...)
	}
	if len(covered) != 1 || covered[0] != slowID {
		t.Errorf("the slow spec was not isolated: its bucket covers %v, want only %q — measured timings did not drive the split", covered, slowID)
	}
	t.Logf("warm makespan %.3fs over %d buckets; slow spec isolated in a bucket of its own", heaviest.Seconds, len(warm.Buckets))
}

func parseAll(t *testing.T, rnr *vitestrunner.Runner, files []string) (*runner.RunSummary, error) {
	t.Helper()
	var readers []io.Reader
	var closers []*os.File
	defer func() {
		for _, c := range closers {
			c.Close()
		}
	}()
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		closers = append(closers, fh)
		readers = append(readers, fh)
	}
	return rnr.ParseTimings(readers...)
}

// loadPlanned round-trips the plan document through JSON into a PlannedCoverage,
// the way the record job loads the uploaded --shard-plan artifact.
func loadPlanned(t *testing.T, doc *core.PlanDocument) *core.PlannedCoverage {
	t.Helper()
	blob, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "shard-plan.json")
	if err := os.WriteFile(p, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	planned, err := core.LoadPlannedCoverage(p)
	if err != nil {
		t.Fatal(err)
	}
	return planned
}

// TestVitestGlobDiscoveryAvoidsCollectionDeadlock is the headline proof for
// issues #26 (glob discovery) and #25 (discovery timeout): a multi-project
// config with a spec whose MODULE never finishes importing.
//
//   - Under `--vitest-discovery=list` (full collection imports the module graph),
//     discovery DEADLOCKS — and the discovery timeout catches it, failing fast
//     with a clear error instead of hanging the whole job.
//   - Under the default glob mode (`vitest list --filesOnly`, no import),
//     discovery resolves BOTH files — including the hanging one — in about a
//     second, green.
//
// Same fixture, same adapter: only the discovery mode differs. That is the exact
// contrast that let consumers drop their `tb-vitest.ts` façade workaround.
func TestVitestGlobDiscoveryAvoidsCollectionDeadlock(t *testing.T) {
	root := multiprojectRoot(t)
	ctx := context.Background()

	// 1. list mode: collection imports pkg-b/hang.vtest.ts, whose top-level await
	//    never resolves — a genuine deadlock. A short discovery timeout turns the
	//    silent hang into a fast, actionable error.
	listRnr, err := vitestrunner.New(vitestrunner.Options{
		Root:             root,
		DiscoveryMode:    "list",
		DiscoveryTimeout: 12 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := listRnr.Discover(ctx); err == nil {
		t.Fatal("`vitest list` collection did NOT deadlock — the fixture stopped reproducing the hang; the glob-vs-list contrast is no longer proven")
	} else {
		elapsed := time.Since(start)
		// It must fail FAST (near the deadline), not hang for minutes.
		if elapsed > 90*time.Second {
			t.Fatalf("discovery did not fail fast: took %s (the timeout did not bound the hang)", elapsed)
		}
		if !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "discovery") {
			t.Fatalf("the deadlock error is not a clear discovery timeout: %v", err)
		}
		t.Logf("list-mode collection deadlocked and was caught by the discovery timeout in %s: %v", elapsed.Round(time.Millisecond), err)
	}

	// 2. glob mode (the default): resolves the file list WITHOUT importing a line
	//    of test code, so the hanging module is enumerated, not executed.
	globRnr, err := vitestrunner.New(vitestrunner.Options{
		Root:             root,
		DiscoveryTimeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	globStart := time.Now()
	live, err := globRnr.Discover(ctx)
	if err != nil {
		t.Fatalf("glob discovery failed on the deadlock fixture (it must be immune to the collection hang): %v", err)
	}
	gotIDs := make([]string, 0, len(live))
	for _, p := range live {
		gotIDs = append(gotIDs, p.ID)
		if !p.HasTests {
			t.Errorf("glob-discovered file %s is not marked HasTests", p.ID)
		}
	}
	sort.Strings(gotIDs)
	wantIDs := []string{"pkg-a/ok.vtest.ts", "pkg-b/hang.vtest.ts"}
	if strings.Join(gotIDs, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("glob discovered %v, want the two multi-project specs %v", gotIDs, wantIDs)
	}
	t.Logf("glob discovery resolved %d files (incl. the hanging module) in %s — no collection", len(live), time.Since(globStart).Round(time.Millisecond))
}

// TestVitestSlicingEndToEnd forces a REAL name-slice against an installed Vitest
// and proves the never-drop invariant physically: the slow spec (two tests) is
// split across buckets, each bucket runs its own `vitest run -t <regex>`, and the
// union of what actually reported is exactly the file's two tests, each once —
// with the audit oracle accepting the run.
func TestVitestSlicingEndToEnd(t *testing.T) {
	root := fixtureRoot(t)
	ctx := context.Background()
	const slowID = "tests/slow.spec.ts"

	// A warm plan needs a store that flags the slow spec for name-slicing. Get it
	// the honest way: run a cold plan, run it, and ingest with a MinShardSeconds
	// low enough that the sub-second fixture crosses the "worth slicing" bar.
	events1 := t.TempDir()
	rnr, err := vitestrunner.New(vitestrunner.Options{Root: root, EventsDir: events1, Timeout: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	live, err := rnr.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	cold, err := core.BuildPlan(ctx, rnr, nil, "cold", planOpts(live, rnr.CanonicalToken()))
	if err != nil {
		t.Fatalf("cold plan: %v", err)
	}
	runBuckets(t, ctx, cold)
	files, _ := filepath.Glob(filepath.Join(events1, "*.json"))
	sum, err := parseAll(t, rnr, files)
	if err != nil {
		t.Fatalf("ingest parse: %v", err)
	}
	st := core.NewStore(rnr.CanonicalToken())
	if _, err := core.ApplyIngest(st, sum, core.IngestOptions{
		Alpha: 1, Count: 1, WhaleK: 2, MinShardSeconds: 0.02, Token: rnr.CanonicalToken(),
		Now: time.Now(), Live: live, LiveAuthoritative: true,
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if st.Units[slowID] == nil || st.Units[slowID].Split != "run" {
		t.Skipf("the fixture's slow spec was not flagged split=run (policy=%v); timing too tight to force a slice on this machine",
			st.Units[slowID])
	}

	// Warm plan — the slow spec is now name-sliced across the two buckets.
	events2 := t.TempDir()
	rnr2, err := vitestrunner.New(vitestrunner.Options{Root: root, EventsDir: events2, Timeout: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	warm, err := core.BuildPlan(ctx, rnr2, st, "", planOpts(live, rnr2.CanonicalToken()))
	if err != nil {
		t.Fatalf("warm plan failed the gate: %v", err)
	}
	slices := 0
	for _, b := range warm.Buckets {
		for _, u := range b.Units {
			if u.Kind == runner.KindRunSlice && len(u.Packages) == 1 && u.Packages[0] == slowID {
				slices++
			}
		}
	}
	if slices < 2 {
		t.Fatalf("the slow spec was not sliced across buckets (%d slice unit(s))", slices)
	}

	// Run the warm plan for real and collect what each -t slice actually executed.
	runBuckets(t, ctx, warm)
	warmFiles, _ := filepath.Glob(filepath.Join(events2, "*.json"))
	warmSum, err := parseAll(t, rnr2, warmFiles)
	if err != nil {
		t.Fatalf("warm parse: %v", err)
	}

	// Physical never-drop: the two tests of the slow spec each reported exactly
	// once across the slices — none dropped, none run twice.
	got := warmSum.TestSeconds[slowID]
	wantTests := []string{"slow calc", "slow io"}
	gotNames := make([]string, 0, len(got))
	for n := range got {
		gotNames = append(gotNames, n)
	}
	sort.Strings(gotNames)
	if strings.Join(gotNames, ",") != strings.Join(wantTests, ",") {
		t.Errorf("slices ran %v, want exactly the slow spec's two tests %v", gotNames, wantTests)
	}
	if warmSum.PackageRuns[slowID] != slices {
		t.Errorf("slow spec reported %d invocations, want %d (one per slice)", warmSum.PackageRuns[slowID], slices)
	}

	// The audit oracle accepts the sliced run against the plan it came from.
	planned := loadPlanned(t, warm)
	if err := core.AuditCoverage(io.Discard, planned, warmSum); err != nil {
		t.Fatalf("audit rejected a complete sliced run: %v", err)
	}
}

// runBuckets executes each non-empty bucket's own emitted script.
func runBuckets(t *testing.T, ctx context.Context, doc *core.PlanDocument) {
	t.Helper()
	for _, b := range doc.Buckets {
		if len(strings.Split(strings.TrimSpace(b.Script), "\n")) < 2 {
			continue
		}
		cmd := exec.CommandContext(ctx, "bash", "-c", b.Script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bucket %d script failed: %v\n%s", b.Index, err, out)
		}
	}
}

// TestVitestGlobSlicingMultiProjectNoDeadlock is the reconciliation proof for
// #21 landing on top of #28's glob discovery: name-slicing needs per-test NAMES,
// which glob discovery (files only) does not provide and which the whole-suite
// `vitest list` collection would DEADLOCK to produce on this multi-project
// fixture (pkg-b never finishes importing). The adapter resolves a whale's names
// with a file- and project-scoped `vitest list <file> --json --project <name>`,
// importing only the whale's own spec in its own project — so slicing works under
// the glob default without ever touching the hanging sibling. This runs it end to
// end: glob discovery, a scoped Runnables, a real slice, and a real run, no drop.
func TestVitestGlobSlicingMultiProjectNoDeadlock(t *testing.T) {
	root := multiprojectRoot(t)
	ctx := context.Background()
	const whale = "pkg-a/ok.vtest.ts"

	// A short discovery budget: if project scoping FAILED to avoid the sibling's
	// import deadlock, this fails fast instead of hanging the suite.
	events := t.TempDir()
	rnr, err := vitestrunner.New(vitestrunner.Options{
		Root: root, EventsDir: events, DiscoveryTimeout: 45 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Glob discovery is active (the default) and resolves both files without
	//    importing the hanging one.
	live, err := rnr.Discover(ctx)
	if err != nil {
		t.Fatalf("glob discovery failed on the multi-project fixture: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("glob discovered %d files, want 2 (pkg-a + pkg-b)", len(live))
	}

	// 2. Runnables on the healthy whale returns its names FAST — proof the
	//    project-scoped list imports only pkg-a, not the deadlocking pkg-b.
	start := time.Now()
	names, err := rnr.Runnables(ctx, runner.LivePackage{ID: whale, HasTests: true})
	if err != nil {
		t.Fatalf("Runnables deadlocked or failed on the multi-project whale: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 40*time.Second {
		t.Fatalf("Runnables took %s — it did not scope to the project and inherited the collection hang", elapsed)
	}
	if strings.Join(names, ",") != "alpha,group > beta,ok" {
		t.Fatalf("project-scoped Runnables = %v, want the three pkg-a names in `>`-form", names)
	}
	t.Logf("project-scoped Runnables resolved %d names in %s (sibling pkg-b never imported)", len(names), time.Since(start).Round(time.Millisecond))

	// 3. Plan over ONLY the healthy project (pkg-b's module is an intentional
	//    never-importing fixture — unrunnable by design), flagging the whale for a
	//    real name-slice. The gate must pass and the file must slice.
	st := core.NewStore(rnr.CanonicalToken())
	st.Units[whale] = &core.UnitStat{
		Seconds: 9, Samples: 3, Split: "run", SplitInto: 3,
		Tests: map[string]float64{"ok": 3, "alpha": 3, "group > beta": 3},
	}
	pkgA := []runner.LivePackage{{ID: whale, HasTests: true}}
	plan, err := core.BuildPlan(ctx, rnr, st, "", planOpts(pkgA, rnr.CanonicalToken()))
	if err != nil {
		t.Fatalf("coverage gate rejected the multi-project slice plan: %v", err)
	}
	slices := 0
	for _, b := range plan.Buckets {
		for _, u := range b.Units {
			if u.Kind == runner.KindRunSlice {
				slices++
			}
		}
	}
	if slices < 2 {
		t.Fatalf("the whale was not sliced across buckets (%d slice unit(s))", slices)
	}

	// 4. Run the slices for real and prove no test dropped or double-ran. Running
	//    a pkg-a file imports only pkg-a (vitest scopes a run by its file filter),
	//    so this does not touch pkg-b either.
	runBuckets(t, ctx, plan)
	evFiles, _ := filepath.Glob(filepath.Join(events, "*.json"))
	if len(evFiles) == 0 {
		t.Fatal("no JSON reports were captured from the sliced run")
	}
	sum, err := parseAll(t, rnr, evFiles)
	if err != nil {
		t.Fatalf("parse sliced-run events: %v", err)
	}
	got := sum.TestSeconds[whale]
	names2 := make([]string, 0, len(got))
	for n := range got {
		names2 = append(names2, n)
	}
	sort.Strings(names2)
	if strings.Join(names2, ",") != "alpha,group > beta,ok" {
		t.Errorf("sliced run reported %v, want the three pkg-a tests each once", names2)
	}
	planned := loadPlanned(t, plan)
	if err := core.AuditCoverage(io.Discard, planned, sum); err != nil {
		t.Fatalf("audit rejected the multi-project sliced run: %v", err)
	}
}

// emptytitleRoot points at the fixture carrying a legal empty-title test.
func emptytitleRoot(t *testing.T) string {
	t.Helper()
	sample, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "vitest-sample"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("no npx on PATH")
	}
	if _, err := os.Stat(filepath.Join(sample, "node_modules")); err != nil {
		t.Skip("vitest sample not installed (run `npm install` in testdata/vitest-sample)")
	}
	return filepath.Join(sample, "emptytitle")
}

// TestVitestSliceIncludesEmptyTitle is the P1 regression proof: Vitest accepts
// `test("", ...)`, and `vitest list --json` reports it as `"name":""` — a legal
// runnable. Earlier the adapter dropped empty names from the slice universe, so a
// whale with an empty-title test got sliced only over its NAMED tests and the
// empty one was silently dropped (the gate passed over the incomplete universe).
// This drives it end to end: glob discovery, a real slice, a real run — and the
// empty-title test is scheduled and runs exactly once.
func TestVitestSliceIncludesEmptyTitle(t *testing.T) {
	root := emptytitleRoot(t)
	ctx := context.Background()
	const whale = "whale.vtest.ts"
	events := t.TempDir()
	rnr, err := vitestrunner.New(vitestrunner.Options{Root: root, EventsDir: events, DiscoveryTimeout: 45 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	// The universe INCLUDES the empty title (the bug was dropping it here).
	live, err := rnr.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	names, err := rnr.Runnables(ctx, runner.LivePackage{ID: whale, HasTests: true})
	if err != nil {
		t.Fatalf("Runnables: %v", err)
	}
	if strings.Join(names, "|") != "|named one|named two" {
		t.Fatalf("Runnables = %v, want the empty title kept alongside the two named tests", names)
	}

	// Flag the whale for a real name-slice and plan over the discovered set.
	st := core.NewStore(rnr.CanonicalToken())
	st.Units[whale] = &core.UnitStat{
		Seconds: 9, Samples: 3, Split: "run", SplitInto: 3,
		Tests: map[string]float64{"": 3, "named one": 3, "named two": 3},
	}
	plan, err := core.BuildPlan(ctx, rnr, st, "", planOpts(live, rnr.CanonicalToken()))
	if err != nil {
		t.Fatalf("coverage gate rejected the empty-title slice plan: %v", err)
	}

	// Run the slices for real; the empty-title test must report exactly once.
	runBuckets(t, ctx, plan)
	evFiles, _ := filepath.Glob(filepath.Join(events, "*.json"))
	sum, err := parseAll(t, rnr, evFiles)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := sum.TestSeconds[whale]
	names2 := make([]string, 0, len(got))
	for n := range got {
		names2 = append(names2, n)
	}
	sort.Strings(names2)
	if strings.Join(names2, "|") != "|named one|named two" {
		t.Errorf("sliced run reported %v, want the empty title AND the two named tests each once", names2)
	}
	if _, ok := got[""]; !ok {
		t.Error("the empty-title test was DROPPED — it did not report from any slice")
	}
	planned := loadPlanned(t, plan)
	if err := core.AuditCoverage(io.Discard, planned, sum); err != nil {
		t.Fatalf("audit rejected the empty-title sliced run: %v", err)
	}
}

func TestVitestRejectsRepeatSweep(t *testing.T) {
	// Vitest has no per-invocation repeat sweep, so a plan at a base above one
	// must be REJECTED by the coverage gate rather than silently rendered as a
	// single run that omits the requested iterations. No Node needed: the gate
	// refuses before anything runs.
	rnr, err := vitestrunner.New(vitestrunner.Options{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	live := []runner.LivePackage{
		{ID: "tests/a.spec.ts", HasTests: true},
		{ID: "tests/b.spec.ts", HasTests: true},
	}
	opt := core.PlanOptions{K: 2, Count: 2, Live: live, Token: rnr.CanonicalToken(), Now: time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)}
	if _, err := core.BuildPlan(context.Background(), rnr, nil, "", opt); err == nil {
		t.Fatal("a Count=2 Vitest plan was accepted; the unsupported sweep would silently vanish")
	} else if !strings.Contains(err.Error(), "exactly once") && !strings.Contains(err.Error(), "repeat sweep") {
		t.Errorf("rejection does not explain the unsupported sweep: %v", err)
	}
	// The supported base of 1 still plans cleanly.
	opt.Count = 1
	if _, err := core.BuildPlan(context.Background(), rnr, nil, "", opt); err != nil {
		t.Fatalf("a Count=1 Vitest plan was rejected: %v", err)
	}
}

// TestVitestRunnablesFileScopedListing is the headline proof for the file-scope
// fix: Runnables lists EXACTLY one file's names by passing the file id as a
// positional filter, so Vitest collects only that spec instead of importing the
// whole project. Two properties, measured against a real Vitest on the four-file
// single-project fixture:
//
//   - Scoped: the raw file-scoped `vitest list` collects rows for ONLY the target
//     file; the other three specs are never imported (the 201s->30s win — on a
//     1,398-file project it is the difference between importing all of them to read
//     one file's names and importing one).
//   - Complete: that scoped name set is IDENTICAL to the old path — a whole-project
//     `vitest list --json` filtered to the same file in Go. No name dropped, none
//     added. The universe the never-drop gate slices over is unchanged, just cheaper.
func TestVitestRunnablesFileScopedListing(t *testing.T) {
	root := fixtureRoot(t)
	ctx := context.Background()
	const target = "tests/slow.spec.ts"

	rnr, err := vitestrunner.New(vitestrunner.Options{Root: root, Timeout: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	// OLD path: import the WHOLE project, then filter to the target file in Go —
	// exactly the name universe the pre-fix Runnables produced. `list --json` with
	// no file argument is the safe (non-clobbering) whole-project form.
	wholeRows := rawVitestList(t, ctx, root, "list", "--json")
	if allFiles := relFilesOf(t, root, wholeRows); len(allFiles) < 2 {
		t.Fatalf("whole-project list collected %v, want all four specs — fixture changed", allFiles)
	}
	oldNames := namesForFile(t, root, target, wholeRows)
	if len(oldNames) == 0 {
		t.Fatalf("whole-project list produced no names for %s — fixture changed", target)
	}

	// NEW path: the adapter's file-scoped Runnables.
	start := time.Now()
	got, err := rnr.Runnables(ctx, runner.LivePackage{ID: target, HasTests: true})
	if err != nil {
		t.Fatalf("file-scoped Runnables failed: %v", err)
	}
	t.Logf("file-scoped Runnables(%s) = %v in %s", target, got, time.Since(start).Round(time.Millisecond))

	// Complete: old vs new name sets are identical.
	if strings.Join(got, "\x00") != strings.Join(oldNames, "\x00") {
		t.Fatalf("file-scoped names %v != whole-project-filtered names %v — a name was dropped or added by scoping", got, oldNames)
	}
	if strings.Join(got, ",") != "slow calc,slow io" {
		t.Fatalf("Runnables = %v, want the slow spec's exactly two tests", got)
	}

	// Scoped: run the adapter's EXACT invocation form and prove the raw output
	// carries rows for ONLY the target file — the other three specs were never
	// collected. (If Vitest imported the whole project, their rows would appear.)
	scopedRows := rawVitestList(t, ctx, root, "list", target, "--json")
	scoped := relFilesOf(t, root, scopedRows)
	if len(scoped) != 1 || scoped[0] != target {
		t.Fatalf("file-scoped `vitest list %s --json` collected %v, want only %q — the import was NOT scoped to the file", target, scoped, target)
	}
	t.Logf("file-scoped list collected exactly %v; the other specs were never imported", scoped)
}

// TestVitestRunnablesFileScopeIsDeadlockSafe proves the file-scoped Runnables
// stays deadlock-safe on the multi-project fixture: `vitest list <file-in-a>
// --json --project a` collects only pkg-a's spec, so pkg-b — whose module never
// finishes importing — is never touched. Both scopes (the file positional and
// --project) exclude it. This asserts the adapter's exact combined invocation does
// not hang, returns pkg-a's precise names, and — via the raw output — that pkg-b's
// hanging file is absent from what was collected.
func TestVitestRunnablesFileScopeIsDeadlockSafe(t *testing.T) {
	root := multiprojectRoot(t)
	ctx := context.Background()
	const whale = "pkg-a/ok.vtest.ts"
	const hangingSibling = "pkg-b/hang.vtest.ts"

	// A short per-command deadline: if the file+project scope FAILED to exclude the
	// hanging sibling, this fails fast instead of hanging the whole suite.
	rnr, err := vitestrunner.New(vitestrunner.Options{Root: root, Timeout: 45 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	// The adapter path: Runnables must return pkg-a's names FAST — proof the scoped
	// list imports only pkg-a's spec, not the deadlocking pkg-b.
	start := time.Now()
	names, err := rnr.Runnables(ctx, runner.LivePackage{ID: whale, HasTests: true})
	if err != nil {
		t.Fatalf("file-scoped Runnables hung or failed on the multi-project whale: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 40*time.Second {
		t.Fatalf("Runnables took %s — it did not scope and inherited pkg-b's collection hang", elapsed)
	}
	if strings.Join(names, ",") != "alpha,group > beta,ok" {
		t.Fatalf("file-scoped Runnables = %v, want pkg-a's three names in `>`-form", names)
	}

	// The raw invocation the adapter emits: its collected rows must name ONLY
	// pkg-a's spec. The hanging sibling never appears — it was never imported, which
	// is exactly why the call returned instead of deadlocking.
	rows := rawVitestList(t, ctx, root, "list", whale, "--json", "--project", "a")
	files := relFilesOf(t, root, rows)
	if len(files) != 1 || files[0] != whale {
		t.Fatalf("file+project-scoped list collected %v, want only %q", files, whale)
	}
	for _, f := range files {
		if f == hangingSibling {
			t.Fatalf("the hanging sibling %q was collected — the scope did not exclude it", hangingSibling)
		}
	}
	t.Logf("file+project-scoped Runnables resolved %d names in %s; %q collected only %v (pkg-b never imported)",
		len(names), elapsed.Round(time.Millisecond), whale, files)
}

// rawVitestList runs a raw `npx vitest <args...>` against root and decodes its
// stdout as list JSON — the test's own window onto what Vitest actually collected,
// independent of the adapter's parsing. It MUST be called only with clobber-safe
// arg orderings (a file positional never immediately after --json; see Runnables),
// since a report written to a file would corrupt the fixture rather than print.
func rawVitestList(t *testing.T, ctx context.Context, root string, args ...string) []listEntryRaw {
	t.Helper()
	// Bound the raw call: if a regression un-scoped it on the multi-project fixture
	// it would import the hanging pkg-b and block forever, so fail fast instead of
	// hanging the whole suite to the go-test deadline.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	full := append([]string{"vitest"}, args...)
	cmd := exec.CommandContext(ctx, "npx", full...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("npx %s failed: %v\n%s", strings.Join(full, " "), err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatalf("`vitest %s` printed nothing on stdout — did an option swallow a positional as its value?\nstderr: %s",
			strings.Join(args, " "), stderr.String())
	}
	var rows []listEntryRaw
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		t.Fatalf("decoding `vitest %s` output: %v\nstdout: %q", strings.Join(args, " "), err, stdout.String())
	}
	return rows
}

// listEntryRaw mirrors the fields the tests read out of Vitest list JSON. Name is
// a pointer so a present-but-empty title ("") is distinct from an absent field.
type listEntryRaw struct {
	Name *string `json:"name"`
	File string  `json:"file"`
}

// relFilesOf reduces raw list rows to the sorted, unique set of root-relative
// slash file ids they collected.
func relFilesOf(t *testing.T, root string, rows []listEntryRaw) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		id := relOf(t, root, r.File)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// namesForFile reduces raw list rows to the sorted, unique test names belonging to
// one target file — the whole-project-then-filter path the pre-fix Runnables ran.
func namesForFile(t *testing.T, root, target string, rows []listEntryRaw) []string {
	t.Helper()
	seen := map[string]bool{}
	var names []string
	for _, r := range rows {
		if relOf(t, root, r.File) != target {
			continue
		}
		if r.Name == nil {
			t.Fatalf("list row for %s has no name field", target)
		}
		if seen[*r.Name] {
			continue
		}
		seen[*r.Name] = true
		names = append(names, *r.Name)
	}
	sort.Strings(names)
	return names
}

func relOf(t *testing.T, root, file string) string {
	t.Helper()
	rel, err := filepath.Rel(root, file)
	if err != nil {
		t.Fatalf("relativising %q under %q: %v", file, root, err)
	}
	return filepath.ToSlash(rel)
}

// dashfileRoot points at the fixture whose single spec's root-relative id starts
// with "-" (`--odd.vtest.ts`). It reuses the sample's node_modules like the other
// nested fixtures and is gated on the same real Vitest install.
func dashfileRoot(t *testing.T) string {
	t.Helper()
	sample, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "vitest-sample"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("no npx on PATH")
	}
	if _, err := os.Stat(filepath.Join(sample, "node_modules")); err != nil {
		t.Skip("vitest sample not installed (run `npm install` in testdata/vitest-sample)")
	}
	return filepath.Join(sample, "dashfile")
}

// TestVitestRunnablesHandlesDashLeadingFileID is the regression proof for the Codex
// finding: a root-level spec whose id starts with '-' (`--odd.vtest.ts`), passed as
// a BARE `vitest list` positional, is read by CAC as an option and fails the whole
// list with "Unknown option --odd" — a valid file the old whole-project-then-filter
// path handled. The adapter `./`-prefixes the filter, so Runnables scopes to the
// file and returns its names, identical to the old path; the common case still
// resolves too.
func TestVitestRunnablesHandlesDashLeadingFileID(t *testing.T) {
	root := dashfileRoot(t)
	ctx := context.Background()
	const dashID = "--odd.vtest.ts"

	rnr, err := vitestrunner.New(vitestrunner.Options{Root: root, Timeout: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	// Glob discovery sees the file even though its id begins with '-'.
	live, err := rnr.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(live) != 1 || live[0].ID != dashID {
		t.Fatalf("glob discovered %v, want exactly [%q]", live, dashID)
	}

	// Runnables must NOT fail with an option-parse error; it scopes to the file.
	got, err := rnr.Runnables(ctx, runner.LivePackage{ID: dashID, HasTests: true})
	if err != nil {
		t.Fatalf("Runnables on a '-'-leading id failed (the positional was read as an option?): %v", err)
	}
	if strings.Join(got, ",") != "odd one,odd two" {
		t.Fatalf("Runnables(%q) = %v, want the file's two names", dashID, got)
	}

	// Complete: identical to the whole-project-then-filter path (the old path a
	// bare positional would have crashed before ever reaching).
	oldNames := namesForFile(t, root, dashID, rawVitestList(t, ctx, root, "list", "--json"))
	if strings.Join(got, "\x00") != strings.Join(oldNames, "\x00") {
		t.Fatalf("`-`-leading names %v != whole-project-filtered %v", got, oldNames)
	}
	// Scoped: the `./`-prefixed invocation the adapter emits collects only this file.
	scoped := relFilesOf(t, root, rawVitestList(t, ctx, root, "list", "./"+dashID, "--json"))
	if len(scoped) != 1 || scoped[0] != dashID {
		t.Fatalf("`./`-prefixed list collected %v, want only %q", scoped, dashID)
	}
	t.Logf("`-`-leading id %q scoped to %v with names %v (a bare positional would have been read as an option)", dashID, scoped, got)
}
