package walltime

import (
	"github.com/invakid404/testbucket/internal/nsmath"
)

// This file is the walltime package's view of the arithmetic domain. Contract
// §1.1 is the single normative authority for the semantics; internal/nsmath
// implements it, and nothing here restates a rule.

// ReporterEwmaNs converts a stored reporter EWMA weight, which v0.2.2 keeps in
// seconds, to the integer nanoseconds the model works in:
//
//	reporter_ewma_ns[u] = round_half_up(reporter_ewma_seconds[u] × 1e9)
//
// This is the only place a seconds-valued stored weight crosses into the
// nanosecond domain. §0.9's superseded form fitted a seconds-valued predictor
// against a nanosecond response, which is wrong by a factor of 1e9.
func ReporterEwmaNs(seconds float64) (int64, error) {
	return nsmath.RoundHalfUp("reporter_ewma_ns", seconds*1e9)
}

// ReporterSumNs is `reporter_sum_ns(b) = Σ_{u ∈ b} reporter_ewma_ns[u]` of
// contract §0.9: an int64 nanosecond count, frozen at plan time.
//
// The accumulation happens in the exact integer domain and narrows once, so a
// bucket whose weights sum past int64 reports E_NS_OVERFLOW rather than
// wrapping to a small or negative weight that would quietly win the partition.
func ReporterSumNs(unitNs []int64) (int64, error) {
	return nsmath.SumNs("reporter_sum_ns", unitNs...)
}

// Round1Seconds renders §0.9's display boundary,
// `est_seconds(b) = round1(A_eta_ns(b) / 1e9)`, evaluated on the integer so a
// nanosecond count above 2^53 rounds where the exact value says it should.
func Round1Seconds(ns int64) float64 { return nsmath.Round1Seconds(ns) }
