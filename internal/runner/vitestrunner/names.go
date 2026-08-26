package vitestrunner

import (
	"fmt"
	"sort"
	"strings"
)

// nameSep is how `vitest list --json` joins a test's task path (ancestor
// describe titles + the test title) into one `name`, e.g.
// "outer group > inner a". It is the neutral per-test IDENTITY this adapter
// uses everywhere — the store key, the `Runnables` universe, and the text a
// run-slice's -t is built from — so all three agree by construction.
//
// It is NOT the string Vitest's -t matches against: the reporter joins the same
// path with a plain space ("outer group inner a") and testNamePattern is tested
// against THAT. runPattern below bridges the two; spaceForm projects onto it for
// the collision check. These forms diverge only for nested describes, so the Go
// adapter (flat top-level names) never has to care.
const nameSep = " > "

// jsRegexMeta is the set escapeRegExp escapes for a JS `new RegExp` — the engine
// Vitest builds testNamePattern with. It is the JS canonical set
// (/[.*+?^${}()|[\]\\]/), NOT Go's regexp/RE2 set: the pattern runs in Node, so
// escaping to Go's rules could both under- and over-escape. `/` is absent
// deliberately — a pattern passed to `new RegExp(str)` (not a /literal/) needs
// no slash escaping.
const jsRegexMeta = `.*+?^${}()|[]\`

// jsRegexEscape escapes one task-path segment for literal matching inside a JS
// RegExp. A Vitest title is arbitrary text — spaces, parens, a literal '|' — so
// unlike a Go identifier it cannot be spliced into an alternation raw; an
// unescaped '(' or '|' would silently change what the -t selects.
func jsRegexEscape(seg string) string {
	var b strings.Builder
	b.Grow(len(seg))
	for _, r := range seg {
		if strings.ContainsRune(jsRegexMeta, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// runPattern builds the anchored -t value that selects exactly the given
// slice ids and nothing else.
//
// Each id is the `" > "`-joined form. That form is AMBIGUOUS: "a > b > c" could
// be the describe path [a,b,c] OR a title that itself contains " > " (e.g.
// [a, "b > c"]). The reporter's fullName — the string -t is matched against —
// resolves it one specific way we cannot know at plan time. So rather than bet,
// every separator position is emitted as `(?: > | )`: it matches whether that
// position was a describe boundary (collapsed to a space in fullName) or a
// literal " > " inside a title (kept). The result matches the true fullName for
// EVERY possible resolution, so a run-slice can never drop a test to a naming
// guess. The ^(...)$ anchor keeps one name from being a prefix of another (the
// same reason the Go adapter anchors -run).
//
// This can only over-match when two DISTINCT ids share a space-form; ambiguous()
// is the guard that forbids exactly that, so within one sliced file the match is
// one-to-one.
func runPattern(ids []string) string {
	alts := make([]string, 0, len(ids))
	for _, id := range ids {
		segs := strings.Split(id, nameSep)
		for i := range segs {
			segs[i] = jsRegexEscape(segs[i])
		}
		alts = append(alts, strings.Join(segs, `(?: > | )`))
	}
	return fmt.Sprintf("^(%s)$", strings.Join(alts, "|"))
}

// spaceForm projects a `" > "`-joined id onto the space-joined form the reporter
// produces and -t matches. Two ids with the same space-form are indistinguishable
// to a -t pattern: each one's runPattern would match a test the other names.
func spaceForm(id string) string {
	return strings.ReplaceAll(id, nameSep, " ")
}

// ambiguous returns the ids that collide with another id under spaceForm — the
// set that makes a file unsafe to name-slice, because their -t patterns would
// overlap and run a test in more than one slice. It is empty for the overwhelming
// common case (every fully-qualified test name is unique once the describe path
// is included). When it is not, the caller refuses to slice the file and runs it
// whole: a slower bucket, never a double-run.
func ambiguous(ids []string) []string {
	bySpace := map[string][]string{}
	for _, id := range ids {
		sf := spaceForm(id)
		bySpace[sf] = append(bySpace[sf], id)
	}
	var bad []string
	for _, group := range bySpace {
		if len(group) > 1 {
			bad = append(bad, group...)
		}
	}
	sort.Strings(bad)
	return bad
}
