package walltime

import (
	"fmt"
	"github.com/invakid404/testbucket/internal/nsmath"
	"sort"
)

// Calibration outcomes of contract §6.7. The proposer is TOTAL over exactly
// these three; it never returns a fourth value and never returns silently.
type CalibrationOutcome string

const (
	CalibrationSufficient             CalibrationOutcome = "sufficient"
	CalibrationStructurallyInfeasible CalibrationOutcome = "structurally_infeasible"
	// CalibrationNotFoundWithinBudget says exactly what happened. The earlier
	// name INFEASIBLE asserted that the candidate universe CANNOT produce rank
	// 4, which failing a few heuristic layouts does not prove — a layout
	// outside the list may succeed.
	CalibrationNotFoundWithinBudget CalibrationOutcome = "not_found_within_budget"
)

// DefaultCalibrationMaxPlans is §6.7's default N. N is an integer N >= 1 with
// no maximum: the layout generator is partial, but the proposer is total, so
// every N >= 1 has exactly one defined outcome.
const DefaultCalibrationMaxPlans = 4

// CalibrationUnit is a candidate unit in the proposer's universe.
type CalibrationUnit struct {
	ID      string
	BaseNs  int64
	IsSlice bool
}

// Packer packs residual units over a set of bucket slots by the same
// deterministic Karmarkar–Karp the reporter basis uses. It is injected so the
// proposer does not reach across into the core planner, matching the
// core/adapter separation §6.5 draws.
type Packer func(units []CalibrationUnit, slots int) [][]CalibrationUnit

// CalibrationEvidence is the `testbucket.calibration-evidence/v1` document.
//
// IT IS THE DOCUMENT §17.3a SPECIFIES, not a subset of it. Seven of the fields
// below were absent entirely — the comparability key, the proposed plan
// digests, the three rank diagnostics, the two column-content summaries and the
// generation instant — so the proposal could not say which population it
// belonged to, which plans it proposed, or on what numeric evidence the rank
// verdict rested. §17.3a persists this document "for later runs"; a later run
// reading one that cannot be attributed has no evidence, only an outcome word.
//
// Nothing here is authority. §17.3a is explicit that a later `--est-basis wall`
// plan re-runs the §6.6 rank check against the ACTUAL ring and still requires
// MIN_ROWS and MIN_RUNS. These fields make the proposal attributable, not
// binding.
type CalibrationEvidence struct {
	Schema  string             `json:"schema"`
	Outcome CalibrationOutcome `json:"outcome"`
	// ComparabilityKeyDigest is which wall population this proposal was made
	// for. Without it a persisted document cannot be matched to the history a
	// later plan is allowed to join (§15.3).
	ComparabilityKeyDigest string `json:"comparability_key_digest"`
	// ProposedPlanDigests identifies the layouts this document proposes, one
	// digest per proposed plan, so W-2's non-scored executions can be bound to
	// the proposal that asked for them rather than to its outcome word.
	// It is ALWAYS SERIALIZED, empty when the proposer accepted no layout. The
	// registry gives it cardinality one, and `omitempty` made a bounded miss emit
	// no key at all — so a reader could not tell "this proposal proposes nothing"
	// from "this producer does not write the field".
	ProposedPlanDigests []Digest `json:"proposed_plan_digests"`
	LayoutsTried        int      `json:"layouts_tried"`
	LayoutBudget        int      `json:"layout_budget"`
	// Rank is a POINTER because absence is a real outcome: a structural proof
	// evaluates no design matrix, so there is no rank to report and `0` would be a
	// number nobody computed. `omitempty` on an int could not express that — it
	// also swallowed a legitimately computed rank of 0 — and it omitted the rank of
	// the decisive matrix on a bounded miss, which §6.6 does produce.
	Rank *int `json:"rank,omitempty"`
	// SigmaMax, Tolerance and MinPivot are §6.6's own numbers for the DECISIVE
	// design matrix — the admitted layout's, or the last one evaluated on a
	// bounded miss. They are the rank verdict's evidence: a reader can see how
	// far from the tolerance the smallest pivot fell instead of taking
	// `admitted` on faith.
	SigmaMax  string `json:"sigma_max,omitempty"`
	Tolerance string `json:"tolerance,omitempty"`
	MinPivot  string `json:"min_pivot,omitempty"`
	// IndicatorValuesPresent and DistinctSliceCounts are columns 3 and 4 of
	// that same decisive matrix, deduplicated and ascending. They are what
	// makes a deficiency diagnosable: a column reported deficient because it
	// holds one value says so here.
	//
	// A structural proof evaluates no layout, so the proven-constant column is
	// reported from the universe and the other is absent rather than invented.
	IndicatorValuesPresent []int `json:"indicator_values_present,omitempty"`
	DistinctSliceCounts    []int `json:"distinct_slice_counts,omitempty"`
	// rankEvaluated records that a design matrix was actually formed, which is
	// what §17.3a ties the six rank diagnostics' presence to. It is not
	// serialized: the document says which outcome it is, and the presence of the
	// diagnostics follows from that.
	rankEvaluated bool `json:"-"`
	// GeneratorExhausted records that L(i) became undefined before the budget
	// was reached, which §6.7 requires to be reported rather than folded into
	// an ordinary bounded miss.
	GeneratorExhausted bool `json:"generator_exhausted,omitempty"`
	// DeficientColumns names what was missing on a bounded miss, and
	// ZeroColumn names the actually-zero column on a structural proof.
	// DeficientColumns is ALWAYS SERIALIZED, empty when nothing was deficient —
	// which is exactly how §17.3a's published example represents a sufficient
	// result. `omitempty` omitted the key on success, so the one outcome that
	// proves no column is missing said nothing about columns at all.
	DeficientColumns []int  `json:"deficient_columns"`
	ZeroColumn       int    `json:"zero_column,omitempty"`
	Reason           string `json:"reason,omitempty"`
	// Buckets and DesignRows are the accepted layout, present iff SUFFICIENT.
	Buckets    [][]string  `json:"buckets,omitempty"`
	DesignRows [][]float64 `json:"design_rows,omitempty"`
	// GeneratedAt is the plan run's own instant, RFC 3339 UTC. It is the
	// planner's `now` rather than a second clock read, so the document a test
	// writes is the document the test can compare.
	GeneratedAt string `json:"generated_at"`
}

// RankEvaluated reports whether the proposer formed a design matrix, which is
// what §17.3a ties the presence of the six rank diagnostics to. A structural
// proof forms none.
func (ev CalibrationEvidence) RankEvaluated() bool { return ev.rankEvaluated }

// columnValues returns the deduplicated ascending integer values of one design
// column, which is how §17.3a reports columns 3 and 4.
func columnValues(rows [][]float64, col int) []int {
	seen := map[int]bool{}
	for _, r := range rows {
		if col < len(r) {
			seen[int(r[col])] = true
		}
	}
	out := make([]int, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

// withDesign records the rank diagnostics and column contents of the design
// matrix a verdict was actually taken on.
func (ev *CalibrationEvidence) withDesign(rows [][]float64, rank RankResult) {
	ev.SigmaMax, ev.Tolerance, ev.MinPivot = rank.SigmaMax, rank.Tolerance, rank.MinPivot
	ev.IndicatorValuesPresent = columnValues(rows, 2)
	ev.DistinctSliceCounts = columnValues(rows, 3)
	// THE DECISIVE MATRIX'S RANK, on every outcome that has one. A bounded miss
	// has a rank — that is WHY it missed — and omitting it left the outcome word
	// as the only evidence for a verdict §6.6 computes from numbers.
	r := rank.Rank
	ev.Rank = &r
	ev.rankEvaluated = true
}

const CalibrationEvidenceSchema = "testbucket.calibration-evidence/v1"

// splitUniverse orders the universe as §17.3a's notation requires: W by
// (reporter_ewma_ns desc, unit_id asc), S by unit_id asc.
func splitUniverse(u []CalibrationUnit) (whole, slices []CalibrationUnit) {
	for _, x := range u {
		if x.IsSlice {
			slices = append(slices, x)
			continue
		}
		whole = append(whole, x)
	}
	sort.SliceStable(whole, func(i, j int) bool {
		if whole[i].BaseNs != whole[j].BaseNs {
			return whole[i].BaseNs > whole[j].BaseNs
		}
		return whole[i].ID < whole[j].ID
	})
	sort.SliceStable(slices, func(i, j int) bool { return slices[i].ID < slices[j].ID })
	return whole, slices
}

// errLayoutUndefined marks L(i) undefined, which the proposer turns into
// exhaustion rather than into an outcome of its own.
type errLayoutUndefined struct{ why string }

func (e *errLayoutUndefined) Error() string { return "layout undefined: " + e.why }

// Layout is L(U, K, i) of contract §17.3a. It is a PARTIAL function: for some
// (U, K, i) no layout exists, and it never pads a missing isolated unit, never
// silently isolates fewer, and never emits fewer than K bucket lists.
func Layout(u []CalibrationUnit, k, i int, pack Packer) ([][]CalibrationUnit, error) {
	if k < 1 {
		return nil, &errLayoutUndefined{fmt.Sprintf("K = %d", k)}
	}
	if i < 1 {
		return nil, &errLayoutUndefined{fmt.Sprintf("i = %d", i)}
	}
	whole, slices := splitUniverse(u)

	out := make([][]CalibrationUnit, k)

	switch {
	case i == 1:
		// The ordinary reporter partition: expandUnits plus the reporter-basis
		// KK packing over every unit.
		packed := pack(u, k)
		copy(out, packed)
		return out, nil

	case i == 2:
		// bucket 0 = ALL of S; W over buckets 1 … K-1 by KK.
		need := 1
		if len(whole) > 0 {
			need++
		}
		if need > k {
			return nil, &errLayoutUndefined{fmt.Sprintf("L(2) needs %d slots, K = %d", need, k)}
		}
		out[0] = slices
		if len(whole) > 0 {
			for bi, g := range pack(whole, k-1) {
				out[1+bi] = g
			}
		}
		return out, nil

	case i >= 3:
		// isolate(n): the first n units of W, each alone in its own bucket.
		// L(3) and L(4) isolate one; L(i >= 5) isolates i-3, which is also
		// what i-3 gives at i = 4.
		n := 1
		if i >= 4 {
			n = i - 3
		}
		if n > len(whole) {
			return nil, &errLayoutUndefined{
				fmt.Sprintf("L(%d) isolates %d whole units but the universe has %d", i, n, len(whole))}
		}
		sliceSlots := 1
		if i >= 4 {
			// L(4) and L(i>=5) reserve min(2, |S|) slice buckets, not always
			// two: with |S| = 1 only ONE slot is reserved, which is the repair
			// that removed the one-slice contradiction.
			sliceSlots = 2
			if len(slices) < 2 {
				sliceSlots = len(slices)
			}
		}
		residual := whole[n:]
		residualSlots := 0
		if len(residual) > 0 {
			residualSlots = 1
		}
		need := n + sliceSlots + residualSlots
		if need > k {
			return nil, &errLayoutUndefined{
				fmt.Sprintf("L(%d) needs %d slots (%d isolated + %d slice + %d residual), K = %d",
					i, need, n, sliceSlots, residualSlots, k)}
		}

		at := 0
		for j := 0; j < n; j++ {
			out[at] = []CalibrationUnit{whole[j]}
			at++
		}
		switch {
		case i == 3:
			out[at] = slices
			at++
		case sliceSlots == 1:
			// A single slice cannot be split, so it occupies the one reserved
			// slice bucket whole.
			out[at] = slices
			at++
		case sliceSlots == 2:
			half := (len(slices) + 1) / 2 // ceil(|S|/2)
			out[at] = slices[:half]
			out[at+1] = slices[half:]
			at += 2
		}
		if len(residual) > 0 {
			remaining := k - at
			if remaining < 1 {
				return nil, &errLayoutUndefined{fmt.Sprintf("L(%d) has no slot for its residual whole units", i)}
			}
			for bi, g := range pack(residual, remaining) {
				out[at+bi] = g
			}
		}
		return out, nil
	}
	return nil, &errLayoutUndefined{fmt.Sprintf("i = %d", i)}
}

// designOfLayout builds §0.9's four ordered columns for a layout. An empty
// bucket is a LEGITIMATE design row contributing reporter_sum_ns = 0, I = 0 and
// slice_count = 0.
func designOfLayout(layout [][]CalibrationUnit) ([][]float64, error) {
	rows := make([][]float64, 0, len(layout))
	for _, b := range layout {
		terms := make([]int64, 0, len(b))
		ind, sc := 0, 0
		for _, u := range b {
			// CHECKED, not `sum += u.BaseNs`. A design row built from a wrapped
			// sum describes a bucket that does not exist, and the fit that
			// follows would identify parameters from it silently. §1.3 makes
			// this a named failure with no artifact.
			terms = append(terms, u.BaseNs)
			if u.IsSlice {
				sc++
				continue
			}
			ind = 1
		}
		sum, err := nsmath.SumNs("calibration_design_reporter_sum_ns", terms...)
		if err != nil {
			return nil, err
		}
		// THE EMPTY BUCKET CHARGES THE INTERCEPT, and it must charge it in both
		// places. Allocation evaluates the same four-term objective for every
		// bucket, so an empty one is `[1,0,0,0]` here exactly as it is there:
		// empty buckets are permitted when U < K, and calibration and
		// validation disagreeing about the same bucket is the one thing that
		// cannot be allowed to depend on which side is asked.
		rows = append(rows, []float64{1, float64(sum), float64(ind), float64(sc)})
	}
	return rows, nil
}

// CalibrateProposer is the TOTAL proposer of contract §6.7 and §17.3a.
//
// For every legal (U, K, N) it returns exactly one of SUFFICIENT,
// STRUCTURALLY_INFEASIBLE or NOT_FOUND_WITHIN_BUDGET — or terminates
// fail-closed with a named condition of §1.3, writing NO evidence document.
// Totality is a statement about those three outcomes, not a promise that
// arithmetic cannot fail.
//
// A bounded miss is NEVER promoted to structural infeasibility:
// STRUCTURALLY_INFEASIBLE is reserved for a whole-universe proof that names an
// actually-zero column.
func CalibrateProposer(u []CalibrationUnit, k, n int, pack Packer) (CalibrationEvidence, error) {
	if n < 1 {
		return CalibrationEvidence{}, fmt.Errorf("calibration: --calibration-max-plans is %d, must be >= 1", n)
	}
	// EVERY CARDINALITY-ONE CONTAINER STARTS EMPTY RATHER THAN NIL, so the
	// document always carries the key and a nil slice never serializes as `null`.
	ev := CalibrationEvidence{
		Schema: CalibrationEvidenceSchema, LayoutBudget: n,
		ProposedPlanDigests: []Digest{},
		DeficientColumns:    []int{},
	}

	whole, slices := splitUniverse(u)

	// Structural infeasibility is a PROOF over the whole universe, and it names
	// a DIFFERENT column in each direction. An earlier revision blamed column 3
	// both ways, which is false for an all-whole universe: empty buckets are
	// permitted, so the indicator need not be constant there.
	switch {
	case len(slices) == 0:
		ev.Outcome = CalibrationStructurallyInfeasible
		ev.ZeroColumn = 4
		// The proof IS the column content: no slice unit means column 4 holds
		// the single value 0 under every partition. Column 3 is not reported,
		// because no layout was evaluated and its values are unknown.
		ev.DistinctSliceCounts = []int{0}
		ev.Reason = "no name-slice unit exists, so slice_count is 0 in every bucket of every partition: column 4 is identically zero and rank(X) <= 3 for ALL layouts"
		return ev, nil
	case len(whole) == 0:
		ev.Outcome = CalibrationStructurallyInfeasible
		ev.ZeroColumn = 3
		ev.IndicatorValuesPresent = []int{0}
		ev.Reason = "no whole-file unit exists, so I(any_whole_file) is 0 everywhere: column 3 is identically zero and rank(X) <= 3 for ALL layouts"
		return ev, nil
	}

	var lastDeficient []int
	var lastRows [][]float64
	var lastRank RankResult
	for i := 1; i <= n; i++ {
		layout, err := Layout(u, k, i, pack)
		if err != nil {
			// L(i) is undefined: record layouts_tried = i - 1, stop, and
			// report a bounded miss with generator_exhausted.
			ev.Outcome = CalibrationNotFoundWithinBudget
			ev.LayoutsTried = i - 1
			ev.GeneratorExhausted = true
			ev.DeficientColumns = orEmptyInts(lastDeficient)
			if lastRows != nil {
				ev.withDesign(lastRows, lastRank)
			}
			ev.Reason = fmt.Sprintf("the layout generator exhausted at i = %d: %v", i, err)
			return ev, nil
		}
		rows, derr := designOfLayout(layout)
		if derr != nil {
			// Fail-closed with a named condition; NO evidence document.
			return CalibrationEvidence{}, derr
		}
		rank, rerr := RankAdmission(rows)
		if rerr != nil {
			// Fail-closed with a named condition; NO evidence document.
			return CalibrationEvidence{}, rerr
		}
		if rank.Admitted {
			ev.Outcome = CalibrationSufficient
			ev.LayoutsTried = i
			ev.DesignRows = rows
			ev.Buckets = bucketIDs(layout)
			// withDesign carries the rank and the three diagnostics, so the rank is
			// written in exactly one place for every outcome that has one.
			ev.withDesign(rows, rank)
			return ev, nil
		}
		lastDeficient = rank.DeficientColumns
		lastRows, lastRank = rows, rank
	}

	ev.Outcome = CalibrationNotFoundWithinBudget
	ev.LayoutsTried = n
	ev.DeficientColumns = orEmptyInts(lastDeficient)
	if lastRows != nil {
		ev.withDesign(lastRows, lastRank)
	}
	ev.Reason = fmt.Sprintf("no evaluated layout reached rank 4 within the budget of %d; a layout outside the enumerated list may still succeed", n)
	return ev, nil
}

func bucketIDs(layout [][]CalibrationUnit) [][]string {
	out := make([][]string, len(layout))
	for i, b := range layout {
		for _, u := range b {
			out[i] = append(out[i], u.ID)
		}
	}
	return out
}

// orEmptyInts renders a nil column list as an empty array, because the registry
// gives `deficient_columns` cardinality one and a JSON `null` is neither a list
// nor an absence.
func orEmptyInts(in []int) []int {
	if in == nil {
		return []int{}
	}
	return in
}
