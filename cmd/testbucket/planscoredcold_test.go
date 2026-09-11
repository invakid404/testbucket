package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestScoredColdPlanIsVetoedAgainstTheBuiltPlan is R02's control.
//
// §0.8's phase-2 veto rejects a scored run over ANY cold plan. `SelectBasis` runs
// before discovery, so its cold determination is about the STORE — present, right
// token, parseable — which is a different question from "is this plan cold". A
// schema-2 store with matching flags and an empty or wholly stale units object
// passes all three store tests; then `meanWeight` finds no measured live target,
// `BuildPlan` records `cold_start`, and the veto had already said yes. The CLI
// wrote the shard plan and printed the matrix: outcome (d) reached a fan-out.
//
// The veto now runs against the plan that was actually built, before any artifact
// exists — so these controls assert on the FILES as well as the exit code.
func TestScoredColdPlanIsVetoedAgainstTheBuiltPlan(t *testing.T) {
	bin := planBinary(t)

	plan := func(t *testing.T, dir, store string, extra ...string) (string, string, error) {
		t.Helper()
		shard := filepath.Join(dir, "shard-plan.json")
		live := filepath.Join(dir, "live.json")
		writeFixture(t, live, liveSet(8))
		args := append([]string{"plan", "--runner", "vitest", "--count", "1", "--k", "8",
			"--live", live, "--store", store, "--json", "--shard-plan", shard}, extra...)
		cmd := exec.Command(bin, args...)
		var out strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		return shard, out.String(), err
	}

	// EVERY OTHER SCORED ADMISSION INPUT IS SUPPLIED, so the only thing left for
	// a refusal to be about is the cold plan itself. AD-8..AD-10 fire before the
	// phase-2 veto and would otherwise hide it.
	scoredArgs := func(t *testing.T, dir string) []string {
		t.Helper()
		cache := filepath.Join(dir, "cache.json")
		writeFixture(t, cache, map[string]any{
			"dependency_cache_mode":        "disabled",
			"dependency_cache_primary_key": "",
			"transform_cache_mode":         "disabled",
			"dependency_cache_producer":    "none",
			"expected_mongo_binary_sha256": "sha256:" + strings.Repeat("1", 64),
		})
		return []string{"--scored", "--runner-class", "ubuntu-x64", "--runs-on-label", "ubuntu-latest",
			"--candidate-sha", strings.Repeat("2", 40),
			"--workload-commit", strings.Repeat("3", 40),
			"--cache-declaration-file", cache}
	}

	t.Run("NEGATIVE: a present store with no measured unit is a cold plan", func(t *testing.T) {
		dir := t.TempDir()
		store := filepath.Join(dir, "store.json")
		// EVERY STORE-LEVEL TEST PASSES: schema 2, matching flags, parses.
		writeFixture(t, store, map[string]any{
			"schema": 2, "flags": "vitest", "updated_at": "2026-09-01T00:00:00Z",
			"units": map[string]any{},
		})
		shard, out, err := plan(t, dir, store, scoredArgs(t, dir)...)
		if err == nil {
			t.Fatalf("a scored plan over a store with no measurement was admitted:\n%s", out)
		}
		if !strings.Contains(out, "cold") {
			t.Errorf("the refusal does not name the cold plan: %s", out)
		}
		if _, statErr := os.Stat(shard); statErr == nil {
			t.Error("a shard plan was written for a vetoed scored run; no artifact may be left to fan out from")
		}
		if strings.Contains(out, `"include"`) {
			t.Errorf("a matrix was printed for a vetoed scored run:\n%s", out)
		}
	})

	t.Run("NEGATIVE: a store measuring only absent targets is a cold plan", func(t *testing.T) {
		dir := t.TempDir()
		store := filepath.Join(dir, "store.json")
		// Rows exist, but for targets no longer in the live set — so no LIVE
		// target has a measurement and the mean is substituted.
		writeFixture(t, store, map[string]any{
			"schema": 2, "flags": "vitest", "updated_at": "2026-09-01T00:00:00Z",
			"units": map[string]any{
				"gone-a.test.ts": map[string]any{"seconds": 10.0, "samples": 4},
				"gone-b.test.ts": map[string]any{"seconds": 12.0, "samples": 4},
			},
		})
		shard, out, err := plan(t, dir, store, scoredArgs(t, dir)...)
		if err == nil {
			t.Fatalf("a scored plan over a wholly stale store was admitted:\n%s", out)
		}
		if _, statErr := os.Stat(shard); statErr == nil {
			t.Error("a shard plan was written for a vetoed scored run")
		}
	})

	t.Run("POSITIVE: the same store plans fine when NOT scored", func(t *testing.T) {
		// Ordinary cold planning is preserved: the veto is phase 2's, and phase 2
		// only applies to a scored run.
		dir := t.TempDir()
		store := filepath.Join(dir, "store.json")
		writeFixture(t, store, map[string]any{
			"schema": 2, "flags": "vitest", "updated_at": "2026-09-01T00:00:00Z",
			"units": map[string]any{},
		})
		shard, out, err := plan(t, dir, store)
		if err != nil {
			t.Fatalf("an unscored cold plan was refused: %v\n%s", err, out)
		}
		if _, statErr := os.Stat(shard); statErr != nil {
			t.Errorf("an unscored cold plan wrote no shard plan: %v", statErr)
		}
		if !strings.Contains(out, "COLD START") {
			t.Errorf("a cold plan must say so loudly:\n%s", out)
		}
	})

	t.Run("POSITIVE: a warm scored plan is still admitted", func(t *testing.T) {
		// The veto must not refuse a scored run whose store actually measures the
		// live set — otherwise it would make scored planning unreachable.
		dir := t.TempDir()
		store := filepath.Join(dir, "store.json")
		units := map[string]any{}
		for _, p := range liveSet(8) {
			id, _ := p["id"].(string)
			units[id] = map[string]any{"seconds": 10.0, "samples": 4}
		}
		writeFixture(t, store, map[string]any{
			"schema": 2, "flags": "vitest", "updated_at": "2026-09-01T00:00:00Z",
			"units": units,
		})
		shard, out, err := plan(t, dir, store, scoredArgs(t, dir)...)
		if err != nil {
			t.Fatalf("a warm scored plan was refused: %v\n%s", err, out)
		}
		if _, statErr := os.Stat(shard); statErr != nil {
			t.Errorf("a warm scored plan wrote no shard plan: %v", statErr)
		}
	})
}
