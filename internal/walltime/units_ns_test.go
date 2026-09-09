package walltime

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

// oracle mirrors the parts of testdata/walltime/test58-feedback-oracle.json
// this test reads. The oracle is EVIDENCE: every value in it was produced by
// the normative planner, and the test compares those bytes rather than
// re-deriving them from prose.
type oracle struct {
	GroundTruthBillions struct {
		Fixed                   int `json:"fixed"`
		Scale                   int `json:"scale"`
		WholeInvocationOverhead int `json:"whole_invocation_overhead"`
		PerSliceOverhead        int `json:"per_slice_overhead"`
	} `json:"ground_truth_model_billions"`
	Corpus struct {
		RowShapes []struct {
			DesignRow []int    `json:"design_row"`
			A         int      `json:"A"`
			Count     int      `json:"count"`
			Runs      []string `json:"runs"`
		} `json:"row_shapes_billions"`
		Rows23   int `json:"rows_23"`
		RankAt23 int `json:"rank_at_23"`
		Row24    struct {
			DesignRow []int  `json:"design_row"`
			A         int    `json:"A"`
			Run       string `json:"run"`
		} `json:"row_24"`
		RankAt24 int `json:"rank_at_24"`
		Row25    struct {
			DesignRow []int  `json:"design_row"`
			A         int    `json:"A"`
			Run       string `json:"run"`
		} `json:"row_25"`
	} `json:"corpus"`
	FitAt24 struct {
		Exact        []string `json:"exact_rational_billions"`
		Coefficients struct {
			FixedNs                   int64   `json:"fixed_ns"`
			Scale                     float64 `json:"scale"`
			WholeInvocationOverheadNs int64   `json:"whole_invocation_overhead_ns"`
			PerSliceOverheadNs        int64   `json:"per_slice_overhead_ns"`
		} `json:"coefficients_ns"`
	} `json:"fit_at_24"`
	FitAt25 struct {
		Exact        []string `json:"exact_rational_billions"`
		Coefficients struct {
			FixedNs                   int64   `json:"fixed_ns"`
			Scale                     float64 `json:"scale"`
			WholeInvocationOverheadNs int64   `json:"whole_invocation_overhead_ns"`
			PerSliceOverheadNs        int64   `json:"per_slice_overhead_ns"`
		} `json:"coefficients_ns"`
	} `json:"fit_at_25"`
}

func loadOracle(t *testing.T) oracle {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "walltime", "test58-feedback-oracle.json"))
	if err != nil {
		t.Fatalf("read oracle: %v", err)
	}
	var o oracle
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatalf("parse oracle: %v", err)
	}
	return o
}

const bn = 1_000_000_000

// oracleRows expands the oracle's shape table into the 23-row corpus, then
// optionally appends rows 24 and 25.
func oracleRows(t *testing.T, o oracle, upTo int) []FitRow {
	t.Helper()
	var rows []FitRow
	add := func(dr []int, a int, run string, bucket int) {
		rows = append(rows, FitRow{
			ReporterSumNs: int64(dr[1]) * bn,
			IAnyWholeFile: dr[2],
			SliceCount:    dr[3],
			ElapsedNs:     int64(a) * bn,
			HeadSHA:       "head",
			RunID:         run,
			RunAttempt:    "1",
			BucketIndex:   bucket,
		})
	}
	bucket := 0
	for _, sh := range o.Corpus.RowShapes {
		for i := 0; i < sh.Count; i++ {
			add(sh.DesignRow, sh.A, sh.Runs[i%len(sh.Runs)], bucket)
			bucket++
		}
	}
	if len(rows) != o.Corpus.Rows23 {
		t.Fatalf("expanded %d rows from the oracle's shape table, want %d", len(rows), o.Corpus.Rows23)
	}
	if upTo >= 24 {
		add(o.Corpus.Row24.DesignRow, o.Corpus.Row24.A, o.Corpus.Row24.Run, bucket)
		bucket++
	}
	if upTo >= 25 {
		add(o.Corpus.Row25.DesignRow, o.Corpus.Row25.A, o.Corpus.Row25.Run, bucket)
	}
	return rows
}

// TestWallModelIsIntegerNanosecondsEndToEnd is §22 test 60 (S-1) and the
// acceptance test the registry names for ID-15.
func TestWallModelIsIntegerNanosecondsEndToEnd(t *testing.T) {
	o := loadOracle(t)

	t.Run("23 rows are rank 3 and insufficient, with no coefficients", func(t *testing.T) {
		rows := oracleRows(t, o, 23)
		res, err := FitModel(rows)
		if err != nil {
			t.Fatal(err)
		}
		if res.Rank.Rank != o.Corpus.RankAt23 {
			t.Fatalf("rank at 23 rows = %d, want %d", res.Rank.Rank, o.Corpus.RankAt23)
		}
		if res.Status != FitInsufficient {
			t.Fatalf("status = %q, want insufficient", res.Status)
		}
		// BOTH gates fail here: rank < 4 and rows < MIN_ROWS. §15.1c's
		// precedence puts the count first.
		if res.Subtype != "rows_below_minimum" {
			t.Fatalf("subtype = %q, want rows_below_minimum by §15.1c precedence", res.Subtype)
		}
		if res.Model != (WallModelCoefficients{}) {
			t.Fatal("an insufficient model must carry no coefficients")
		}
	})

	t.Run("row 24 takes the design to rank 4 and recovers the ground truth", func(t *testing.T) {
		rows := oracleRows(t, o, 24)
		res, err := FitModel(rows)
		if err != nil {
			t.Fatal(err)
		}
		if res.Rank.Rank != o.Corpus.RankAt24 {
			t.Fatalf("rank at 24 rows = %d, want %d", res.Rank.Rank, o.Corpus.RankAt24)
		}
		if res.Status != FitOK {
			t.Fatalf("status = %q (%s), want ok", res.Status, res.Subtype)
		}
		want := o.FitAt24.Coefficients
		if res.Model.FixedNs != want.FixedNs ||
			res.Model.WholeInvocationOverheadNs != want.WholeInvocationOverheadNs ||
			res.Model.PerSliceOverheadNs != want.PerSliceOverheadNs {
			t.Fatalf("coefficients = %+v, want %+v", res.Model, want)
		}
		if !closeEnough(res.Model.Scale, want.Scale) {
			t.Fatalf("scale = %v, want %v", res.Model.Scale, want.Scale)
		}
		// The oracle's exact rationals, in billions.
		assertExact(t, o.FitAt24.Exact, res.Model)
		// All four non-negative, which is what NNLS guarantees.
		if res.Model.FixedNs < 0 || res.Model.Scale < 0 ||
			res.Model.WholeInvocationOverheadNs < 0 || res.Model.PerSliceOverheadNs < 0 {
			t.Fatal("NNLS produced a negative coefficient")
		}
	})

	t.Run("row 25 refits and three of four coefficients change", func(t *testing.T) {
		res24, err := FitModel(oracleRows(t, o, 24))
		if err != nil {
			t.Fatal(err)
		}
		res25, err := FitModel(oracleRows(t, o, 25))
		if err != nil {
			t.Fatal(err)
		}
		if res25.Status != FitOK {
			t.Fatalf("status = %q (%s), want ok", res25.Status, res25.Subtype)
		}
		want := o.FitAt25.Coefficients
		if res25.Model.FixedNs != want.FixedNs ||
			res25.Model.WholeInvocationOverheadNs != want.WholeInvocationOverheadNs ||
			res25.Model.PerSliceOverheadNs != want.PerSliceOverheadNs {
			t.Fatalf("coefficients = %+v, want %+v", res25.Model, want)
		}
		if !closeEnough(res25.Model.Scale, want.Scale) {
			t.Fatalf("scale = %v, want %v", res25.Model.Scale, want.Scale)
		}
		assertExact(t, o.FitAt25.Exact, res25.Model)

		// fixed_ns is unchanged; the other three move.
		if res25.Model.FixedNs != res24.Model.FixedNs {
			t.Error("fixed_ns changed between rows 24 and 25; the oracle says it does not")
		}
		if res25.Model.Scale == res24.Model.Scale ||
			res25.Model.WholeInvocationOverheadNs == res24.Model.WholeInvocationOverheadNs ||
			res25.Model.PerSliceOverheadNs == res24.Model.PerSliceOverheadNs {
			t.Error("three of four coefficients must change between rows 24 and 25")
		}
	})

	t.Run("the published residual aggregates reproduce", func(t *testing.T) {
		// §22 test 58's R12-D7 row: over the 25 rows with the deployed row-25
		// coefficients, residual_mae_ns = 1 545 660 377 (round_half_up of
		// 1 545 660 377.2) and residual_p90_ns = 1 811 320 754 at nearest rank
		// index ceil(0.9 x 25) = 23.
		res, err := FitModel(oracleRows(t, o, 25))
		if err != nil {
			t.Fatal(err)
		}
		if res.ResidualMAENs != 1_545_660_377 {
			t.Errorf("residual_mae_ns = %d, want 1545660377", res.ResidualMAENs)
		}
		if res.ResidualP90Ns != 1_811_320_754 {
			t.Errorf("residual_p90_ns = %d, want 1811320754", res.ResidualP90Ns)
		}
	})

	t.Run("the superseded mixed-unit expression differs by a factor of 1e9", func(t *testing.T) {
		// §0.9's superseded form divided three coefficients by 1e9 while
		// leaving scale x sum(base SECONDS) unconverted. On this fixture the
		// two disagree by 1e9, so the test cannot pass under it.
		m := WallModelCoefficients{
			FixedNs: 5 * bn, Scale: 1.0,
			WholeInvocationOverheadNs: 8 * bn, PerSliceOverheadNs: 3 * bn,
		}
		row := FitRow{ReporterSumNs: 30 * bn, IAnyWholeFile: 1, SliceCount: 2}
		correct, err := aEtaFromRow(m, row)
		if err != nil {
			t.Fatal(err)
		}
		// The superseded expression, literally.
		superseded := float64(m.FixedNs)/1e9 +
			m.Scale*(float64(row.ReporterSumNs)/1e9) +
			float64(m.WholeInvocationOverheadNs)/1e9 +
			float64(m.PerSliceOverheadNs)/1e9*float64(row.SliceCount)
		if superseded*1e9 != float64(correct) {
			t.Fatalf("the fixture no longer separates the two expressions: correct %d, superseded %v", correct, superseded)
		}
		if float64(correct) == superseded {
			t.Fatal("the two expressions agree; the 1e9 error is not exercised")
		}
	})

	t.Run("no serialized *_ns field is a float and no predictor is in seconds", func(t *testing.T) {
		res, err := FitModel(oracleRows(t, o, 25))
		if err != nil {
			t.Fatal(err)
		}
		// The three nanosecond coefficients are int64 by type; scale is the
		// one float64 and is dimensionless.
		var _ int64 = res.Model.FixedNs
		var _ int64 = res.Model.WholeInvocationOverheadNs
		var _ int64 = res.Model.PerSliceOverheadNs
		var _ float64 = res.Model.Scale
		var _ int64 = res.ResidualMAENs
		var _ int64 = res.ResidualP90Ns
	})

	t.Run("model_parameters_digest quotes scale so no formatter can vary it", func(t *testing.T) {
		m := WallModelCoefficients{FixedNs: 5 * bn, Scale: 1.0, WholeInvocationOverheadNs: 8 * bn, PerSliceOverheadNs: 3 * bn}
		d := ModelParametersDigest(m)
		if !d.Valid() {
			t.Fatalf("digest %q is not valid", d)
		}
		// A different scale changes the digest.
		m2 := m
		m2.Scale = 1.0000000000000002
		if ModelParametersDigest(m2) == d {
			t.Fatal("a changed scale did not change the digest")
		}
		// And it is stable across repeated evaluation.
		for i := 0; i < 3; i++ {
			if ModelParametersDigest(m) != d {
				t.Fatal("the digest is not a pure function of the coefficients")
			}
		}
	})

	t.Run("the fit is deterministic across shuffled input order", func(t *testing.T) {
		rows := oracleRows(t, o, 25)
		a, err := FitModel(rows)
		if err != nil {
			t.Fatal(err)
		}
		shuffled := append([]FitRow(nil), rows...)
		for i, j := 0, len(shuffled)-1; i < j; i, j = i+1, j-1 {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		}
		c, err := FitModel(shuffled)
		if err != nil {
			t.Fatal(err)
		}
		if ModelParametersDigest(a.Model) != ModelParametersDigest(c.Model) {
			t.Fatalf("shuffling changed the fit: %+v vs %+v", a.Model, c.Model)
		}
		if a.ResidualMAENs != c.ResidualMAENs || a.ResidualP90Ns != c.ResidualP90Ns {
			t.Fatal("shuffling changed the residual aggregates")
		}
	})
}

// assertExact compares the fitted coefficients against the oracle's exact
// rationals, expressed in billions of nanoseconds.
func assertExact(t *testing.T, exact []string, m WallModelCoefficients) {
	t.Helper()
	if len(exact) != 4 {
		t.Fatalf("oracle exact rationals = %v, want four entries", exact)
	}
	want := []*big.Rat{}
	for _, s := range exact {
		r, ok := new(big.Rat).SetString(s)
		if !ok {
			t.Fatalf("oracle rational %q does not parse", s)
		}
		want = append(want, r)
	}
	bnR := new(big.Rat).SetInt64(bn)
	check := func(name string, got *big.Rat, w *big.Rat) {
		scaled := new(big.Rat).Mul(w, bnR)
		// The stored value is round_half_up of the exact rational.
		rounded, err := nsRoundRat(scaled)
		if err != nil {
			t.Fatal(err)
		}
		if gotI, _ := got.Float64(); gotI != float64(rounded) {
			t.Errorf("%s = %v, want round_half_up(%s x 1e9) = %d", name, gotI, w.RatString(), rounded)
		}
	}
	check("fixed_ns", new(big.Rat).SetInt64(m.FixedNs), want[0])
	// scale is dimensionless: compare it directly, not scaled by 1e9.
	if sw, _ := want[1].Float64(); !closeEnough(m.Scale, sw) {
		t.Errorf("scale = %v, want %s = %v", m.Scale, want[1].RatString(), sw)
	}
	check("whole_invocation_overhead_ns", new(big.Rat).SetInt64(m.WholeInvocationOverheadNs), want[2])
	check("per_slice_overhead_ns", new(big.Rat).SetInt64(m.PerSliceOverheadNs), want[3])
}

func closeEnough(a, b float64) bool {
	if a == b {
		return true
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	scale := 1.0
	if b != 0 {
		scale = b
		if scale < 0 {
			scale = -scale
		}
	}
	return d/scale < 1e-12
}

func nsRoundRat(q *big.Rat) (int64, error) {
	return roundHalfUpRatForTest(q)
}

var _ = fmt.Sprintf
