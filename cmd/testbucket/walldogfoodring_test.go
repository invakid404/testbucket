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

// sameRepoPlanAndStore writes a shard plan whose canonical profile DECLARES the
// same-repository workload, plus the schema-2 store that receives its rows.
//
// The declaration lives in the profile block the observation copies verbatim, so
// the plan is the only thing that can grant §13.0's carve-out — which is what
// makes the ring gate's copy of the decision a read rather than a re-derivation.
func sameRepoPlanAndStore(t *testing.T, dir string) (store, plan string, digest walltime.Digest) {
	t.Helper()
	prof := sharedProfile()
	prof.SameRepositoryWorkload = true
	blk, err := walltime.NewProfileBlock(prof)
	if err != nil {
		t.Fatal(err)
	}

	store = filepath.Join(dir, "store.json")
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

	plan = filepath.Join(dir, "shard-plan.json")
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
		"profile":                         json.RawMessage(blk.Raw()),
		"runtime_profile_declared":        rpMap,
		"runtime_profile_declared_digest": string(walltime.RuntimeProfileDigest(rp)),
		"buckets": []map[string]any{{
			"bucket": 0, "name": "bucket-0", "est_seconds": 10.0, "needs_node": true,
			"units": []map[string]any{{"id": "f0.test.ts", "kind": "package",
				"packages": []string{"f0.test.ts"}, "est_seconds": 10.0}},
			"invocations": []map[string]any{{
				"dir": ".", "args": []string{"run", "f0.test.ts"},
				"desc": "f0.test.ts", "units": []string{"f0.test.ts"},
				"selector": []string{"./f0.test.ts"},
			}},
			"script": "vitest run f0.test.ts\n",
		}},
	}
	if err := writeJSONFile(plan, doc); err != nil {
		t.Fatal(err)
	}
	parsed, err := core.ParseShardPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	d, err := walltime.DigestJSON(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return store, plan, d
}

// TestDeclaredDogfoodRowReachesTheRing is R01's control.
//
// §13.0's carve-out admits one commit in all three provenance identities for an
// explicitly unscored run whose PLAN declared the workload to be the
// orchestration checkout. The qualifier applied it; the RING-level gate at
// `core.QC15` re-derived the pre-carve-out rule from the row alone and discarded
// the qualifying row one call before AppendRow — so the dogfood lane could never
// reach wall history, which is the whole thing the carve-out was decided for.
//
// This drives the shipped `ingest`, then re-reads the SAVED store, because the
// defect sat between qualification and persistence.
func TestDeclaredDogfoodRowReachesTheRing(t *testing.T) {
	bin := planBinary(t)

	run := func(t *testing.T, store, plan, obsDir string, events []string) string {
		t.Helper()
		args := append([]string{"ingest", "--store", store, "--no-golist",
			"--wall-observations", obsDir, "--wall-shard-plan", plan,
			"--runs-on-label", "ubuntu-latest"}, events...)
		cmd := exec.Command(bin, args...)
		var out strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("ingest failed: %v\n%s", err, out.String())
		}
		return out.String()
	}

	// ONE COMMIT, truthfully, in all three fields — the dogfood's real shape.
	one := strings.Repeat("a", 40)

	observation := func(t *testing.T, digest walltime.Digest, mutate func(*walltime.Observation)) walltime.Observation {
		t.Helper()
		prof := sharedProfile()
		prof.SameRepositoryWorkload = true
		blk, err := walltime.NewProfileBlock(prof)
		if err != nil {
			t.Fatal(err)
		}
		o := observationFixture("bucket-0", 0, "run-1", digest)
		o.Profile = blk
		o.HeadSHA, o.CandidateSHA, o.WorkloadCommit = one, one, one
		if mutate != nil {
			mutate(&o)
		}
		return o
	}

	t.Run("POSITIVE: the row is appended, saved and reloaded", func(t *testing.T) {
		dir := t.TempDir()
		store, plan, digest := sameRepoPlanAndStore(t, dir)
		obsDir := filepath.Join(dir, "obs")
		if err := os.MkdirAll(obsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, filepath.Join(obsDir, "b0.json"), observation(t, digest, nil))
		events := writeEvents(t, dir)

		out := run(t, store, plan, obsDir, []string{events})
		if strings.Contains(out, "QC15") {
			t.Fatalf("the ring refused a declared same-repository row:\n%s", out)
		}

		// FROM THE SAVED STORE, re-read after the process exited: the defect was
		// between qualification and persistence, so an in-memory assertion would
		// not have seen it.
		rows := ringRows(t, store)
		if len(rows) != 1 {
			t.Fatalf("the saved ring holds %d row(s), want 1:\n%s", len(rows), out)
		}
		for _, f := range []string{"head_sha", "candidate_sha", "workload_commit"} {
			if got, _ := rows[0][f].(string); got != one {
				t.Errorf("saved row %s is %q, want %q", f, got, one)
			}
		}

		t.Run("and a second ingest reloads it without refusing it", func(t *testing.T) {
			// The reload path re-reads the row through the decoder and rebuilds
			// the ring facts from it.
			obsDir2 := filepath.Join(dir, "obs2")
			if err := os.MkdirAll(obsDir2, 0o755); err != nil {
				t.Fatal(err)
			}
			o := observation(t, digest, func(o *walltime.Observation) {
				o.RunID, o.JobID = "run-2", "job-2"
			})
			writeFixture(t, filepath.Join(obsDir2, "b0.json"), o)
			out := run(t, store, plan, obsDir2, []string{events})
			if rows := ringRows(t, store); len(rows) != 2 {
				t.Fatalf("the reloaded ring holds %d row(s), want 2:\n%s", len(rows), out)
			}
		})
	})

	t.Run("NEGATIVE: an undeclared plan still refuses the collapsed row", func(t *testing.T) {
		dir := t.TempDir()
		store := filepath.Join(dir, "store.json")
		duplicateFixtureStore(t, store)
		plan, digest := writePlanFor(t, dir, "bucket-0", 0, []string{"run", "f0.test.ts"}, ".")
		obsDir := filepath.Join(dir, "obs")
		if err := os.MkdirAll(obsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		o := observationFixture("bucket-0", 0, "run-1", digest)
		o.HeadSHA, o.CandidateSHA, o.WorkloadCommit = one, one, one
		writeFixture(t, filepath.Join(obsDir, "b0.json"), o)

		out := run(t, store, plan, obsDir, []string{writeEvents(t, dir)})
		if !strings.Contains(out, "QC15") {
			t.Fatalf("a collapsed row under an undeclared plan was admitted:\n%s", out)
		}
		if rows := ringRows(t, store); len(rows) != 0 {
			t.Fatalf("%d row(s) entered the ring:\n%s", len(rows), out)
		}
	})

	t.Run("NEGATIVE: a missing identity is refused under the declaration", func(t *testing.T) {
		dir := t.TempDir()
		store, plan, digest := sameRepoPlanAndStore(t, dir)
		obsDir := filepath.Join(dir, "obs")
		if err := os.MkdirAll(obsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, filepath.Join(obsDir, "b0.json"),
			observation(t, digest, func(o *walltime.Observation) { o.CandidateSHA = "" }))

		out := run(t, store, plan, obsDir, []string{writeEvents(t, dir)})
		if !strings.Contains(out, "QC15") {
			t.Fatalf("the carve-out excused an absent candidate_sha:\n%s", out)
		}
		if rows := ringRows(t, store); len(rows) != 0 {
			t.Fatalf("%d row(s) entered the ring:\n%s", len(rows), out)
		}
	})
}
