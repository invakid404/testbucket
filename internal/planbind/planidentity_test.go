package planbind

import (
	"context"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// TestThePlannerRefusesInventedImplementationIdentities is the F2 regression.
//
// The bundle bound LABELS: seven parser digests were SHA-256 of strings like
// `"vitest-discovery-parser/testbucket/v0.3"`, the two algorithm identities
// carried a shared version string, three required stages (lock, unit expansion,
// coverage) were not bound at all, and nothing ever compared any of it with the
// implementations the planner executes. A caller could invent every identity
// and the plan would be produced regardless, with Stage 2 echoing the claim.
func TestThePlannerRefusesInventedImplementationIdentities(t *testing.T) {
	root := t.TempDir()
	base := baseAcquire(t, root, nil)
	good, err := Acquire(base)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// The genuine bundle plans.
	if _, err := Plan(context.Background(), PlanOptions{Bundle: good, Stage1: "sha256:stage1"}); err != nil {
		t.Fatalf("the genuine bundle does not plan: %v", err)
	}

	for _, tc := range []struct {
		name string
		edit func(*walltime.PlanningInputBundle)
		want string
	}{
		{"an invented parser digest", func(b *walltime.PlanningInputBundle) {
			b.Parsers = append([]walltime.ParserIdentity(nil), b.Parsers...)
			b.Parsers[0].Digest = walltime.DigestBytes([]byte("invented parser bytes"))
		}, "the implementation that will run"},
		{"an invented parser version", func(b *walltime.PlanningInputBundle) {
			b.Parsers = append([]walltime.ParserIdentity(nil), b.Parsers...)
			b.Parsers[2].Version = "some-other-version"
		}, "the implementation that will run"},
		{"an invented full-plan canonicaliser", func(b *walltime.PlanningInputBundle) {
			b.Algorithms.FullPlan.Canonicalizer = "invented-canonicaliser"
		}, "the implementation that will run"},
		{"an invented semantic-plan implementation", func(b *walltime.PlanningInputBundle) {
			b.Algorithms.SemanticPlan.Implementation = "invented-implementation"
		}, "the implementation that will run"},
		{"an inventory missing the lock parser", func(b *walltime.PlanningInputBundle) {
			var kept []walltime.ParserIdentity
			for _, p := range b.Parsers {
				if p.Name != walltime.ParserLock {
					kept = append(kept, p)
				}
			}
			b.Parsers = kept
		}, walltime.ParserLock + " is not bound at all"},
		{"an inventory missing coverage policy", func(b *walltime.PlanningInputBundle) {
			var kept []walltime.ParserIdentity
			for _, p := range b.Parsers {
				if p.Name != walltime.ParserCoverage {
					kept = append(kept, p)
				}
			}
			b.Parsers = kept
		}, walltime.ParserCoverage + " is not bound at all"},
		{"a parser this build does not implement", func(b *walltime.PlanningInputBundle) {
			b.Parsers = append(append([]walltime.ParserIdentity(nil), b.Parsers...),
				walltime.ParserIdentity{
					Name: "invented-policy", Version: walltime.PlanImplementationVersion, Digest: walltime.SelfDigest(),
				})
		}, "no such parser or policy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := Acquire(baseAcquire(t, t.TempDir(), nil))
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(b)
			_, err = Plan(context.Background(), PlanOptions{Bundle: b, Stage1: "sha256:stage1"})
			if err == nil {
				t.Fatalf("the production planner executed under %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestTheBundleBindsTheCompleteRequiredInventory: the contract names every
// stage that decides the plan, and the previous list bound seven of them.
func TestTheBundleBindsTheCompleteRequiredInventory(t *testing.T) {
	b, err := Acquire(baseAcquire(t, t.TempDir(), nil))
	if err != nil {
		t.Fatal(err)
	}
	bound := map[string]walltime.ParserIdentity{}
	for _, p := range b.Parsers {
		bound[p.Name] = p
	}
	for _, want := range walltime.RequiredParserIdentities {
		p, ok := bound[want]
		if !ok {
			t.Errorf("the bundle binds no identity for %s, which the contract requires", want)
			continue
		}
		// Every identity is the RUNNING BINARY, not a hash of its own name.
		if p.Digest != walltime.SelfDigest() {
			t.Errorf("%s is bound as %s, not the implementation digest %s", want, p.Digest, walltime.SelfDigest())
		}
		if p.Digest == walltime.DigestBytes([]byte(want+"/"+p.Version)) {
			t.Errorf("%s is bound as a digest of its own label, which identifies a name and not an implementation", want)
		}
	}
}

// TestStage2RecordsTheImplementationsThatRan: the receipt used to copy the
// bundle's algorithm identities, so it repeated a claim rather than reporting
// what executed.
func TestStage2RecordsTheImplementationsThatRan(t *testing.T) {
	b, err := Acquire(baseAcquire(t, t.TempDir(), nil))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Plan(context.Background(), PlanOptions{Bundle: b, Stage1: "sha256:stage1"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got, want := res.Receipt.Algorithms.FullPlan, walltime.ImplementedFullPlanAlgorithm(); got != want {
		t.Errorf("Stage 2 records full-plan algorithm %+v, want the implementation %+v", got, want)
	}
	if got, want := res.Receipt.Algorithms.SemanticPlan, walltime.ImplementedSemanticPlanAlgorithm(); got != want {
		t.Errorf("Stage 2 records semantic-plan algorithm %+v, want the implementation %+v", got, want)
	}
}
