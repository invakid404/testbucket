package walltime

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// The frozen numeric gates. They are constants, not configuration: a threshold
// a run can choose is a threshold a run can pass.
const (
	// ReconMAELimit and ReconMaxLimit bound the LIKE-FOR-LIKE reconciliation
	// between a trace and its own containment peer. They are deliberately not
	// applied to trace-versus-physical: the peer and the trace bracket the same
	// admission-to-verified-empty lifecycle, while the physical envelope also
	// contains real bootstrap and epilogue work, so comparing those two would
	// fail a correctly instrumented run for accounting honestly.
	ReconMAELimit = 50 * millisecond
	ReconMaxLimit = 100 * millisecond

	// PcheckInvocationMAELimit, PcheckInvocationMaxLimit and
	// PcheckBucketMAELimit bound the PREDICTOR against observed physical V.
	// Instrumentation agreement is not prediction accuracy; these are separate
	// and neither substitutes for the other.
	PcheckInvocationMAELimit = 5 * second
	PcheckInvocationMaxLimit = 10 * second
	PcheckBucketMAELimit     = 5 * second

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

// PredictorSample pairs one invocation's frozen projection with its observed
// physical duration.
type PredictorSample struct {
	InvocationSeq int   `json:"invocation_seq"`
	BucketIndex   int   `json:"bucket"`
	PredictedNs   int64 `json:"predicted_ns"`
	ObservedNs    int64 `json:"observed_ns"`
}

// EvaluatePredictor runs the Pcheck-versus-observed-V gates: invocation MAE,
// individual invocation error, and bucket MAE. All three must pass; none of
// them can be traded for instrumentation agreement.
func EvaluatePredictor(samples []PredictorSample) []GateResult {
	if len(samples) == 0 {
		return []GateResult{{
			Name: "predictor:invocation-max", Scope: ScopeRow,
			Required: fmt.Sprintf("every |error| <= %s", dur(PcheckInvocationMaxLimit)),
			Observed: "no sample", Detail: "no frozen projection was paired with an observed invocation",
		}}
	}
	invMAE, worst := invocationErrors(samples)
	// ONE ROW PER BUCKET OF THIS MEASUREMENT. In row context every sample
	// belongs to the same scored row, so the bucket index separates nothing
	// and the whole set is one bucket total — which is exactly the quantity
	// the gate is about.
	byBucket := map[int][]PredictorSample{}
	var order []int
	for _, s := range samples {
		if _, seen := byBucket[s.BucketIndex]; !seen {
			order = append(order, s.BucketIndex)
		}
		byBucket[s.BucketIndex] = append(byBucket[s.BucketIndex], s)
	}
	groups := make([][]PredictorSample, 0, len(order))
	for _, b := range order {
		groups = append(groups, byBucket[b])
	}
	bucketMAE, bucketRows := bucketMeanAbsoluteError(groups)

	return []GateResult{
		{
			Name: "predictor:invocation-mae", Scope: ScopeCampaign, Required: "<= " + dur(PcheckInvocationMAELimit),
			Observed: dur(invMAE), Population: len(samples), Pass: invMAE <= PcheckInvocationMAELimit,
		},
		{
			Name: "predictor:invocation-max", Scope: ScopeRow, Required: "<= " + dur(PcheckInvocationMaxLimit),
			Observed: dur(worst), Population: len(samples), Pass: worst <= PcheckInvocationMaxLimit,
		},
		{
			Name: "predictor:bucket-mae", Scope: ScopeRow, Required: "<= " + dur(PcheckBucketMAELimit),
			Observed: dur(bucketMAE), Population: bucketRows, Expected: bucketRows, Pass: bucketMAE <= PcheckBucketMAELimit,
		},
	}
}

// invocationErrors is the INVOCATION population: the mean absolute error over
// every paired projection and observation, and the worst single one. Both are
// unchanged by the bucket rule below; they answer a different question and the
// contract names them separately.
func invocationErrors(samples []PredictorSample) (mae, worst int64) {
	var sum int64
	for _, s := range samples {
		e := abs64(s.PredictedNs - s.ObservedNs)
		sum += e
		if e > worst {
			worst = e
		}
	}
	if len(samples) == 0 {
		return 0, 0
	}
	return sum / int64(len(samples)), worst
}

// bucketMeanAbsoluteError is the frozen bucket statistic: for each scored
// (run, bucket) row, the error of that row's AGGREGATE projection against its
// aggregate observation, averaged over the rows.
//
// SUMMATION PRECEDES THE ABSOLUTE VALUE, and that is the whole content of the
// gate. What used to be computed took the absolute value of each invocation
// error first, grouped those by bucket ordinal and averaged twice — which is
// an invocation-error statistic wearing a bucket's name. With equal invocation
// counts it reproduces the invocation MAE exactly, so the contract's third
// independent gate silently duplicated its first.
//
// The difference is not academic. Six invocations each under-predicted by one
// second give an invocation MAE of one second and a maximum of one second,
// both comfortably inside their limits, while the bucket they belong to is
// wrong by six. Averaging absolute invocation errors reports one second; the
// bucket's projection is out by six, and it is the bucket total the contract
// bounds. A systematic small under-prediction is enough to authorize a release
// whose genuine campaign misses every one of its eighty bucket totals.
func bucketMeanAbsoluteError(groups [][]PredictorSample) (mae int64, rows int) {
	var sum int64
	for _, g := range groups {
		if len(g) == 0 {
			// A row that retains no sample is not a bucket error of zero; it
			// is an absent observation, which the coverage gate reports.
			continue
		}
		var predicted, observed int64
		for _, s := range g {
			// SIGNED, both of them, all the way to the row total. Discarding
			// the sign per invocation is what turned this into a different
			// statistic.
			predicted += s.PredictedNs
			observed += s.ObservedNs
		}
		sum += abs64(predicted - observed)
		rows++
	}
	if rows == 0 {
		return 0, 0
	}
	return sum / int64(rows), rows
}

// RowScope selects the gates one verified row decides for itself. The rest are
// campaign-scope and belong to `wall campaign` over the full population; a
// per-run verdict reports them without ever passing them.
func RowScope(gates []GateResult) []GateResult {
	var out []GateResult
	for _, g := range gates {
		if g.Scope == ScopeRow {
			out = append(out, g)
		}
	}
	return out
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

// campaignScoped keeps only the gates the full population decides, and marks
// them as such. The row-scope members of those sets were already decided by
// each row's own verifier verdict; re-deciding them here would double-count a
// judgement that has already been made.
func campaignScoped(gates []GateResult) []GateResult {
	var out []GateResult
	for _, g := range gates {
		if g.Scope != ScopeCampaign {
			continue
		}
		out = append(out, g)
	}
	return out
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

func medianFloat(v []float64) float64 {
	if len(v) == 0 {
		return math.Inf(1)
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
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

// EvaluateCampaignPredictor runs the frozen Pcheck-versus-observed-V gates
// over the WHOLE population, and first checks that the population is there.
//
// The coverage gate is the load-bearing one. `EvaluatePredictor` on an empty
// sample set returns a row-scope finding, and campaign scope discards row-scope
// gates — so a campaign whose eighty verdicts all omitted their predictor
// samples used to report no predictor gate at all and pass. Absence has to be
// a failure, and it has to be distinguishable from a row that legitimately
// measured no invocations, which is why each row carries its own invocation
// count.
func EvaluateCampaignPredictor(samples []PredictorSample, predictorRows [][]PredictorSample, invocations, rows, covered int) []GateResult {
	coverage := GateResult{
		Name: "predictor:coverage", Scope: ScopeCampaign,
		Required:   "one Pcheck/observed-V sample per scored invocation, in every row",
		Observed:   fmt.Sprintf("%d sample(s) for %d invocation(s) across %d row(s)", len(samples), invocations, rows),
		Population: covered, Expected: rows,
	}
	switch {
	case rows == 0:
		coverage.Detail = "the population retains no rows, so predictor coverage cannot be established"
	case covered < rows:
		coverage.Detail = fmt.Sprintf("%d of %d row(s) retain a sample for every invocation they measured; the rest prove nothing about Pcheck against observed V", covered, rows)
	case len(samples) != invocations:
		coverage.Detail = fmt.Sprintf("the population holds %d sample(s) for %d scored invocation(s)", len(samples), invocations)
	case invocations == 0:
		// Every row measured zero invocations and every row says so. That is
		// consistent and it is not predictor evidence, so it does not pass.
		coverage.Detail = "no row measured any invocation, so the Pcheck-versus-observed-V gates have no population"
	default:
		coverage.Pass = true
	}

	out := []GateResult{coverage}
	for _, g := range EvaluatePredictor(samples) {
		// Every predictor gate is decided at CAMPAIGN scope here: the contract
		// states invocation MAE, individual invocation error and bucket MAE
		// over the campaign population, and a row-scope copy of them would be
		// filtered out of a campaign verdict.
		g.Scope = ScopeCampaign
		// THE BUCKET GATE COUNTS BUCKET ROWS, not invocations. Overwriting its
		// expected population with the invocation count reported that eighty
		// bucket totals were four hundred and eighty of something, which is
		// how a population of eight passed a gate the contract sizes at
		// eighty.
		if g.Name == "predictor:bucket-mae" {
			bucketMAE, populated := bucketMeanAbsoluteError(predictorRows)
			g.Observed, g.Population, g.Expected = dur(bucketMAE), populated, rows
			g.Pass = bucketMAE <= PcheckBucketMAELimit && populated == rows
			if populated != rows {
				g.Detail = fmt.Sprintf(
					"%d of %d scored bucket row(s) retain a projection to compare; a bucket mean over fewer rows is not the frozen statistic", populated, rows)
			}
		} else {
			g.Expected = invocations
		}
		if !coverage.Pass {
			g.Pass = false
			g.Detail = firstNonEmptyStr(g.Detail, "predictor coverage is incomplete, so this statistic answers a different question than the frozen gate")
		}
		out = append(out, g)
	}
	return out
}
