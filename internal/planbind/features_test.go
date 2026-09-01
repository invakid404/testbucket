package planbind

import (
	"context"
	"sort"
	"strings"
	"testing"

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
			ReceiptSetDigest: "sha256:sealed", Cutoff: "2026-08-30T00:00:00Z",
			Epoch: "vitest-4.1.10", ScorerID: "test-scorer",
			Algorithm: "ridge-least-squares", TieBreak: "unit_id_ascending",
		},
	}
}

// TestPallocAllocatesWithoutTouchingTheMatrix is the separation the contract
// insists on: the frozen score decides the SPLIT, the store's measured weights
// still decide what a human (and every consumer workflow) reads.
func TestPallocAllocatesWithoutTouchingTheMatrix(t *testing.T) {
	root := t.TempDir()
	b := acquire(t, root, nil)

	plain := plan(t, b)
	scored, err := Plan(context.Background(), PlanOptions{Bundle: b, Stage1: "sha256:stage1", Scorer: frozenScorer()})
	if err != nil {
		t.Fatalf("Plan with a frozen scorer: %v", err)
	}

	// Every reported estimate is the store's, in both plans.
	for i, want := range plain.Doc.Buckets {
		got := scored.Doc.Buckets[i]
		if got.Seconds != want.Seconds {
			t.Errorf("bucket %d est_seconds = %v with a scorer, %v without; the matrix must not move",
				i, got.Seconds, want.Seconds)
		}
	}
	matrix, err := scored.Doc.MatrixJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(matrix), "palloc") {
		t.Errorf("the allocation score leaked into the matrix:\n%s", matrix)
	}
	// And the plan says out loud that it was packed by something other than
	// the estimates a reader can see.
	if !strings.Contains(scored.Doc.Algorithm, "allocation score") {
		t.Errorf("the plan does not record that it was packed by the frozen score: %q", scored.Doc.Algorithm)
	}
	if scored.Receipt.PlanDigest == plain.Receipt.PlanDigest {
		t.Errorf("planning by a different rule produced an identical document digest")
	}

	// Pcheck projects the SAME frozen values the partition used, over the
	// renderer's reported membership.
	pcheck, err := PcheckFor(scored.Doc, -1, scored.Receipt.SemanticDigest, scored.Receipt.MembershipDigest, scored.Allocator)
	if err != nil {
		t.Fatalf("PcheckFor: %v", err)
	}
	if len(pcheck.Invocations) == 0 {
		t.Fatalf("the projection covered no invocation")
	}
	values := scored.Allocator.Values()
	for _, inv := range pcheck.Invocations {
		var want float64
		for _, u := range inv.Units {
			v, ok := values[u]
			if !ok {
				t.Fatalf("invocation %d cites unit %q, which was never scored", inv.Seq, u)
			}
			want += v
		}
		if got := float64(inv.PredictedNs) / 1e9; got < want-0.001 || got > want+0.001 {
			t.Errorf("invocation %d projects %.3f s, want %.3f s", inv.Seq, got, want)
		}
	}
}

// TestAllocationFailsClosed: a unit the frozen scorer cannot score fails the
// plan. Falling back to the store weight would be the leak the two surfaces
// exist to prevent, wearing the costume of robustness.
func TestAllocationFailsClosed(t *testing.T) {
	root := t.TempDir()
	b := acquire(t, root, nil)
	sc := frozenScorer()
	sc.FeatureSchema = append(sc.FeatureSchema, "previous_run_seconds")
	_, err := Plan(context.Background(), PlanOptions{Bundle: b, Stage1: "sha256:stage1", Scorer: sc})
	if err == nil {
		t.Fatalf("a plan succeeded with a scorer whose schema the builder cannot satisfy")
	}
	if !strings.Contains(err.Error(), "previous_run_seconds") {
		t.Errorf("the error does not name the missing feature: %v", err)
	}
}

// TestFeatureVectorsCarryOnlyPreplanProvenance is the leakage check at the
// point where features are actually built.
func TestFeatureVectorsCarryOnlyPreplanProvenance(t *testing.T) {
	root := t.TempDir()
	b := acquire(t, root, nil)
	res, err := Plan(context.Background(), PlanOptions{Bundle: b, Stage1: "sha256:stage1", Scorer: frozenScorer()})
	if err != nil {
		t.Fatal(err)
	}
	fb := NewFeatureBuilder(b, nil, "sha256:stage1")
	allowed := map[string]bool{
		walltime.ProvUnitIdentity: true, walltime.ProvDiscoverySnapshot: true,
		walltime.ProvRunnableSnapshot: true, walltime.ProvPreplanAtom: true,
	}
	if len(res.Doc.Buckets) == 0 {
		t.Fatalf("the plan has no buckets to check")
	}
	// The builder's own output is what the scorer sees; check every feature's
	// provenance is one of the four immutable pre-plan classes.
	v := fb.Vector(unitFixture())
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

// unitFixture is one scheduled unit, shaped like a name slice so every feature
// in the schema takes a non-default value.
func unitFixture() runner.Unit {
	return runner.Unit{
		ID: "tests/alpha.spec.ts[alpha one]", Kind: runner.KindRunSlice, Count: 1,
		Run:      []string{"alpha one"},
		Packages: []runner.LivePackage{{ID: "tests/alpha.spec.ts", HasTests: true}},
	}
}

// TestRunnableCountComesFromTheFrozenListing is the numeric half of the
// provenance check. Provenance says WHERE a feature came from; this says the
// value is actually the one the frozen evidence carries.
//
// The failure it guards against is quiet: a bundle that freezes raw listing
// bytes but no parsed names presents a satisfied scorer schema while reporting
// a count of zero, which changes allocation, Pcheck and Aeta together while
// looking entirely well-formed.
func TestRunnableCountComesFromTheFrozenListing(t *testing.T) {
	root := t.TempDir()
	b := acquire(t, root, nil)

	if len(b.Runnables) != 1 {
		t.Fatalf("the bundle froze %d runnable listing(s), want 1", len(b.Runnables))
	}
	snap := b.Runnables[0]
	if got, want := len(snap.Names), 2; got != want {
		t.Errorf("the frozen listing parsed to %d names (%v), want %d", got, snap.Names, want)
	}
	// Vitest's -t matches the space-joined form, so the parser sorts; the
	// frozen order is the parser's, not the capture's.
	if len(snap.Names) == 2 && !sort.StringsAreSorted(snap.Names) {
		t.Errorf("the frozen names are not in the parser's order: %v", snap.Names)
	}

	fb := NewFeatureBuilder(b, nil, "sha256:stage1")
	v := fb.Vector(runner.Unit{
		ID: "tests/alpha.spec.ts", Kind: runner.KindPackage, Count: 1,
		Packages: []runner.LivePackage{{ID: "tests/alpha.spec.ts", HasTests: true}},
	})
	got, ok := v.Value("runnable_count")
	if !ok {
		t.Fatalf("the vector has no runnable_count")
	}
	if got != 2 {
		t.Errorf("runnable_count = %v, want 2 — the frozen listing names two tests", got)
	}

	// A slice of one of those two names is half the file.
	slice := fb.Vector(runner.Unit{
		ID: "tests/alpha.spec.ts[alpha one]", Kind: runner.KindRunSlice, Count: 1,
		Run:      []string{"alpha one"},
		Packages: []runner.LivePackage{{ID: "tests/alpha.spec.ts", HasTests: true}},
	})
	if share, _ := slice.Value("slice_share"); share != 0.5 {
		t.Errorf("slice_share = %v, want 0.5", share)
	}
}

// TestFrozenListingRefusesTruncatedEvidence: a listing whose bytes are present
// but parse to nothing is refused at acquisition, not silently frozen as a
// zero count. The empty-title case must survive, because `test("")` is a real
// runnable and dropping it would slice a whale over an incomplete universe.
func TestFrozenListingRefusesTruncatedEvidence(t *testing.T) {
	root := t.TempDir()
	if _, err := Acquire(baseAcquire(t, root, func(o *AcquireOptions) {
		o.Runnables = map[string][]byte{"tests/alpha.spec.ts": []byte(`[]`)}
	})); err == nil {
		t.Errorf("a listing that parses to no names was frozen for a sliced target")
	}
	// A row with no name field at all is a truncated capture, and the bound
	// parser refuses it rather than dropping a test.
	if _, err := Acquire(baseAcquire(t, root, func(o *AcquireOptions) {
		o.Runnables = map[string][]byte{"tests/alpha.spec.ts": []byte(`[{"file":"tests/alpha.spec.ts"}]`)}
	})); err == nil {
		t.Errorf("a truncated listing row was frozen")
	}
	// A legal empty title is kept.
	b, err := Acquire(baseAcquire(t, root, func(o *AcquireOptions) {
		o.Runnables = map[string][]byte{
			"tests/alpha.spec.ts": []byte(`[{"name":"","file":"tests/alpha.spec.ts"},{"name":"named","file":"tests/alpha.spec.ts"}]`),
		}
	}))
	if err != nil {
		t.Fatalf("a legal empty-title runnable was refused: %v", err)
	}
	if got := len(b.Runnables[0].Names); got != 2 {
		t.Errorf("froze %d names, want 2 — the empty title is a real runnable", got)
	}
}
