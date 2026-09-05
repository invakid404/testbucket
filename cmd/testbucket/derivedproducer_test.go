package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/planbind"
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// TestTheDerivedProjectionsHaveAProductionProducer is part of the F5
// regression.
//
// The campaign's ablation gate loads the atom/topology/membership document,
// rederives its digests against the Stage-2 receipt and reads it to decide
// whether the stratum a row is authorised into is the topology the row
// actually ran — and NOTHING IN PRODUCTION WROTE THAT DOCUMENT. The gate could
// only ever be handed one somebody composed by hand, which is precisely what
// it exists to refuse.
func TestTheDerivedProjectionsHaveAProductionProducer(t *testing.T) {
	b, err := os.ReadFile("wallplan.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, `filepath.Join(outDir, "derived.json"), res.Derived`) {
		t.Error("the planner does not write the derived projections, so the ablation gate has no production producer to read")
	}
	// And the document it writes is built from the SAME maps the receipt's
	// digests are taken over, so the published document and the bound digests
	// cannot come from different plans.
	pb, err := os.ReadFile(filepath.Join("..", "..", "internal", "planbind", "planbind.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pb), "derived := DerivedProjections(doc, live)") {
		t.Error("the receipt's digests are not taken over the document the planner publishes")
	}
}

// TestTheTopologyProjectionCarriesFileIdentity is the other half of F5.
//
// The projection recorded unit KINDS, so a plan running one file twice and a
// plan running two files projected identically — and the multi-file condition
// could not be proved from the document that is supposed to prove it.
func TestTheTopologyProjectionCarriesFileIdentity(t *testing.T) {
	doc := &core.PlanDocument{Buckets: []core.PlanBucket{{
		Index: 0,
		Units: []core.PlanUnit{
			{ID: "suite/a.spec.ts", Kind: runner.KindPackage, Packages: []string{"suite/a.spec.ts"}},
			{ID: "suite/b.spec.ts", Kind: runner.KindPackage, Packages: []string{"suite/b.spec.ts"}},
		},
		Invocations: []runner.Invocation{{Units: []string{"suite/a.spec.ts", "suite/b.spec.ts"}}},
	}}}
	derived := planbind.DerivedProjections(doc, nil)
	entries := derived.Topology["bucket-0"]
	if len(entries) != 2 {
		t.Fatalf("the topology projects %d entries for two scheduled units: %v", len(entries), entries)
	}
	for _, e := range entries {
		if !strings.Contains(e, ":") {
			t.Errorf("topology entry %q states a kind with no file identity", e)
		}
	}
	if entries[0] == entries[1] {
		t.Errorf("two units covering different files projected identically as %q", entries[0])
	}
	// The same plan restricted to ONE file must not read as a multi-file
	// topology, which is what a kind-only projection could not distinguish.
	one := &core.PlanDocument{Buckets: []core.PlanBucket{{
		Index: 0,
		Units: []core.PlanUnit{
			{ID: "suite/a.spec.ts", Kind: runner.KindPackage, Packages: []string{"suite/a.spec.ts"}},
			{ID: "suite/a.spec.ts#shard1", Kind: runner.KindCountShard, Packages: []string{"suite/a.spec.ts"}},
		},
		Invocations: []runner.Invocation{{Units: []string{"suite/a.spec.ts", "suite/a.spec.ts#shard1"}}},
	}}}
	if got := walltime.RealizesForTest(planbind.DerivedProjections(one, nil), walltime.StratumWholeFileMultiFile); got == "" {
		t.Error("two units of one file were accepted as a multi-file whole-file topology")
	}
}
