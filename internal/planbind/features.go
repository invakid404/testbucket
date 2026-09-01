package planbind

import (
	"fmt"
	"sort"
	"strings"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// FeatureSchema is the canonical runtime feature set: everything the frozen
// scorer may see about a unit before the plan exists.
//
// What is NOT here is the point. There is no store weight, no reporter
// duration, no previous run, no host or cache state — the store's rolling EWMA
// is the most useful number available at plan time and it is built from
// reporter outcomes, so admitting it would leak an outcome into allocation
// through the side door. Everything below is derived from bytes Stage 1 froze.
var FeatureSchema = []string{
	"atom_size",
	"file_count",
	"is_slice",
	"path_depth",
	"runnable_count",
	"slice_share",
}

// FeatureBuilder turns a scheduled unit into its immutable pre-plan feature
// vector, using only the frozen bundle.
type FeatureBuilder struct {
	stage1 walltime.Digest
	// runnableCount is the number of names the frozen listing recorded per
	// target. A target with no listing has no count, which is a 0 — the
	// listing's absence is itself a bound fact.
	runnableCount map[string]int
	// atomSize is how many targets share each co-scheduling key.
	atomSize map[string]int
}

// NewFeatureBuilder derives the lookup tables from the bundle and the
// discovered target set. Both come from frozen bytes: the listings are in the
// bundle, and the targets were parsed from the bundle's discovery snapshot.
func NewFeatureBuilder(b *walltime.PlanningInputBundle, live []runner.LivePackage, stage1 walltime.Digest) *FeatureBuilder {
	fb := &FeatureBuilder{
		stage1:        stage1,
		runnableCount: map[string]int{},
		atomSize:      map[string]int{},
	}
	for _, r := range b.Runnables {
		fb.runnableCount[r.TargetID] = len(r.Names)
	}
	for _, p := range live {
		if k := p.AtomKey(); k != "" {
			fb.atomSize[k]++
		}
	}
	return fb
}

// Vector builds one unit's feature vector. Every feature carries the
// provenance the scorer checks, so exclusion is proven by reading the vector
// rather than by trusting this function.
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
		Stage1: fb.stage1,
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

// Allocator is the runtime allocation surface wired to the planner:
// Palloc[u] = frozen_scorer(frozen_preplan_unit_feature_vector[u]), and
// nothing else.
type Allocator struct {
	scorer  walltime.Scorer
	builder *FeatureBuilder
	// values records every score it produced, so the Pcheck projection can be
	// taken over the SAME frozen numbers the partition used rather than
	// recomputed later from a scorer that might have moved.
	values map[string]float64
	// vectors records the feature vector each score came FROM, so the
	// projection can be re-derived by running the frozen scorer again rather
	// than only checked against its own arithmetic.
	vectors map[string]walltime.FeatureVector
}

// NewAllocator binds a frozen scorer to a feature builder.
func NewAllocator(scorer walltime.Scorer, builder *FeatureBuilder) *Allocator {
	return &Allocator{
		scorer: scorer, builder: builder,
		values:  map[string]float64{},
		vectors: map[string]walltime.FeatureVector{},
	}
}

// Score is the core's AllocationScore callback. It fails rather than falls
// back: a unit the frozen scorer cannot score must fail the plan, because
// packing it by some other rule is the leak the two surfaces exist to prevent.
func (a *Allocator) Score(u runner.Unit) (float64, error) {
	fv := a.builder.Vector(u)
	v, err := a.scorer.Score(fv)
	if err != nil {
		return 0, err
	}
	a.values[u.ID], a.vectors[u.ID] = v, fv
	return v, nil
}

// Vectors is the frozen feature vector behind every score, in unit order, so
// the audit projection can carry what it was derived from.
func (a *Allocator) Vectors() []walltime.FeatureVector {
	ids := make([]string, 0, len(a.vectors))
	for id := range a.vectors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]walltime.FeatureVector, 0, len(ids))
	for _, id := range ids {
		out = append(out, a.vectors[id])
	}
	return out
}

// Values is the frozen Palloc map, for the audit projection.
func (a *Allocator) Values() map[string]float64 {
	out := make(map[string]float64, len(a.values))
	for k, v := range a.values {
		out[k] = v
	}
	return out
}

// PcheckFor builds the post-render audit projection over the plan's
// deterministic membership — the unit ids the RENDERER reported for each
// invocation, not a re-derivation from descriptions, because two name slices
// of one file are indistinguishable from their descriptions alone.
//
// It runs after rendering and changes nothing: it cannot re-run the partition,
// alter topology, or feed the scorer.
func PcheckFor(doc *core.PlanDocument, bucket int, stage2, membership walltime.Digest, a *Allocator) (*walltime.PcheckDocument, error) {
	if a == nil {
		return nil, fmt.Errorf("planbind: no frozen allocation score, so there is nothing to project")
	}
	var invocations []walltime.PcheckInvocation
	// The projection names the bucket it is FOR, so a document built for one
	// bucket cannot be verified against another's measurement. Every other
	// field two buckets of one plan carry is identical.
	name := ""
	for _, b := range doc.Buckets {
		if bucket >= 0 && b.Index != bucket {
			continue
		}
		name = b.Name
		for i, inv := range b.Invocations {
			ids := append([]string(nil), inv.Units...)
			sort.Strings(ids)
			invocations = append(invocations, walltime.PcheckInvocation{
				Seq: i, BucketIndex: b.Index, Units: ids,
			})
		}
	}
	return walltime.BuildPcheck(stage2, membership, a.scorer, a.Values(), a.Vectors(), invocations, bucket, name)
}

// PallocTotal is one bucket's frozen pre-KK Palloc total, which is what the
// Aeta template's test-dependent component is instantiated from.
func PallocTotal(doc *core.PlanDocument, bucket int, a *Allocator) (float64, error) {
	if a == nil {
		return 0, fmt.Errorf("planbind: no frozen allocation score")
	}
	values := a.Values()
	var sum float64
	for _, b := range doc.Buckets {
		if b.Index != bucket {
			continue
		}
		for _, u := range b.Units {
			v, ok := values[u.ID]
			if !ok {
				return 0, fmt.Errorf("planbind: unit %q has no frozen Palloc value", u.ID)
			}
			sum += v
		}
	}
	return sum, nil
}
