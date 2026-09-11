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

// degradedWallLeaves is a fit whose residuals exceeded the ceiling: it HAS
// coefficients, and that is the point.
func degradedWallLeaves(key string) map[string]any {
	w := map[string]any{
		"model_version":            1,
		"comparability_key_digest": key,
		"status":                   "degraded",
		"failure_subtype":          "mae_ceiling_exceeded",
		"observations":             []any{},
	}
	for k, v := range fittedWallLeaves() {
		w[k] = v
	}
	return w
}

// TestADegradedStoreSurvivesTheSaveReloadCycle is F2.
//
// The ingest adapter cleared `wall.fit` for every status but `ok`, while
// §15.1c's presence matrix requires the group for `ok` AND `degraded`. A
// degraded fit therefore wrote a store that failed its own validation on the
// next load — and the failure surfaced as a COLD START, one run later, where
// nothing points at the write that caused it.
//
// A degraded model is one whose residuals exceeded the ceiling, not one with
// no coefficients. The numbers are what make the degradation diagnosable.
func TestADegradedStoreSurvivesTheSaveReloadCycle(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	store := filepath.Join(dir, "store.json")
	writeFixture(t, live, liveSet(8))

	cold := warmStore(8)
	cold["schema"] = 2
	writeFixture(t, store, cold)
	key := planComparabilityKey(t, bin, dir, live, store)

	degraded := warmStore(8)
	degraded["schema"] = 2
	degraded["wall"] = degradedWallLeaves(key)
	writeFixture(t, store, degraded)

	// THE SHIPPED LOADER, not a unit call: the defect was that this exact
	// path reported a cold start.
	st, reason, err := core.LoadStore(store)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if reason != "" {
		t.Fatalf("a degraded store was refused on load: %s", reason)
	}
	if st == nil || st.Wall == nil {
		t.Fatal("the degraded store loaded with no wall object")
	}
	if st.Wall.Status != core.WallStatusDegraded {
		t.Errorf("status is %q, want degraded", st.Wall.Status)
	}
	if st.Wall.Fit == nil {
		t.Fatal("the degraded fit lost its coefficients; §15.1c requires the group for ok AND degraded")
	}
	if st.Wall.Fit.FixedNs != 1_000_000_000 {
		t.Errorf("fixed_ns is %d, want the stored 1000000000", st.Wall.Fit.FixedNs)
	}

	// And it round-trips: save what was loaded, load it again.
	again := filepath.Join(dir, "store-again.json")
	if err := st.Save(again); err != nil {
		t.Fatalf("Save: %v", err)
	}
	st2, reason2, err := core.LoadStore(again)
	if err != nil || reason2 != "" {
		t.Fatalf("the saved degraded store did not reload: %v / %s", err, reason2)
	}
	if st2.Wall == nil || st2.Wall.Fit == nil || st2.Wall.Status != core.WallStatusDegraded {
		t.Error("the round trip lost the degraded status or its coefficients")
	}
}

// TestADegradedFitEmitsNoWallShadow is F3, problem 2.
//
// §5.1 permits `wall_est_seconds` for reporter basis AND status ok. The
// planner built a model from any non-nil fit group, so a degraded fit — which
// HAS coefficients, by §15.1c — produced the shadow from a model whose
// residuals had already exceeded the ceiling.
func TestADegradedFitEmitsNoWallShadow(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	store := filepath.Join(dir, "store.json")
	writeFixture(t, live, liveSet(8))

	cold := warmStore(8)
	cold["schema"] = 2
	writeFixture(t, store, cold)
	key := planComparabilityKey(t, bin, dir, live, store)

	degraded := warmStore(8)
	degraded["schema"] = 2
	degraded["wall"] = degradedWallLeaves(key)
	writeFixture(t, store, degraded)

	plan := filepath.Join(dir, "plan-degraded.json")
	cmd := exec.Command(bin, "plan", "--runner", "vitest", "--count", "1", "--k", "8",
		"--live", live, "--store", store, "--json", "--shard-plan", plan,
		"--est-basis", "reporter")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("reporter plan over a degraded store failed: %v\n%s", err, stderr.String())
	}
	for i, b := range readPlanRaw(t, plan).Buckets {
		if _, ok := b["wall_est_seconds"]; ok {
			t.Errorf("bucket %d carries wall_est_seconds under a DEGRADED fit; §5.1 permits it only at status ok", i)
		}
	}
}

// TestTheWallSummaryTakesTheMeanInIntegerNanoseconds is F3, problem 1.
//
// The summary summed the ALREADY-ROUNDED per-bucket seconds, so the mean
// carried one display rounding per bucket before the division. §0.9 fixes
// A_eta in the exact integer domain precisely so a display rounding never
// becomes an input to another number.
func TestTheWallSummaryTakesTheMeanInIntegerNanoseconds(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	live := filepath.Join(dir, "live.json")
	store := filepath.Join(dir, "store.json")
	// Two targets, two buckets: the smallest shape where a per-bucket rounding
	// can move the mean.
	writeFixture(t, live, liveSet(2))

	cold := map[string]any{
		"schema": 2, "flags": "vitest", "updated_at": "2026-09-01T00:00:00Z",
		"units": map[string]any{
			"f0.test.ts": map[string]any{"seconds": 0.06, "samples": 4},
			"f1.test.ts": map[string]any{"seconds": 0.04, "samples": 4},
		},
	}
	writeFixture(t, store, cold)
	key := planComparabilityKey(t, bin, dir, live, store, "--k", "2")

	// fixed_ns 100ms, scale 1: the two buckets cost 160ms and 140ms, which
	// round to 0.2 and 0.1 — summing THOSE gives a mean of 0.15 → 0.1, while
	// the objective's own mean is 150ms → 0.2.
	warm := map[string]any{}
	for k, v := range cold {
		warm[k] = v
	}
	wall := map[string]any{
		"model_version":            1,
		"comparability_key_digest": key,
		"status":                   "ok",
		// A RING THAT SUPPORTS THE FIT BESIDE IT — see supportingRing.
		"observations": supportingRing(key),
	}
	for k, v := range fittedWallLeaves() {
		wall[k] = v
	}
	wall["fixed_ns"] = "100000000"
	wall["scale"] = "1"
	warm["wall"] = wall
	writeFixture(t, store, warm)

	plan := filepath.Join(dir, "plan-wall.json")
	cmd := exec.Command(bin, "plan", "--runner", "vitest", "--count", "1", "--k", "2",
		"--live", live, "--store", store, "--json", "--shard-plan", plan,
		"--est-basis", "wall")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("wall plan failed: %v\n%s", err, stderr.String())
	}

	doc := readPlanRaw(t, plan)
	var sumNs int64
	for _, b := range doc.Buckets {
		raw, _ := b["a_eta_ns"].(string)
		var ns int64
		if _, err := fmtSscan(raw, &ns); err != nil {
			t.Fatalf("bucket a_eta_ns %q: %v", raw, err)
		}
		sumNs += ns
	}
	if len(doc.Buckets) == 0 || sumNs == 0 {
		t.Fatal("the wall plan reported no objective to take a mean of")
	}
	// The mean, taken the contract's way: integer nanoseconds, then displayed.
	want := round1(float64(sumNs/int64(len(doc.Buckets))) / 1e9)
	got, _ := doc.Summary["ideal_seconds"].(float64)
	if got != want {
		t.Errorf("summary ideal_seconds is %v; the mean of the integer objectives (%d ns over %d buckets) displays as %v",
			got, sumNs, len(doc.Buckets), want)
	}
}

// round1 is the display rounding, half away from zero, as §5.1 spells it.
func round1(v float64) float64 {
	scaled := v * 10
	if scaled >= 0 {
		return float64(int64(scaled+0.5)) / 10
	}
	return float64(int64(scaled-0.5)) / 10
}

// fmtSscan parses one decimal integer, kept separate so the test above reads
// as arithmetic rather than as parsing.
func fmtSscan(s string, out *int64) (int, error) {
	var n int64
	var neg bool
	i := 0
	if strings.HasPrefix(s, "-") {
		neg, i = true, 1
	}
	if i >= len(s) {
		return 0, os.ErrInvalid
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int64(s[i]-'0')
	}
	if neg {
		n = -n
	}
	*out = n
	return 1, nil
}

// TestTheComparabilityKeyHashesTheSetupCommandVerbatim is F6.
//
// The key was computed over strings.TrimSpace(setupCommand), so two
// provisioning commands that differ only in surrounding whitespace shared one
// wall history — and they can build different environments. §15.3 and the
// flag both say verbatim.
func TestTheComparabilityKeyHashesTheSetupCommandVerbatim(t *testing.T) {
	bin := planBinary(t)
	base := "printf x"
	variants := map[string]string{
		"bare":               base,
		"leading ascii":      "  " + base,
		"trailing ascii":     base + "  ",
		"leading newline":    "\n" + base,
		"trailing newline":   base + "\n",
		"leading unicode":    " " + base,
		"internal different": "printf  x",
	}
	seen := map[string]string{}
	for name, cmd := range variants {
		dir := t.TempDir()
		live := filepath.Join(dir, "live.json")
		store := filepath.Join(dir, "store.json")
		writeFixture(t, live, liveSet(2))
		writeFixture(t, store, warmStore(2))
		key := planComparabilityKey(t, bin, dir, live, store, "--k", "2", "--setup-command", cmd)
		if prev, clash := seen[key]; clash {
			t.Errorf("%q and %q produce the same comparability key; the setup command is hashed verbatim",
				prev, name)
		}
		seen[key] = name
	}
	if len(seen) != len(variants) {
		t.Errorf("%d distinct keys for %d distinct setup commands", len(seen), len(variants))
	}
}

// TestTheStoreRejectsAnUnknownWallStatus is F4, the store half.
//
// Every branch keyed on `== ok` and treated any other string as a generic
// failure, so `unknown-status` loaded as a valid non-ok wall object: subtype
// and fit presence were being decided from a status the reader could not name.
func TestTheStoreRejectsAnUnknownWallStatus(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "store.json")
	bad := warmStore(2)
	bad["schema"] = 2
	bad["wall"] = map[string]any{
		"model_version":            1,
		"comparability_key_digest": "sha256:" + strings.Repeat("a", 64),
		"status":                   "unknown-status",
		"failure_subtype":          "rows_below_minimum",
		"observations":             []any{},
	}
	writeFixture(t, store, bad)

	st, reason, err := core.LoadStore(store)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if reason == "" {
		t.Fatal("a wall object with an unnameable status loaded as usable")
	}
	if !strings.Contains(reason, "vocabulary") {
		t.Errorf("the refusal does not name the closed vocabulary: %s", reason)
	}
	if st != nil {
		t.Error("an unusable store was returned rather than refused")
	}
	// The three §15.1c spells still load.
	for _, ok := range []string{"ok", "degraded", "insufficient"} {
		t.Run("status "+ok+" is accepted", func(t *testing.T) {
			good := warmStore(2)
			good["schema"] = 2
			w := map[string]any{
				"model_version":            1,
				"comparability_key_digest": "sha256:" + strings.Repeat("a", 64),
				"status":                   ok,
				"observations":             []any{},
			}
			if ok != "ok" {
				w["failure_subtype"] = "mae_ceiling_exceeded"
			}
			if ok != "insufficient" {
				for k, v := range fittedWallLeaves() {
					w[k] = v
				}
			}
			good["wall"] = w
			p := filepath.Join(t.TempDir(), "store.json")
			writeFixture(t, p, good)
			if _, reason, err := core.LoadStore(p); err != nil || reason != "" {
				t.Errorf("status %q was refused: %v / %s", ok, err, reason)
			}
		})
	}
}

// TestTheAssemblerRefusesAnIncompleteClosingRecord is F4, the assembler half.
//
// An absent terminal was converted to "passed" — the most consequential
// default in the document, because QC9 admits a row for training on exactly
// that value. A closing record that never said how the interval ended became a
// trainable measurement of a run nobody could describe.
func TestTheAssemblerRefusesAnIncompleteClosingRecord(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	records := filepath.Join(dir, "records")
	if err := os.MkdirAll(records, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		var out strings.Builder
		cmd.Stderr, cmd.Stdout = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
	}
	run("wall", "begin", "--dir", records, "--bucket-id", "bucket-0")
	inner := bin + " wall exec --dir " + records + " --level invocation" +
		" --bucket-id bucket-0 --cwd " + dir +
		" --selector ./f0.test.ts --unit-digest " + string(walltime.DigestJSONOrEmpty([]string{"f0.test.ts"})) +
		" -- sh -c true"
	run("wall", "exec", "--dir", records, "--level", "script", "--bucket-id", "bucket-0",
		"--cwd", dir, "--", "sh", "-c", inner)
	// The closing record with NO terminal: `wall end` is given an empty one.
	run("wall", "end", "--dir", records, "--terminal", "")

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
	var out strings.Builder
	cmd.Stderr, cmd.Stdout = &out, &out
	err := cmd.Run()

	if err == nil {
		b, readErr := os.ReadFile(obsFile)
		if readErr == nil {
			var obs map[string]any
			_ = json.Unmarshal(b, &obs)
			t.Fatalf("an incomplete closing record assembled an observation with terminal %v", obs["terminal"])
		}
		t.Fatal("assemble-observation exited 0 on an incomplete closing record")
	}
	if _, statErr := os.Stat(obsFile); statErr == nil {
		t.Error("a refused assembly wrote an observation anyway")
	}

	// TWO LAYERS REFUSE IT, and the verifier gets there first.
	//
	// The assembler's own guard is the backstop; the verifier is what decides
	// whether the records are a complete measurement at all, and an absent
	// terminal now makes them incomplete. Asserting only the assembler's
	// wording would break the moment the verifier improved — so this asserts
	// the refusal happened AND that the verifier names the reason.
	if !strings.Contains(out.String(), "not a complete measurement") &&
		!strings.Contains(out.String(), "terminal") {
		t.Errorf("the refusal names neither the incomplete envelope nor the missing terminal:\n%s", out.String())
	}

	verify := exec.Command(bin, "wall", "verify", "--dir", records)
	var vout strings.Builder
	verify.Stdout, verify.Stderr = &vout, &vout
	if verify.Run() == nil {
		t.Error("wall verify accepted a stream whose closing record carries no terminal state")
	}
	if !strings.Contains(vout.String(), "no terminal state") {
		t.Errorf("the verifier does not name the missing terminal:\n%s", vout.String())
	}
}

// TestTheVerifierRejectsAnAbsentSchema is the other half of F4's verify fix.
//
// The schema check refused a record only when one was PRESENT and wrong, so a
// record carrying none went through silently — the discriminant failed open on
// exactly the input it cannot interpret. A reader that does not know which
// epoch a record belongs to knows less than one holding a wrong version.
func TestTheVerifierRejectsAnAbsentSchema(t *testing.T) {
	dir := t.TempDir()
	records := filepath.Join(dir, "records")
	if err := os.MkdirAll(records, 0o755); err != nil {
		t.Fatal(err)
	}
	// A hand-built stream: a start and an end boundary with NO schema and NO
	// terminal. This is the shape the discriminants used to wave through.
	lines := []string{
		`{"seq":0,"kind":"boundary","level":"action","boundary":"start","producer":"physical_wrapper","producer_id":"physical","source":"wrapper_annotation","run":{"bucket_id":"b1","run_id":"run-1"},"instant":{"clock_id":"CLOCK_MONOTONIC","mono_ns":"1000","realtime":"2026-09-01T00:00:00Z/2026-09-01T00:00:00Z","boot_id":"boot-1"}}`,
		`{"seq":1,"kind":"boundary","level":"action","boundary":"end","producer":"physical_wrapper","producer_id":"physical","source":"wrapper_annotation","run":{"bucket_id":"b1","run_id":"run-1"},"instant":{"clock_id":"CLOCK_MONOTONIC","mono_ns":"2000","realtime":"2026-09-01T00:00:01Z/2026-09-01T00:00:01Z","boot_id":"boot-1"}}`,
	}
	if err := os.WriteFile(filepath.Join(records, "physical-action-00.jsonl"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := planBinary(t)
	cmd := exec.Command(bin, "wall", "verify", "--dir", records)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if cmd.Run() == nil {
		t.Fatalf("a stream with no schema and no terminal verified clean:\n%s", out.String())
	}
	text := out.String()
	if !strings.Contains(text, "carries no schema") {
		t.Errorf("the verifier does not name the absent schema:\n%s", text)
	}
	if !strings.Contains(text, "no terminal state") {
		t.Errorf("the verifier does not name the absent terminal:\n%s", text)
	}
}
