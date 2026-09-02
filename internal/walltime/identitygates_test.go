package walltime

import (
	"strings"
	"testing"
)

// TestABlankVerifierIdentityIsNotAnIdentity is the F3 regression.
//
// runIdentityDiff answers "do these records agree about who verified this
// row". A row whose every record, roster and seal repeats an EMPTY verifier
// agrees with itself perfectly, so it passed and was scorable: nothing named
// who verified it, and nothing asked. Attribution is a property of the value,
// not of the agreement — an empty string is a wildcard, and a wildcard matched
// every check downstream.
func TestABlankVerifierIdentityIsNotAnIdentity(t *testing.T) {
	for _, blank := range []string{"", "   "} {
		v := verifySynth(t, func(_ Level, _ int, _ Producer, _ string, r *Record) {
			r.Run.VerifierID = blank
		}, func(s *synthRun) {
			// The sidecars agree with the records, which is precisely the
			// shape that used to pass: everything consistent, nothing named.
			s.rosterRun = func(id *RunIdentity) { id.VerifierID = blank }
			s.sealRun = func(id *RunIdentity) { id.VerifierID = blank }
		})
		if len(findingsMentioning(v, "WT-026", "names no verifier identity")) == 0 {
			t.Errorf("a row whose records all agree about having no verifier (%q) raised no WT-026", blank)
		}
		if v.Eligible {
			t.Errorf("a row attributable to nobody (verifier %q) scored", blank)
		}
	}
}

// TestABlankRecordVerifierIsNotAttestedByAReplay is the other half of F3: the
// replay attestation joins its own verifier identity to the one the MEASURED
// RECORDS carry, and comparing two blanks is agreement about nothing.
func TestABlankRecordVerifierIsNotAttestedByAReplay(t *testing.T) {
	// The attestation names a real party, so what is under test is the blank
	// on the RECORD side alone. Blanking the attestation too would trip its
	// own "names no verifier" check and pass this test against unfixed code.
	a := ReplayAttestation{Kind: ReplayKind, VerifierID: "ewj2-verifier"}
	for _, blank := range []string{"", "   "} {
		problems := a.Verify(Stage2Receipt{}, "", "", InstrumentationIdentity{}, blank)
		joined := strings.Join(problems, "\n")
		if !strings.Contains(joined, "measured records name no verifier identity") {
			t.Errorf("an attestation checked against record verifier %q reported %v; a blank record identity is not something an attestation can be equivalent to",
				blank, problems)
		}
	}
}

// TestTheExpectedAuthorityIsRequiredNotOptional is the F5 regression for the
// public verifier path.
//
// The label used to be compared only when a caller supplied one, which made
// omitting it a WILDCARD: the key check still ran, but a key signs under
// whatever label it is given, so a correctly keyed manifest approved under
// some other protected environment was accepted whenever the caller said
// nothing. The frozen contract names exactly one environment that may approve
// inputs; "the caller did not say" is not that environment.
func TestTheExpectedAuthorityIsRequiredNotOptional(t *testing.T) {
	for _, authority := range []string{"", "   "} {
		dir := t.TempDir()
		s := newSynthRun(dir + "/records")
		docs := writeFrozenDocs(t, dir, s)
		s.stage2 = docs.digest
		s.write(t, nil)
		v, err := VerifyDir(VerifyOptions{
			Dir: s.dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
			RegistryPath: docs.registry, AetaPath: docs.aeta, PcheckPath: docs.pcheck,
			ScorerPath: docs.scorer, TrainingSetPath: docs.trainingSet, Audit: docs.audit,
			ReplayPath: docs.replay, InvocationsPath: docs.invocations,
			StepAttemptPath: docs.stepAttempt,
			AuthorityKeys:   []string{docs.authority},
			Authority:       authority,
		})
		if err != nil {
			t.Fatal(err)
		}
		if v.Eligible {
			t.Errorf("a row verified with no expected authority (%q) scored; any protected environment's approval would do", authority)
		}
		// The EXACT refusal. Other gates mention "authority" for their own
		// reasons, so a looser assertion would pass against a verifier that
		// still treats an unnamed environment as a wildcard.
		if len(findingsMentioning(v, "WT-018", "no expected protected authority was named")) == 0 {
			t.Errorf("nothing in the verdict says the expected protected environment was never stated: %+v", v.Findings)
		}
	}
}

// TestACampaignRefusesAnUnstatedExpectedAuthority is F5 in the campaign path.
// It loads the same manifests through the same rule, so the wildcard has to be
// closed in both or a campaign launders what the verifier refuses.
func TestACampaignRefusesAnUnstatedExpectedAuthority(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	for _, authority := range []string{"", "  "} {
		_, problems := LoadCampaign(idx, loader, keys, authority)
		if len(problems) == 0 {
			t.Fatalf("a campaign loaded with no expected authority (%q) reported no problem", authority)
		}
		joined := strings.Join(problems, "\n")
		if !strings.Contains(joined, "no expected protected authority") {
			t.Errorf("problems %v do not say the expected protected environment was never stated", problems)
		}
	}
}

// TestACampaignRefusesAVerdictSignedByAnotherIdentity is the F6 regression.
//
// A verdict's signature covers `authority NUL digest`, and the authority it
// retains is the party that signed it. The campaign verified the signature
// against the declared verdict keys and never compared that party with the
// delivery verifier the verdict's own BODY names — so a declared key could
// sign a verdict attributing the row to somebody else entirely, and the
// population counted it under a verifier identity that never verified it.
func TestACampaignRefusesAVerdictSignedByAnotherIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Verdict)
		want string
	}{
		{"a body naming a verifier the signature was not made under",
			func(v *Verdict) { v.Run.VerifierID = "some-other-verifier" },
			"delivery verifier"},
		{"a body naming no verifier at all",
			func(v *Verdict) { v.Run.VerifierID = "" },
			"no delivery verifier"},
		{"a body naming a blank verifier",
			func(v *Verdict) { v.Run.VerifierID = "   " },
			"no delivery verifier"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, loader, keys, _ := campaignFixture(t)
			// Re-signed under the fixture's genuine verdict identity, so what
			// this case isolates is the DISAGREEMENT between the signer and
			// the body — not a broken signature, which has its own case.
			resign(loader.verdicts["candidate-0-0.json"], testVerdictAuthority, tc.edit)
			_, problems := LoadCampaign(idx, loader, keys, "ewj2-campaign")
			joined := strings.Join(problems, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("problems %v do not report %q", problems, tc.want)
			}
		})
	}
}
