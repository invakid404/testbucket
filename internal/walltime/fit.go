package walltime

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"

	"github.com/invakid404/testbucket/internal/nsmath"
)

// Frozen thresholds of contract §10.4. None may be chosen, tuned or adjusted
// from pilot or campaign outcomes.
const (
	MinRows = 24
	MinRuns = 3
	// ModelMAECeilingNs is §10.4's 20.000 s, in the nanoseconds the model works
	// in.
	ModelMAECeilingNs int64 = 20_000_000_000
)

// NNLS budget of contract §6.8: 3 × 4 outer iterations.
const nnlsOuterCap = 3 * DesignColumns

// FitStatus and FitSubtype mirror the store's vocabulary without importing it,
// so the numeric layer does not depend on the store layout.
type FitStatus string

const (
	FitOK           FitStatus = "ok"
	FitDegraded     FitStatus = "degraded"
	FitInsufficient FitStatus = "insufficient"
)

// FitRow is one selected ring row reduced to what the fit reads: the three
// stored regressors and the response.
type FitRow struct {
	ReporterSumNs int64
	IAnyWholeFile int
	SliceCount    int
	ElapsedNs     int64
	// Ordering keys for §6.8 step 1's summation order.
	HeadSHA     string
	RunID       string
	RunAttempt  string
	BucketIndex int
}

// FitResult is the outcome of contract §6.8.
type FitResult struct {
	Status  FitStatus
	Subtype string

	Model WallModelCoefficients
	// RowsUsed and RunsUsed describe the population actually fitted.
	RowsUsed int
	RunsUsed int
	// Rank carries §6.6's verdict, so a caller can report why an insufficient
	// model is insufficient without re-running the admission.
	Rank RankResult

	ResidualMAENs int64
	ResidualP90Ns int64
}

// WallModelCoefficients is the four-parameter model. Exactly four coefficients
// are stored: there is no per_invocation_ns field and no degenerate_columns
// field, both superseded.
type WallModelCoefficients struct {
	FixedNs                   int64
	Scale                     float64
	WholeInvocationOverheadNs int64
	PerSliceOverheadNs        int64
}

// ModelParametersDigest is contract §6.8 step 6: SHA-256 over the UTF-8
// canonical JSON of exactly the four keys, in that order, no whitespace,
// integers as bare decimal digits, and `scale` as a QUOTED STRING so no float
// formatter can vary the bytes.
func ModelParametersDigest(m WallModelCoefficients) Digest {
	var b bytes.Buffer
	b.WriteString(`{"fixed_ns":`)
	b.WriteString(strconv.FormatInt(m.FixedNs, 10))
	b.WriteString(`,"scale":"`)
	b.WriteString(strconv.FormatFloat(m.Scale, 'g', -1, 64))
	b.WriteString(`","whole_invocation_overhead_ns":`)
	b.WriteString(strconv.FormatInt(m.WholeInvocationOverheadNs, 10))
	b.WriteString(`,"per_slice_overhead_ns":`)
	b.WriteString(strconv.FormatInt(m.PerSliceOverheadNs, 10))
	b.WriteString(`}`)
	return DigestBytes(b.Bytes())
}

// SortForFit is contract §6.8 step 1: order the selected rows for arithmetic
// reproducibility by (head_sha, run_id, run_attempt, bucket_index). This is a
// SUMMATION ORDER, never a definition of recency.
func SortForFit(rows []FitRow) []FitRow {
	out := append([]FitRow(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.HeadSHA != b.HeadSHA {
			return a.HeadSHA < b.HeadSHA
		}
		if a.RunID != b.RunID {
			return a.RunID < b.RunID
		}
		if a.RunAttempt != b.RunAttempt {
			return a.RunAttempt < b.RunAttempt
		}
		return a.BucketIndex < b.BucketIndex
	})
	return out
}

// designOf builds §0.9's four ordered columns and the response.
func designOf(rows []FitRow) (x [][]float64, y []float64) {
	x = make([][]float64, len(rows))
	y = make([]float64, len(rows))
	for i, r := range rows {
		x[i] = []float64{1, float64(r.ReporterSumNs), float64(r.IAnyWholeFile), float64(r.SliceCount)}
		y[i] = float64(r.ElapsedNs)
	}
	return x, y
}

// FitModel runs contract §6.8's procedure. Selection is the caller's job and is
// §15.1b's `trainable == true` and no other predicate.
func FitModel(selected []FitRow) (FitResult, error) {
	rows := SortForFit(selected)
	res := FitResult{RowsUsed: len(rows)}

	runs := map[[2]string]bool{}
	for _, r := range rows {
		runs[[2]string{r.RunID, r.RunAttempt}] = true
	}
	res.RunsUsed = len(runs)

	// Counts are decidable before a design matrix is formed, which is why
	// §15.1c puts them ahead of rank in the precedence order.
	holding := map[string]bool{}
	if len(rows) < MinRows {
		holding["rows_below_minimum"] = true
	}
	if res.RunsUsed < MinRuns {
		holding["runs_below_minimum"] = true
	}

	if len(rows) == 0 {
		res.Status, res.Subtype = FitInsufficient, "rows_below_minimum"
		return res, nil
	}

	x, y := designOf(rows)

	// Step 2: rank sufficiency BEFORE fitting.
	rank, err := RankAdmission(x)
	if err != nil {
		// E_RANK_NON_CONVERGENT: fail closed, store nothing, leave the model
		// untouched. Not a status.
		return FitResult{}, err
	}
	res.Rank = rank
	if !rank.Admitted {
		holding["rank_insufficient"] = true
	}

	// Step 3: Lawson-Hanson NNLS.
	var coef []float64
	if !holding["rows_below_minimum"] && !holding["runs_below_minimum"] && rank.Admitted {
		var converged bool
		coef, converged = lawsonHanson(x, y)
		if !converged {
			holding["nnls_budget_exhausted"] = true
		}
	}

	if sub, ok := firstSubtype(holding); ok {
		res.Status, res.Subtype = FitInsufficient, sub
		return res, nil
	}

	// Step 4: round the three nanosecond coefficients; keep scale float64.
	fixed, err := nsmath.RoundHalfUp("fixed_ns", coef[0])
	if err != nil {
		return FitResult{}, err
	}
	whole, err := nsmath.RoundHalfUp("whole_invocation_overhead_ns", coef[2])
	if err != nil {
		return FitResult{}, err
	}
	slice, err := nsmath.RoundHalfUp("per_slice_overhead_ns", coef[3])
	if err != nil {
		return FitResult{}, err
	}
	res.Model = WallModelCoefficients{
		FixedNs:                   fixed,
		Scale:                     coef[1],
		WholeInvocationOverheadNs: whole,
		PerSliceOverheadNs:        slice,
	}

	// Step 5: residual aggregates from the ROUNDED coefficients — from exactly
	// the model that will plan, so a stored residual_mae_ns always describes
	// the deployed predictor.
	mae, p90, err := residualAggregates(res.Model, rows)
	if err != nil {
		return FitResult{}, err
	}
	res.ResidualMAENs, res.ResidualP90Ns = mae, p90

	if mae > ModelMAECeilingNs {
		res.Status, res.Subtype = FitDegraded, "mae_ceiling_exceeded"
		return res, nil
	}
	res.Status = FitOK
	return res, nil
}

// firstSubtype applies §15.1c's fixed precedence over simultaneously-holding
// insufficiency predicates.
func firstSubtype(holding map[string]bool) (string, bool) {
	for _, s := range []string{
		"migrated_no_history", "rows_below_minimum", "runs_below_minimum",
		"rank_insufficient", "nnls_budget_exhausted",
	} {
		if holding[s] {
			return s, true
		}
	}
	return "", false
}

// residualAggregates computes §6.8 step 5 in §1.1's checked domain.
func residualAggregates(m WallModelCoefficients, rows []FitRow) (mae, p90 int64, err error) {
	abs := make([]int64, 0, len(rows))
	sum := new(big.Int)
	for _, r := range rows {
		pred, err := aEtaFromRow(m, r)
		if err != nil {
			return 0, 0, err
		}
		d := nsmath.AbsExact(pred, r.ElapsedNs)
		sum.Add(sum, d)
		v, err := nsmath.Narrow("residual", d)
		if err != nil {
			return 0, 0, err
		}
		abs = append(abs, v)
	}
	if len(abs) == 0 {
		return 0, 0, fmt.Errorf("residual aggregates over an empty population")
	}
	// The exact rational mean, never a float.
	q := new(big.Rat).SetFrac(sum, big.NewInt(int64(len(abs))))
	mae, err = nsmath.RoundHalfUpRat("residual_mae_ns", q)
	if err != nil {
		return 0, 0, err
	}
	sorted := append([]int64(nil), abs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	p90, err = nsmath.NearestRank(sorted, 9, 10)
	if err != nil {
		return 0, 0, err
	}
	return mae, p90, nil
}

// aEtaFromRow evaluates §0.9's objective for a stored row's frozen regressors.
func aEtaFromRow(m WallModelCoefficients, r FitRow) (int64, error) {
	scaled, err := nsmath.MulScale("scale_x_reporter_sum_ns", m.Scale, r.ReporterSumNs)
	if err != nil {
		return 0, err
	}
	ind := int64(0)
	if r.IAnyWholeFile > 0 {
		ind = 1
	}
	terms := []int64{m.FixedNs, scaled, ind * m.WholeInvocationOverheadNs}
	for i := 0; i < r.SliceCount; i++ {
		terms = append(terms, m.PerSliceOverheadNs)
	}
	return nsmath.SumNs("a_eta_ns", terms...)
}

// lawsonHanson is the classical active-set NNLS of contract §6.8, with every
// frozen choice that section makes: passive set empty and b = 0 initially, the
// column of MAXIMUM DUAL selected with ties to the lowest index, the inner
// unconstrained solve by Cholesky over the passive set in ascending column
// order, the classical alpha = min ratio test with ties to the lowest index,
// and the stopping test max(w) <= tol_nnls.
//
// It reports convergence separately from the solution: §6.8 forbids deploying a
// partially converged vector, so the caller turns a false here into status
// insufficient with subtype nnls_budget_exhausted rather than storing what it
// has.
func lawsonHanson(x [][]float64, y []float64) (coef []float64, converged bool) {
	m, n := len(x), len(x[0])
	b := make([]float64, n)
	passive := make([]bool, n)

	// tol_nnls = 10 * 4 * eps * ||X^T y||_inf
	xty := make([]float64, n)
	for j := 0; j < n; j++ {
		var s float64
		for i := 0; i < m; i++ {
			s += x[i][j] * y[i]
		}
		xty[j] = s
	}
	var inf float64
	for _, v := range xty {
		if a := math.Abs(v); a > inf {
			inf = a
		}
	}
	tol := 10 * float64(n) * Float64Eps * inf

	dual := func() []float64 {
		w := make([]float64, n)
		for j := 0; j < n; j++ {
			var s float64
			for i := 0; i < m; i++ {
				var xb float64
				for k := 0; k < n; k++ {
					xb += x[i][k] * b[k]
				}
				s += x[i][j] * (y[i] - xb)
			}
			w[j] = s
		}
		return w
	}

	outerCap := nnlsOuterCap
	if nnlsOuterCapOverride > 0 {
		outerCap = nnlsOuterCapOverride
	}
	for iter := 0; iter < outerCap; iter++ {
		w := dual()
		// Maximum dual over the ACTIVE set; ties to the lowest column index.
		best, bestW := -1, 0.0
		for j := 0; j < n; j++ {
			if passive[j] {
				continue
			}
			if best < 0 || w[j] > bestW {
				best, bestW = j, w[j]
			}
		}
		if best < 0 || bestW <= tol {
			return b, true
		}
		passive[best] = true

		for {
			z := solvePassive(x, y, passive)
			if z == nil {
				passive[best] = false
				return b, true
			}
			// Feasible?
			minZ, minIdx := math.Inf(1), -1
			for j := 0; j < n; j++ {
				if passive[j] && z[j] <= 0 && z[j] < minZ {
					minZ, minIdx = z[j], j
				}
			}
			if minIdx < 0 {
				b = z
				break
			}
			// Classical alpha = min ratio test; ties to the lowest index.
			alpha, aIdx := math.Inf(1), -1
			for j := 0; j < n; j++ {
				if !passive[j] || z[j] > 0 {
					continue
				}
				den := b[j] - z[j]
				if den == 0 {
					continue
				}
				r := b[j] / den
				if r < alpha {
					alpha, aIdx = r, j
				}
			}
			if aIdx < 0 {
				b = z
				break
			}
			for j := 0; j < n; j++ {
				b[j] += alpha * (z[j] - b[j])
			}
			for j := 0; j < n; j++ {
				if passive[j] && math.Abs(b[j]) < 1e-300 {
					passive[j] = false
					b[j] = 0
				}
			}
			passive[aIdx] = false
			b[aIdx] = 0
		}
	}

	// The cap fired before the stopping test was met.
	w := dual()
	maxW := math.Inf(-1)
	for j := 0; j < n; j++ {
		if !passive[j] && w[j] > maxW {
			maxW = w[j]
		}
	}
	if math.IsInf(maxW, -1) || maxW <= tol {
		return b, true
	}
	return b, false
}

// solvePassive solves the normal equations over the passive set by Cholesky
// with columns in ascending index order. It returns nil when the reduced system
// is not positive definite.
func solvePassive(x [][]float64, y []float64, passive []bool) []float64 {
	var idx []int
	for j := range passive {
		if passive[j] {
			idx = append(idx, j)
		}
	}
	p := len(idx)
	if p == 0 {
		return make([]float64, len(passive))
	}
	a := make([][]float64, p)
	rhs := make([]float64, p)
	for i := 0; i < p; i++ {
		a[i] = make([]float64, p)
		for j := 0; j < p; j++ {
			var s float64
			for k := range x {
				s += x[k][idx[i]] * x[k][idx[j]]
			}
			a[i][j] = s
		}
		var s float64
		for k := range x {
			s += x[k][idx[i]] * y[k]
		}
		rhs[i] = s
	}
	// Cholesky.
	l := make([][]float64, p)
	for i := range l {
		l[i] = make([]float64, p)
	}
	for i := 0; i < p; i++ {
		for j := 0; j <= i; j++ {
			s := a[i][j]
			for k := 0; k < j; k++ {
				s -= l[i][k] * l[j][k]
			}
			if i == j {
				if s <= 0 {
					return nil
				}
				l[i][i] = math.Sqrt(s)
				continue
			}
			l[i][j] = s / l[j][j]
		}
	}
	// Forward then back substitution.
	t := make([]float64, p)
	for i := 0; i < p; i++ {
		s := rhs[i]
		for k := 0; k < i; k++ {
			s -= l[i][k] * t[k]
		}
		t[i] = s / l[i][i]
	}
	sol := make([]float64, p)
	for i := p - 1; i >= 0; i-- {
		s := t[i]
		for k := i + 1; k < p; k++ {
			s -= l[k][i] * sol[k]
		}
		sol[i] = s / l[i][i]
	}
	out := make([]float64, len(passive))
	for i, j := range idx {
		out[j] = sol[i]
	}
	return out
}

// nnlsOuterCapOverride is zero in production. A test sets it to force §6.8's
// budget-exhaustion path, which resolves to a STATUS rather than a failure —
// the one cap in this product that does.
var nnlsOuterCapOverride int

// ErrRingAdmission is the class AdmitWallRing returns, so a caller can wrap it
// in its own unusable-model error without string matching.
var ErrRingAdmission = errors.New("the real wall ring does not support wall allocation")

// AdmitWallRing re-applies §6.7's three ring preconditions — MIN_ROWS, MIN_RUNS
// and rank — against the population a plan is about to allocate from.
//
// §6.7 is explicit: "MIN_ROWS/MIN_RUNS/rank are re-checked against the real ring
// on every wall plan." Nothing did. A stored `ok` status and a complete fit group
// were taken as standing permission, so a store whose observations array had
// since been emptied — by eviction, by a migration, by a hand edit, by a
// comparability reset that cleared rows and left the status behind — still
// reached wall allocation and emitted a matrix from coefficients no current row
// supports. A fit-time admission is evidence about the population that existed
// at fit time.
//
// The three checks are exactly the three the contract names here. The residual
// ceiling is NOT re-applied: that is §6.8's fit-time judgement and re-deciding
// it at plan time would silently change which models are usable.
func AdmitWallRing(selected []FitRow) error {
	if len(selected) < MinRows {
		return fmt.Errorf("%w: %d trainable row(s) in the ring, MIN_ROWS is %d",
			ErrRingAdmission, len(selected), MinRows)
	}
	runs := map[[2]string]bool{}
	for _, r := range selected {
		runs[[2]string{r.RunID, r.RunAttempt}] = true
	}
	if len(runs) < MinRuns {
		return fmt.Errorf("%w: %d distinct run(s) in the ring, MIN_RUNS is %d",
			ErrRingAdmission, len(runs), MinRuns)
	}
	// THE SAME MATRIX THE FIT USED, built by the same function, so the plan
	// cannot check a different X from the one the coefficients came from.
	x, _ := designOf(SortForFit(selected))
	rank, err := RankAdmission(x)
	if err != nil {
		// E_RANK_NON_CONVERGENT is a named §1.3 condition, not a verdict: it
		// propagates rather than becoming "not admitted".
		return err
	}
	if !rank.Admitted {
		return fmt.Errorf("%w: rank(X) over the current ring is %d, not %d (deficient columns %v)",
			ErrRingAdmission, rank.Rank, DesignColumns, rank.DeficientColumns)
	}
	return nil
}
