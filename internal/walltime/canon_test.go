package walltime

import (
	"encoding/json"
	"strings"
	"testing"
)

// The canonical form is the identity of every receipt in this package, so
// these are conformance tests, not style tests: a change that makes any of
// them fail changes what a previously issued digest means.
func TestCanonicalJSON(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "object members sort by code unit, not by declaration",
			in:   map[string]any{"b": 1, "a": 2, "C": 3},
			want: `{"C":3,"a":2,"b":1}`,
		},
		{
			name: "no insignificant whitespace",
			in:   map[string]any{"a": []any{1, 2, 3}},
			want: `{"a":[1,2,3]}`,
		},
		{
			name: "only the mandatory and short escapes",
			in:   map[string]any{"k": "a\"b\\c\nd\tef"},
			want: `{"k":"a\"b\\c\nd\tef"}`,
		},
		{
			name: "non-ASCII stays literal UTF-8",
			in:   map[string]any{"k": "héllo→"},
			want: "{\"k\":\"héllo→\"}",
		},
		{
			name: "ECMAScript number formatting",
			in:   map[string]any{"a": 1.0, "b": 0.5, "c": 1e21, "d": 1e-7},
			want: `{"a":1,"b":0.5,"c":1e+21,"d":1e-7}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalJSON(tc.in)
			if err != nil {
				t.Fatalf("CanonicalJSON: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// A digest must not depend on the order the fields happened to be written in.
func TestDigestIsOrderIndependent(t *testing.T) {
	a := map[string]any{"one": 1, "two": map[string]any{"x": "y", "a": "b"}}
	b := map[string]any{"two": map[string]any{"a": "b", "x": "y"}, "one": 1}
	da, err := DigestJSON(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := DigestJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Errorf("digests differ by field order: %s vs %s", da, db)
	}
	if !strings.HasPrefix(string(da), "sha256:") {
		t.Errorf("digest %q is not self-describing", da)
	}
}

// An integer that cannot survive a float64 round trip must be refused, not
// rounded: a receipt whose digest depends on rounding is not an identity.
func TestCanonicalJSONRefusesInexactIntegers(t *testing.T) {
	_, err := CanonicalJSON(map[string]any{"ns": json.Number("1756684800123456789")})
	if err == nil {
		t.Fatalf("an epoch-nanosecond integer was accepted; it cannot be represented exactly")
	}
	if !strings.Contains(err.Error(), "string") {
		t.Errorf("the error should point at the string workaround, got %v", err)
	}
	// Which is why record readings are Nanos, and Nanos serialises as a string.
	if _, err := CanonicalJSON(Instant{ClockID: ClockMonotonic, Mono: Nanos(1756684800123456789)}); err != nil {
		t.Errorf("an Instant with a large reading must canonicalise: %v", err)
	}
}

func TestNanosRoundTrip(t *testing.T) {
	const want Nanos = -9223372036854775
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"-9223372036854775"` {
		t.Errorf("Nanos marshalled as %s, want a quoted decimal", b)
	}
	var got Nanos
	if err := json.Unmarshal(b, &got); err != nil || got != want {
		t.Errorf("round trip gave %d, %v", got, err)
	}
	// A hand-written fixture may use a bare integer; that must still load.
	if err := json.Unmarshal([]byte("42"), &got); err != nil || got != 42 {
		t.Errorf("bare integer gave %d, %v", got, err)
	}
}
