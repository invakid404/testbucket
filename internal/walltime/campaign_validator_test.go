package walltime

import (
	"fmt"
	"strings"
	"testing"
)

func plannedEight() map[int]string {
	out := map[int]string{}
	for i := 0; i < BucketsPerRun; i++ {
		out[i] = fmt.Sprintf("bucket-%d", i)
	}
	return out
}

func conformingArm(digest string) ArmRunProfile {
	p := ArmRunProfile{
		RunnerToken: "vitest", K: BucketsPerRun, Count: 1, FileParallelism: 1,
		PlanDigest: "sha256:plan", ExpandedUnitSetDigest: digest,
	}
	for i := 0; i < BucketsPerRun; i++ {
		p.Rows = append(p.Rows, ArmRunRow{
			BucketIndex: i, BucketName: fmt.Sprintf("bucket-%d", i),
			Terminal: "passed", PlanDigest: "sha256:plan",
		})
	}
	return p
}

// TestCampaignValidatorProfileAndPairInvariants is §22 test 14.
func TestCampaignValidatorProfileAndPairInvariants(t *testing.T) {
	planned := plannedEight()

	t.Run("a conforming arm-run and pair validate", func(t *testing.T) {
		if err := ValidateArmRunProfile(conformingArm("sha256:u"), planned); err != nil {
			t.Fatalf("a conforming arm was rejected: %v", err)
		}
		if err := ValidatePairProfile(conformingArm("sha256:u"), conformingArm("sha256:u"), planned); err != nil {
			t.Fatalf("a conforming pair was rejected: %v", err)
		}
	})

	t.Run("K = 9 is rejected", func(t *testing.T) {
		p := conformingArm("sha256:u")
		p.K = 9
		if err := ValidateArmRunProfile(p, planned); err == nil {
			t.Fatal("K = 9 was accepted")
		}
	})

	t.Run("a bucket-set gap is rejected", func(t *testing.T) {
		// Eight DISTINCT rows are not sufficient; the set must be COMPLETE.
		p := conformingArm("sha256:u")
		p.Rows = p.Rows[:len(p.Rows)-1]
		err := ValidateArmRunProfile(p, planned)
		if err == nil {
			t.Fatal("an incomplete bucket set was accepted")
		}
		if !strings.Contains(err.Error(), "complete") {
			t.Fatalf("the rejection must name completeness, got %v", err)
		}
	})

	t.Run("a K > 8 plan cannot contribute a favourable eight", func(t *testing.T) {
		nine := plannedEight()
		nine[8] = "bucket-8"
		if err := ValidateArmRunProfile(conformingArm("sha256:u"), nine); err == nil {
			t.Fatal("an eight-row arm against a nine-bucket plan was accepted")
		}
	})

	t.Run("a name/index mismatch is rejected", func(t *testing.T) {
		p := conformingArm("sha256:u")
		p.Rows[3].BucketName = "bucket-7"
		err := ValidateArmRunProfile(p, planned)
		if err == nil {
			t.Fatal("a name/index mismatch was accepted")
		}
		if !strings.Contains(err.Error(), "bucket-7") {
			t.Fatalf("the rejection must name the offending value, got %v", err)
		}
	})

	t.Run("a differing expanded_unit_set_digest rejects the pair", func(t *testing.T) {
		err := ValidatePairProfile(conformingArm("sha256:u"), conformingArm("sha256:other"), planned)
		if err == nil {
			t.Fatal("a pair with differing unit topology was accepted")
		}
		if !strings.Contains(err.Error(), "topology is not part of the treatment") {
			t.Fatalf("the rejection must say topology is not the treatment, got %v", err)
		}
	})

	t.Run("a pair differing in any BC-INV leaf is rejected", func(t *testing.T) {
		// est_basis IS the treatment, so it is not a BC-INV member; what the
		// validator rejects is a pair differing in any OTHER invariant.
		membership, tupleLeaves := bcInvMembershipFromRegistry(t)
		expanded := ExpandMembership(membership, tupleLeaves)
		values := map[string]string{}
		for i, m := range expanded {
			values[m] = fmt.Sprintf("v%03d", i)
		}
		b, err := NewBCInvariantTuple(expanded, values)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidatePairInvariant(b, b); err != nil {
			t.Fatalf("identical invariants were rejected: %v", err)
		}
		mutated := map[string]string{}
		for k, v := range values {
			mutated[k] = v
		}
		mutated[expanded[0]] = "different"
		c, err := NewBCInvariantTuple(expanded, mutated)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidatePairInvariant(b, c); err == nil {
			t.Fatal("a pair differing in a BC-INV leaf was accepted")
		}
	})

	t.Run("plan_digest must be identical across all eight rows of one arm-run", func(t *testing.T) {
		p := conformingArm("sha256:u")
		p.Rows[5].PlanDigest = "sha256:different"
		if err := ValidateArmRunProfile(p, planned); err == nil {
			t.Fatal("an arm-run with two plan digests was accepted")
		}
	})

	t.Run("a non-terminal row is rejected", func(t *testing.T) {
		p := conformingArm("sha256:u")
		p.Rows[0].Terminal = "failed"
		if err := ValidateArmRunProfile(p, planned); err == nil {
			t.Fatal("a non-terminal row was accepted")
		}
	})
}

// TestCampaignProvenanceAndCutoff is §22 test 15.
func TestCampaignProvenanceAndCutoff(t *testing.T) {
	base := CampaignProvenance{
		FittedAt:                "2026-08-25T00:00:00Z",
		FirstAuthenticatedStart: "2026-09-01T00:00:00Z",
		ExcludedRunIDs:          []string{"harness-1"},
		ExcludedWindowStart:     "2026-09-01T00:00:00Z",
		ExcludedWindowEnd:       "2026-09-15T00:00:00Z",
		ExclusionDomains:        []string{"harness runs", "campaign window"},
	}

	t.Run("a conforming provenance validates", func(t *testing.T) {
		if err := ValidateProvenance(base, []string{"warm-1"},
			map[string]string{"warm-1": "2026-08-01T00:00:00Z"}); err != nil {
			t.Fatalf("a conforming provenance was rejected: %v", err)
		}
	})

	t.Run("fitted_at after the first authenticated start fails", func(t *testing.T) {
		p := base
		p.FittedAt = "2026-09-02T00:00:00Z"
		err := ValidateProvenance(p, nil, nil)
		if err == nil {
			t.Fatal("a model fitted after the campaign began was accepted")
		}
		if !strings.Contains(err.Error(), "frozen before") {
			t.Fatalf("the rejection must cite the freeze, got %v", err)
		}
	})

	t.Run("a ring containing a harness run_id fails", func(t *testing.T) {
		if err := ValidateProvenance(base, []string{"warm-1", "harness-1"},
			map[string]string{"warm-1": "2026-08-01T00:00:00Z"}); err == nil {
			t.Fatal("a ring containing an excluded harness run was accepted")
		}
	})

	t.Run("a row inside the excluded window fails", func(t *testing.T) {
		if err := ValidateProvenance(base, []string{"warm-1"},
			map[string]string{"warm-1": "2026-09-05T00:00:00Z"}); err == nil {
			t.Fatal("a ring row inside the excluded window was accepted")
		}
	})

	t.Run("an unnamed exclusion domain fails", func(t *testing.T) {
		p := base
		p.ExclusionDomains = []string{"harness runs", "  "}
		if err := ValidateProvenance(p, nil, nil); err == nil {
			t.Fatal("an unnamed exclusion domain was accepted")
		}
		p.ExclusionDomains = nil
		if err := ValidateProvenance(p, nil, nil); err == nil {
			t.Fatal("no exclusion domain at all was accepted")
		}
	})

	t.Run("these predicates are operator-side, not in-CI", func(t *testing.T) {
		// §15.1b: the in-CI path selects on `trainable` alone. The window and
		// run-id predicates are evaluated here, with the operator's
		// credentials, and a disagreement fails the campaign.
		src := readWalltimeSource(t, "feedback.go")
		for _, banned := range []string{"ExcludedRunIDs", "ExcludedWindowStart"} {
			if strings.Contains(src, banned) {
				t.Errorf("the in-CI ingest path reads %q; that predicate is operator-side", banned)
			}
		}
	})
}

// TestCampaignIdentitiesAreSeparateFields is §22 test 16 (S-6).
func TestCampaignIdentitiesAreSeparateFields(t *testing.T) {
	membership, tupleLeaves := bcInvMembershipFromRegistry(t)
	expanded := ExpandMembership(membership, tupleLeaves)

	identities := []string{
		"manifest.orchestration_commit",
		"manifest.workload_commit",
		"manifest.candidate_sha",
	}

	t.Run("all three are separate required members", func(t *testing.T) {
		members := map[string]bool{}
		for _, m := range expanded {
			members[m] = true
		}
		for _, id := range identities {
			if !members[id] {
				t.Errorf("%q is not a separate bc_inv member", id)
			}
		}
	})

	t.Run("each must be equal across the arms", func(t *testing.T) {
		values := map[string]string{}
		for i, m := range expanded {
			values[m] = fmt.Sprintf("v%03d", i)
		}
		base, err := NewBCInvariantTuple(expanded, values)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range identities {
			mutated := map[string]string{}
			for k, v := range values {
				mutated[k] = v
			}
			mutated[id] = "substituted"
			other, err := NewBCInvariantTuple(expanded, mutated)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidatePairInvariant(base, other); err == nil {
				t.Errorf("a differing %s was accepted across the arms", id)
			}
		}
	})

	t.Run("none is derivable from another", func(t *testing.T) {
		// Three distinct registry paths, and the fixture gives each a distinct
		// value, so a substitution could not pass unnoticed.
		seen := map[string]bool{}
		for _, id := range identities {
			if seen[id] {
				t.Fatalf("identity %q is listed twice", id)
			}
			seen[id] = true
		}
		if len(seen) != 3 {
			t.Fatalf("expected three distinct identities, got %d", len(seen))
		}
	})
}

// TestCampaignVoidAndITTBoundary is §22 test 40 (S-7).
func TestCampaignVoidAndITTBoundary(t *testing.T) {
	t.Run("a pre-start void is voidable", func(t *testing.T) {
		runs := tenScoredRuns()
		runs = append(runs, ArmRun{
			Pair: 1, Arm: ArmB, RunID: "v1", RunAttempt: 1,
			StartedAt: "2026-09-01T00:00:00Z", CompletedAt: "2026-09-01T00:01:00Z",
			StartedBucketScript: false, Disposition: DispositionVoidPreStart,
		})
		if err := (CampaignAttempts{Runs: runs}).ValidateITT(); err != nil {
			t.Fatalf("a pre-start void must be permitted: %v", err)
		}
	})

	t.Run("the first and second reschedules of one pair are permitted, the third ends it", func(t *testing.T) {
		mk := func(n int) CampaignAttempts {
			runs := tenScoredRuns()
			for i := 0; i < n; i++ {
				runs = append(runs, ArmRun{
					Pair: 2, Arm: ArmC, RunID: fmt.Sprintf("rs-%d", i), RunAttempt: 1,
					StartedAt: "2026-09-02T00:00:00Z", CompletedAt: "2026-09-02T00:01:00Z",
					StartedBucketScript: false, Disposition: DispositionRescheduled,
				})
			}
			return CampaignAttempts{Runs: runs}
		}
		for n := 1; n <= MaxRescheduleEvents; n++ {
			if err := mk(n).ValidateITT(); err != nil {
				t.Fatalf("reschedule %d must be permitted: %v", n, err)
			}
		}
		if err := mk(MaxRescheduleEvents + 1).ValidateITT(); err == nil {
			t.Fatalf("the %dth void/reschedule must end the campaign", MaxRescheduleEvents+1)
		}
	})

	t.Run("a post-start failure is retained and unscored", func(t *testing.T) {
		runs := tenScoredRuns()
		runs[2].Disposition = DispositionRetainedUnscored
		a := CampaignAttempts{Runs: runs}
		if err := a.ValidateITT(); err != nil {
			t.Fatal(err)
		}
		p := a.Populations()
		if p.Scored != CampaignRuns-1 {
			t.Fatalf("scored = %d, want %d", p.Scored, CampaignRuns-1)
		}
		if p.Attempted != CampaignRuns {
			t.Fatalf("attempted = %d, want %d — the row is retained", p.Attempted, CampaignRuns)
		}
	})

	t.Run("run_attempt > 1 never replaces attempt 1", func(t *testing.T) {
		runs := tenScoredRuns()
		runs = append(runs, ArmRun{
			Pair: 1, Arm: ArmB, RunID: "retry", RunAttempt: 2,
			StartedAt: "2026-09-01T05:00:00Z", CompletedAt: "2026-09-01T06:00:00Z",
			StartedBucketScript: true, Disposition: DispositionScored,
		})
		if err := (CampaignAttempts{Runs: runs}).ValidateITT(); err == nil {
			t.Fatal("attempt 2 with no attempt 1 was accepted")
		}
	})

	t.Run("every run the Actions API returns is accounted for", func(t *testing.T) {
		a := CampaignAttempts{Runs: tenScoredRuns()}
		var ids []string
		for _, r := range a.Runs {
			ids = append(ids, r.RunID)
		}
		if err := a.AccountForAPIRuns(ids); err != nil {
			t.Fatal(err)
		}
		if err := a.AccountForAPIRuns(append(ids, "ghost")); err == nil {
			t.Fatal("an unaccounted API run was accepted")
		}
	})
}
