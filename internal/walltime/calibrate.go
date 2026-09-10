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
type CalibrationEvidence struct {
	Schema       string             `json:"schema"`
	Outcome      CalibrationOutcome `json:"outcome"`
	LayoutsTried int                `json:"layouts_tried"`
	LayoutBudget int                `json:"layout_budget"`
	Rank         int                `json:"rank,omitempty"`
	// GeneratorExhausted records that L(i) became undefined before the budget
	// was reached, which §6.7 requires to be reported rather than folded into
	// an ordinary bounded miss.
	GeneratorExhausted bool `json:"generator_exhausted,omitempty"`
	// DeficientColumns names what was missing on a bounded miss, and
	// ZeroColumn names the actually-zero column on a structural proof.
	DeficientColumns []int  `json:"deficient_columns,omitempty"`
	ZeroColumn       int    `json:"zero_column,omitempty"`
	Reason           string `json:"reason,omitempty"`
	// Buckets and DesignRows are the accepted layout, present iff SUFFICIENT.
	Buckets    [][]string  `json:"buckets,omitempty"`
	DesignRows [][]float64 `json:"design_rows,omitempty"`
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
	ev := CalibrationEvidence{Schema: CalibrationEvidenceSchema, LayoutBudget: n}

	whole, slices := splitUniverse(u)

	// Structural infeasibility is a PROOF over the whole universe, and it names
	// a DIFFERENT column in each direction. An earlier revision blamed column 3
	// both ways, which is false for an all-whole universe: empty buckets are
	// permitted, so the indicator need not be constant there.
	switch {
	case len(slices) == 0:
		ev.Outcome = CalibrationStructurallyInfeasible
		ev.ZeroColumn = 4
		ev.Reason = "no name-slice unit exists, so slice_count is 0 in every bucket of every partition: column 4 is identically zero and rank(X) <= 3 for ALL layouts"
		return ev, nil
	case len(whole) == 0:
		ev.Outcome = CalibrationStructurallyInfeasible
		ev.ZeroColumn = 3
		ev.Reason = "no whole-file unit exists, so I(any_whole_file) is 0 everywhere: column 3 is identically zero and rank(X) <= 3 for ALL layouts"
		return ev, nil
	}

	var lastDeficient []int
	for i := 1; i <= n; i++ {
		layout, err := Layout(u, k, i, pack)
		if err != nil {
			// L(i) is undefined: record layouts_tried = i - 1, stop, and
			// report a bounded miss with generator_exhausted.
			ev.Outcome = CalibrationNotFoundWithinBudget
			ev.LayoutsTried = i - 1
			ev.GeneratorExhausted = true
			ev.DeficientColumns = lastDeficient
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
			ev.Rank = rank.Rank
			ev.DesignRows = rows
			ev.Buckets = bucketIDs(layout)
			return ev, nil
		}
		lastDeficient = rank.DeficientColumns
	}

	ev.Outcome = CalibrationNotFoundWithinBudget
	ev.LayoutsTried = n
	ev.DeficientColumns = lastDeficient
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
