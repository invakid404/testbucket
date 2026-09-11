package walltime

import (
	"testing"
)

// The gates are the whole point of the measurement, so they are tested at
// their edges: one nanosecond either side of a frozen threshold, and the
// populations the contract froze them over.

func TestAetaGates(t *testing.T) {
	inside := []AetaSample{{
		BucketID: "b1", PointNs: 100 * second, LowerNs: 90 * second, UpperNs: 110 * second, ObservedNs: 105 * second,
	}}
	for _, g := range EvaluateAeta(inside, 1) {
		if !g.Pass {
			t.Errorf("gate %s failed on a calibrated forecast: %s", g.Name, g.Observed)
		}
	}
	// An interval wide enough to be an allowance rather than a forecast.
	wide := []AetaSample{{
		BucketID: "b1", PointNs: 100 * second, LowerNs: 0, UpperNs: 200 * second, ObservedNs: 105 * second,
	}}
	failed := map[string]bool{}
	for _, g := range EvaluateAeta(wide, 1) {
		if !g.Pass {
			failed[g.Name] = true
		}
	}
	if !failed["aeta:interval-width"] {
		t.Errorf("a 200 s interval around a 100 s point passed the width limit")
	}
	// A is outside its own interval.
	outside := []AetaSample{{
		BucketID: "b1", PointNs: 100 * second, LowerNs: 95 * second, UpperNs: 105 * second, ObservedNs: 118 * second,
	}}
	failed = map[string]bool{}
	for _, g := range EvaluateAeta(outside, 1) {
		if !g.Pass {
			failed[g.Name] = true
		}
	}
	// 18 s is inside the 20 s individual limit but outside both the 10 s MAE
	// and the forecast's own interval, and either of those is disqualifying.
	if !failed["aeta:interval-contains-a"] || !failed["aeta:point-mae"] {
		t.Errorf("an 18 s miss outside the interval passed: %v", failed)
	}
	// A short population never passes, however good the numbers are.
	for _, g := range EvaluateAeta(inside, ScoredActionRows) {
		if g.Pass {
			t.Errorf("gate %s passed on 1 of %d rows", g.Name, ScoredActionRows)
		}
	}
}

// TestCampaignDecisionRule exercises the frozen five-pair rule, including the
// two ways a candidate that looks better on average still fails.
// campaignFixtureInvocations is how many invocations each fixture bucket
// measures, so the invocation-peer population is 80 rows times this.
const campaignFixtureInvocations = 2

func TestMedianIsConventional(t *testing.T) {
	// Even n takes the arithmetic mean of the two middle values, with no
	// outlier deletion and no rounding allowance.
	if got := medianNs([]int64{1, 2, 3, 4}); got != 2 {
		t.Errorf("median of 1,2,3,4 = %d, want 2 (integer mean of 2 and 3)", got)
	}
	if got := medianNs([]int64{5, 1, 3}); got != 3 {
		t.Errorf("median of 1,3,5 = %d, want 3", got)
	}
}
