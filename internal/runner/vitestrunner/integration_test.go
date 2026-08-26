package vitestrunner_test

// End-to-end proof that the Vitest adapter drives the SAME core the Go adapter
// does: discover a real sample project, run the tool's own emitted commands,
// ingest the captured timings, and re-plan into a time-balanced split whose
// coverage gate is green and whose run the audit oracle accepts.
//
// It is gated on a real Vitest install (Node + the fixture's node_modules) and
// skipped otherwise, so the offline unit tests remain the always-on coverage.

import (
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
// with a project-SCOPED `vitest list --json --project <name>`, importing only the
// whale's own project — so slicing works under the glob default without ever
// touching the hanging sibling. This runs it end to end: glob discovery, a
// project-scoped Runnables, a real slice, and a real run, with no drop.
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
