package walltime

import "testing"

// TestTheScorerFloorKeepsUnitsSchedulable: a unit the model scores at or below
// zero still has to be packed somewhere, and a zero-weight unit would make the
// partition think it is free.
//
// The floor is applied inside Score rather than by each caller, because a
// caller that forgot would not fail — it would quietly produce a partition
// that schedules an unbounded number of "free" units into one bucket.
func TestTheScorerFloorKeepsUnitsSchedulable(t *testing.T) {
	scorer := Scorer{
		Kind:          "linear",
		ID:            "test-scorer",
		FeatureSchema: []string{"runnable_count", "atom_size"},
		Coefficients:  map[string]float64{"runnable_count": 0.5, "atom_size": 0.25},
		Intercept:     1,
		Floor:         0.05,
	}
	// A vector that drives the linear model far NEGATIVE: without a floor this
	// unit would be scored at -498.5 seconds.
	v := FeatureVector{UnitID: "u", Features: []Feature{
		{Name: "runnable_count", Value: -1000, Provenance: ProvRunnableSnapshot},
		{Name: "atom_size", Value: 0, Provenance: ProvPreplanAtom},
	}}
	got, err := scorer.Score(v)
	if err != nil {
		t.Fatal(err)
	}
	if got < scorer.Floor {
		t.Errorf("Palloc = %v, below the %v floor", got, scorer.Floor)
	}
	if got != scorer.Floor {
		t.Errorf("Palloc = %v, want exactly the floor %v: the floor replaces the score, it does not offset it", got, scorer.Floor)
	}

	// And it is a floor, not a clamp: a unit the model scores ABOVE it keeps
	// its own value.
	above := FeatureVector{UnitID: "u", Features: []Feature{
		{Name: "runnable_count", Value: 4, Provenance: ProvRunnableSnapshot},
		{Name: "atom_size", Value: 8, Provenance: ProvPreplanAtom},
	}}
	got, err = scorer.Score(above)
	if err != nil {
		t.Fatal(err)
	}
	if want := 1 + 0.5*4 + 0.25*8; got != want {
		t.Errorf("Palloc = %v, want %v", got, want)
	}
}
