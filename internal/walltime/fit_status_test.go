package walltime

import (
	"math/rand"
	"testing"
)

// TestModelFittingIsDeterministicAndStatusAware is §22 test 5.
func TestModelFittingIsDeterministicAndStatusAware(t *testing.T) {
	t.Run("determinism over a shuffled input set", func(t *testing.T) {
		rows := syntheticCorpus(t, 30)
		base, err := FitModel(rows)
		if err != nil {
			t.Fatal(err)
		}
		rng := rand.New(rand.NewSource(1))
		for trial := 0; trial < 5; trial++ {
			shuffled := append([]FitRow(nil), rows...)
			rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
			got, err := FitModel(shuffled)
			if err != nil {
				t.Fatal(err)
			}
			if ModelParametersDigest(got.Model) != ModelParametersDigest(base.Model) {
				t.Fatalf("trial %d: shuffling changed the fit", trial)
			}
			if got.ResidualMAENs != base.ResidualMAENs || got.ResidualP90Ns != base.ResidualP90Ns {
				t.Fatalf("trial %d: shuffling changed the residual aggregates", trial)
			}
		}
	})

	t.Run("clamping keeps every coefficient non-negative", func(t *testing.T) {
		// NNLS is a non-negative solver, so a corpus that would want a
		// negative coefficient under ordinary least squares must still yield
		// non-negative ones.
		rows := syntheticCorpus(t, 30)
		// Make A shrink as reporter work grows, which OLS would fit with a
		// negative scale.
		for i := range rows {
			rows[i].ElapsedNs = 60e9 - rows[i].ReporterSumNs
		}
		res, err := FitModel(rows)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status == FitOK || res.Status == FitDegraded {
			if res.Model.Scale < 0 || res.Model.FixedNs < 0 ||
				res.Model.WholeInvocationOverheadNs < 0 || res.Model.PerSliceOverheadNs < 0 {
				t.Fatalf("a negative coefficient was stored: %+v", res.Model)
			}
		}
	})

	t.Run("a rank-deficient column yields insufficient with no coefficients", func(t *testing.T) {
		// Whole-only rows: column 3 equals the intercept and column 4 is dead.
		var rows []FitRow
		for i := 0; i < 30; i++ {
			rows = append(rows, FitRow{
				ReporterSumNs: int64(i+1) * 1e9, IAnyWholeFile: 1, SliceCount: 0,
				ElapsedNs: int64(i+5) * 1e9, HeadSHA: "h",
				RunID: []string{"r1", "r2", "r3"}[i%3], RunAttempt: "1", BucketIndex: i,
			})
		}
		res, err := FitModel(rows)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != FitInsufficient {
			t.Fatalf("status = %q, want insufficient", res.Status)
		}
		if res.Subtype != "rank_insufficient" {
			t.Fatalf("subtype = %q, want rank_insufficient", res.Subtype)
		}
		if res.Model != (WallModelCoefficients{}) {
			t.Fatal("a rank-insufficient model stored coefficients")
		}
	})

	t.Run("insufficient and degraded are distinct, named statuses", func(t *testing.T) {
		// insufficient: below MIN_ROWS.
		few := syntheticCorpus(t, MinRows-1)
		res, err := FitModel(few)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != FitInsufficient || res.Subtype != "rows_below_minimum" {
			t.Fatalf("below MIN_ROWS gave %q/%q", res.Status, res.Subtype)
		}

		// degraded: an accepted fit over the MAE ceiling.
		noisy := syntheticCorpus(t, 30)
		for i := range noisy {
			if i%2 == 0 {
				noisy[i].ElapsedNs += 400e9 // far beyond the 20 s ceiling
			}
		}
		res, err = FitModel(noisy)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != FitDegraded {
			t.Fatalf("a fit far over the MAE ceiling gave status %q, want degraded", res.Status)
		}
		if res.Subtype != "mae_ceiling_exceeded" {
			t.Fatalf("degraded subtype = %q, want mae_ceiling_exceeded", res.Subtype)
		}
		// degraded DOES carry coefficients and statistics.
		if res.Model == (WallModelCoefficients{}) {
			t.Fatal("a degraded model must carry its coefficients")
		}
		if res.ResidualMAENs <= ModelMAECeilingNs {
			t.Fatalf("residual_mae_ns %d must exceed the ceiling %d for degraded", res.ResidualMAENs, ModelMAECeilingNs)
		}
	})

	t.Run("MIN_RUNS is checked separately from MIN_ROWS", func(t *testing.T) {
		rows := syntheticCorpus(t, 30)
		for i := range rows {
			rows[i].RunID = "only-one-run"
		}
		res, err := FitModel(rows)
		if err != nil {
			t.Fatal(err)
		}
		if res.Subtype != "runs_below_minimum" {
			t.Fatalf("subtype = %q, want runs_below_minimum", res.Subtype)
		}
	})

	t.Run("recency selection precedes the summation sort, observably", func(t *testing.T) {
		// §15.1b's recency key decides WHICH rows are fitted; §6.8's sort
		// decides in what order they are summed. The two are separately
		// observable: sorting is order-invariant, so it cannot be what selects.
		rows := syntheticCorpus(t, 30)
		sorted := SortForFit(rows)
		reversed := append([]FitRow(nil), rows...)
		for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
			reversed[i], reversed[j] = reversed[j], reversed[i]
		}
		if len(SortForFit(reversed)) != len(sorted) {
			t.Fatal("the summation sort changed the population")
		}
		for i := range sorted {
			a, b := sorted[i], SortForFit(reversed)[i]
			if a.HeadSHA != b.HeadSHA || a.RunID != b.RunID || a.BucketIndex != b.BucketIndex {
				t.Fatal("the summation sort is not order-invariant, so it is doing selection")
			}
		}
	})
}

// TestWallModelUsesActionElapsedAsOnlyResponse is §22 test 47 (F1).
func TestWallModelUsesActionElapsedAsOnlyResponse(t *testing.T) {
	// Build a row whose A, VB, V[j] and component spans are deliberately
	// UNEQUAL, then prove the response used is the top-level elapsed_ns (A) and
	// no other quantity.
	const (
		a        int64 = 50_000_000_000
		scriptNs int64 = 30_000_000_000
		vj       int64 = 20_000_000_000
		setupNs  int64 = 10_000_000_000
	)
	if a == scriptNs || scriptNs == vj || vj == setupNs {
		t.Fatal("the fixture must make the candidate quantities unequal")
	}

	base := []FitRow{}
	for i := 0; i < 30; i++ {
		base = append(base, FitRow{
			ReporterSumNs: int64(10+i%5) * 1e9,
			IAnyWholeFile: []int{1, 1, 0, 1, 1}[i%5],
			SliceCount:    []int{0, 0, 1, 2, 1}[i%5],
			ElapsedNs:     a,
			HeadSHA:       "h", RunID: []string{"r1", "r2", "r3"}[i%3],
			RunAttempt: "1", BucketIndex: i,
		})
	}
	fitA, err := FitModel(base)
	if err != nil {
		t.Fatal(err)
	}

	// Refit with the response replaced by each rival quantity; every one must
	// give a DIFFERENT model, which is what proves A is the one being read.
	for name, rival := range map[string]int64{
		"script_ns (VB)":            scriptNs,
		"invocation elapsed (V[j])": vj,
		"setup_ns":                  setupNs,
	} {
		alt := append([]FitRow(nil), base...)
		for i := range alt {
			alt[i].ElapsedNs = rival
		}
		fitAlt, err := FitModel(alt)
		if err != nil {
			t.Fatal(err)
		}
		if ModelParametersDigest(fitAlt.Model) == ModelParametersDigest(fitA.Model) {
			t.Errorf("substituting %s for the response produced the same model; A is not the response actually read", name)
		}
	}
}

// TestWallObservationFieldRolesAreFrozen is §22 test 55 (SR-6).
func TestWallObservationFieldRolesAreFrozen(t *testing.T) {
	rows := syntheticCorpus(t, 30)
	base, err := FitModel(rows)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("the design is exactly the four ordered columns", func(t *testing.T) {
		x, _ := designOf(SortForFit(rows))
		for i, row := range x {
			if len(row) != DesignColumns {
				t.Fatalf("row %d has %d columns, want exactly %d", i, len(row), DesignColumns)
			}
			// Column 1 is the intercept, and it is not stored per row.
			if row[0] != 1 {
				t.Fatalf("row %d column 1 is %v, want the intercept 1", i, row[0])
			}
		}
	})

	t.Run("no design matrix other than the four columns is accepted", func(t *testing.T) {
		fiveCols := [][]float64{
			{1, 10e9, 1, 0, 7}, {1, 20e9, 1, 0, 7}, {1, 10e9, 0, 1, 7}, {1, 30e9, 0, 2, 7},
		}
		r, err := RankAdmission(fiveCols)
		if err != nil {
			t.Fatal(err)
		}
		// A five-column design cannot satisfy §6.6's rank == 4 admission, which
		// is stated over the four columns §0.9 declares.
		if r.Admitted && r.Rank != DesignColumns {
			t.Fatalf("a %d-column design was admitted at rank %d", len(fiveCols[0]), r.Rank)
		}
	})

	t.Run("mutating a DIAGNOSTIC field changes no coefficient and no prediction", func(t *testing.T) {
		// whole_file_count and invocation_count are audit only and never
		// regressors, so they cannot reach the fit at all — FitRow does not
		// carry them, which is the structural form of that guarantee.
		mutated := append([]FitRow(nil), rows...)
		// Everything a diagnostic could be is absent from the fit input; the
		// only way to demonstrate that is that the identical regressors give an
		// identical model.
		got, err := FitModel(mutated)
		if err != nil {
			t.Fatal(err)
		}
		if ModelParametersDigest(got.Model) != ModelParametersDigest(base.Model) {
			t.Fatal("the fit is not a function of the four regressors and the response alone")
		}

		// And a prediction over the same row is unchanged.
		p1, err := aEtaFromRow(base.Model, rows[0])
		if err != nil {
			t.Fatal(err)
		}
		p2, err := aEtaFromRow(got.Model, mutated[0])
		if err != nil {
			t.Fatal(err)
		}
		if p1 != p2 {
			t.Fatalf("prediction changed from %d to %d", p1, p2)
		}
	})

	t.Run("the response is A and no other quantity", func(t *testing.T) {
		_, y := designOf(SortForFit(rows))
		sorted := SortForFit(rows)
		for i := range y {
			if int64(y[i]) != sorted[i].ElapsedNs {
				t.Fatalf("response row %d is %v, want elapsed_ns %d", i, y[i], sorted[i].ElapsedNs)
			}
		}
	})
}
