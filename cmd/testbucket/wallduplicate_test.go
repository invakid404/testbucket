package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/walltime"
)

// ringRows reads the wall ring back off disk.
func ringRows(t *testing.T, store string) []map[string]any {
	t.Helper()
	var saved struct {
		Wall struct {
			Observations []map[string]any `json:"observations"`
		} `json:"wall"`
	}
	b, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	return saved.Wall.Observations
}

// duplicateFixtureStore is a schema-2 store with an empty ring, the state a
// second run of the same campaign starts from.
func duplicateFixtureStore(t *testing.T, path string) {
	t.Helper()
	writeFixture(t, path, map[string]any{
		"schema": 2, "flags": "vitest", "updated_at": "2026-09-01T00:00:00Z",
		"units": map[string]any{"f0.test.ts": map[string]any{"seconds": 10.0, "samples": 4}},
		"wall": map[string]any{
			"model_version":            1,
			"comparability_key_digest": sharedComparabilityKey,
			"status":                   "insufficient",
			"failure_subtype":          "migrated_no_history",
			"observations":             []any{},
		},
	})
}

// ingestObservations runs the shipped ingest over one directory and returns its
// stderr. A non-zero exit fails the test: QC11 rejects a ROW, it does not fail
// the command.
func ingestObservations(t *testing.T, bin, store, obsDir, plan, events string) string {
	t.Helper()
	cmd := exec.Command(bin, "ingest", "--store", store, "--no-golist",
		"--wall-observations", obsDir, "--wall-shard-plan", plan,
		"--runs-on-label", "ubuntu-latest", events)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("ingest failed: %v\n%s", err, stderr.String())
	}
	return stderr.String()
}

// TestQC11SurvivesTheStoreBeingSavedAndReloaded is the durability half.
//
// QC11 permits exactly one observation per (head_sha, run_id, run_attempt,
// bucket_name). The facts it reads were rebuilt from the saved ring with
// SeenObservationKeys left EMPTY, so the constraint held only within a single
// ingest invocation: two observations of one execution key that differed only
// in job_id had distinct intrinsic ids, passed QC16, and both entered the ring
// as trainable rows — refitting the model twice for one execution.
func TestQC11SurvivesTheStoreBeingSavedAndReloaded(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	store := filepath.Join(dir, "store.json")
	duplicateFixtureStore(t, store)
	plan, planDigest := writePlanFor(t, dir, "bucket-0", 0, []string{"run", "f0.test.ts"}, ".")
	events := writeEvents(t, dir)

	first := filepath.Join(dir, "obs-1")
	second := filepath.Join(dir, "obs-2")
	for _, d := range []string{first, second} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a := observationFixture("bucket-0", 0, "run-1", planDigest)
	b := observationFixture("bucket-0", 0, "run-1", planDigest)
	// THE ONLY DIFFERENCE. Same head, run, attempt, bucket — one execution,
	// measured twice under two job ids.
	b.JobID = "job-2"
	writeFixture(t, filepath.Join(first, "b0.json"), a)
	writeFixture(t, filepath.Join(second, "b0.json"), b)

	if out := ingestObservations(t, bin, store, first, plan, events); !strings.Contains(out, "accept") {
		t.Fatalf("the first observation was not accepted:\n%s", out)
	}
	out := ingestObservations(t, bin, store, second, plan, events)

	if !strings.Contains(out, "QC11") {
		t.Errorf("the duplicate execution key was not refused by QC11 after a reload:\n%s", out)
	}
	rows := ringRows(t, store)
	if len(rows) != 1 {
		t.Fatalf("the ring holds %d rows for ONE execution key; QC11 permits exactly one:\n%s",
			len(rows), out)
	}
	if rows[0]["job_id"] != "job-1" {
		t.Errorf("the surviving row is %v, want the one that was admitted first", rows[0]["job_id"])
	}
}

// TestQC11RejectsEveryRowOfADuplicatedKeyInOneBatch is the all-reject half.
//
// §7.1 says a duplicate rejects ALL rows for the key. Deciding it inside the
// append loop made it FIRST-WINS: the first arrival was appended and refitted
// and only the second was refused, so which of two contradictory measurements
// trained the model was settled by directory order. A key that appears twice is
// unresolvable by construction.
func TestQC11RejectsEveryRowOfADuplicatedKeyInOneBatch(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	store := filepath.Join(dir, "store.json")
	duplicateFixtureStore(t, store)
	plan, planDigest := writePlanFor(t, dir, "bucket-0", 0, []string{"run", "f0.test.ts"}, ".")
	events := writeEvents(t, dir)

	obsDir := filepath.Join(dir, "obs")
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := observationFixture("bucket-0", 0, "run-1", planDigest)
	b := observationFixture("bucket-0", 0, "run-1", planDigest)
	b.JobID = "job-2"
	writeFixture(t, filepath.Join(obsDir, "a.json"), a)
	writeFixture(t, filepath.Join(obsDir, "b.json"), b)

	out := ingestObservations(t, bin, store, obsDir, plan, events)

	if strings.Contains(out, "accept ") {
		t.Errorf("a row of a duplicated execution key was accepted; the rule is all-reject:\n%s", out)
	}
	if !strings.Contains(out, "0 of 2 observation(s) appended") {
		t.Errorf("the batch summary is not 0 of 2:\n%s", out)
	}
	if !strings.Contains(out, "fitter called 0 time(s)") {
		t.Errorf("an unresolvable batch still refit the model:\n%s", out)
	}
	if rows := ringRows(t, store); len(rows) != 0 {
		t.Errorf("%d row(s) of a duplicated execution key reached the ring", len(rows))
	}
}

// TestQC11LeavesDistinctExecutionKeysAlone is the control that keeps the two
// tests above from passing by refusing everything: two buckets of one run are
// two keys, and both belong in the ring.
func TestQC11LeavesDistinctExecutionKeysAlone(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	store := filepath.Join(dir, "store.json")
	duplicateFixtureStore(t, store)
	plan, planDigest := writePlanTwoBuckets(t, dir)
	// EACH BUCKET'S OWN TARGET, and events for both — which is what a real
	// two-bucket fan-in is. The plan's coverage gate puts every target in exactly
	// one bucket, so the old fixture's two buckets holding the SAME unit was a
	// plan no planner emits, and auditing the whole plan against the merged
	// evidence now says so.
	events := writeEventsForUnits(t, dir, "f0.test.ts", "g0.test.ts")

	obsDir := filepath.Join(dir, "obs")
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, c := range []struct{ name, unit string }{
		{"bucket-0", "f0.test.ts"},
		{"bucket-1", "g0.test.ts"},
	} {
		o := observationFixtureForUnit(c.name, i, "run-1", planDigest, c.unit)
		o.JobID = "job-" + c.name
		writeFixture(t, filepath.Join(obsDir, c.name+".json"), o)
	}

	out := ingestObservations(t, bin, store, obsDir, plan, events)
	if rows := ringRows(t, store); len(rows) != 2 {
		t.Fatalf("the ring holds %d rows; two distinct execution keys are two rows:\n%s", len(rows), out)
	}
}

// writePlanTwoBuckets renders a LEGAL two-bucket plan: each bucket holds its own
// target, as the coverage gate guarantees, so two observations of one run differ
// in the bucket they name and in the unit that bucket ran.
func writePlanTwoBuckets(t *testing.T, dir string) (string, walltime.Digest) {
	t.Helper()
	path := filepath.Join(dir, "shard-plan-2.json")
	rp := sharedRuntimeProfile()
	var rpMap map[string]string
	if err := json.Unmarshal(rp.OrderedJSON(), &rpMap); err != nil {
		t.Fatal(err)
	}
	bucket := func(index int, name, unit string) map[string]any {
		return map[string]any{
			"bucket": index, "name": name, "est_seconds": 10.0, "needs_node": true,
			"units": []map[string]any{{"id": unit, "kind": "package",
				"packages": []string{unit}, "est_seconds": 10.0}},
			"invocations": []map[string]any{{
				"dir": ".", "args": []string{"run", unit},
				"desc": unit, "units": []string{unit},
				"selector": []string{"./" + unit},
			}},
			"script": "vitest run " + unit + "\n",
		}
	}
	doc := map[string]any{
		"k": 8, "flags": "vitest", "algorithm": "karmarkar-karp",
		"store":                           "test-timings.json",
		"est_basis":                       "reporter",
		"comparability_key_digest":        sharedComparabilityKey,
		"expanded_unit_set_digest":        "sha256:" + strings.Repeat("c", 64),
		"profile":                         json.RawMessage(sharedProfileBlock(t).Raw()),
		"runtime_profile_declared":        rpMap,
		"runtime_profile_declared_digest": string(walltime.RuntimeProfileDigest(rp)),
		"buckets": []map[string]any{
			bucket(0, "bucket-0", "f0.test.ts"),
			bucket(1, "bucket-1", "g0.test.ts"),
		},
	}
	if err := writeJSONFile(path, doc); err != nil {
		t.Fatal(err)
	}
	parsed, err := core.ParseShardPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := walltime.DigestJSON(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return path, d
}
