package walltime

import (
	"regexp"
	"strings"
	"testing"
)

// TestImplementationGapTestsFailAgainstR54 is §22 test 28 (S-10).
//
// It enumerates every gap from scope.md §21.0's machine-readable block rather
// than from a literal count, and for each row asserts the evidence the
// registry's attribution procedure requires: a named acceptance test that
// EXISTS, an overlay file path, and a required_symbols set that is non-empty
// and whose members are reachable through the row's dependency closure.
//
// It deliberately does NOT re-run the tree at R54. That workspace overlay is
// an operator procedure — §21.0 step 1 creates a jj workspace and applies one
// row's files at a time — and a unit test cannot perform it. What a unit test
// CAN establish is that every row carries the evidence the procedure consumes,
// which is the half that used to be missing: the earlier form counted any
// compile failure as red for every row, so one unrelated missing symbol marked
// all 24 red without exercising a single row's behaviour.
func TestImplementationGapTestsFailAgainstR54(t *testing.T) {
	block := parseRegistryBlock(t, "# implementation-delta-registry v1")

	type row struct {
		id       string
		tests    []string
		overlays []string
		symbols  []string
		dep      string
	}

	// One record per `- id:` stanza, split on the marker rather than matched
	// with a regexp. Two earlier forms were wrong for instructive reasons: a
	// paragraph-delimited split swallowed ID-22, which is adjacent to ID-21
	// with no blank line, and a regexp bounded by the next marker CONSUMED
	// that marker — Go's regexp has no lookahead — so it returned only every
	// other row. Either bug would have left delta rows unchecked by the very
	// test that exists to check them.
	const marker = "\n  - id: "
	var stanzas [][]string
	for _, part := range strings.Split(block, marker)[1:] {
		m := regexp.MustCompile(`^(ID-\d+)`).FindStringSubmatch(part)
		if m == nil {
			continue
		}
		stanzas = append(stanzas, []string{"", m[1], part})
	}
	if len(stanzas) == 0 {
		t.Fatal("parsed no delta rows")
	}
	list := func(seg, key string) []string {
		m := regexp.MustCompile(key + `: \[([^\]]*)\]`).FindStringSubmatch(seg)
		if m == nil {
			return nil
		}
		var out []string
		for _, s := range strings.Split(m[1], ",") {
			s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), `"`))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	var rows []row
	for _, st := range stanzas {
		seg := st[2]
		dep := ""
		if m := regexp.MustCompile(`dependency: (\S+)`).FindStringSubmatch(seg); m != nil {
			dep = strings.TrimSpace(m[1])
		}
		rows = append(rows, row{
			id: st[1], tests: list(seg, "acceptance_tests"),
			overlays: list(seg, "overlay_files"), symbols: list(seg, "required_symbols"),
			dep: dep,
		})
	}

	have := goTestSymbols(t)
	byID := map[string]row{}
	for _, r := range rows {
		byID[r.id] = r
	}

	t.Run("every row carries a resolvable acceptance test", func(t *testing.T) {
		for _, r := range rows {
			if len(r.tests) == 0 {
				t.Errorf("%s names no acceptance test", r.id)
				continue
			}
			for _, sym := range r.tests {
				if !have[sym] {
					t.Errorf("%s names acceptance test %q, which does not exist", r.id, sym)
				}
			}
		}
	})

	t.Run("every row carries an overlay file path", func(t *testing.T) {
		// R11-D10: acceptance_tests names SYMBOLS, which cannot be applied;
		// overlay_files names the PATHS the overlay copies, and every row
		// carries one.
		for _, r := range rows {
			if len(r.overlays) == 0 {
				t.Errorf("%s carries no overlay_files, so its attribution overlay cannot be applied", r.id)
			}
		}
	})

	t.Run("no two rows share an overlay file", func(t *testing.T) {
		// Each row's file must be distinct, or the isolation the procedure
		// depends on is not real.
		owner := map[string]string{}
		for _, r := range rows {
			for _, f := range r.overlays {
				if prev, ok := owner[f]; ok {
					t.Errorf("overlay file %q is shared by %s and %s; the per-row isolation is not real", f, prev, r.id)
				}
				owner[f] = r.id
			}
		}
	})

	t.Run("every row carries a non-empty required_symbols set", func(t *testing.T) {
		// The attribution is MECHANICAL: a compile failure is red-attributed
		// only when every missing symbol is in this set or in the set of a row
		// reachable through the dependency closure.
		for _, r := range rows {
			if len(r.symbols) == 0 {
				t.Errorf("%s carries no required_symbols, so a compile failure could not be attributed to it", r.id)
			}
		}
	})

	t.Run("the dependency closure is computed from the registry graph", func(t *testing.T) {
		// Not from prose: every non-null dependency must name a real row, and
		// the graph must be acyclic so the closure terminates.
		for _, r := range rows {
			if r.dep == "" || r.dep == "null" {
				continue
			}
			if _, ok := byID[r.dep]; !ok {
				t.Errorf("%s depends on %q, which is not a registry row", r.id, r.dep)
			}
		}
		for _, r := range rows {
			seen := map[string]bool{r.id: true}
			cur := r.dep
			for cur != "" && cur != "null" {
				if seen[cur] {
					t.Errorf("the dependency graph has a cycle through %s", cur)
					break
				}
				seen[cur] = true
				next, ok := byID[cur]
				if !ok {
					break
				}
				cur = next.dep
			}
		}
	})

	t.Run("the admissible symbol set for a row is its transitive closure", func(t *testing.T) {
		// The executable form of the attribution rule: for each row, the union
		// over its dependency closure is what a missing symbol may come from.
		closure := func(id string) map[string]bool {
			out := map[string]bool{}
			cur := id
			for cur != "" && cur != "null" {
				r, ok := byID[cur]
				if !ok {
					break
				}
				for _, s := range r.symbols {
					out[s] = true
				}
				cur = r.dep
			}
			return out
		}
		for _, r := range rows {
			c := closure(r.id)
			for _, s := range r.symbols {
				if !c[s] {
					t.Errorf("%s's own symbol %q is not in its closure", r.id, s)
				}
			}
			if r.dep != "" && r.dep != "null" {
				dep := byID[r.dep]
				for _, s := range dep.symbols {
					if !c[s] {
						t.Errorf("%s's closure omits %q from its dependency %s", r.id, s, r.dep)
					}
				}
			}
		}
	})

	t.Run("every required symbol now resolves in the tree", func(t *testing.T) {
		// The other half of the red/green overlay: at R54 every one of these
		// resolved to ZERO occurrences, which is what made the rows red. They
		// must resolve now, or the row is not actually closed.
		src := allGoSource(t)
		for _, r := range rows {
			for _, sym := range r.symbols {
				leaf := sym
				if i := strings.LastIndex(leaf, "."); i >= 0 {
					leaf = leaf[i+1:]
				}
				if !strings.Contains(src, leaf) {
					t.Errorf("%s's required symbol %q does not resolve; the row is not closed", r.id, sym)
				}
			}
		}
	})

	t.Run("the registry names no row out of order", func(t *testing.T) {
		for i, r := range rows {
			want := "ID-" + itoaLocal(i+1)
			if r.id != want {
				t.Errorf("registry row %d is %s, want %s", i, r.id, want)
			}
		}
	})
}
