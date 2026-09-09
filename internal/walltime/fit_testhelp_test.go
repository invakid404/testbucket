package walltime

import (
	"math/big"

	"github.com/invakid404/testbucket/internal/nsmath"
)

func roundHalfUpRatForTest(q *big.Rat) (int64, error) {
	return nsmath.RoundHalfUpRat("oracle", q)
}
