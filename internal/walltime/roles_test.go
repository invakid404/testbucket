package walltime

import (
	"strings"
	"testing"
)

// TestASignatureCannotBeRelabelledAfterSigning is the F4 regression.
//
// `Signature.Authority` sits outside every document's `DigestOf` — the whole
// Signature struct is nil'd before hashing — and the signature covered the
// digest alone. A valid approval from an unprotected context could therefore
// be relabelled `ewj2-campaign` after the fact, and every later check that
// compared the protected authority name was reading a field its own signature
// did not cover.
func TestASignatureCannotBeRelabelledAfterSigning(t *testing.T) {
	reg := testRegistry()
	regDigest, err := reg.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	m := testManifest(testBundle(), regDigest)
	key := mustSigningKey()
	if err := m.Sign("some-unprotected-context", key); err != nil {
		t.Fatal(err)
	}
	// The genuine label verifies.
	if err := m.RequireApproval([]string{PublicKeyOf(key)}, "some-unprotected-context"); err != nil {
		t.Fatalf("the genuine approval does not verify: %v", err)
	}
	// Relabelled, with the same key, digest and signature bytes, it does not.
	m.Signature.Authority = "ewj2-campaign"
	err = m.RequireApproval([]string{PublicKeyOf(key)}, "ewj2-campaign")
	if err == nil {
		t.Fatal("a signature relabelled after signing as the protected campaign authority verified")
	}
	if !strings.Contains(err.Error(), "does not verify") {
		t.Errorf("error %q does not report a failed signature", err)
	}
}

// TestSignedDocumentsRefuseUnknownAndTrailingContent is the F5/F6 regression.
//
// These documents are checked by recomputing a canonical digest over the
// DECODED value. Anything the decoder silently drops is therefore outside the
// digest, outside the signature, and invisible to every check downstream.
func TestSignedDocumentsRefuseUnknownAndTrailingContent(t *testing.T) {
	type doc struct {
		Kind string `json:"kind"`
	}
	for _, tc := range []struct {
		name, body, want string
	}{
		{"an unknown field", `{"kind":"x","unsigned_security_extension":true}`, "unknown field"},
		{"a second document", `{"kind":"x"}` + "\n" + `{"campaign_id":"unsigned-suffix"}`, "second JSON value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var d doc
			err := DecodeStrictJSON([]byte(tc.body), &d)
			if err == nil {
				t.Fatalf("strict decoding accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
	// The genuine single document still decodes.
	var d doc
	if err := DecodeStrictJSON([]byte(`{"kind":"x"}`), &d); err != nil {
		t.Errorf("a well-formed document was refused: %v", err)
	}
}

// TestTrainingEvidenceRefusesAnUnsignedSuffix is the sharper half of F6.
//
// The label's ReceiptHash addresses the EXACT BYTES of its evidence, so a
// second JSON value appended to them changes the hash the sealed set admits
// while the inner signature still covers only the first value. The suffix is
// then inside the evidence and outside everything that signed it.
func TestTrainingEvidenceRefusesAnUnsignedSuffix(t *testing.T) {
	l := evidenceLabel("suffix-control", 2*second,
		Feature{Name: "runnable_count", Value: 1, Provenance: ProvRunnableSnapshot},
		Feature{Name: "atom_size", Value: 1, Provenance: ProvPreplanAtom},
	)
	if problems := l.VerifyEvidence(testEvidenceSigners()); len(problems) > 0 {
		t.Fatalf("the unedited label does not verify: %v", problems)
	}
	l.Evidence.ReceiptBytes = append(l.Evidence.ReceiptBytes, []byte("\n{\"campaign_id\":\"unsigned-suffix\"}\n")...)
	l.ReceiptHash = DigestBytes(l.Evidence.ReceiptBytes)
	if problems := l.VerifyEvidence(testEvidenceSigners()); len(problems) == 0 {
		t.Fatal("evidence whose exact receipt bytes carry an unsigned trailing JSON value verified")
	}
}

// TestACallerCannotEnlargeTheStage1SignerSet is the F7 regression.
//
// `record-signer` is a caller input all the way down through the CLI, the
// composite action and the reusable workflow, and it used to be UNIONED with
// the authority-signed Stage-1 signer set. A caller could therefore authorise
// the roster and seal key that Stage 1 never declared, while supplying the
// matching run-key secret itself — the measured work choosing who attests it.
func TestACallerCannotEnlargeTheStage1SignerSet(t *testing.T) {
	dir := t.TempDir()
	s := newSynthRun(dir)
	s.write(t, nil)
	declared := []string{PublicKeyOf(mustSigningKey())}

	control := &Verdict{Run: s.run()}
	verifySignerSet(control, VerifyOptions{Dir: dir}, declared, nil)
	if len(control.Findings) == 0 {
		t.Fatal("the control did not reject a roster and seal signed by a key Stage 1 never declared")
	}

	// The same run, with the caller supplying exactly the key Stage 1 omitted.
	got := &Verdict{Run: s.run()}
	verifySignerSet(got, VerifyOptions{Dir: dir, SignerKeys: []string{s.RunSigner()}}, declared, nil)
	if len(got.Findings) == 0 {
		t.Fatal("caller-supplied verifier options enlarged the authority-signed Stage-1 signer set")
	}

	// And where Stage 1 declares nothing there is no authority to defer to, so
	// a caller-supplied set is still the only thing available.
	unbound := &Verdict{Run: s.run()}
	verifySignerSet(unbound, VerifyOptions{Dir: dir, SignerKeys: []string{s.RunSigner()}}, nil, nil)
	for _, f := range unbound.Findings {
		if strings.Contains(f.Detail, "no run-key signer was predeclared") {
			t.Error("an unbound run rejected its own caller-supplied signer set")
		}
	}
}

// TestAVerdictKeyCannotApproveStage1 is the F8 regression.
//
// One key set served both the campaign authority that approves Stage-1 inputs
// and the verifier that signs verdicts, so a verdict-signing key necessarily
// also carried Stage-1 authority: the party deciding whether a row is eligible
// could approve the inputs it was judging.
func TestAVerdictKeyCannotApproveStage1(t *testing.T) {
	idx, loader, authorityKeys, _ := campaignFixture(t)
	// The control: verdicts signed by the declared verdict signer, manifests
	// by the campaign authority. Two roles, two keys, and it passes.
	if _, problems := EvaluateCampaignIndex(idx, loader, authorityKeys, "ewj2-campaign", testRelease()); len(problems) != 0 {
		t.Fatalf("the two-party control failed: %v", problems)
	}

	// Now re-sign a Stage-1 manifest with the VERDICT key and offer that key
	// as an authority key too. It must not approve inputs.
	idx, loader, authorityKeys, _ = campaignFixture(t)
	if err := loader.manifests["candidate.json"].Sign("ewj2-campaign", testVerdictAuthority); err != nil {
		t.Fatal(err)
	}
	keys := append(append([]string(nil), authorityKeys...), PublicKeyOf(testVerdictAuthority))
	_, problems := EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign", testRelease())
	if len(problems) == 0 {
		t.Fatal("a verdict-signing key was accepted as a Stage-1 campaign authority")
	}
}
