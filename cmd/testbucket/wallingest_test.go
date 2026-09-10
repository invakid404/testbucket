package main

import (
	"encoding/json"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/walltime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestProductionObservationIngestCycle is the F2 regression, and it is BLACK
// BOX on purpose.
//
// `ingest --wall-observations` used to read the rows, print the accept/reject
// table, and stop. The §7.1 qualification, the ring append and the conditional
// refit all existed and all had passing tests; nothing in production called
// any of them, so no measured bucket could change a later plan. A test that
// invokes those helpers cannot see that. This one runs the shipped binary and
// reads the store back off disk.
func TestProductionObservationIngestCycle(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	store := filepath.Join(dir, "store.json")
	obsDir := filepath.Join(dir, "obs")
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A schema-2 store with an empty ring: the migration has already happened,
	// which is the state a second run is in.
	writeFixture(t, store, map[string]any{
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
	plan, planDigest := writePlanFor(t, dir, "bucket-0", 0, []string{"run", "f0.test.ts"}, ".")
	writeFixture(t, filepath.Join(obsDir, "b0.json"), observationFixture("bucket-0", 0, "run-1", planDigest))

	events := filepath.Join(dir, "ev.ndjson")
	if err := os.WriteFile(events, []byte(
		`{"Action":"run","Package":"f0.test.ts","Test":"T"}`+"\n"+
			`{"Action":"pass","Package":"f0.test.ts","Test":"T","Elapsed":1}`+"\n"+
			`{"Action":"pass","Package":"f0.test.ts","Elapsed":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "ingest", "--store", store, "--no-golist",
		"--wall-observations", obsDir, "--wall-shard-plan", plan,
		"--runs-on-label", "ubuntu-latest", events)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("ingest failed: %v\n%s", err, stderr.String())
	}

	// The loop must have RUN, not merely reported.
	if !strings.Contains(stderr.String(), "observation(s) appended") {
		t.Errorf("ingest printed no append summary; the loop did not run:\n%s", stderr.String())
	}

	var got struct {
		Wall struct {
			Observations []map[string]any `json:"observations"`
			Status       string           `json:"status"`
		} `json:"wall"`
	}
	b, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Wall.Observations) != 1 {
		t.Fatalf("the ring holds %d row(s) after ingesting one qualifying observation, want 1:\n%s",
			len(got.Wall.Observations), stderr.String())
	}
	row := got.Wall.Observations[0]
	if row["run_id"] != "run-1" {
		t.Errorf("the appended row names run %v, want run-1", row["run_id"])
	}
	if row["trainable"] != true {
		t.Errorf("an ordinary unscored row was appended non-trainable: %v", row["trainable"])
	}
}

// TestAnUnqualifiedObservationNeverReachesTheRing: qualification is the gate,
// so a row that fails it must leave the ring exactly as it was. Reporting the
// rejection and appending anyway would be worse than not checking.
func TestAnUnqualifiedObservationNeverReachesTheRing(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	store := filepath.Join(dir, "store.json")
	obsDir := filepath.Join(dir, "obs")
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, store, map[string]any{
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

	// §3.1's floor is A >= setup_ns + script_ns. This row breaks it, so QC7
	// must refuse it.
	plan, planDigest := writePlanFor(t, dir, "bucket-0", 0, []string{"run", "f0.test.ts"}, ".")
	bad := observationFixture("bucket-0", 0, "run-2", planDigest)
	bad.ElapsedNs = 1
	bad.SetupNs = 1_000_000_000
	bad.ScriptNs = 1_000_000_000
	writeFixture(t, filepath.Join(obsDir, "b0.json"), bad)

	events := filepath.Join(dir, "ev.ndjson")
	if err := os.WriteFile(events, []byte(
		`{"Action":"pass","Package":"f0.test.ts","Elapsed":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "ingest", "--store", store, "--no-golist",
		"--wall-observations", obsDir, "--wall-shard-plan", plan,
		"--runs-on-label", "ubuntu-latest", events)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("ingest failed: %v\n%s", err, stderr.String())
	}

	var got struct {
		Wall struct {
			Observations []map[string]any `json:"observations"`
		} `json:"wall"`
	}
	b, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Wall.Observations) != 0 {
		t.Errorf("an observation that fails §3.1's floor reached the ring: %v", got.Wall.Observations)
	}
	if !strings.Contains(stderr.String(), "REJECT") {
		t.Errorf("the rejection was not reported:\n%s", stderr.String())
	}
}

// observationFixture is a schema-valid unscored observation for one bucket.
//
// It is built through the PRODUCTION constructors — the profile block and the
// runtime-profile digest — rather than hand-written JSON, because several of
// §7.1's checks compare a document against a digest of its own contents. A
// hand-written fixture either has to restate those digests (and then agrees
// with itself while disagreeing with the code) or fails qualification for a
// reason that has nothing to do with what is being tested.
func observationFixture(bucket string, index int, runID string, planDigest walltime.Digest) walltime.Observation {
	block, err := walltime.NewProfileBlock(sharedProfile())
	if err != nil {
		panic(err)
	}
	rp := sharedRuntimeProfile()
	decl := walltime.CacheDeclaration{
		DependencyCacheMode: "disabled", TransformCacheMode: "disabled",
		DependencyCacheProducer:   "none",
		ExpectedMongoBinarySHA256: strings.Repeat("a", 64),
	}
	return walltime.Observation{
		Schema:                 walltime.ObservationSchema,
		ComparabilityKeyDigest: walltime.Digest(sharedComparabilityKey),
		Repository:             "owner/name",
		HeadSHA:                strings.Repeat("1", 40),
		CandidateSHA:           strings.Repeat("2", 40),
		WorkloadCommit:         strings.Repeat("3", 40),
		RunID:                  runID,
		RunAttempt:             "1",
		JobID:                  "job-1",
		BucketIndex:            index,
		BucketName:             bucket,
		PlanDigest:             planDigest,
		Profile:                block,
		EstSeconds:             10.0,
		AEtaNs:                 10_000_000_000,
		ProcessGroupID:         "pg-1",
		ActualRunnerName:       "runner-1",
		ObservedRunsOnLabel:    "ubuntu-latest",
		UnitIDs:                []string{"f0.test.ts"},
		Invocations: []walltime.Invocation{{
			Seq: 0, Units: []string{"f0.test.ts"},
			// Derived from the SAME argv and cwd the plan declares, through
			// the production digester. QC6 then compares two independently
			// derived values instead of a placeholder against itself.
			ArgvDigest: walltime.DigestJSONOrEmpty([]string{"run", "f0.test.ts"}),
			CwdDigest:  walltime.DigestJSONOrEmpty("."),
			Selector:   []string{"./f0.test.ts"}, Atoms: []string{},
			ProcessGroupID: "pg-1",
			StartedMonoNs:  1000, EndedMonoNs: 2_000_000_000,
			ElapsedNs: 1_999_999_000,
		}},
		StartedMonoNs:    0,
		EndedMonoNs:      10_000_000_000,
		ElapsedNs:        10_000_000_000,
		SetupNs:          1_000_000_000,
		ScriptNs:         8_000_000_000,
		ScriptOverheadNs: 1_000_000,
		WrapperNs:        1_000_000_000,
		RealtimeStart:    "2026-09-01T00:00:00Z",
		RealtimeEnd:      "2026-09-01T00:00:10Z",
		CacheState: walltime.CacheState{
			DependencyCacheMode: "disabled", TransformCacheMode: "disabled",
			DependencyCacheProducer:     "none",
			MongoBinarySHA256:           strings.Repeat("a", 64),
			ExpectedMongoBinarySHA256:   strings.Repeat("a", 64),
			MongoBinaryPath:             "/opt/mongodb/bin/mongod",
			MongoBinaryVerifiedOnRunner: true,
			DependencyCacheDisposition:  "disabled",
		},
		CacheDeclarationDigest: decl.Digest(),
		RuntimeProfile:         rp,
		RuntimeProfileDigest:   walltime.RuntimeProfileDigest(rp),
		Terminal:               "passed",
		Limitations:            walltime.CanonicalLimitations(),
	}
}

// sharedProfile is the canonical profile BOTH the plan and the observation
// carry. §13.0 has the observation copy the plan's profile verbatim, and QC13
// compares the two — so the test builds one value and hands it to both, which
// is what the production path does.
func sharedProfile() walltime.CanonicalProfile {
	return walltime.CanonicalProfile{
		Scored: false, RunnerToken: "vitest", K: 8, Count: 1, FileParallelism: 1,
		BucketIndices:         []int{0, 1, 2, 3, 4, 5, 6, 7},
		EstBasis:              walltime.EstBasis("reporter"),
		StoreSHA256:           "sha256:" + strings.Repeat("b", 64),
		ExpandedUnitSetDigest: "sha256:" + strings.Repeat("c", 64),
	}
}

// sharedProfileBlock is that profile in its CANONICAL serialization, which is
// what the plan carries and the observation copies verbatim.
func sharedProfileBlock(t *testing.T) walltime.ProfileBlock {
	t.Helper()
	b, err := walltime.NewProfileBlock(sharedProfile())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// sharedRuntimeProfile is the runtime profile the plan DECLARES and the
// observation reports as EXECUTED. QC17 compares them field by field.
func sharedRuntimeProfile() walltime.RuntimeProfile {
	return walltime.RuntimeProfile{
		NodeVersion: "26.8.1", PnpmVersion: "10.29.3", VitestVersion: "4.1.11",
		TestbucketSHA256:    "sha256:" + strings.Repeat("8", 64),
		FacadeCommand:       "pnpm exec tsx scripts/tb-vitest.ts",
		LockSHA256:          "sha256:" + strings.Repeat("7", 64),
		DependencyCacheMode: "disabled",
	}
}

const sharedComparabilityKey = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// writePlanFor writes the plan document the observation fixture claims to have
// been fanned out from, with the one invocation the fixture records. The
// argv/cwd digests are computed the way the planner computes them, so QC6
// compares two independently derived values rather than one value with itself.
func writePlanFor(t *testing.T, dir, bucket string, index int, argv []string, cwd string) (string, walltime.Digest) {
	t.Helper()
	path := filepath.Join(dir, "shard-plan.json")
	rp := sharedRuntimeProfile()
	var rpMap map[string]string
	if err := json.Unmarshal(rp.OrderedJSON(), &rpMap); err != nil {
		t.Fatal(err)
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
		"buckets": []map[string]any{{
			"bucket": index, "name": bucket, "est_seconds": 10.0, "needs_node": true,
			"units": []map[string]any{{"id": "f0.test.ts", "kind": "package",
				"packages": []string{"f0.test.ts"}, "est_seconds": 10.0}},
			"invocations": []map[string]any{{
				"dir": cwd, "args": argv,
				"desc": "f0.test.ts", "units": []string{"f0.test.ts"},
				"selector": []string{"./f0.test.ts"},
			}},
			"script": "vitest run f0.test.ts\n",
		}},
	}
	writeFixture(t, path, doc)
	// The digest QC3 compares against is the one the record job derives by
	// PARSING the plan, so the test derives it the same way rather than
	// hashing the bytes it just wrote.
	parsed, err := core.ParseShardPlan(path)
	if err != nil {
		t.Fatalf("ParseShardPlan: %v", err)
	}
	d, err := walltime.DigestJSON(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return path, d
}

// TestAssembledObservationSurvivesIngest closes the loop on itself.
//
// The two halves were separately absent: nothing produced an observation
// document, and nothing consumed one. Testing them apart would leave the
// interesting failure — a producer whose output the consumer refuses —
// invisible. So this measures a real command with `wall exec`, assembles the
// observation from those records and the plan, and ingests it, all through the
// shipped binary.
func TestAssembledObservationSurvivesIngest(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	records := filepath.Join(dir, "records")
	obsDir := filepath.Join(dir, "obs")
	for _, d := range []string{records, obsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// A real measured action: begin, a setup command, the script, one
	// invocation, end. These are the records the assembler reads.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	run("wall", "begin", "--dir", records, "--bucket-id", "bucket-0")
	run("wall", "run", "--dir", records, "--", "sh", "-c", "true")
	// The invocation runs INSIDE the script, as the generated bucket script
	// runs it. Measuring them as siblings would put V outside VB and break
	// §3.1's `script_ns >= Σ V[j]` — which the assembler correctly refuses,
	// and which is a property of the composition rather than of the tooling.
	inner := bin + " wall exec --dir " + records + " --level invocation" +
		" --bucket-id bucket-0 --cwd " + dir + " -- sh -c true"
	run("wall", "exec", "--dir", records, "--level", "script", "--bucket-id", "bucket-0",
		"--cwd", dir, "--", "sh", "-c", inner)
	run("wall", "end", "--dir", records, "--terminal", "passed")

	plan, _ := writePlanFor(t, dir, "bucket-0", 0, []string{"sh", "-c", "true"}, dir)
	obsFile := filepath.Join(obsDir, "bucket-0.json")
	cmd := exec.Command(bin, "wall", "assemble-observation",
		"--dir", records, "--shard-plan", plan, "--bucket-name", "bucket-0",
		"--out", obsFile, "--runs-on-label", "ubuntu-latest",
		"--repository", "owner/name", "--run-id", "run-9", "--attempt-id", "1",
		"--job", "job-1", "--head-sha", strings.Repeat("1", 40),
		"--candidate-sha", strings.Repeat("2", 40),
		"--workload-commit", strings.Repeat("3", 40))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("assemble-observation failed: %v\n%s", err, stderr.String())
	}

	// The assembled document must be the shape the loop consumes.
	b, err := os.ReadFile(obsFile)
	if err != nil {
		t.Fatal(err)
	}
	var obs map[string]any
	if err := json.Unmarshal(b, &obs); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"schema", "plan_digest", "profile", "elapsed_ns", "setup_ns", "script_ns"} {
		if obs[field] == nil {
			t.Errorf("the assembled observation carries no %s", field)
		}
	}

	// AND IT MUST SURVIVE THE GATE IT WAS BUILT FOR.
	//
	// This test used to stop at the JSON shape, which is why an assembler
	// whose every output the shipped ingest REJECTED could pass it. A producer
	// is only correct with respect to its consumer, so the consumer runs here.
	store := filepath.Join(dir, "store.json")
	writeFixture(t, store, map[string]any{
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
	events := filepath.Join(dir, "ev.ndjson")
	if err := os.WriteFile(events, []byte(
		`{"Action":"pass","Package":"f0.test.ts","Elapsed":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ing := exec.Command(bin, "ingest", "--store", store, "--no-golist",
		"--wall-observations", obsDir, "--wall-shard-plan", plan,
		"--runs-on-label", "ubuntu-latest", events)
	var ingErr strings.Builder
	ing.Stderr = &ingErr
	if err := ing.Run(); err != nil {
		t.Fatalf("ingest of the assembled observation failed: %v\n%s", err, ingErr.String())
	}
	if strings.Contains(ingErr.String(), "REJECT") {
		t.Fatalf("the shipped ingest REJECTED the shipped assembler's own output:\n%s", ingErr.String())
	}

	var saved struct {
		Wall struct {
			Observations []core.WallRingRow `json:"observations"`
		} `json:"wall"`
	}
	sb, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(sb, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Wall.Observations) != 1 {
		t.Fatalf("the assembled observation did not reach the ring (%d row(s)):\n%s",
			len(saved.Wall.Observations), ingErr.String())
	}
	// THE PERSISTED REGRESSORS MUST BE THE PLAN'S, NOT THE OBSERVATION'S.
	//
	// The bucket's single unit weighs 10.0s, so the plan-frozen reporter sum
	// is exactly 10e9 ns. The old row read `int64(est_seconds * 1e9)` off the
	// observation, which under the wall basis is the model's own A_eta
	// display — the model regressing on its own output.
	row := saved.Wall.Observations[0]
	if row.ReporterSumNs != 10_000_000_000 {
		t.Errorf("reporter_sum_ns is %d, want the plan's 10000000000", row.ReporterSumNs)
	}
	if row.StoreSHA256 == "" {
		t.Error("store_sha256 is empty; the plan's canonical profile carries one")
	}
	if row.WholeFileCount != 1 || row.SliceCount != 0 || row.IAnyWholeFile != 1 {
		t.Errorf("topology is whole=%d slice=%d indicator=%d, want the plan's 1/0/1",
			row.WholeFileCount, row.SliceCount, row.IAnyWholeFile)
	}

	// setup_ns is the term §3.1's floor needs and the one that was previously
	// underivable: `wall run` recorded no interval at all.
	if s, _ := obs["setup_ns"].(string); s == "" || s == "0" {
		t.Errorf("setup_ns is %q; the setup command's interval did not reach the observation", s)
	}
	if len(obs["invocations"].([]any)) != 1 {
		t.Errorf("the observation records %v invocations, want the one that ran", obs["invocations"])
	}
}

// TestPlanFrozenRegressorsCountSlicesTheObservationCannotSee is the sliced half
// of the same defect.
//
// Topology used to be inferred from `obs.invocations[].selector`, which the
// production assembler leaves nil, so slice_count was structurally zero for
// every row ever admitted and the fit's slice term could never be identified.
// The plan's selectors are always populated, so they are what is counted.
func TestPlanFrozenRegressorsCountSlicesTheObservationCannotSee(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shard-plan.json")
	writeFixture(t, path, map[string]any{
		"k": 8, "flags": "vitest", "algorithm": "karmarkar-karp",
		"store": "test-timings.json", "est_basis": "wall",
		"comparability_key_digest": sharedComparabilityKey,
		"profile":                  json.RawMessage(sharedProfileBlock(t).Raw()),
		"buckets": []map[string]any{{
			"bucket": 0, "name": "bucket-0", "est_seconds": 999.0, "needs_node": true,
			"units": []map[string]any{
				{"id": "a.test.ts", "kind": "package", "packages": []string{"a.test.ts"}, "est_seconds": 1.5},
				{"id": "b.test.ts[x]", "kind": "run-slice", "packages": []string{"b.test.ts"},
					"run": []string{"x"}, "est_seconds": 0.25},
			},
			"invocations": []map[string]any{
				{"dir": ".", "args": []string{"vitest", "run"}, "desc": "a", "units": []string{"a.test.ts"},
					"selector": []string{"./a.test.ts"}},
				{"dir": ".", "args": []string{"vitest", "run"}, "desc": "b", "units": []string{"b.test.ts[x]"},
					"selector": []string{"./b.test.ts", "-t", "^x$"}},
			},
			"script": "vitest run\n",
		}},
	})
	st := core.NewStore("vitest")
	_, frozen, err := planContextOf(path, "ubuntu-latest", st)
	if err != nil {
		t.Fatalf("planContextOf: %v", err)
	}
	got, ok := frozen["bucket-0"]
	if !ok {
		t.Fatal("the plan's bucket carries no frozen regressors")
	}
	// 1.5s + 0.25s, through the exact rational conversion — NOT the bucket's
	// 999.0 est_seconds, which under the wall basis is the A_eta display.
	if got.ReporterSumNs != 1_750_000_000 {
		t.Errorf("reporter_sum_ns is %d, want 1750000000", got.ReporterSumNs)
	}
	if got.WholeFiles != 1 || got.Slices != 1 {
		t.Errorf("topology is whole=%d slice=%d, want 1 whole and 1 slice", got.WholeFiles, got.Slices)
	}
	if got.StoreSHA256 == "" {
		t.Error("store_sha256 did not come off the plan's canonical profile")
	}
}
