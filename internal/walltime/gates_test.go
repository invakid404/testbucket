package walltime

import "testing"

// The gates are the whole point of the measurement, so they are tested at
// their edges: one nanosecond either side of a frozen threshold, and the
// populations the contract froze them over.

func TestReconciliationGate(t *testing.T) {
	cases := []struct {
		name     string
		deltas   []int64
		expected int
		pass     bool
	}{
		{name: "well inside both limits", deltas: []int64{2 * millisecond, -3 * millisecond}, expected: 0, pass: true},
		{name: "MAE exactly at the limit", deltas: []int64{ReconMAELimit, ReconMAELimit}, expected: 0, pass: true},
		{name: "MAE one nanosecond over", deltas: []int64{ReconMAELimit + 1, ReconMAELimit + 1}, expected: 0, pass: false},
		{name: "mean passes but one error does not", deltas: []int64{0, 0, 0, ReconMaxLimit + 1}, expected: 0, pass: false},
		{name: "an individual error exactly at the limit", deltas: []int64{ReconMaxLimit, 0, 0, 0}, expected: 0, pass: true},
		{name: "no observation is not a pass", deltas: nil, expected: 0, pass: false},
		{name: "a short population is incomplete, not smaller", deltas: []int64{millisecond}, expected: 80, pass: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Reconciliation{Level: LevelAction, Deltas: tc.deltas}
			got := r.Gate(tc.expected)
			if got.Pass != tc.pass {
				t.Errorf("pass = %v, want %v (%s; %s)", got.Pass, tc.pass, got.Observed, got.Detail)
			}
		})
	}
}

func TestPredictorGates(t *testing.T) {
	// Two invocations, each predicted 1 s short: inside the 5 s invocation MAE
	// and the 10 s individual limit.
	ok := []PredictorSample{
		{InvocationSeq: 0, BucketIndex: 1, PredictedNs: 9 * second, ObservedNs: 10 * second},
		{InvocationSeq: 1, BucketIndex: 1, PredictedNs: 4 * second, ObservedNs: 5 * second},
	}
	for _, g := range EvaluatePredictor(ok) {
		if !g.Pass {
			t.Errorf("gate %s failed on a well-predicted bucket: %s", g.Name, g.Observed)
		}
	}
	// One badly predicted invocation fails the individual limit even though
	// the mean would survive.
	bad := append(append([]PredictorSample(nil), ok...),
		PredictorSample{InvocationSeq: 2, BucketIndex: 1, PredictedNs: 1 * second, ObservedNs: 12 * second})
	failed := map[string]bool{}
	for _, g := range EvaluatePredictor(bad) {
		if !g.Pass {
			failed[g.Name] = true
		}
	}
	if !failed["predictor:invocation-max"] {
		t.Errorf("an 11 s individual error passed the 10 s limit")
	}
	// No projection is not a pass.
	for _, g := range EvaluatePredictor(nil) {
		if g.Pass {
			t.Errorf("gate %s passed with no sample", g.Name)
		}
	}
}

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
func TestCampaignDecisionRule(t *testing.T) {
	// The baseline is imbalanced (one long bucket, three short); the candidate
	// is even. That is the shape the campaign is meant to reward: a smaller
	// makespan AND a smaller makespan-over-median.
	pair := func(bmax, cmax int64) CampaignPair {
		b := CampaignRun{RunID: "b", ActionNs: []int64{bmax, bmax - 10*second, bmax - 20*second, bmax - 30*second}}
		c := CampaignRun{RunID: "c", ActionNs: []int64{cmax, cmax, cmax, cmax}}
		return CampaignPair{Baseline: b, Candidate: c}
	}
	good := []CampaignPair{
		pair(100*second, 88*second), pair(102*second, 90*second), pair(99*second, 87*second),
		pair(101*second, 89*second), pair(100*second, 88*second),
	}
	for _, g := range EvaluateCampaign(good) {
		if !g.Pass {
			t.Errorf("gate %s failed on a clearly improving campaign: %s", g.Name, g.Observed)
		}
	}
	// Four pairs is not a smaller campaign; it is an incomplete one.
	for _, g := range EvaluateCampaign(good[:4]) {
		if g.Pass {
			t.Errorf("gate %s passed on 4 of %d pairs", g.Name, CampaignPairs)
		}
	}
	// A single regressing tail fails even with a healthy median.
	tail := append([]CampaignPair(nil), good...)
	tail[4] = pair(100*second, 115*second)
	failed := map[string]bool{}
	for _, g := range EvaluateCampaign(tail) {
		if !g.Pass {
			failed[g.Name] = true
		}
	}
	if !failed["campaign:tail-non-regression"] {
		t.Errorf("a 1.15 regression passed the tail gate")
	}
}

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
