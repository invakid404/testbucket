package core

import "github.com/invakid404/testbucket/internal/nsmath"

func sumNsForTest(v []int64) (int64, error) { return nsmath.SumNs("reporter_sum_ns", v...) }
