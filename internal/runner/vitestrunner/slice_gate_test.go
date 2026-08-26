package vitestrunner_test

// Never-drop-a-test at the TEST level for Vitest name slices — the correctness
// crux of #21, proven offline (no Node needed) by driving the real core planner
// through the real Vitest adapter's Render + ValidateUnit, with the `vitest list`
// universe injected via PlanOptions.Runnables.

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/runner/vitestrunner"
)

// whaleStore builds a store where `whale` is flagged for name-slicing with the
// given per-test weights, plus a couple of small whole-file rows for balance.
func whaleStore(token, whale string, tests map[string]float64, small map[string]float64) *core.Store {
	st := core.NewStore(token)
	total := 0.0
	for _, w := range tests {
		total += w
	}
	st.Units[whale] = &core.UnitStat{
		Seconds: total, Samples: 3, Split: "run", SplitInto: 3,
		SplitReason: "name-divisible (test fixture)", Tests: tests,
	}
	for id, w := range small {
		st.Units[id] = &core.UnitStat{Seconds: w, Samples: 3}
	}
	return st
}

func TestVitestSliceGateNeverDropsATest(t *testing.T) {
	rnr, err := vitestrunner.New(vitestrunner.Options{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	const whale = "tests/whale.spec.ts"
	live := []runner.LivePackage{
		{ID: whale, HasTests: true},
		{ID: "tests/a.spec.ts", HasTests: true},
		{ID: "tests/b.spec.ts", HasTests: true},
	}

	// The store knows six of the whale's tests (nested + flat + a literal-'>'
	// title). The live universe adds a SEVENTH the store has never seen — a test
	// added since the last record. It must still land in exactly one slice.
	stored := map[string]float64{
		"outer > inner a":            40,
		"outer > inner group > deep": 35,
		"flat one":                   30,
		"has (parens) and | pipe":    25,
		"a > b literal":              20,
		"outer > inner b":            15,
	}
	universe := make([]string, 0, len(stored)+1)
	for n := range stored {
		universe = append(universe, n)
	}
	universe = append(universe, "brand new test") // not in the store
	sort.Strings(universe)

	opt := core.PlanOptions{
		K: 3, Count: 1, Live: live, Token: rnr.CanonicalToken(),
		Now: time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC),
		Runnables: func(p runner.LivePackage) ([]string, error) {
			if p.ID == whale {
				return append([]string(nil), universe...), nil
			}
			return nil, nil
		},
	}
	st := whaleStore(rnr.CanonicalToken(), whale, stored, map[string]float64{
		"tests/a.spec.ts": 50, "tests/b.spec.ts": 45,
	})

	doc, err := core.BuildPlan(context.Background(), rnr, st, "", opt)
	if err != nil {
		// A gate failure here IS the never-drop invariant firing — surface it.
		t.Fatalf("coverage gate rejected the vitest slice plan: %v", err)
	}

	// Collect every runnable name scheduled for the whale across all buckets, and
	// count how many slices carry it. Exactly-once is the whole ballgame.
	seen := map[string]int{}
	sliceCount := 0
	for _, b := range doc.Buckets {
		for _, u := range b.Units {
			if u.Kind != runner.KindRunSlice {
				continue
			}
			if len(u.Packages) != 1 || u.Packages[0] != whale {
				continue
			}
			sliceCount++
			for _, n := range u.Run {
				seen[n]++
			}
		}
	}
	if sliceCount < 2 {
		t.Fatalf("the whale was not sliced across buckets (%d slice unit(s))", sliceCount)
	}
	for _, n := range universe {
		switch seen[n] {
		case 1: // exactly once — correct
		case 0:
			t.Errorf("test %q was DROPPED: in no slice", n)
		default:
			t.Errorf("test %q is in %d slices (double-run)", n, seen[n])
		}
	}
	// And nothing outside the universe was invented.
	for n := range seen {
		if !contains(universe, n) {
			t.Errorf("slice names %q, which is not a live runnable", n)
		}
	}
	// The brand-new test specifically must be scheduled — the property that keeps a
	// freshly-added test inside a harpooned whale from vanishing.
	if seen["brand new test"] != 1 {
		t.Errorf("the brand-new test (unknown to the store) was scheduled %d times, want 1", seen["brand new test"])
	}
}

// TestVitestSliceGateCatchesADrop is the negative control: a test that only ever
// watches the gate pass cannot tell a real gate from a rubber stamp. A whale
// flagged for name-slicing whose runnable universe comes back EMPTY (a `vitest
// list` that reported no tests for it) must fail the plan loudly — the file has
// tests per discovery, so scheduling it with no slice would silently drop them
// all.
func TestVitestSliceGateCatchesADrop(t *testing.T) {
	rnr, err := vitestrunner.New(vitestrunner.Options{Root: "."})
	if err != nil {
		t.Fatal(err)
	}
	const whale = "tests/whale.spec.ts"
	live := []runner.LivePackage{{ID: whale, HasTests: true}}
	st := whaleStore(rnr.CanonicalToken(), whale,
		map[string]float64{"t1": 40, "t2": 35, "t3": 30}, nil)

	opt := core.PlanOptions{
		K: 3, Count: 1, Live: live, Token: rnr.CanonicalToken(),
		Now: time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC),
		// The universe resolves to nothing — no slice can be built.
		Runnables: func(p runner.LivePackage) ([]string, error) { return nil, nil },
	}
	_, err = core.BuildPlan(context.Background(), rnr, st, "", opt)
	if err == nil {
		t.Fatal("plan accepted a whale sliced into nothing; every test in it would be dropped")
	}
	if !strings.Contains(err.Error(), whale) {
		t.Errorf("gate failure does not name the dropped file: %v", err)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
