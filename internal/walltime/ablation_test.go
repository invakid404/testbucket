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

// TestAnAblationManifestIsValidatedNotJustSigned is the F5 regression.
//
// checkAblationRow verified the authority signature and nothing else, while
// loadArm validates an arm's manifest first. So a document the campaign
// authority genuinely signed but which is not a well-formed Stage-1 manifest
// at all — the wrong kind, no reviewed actions — satisfied the mandatory
// pre-campaign prerequisite. A signature says who wrote a document; it does
// not make the document mean anything.
func TestAnAblationManifestIsValidatedNotJustSigned(t *testing.T) {
	idx, loader, keys, authorityKey := campaignFixture(t)
	genuine := loader.manifests[idx.Ablations[0].Stage1Path]
	bad := *genuine
	bad.Kind = "not-a-stage1-manifest"
	bad.Actions = nil
	bad.Signature = nil
	if err := bad.Sign(CampaignAuthority, authorityKey); err != nil {
		t.Fatal(err)
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("the control needs a manifest Validate rejects")
	}
	// Genuinely signed by the campaign authority: the signature is not the
	// thing that is wrong.
	badDigest, err := bad.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	if err := bad.RequireApproval(keys, CampaignAuthority); err != nil {
		t.Fatalf("the control needs the authority approval to pass: %v", err)
	}
	loader.manifests["invalid-ablation-stage1.json"] = &bad
	idx.Ablations[0].Stage1Path = "invalid-ablation-stage1.json"
	resign(loader.verdicts[idx.Ablations[0].VerdictPath], testVerdictAuthority, func(v *Verdict) {
		v.Run.Stage1 = badDigest
		v.VerifierBinary = bad.Instrumentation.VerifierBinary
	})
	_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
	if len(problems) == 0 {
		t.Fatal("a signed but structurally invalid Stage-1 satisfied the ablation prerequisite")
	}
	if !strings.Contains(strings.Join(problems, "\n"), "ablation 0") {
		t.Errorf("problems %v do not attribute the failure to the ablation", problems)
	}
}

// TestTheAblationStratumComesFromTheSignedManifest is the other half of F5.
//
// The stratum lived only in the unsigned campaign index, so swapping two
// labels changed no manifest, verdict, records digest or signature — and the
// rule requiring three in each of four strata counted the relabelled pair
// exactly as before. The authority decides which experiment a run belongs to,
// before the run.
func TestTheAblationStratumComesFromTheSignedManifest(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	if idx.Ablations[0].Stratum == idx.Ablations[3].Stratum {
		t.Fatal("the fixture's selected ablations are in the same stratum")
	}
	idx.Ablations[0].Stratum, idx.Ablations[3].Stratum = idx.Ablations[3].Stratum, idx.Ablations[0].Stratum
	_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "its authority-signed manifest authorises stratum") {
		t.Errorf("an unsigned stratum relabel was accepted: %v", problems)
	}

	// And a manifest that declares no stratum at all cannot authorise one.
	// It is re-signed and the verdict rebound to its new digest, so what this
	// case isolates is the silent stratum rather than a broken signature or an
	// unbound Stage-1, both of which have their own cases.
	idx, loader, keys, authorityKey := campaignFixture(t)
	silent := *loader.manifests[idx.Ablations[0].Stage1Path]
	silent.AblationStratum = ""
	silent.Signature = nil
	if err := silent.Sign(CampaignAuthority, authorityKey); err != nil {
		t.Fatal(err)
	}
	silentDigest, err := silent.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	loader.manifests[idx.Ablations[0].Stage1Path] = &silent
	resign(loader.verdicts[idx.Ablations[0].VerdictPath], testVerdictAuthority, func(v *Verdict) {
		v.Run.Stage1 = silentDigest
	})
	_, problems = LoadCampaign(idx, loader, keys, CampaignAuthority)
	if !strings.Contains(strings.Join(problems, "\n"), "declares no ablation stratum") {
		t.Errorf("an ablation whose manifest names no stratum was accepted: %v", problems)
	}
}

// TestAnAblationMustProveItsRealizedTopology is the F5 regression.
//
// A signed stratum in the Stage-1 manifest states an INTENT. The gate believed
// it, so twelve rows built by copying one baseline manifest, changing only that
// label, re-signing and pairing a generic eligible verdict passed the whole
// mandatory prerequisite — no derived plan, no invocation timings, no
// peer/trace reconciliation, nothing showing twelve experiments had happened
// rather than one label written twelve times.
func TestAnAblationMustProveItsRealizedTopology(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*CampaignIndex, memoryLoader)
		want string
	}{
		{"no derived plan at all", func(idx *CampaignIndex, _ memoryLoader) {
			idx.Ablations[0].Stage2Path = ""
		}, "names no Stage-2 receipt"},

		{"a plan the row did not measure", func(idx *CampaignIndex, l memoryLoader) {
			resign(l.verdicts[idx.Ablations[0].VerdictPath], testVerdictAuthority, func(v *Verdict) {
				v.Run.Stage2 = "sha256:some-other-plan"
			})
		}, "but the receipt it names digests to"},

		{"a plan derived from another Stage-1", func(idx *CampaignIndex, l memoryLoader) {
			r := *l.stage2[idx.Ablations[0].Stage2Path]
			r.Stage1Digest = "sha256:elsewhere"
			l.stage2[idx.Ablations[0].Stage2Path] = &r
		}, "not the"},

		// A reconciliation entry exists only where a peer AND a trace both
		// bracketed the same lifecycle. Its absence means the row was not
		// trace-qualified, whatever its label says.
		{"no peer/trace reconciliation", func(idx *CampaignIndex, l memoryLoader) {
			resign(l.verdicts[idx.Ablations[0].VerdictPath], testVerdictAuthority, func(v *Verdict) {
				v.Recon = nil
			})
		}, "retains no peer/trace reconciliation"},

		{"no invocation observations", func(idx *CampaignIndex, l memoryLoader) {
			resign(l.verdicts[idx.Ablations[0].VerdictPath], testVerdictAuthority, func(v *Verdict) {
				v.InvocationNs = nil
			})
		}, "realized invocation topology is unstated"},

		// The one stratum shape retained evidence can decide: a single
		// invocation cannot realize a sequential-invocation experiment.
		{"a sequential stratum realized by one invocation", func(idx *CampaignIndex, l memoryLoader) {
			for i, a := range idx.Ablations {
				if a.Stratum != StratumSequentialInvocs {
					continue
				}
				resign(l.verdicts[idx.Ablations[i].VerdictPath], testVerdictAuthority, func(v *Verdict) {
					v.InvocationNs = []int64{60 * second}
				})
				return
			}
		}, "that topology is not realized by one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, loader, keys, _ := campaignFixture(t)
			tc.edit(&idx, loader)
			_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Errorf("problems %v do not report %q", problems, tc.want)
			}
		})
	}
}

// TestTwelveLabelOnlyCopiesDoNotQualify is the shape the defect had: one
// baseline manifest copied twelve times with only the stratum changed, and
// generic verdicts carrying no derived topology at all.
func TestTwelveLabelOnlyCopiesDoNotQualify(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	for _, a := range idx.Ablations {
		resign(loader.verdicts[a.VerdictPath], testVerdictAuthority, func(v *Verdict) {
			v.Run.Stage2 = ""
			v.Recon = nil
			v.InvocationNs = nil
		})
	}
	_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
	if len(problems) == 0 {
		t.Fatal("twelve signed stratum labels with no derived topology satisfied the prerequisite")
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"retains no peer/trace reconciliation", "realized invocation topology is unstated"} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems do not report %q: %v", want, problems)
		}
	}
}

// TestTheFourStrataMustRealizeDistinctPlans is the F6 repair.
//
// Binding each ablation to a Stage-2 receipt proves it derived a plan; it does
// not prove the four strata are four experiments. Three strata handed ONE
// generic receipt shape — the same atom, membership, topology and invocation
// digests, differing only in the signed Stage-1 label above them — satisfied
// every per-row check while realizing one topology three times.
func TestTheFourStrataMustRealizeDistinctPlans(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)

	// The control's own shape: give three non-sequential strata one generic
	// receipt, keeping each signed Stage-1 parent and label.
	var generic Stage2Receipt
	for _, a := range idx.Ablations {
		if a.Stratum == StratumWholeFileMultiFile {
			generic = *loader.stage2[a.Stage2Path]
			break
		}
	}
	seen := map[string]bool{}
	for _, a := range idx.Ablations {
		if a.Stratum == StratumSequentialInvocs || seen[a.Stratum] {
			continue
		}
		seen[a.Stratum] = true
		stage1, err := loader.manifests[a.Stage1Path].DigestOf()
		if err != nil {
			t.Fatal(err)
		}
		copyOfGeneric := generic
		copyOfGeneric.Stage1Digest = stage1
		loader.stage2[a.Stage2Path] = &copyOfGeneric
		stage2, err := copyOfGeneric.DigestOf()
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range idx.Ablations {
			if b.Stratum != a.Stratum {
				continue
			}
			loader.stage2[b.Stage2Path] = &copyOfGeneric
			resign(loader.verdicts[b.VerdictPath], testVerdictAuthority, func(v *Verdict) {
				v.Run.Stage2 = stage2
			})
		}
	}
	if len(seen) != 3 {
		t.Fatalf("the generic shape covered %d non-sequential strata, want 3", len(seen))
	}

	_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "one experiment wearing two stratum labels") {
		t.Errorf("three strata realizing one plan were accepted: %v", problems)
	}
}

// TestRepetitionsWithinOneStratumAreNotADuplicate is the positive control: the
// three runs of a single stratum ARE the same experiment repeated, so an
// identical plan there says nothing and must not be refused.
func TestRepetitionsWithinOneStratumAreNotADuplicate(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
	for _, p := range problems {
		if strings.Contains(p, "one experiment wearing two stratum labels") {
			t.Errorf("the fixture's within-stratum repetitions were reported as duplicates: %s", p)
		}
	}
}

// TestAnAblationMustExhibitItsStratumInItsOwnPlan is the F5 regression.
//
// The gate held four digest STRINGS and compared them for inequality. Four
// distinct opaque hashes prove inequality and nothing else, so four arbitrary
// tuples standing for one generic topology satisfied the whole mandatory
// prerequisite. Whether a collision atom or a legal slice was ever exercised
// is a property of the derived plan, and the only way to know it is to read
// the plan.
func TestAnAblationMustExhibitItsStratumInItsOwnPlan(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stratum string
		edit    func(*AblationDerived)
		want    string
	}{
		{"a collision stratum with no colliding atom", StratumCollisionAtom, func(d *AblationDerived) {
			d.Atoms = map[string][]string{"suite/a.spec.ts::solo": {"pkg-a"}}
		}, "no atom holds more than one package"},

		{"a slice stratum with no slice", StratumLegalNonAtomSlice, func(d *AblationDerived) {
			d.Membership = map[string][]string{"bucket-0/inv-0": {"u1", "u2"}}
		}, "no legal non-atom slice was exercised"},

		{"a sequential stratum with one invocation", StratumSequentialInvocs, func(d *AblationDerived) {
			d.Membership = map[string][]string{"bucket-0/inv-0": {"u1"}}
		}, "no sequence of them was exercised"},

		{"a multi-file stratum covering one unit", StratumWholeFileMultiFile, func(d *AblationDerived) {
			d.Membership = map[string][]string{"bucket-0/inv-0": {"u1"}}
		}, "no multi-file whole-file topology was exercised"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, loader, keys, _ := campaignFixture(t)
			for i, a := range idx.Ablations {
				if a.Stratum != tc.stratum {
					continue
				}
				// Edited COHERENTLY: the receipt binds the digests of the
				// edited documents, so the case exercises the stratum's shape
				// rather than tripping the rederivation check on its way.
				d := *loader.derived[a.DerivedPath]
				tc.edit(&d)
				loader.derived[a.DerivedPath] = &d
				atoms, topo, membership, err := d.Digests()
				if err != nil {
					t.Fatal(err)
				}
				r := *loader.stage2[a.Stage2Path]
				r.AtomDigest, r.TopologyDigest, r.MembershipDigest = atoms, topo, membership
				loader.stage2[a.Stage2Path] = &r
				stage2, err := r.DigestOf()
				if err != nil {
					t.Fatal(err)
				}
				resign(loader.verdicts[idx.Ablations[i].VerdictPath], testVerdictAuthority, func(v *Verdict) {
					v.Run.Stage2 = stage2
				})
			}
			_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Errorf("problems %v do not report %q", problems, tc.want)
			}
		})
	}
}

// TestTheDerivedProjectionsMustBeTheOnesTheReceiptBound: supplying documents
// that are not the ones the Stage-2 receipt claims would let any plan stand in
// for any other.
func TestTheDerivedProjectionsMustBeTheOnesTheReceiptBound(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	a := idx.Ablations[0]
	d := *loader.derived[a.DerivedPath]
	d.Topology = map[string][]string{"bucket-0": {"substituted"}}
	loader.derived[a.DerivedPath] = &d
	_, problems := LoadCampaign(idx, loader, keys, CampaignAuthority)
	if !strings.Contains(strings.Join(problems, "\n"), "but the Stage-2 receipt it ran binds") {
		t.Errorf("substituted projections were accepted: %v", problems)
	}

	// And an ablation naming none at all is a label over opaque digests.
	idx, loader, keys, _ = campaignFixture(t)
	idx.Ablations[0].DerivedPath = ""
	_, problems = LoadCampaign(idx, loader, keys, CampaignAuthority)
	if !strings.Contains(strings.Join(problems, "\n"), "names no derived atom/topology/membership projections") {
		t.Errorf("an ablation with no derived plan was accepted: %v", problems)
	}
}
