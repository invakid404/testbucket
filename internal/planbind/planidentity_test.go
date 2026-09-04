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
	if _, err := Plan(context.Background(), withClaim(t, PlanOptions{Bundle: good, Stage1: "sha256:ef24c98b6f6843d9d586189733598c533de9fa109464aa1d7045c667a4621b0f"})); err != nil {
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
			_, err = Plan(context.Background(), withClaim(t, PlanOptions{Bundle: b, Stage1: "sha256:ef24c98b6f6843d9d586189733598c533de9fa109464aa1d7045c667a4621b0f"}))
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

// TestARealReceiptIsComparedByEveryClaimItCarries joins the two halves that
// were repaired separately: the production DERIVATION and the production
// COMPARISON.
//
// Issuance was made honest one round before the comparison was made complete,
// which is exactly the arrangement that hides a gap — the receipt stated the
// truth and the replay would have accepted a lie. Planning twice and comparing
// real receipts is the only check that spans both.
func TestARealReceiptIsComparedByEveryClaimItCarries(t *testing.T) {
	// ONE bundle, planned twice. The bundle carries its own root, so two
	// bundles acquired from two directories are two different frozen inputs
	// and would differ for an uninteresting reason; the property under test is
	// that the same frozen inputs derive the same receipt.
	root := t.TempDir()
	bundle, err := Acquire(baseAcquire(t, root, nil))
	if err != nil {
		t.Fatal(err)
	}
	plan := func(t *testing.T) walltime.Stage2Receipt {
		t.Helper()
		res, err := Plan(context.Background(), withClaim(t, PlanOptions{Bundle: bundle, Stage1: "sha256:ef24c98b6f6843d9d586189733598c533de9fa109464aa1d7045c667a4621b0f"}))
		if err != nil {
			t.Fatalf("Plan: %v", err)
		}
		return res.Receipt
	}
	issued := plan(t)

	// Two independent derivations of the same frozen inputs agree, including
	// every field the comparison now reaches.
	if err := issued.Matches(plan(t)); err != nil {
		t.Fatalf("two independent derivations of the same bundle disagree: %v", err)
	}

	// And each claim, mutated on a REAL receipt, is caught.
	for _, tc := range []struct {
		name string
		edit func(*walltime.Stage2Receipt)
		want string
	}{
		{"a claimed full-plan implementation nobody ran", func(r *walltime.Stage2Receipt) {
			r.Algorithms.FullPlan.Implementation = "sha256:3e5bfc834bb08eb81022641fe1f0e4e79649af49645e5a08c48632ccb1ad1423"
		}, "full-plan digest algorithm mismatch"},
		{"a claimed semantic-plan canonicaliser nobody ran", func(r *walltime.Stage2Receipt) {
			r.Algorithms.SemanticPlan.Canonicalizer = "some-other-canonicaliser"
		}, "semantic-plan digest algorithm mismatch"},
		{"an input access that did not happen", func(r *walltime.Stage2Receipt) {
			r.InputAccess = append(append([]walltime.InputAccess(nil), r.InputAccess...),
				walltime.InputAccess{Field: "an-input-nobody-read", Digest: "sha256:80914f0274ced542b3c64fd18666296efc1be86f90fc57f02ac5b46ed46d4489"})
		}, "input-access mismatch"},
		{"a planner result the plan does not produce", func(r *walltime.Stage2Receipt) {
			r.PlannerResult = "k=99 buckets=99 units=99"
		}, "planner verifier result mismatch"},
		{"a renderer result the plan does not produce", func(r *walltime.Stage2Receipt) {
			r.RendererResult = "invocations=999"
		}, "renderer verifier result mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tampered := issued
			tc.edit(&tampered)
			err := tampered.Matches(plan(t))
			if err == nil {
				t.Fatalf("an independent replay accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
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
	res, err := Plan(context.Background(), withClaim(t, PlanOptions{Bundle: b, Stage1: "sha256:ef24c98b6f6843d9d586189733598c533de9fa109464aa1d7045c667a4621b0f"}))
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
