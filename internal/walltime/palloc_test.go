package walltime

import "testing"

// TestPreplanFeaturesRefuseOutcomeProvenance is what palloc.go still owes.
//
// This file used to hold TestTheScorerFloorKeepsUnitsSchedulable, which
// exercised the sealed frozen scorer. The scorer, its training lineage and the
// Pcheck re-derivation are REMOVE-classified — the component map replaces them
// with the ordinary-history wall model — so the surface that test covered no
// longer exists. What remains in palloc.go is the deterministic pre-plan
// feature projection, and its one rule is the leakage rule: a feature derived
// from an OUTCOME may not reach allocation.
//
// The file is reduced rather than deleted because a package that loses all its
// tests still reports `ok`.
func TestPreplanFeaturesRefuseOutcomeProvenance(t *testing.T) {
	schema := []string{"atom_size"}

	t.Run("an admissible pre-plan feature passes", func(t *testing.T) {
		v := FeatureVector{UnitID: "a.test.ts", Features: []Feature{
			{Name: "atom_size", Value: 2, Provenance: ProvPreplanAtom},
		}}
		if err := v.Validate(schema); err != nil {
			t.Errorf("an immutable pre-plan feature was refused: %v", err)
		}
		if got, ok := v.Value("atom_size"); !ok || got != 2 {
			t.Errorf("Value(atom_size) = %v, %v; the value is as much the contract as the provenance", got, ok)
		}
	})

	// THE STORE'S OWN EWMA IS THE INTERESTING CASE. It is the most useful
	// number available at plan time and it is built from reporter timings, so
	// admitting it would leak an outcome into allocation through the side door.
	for _, prov := range []string{"store_ewma", "reporter_timing", "observed_timing", "physical_envelope"} {
		t.Run("outcome provenance "+prov+" is refused", func(t *testing.T) {
			v := FeatureVector{UnitID: "a.test.ts", Features: []Feature{
				{Name: "atom_size", Value: 2, Provenance: prov},
			}}
			if err := v.Validate(schema); err == nil {
				t.Errorf("a feature with %q provenance reached allocation", prov)
			}
		})
	}

	t.Run("an unrecognised provenance is refused rather than allowed through", func(t *testing.T) {
		v := FeatureVector{UnitID: "a.test.ts", Features: []Feature{
			{Name: "atom_size", Value: 2, Provenance: "something_new"},
		}}
		if err := v.Validate(schema); err == nil {
			t.Error("an unknown provenance class was admitted; the list is closed, not a denylist")
		}
	})

	t.Run("a missing schema feature is refused", func(t *testing.T) {
		v := FeatureVector{UnitID: "a.test.ts", Features: []Feature{
			{Name: "file_count", Value: 1, Provenance: ProvDiscoverySnapshot},
		}}
		if err := v.Validate(schema); err == nil {
			t.Error("a vector missing a schema feature was accepted")
		}
	})
}
