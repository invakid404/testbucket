package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/walltime"
)

// These are BLACK-BOX tests: they build the shipped binary and run it, because
// the defect they exist for was that `runPlan` validated its flags and then
// called the planner with none of them. Every helper involved had passing
// tests of its own; the production command called none of it. A test that
// invokes a helper cannot see that, and a test that inspects the emitted bytes
// cannot miss it.

// planBinary builds the CLI once per test.
func planBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "testbucket")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/invakid404/testbucket/cmd/testbucket")
	cmd.Dir = filepath.Join("..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func writeFixture(t *testing.T, path string, doc any) {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// liveSet is n whole-file Vitest targets.
func liveSet(n int) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, map[string]any{"id": itoaTest(i) + ".test.ts", "has_tests": true})
	}
	return out
}

func itoaTest(n int) string {
	if n == 0 {
		return "f0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return "f" + string(b)
}

// warmStore is a schema-1 store measuring every target.
func warmStore(n int) map[string]any {
	units := map[string]any{}
	for i := 0; i < n; i++ {
		units[itoaTest(i)+".test.ts"] = map[string]any{"seconds": 10.0 + float64(i), "samples": 4}
	}
	return map[string]any{
		"schema": 1, "flags": "vitest", "updated_at": "2026-09-01T00:00:00Z", "units": units,
	}
}

// TestExplicitWallWithNoModelEmitsNoMatrix is contract §0.8 outcome (c), as the
// shipped command performs it.
//
// The candidate exited 0 here and emitted a reporter matrix with a plan whose
// `est_basis` was the empty string: an explicit wall request was silently
// served as something else. The contract calls that a hard error with no
// matrix, and "no matrix" is a property of the BYTES, so the bytes are what
// this reads.
func TestExplicitWallWithNoModelEmitsNoMatrix(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	writeFixture(t, live, liveSet(2))
	plan := filepath.Join(dir, "plan.json")

	cmd := exec.Command(bin, "plan", "--runner", "vitest", "--count", "1", "--k", "1",
		"--live", live, "--store", filepath.Join(dir, "absent.json"),
		"--est-basis", "wall", "--json", "--shard-plan", plan)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()

	if err == nil {
		t.Fatalf("explicit --est-basis wall with no store exited 0 and emitted:\n%s", stdout.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "" {
		t.Errorf("a refused plan still wrote %d byte(s) of matrix to stdout: %q", len(got), got)
	}
	if _, statErr := os.Stat(plan); statErr == nil {
		t.Error("a refused plan still wrote a shard plan; a caller could fan out over it")
	}
	if !strings.Contains(stderr.String(), "wall model unusable") {
		t.Errorf("the refusal does not name the reason:\n%s", stderr.String())
	}
}

// TestAScoredPlanEmitsEveryPracticalField is the positive half.
//
// Every field below is registered in the field registry as a plan artifact
// leaf. The candidate emitted them empty or absent, which meant an observation
// had nothing to copy and QC12/QC13 had nothing to compare.
func TestAScoredPlanEmitsEveryPracticalField(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	store := filepath.Join(dir, "store.json")
	cache := filepath.Join(dir, "cache.json")
	plan := filepath.Join(dir, "plan.json")
	writeFixture(t, live, liveSet(8))
	writeFixture(t, store, warmStore(8))
	writeFixture(t, cache, map[string]any{
		"dependency_cache_mode":        "disabled",
		"dependency_cache_primary_key": "",
		"transform_cache_mode":         "disabled",
		"dependency_cache_producer":    "none",
		"expected_mongo_binary_sha256": "sha256:" + strings.Repeat("1", 64),
	})

	cmd := exec.Command(bin, "plan", "--runner", "vitest", "--count", "1", "--k", "8",
		"--live", live, "--store", store, "--json", "--shard-plan", plan,
		"--scored", "--runner-class", "ubuntu-x64", "--runs-on-label", "ubuntu-latest",
		"--candidate-sha", strings.Repeat("d", 40),
		"--workload-commit", "d9ae1d433bb45012c04d567879b66fc4bf6112c6",
		"--cache-declaration-file", cache)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("scored plan failed: %v\n%s", err, stderr.String())
	}

	var doc map[string]any
	b, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"est_basis", "comparability_key_digest", "expanded_unit_set_digest",
		"runtime_profile_declared_digest",
	} {
		if s, _ := doc[field].(string); strings.TrimSpace(s) == "" {
			t.Errorf("plan.%s is empty; the registry declares it a plan leaf", field)
		}
	}
	if doc["profile"] == nil {
		t.Error("plan.profile is absent; an observation has nothing to copy verbatim")
	} else {
		prof, _ := doc["profile"].(map[string]any)
		if scored, _ := prof["scored"].(bool); !scored {
			t.Error("plan.profile does not record that this plan is scored")
		}
		if k, _ := prof["k"].(float64); int(k) != 8 {
			t.Errorf("plan.profile.k = %v, want the K this plan was built at", prof["k"])
		}
	}
	rp, _ := doc["runtime_profile_declared"].(map[string]any)
	if len(rp) == 0 {
		t.Fatal("plan.runtime_profile_declared is absent")
	}
	// The one leaf this process can always answer for is its own binary: the
	// code that decided the plan is on disk in front of it.
	if s, _ := rp["testbucket_sha256"].(string); !strings.HasPrefix(s, "sha256:") {
		t.Errorf("runtime_profile_declared.testbucket_sha256 = %q, want the planning binary's digest", s)
	}
	if s, _ := rp["dependency_cache_mode"].(string); s != "disabled" {
		t.Errorf("runtime_profile_declared.dependency_cache_mode = %q, want the declared mode", s)
	}
}

// TestAMalformedCacheDeclarationRefusesBeforeAnyMatrix: AD-9 requires a scored
// plan to carry a declaration, and §10.5.0 fixes its leaves. The candidate
// accepted a declaration whose entire content was `{}`.
func TestAMalformedCacheDeclarationRefusesBeforeAnyMatrix(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	store := filepath.Join(dir, "store.json")
	cache := filepath.Join(dir, "cache.json")
	plan := filepath.Join(dir, "plan.json")
	writeFixture(t, live, liveSet(8))
	writeFixture(t, store, warmStore(8))
	if err := os.WriteFile(cache, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "plan", "--runner", "vitest", "--count", "1", "--k", "8",
		"--live", live, "--store", store, "--json", "--shard-plan", plan,
		"--scored", "--runner-class", "ubuntu-x64", "--runs-on-label", "ubuntu-latest",
		"--candidate-sha", strings.Repeat("d", 40),
		"--workload-commit", "d9ae1d433bb45012c04d567879b66fc4bf6112c6",
		"--cache-declaration-file", cache)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err == nil {
		t.Fatalf("an empty cache declaration was accepted and emitted:\n%s", stdout.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "" {
		t.Errorf("a refused plan still wrote a matrix: %q", got)
	}
	if _, statErr := os.Stat(plan); statErr == nil {
		t.Error("a refused plan still wrote a shard plan")
	}
	if !strings.Contains(stderr.String(), "cache declaration") {
		t.Errorf("the refusal does not name the declaration:\n%s", stderr.String())
	}
}

// TestTheWallBasisPacksAndDisplaysOneObjective: §0.9 makes A_eta_ns both the
// optimized and the displayed quantity — one expression, evaluated once. A
// plan that packed by one number and displayed another would let a reader
// compare an estimate against a balance it was never part of.
func TestTheWallBasisPacksAndDisplaysOneObjective(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	store := filepath.Join(dir, "store.json")
	plan := filepath.Join(dir, "plan.json")
	writeFixture(t, live, liveSet(8))

	st := warmStore(8)
	st["schema"] = 2
	writeFixture(t, store, st)
	// WARMED UNDER THIS RUN'S OWN KEY. The fixture used to name a constant
	// `sha256:aaaa…`, which the planner now correctly refuses as a foreign
	// population — so the key is derived from a reporter plan over the same
	// configuration rather than asserted. This test is about the objective,
	// not about the key.
	key := planComparabilityKey(t, bin, dir, live, store)
	// The REGISTERED wire format: the fit's leaves are flat under `wall`, and
	// `scale` is a string. This fixture used to nest them under `wall.fit`
	// with a numeric scale, which is the shape the field registry does not
	// declare — a store written that way is one no consumer reading the
	// registered paths can find.
	st["wall"] = map[string]any{
		"model_version":                1,
		"comparability_key_digest":     key,
		"status":                       "ok",
		"observations":                 []any{},
		"fixed_ns":                     "2000000000",
		"scale":                        "1.25",
		"whole_invocation_overhead_ns": "500000000",
		"per_slice_overhead_ns":        "100000000",
		"fitted_at":                    "2026-09-01T00:00:00Z",
		"rows_used":                    40,
		"runs_used":                    5,
		"residual_mae_ns":              "1000000",
		"residual_p90_ns":              "2000000",
		"rank_support":                 []string{"fixed", "scale", "whole", "slice"},
	}
	writeFixture(t, store, st)

	cmd := exec.Command(bin, "plan", "--runner", "vitest", "--count", "1", "--k", "8",
		"--live", live, "--store", store, "--est-basis", "wall",
		"--json", "--shard-plan", plan)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("wall plan with a fitted model failed: %v\n%s", err, stderr.String())
	}

	var doc struct {
		EstBasis string `json:"est_basis"`
		Buckets  []struct {
			Index   int     `json:"bucket"`
			Seconds float64 `json:"est_seconds"`
			AEtaNs  *string `json:"a_eta_ns"`
		} `json:"buckets"`
	}
	b, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.EstBasis != "wall" {
		t.Errorf("est_basis = %q, want wall", doc.EstBasis)
	}
	if len(doc.Buckets) != 8 {
		t.Fatalf("the plan has %d buckets, want 8", len(doc.Buckets))
	}
	for _, bk := range doc.Buckets {
		if bk.AEtaNs == nil {
			t.Errorf("bucket %d carries no a_eta_ns under the wall basis", bk.Index)
			continue
		}
		var ns int64
		if _, err := fmt.Sscan(*bk.AEtaNs, &ns); err != nil {
			t.Errorf("bucket %d a_eta_ns %q is not an integer: %v", bk.Index, *bk.AEtaNs, err)
			continue
		}
		// round_half_up(ns / 1e9) to one decimal, in the integer domain.
		tenths := (ns + 50_000_000) / 100_000_000
		if want := float64(tenths) / 10; want != bk.Seconds {
			t.Errorf("bucket %d displays %v s but its objective is %d ns (want %v s)",
				bk.Index, bk.Seconds, ns, want)
		}
	}
}

// TestThePlanArtifactCarriesTheCanonicalProfileVerbatim is the byte path QC13
// depends on, taken over the SHIPPED planner's own output.
//
// The planner wrote the shard plan with an indenting encoder. json.Indent
// reformats embedded raw JSON, so the artifact no longer held §13.0's
// canonical block at all: the assembler copied the block it was given, wrote
// it compactly as a canonical value must be written, and ingest rejected the
// row with "observation profile block is not the plan's block verbatim". Every
// unit test passed, because every fixture was already compact.
func TestThePlanArtifactCarriesTheCanonicalProfileVerbatim(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	store := filepath.Join(dir, "store.json")
	plan := filepath.Join(dir, "plan.json")
	writeFixture(t, live, liveSet(8))
	writeFixture(t, store, warmStore(8))

	cmd := exec.Command(bin, "plan", "--runner", "vitest", "--count", "1", "--k", "8",
		"--live", live, "--store", store, "--json", "--shard-plan", plan)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("plan failed: %v\n%s", err, stderr.String())
	}

	doc, err := core.ParseShardPlan(plan)
	if err != nil {
		t.Fatalf("ParseShardPlan: %v", err)
	}
	if len(doc.Profile) == 0 {
		t.Fatal("the plan artifact carries no profile block")
	}
	// The bytes on disk must BE the canonical serialization of their own
	// values — which is what an observation copies and QC13 compares.
	var cp walltime.CanonicalProfile
	if err := json.Unmarshal(doc.Profile, &cp); err != nil {
		t.Fatalf("the profile block does not parse: %v\n%s", err, doc.Profile)
	}
	want, err := walltime.NewProfileBlock(cp)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal([]byte(doc.Profile), want.Raw()) {
		t.Errorf("the plan artifact's profile block is not canonical, so no observation can copy it verbatim:\n  on disk: %s\n  canonical: %s",
			doc.Profile, want.Raw())
	}
}

// TestAForeignWallModelIsRefusedBeforeItCanAllocate is F3.
//
// The planner loaded the store's status and fit, called SelectBasis and
// installed that fit — and only afterwards constructed the current §15.3
// comparability key, which it then never compared against the store's. The
// key-change reset existed only in ingest, so a store fitted under a different
// runner, toolchain, cache declaration or setup command drove THIS allocation
// once and was cleared on the NEXT ingest. Explicit `--est-basis wall` exited 0
// and emitted a wall-basis plan from a foreign fit.
//
// Measurements from another population are not a warm start.
func TestAForeignWallModelIsRefusedBeforeItCanAllocate(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	store := filepath.Join(dir, "store.json")
	plan := filepath.Join(dir, "plan.json")
	writeFixture(t, live, liveSet(8))

	warm := warmStore(8)
	warm["wall"] = map[string]any{
		"model_version":            1,
		"comparability_key_digest": "sha256:" + strings.Repeat("a", 64),
		"status":                   "ok",
		"observations":             []any{},
	}
	// THE REGISTERED WIRE FORMAT IS FLAT: the registry declares fixed_ns,
	// scale and fitted_at directly under `wall`, with scale as a STRING. A
	// nested `fit` object unmarshals to no fit at all, which would make this
	// test pass for the wrong reason.
	for k, v := range fittedWallLeaves() {
		warm["wall"].(map[string]any)[k] = v
	}
	writeFixture(t, store, warm)

	// EXPLICIT wall basis. §0.8 outcome (c) is a hard error with no matrix when
	// no usable model exists — and a model measured under another key is not a
	// usable model, so this must fail rather than silently allocating from it.
	cmd := exec.Command(bin, "plan", "--runner", "vitest", "--count", "1", "--k", "8",
		"--live", live, "--store", store, "--json", "--shard-plan", plan,
		"--est-basis", "wall", "--runs-on-label", "ubuntu-latest")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("a foreign fit produced a wall-basis plan and exited 0:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "comparability key") {
		t.Errorf("the refusal does not name the key that made the model foreign:\n%s", stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "" {
		t.Errorf("a refused plan emitted matrix bytes:\n%s", stdout.String())
	}
	if _, statErr := os.Stat(plan); statErr == nil {
		t.Error("a refused plan wrote a shard-plan artifact")
	}
}

// TestAMatchingComparabilityKeyStillWarms is the control: the fix must refuse
// a FOREIGN population, not every stored model.
func TestAMatchingComparabilityKeyStillWarms(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	store := filepath.Join(dir, "store.json")
	writeFixture(t, live, liveSet(8))
	writeFixture(t, store, warmStore(8))

	// The key this configuration computes is not knowable to the test without
	// re-deriving it, so the store is warmed in two steps: plan once under the
	// reporter basis to learn the key the run reports, then write it back.
	key := planComparabilityKey(t, bin, dir, live, store, "--runs-on-label", "ubuntu-latest")
	warm := warmStore(8)
	warm["wall"] = map[string]any{
		"model_version":            1,
		"comparability_key_digest": key,
		"status":                   "ok",
		"observations":             []any{},
	}
	// THE REGISTERED WIRE FORMAT IS FLAT: the registry declares fixed_ns,
	// scale and fitted_at directly under `wall`, with scale as a STRING. A
	// nested `fit` object unmarshals to no fit at all, which would make this
	// test pass for the wrong reason.
	for k, v := range fittedWallLeaves() {
		warm["wall"].(map[string]any)[k] = v
	}
	writeFixture(t, store, warm)

	plan := filepath.Join(dir, "plan-warm.json")
	cmd := exec.Command(bin, "plan", "--runner", "vitest", "--count", "1", "--k", "8",
		"--live", live, "--store", store, "--json", "--shard-plan", plan,
		"--est-basis", "wall", "--runs-on-label", "ubuntu-latest")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("a model measured under THIS key was refused: %v\n%s", err, stderr.String())
	}
	doc, err := core.ParseShardPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if doc.EstBasis != core.BasisWall {
		t.Errorf("the plan's basis is %q, want wall", doc.EstBasis)
	}
}

// planComparabilityKey runs the planner once and reads back the key it
// computed for this exact configuration.
func planComparabilityKey(t *testing.T, bin, dir, live, store string, extra ...string) string {
	t.Helper()
	plan := filepath.Join(dir, "plan-key.json")
	args := append([]string{"plan", "--runner", "vitest", "--count", "1", "--k", "8",
		"--live", live, "--store", store, "--json", "--shard-plan", plan}, extra...)
	cmd := exec.Command(bin, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("reporter plan failed: %v\n%s", err, stderr.String())
	}
	doc, err := core.ParseShardPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if doc.ComparabilityKeyDigest == "" {
		t.Fatal("the plan carries no comparability key")
	}
	return doc.ComparabilityKeyDigest
}

// fittedWallLeaves is an accepted §15.1c fit on the registered flat surface.
func fittedWallLeaves() map[string]any {
	return map[string]any{
		"fitted_at":                    "2026-09-01T00:00:00Z",
		"rows_used":                    24,
		"runs_used":                    6,
		"fixed_ns":                     "1000000000",
		"scale":                        "1",
		"whole_invocation_overhead_ns": "0",
		"per_slice_overhead_ns":        "0",
		"residual_mae_ns":              "0",
		"residual_p90_ns":              "0",
		"rank_support":                 []string{"fixed", "scale"},
	}
}
