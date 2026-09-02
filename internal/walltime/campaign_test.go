package walltime

import (
	"crypto/ed25519"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// memoryLoader serves campaign artifacts from memory, so these tests exercise
// the authentication rules rather than a filesystem.
type memoryLoader struct {
	verdicts  map[string]*Verdict
	manifests map[string]*Stage1Manifest
	stage2    map[string]*Stage2Receipt
	derived   map[string]*AblationDerived
}

func (m memoryLoader) Derived(path string) (*AblationDerived, error) {
	d, ok := m.derived[path]
	if !ok {
		return nil, fmt.Errorf("no derived projections at %s", path)
	}
	c := *d
	return &c, nil
}

func (m memoryLoader) Stage2(path string) (*Stage2Receipt, error) {
	r, ok := m.stage2[path]
	if !ok {
		return nil, fmt.Errorf("no stage-2 receipt at %s", path)
	}
	c := *r
	return &c, nil
}

func (m memoryLoader) Verdict(path string) (*Verdict, error) {
	v, ok := m.verdicts[path]
	if !ok {
		return nil, fmt.Errorf("no verdict at %s", path)
	}
	c := *v
	return &c, nil
}

func (m memoryLoader) Manifest(path string) (*Stage1Manifest, error) {
	v, ok := m.manifests[path]
	if !ok {
		return nil, fmt.Errorf("no manifest at %s", path)
	}
	c := *v
	return &c, nil
}

// candidateBinaryDigest is the exact asset the candidate arm delivers, and
// therefore the one a release cut from this campaign publishes.
var candidateBinaryDigest = DigestBytes([]byte("candidate testbucket binary"))

// testRelease is the delivery the fixture campaign was produced for: the
// candidate arm's reviewed tip and its exact built binary. A campaign
// authorises this delivery and no other.
// testReleaseManifest is a real publish set in the shape goreleaser produces:
// four archives and a checksums file, with the campaign's delivered binary
// living INSIDE one of the archives rather than beside them. That is the
// arrangement a release actually has, and the one a gate has to be able to
// reason about.
func testReleaseManifest() *ReleaseManifest {
	archive := func(os, arch string, binary Digest) ReleaseAsset {
		name := "testbucket_0.3.0_" + os + "_" + arch + ".tar.gz"
		return ReleaseAsset{
			Name: name, Path: "dist/" + name, Digest: DigestBytes([]byte("archive " + name)),
			Contains: []ReleaseAssetMember{
				{Name: "LICENSE", Digest: DigestBytes([]byte("license"))},
				{Name: "README.md", Digest: DigestBytes([]byte("readme"))},
				{Name: "testbucket", Digest: binary},
			},
		}
	}
	return &ReleaseManifest{Kind: ReleaseManifestKind, Assets: []ReleaseAsset{
		archive("darwin", "amd64", DigestBytes([]byte("darwin amd64 binary"))),
		archive("darwin", "arm64", DigestBytes([]byte("darwin arm64 binary"))),
		archive("linux", "amd64", candidateBinaryDigest),
		archive("linux", "arm64", DigestBytes([]byte("linux arm64 binary"))),
		{Name: "checksums.txt", Path: "dist/checksums.txt", Digest: DigestBytes([]byte("checksums"))},
	}}
}

func testRelease() CampaignRelease {
	return CampaignRelease{SHA: testTip, Manifest: testReleaseManifest()}
}

// campaignFixture builds a fully authenticated five-pair campaign: signed
// manifests for both arms, and eight eligible verdicts per run.
func campaignFixture(t *testing.T) (CampaignIndex, memoryLoader, []string, ed25519.PrivateKey) {
	t.Helper()
	reg := testRegistry()
	regDigest, err := reg.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	key, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}

	// The authority freezes WHICH five pairs, which arm is which, and the
	// order and dates they run in — before any of them runs. Both manifests
	// carry the same schedule, and the index below must execute exactly it.
	schedule := testSchedule()
	sign := func(role string, candidateTuple bool) (*Stage1Manifest, Digest) {
		m := testManifest(testBundle(), regDigest)
		m.Role = role
		m.Schedule = schedule
		if candidateTuple {
			// The enumerated permitted difference: a different testbucket
			// source/action/binary, and the wrappers that come with it.
			candidateBinary := candidateBinaryDigest
			m.Source.BinaryDigest = candidateBinary
			// The attestation is part of the delivered tuple: it attests THIS
			// binary, so a candidate arm shipping a different build carries a
			// different signed statement about it.
			m.Source.BuildAttestation = testBuildAttestation(candidateBinary, m.Source.ReviewTip)
			m.Instrumentation.PhysicalBinary = candidateBinary
			m.Instrumentation.PeerBinary = candidateBinary
			m.Instrumentation.TraceBinary = candidateBinary
			m.Instrumentation.VerifierBinary = candidateBinary
		}
		if err := m.Sign("ewj2-campaign", key); err != nil {
			t.Fatal(err)
		}
		d, err := m.DigestOf()
		if err != nil {
			t.Fatal(err)
		}
		return &m, d
	}
	baseline, baselineDigest := sign("baseline", false)
	candidate, candidateDigest := sign("candidate", true)

	loader := memoryLoader{
		verdicts:  map[string]*Verdict{},
		manifests: map[string]*Stage1Manifest{"baseline.json": baseline, "candidate.json": candidate},
		stage2:    map[string]*Stage2Receipt{},
		derived:   map[string]*AblationDerived{},
	}
	order, err := schedule.OrderDigest()
	if err != nil {
		t.Fatal(err)
	}
	idx := CampaignIndex{Kind: CampaignIndexKind, CampaignID: "ewj2", OrderDigest: order}
	for pair := 0; pair < CampaignPairs; pair++ {
		arm := func(role, runID string, stage1, verifier Digest, top, step int64) CampaignArm {
			a := CampaignArm{
				RunID: runID, Terminal: TerminalPassed,
				StartedAt:  scheduledInstant(pair),
				Stage1Path: role + ".json",
			}
			for b := 0; b < BucketsPerRun; b++ {
				path := fmt.Sprintf("%s-%d-%d.json", role, pair, b)
				observed := top - int64(b)*step
				// A verdict a campaign will count: signed by the approved
				// authority, bound to the exact records it describes and to
				// the verifier build Stage 1 approved, and carrying its own
				// gate samples so the population-wide means are computable.
				v := &Verdict{
					Schema: SchemaVersion, Complete: true, Eligible: true,
					ActionNs:       observed,
					RecordsDigest:  Digest(fmt.Sprintf("sha256:records-%s-%d-%d", role, pair, b)),
					VerifierBinary: verifier,
					AetaSample: &AetaSample{
						BucketID: fmt.Sprint(b), PointNs: observed,
						LowerNs: observed - 5*second, UpperNs: observed + 5*second,
						ObservedNs: observed,
					},
					// A row that measured invocations and RETAINS the evidence
					// about them. The fixture used to carry an Aeta sample and
					// nothing else, which is why the Pcheck-versus-observed-V
					// and like-for-like gates could be absent from a campaign
					// this same fixture called fully authenticated.
					InvocationNs:    invocationNs(observed),
					PredictorSample: predictorSamples(b, observed),
					Recon:           reconFor(len(invocationNs(observed))),
					// The row's OWN start and outcome, as a verifier derives
					// them from the action envelope's records. The fixture
					// used to leave both to the unsigned index, which is how a
					// campaign's date, window and retention rules ended up
					// resting on a plain file.
					StartedAt: scheduledInstant(pair),
					Terminal:  TerminalPassed,
					Run: RunIdentity{
						CampaignID: "ewj2", RunID: runID, BucketID: fmt.Sprint(b), Stage1: stage1,
						// The delivery-bound verifier that judged this row.
						// It must be the identity the verdict is signed
						// under, or the string is signed but attributable to
						// nobody.
						VerifierID: testVerdictIdentity,
					},
				}
				// Signed by the VERDICT signer Stage 1 declares, not by the
				// campaign authority. One key doing both would be the party
				// judging a row also approving the inputs it judges.
				if err := v.Sign(testVerdictIdentity, testVerdictAuthority); err != nil {
					t.Fatal(err)
				}
				loader.verdicts[path] = v
				a.VerdictPaths = append(a.VerdictPaths, path)
			}
			return a
		}
		idx.Pairs = append(idx.Pairs, CampaignPairRef{
			Baseline: arm("baseline", fmt.Sprintf("b%d", pair), baselineDigest,
				baseline.Instrumentation.VerifierBinary, 100*second, 5*second),
			Candidate: arm("candidate", fmt.Sprintf("c%d", pair), candidateDigest,
				candidate.Instrumentation.VerifierBinary, 84*second, 0),
		})
	}
	addFixtureAblations(t, &idx, loader, baseline, key)
	return idx, loader, []string{PublicKeyOf(key)}, key
}

// derivedFor is the plan each stratum's experiment actually derives. The four
// are materially different documents, because the four are different
// experiments: a collision run holds an atom with two packages, a slice run
// renders an invocation covering part of a file, a multi-file run schedules
// several files, and a sequential run renders more than one invocation.
func derivedFor(stratum string) AblationDerived {
	switch stratum {
	case StratumCollisionAtom:
		return AblationDerived{
			Atoms:      map[string][]string{"suite/a.spec.ts::collide": {"pkg-a", "pkg-b"}},
			Topology:   map[string][]string{"bucket-0": {"file"}},
			Membership: map[string][]string{"bucket-0/inv-0": {"u1", "u2"}},
		}
	case StratumLegalNonAtomSlice:
		return AblationDerived{
			Atoms:    map[string][]string{"suite/b.spec.ts::solo": {"pkg-c"}},
			Topology: map[string][]string{"bucket-0": {"slice"}},
			Membership: map[string][]string{
				"bucket-0/inv-0": {"u1", "u2", "u3"},
				"bucket-0/inv-1": {"u1"},
			},
		}
	case StratumSequentialInvocs:
		return AblationDerived{
			Atoms:    map[string][]string{"suite/c.spec.ts::one": {"pkg-d"}},
			Topology: map[string][]string{"bucket-0": {"file", "file", "file"}},
			Membership: map[string][]string{
				"bucket-0/inv-0": {"u1"},
				"bucket-0/inv-1": {"u2"},
				"bucket-0/inv-2": {"u3"},
			},
		}
	default: // whole-file multi-file
		return AblationDerived{
			Atoms:    map[string][]string{"suite/d.spec.ts::one": {"pkg-e"}},
			Topology: map[string][]string{"bucket-0": {"file", "file"}},
			Membership: map[string][]string{
				"bucket-0/inv-0": {"u1", "u2"},
			},
		}
	}
}

// ablationInvocations is the realized invocation topology per stratum. The
// sequential stratum runs more than one invocation; the others run one.
func ablationInvocations(stratum string) []int64 {
	if stratum == StratumSequentialInvocs {
		return []int64{40 * second, 35 * second, 30 * second}
	}
	return []int64{60 * second}
}

// addFixtureAblations gives the fixture the twelve controlled ablations the
// contract requires to precede the campaign: three in each of the four
// topology strata, each a genuine eligible verdict signed by the delivery
// verifier, and every one of them dated BEFORE the campaign's first pair.
//
// They are built the same way the arms are rather than stubbed, because the
// gate authenticates them the same way the arms are authenticated. A fixture
// that satisfied the count with unsigned or ineligible rows would prove only
// that the counter counts.
func addFixtureAblations(t *testing.T, idx *CampaignIndex, loader memoryLoader, baseline *Stage1Manifest, authorityKey ed25519.PrivateKey) {
	t.Helper()
	verifier := baseline.Instrumentation.VerifierBinary
	for i, stratum := range AblationStrata {
		// ONE AUTHORITY-SIGNED MANIFEST PER STRATUM. The stratum is a property
		// the authority fixes before the run, not a label the unsigned index
		// carries — so each ablation is authorised into its stratum by a
		// document nobody downstream can relabel.
		manifestPath := fmt.Sprintf("ablation-stage1-%s.json", stratum)
		m := *baseline
		m.AblationStratum = stratum
		m.Signature = nil
		if err := m.Sign(CampaignAuthority, authorityKey); err != nil {
			t.Fatal(err)
		}
		if err := m.Validate(); err != nil {
			t.Fatalf("the fixture ablation manifest for %s does not validate: %v", stratum, err)
		}
		stage1, err := m.DigestOf()
		if err != nil {
			t.Fatal(err)
		}
		loader.manifests[manifestPath] = &m
		// THE DERIVED PLAN this stratum's ablations actually ran, bound to the
		// manifest that authorised it. A stratum label with no plan behind it
		// is an intent; this is what the run realized.
		receiptPath := fmt.Sprintf("ablation-stage2-%s.json", stratum)
		receipt := testReceipt(stage1, Digest("sha256:ablation-bundle-"+stratum))
		// Four DIFFERENT experiments derive four different plans. Whole-file
		// multi-file, collision-atom, legal-non-atom-slice and sequential
		// runs produce materially different atom and membership documents, and
		// the fixture says so rather than handing them one generic shape.
		receipt.PlanDigest = Digest("sha256:plan-" + stratum)
		receipt.TopologyDigest = Digest("sha256:topology-" + stratum)
		receipt.InvocationDigest = Digest("sha256:invocations-" + stratum)
		// THE PLAN THIS STRATUM ACTUALLY DERIVED. Four different experiments
		// produce four materially different documents, and the receipt binds
		// the digests OF THOSE DOCUMENTS rather than four arbitrary strings.
		derived := derivedFor(stratum)
		atoms, topo, membership, err := derived.Digests()
		if err != nil {
			t.Fatal(err)
		}
		receipt.AtomDigest, receipt.TopologyDigest, receipt.MembershipDigest = atoms, topo, membership
		derivedPath := fmt.Sprintf("ablation-derived-%s.json", stratum)
		loader.derived[derivedPath] = &derived
		if err := receipt.Validate(); err != nil {
			t.Fatalf("the fixture ablation Stage-2 for %s does not validate: %v", stratum, err)
		}
		stage2, err := receipt.DigestOf()
		if err != nil {
			t.Fatal(err)
		}
		loader.stage2[receiptPath] = &receipt
		for n := 0; n < AblationsPerStratum; n++ {
			path := fmt.Sprintf("ablation-%s-%d.json", stratum, n)
			// August: strictly before the campaign, which starts in September.
			at := fmt.Sprintf("2026-08-%02dT0%d:00:00Z", 10+i, n)
			v := &Verdict{
				Schema: SchemaVersion, Complete: true, Eligible: true,
				ActionNs:       90 * second,
				RecordsDigest:  Digest(fmt.Sprintf("sha256:records-ablation-%s-%d", stratum, n)),
				VerifierBinary: verifier,
				StartedAt:      at, Terminal: TerminalPassed,
				// The REALIZED topology: the invocations this ablation
				// measured and the peer/trace reconciliation that only exists
				// where both observers bracketed the same lifecycle. The
				// sequential-invocation stratum measures more than one, which
				// is the part of its shape retained evidence can decide.
				InvocationNs: ablationInvocations(stratum),
				Recon:        []Reconciliation{{Level: LevelInvocation, Deltas: []int64{millisecond, -millisecond}}},
				Run: RunIdentity{
					CampaignID: "ewj2", RunID: fmt.Sprintf("ablation-%s-%d", stratum, n),
					BucketID: "0", Stage1: stage1, Stage2: stage2, VerifierID: testVerdictIdentity,
				},
			}
			if err := v.Sign(testVerdictIdentity, testVerdictAuthority); err != nil {
				t.Fatal(err)
			}
			loader.verdicts[path] = v
			idx.Ablations = append(idx.Ablations, CampaignAblationRef{
				Stratum: stratum, RunID: v.Run.RunID,
				Stage1Path: manifestPath, Stage2Path: receiptPath,
				DerivedPath: derivedPath, VerdictPath: path,
			})
		}
	}
}

// TestCampaignIndexAuthenticatesEveryRow is the positive control: a campaign
// assembled from eligible verdicts and signed manifests reaches the arithmetic
// and passes it.
func TestCampaignIndexAuthenticatesEveryRow(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	gates, problems := EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign", testRelease())
	if len(problems) != 0 {
		t.Fatalf("an authenticated campaign reported problems: %v", problems)
	}
	for _, g := range gates {
		if !g.Pass {
			t.Errorf("gate %s failed: %s (%s)", g.Name, g.Observed, g.Detail)
		}
	}
	if gates[0].Name != "campaign:authenticated-population" {
		t.Errorf("authentication is not the first gate: %s", gates[0].Name)
	}
}

// TestCampaignIndexRefusals is the point of the whole document: fake
// digest-shaped strings used to pass every arithmetic gate. Each case below is
// one way a population can look right and not be one.
func TestCampaignIndexRefusals(t *testing.T) {
	cases := []struct {
		name string
		edit func(*CampaignIndex, memoryLoader, ed25519.PrivateKey)
		keys func([]string) []string
		want string
	}{
		{
			name: "a row whose verdict is not eligible",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["candidate-0-0.json"], testVerdictAuthority, func(v *Verdict) { v.Eligible = false })
			},
			want: "not eligible",
		},
		{
			name: "a row whose measurement is not complete",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["baseline-1-2.json"], testVerdictAuthority, func(v *Verdict) { v.Complete = false })
			},
			want: "not a complete measurement",
		},
		{
			name: "a row borrowed from another campaign",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["baseline-2-0.json"], testVerdictAuthority, func(v *Verdict) { v.Run.CampaignID = "some-other-campaign" })
			},
			want: "belongs to campaign",
		},
		{
			name: "a row recorded under another run",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["candidate-3-1.json"], testVerdictAuthority, func(v *Verdict) { v.Run.RunID = "someone-elses-run" })
			},
			want: "recorded under run",
		},
		{
			name: "a row bound to another Stage-1 manifest",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["baseline-0-3.json"], testVerdictAuthority, func(v *Verdict) { v.Run.Stage1 = "sha256:elsewhere" })
			},
			want: "names Stage-1",
		},
		{
			name: "a verdict file that does not exist",
			edit: func(i *CampaignIndex, l memoryLoader, _ ed25519.PrivateKey) {
				i.Pairs[0].Baseline.VerdictPaths[0] = "missing.json"
			},
			want: "no verdict at",
		},
		{
			name: "an arm with no Stage-1 manifest",
			edit: func(i *CampaignIndex, l memoryLoader, _ ed25519.PrivateKey) {
				i.Pairs[1].Candidate.Stage1Path = ""
			},
			want: "names no Stage-1 manifest",
		},
		{
			name: "arms that differ outside the allowed-difference matrix",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				// Re-signed after the edit: this case is about two VALIDLY
				// signed manifests that disagree, not about a tampered one.
				l.manifests["candidate.json"].Bundle.Selection.K = 4
				if err := l.manifests["candidate.json"].Sign("ewj2-campaign", key); err != nil {
					panic(err)
				}
			},
			want: "outside the allowed-difference matrix",
		},
		{
			name: "a manifest signed by an undeclared key",
			keys: func([]string) []string { return []string{strings.Repeat("11", 32)} },
			want: "not an authorised authority key",
		},
		{
			name: "no predeclared authority at all",
			keys: func([]string) []string { return nil },
			want: "no authority key was predeclared",
		},
		{
			name: "a manifest approved by another protected environment",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				m := l.manifests["candidate.json"]
				if err := m.Sign("some-other-environment", key); err != nil {
					panic(err)
				}
			},
			want: "not the required",
		},
		{
			name: "an unsigned verdict",
			edit: func(i *CampaignIndex, l memoryLoader, _ ed25519.PrivateKey) {
				l.verdicts["baseline-0-0.json"].Signature = nil
			},
			want: "attributes its verdict to nobody",
		},
		{
			name: "a verdict edited after it was signed",
			edit: func(i *CampaignIndex, l memoryLoader, _ ed25519.PrivateKey) {
				// The exact forgery the old campaign accepted: flip the
				// duration on a signed row.
				l.verdicts["candidate-1-0.json"].ActionNs = 1
			},
			want: "verdict signature",
		},
		{
			name: "a verdict signed by a key nobody declared",
			edit: func(i *CampaignIndex, l memoryLoader, _ ed25519.PrivateKey) {
				other, err := NewSigningKey()
				if err != nil {
					panic(err)
				}
				resign(l.verdicts["baseline-2-2.json"], other, func(v *Verdict) {})
			},
			want: "not an authorised authority key",
		},
		{
			name: "a verdict naming no records",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["candidate-4-3.json"], testVerdictAuthority, func(v *Verdict) { v.RecordsDigest = "" })
			},
			want: "names no records digest",
		},
		{
			name: "a verdict produced by an unapproved verifier",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["baseline-3-0.json"], testVerdictAuthority, func(v *Verdict) {
					v.VerifierBinary = Digest("sha256:" + strings.Repeat("cc", 32))
				})
			},
			want: "not the",
		},
		{
			name: "a verdict that retained no Aeta sample",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["candidate-2-5.json"], testVerdictAuthority, func(v *Verdict) { v.AetaSample = nil })
			},
			want: "retains no Aeta sample",
		},
		{
			name: "an index that names no campaign",
			edit: func(i *CampaignIndex, l memoryLoader, _ ed25519.PrivateKey) { i.CampaignID = "" },
			want: "names no campaign",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, loader, keys, key := campaignFixture(t)
			if tc.edit != nil {
				tc.edit(&idx, loader, key)
			}
			if tc.keys != nil {
				keys = tc.keys(keys)
			}
			gates, problems := EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign", testRelease())
			if len(problems) == 0 {
				t.Fatalf("the campaign authenticated")
			}
			if !strings.Contains(strings.Join(problems, "; "), tc.want) {
				t.Errorf("problems do not mention %q:\n%s", tc.want, strings.Join(problems, "\n"))
			}
			for _, g := range gates {
				if g.Pass {
					t.Errorf("gate %s passed on an unauthenticated campaign", g.Name)
				}
			}
		})
	}
}

// TestACampaignAuthorisesOnlyTheDeliveryItWasProducedFor is the F3 regression.
//
// The release gate ran `wall campaign` and nothing else: the evaluator was
// never told which commit was being tagged, and `LoadCampaign` only required
// each arm's ReviewTip to equal its OWN ReleaseRefSHA — which a historical
// campaign satisfies perfectly while describing a commit unrelated to the tag.
// A valid campaign committed at campaign/index.json therefore authorised every
// later release, and the locally built binary was never compared to the one
// the campaign was measured with.
func TestACampaignAuthorisesOnlyTheDeliveryItWasProducedFor(t *testing.T) {
	otherSHA := "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"
	for _, tc := range []struct {
		name    string
		release CampaignRelease
		want    string
	}{
		{"no expected delivery at all", CampaignRelease{},
			"cannot authorise any release"},
		{"an abbreviated release SHA", CampaignRelease{SHA: "693a1998", Manifest: testReleaseManifest()},
			"full 40"},
		{"no publish set at all", CampaignRelease{SHA: testTip},
			"authorises an asset it never saw"},
		{"an empty publish set", CampaignRelease{SHA: testTip, Manifest: &ReleaseManifest{Kind: ReleaseManifestKind}},
			"authorises an asset it never saw"},
		{"another commit", CampaignRelease{SHA: otherSHA, Manifest: testReleaseManifest()},
			"not the " + otherSHA + " being released"},
		{"a publish set that does not carry the delivered binary", CampaignRelease{SHA: testTip,
			Manifest: &ReleaseManifest{Kind: ReleaseManifestKind, Assets: []ReleaseAsset{
				{Name: "testbucket_0.3.0_linux_amd64.tar.gz", Path: "dist/a.tar.gz",
					Digest: DigestBytes([]byte("archive")),
					Contains: []ReleaseAssetMember{
						{Name: "testbucket", Digest: DigestBytes([]byte("a later, differently built binary"))},
					}},
				{Name: "checksums.txt", Path: "dist/checksums.txt", Digest: DigestBytes([]byte("checksums"))},
			}}},
			"not published by, or contained in, any of the 2 asset(s)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, loader, keys, _ := campaignFixture(t)
			gates, problems := EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign", tc.release)
			if len(problems) != 0 {
				t.Fatalf("the fixture campaign is not authenticated: %v", problems)
			}
			var binding *GateResult
			for i := range gates {
				if gates[i].Name == "campaign:release-binding" {
					binding = &gates[i]
				}
			}
			if binding == nil {
				t.Fatal("no release-binding gate was reported")
			}
			if binding.Pass {
				t.Fatalf("a campaign authorised a delivery it was not produced for (%s)", tc.name)
			}
			if !strings.Contains(binding.Observed+" "+binding.Detail, tc.want) {
				t.Errorf("the gate does not mention %q: %s / %s", tc.want, binding.Observed, binding.Detail)
			}
		})
	}
}

// TestTheReleaseBindingNamesEveryPublishedArtifact: a release is not one file.
// The gate must record the whole set it hashed, so a reader of the verdict can
// see which bytes were authorised rather than only that "a" binary matched.
func TestTheReleaseBindingNamesEveryPublishedArtifact(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	release := testRelease()
	gates, problems := EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign", release)
	if len(problems) != 0 {
		t.Fatalf("the fixture campaign is not authenticated: %v", problems)
	}
	for _, g := range gates {
		if g.Name != "campaign:release-binding" {
			continue
		}
		if !g.Pass {
			t.Fatalf("the campaign did not authorise its own delivery: %s (%s)", g.Observed, g.Detail)
		}
		for _, name := range release.Manifest.UploadNames() {
			if !strings.Contains(g.Detail, name) {
				t.Errorf("the gate does not name published asset %q: %s", name, g.Detail)
			}
		}
		// And it says WHERE the measured binary is published, which is the
		// fact a reader needs: the campaign binds an executable, the release
		// ships archives, and "it matched something" is not an answer.
		if !strings.Contains(g.Detail, "is published inside testbucket_0.3.0_linux_amd64.tar.gz as testbucket") {
			t.Errorf("the gate does not say where the delivered binary is published: %s", g.Detail)
		}
		return
	}
	t.Fatal("no release-binding gate was reported")
}

// TestTheRightDeliveryPassesTheReleaseBinding is the positive control: the
// campaign's own candidate tip and its own built binary authorise exactly that
// release, so the gate above is refusing the wrong delivery rather than every
// delivery.
func TestTheRightDeliveryPassesTheReleaseBinding(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	gates, problems := EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign", testRelease())
	if len(problems) != 0 {
		t.Fatalf("the fixture campaign is not authenticated: %v", problems)
	}
	for _, g := range gates {
		if g.Name == "campaign:release-binding" {
			if !g.Pass {
				t.Fatalf("the campaign did not authorise its own delivery: %s (%s)", g.Observed, g.Detail)
			}
			if g.Population != CampaignPairs {
				t.Errorf("the gate bound %d candidate arm(s), want %d", g.Population, CampaignPairs)
			}
			return
		}
	}
	t.Fatal("no release-binding gate was reported")
}

// resign applies an edit and re-signs, so a refusal case tests the field it
// edits rather than tripping the signature check that has its own case.
// resign applies an edit and re-signs AS THE VERDICT SIGNER, so a refusal case
// tests the field it edits rather than tripping the role separation that has
// its own case. A caller passing some other key is testing exactly that.
func resign(v *Verdict, key ed25519.PrivateKey, edit func(*Verdict)) {
	edit(v)
	v.Signature = nil
	if err := v.Sign(testVerdictIdentity, key); err != nil {
		panic(err)
	}
}

// TestUnauthenticatedRowsNeverReachTheArithmetic: when authentication fails,
// the arithmetic gates are not reported as passing on whatever numbers
// happened to survive. A ratio over unauthenticated rows answers a question
// nobody asked.
func TestUnauthenticatedRowsNeverReachTheArithmetic(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	loader.verdicts["candidate-0-0.json"].Eligible = false
	gates, _ := EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign", testRelease())
	if len(gates) != 1 || gates[0].Name != "campaign:authenticated-population" {
		t.Fatalf("the arithmetic ran on an unauthenticated population: %d gate(s)", len(gates))
	}
}

// TestCampaignEnforcesThePopulationWideAetaMean is the aggregate the contract
// defines over all eighty rows.
//
// Eighty individually acceptable forecasts can still miss it: each row's own
// gate allows a 20-second error, and the population mean must be within 10.
// A campaign that kept only durations could not compute that, so eighty rows
// each 15 seconds out would pass every row check and the campaign would have
// nothing to say.
func TestCampaignEnforcesThePopulationWideAetaMean(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)

	// Give every row a forecast that is 15 s out — inside the 20 s per-row
	// limit, outside the 10 s population mean.
	//
	// The PAIR rows only. The pre-campaign ablations are in the same loader
	// and are deliberately not part of any population the gates compute over:
	// the contract says their outcome cannot tune these gates, so a test that
	// swept them in would be measuring something the evaluator must never
	// read.
	for _, v := range pairVerdicts(loader, idx) {
		resign(v, testVerdictAuthority, func(v *Verdict) {
			v.AetaSample.PointNs = v.ActionNs + 15*second
			v.AetaSample.LowerNs = v.AetaSample.PointNs - 20*second
			v.AetaSample.UpperNs = v.AetaSample.PointNs + 20*second
		})
	}
	gates, problems := EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign", testRelease())
	if len(problems) != 0 {
		t.Fatalf("the population failed authentication: %v", problems)
	}
	var mae *GateResult
	for i := range gates {
		if gates[i].Name == "aeta:point-mae" {
			mae = &gates[i]
		}
	}
	if mae == nil {
		t.Fatalf("the campaign never evaluated the population-wide Aeta mean; gates: %v", gateNames(gates))
	}
	if mae.Population != ScoredActionRows {
		t.Errorf("the aggregate was taken over %d rows, want %d", mae.Population, ScoredActionRows)
	}
	if mae.Pass {
		t.Errorf("a 15 s population mean passed the %s limit", dur(AetaMAELimit))
	}

	// And a well-calibrated population passes it.
	idx, loader, keys, _ = campaignFixture(t)
	for _, v := range pairVerdicts(loader, idx) {
		resign(v, testVerdictAuthority, func(v *Verdict) { v.AetaSample.PointNs = v.ActionNs + 2*second })
	}
	gates, problems = EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign", testRelease())
	if len(problems) != 0 {
		t.Fatalf("the population failed authentication: %v", problems)
	}
	for _, g := range gates {
		if !g.Pass {
			t.Errorf("gate %s failed on a well-calibrated campaign: %s (%s)", g.Name, g.Observed, g.Detail)
		}
	}
}

func gateNames(gates []GateResult) []string {
	var out []string
	for _, g := range gates {
		out = append(out, g.Name)
	}
	return out
}

// invocationsPerBucket is how many invocations each fixture bucket measures.
// It is small and fixed so the campaign-wide invocation-peer population is a
// number a reader can check by hand: 80 rows times this.
const invocationsPerBucket = 2

// invocationNs splits a bucket's action into its measured invocations. The
// values only have to be positive and consistent with the samples below; what
// is under test is that they are RETAINED and COUNTED, not their magnitude.
func invocationNs(action int64) []int64 {
	out := make([]int64, invocationsPerBucket)
	for i := range out {
		out[i] = action / int64(invocationsPerBucket+2)
	}
	return out
}

// predictorSamples pairs each invocation's frozen projection with what it
// observed, one per invocation — the coverage the campaign now requires.
func predictorSamples(bucket int, action int64) []PredictorSample {
	var out []PredictorSample
	for i, ns := range invocationNs(action) {
		out = append(out, PredictorSample{
			InvocationSeq: i, BucketIndex: bucket,
			// Within the frozen 5 s MAE and 10 s individual limits.
			PredictedNs: ns + int64(i)*100*millisecond, ObservedNs: ns,
		})
	}
	return out
}

// reconFor is one row's like-for-like population: its action peer, its script
// peer and one peer per invocation, each inside the frozen 50 ms / 100 ms
// bounds.
func reconFor(invocations int) []Reconciliation {
	out := []Reconciliation{
		{Level: LevelAction, Deltas: []int64{3 * millisecond}},
		{Level: LevelScript, Deltas: []int64{-2 * millisecond}},
	}
	inv := Reconciliation{Level: LevelInvocation}
	for i := 0; i < invocations; i++ {
		inv.Deltas = append(inv.Deltas, int64(i+1)*millisecond)
	}
	if len(inv.Deltas) > 0 {
		out = append(out, inv)
	}
	return out
}

// The frozen contract puts the Pcheck-versus-observed-V gates and the
// like-for-like 50 ms / 100 ms reconciliation over the WHOLE population. Both
// used to be satisfiable by absence: a verdict that retained neither was
// accepted, `EvaluatePredictor` on an empty set returned a row-scope finding
// that campaign scope discarded, and no reconciliation gate was computed at
// all. Eighty such rows reached a passing campaign.
//
// Each case starts from the positive fixture and removes exactly one kind of
// evidence.
func TestCampaignRefusesAPopulationMissingItsEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		// strip edits one row of the otherwise complete population.
		strip func(v *Verdict)
		want  string
	}{
		{
			name:  "a row that retains no predictor samples",
			strip: func(v *Verdict) { v.PredictorSample = nil },
			want:  "retains 0 Pcheck/observed-V sample(s)",
		},
		{
			name:  "a row that retains fewer samples than it measured invocations",
			strip: func(v *Verdict) { v.PredictorSample = v.PredictorSample[:1] },
			want:  "retains 1 Pcheck/observed-V sample(s)",
		},
		{
			name: "a row that claims samples for invocations it never measured",
			strip: func(v *Verdict) {
				v.InvocationNs = nil
			},
			want: "measured 0 invocation(s)",
		},
		{
			name:  "a row that retains no reconciliation",
			strip: func(v *Verdict) { v.Recon = nil },
			want:  "retains no like-for-like reconciliation",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, loader, keys, _ := campaignFixture(t)
			resign(loader.verdicts["baseline-2-3.json"], testVerdictAuthority, tc.strip)

			_, problems := LoadCampaign(idx, loader, keys, "ewj2-campaign")
			if len(problems) == 0 {
				t.Fatalf("a population missing its evidence was accepted")
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Errorf("no problem mentions %q:\n%s", tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}

// And the gates themselves: even a population that loads must be REFUSED when
// the evidence it carries does not cover what the contract measures. A
// campaign that reconciled sixty of its eighty action peers perfectly has not
// reconciled the eighty it claims.
func TestCampaignGatesRefuseIncompleteEvidence(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	pairs, problems := LoadCampaign(idx, loader, keys, "ewj2-campaign")
	if len(problems) != 0 {
		t.Fatalf("the positive fixture no longer loads: %v", problems)
	}
	if failed := failedGates(EvaluateCampaign(pairs)); len(failed) != 0 {
		t.Fatalf("the complete population failed: %v", failed)
	}

	for _, tc := range []struct {
		name string
		edit func(p []CampaignPair)
		want string
	}{
		{
			name: "the predictor samples are dropped after loading",
			edit: func(p []CampaignPair) {
				for i := range p {
					p[i].Baseline.PredictorSamples = nil
					p[i].Candidate.PredictorSamples = nil
				}
			},
			want: "predictor:coverage",
		},
		{
			name: "the reconciliation population is dropped after loading",
			edit: func(p []CampaignPair) {
				for i := range p {
					p[i].Baseline.Recon = nil
					p[i].Candidate.Recon = nil
				}
			},
			want: "campaign:reconciliation:action",
		},
		{
			name: "one action peer is missing from the eighty",
			edit: func(p []CampaignPair) {
				for i := range p[0].Baseline.Recon {
					if p[0].Baseline.Recon[i].Level == LevelAction {
						p[0].Baseline.Recon[i].Deltas = nil
						break
					}
				}
			},
			want: "campaign:reconciliation:action",
		},
		{
			name: "one peer delta exceeds the frozen maximum",
			edit: func(p []CampaignPair) {
				for i := range p[0].Candidate.Recon {
					if p[0].Candidate.Recon[i].Level == LevelScript {
						p[0].Candidate.Recon[i].Deltas[0] = 250 * millisecond
						break
					}
				}
			},
			want: "campaign:reconciliation:script",
		},
		{
			name: "one invocation's prediction misses by more than the frozen limit",
			edit: func(p []CampaignPair) {
				p[0].Candidate.PredictorSamples[0].PredictedNs += 30 * second
			},
			want: "predictor:invocation-max",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh, problems := LoadCampaign(idx, loader, keys, "ewj2-campaign")
			if len(problems) != 0 {
				t.Fatalf("fixture: %v", problems)
			}
			tc.edit(fresh)
			failed := failedGates(EvaluateCampaign(fresh))
			if len(failed) == 0 {
				t.Fatalf("a campaign missing %s still passed every gate", tc.want)
			}
			found := false
			for _, name := range failed {
				if name == tc.want {
					found = true
				}
			}
			if !found {
				t.Errorf("gate %q did not fail; failures were %v", tc.want, failed)
			}
		})
	}
}

// failedGates names the gates that did not pass.
func failedGates(gates []GateResult) []string {
	var out []string
	for _, g := range gates {
		if !g.Pass {
			out = append(out, g.Name)
		}
	}
	return out
}

// testSchedule is the authority-frozen pair order the fixture executes: five
// predeclared pairs, named arms, across three UTC dates.
func testSchedule() CampaignSchedule {
	s := CampaignSchedule{Kind: ScheduleKind, CampaignID: "ewj2", Seed: 20260901}
	for pair := 0; pair < CampaignPairs; pair++ {
		s.Pairs = append(s.Pairs, ScheduledPair{
			Index:        pair,
			BaselineRun:  fmt.Sprintf("b%d", pair),
			CandidateRun: fmt.Sprintf("c%d", pair),
			Date:         scheduledDate(pair),
		})
	}
	return s
}

// scheduledDate and scheduledInstant keep the schedule and the runs that
// execute it derived from one source, so the fixture cannot drift the way the
// verifier now refuses to let a campaign drift.
func scheduledDate(pair int) string { return fmt.Sprintf("2026-09-%02d", 1+pair%3) }
func scheduledInstant(pair int) string {
	return fmt.Sprintf("%sT0%d:00:00Z", scheduledDate(pair), pair)
}

// The contract requires the authority-signed Stage-1 artifact to bind campaign
// and pair order BEFORE planning and role assignment, and to freeze that order
// before the first candidate run. Without it, which five pairs count, which arm
// is baseline, and the sequence they are attempted in are all decided after the
// authority has signed — and five genuine pairs chosen after the fact from ten
// attempts pass every other check in this package.
func TestCampaignMustExecuteTheFrozenPairOrder(t *testing.T) {
	for _, tc := range []struct {
		name string
		// edit changes the index, the schedule, or both.
		edit func(t *testing.T, idx *CampaignIndex, m map[string]*Stage1Manifest, key ed25519.PrivateKey)
		want string
	}{
		{
			name: "no frozen schedule at all",
			edit: func(t *testing.T, idx *CampaignIndex, m map[string]*Stage1Manifest, key ed25519.PrivateKey) {
				for _, mf := range m {
					resignManifest(t, mf, key, func(mf *Stage1Manifest) { mf.Schedule = CampaignSchedule{} })
				}
			},
			want: "binds a frozen campaign schedule",
		},
		{
			name: "the index names no order",
			edit: func(t *testing.T, idx *CampaignIndex, _ map[string]*Stage1Manifest, _ ed25519.PrivateKey) {
				idx.OrderDigest = ""
			},
			want: "names no frozen pair order",
		},
		{
			name: "the index claims an order the authority did not freeze",
			edit: func(t *testing.T, idx *CampaignIndex, _ map[string]*Stage1Manifest, _ ed25519.PrivateKey) {
				idx.OrderDigest = "sha256:some-other-order"
			},
			want: "but the authority froze",
		},
		{
			name: "the pairs are run in a different sequence",
			edit: func(t *testing.T, idx *CampaignIndex, _ map[string]*Stage1Manifest, _ ed25519.PrivateKey) {
				idx.Pairs[0], idx.Pairs[4] = idx.Pairs[4], idx.Pairs[0]
			},
			want: "the frozen order predeclared",
		},
		{
			name: "a run nobody predeclared is substituted into a pair",
			edit: func(t *testing.T, idx *CampaignIndex, _ map[string]*Stage1Manifest, _ ed25519.PrivateKey) {
				idx.Pairs[2].Candidate.RunID = "c-rerun"
			},
			want: `pair 2's candidate is run "c-rerun"`,
		},
		{
			name: "a pair ran on a date it was not scheduled for",
			edit: func(t *testing.T, idx *CampaignIndex, _ map[string]*Stage1Manifest, _ ed25519.PrivateKey) {
				idx.Pairs[1].Baseline.StartedAt = "2026-09-09T01:00:00Z"
			},
			want: "but the frozen order scheduled",
		},
		{
			name: "the two arms are authorised by different orders",
			edit: func(t *testing.T, idx *CampaignIndex, m map[string]*Stage1Manifest, key ed25519.PrivateKey) {
				resignManifest(t, m["candidate.json"], key, func(mf *Stage1Manifest) {
					mf.Schedule.Seed = 999
				})
			},
			want: "authorised by a different frozen pair order",
		},
		{
			name: "the schedule reuses one run across two pairs",
			edit: func(t *testing.T, idx *CampaignIndex, m map[string]*Stage1Manifest, key ed25519.PrivateKey) {
				for _, mf := range m {
					resignManifest(t, mf, key, func(mf *Stage1Manifest) {
						mf.Schedule.Pairs[3].BaselineRun = mf.Schedule.Pairs[0].BaselineRun
					})
				}
			},
			want: "pairs would not be independent",
		},
		{
			name: "the schedule spans fewer than the frozen minimum of UTC dates",
			edit: func(t *testing.T, idx *CampaignIndex, m map[string]*Stage1Manifest, key ed25519.PrivateKey) {
				for _, mf := range m {
					resignManifest(t, mf, key, func(mf *Stage1Manifest) {
						for i := range mf.Schedule.Pairs {
							mf.Schedule.Pairs[i].Date = "2026-09-01"
						}
					})
				}
			},
			want: "UTC date(s), and the frozen campaign needs at least",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, loader, keys, key := campaignFixture(t)
			tc.edit(t, &idx, loader.manifests, key)

			_, problems := LoadCampaign(idx, loader, keys, "ewj2-campaign")
			if len(problems) == 0 {
				t.Fatalf("a campaign that did not execute the frozen order was accepted")
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Errorf("no problem mentions %q:\n%s", tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}

// resignManifest applies an edit and re-signs, so a refusal case tests the
// field it names rather than the broken signature the edit would leave.
func resignManifest(t *testing.T, m *Stage1Manifest, key ed25519.PrivateKey, edit func(*Stage1Manifest)) {
	t.Helper()
	edit(m)
	m.Signature = nil
	if err := m.Sign("ewj2-campaign", key); err != nil {
		t.Fatal(err)
	}
}

// The campaign index is a plain file. Where it repeats a fact the signed
// evidence already carries — when a run started, how it ended — the two must
// agree and the evidence decides.
//
// Both facts are load-bearing: the start decides the three-UTC-date rule and
// the fourteen-day window, and the terminal state decides intention-to-treat
// retention. Taken from the index, an attacker with a genuine, fully signed row
// set could move a run into the window or report a cancelled run as passed
// without touching a single authenticated byte.
func TestTheIndexCannotRestateWhatTheRecordsShow(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(idx *CampaignIndex, l memoryLoader, key ed25519.PrivateKey)
		want string
	}{
		{
			name: "the index moves a run to a different date",
			edit: func(idx *CampaignIndex, _ memoryLoader, _ ed25519.PrivateKey) {
				idx.Pairs[1].Baseline.StartedAt = "2026-09-03T01:00:00Z"
			},
			want: "the campaign index says it started on 2026-09-03 but its signed records say 2026-09-02",
		},
		{
			name: "the index reports a cancelled run as passed",
			edit: func(idx *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				for b := 0; b < BucketsPerRun; b++ {
					resign(l.verdicts[fmt.Sprintf("candidate-3-%d.json", b)], testVerdictAuthority, func(v *Verdict) {
						v.Terminal = TerminalCancelled
					})
				}
				idx.Pairs[3].Candidate.Terminal = TerminalPassed
			},
			want: "the campaign index says it passed but its signed records say cancelled",
		},
		{
			name: "a row carries no authenticated start at all",
			edit: func(_ *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["baseline-0-2.json"], testVerdictAuthority, func(v *Verdict) { v.StartedAt = "" })
			},
			want: "carries no authenticated start instant",
		},
		{
			name: "a row carries no authenticated terminal state",
			edit: func(_ *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["candidate-4-1.json"], testVerdictAuthority, func(v *Verdict) { v.Terminal = "" })
			},
			want: "carries no authenticated terminal state",
		},
		{
			name: "two buckets of one run disagree about the outcome",
			edit: func(_ *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["baseline-2-5.json"], testVerdictAuthority, func(v *Verdict) {
					v.Terminal = TerminalCrashUnclosed
				})
			},
			want: "an earlier bucket of the same run reports",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, loader, keys, key := campaignFixture(t)
			tc.edit(&idx, loader, key)

			_, problems := LoadCampaign(idx, loader, keys, "ewj2-campaign")
			if len(problems) == 0 {
				t.Fatalf("an index that restated the evidence was accepted")
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Errorf("no problem mentions %q:\n%s", tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}

// And the population gates must read the AUTHENTICATED dates.
//
// The index here claims the three distinct UTC dates the contract requires,
// while every signed record says the whole campaign ran on one afternoon. If
// the gate read the index it would pass; it reads the records, so it fails.
func TestThePopulationDatesComeFromTheRecords(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	for _, path := range sortedVerdictPaths(loader) {
		resign(loader.verdicts[path], testVerdictAuthority, func(v *Verdict) {
			v.StartedAt = "2026-09-01T01:00:00Z"
		})
	}
	// The index keeps its schedule-shaped three dates, which is exactly the
	// claim an attacker would make.
	dates := map[string]bool{}
	for _, p := range idx.Pairs {
		dates[utcDate(p.Baseline.StartedAt)] = true
	}
	if len(dates) < CampaignDates {
		t.Fatalf("the fixture index claims %d date(s); this test needs it to claim the full %d", len(dates), CampaignDates)
	}

	pairs, problems := LoadCampaign(idx, loader, keys, "ewj2-campaign")
	// The restatement is itself refused, which is the F2 control.
	if !strings.Contains(strings.Join(problems, "\n"), "but its signed records say") {
		t.Errorf("an index claiming dates the records contradict was not refused:\n%s", strings.Join(problems, "\n"))
	}
	// And the gate, given the loaded population, decides on the records.
	var dateGate *GateResult
	gates := EvaluateCampaign(pairs)
	for i := range gates {
		if strings.Contains(gates[i].Name, "population") {
			dateGate = &gates[i]
		}
	}
	if dateGate == nil {
		t.Fatalf("no population gate was reported")
	}
	if dateGate.Pass {
		t.Errorf("a population whose records all fall on one UTC date passed the population gate: %s", dateGate.Observed)
	}
}

// pairVerdicts is the verdicts the campaign's five pairs are made of, in a
// stable order. It deliberately excludes the pre-campaign ablations, which the
// evaluator must never aggregate.
func pairVerdicts(l memoryLoader, idx CampaignIndex) []*Verdict {
	var out []*Verdict
	for _, p := range idx.Pairs {
		for _, arm := range []CampaignArm{p.Baseline, p.Candidate} {
			for _, path := range arm.VerdictPaths {
				if v, ok := l.verdicts[path]; ok {
					out = append(out, v)
				}
			}
		}
	}
	return out
}

// sortedVerdictPaths is every verdict in the loader, in a stable order.
func sortedVerdictPaths(l memoryLoader) []string {
	out := make([]string, 0, len(l.verdicts))
	for p := range l.verdicts {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
