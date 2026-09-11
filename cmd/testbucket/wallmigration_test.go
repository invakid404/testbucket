package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// storeShape is the part of the on-disk store §15.2 and §15.3 are about.
type storeShape struct {
	Schema       int  `json:"schema"`
	MigratedFrom *int `json:"migrated_from"`
	Wall         *struct {
		ComparabilityKeyDigest string           `json:"comparability_key_digest"`
		FailureSubtype         string           `json:"failure_subtype"`
		Observations           []map[string]any `json:"observations"`
	} `json:"wall"`
}

func readStoreShape(t *testing.T, path string) storeShape {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out storeShape
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("parse store: %v\n%s", err, b)
	}
	return out
}

// runIngestBinary runs the shipped ingest the way the record action does and
// returns its stderr. A non-zero exit fails the test with that stderr, because
// the failures this file is about were all exit-1 with an explanatory message
// nobody was reading.
func runIngestBinary(t *testing.T, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"ingest", "--no-golist"}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("ingest %v failed: %v\n%s", args, err, stderr.String())
	}
	return stderr.String()
}

// writeEvents writes the one reporter event the ingests below consume, UNDER THE
// NAME THE RENDERER GIVES IT.
//
// The Vitest renderer writes each invocation's reporter output to
// `bucket-<index>-<seq>.json`, and §7.1 QC10's per-bucket verdict is derived from
// exactly that: which bucket produced which results is carried by the path. A
// fixture that hands ingest an unattributable blob is handing it an input the
// shipped action never produces.
func writeEvents(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "bucket-0-00.json")
	if err := os.WriteFile(path, []byte(
		`{"Action":"pass","Package":"f0.test.ts","Elapsed":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestASchemaOneStoreMigratesWithTheKeyTheePlanCarries is the ordinary v0.2.2
// upgrade, which was broken.
//
// The record action has no `comparability-key` input, so nothing ever passed
// the flag, so §15.2's forward migration could not run and EVERY ingest over a
// restored v0.2.2 store exited 1 — with no wall observations anywhere in
// sight. The plan already rides into the record job and already carries the
// key, so the key travels with it.
func TestASchemaOneStoreMigratesWithTheKeyThePlanCarries(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	store := filepath.Join(dir, "store.json")
	writeFixture(t, store, map[string]any{
		"schema": 1, "flags": "vitest", "updated_at": "2026-09-01T00:00:00Z",
		"units": map[string]any{"f0.test.ts": map[string]any{"seconds": 10.0, "samples": 4}},
	})
	plan, _ := writePlanFor(t, dir, "bucket-0", 0, []string{"sh", "-c", "true"}, dir)

	// NO --comparability-key, exactly as the record action invokes it.
	runIngestBinary(t, bin, "--store", store, "--wall-shard-plan", plan, writeEvents(t, dir))

	got := readStoreShape(t, store)
	if got.Schema != 2 {
		t.Errorf("the store is schema %d after ingest, want 2", got.Schema)
	}
	if got.MigratedFrom == nil || *got.MigratedFrom != 1 {
		t.Errorf("migrated_from is %v, want 1: a schema-2 store with no marker cannot be told from one born at 2", got.MigratedFrom)
	}
	if got.Wall == nil {
		t.Fatal("the migrated store carries no wall object")
	}
	if got.Wall.ComparabilityKeyDigest != sharedComparabilityKey {
		t.Errorf("wall key is %q, want the plan's %q", got.Wall.ComparabilityKeyDigest, sharedComparabilityKey)
	}
	if got.Wall.FailureSubtype != "migrated_no_history" {
		t.Errorf("failure_subtype is %q, want migrated_no_history", got.Wall.FailureSubtype)
	}
}

// TestAFreshStoreGetsAWallObjectBeforeItIsAskedForOne covers the cold start.
//
// core.NewStore leaves Wall nil and Save stamps schema 2, so
// NeedsWallMigration is false forever: a store born on this version stayed
// wall-less, and every --wall-observations run against it exited 1 telling the
// caller to pass a flag that would change nothing.
func TestAFreshStoreGetsAWallObjectBeforeItIsAskedForOne(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	store := filepath.Join(dir, "store.json")
	writeFixture(t, store, map[string]any{
		"schema": 2, "flags": "vitest", "updated_at": "2026-09-01T00:00:00Z",
		"units": map[string]any{"f0.test.ts": map[string]any{"seconds": 10.0, "samples": 4}},
	})
	plan, _ := writePlanFor(t, dir, "bucket-0", 0, []string{"sh", "-c", "true"}, dir)

	runIngestBinary(t, bin, "--store", store, "--wall-shard-plan", plan, writeEvents(t, dir))

	got := readStoreShape(t, store)
	if got.Wall == nil {
		t.Fatal("a fresh schema-2 store still carries no wall object; --wall-observations can never work against it")
	}
	if got.Wall.ComparabilityKeyDigest != sharedComparabilityKey {
		t.Errorf("wall key is %q, want the plan's %q", got.Wall.ComparabilityKeyDigest, sharedComparabilityKey)
	}
	// Not a migration: this store was born at 2.
	if got.MigratedFrom != nil {
		t.Errorf("migrated_from is %v on a store that was never schema 1", *got.MigratedFrom)
	}
}

// TestAChangedComparabilityKeyDiscardsTheOldHistory is §15.3's second reset
// rule, which had no production caller: rows measured under one runtime could
// be joined with rows measured under another, and the fit would train across
// two populations.
func TestAChangedComparabilityKeyDiscardsTheOldHistory(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	store := filepath.Join(dir, "store.json")
	writeFixture(t, store, map[string]any{
		"schema": 2, "flags": "vitest", "updated_at": "2026-09-01T00:00:00Z",
		"units": map[string]any{"f0.test.ts": map[string]any{"seconds": 10.0, "samples": 4}},
		"wall": map[string]any{
			"model_version":            1,
			"comparability_key_digest": "sha256:" + strings.Repeat("9", 64),
			"status":                   "insufficient",
			"failure_subtype":          "rows_below_minimum",
			"observations": []any{map[string]any{
				"repository": "owner/name", "job_id": "job-1",
				"head_sha": strings.Repeat("1", 40), "candidate_sha": strings.Repeat("2", 40),
				"workload_commit": strings.Repeat("3", 40),
				"run_id":          "run-1", "run_attempt": "1",
				"observed_start_realtime": "2026-09-01T00:00:00Z",
				"trainable":               true, "ingest_seq": 1,
				"bucket_index": 0, "plan_digest": "sha256:p",
				"store_sha256": "sha256:s", "comparability_key_digest": "sha256:" + strings.Repeat("9", 64),
				"reporter_sum_ns": "1000000000", "i_any_whole_file": 1, "slice_count": 0,
				"elapsed_ns": "2000000000", "whole_file_count": 1, "invocation_count": 1,
				"terminal": "passed",
			}},
		},
	})
	plan, _ := writePlanFor(t, dir, "bucket-0", 0, []string{"sh", "-c", "true"}, dir)

	stderr := runIngestBinary(t, bin, "--store", store, "--wall-shard-plan", plan, writeEvents(t, dir))

	got := readStoreShape(t, store)
	if got.Wall == nil {
		t.Fatal("the store lost its wall object")
	}
	if got.Wall.ComparabilityKeyDigest != sharedComparabilityKey {
		t.Errorf("wall key is %q, want the plan's new %q", got.Wall.ComparabilityKeyDigest, sharedComparabilityKey)
	}
	if len(got.Wall.Observations) != 0 {
		t.Errorf("%d row(s) measured under the OLD key survived the change", len(got.Wall.Observations))
	}
	if !strings.Contains(stderr, "comparability key changed") {
		t.Errorf("the discard was silent; stderr was:\n%s", stderr)
	}
	// The two reset rules are independent: reporter EWMAs are untouched.
	var reporter struct {
		Units map[string]struct {
			Seconds float64 `json:"seconds"`
		} `json:"units"`
	}
	b, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &reporter); err != nil {
		t.Fatal(err)
	}
	if _, ok := reporter.Units["f0.test.ts"]; !ok {
		t.Error("the key change cleared reporter rows; §15.3's rules are independent")
	}
}

// writeEventsForUnits writes one bucket's reporter output per named target, in
// bucket order — target i to bucket i — which is what the downloaded artifact set
// looks like after the record job collects every bucket's events.
//
// One FILE PER BUCKET, because that is how the renderer writes them and how QC10
// recovers which bucket produced which results.
func writeEventsForUnits(t *testing.T, dir string, units ...string) []string {
	t.Helper()
	perBucket := make([][]string, len(units))
	for i, u := range units {
		perBucket[i] = []string{u}
	}
	return writeBucketEvents(t, dir, perBucket...)
}

// writeBucketEvents writes one reporter output file per bucket, each holding that
// bucket's own events, and returns the paths in bucket order.
//
// A nil entry writes no file at all, which is what a bucket that produced no
// artifact looks like.
func writeBucketEvents(t *testing.T, dir string, perBucket ...[]string) []string {
	t.Helper()
	var paths []string
	for i, lines := range perBucket {
		if lines == nil {
			continue
		}
		path := filepath.Join(dir, fmt.Sprintf("bucket-%d-00.json", i))
		var b strings.Builder
		for _, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "{") {
				// A raw event line, for a fixture that needs a name slice.
				b.WriteString(l + "\n")
				continue
			}
			fmt.Fprintf(&b, `{"Action":"pass","Package":%q,"Elapsed":1}`+"\n", l)
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths
}
