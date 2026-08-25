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
