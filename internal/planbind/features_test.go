package planbind

import (
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// frozenScorer is a scorer with hand-written coefficients, which is what a
// fitted wall model produces. It scores by runnable count, so it deliberately
// disagrees with any measured store weight — that disagreement is what makes
// the separation assertions below meaningful.
//
// It is built as a literal rather than trained: the sealed training lineage
// went with the Stage receipts, and what the allocator adapter needs from a
// scorer is the frozen coefficients, not the ceremony that produced them.
func frozenScorer() walltime.Scorer {
	return walltime.Scorer{
		Kind: walltime.ScorerKind, ID: "test-scorer", Version: "1",
		FeatureSchema: FeatureSchema,
		Coefficients: map[string]float64{
			"atom_size": 1, "file_count": 0, "is_slice": 0,
			"path_depth": 0, "runnable_count": 2, "slice_share": 0,
		},
		Intercept: 5, Floor: 0.1,
	}
}

// unitFixture is one scheduled unit, shaped like a name slice so every feature
// in the schema takes a non-default value.
func unitFixture() runner.Unit {
	return runner.Unit{
		ID: "tests/alpha.spec.ts[alpha one]", Kind: runner.KindRunSlice, Count: 1,
		Run:      []string{"alpha one"},
		Packages: []runner.LivePackage{{ID: "tests/alpha.spec.ts", HasTests: true}},
	}
}

// builderFixture is the frozen pre-plan evidence the builder reads: two names
// listed for the sliced target, and one co-scheduling atom of size 2.
func builderFixture() *FeatureBuilder {
	return &FeatureBuilder{
		stage1:        "sha256:ef24c98b6f6843d9d586189733598c533de9fa109464aa1d7045c667a4621b0f",
		runnableCount: map[string]int{"tests/alpha.spec.ts": 2},
		atomSize:      map[string]int{},
	}
}

// TestFeatureVectorsCarryOnlyPreplanProvenance is the leakage check at the
// point where features are actually built.
//
// What must NOT be in the vector is the point: no store weight, no reporter
// duration, no previous run. The store's rolling EWMA is the most useful
// number available at plan time and it is built from reporter outcomes, so
// admitting it would leak an outcome into allocation through the side door.
// Exclusion is proven by READING the vector rather than by trusting the
// function that built it.
func TestFeatureVectorsCarryOnlyPreplanProvenance(t *testing.T) {
	allowed := map[string]bool{
		walltime.ProvUnitIdentity: true, walltime.ProvDiscoverySnapshot: true,
		walltime.ProvRunnableSnapshot: true, walltime.ProvPreplanAtom: true,
	}
	v := builderFixture().Vector(unitFixture())
	if len(v.Features) != len(FeatureSchema) {
		t.Errorf("the builder produced %d features, the canonical schema has %d", len(v.Features), len(FeatureSchema))
	}
	for _, f := range v.Features {
		if !allowed[f.Provenance] {
			t.Errorf("feature %q has provenance %q, which is not an immutable pre-plan class", f.Name, f.Provenance)
		}
	}
	if err := v.Validate(FeatureSchema); err != nil {
		t.Errorf("the builder's own vector does not satisfy the canonical schema: %v", err)
	}
}

// TestRunnableCountComesFromTheFrozenListing is the numeric half of the
// provenance check. Provenance says WHERE a feature came from; this says the
// value is actually the one the pre-plan evidence carries.
//
// The failure it guards against is quiet: a builder that holds listing bytes
// but no parsed names presents a satisfied scorer schema while reporting a
// count of zero, which changes allocation and the projection together while
// looking entirely well-formed.
func TestRunnableCountComesFromTheFrozenListing(t *testing.T) {
	v := builderFixture().Vector(unitFixture())
	got, ok := v.Value("runnable_count")
	if !ok {
		t.Fatal("the vector carries no runnable_count")
	}
	if got != 2 {
		t.Errorf("runnable_count = %v, want the 2 names the listing recorded", got)
	}
	// And the slice share is derived from that same count, not from a
	// placeholder: one name of two is half the file.
	if share, _ := v.Value("slice_share"); share != 0.5 {
		t.Errorf("slice_share = %v, want 0.5", share)
	}

	// A target with NO listing has no count. The absence is itself a bound
	// fact, and it must not be dressed up as a satisfied feature elsewhere.
	empty := &FeatureBuilder{runnableCount: map[string]int{}, atomSize: map[string]int{}}
	v = empty.Vector(unitFixture())
	if got, _ := v.Value("runnable_count"); got != 0 {
		t.Errorf("an unlisted target reported runnable_count = %v, want 0", got)
	}
	if share, _ := v.Value("slice_share"); share != 0 {
		t.Errorf("a slice of an unlisted target reported slice_share = %v, want 0", share)
	}
}

// TestTheAllocatorScoresFromTheFrozenScorerAlone is the separation the
// contract insists on: the frozen score decides the SPLIT, and it is a
// function of the pre-plan vector and nothing else.
func TestTheAllocatorScoresFromTheFrozenScorerAlone(t *testing.T) {
	a := NewAllocator(frozenScorer(), builderFixture())
	u := unitFixture()
	// The unit carries a measured weight. It must not reach the score.
	u.Seconds = 999

	got, err := a.Score(u)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	// intercept 5 + atom_size 1*1 + runnable_count 2*2 = 10.
	if want := 10.0; got != want {
		t.Errorf("Palloc = %v, want %v; the score is the frozen scorer's, not the store's", got, want)
	}
	if a.Values()[u.ID] != got {
		t.Errorf("the allocator retained %v, having returned %v", a.Values()[u.ID], got)
	}
	// The vector it scored FROM is retained too, so the projection can be
	// re-derived by running the frozen scorer again rather than only checked
	// against the allocator's own arithmetic.
	vecs := a.Vectors()
	if len(vecs) != 1 || vecs[0].UnitID != u.ID {
		t.Fatalf("the allocator retained %d vectors, want the one it scored", len(vecs))
	}
	again, err := frozenScorer().Score(vecs[0])
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Errorf("re-running the frozen scorer over the retained vector gives %v, not %v", again, got)
	}
}

// TestAllocationFailsClosed: a unit the frozen scorer cannot score fails the
// plan. Falling back to the store weight would be the leak the two surfaces
// exist to prevent, wearing the costume of robustness.
func TestAllocationFailsClosed(t *testing.T) {
	sc := frozenScorer()
	sc.FeatureSchema = append(append([]string(nil), sc.FeatureSchema...), "previous_run_seconds")
	a := NewAllocator(sc, builderFixture())

	got, err := a.Score(unitFixture())
	if err == nil {
		t.Fatalf("a unit scored %v under a schema the builder cannot satisfy", got)
	}
	if !strings.Contains(err.Error(), "previous_run_seconds") {
		t.Errorf("the error does not name the missing feature: %v", err)
	}
	if len(a.Values()) != 0 {
		t.Errorf("a refused unit was still recorded: %v", a.Values())
	}
}
