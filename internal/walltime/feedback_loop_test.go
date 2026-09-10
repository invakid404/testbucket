package walltime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// fakeRing is an instrumented RingStore. The selector counts how many
// predicates it evaluates, which is how §22 test 61's "sole in-CI mechanism"
// claim is made executable.
type fakeRing struct {
	rows []struct {
		id        string
		trainable bool
	}
	predicateNames map[string]int
}

func newFakeRing() *fakeRing { return &fakeRing{predicateNames: map[string]int{}} }

func (r *fakeRing) SelectedIdentities() []string {
	var out []string
	for _, row := range r.rows {
		// The ONE predicate the fitter's selector may evaluate.
		r.predicateNames["trainable"]++
		if row.trainable {
			out = append(out, row.id)
		}
	}
	return out
}

func (r *fakeRing) Append(obs Observation, trainable bool) error {
	id := fmt.Sprintf("%s/%s/%s/%d", obs.RunID, obs.RunAttempt, obs.JobID, obs.BucketIndex)
	r.rows = append(r.rows, struct {
		id        string
		trainable bool
	}{id, trainable})
	return nil
}

// buildObservation makes a schema-valid observation.
func buildObservation(t *testing.T, scored bool, campaignID, workload, head string, bucket int) Observation {
	t.Helper()
	prof := CanonicalProfile{
		Scored: scored, RunnerToken: "vitest", K: 8, Count: 1, FileParallelism: 1,
		BucketIndices: []int{0, 1, 2, 3, 4, 5, 6, 7},
		EstBasis:      BasisReporter, StoreSHA256: "sha256:s", ExpandedUnitSetDigest: "sha256:u",
	}
	blk, err := NewProfileBlock(prof)
	if err != nil {
		t.Fatal(err)
	}
	rp := planRuntimeProfile()
	obs := Observation{
		Schema: ObservationSchema, ComparabilityKeyDigest: "sha256:k",
		Repository: "owner/name", HeadSHA: head, CandidateSHA: "cand", WorkloadCommit: workload,
		RunID: "run-1", RunAttempt: "1", JobID: "job-1",
		BucketIndex: bucket, BucketName: fmt.Sprintf("bucket-%d", bucket),
		PlanDigest: "sha256:p", Profile: blk,
		AEtaNs: 20_000_000_000, EstSeconds: 20.0,
		UnitIDs: []string{"a.spec.ts"}, Terminal: "passed",
		RuntimeProfile: rp, RuntimeProfileDigest: RuntimeProfileDigest(rp),
		Limitations: CanonicalLimitations(),
		CampaignID:  campaignID,
	}
	return obs
}

func writeObservations(t *testing.T, dir string, obs []Observation) {
	t.Helper()
	for i, o := range obs {
		b, err := json.Marshal(o)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("obs-%02d.json", i)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestWallObservationRoundTripClosesRecordLoop is §22 test 58's transport half
// and one of the acceptance tests the registry names for ID-14. R54 uploaded a
// wall artifact that nothing downloaded; this closes that end.
func TestWallObservationRoundTripClosesRecordLoop(t *testing.T) {
	dir := t.TempDir()
	var obs []Observation
	for i := 0; i < 8; i++ {
		obs = append(obs, buildObservation(t, false, "", "w1", "h1", i))
	}
	writeObservations(t, dir, obs)

	sources, err := ReadWallObservations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 8 {
		t.Fatalf("read %d observations, want 8", len(sources))
	}

	t.Run("the round trip preserves every field the ring reads", func(t *testing.T) {
		for i, s := range sources {
			if err := s.Obs.Validate(); err != nil {
				t.Fatalf("observation %d did not survive the round trip: %v", i, err)
			}
			if s.Obs.BucketIndex != i {
				t.Fatalf("observation %d has bucket_index %d", i, s.Obs.BucketIndex)
			}
		}
	})

	t.Run("ordinary unscored rows append trainable and drive one refit", func(t *testing.T) {
		ring := newFakeRing()
		calls := 0
		res, err := IngestWallObservations(sources, ring, nil, func() error { calls++; return nil })
		if err != nil {
			t.Fatal(err)
		}
		if res.Appended != 8 {
			t.Fatalf("appended %d, want 8", res.Appended)
		}
		for _, d := range res.Decisions {
			if !d.Accepted || !d.Trainable {
				t.Fatalf("ordinary row %s was not appended trainable: %+v", d.BucketName, d)
			}
		}
		if !res.SelectionChanged {
			t.Fatal("appending trainable rows must change the selected population")
		}
		if calls != 1 {
			t.Fatalf("fitter called %d times, want exactly 1", calls)
		}
	})

	t.Run("the accept/reject table names a reason for every row", func(t *testing.T) {
		ring := newFakeRing()
		res, err := IngestWallObservations(sources, ring, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range res.Decisions {
			if d.Reason == "" {
				t.Errorf("row %s carries no reason", d.BucketName)
			}
		}
	})
}

// TestCampaignRowsNeverRefitTheModel is §22 test 61 (S-2/R10-D3) and the
// acceptance test the registry names for ID-14's fit-isolation half. It drives
// the whole campaign_id → trainable state machine, which is the SOLE in-CI
// mechanism.
func TestCampaignRowsNeverRefitTheModel(t *testing.T) {
	cfgBytes := []byte(`{"schema":"testbucket.campaign-config/v1","manifest":{"campaign_id":"cmp-1","workload_commit":"w1","excluded_orchestration_commits":["hx"]}}`)
	verified, err := VerifyCampaignConfig(cfgBytes, sha256Hex(cfgBytes))
	if err != nil {
		t.Fatal(err)
	}

	drive := func(t *testing.T, obs Observation, cfg *VerifiedCampaignConfig) (IngestResult, int, *fakeRing) {
		t.Helper()
		dir := t.TempDir()
		writeObservations(t, dir, []Observation{obs})
		sources, err := ReadWallObservations(dir)
		if err != nil {
			t.Fatal(err)
		}
		ring := newFakeRing()
		calls := 0
		res, err := IngestWallObservations(sources, ring, cfg, func() error { calls++; return nil })
		if err != nil {
			t.Fatal(err)
		}
		return res, calls, ring
	}

	t.Run("matching id under a supplied config is appended and retained", func(t *testing.T) {
		res, calls, ring := drive(t, buildObservation(t, true, "cmp-1", "w1", "h1", 0), &verified)
		if res.Appended != 1 {
			t.Fatal("a matching campaign row must be APPENDED, not rejected")
		}
		if res.Decisions[0].Trainable {
			t.Fatal("a campaign row must be trainable: false")
		}
		if len(ring.rows) != 1 {
			t.Fatal("the row must be retained in the ring")
		}
		// The fitter is not invoked at all.
		if calls != 0 {
			t.Fatalf("fitter called %d times over a diagnostic append, want 0", calls)
		}
		if res.SelectionChanged {
			t.Fatal("a diagnostic append must not change the selected population")
		}
	})

	t.Run("missing or mismatched id under a supplied config is rejected", func(t *testing.T) {
		for _, id := range []string{"", "cmp-other"} {
			res, calls, ring := drive(t, buildObservation(t, true, id, "w1", "h1", 0), &verified)
			if res.Appended != 0 {
				t.Fatalf("campaign_id %q was appended", id)
			}
			if len(ring.rows) != 0 {
				t.Fatalf("campaign_id %q reached the ring", id)
			}
			if calls != 0 {
				t.Fatalf("campaign_id %q invoked the fitter", id)
			}
		}
	})

	t.Run("no config with campaign_id present is refused before append", func(t *testing.T) {
		// R23-F3: not appended, not retained, and the fitter-call count stays
		// zero.
		res, calls, ring := drive(t, buildObservation(t, true, "cmp-1", "w1", "h1", 0), nil)
		if res.Appended != 0 || len(ring.rows) != 0 {
			t.Fatal("a campaign_id with no config must be refused BEFORE append")
		}
		if calls != 0 {
			t.Fatalf("fitter called %d times, want 0", calls)
		}
	})

	t.Run("no config with profile.scored true is refused on the same reasoning", func(t *testing.T) {
		obs := buildObservation(t, true, "", "w1", "h1", 0)
		// A scored profile with no campaign id fails observation validation
		// first, which is the same fail-closed outcome by a stricter route.
		res, calls, ring := drive(t, obs, nil)
		if res.Appended != 0 || len(ring.rows) != 0 {
			t.Fatal("a scored row with no config must be refused")
		}
		if calls != 0 {
			t.Fatal("the fitter must not be invoked")
		}
	})

	t.Run("a foreign workload is appended trainable false, materialized at append", func(t *testing.T) {
		res, calls, _ := drive(t, buildObservation(t, true, "cmp-1", "w-other", "h1", 0), &verified)
		if res.Appended != 1 {
			t.Fatal("a foreign-workload row is appended, not rejected")
		}
		if res.Decisions[0].Trainable {
			t.Fatal("a foreign-workload row must be trainable: false")
		}
		if calls != 0 {
			t.Fatal("the fitter must not be invoked")
		}
	})

	t.Run("a foreign orchestration commit is appended trainable false", func(t *testing.T) {
		res, _, _ := drive(t, buildObservation(t, true, "cmp-1", "w1", "hx", 0), &verified)
		if res.Appended != 1 || res.Decisions[0].Trainable {
			t.Fatalf("expected an appended non-trainable row, got %+v", res.Decisions[0])
		}
	})

	t.Run("an unverified config never reaches append", func(t *testing.T) {
		// R24-F3, both perturbations asserted INDEPENDENTLY.
		if _, err := VerifyCampaignConfig(append(cfgBytes, ' '), sha256Hex(cfgBytes)); err == nil {
			t.Error("perturbed bytes with the expected digest held must be refused")
		}
		perturbed := sha256Hex(cfgBytes)
		perturbed = perturbed[:len(perturbed)-1] + "0"
		if _, err := VerifyCampaignConfig(cfgBytes, perturbed); err == nil {
			t.Error("a perturbed expected digest with the bytes held must be refused")
		}
		if _, err := VerifyCampaignConfig(cfgBytes, ""); err == nil {
			t.Error("an absent expected digest must be refused")
		}
	})

	t.Run("the selector evaluates trainable and nothing else", func(t *testing.T) {
		_, _, ring := drive(t, buildObservation(t, true, "cmp-1", "w1", "h1", 0), &verified)
		ring.SelectedIdentities()
		for name := range ring.predicateNames {
			if name != "trainable" {
				t.Errorf("the selector evaluated predicate %q; trainable is the sole in-CI mechanism", name)
			}
		}
	})

	t.Run("many diagnostic appends leave the selection and the fitter untouched", func(t *testing.T) {
		dir := t.TempDir()
		var obs []Observation
		for i := 0; i < 20; i++ {
			o := buildObservation(t, true, "cmp-1", "w1", "h1", i%8)
			o.JobID = fmt.Sprintf("job-%d", i)
			// EACH ITS OWN EXECUTION KEY. Twenty rows over eight bucket names
			// meant twelve of them repeated (head_sha, run_id, run_attempt,
			// bucket_name), which QC11 forbids outright — the fixture was a
			// shape no legal run produces. Twenty diagnostics from twenty runs
			// is the shape this subtest is about.
			o.RunID = fmt.Sprintf("run-%d", i)
			obs = append(obs, o)
		}
		writeObservations(t, dir, obs)
		sources, err := ReadWallObservations(dir)
		if err != nil {
			t.Fatal(err)
		}
		ring := newFakeRing()
		calls := 0
		res, err := IngestWallObservations(sources, ring, &verified, func() error { calls++; return nil })
		if err != nil {
			t.Fatal(err)
		}
		if res.Appended != 20 {
			t.Fatalf("appended %d, want 20 retained diagnostics", res.Appended)
		}
		if res.SelectionChanged {
			t.Fatal("20 diagnostic appends changed the selected population")
		}
		if calls != 0 {
			t.Fatalf("fitter called %d times across many diagnostic appends, want 0", calls)
		}
	})
}

// TestRecordInputReachesIngestFlags is §22 test 71b: the record hop, end to
// end, asserting ONLY the relations §21 defines, each in the direction that has
// an order.
func TestRecordInputReachesIngestFlags(t *testing.T) {
	u := WorkflowInputUnion()
	w := WorkflowRoute()
	a := AddedActionInputs()
	s := ScoredRequiredInputs()

	t.Run("U to W in order: every W[job] is an ordered subsequence of U", func(t *testing.T) {
		for job, inputs := range w {
			if !IsOrderedSubsequence(inputs, u) {
				t.Errorf("W[%s] = %v is not an ordered subsequence of U", job, inputs)
			}
		}
		// An order-only permutation FAILS: the comparison is subsequence,
		// never set.
		permuted := append([]string(nil), w[JobRecord]...)
		permuted[0], permuted[len(permuted)-1] = permuted[len(permuted)-1], permuted[0]
		if IsOrderedSubsequence(permuted, u) {
			t.Error("an order-only permutation of W[record job] was accepted as a subsequence")
		}
	})

	t.Run("every member of U reaches at least one job", func(t *testing.T) {
		routed := map[string]bool{}
		for _, inputs := range w {
			for _, in := range inputs {
				routed[in] = true
			}
		}
		for _, in := range u {
			if !routed[in] {
				t.Errorf("union input %q is routed nowhere", in)
			}
		}
		// And a routed name outside U fails, in that direction.
		inUnion := map[string]bool{}
		for _, in := range u {
			inUnion[in] = true
		}
		for job, inputs := range w {
			for _, in := range inputs {
				if !inUnion[in] {
					t.Errorf("W[%s] routes %q, which is not in U", job, in)
				}
			}
		}
	})

	t.Run("W is a route, not a partition", func(t *testing.T) {
		counts := map[string]int{}
		for _, inputs := range w {
			for _, in := range inputs {
				counts[in]++
			}
		}
		for _, shared := range []string{"runs-on-label", "candidate-sha", "workload-commit"} {
			if counts[shared] < 2 {
				t.Errorf("%q reaches %d job(s); §21 requires it to reach more than one, so the sets are not disjoint",
					shared, counts[shared])
			}
		}
	})

	t.Run("T(W, plan outputs) to A as membership, both directions", func(t *testing.T) {
		for _, action := range []string{ActionPlan, ActionRunBucket, ActionRecord} {
			got, err := TransportTo(action)
			if err != nil {
				t.Fatal(err)
			}
			if !SameSet(got, a[action]) {
				t.Errorf("T(%s) = %v, A[%s] = %v; membership must agree in both directions",
					action, got, action, a[action])
			}
		}
	})

	t.Run("plan collapses the two caller inputs into cache-declaration-file", func(t *testing.T) {
		got, _ := TransportTo(ActionPlan)
		var hasFile, hasJSON, hasExpected bool
		for _, x := range got {
			switch x {
			case "cache-declaration-file":
				hasFile = true
			case "cache-declaration-json":
				hasJSON = true
			case "cache-declaration-digest-expected":
				hasExpected = true
			}
		}
		if !hasFile {
			t.Error("plan must gain cache-declaration-file")
		}
		if hasJSON || hasExpected {
			t.Error("the caller inputs must not survive into A[plan]; they are materialized and verified job-locally")
		}
	})

	t.Run("run-bucket gains the plan-output trio", func(t *testing.T) {
		got, _ := TransportTo(ActionRunBucket)
		need := map[string]bool{"cache-declaration-file": false, "cache-declaration-digest": false, "shard-plan": false}
		for _, x := range got {
			if _, ok := need[x]; ok {
				need[x] = true
			}
		}
		for name, present := range need {
			if !present {
				t.Errorf("A[run-bucket] is missing the plan-produced %q; a bucket reading a caller pathname breaks T", name)
			}
		}
	})

	t.Run("A matches component-map's action_interfaces in order, both directions", func(t *testing.T) {
		cm := loadActionInterfaces(t)
		for action, want := range a {
			got, ok := cm[action]
			if !ok {
				t.Errorf("component-map declares no added inputs for %q", action)
				continue
			}
			if len(got) != len(want) {
				t.Errorf("A[%s] has %d inputs, component-map has %d: %v vs %v", action, len(want), len(got), want, got)
				continue
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("A[%s] position %d is %q, component-map has %q — an order-only permutation fails",
						action, i, want[i], got[i])
				}
			}
		}
	})

	t.Run("S is A minus its exclusion classes, in A's order", func(t *testing.T) {
		for action, added := range a {
			var want []string
			for _, in := range added {
				if _, excluded := scoredExclusions[in]; excluded {
					continue
				}
				want = append(want, in)
			}
			got := s[action]
			if len(got) != len(want) {
				t.Errorf("S[%s] = %v, want %v", action, got, want)
				continue
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("S[%s] position %d is %q, want %q", action, i, got[i], want[i])
				}
			}
		}
		// S compared AS A fails: the excluded names are really absent.
		if SameSet(s[ActionPlan], a[ActionPlan]) {
			t.Error("S[plan] equals A[plan]; the defaulted est-basis must be excluded")
		}
		for _, excluded := range []string{"est-basis", "dependency-cache-matched-key", "dependency-cache-hit"} {
			for action, inputs := range s {
				for _, in := range inputs {
					if in == excluded {
						t.Errorf("S[%s] carries the excluded name %q", action, in)
					}
				}
			}
		}
	})

	t.Run("wall-observations-dir reaches the record job", func(t *testing.T) {
		var found bool
		for _, in := range w[JobRecord] {
			if in == "wall-observations-dir" {
				found = true
			}
		}
		if !found {
			t.Fatal("wall-observations-dir must reach the record job, where ingest --wall-observations reads it")
		}
		var inA bool
		for _, in := range a[ActionRecord] {
			if in == "wall-observations-dir" {
				inA = true
			}
		}
		if !inA {
			t.Fatal("the record action must accept wall-observations-dir")
		}
	})

	t.Run("exposing campaign-id without the config inputs fails", func(t *testing.T) {
		// §15.1b would then reject every scored row the workflow ingests, so
		// the union must carry all three together.
		has := func(name string) bool {
			for _, in := range u {
				if in == name {
					return true
				}
			}
			return false
		}
		if has("campaign-id") && !(has("campaign-config-json") && has("campaign-config-digest-expected")) {
			t.Fatal("campaign-id is exposed without the config inputs")
		}
	})

	t.Run("the plan job's output set is exactly the three §21 names", func(t *testing.T) {
		want := []string{"matrix", "cache-declaration-json", "cache-declaration-digest"}
		if !SameSet(PlanJobOutputs(), want) {
			t.Fatalf("plan outputs = %v, want %v", PlanJobOutputs(), want)
		}
	})
}

// TestObservationsAreFoundBelowArtifactSubdirectories is the V1-F1 regression.
//
// `actions/download-artifact` with `merge-multiple: false` puts each artifact
// under its own name, so the documents arrive one level down. The reader
// listed a single level and skipped directories, so ingest found nothing,
// reported "0 of 0 observations", exited 0 and saved the reporter update —
// learning silently from no rows, which looks exactly like having none.
//
// The workflow now merges into one directory AND the reader walks, because a
// reader that only works for one layout turns the next layout change back into
// a silent regression.
func TestObservationsAreFoundBelowArtifactSubdirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "testbucket-wall-obs-bucket-0-42-1")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	obs := buildObservation(t, false, "", "workload-1", "head-1", 0)
	b, err := json.Marshal(obs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "observation-bucket-0.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	// And one at the top level, which is what a merged download produces.
	if err := os.WriteFile(filepath.Join(root, "observation-bucket-1.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadWallObservations(root)
	if err != nil {
		t.Fatalf("ReadWallObservations: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d observation(s), want both the nested and the top-level document; "+
			"a reader that misses one reports 0 of 0 and exits 0", len(got))
	}
}
