package walltime

import (
	"reflect"
	"testing"
)

const b = 1e9 // §22 test 54 publishes the reporter column "in billions"

// TestWallRankToleranceBoundary is §22 test 54 (SR-4/R11-D6). The matrices are
// LITERAL and published in the contract, so the boundary is checkable without
// running the planner.
func TestWallRankToleranceBoundary(t *testing.T) {
	cases := []struct {
		name      string
		x         [][]float64
		rank      int
		deficient []int
		admitted  bool
	}{
		{
			name: "full rank",
			x: [][]float64{
				{1, 10 * b, 1, 0},
				{1, 20 * b, 1, 0},
				{1, 10 * b, 0, 1},
				{1, 30 * b, 0, 2},
			},
			rank: 4, deficient: nil, admitted: true,
		},
		{
			name: "column 4 dead",
			x: [][]float64{
				{1, 10 * b, 1, 0},
				{1, 20 * b, 1, 0},
				{1, 30 * b, 1, 0},
				{1, 40 * b, 1, 0},
			},
			rank: 2, deficient: []int{3, 4}, admitted: false,
		},
		{
			name: "column 3 dead",
			x: [][]float64{
				{1, 10 * b, 0, 1},
				{1, 20 * b, 0, 2},
				{1, 30 * b, 0, 3},
				{1, 40 * b, 0, 4},
			},
			rank: 2, deficient: []int{3, 4}, admitted: false,
		},
		{
			// column 2 = column 1 + column 3 + column 4, which no prose
			// heuristic catches — that is the point of the fixture.
			//
			// deficient_columns is [3] here, NOT the [4] §22 test 54's table
			// prints. That cell is not producible by the algorithm §6.6
			// freezes, and the reason is arithmetic rather than a tolerance
			// choice: §6.6 pivots on MAXIMUM remaining norm, and at the step
			// where column 4 could be dropped its remaining norm is the
			// LARGEST of the three candidates (1.0, against 0.8718 for column
			// 1 and 0.7483 for column 3). Max-norm pivoting therefore selects
			// column 4 SECOND and can never leave it last, so no tie-break
			// reading reaches [4]; only a minimum-norm rule would, and that is
			// not rank-revealing QR.
			//
			// The two load-bearing values — rank 3 and REJECTED — agree with
			// the contract, and they are what admission turns on. The
			// discrepancy is recorded in implementation.md as a
			// PRODUCT_DECISION item rather than resolved here in either
			// direction: silently asserting [4] would require abandoning the
			// frozen pivot rule, and silently asserting [3] without saying so
			// would hide a defect in a published fixture.
			name: "hidden collinearity",
			x: [][]float64{
				{1, 1 * b, 0, 0},
				{1, 2 * b, 0, 1},
				{1, 2 * b, 1, 0},
				{1, 4 * b, 1, 2},
			},
			rank: 3, deficient: []int{3}, admitted: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := RankAdmission(c.x)
			if err != nil {
				t.Fatalf("rank admission: %v", err)
			}
			if got.Rank != c.rank {
				t.Errorf("rank = %d, want %d (pivots %v, tol %s)", got.Rank, c.rank, got.pivots, got.Tolerance)
			}
			if len(c.deficient) == 0 {
				if len(got.DeficientColumns) != 0 {
					t.Errorf("deficient_columns = %v, want []", got.DeficientColumns)
				}
			} else if !reflect.DeepEqual(got.DeficientColumns, c.deficient) {
				t.Errorf("deficient_columns = %v, want %v", got.DeficientColumns, c.deficient)
			}
			if got.Admitted != c.admitted {
				t.Errorf("admitted = %v, want %v", got.Admitted, c.admitted)
			}
		})
	}

	t.Run("column 4 cannot be pivoted last on the hidden-collinearity fixture", func(t *testing.T) {
		// The executable form of the finding above: §6.6's rule is maximum
		// remaining norm, and column 4's remaining norm at the deciding step
		// exceeds both rivals, so the published [4] is unreachable under the
		// frozen algorithm regardless of how ties are broken.
		x := [][]float64{
			{1, 1 * b, 0, 0},
			{1, 2 * b, 0, 1},
			{1, 2 * b, 1, 0},
			{1, 4 * b, 1, 2},
		}
		r, err := RankAdmission(x)
		if err != nil {
			t.Fatal(err)
		}
		// Column 4 retains a healthy pivot; the dropped column is 3.
		if len(r.pivots) != DesignColumns {
			t.Fatalf("expected %d pivots, got %d", DesignColumns, len(r.pivots))
		}
		tol := mustParse(t, r.Tolerance)
		if r.pivots[3] < tol {
			t.Fatalf("column 4 pivot %g fell below tol %g; the finding no longer holds", r.pivots[3], tol)
		}
		if r.pivots[2] >= tol {
			t.Fatalf("column 3 pivot %g is at or above tol %g; the finding no longer holds", r.pivots[2], tol)
		}
	})

	t.Run("the three floats serialize as shortest round-tripping decimals", func(t *testing.T) {
		r, err := RankAdmission(cases[0].x)
		if err != nil {
			t.Fatal(err)
		}
		for name, s := range map[string]string{"sigma_max": r.SigmaMax, "tolerance": r.Tolerance, "min_pivot": r.MinPivot} {
			if s == "" {
				t.Errorf("%s is empty", name)
			}
			if s != fmtG(mustParse(t, s)) {
				t.Errorf("%s = %q is not the shortest round-tripping decimal", name, s)
			}
		}
	})

	t.Run("admission bytes are identical after row shuffling", func(t *testing.T) {
		x := cases[0].x
		shuffled := [][]float64{x[3], x[1], x[0], x[2]}
		a, err := RankAdmission(x)
		if err != nil {
			t.Fatal(err)
		}
		c, err := RankAdmission(shuffled)
		if err != nil {
			t.Fatal(err)
		}
		if a.Rank != c.Rank || a.Admitted != c.Admitted {
			t.Fatalf("shuffling rows changed admission: %+v vs %+v", a, c)
		}
	})

	t.Run("the tolerance is the frozen expression", func(t *testing.T) {
		r, err := RankAdmission(cases[0].x)
		if err != nil {
			t.Fatal(err)
		}
		want := mustParse(t, r.SigmaMax) * DesignColumns * Float64Eps
		if got := mustParse(t, r.Tolerance); got != want {
			t.Fatalf("tolerance = %v, want sigma_max * 4 * eps = %v", got, want)
		}
	})
}

// TestWallModelRankAdmissionForWholeSliceAndMixedCandidates is §22 test 50
// (F4) and the acceptance test the registry names for ID-10: the SOLE
// rank-admission test over the candidate shapes refinement may score.
func TestWallModelRankAdmissionForWholeSliceAndMixedCandidates(t *testing.T) {
	cases := []struct {
		name     string
		x        [][]float64
		admitted bool
		why      string
	}{
		{
			name: "all I=1 with whole-only candidates",
			x: [][]float64{
				{1, 10 * b, 1, 0}, {1, 20 * b, 1, 0}, {1, 30 * b, 1, 0}, {1, 40 * b, 1, 0},
			},
			admitted: false,
			why:      "column 3 is identical to the intercept and column 4 is dead",
		},
		{
			name: "all I=0 slice-only",
			x: [][]float64{
				{1, 10 * b, 0, 1}, {1, 20 * b, 0, 2}, {1, 30 * b, 0, 3}, {1, 40 * b, 0, 4},
			},
			admitted: false,
			why:      "column 3 is dead",
		},
		{
			// Multiple distinct slice counts do NOT rescue identifiability
			// when reporter_sum_ns moves with slice_count.
			name: "reporter_sum_ns/slice_count collinearity despite multiple slice counts",
			x: [][]float64{
				{1, 1 * b, 0, 1}, {1, 2 * b, 0, 2}, {1, 3 * b, 0, 3}, {1, 4 * b, 0, 4},
			},
			admitted: false,
			why:      "column 2 is a multiple of column 4",
		},
		{
			name: "full-rank mixed",
			x: [][]float64{
				{1, 10 * b, 1, 0}, {1, 20 * b, 1, 0}, {1, 10 * b, 0, 1}, {1, 30 * b, 0, 2},
			},
			admitted: true,
			why:      "whole-only and slice-only rows together identify all four columns",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := RankAdmission(c.x)
			if err != nil {
				t.Fatalf("rank admission: %v", err)
			}
			if r.Admitted != c.admitted {
				t.Fatalf("admitted = %v, want %v (%s); rank %d, deficient %v",
					r.Admitted, c.admitted, c.why, r.Rank, r.DeficientColumns)
			}
			if !c.admitted && r.Rank == DesignColumns {
				t.Fatal("a rejected design reported full rank")
			}
		})
	}

	t.Run("counts are necessary and never sufficient", func(t *testing.T) {
		// Many rows, still unidentifiable: MIN_ROWS/MIN_RUNS do not imply
		// identifiability, which is why §6.6 is a rank criterion and not a
		// count.
		var x [][]float64
		for i := 1; i <= 40; i++ {
			x = append(x, []float64{1, float64(i) * b, 1, 0})
		}
		r, err := RankAdmission(x)
		if err != nil {
			t.Fatal(err)
		}
		if r.Admitted {
			t.Fatal("40 rows of one topology were admitted; a count is not identifiability")
		}
	})
}

func mustParse(t *testing.T, s string) float64 {
	t.Helper()
	var v float64
	if _, err := fmtSscan(s, &v); err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}
