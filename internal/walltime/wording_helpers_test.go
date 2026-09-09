package walltime

import "github.com/invakid404/testbucket/internal/nsmath"

// meanForGate is the exact rational mean, narrowed for a relative-gate bound.
func meanForGate(v []int64) (int64, error) {
	q, err := nsmath.MeanRat(v)
	if err != nil {
		return 0, err
	}
	return nsmath.RoundHalfUpRat("mean_A", q)
}
