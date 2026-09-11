package walltime

import (
	"fmt"
	"sort"
	"time"
)

// The frozen numeric gates. They are constants, not configuration: a threshold
// a run can choose is a threshold a run can pass.
const (
	// FIVE THRESHOLDS ARE REMOVED with the machinery they bounded: two that
	// reconciled a trace against its own containment peer, and three that bounded
	// a frozen predictor projection against observed V. There is no peer, no
	// trace and no projection. Nothing in production read any of them, and a
	// threshold naming a quantity the product does not have reads as a gate
	// somebody could still apply. The symbols are named in the semantic-removal
	// control that keeps them out, not here.

	// AetaMAELimit and AetaMaxLimit bound the user-facing action forecast
	// against observed A. AetaMinWidth and AetaWidthFraction bound how wide an
	// interval may be to count as a forecast rather than an allowance.
	AetaMAELimit      = 10 * second
	AetaMaxLimit      = 20 * second
	AetaMinWidth      = 20 * second
	AetaWidthFraction = 0.20

	// MaterialThreshold is the duration above which a physical component must
	// be forecast or bounded by name. ResidualComponentLimit,
	// ResidualTotalLimit and ResidualFraction bound what may stay unnamed.
	MaterialThreshold      = 500 * millisecond
	ResidualComponentLimit = 500 * millisecond
	ResidualTotalLimit     = 1000 * millisecond
	ResidualFraction       = 0.005

	// ScoredActionRows is the campaign's action population: ten eligible
	// workflow runs — five pairs, two arms — of eight buckets each. A
	// statistic over fewer rows is not a smaller sample, it is an incomplete
	// one, and EvaluateCampaign enforces the count rather than trusting the
	// caller to supply it.
	ScoredActionRows = 80
	// CampaignPairs is the number of precommitted baseline/candidate pairs.
	//
	// The sequence is a FIXED COUNTERBALANCED one declared before run 1
	// (§0.2, §19.5): pairs 1, 3, 5 run B->C and pairs 2, 4 run C->B.
	// Counterbalancing is a control, not a draw — the campaign is not
	// randomized, and §22 test 18 fails on any shipped string that calls it
	// randomized or claims a seed, draw or shuffle in a campaign-order
	// context.
	CampaignPairs = 5
	// BucketsPerRun is K for the scored profile. Every eligible run
	// contributes exactly this many action observations; a run with fewer is
	// an incomplete run, not a smaller one.
	BucketsPerRun = 8
	// CampaignDates is the minimum number of distinct UTC dates the five pairs
	// must span, and CampaignWindow the maximum span between the first and
	// last run. Both exist so a campaign cannot be five runs of one hour on
	// one machine's quiet afternoon.
	CampaignDates  = 3
	CampaignWindow = 14 * 24 * time.Hour
)

// Durations in nanoseconds, so every comparison in this package is exact
// integer arithmetic rather than float seconds that round at the gate.
const (
	millisecond = int64(1_000_000)
	second      = int64(1_000_000_000)
)

// Gate scopes. The distinction is load-bearing: an individual error limit can
// be decided from one row, and a mean over eighty rows cannot. Reporting both
// against one run is how a single measurement ends up claiming a population
// statistic it never had.
const (
	// ScopeRow is a gate one verified row decides for itself.
	ScopeRow = "row"
	// ScopeCampaign is a gate only the full frozen population decides. A
	// per-run verdict reports it with its population and never passes it.
	ScopeCampaign = "campaign"
)

// GateResult is one frozen threshold's outcome. Population is carried with the
// result because "MAE 12 ms" over three rows is not the gate the contract
// froze over eighty.
type GateResult struct {
	Name string `json:"name"`
	// Scope says which population decides this gate.
	Scope      string `json:"scope,omitempty"`
	Required   string `json:"required"`
	Observed   string `json:"observed"`
	Population int    `json:"population"`
	// Expected is the population the frozen gate is defined over, when there
	// is one. A short population never passes: it is reported as incomplete.
	Expected int    `json:"expected_population,omitempty"`
	Pass     bool   `json:"pass"`
	Detail   string `json:"detail,omitempty"`
}

// AetaSample pairs one bucket's pre-action forecast with the observed complete
// physical action.
type AetaSample struct {
	BucketID   string `json:"bucket_id"`
	PointNs    int64  `json:"point_ns"`
	LowerNs    int64  `json:"lower_ns"`
	UpperNs    int64  `json:"upper_ns"`
	ObservedNs int64  `json:"observed_ns"`
}

// EvaluateAeta runs the action-level ETA calibration gates: point MAE, worst
// point error, containment of A in its own finite interval, and interval
// width. expected is the frozen action population.
func EvaluateAeta(samples []AetaSample, expected int) []GateResult {
	if len(samples) == 0 {
		return []GateResult{{
			Name: "aeta:point-max", Scope: ScopeRow, Required: "<= " + dur(AetaMaxLimit), Observed: "no sample",
			Expected: expected, Detail: "no pre-action forecast was instantiated",
		}}
	}
	var sum, worst int64
	contained, widthOK := 0, 0
	for _, s := range samples {
		e := abs64(s.PointNs - s.ObservedNs)
		sum += e
		if e > worst {
			worst = e
		}
		if s.UpperNs > s.LowerNs || s.UpperNs == s.LowerNs {
			if s.ObservedNs >= s.LowerNs && s.ObservedNs <= s.UpperNs {
				contained++
			}
		}
		limit := AetaMinWidth
		if f := int64(AetaWidthFraction * float64(s.PointNs)); f > limit {
			limit = f
		}
		if s.UpperNs-s.LowerNs <= limit {
			widthOK++
		}
	}
	mae := sum / int64(len(samples))
	full := expected == 0 || len(samples) >= expected
	return []GateResult{
		{Name: "aeta:point-mae", Scope: ScopeCampaign, Required: "<= " + dur(AetaMAELimit), Observed: dur(mae),
			Population: len(samples), Expected: expected, Pass: full && mae <= AetaMAELimit},
		{Name: "aeta:point-max", Scope: ScopeRow, Required: "<= " + dur(AetaMaxLimit), Observed: dur(worst),
			Population: len(samples), Expected: expected, Pass: full && worst <= AetaMaxLimit},
		{Name: "aeta:interval-contains-a", Scope: ScopeRow, Required: "every A within its own [L,U]",
			Observed: fmt.Sprintf("%d/%d", contained, len(samples)), Population: len(samples),
			Expected: expected, Pass: full && contained == len(samples)},
		{Name: "aeta:interval-width", Scope: ScopeRow, Required: fmt.Sprintf("<= max(%s, %.2f * point)", dur(AetaMinWidth), AetaWidthFraction),
			Observed: fmt.Sprintf("%d/%d within limit", widthOK, len(samples)), Population: len(samples),
			Expected: expected, Pass: full && widthOK == len(samples)},
	}
}

// medianNs is the conventional even-n arithmetic mean of the two middle
// values, as the contract specifies. No outlier deletion, no rounding
// allowance.
func medianNs(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// dur renders a nanosecond count the way the gates are written, so a report
// line can be compared to the contract without arithmetic.
func dur(ns int64) string {
	switch {
	case ns == 0:
		return "0"
	case abs64(ns) < millisecond:
		return fmt.Sprintf("%.3f ms", float64(ns)/float64(millisecond))
	case abs64(ns) < second:
		return fmt.Sprintf("%.1f ms", float64(ns)/float64(millisecond))
	default:
		return fmt.Sprintf("%.3f s", float64(ns)/float64(second))
	}
}
