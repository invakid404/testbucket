package walltime

import (
	"fmt"
	"math"
	"sort"
	"strings"
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
	// CampaignPairs is the number of randomized baseline/candidate pairs.
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

// Reconciliation is one level's like-for-like peer/trace agreement.
type Reconciliation struct {
	Level Level `json:"level"`
	// Deltas are signed trace-minus-peer differences in nanoseconds, retained
	// individually so a later reader can re-derive the statistic rather than
	// trust it.
	Deltas []int64 `json:"deltas_ns"`
}

// MAE is the mean absolute error in nanoseconds.
func (r Reconciliation) MAE() int64 {
	if len(r.Deltas) == 0 {
		return 0
	}
	var sum int64
	for _, d := range r.Deltas {
		sum += abs64(d)
	}
	return sum / int64(len(r.Deltas))
}

// Max is the largest absolute error in nanoseconds.
func (r Reconciliation) Max() int64 {
	var m int64
	for _, d := range r.Deltas {
		if a := abs64(d); a > m {
			m = a
		}
	}
	return m
}

// Gate evaluates the 50 ms / 100 ms reconciliation for one level. expected is
// the frozen population size, or 0 when the level has no fixed one (every
// actual invocation counts, however many there are).
func (r Reconciliation) Gate(expected int) GateResult {
	res := GateResult{
		Name:       fmt.Sprintf("reconciliation:%s", r.Level),
		Scope:      ScopeRow,
		Required:   fmt.Sprintf("MAE <= %s and every |error| <= %s", dur(ReconMAELimit), dur(ReconMaxLimit)),
		Observed:   fmt.Sprintf("MAE %s, max %s", dur(r.MAE()), dur(r.Max())),
		Population: len(r.Deltas),
		Expected:   expected,
	}
	switch {
	case len(r.Deltas) == 0:
		res.Detail = "no like-for-like peer/trace pair was observed"
	case expected > 0 && len(r.Deltas) < expected:
		res.Detail = fmt.Sprintf("population is %d of the frozen %d", len(r.Deltas), expected)
	case r.MAE() > ReconMAELimit:
		res.Detail = "mean absolute error exceeds the frozen limit"
	case r.Max() > ReconMaxLimit:
		res.Detail = "an individual absolute error exceeds the frozen limit"
	default:
		res.Pass = true
	}
	return res
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

// CampaignRun is one workflow run's per-bucket complete action times for one
// arm. Bucket rows are not independent primary samples; the run-level Amax and
// TA are.
//
// StartedAt and Terminal are not decoration. The frozen campaign spans at
// least three UTC dates within fourteen days and is intention-to-treat: a run
// that failed, was cancelled or was incomplete stays in the population as a
// non-passing row rather than being dropped, and a population with no dates
// cannot be checked for either rule.
type CampaignRun struct {
	RunID string `json:"run_id"`
	// StartedAt is the run's UTC instant, RFC3339.
	StartedAt string `json:"started_at"`
	// Terminal is the run's retained outcome: passed, failed, cancelled, ...
	Terminal string `json:"terminal"`
	// ActionNs is one complete physical A per bucket. It must hold exactly
	// BucketsPerRun entries.
	ActionNs []int64 `json:"action_ns"`
	// AetaSamples and PredictorSamples are the rows' own gate observations,
	// carried up from their verdicts so the campaign can compute the
	// population-wide means the contract requires over all eighty rows. A
	// campaign that kept only durations could not: eighty individually
	// acceptable forecasts can still miss a 10-second aggregate MAE, and
	// nothing would notice.
	AetaSamples      []AetaSample      `json:"aeta_samples,omitempty"`
	PredictorSamples []PredictorSample `json:"predictor_samples,omitempty"`
	// Invocations is, per bucket in order, how many scored invocations that
	// bucket measured. It is what makes an EMPTY predictor sample set
	// checkable: without it, "this row measured no invocations" and "this row
	// discarded its predictor evidence" are the same absence, and a campaign
	// could pass having proved nothing about Pcheck against observed V.
	Invocations []int `json:"invocations"`
	// Recon is the like-for-like peer/trace population, carried up per level.
	// The contract requires the 50 ms / 100 ms gates over ALL 80 action peers,
	// ALL 80 script peers and EVERY actual invocation peer; a campaign that
	// kept only the per-row verdicts could not compute any of the three, and
	// would report nothing rather than a failure.
	Recon []Reconciliation `json:"reconciliation,omitempty"`
	// VerdictDigests names the per-row verdict each observation came from —
	// one per bucket, in the same order — so every number in a campaign can be
	// traced back to the records and the verifier verdict that produced it. A
	// population assembled from bare durations is a spreadsheet, not evidence.
	VerdictDigests []Digest `json:"verdict_digests"`
}

// Amax is the makespan of the run: the longest complete action.
func (r CampaignRun) Amax() int64 {
	var m int64
	for _, a := range r.ActionNs {
		if a > m {
			m = a
		}
	}
	return m
}

// TA is the total action cost of the run.
func (r CampaignRun) TA() int64 {
	var s int64
	for _, a := range r.ActionNs {
		s += a
	}
	return s
}

// DA is the equality statistic: makespan over median bucket.
func (r CampaignRun) DA() float64 {
	m := medianNs(r.ActionNs)
	if m == 0 {
		return math.Inf(1)
	}
	return float64(r.Amax()) / float64(m)
}

// CampaignPair is one randomized baseline/candidate pair.
type CampaignPair struct {
	Baseline  CampaignRun `json:"baseline"`
	Candidate CampaignRun `json:"candidate"`
}

// RA is the pair's action improvement ratio.
func (p CampaignPair) RA() float64 {
	if p.Baseline.Amax() == 0 {
		return math.Inf(1)
	}
	return float64(p.Candidate.Amax()) / float64(p.Baseline.Amax())
}

// EvaluateCampaign applies the frozen decision rule to the five pairs.
//
// It is the WHOLE rule, and the population is part of it. Five pairs of
// ten-bucket runs is not "most of" a campaign of eight-bucket runs: the
// denominators differ, and a ratio taken over half the rows answers a
// different question. So the population is checked first and, when it is
// short, every downstream gate is reported incomplete rather than passing on
// the numbers it happened to be handed.
func EvaluateCampaign(pairs []CampaignPair) []GateResult {
	population := campaignPopulation(pairs)
	full := population.Pass
	var ra []float64
	var daB, daC []float64
	var taB, taC []int64
	var maxB, maxC int64
	within := 0
	tailOK := true
	for _, p := range pairs {
		r := p.RA()
		ra = append(ra, r)
		if r <= 1.00 {
			within++
		}
		if r > 1.10 {
			tailOK = false
		}
		daB, daC = append(daB, p.Baseline.DA()), append(daC, p.Candidate.DA())
		taB, taC = append(taB, p.Baseline.TA()), append(taC, p.Candidate.TA())
		if v := p.Baseline.Amax(); v > maxB {
			maxB = v
		}
		if v := p.Candidate.Amax(); v > maxC {
			maxC = v
		}
	}
	// The population-wide gates the contract defines over all eighty rows.
	// They live here and nowhere else: a single verified row cannot decide a
	// mean, and if the campaign did not compute one nothing would.
	var aeta []AetaSample
	var predictor []PredictorSample
	// ONE GROUP PER SCORED ROW, kept apart from the flat sample list.
	//
	// Flattening lost the only thing that distinguishes the eighty bucket
	// rows: which RUN each came from. Every arm re-keys its samples to the
	// bucket ordinals 0..7, so ten runs share eight ordinals and grouping by
	// ordinal produced eight groups where the contract names eighty rows. The
	// grouping is built here, where the run is still known, rather than
	// recovered afterwards from numbers that no longer identify anything.
	var predictorRows [][]PredictorSample
	var recon []Reconciliation
	invocations, rowsWithCoverage, rows := 0, 0, 0
	for _, p := range pairs {
		for _, r := range []CampaignRun{p.Baseline, p.Candidate} {
			aeta = append(aeta, r.AetaSamples...)
			predictor = append(predictor, r.PredictorSamples...)
			predictorRows = append(predictorRows, r.predictorRows()...)
			recon = append(recon, r.Recon...)
			// Coverage is counted PER ROW against that row's own measured
			// invocation count, so a population cannot be padded by one
			// heavily-instrumented bucket standing in for the rest.
			for i, n := range r.Invocations {
				rows++
				invocations += n
				if i < len(r.perBucketSamples()) && r.perBucketSamples()[i] == n {
					rowsWithCoverage++
				}
			}
		}
	}

	medRA := medianFloat(ra)
	out := []GateResult{
		population,
		{Name: "campaign:action-improvement", Scope: ScopeCampaign, Required: "median(RA) <= 0.95", Observed: fmt.Sprintf("%.4f", medRA),
			Population: len(pairs), Expected: CampaignPairs, Pass: full && medRA <= 0.95},
		{Name: "campaign:pairs-not-worse", Scope: ScopeCampaign, Required: "at least 4/5 pairs with RA <= 1.00",
			Observed: fmt.Sprintf("%d/%d", within, len(pairs)), Population: len(pairs), Expected: CampaignPairs,
			Pass: full && within >= 4},
		{Name: "campaign:tail-non-regression", Scope: ScopeCampaign, Required: "every RA <= 1.10 and max Amax[C] <= 1.05 * max Amax[B]",
			Observed:   fmt.Sprintf("tail %v, %s vs %s", tailOK, dur(maxC), dur(maxB)),
			Population: len(pairs), Expected: CampaignPairs,
			Pass: full && tailOK && float64(maxC) <= 1.05*float64(maxB)},
		{Name: "campaign:equality", Scope: ScopeCampaign, Required: "median(DA[C]) <= 0.95 * median(DA[B])",
			Observed:   fmt.Sprintf("%.4f vs %.4f", medianFloat(daC), medianFloat(daB)),
			Population: len(pairs), Expected: CampaignPairs,
			Pass: full && medianFloat(daC) <= 0.95*medianFloat(daB)},
		{Name: "campaign:total-action-cost", Scope: ScopeCampaign, Required: "median(TA[C]) <= 1.05 * median(TA[B])",
			Observed:   fmt.Sprintf("%s vs %s", dur(medianNs(taC)), dur(medianNs(taB))),
			Population: len(pairs), Expected: CampaignPairs,
			Pass: full && float64(medianNs(taC)) <= 1.05*float64(medianNs(taB))},
	}
	// Aggregate forecast and predictor accuracy over the whole population.
	// EvaluateAeta is given the frozen expected row count, so a short
	// population cannot pass it either.
	out = append(out, campaignScoped(EvaluateAeta(aeta, ScoredActionRows))...)
	out = append(out, EvaluateCampaignPredictor(predictor, predictorRows, invocations, rows, rowsWithCoverage)...)
	out = append(out, EvaluateCampaignRecon(recon, invocations)...)

	if !full {
		for i := range out {
			if out[i].Name == population.Name {
				continue
			}
			// Pass is cleared, not merely annotated. Every statistic here is
			// defined over the frozen population; computed over a shorter one
			// it answers a different question, and a gate that answered a
			// different question and reported "pass" would be the campaign
			// deciding itself.
			out[i].Pass = false
			out[i].Detail = "the population is incomplete, so this statistic answers a different question than the frozen gate"
		}
	}
	return out
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

// campaignPopulation is the gate the frozen thresholds are defined over:
// exactly five pairs, ten eligible runs, eighty complete action observations
// per arm, every run contributing exactly K rows, at least three distinct UTC
// dates, a window of at most fourteen days, and every run retained with its
// terminal state.
//
// It is a gate and not a precondition on purpose. A short campaign is a
// reportable FAIL with its own line, not an error the caller can catch and
// paper over.
func campaignPopulation(pairs []CampaignPair) GateResult {
	res := GateResult{
		Name: "campaign:population", Scope: ScopeCampaign,
		Required: fmt.Sprintf("%d pairs, %d runs, %d action rows (%d per run), >= %d UTC dates within %s, every run retained",
			CampaignPairs, CampaignPairs*2, ScoredActionRows, BucketsPerRun, CampaignDates, CampaignWindow),
		Expected: CampaignPairs,
	}
	var problems []string
	if len(pairs) != CampaignPairs {
		problems = append(problems, fmt.Sprintf("%d pairs", len(pairs)))
	}
	res.Population = len(pairs)

	rowsPerArm := map[string]int{}
	rows := 0
	dates := map[string]bool{}
	var earliest, latest time.Time
	runs := 0
	for i, p := range pairs {
		for arm, r := range map[string]CampaignRun{"baseline": p.Baseline, "candidate": p.Candidate} {
			runs++
			rows += len(r.ActionNs)
			rowsPerArm[arm] += len(r.ActionNs)
			if len(r.ActionNs) != BucketsPerRun {
				problems = append(problems, fmt.Sprintf("pair %d %s run %q contributed %d of %d buckets",
					i, arm, r.RunID, len(r.ActionNs), BucketsPerRun))
			}
			for j, ns := range r.ActionNs {
				if ns <= 0 {
					problems = append(problems, fmt.Sprintf("pair %d %s bucket %d has no positive complete action", i, arm, j))
				}
			}
			if len(r.VerdictDigests) != len(r.ActionNs) {
				problems = append(problems, fmt.Sprintf("pair %d %s run %q names %d verdict(s) for %d action row(s); every row must be traceable to the verdict that verified it",
					i, arm, r.RunID, len(r.VerdictDigests), len(r.ActionNs)))
			}
			for j, d := range r.VerdictDigests {
				if d == "" {
					problems = append(problems, fmt.Sprintf("pair %d %s bucket %d names no verdict", i, arm, j))
				}
			}
			// Intention-to-treat: a non-passing run is retained AND makes its
			// pair non-passing. It is never dropped to keep the count.
			if r.Terminal != TerminalPassed {
				problems = append(problems, fmt.Sprintf("pair %d %s run %q is terminal %q", i, arm, r.RunID, firstNonEmptyStr(r.Terminal, "unstated")))
			}
			t, err := time.Parse(time.RFC3339, r.StartedAt)
			if err != nil {
				problems = append(problems, fmt.Sprintf("pair %d %s run %q has no RFC3339 start instant", i, arm, r.RunID))
				continue
			}
			t = t.UTC()
			dates[t.Format("2006-01-02")] = true
			if earliest.IsZero() || t.Before(earliest) {
				earliest = t
			}
			if latest.IsZero() || t.After(latest) {
				latest = t
			}
		}
	}
	if runs != CampaignPairs*2 {
		problems = append(problems, fmt.Sprintf("%d eligible runs, want %d", runs, CampaignPairs*2))
	}
	if rows != ScoredActionRows {
		problems = append(problems, fmt.Sprintf("%d action rows, want %d", rows, ScoredActionRows))
	}
	if len(dates) < CampaignDates {
		problems = append(problems, fmt.Sprintf("%d distinct UTC date(s), want at least %d", len(dates), CampaignDates))
	}
	if !earliest.IsZero() && latest.Sub(earliest) > CampaignWindow {
		problems = append(problems, fmt.Sprintf("the runs span %s, longer than %s", latest.Sub(earliest), CampaignWindow))
	}
	res.Observed = fmt.Sprintf("%d pairs, %d runs, %d/%d action rows (%d baseline, %d candidate), %d UTC date(s)",
		len(pairs), runs, rows, ScoredActionRows, rowsPerArm["baseline"], rowsPerArm["candidate"], len(dates))
	if len(problems) == 0 {
		res.Pass = true
		return res
	}
	res.Detail = strings.Join(problems, "; ")
	return res
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

// perBucketSamples counts this run's retained predictor samples per bucket, in
// bucket order. It is derived rather than stored so it cannot disagree with the
// samples themselves.
// predictorRows splits this run's samples into one group per scored bucket
// row, in bucket order. Within a run the bucket ordinal does identify a row —
// it is only across runs that it stops doing so — which is why the split
// happens here, per run, and the groups are kept separate afterwards.
func (r CampaignRun) predictorRows() [][]PredictorSample {
	out := make([][]PredictorSample, len(r.Invocations))
	for _, s := range r.PredictorSamples {
		if s.BucketIndex >= 0 && s.BucketIndex < len(out) {
			out[s.BucketIndex] = append(out[s.BucketIndex], s)
		}
	}
	return out
}

func (r CampaignRun) perBucketSamples() []int {
	out := make([]int, len(r.Invocations))
	for _, s := range r.PredictorSamples {
		if s.BucketIndex >= 0 && s.BucketIndex < len(out) {
			out[s.BucketIndex]++
		}
	}
	return out
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

// EvaluateCampaignRecon runs the frozen like-for-like reconciliation over the
// three complete populations the contract names: all 80 scored action
// containment peers, all 80 scored complete-script peers, and every actual
// nested-invocation peer.
//
// Coverage is EXACT, not a minimum. "All 80" is a population, and a campaign
// that reconciled sixty of them perfectly has not reconciled the eighty it
// claims. An absent population fails rather than disappearing.
func EvaluateCampaignRecon(recon []Reconciliation, invocations int) []GateResult {
	merged := map[Level]*Reconciliation{}
	for _, r := range recon {
		m, ok := merged[r.Level]
		if !ok {
			m = &Reconciliation{Level: r.Level}
			merged[r.Level] = m
		}
		m.Deltas = append(m.Deltas, r.Deltas...)
	}
	expected := map[Level]int{
		LevelAction:     ScoredActionRows,
		LevelScript:     ScoredActionRows,
		LevelInvocation: invocations,
	}
	var out []GateResult
	for _, l := range []Level{LevelAction, LevelScript, LevelInvocation} {
		r := merged[l]
		if r == nil {
			r = &Reconciliation{Level: l}
		}
		g := r.Gate(expected[l])
		g.Name, g.Scope = "campaign:"+g.Name, ScopeCampaign
		// Gate() passes a population that merely REACHES the expected count.
		// At campaign scope the count is the population itself, so more peers
		// than the frozen population is as wrong as fewer: it means peers from
		// outside the campaign were counted in.
		if g.Pass && expected[l] > 0 && len(r.Deltas) != expected[l] {
			g.Pass = false
			g.Detail = fmt.Sprintf("population is %d, not the frozen %d", len(r.Deltas), expected[l])
		}
		if expected[l] == 0 {
			g.Pass = false
			g.Detail = "the population has no invocation peers to reconcile"
		}
		out = append(out, g)
	}
	return out
}
