package walltime

import (
	"fmt"
	"strings"
	"testing"
)

func scoredRun(pair int, arm Arm, id string, start, end string) ArmRun {
	return ArmRun{
		Pair: pair, Arm: arm, RunID: id, RunAttempt: 1,
		StartedAt: start, CompletedAt: end,
		StartedBucketScript: true, Disposition: DispositionScored,
	}
}

// tenScoredRuns builds a conforming campaign: five pairs, two arms each, in the
// declared counterbalanced directions, sequential and spanning enough days.
func tenScoredRuns() []ArmRun {
	seq := CounterbalancedSequence()
	var out []ArmRun
	for i, d := range seq {
		day := fmt.Sprintf("2026-09-%02d", i+1)
		out = append(out,
			scoredRun(i+1, d.First, fmt.Sprintf("r%d-first", i+1), day+"T01:00:00Z", day+"T02:00:00Z"),
			scoredRun(i+1, d.Second, fmt.Sprintf("r%d-second", i+1), day+"T03:00:00Z", day+"T04:00:00Z"),
		)
	}
	return out
}

// TestCampaignOrderIsCounterbalancedNotRandomized is §22 test 18.
func TestCampaignOrderIsCounterbalancedNotRandomized(t *testing.T) {
	seq := CounterbalancedSequence()
	if len(seq) != CampaignPairs {
		t.Fatalf("sequence covers %d pairs, want %d", len(seq), CampaignPairs)
	}
	// Pairs 1, 3, 5 run B→C; pairs 2, 4 run C→B.
	want := []Direction{
		{ArmB, ArmC}, {ArmC, ArmB}, {ArmB, ArmC}, {ArmC, ArmB}, {ArmB, ArmC},
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Errorf("pair %d runs %s→%s, want %s→%s", i+1, seq[i].First, seq[i].Second, want[i].First, want[i].Second)
		}
	}

	t.Run("the sequence is declared before run 1 and does not depend on outcomes", func(t *testing.T) {
		// A control, not a draw: two evaluations agree and nothing is seeded.
		a, b := CounterbalancedSequence(), CounterbalancedSequence()
		for i := range a {
			if a[i] != b[i] {
				t.Fatal("the sequence is not fixed")
			}
		}
	})

	t.Run("no shipped string claims randomization in a campaign-order context", func(t *testing.T) {
		// §19.5 permits these words only inside the NEGATIVE statement it
		// requires, so the check reads a context window rather than one line:
		// a denial routinely wraps across lines and would otherwise convict
		// itself.
		// campaign.go and schedule.go are deleted by the removal plan, so the
		// scan is over what still ships.
		files := []string{"campaign_practical.go", "gates.go"}
		negations := []string{"not randomized", "never", "no document", "fails on",
			"is not a", "rather than a draw", "not a draw", "no seed"}
		for _, file := range files {
			lines := strings.Split(readWalltimeSource(t, file), "\n")
			for i, line := range lines {
				l := strings.ToLower(line)
				for _, banned := range []string{"randomized", "randomised", "seed-derived", "shuffle", "reproducible draw"} {
					if !strings.Contains(l, banned) {
						continue
					}
					lo, hi := i-3, i+4
					if lo < 0 {
						lo = 0
					}
					if hi > len(lines) {
						hi = len(lines)
					}
					window := strings.ToLower(strings.Join(lines[lo:hi], " "))
					exempt := false
					for _, n := range negations {
						if strings.Contains(window, n) {
							exempt = true
							break
						}
					}
					if !exempt {
						t.Errorf("%s:%d: %q appears in a campaign-order context: %s",
							file, i+1, banned, strings.TrimSpace(line))
					}
				}
			}
		}
	})
}

// TestPairsRunSequentiallyByAuthenticatedCompletion is §22 tests 30 and 65
// (S-9) and the acceptance test the registry names for ID-20.
func TestPairsRunSequentiallyByAuthenticatedCompletion(t *testing.T) {
	seq := CounterbalancedSequence()
	runs := tenScoredRuns()

	byPair := map[int][]ArmRun{}
	for _, r := range runs {
		byPair[r.Pair] = append(byPair[r.Pair], r)
	}

	t.Run("a conforming campaign passes every pair", func(t *testing.T) {
		for i, d := range seq {
			if err := SequentialOrder(i+1, d, byPair[i+1]); err != nil {
				t.Fatalf("pair %d: %v", i+1, err)
			}
		}
	})

	t.Run("overlapping arms are rejected even when launch order holds", func(t *testing.T) {
		// The distinguishing case: started_at(first) < started_at(second), so
		// LAUNCH order holds, but the first arm is still running when the
		// second starts.
		pair := []ArmRun{
			scoredRun(1, ArmB, "b", "2026-09-01T01:00:00Z", "2026-09-01T05:00:00Z"),
			scoredRun(1, ArmC, "c", "2026-09-01T03:00:00Z", "2026-09-01T06:00:00Z"),
		}
		err := SequentialOrder(1, Direction{ArmB, ArmC}, pair)
		if err == nil {
			t.Fatal("overlapping arms were accepted; launch order alone does not satisfy this gate")
		}
		if !strings.Contains(err.Error(), "OVERLAP") {
			t.Fatalf("the rejection must name the overlap, got %q", err)
		}
	})

	t.Run("a contradicted direction is rejected", func(t *testing.T) {
		pair := []ArmRun{
			scoredRun(1, ArmB, "b", "2026-09-01T05:00:00Z", "2026-09-01T06:00:00Z"),
			scoredRun(1, ArmC, "c", "2026-09-01T01:00:00Z", "2026-09-01T02:00:00Z"),
		}
		if err := SequentialOrder(1, Direction{ArmB, ArmC}, pair); err == nil {
			t.Fatal("a pair whose executed order contradicts the declared direction was accepted")
		}
	})

	t.Run("an empty or unparseable instant is a failure, not a skip", func(t *testing.T) {
		for _, bad := range []string{"", "2026-09-01", "not-a-time"} {
			pair := []ArmRun{
				scoredRun(1, ArmB, "b", "2026-09-01T01:00:00Z", bad),
				scoredRun(1, ArmC, "c", "2026-09-01T03:00:00Z", "2026-09-01T04:00:00Z"),
			}
			if err := SequentialOrder(1, Direction{ArmB, ArmC}, pair); err == nil {
				t.Fatalf("instant %q was skipped rather than failed", bad)
			}
		}
	})

	t.Run("touching instants satisfy the non-strict predicate", func(t *testing.T) {
		// completed_at(first) == started_at(second) is legal: the predicate is
		// less-than-or-equal.
		pair := []ArmRun{
			scoredRun(1, ArmB, "b", "2026-09-01T01:00:00Z", "2026-09-01T02:00:00Z"),
			scoredRun(1, ArmC, "c", "2026-09-01T02:00:00Z", "2026-09-01T03:00:00Z"),
		}
		if err := SequentialOrder(1, Direction{ArmB, ArmC}, pair); err != nil {
			t.Fatalf("touching instants were rejected: %v", err)
		}
	})
}

// TestCampaignDatesAreAuthenticated is §22 test 17.
func TestCampaignDatesAreAuthenticated(t *testing.T) {
	runs := tenScoredRuns()

	t.Run("scheduled dates matching authenticated run dates pass", func(t *testing.T) {
		scheduled := []string{"2026-09-01", "2026-09-02", "2026-09-03", "2026-09-04", "2026-09-05"}
		if err := AuthenticatedDates(scheduled, runs); err != nil {
			t.Fatalf("a conforming campaign was rejected: %v", err)
		}
	})

	t.Run("a scheduled date with no authenticated match fails", func(t *testing.T) {
		scheduled := []string{"2026-09-01", "2026-09-30"}
		if err := AuthenticatedDates(scheduled, runs); err == nil {
			t.Fatal("a scheduled date matching no authenticated run date was accepted")
		}
	})

	t.Run("fewer than three distinct authenticated dates fails", func(t *testing.T) {
		var oneDay []ArmRun
		for i, r := range runs {
			r.StartedAt = "2026-09-01T0" + fmt.Sprint(i%9+1) + ":00:00Z"
			oneDay = append(oneDay, r)
		}
		if err := AuthenticatedDates([]string{"2026-09-01"}, oneDay); err == nil {
			t.Fatal("a single-date campaign was accepted")
		}
	})

	t.Run("a window beyond 14 days fails", func(t *testing.T) {
		wide := append([]ArmRun(nil), runs...)
		wide[len(wide)-1].StartedAt = "2026-10-15T01:00:00Z"
		if err := AuthenticatedDates(nil, wide); err == nil {
			t.Fatal("a campaign spanning more than 14 days was accepted")
		}
	})

	t.Run("an empty authenticated instant fails rather than being skipped", func(t *testing.T) {
		bad := append([]ArmRun(nil), runs...)
		bad[0].StartedAt = ""
		if err := AuthenticatedDates(nil, bad); err == nil {
			t.Fatal("an empty instant was skipped")
		}
	})
}

// TestCampaignAttemptAccountingAndITT is §22 tests 40 and 46 (F8) and the
// acceptance test the registry names for ID-9 and ID-13.
func TestCampaignAttemptAccountingAndITT(t *testing.T) {
	t.Run("10 scored runs with zero voids give 10 scored and 10 attempted", func(t *testing.T) {
		a := CampaignAttempts{Runs: tenScoredRuns()}
		if err := a.ValidateITT(); err != nil {
			t.Fatal(err)
		}
		p := a.Populations()
		if p.Scored != CampaignRuns || p.Attempted != CampaignRuns {
			t.Fatalf("populations = %d scored / %d attempted, want %d / %d",
				p.Scored, p.Attempted, CampaignRuns, CampaignRuns)
		}
	})

	t.Run("a two-arm pre-start void plus reschedule gives 12 attempted and 10 scored", func(t *testing.T) {
		runs := tenScoredRuns()
		// Two voided arm-runs, neither of which started a bucket script.
		runs = append(runs,
			ArmRun{Pair: 1, Arm: ArmB, RunID: "void-b", RunAttempt: 1,
				StartedAt: "2026-09-01T00:00:00Z", CompletedAt: "2026-09-01T00:05:00Z",
				StartedBucketScript: false, Disposition: DispositionVoidPreStart},
			ArmRun{Pair: 1, Arm: ArmC, RunID: "void-c", RunAttempt: 1,
				StartedAt: "2026-09-01T00:00:00Z", CompletedAt: "2026-09-01T00:05:00Z",
				StartedBucketScript: false, Disposition: DispositionVoidPreStart},
		)
		a := CampaignAttempts{Runs: runs}
		if err := a.ValidateITT(); err != nil {
			t.Fatal(err)
		}
		p := a.Populations()
		if p.Scored != 10 || p.Attempted != 12 {
			t.Fatalf("populations = %d scored / %d attempted, want 10 / 12", p.Scored, p.Attempted)
		}
		// The two counts are expected to differ and are never reconciled.
		if p.Scored == p.Attempted {
			t.Fatal("the two populations collapsed into one number")
		}
	})

	t.Run("a post-start failure is retained, unscored and non-passing", func(t *testing.T) {
		runs := tenScoredRuns()
		runs[0].Disposition = DispositionRetainedUnscored
		a := CampaignAttempts{Runs: runs}
		if err := a.ValidateITT(); err != nil {
			t.Fatal(err)
		}
		p := a.Populations()
		if p.Scored != 9 {
			t.Fatalf("scored = %d, want 9 — the retained row is excluded from scored", p.Scored)
		}
		if p.Attempted != 10 {
			t.Fatalf("attempted = %d, want 10 — the row stays in the attempted population", p.Attempted)
		}
	})

	t.Run("a post-start outcome may not be marked void or rescheduled", func(t *testing.T) {
		for _, d := range []string{DispositionVoidPreStart, DispositionRescheduled} {
			runs := tenScoredRuns()
			runs[0].Disposition = d // still StartedBucketScript: true
			a := CampaignAttempts{Runs: runs}
			if err := a.ValidateITT(); err == nil {
				t.Fatalf("a post-start run marked %q was accepted", d)
			}
		}
	})

	t.Run("a pre-start run may not be marked retained-unscored", func(t *testing.T) {
		runs := tenScoredRuns()
		runs[0].StartedBucketScript = false
		runs[0].Disposition = DispositionRetainedUnscored
		if err := (CampaignAttempts{Runs: runs}).ValidateITT(); err == nil {
			t.Fatal("a pre-start run marked retained-unscored was accepted")
		}
	})

	t.Run("two reschedules are permitted and a third is rejected", func(t *testing.T) {
		mk := func(n int) CampaignAttempts {
			runs := tenScoredRuns()
			for i := 0; i < n; i++ {
				runs = append(runs, ArmRun{
					Pair: 1, Arm: ArmB, RunID: fmt.Sprintf("resched-%d", i), RunAttempt: 1,
					StartedAt: "2026-09-01T00:00:00Z", CompletedAt: "2026-09-01T00:05:00Z",
					StartedBucketScript: false, Disposition: DispositionRescheduled,
				})
			}
			return CampaignAttempts{Runs: runs}
		}
		if err := mk(MaxRescheduleEvents).ValidateITT(); err != nil {
			t.Fatalf("%d reschedules must be permitted: %v", MaxRescheduleEvents, err)
		}
		if err := mk(MaxRescheduleEvents + 1).ValidateITT(); err == nil {
			t.Fatal("a third void/reschedule must end the campaign")
		}
	})

	t.Run("run_attempt > 1 never erases attempt 1", func(t *testing.T) {
		runs := tenScoredRuns()
		runs = append(runs, ArmRun{
			Pair: 1, Arm: ArmB, RunID: "orphan", RunAttempt: 2,
			StartedAt: "2026-09-01T01:00:00Z", CompletedAt: "2026-09-01T02:00:00Z",
			StartedBucketScript: true, Disposition: DispositionScored,
		})
		if err := (CampaignAttempts{Runs: runs}).ValidateITT(); err == nil {
			t.Fatal("attempt 2 without attempt 1 was accepted")
		}
		// With both present, both appear in the attempted population.
		runs = append(runs, ArmRun{
			Pair: 1, Arm: ArmB, RunID: "orphan", RunAttempt: 1,
			StartedAt: "2026-09-01T00:00:00Z", CompletedAt: "2026-09-01T00:30:00Z",
			StartedBucketScript: true, Disposition: DispositionRetainedUnscored,
		})
		a := CampaignAttempts{Runs: runs}
		if err := a.ValidateITT(); err != nil {
			t.Fatalf("both attempts present must be accepted: %v", err)
		}
	})

	t.Run("an Actions-API run absent from the manifest rejects the campaign", func(t *testing.T) {
		a := CampaignAttempts{Runs: tenScoredRuns()}
		ids := []string{}
		for _, r := range a.Runs {
			ids = append(ids, r.RunID)
		}
		if err := a.AccountForAPIRuns(ids); err != nil {
			t.Fatalf("a complete manifest was rejected: %v", err)
		}
		if err := a.AccountForAPIRuns(append(ids, "unaccounted-run")); err == nil {
			t.Fatal("an unaccounted Actions-API run was accepted")
		}
	})
}

// TestArmsDifferOnlyByPlannerMode is §22 test 66 and the acceptance test the
// registry names for ID-3.
//
// The mutation matrix is GENERATED by walking memberships.bc_inv and recursing
// into memberships.tuple_leaves. The test source contains no literal field
// count, and the membership comes from the parsed registry rather than from a
// Go struct — which is the repair: a first cut modelled sixteen leaves against
// a membership carrying thirty-four, and the eighteen it missed would have been
// compared by nothing at all.
func TestArmsDifferOnlyByPlannerMode(t *testing.T) {
	membership, tupleLeaves := bcInvMembershipFromRegistry(t)
	expanded := ExpandMembership(membership, tupleLeaves)
	if len(expanded) == 0 {
		t.Fatal("parsed no bc_inv membership")
	}

	// One value per member, distinct so any silent substitution shows up.
	values := map[string]string{}
	for i, m := range expanded {
		values[m] = fmt.Sprintf("v%03d", i)
	}
	base, err := NewBCInvariantTuple(expanded, values)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("two arms agreeing at every leaf are accepted", func(t *testing.T) {
		other, err := NewBCInvariantTuple(expanded, values)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidatePairInvariant(base, other); err != nil {
			t.Fatalf("identical arms were rejected: %v", err)
		}
	})

	t.Run("est_basis is not a BC-INV member — it is the treatment", func(t *testing.T) {
		for _, name := range expanded {
			if name == "est_basis" || strings.HasSuffix(name, ".est_basis") {
				t.Fatalf("%q is in the invariant membership; it is the treatment, not a control", name)
			}
		}
	})

	t.Run("the mutation matrix rejects a difference at EVERY member", func(t *testing.T) {
		// One case per bc_inv scalar leaf, enumerated by recursion rather than
		// by a literal list.
		for _, name := range expanded {
			mutated := map[string]string{}
			for k, v := range values {
				mutated[k] = v
			}
			mutated[name] = "MUTATED"
			other, err := NewBCInvariantTuple(expanded, mutated)
			if err != nil {
				t.Fatal(err)
			}
			err = ValidatePairInvariant(base, other)
			if err == nil {
				t.Errorf("a difference at bc_inv leaf %q was accepted", name)
				continue
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the rejection for %q does not name the leaf: %v", name, err)
			}
		}
	})

	t.Run("a container is expanded, never compared opaquely", func(t *testing.T) {
		// manifest.env_tuple expands via tuple_leaves; comparing it as one
		// value would hide a difference in any single leaf.
		if _, ok := tupleLeaves["manifest.env_tuple"]; !ok {
			t.Fatal("the registry no longer declares manifest.env_tuple's leaves")
		}
		for _, m := range expanded {
			if m == "manifest.env_tuple" {
				t.Error("manifest.env_tuple survived expansion as an opaque member")
			}
		}
		var found int
		for _, m := range expanded {
			if strings.HasPrefix(m, "manifest.env_tuple.") {
				found++
			}
		}
		if found == 0 {
			t.Error("no env_tuple leaf reached the expanded membership")
		}
	})

	t.Run("the membership is the registry's, in both directions", func(t *testing.T) {
		// An extra leaf would be compared without authority; a missing one
		// would not be compared at all.
		extra := map[string]string{"not_a_member": "x"}
		for k, v := range values {
			extra[k] = v
		}
		if _, err := NewBCInvariantTuple(expanded, extra); err == nil {
			t.Error("a leaf outside the registry membership was accepted")
		}
		short := map[string]string{}
		for i, m := range expanded {
			if i == 0 {
				continue
			}
			short[m] = values[m]
		}
		if _, err := NewBCInvariantTuple(expanded, short); err == nil {
			t.Error("a tuple missing a member was accepted")
		}
	})

	t.Run("the cache subset is the declaration leaves, not the outcomes", func(t *testing.T) {
		members := map[string]bool{}
		for _, n := range expanded {
			members[n] = true
		}
		for _, outcome := range []string{
			"cache_state.dependency_cache_matched_key",
			"cache_state.dependency_cache_disposition",
		} {
			if members[outcome] {
				t.Errorf("outcome leaf %q is a whole-run cross-arm invariant; it varies legitimately between buckets", outcome)
			}
		}
		for _, decl := range []string{
			"cache_state.dependency_cache_producer",
			"cache_state.dependency_cache_mode",
			"manifest.candidate_sha",
		} {
			if !members[decl] {
				t.Errorf("%q must be a bc_inv member", decl)
			}
		}
	})
}
