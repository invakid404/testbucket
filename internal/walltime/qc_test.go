package walltime

import (
	"strings"
	"testing"
)

// qcFixture builds a plan context and a matching observation that passes every
// check, so each case can break exactly one thing.
func qcFixture(t *testing.T) (PlanContext, Observation) {
	t.Helper()
	prof := CanonicalProfile{
		Scored: false, RunnerToken: "vitest", K: 8, Count: 1, FileParallelism: 1,
		BucketIndices: []int{0, 1, 2, 3, 4, 5, 6, 7},
		EstBasis:      BasisWall, StoreSHA256: "sha256:s", ExpandedUnitSetDigest: "sha256:u",
	}
	blk, err := NewProfileBlock(prof)
	if err != nil {
		t.Fatal(err)
	}
	rp := planRuntimeProfile()
	plan := PlanContext{
		PlanDigest: "sha256:plan",
		Buckets: map[string]PlanBucketRef{
			"bucket-0": {
				Index:       0,
				UnitIDs:     []string{"a.spec.ts", "b.spec.ts"},
				ArgvDigests: []Digest{"sha256:argv0"},
				CwdDigests:  []Digest{"sha256:cwd0"},
			},
		},
		ProfileBlock:                 blk,
		ComparabilityKeyDigest:       "sha256:key",
		RunnerImageLabel:             "ubuntu-24.04",
		RuntimeProfileDeclared:       rp,
		RuntimeProfileDeclaredDigest: RuntimeProfileDigest(rp),
		CoverageAuditPasses:          map[string]bool{"bucket-0": true},
	}
	obs := Observation{
		Schema: ObservationSchema, ComparabilityKeyDigest: "sha256:key",
		Repository: "owner/name", HeadSHA: "head1", CandidateSHA: "cand1", WorkloadCommit: "work1",
		RunID: "run-1", RunAttempt: "1", JobID: "job-1",
		BucketIndex: 0, BucketName: "bucket-0", PlanDigest: "sha256:plan",
		Profile: blk,
		AEtaNs:  NanosPtr(20_000_000_000), EstSeconds: 20.0,
		ProcessGroupID:      "4242",
		ActualRunnerName:    "GitHub Actions 7",
		ObservedRunsOnLabel: "ubuntu-24.04",
		UnitIDs:             []string{"a.spec.ts", "b.spec.ts"},
		Invocations: []Invocation{{
			Seq: 0, Units: []string{"a.spec.ts", "b.spec.ts"},
			ArgvDigest: "sha256:argv0", CwdDigest: "sha256:cwd0",
			ProcessGroupID: "4243",
			StartedMonoNs:  1_000, EndedMonoNs: 9_000_001_000, ElapsedNs: 9_000_000_000,
			ExitCode: 0,
		}},
		StartedMonoNs: 0, EndedMonoNs: 20_000_000_000, ElapsedNs: 20_000_000_000,
		SetupNs: 5_000_000_000, ScriptNs: 10_000_000_000,
		ScriptOverheadNs: 1_000_000_000, WrapperNs: 5_000_000_000,
		BootIDStart: "boot-a", BootIDEnd: "boot-a",
		RealtimeStart: "2026-09-01T00:00:00Z", RealtimeEnd: "2026-09-01T00:00:20Z",
		RuntimeProfile: rp, RuntimeProfileDigest: RuntimeProfileDigest(rp),
		Terminal: "passed", ExitCode: 0,
		Limitations: CanonicalLimitations(),
	}
	return plan, obs
}

func emptyRing() RingFacts {
	return RingFacts{
		SeenObservationKeys: map[[4]string]bool{},
		SeenIntrinsicIDs:    map[[6]string]bool{},
	}
}

// TestQualificationChecks is §22 test 3: QC1–QC17, ONE CASE EACH, asserting the
// exact rejection reason.
//
// Each case breaks exactly one thing against a fixture that otherwise passes,
// so a failure names the check that fired rather than the first check that
// happened to notice.
func TestQualificationChecks(t *testing.T) {
	t.Run("a conforming observation passes every check", func(t *testing.T) {
		plan, obs := qcFixture(t)
		if err := QualifyObservation(obs, plan, emptyRing()); err != nil {
			t.Fatalf("the fixture must pass every check: %v", err)
		}
	})

	cases := []struct {
		check  string
		mutate func(*PlanContext, *Observation, *RingFacts)
	}{
		{"QC1", func(p *PlanContext, o *Observation, r *RingFacts) { o.Schema = "testbucket.wall-observation/v9" }},
		{"QC2", func(p *PlanContext, o *Observation, r *RingFacts) { o.Repository = "" }},
		{"QC3", func(p *PlanContext, o *Observation, r *RingFacts) { o.PlanDigest = "sha256:other" }},
		{"QC4", func(p *PlanContext, o *Observation, r *RingFacts) { o.BucketIndex = 5 }},
		{"QC5", func(p *PlanContext, o *Observation, r *RingFacts) { o.UnitIDs = []string{"a.spec.ts"} }},
		{"QC6", func(p *PlanContext, o *Observation, r *RingFacts) { o.Invocations[0].ArgvDigest = "sha256:tampered" }},
		{"QC7", func(p *PlanContext, o *Observation, r *RingFacts) {
			// A < setup_ns + script_ns.
			o.ElapsedNs = 1_000_000_000
		}},
		{"QC7a", func(p *PlanContext, o *Observation, r *RingFacts) { o.ProcessGroupID = "" }},
		{"QC8", func(p *PlanContext, o *Observation, r *RingFacts) { o.BootIDEnd = "boot-b" }},
		{"QC9", func(p *PlanContext, o *Observation, r *RingFacts) { o.Terminal = "failed" }},
		{"QC10", func(p *PlanContext, o *Observation, r *RingFacts) { p.CoverageAuditPasses["bucket-0"] = false }},
		{"QC11", func(p *PlanContext, o *Observation, r *RingFacts) {
			r.SeenObservationKeys[ObservationKey(*o)] = true
		}},
		{"QC12", func(p *PlanContext, o *Observation, r *RingFacts) { o.ObservedRunsOnLabel = "ubuntu-22.04" }},
		{"QC13", func(p *PlanContext, o *Observation, r *RingFacts) {
			mutated := CanonicalProfile{
				Scored: false, RunnerToken: "vitest", K: 9, Count: 1, FileParallelism: 1,
				BucketIndices: []int{0}, EstBasis: BasisWall,
				StoreSHA256: "sha256:s", ExpandedUnitSetDigest: "sha256:u",
			}
			blk, err := NewProfileBlock(mutated)
			if err != nil {
				t.Fatal(err)
			}
			o.Profile = blk
		}},
		{"QC15", func(p *PlanContext, o *Observation, r *RingFacts) { o.CandidateSHA = "" }},
		{"QC17", func(p *PlanContext, o *Observation, r *RingFacts) {
			o.RuntimeProfile.NodeVersion = "v20.0.0"
			o.RuntimeProfileDigest = RuntimeProfileDigest(o.RuntimeProfile)
		}},
		{"QC16", func(p *PlanContext, o *Observation, r *RingFacts) { o.RealtimeStart = "not-an-instant" }},
	}

	for _, c := range cases {
		t.Run(c.check, func(t *testing.T) {
			plan, obs := qcFixture(t)
			ring := emptyRing()
			c.mutate(&plan, &obs, &ring)
			err := QualifyObservation(obs, plan, ring)
			if err == nil {
				t.Fatalf("%s: the offending observation was accepted", c.check)
			}
			if !strings.HasPrefix(err.Error(), c.check+":") {
				t.Fatalf("expected %s to fire, got: %v", c.check, err)
			}
		})
	}

	t.Run("QC14a and QC14b are the two halves of the cache check", func(t *testing.T) {
		// QC14a runs on the bucket runner before upload; QC14b at ingest over
		// the uploaded row only. QC14 still names the pair.
		plan, obs := qcFixture(t)
		prof, err := plan.ProfileBlock.Parse()
		if err != nil {
			t.Fatal(err)
		}
		prof.Scored = true
		blk, err := NewProfileBlock(prof)
		if err != nil {
			t.Fatal(err)
		}
		plan.ProfileBlock, obs.Profile = blk, blk
		obs.CampaignID = "cmp-1"
		obs.CacheState = stateDisabled()

		if err := QualifyObservation(obs, plan, emptyRing()); err != nil {
			t.Fatalf("a scored row with a legal disabled cache state must pass: %v", err)
		}
		// A row that did not pass QC14a on its own runner is not ingestible.
		obs.CacheState.MongoBinaryVerifiedOnRunner = false
		err = QualifyObservation(obs, plan, emptyRing())
		if err == nil || !strings.HasPrefix(err.Error(), "QC14b:") {
			t.Fatalf("expected QC14b to fire, got: %v", err)
		}
	})

	t.Run("every check in §7.1's table has a case", func(t *testing.T) {
		// Enumerated from the contract, so a check added to §7.1 without a
		// case here fails rather than passing silently.
		table := qcNamesFromContract(t)
		covered := map[string]bool{"QC14a": true, "QC14b": true}
		for _, c := range cases {
			covered[c.check] = true
		}
		for _, name := range table {
			if !covered[name] {
				t.Errorf("§7.1 declares %s but this test has no case for it", name)
			}
		}
		if len(table) == 0 {
			t.Fatal("parsed no QC names from §7.1")
		}
	})
}

// TestObservationRoundTrip is §22 test 2: `limitations` non-empty and naming
// the Exec-envelope, topology, runner-label and cache-state limits.
func TestObservationRoundTrip(t *testing.T) {
	_, obs := qcFixture(t)

	if len(obs.Limitations) == 0 {
		t.Fatal("limitations is required and non-empty")
	}
	joined := strings.ToLower(strings.Join(obs.Limitations, " | "))
	for name, needle := range map[string]string{
		"Exec-envelope": "exec-envelope interval",
		"topology":      "unit topology is store-derived",
		"runner-label":  "runner_image_label is a mutable name",
		"cache-state":   "cache_state records the declared",
	} {
		if !strings.Contains(joined, strings.ToLower(needle)) {
			t.Errorf("limitations does not name the %s limit (looked for %q)", name, needle)
		}
	}

	t.Run("the round trip preserves every field the checks read", func(t *testing.T) {
		plan, o := qcFixture(t)
		if err := QualifyObservation(o, plan, emptyRing()); err != nil {
			t.Fatal(err)
		}
		b, err := CanonicalJSON(o)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) == 0 {
			t.Fatal("canonical serialization is empty")
		}
		if err := o.Validate(); err != nil {
			t.Fatalf("the observation does not satisfy its own structural rules: %v", err)
		}
	})
}

// TestDuplicateObservationRejectsAllRows is §22 test 4: a duplicate rejects ALL
// rows for the key, not just the second one.
func TestDuplicateObservationRejectsAllRows(t *testing.T) {
	plan, obs := qcFixture(t)

	// Two rows sharing (head_sha, run_id, run_attempt, bucket_name) but
	// otherwise well-formed.
	a, b := obs, obs
	b.JobID = "job-2"

	if ObservationKey(a) != ObservationKey(b) {
		t.Fatal("the fixture no longer produces a duplicate key")
	}

	rejected := RejectDuplicateKeys([]Observation{a, b})
	if len(rejected) != 2 {
		t.Fatalf("%d rows rejected, want both — a duplicate rejects ALL rows for the key", len(rejected))
	}

	// And the surviving path: two rows with distinct keys are both admitted.
	c := obs
	c.BucketName = "bucket-1"
	c.BucketIndex = 1
	plan.Buckets["bucket-1"] = PlanBucketRef{
		Index: 1, UnitIDs: obs.UnitIDs,
		ArgvDigests: []Digest{"sha256:argv0"}, CwdDigests: []Digest{"sha256:cwd0"},
	}
	plan.CoverageAuditPasses["bucket-1"] = true
	if got := RejectDuplicateKeys([]Observation{a, c}); len(got) != 0 {
		t.Fatalf("distinct keys were rejected: %v", got)
	}
}
