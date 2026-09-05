package walltime

import (
	"fmt"
	"strings"
	"testing"
)

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
// campaignFixtureInvocations is how many invocations each fixture bucket
// measures, so the invocation-peer population is 80 rows times this.
const campaignFixtureInvocations = 2

func TestCampaignDecisionRule(t *testing.T) {
	// The baseline is imbalanced (one long bucket, the rest shorter); the
	// candidate is even. That is the shape the campaign is meant to reward: a
	// smaller makespan AND a smaller makespan-over-median.
	//
	// Every run carries exactly K=8 buckets, a UTC start instant and a
	// terminal state, because the population is part of the frozen rule and a
	// test that asserted PASS on half a campaign would be asserting that the
	// rule does not apply.
	buckets := func(top int64, step int64) []int64 {
		out := make([]int64, BucketsPerRun)
		for i := range out {
			out[i] = top - int64(i)*step
		}
		return out
	}
	// One verdict per action row: a campaign number that cannot be traced back
	// to the records that produced it is not evidence.
	verdicts := func(prefix string) []Digest {
		out := make([]Digest, BucketsPerRun)
		for i := range out {
			out[i] = Digest(fmt.Sprintf("sha256:%s-%d", prefix, i))
		}
		return out
	}
	// A run carries the evidence a campaign is required to aggregate, not only
	// its durations: how many invocations each bucket measured, one
	// Pcheck/observed-V sample per invocation, and the like-for-like peer
	// deltas. A fixture that carried only durations could assert PASS on a
	// campaign that proved nothing about prediction or reconciliation, which
	// is exactly the hole these gates close.
	run := func(prefix string, action []int64, start string) CampaignRun {
		r := CampaignRun{RunID: prefix, StartedAt: start, Terminal: TerminalPassed,
			ActionNs: action, VerdictDigests: verdicts(prefix)}
		for b, ns := range action {
			r.Invocations = append(r.Invocations, campaignFixtureInvocations)
			for i := 0; i < campaignFixtureInvocations; i++ {
				each := ns / (campaignFixtureInvocations + 2)
				r.PredictorSamples = append(r.PredictorSamples, PredictorSample{
					InvocationSeq: i, BucketIndex: b,
					PredictedNs: each + 100*millisecond, ObservedNs: each,
				})
			}
			r.Recon = append(r.Recon,
				Reconciliation{Level: LevelAction, Deltas: []int64{3 * millisecond}},
				Reconciliation{Level: LevelScript, Deltas: []int64{-2 * millisecond}},
				Reconciliation{Level: LevelInvocation, Deltas: []int64{1 * millisecond, 2 * millisecond}},
			)
		}
		return r
	}
	day := 0
	pair := func(bmax, cmax int64) CampaignPair {
		// Three distinct UTC dates inside the fourteen-day window.
		start := fmt.Sprintf("2026-09-%02dT0%d:00:00Z", 1+day%3, day%9)
		day++
		return CampaignPair{
			Baseline:  run("b", buckets(bmax, 5*second), start),
			Candidate: run("c", buckets(cmax, 0), start),
		}
	}
	// The candidate is flat and slightly cheaper in total: a real improvement
	// buys a smaller makespan without buying it with more total work, which is
	// exactly what campaign:total-action-cost is there to stop.
	good := []CampaignPair{
		pair(100*second, 84*second), pair(102*second, 86*second), pair(99*second, 83*second),
		pair(101*second, 85*second), pair(100*second, 84*second),
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

	// And neither is five pairs of half-sized runs. This is the exact
	// adversarial replay that got past the calculator before: 5 pairs, 40
	// action rows per arm, every ratio healthy.
	half := make([]CampaignPair, len(good))
	for i, p := range good {
		p.Baseline.ActionNs = p.Baseline.ActionNs[:BucketsPerRun/2]
		p.Candidate.ActionNs = p.Candidate.ActionNs[:BucketsPerRun/2]
		half[i] = p
	}
	sawPopulation := false
	for _, g := range EvaluateCampaign(half) {
		if g.Pass {
			t.Errorf("gate %s passed on %d of %d action rows", g.Name, ScoredActionRows/2, ScoredActionRows)
		}
		if g.Name == "campaign:population" {
			sawPopulation = true
			if !strings.Contains(g.Detail, "action rows") {
				t.Errorf("the population gate does not name the short row count: %s", g.Detail)
			}
		}
	}
	if !sawPopulation {
		t.Errorf("EvaluateCampaign reported no population gate")
	}

	// Each remaining population rule, one at a time.
	for _, tc := range []struct {
		name string
		edit func([]CampaignPair) []CampaignPair
		want string
	}{
		{"a run on one UTC date only", func(p []CampaignPair) []CampaignPair {
			for i := range p {
				p[i].Baseline.StartedAt, p[i].Candidate.StartedAt = "2026-09-01T00:00:00Z", "2026-09-01T01:00:00Z"
			}
			return p
		}, "UTC date"},
		{"runs spread beyond the window", func(p []CampaignPair) []CampaignPair {
			p[4].Baseline.StartedAt, p[4].Candidate.StartedAt = "2026-10-30T00:00:00Z", "2026-10-30T01:00:00Z"
			return p
		}, "span"},
		{"a cancelled run is retained and non-passing", func(p []CampaignPair) []CampaignPair {
			p[2].Candidate.Terminal = TerminalCancelled
			return p
		}, "terminal"},
		{"a run with no start instant", func(p []CampaignPair) []CampaignPair {
			p[1].Baseline.StartedAt = ""
			return p
		}, "RFC3339"},
		{"an action row that names no verdict", func(p []CampaignPair) []CampaignPair {
			p[3].Candidate.VerdictDigests = p[3].Candidate.VerdictDigests[:BucketsPerRun-1]
			return p
		}, "traceable to the verdict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edited := tc.edit(clonePairs(good))
			gates := EvaluateCampaign(edited)
			if gates[0].Name != "campaign:population" {
				t.Fatalf("the population gate is not first: %s", gates[0].Name)
			}
			if gates[0].Pass {
				t.Fatalf("the population gate passed")
			}
			if !strings.Contains(gates[0].Detail, tc.want) {
				t.Errorf("detail %q does not mention %q", gates[0].Detail, tc.want)
			}
		})
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

// clonePairs deep-copies a campaign so one edit does not leak into the next
// case.
func clonePairs(in []CampaignPair) []CampaignPair {
	out := make([]CampaignPair, len(in))
	for i, p := range in {
		p.Baseline.ActionNs = append([]int64(nil), p.Baseline.ActionNs...)
		p.Candidate.ActionNs = append([]int64(nil), p.Candidate.ActionNs...)
		p.Baseline.VerdictDigests = append([]Digest(nil), p.Baseline.VerdictDigests...)
		p.Candidate.VerdictDigests = append([]Digest(nil), p.Candidate.VerdictDigests...)
		out[i] = p
	}
	return out
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

// THE BUCKET GATE BOUNDS BUCKET TOTALS, not invocation errors.
//
// `Pcheck[b,j]` is a per-invocation projection, and the contract names three
// independent predictor gates over it: invocation MAE, worst single invocation
// error, and BUCKET MAE. The third is the error of each bucket's aggregate
// projection against its aggregate observation —
// abs(sum_j Pcheck[b,j] - sum_j V[b,j]) — averaged over the scored rows.
//
// Summation precedes the absolute value, and that is the entire content of it.
// Taking the absolute value per invocation first and averaging by bucket is an
// invocation-error statistic wearing a bucket's name: with equal invocation
// counts it reproduces the invocation MAE exactly, so the contract's third
// gate silently duplicated its first and bounded nothing new.
func TestBucketMAEIsTheErrorOfTheBucketTotal(t *testing.T) {
	bucket := func(b int, pairs ...[2]int64) []PredictorSample {
		var out []PredictorSample
		for i, p := range pairs {
			out = append(out, PredictorSample{InvocationSeq: i, BucketIndex: b, PredictedNs: p[0], ObservedNs: p[1]})
		}
		return out
	}
	gate := func(t *testing.T, samples []PredictorSample) GateResult {
		t.Helper()
		for _, g := range EvaluatePredictor(samples) {
			if g.Name == "predictor:bucket-mae" {
				return g
			}
		}
		t.Fatal("predictor:bucket-mae was not emitted")
		return GateResult{}
	}

	// ACCUMULATION. Six one-second misses in one direction are a six-second
	// bucket, however small each one is.
	t.Run("same-direction misses accumulate into the bucket total", func(t *testing.T) {
		var s []PredictorSample
		for i := 0; i < 6; i++ {
			s = append(s, PredictorSample{InvocationSeq: i, BucketIndex: 0, PredictedNs: 1 * second, ObservedNs: 2 * second})
		}
		g := gate(t, s)
		if g.Pass {
			t.Errorf("a bucket whose total is out by 6s passed as %s", g.Observed)
		}
		if g.Observed != dur(6*second) {
			t.Errorf("bucket error is %s, want %s", g.Observed, dur(6*second))
		}
		// And the invocation gates, which bound a different quantity, are
		// untouched: each individual miss really is only one second.
		for _, other := range EvaluatePredictor(s) {
			if other.Name != "predictor:bucket-mae" && !other.Pass {
				t.Errorf("%s failed on one-second invocation errors: %s", other.Name, other.Observed)
			}
		}
	})

	// SIGN. Opposing errors genuinely cancel in the aggregate, because the
	// bucket's total projection is then correct. This is the case the frozen
	// definition chooses, and asserting it stops the rule being "re-derive the
	// invocation statistic more expensively".
	t.Run("opposing invocation errors cancel in the bucket total", func(t *testing.T) {
		g := gate(t, bucket(0, [2]int64{10 * second, 4 * second}, [2]int64{1 * second, 7 * second}))
		if !g.Pass || g.Observed != dur(0) {
			t.Errorf("a bucket whose over- and under-prediction cancel reported %s, want 0", g.Observed)
		}
	})

	// THRESHOLD EDGE, both sides of it.
	for _, tc := range []struct {
		name  string
		error int64
		pass  bool
	}{
		{"exactly at the limit", PcheckBucketMAELimit, true},
		{"one nanosecond past the limit", PcheckBucketMAELimit + 1, false},
	} {
		t.Run("a bucket "+tc.name, func(t *testing.T) {
			g := gate(t, bucket(0, [2]int64{0, tc.error}))
			if g.Pass != tc.pass {
				t.Errorf("bucket error %s: pass=%v, want %v", g.Observed, g.Pass, tc.pass)
			}
		})
	}

	// UNEQUAL COUNTS. A row with more invocations must not be diluted: the
	// statistic is the row's total error, not its per-invocation average.
	t.Run("a bucket with more invocations is not diluted", func(t *testing.T) {
		var s []PredictorSample
		for i := 0; i < 12; i++ {
			s = append(s, PredictorSample{InvocationSeq: i, BucketIndex: 0, PredictedNs: 1 * second, ObservedNs: 1*second + 500*millisecond})
		}
		g := gate(t, s)
		if g.Pass {
			t.Errorf("twelve half-second misses (6s total) passed as %s", g.Observed)
		}
	})

	// ABSENCE. No sample is not a bucket error of zero.
	t.Run("no population is not a passing bucket", func(t *testing.T) {
		for _, g := range EvaluatePredictor(nil) {
			if g.Name == "predictor:bucket-mae" {
				t.Error("an empty population emitted a bucket verdict")
			}
		}
	})
}

// AND EIGHTY ROWS ARE EIGHTY ROWS, not eight ordinals.
//
// Every arm re-keys its samples to the bucket ordinals 0..7, so across ten
// runs the ordinals repeat. Grouping the campaign's samples by ordinal
// produced eight groups where the contract names eighty scored rows — and the
// gate reported an expected population of 480 invocations, which is neither.
// Two distinct rows that share ordinal 0 must be two bucket errors.
func TestTheCampaignBucketPopulationIsScoredRowsNotOrdinals(t *testing.T) {
	// Two rows, both ordinal 0, from two different runs. Each is out by 6s.
	row := func() []PredictorSample {
		var out []PredictorSample
		for i := 0; i < 6; i++ {
			out = append(out, PredictorSample{InvocationSeq: i, BucketIndex: 0, PredictedNs: 1 * second, ObservedNs: 2 * second})
		}
		return out
	}
	flat := append(row(), row()...)
	rows := [][]PredictorSample{row(), row()}

	var found bool
	for _, g := range EvaluateCampaignPredictor(flat, rows, 12, 2, 2) {
		if g.Name != "predictor:bucket-mae" {
			continue
		}
		found = true
		if g.Population != 2 || g.Expected != 2 {
			t.Errorf("the bucket gate counted population %d of %d, want 2 of 2; two distinct rows sharing an ordinal are two rows",
				g.Population, g.Expected)
		}
		if g.Pass {
			t.Errorf("two rows each out by 6s passed as %s", g.Observed)
		}
	}
	if !found {
		t.Fatal("predictor:bucket-mae was not emitted at campaign scope")
	}

	// A SHORT POPULATION cannot pass by averaging the rows it does have.
	t.Run("a bucket mean over fewer rows than the population is not the frozen statistic", func(t *testing.T) {
		good := [][]PredictorSample{{{BucketIndex: 0, PredictedNs: second, ObservedNs: second}}}
		for _, g := range EvaluateCampaignPredictor(good[0], good, 1, 2, 1) {
			if g.Name == "predictor:bucket-mae" && g.Pass {
				t.Errorf("a bucket mean over 1 of 2 scored rows passed as %s", g.Observed)
			}
		}
	})
}
