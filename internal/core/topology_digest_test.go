package core

import (
	"context"
	"io"
	"testing"

	"github.com/invakid404/testbucket/internal/runner"
)

// topoRunner is the smallest runner the expansion needs: it reports the runnable
// names of a package and validates nothing.
type topoRunner struct{ names map[string][]string }

func (r topoRunner) Discover(context.Context) ([]runner.LivePackage, error) { return nil, nil }
func (r topoRunner) Runnables(_ context.Context, p runner.LivePackage) ([]string, error) {
	return r.names[p.ID], nil
}
func (r topoRunner) Render(runner.Bucket) runner.Rendered { return runner.Rendered{} }
func (r topoRunner) ValidateUnit(runner.Unit, map[string]runner.LivePackage, int) []string {
	return nil
}
func (r topoRunner) CanonicalToken() string { return "vitest" }
func (r topoRunner) ParseTimings(...io.Reader) (*runner.RunSummary, error) {
	return &runner.RunSummary{}, nil
}

// sliced gives a unit a stored weight and the name-slice policy, which is the
// stored topology the digest has to be able to see.
func sliced(st *Store, id string, seconds float64, into int, tests []string) {
	u := &UnitStat{Seconds: seconds, Samples: 5, Split: "run", SplitInto: into,
		Tests: map[string]float64{}}
	for _, name := range tests {
		u.Tests[name] = seconds / float64(len(tests))
	}
	st.Units[id] = u
}

// whole gives a unit a stored weight and no split policy.
func whole(st *Store, id string, seconds float64) {
	st.Units[id] = &UnitStat{Seconds: seconds, Samples: 5}
}

// TestExpandedTopologyDigestSeesSliceMembership is F07's control.
//
// The digest it replaces hashed the discovered FILE set, so two plans over
// exactly the same files with different stored name-slice membership carried the
// same expanded_unit_set_digest — while §10 and §19.2 put that digest in the B/C
// invariant to make exactly that difference visible. Discovery identity is not
// topology identity.
func TestExpandedTopologyDigestSeesSliceMembership(t *testing.T) {
	ctx := context.Background()
	live := []runner.LivePackage{
		{ID: "a.spec.ts", HasTests: true},
		{ID: "b.spec.ts", HasTests: true},
	}
	rnr := topoRunner{names: map[string][]string{
		"a.spec.ts": {"one", "two", "three", "four"},
		"b.spec.ts": {"only"},
	}}
	opt := PlanOptions{K: 4, Count: 1, Token: "vitest", Live: live}

	// A store that makes a.spec.ts heavy enough to be name-sliced, and one that
	// does not. The FILE SET is identical in both.
	heavy := NewStore("vitest")
	sliced(heavy, "a.spec.ts", 600.0, 4, []string{"one", "two", "three", "four"})
	whole(heavy, "b.spec.ts", 1.0)
	flat := NewStore("vitest")
	whole(flat, "a.spec.ts", 1.0)
	whole(flat, "b.spec.ts", 1.0)

	dHeavy, err := ExpandedTopologyDigest(ctx, rnr, heavy, "", opt)
	if err != nil {
		t.Fatal(err)
	}
	dFlat, err := ExpandedTopologyDigest(ctx, rnr, flat, "", opt)
	if err != nil {
		t.Fatal(err)
	}

	// First prove the two stores really do expand differently, so a digest that
	// agrees is a defect rather than a coincidence of the fixture.
	uHeavy, err := ExpandUnitsFor(ctx, rnr, heavy, opt)
	if err != nil {
		t.Fatal(err)
	}
	uFlat, err := ExpandUnitsFor(ctx, rnr, flat, opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(uHeavy) == len(uFlat) {
		t.Skipf("the fixture no longer produces two different expansions (%d units both ways); the digest claim is untested",
			len(uHeavy))
	}
	if dHeavy == dFlat {
		t.Fatalf("two different expansions (%d and %d units) share digest %s; the digest cannot see slice membership",
			len(uHeavy), len(uFlat), dHeavy)
	}

	t.Run("the digest is stable for one expansion", func(t *testing.T) {
		again, err := ExpandedTopologyDigest(ctx, rnr, heavy, "", opt)
		if err != nil {
			t.Fatal(err)
		}
		if again != dHeavy {
			t.Fatalf("the same expansion digested twice gave %s then %s; the B/C invariant needs it byte-identical across the pair",
				dHeavy, again)
		}
	})

	t.Run("an unusable store digests the cold expansion the plan will get", func(t *testing.T) {
		// BuildPlan discards a store recorded under another token. The digest
		// must describe the expansion that really happens, not one taken over a
		// store about to be thrown away.
		foreign := NewStore("go -count=1")
		sliced(foreign, "a.spec.ts", 600.0, 4, []string{"one", "two", "three", "four"})
		got, err := ExpandedTopologyDigest(ctx, rnr, foreign, "", opt)
		if err != nil {
			t.Fatal(err)
		}
		cold, err := ExpandedTopologyDigest(ctx, rnr, nil, "no store", opt)
		if err != nil {
			t.Fatal(err)
		}
		if got != cold {
			t.Errorf("a wrong-token store digested as %s, the cold expansion as %s; the plan cold-starts, so they are the same expansion",
				got, cold)
		}
	})
}
