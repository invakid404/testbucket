package walltime

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Nanos is a nanosecond count that serialises as a JSON STRING.
//
// The reason is the canonical digest: RFC 8785 renders numbers through
// ECMAScript's double formatting, so an integer above 2^53 — which any epoch
// nanosecond count is — would round, and two verifiers could canonicalise the
// same record to different bytes. Carrying it as a string keeps the value
// exact and the digest stable, at the cost of one conversion at the boundary.
type Nanos int64

// MarshalJSON writes the exact decimal value in quotes.
func (n Nanos) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(strconv.FormatInt(int64(n), 10))), nil
}

// UnmarshalJSON accepts the string form and, for tolerance when reading a
// hand-written fixture, a plain integer.
func (n *Nanos) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("nanoseconds %q: %w", s, err)
		}
		*n = Nanos(v)
		return nil
	}
	var v int64
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("nanoseconds %s: %w", b, err)
	}
	*n = Nanos(v)
	return nil
}
