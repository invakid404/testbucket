package walltime

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestACampaignWithoutTheTwelveAblationsIsRefused is the F5 regression.
//
// The contract requires twelve controlled physical-envelope /
// containment-peer / trace-qualified ablations — three in each of the four
// topology strata — to precede the campaign. Nothing in the index, the loader,
// the gates, the CLI or the release path represented them, so a fully
// authenticated five-pair population passed every campaign gate while having
// skipped a mandatory experimental prerequisite entirely. The release gate
// would then authorise a delivery on that evidence.
func TestACampaignWithoutTheTwelveAblationsIsRefused(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	idx.Ablations = nil

	// The shape the defect had: nothing in the serialised index even mentions
	// an ablation, so there was nothing for a reader to notice was missing.
	raw, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "ablation") && !strings.Contains(string(raw), `"ablations":null`) {
		t.Logf("index still mentions ablations: %s", raw)
	}

	gates, problems := EvaluateCampaignIndex(idx, loader, keys, CampaignAuthority, testRelease())
	if len(problems) == 0 {
		t.Fatal("a campaign with no pre-campaign ablations reported no problem")
	}
	if !strings.Contains(strings.Join(problems, "\n"), "no pre-campaign ablations") {
		t.Errorf("problems %v do not say the ablations are missing", problems)
	}
	for _, g := range gates {
		if g.Pass {
			t.Errorf("gate %s passed a population with no ablation evidence", g.Name)
		}
	}
}

// TestTheAblationGateRequiresThreeInEachStratum: twelve of one shape is not
// four shapes, and the strata are the point of the requirement.
func TestTheAblationGateRequiresThreeInEachStratum(t *testing.T) {
	if RequiredAblations != AblationsPerStratum*len(AblationStrata) {
		t.Fatalf("RequiredAblations=%d does not match %d per stratum across %d strata",
			RequiredAblations, AblationsPerStratum, len(AblationStrata))
	}
	for _, tc := range []struct {
		name string
		edit func(*CampaignIndex)
		want string
	}{
		{"all twelve in one stratum", func(idx *CampaignIndex) {
			for i := range idx.Ablations {
				idx.Ablations[i].Stratum = StratumCollisionAtom
			}
		}, "requires 3 in each of the four"},

		{"a stratum the contract does not fix", func(idx *CampaignIndex) {
			idx.Ablations[0].Stratum = "a-stratum-somebody-invented"
		}, "not one of the four the contract fixes"},

		{"one ablation short", func(idx *CampaignIndex) {
			idx.Ablations = idx.Ablations[:RequiredAblations-1]
		}, "requires exactly 12"},

		// Twelve references to fewer measurements is fewer measurements.
		{"one verdict counted twice", func(idx *CampaignIndex) {
			idx.Ablations[1].VerdictPath = idx.Ablations[0].VerdictPath
		}, "which another ablation already counted"},

		{"an ablation with no verdict behind it", func(idx *CampaignIndex) {
			idx.Ablations[2].VerdictPath = ""
		}, "names no verdict"},

		{"an ablation no manifest authorised", func(idx *CampaignIndex) {
			idx.Ablations[3].Stage1Path = ""
		}, "names no Stage-1 manifest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, loader, keys, _ := campaignFixture(t)
			tc.edit(&idx)
			_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
			joined := strings.Join(problems, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("problems %v do not report %q", problems, tc.want)
			}
		})
	}
}

// TestAnAblationMustBeAnEligibleAttributableRow: the gate authenticates each
// ablation the way it authenticates an arm. A count satisfied by rows nobody
// could score, or nobody is attributable for, proves only that the counter
// counts.
func TestAnAblationMustBeAnEligibleAttributableRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Verdict)
		want string
	}{
		{"an ineligible row", func(v *Verdict) { v.Eligible = false }, "not an eligible measured row"},
		{"a row naming no verifier", func(v *Verdict) { v.Run.VerifierID = "" }, "names no delivery verifier"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, loader, keys, _ := campaignFixture(t)
			resign(loader.verdicts[idx.Ablations[0].VerdictPath], testVerdictAuthority, tc.edit)
			_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Errorf("problems %v do not report %q", problems, tc.want)
			}
		})
	}
	// An unsigned ablation verdict is attributable to nobody.
	idx, loader, keys, _ := campaignFixture(t)
	loader.verdicts[idx.Ablations[0].VerdictPath].Signature = nil
	_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
	if !strings.Contains(strings.Join(problems, "\n"), "unsigned verdict") {
		t.Errorf("problems %v do not report an unsigned ablation", problems)
	}
}

// TestAnAblationMustPrecedeTheCampaign: an "ablation" run after the pairs it
// is supposed to have informed is a post-hoc measurement wearing the name, and
// the contract says these precede the campaign.
func TestAnAblationMustPrecedeTheCampaign(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	// Move one ablation to the day AFTER the campaign's last pair.
	resign(loader.verdicts[idx.Ablations[0].VerdictPath], testVerdictAuthority, func(v *Verdict) {
		v.StartedAt = "2026-10-01T00:00:00Z"
	})
	_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
	if !strings.Contains(strings.Join(problems, "\n"), "cannot have preceded it") {
		t.Errorf("problems %v do not report an ablation that followed the campaign", problems)
	}
}

// TestTheAblationsAreNeverAggregatedIntoAGate is the other half of the
// contract's rule: the twelve must have run, and their OUTCOME cannot tune the
// campaign's gates. Changing every ablation's measured duration must change no
// gate observation.
func TestTheAblationsAreNeverAggregatedIntoAGate(t *testing.T) {
	before, loader, keys, _ := campaignFixture(t)
	baseline, problems := EvaluateCampaignIndex(before, loader, keys, CampaignAuthority, testRelease())
	if len(problems) != 0 {
		t.Fatalf("the fixture campaign is not authenticated: %v", problems)
	}
	for _, a := range before.Ablations {
		resign(loader.verdicts[a.VerdictPath], testVerdictAuthority, func(v *Verdict) {
			v.ActionNs = 5 * second
		})
	}
	after, problems := EvaluateCampaignIndex(before, loader, keys, CampaignAuthority, testRelease())
	if len(problems) != 0 {
		t.Fatalf("the campaign stopped authenticating after the ablation durations changed: %v", problems)
	}
	if len(baseline) != len(after) {
		t.Fatalf("the gate set changed shape: %d then %d", len(baseline), len(after))
	}
	for i := range baseline {
		if baseline[i].Observed != after[i].Observed || baseline[i].Pass != after[i].Pass {
			t.Errorf("gate %s changed when only ablation outcomes changed: %q/%v -> %q/%v; an ablation's outcome must not tune the campaign's gates",
				baseline[i].Name, baseline[i].Observed, baseline[i].Pass, after[i].Observed, after[i].Pass)
		}
	}
}

// TestAnAblationVerdictIsCryptographicallyAuthenticated is the F5 regression.
//
// The gate checked that a Signature object existed and that its authority
// string equalled the verdict's own declared verifier id — two fields of the
// same unverified document agreeing with each other. It never recomputed the
// digest, never called VerifySigned, and never used the signers the manifest
// declares, so a verdict whose signed body had been edited after signing
// satisfied the mandatory prerequisite while the signature primitive itself
// rejected the document.
func TestAnAblationVerdictIsCryptographicallyAuthenticated(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	v := loader.verdicts[idx.Ablations[0].VerdictPath]
	// Edited AFTER signing, exactly as a tamper is: the signature is still
	// present and still names the right identity.
	v.RecordsDigest = DigestBytes([]byte("tampered ablation records"))

	d, err := v.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySigned(v.Signature, d, testVerdictSigners()); err == nil {
		t.Fatal("the control needs a signature the primitive itself rejects")
	}
	_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
	if !strings.Contains(strings.Join(problems, "\n"), "verdict signature") {
		t.Errorf("problems %v do not report the unverifiable ablation signature", problems)
	}
}

// TestOneAblationCannotStandInForTwelve: copying one genuinely signed
// measurement under twelve pathnames and spreading those names across the four
// strata satisfied a path-only duplicate check exactly as well as twelve
// measurements did. Identity is the measurement, not the filename.
func TestOneAblationCannotStandInForTwelve(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	first := *loader.verdicts[idx.Ablations[0].VerdictPath]
	for i := range idx.Ablations {
		path := fmt.Sprintf("copied-ablation-%d.json", i)
		copyOfFirst := first
		loader.verdicts[path] = &copyOfFirst
		idx.Ablations[i].VerdictPath = path
	}
	_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
	joined := strings.Join(problems, "\n")
	for _, want := range []string{
		"twelve references to one measurement is one measurement",
		"is byte-identical to the verdict",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems do not report %q: %v", want, problems)
		}
	}
}

// TestAnAblationIsBoundToItsOwnDelivery: an authenticated verdict for some
// other run, Stage-1 or verifier build cannot stand in for this ablation.
func TestAnAblationIsBoundToItsOwnDelivery(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Verdict)
		want string
	}{
		{"another Stage-1", func(v *Verdict) { v.Run.Stage1 = "sha256:elsewhere" }, "not the"},
		{"another verifier build", func(v *Verdict) { v.VerifierBinary = "sha256:other-build" }, "not the"},
		{"another run", func(v *Verdict) { v.Run.RunID = "some-other-run" }, "but its verdict was recorded under"},
		{"no records digest", func(v *Verdict) { v.RecordsDigest = "" }, "names no records digest"},
		{"an incomplete measurement", func(v *Verdict) { v.Complete = false }, "not a complete measurement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, loader, keys, _ := campaignFixture(t)
			resign(loader.verdicts[idx.Ablations[0].VerdictPath], testVerdictAuthority, tc.edit)
			_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Errorf("problems %v do not report %q", problems, tc.want)
			}
		})
	}
}
