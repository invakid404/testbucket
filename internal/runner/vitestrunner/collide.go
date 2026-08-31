package vitestrunner

import (
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
)

// filterAtomPrefix marks a co-scheduling group formed because Vitest's positional
// FILE filters cannot tell the group's members apart. The key is the group's
// smallest id — the ambiguous filter itself — so the unit the core renders reads
// `mod:filter:lib/keto/organizations.test.ts`: the files that all answer to that
// one filter.
const filterAtomPrefix = "filter:"

// filterSelects reports whether passing `filter` as a Vitest positional also
// selects the file `candidate`. Both are root-relative ids.
//
// It is a transcription of Vitest's own file filtering (TestProject.filterFiles).
// The source below is BYTE-IDENTICAL in vitest 4.1.11 — what this repo's fixture
// pins and what CI installs, so the integration test measures exactly this — and
// in 4.1.10, the version the Mandel consumer pins:
//
//	const testFile = relative(dir, t).toLocaleLowerCase()
//	if (isAbsolute(f) && t.startsWith(f)) return true
//	const relativePath = f.endsWith('/') ? join(relative(dir, f), '/') : relative(dir, f)
//	return testFile.includes(f.toLocaleLowerCase())
//	    || testFile.includes(relativePath.toLocaleLowerCase())
//
// Three facts about that matter here, and they are why this adapter cannot make
// a run exact by rewriting the ARGUMENT:
//
//   - the test is `includes`, a SUBSTRING test, never a path or prefix match;
//   - `relative(dir, f)` normalises every spelling of one file — bare `x`,
//     `./x`, and the absolute `<root>/x` — back to the SAME root-relative `x`
//     before that substring test, so no spelling is more selective than another;
//   - `testFile` is itself root-relative, so it never carries a leading `/` or
//     `./` a filter could anchor against.
//
// Together those make over-matching unavoidable by construction: if id `a` is a
// substring of id `b` then EVERY filter that selects `a` also selects `b`, since
// any filter selecting `a` must be a substring of `a`. The absolute-path clause
// is an OR, so it can only ever ADD matches. Exactness therefore has to come
// from SCHEDULING, not from argument syntax — see assignFilterAtoms.
//
// The case fold is filterFold, NOT strings.ToLower: Vitest folds with
// toLocaleLowerCase(), whose result depends on the runner's locale, and this
// predicate must never MISS a collision Vitest would make. See filterFold.
func filterSelects(filter, candidate string) bool {
	return collidesUnderSomeProjectRoot(filterFold(filter), filterFold(candidate))
}

// collidesUnderSomeProjectRoot reports whether a positional naming `a` can also
// select `b`, for SOME directory Vitest might evaluate the filter against. Both
// arguments are already folded, root-relative ids.
//
// The plain `strings.Contains(b, a)` is only the workspace-root case. Vitest
// evaluates a filter once PER PROJECT, against that project's own directory:
//
//	globTestFiles()  ->  const dir = this.config.dir || this.config.root
//	filterFiles()    ->  relative(dir, t).includes(relative(dir, filter))
//
// A project rooted below the workspace root therefore compares SHORTER strings
// than this adapter's ids, and a pair that shares no substring at the workspace
// root can share one there. Measured against a real Vitest, with a project rooted
// at `projects/unit`:
//
//	filter ./projects/unit/lib/keto/organizations.vtest.ts
//	  selects projects/unit/lib/keto/organizations.vtest.ts
//	  selects projects/unit/shared/f/lib/keto/organizations.vtest.ts   <- over-match
//
// yet `projects/unit/shared/f/lib/keto/...` does not contain
// `projects/unit/lib/keto/...`. Comparing only at the workspace root misses that
// collision, leaves the pair un-atomized, and double-runs the long file.
//
// So the test walks the shared leading directories one segment at a time and asks
// the question again at each depth — i.e. at every directory that could be a
// project root for BOTH files. A directory that is an ancestor of only `b` cannot
// produce a match: `relative(dir, a)` then begins with `..`, which the
// project-relative `testFile` never contains. Once the leading segments diverge,
// nothing deeper is a common ancestor and the walk stops.
func collidesUnderSomeProjectRoot(a, b string) bool {
	for {
		if strings.Contains(b, a) {
			return true
		}
		ai, bi := strings.IndexByte(a, '/'), strings.IndexByte(b, '/')
		if ai < 0 || bi < 0 {
			return false // no directory left to strip on one side
		}
		if a[:ai] != b[:bi] {
			return false // the roots diverge; nothing deeper is a common ancestor
		}
		a, b = a[ai+1:], b[bi+1:]
	}
}

// The runes a lower-case fold treats specially — the ones that make Vitest's
// toLocaleLowerCase() disagree with Go's strings.ToLower. See filterFold.
const (
	dotlessI          = 'ı'      // U+0131 LATIN SMALL LETTER DOTLESS I
	combiningDotAbove = '\u0307' // what a root-locale fold of 'İ' leaves behind
	finalSigma        = 'ς'      // U+03C2, what a word-final 'Σ' folds to
	sigma             = 'σ'      // U+03C3, what Go's fold always produces
)

// accentedDottedI decomposes the three precomposed accented i's whose UPPERCASE
// forms a Lithuanian fold splits apart. `lt` lower-cases 'Ì' (U+00CC) to
// i + U+0307 + U+0300, while Go's table gives the precomposed 'ì' (U+00EC); a
// path carrying the decomposed sequence would then collide for Vitest and not
// for us. Decomposing here (and dropping U+0307, below) makes the two agree.
// U+00CC/U+00CD/U+0128 are the complete set — see the scan cited in filterFold.
var accentedDottedI = map[rune]string{
	'ì': "i\u0300", // U+00EC -> i + combining grave
	'í': "i\u0301", // U+00ED -> i + combining acute
	'ĩ': "i\u0303", // U+0129 -> i + combining tilde
}

// filterFold case-folds an id for the collision test so the result is a
// CONSERVATIVE SUPERSET of Vitest's fold — under every locale AND every
// surrounding context.
//
// Vitest calls toLocaleLowerCase(). Go's strings.ToLower is a per-rune table
// lookup, so it disagrees with it in exactly the places Unicode's SpecialCasing
// gives lower-casing a special or conditional rule. Measured against Node:
//
//	          root locale        tr / az locale     Go strings.ToLower
//	'I'   ->  "i"                "ı"                "i"
//	'i'   ->  "i"                "i"                "i"
//	'İ'   ->  "i" + U+0307       "i"                "i"
//	'ı'   ->  "ı"                "ı"                "ı"
//	'Σ'   ->  "σ"                "σ"                "σ"
//	"AΣ"  ->  "aς"               "aς"               "aσ"   <- word-FINAL sigma
//
// So plain strings.ToLower is NOT conservative, in two independent ways:
//
//   - On a Turkish-locale runner the filter `FILE.test.ts` folds to
//     `fıle.test.ts` and really does select `shared/fıle.test.ts`.
//   - In EVERY locale, `dir/AΣ/foo.vtest.ts` folds to `dir/aς/foo.vtest.ts` and
//     really does select `shared/dir/aς/foo.vtest.ts`. Final sigma is
//     CONTEXT-sensitive, not locale-sensitive: a 'Σ' that ends a word becomes
//     'ς', which Go's table never produces.
//
// Each is a collision Go's fold misses, leaving the pair un-atomized, split
// across buckets, and double-running the long file. That is the one direction
// this predicate may never get wrong: a MISSED collision splits a pair and
// causes an unplanned run, while an EXTRA collision only co-schedules two files
// that did not have to ride together.
//
// filterFold therefore normalises every rune SpecialCasing treats specially,
// after strings.ToLower (which already sends 'I' and 'İ' to 'i', and 'Σ' to
// 'σ'):
//
//	'ı' -> 'i'          the dotless i joins the family
//	'ς' -> 'σ'          final sigma joins ordinary sigma
//	'ì','í','ĩ' -> i+mark   decomposed, matching what a Lithuanian fold emits
//	U+0307 dropped      the combining dot 'İ' (or a Lithuanian fold) leaves
//
// That list is CLOSED, and closed by measurement rather than by reading the
// spec. Scanning every code point up to U+2FFFF through ICU for (a) a
// multi-character lower-case expansion, (b) a lower-casing that changes with
// surrounding context, and (c) a lower-casing that changes under tr/az/lt,
// yields exactly:
//
//	expansions        U+0130
//	context-sensitive U+03A3            (word-final -> ς)
//	locale-sensitive  U+0049, U+00CC, U+00CD, U+0128, U+0130
//
// Every one of those is neutralised by the rewrites above, so there is no
// remaining code point where Vitest's fold can join two ids that this one keeps
// apart.
//
// Why that makes the result conservative rather than merely broader: every one
// of those folds can be turned into filterFold by a further per-character
// relabel-and-delete map, and such a map is a string homomorphism — it preserves
// substring containment. So whenever some locale-and-context fold L makes `L(b)`
// contain `L(a)`, filterFold(b) contains filterFold(a) too. The property holds
// by construction, including for folds not enumerated here, as long as they stay
// per-character; a future rule that REORDERED characters would need rechecking.
// (For the Lithuanian entries the relabel is on Go's side — filterFold expands
// the precomposed form — so the map from the lt fold is a pure deletion of
// U+0307, which is a homomorphism just the same.)
//
// Ids that are pure ASCII are safe under this reasoning as well, and would in
// fact already be safe under strings.ToLower: for them Go's fold is COARSER than
// tr's (it merges 'I' and 'i', which tr keeps apart), and a coarser fold can only
// add matches. The special folding matters only once a real path carries one of
// the runes above.
func filterFold(id string) string {
	lowered := strings.ToLower(id)
	if !needsSpecialFold(lowered) {
		return lowered // the overwhelmingly common all-ASCII path: no allocation
	}
	var b strings.Builder
	b.Grow(len(lowered))
	for _, r := range lowered {
		switch r {
		case combiningDotAbove: // dropped
		case dotlessI:
			b.WriteRune('i')
		case finalSigma:
			b.WriteRune(sigma)
		default:
			if decomposed, ok := accentedDottedI[r]; ok {
				b.WriteString(decomposed)
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// specialFoldRunes are the runes filterFold rewrites once strings.ToLower has
// run — the residue of SpecialCasing's lower-case rules.
var specialFoldRunes = []rune{dotlessI, combiningDotAbove, finalSigma, 'ì', 'í', 'ĩ'}

// needsSpecialFold reports whether an already-lower-cased id carries any of them,
// so the overwhelmingly common case can skip the rebuild.
func needsSpecialFold(lowered string) bool {
	return strings.ContainsAny(lowered, string(specialFoldRunes))
}

// assignFilterAtoms co-schedules, into one invocation, every group of test files
// that Vitest's positional filters cannot separate.
//
// The defect it closes: Mandel carries pairs like
//
//	lib/keto/organizations.test.ts
//	shared/f/lib/keto/organizations.test.ts
//
// The short id is a SUBSTRING of the long one, so a bucket that plans only the
// short file runs BOTH. The long file then reports once from its own bucket and
// once from the short file's bucket — two invocations against one planned one,
// which the coverage audit correctly rejects as an over-run. Worse, the wasted
// second execution is real CI time nobody asked for.
//
// The fix is the file-level twin of what names.go already does for TEST NAMES:
// when two ids collide under the filter grammar, refuse to separate them and run
// them together — a slightly less balanced bucket, never a double run. Setting a
// shared Atom is exactly how the neutral core is told that ("a non-empty value
// means this target must ride in one invocation with every other target sharing
// the key"): the core packs the group into a single KindModuleAtom unit, the
// renderer merges that unit's files into ONE `vitest run`, and the group's
// mutual over-matching is then contained inside an invocation that planned all
// of them. Each file runs exactly once.
//
// Packing the group whole also suppresses name-slicing for its members, which is
// required rather than incidental: a slice renders its own single-file
// invocation, whose positional would drag the mate back in and run it a second
// time under a -t that does not name its tests.
//
// Grouping is by the TRANSITIVE closure of TWO relations, so the result never
// half-breaks a group the core has been told must ride together:
//
//   - collidesUnderSomeProjectRoot in either direction — substring containment
//     asked again at every shared leading directory, because Vitest evaluates a
//     filter against each PROJECT's root, not the workspace root. A chain
//     (a ⊂ b ⊂ c) lands in one group rather than two overlapping pairs;
//   - equality of a pre-existing non-empty Atom. An offline live set can arrive
//     with atoms already on it (LoadLivePackages accepts the neutral LivePackage
//     shape). Unioning them in means a collision that touches ONE member of a
//     caller's atom group pulls in the WHOLE group, instead of rewriting the
//     colliding pair and stranding the caller's remaining members on a now-split
//     key — which would break the very contract Atom states.
//
// A component's key is then chosen to disturb the caller as little as possible:
// if the whole component already shares one non-empty atom, it is kept as-is; if
// exactly one non-empty atom appears in it, that name is adopted by the members
// that lacked it; otherwise (none, or two caller groups fused by a collision) the
// component takes the deterministic `filter:<smallest id>` key.
//
// The scan is O(n^2) pair tests over short path strings, each walking at most the
// shared directory depth; at Mandel's ~1.4k test files that is a few
// milliseconds, once per plan.
func assignFilterAtoms(live []runner.LivePackage) {
	n := len(live)
	if n < 2 {
		return
	}
	// The SAME fold filterSelects uses — locale-conservative, not plain ToLower.
	lower := make([]string, n)
	for i, p := range live {
		lower[i] = filterFold(p.ID)
	}

	// Union-find over the collision relation.
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]] // path halving
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		switch {
		case ra == rb:
		case ra < rb:
			parent[rb] = ra
		default:
			parent[ra] = rb
		}
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			// Either direction counts: whichever id is passed as the positional,
			// the pair must not end up in different invocations.
			if collidesUnderSomeProjectRoot(lower[i], lower[j]) ||
				collidesUnderSomeProjectRoot(lower[j], lower[i]) {
				union(i, j)
			}
		}
	}

	// Fold each pre-existing atom group in as one more edge. Without this, a
	// collision that reaches only PART of a caller's atom group would move that
	// part onto a new key and leave the rest behind — two groups where the caller
	// declared one, and a core that may then schedule them in different
	// invocations.
	firstOfAtom := map[string]int{}
	for i, p := range live {
		if p.Atom == "" {
			continue
		}
		if first, seen := firstOfAtom[p.Atom]; seen {
			union(first, i)
		} else {
			firstOfAtom[p.Atom] = i
		}
	}

	groups := map[int][]int{}
	for i := range live {
		r := find(i)
		groups[r] = append(groups[r], i)
	}
	for _, idx := range groups {
		if len(idx) < 2 {
			continue // a file only ever selected by its own filter: mixes freely
		}

		// Which caller atoms does this component already carry?
		existing := map[string]bool{}
		withAtom := 0
		for _, i := range idx {
			if live[i].Atom != "" {
				existing[live[i].Atom] = true
				withAtom++
			}
		}
		// The whole component already rides one caller key: leave it untouched, so
		// a hand-authored co-scheduling choice survives verbatim.
		if len(existing) == 1 && withAtom == len(idx) {
			continue
		}
		// Exactly one caller key, but not on every member (a collision dragged a
		// newcomer in): extend that key rather than renaming the caller's group.
		key := ""
		if len(existing) == 1 {
			for k := range existing {
				key = k
			}
		} else {
			// None, or two caller groups fused by a collision. Key by the smallest
			// id — the ambiguous filter itself — so the key is a pure, stable
			// function of the live set and reads meaningfully in the plan.
			smallest := live[idx[0]].ID
			for _, i := range idx[1:] {
				if live[i].ID < smallest {
					smallest = live[i].ID
				}
			}
			key = filterAtomPrefix + smallest
		}
		for _, i := range idx {
			live[i].Atom = key
		}
	}
}
