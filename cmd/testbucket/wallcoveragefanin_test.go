package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMergedMultiBucketIngestAuditsPerBucketEvidence is F08's control.
//
// The record job downloads EVERY bucket's event artifact and parses one combined
// summary. `coverageVerdicts` handed that whole summary to each individual
// bucket's audit, and AuditCoverage then reported the other buckets' packages as
// unplanned for that bucket — correctly, by its own rule. So a perfectly valid
// two-bucket fan-in produced `map[bucket-0:false bucket-1:false]` and QC10
// rejected both rows, including the dogfood lane's whole point.
//
// Per-bucket verdicts are now bound to per-bucket evidence, with the whole plan
// still audited against the whole merged set. This test runs the real `ingest`
// over a real two-bucket plan with merged events and asserts both rows land; then
// it removes one bucket's events and asserts the fail-closed behaviour survives.
func TestMergedMultiBucketIngestAuditsPerBucketEvidence(t *testing.T) {
	bin := planBinary(t)

	ingest := func(t *testing.T, store, obsDir, plan, events string) (string, error) {
		t.Helper()
		cmd := exec.Command(bin, "ingest", "--store", store, "--no-golist",
			"--wall-observations", obsDir, "--wall-shard-plan", plan,
			"--runs-on-label", "ubuntu-latest", events)
		var out strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		return out.String(), err
	}

	setup := func(t *testing.T, units ...string) (store, obsDir, planPath, events string) {
		t.Helper()
		dir := t.TempDir()
		store = filepath.Join(dir, "store.json")
		duplicateFixtureStore(t, store)
		plan, digest := writePlanTwoBuckets(t, dir)
		events = writeEventsForUnits(t, dir, units...)
		obsDir = filepath.Join(dir, "obs")
		if err := os.MkdirAll(obsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for i, c := range []struct{ name, unit string }{
			{"bucket-0", "f0.test.ts"},
			{"bucket-1", "g0.test.ts"},
		} {
			o := observationFixtureForUnit(c.name, i, "run-1", digest, c.unit)
			o.JobID = "job-" + c.name
			writeFixture(t, filepath.Join(obsDir, c.name+".json"), o)
		}
		return store, obsDir, plan, events
	}

	t.Run("POSITIVE: a merged two-bucket fan-in appends both rows", func(t *testing.T) {
		// Both buckets' events, merged into one stream, exactly as the record
		// action's download-and-parse produces.
		store, obsDir, plan, events := setup(t, "f0.test.ts", "g0.test.ts")
		out, err := ingest(t, store, obsDir, plan, events)
		if err != nil {
			t.Fatalf("a valid merged two-bucket ingest failed: %v\n%s", err, out)
		}
		if strings.Contains(out, "QC10") {
			t.Fatalf("QC10 refused a bucket whose own events are present:\n%s", out)
		}
		rows := ringRows(t, store)
		if len(rows) != 2 {
			t.Fatalf("the ring holds %d row(s); two buckets with complete evidence are two rows:\n%s",
				len(rows), out)
		}
		seen := map[string]bool{}
		for _, r := range rows {
			if n, ok := r["bucket_index"].(float64); ok {
				seen[map[float64]string{0: "bucket-0", 1: "bucket-1"}[n]] = true
			}
		}
		for _, want := range []string{"bucket-0", "bucket-1"} {
			if !seen[want] {
				t.Errorf("the ring carries no row for %s: %v", want, rows)
			}
		}
	})

	t.Run("NEGATIVE: one bucket's events missing still refuses that bucket", func(t *testing.T) {
		// Fail-closed survives the scope fix: bucket-1 planned g0.test.ts and the
		// merged set reports only f0.test.ts, so bucket-1 has no evidence that it
		// ran its plan.
		store, obsDir, plan, events := setup(t, "f0.test.ts")
		out, err := ingest(t, store, obsDir, plan, events)
		if err != nil {
			t.Fatalf("ingest itself must not fail; the row is refused, not the command: %v\n%s", err, out)
		}
		if !strings.Contains(out, "QC10") {
			t.Fatalf("a bucket with no events for its planned target was not refused by QC10:\n%s", out)
		}
		for _, r := range ringRows(t, store) {
			if n, ok := r["bucket_index"].(float64); ok && n == 1 {
				t.Fatalf("bucket-1 entered the ring with no evidence that it ran:\n%s", out)
			}
		}
	})

	t.Run("NEGATIVE: an event belonging to no bucket fails every row", func(t *testing.T) {
		// The direction a per-bucket projection cannot see, which is why the whole
		// plan is still audited against the whole merged set.
		store, obsDir, plan, events := setup(t, "f0.test.ts", "g0.test.ts", "stowaway.test.ts")
		out, err := ingest(t, store, obsDir, plan, events)
		if err != nil {
			t.Fatalf("ingest itself must not fail: %v\n%s", err, out)
		}
		if !strings.Contains(out, "QC10") {
			t.Fatalf("an event for a target no bucket planned was accepted:\n%s", out)
		}
		if rows := ringRows(t, store); len(rows) != 0 {
			t.Fatalf("%d row(s) entered the ring over evidence that does not match the plan:\n%s",
				len(rows), out)
		}
	})
}
