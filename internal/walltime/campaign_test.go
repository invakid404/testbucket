package walltime

import (
	"crypto/ed25519"
	"fmt"
	"strings"
	"testing"
)

// memoryLoader serves campaign artifacts from memory, so these tests exercise
// the authentication rules rather than a filesystem.
type memoryLoader struct {
	verdicts  map[string]*Verdict
	manifests map[string]*Stage1Manifest
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

	sign := func(role string, candidateTuple bool) (*Stage1Manifest, Digest) {
		m := testManifest(testBundle(), regDigest)
		m.Role = role
		if candidateTuple {
			// The enumerated permitted difference: a different testbucket
			// source/action/binary, and the wrappers that come with it.
			candidateBinary := DigestBytes([]byte("candidate testbucket binary"))
			m.Source.BinaryDigest = candidateBinary
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
	}
	idx := CampaignIndex{Kind: CampaignIndexKind, CampaignID: "ewj2"}
	for pair := 0; pair < CampaignPairs; pair++ {
		arm := func(role, runID string, stage1, verifier Digest, top, step int64) CampaignArm {
			a := CampaignArm{
				RunID: runID, Terminal: TerminalPassed,
				StartedAt:  fmt.Sprintf("2026-09-%02dT0%d:00:00Z", 1+pair%3, pair),
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
					Run: RunIdentity{
						CampaignID: "ewj2", RunID: runID, BucketID: fmt.Sprint(b), Stage1: stage1,
					},
				}
				if err := v.Sign("ewj2-campaign", key); err != nil {
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
	return idx, loader, []string{PublicKeyOf(key)}, key
}

// TestCampaignIndexAuthenticatesEveryRow is the positive control: a campaign
// assembled from eligible verdicts and signed manifests reaches the arithmetic
// and passes it.
func TestCampaignIndexAuthenticatesEveryRow(t *testing.T) {
	idx, loader, keys, _ := campaignFixture(t)
	gates, problems := EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign")
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
				resign(l.verdicts["candidate-0-0.json"], key, func(v *Verdict) { v.Eligible = false })
			},
			want: "not eligible",
		},
		{
			name: "a row whose measurement is not complete",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["baseline-1-2.json"], key, func(v *Verdict) { v.Complete = false })
			},
			want: "not a complete measurement",
		},
		{
			name: "a row borrowed from another campaign",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["baseline-2-0.json"], key, func(v *Verdict) { v.Run.CampaignID = "some-other-campaign" })
			},
			want: "belongs to campaign",
		},
		{
			name: "a row recorded under another run",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["candidate-3-1.json"], key, func(v *Verdict) { v.Run.RunID = "someone-elses-run" })
			},
			want: "recorded under run",
		},
		{
			name: "a row bound to another Stage-1 manifest",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["baseline-0-3.json"], key, func(v *Verdict) { v.Run.Stage1 = "sha256:elsewhere" })
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
				resign(l.verdicts["candidate-4-3.json"], key, func(v *Verdict) { v.RecordsDigest = "" })
			},
			want: "names no records digest",
		},
		{
			name: "a verdict produced by an unapproved verifier",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["baseline-3-0.json"], key, func(v *Verdict) {
					v.VerifierBinary = Digest("sha256:" + strings.Repeat("cc", 32))
				})
			},
			want: "not the",
		},
		{
			name: "a verdict that retained no Aeta sample",
			edit: func(i *CampaignIndex, l memoryLoader, key ed25519.PrivateKey) {
				resign(l.verdicts["candidate-2-5.json"], key, func(v *Verdict) { v.AetaSample = nil })
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
			gates, problems := EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign")
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

// resign applies an edit and re-signs, so a refusal case tests the field it
// edits rather than tripping the signature check that has its own case.
func resign(v *Verdict, key ed25519.PrivateKey, edit func(*Verdict)) {
	edit(v)
	v.Signature = nil
	if err := v.Sign("ewj2-campaign", key); err != nil {
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
	gates, _ := EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign")
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
	idx, loader, keys, key := campaignFixture(t)

	// Give every row a forecast that is 15 s out — inside the 20 s per-row
	// limit, outside the 10 s population mean.
	for path, v := range loader.verdicts {
		_ = path
		resign(v, key, func(v *Verdict) {
			v.AetaSample.PointNs = v.ActionNs + 15*second
			v.AetaSample.LowerNs = v.AetaSample.PointNs - 20*second
			v.AetaSample.UpperNs = v.AetaSample.PointNs + 20*second
		})
	}
	gates, problems := EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign")
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
	idx, loader, keys, key = campaignFixture(t)
	for _, v := range loader.verdicts {
		resign(v, key, func(v *Verdict) { v.AetaSample.PointNs = v.ActionNs + 2*second })
	}
	gates, problems = EvaluateCampaignIndex(idx, loader, keys, "ewj2-campaign")
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
