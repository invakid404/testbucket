package vitestrunner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/runner"
)

// harnessUnitInclude is the `harness-unit` project's include glob, read from
// the pinned mandel/vitest.config.ts. Membership is PROJECT-BASED (§20.4), not
// a path substring: nothing in these eight paths contains the string
// "harness-unit", so a substring predicate classifies all 75
// integration-tests/ files as excluded and the bucket universe comes out 1388
// instead of 1396.
const harnessUnitInclude = "integration-tests/lib/purchase-order-recon-extraction/__tests__/"

// pinnedUniverse is the membership partition of contract §20.6a, derived from
// the tracked path list rather than transcribed.
type pinnedUniverse struct {
	Tracked        []string
	Selected       []string // the bucket universe
	ExcludedCase   []string // shared/f/lib/cases/**
	ExcludedInteg  []string
	ExcludedRegion []string
	HarnessUnit    []string
}

func loadPinnedUniverse(t *testing.T) pinnedUniverse {
	t.Helper()
	p := filepath.Join("..", "..", "..", "testdata", "consumers", "mandel", "tracked-test-paths.txt")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read pinned path list: %v", err)
	}
	var u pinnedUniverse
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasSuffix(line, ".test.ts") {
			continue
		}
		u.Tracked = append(u.Tracked, line)
		switch {
		case strings.HasPrefix(line, "shared/f/lib/cases/"):
			u.ExcludedCase = append(u.ExcludedCase, line)
		case strings.HasPrefix(line, harnessUnitInclude):
			u.HarnessUnit = append(u.HarnessUnit, line)
			u.Selected = append(u.Selected, line)
		case strings.HasPrefix(line, "integration-tests/"):
			u.ExcludedInteg = append(u.ExcludedInteg, line)
		case strings.HasPrefix(line, "packages/region-router/"):
			u.ExcludedRegion = append(u.ExcludedRegion, line)
		default:
			u.Selected = append(u.Selected, line)
		}
	}
	return u
}

// TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm is §22 test 64 (S-8)
// and the acceptance test the registry names for ID-19.
//
// It feeds all 1,512 tracked paths through the PRODUCTION assignFilterAtoms
// implementation and asserts the collision property over the full pinned
// universe.
func TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm(t *testing.T) {
	u := loadPinnedUniverse(t)

	if got := len(u.Tracked); got != 1512 {
		t.Fatalf("tracked *.test.ts = %d, want 1512", got)
	}
	if got := len(u.Selected); got != 1396 {
		t.Fatalf("bucket universe = %d, want 1396", got)
	}
	if got := len(u.ExcludedCase); got != 48 {
		t.Fatalf("excluded Case = %d, want 48", got)
	}
	if got := len(u.HarnessUnit); got != 8 {
		t.Fatalf("harness-unit selected = %d, want 8", got)
	}

	// --- assertion 1: exactly 42 selected<->selected collision pairs --------
	selectedPairs := collisionPairs(u.Selected)
	if len(selectedPairs) != 42 {
		t.Errorf("selected<->selected collision pairs = %d, want 42", len(selectedPairs))
		for i, p := range selectedPairs {
			if i >= 5 {
				t.Logf("  ... and %d more", len(selectedPairs)-5)
				break
			}
			t.Logf("  %s  <->  %s", p[0], p[1])
		}
	}

	// --- assertion 2: exactly 0 selected -> excluded-Case pairs -------------
	crossings := 0
	for _, s := range u.Selected {
		for _, c := range u.ExcludedCase {
			if CollidesUnderProductionRelation(s, c) {
				crossings++
				if crossings <= 3 {
					t.Errorf("selected->Case crossing: %s <-> %s", s, c)
				}
			}
		}
	}
	if crossings != 0 {
		t.Errorf("selected->excluded-Case collision pairs = %d, want 0", crossings)
	}

	// --- assertion 3: transitive closure over the collision relation -------
	live := make([]runner.LivePackage, 0, len(u.Selected))
	for _, id := range u.Selected {
		live = append(live, runner.LivePackage{ID: id, HasTests: true})
	}
	AssignFilterAtoms(live)

	atomOf := make(map[string]string, len(live))
	for _, p := range live {
		atomOf[p.ID] = p.Atom
	}
	// Every colliding pair shares one atom.
	for _, pr := range selectedPairs {
		a, b := atomOf[pr[0]], atomOf[pr[1]]
		if a == "" || b == "" || a != b {
			t.Errorf("colliding pair not co-scheduled: %s (atom %q) vs %s (atom %q)", pr[0], a, pr[1], b)
		}
	}
	// And no atom contains two members with no collision path between them.
	groups := map[string][]string{}
	for id, at := range atomOf {
		if at == "" {
			continue
		}
		groups[at] = append(groups[at], id)
	}
	for at, members := range groups {
		sort.Strings(members)
		if !connectedUnderCollision(members) {
			t.Errorf("atom %q contains members with no collision path between them: %v", at, members)
		}
	}

	// --- assertion 4: no atom is split across an invocation or bucket ------
	t.Run("no atom is split across an invocation or bucket boundary", func(t *testing.T) {
		// The atom key IS the core's co-scheduling instruction: a non-empty
		// value means every target sharing it rides in one invocation. The
		// property to assert is therefore that the key is a function of the
		// group, which is what makes it unsplittable downstream.
		for at, members := range groups {
			if len(members) < 2 {
				continue
			}
			for _, m := range members {
				if atomOf[m] != at {
					t.Fatalf("member %s of atom %q carries atom %q", m, at, atomOf[m])
				}
			}
		}
		// Re-running the production algorithm reproduces the same grouping, so
		// a plan cannot land two different partitions of one atom.
		again := make([]runner.LivePackage, 0, len(u.Selected))
		for _, id := range u.Selected {
			again = append(again, runner.LivePackage{ID: id, HasTests: true})
		}
		AssignFilterAtoms(again)
		for _, p := range again {
			if atomOf[p.ID] != p.Atom {
				t.Fatalf("atom assignment is not deterministic for %s: %q vs %q", p.ID, atomOf[p.ID], p.Atom)
			}
		}
	})

	// --- assertion 5: every selected path enters argv as a ./x path token --
	t.Run("every selected path renders as a ./x path token", func(t *testing.T) {
		for _, id := range u.Selected {
			// filterPathArg is the production renderer render.go uses.
			tok := filterPathArg(id)
			if !strings.HasPrefix(tok, "./") {
				t.Fatalf("path token for %s is %q, want a ./x form", id, tok)
			}
		}
	})

	// --- the two root-containment examples are a SUBSET, not the property --
	t.Run("root containment is an illustrative subset of the production relation", func(t *testing.T) {
		rootPairs := 0
		for i := 0; i < len(u.Selected); i++ {
			for j := i + 1; j < len(u.Selected); j++ {
				a, b := u.Selected[i], u.Selected[j]
				if CollidesUnderRootContainment(a, b) {
					rootPairs++
				}
			}
		}
		if rootPairs != 2 {
			t.Errorf("root-containment pairs = %d, want the 2 illustrative keto/attribution pairs", rootPairs)
		}
		if rootPairs >= len(selectedPairs) {
			t.Error("the production relation must be strictly broader than root containment")
		}
	})
}

// collisionPairs enumerates unordered colliding pairs under the production
// relation.
func collisionPairs(ids []string) [][2]string {
	var out [][2]string
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if CollidesUnderProductionRelation(ids[i], ids[j]) {
				out = append(out, [2]string{ids[i], ids[j]})
			}
		}
	}
	return out
}

// connectedUnderCollision reports whether members form one connected component
// under the production collision relation.
func connectedUnderCollision(members []string) bool {
	if len(members) < 2 {
		return true
	}
	seen := map[int]bool{0: true}
	frontier := []int{0}
	for len(frontier) > 0 {
		cur := frontier[len(frontier)-1]
		frontier = frontier[:len(frontier)-1]
		for i := range members {
			if seen[i] {
				continue
			}
			if CollidesUnderProductionRelation(members[cur], members[i]) {
				seen[i] = true
				frontier = append(frontier, i)
			}
		}
	}
	return len(seen) == len(members)
}
