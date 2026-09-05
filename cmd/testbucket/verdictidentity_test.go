package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// TestTheProducedVerdictIsSignedByTheIdentityTheCampaignRequires is the F3
// regression, and it deliberately runs the PRODUCER against the CONSUMER.
//
// `wall verify --json` signed the verdict under --authority, which in the
// scored workflow is the protected Stage-1 environment `ewj2-campaign`, while
// the verdict body's verifier identity comes from the measured records and is
// `ewj2-verifier`. LoadCampaign requires the signature authority to equal the
// body's verifier. The documented production path therefore emitted, by
// construction, exactly the verdict production refuses — a defect no test
// could see while the campaign fixtures signed themselves under the identity
// they wanted.
func TestTheProducedVerdictIsSignedByTheIdentityTheCampaignRequires(t *testing.T) {
	const (
		deliveryVerifier = "ewj2-verifier"
		stage1Authority  = walltime.CampaignAuthority
	)
	key, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	v := &walltime.Verdict{Run: walltime.RunIdentity{VerifierID: deliveryVerifier}}

	identity, err := verdictSigningIdentity(v)
	if err != nil {
		t.Fatalf("a verdict naming a delivery verifier could not be signed: %v", err)
	}
	if identity != deliveryVerifier {
		t.Errorf("the verdict would be signed as %q, want the delivery verifier %q", identity, deliveryVerifier)
	}
	if identity == stage1Authority {
		t.Errorf("the verdict would be signed as the Stage-1 authority %q, which the campaign loader refuses against a body naming %q",
			stage1Authority, deliveryVerifier)
	}
	if err := v.Sign(identity, key); err != nil {
		t.Fatal(err)
	}

	// THE CONSUMER'S OWN RULE, applied to what the producer just emitted.
	if v.Signature.Authority != v.Run.VerifierID {
		t.Errorf("the produced verdict carries signature authority %q against body verifier %q; LoadCampaign refuses that pairing",
			v.Signature.Authority, v.Run.VerifierID)
	}
	d, err := v.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	if err := walltime.VerifySigned(v.Signature, d, []string{walltime.PublicKeyOf(key)}); err != nil {
		t.Fatalf("the produced signature does not verify under its own key: %v", err)
	}
}

// TestAVerdictNamingNoVerifierIsNotSigned: signing an unattributable verdict
// under some convenient label is how the wrong identity got there in the first
// place. There is nobody to sign as, and saying so beats inventing one.
func TestAVerdictNamingNoVerifierIsNotSigned(t *testing.T) {
	for _, blank := range []string{"", "   "} {
		v := &walltime.Verdict{Run: walltime.RunIdentity{VerifierID: blank}}
		if _, err := verdictSigningIdentity(v); err == nil {
			t.Errorf("a verdict naming no delivery verifier (%q) was given a signing identity", blank)
		}
	}
}

// TestTheDocumentedScoredIdentitiesStayDistinct pins the premise the defect
// lived in: the scored example deliberately uses a protected Stage-1
// environment that is NOT the delivery verifier. If those two ever became the
// same string the bug would hide itself, so the distinction is asserted rather
// than assumed.
func TestTheDocumentedScoredIdentitiesStayDistinct(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "verifier-id: ewj2-verifier") || !strings.Contains(s, "authority: ewj2-campaign") {
		t.Skip("the documented scored identities changed; this premise no longer applies")
	}
	if walltime.CampaignAuthority == "ewj2-verifier" {
		t.Fatal("the protected authority and the delivery verifier are the same identity, so the campaign's signer rule proves nothing")
	}
}
