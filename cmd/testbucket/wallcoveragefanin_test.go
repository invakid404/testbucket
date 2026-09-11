package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner/vitestrunner"
	"github.com/invakid404/testbucket/internal/walltime"
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

	ingest := func(t *testing.T, store, obsDir, plan string, events []string) (string, error) {
		t.Helper()
		args := append([]string{"ingest", "--store", store, "--no-golist",
			"--wall-observations", obsDir, "--wall-shard-plan", plan,
			"--runs-on-label", "ubuntu-latest"}, events...)
		cmd := exec.Command(bin, args...)
		var out strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		return out.String(), err
	}

	setup := func(t *testing.T, units ...string) (store, obsDir, planPath string, events []string) {
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

// TestSlicedFileAcrossBucketsIngestsBothRows is F08's residual control.
//
// A name-sliced file legitimately has its slices in DIFFERENT buckets — the
// planner's own coverage gate permits it, and the real Vitest warm-plan path
// produces it. Both earlier attempts at the per-bucket verdict lost the
// attribution: the first audited each bucket against the whole merged summary,
// the second filtered that summary by PACKAGE. Either way each bucket was charged
// with the other's run of `shared.test.ts` and the other's name, and
// `coverageVerdicts` returned false for both.
//
// The evidence to tell them apart was always there: the renderer writes each
// invocation's reporter output to `bucket-<index>-<seq>.json`. This test drives
// the real `ingest` over exactly that file layout.
func TestSlicedFileAcrossBucketsIngestsBothRows(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	store := filepath.Join(dir, "store.json")
	duplicateFixtureStore(t, store)

	// TWO SLICES OF ONE FILE, one per bucket — `shared.test.ts[alpha]` in
	// bucket-0 and `shared.test.ts[beta]` in bucket-1.
	plan, digest := writePlanSlicedAcrossBuckets(t, dir)

	// Each bucket's OWN reporter output: its own run of the file and its own
	// name. Nothing here divides an aggregate or narrows observed names.
	events := writeBucketEvents(t, dir,
		[]string{
			`{"Action":"run","Package":"shared.test.ts","Test":"alpha"}`,
			`{"Action":"pass","Package":"shared.test.ts","Test":"alpha","Elapsed":1}`,
			`{"Action":"pass","Package":"shared.test.ts","Elapsed":1}`,
		},
		[]string{
			`{"Action":"run","Package":"shared.test.ts","Test":"beta"}`,
			`{"Action":"pass","Package":"shared.test.ts","Test":"beta","Elapsed":1}`,
			`{"Action":"pass","Package":"shared.test.ts","Elapsed":1}`,
		},
	)

	obsDir := filepath.Join(dir, "obs")
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, c := range []struct{ name, unit, run string }{
		{"bucket-0", "shared.test.ts[alpha]", "alpha"},
		{"bucket-1", "shared.test.ts[beta]", "beta"},
	} {
		o := observationFixtureForUnit(c.name, i, "run-1", digest, c.unit)
		o.JobID = "job-" + c.name
		// The rendered invocation of a name slice carries the -t filter, and its
		// selection identities are the digests of what it actually selected.
		sel := []string{"./shared.test.ts", "-t", "^" + c.run + "$"}
		o.Invocations[0].Selector = sel
		o.Invocations[0].SelectorDigest = walltime.DigestJSONOrEmpty(sel)
		o.Invocations[0].ArgvDigest = walltime.DigestJSONOrEmpty(
			[]string{"run", "shared.test.ts", "-t", "^" + c.run + "$"})
		writeFixture(t, filepath.Join(obsDir, c.name+".json"), o)
	}

	args := append([]string{"ingest", "--store", store, "--no-golist",
		"--wall-observations", obsDir, "--wall-shard-plan", plan,
		"--runs-on-label", "ubuntu-latest"}, events...)
	cmd := exec.Command(bin, args...)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("ingest failed: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "QC10") {
		t.Fatalf("QC10 refused a bucket holding one slice of a shared file:\n%s", out.String())
	}
	if rows := ringRows(t, store); len(rows) != 2 {
		t.Fatalf("the ring holds %d row(s); two slices of one file in two buckets are two rows:\n%s",
			len(rows), out.String())
	}

	t.Run("NEGATIVE: one slice's events missing still refuses that bucket", func(t *testing.T) {
		dir := t.TempDir()
		store := filepath.Join(dir, "store.json")
		duplicateFixtureStore(t, store)
		plan, digest := writePlanSlicedAcrossBuckets(t, dir)
		// Only bucket-0's artifact: bucket-1 ran its slice and reported nothing.
		events := writeBucketEvents(t, dir, []string{
			`{"Action":"run","Package":"shared.test.ts","Test":"alpha"}`,
			`{"Action":"pass","Package":"shared.test.ts","Test":"alpha","Elapsed":1}`,
			`{"Action":"pass","Package":"shared.test.ts","Elapsed":1}`,
		}, nil)

		obsDir := filepath.Join(dir, "obs")
		if err := os.MkdirAll(obsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for i, c := range []struct{ name, unit, run string }{
			{"bucket-0", "shared.test.ts[alpha]", "alpha"},
			{"bucket-1", "shared.test.ts[beta]", "beta"},
		} {
			o := observationFixtureForUnit(c.name, i, "run-1", digest, c.unit)
			o.JobID = "job-" + c.name
			sel := []string{"./shared.test.ts", "-t", "^" + c.run + "$"}
			o.Invocations[0].Selector = sel
			o.Invocations[0].SelectorDigest = walltime.DigestJSONOrEmpty(sel)
			o.Invocations[0].ArgvDigest = walltime.DigestJSONOrEmpty(
				[]string{"run", "shared.test.ts", "-t", "^" + c.run + "$"})
			writeFixture(t, filepath.Join(obsDir, c.name+".json"), o)
		}
		args := append([]string{"ingest", "--store", store, "--no-golist",
			"--wall-observations", obsDir, "--wall-shard-plan", plan,
			"--runs-on-label", "ubuntu-latest"}, events...)
		cmd := exec.Command(bin, args...)
		var out strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("ingest itself must not fail: %v\n%s", err, out.String())
		}
		if !strings.Contains(out.String(), "QC10") {
			t.Fatalf("a slice that reported nothing was admitted:\n%s", out.String())
		}
		for _, r := range ringRows(t, store) {
			if n, ok := r["bucket_index"].(float64); ok && n == 1 {
				t.Fatalf("bucket-1 entered the ring with no evidence:\n%s", out.String())
			}
		}
	})

	t.Run("NEGATIVE: a bucket reporting the OTHER slice's name is refused", func(t *testing.T) {
		// The direction a per-package filter could never see, and the one the
		// report forbids hiding: bucket-0's artifact reports `beta`, which is
		// bucket-1's name. Its own slice never ran.
		dir := t.TempDir()
		store := filepath.Join(dir, "store.json")
		duplicateFixtureStore(t, store)
		plan, digest := writePlanSlicedAcrossBuckets(t, dir)
		events := writeBucketEvents(t, dir,
			[]string{
				`{"Action":"run","Package":"shared.test.ts","Test":"beta"}`,
				`{"Action":"pass","Package":"shared.test.ts","Test":"beta","Elapsed":1}`,
				`{"Action":"pass","Package":"shared.test.ts","Elapsed":1}`,
			},
			[]string{
				`{"Action":"run","Package":"shared.test.ts","Test":"beta"}`,
				`{"Action":"pass","Package":"shared.test.ts","Test":"beta","Elapsed":1}`,
				`{"Action":"pass","Package":"shared.test.ts","Elapsed":1}`,
			},
		)
		obsDir := filepath.Join(dir, "obs")
		if err := os.MkdirAll(obsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		o := observationFixtureForUnit("bucket-0", 0, "run-1", digest, "shared.test.ts[alpha]")
		sel := []string{"./shared.test.ts", "-t", "^alpha$"}
		o.Invocations[0].Selector = sel
		o.Invocations[0].SelectorDigest = walltime.DigestJSONOrEmpty(sel)
		o.Invocations[0].ArgvDigest = walltime.DigestJSONOrEmpty(
			[]string{"run", "shared.test.ts", "-t", "^alpha$"})
		writeFixture(t, filepath.Join(obsDir, "bucket-0.json"), o)

		args := append([]string{"ingest", "--store", store, "--no-golist",
			"--wall-observations", obsDir, "--wall-shard-plan", plan,
			"--runs-on-label", "ubuntu-latest"}, events...)
		cmd := exec.Command(bin, args...)
		var out strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("ingest itself must not fail: %v\n%s", err, out.String())
		}
		if !strings.Contains(out.String(), "QC10") {
			t.Fatalf("a bucket whose artifact reports the other slice's name was admitted:\n%s", out.String())
		}
	})
}

// writePlanSlicedAcrossBuckets renders the shape F08 is about: ONE file, two name
// slices, one per bucket. It is what the planner emits for a whale file under a
// `split: run` policy, and the coverage gate admits it.
func writePlanSlicedAcrossBuckets(t *testing.T, dir string) (string, walltime.Digest) {
	t.Helper()
	path := filepath.Join(dir, "shard-plan-sliced.json")
	rp := sharedRuntimeProfile()
	var rpMap map[string]string
	if err := json.Unmarshal(rp.OrderedJSON(), &rpMap); err != nil {
		t.Fatal(err)
	}
	bucket := func(index int, name, run string) map[string]any {
		unit := "shared.test.ts[" + run + "]"
		return map[string]any{
			"bucket": index, "name": name, "est_seconds": 10.0, "needs_node": true,
			"units": []map[string]any{{
				"id": unit, "kind": "run-slice",
				"packages": []string{"shared.test.ts"},
				"run":      []string{run},
				// The structural Run field, which PlannedCoverageForBucket prefers
				// over parsing the id — a Vitest title can contain the '|' the id
				// joins on.
				"est_seconds": 10.0,
			}},
			"invocations": []map[string]any{{
				"dir": ".", "args": []string{"run", "shared.test.ts", "-t", "^" + run + "$"},
				"desc": unit, "units": []string{unit},
				"selector": []string{"./shared.test.ts", "-t", "^" + run + "$"},
			}},
			"script": "vitest run shared.test.ts -t '^" + run + "$'\n",
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
			bucket(0, "bucket-0", "alpha"),
			bucket(1, "bucket-1", "beta"),
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

// TestTheEventPathRouteMatchesTheRendererIsNaming binds the two independent ends
// of F08's evidence route.
//
// The per-bucket verdict recovers which bucket produced which results from the
// event file NAMES, and those names are written by the Vitest renderer in another
// package. Nothing else connects them: a rename on either side would silently
// send every bucket back to "no attributable evidence", which fails closed but
// for the wrong reason and would look like missing artifacts.
func TestTheEventPathRouteMatchesTheRendererIsNaming(t *testing.T) {
	dir := t.TempDir()
	// The renderer's own name for bucket 3, invocation 7, produced by the shipped
	// adapter rather than restated here.
	rendered := vitestrunner.EventsFilePathForTest(dir, 3, 7)

	byBucket, unattributed := eventPathsByBucket([]string{rendered})
	if len(unattributed) != 0 {
		t.Fatalf("the renderer's own event path %q is not attributable to a bucket", rendered)
	}
	if got := byBucket[3]; len(got) != 1 || got[0] != rendered {
		t.Fatalf("the renderer's path for bucket 3 grouped as %v", byBucket)
	}

	t.Run("a path with no bucket in its name is unattributed", func(t *testing.T) {
		byBucket, unattributed := eventPathsByBucket([]string{
			filepath.Join(dir, "merged.ndjson"),
			filepath.Join(dir, "bucket-notanumber-00.json"),
		})
		if len(byBucket) != 0 {
			t.Errorf("an unnamed stream was attributed to %v", byBucket)
		}
		if len(unattributed) != 2 {
			t.Errorf("got %d unattributed path(s), want 2", len(unattributed))
		}
	})

	t.Run("two invocations of one bucket group together", func(t *testing.T) {
		a := vitestrunner.EventsFilePathForTest(dir, 1, 0)
		b := vitestrunner.EventsFilePathForTest(dir, 1, 1)
		byBucket, _ := eventPathsByBucket([]string{a, b})
		if len(byBucket[1]) != 2 {
			t.Fatalf("bucket 1's two invocations grouped as %v", byBucket)
		}
	})
}
