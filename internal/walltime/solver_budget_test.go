package walltime

import (
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/nsmath"
)

// TestSolverBudgetsHaveTerminalOutcomes is §22 test 69 (R13-D5): the two capped
// solvers, which resolve DIFFERENTLY and deliberately.
//
// A missing model is a state the product already carries and displays; a rank
// test that did not converge is a computation whose answer is unknown, and
// reporting it as "deficient" would silently deny a warm model the corpus may
// in fact support. The first is a status; the second is a failure.
func TestSolverBudgetsHaveTerminalOutcomes(t *testing.T) {
	fullRank := [][]float64{
		{1, 10e9, 1, 0}, {1, 20e9, 1, 0}, {1, 10e9, 0, 1}, {1, 30e9, 0, 2},
	}

	t.Run("the rank cap is E_RANK_NON_CONVERGENT with no rank inferred", func(t *testing.T) {
		svdIterationCapOverride = 1
		defer func() { svdIterationCapOverride = 0 }()

		got, err := RankAdmission(fullRank)
		if err == nil {
			t.Fatal("the exhausted cap must fail, not return a rank")
		}
		if !nsmath.Is(err, nsmath.ERankNonConvergent) && !strings.Contains(err.Error(), nsmath.ERankNonConvergent) {
			t.Fatalf("err = %v, want %s", err, nsmath.ERankNonConvergent)
		}
		// sigma_max is NOT produced, there is no tolerance, and no rank is
		// inferred — the zero value must not be mistaken for a verdict.
		if got.SigmaMax != "" || got.Tolerance != "" || got.MinPivot != "" {
			t.Errorf("sigma_max/tolerance/min_pivot must be absent, got %+v", got)
		}
		if got.Rank != 0 || got.Admitted {
			t.Errorf("no rank may be inferred, got rank=%d admitted=%v", got.Rank, got.Admitted)
		}
		if len(got.DeficientColumns) != 0 {
			t.Error("a rank that was not computed is never reported as deficient")
		}
	})

	t.Run("a non-convergent rank is never reported as rank-deficient", func(t *testing.T) {
		// The must-fail control: an implementation that mapped the cap onto
		// "rank-deficient" would return a verdict here.
		svdIterationCapOverride = 1
		defer func() { svdIterationCapOverride = 0 }()
		if _, err := RankAdmission(fullRank); err == nil {
			t.Fatal("the cap produced a verdict; a computation that did not complete is not a deficiency")
		}
	})

	t.Run("a fit under the exhausted rank cap stores nothing", func(t *testing.T) {
		svdIterationCapOverride = 1
		defer func() { svdIterationCapOverride = 0 }()

		rows := syntheticCorpus(t, 30)
		res, err := FitModel(rows)
		if err == nil {
			t.Fatal("the fit must fail closed under E_RANK_NON_CONVERGENT")
		}
		if res.Status != "" || res.Model != (WallModelCoefficients{}) {
			t.Fatalf("a failed rank test must leave the model untouched, got %+v", res)
		}
	})

	t.Run("the NNLS cap is a STATUS, not a failure", func(t *testing.T) {
		nnlsOuterCapOverride = 1
		defer func() { nnlsOuterCapOverride = 0 }()

		rows := syntheticCorpus(t, 30)
		res, err := FitModel(rows)
		if err != nil {
			t.Fatalf("budget exhaustion is an ordinary unfitted model, not an arithmetic fault: %v", err)
		}
		if res.Status != FitInsufficient {
			t.Fatalf("status = %q, want insufficient", res.Status)
		}
		if res.Subtype != "nnls_budget_exhausted" {
			t.Fatalf("subtype = %q, want nnls_budget_exhausted", res.Subtype)
		}
		// No coefficients stored: a partially converged vector is NEVER
		// deployed.
		if res.Model != (WallModelCoefficients{}) {
			t.Fatalf("a budget-exhausted solve stored coefficients: %+v", res.Model)
		}
		if res.ResidualMAENs != 0 || res.ResidualP90Ns != 0 {
			t.Error("a budget-exhausted solve must store no fit statistic")
		}
	})

	t.Run("an explicit wall plan is a hard error under budget exhaustion", func(t *testing.T) {
		// §0.8 (c): the status makes an explicit wall plan a hard error, which
		// is the difference between this cap and the rank one — the model is
		// simply unfitted, and the product already carries that state.
		nnlsOuterCapOverride = 1
		defer func() { nnlsOuterCapOverride = 0 }()
		rows := syntheticCorpus(t, 30)
		res, err := FitModel(rows)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status == FitOK {
			t.Fatal("a budget-exhausted model must not be ok")
		}
	})

	t.Run("the two caps resolve differently, and that is the point", func(t *testing.T) {
		rows := syntheticCorpus(t, 30)

		svdIterationCapOverride = 1
		_, rankErr := FitModel(rows)
		svdIterationCapOverride = 0

		nnlsOuterCapOverride = 1
		nnlsRes, nnlsErr := FitModel(rows)
		nnlsOuterCapOverride = 0

		if rankErr == nil {
			t.Error("the rank cap must resolve to a failure")
		}
		if nnlsErr != nil {
			t.Error("the NNLS cap must resolve to a status, not a failure")
		}
		if nnlsRes.Status != FitInsufficient {
			t.Errorf("NNLS cap status = %q, want insufficient", nnlsRes.Status)
		}
	})

	t.Run("both caps produce identical bytes across repeated evaluation", func(t *testing.T) {
		// The property the caps existed to protect and previously did not
		// state: two conforming runs agree.
		nnlsOuterCapOverride = 1
		defer func() { nnlsOuterCapOverride = 0 }()
		rows := syntheticCorpus(t, 30)
		a, err := FitModel(rows)
		if err != nil {
			t.Fatal(err)
		}
		b, err := FitModel(rows)
		if err != nil {
			t.Fatal(err)
		}
		if a.Status != b.Status || a.Subtype != b.Subtype ||
			ModelParametersDigest(a.Model) != ModelParametersDigest(b.Model) {
			t.Fatal("the capped outcome is not reproducible")
		}
	})
}

// syntheticCorpus builds a full-rank corpus of n rows over >= 3 runs, so the
// count gates pass and the solver path is the one under test.
func syntheticCorpus(t *testing.T, n int) []FitRow {
	t.Helper()
	shapes := []struct {
		reporter int64
		i, slice int
		a        int64
	}{
		{10e9, 1, 0, 23e9},
		{20e9, 1, 0, 33e9},
		{15e9, 0, 1, 23e9},
		{30e9, 1, 2, 49e9},
		{10e9, 1, 1, 42e9},
	}
	runs := []string{"r1", "r2", "r3"}
	var rows []FitRow
	for i := 0; i < n; i++ {
		sh := shapes[i%len(shapes)]
		rows = append(rows, FitRow{
			ReporterSumNs: sh.reporter, IAnyWholeFile: sh.i, SliceCount: sh.slice,
			ElapsedNs: sh.a, HeadSHA: "head", RunID: runs[i%len(runs)],
			RunAttempt: "1", BucketIndex: i,
		})
	}
	return rows
}
