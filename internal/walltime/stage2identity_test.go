package walltime

import (
	"strings"
	"testing"
)

// THE NESTED IDENTITIES ARE VALIDATED, NOT CARRIED.
//
// `Stage2Receipt.Validate` checked its own top-level digests and accepted
// everything nested inside it as written: the Stage-1 approval the planner
// says it saw, each input-access digest, each sidecar digest and both
// algorithm implementations. A receipt could therefore name an approval with
// no authority, an input access whose "digest" was a sentence, or an algorithm
// implemented by "testbucket" — a LABEL any build may claim — and validate,
// after which every downstream comparison compared malformed identities to
// each other and agreed.
//
// Each case below removes exactly one nested identity from an otherwise valid
// receipt.
func TestEveryNestedIdentityInAStage2ReceiptIsValidated(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Stage2Receipt)
		want string
	}{
		{
			name: "the approval the planner saw",
			edit: func(r *Stage2Receipt) { r.Stage1Approval = Stage1Approval{} },
			want: "records no well-formed Stage-1 approval",
		},
		{
			name: "an approval signed by nothing",
			edit: func(r *Stage2Receipt) { r.Stage1Approval.SignatureDigest = "approved" },
			want: "records no well-formed Stage-1 approval",
		},
		{
			name: "an input access with no field",
			edit: func(r *Stage2Receipt) { r.InputAccess[0].Field = "" },
			want: "input-access record",
		},
		{
			name: "an input access whose digest is prose",
			edit: func(r *Stage2Receipt) { r.InputAccess[1].Digest = "the store as it was" },
			want: "input-access record",
		},
		{
			name: "a sidecar digest that is not a digest",
			edit: func(r *Stage2Receipt) { r.Sidecars["pcheck-1"] = "sha256:short" },
			want: "binds derived document",
		},
		{
			name: "an algorithm implemented by a label",
			edit: func(r *Stage2Receipt) { r.Algorithms.FullPlan.Implementation = "testbucket" },
			want: "implementation",
		},
		{
			name: "a semantic algorithm implemented by a label",
			edit: func(r *Stage2Receipt) { r.Algorithms.SemanticPlan.Implementation = "testbucket" },
			want: "implementation",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := claimed(matchableReceipt())
			if err := r.Validate(); err != nil {
				t.Fatalf("the fixture receipt is not valid to begin with: %v", err)
			}
			tc.edit(&r)
			err := r.Validate()
			if err == nil {
				t.Fatalf("a receipt whose %s is malformed validated; every comparison downstream would compare it to another malformed identity", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not name what is wrong (want %q): %v", tc.want, err)
			}
		})
	}
}

// A CLAIM MAY NOT SUPPLY THE AUTHORITY THAT VOUCHES FOR IT.
//
// The planner claim carried both the attestation and the key set the
// attestation was checked against, so any self-generated key vouched for any
// claim: the store's durability was asserted by the document asserting it.
// The keys now come from the deployment's predeclared registration, and the
// claim's own list may only be a subset of it.
func TestAPlannerClaimIsCheckedAgainstPredeclaredAuthorityKeys(t *testing.T) {
	r := claimed(matchableReceipt())
	if err := r.Validate(); err != nil {
		t.Fatalf("the fixture receipt is not valid to begin with: %v", err)
	}

	t.Run("a key the deployment never declared", func(t *testing.T) {
		rogue := mustSigningKey()
		bad := *r.PlannerClaim
		subject := PlannerClaimStoreSubject(bad.Store)
		bad.Attestation = SignApproval(CampaignAuthority, rogue, subject)
		bad.AuthorityKeys = []string{PublicKeyOf(rogue)}
		with := r
		with.PlannerClaim = &bad
		err := with.Validate()
		if err == nil {
			t.Fatal("a claim vouched for by a key it generated itself was accepted; the store's durability is then asserted by the document asserting it")
		}
		if !strings.Contains(err.Error(), "not one of the predeclared") {
			t.Errorf("the refusal does not say the key was never declared: %v", err)
		}
	})

	t.Run("no predeclared key set at all", func(t *testing.T) {
		original := trustedCampaignAuthorityKeys
		RegisterCampaignAuthorityKeys(nil)
		t.Cleanup(func() { trustedCampaignAuthorityKeys = original })
		err := r.Validate()
		if err == nil {
			t.Fatal("a claim was accepted with no predeclared authority at all, so whatever signed it authorised it")
		}
		if !strings.Contains(err.Error(), "no predeclared campaign-authority key set is registered") {
			t.Errorf("the refusal does not say the deployment declared nothing: %v", err)
		}
	})

	t.Run("an attestation that does not verify", func(t *testing.T) {
		bad := *r.PlannerClaim
		bad.Attestation = SignApproval(CampaignAuthority, fixtureAuthorityKey, PlannerClaimStoreSubject("some/other/store"))
		with := r
		with.PlannerClaim = &bad
		if err := with.Validate(); err == nil {
			t.Fatal("an attestation over a different store vouched for this one")
		}
	})

	// AND THE ATTESTATION IS PART OF THE CLAIM'S IDENTITY. Two claims that
	// differ only in who vouched for them are two different claims.
	t.Run("the attestation and keys are part of the identity", func(t *testing.T) {
		a := *r.PlannerClaim
		b := a
		b.Attestation = "a different signature"
		if a.identity() == b.identity() {
			t.Error("a claim's identity ignores its attestation, so a replay comparing identities would accept one vouched for by somebody else")
		}
		c := a
		c.AuthorityKeys = append([]string{"another-key"}, a.AuthorityKeys...)
		if a.identity() == c.identity() {
			t.Error("a claim's identity ignores its authority keys")
		}
	})
}
