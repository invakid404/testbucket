// Package nsmath is the checked-integer arithmetic domain of
// acceptance-contract.md §1.1, its rounding operators (§1.3) and the
// arbitrary-precision rational comparison of §1.2.
//
// The contract fixes the *domain* as well as the algebra: every serialized
// `*_ns` quantity, every ring value and every residual is a signed 64-bit
// nanosecond count, but the sums and products that produce them are evaluated
// exactly first and only then admitted to int64. Two's-complement wrap-around
// and saturating arithmetic are both non-conforming, so no accumulator in this
// package is an int64 — the accumulators are math/big and the narrowing is a
// single range-checked step with a named site.
package nsmath

import (
	"fmt"
	"math"
	"math/big"
)

// Terminal condition names of §1.3. They are the reported condition, never a
// serialized field, so they live as error kinds rather than as constants on
// the wire.
const (
	ENSOverflow        = "E_NS_OVERFLOW"
	ENonFinite         = "E_NON_FINITE"
	EConversionRange   = "E_CONVERSION_RANGE"
	ERankNonConvergent = "E_RANK_NON_CONVERGENT"
)

// Error is a terminal arithmetic outcome. Every one is fail-closed: the
// containing operation writes no plan, matrix, model, observation or
// calibration-evidence document. The message names the site and the operands
// because §1.3 requires a failure to be diagnosable without re-deriving it.
type Error struct {
	Condition string
	Site      string
	Operands  []string
}

func (e *Error) Error() string {
	if len(e.Operands) == 0 {
		return fmt.Sprintf("%s at %s", e.Condition, e.Site)
	}
	return fmt.Sprintf("%s at %s: operands %v", e.Condition, e.Site, e.Operands)
}

// Is reports whether err carries the named terminal condition.
func Is(err error, condition string) bool {
	var e *Error
	for err != nil {
		if v, ok := err.(*Error); ok {
			e = v
			break
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return e != nil && e.Condition == condition
}

func overflow(site string, operands ...string) error {
	return &Error{Condition: ENSOverflow, Site: site, Operands: operands}
}

func nonFinite(site string, operands ...string) error {
	return &Error{Condition: ENonFinite, Site: site, Operands: operands}
}

func conversionRange(site string, operands ...string) error {
	return &Error{Condition: EConversionRange, Site: site, Operands: operands}
}

var (
	minInt64 = big.NewInt(math.MinInt64)
	maxInt64 = big.NewInt(math.MaxInt64)
)

// InInt64 reports whether the exact value v is representable as an int64.
func InInt64(v *big.Int) bool {
	return v.Cmp(minInt64) >= 0 && v.Cmp(maxInt64) <= 0
}

// Narrow admits an exact accumulator to int64, range-checked. This is the only
// way a value computed in the wide domain becomes a serializable nanosecond
// count, and the site name travels with the failure.
func Narrow(site string, v *big.Int) (int64, error) {
	if !InInt64(v) {
		return 0, overflow(site, v.String())
	}
	return v.Int64(), nil
}

// SumNs adds nanosecond counts in the exact integer domain and narrows once at
// the end. It is the `reporter_sum_ns` and four-term-objective accumulator of
// §1.1's table: an int64 running total would wrap, which the contract outlaws.
func SumNs(site string, terms ...int64) (int64, error) {
	acc := new(big.Int)
	for _, t := range terms {
		acc.Add(acc, big.NewInt(t))
	}
	return Narrow(site, acc)
}

// SumExact adds nanosecond counts and returns the exact sum without narrowing,
// for the `Σ|r_i|` site that §1.1 marks "not narrowed — it is a numerator".
func SumExact(terms ...int64) *big.Int {
	acc := new(big.Int)
	for _, t := range terms {
		acc.Add(acc, big.NewInt(t))
	}
	return acc
}

// AbsExact returns |a − b| exactly. Residuals are formed here rather than in
// int64 because a difference of two legal int64 nanosecond counts can exceed
// int64.
func AbsExact(a, b int64) *big.Int {
	d := new(big.Int).Sub(big.NewInt(a), big.NewInt(b))
	return d.Abs(d)
}

// RoundHalfUp implements §1.3's `round_half_up`: the integer nearest x, ties
// resolved to the integer of larger magnitude. x must be finite and the result
// must lie in int64.
//
// The tie rule is pinned in both directions even though every argument this
// product reaches it with is non-negative, so no implementation has to choose.
func RoundHalfUp(site string, x float64) (int64, error) {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0, conversionRange(site, fmt.Sprint(x))
	}
	// math.Round is already half-away-from-zero, which is the rule §1.3 states.
	r := math.Round(x)
	if r > math.MaxInt64 || r < math.MinInt64 {
		return 0, conversionRange(site, fmt.Sprint(x))
	}
	// The float64 comparison above admits 2^63 exactly, which is one past the
	// range, so re-check through the exact integer.
	bf := new(big.Float).SetFloat64(r)
	bi, _ := bf.Int(nil)
	if !InInt64(bi) {
		return 0, conversionRange(site, fmt.Sprint(x))
	}
	return bi.Int64(), nil
}

// RoundHalfUpRat implements `round_half_up` over an exact rational, which is
// how `residual_mae_ns` is produced: §1.1 requires the mean to be "the exact
// rational mean, never a float".
func RoundHalfUpRat(site string, q *big.Rat) (int64, error) {
	num, den := q.Num(), q.Denom()
	// Exact half-away-from-zero: (2|num| + den) / (2den), truncated.
	n := new(big.Int).Abs(num)
	n.Mul(n, big.NewInt(2))
	n.Add(n, den)
	d := new(big.Int).Mul(den, big.NewInt(2))
	n.Quo(n, d)
	if num.Sign() < 0 {
		n.Neg(n)
	}
	if !InInt64(n) {
		return 0, conversionRange(site, q.RatString())
	}
	return n.Int64(), nil
}

// MulScale evaluates `round_half_up(scale × reporter_sum_ns)` — the one place
// the dimensionless float64 coefficient meets a nanosecond count. A stored
// scale that parses non-finite, or a non-finite product, is E_NON_FINITE; the
// rounding range check is E_CONVERSION_RANGE.
func MulScale(site string, scale float64, ns int64) (int64, error) {
	if math.IsNaN(scale) || math.IsInf(scale, 0) {
		return 0, nonFinite(site, fmt.Sprint(scale), fmt.Sprint(ns))
	}
	p := scale * float64(ns)
	if math.IsNaN(p) || math.IsInf(p, 0) {
		return 0, nonFinite(site, fmt.Sprint(scale), fmt.Sprint(ns))
	}
	// Round through the exact rational so a product above 2^53 does not lose
	// the bits that decide the rounding direction.
	r := new(big.Rat).SetFloat64(scale)
	if r == nil {
		return 0, nonFinite(site, fmt.Sprint(scale), fmt.Sprint(ns))
	}
	r.Mul(r, new(big.Rat).SetInt64(ns))
	v, err := RoundHalfUpRat(site, r)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// Round1Tenths implements §0.9's display boundary exactly: it returns
// `round1(ns / 1e9)` as a count of tenths of a second.
//
// The division is never performed in float64. `ns/1e9` rounded to a multiple
// of 0.1 is `round_half_up(ns/1e8)` tenths, and that is evaluated on the
// integer, so a nanosecond count above 2^53 still rounds where the exact value
// says it should.
func Round1Tenths(ns int64) int64 {
	q := new(big.Rat).SetFrac(big.NewInt(ns), big.NewInt(100_000_000))
	// The argument is an exact rational and the result is bounded by
	// ns/1e8, so neither error branch of RoundHalfUpRat is reachable here.
	t, err := RoundHalfUpRat("round1", q)
	if err != nil {
		panic("nsmath: round1 of an int64 nanosecond count cannot overflow: " + err.Error())
	}
	return t
}

// Round1Seconds renders §0.9's `est_seconds(b) = round1(A_eta_ns(b) / 1e9)`.
// The value is a multiple of 0.1 by construction, so the float64 it returns
// round-trips through JSON as the one-decimal number the display promises.
func Round1Seconds(ns int64) float64 {
	return float64(Round1Tenths(ns)) / 10
}

// CmpRat compares two exact rationals by cross-multiplication in
// arbitrary-precision integers, which is §1.2's sole admitted procedure. No
// division and no fixed-width integer appears in it: the campaign witness of
// §1.2 forms products of 128 magnitude bits that differ by 2, so any fixed
// width — signed int128 included — decides it wrongly.
func CmpRat(a, b *big.Rat) int {
	left := new(big.Int).Mul(a.Num(), b.Denom())
	right := new(big.Int).Mul(b.Num(), a.Denom())
	return left.Cmp(right)
}

// CrossProducts returns the two arbitrary-precision products CmpRat forms, so
// a test can assert the comparison really was decided on the full width.
func CrossProducts(a, b *big.Rat) (left, right *big.Int) {
	return new(big.Int).Mul(a.Num(), b.Denom()), new(big.Int).Mul(b.Num(), a.Denom())
}

// MeanRat is the exact rational mean of a nanosecond population. §1.1 forbids
// evaluating it as a float, and an even-cardinality campaign mean is routinely
// non-integral.
func MeanRat(values []int64) (*big.Rat, error) {
	if len(values) == 0 {
		return nil, &Error{Condition: EConversionRange, Site: "mean", Operands: []string{"empty population"}}
	}
	return new(big.Rat).SetFrac(SumExact(values...), big.NewInt(int64(len(values)))), nil
}

// MedianRat is the exact rational median of §10: for an even population, the
// arithmetic mean of the two middle values. values must already be sorted
// ascending.
func MedianRat(sorted []int64) (*big.Rat, error) {
	n := len(sorted)
	if n == 0 {
		return nil, &Error{Condition: EConversionRange, Site: "median", Operands: []string{"empty population"}}
	}
	if n%2 == 1 {
		return new(big.Rat).SetInt64(sorted[n/2]), nil
	}
	lo, hi := sorted[n/2-1], sorted[n/2]
	return new(big.Rat).SetFrac(SumExact(lo, hi), big.NewInt(2)), nil
}

// NearestRank returns the 1-based nearest-rank element at percentile p of a
// sorted population, with no interpolation — the `residual_p90_ns` rule of
// §1.1 at index `ceil(p × n)`.
func NearestRank(sorted []int64, num, den int) (int64, error) {
	n := len(sorted)
	if n == 0 {
		return 0, &Error{Condition: EConversionRange, Site: "nearest_rank", Operands: []string{"empty population"}}
	}
	// ceil(num/den × n) in integers.
	idx := (num*n + den - 1) / den
	if idx < 1 {
		idx = 1
	}
	if idx > n {
		idx = n
	}
	return sorted[idx-1], nil
}
