package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCoverageAuditAcceptsAnEmptyDesignBucket is F23's control.
//
// §17.3a admits an empty bucket as a legitimate design row — it is produced
// whenever U < K — and the repaired QC18 treats its displayed estimate of 0 as a
// real value, not an absence. Such a bucket renders no invocation and therefore
// emits no event file, while still having a real action and setup interval worth
// measuring. The audit reported every one of them as a coverage problem, which is
// terminal, so a legal plan shape could not be verified at all.
//
// The second half is what keeps the check a check: planned work with no events is
// still exactly the failure this audit exists to catch.
func TestCoverageAuditAcceptsAnEmptyDesignBucket(t *testing.T) {
	write := func(t *testing.T, dir string, buckets []map[string]any) string {
		t.Helper()
		path := filepath.Join(dir, "shard-plan.json")
		doc := map[string]any{
			"k": len(buckets), "flags": "vitest", "algorithm": "karmarkar-karp",
			"store": "t.json", "est_basis": "reporter", "buckets": buckets,
		}
		if err := writeJSONFile(path, doc); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("zero planned units and zero events is not a problem", func(t *testing.T) {
		dir := t.TempDir()
		events := filepath.Join(dir, "events")
		if err := os.MkdirAll(events, 0o755); err != nil {
			t.Fatal(err)
		}
		plan := write(t, dir, []map[string]any{{
			"bucket": 0, "name": "bucket-0", "est_seconds": 0.0, "needs_node": true,
			"units": []map[string]any{}, "invocations": []map[string]any{}, "script": "",
		}})

		ev, err := coverageAudit(plan, events, "vitest")("bucket-0")
		if err != nil {
			t.Fatalf("the audit errored on an empty design row: %v", err)
		}
		if len(ev.Problems) != 0 {
			t.Fatalf("an empty bucket with no events was reported as a coverage problem: %v", ev.Problems)
		}
		if ev.Planned != 0 || ev.Reported != 0 {
			t.Errorf("planned/reported is %d/%d, want 0/0", ev.Planned, ev.Reported)
		}
		if ev.Report == "" {
			t.Error("a passing verdict with no report asks the reader to trust a boolean")
		}
	})

	t.Run("planned units with no events is still a problem", func(t *testing.T) {
		dir := t.TempDir()
		events := filepath.Join(dir, "events")
		if err := os.MkdirAll(events, 0o755); err != nil {
			t.Fatal(err)
		}
		plan := write(t, dir, []map[string]any{{
			"bucket": 0, "name": "bucket-0", "est_seconds": 10.0, "needs_node": true,
			"units": []map[string]any{{"id": "f0.test.ts", "kind": "package",
				"packages": []string{"f0.test.ts"}, "est_seconds": 10.0}},
			"invocations": []map[string]any{{"dir": ".", "args": []string{"run", "f0.test.ts"},
				"desc": "f0.test.ts", "units": []string{"f0.test.ts"}}},
			"script": "vitest run f0.test.ts\n",
		}})

		ev, err := coverageAudit(plan, events, "vitest")("bucket-0")
		if err != nil {
			t.Fatalf("audit: %v", err)
		}
		if len(ev.Problems) == 0 {
			t.Fatal("a bucket that planned a unit and produced no events must be a coverage problem")
		}
	})
}
