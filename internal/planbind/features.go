package planbind

import (
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// FeatureSchema is the canonical pre-plan feature set: everything that is
// knowable about a unit before the plan exists.
//
// What is NOT here is the point. There is no store weight, no reporter
// duration, no previous run, no host or cache state — the store's rolling EWMA
// is the most useful number available at plan time and it is built from
// reporter outcomes, so admitting it would leak an outcome into allocation
// through the side door.
var FeatureSchema = []string{
	"atom_size",
	"file_count",
	"is_slice",
	"path_depth",
	"runnable_count",
	"slice_share",
}

// FeatureBuilder turns a scheduled unit into its immutable pre-plan feature
// vector.
type FeatureBuilder struct {
	// runnableCount is the number of names the listing recorded per
	// target. A target with no listing has no count, which is a 0 — the
	// listing's absence is itself a bound fact.
	runnableCount map[string]int
	// atomSize is how many targets share each co-scheduling key.
	atomSize map[string]int
}

// Vector builds one unit's feature vector. Every feature carries its
// provenance, so exclusion is proven by reading the vector rather than by
// trusting this function.
func (fb *FeatureBuilder) Vector(u runner.Unit) walltime.FeatureVector {
	atom := 1
	depth := 0
	for _, p := range u.Packages {
		if n := fb.atomSize[p.AtomKey()]; n > atom {
			atom = n
		}
		if d := strings.Count(p.ID, "/") + 1; d > depth {
			depth = d
		}
	}
	runnables := 0
	if len(u.Packages) == 1 {
		runnables = fb.runnableCount[u.Packages[0].ID]
	}
	isSlice := 0.0
	shareOfFile := 1.0
	if u.Kind == runner.KindRunSlice {
		isSlice = 1
		shareOfFile = 0
		if runnables > 0 {
			shareOfFile = float64(len(u.Run)) / float64(runnables)
		}
	}
	return walltime.FeatureVector{
		UnitID: u.ID,
		Features: []walltime.Feature{
			{Name: "atom_size", Value: float64(atom), Provenance: walltime.ProvPreplanAtom},
			{Name: "file_count", Value: float64(len(u.Packages)), Provenance: walltime.ProvDiscoverySnapshot},
			{Name: "is_slice", Value: isSlice, Provenance: walltime.ProvUnitIdentity},
			{Name: "path_depth", Value: float64(depth), Provenance: walltime.ProvDiscoverySnapshot},
			{Name: "runnable_count", Value: float64(runnables), Provenance: walltime.ProvRunnableSnapshot},
			{Name: "slice_share", Value: shareOfFile, Provenance: walltime.ProvUnitIdentity},
		},
	}
}

// THE SEALED ALLOCATOR IS REPLACED, NOT REIMPLEMENTED.
//
// Allocator, PcheckFor and PallocTotal wired the planner to
// `Palloc[u] = frozen_scorer(frozen_preplan_unit_feature_vector[u])` and
// re-derived that allocation into a Pcheck document. The component map replaces
// the sealed static-feature scorer with a versioned ORDINARY-HISTORY wall
// model, which is core's fitted A_eta over the §15.1a ring driving §6.5's
// two-stage allocator — so the scorer, its digest, the Pcheck projection and
// the adapter that carried them are gone. What survives here is the
// deterministic unit/membership projection above, which is what the map keeps.
