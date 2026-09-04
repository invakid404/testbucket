package walltime

import (
	"strings"
	"testing"
)

// matchableReceipt is a complete, valid Stage-2 receipt — every field the
// replay comparison has to reach.
// matchableReceipt deliberately carries NO planner claim: a receipt that
// records none must not validate, and that refusal is exercised directly.
// Tests that need a well-formed receipt call claimed() on it.
func claimed(r Stage2Receipt) Stage2Receipt {
	r.PlannerClaim = fixtureClaim(r.Stage1Digest, r.BundleDigest)
	return r
}

func matchableReceipt() Stage2Receipt {
	r := Stage2Receipt{
		Kind:         Stage2Kind,
		Stage1Digest: "sha256:stage1",
		BundleDigest: "sha256:bundle",
		InputAccess: []InputAccess{
			{Field: "discovery[0]", Digest: "sha256:discovery"},
			{Field: "store", Digest: "sha256:store"},
		},
		PlanDigest:       "sha256:plan",
		SemanticDigest:   "sha256:semantic",
		AtomDigest:       "sha256:atoms",
		TopologyDigest:   "sha256:topology",
		MembershipDigest: "sha256:membership",
		InvocationDigest: "sha256:invocations",
		ScriptDigest:     "sha256:script",
		MatrixDigest:     "sha256:matrix",
		PlannerResult:    "planner verified",
		RendererResult:   "renderer verified",
		Sidecars:         map[string]Digest{"pcheck-1": "sha256:pcheck"},
	}
	r.Algorithms.FullPlan = ImplementedFullPlanAlgorithm()
	r.Algorithms.SemanticPlan = ImplementedSemanticPlanAlgorithm()
	return r
}

// TestTheReplayComparesEveryStage2DerivationClaim is the F2 regression.
//
// `Matches` compared ten digests, the Stage-1 approval and the sidecars, then
// returned success — leaving the input-access receipt, both algorithm
// identities and the two deterministic verifier results uncompared. Issuance
// had already been repaired to record genuine implementation identities, which
// made the gap easy to miss: the receipt stated the truth and the replay would
// have accepted a lie.
//
// Digest agreement is not a substitute for any of these. Two implementations
// of one named algorithm agree until the day they diverge, which is the day
// the comparison exists for; a different input-access record describes a
// different derivation whose plan digest happens to match; and a verifier
// result nobody compares is a sentence in a file.
func TestTheReplayComparesEveryStage2DerivationClaim(t *testing.T) {
	issued := claimed(matchableReceipt())
	if err := issued.Validate(); err != nil {
		t.Fatalf("the control receipt is invalid: %v", err)
	}
	// The positive control: a replay that recomputed the same thing agrees.
	if err := issued.Matches(claimed(matchableReceipt())); err != nil {
		t.Fatalf("an identical replay was rejected: %v", err)
	}

	for _, tc := range []struct {
		name string
		edit func(*Stage2Receipt)
		want string
	}{
		{"a different full-plan implementation", func(r *Stage2Receipt) {
			r.Algorithms.FullPlan.Implementation = "sha256:independently-recomputed-full"
		}, "full-plan digest algorithm mismatch"},
		{"a different full-plan canonicaliser", func(r *Stage2Receipt) {
			r.Algorithms.FullPlan.Canonicalizer = "some-other-canonicaliser"
		}, "full-plan digest algorithm mismatch"},
		{"a different semantic-plan implementation", func(r *Stage2Receipt) {
			r.Algorithms.SemanticPlan.Implementation = "sha256:independently-recomputed-semantic"
		}, "semantic-plan digest algorithm mismatch"},
		{"a different input-access digest", func(r *Stage2Receipt) {
			r.InputAccess = []InputAccess{
				{Field: "discovery[0]", Digest: "sha256:discovery"},
				{Field: "store", Digest: "sha256:a-different-store"},
			}
		}, "input-access record 1 mismatch"},
		{"a different input-access field", func(r *Stage2Receipt) {
			r.InputAccess = []InputAccess{
				{Field: "discovery[0]", Digest: "sha256:discovery"},
				{Field: "some-other-input", Digest: "sha256:store"},
			}
		}, "input-access record 1 mismatch"},
		{"fewer input accesses", func(r *Stage2Receipt) {
			r.InputAccess = r.InputAccess[:1]
		}, "input-access mismatch"},
		{"reordered input accesses", func(r *Stage2Receipt) {
			r.InputAccess = []InputAccess{r.InputAccess[1], r.InputAccess[0]}
		}, "input-access record 0 mismatch"},
		{"a different planner result", func(r *Stage2Receipt) {
			r.PlannerResult = "a different planner result"
		}, "planner verifier result mismatch"},
		{"a different renderer result", func(r *Stage2Receipt) {
			r.RendererResult = "a different renderer result"
		}, "renderer verifier result mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recomputed := claimed(matchableReceipt())
			tc.edit(&recomputed)
			err := issued.Matches(recomputed)
			if err == nil {
				t.Fatalf("the replay accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
			// And the comparison is symmetric: whichever side recomputed, a
			// divergence is a divergence.
			if err := recomputed.Matches(issued); err == nil {
				t.Errorf("the reverse comparison accepted %s", tc.name)
			}
		})
	}
}

// TestTheReplayStillComparesWhatItAlreadyDid: the fields added above are in
// addition to the ones that were compared, not instead of them.
func TestTheReplayStillComparesWhatItAlreadyDid(t *testing.T) {
	issued := claimed(matchableReceipt())
	for _, tc := range []struct {
		name string
		edit func(*Stage2Receipt)
		want string
	}{
		{"a different plan digest", func(r *Stage2Receipt) { r.PlanDigest = "sha256:elsewhere" }, "full plan document"},
		{"a different matrix digest", func(r *Stage2Receipt) { r.MatrixDigest = "sha256:elsewhere" }, "matrix"},
		{"a different Stage-1 parent", func(r *Stage2Receipt) { r.Stage1Digest = "sha256:elsewhere" }, "stage-1 parent"},
		{"a missing sidecar", func(r *Stage2Receipt) { r.Sidecars = nil }, "derived-document binding mismatch"},
		{"a different sidecar", func(r *Stage2Receipt) {
			r.Sidecars = map[string]Digest{"pcheck-1": "sha256:elsewhere"}
		}, "derived document"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recomputed := claimed(matchableReceipt())
			tc.edit(&recomputed)
			if err := issued.Matches(recomputed); err == nil {
				t.Fatalf("the replay accepted %s", tc.name)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// fixtureClaim is the one-shot planner claim a derivation is performed under.
//
// A Stage-2 receipt that records none cannot be told apart from one produced
// by a second attempt, so the schema requires it and these fixtures carry one
// like any real derivation does. A REAL store's durability is attested by the
// campaign authority and checked where the claim is taken; here the point is
// only that the receipt names the claim its own parents belong to.
func fixtureClaim(stage1, bundle Digest) *PlannerClaimReceipt {
	return &PlannerClaimReceipt{
		Store: "authority/durable-claims", Durable: true,
		Key: "fixture", Stage1: stage1, Bundle: bundle,
	}
}
