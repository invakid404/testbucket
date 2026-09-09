package nsmath

import (
	"math"
	"math/big"
	"testing"
)

// TestWallArithmeticIsCheckedAndFailsClosed is acceptance-contract.md §22 test
// 68: the checked-integer domain of §1.1, its terminal outcomes (§1.3) and the
// arbitrary-precision campaign comparison of §1.2.
//
// Each subtest carries a *control* — the non-conforming implementation the
// contract names — and asserts the conforming path does not produce it. A test
// that only checked the right answer would pass against a wrapping
// accumulator whose wrap happened to be invisible on the fixture.
func TestWallArithmeticIsCheckedAndFailsClosed(t *testing.T) {
	t.Run("W240 residual sum is exact and never narrowed", func(t *testing.T) {
		rows := make([]int64, 240)
		for i := range rows {
			rows[i] = math.MaxInt64
		}

		sum := SumExact(rows...)
		if got, want := sum.String(), "2213609288845146193680"; got != want {
			t.Fatalf("exact Σ|r_i| = %s, want %s", got, want)
		}
		if InInt64(sum) {
			t.Fatal("Σ|r_i| must exceed int64 on this fixture; it is a numerator and is never narrowed")
		}

		mean, err := MeanRat(rows)
		if err != nil {
			t.Fatal(err)
		}
		mae, err := RoundHalfUpRat("residual_mae_ns", mean)
		if err != nil {
			t.Fatal(err)
		}
		if mae != math.MaxInt64 {
			t.Fatalf("residual_mae_ns = %d, want %d", mae, int64(math.MaxInt64))
		}

		// Control: naive signed-int64 accumulation wraps to -240. §1.1 outlaws
		// it, so the conforming value must differ from it.
		var wrapped int64
		for _, r := range rows {
			wrapped += r
		}
		if wrapped != -240 {
			t.Fatalf("wrapping control = %d, want -240 (fixture no longer exercises the wrap)", wrapped)
		}
		if big.NewInt(wrapped).Cmp(sum) == 0 {
			t.Fatal("conforming sum equals the wrapping control")
		}

		// Control: a saturating accumulator pins at MaxInt64, which is equally
		// non-conforming — it is not the exact sum either.
		saturating := int64(0)
		for _, r := range rows {
			if saturating > math.MaxInt64-r {
				saturating = math.MaxInt64
				continue
			}
			saturating += r
		}
		if big.NewInt(saturating).Cmp(sum) == 0 {
			t.Fatal("conforming sum equals the saturating control")
		}
	})

	t.Run("narrowing sites report E_NS_OVERFLOW", func(t *testing.T) {
		// reporter_sum_ns and the four-term objective both narrow to int64.
		if _, err := SumNs("reporter_sum_ns", math.MaxInt64, 1); !Is(err, ENSOverflow) {
			t.Fatalf("reporter_sum_ns overflow: err = %v, want %s", err, ENSOverflow)
		}
		if _, err := SumNs("a_eta_ns", math.MaxInt64, math.MaxInt64, 1, 1); !Is(err, ENSOverflow) {
			t.Fatalf("a_eta_ns overflow: err = %v, want %s", err, ENSOverflow)
		}
		// r_i is formed exactly, then narrowed.
		r := AbsExact(math.MinInt64, math.MaxInt64)
		if _, err := Narrow("residual", r); !Is(err, ENSOverflow) {
			t.Fatalf("residual overflow: err = %v, want %s", err, ENSOverflow)
		}

		var e *Error
		if _, err := SumNs("reporter_sum_ns", math.MaxInt64, 1); err != nil {
			e, _ = err.(*Error)
		}
		if e == nil || e.Site != "reporter_sum_ns" || len(e.Operands) == 0 {
			t.Fatalf("failure must name the site and the operands, got %+v", e)
		}
	})

	t.Run("non-finite scale is E_NON_FINITE", func(t *testing.T) {
		for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			if _, err := MulScale("scale_x_reporter_sum_ns", bad, 1); !Is(err, ENonFinite) {
				t.Fatalf("scale %v: err = %v, want %s", bad, err, ENonFinite)
			}
		}
		// A finite scale whose product with a legal count is non-finite.
		if _, err := MulScale("scale_x_reporter_sum_ns", math.MaxFloat64, math.MaxInt64); !Is(err, ENonFinite) {
			t.Fatal("a non-finite product must be E_NON_FINITE")
		}
	})

	t.Run("rounding range is E_CONVERSION_RANGE", func(t *testing.T) {
		for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			if _, err := RoundHalfUp("round_half_up", bad); !Is(err, EConversionRange) {
				t.Fatalf("round_half_up(%v): err = %v, want %s", bad, err, EConversionRange)
			}
		}
		if _, err := RoundHalfUp("round_half_up", 1e30); !Is(err, EConversionRange) {
			t.Fatal("a finite argument rounding outside int64 must be E_CONVERSION_RANGE")
		}
	})

	t.Run("round_half_up ties resolve away from zero at both signs", func(t *testing.T) {
		cases := []struct {
			in   *big.Rat
			want int64
		}{
			{big.NewRat(1, 2), 1},
			{big.NewRat(-1, 2), -1},
			{big.NewRat(3, 2), 2},
			{big.NewRat(-3, 2), -2},
			{big.NewRat(5, 2), 3},
			{big.NewRat(-5, 2), -3},
		}
		for _, c := range cases {
			got, err := RoundHalfUpRat("tie", c.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("round_half_up(%s) = %d, want %d", c.in.RatString(), got, c.want)
			}
		}
	})

	t.Run("campaign DA comparison needs arbitrary precision", func(t *testing.T) {
		// §1.2's witness. Both runs are legal: eight complete buckets, every
		// value a positive int64 nanosecond count.
		M := int64(math.MaxInt64)
		P := []int64{1, 1, 1, M - 1, M, M, M, M}
		Q := []int64{1, 1, 1, M - 2, M - 1, M - 1, M - 1, M - 1}

		da := func(sorted []int64) *big.Rat {
			med, err := MedianRat(sorted)
			if err != nil {
				t.Fatal(err)
			}
			return new(big.Rat).Quo(new(big.Rat).SetInt64(sorted[len(sorted)-1]), med)
		}
		dp, dq := da(P), da(Q)

		if got, want := dp.RatString(), "18446744073709551614/18446744073709551613"; got != want {
			t.Fatalf("DA(P) = %s, want %s", got, want)
		}
		if got, want := dq.RatString(), "18446744073709551612/18446744073709551611"; got != want {
			t.Fatalf("DA(Q) = %s, want %s", got, want)
		}

		left, right := CrossProducts(dp, dq)
		if got, want := left.String(), "340282366920938463334247398915801350154"; got != want {
			t.Fatalf("cross product left = %s, want %s", got, want)
		}
		if got, want := right.String(), "340282366920938463334247398915801350156"; got != want {
			t.Fatalf("cross product right = %s, want %s", got, want)
		}

		// Each product needs 128 magnitude bits, so both exceed the signed
		// int128 maximum; they differ by 2, so no truncation decides it.
		int128Max, _ := new(big.Int).SetString("170141183460469231731687303715884105727", 10)
		for _, p := range []*big.Int{left, right} {
			if p.BitLen() != 128 {
				t.Fatalf("cross product has %d magnitude bits, want 128", p.BitLen())
			}
			if p.Cmp(int128Max) <= 0 {
				t.Fatal("cross product must exceed the signed int128 maximum")
			}
		}
		if d := new(big.Int).Sub(right, left); d.Cmp(big.NewInt(2)) != 0 {
			t.Fatalf("cross products differ by %s, want 2", d)
		}

		if CmpRat(dp, dq) >= 0 {
			t.Fatal("want DA(P) < DA(Q)")
		}

		// Control: a signed-int128 evaluation cannot hold either operand. The
		// products wrap to NEGATIVE values, so an implementation that formed
		// them in that type would be comparing two corrupted magnitudes — the
		// executable form of "fails any implementation that evaluates the
		// comparison in a fixed-width type".
		twoPow128 := new(big.Int).Lsh(big.NewInt(1), 128)
		asSignedInt128 := func(v *big.Int) *big.Int {
			w := new(big.Int).Mod(v, twoPow128)
			if w.Cmp(int128Max) > 0 {
				w.Sub(w, twoPow128)
			}
			return w
		}
		for name, p := range map[string]*big.Int{"left": left, "right": right} {
			if w := asSignedInt128(p); w.Sign() >= 0 {
				t.Fatalf("%s cross product must wrap negative in signed int128, got %s", name, w)
			}
		}
	})

	t.Run("nearest rank takes the ceil index with no interpolation", func(t *testing.T) {
		sorted := make([]int64, 25)
		for i := range sorted {
			sorted[i] = int64(i + 1)
		}
		// ceil(0.9 × 25) = 23, 1-based.
		got, err := NearestRank(sorted, 9, 10)
		if err != nil {
			t.Fatal(err)
		}
		if got != 23 {
			t.Fatalf("residual_p90 index element = %d, want 23", got)
		}
	})
}

// TestDisplayBoundaryDividesOnce is the §0.9 display rule: est_seconds is
// round1 of the complete A_eta_ns expression and nothing is divided before it.
func TestDisplayBoundaryDividesOnce(t *testing.T) {
	cases := []struct {
		ns   int64
		want float64
	}{
		{0, 0},
		{49_999_999, 0},
		{50_000_000, 0.1},
		{150_000_000, 0.2},
		{1_000_000_000, 1.0},
		{18_000_000_000, 18.0},
		{24_811_320_754, 24.8},
		{16_018_867_925, 16.0},
	}
	for _, c := range cases {
		if got := Round1Seconds(c.ns); got != c.want {
			t.Errorf("round1(%d/1e9) = %v, want %v", c.ns, got, c.want)
		}
	}
}
