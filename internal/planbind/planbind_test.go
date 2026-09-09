package planbind

import (
	"reflect"
	"testing"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
)

// planFixture is a two-bucket plan whose second bucket runs two NAME SLICES of
// one file. That shape is the whole reason the projections are taken over
// identities rather than descriptions: two legal slices of one file share a
// description and differ in their units.
func planFixture() *core.PlanDocument {
	return &core.PlanDocument{
		K: 2, Flags: "vitest", Algorithm: "karmarkar-karp",
		Buckets: []core.PlanBucket{
			{
				Index: 0, Name: "bucket-0", Seconds: 40, NeedsNode: true,
				Units: []core.PlanUnit{{
					ID: "tests/beta.spec.ts", Kind: runner.KindPackage,
					Packages: []string{"tests/beta.spec.ts"}, Seconds: 40,
				}},
				Invocations: []runner.Invocation{{
					Dir: ".", Desc: "tests/beta.spec.ts", Units: []string{"tests/beta.spec.ts"},
					Args: []string{"run", "tests/beta.spec.ts"}, Selector: []string{"tests/beta.spec.ts"},
				}},
				Script: "vitest run tests/beta.spec.ts\n",
			},
			{
				Index: 1, Name: "bucket-1", Seconds: 90, NeedsNode: true,
				Units: []core.PlanUnit{
					{
						ID: "tests/alpha.spec.ts[alpha one]", Kind: runner.KindRunSlice,
						Packages: []string{"tests/alpha.spec.ts"}, Run: []string{"alpha one"}, Seconds: 60,
					},
					{
						ID: "tests/alpha.spec.ts[alpha two]", Kind: runner.KindRunSlice,
						Packages: []string{"tests/alpha.spec.ts"}, Run: []string{"alpha two"}, Seconds: 30,
					},
				},
				// Both slices ride in one call: same description, different units.
				Invocations: []runner.Invocation{{
					Dir: ".", Desc: "tests/alpha.spec.ts",
					Units:    []string{"tests/alpha.spec.ts[alpha two]", "tests/alpha.spec.ts[alpha one]"},
					Args:     []string{"run", "tests/alpha.spec.ts", "-t", "alpha one|alpha two"},
					Selector: []string{"tests/alpha.spec.ts", "-t", "alpha one|alpha two"},
				}},
				Script: "vitest run tests/alpha.spec.ts -t 'alpha one|alpha two'\n",
			},
		},
	}
}

// TestTheSemanticProjectionCarriesWorkAndNotForecast is what the semantic
// digest is for: two plans that differ only in a human counter share it, and
// two plans that would run one different test never do.
func TestTheSemanticProjectionCarriesWorkAndNotForecast(t *testing.T) {
	doc := planFixture()
	base := SemanticProjection(doc)
	if base.K != 2 || len(base.Buckets) != 2 {
		t.Fatalf("the projection is K=%d over %d buckets, want 2 over 2", base.K, len(base.Buckets))
	}

	// A CHANGED ESTIMATE is a different forecast, not different work.
	doc.Buckets[1].Seconds = 1234
	doc.Buckets[1].Units[0].Seconds = 1000
	doc.Summary = core.PlanSummary{}
	if got := SemanticProjection(doc); !reflect.DeepEqual(got, base) {
		t.Error("re-weighting a unit changed the semantic projection; an estimate is not work")
	}

	// A CHANGED SELECTION is different work, and must move it.
	doc = planFixture()
	doc.Buckets[1].Units[1].Run = []string{"alpha three"}
	if got := SemanticProjection(doc); reflect.DeepEqual(got, base) {
		t.Error("running a different test left the semantic projection unchanged")
	}

	// And it is DETERMINISTIC: the same document projects identically every
	// time, which is what makes the digest taken over it an identity.
	doc = planFixture()
	for i := 0; i < 4; i++ {
		if got := SemanticProjection(doc); !reflect.DeepEqual(got, base) {
			t.Fatalf("projection %d differs from the first over one unchanged document", i)
		}
	}
}

// TestTheMembershipProjectionReadsUnitsAndNotDescriptions is the immutable
// membership the audit and the predictor projection are taken over.
//
// Two legal name slices of one file have the same description and different
// units, so a membership digest taken over descriptions cannot tell them
// apart — which is exactly the slice identity the contract makes terminal.
func TestTheMembershipProjectionReadsUnitsAndNotDescriptions(t *testing.T) {
	doc := planFixture()
	got := membershipProjection(doc)
	want := map[string][]string{
		"bucket-0/inv-0": {"tests/beta.spec.ts"},
		// SORTED, so the membership of one invocation is a set rather than
		// whatever order the renderer happened to emit.
		"bucket-1/inv-0": {"tests/alpha.spec.ts[alpha one]", "tests/alpha.spec.ts[alpha two]"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("membership = %v, want %v", got, want)
	}

	// Dropping one slice from the call changes the membership even though
	// every description stays identical.
	doc.Buckets[1].Invocations[0].Units = []string{"tests/alpha.spec.ts[alpha one]"}
	if reflect.DeepEqual(membershipProjection(doc), want) {
		t.Error("covering one slice instead of two left the membership unchanged")
	}
}

// TestTheAtomProjectionClosesOverCoScheduledTargets: an atom split is
// terminal, so which targets must ride together is projected as its own
// identity rather than inferred from a bucket layout.
func TestTheAtomProjectionClosesOverCoScheduledTargets(t *testing.T) {
	live := []runner.LivePackage{
		{ID: "tests/beta.spec.ts", Atom: "suffix:spec.ts"},
		{ID: "tests/alpha.spec.ts", Atom: "suffix:spec.ts"},
		{ID: "tests/solo.test.ts", Atom: "suffix:test.ts"},
		// A target with no atom key is not a one-member atom; it has none.
		{ID: "tests/none.ts"},
	}
	got := atomProjection(live)
	want := map[string][]string{
		"suffix:spec.ts": {"tests/alpha.spec.ts", "tests/beta.spec.ts"},
		"suffix:test.ts": {"tests/solo.test.ts"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("atoms = %v, want %v", got, want)
	}
	// Discovery order must not reach the projection: the members are sorted,
	// so two runs that discovered the same tree in different orders produce
	// the same atom identity.
	reversed := append([]runner.LivePackage(nil), live...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	if !reflect.DeepEqual(atomProjection(reversed), want) {
		t.Error("reversing the discovery order changed the atom projection")
	}
}

// TestPallocTotalSumsTheScoresTheParititonUsed: the per-bucket total the
// projection reports has to be the sum of the SAME frozen numbers the
// partition packed with, not a re-score taken afterwards.
func TestPallocTotalSumsTheScoresThePartitionUsed(t *testing.T) {
	doc := planFixture()
	a := NewAllocator(frozenScorer(), builderFixture())
	// Score every unit the way the planner does, through the allocator.
	var want float64
	for _, b := range doc.Buckets {
		for _, u := range b.Units {
			if b.Index != 1 {
				continue
			}
			v, err := a.Score(runner.Unit{
				ID: u.ID, Kind: u.Kind, Run: u.Run,
				Packages: []runner.LivePackage{{ID: u.Packages[0], HasTests: true}},
			})
			if err != nil {
				t.Fatalf("Score(%s): %v", u.ID, err)
			}
			want += v
		}
	}
	got, err := PallocTotal(doc, 1, a)
	if err != nil {
		t.Fatalf("PallocTotal: %v", err)
	}
	if got != want {
		t.Errorf("PallocTotal = %v, want the %v the partition used", got, want)
	}

	// A unit the allocator never scored is an ERROR, never a silent zero: a
	// bucket total that quietly omitted a unit would under-report the lane the
	// projection is meant to check.
	if _, err := PallocTotal(doc, 0, a); err == nil {
		t.Error("a bucket with an unscored unit reported a total anyway")
	}
}
