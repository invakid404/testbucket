package walltime

import (
	"fmt"
	"math"
	"strconv"

	"github.com/invakid404/testbucket/internal/nsmath"
)

// Float64Eps is the float64 machine epsilon contract §6.6 freezes literally.
const Float64Eps = 2.220446049250313e-16

// DesignColumns is the column count of §0.9's design matrix
// X = [1, reporter_sum_ns, I(any_whole_file), slice_count].
const DesignColumns = 4

// svdIterationCapPerColumn is §6.6's `75 × min(rows, 4)` bound on the
// bidiagonal QR sweep.
const svdIterationCapPerColumn = 75

// ErrRankNonConvergent is contract §1.3's `E_RANK_NON_CONVERGENT`. The
// Golub–Reinsch iteration exhausted its cap without meeting the stopping test,
// so `sigma_max` is NOT produced, no rank is inferred, and no artifact is
// written.
//
// It resolves to a FAILURE rather than to a status, unlike the NNLS cap: a rank
// test that did not converge is a computation whose answer is unknown, and
// reporting it as "deficient" would silently deny a warm model the corpus may
// in fact support.
type ErrRankNonConvergent struct {
	Iterations int
}

func (e *ErrRankNonConvergent) Error() string {
	return fmt.Sprintf("%s: Golub-Reinsch bidiagonal QR did not meet its stopping test within %d iterations; sigma_max is not produced and no rank is inferred",
		nsmath.ERankNonConvergent, e.Iterations)
}

// RankResult is the frozen admission verdict of contract §6.6. The three
// float fields serialize as shortest round-tripping decimal strings.
type RankResult struct {
	Rank             int    `json:"rank"`
	DeficientColumns []int  `json:"deficient_columns"`
	SigmaMax         string `json:"sigma_max"`
	Tolerance        string `json:"tolerance"`
	MinPivot         string `json:"min_pivot"`
	Admitted         bool   `json:"admitted"`
	pivots           []float64
}

// fmtG renders a float as §6.6 requires: Go 'g', -1, 64 — the shortest decimal
// that round-trips.
func fmtG(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// RankAdmission evaluates contract §6.6's frozen criterion over the design
// matrix X.
//
// A wall-basis plan is admitted only if rank(X) == 4. The tolerance is
//
//	tol = sigma_max(X) * 4 * 2.220446049250313e-16
//
// where 4 is the column count and the constant is float64 machine epsilon, and
// a column is rank-deficient when its QR pivot is STRICTLY less than tol.
//
// Row and run counts are necessary and never sufficient: MIN_ROWS and MIN_RUNS
// do not imply identifiability, which is the whole reason this criterion exists
// rather than a count.
func RankAdmission(x [][]float64) (RankResult, error) {
	if len(x) == 0 {
		return RankResult{}, fmt.Errorf("rank admission: empty design matrix")
	}
	n := len(x[0])
	for _, row := range x {
		if len(row) != n {
			return RankResult{}, fmt.Errorf("rank admission: ragged design matrix")
		}
	}

	sigmaMax, err := sigmaMaxGolubReinsch(x)
	if err != nil {
		// E_RANK_NON_CONVERGENT: no sigma_max, no tolerance, no rank.
		return RankResult{}, err
	}

	tol := sigmaMax * float64(n) * Float64Eps
	pivots := householderPivots(x)

	var deficient []int
	minPivot := math.Inf(1)
	rank := 0
	for i, p := range pivots {
		if p < minPivot {
			minPivot = p
		}
		// Strictly less than tol is deficient; at or above tol counts.
		if p < tol {
			deficient = append(deficient, i+1) // original column indices, 1-based
			continue
		}
		rank++
	}
	if math.IsInf(minPivot, 1) {
		minPivot = 0
	}

	return RankResult{
		Rank:             rank,
		DeficientColumns: deficient,
		SigmaMax:         fmtG(sigmaMax),
		Tolerance:        fmtG(tol),
		MinPivot:         fmtG(minPivot),
		Admitted:         rank == n,
		pivots:           pivots,
	}, nil
}

// householderPivots runs Householder QR with COLUMN PIVOTING and returns the
// absolute pivot magnitudes indexed by ORIGINAL column.
//
// The pivot is the column of maximum remaining norm, ties resolved to the
// LOWEST column index (§6.6).
func householderPivots(x [][]float64) []float64 {
	m, n := len(x), len(x[0])
	a := make([][]float64, m)
	for i := range a {
		a[i] = append([]float64(nil), x[i]...)
	}
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}
	out := make([]float64, n)

	steps := n
	if m < n {
		steps = m
	}
	for k := 0; k < steps; k++ {
		// §6.6: the pivot is the column of maximum remaining norm, with ties
		// resolved to the LOWEST COLUMN INDEX. The index compared is the
		// ORIGINAL one, so the choice does not depend on where earlier swaps
		// happened to leave a column and the result is permutation-invariant.
		best, bestNorm := -1, -1.0
		for j := k; j < n; j++ {
			var s float64
			for i := k; i < m; i++ {
				s += a[i][j] * a[i][j]
			}
			switch {
			case best < 0, s > bestNorm:
				best, bestNorm = j, s
			case s == bestNorm && perm[j] < perm[best]:
				best = j
			}
		}
		if best < 0 {
			break
		}
		if best != k {
			for i := 0; i < m; i++ {
				a[i][k], a[i][best] = a[i][best], a[i][k]
			}
			perm[k], perm[best] = perm[best], perm[k]
		}

		// Householder reflection zeroing below the diagonal in column k.
		var norm float64
		for i := k; i < m; i++ {
			norm += a[i][k] * a[i][k]
		}
		norm = math.Sqrt(norm)
		out[perm[k]] = norm
		if norm == 0 {
			continue
		}
		alpha := -norm
		if a[k][k] < 0 {
			alpha = norm
		}
		v := make([]float64, m)
		for i := k; i < m; i++ {
			v[i] = a[i][k]
		}
		v[k] -= alpha
		var vnorm float64
		for i := k; i < m; i++ {
			vnorm += v[i] * v[i]
		}
		if vnorm == 0 {
			continue
		}
		for j := k; j < n; j++ {
			var dot float64
			for i := k; i < m; i++ {
				dot += v[i] * a[i][j]
			}
			f := 2 * dot / vnorm
			for i := k; i < m; i++ {
				a[i][j] -= f * v[i]
			}
		}
	}
	return out
}

// sigmaMaxGolubReinsch returns the largest singular value by the classical
// LAPACK dgesvd path: Householder bidiagonalisation then implicit-shift QR on
// the bidiagonal, iterating until
//
//	|e_i| ≤ 2.220446049250313e-16 × (|d_i| + |d_{i+1}|)
//
// capped at 75 × min(rows, 4) iterations. It is never a randomised or truncated
// estimator, and cap exhaustion is E_RANK_NON_CONVERGENT rather than a guess.
func sigmaMaxGolubReinsch(x [][]float64) (float64, error) {
	m, n := len(x), len(x[0])
	a := make([][]float64, m)
	for i := range a {
		a[i] = append([]float64(nil), x[i]...)
	}

	d := make([]float64, n) // diagonal
	e := make([]float64, n) // superdiagonal, e[i] couples d[i] and d[i+1]

	// --- Householder bidiagonalisation ---
	for k := 0; k < n && k < m; k++ {
		// Left reflection on column k.
		var norm float64
		for i := k; i < m; i++ {
			norm += a[i][k] * a[i][k]
		}
		norm = math.Sqrt(norm)
		if norm != 0 {
			alpha := -norm
			if a[k][k] < 0 {
				alpha = norm
			}
			v := make([]float64, m)
			for i := k; i < m; i++ {
				v[i] = a[i][k]
			}
			v[k] -= alpha
			var vn float64
			for i := k; i < m; i++ {
				vn += v[i] * v[i]
			}
			if vn != 0 {
				for j := k; j < n; j++ {
					var dot float64
					for i := k; i < m; i++ {
						dot += v[i] * a[i][j]
					}
					f := 2 * dot / vn
					for i := k; i < m; i++ {
						a[i][j] -= f * v[i]
					}
				}
			}
			d[k] = a[k][k]
		} else {
			d[k] = 0
		}

		// Right reflection on row k, over columns k+1..n-1.
		if k+1 < n {
			var rnorm float64
			for j := k + 1; j < n; j++ {
				rnorm += a[k][j] * a[k][j]
			}
			rnorm = math.Sqrt(rnorm)
			if rnorm != 0 {
				alpha := -rnorm
				if a[k][k+1] < 0 {
					alpha = rnorm
				}
				u := make([]float64, n)
				for j := k + 1; j < n; j++ {
					u[j] = a[k][j]
				}
				u[k+1] -= alpha
				var un float64
				for j := k + 1; j < n; j++ {
					un += u[j] * u[j]
				}
				if un != 0 {
					for i := k; i < m; i++ {
						var dot float64
						for j := k + 1; j < n; j++ {
							dot += u[j] * a[i][j]
						}
						f := 2 * dot / un
						for j := k + 1; j < n; j++ {
							a[i][j] -= f * u[j]
						}
					}
				}
				e[k] = a[k][k+1]
			} else {
				e[k] = 0
			}
		}
	}

	// --- implicit-shift QR on the bidiagonal ---
	// The cap is §6.6's 75 x min(rows, 4). svdIterationCapOverride lets a test
	// drive the EXHAUSTION path without fabricating a pathological bidiagonal
	// matrix: the property under test is that the cap resolves to
	// E_RANK_NON_CONVERGENT with no rank inferred, not the numerology of which
	// matrices happen to converge slowly.
	cap := svdIterationCapPerColumn * minInt(m, DesignColumns)
	if svdIterationCapOverride > 0 {
		cap = svdIterationCapOverride
	}
	iters := 0
	for p := n - 1; p > 0; {
		// Deflate any negligible SUPERDIAGONAL: the block splits there.
		converged := false
		for i := p - 1; i >= 0; i-- {
			if math.Abs(e[i]) <= Float64Eps*(math.Abs(d[i])+math.Abs(d[i+1])) {
				e[i] = 0
				if i == p-1 {
					p--
					converged = true
				}
				break
			}
		}
		if converged {
			continue
		}

		// Deflate a negligible DIAGONAL entry. This is the case the
		// implicit-shift sweep cannot resolve on its own: with d[k] = 0 the
		// Wilkinson shift degenerates and the bulge chase leaves e[k]
		// unchanged, so the iteration spins until the cap fires and reports
		// E_RANK_NON_CONVERGENT on a design that is merely rank-deficient.
		//
		// It is reachable in this product: the calibration proposer of §17.3a
		// evaluates K-bucket designs, and a K = 2 layout gives a 2x4 matrix
		// whose trailing diagonal entries are structurally zero.
		//
		// A zero diagonal entry means the block splits at k with a zero
		// singular value, so the adjacent superdiagonal is negligible in the
		// same sense and is cleared.
		scale := 0.0
		for i := 0; i <= p; i++ {
			if v := math.Abs(d[i]); v > scale {
				scale = v
			}
			if i < p {
				if v := math.Abs(e[i]); v > scale {
					scale = v
				}
			}
		}
		split := false
		for k := 0; k <= p; k++ {
			if math.Abs(d[k]) > Float64Eps*scale {
				continue
			}
			switch {
			case k < p:
				e[k] = 0
			case k > 0:
				e[k-1] = 0
			}
			split = true
			break
		}
		if split {
			continue
		}
		if iters >= cap {
			return 0, &ErrRankNonConvergent{Iterations: iters}
		}
		iters++

		// Find the start of the unreduced block ending at p.
		q := p - 1
		for q > 0 && e[q-1] != 0 {
			q--
		}

		// Wilkinson shift from the trailing 2x2 of B^T B.
		dm, dp := d[p-1], d[p]
		em := e[p-1]
		var eq float64
		if p-2 >= 0 {
			eq = e[p-2]
		}
		a11 := dm*dm + eq*eq
		a22 := dp*dp + em*em
		a12 := dm * em
		delta := (a11 - a22) / 2
		var mu float64
		den := delta + sign(delta)*math.Hypot(delta, a12)
		if den != 0 {
			mu = a22 - a12*a12/den
		} else {
			mu = a22
		}

		// Chase the bulge with Givens rotations.
		f := d[q]*d[q] - mu
		g := d[q] * e[q]
		for k := q; k < p; k++ {
			c, s := givens(f, g)
			if k > q {
				e[k-1] = math.Hypot(f, g)
			}
			f = c*d[k] + s*e[k]
			e[k] = c*e[k] - s*d[k]
			g = s * d[k+1]
			d[k+1] = c * d[k+1]

			c, s = givens(f, g)
			d[k] = math.Hypot(f, g)
			f = c*e[k] + s*d[k+1]
			d[k+1] = c*d[k+1] - s*e[k]
			if k+1 < p {
				g = s * e[k+1]
				e[k+1] = c * e[k+1]
			}
		}
		e[p-1] = f
	}

	// The singular values are |d_i|; sigma_max is the largest.
	sigma := 0.0
	for _, v := range d {
		if av := math.Abs(v); av > sigma {
			sigma = av
		}
	}
	return sigma, nil
}

func givens(f, g float64) (c, s float64) {
	if g == 0 {
		return 1, 0
	}
	r := math.Hypot(f, g)
	return f / r, g / r
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// svdIterationCapOverride is zero in production. A test sets it to force the
// §6.6 cap and restores it immediately; it is never read from configuration.
var svdIterationCapOverride int
