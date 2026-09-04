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
		Stage1Digest: "sha256:ef24c98b6f6843d9d586189733598c533de9fa109464aa1d7045c667a4621b0f",
		BundleDigest: "sha256:1e6ed65d77d6364eeaed5a745ba5c4985ae2b700dd85d7cf7f027bdf294a33fc",
		InputAccess: []InputAccess{
			{Field: "discovery[0]", Digest: "sha256:fa44132b238e67958fb17d33a71d325221805079909c3a7f5bed1a03666cf834"},
			{Field: "store", Digest: "sha256:824d80d71985f082a26997a8db88b5d1dd45b777d73585d03d236303e21bde97"},
		},
		PlanDigest:       "sha256:64879f7d6b960a01909762d911a32d4582c20010c5641ee90278b644a9e3b525",
		SemanticDigest:   "sha256:3784070fe3e7e3de5f0ec08eadfa10acbaa0f543916b1ab2c68f371924ff7db3",
		AtomDigest:       "sha256:bb112e00adab41da3eb94bae7e85c88c6eb4a71738ca9d3b432fabe1e91d5813",
		TopologyDigest:   "sha256:e6e2b826e31fca5c36125c48f130dcb6f961e698ff8a8776a1f290cf0892e8e6",
		MembershipDigest: "sha256:bf5cf59e356652253268c604cbf8df8cfdb03a4a0d32b27ad158e581709c80e4",
		InvocationDigest: "sha256:60bbdfe432754c3b18c1f8fe5707acf7a6829bea0e58ccf7f36e26581c166a70",
		ScriptDigest:     "sha256:21a0270b7f66a1e4c25933f13a1e5a1bbb4757578072930c8189131f9c6aaae1",
		MatrixDigest:     "sha256:6e00cd562cc2d88e238dfb81d9439de7ec843ee9d0c9879d549cb1436786f975",
		PlannerResult:    "planner verified",
		RendererResult:   "renderer verified",
		Sidecars:         map[string]Digest{"pcheck-1": "sha256:abc9ace5894f0abfb7ae98724e5f744d4d561446558561f245be0e51a946c0fd"},
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
			r.Algorithms.FullPlan.Implementation = "sha256:7ccd6495d9ee5e335a1c1f73d43fbab43864a6dd829f2fb5f815f4a86fe3f993"
		}, "full-plan digest algorithm mismatch"},
		{"a different full-plan canonicaliser", func(r *Stage2Receipt) {
			r.Algorithms.FullPlan.Canonicalizer = "some-other-canonicaliser"
		}, "full-plan digest algorithm mismatch"},
		{"a different semantic-plan implementation", func(r *Stage2Receipt) {
			r.Algorithms.SemanticPlan.Implementation = "sha256:6d531731d0247c0ce4bb626a964f79efc720a4532cbe4c1836ad6a677930908e"
		}, "semantic-plan digest algorithm mismatch"},
		{"a different input-access digest", func(r *Stage2Receipt) {
			r.InputAccess = []InputAccess{
				{Field: "discovery[0]", Digest: "sha256:fa44132b238e67958fb17d33a71d325221805079909c3a7f5bed1a03666cf834"},
				{Field: "store", Digest: "sha256:20bb81051c2a6bd3e94a172683397f9a30037691e1e2f52e4d52cfee35a840d0"},
			}
		}, "input-access record 1 mismatch"},
		{"a different input-access field", func(r *Stage2Receipt) {
			r.InputAccess = []InputAccess{
				{Field: "discovery[0]", Digest: "sha256:fa44132b238e67958fb17d33a71d325221805079909c3a7f5bed1a03666cf834"},
				{Field: "some-other-input", Digest: "sha256:824d80d71985f082a26997a8db88b5d1dd45b777d73585d03d236303e21bde97"},
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
		{"a different plan digest", func(r *Stage2Receipt) {
			r.PlanDigest = "sha256:7b1b763ee8f62eb88e4742a760f912d0b19bcd58b2b948999784bacc15a7f4d7"
		}, "full plan document"},
		{"a different matrix digest", func(r *Stage2Receipt) {
			r.MatrixDigest = "sha256:7b1b763ee8f62eb88e4742a760f912d0b19bcd58b2b948999784bacc15a7f4d7"
		}, "matrix"},
		{"a different Stage-1 parent", func(r *Stage2Receipt) {
			r.Stage1Digest = "sha256:7b1b763ee8f62eb88e4742a760f912d0b19bcd58b2b948999784bacc15a7f4d7"
		}, "stage-1 parent"},
		{"a missing sidecar", func(r *Stage2Receipt) { r.Sidecars = nil }, "derived-document binding mismatch"},
		{"a different sidecar", func(r *Stage2Receipt) {
			r.Sidecars = map[string]Digest{"pcheck-1": "sha256:7b1b763ee8f62eb88e4742a760f912d0b19bcd58b2b948999784bacc15a7f4d7"}
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
// It carries what the schema now requires and no longer what a fixture could
// simply assert: the CANONICAL key (the hash of the two parents, which any
// checker recomputes) and the campaign authority's real signature over the
// store identity. A hand-authored `Durable: true` beside `Key: "fixture"` used
// to pass, which meant Stage 2 could not tell an earned claim from a declared
// one.
func fixtureClaim(stage1, bundle Digest) *PlannerClaimReceipt {
	const store = "authority/durable-claims"
	subject := PlannerClaimStoreSubject(store)
	return &PlannerClaimReceipt{
		Store: store, Durable: true,
		Key: PlannerClaimKey(stage1, bundle), Stage1: stage1, Bundle: bundle,
		Attestation:   SignApproval(CampaignAuthority, fixtureAuthorityKey, subject),
		AuthorityKeys: []string{PublicKeyOf(fixtureAuthorityKey)},
	}
}

// fixtureAuthorityKey stands in for the protected campaign authority. It is
// minted once so every fixture claim verifies against the same predeclared
// key, exactly as a real deployment's would.
var fixtureAuthorityKey = mustSigningKey()
