package walltime

import "strconv"

// fmtSscan parses a shortest-round-trip decimal back to float64.
func fmtSscan(s string, out *float64) (int, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	*out = v
	return 1, nil
}
