package planbind

import (
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// frozenScorer is a scorer with hand-written coefficients, which is what a
// sealed training run would produce. It scores by runnable count, so it
// deliberately disagrees with the store's measured weights — that disagreement
// is what makes the "matrix is unchanged" assertion below meaningful.
func frozenScorer() *walltime.Scorer {
	return &walltime.Scorer{
		Kind: walltime.ScorerKind, ID: "test-scorer", Version: "1",
		FeatureSchema: []string{"atom_size", "runnable_count"},
		Coefficients:  map[string]float64{"atom_size": 1, "runnable_count": 2},
		Intercept:     5, Floor: 0.1,
		Lineage: walltime.TrainingLineageID{
			ReceiptSetDigest: "sha256:c9d0036bed6744bcdf692fc980d8717d7e5f5a4f4e8266b4a84982602fb1cd09", Cutoff: "2026-08-30T00:00:00Z",
			Epoch: "vitest-4.1.10", ScorerID: "test-scorer",
			Algorithm: "ridge-least-squares", TieBreak: "unit_id_ascending",
		},
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
