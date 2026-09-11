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
				// The per-invocation SELECTION identities QC6 compares. Without
				// them the check could see only the count, the sequence and the
				// argv digest — and "membership" is what it is named for.
				SelectorDigests: []Digest{"sha256:sel0"},
				UnitDigests:     []Digest{"sha256:units0"},
				AtomDigests:     []Digest{"sha256:atoms0"},
				// What the PLAN decided. QC18 compares the row's echo against
				// these; without them the fixture's observation agreed only
				// with itself.
				EstSeconds: 20.0,
				AEtaNs:     NanosPtr(20_000_000_000),
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
		// 40 HEX, which is the domain §13 declares. The fixture named these
		// "head1"/"cand1"/"work1" and passed, because QC15 compared three
		// non-empty strings for distinctness and nothing else.
		Repository: "owner/name", HeadSHA: sha40("head1"), CandidateSHA: sha40("cand1"), WorkloadCommit: sha40("work1"),
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
			SelectorDigest: "sha256:sel0", UnitDigest: "sha256:units0", AtomDigest: "sha256:atoms0",
			ProcessGroupID: "4243",
			StartedMonoNs:  1_000, EndedMonoNs: 9_000_001_000, ElapsedNs: 9_000_000_000,
			ExitCode: 0,
		}},
		StartedMonoNs: 0, EndedMonoNs: 20_000_000_000, ElapsedNs: 20_000_000_000,
		SetupNs: 5_000_000_000, ScriptNs: 10_000_000_000,
		ScriptOverheadNs: 1_000_000_000, WrapperNs: 5_000_000_000,
		BootIDStart: "boot-a", BootIDEnd: "boot-a",
		// The clock the endpoints were read from. §13.1 admits CLOCK_MONOTONIC
		// only, and the fixture now says which one it means rather than leaving
		// the field empty and unexamined.
		ClockID:       ClockMonotonic,
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

// TestQualificationChecks is §22 test 3: QC1–QC18, ONE CASE EACH, asserting the
// exact rejection reason — and THREE for QC18, which §22 test 3 requires by
// name. The check compares two separate echoes, so one case leaves the other
// field uncompared, and a case built on a non-zero planned estimate passes
// whether or not zero is compared at all.
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
		check string
		// sub names the subtest where one check needs more than one case. The
		// assertion is still on `check`, so the completeness subtest below sees
		// the same closed set §7.1 names.
		sub    string
		mutate func(*PlanContext, *Observation, *RingFacts)
	}{
		{check: "QC1", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { o.Schema = "testbucket.wall-observation/v9" }},
		{check: "QC2", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { o.Repository = "" }},
		{check: "QC3", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { o.PlanDigest = "sha256:other" }},
		{check: "QC4", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { o.BucketIndex = 5 }},
		{check: "QC5", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { o.UnitIDs = []string{"a.spec.ts"} }},
		{check: "QC6", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { o.Invocations[0].ArgvDigest = "sha256:tampered" }},
		// MEMBERSHIP, which is QC6's stated subject and was not compared at all.
		// A row with the right bucket-wide unit_ids and the right argv digests,
		// over an invocation that selected something else, passed.
		{check: "QC6", sub: "QC6 a selector that is not the plan's", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			o.Invocations[0].SelectorDigest = "sha256:other-selection"
		}},
		{check: "QC6", sub: "QC6 an absent selector digest", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			o.Invocations[0].SelectorDigest = ""
		}},
		{check: "QC6", sub: "QC6 a unit set that is not the plan's", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			o.Invocations[0].UnitDigest = "sha256:other-units"
		}},
		{check: "QC6", sub: "QC6 an atom set that is not the plan's", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			o.Invocations[0].AtomDigest = "sha256:other-atoms"
		}},
		{check: "QC6", sub: "QC6 a plan context carrying no membership identities", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			ref := p.Buckets["bucket-0"]
			ref.SelectorDigests = nil
			p.Buckets["bucket-0"] = ref
		}},
		{check: "QC7", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			// A < setup_ns + script_ns.
			o.ElapsedNs = 1_000_000_000
		}},
		{check: "QC7a", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { o.ProcessGroupID = "" }},
		{check: "QC8", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { o.BootIDEnd = "boot-b" }},
		// THE CLOCK DOMAIN, for a SCORED row. §13.1 admits CLOCK_MONOTONIC only;
		// the non-Linux fallback reads host realtime under an honest name, and its
		// readings advance with a fixed boot marker that matches itself, so every
		// other check here passed.
		{check: "QC8", sub: "QC8 a scored row on an unscorable clock", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			prof, err := p.ProfileBlock.Parse()
			if err != nil {
				t.Fatal(err)
			}
			prof.Scored = true
			blk, err := NewProfileBlock(prof)
			if err != nil {
				t.Fatal(err)
			}
			p.ProfileBlock, o.Profile = blk, blk
			o.CampaignID = "cmp-1"
			o.CacheState = stateDisabled()
			o.ClockID = ClockRealtimeUnscored
		}},
		{check: "QC9", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { o.Terminal = "failed" }},
		{check: "QC10", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { p.CoverageAuditPasses["bucket-0"] = false }},
		// AN ABSENT VERDICT IS NOT A PASSING ONE. The guard was
		// `CoverageAuditPasses != nil && !passes[name]`, so a nil map — or a map
		// with no entry for this bucket — admitted the row, and the public ingest
		// command supplied exactly that.
		{check: "QC10", sub: "QC10 no verdict for this bucket", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			delete(p.CoverageAuditPasses, "bucket-0")
		}},
		{check: "QC10", sub: "QC10 no verdict map at all", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			p.CoverageAuditPasses = nil
		}},
		{check: "QC11", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			r.SeenObservationKeys[ObservationKey(*o)] = true
		}},
		{check: "QC12", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { o.ObservedRunsOnLabel = "ubuntu-22.04" }},
		{check: "QC13", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
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
		{check: "QC15", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { o.CandidateSHA = "" }},
		// THE DECLARED DOMAIN, not merely presence. §13 spells all three as 40
		// hex characters; QC15 compared three non-empty strings for distinctness,
		// so "a"/"b"/"c" passed — three values, none of them a commit.
		{check: "QC15", sub: "QC15 a candidate_sha that is not a commit", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			o.CandidateSHA = "b"
		}},
		{check: "QC15", sub: "QC15 an uppercase hex identity", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			o.WorkloadCommit = strings.ToUpper(o.WorkloadCommit)
		}},
		// THE CARVE-OUT IS THE PLAN'S TO GRANT, and this fixture's plan does not.
		// Collapsing the three identities without a declared same-repository
		// workload is still S-6's defect.
		{check: "QC15", sub: "QC15 all three equal with no declaration", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			o.CandidateSHA, o.WorkloadCommit = o.HeadSHA, o.HeadSHA
		}},
		// §13.1's PGID domain, for the same reason. `0` is the sharp one: in
		// every negative-PGID signal API it names the CALLER's group.
		{check: "QC7a", sub: "QC7a a process_group_id of zero", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			o.ProcessGroupID = "0"
		}},
		{check: "QC7a", sub: "QC7a a non-numeric process_group_id", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			o.Invocations[0].ProcessGroupID = "abc"
		}},
		{check: "QC17", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			o.RuntimeProfile.NodeVersion = "v20.0.0"
			o.RuntimeProfileDigest = RuntimeProfileDigest(o.RuntimeProfile)
		}},
		// QC18, THREE CASES. The row carries two estimate echoes and the
		// assembler derives one from the other, so each must be falsifiable on
		// its own; and the planned display must be compared even when it is
		// zero, which is the bypass a single non-zero case cannot see.
		{check: "QC18", sub: "QC18 a_eta_ns alone", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			// Only the objective moves. est_seconds still equals the plan's
			// display, so the a_eta_ns comparison is the only one that can fire.
			o.AEtaNs = NanosPtr(21_000_000_000)
		}},
		{check: "QC18", sub: "QC18 est_seconds alone", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			// Only the display moves. The objective still equals the plan's, so
			// the est_seconds comparison is the only one that can fire — the row
			// is internally inconsistent, which is exactly the shape §5.1's echo
			// rule exists to refuse and which nothing but the PLAN can detect.
			o.EstSeconds = 99.0
		}},
		{check: "QC18", sub: "QC18 a plan bucket that displayed zero", mutate: func(p *PlanContext, o *Observation, r *RingFacts) {
			// A LEGITIMATELY ZERO PLANNED ESTIMATE. §17.3a admits an empty
			// bucket as a design row whose reporter sum is 0, and a reporter
			// basis optimizes no objective, so a displayed 0.0 with no a_eta_ns
			// is a plan bucket a real run produces. Zero was read as "the plan
			// said nothing" and the comparison was skipped, so such a bucket
			// accepted a row displaying any estimate at all.
			prof, err := p.ProfileBlock.Parse()
			if err != nil {
				t.Fatal(err)
			}
			prof.EstBasis = BasisReporter
			blk, err := NewProfileBlock(prof)
			if err != nil {
				t.Fatal(err)
			}
			// QC13 is byte-identity, so the plan and the row carry one block.
			p.ProfileBlock, o.Profile = blk, blk
			ref := p.Buckets["bucket-0"]
			ref.EstSeconds, ref.AEtaNs = 0, nil
			p.Buckets["bucket-0"] = ref
			o.AEtaNs, o.EstSeconds = nil, 99.0
		}},
		{check: "QC16", mutate: func(p *PlanContext, o *Observation, r *RingFacts) { o.RealtimeStart = "not-an-instant" }},
	}

	for _, c := range cases {
		name := c.check
		if c.sub != "" {
			name = c.sub
		}
		t.Run(name, func(t *testing.T) {
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
		// §10.5.6 REQUIREMENT 3's two sides. The row's declaration leaves digest
		// to what the row claims, and the plan validated the same declaration;
		// without both, a misrouted row naming a different exact key was admitted
		// while echoing the expected profile and key digests.
		declDigest := DeclarationOf(obs.CacheState).Digest()
		obs.CacheDeclarationDigest = declDigest
		plan.CacheDeclarationDigest = declDigest

		if err := QualifyObservation(obs, plan, emptyRing()); err != nil {
			t.Fatalf("a scored row with a legal disabled cache state must pass: %v", err)
		}

		// A DIFFERENT declaration that leaves §10.5.1's tuple legal, so the
		// failure is requirement 3's and not the tuple rule's. The expected and
		// executed binary digests move together, which keeps QC14b's own equality
		// check satisfied.
		otherBinary := strings.Repeat("b", 64)

		t.Run("the row's leaves must be the declaration it names", func(t *testing.T) {
			bad := obs
			bad.CacheState.MongoBinarySHA256 = otherBinary
			bad.CacheState.ExpectedMongoBinarySHA256 = otherBinary
			// CacheDeclarationDigest left at the old value: the leaves and the
			// digest the row claims no longer agree.
			if err := QualifyObservation(bad, plan, emptyRing()); err == nil ||
				!strings.HasPrefix(err.Error(), "QC14b:") {
				t.Fatalf("a row whose leaves are not the declaration it names was accepted: %v", err)
			}
		})
		t.Run("a misrouted row is refused even when internally coherent", func(t *testing.T) {
			// THE EXACT HAZARD: a DIFFERENT declaration, internally consistent
			// and correctly self-digested, echoing the right profile and key.
			other := obs
			other.CacheState.MongoBinarySHA256 = otherBinary
			other.CacheState.ExpectedMongoBinarySHA256 = otherBinary
			other.CacheDeclarationDigest = DeclarationOf(other.CacheState).Digest()
			if err := QualifyObservation(other, plan, emptyRing()); err == nil ||
				!strings.HasPrefix(err.Error(), "QC14b:") {
				t.Fatalf("a coherent row under another declaration was accepted: %v", err)
			}
		})
		t.Run("an absent plan declaration is not a verified one", func(t *testing.T) {
			p2 := plan
			p2.CacheDeclarationDigest = ""
			if err := QualifyObservation(obs, p2, emptyRing()); err == nil ||
				!strings.HasPrefix(err.Error(), "QC14b:") {
				t.Fatalf("an unverifiable declaration read as verified: %v", err)
			}
		})
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
		SelectorDigests: []Digest{"sha256:sel0"},
		UnitDigests:     []Digest{"sha256:units0"},
		AtomDigests:     []Digest{"sha256:atoms0"},
	}
	plan.CoverageAuditPasses["bucket-1"] = true
	if got := RejectDuplicateKeys([]Observation{a, c}); len(got) != 0 {
		t.Fatalf("distinct keys were rejected: %v", got)
	}
}

// sameRepoProfile rebuilds the fixture's profile block with the same-repository
// declaration set as the caller asks, on BOTH sides.
//
// §13.0 has the observation copy the plan's block VERBATIM and QC13 compares it
// byte for byte, which is the whole reason the declaration lives there: a bucket
// runner cannot award itself the carve-out, because a profile that differs from
// the plan's is refused before QC15 is reached.
func sameRepoProfile(t *testing.T, plan *PlanContext, obs *Observation, sameRepo, scored bool) {
	t.Helper()
	prof, err := plan.ProfileBlock.Parse()
	if err != nil {
		t.Fatal(err)
	}
	prof.SameRepositoryWorkload = sameRepo
	prof.Scored = scored
	blk, err := NewProfileBlock(prof)
	if err != nil {
		t.Fatal(err)
	}
	plan.ProfileBlock, obs.Profile = blk, blk
}

// TestQC15AdmitsADeclaredSameRepositoryDogfoodAndNothingElse is the owner's
// narrow carve-out, with both controls the decision requires.
//
// QC15 rejects a row carrying one value in all three provenance identities, and
// that is right for every shape but one: S-6's defect was one `head_sha`
// overloaded into three fields. A same-repository dogfood is not that defect —
// when the project runs its own suite from its own checkout with a `local` build,
// the orchestration head, the compiled source and the workload checkout ARE one
// commit, and there is no truthful distinct value for the other two fields.
// Inventing one would be the overloading this rule exists to stop, backwards.
//
// The carve-out is therefore granted by the PLAN and only for an unscored row.
func TestQC15AdmitsADeclaredSameRepositoryDogfoodAndNothingElse(t *testing.T) {
	// ONE COMMIT, truthfully, in all three fields.
	one := sha40("the-dogfood-commit")

	t.Run("POSITIVE: a declared, unscored, same-SHA dogfood row passes", func(t *testing.T) {
		plan, obs := qcFixture(t)
		sameRepoProfile(t, &plan, &obs, true, false)
		obs.HeadSHA, obs.CandidateSHA, obs.WorkloadCommit = one, one, one
		if err := QualifyObservation(obs, plan, emptyRing()); err != nil {
			t.Fatalf("a declared same-repository dogfood row was rejected: %v", err)
		}
	})

	t.Run("NEGATIVE: a scored row with all three equal is still rejected", func(t *testing.T) {
		plan, obs := qcFixture(t)
		sameRepoProfile(t, &plan, &obs, false, true)
		obs.CampaignID = "cmp-1"
		obs.CacheState = stateDisabled()
		declDigest := DeclarationOf(obs.CacheState).Digest()
		obs.CacheDeclarationDigest, plan.CacheDeclarationDigest = declDigest, declDigest
		obs.HeadSHA, obs.CandidateSHA, obs.WorkloadCommit = one, one, one

		err := QualifyObservation(obs, plan, emptyRing())
		if err == nil {
			t.Fatal("a scored row collapsed its three provenance identities and was accepted")
		}
		if !strings.HasPrefix(err.Error(), "QC15:") {
			t.Fatalf("expected QC15 to fire, got: %v", err)
		}
		if !strings.Contains(err.Error(), "scored") {
			t.Errorf("the refusal must say WHY a scored row cannot use the carve-out, got %q", err)
		}
	})

	t.Run("NEGATIVE: a scored row may not even declare the carve-out", func(t *testing.T) {
		// The same rule, stated where it can stop the run instead of discarding
		// the row: a scored plan that declares a same-repository workload emits
		// no matrix.
		prof := CanonicalProfile{
			Scored: true, RunnerToken: "vitest", K: 8, Count: 1, FileParallelism: 1,
			BucketIndices:          []int{0, 1, 2, 3, 4, 5, 6, 7},
			EstBasis:               BasisWall,
			StoreSHA256:            "sha256:s",
			ExpandedUnitSetDigest:  "sha256:u",
			SameRepositoryWorkload: true,
		}
		err := AdmitScoredProfile(prof, "class", "ubuntu-24.04", sha40("cand"), sha40("work"), true)
		if err == nil {
			t.Fatal("a scored plan declaring a same-repository workload was admitted")
		}
		if !strings.Contains(err.Error(), "same_repository_workload") {
			t.Errorf("the refusal does not name the declaration: %v", err)
		}
	})

	t.Run("NEGATIVE: an undeclared unscored row with all three equal is rejected", func(t *testing.T) {
		plan, obs := qcFixture(t)
		sameRepoProfile(t, &plan, &obs, false, false)
		obs.HeadSHA, obs.CandidateSHA, obs.WorkloadCommit = one, one, one
		err := QualifyObservation(obs, plan, emptyRing())
		if err == nil || !strings.HasPrefix(err.Error(), "QC15:") {
			t.Fatalf("an undeclared collapsed row was accepted: %v", err)
		}
	})

	t.Run("NEGATIVE: the carve-out does not excuse an absent identity", func(t *testing.T) {
		// It permits EQUALITY, never omission. A row that simply has no
		// candidate_sha is still missing an identity, declared or not.
		plan, obs := qcFixture(t)
		sameRepoProfile(t, &plan, &obs, true, false)
		obs.HeadSHA, obs.WorkloadCommit = one, one
		obs.CandidateSHA = ""
		err := QualifyObservation(obs, plan, emptyRing())
		if err == nil || !strings.HasPrefix(err.Error(), "QC15:") {
			t.Fatalf("an absent candidate_sha was accepted under the carve-out: %v", err)
		}
	})

	t.Run("NEGATIVE: a row cannot award itself the carve-out", func(t *testing.T) {
		// The declaration is in the §13.0 block QC13 compares byte for byte, so a
		// row that sets it while the plan did not is refused BEFORE QC15 — by the
		// check that exists to make the block one value rather than two copies.
		plan, obs := qcFixture(t)
		prof, err := obs.Profile.Parse()
		if err != nil {
			t.Fatal(err)
		}
		prof.SameRepositoryWorkload = true
		blk, err := NewProfileBlock(prof)
		if err != nil {
			t.Fatal(err)
		}
		obs.Profile = blk // the PLAN's block is left alone
		obs.HeadSHA, obs.CandidateSHA, obs.WorkloadCommit = one, one, one
		err = QualifyObservation(obs, plan, emptyRing())
		if err == nil {
			t.Fatal("a row declared its own carve-out and was accepted")
		}
		if !strings.HasPrefix(err.Error(), "QC13:") {
			t.Fatalf("expected QC13's byte-identity to refuse it first, got: %v", err)
		}
	})

	t.Run("partial equality needs no declaration", func(t *testing.T) {
		// A `local` build's source can equal the orchestration head while the
		// workload is somewhere else. That was always legal and still is.
		plan, obs := qcFixture(t)
		sameRepoProfile(t, &plan, &obs, false, false)
		obs.HeadSHA, obs.CandidateSHA = one, one
		obs.WorkloadCommit = sha40("a-different-workload")
		if err := QualifyObservation(obs, plan, emptyRing()); err != nil {
			t.Fatalf("two equal identities and one distinct was rejected: %v", err)
		}
	})
}
