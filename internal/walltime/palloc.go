package walltime

import (
	"fmt"
	"math"
)

// THE TWO SEALED SURFACES ARE GONE, and with them the frozen scorer.
//
// Palloc used to be exactly frozen_scorer(frozen_preplan_unit_feature_vector),
// sealed against an offline training surface bound by a Stage-1 receipt
// lineage. The component map replaces that sealed static-feature scorer with a
// versioned ORDINARY-HISTORY wall model, which is core's fitted A_eta over the
// §15.1a ring — so the scorer, its training lineage, its digest and the Pcheck
// document that re-derived a sealed allocation all go.
//
// What remains is the deterministic pre-plan feature projection: a unit's
// immutable identity and topology facts, with their provenance attached as a
// FIELD so exclusion is read rather than asserted.

// Provenance classes admissible as pre-plan features. Each one is an immutable
// fact about a unit that exists before anything is scheduled.
const (
	ProvUnitIdentity      = "frozen_unit_identity"
	ProvDiscoverySnapshot = "frozen_discovery_snapshot"
	ProvRunnableSnapshot  = "frozen_runnable_snapshot"
	ProvPreplanAtom       = "frozen_preplan_atom_membership"
)

// prohibitedRuntimeProvenance is the outcome-derived world a pre-plan feature
// may not see. The store's rolling EWMA is on this list on purpose: it is
// built from reporter timings, so using it as a pre-plan feature would leak an
// outcome into allocation through the side door.
var prohibitedRuntimeProvenance = map[string]string{
	"reporter_timing":     "reporter data is an outcome",
	"store_ewma":          "the timing store is built from reporter outcomes",
	"observed_timing":     "an observed duration is an outcome",
	"candidate":           "candidate-derived input leaks the arm under test",
	"campaign":            "campaign-derived input leaks the experiment",
	"current_run":         "current-run input leaks this run's outcome",
	"trace":               "trace time is an outcome",
	"physical_envelope":   "A, AT, VB and V are outcomes",
	"host":                "host state is not an immutable pre-plan input",
	"cache":               "cache state is not an immutable pre-plan input",
	"setup":               "setup/action-intercept timing is an outcome",
	"process":             "process timing is an outcome",
	"result":              "a result is an outcome",
	"rendered_membership": "rendered membership is post-plan",
	"post_plan_topology":  "topology after planning is post-plan",
}

// Feature is one runtime input with its provenance attached. Provenance is a
// FIELD, not a convention: the verifier proves exclusion by reading it.
type Feature struct {
	Name       string  `json:"name"`
	Value      float64 `json:"value"`
	Provenance string  `json:"provenance"`
}

// FeatureVector is one candidate unit's immutable pre-plan features.
type FeatureVector struct {
	UnitID   string    `json:"unit_id"`
	Features []Feature `json:"features"`
}

// Validate rejects any prohibited provenance, an unknown class, or a
// non-finite value.
func (v FeatureVector) Validate(schema []string) error {
	if len(v.Features) == 0 {
		return fmt.Errorf("feature vector for %q is empty", v.UnitID)
	}
	seen := map[string]bool{}
	for _, f := range v.Features {
		if why, bad := prohibitedRuntimeProvenance[f.Provenance]; bad {
			return fmt.Errorf("runtime feature %q has prohibited provenance %q: %s", f.Name, f.Provenance, why)
		}
		switch f.Provenance {
		case ProvUnitIdentity, ProvDiscoverySnapshot, ProvRunnableSnapshot, ProvPreplanAtom:
		default:
			return fmt.Errorf("runtime feature %q has unrecognised provenance %q", f.Name, f.Provenance)
		}
		if math.IsNaN(f.Value) || math.IsInf(f.Value, 0) {
			return fmt.Errorf("runtime feature %q is not finite", f.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("runtime feature %q appears twice", f.Name)
		}
		seen[f.Name] = true
	}
	for _, name := range schema {
		if !seen[name] {
			return fmt.Errorf("feature vector for %q is missing schema feature %q", v.UnitID, name)
		}
	}
	// A feature outside the schema is allowed through: it has already passed
	// the provenance check above, and nothing downstream reads a name the
	// schema does not list.
	return nil
}

// Value looks a feature up by name. It is exported because the value a
// feature carries is as much a part of the contract as its provenance: a test
// that only checks where a number came from cannot notice that it is wrong.
func (v FeatureVector) Value(name string) (float64, bool) { return v.value(name) }

// value looks a feature up by name.
func (v FeatureVector) value(name string) (float64, bool) {
	for _, f := range v.Features {
		if f.Name == name {
			return f.Value, true
		}
	}
	return 0, false
}
