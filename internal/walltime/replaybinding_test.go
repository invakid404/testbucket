package walltime

import (
	"strings"
	"testing"
)

// TestEverySignedReplayClaimIsChecked is the F1/F2 regression.
//
// `Verify` compared Stage-1, Stage-2, the recomputed receipt and the verifier
// binary, and checked the verifier id for non-emptiness. It never compared the
// attestation's OWN top-level bundle digest, and never joined its verifier
// identity to the signature that carries it or to the row it attests.
//
// Both gaps have the same shape: a field is signed, so it is covered by the
// signature — and then nothing reads it. A signature over a contradiction
// authenticates the contradiction.
func TestEverySignedReplayClaimIsChecked(t *testing.T) {
	const verifierID = "independent-verifier"
	issued := claimed(matchableReceipt())
	issuedDigest, err := issued.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	instr := InstrumentationIdentity{VerifierBinary: "sha256:88c9eae68eb300b2971a2bec9e5a26ff4179fd661d6b7d861e4c6557b9aaee14"}

	good := func() ReplayAttestation {
		return ReplayAttestation{
			Kind:           ReplayKind,
			Stage1Digest:   issued.Stage1Digest,
			Stage2Digest:   issuedDigest,
			BundleDigest:   issued.BundleDigest,
			Recomputed:     claimed(matchableReceipt()),
			VerifierID:     verifierID,
			VerifierBinary: "sha256:88c9eae68eb300b2971a2bec9e5a26ff4179fd661d6b7d861e4c6557b9aaee14",
			Signature:      &Signature{Authority: verifierID},
		}
	}
	// The positive control: a complete, self-consistent attestation verifies.
	if problems := good().Verify(issued, issuedDigest, issued.Stage1Digest, instr, verifierID); len(problems) > 0 {
		t.Fatalf("a genuine attestation was rejected: %v", problems)
	}

	for _, tc := range []struct {
		name             string
		edit             func(*ReplayAttestation)
		recordVerifierID string
		want             string
	}{
		{"a top-level bundle claim its own receipt contradicts", func(a *ReplayAttestation) {
			a.BundleDigest = "sha256:18d9a6c2e32e9c88dd6e3eac86f55326aef52245b016ab455ca9d7e006940366"
		}, verifierID, "while the receipt it recomputed names"},
		{"a top-level bundle claim the issued receipt contradicts", func(a *ReplayAttestation) {
			a.BundleDigest = "sha256:18d9a6c2e32e9c88dd6e3eac86f55326aef52245b016ab455ca9d7e006940366"
			a.Recomputed.BundleDigest = "sha256:18d9a6c2e32e9c88dd6e3eac86f55326aef52245b016ab455ca9d7e006940366"
		}, verifierID, "but the issued receipt was derived from"},
		{"no verifier identity", func(a *ReplayAttestation) {
			a.VerifierID = ""
		}, verifierID, "names no verifier"},
		{"a verifier identity the signature does not carry", func(a *ReplayAttestation) {
			a.VerifierID = "attacker-controlled-unbound-verifier"
		}, "", "must be the identity that signed"},
		{"a verifier identity the measured row was not delivered against", func(a *ReplayAttestation) {
			a.VerifierID = "attacker-controlled-unbound-verifier"
			a.Signature.Authority = "attacker-controlled-unbound-verifier"
		}, verifierID, "the measured records were delivered against"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := good()
			tc.edit(&a)
			problems := a.Verify(issued, issuedDigest, issued.Stage1Digest, instr, tc.recordVerifierID)
			if len(problems) == 0 {
				t.Fatalf("Verify accepted %s", tc.name)
			}
			if !strings.Contains(strings.Join(problems, "; "), tc.want) {
				t.Errorf("no problem mentions %q:\n%s", tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}

// TestAValidlySignedContradictionIsStillRefused: the point of F1 is that these
// documents are SIGNED. A mutation that is re-signed by the admitted replay key
// is not a broken signature — it is an authentic statement of something false,
// and only reading the field catches it.
func TestAValidlySignedContradictionIsStillRefused(t *testing.T) {
	const verifierID = "independent-verifier"
	key := mustSigningKey()
	issued := claimed(matchableReceipt())
	issuedDigest, err := issued.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	a := ReplayAttestation{
		Kind: ReplayKind, Stage1Digest: issued.Stage1Digest, Stage2Digest: issuedDigest,
		BundleDigest: "sha256:32c40e72d203947a6c3104b9868180caee9c2353a587c83af822887d59511ec6",
		Recomputed:   claimed(matchableReceipt()), VerifierID: verifierID,
	}
	d, err := a.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	a.Signature = &Signature{
		Authority: verifierID, KeyID: PublicKeyOf(key), Digest: d,
		Value: SignApproval(verifierID, key, d),
	}
	// The signature is genuine — the admitted key really did sign these exact
	// bytes.
	if err := VerifySigned(a.Signature, d, []string{PublicKeyOf(key)}); err != nil {
		t.Fatalf("the control's signature is not genuine: %v", err)
	}
	// And it is still refused, because the claim it authenticates is false.
	problems := a.Verify(issued, issuedDigest, issued.Stage1Digest, InstrumentationIdentity{}, verifierID)
	if len(problems) == 0 {
		t.Fatal("a validly signed attestation contradicting its own recomputed bundle was accepted")
	}
}

// TestTheFrozenProfileIsEnforcedNotAssumed is the F4 regression.
//
// `FrozenProfileCommit` existed and only a test used it. Validation required
// the caller's repository and commit fields to agree with EACH OTHER and the
// commit to be a well-formed full SHA — so an internally consistent,
// authority-signed manifest for another workload passed every check and
// reached the campaign gates. Consistency proves a manifest describes ONE
// workload; only this proves it describes the one the contract froze.
func TestTheFrozenProfileIsEnforcedNotAssumed(t *testing.T) {
	reg := testRegistry()
	regDigest, err := reg.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	// The positive control: the genuine frozen identity validates.
	if err := testManifest(testBundle(), regDigest).Validate(); err != nil {
		t.Fatalf("the frozen Mandel profile was refused: %v", err)
	}

	const wrongRepo = "attacker/other-consumer"
	wrongCommit := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name string
		edit func(*Stage1Manifest)
		want string
	}{
		{"an internally consistent manifest for another workload", func(m *Stage1Manifest) {
			m.Consumer.Repository, m.Consumer.Commit = wrongRepo, wrongCommit
			m.Consumer.DownstreamRef = "refs/heads/main@" + wrongCommit
			m.SourceProfile.Repository, m.SourceProfile.Commit = wrongRepo, wrongCommit
			m.Bundle.Source.Repository, m.Bundle.Source.Commit = wrongRepo, wrongCommit
		}, "the frozen acceptance contract profiles exactly"},
		{"the right commit in another repository", func(m *Stage1Manifest) {
			m.Consumer.Repository = wrongRepo
			m.SourceProfile.Repository = wrongRepo
			m.Bundle.Source.Repository = wrongRepo
		}, "the frozen acceptance contract profiles exactly"},
		{"the right repository at another commit", func(m *Stage1Manifest) {
			m.Consumer.Commit = wrongCommit
			m.Consumer.DownstreamRef = "refs/heads/main@" + wrongCommit
			m.SourceProfile.Commit = wrongCommit
			m.Bundle.Source.Commit = wrongCommit
		}, "the frozen acceptance contract profiles exactly"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testManifest(testBundle(), regDigest)
			tc.edit(&m)
			err := m.Validate()
			if err == nil {
				t.Fatalf("Validate accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not name the frozen profile rule", err)
			}
			// And a valid authority signature does not rescue it: the
			// refusal is about WHICH workload, not about who approved it.
			authority := mustSigningKey()
			if err := m.Sign(CampaignAuthority, authority); err != nil {
				t.Fatal(err)
			}
			if err := m.Validate(); err == nil {
				t.Fatal("an authority-signed manifest for another workload validated")
			}
		})
	}
}
