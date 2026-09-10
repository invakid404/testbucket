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

// planDocRaw is the shipped shard plan, read as JSON rather than through the
// typed parser, so an ABSENT field is distinguishable from a zero one.
type planDocRaw struct {
	EstBasis string           `json:"est_basis"`
	Summary  map[string]any   `json:"summary"`
	Buckets  []map[string]any `json:"buckets"`
}

func readPlanRaw(t *testing.T, path string) planDocRaw {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc planDocRaw
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse plan: %v\n%s", err, b)
	}
	return doc
}

// TestTheReporterBasisEmitsItsWallShadow is F4(b).
//
// §5.1 makes `wall_est_seconds` an additive shadow, emitted iff the basis is
// reporter AND a fitted model with status ok exists. The field was declared on
// PlanBucket and copied onto matrix rows, and production never assigned it —
// so the B arm of a scored pair shipped the field permanently absent while a
// usable model sat in its store.
func TestTheReporterBasisEmitsItsWallShadow(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	store := filepath.Join(dir, "store.json")
	writeFixture(t, live, liveSet(8))

	cold := warmStore(8)
	cold["schema"] = 2
	writeFixture(t, store, cold)
	key := planComparabilityKey(t, bin, dir, live, store)

	warm := warmStore(8)
	warm["schema"] = 2
	warm["wall"] = map[string]any{
		"model_version":            1,
		"comparability_key_digest": key,
		"status":                   "ok",
		"observations":             []any{},
	}
	for k, v := range fittedWallLeaves() {
		warm["wall"].(map[string]any)[k] = v
	}
	writeFixture(t, store, warm)

	plan := filepath.Join(dir, "plan-reporter.json")
	cmd := exec.Command(bin, "plan", "--runner", "vitest", "--count", "1", "--k", "8",
		"--live", live, "--store", store, "--json", "--shard-plan", plan,
		"--est-basis", "reporter")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("reporter plan failed: %v\n%s", err, stderr.String())
	}

	doc := readPlanRaw(t, plan)
	if doc.EstBasis != "reporter" {
		t.Fatalf("basis is %q, want reporter", doc.EstBasis)
	}
	for i, b := range doc.Buckets {
		if _, ok := b["wall_est_seconds"]; !ok {
			t.Errorf("bucket %d omits wall_est_seconds although an ok fitted model is stored", i)
		}
		// The shadow is ADDITIVE: est_seconds stays the store's measured
		// weights, so the two must be different quantities and both present.
		if _, ok := b["est_seconds"]; !ok {
			t.Errorf("bucket %d lost est_seconds", i)
		}
		if _, ok := b["a_eta_ns"]; ok {
			t.Errorf("bucket %d carries a_eta_ns under the reporter basis; there is no objective to report", i)
		}
	}
}

// TestAColdReporterPlanEmitsNoWallShadow is the other half of §5.1's "iff":
// with no fitted model the field must stay absent rather than become zero.
func TestAColdReporterPlanEmitsNoWallShadow(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	store := filepath.Join(dir, "store.json")
	plan := filepath.Join(dir, "plan-cold.json")
	writeFixture(t, live, liveSet(8))
	writeFixture(t, store, warmStore(8))

	cmd := exec.Command(bin, "plan", "--runner", "vitest", "--count", "1", "--k", "8",
		"--live", live, "--store", store, "--json", "--shard-plan", plan)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("cold reporter plan failed: %v\n%s", err, stderr.String())
	}
	for i, b := range readPlanRaw(t, plan).Buckets {
		if _, ok := b["wall_est_seconds"]; ok {
			t.Errorf("bucket %d carries wall_est_seconds with no fitted model", i)
		}
	}
}

// TestTheWallBasisSummaryIsInWallSeconds is F4(c).
//
// The balance summary read the reporter bucket sums under BOTH bases, so a
// wall plan whose buckets each cost 31.3 s reported ideal, makespan and
// lightest of 23.0 s — the split's balance described in a quantity the split
// was not made from. The banner line likewise called every estimate a serial
// reporter-work sum.
func TestTheWallBasisSummaryIsInWallSeconds(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	store := filepath.Join(dir, "store.json")
	writeFixture(t, live, liveSet(8))

	cold := warmStore(8)
	cold["schema"] = 2
	writeFixture(t, store, cold)
	key := planComparabilityKey(t, bin, dir, live, store)

	warm := warmStore(8)
	warm["schema"] = 2
	warm["wall"] = map[string]any{
		"model_version":            1,
		"comparability_key_digest": key,
		"status":                   "ok",
		"observations":             []any{},
	}
	for k, v := range fittedWallLeaves() {
		warm["wall"].(map[string]any)[k] = v
	}
	// A model with a real fixed cost, so the wall estimate cannot coincide
	// with the reporter sum by accident.
	warm["wall"].(map[string]any)["fixed_ns"] = "9000000000"
	writeFixture(t, store, warm)

	plan := filepath.Join(dir, "plan-wall.json")
	cmd := exec.Command(bin, "plan", "--runner", "vitest", "--count", "1", "--k", "8",
		"--live", live, "--store", store, "--json", "--shard-plan", plan,
		"--est-basis", "wall")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("wall plan failed: %v\n%s", err, stderr.String())
	}

	doc := readPlanRaw(t, plan)
	if doc.EstBasis != "wall" {
		t.Fatalf("basis is %q, want wall", doc.EstBasis)
	}
	maxEst := 0.0
	minEst := 0.0
	for i, b := range doc.Buckets {
		sec, _ := b["est_seconds"].(float64)
		if i == 0 || sec > maxEst {
			maxEst = sec
		}
		if i == 0 || sec < minEst {
			minEst = sec
		}
	}
	makespan, _ := doc.Summary["makespan_seconds"].(float64)
	lightest, _ := doc.Summary["lightest_seconds"].(float64)
	if makespan != maxEst {
		t.Errorf("summary makespan is %vs while the heaviest wall bucket is %vs; the balance is described in the wrong quantity",
			makespan, maxEst)
	}
	if lightest != minEst {
		t.Errorf("summary lightest is %vs while the lightest wall bucket is %vs", lightest, minEst)
	}
	ideal, _ := doc.Summary["ideal_seconds"].(float64)
	if ideal > makespan || ideal < lightest {
		t.Errorf("summary ideal %vs lies outside [%v, %v]; it is not the mean of what was allocated",
			ideal, lightest, makespan)
	}
	// §16.4's execution-model line must not call a model prediction a sum.
	if strings.Contains(stderr.String(), "serial reporter-work sum") {
		t.Errorf("the wall-basis summary describes the estimate as a reporter-work sum:\n%s", stderr.String())
	}
}

// TestAReporterObservationCarriesNoObjective is F4(a).
//
// A_eta is the wall objective and exists only where a plan optimized one. The
// field was unconditional and unconditionally validated, so the assembler
// converted the reporter estimate into nanoseconds and stored it AS the
// objective: a reporter row shipped `"a_eta_ns": "120000000000"` describing a
// prediction nothing had computed.
func TestAReporterObservationCarriesNoObjective(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	records := filepath.Join(dir, "records")
	if err := os.MkdirAll(records, 0o755); err != nil {
		t.Fatal(err)
	}
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
	inner := bin + " wall exec --dir " + records + " --level invocation" +
		" --bucket-id bucket-0 --cwd " + dir + " -- sh -c true"
	run("wall", "exec", "--dir", records, "--level", "script", "--bucket-id", "bucket-0",
		"--cwd", dir, "--", "sh", "-c", inner)
	run("wall", "end", "--dir", records, "--terminal", "passed")

	// writePlanFor emits a reporter-basis plan, whose buckets carry no
	// a_eta_ns — which is the case that used to be fabricated.
	plan, _ := writePlanDeclaring(t, dir, "bucket-0", 0, []string{"sh", "-c", "true"}, dir,
		observedProfileOf(t, bin))
	obsFile := filepath.Join(dir, "observation.json")
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

	var raw map[string]any
	b, err := os.ReadFile(obsFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["a_eta_ns"]; ok {
		t.Errorf("a reporter-basis observation reports a_eta_ns %v; there was no objective to report", v)
	}
	if _, ok := raw["est_seconds"]; !ok {
		t.Error("the observation lost est_seconds, which the reporter basis does display")
	}

	// And the document still qualifies: an absent objective is legal, not a
	// hole the gate happens to tolerate.
	var obs walltime.Observation
	if err := json.Unmarshal(b, &obs); err != nil {
		t.Fatal(err)
	}
	if err := obs.Validate(); err != nil {
		t.Errorf("the assembled reporter observation is invalid: %v", err)
	}
	if obs.AEtaNs != nil {
		t.Errorf("a_eta_ns parsed to %v, want absent", *obs.AEtaNs)
	}
	_ = core.BasisReporter
}
