package planbind

import (
	"testing"

	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// unitFixture is one scheduled unit, shaped like a name slice so every feature
// in the schema takes a non-default value.
func unitFixture() runner.Unit {
	return runner.Unit{
		ID: "tests/alpha.spec.ts[alpha one]", Kind: runner.KindRunSlice, Count: 1,
		Run:      []string{"alpha one"},
		Packages: []runner.LivePackage{{ID: "tests/alpha.spec.ts", HasTests: true}},
	}
}

// builderFixture is the pre-plan evidence the builder reads: two names listed
// for the sliced target, and one co-scheduling atom of size 2.
func builderFixture() *FeatureBuilder {
	return &FeatureBuilder{
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

// TestRunnableCountComesFromTheRecordedListing is the numeric half of the
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
