package vitestrunner

import (
	"strings"
	"testing"
)

// TestJSRegexEscape escapes exactly the JS RegExp metacharacters and nothing
// else — a Vitest title is arbitrary text, so the renderer must be able to match
// one literally without the title's punctuation changing the alternation.
func TestJSRegexEscape(t *testing.T) {
	cases := map[string]string{
		"plain name":          "plain name",
		"has (parens)":        `has \(parens\)`,
		"a | b":               `a \| b`,
		"dot.star*":           `dot\.star\*`,
		`back\slash`:          `back\\slash`,
		"a+b?c^d$e":           `a\+b\?c\^d\$e`,
		"[bracket]{brace}":    `\[bracket\]\{brace\}`,
		"slash/kept":          "slash/kept", // '/' is not escaped for `new RegExp(str)`
		"unicode ✓ and space": "unicode ✓ and space",
	}
	for in, want := range cases {
		if got := jsRegexEscape(in); got != want {
			t.Errorf("jsRegexEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRunPattern pins the emitted -t value against the forms verified empirically
// against Vitest 4 (see the vitest-name-slice-semantics note): a flat title
// matches literally; a nested id's separators become (?: > | ) so the pattern
// matches the reporter's space-joined fullName WHATEVER the true describe nesting
// was; metacharacters are escaped; the whole thing is anchored.
func TestRunPattern(t *testing.T) {
	cases := []struct {
		name string
		ids  []string
		want string
	}{
		{"flat", []string{"top level plain"}, `^(top level plain)$`},
		{"nested", []string{"outer group > inner a"}, `^(outer group(?: > | )inner a)$`},
		{"deep nested", []string{"outer group > inner group > deep test"}, `^(outer group(?: > | )inner group(?: > | )deep test)$`},
		{"metachars escaped", []string{"has (parens) and | pipe"}, `^(has \(parens\) and \| pipe)$`},
		{"in-title separator", []string{"a > b literal"}, `^(a(?: > | )b literal)$`},
		{"alternation", []string{"one", "two"}, `^(one|two)$`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runPattern(tc.ids); got != tc.want {
				t.Errorf("runPattern(%q) = %q, want %q", tc.ids, got, tc.want)
			}
		})
	}
}

// TestSpaceForm collapses the separator onto the space -t actually matches.
func TestSpaceForm(t *testing.T) {
	cases := map[string]string{
		"flat":                 "flat",
		"a > b":                "a b",
		"a > b > c":            "a b c",
		"a > b literal > leaf": "a b literal leaf",
	}
	for in, want := range cases {
		if got := spaceForm(in); got != want {
			t.Errorf("spaceForm(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAmbiguous flags exactly the ids that collapse to a shared space-form — the
// ones a -t pattern cannot tell apart — and leaves distinct ones alone.
func TestAmbiguous(t *testing.T) {
	// Two DISTINCT ids that project to the same space-form "a b" — "a > b" and a
	// flat "a b" — are a collision.
	got := ambiguous([]string{"a > b", "a b", "plain"})
	if strings.Join(got, ",") != "a > b,a b" {
		t.Errorf("ambiguous = %v, want the two colliding ids", got)
	}
	if len(ambiguous([]string{"outer > inner", "outer > other", "flat"})) != 0 {
		t.Error("distinct nested names were wrongly flagged ambiguous")
	}
	// IDENTICAL ids (a genuine duplicate title, or two `test("")`) are NOT a
	// collision — they share one universe entry and one slice, so they stay
	// sliceable. Flagging them would needlessly demote a fine file.
	if len(ambiguous([]string{"dup", "dup", "other"})) != 0 {
		t.Error("a duplicate name was wrongly flagged ambiguous")
	}
	if len(ambiguous([]string{"", "", "x"})) != 0 {
		t.Error("two empty titles (a duplicate) were wrongly flagged ambiguous")
	}
	if len(ambiguous(nil)) != 0 {
		t.Error("empty input flagged ambiguous")
	}
}
