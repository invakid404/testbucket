package walltime

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// frozenDocs is the set of documents a SCORABLE run must be bound to: the
// Stage-1 input manifest, the single Stage-2 derived-plan receipt, the Aeta
// component registry, the instantiated pre-action forecast, and the
// post-render Pcheck projection.
type frozenDocs struct {
	stage1   string
	stage2   string
	registry string
	aeta     string
	pcheck   string
	replay   string
	// scorer is the frozen model the Pcheck projection must re-derive from,
	// and trainingSet is the sealed offline surface the verifier REFITS to
	// prove the model came from it.
	scorer      string
	trainingSet string
	invocations string
	stepAttempt string
	digest      Digest
	// authority is the PREDECLARED public key of the protected environment.
	// The verifier refuses to treat a signature as authority approval without
	// one, so the fixture has to carry it the way a campaign would.
	authority string
	// audit is a coverage audit that passes for this bucket. Supplying one is
	// what a real measured row does; the tests that omit it are testing the
	// omission.
	audit AuditFunc
}

// testStoreBytes is an admitted store the fixture receipt genuinely describes:
// three rows with positive weight, two measured rows weighing zero, and one
// recorded coverage target.
func testStoreBytes() []byte {
	return storeBytesFor(3, 2, 0, []string{"tests/alpha.spec.ts"})
}

// storeBytesFor writes a store holding exactly the rows asked for, so a test
// can state a classification and get bytes that support it rather than
// hand-maintaining two halves that must agree.
func storeBytesFor(positive, zero, unmeasured int, coverage []string) []byte {
	units := map[string]map[string]any{}
	for i := 0; i < positive; i++ {
		units[fmt.Sprintf("p%d", i)] = map[string]any{"seconds": 1.5, "samples": 3}
	}
	for i := 0; i < zero; i++ {
		units[fmt.Sprintf("z%d", i)] = map[string]any{"seconds": 0, "samples": 2}
	}
	for i := 0; i < unmeasured; i++ {
		units[fmt.Sprintf("m%d", i)] = map[string]any{"seconds": 0, "samples": 0}
	}
	b, err := json.Marshal(map[string]any{
		"schema": 1, "flags": "vitest", "coverage": coverage, "units": units,
	})
	if err != nil {
		panic(err)
	}
	return b
}

func writeFrozenDocs(t *testing.T, dir string, s *synthRun) frozenDocs {
	t.Helper()
	bundle := testBundle()
	bd, err := bundle.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	reg := testRegistry()
	regDigest, err := reg.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	m := testManifest(bundle, regDigest)
	key, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	// A key of the REPLAY party's own. The fixture used to sign the
	// attestation with the authority key, which is precisely the pairing an
	// independent replay is supposed to exclude — so the fixture now models
	// two parties, and the verifier refuses them being one.
	replayKey, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	// Stage 1 is where both signer sets are declared: the run key its roster
	// and seal were actually signed with, and the separate replay signer.
	m.Instrumentation.Signers = []string{s.RunSigner()}
	m.Instrumentation.ReplaySigners = []string{PublicKeyOf(replayKey)}
	if err := m.Sign("ewj2-campaign", key); err != nil {
		t.Fatal(err)
	}
	m1, err := m.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	receipt := testReceipt(m1, bd)
	// The approval the planner saw BEFORE it planned. The fixture used to omit
	// it, which is exactly the gap: an unsigned Stage-1 could drive planning
	// and be signed afterwards without changing the digest Stage 2 records.
	approval, err := ApprovalOf(m)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Stage1Approval = approval
	// The PLAN identity, which is what the derived documents cite. The receipt
	// binds those documents and they name it back, so one side of the circle
	// has to cite an identity that excludes the binding — exactly as the
	// planner does.
	plan2, err := receipt.PlanDigestOf()
	if err != nil {
		t.Fatal(err)
	}

	aeta, err := reg.Instantiate(AetaInputs{
		BucketID: "bucket-1", BucketIndex: 1, PallocSeconds: 5.18,
		Invocations: len(s.invocations), Stage2: plan2,
	})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	// The projection is built the way production builds it, so the verifier's
	// recomputation is exercised against a real document rather than a
	// hand-written one that happens to agree.
	// Every Palloc value is SCORED, not chosen: the fixture builds the feature
	// vector that yields the number it wants and runs the frozen scorer over
	// it, exactly as the allocator does. A fixture that wrote the numbers
	// directly could not exercise a check that re-derives them.
	scorer := testScorer()
	palloc := map[string]float64{}
	var features []FeatureVector
	var invocations []PcheckInvocation
	for i, inv := range s.invocations {
		unit := fmt.Sprintf("t%d.spec.ts", i)
		want := float64(inv[1]-inv[0]+250_000_000) / float64(second)
		fv := FeatureVector{UnitID: unit, Features: []Feature{
			// testScorer is 1 + runnable_count, so this is the vector whose
			// score is `want`.
			{Name: "runnable_count", Value: want - scorer.Intercept, Provenance: ProvRunnableSnapshot},
		}}
		scored, err := scorer.Score(fv)
		if err != nil {
			t.Fatal(err)
		}
		palloc[unit], features = scored, append(features, fv)
		invocations = append(invocations, PcheckInvocation{Seq: i, BucketIndex: 1, Units: []string{unit}})
	}
	pcheck, err := BuildPcheck(plan2, receipt.MembershipDigest, scorer, palloc, features, invocations, 1, "bucket-1")
	if err != nil {
		t.Fatal(err)
	}

	// What the plan rendered for this bucket. The synthetic run's specs are
	// built from the same values, so the verifier's comparison is exercised
	// against a manifest that genuinely describes it.
	manifest := InvocationManifest{
		Kind: InvocationManifestKind, Stage2: plan2, BucketIndex: 1, BucketName: "bucket-1",
	}
	for i := range s.invocations {
		spec := s.spec(i)
		manifest.Invocations = append(manifest.Invocations, InvocationIdentity{
			Seq: i, ArgvDigest: spec.ArgvDigest, Cwd: spec.Cwd,
			SelectorDigest: spec.SelectorDigest, UnitDigest: spec.UnitDigest,
			AtomDigest: spec.AtomDigest,
			Units:      []string{fmt.Sprintf("t%d.spec.ts", i)},
		})
	}

	// The step attempt the action ran as. The synthetic records carry realtime
	// brackets derived from their monotonic readings, so the window below is
	// built from the same origin — widened, because the API reports seconds.
	firstInv, lastInv := s.invocations[0], s.invocations[len(s.invocations)-1]
	actionStart := firstInv[0] - 100_000_000 - 500_000_000
	actionEnd := lastInv[1] + 80_000_000 + 300_000_000
	step := StepAttemptDocument{Kind: AGHKind, Attempt: StepAttempt{
		Repository: "example/mandel", WorkflowRun: "run-1", Job: "test",
		Step: "run-bucket", Attempt: "1", Precision: "1s",
		// The step starts just before AT_start and ends after AT_end, which is
		// what a MEASURED step attempt looks like now that the wrapper is
		// installed by the caller: what precedes the envelope is the runner's
		// own step startup, inside A_GH's one-second resolution. The fixture
		// used to model a three-second wrapper install, which is exactly the
		// prefix the gate now refuses.
		StartedAt:   time.Unix(0, actionStart).UTC().Format(time.RFC3339),
		CompletedAt: time.Unix(0, actionEnd).UTC().Add(2 * time.Second).Format(time.RFC3339),
	}}

	// The receipt BINDS every derived document by digest, the way the planner
	// does. Without it each sidecar would carry nothing but a Stage-2 string,
	// which any substituted document can also carry.
	receipt.Sidecars = map[string]Digest{
		SidecarName(SidecarInvocations, manifest.BucketIndex): DigestJSONOrEmpty(manifest),
		SidecarName(SidecarPcheck, pcheck.BucketIndex):        DigestJSONOrEmpty(pcheck),
		SidecarName(SidecarAeta, aeta.Inputs.BucketIndex):     DigestJSONOrEmpty(aeta),
	}
	// The receipt's FULL identity, covering the binding. This is what the
	// records and the independent attestation name.
	r2, err := receipt.DigestOf()
	if err != nil {
		t.Fatal(err)
	}

	// The independent replay attestation, built LAST: a separate party
	// re-derived the plan from the frozen bundle, derived the same per-bucket
	// documents, and got the same receipt — binding and all. It signs with ITS
	// OWN key, one Stage 1 declared as a replay signer and which is not the
	// authority's.
	replay := ReplayAttestation{
		Kind: ReplayKind, Stage1Digest: m1, Stage2Digest: r2, BundleDigest: bd,
		Recomputed: receipt, VerifierID: "synthetic-verifier", VerifierBinary: synthBinary,
	}
	rd, err := replay.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	// Signed AS the verifier it names. The signature covers the authority
	// label, so that label is the only signer identity the document carries —
	// a retained VerifierID that disagreed with it would be an unchecked
	// string beside an authenticated one.
	replay.Signature = &Signature{
		Authority: replay.VerifierID, KeyID: PublicKeyOf(replayKey), Digest: rd,
		Value: signValue(replay.VerifierID, replayKey, rd),
	}

	docs := frozenDocs{
		stage1:      filepath.Join(dir, "stage1.json"),
		stage2:      filepath.Join(dir, "stage2.json"),
		registry:    filepath.Join(dir, "registry.json"),
		aeta:        filepath.Join(dir, "aeta.json"),
		pcheck:      filepath.Join(dir, "pcheck.json"),
		replay:      filepath.Join(dir, "replay.json"),
		scorer:      filepath.Join(dir, "scorer.json"),
		trainingSet: filepath.Join(dir, "training-set.json"),
		audit: func(bucket string) (*AuditEvidence, error) {
			return &AuditEvidence{
				Bucket: bucket, PlanDigest: receipt.PlanDigest,
				Planned: len(s.invocations), Reported: len(s.invocations),
				Report: "PASS — every planned package reported exactly the invocations the plan scheduled",
			}, nil
		},
		invocations: filepath.Join(dir, "invocations.json"),
		stepAttempt: filepath.Join(dir, "step-attempt.json"),
		digest:      r2,
		authority:   m.Signature.KeyID,
	}
	for path, v := range map[string]any{
		docs.stage1: m, docs.stage2: receipt, docs.registry: reg,
		docs.aeta: aeta, docs.pcheck: pcheck, docs.replay: replay, docs.scorer: scorer,
		docs.trainingSet: testTrainingSet(),
		docs.invocations: manifest, docs.stepAttempt: step,
	} {
		if err := WriteJSONFile(path, v); err != nil {
			t.Fatal(err)
		}
	}
	return docs
}

// testTools is a complete tool closure: every entry has a version AND the
// integrity of the bytes that reported it.
func testTools(head string) map[string]ToolIdentity {
	return map[string]ToolIdentity{
		head:   {Version: "4.1.10", Path: "/repo/node_modules/.bin/" + head, Integrity: "sha256:head"},
		"node": {Version: "24.19.0", Path: "/usr/bin/node", Integrity: "sha256:node"},
	}
}

func testBundle() PlanningInputBundle {
	var b PlanningInputBundle
	b.Kind = BundleKind
	b.Clock = ClockPolicy{
		Policy: "frozen_canonical_instant", Instant: "2026-08-31T12:00:00Z",
		Precision: "1ns", TimeZone: "UTC", PermittedSources: []string{"stage1_bundle"},
		StaleThreshold: "336h0m0s",
	}
	disc := NewRawSnapshot("vitest-list", []string{"vitest", "list", "--filesOnly", "--json"}, "/repo", []byte(`[{"file":"/repo/t0.spec.ts"}]`))
	// The snapshot's OWN closure, for the argv it was taken by. A discovery
	// listing with no bound environment or no resolved binary is not a frozen
	// input, and the fixture must not be able to pass on evidence the
	// contract refuses.
	disc.Env = map[string]string{"TB_DISCOVERY_EXCLUDE_PREFIXES": "shared/f/lib/cases/"}
	disc.Executables = map[string]string{"vitest": "/repo/node_modules/.bin/vitest"}
	disc.Tools = testTools("vitest")
	b.Discovery = []RawSnapshot{disc}
	// A store whose BYTES match the receipt that describes them. It used to be
	// an empty store beside a receipt claiming five classified rows, which is
	// exactly the disagreement the receipt is now derived-checked for.
	b.Store = NewRawSnapshot("test-timings.json", nil, "/repo", testStoreBytes())
	b.Source.Repository = FrozenProfileRepository
	b.Source.Commit = testConsumerCommit
	b.Source.Tree = "sha256:tree"
	b.Acquisition.Argv = []string{"testbucket", "wall", "bundle"}
	b.Acquisition.Cwd = "/repo"
	b.Acquisition.Env = map[string]string{"TB_DISCOVERY_EXCLUDE_PREFIXES": "shared/f/lib/cases/"}
	b.Acquisition.Executables = map[string]string{"testbucket": "/usr/local/bin/testbucket"}
	b.Acquisition.Tools = testTools("testbucket")
	// The identities of the implementations that would actually run — the
	// complete inventory, not a label for one of them.
	b.Parsers = ImplementedParserIdentities()
	b.Algorithms.FullPlan = ImplementedFullPlanAlgorithm()
	b.Algorithms.SemanticPlan = ImplementedSemanticPlanAlgorithm()
	b.Selection.K = 8
	b.Selection.Count = 1
	b.Selection.Token = "vitest"
	b.Selection.Runner = "vitest"
	b.Selection.Renderer = "vitest/v0.2.2"
	b.Selection.TieBreak = "unit_id_ascending"
	b.AbsentInputs = []string{"runnable_snapshots(no name-sliced unit)"}
	return b
}

// testPnpmLock is a pnpm-lock.yaml v9 fragment in exactly the shape pnpm
// writes. The receipt's declared closure is checked AGAINST it rather than
// beside it, so the fixture has to name every Vitest-family package the lock
// resolves — which is the property the check exists to enforce.
const testPnpmLock = `lockfileVersion: '9.0'

settings:
  autoInstallPeers: true

packages:

  vitest@4.1.10:
    resolution: {integrity: sha512-vitest}
    engines: {node: ^20.0.0}

  '@vitest/runner@4.1.10':
    resolution: {integrity: sha512-runner}

  '@vitest/expect@4.1.10(vite@7.0.0)':
    resolution: {integrity: sha512-expect}

  tinyrainbow@2.0.0:
    resolution: {integrity: sha512-tinyrainbow}

  '@ai-sdk/provider-utils@2.2.8':
    resolution: {integrity: sha512-provider-utils-2}

  '@ai-sdk/provider-utils@3.0.30':
    resolution: {integrity: sha512-provider-utils-3}
`

const testFacade = "await import('vitest/node')\n"
const testViteConfig = "export default { test: { fileParallelism: false } }\n"

// testBuilderAuthority is the party that built and attested the delivered
// binary — a different party from the campaign authority that approves the
// inputs, because a builder vouching for its own approval is one signature
// doing two jobs.
var testBuilderAuthority = mustSigningKey()

// testDeliveryVerifierAuthority is the INDEPENDENT verifier that countersigns
// a build. It is a separate key held by a separate party: the builder signs
// what it built, and somebody else obtains the artifact, re-derives its digest
// and signs what they concluded. Two labels over one key would be one party.
var testDeliveryVerifierAuthority = mustSigningKey()

func testBuilderKeys() []string {
	return []string{PublicKeyOf(testBuilderAuthority), PublicKeyOf(testDeliveryVerifierAuthority)}
}

// testVerdictAuthority signs verifier verdicts. It is a THIRD party: the
// campaign authority approves the inputs, this one judges the rows, and one
// key doing both is one party performing a two-party check.
var testVerdictAuthority = mustSigningKey()

func testVerdictSigners() []string { return []string{PublicKeyOf(testVerdictAuthority)} }

// testVerdictIdentity is the delivery-bound verifier that judges a row. A
// verdict must be signed UNDER it: the body names who judged, the signature
// names who signed, and a verdict where those differ is a judgement
// attributable to nobody.
const testVerdictIdentity = "ewj2-verifier"

// testBuildAttestation is a complete signed statement about one build: every
// identity the contract asks to be retained, and a result that admits it.
func testBuildAttestation(binary Digest, commit string) BuildAttestation {
	a := BuildAttestation{
		Kind: BuildAttestationKind, SubjectName: "testbucket", SubjectDigest: binary,
		SourceRepository: "invakid404/testbucket", SourceCommit: commit,
		BuilderID: "invakid404/testbucket/.github/workflows/release.yml@refs/tags/v0.3.0",
		Issuer:    "https://token.actions.githubusercontent.com",
		BuildRun:  "run-1", BuildAttempt: "1",
		VerifierID: "ewj2-delivery", VerifierBinary: synthBinary, VerifierVersion: "v0.3.0",
		VerifiedAt: "2026-08-31T12:00:00Z", Result: AttestationVerified,
	}
	// Signed AS the builder it names: the signature's authority label is the
	// only signer identity it carries, so a retained builder identity that
	// disagreed with it would be an unchecked string beside an authenticated
	// one.
	if err := a.Sign(a.BuilderID, testBuilderAuthority); err != nil {
		panic(err)
	}
	// AND COUNTERSIGNED by the verifier it names, under the verifier's own
	// key. Without this the verifier is a string the builder wrote beside a
	// result the builder also wrote.
	if err := a.Countersign(a.VerifierID, testDeliveryVerifierAuthority); err != nil {
		panic(err)
	}
	return a
}

func mustLockParserIdentity(name string) ParserIdentity {
	id, err := LockParserIdentity(name)
	if err != nil {
		panic(err)
	}
	return id
}

// testSourceProfile is the complete, lock-derivable source profile: the exact
// façade, config and lockfile bytes, and a closure that is exactly what the
// lockfile resolves.
func testSourceProfile() SourceProfileReceipt {
	lock := []byte(testPnpmLock)
	return SourceProfileReceipt{
		Repository: FrozenProfileRepository, Commit: testConsumerCommit,
		Facade:      DigestBytes([]byte(testFacade)),
		Config:      DigestBytes([]byte(testViteConfig)),
		Lockfile:    DigestBytes(lock),
		FacadeBytes: []byte(testFacade), ConfigBytes: []byte(testViteConfig), LockfileBytes: lock,
		// The identity of the parser that actually runs, not a caller's claim
		// about one.
		ParserID: mustLockParserIdentity(LockParserPNPM),
		// EXACTLY what the lockfile resolves — every NODE, keyed by the lock's
		// own identity, not just the Vitest family keyed by name. tinyrainbow
		// is here because the lock resolves it; @ai-sdk/provider-utils appears
		// TWICE at two versions, which is the shape a name-keyed closure could
		// not represent at all.
		Packages: map[string]string{
			"vitest@" + RequiredVitest:                          RequiredVitest,
			"@vitest/runner@" + RequiredVitest:                  RequiredVitest,
			"@vitest/expect@" + RequiredVitest + "(vite@7.0.0)": RequiredVitest,
			"tinyrainbow@2.0.0":                                 "2.0.0",
			"@ai-sdk/provider-utils@2.2.8":                      "2.2.8",
			"@ai-sdk/provider-utils@3.0.30":                     "3.0.30",
		},
		Integrities: map[string]string{
			"vitest@" + RequiredVitest:                          "sha512-vitest",
			"@vitest/runner@" + RequiredVitest:                  "sha512-runner",
			"@vitest/expect@" + RequiredVitest + "(vite@7.0.0)": "sha512-expect",
			"tinyrainbow@2.0.0":                                 "sha512-tinyrainbow",
			"@ai-sdk/provider-utils@2.2.8":                      "sha512-provider-utils-2",
			"@ai-sdk/provider-utils@3.0.30":                     "sha512-provider-utils-3",
		},
	}
}

// testTrainingAuthority is the party that sealed the fixture's offline
// surface. It is separate from the campaign authority on purpose: the contract
// seals the training set once, long before any campaign, and a verifier that
// accepted the campaign's own key here would be letting the planner attest its
// own model.
var testTrainingAuthority = mustSigningKey()

func testTrainingKeys() []string { return []string{PublicKeyOf(testTrainingAuthority)} }

// testEvidenceAuthority attests each label's physical-V receipt, selected-work
// document and topology receipt. It is separate from the training authority
// that seals the set: one party observed the historical runs, another sealed
// the surface built from them, and collapsing the two would let the sealer
// vouch for its own evidence.
var testEvidenceAuthority = mustSigningKey()

func testEvidenceSigners() []string { return []string{PublicKeyOf(testEvidenceAuthority)} }

// signEvidence seals one evidence document and returns its exact bytes plus
// the digest that addresses them. The BYTES are what a label references, so
// they are produced once here and never re-serialised.
func signEvidence[T any](doc *T, sign func(*T, Digest)) ([]byte, Digest) {
	d, err := evidenceDigest(doc)
	if err != nil {
		panic(err)
	}
	sign(doc, d)
	raw, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	return raw, DigestBytes(raw)
}

func sig(d Digest) *Signature {
	return &Signature{
		Authority: "ewj2-observation", KeyID: PublicKeyOf(testEvidenceAuthority),
		Digest: d, Value: SignApproval("ewj2-observation", testEvidenceAuthority, d),
	}
}

// evidenceLabel builds one admissible label WITH the real bytes of its
// physical-V receipt, its selected work and its topology validation. Every
// fixture that needs a label goes through it, so no test can accidentally
// exercise the old shape where the three references were strings pointing at
// nothing.
func evidenceLabel(id string, ns int64, features ...Feature) TrainingLabel {
	{
		const at = "2026-08-01T00:00:00Z"

		work := &SelectedWorkDocument{Kind: SelectedWorkKind, UnitID: id, Units: []string{id + ".spec.ts"}}
		workBytes, workDigest := signEvidence(work, func(d *SelectedWorkDocument, dg Digest) { d.Signature = sig(dg) })

		topology := &TopologyValidationReceipt{
			Kind: TopologyReceiptKind, UnitID: id, SelectedWorkDigest: workDigest,
			Validated: true, Validator: "ewj2-topology",
		}
		topologyBytes, topologyDigest := signEvidence(topology, func(d *TopologyValidationReceipt, dg Digest) { d.Signature = sig(dg) })

		receipt := &PhysicalVReceipt{
			Kind: PhysicalVReceiptKind, ReceiptID: id, UnitID: id,
			Level: LevelInvocation, Producer: ProducerPhysical, Source: SourceContainment,
			Containment: ContainmentIdentity{
				Primitive: PrimitiveCgroup2, ID: "tb-" + id, Inode: "9" + id, BootID: "boot-1",
				// The root process identity a scorable containment must carry:
				// a pid alone is a number the kernel reuses.
				RootPID: 4242, RootStart: "778899",
				// Owned by a credential the measured workload does not have:
				// on cgroup-v2 `cgroup.procs` is the migration control. The
				// INPUTS to that decision are retained beside it, because a
				// conclusion whose inputs nobody kept cannot be rederived.
				OwnerUID: 1000, OwnerGID: 900, Mode: 0o770,
				WorkloadUID: 1001, WorkloadGIDs: []int{1001},
				MembershipControl: MembershipSupervisorOwned,
			},
			Terminal: TerminalPassed, ObservedAt: at, DurationNs: ns,
			SelectedWorkDigest: workDigest, TopologyReceipt: topologyDigest,
		}
		receiptBytes, receiptDigest := signEvidence(receipt, func(d *PhysicalVReceipt, dg Digest) { d.Signature = sig(dg) })

		return TrainingLabel{
			ReceiptID: id, UnitID: id, Provenance: LabelProvenance,
			ReceiptHash: receiptDigest, SelectedWorkDigest: workDigest, TopologyReceipt: topologyDigest,
			ObservedAt: at, ObservedNs: ns,
			Features: features,
			Evidence: &LabelEvidence{
				ReceiptBytes: receiptBytes, SelectedWorkBytes: workBytes, TopologyBytes: topologyBytes,
			},
		}
	}
}

// testTrainingSet is the sealed offline surface the fixture's scorer is fitted
// from.
func testTrainingSet() TrainingReceiptSet {
	label := func(id string, runnables float64, ns int64) TrainingLabel {
		return evidenceLabel(id, ns, Feature{Name: "runnable_count", Value: runnables, Provenance: ProvRunnableSnapshot})
	}
	set := TrainingReceiptSet{
		Kind: TrainingSetKind, Epoch: "vitest-4.1.10", Cutoff: "2026-08-30T00:00:00Z",
		FeatureSchema: []string{"runnable_count"},
		Algorithm:     "ridge-least-squares", Configuration: "lambda=0.01", Lambda: 0.01, Seed: 1,
		EvidenceSigners: testEvidenceSigners(),
		Labels: []TrainingLabel{
			label("h1", 1, 2*int64(second)),
			label("h2", 2, 3*int64(second)),
			label("h3", 3, 4*int64(second)),
			label("h4", 4, 5*int64(second)),
		},
	}
	if err := set.Seal("ewj2-training", testTrainingAuthority); err != nil {
		panic(err)
	}
	return set
}

// testScorer is the frozen scorer the synthetic Pcheck came from; Stage 1
// binds its digest through the training lineage. It is FITTED rather than
// written out, so the fixture exercises the same recomputation the verifier
// performs instead of a model nobody could reproduce.
func testScorer() Scorer {
	sc, err := TrainScorer(testTrainingSet(), "synthetic", testTrainingKeys())
	if err != nil {
		panic(err)
	}
	return *sc
}

// testTip and testConsumerCommit are FULL commit SHAs, because that is what
// the manifest requires: an abbreviation is a prefix another object can grow
// into, and the fixture must not be able to pass on something the contract
// would refuse.
const (
	testTip            = "693a19981fb6e0061d3fab62e59d75dc1c01ff3f"
	testConsumerCommit = "d9ae1d433bb45012c04d567879b66fc4bf6112c6"
	testWorkflowSHA    = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"
)

func testManifest(b PlanningInputBundle, registry Digest) Stage1Manifest {
	m := Stage1Manifest{Kind: Stage1Kind, Role: "candidate", Bundle: b}
	m.Actions = map[string]ActionIdentity{
		"plan":       {Commit: testTip, ContentDigest: "sha256:plan"},
		"run-bucket": {Commit: testTip, ContentDigest: "sha256:run"},
		"record":     {Commit: testTip, ContentDigest: "sha256:record"},
	}
	m.Source.ReviewTip = testTip
	m.Source.ReleaseRefSHA = testTip
	// The delivered binary IS the binary the producers run; the manifest now
	// requires them to agree.
	m.Source.BinaryDigest = synthBinary
	m.Source.BuildAttestation = testBuildAttestation(synthBinary, testTip)
	m.BuilderKeys = testBuilderKeys()
	m.Consumer.Repository = FrozenProfileRepository
	m.Consumer.Commit = testConsumerCommit
	m.Consumer.WorkflowSHA = testWorkflowSHA
	m.Consumer.DownstreamRef = "refs/heads/main@" + testConsumerCommit
	m.Consumer.RunnerImage = "ubuntu-24.04@sha256:" + strings.Repeat("cd", 32)
	profile := testSourceProfile()
	m.Consumer.Facade = profile.Facade
	m.Consumer.Config = profile.Config
	m.Consumer.Lockfile = profile.Lockfile
	m.Store = StoreReceipt{
		Digest: b.Store.Digest, Schema: 1, MigrationID: "store/v1", Token: "vitest",
		CacheKey: "testbucket-timings-demo-v1", RestoreMethod: "exact-key",
		StaleAt: "2099-01-01T00:00:00Z",
		// A measured zero is its own state, not a gap.
		Classifications: map[string]int{
			RowObservedPositive: 3, RowObservedZero: 1, RowNoTests: 1,
		},
		Rows:     5,
		Coverage: []string{"tests/alpha.spec.ts"},
	}
	m.SourceProfile = testSourceProfile()
	m.Instrumentation = InstrumentationIdentity{
		Schema: SchemaVersion, PhysicalBinary: synthBinary, PeerBinary: synthBinary,
		TraceBinary: synthBinary, VerifierBinary: synthBinary,
		ContainmentPolicy: "cgroup2-dedicated-subtree", ChildAdmission: "clone-into-cgroup",
		EndpointOrder: "physical<=peer<=trace", CancellationPolicy: CancellationPolicyID,
		RawSourceTaxonomy: []string{SourceContainment, SourceProcessLifecycle, SourceReporter, SourceWrapper},
	}
	m.AllowedDifferences = []string{"testbucket source/action/binary tuple"}
	m.Registry = registry
	sc := testScorer()
	m.TrainingLineage = sc.Lineage
	if d, err := sc.DigestOf(); err == nil {
		m.TrainingLineage.ScorerDigest = d
	}
	m.TrainingAuthorityKeys = testTrainingKeys()
	m.VerdictSigners = testVerdictSigners()
	return m
}

func testReceipt(stage1, bundle Digest) Stage2Receipt {
	r := Stage2Receipt{
		Kind: Stage2Kind, Stage1Digest: stage1, BundleDigest: bundle,
		// Every real derivation is performed under a one-shot claim, so the
		// fixture that stands in for one carries it too.
		PlannerClaim:     fixtureClaim(stage1, bundle),
		InputAccess:      []InputAccess{{Field: "discovery[0]", Digest: "sha256:disc"}},
		PlanDigest:       "sha256:plan-full",
		SemanticDigest:   "sha256:plan-semantic",
		AtomDigest:       "sha256:atoms",
		TopologyDigest:   "sha256:topology",
		MembershipDigest: "sha256:membership",
		InvocationDigest: "sha256:invocations",
		ScriptDigest:     "sha256:script",
		MatrixDigest:     "sha256:matrix",
		PlannerResult:    "ok", RendererResult: "ok",
	}
	r.Algorithms.FullPlan = AlgorithmIdentity{Name: FullPlanDigestAlgorithm, Canonicalizer: CanonAlgorithm, Implementation: "testbucket"}
	r.Algorithms.SemanticPlan = AlgorithmIdentity{Name: SemanticPlanDigestAlgorithm, Canonicalizer: CanonAlgorithm, Implementation: "testbucket"}
	return r
}

// testRegistry maps every phase the verifier derives. The action-level
// components are the ones included in the A forecast; the script- and
// invocation-level ones are present but not summed, because their time is
// already inside bucket_script.
func testRegistry() AetaRegistry {
	// Every physical component carries its OWN bound. This fixture used to
	// omit them on the action-only and Palloc components and was then asserted
	// to be the registry a scored row is measured against — which made an
	// unbounded component the shape of a good one and left the contract's
	// component-local limit unenforced for most of the taxonomy.
	//
	// The bound is derived from the component's own forecast rather than a
	// blanket constant, so it is a limit about THIS component: generous enough
	// not to fire on the fixture's own numbers, specific enough to mean
	// something.
	bound := func(point int64) int64 { return point + 2*second }
	c := func(id, parent string, class ComponentClass, included bool, point int64) Component {
		return Component{ID: id, Parent: parent, Owner: "testbucket", Class: class, Included: included,
			Formula: FormulaConstant, Inputs: []string{"stage1"}, PointNs: point,
			IntervalNs: 100 * millisecond, BoundNs: bound(point)}
	}
	return AetaRegistry{
		Kind: RegistryKind, Version: "1",
		Components: []Component{
			c("action_containment_bootstrap", "action", ClassActionOnly, true, 20*millisecond),
			c("action_prologue", "action", ClassActionOnly, true, 480*millisecond),
			{ID: "bucket_script", Parent: "action", Owner: "testbucket", Class: ClassPalloc, Included: true,
				Formula: FormulaPallocSum, Inputs: []string{"palloc"}, IntervalNs: 2 * second,
				BoundNs: 600 * second},
			c("action_epilogue_flush", "action", ClassActionOnly, true, 285*millisecond),
			c("action_suffix", "action", ClassActionOnly, true, 15*millisecond),
			c("script_containment_bootstrap", "script", ClassActionOnly, false, 20*millisecond),
			c("between_invocation_gap", "script", ClassActionOnly, false, 80*millisecond),
			{ID: "invocation", Parent: "script", Owner: "testbucket", Class: ClassPalloc, Included: false,
				Formula: FormulaPallocSum, Inputs: []string{"palloc"}, BoundNs: 600 * second},
			c("script_epilogue", "script", ClassActionOnly, false, 65*millisecond),
			c("script_suffix", "script", ClassActionOnly, false, 15*millisecond),
			c("invocation_bootstrap", "invocation", ClassActionOnly, false, 20*millisecond),
			{ID: "invocation_containment", Parent: "invocation", Owner: "testbucket", Class: ClassPalloc, Included: false,
				Formula: FormulaPallocSum, Inputs: []string{"palloc"}, BoundNs: 600 * second},
			c("invocation_suffix", "invocation", ClassActionOnly, false, 15*millisecond),
		},
	}
}

// verifySynth writes a synthetic run plus its frozen documents and verifies.
func verifySynth(t *testing.T, mutate mutation, tweak func(*synthRun)) *Verdict {
	t.Helper()
	dir := t.TempDir()
	s := newSynthRun(filepath.Join(dir, "records"))
	if tweak != nil {
		tweak(s)
	}
	docs := writeFrozenDocs(t, dir, s)
	s.stage2 = docs.digest
	s.write(t, mutate)
	v, err := VerifyDir(VerifyOptions{
		Dir: s.dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
		RegistryPath: docs.registry, AetaPath: docs.aeta, PcheckPath: docs.pcheck,
		ScorerPath: docs.scorer, TrainingSetPath: docs.trainingSet, Audit: docs.audit,
		ReplayPath: docs.replay, InvocationsPath: docs.invocations,
		StepAttemptPath: docs.stepAttempt,
		AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
	})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	return v
}

// TestVerifierScoresACompleteBoundRun is the positive control. Everything the
// contract asks for is present: three independent producers per level, the
// endpoint containment, a scored containment primitive, a real clock, the
// frozen documents, and every gate inside its threshold.
func TestVerifierScoresACompleteBoundRun(t *testing.T) {
	v := verifySynth(t, nil, nil)
	for _, f := range v.Findings {
		t.Errorf("unexpected finding %s/%s: %s", f.Code, f.Severity, f.Detail)
	}
	// Row-scope gates are the ones a single verified row decides. The
	// campaign-scope entries in the table are reported, never passed, and
	// belong to `wall campaign` over the full population.
	for _, g := range RowScope(v.Gates) {
		if !g.Pass {
			t.Errorf("row gate %s failed: required %s, observed %s (%s)", g.Name, g.Required, g.Observed, g.Detail)
		}
	}
	for _, g := range v.Gates {
		if g.Scope == ScopeCampaign && g.Pass {
			t.Errorf("campaign gate %s passed on a single row", g.Name)
		}
	}
	if !v.Complete || !v.Eligible {
		t.Fatalf("complete=%v eligible=%v, want both true", v.Complete, v.Eligible)
	}
	if v.ActionNs != newSynthRun("").actionNs() {
		t.Errorf("action = %d ns, want %d", v.ActionNs, newSynthRun("").actionNs())
	}
	// The physical partition must be exact, not merely plausible.
	var actionTotal int64
	for _, p := range v.Phases {
		if p.Parent == "action" {
			actionTotal += p.Duration()
		}
	}
	if actionTotal != v.ActionNs {
		t.Errorf("action phases total %d ns, envelope %d ns", actionTotal, v.ActionNs)
	}
}

// TestVerifierRefusals is the negative control: one broken invariant at a
// time, each expected to be caught by its own code and to make the run
// unscorable. These are the failures a plausible-but-wrong implementation
// produces, so each row is a claim about what this verifier would have
// noticed.
func TestVerifierRefusals(t *testing.T) {
	cases := []struct {
		name   string
		code   string
		mutate mutation
		tweak  func(*synthRun)
	}{
		{
			name: "trace endpoint copied from the peer", code: "WT-007",
			mutate: func(l Level, seq int, p Producer, b string, r *Record) {
				if l == LevelInvocation && seq == 0 && p == ProducerTrace && b == "start" {
					// The classic shortcut: reuse the peer's reading instead of
					// taking one. The peer's start is bootstrapNs after the
					// physical start.
					r.Instant.Mono = Nanos(1_000_000_000 + 20_000_000)
				}
			},
		},
		{
			name: "peer and trace cite one raw event", code: "WT-006",
			mutate: func(l Level, seq int, p Producer, b string, r *Record) {
				if l == LevelInvocation && seq == 0 && p == ProducerTrace {
					r.RawEventID = "containment_peer:invocation:" + b + ":0"
					r.RawEventDigest = DigestBytes([]byte(r.RawEventID))
				}
			},
		},
		{
			name: "peer and trace share an execution context", code: "WT-005",
			tweak: func(s *synthRun) { s.sharedObserverContext = true },
		},
		{
			name: "a lifecycle delimited by a wrapper annotation", code: "WT-013",
			mutate: func(l Level, seq int, p Producer, b string, r *Record) {
				if l == LevelScript && p == ProducerTrace && b == "start" {
					r.Source = SourceWrapper
				}
			},
		},
		{
			name: "the trace escapes its peer's bracket", code: "WT-012",
			mutate: func(l Level, seq int, p Producer, b string, r *Record) {
				if l == LevelScript && p == ProducerTrace && b == "end" {
					r.Instant.Mono += Nanos(50 * millisecond)
				}
			},
		},
		{
			name: "an envelope names a different containment", code: "WT-008",
			mutate: func(l Level, seq int, p Producer, b string, r *Record) {
				if l == LevelInvocation && seq == 1 && p == ProducerTrace {
					r.Containment.Inode = "111111"
				}
			},
		},
		{
			name: "containment cannot prove no descendant escaped", code: "WT-009",
			tweak: func(s *synthRun) { s.containmentPrimitive = PrimitiveProcessGroup },
		},
		{
			name: "readings come from an unscorable clock", code: "WT-010",
			tweak: func(s *synthRun) { s.clockID = ClockRealtimeUnscored },
		},
		{
			name: "a terminal row is retained, never scored", code: "WT-014",
			mutate: func(l Level, seq int, p Producer, b string, r *Record) {
				if l == LevelInvocation && seq == 1 && p == ProducerPhysical && b == "end" {
					r.Terminal, r.Reason = TerminalCrashUnclosed, "a descendant outlived the root"
				}
			},
		},
		{
			name: "records name a plan nobody authorised", code: "WT-018",
			mutate: func(l Level, seq int, p Producer, b string, r *Record) {
				r.Run.Stage2 = "sha256:some-other-plan"
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := verifySynth(t, tc.mutate, tc.tweak)
			found := false
			for _, f := range v.Findings {
				if f.Code == tc.code {
					found = true
				}
			}
			if !found {
				t.Errorf("no %s finding; got %+v", tc.code, v.Findings)
			}
			if v.Eligible {
				t.Errorf("the run remained eligible after %s", tc.name)
			}
		})
	}
}

// TestVerifierRefusesAnUnboundRun proves the absence of the frozen documents
// is itself disqualifying: an unbound measurement cannot be scored just
// because its records are tidy.
func TestVerifierRefusesAnUnboundRun(t *testing.T) {
	dir := t.TempDir()
	s := newSynthRun(filepath.Join(dir, "records"))
	s.write(t, nil)
	v, err := VerifyDir(VerifyOptions{Dir: s.dir})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Complete {
		t.Errorf("the records are well-formed; Complete should be true")
	}
	if v.Eligible {
		t.Errorf("an unbound run must never be eligible")
	}
	wants := []string{"Stage-1", "Stage-2", "component registry"}
	joined := ""
	for _, f := range v.Findings {
		joined += f.Detail + "\n"
	}
	for _, w := range wants {
		if !strings.Contains(joined, w) {
			t.Errorf("findings do not mention the missing %s: %s", w, joined)
		}
	}
}

// TestVerifierRefusesAnEmptyDirectory is the sharpest fail-closed case: no
// records at all is not a zero-length measurement.
func TestVerifierRefusesAnEmptyDirectory(t *testing.T) {
	v, err := VerifyDir(VerifyOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if v.Complete || v.Eligible {
		t.Errorf("an empty directory verified as complete=%v eligible=%v", v.Complete, v.Eligible)
	}
}

// TestVerifierRefusesUnboundDerivedDocuments covers the second half of the
// audit: it is not enough that records are well formed and the frozen
// documents exist. The forecast and the projection have to be RE-DERIVABLE and
// bound to the same plan, or the 10/20-second and 5/10-second gates are
// comparing observations against numbers somebody chose afterwards.
func TestVerifierRefusesUnboundDerivedDocuments(t *testing.T) {
	cases := []struct {
		name string
		// edit rewrites one frozen document on disk after it was written.
		edit func(t *testing.T, docs frozenDocs)
		// opts adjusts what the verifier was told to trust.
		opts func(*VerifyOptions)
		want string
	}{
		{
			name: "no predeclared authority key",
			opts: func(o *VerifyOptions) { o.AuthorityKeys = nil },
			want: "no authority key was predeclared",
		},
		{
			name: "a signature from a key nobody declared",
			opts: func(o *VerifyOptions) { o.AuthorityKeys = []string{"00" + strings.Repeat("11", 31)} },
			want: "not an authorised authority key",
		},
		{
			name: "the manifest names another protected environment",
			opts: func(o *VerifyOptions) { o.Authority = "some-other-environment" },
			want: "names authority",
		},
		{
			name: "a prediction edited after the action",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.pcheck, func(m map[string]any) {
					invs := m["invocations"].([]any)
					invs[0].(map[string]any)["predicted_ns"] = float64(1)
				})
			},
			want: "does not recompute",
		},
		{
			name: "a frozen Palloc value edited after the action",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.pcheck, func(m map[string]any) {
					for k := range m["palloc"].(map[string]any) {
						m["palloc"].(map[string]any)[k] = float64(99)
					}
				})
			},
			want: "does not recompute",
		},
		{
			name: "a projection of some other plan's membership",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.pcheck, func(m map[string]any) {
					m["rendered_membership_digest"] = "sha256:elsewhere"
				})
			},
			want: "covers membership",
		},
		{
			name: "a forecast point moved after the action",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.aeta, func(m map[string]any) {
					m["point_ns"] = m["point_ns"].(float64) + 3e9
				})
			},
			want: "does not re-derive",
		},
		{
			name: "no independent replay attestation",
			opts: func(o *VerifyOptions) { o.ReplayPath = "" },
			want: "nobody re-derived",
		},
		{
			name: "a replay that derived a different plan",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.replay, func(m map[string]any) {
					m["recomputed"].(map[string]any)["atom_digest"] = "sha256:different-atoms"
				})
			},
			want: "derived a different plan",
		},
		{
			name: "a replay attesting to another receipt",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.replay, func(m map[string]any) { m["stage2_digest"] = "sha256:elsewhere" })
			},
			want: "attests to Stage-2 receipt",
		},
		{
			name: "a replay run by an unapproved verifier binary",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.replay, func(m map[string]any) {
					m["verifier_binary"] = "sha256:" + strings.Repeat("ab", 32)
				})
			},
			want: "not the",
		},
		{
			name: "an unsigned replay attestation",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.replay, func(m map[string]any) { delete(m, "signature") })
			},
			want: "unsigned",
		},
		{
			name: "no invocation manifest to check the measured specs against",
			opts: func(o *VerifyOptions) { o.InvocationsPath = "" },
			want: "not checked against the authorised plan",
		},
		{
			name: "a measured invocation that selected something else",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.invocations, func(m map[string]any) {
					invs := m["invocations"].([]any)
					invs[0].(map[string]any)["selector_digest"] = "sha256:another-selection"
				})
			},
			want: "selector digest",
		},
		{
			name: "a measured invocation covering different units",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.invocations, func(m map[string]any) {
					invs := m["invocations"].([]any)
					invs[1].(map[string]any)["unit_digest"] = "sha256:different-membership"
				})
			},
			want: "unit membership digest",
		},
		{
			name: "a plan that rendered more invocations than ran",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.invocations, func(m map[string]any) {
					invs := m["invocations"].([]any)
					m["invocations"] = append(invs, map[string]any{"seq": float64(9)})
				})
			},
			want: "were measured",
		},
		{
			name: "an invocation manifest for another plan",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.invocations, func(m map[string]any) { m["stage2_digest"] = "sha256:elsewhere" })
			},
			want: "invocation manifest names Stage-2",
		},
		{
			name: "a forecast for another plan",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.aeta, func(m map[string]any) { m["stage2_digest"] = "sha256:elsewhere" })
			},
			want: "names Stage-2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s := newSynthRun(filepath.Join(dir, "records"))
			docs := writeFrozenDocs(t, dir, s)
			s.stage2 = docs.digest
			s.write(t, nil)
			if tc.edit != nil {
				tc.edit(t, docs)
			}
			opts := VerifyOptions{
				Dir: s.dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
				RegistryPath: docs.registry, AetaPath: docs.aeta, PcheckPath: docs.pcheck, ScorerPath: docs.scorer,
				TrainingSetPath: docs.trainingSet, Audit: docs.audit,
				ReplayPath: docs.replay, InvocationsPath: docs.invocations,
				StepAttemptPath: docs.stepAttempt,
				AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
			}
			if tc.opts != nil {
				tc.opts(&opts)
			}
			v, err := VerifyDir(opts)
			if err != nil {
				t.Fatal(err)
			}
			if v.Eligible {
				t.Errorf("the run remained eligible")
			}
			var details []string
			for _, f := range v.Findings {
				details = append(details, f.Detail)
			}
			if !strings.Contains(strings.Join(details, "\n"), tc.want) {
				t.Errorf("no finding mentions %q; got:\n%s", tc.want, strings.Join(details, "\n"))
			}
		})
	}
}

// TestVerifierRefusesAnUnapprovedProducerBinary ties every record back to the
// build Stage 1 approved. The per-record signing keys are minted per run and
// cannot be predeclared; the binary that mints them can be.
func TestVerifierRefusesAnUnapprovedProducerBinary(t *testing.T) {
	dir := t.TempDir()
	s := newSynthRun(filepath.Join(dir, "records"))
	docs := writeFrozenDocs(t, dir, s)
	s.stage2 = docs.digest
	s.write(t, nil)
	editJSON(t, docs.stage1, func(m map[string]any) {
		m["instrumentation"].(map[string]any)["trace_binary"] = "sha256:" + strings.Repeat("ab", 32)
	})
	v, err := VerifyDir(VerifyOptions{
		Dir: s.dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
		RegistryPath: docs.registry, AetaPath: docs.aeta, PcheckPath: docs.pcheck, ScorerPath: docs.scorer,
		TrainingSetPath: docs.trainingSet, Audit: docs.audit,
		ReplayPath: docs.replay, InvocationsPath: docs.invocations,
		StepAttemptPath: docs.stepAttempt,
		AuthorityKeys:   []string{docs.authority},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Eligible {
		t.Errorf("records from an unapproved build were scored")
	}
	found := false
	for _, f := range v.Findings {
		if strings.Contains(f.Detail, "Stage 1 approved") {
			found = true
		}
	}
	if !found {
		t.Errorf("no binary-identity finding: %+v", v.Findings)
	}
}

// signValue signs a digest the way Sign does, for a document the fixture
// assembles by hand. It covers the authority label as production does: a
// signature is bound to the label recorded beside it.
func signValue(authority string, key ed25519.PrivateKey, d Digest) string {
	return SignApproval(authority, key, d)
}

// editJSON rewrites one field of a frozen document on disk, which is how a
// post-hoc adjustment would actually arrive.
func editJSON(t *testing.T, path string, edit func(map[string]any)) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestStage1BindsEveryRequiredIdentity walks the manifest field by field. A
// manifest that binds MOST of the delivery identity proves most of the
// delivery, and "most" is indistinguishable from "none" when the question is
// whether two arms ran the same thing.
func TestStage1BindsEveryRequiredIdentity(t *testing.T) {
	reg := testRegistry()
	regDigest, err := reg.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	base := func() Stage1Manifest { return testManifest(testBundle(), regDigest) }
	if err := base().Validate(); err != nil {
		t.Fatalf("a fully bound manifest was rejected: %v", err)
	}
	cases := []struct {
		name string
		edit func(*Stage1Manifest)
		want string
	}{
		{"an abbreviated action commit", func(m *Stage1Manifest) {
			m.Actions["plan"] = ActionIdentity{Commit: "693a1998", ContentDigest: "sha256:plan"}
		}, "full 40-character commit SHA"},
		{"no release ref to prove the delivery", func(m *Stage1Manifest) { m.Source.ReleaseRefSHA = "" }, "release ref SHA"},
		{"a release ref that is not the reviewed tip", func(m *Stage1Manifest) {
			m.Source.ReleaseRefSHA = strings.Repeat("a", 40)
		}, "the reviewed tip is"},
		{"no build attestation", func(m *Stage1Manifest) { m.Source.BuildAttestation = BuildAttestation{} }, "build attestation"},
		{"a build attestation for another binary", func(m *Stage1Manifest) {
			m.Source.BuildAttestation = testBuildAttestation(DigestBytes([]byte("some other build")), testTip)
		}, "but the delivered binary is"},
		{"a build attestation for another source", func(m *Stage1Manifest) {
			m.Source.BuildAttestation = testBuildAttestation(synthBinary, strings.Repeat("b", 40))
		}, "but the reviewed tip is"},
		{"a build attestation signed by an undeclared builder", func(m *Stage1Manifest) {
			a := testBuildAttestation(synthBinary, testTip)
			a.Signature = nil
			if err := a.Sign(a.BuilderID, mustSigningKey()); err != nil {
				panic(err)
			}
			m.Source.BuildAttestation = a
		}, "signature"},
		{"a build attestation whose signer is not the builder it names", func(m *Stage1Manifest) {
			a := testBuildAttestation(synthBinary, testTip)
			a.Signature = nil
			if err := a.Sign("somebody-else", testBuilderAuthority); err != nil {
				panic(err)
			}
			m.Source.BuildAttestation = a
		}, "must be the identity that signed"},
		{"a build attestation with no build run", func(m *Stage1Manifest) {
			a := testBuildAttestation(synthBinary, testTip)
			a.BuildRun = ""
			a.Signature = nil
			if err := a.Sign(a.BuilderID, testBuilderAuthority); err != nil {
				panic(err)
			}
			m.Source.BuildAttestation = a
		}, "build run"},
		{"no predeclared verdict signer", func(m *Stage1Manifest) { m.VerdictSigners = nil }, "no verdict signer is predeclared"},
		{"a verdict signer that is also the approving authority", func(m *Stage1Manifest) {
			// Signed here so the manifest carries the signature the
			// disjointness check compares against.
			approver := mustSigningKey()
			if err := m.Sign("ewj2-campaign", approver); err != nil {
				panic(err)
			}
			m.VerdictSigners = []string{PublicKeyOf(approver)}
		}, "may not approve what it judges"},
		{"a build attestation whose retained result is not a verification", func(m *Stage1Manifest) {
			a := testBuildAttestation(synthBinary, testTip)
			a.Result = "probably fine"
			a.Signature = nil
			if err := a.Sign(a.BuilderID, testBuilderAuthority); err != nil {
				panic(err)
			}
			m.Source.BuildAttestation = a
		}, "retained result is"},
		{"no predeclared builder key", func(m *Stage1Manifest) { m.BuilderKeys = nil }, "no builder key is predeclared"},
		{"no binary digest", func(m *Stage1Manifest) { m.Source.BinaryDigest = "" }, "binary digest"},
		{"no consumer repository", func(m *Stage1Manifest) { m.Consumer.Repository = "" }, "consumer repository"},
		{"an abbreviated consumer commit", func(m *Stage1Manifest) { m.Consumer.Commit = "d9ae1d43" }, "consumer commit"},
		{"no caller workflow", func(m *Stage1Manifest) { m.Consumer.WorkflowSHA = "" }, "caller workflow"},
		{"no downstream ref", func(m *Stage1Manifest) { m.Consumer.DownstreamRef = "" }, "downstream ref"},
		{"a runner-image alias", func(m *Stage1Manifest) { m.Consumer.RunnerImage = "ubuntu-latest" }, "is an alias"},
		{"no component registry", func(m *Stage1Manifest) { m.Registry = "" }, "component registry"},
		{"no training lineage", func(m *Stage1Manifest) { m.TrainingLineage = TrainingLineageID{} }, "training"},
		{"no allowed-difference matrix", func(m *Stage1Manifest) { m.AllowedDifferences = nil }, "allowed differences"},
		{"no containment policy", func(m *Stage1Manifest) { m.Instrumentation.ContainmentPolicy = "" }, "containment policy"},
		{"no raw-source taxonomy", func(m *Stage1Manifest) { m.Instrumentation.RawSourceTaxonomy = nil }, "raw-source taxonomy"},
		{"an unbound source-profile commit", func(m *Stage1Manifest) { m.SourceProfile.Commit = "" }, "source profile"},
		{"an unbound lock parser", func(m *Stage1Manifest) { m.SourceProfile.ParserID = ParserIdentity{} }, "lock parser"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base()
			m.Actions = map[string]ActionIdentity{
				"plan":       {Commit: testTip, ContentDigest: "sha256:plan"},
				"run-bucket": {Commit: testTip, ContentDigest: "sha256:run"},
				"record":     {Commit: testTip, ContentDigest: "sha256:record"},
			}
			tc.edit(&m)
			err := m.Validate()
			if err == nil {
				t.Fatalf("the manifest validated")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestTheTrainingSurfaceIsIndependentlyReproved is the F5 regression.
//
// The offline constructor was already strong: signature, cutoff, exclusions,
// causal attribution, topology, uniqueness, schema. All of it was thrown away
// after `wall train`. What reached Stage 1 and verification was a scorer
// carrying a receipt-set digest string, and the only thing ever checked about
// it was that the string was not empty — so an independently written scorer
// could claim any sealed set and allocate the whole campaign.
//
// Each case below is the accepted run with the offline surface falsified in
// one way. Every one of them scored before.
func TestTheTrainingSurfaceIsIndependentlyReproved(t *testing.T) {
	cases := []struct {
		name string
		edit func(*VerifyOptions, *testing.T, string)
		want string
	}{
		{"no sealed set at all", func(o *VerifyOptions, t *testing.T, dir string) {
			o.TrainingSetPath = ""
		}, "no sealed training receipt set was supplied"},
		{"a set sealed by nobody", func(o *VerifyOptions, t *testing.T, dir string) {
			set := testTrainingSet()
			set.Signature = nil
			o.TrainingSetPath = writeDoc(t, dir, "unsigned-set.json", set)
		}, "unsigned"},
		{"a set sealed by a key the authority did not declare", func(o *VerifyOptions, t *testing.T, dir string) {
			set := testTrainingSet()
			if err := set.Seal("ewj2-training", mustSigningKey()); err != nil {
				t.Fatal(err)
			}
			o.TrainingSetPath = writeDoc(t, dir, "foreign-set.json", set)
		}, "signature"},
		{"a set the scorer does not name", func(o *VerifyOptions, t *testing.T, dir string) {
			set := testTrainingSet()
			// One extra FULLY ADMISSIBLE label — real evidence and all — so
			// what this case isolates is the lineage mismatch and not a
			// second defect in the row it appends.
			set.Labels = append(set.Labels, evidenceLabel("h9", 6*int64(second),
				Feature{Name: "runnable_count", Value: 5, Provenance: ProvRunnableSnapshot}))
			if err := set.Seal("ewj2-training", testTrainingAuthority); err != nil {
				t.Fatal(err)
			}
			o.TrainingSetPath = writeDoc(t, dir, "other-set.json", set)
		}, "digests to"},
		{"a scorer whose coefficients the set does not produce", func(o *VerifyOptions, t *testing.T, dir string) {
			sc := testScorer()
			sc.Coefficients["runnable_count"] += 5
			o.ScorerPath = writeDoc(t, dir, "tampered-scorer.json", sc)
		}, "did not come from the set it names"},
		{"a scorer with a fabricated intercept", func(o *VerifyOptions, t *testing.T, dir string) {
			sc := testScorer()
			sc.Intercept += 1
			o.ScorerPath = writeDoc(t, dir, "intercept-scorer.json", sc)
		}, "intercept"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s := newSynthRun(filepath.Join(dir, "records"))
			docs := writeFrozenDocs(t, dir, s)
			s.stage2 = docs.digest
			s.write(t, nil)
			opt := VerifyOptions{
				Dir: s.dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
				RegistryPath: docs.registry, AetaPath: docs.aeta, PcheckPath: docs.pcheck,
				ScorerPath: docs.scorer, TrainingSetPath: docs.trainingSet, Audit: docs.audit,
				ReplayPath: docs.replay, InvocationsPath: docs.invocations,
				StepAttemptPath: docs.stepAttempt,
				AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
			}
			tc.edit(&opt, t, dir)
			v, err := VerifyDir(opt)
			if err != nil {
				t.Fatalf("VerifyDir: %v", err)
			}
			if v.Eligible {
				t.Fatalf("a row allocated by an unverifiable training surface was scored (%s)", tc.name)
			}
			found := ""
			for _, f := range v.Findings {
				if f.Code == "WT-027" {
					found += f.Detail + "\n"
				}
			}
			if found == "" {
				t.Fatalf("no WT-027 finding; got %v", v.Findings)
			}
			if !strings.Contains(found, tc.want) {
				t.Errorf("no WT-027 finding mentions %q:\n%s", tc.want, found)
			}
		})
	}
}

// TestAVerifiedTrainingSurfaceLeavesTheRowScorable is the positive control for
// the check above: the real sealed set, refitted, must produce no finding at
// all. Without this the test beside it would pass just as happily if the
// verifier refused every run.
func TestAVerifiedTrainingSurfaceLeavesTheRowScorable(t *testing.T) {
	v := verifySynth(t, nil, nil)
	if hasFinding(v, "WT-027") {
		t.Errorf("the genuine sealed training surface did not verify: %v", v.Findings)
	}
	if !v.Eligible {
		t.Errorf("Eligible = false on a fully bound run: %v", v.Findings)
	}
}

// TestStage1RefusesAScorerItsSealedSetDoesNotProduce: the manifest refits
// before signing, so a scorer built from other evidence never obtains a
// signed Stage-1 binding in the first place.
func TestStage1RefusesAScorerItsSealedSetDoesNotProduce(t *testing.T) {
	set := testTrainingSet()
	sc := testScorer()
	sc.Coefficients["runnable_count"] += 3
	lineage := sc.Lineage
	if d, err := sc.DigestOf(); err == nil {
		lineage.ScorerDigest = d
	}
	problems := VerifyTrainingSurface(set, lineage, sc, testTrainingKeys())
	if len(problems) == 0 {
		t.Fatal("a scorer the sealed set does not produce passed the refit")
	}
	// And the genuine pair passes, so the check is not simply always failing.
	good := testScorer()
	goodLineage := good.Lineage
	if d, err := good.DigestOf(); err == nil {
		goodLineage.ScorerDigest = d
	}
	if p := VerifyTrainingSurface(set, goodLineage, good, testTrainingKeys()); len(p) > 0 {
		t.Errorf("the genuine scorer failed its own refit: %v", p)
	}
}

// writeDoc writes one JSON document into dir and returns its path.
func writeDoc(t *testing.T, dir, name string, v any) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := WriteJSONFile(path, v); err != nil {
		t.Fatal(err)
	}
	return path
}

// hasFinding reports whether the verdict carries a finding with this code.
func hasFinding(v *Verdict, code string) bool {
	for _, f := range v.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

// TestMixedRepeatedRunIdentityIsRefused is the F2 regression.
//
// Each case takes the accepted synthetic run and changes ONE field of the
// repeated identity on one producer's stream, then lets the production writer
// sign it, chain it, register its key and seal it. Nothing is tampered with:
// every signature verifies, every hash chain closes, and the closing run-key
// seal covers the exact bytes on disk. What is wrong is semantic — two
// identities in one directory are two measurements — and before this the
// verifier scored them as one.
func TestMixedRepeatedRunIdentityIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*RunIdentity)
	}{
		{"a different run", func(r *RunIdentity) { r.RunID = "different-run" }},
		{"a different attempt", func(r *RunIdentity) { r.AttemptID = "2" }},
		{"a different bucket", func(r *RunIdentity) { r.BucketID = "bucket-2" }},
		{"a different job", func(r *RunIdentity) { r.Job = "other-job" }},
		{"a different step attempt", func(r *RunIdentity) { r.StepAttempt = "7" }},
		{"a different workflow run", func(r *RunIdentity) { r.WorkflowRun = "run-2" }},
		{"a different Stage-1 manifest", func(r *RunIdentity) { r.Stage1 = Digest("sha256:" + strings.Repeat("9", 64)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := verifySynth(t, func(level Level, seq int, producer Producer, boundary string, r *Record) {
				if level == LevelScript && producer == ProducerTrace {
					tc.edit(&r.Run)
				}
			}, nil)
			if v.Eligible {
				t.Fatal("the verifier scored a sealed stream carrying a different repeated run identity")
			}
			if !hasFinding(v, "WT-026") {
				t.Errorf("no WT-026 finding; got %v", v.Findings)
			}
		})
	}
}

// TestTheRosterAndSealMustNameTheMeasuredRun: the sidecars repeat the same
// identity, and a sidecar for another run decides the signer set — or fixes
// the stream bytes — of a measurement it does not belong to.
func TestTheRosterAndSealMustNameTheMeasuredRun(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tweak func(*synthRun)
	}{
		{"a roster for another job", func(s *synthRun) { s.rosterRun = func(r *RunIdentity) { r.Job = "other-job" } }},
		{"a seal for another attempt", func(s *synthRun) { s.sealRun = func(r *RunIdentity) { r.AttemptID = "2" } }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := verifySynth(t, nil, tc.tweak)
			if v.Eligible {
				t.Fatal("the verifier scored a run whose sidecar names a different delivery identity")
			}
			if !hasFinding(v, "WT-023") {
				t.Errorf("no WT-023 finding; got %v", v.Findings)
			}
		})
	}
}

// TestBundleBindsItsAcquisitionClosure: a bundle that names no tree, no argv
// and no resolved executables cannot say what reproducing its inputs would
// take, which is the whole reason it exists.
func TestBundleBindsItsAcquisitionClosure(t *testing.T) {
	cases := []struct {
		name string
		edit func(*PlanningInputBundle)
		want string
	}{
		{"no source tree", func(b *PlanningInputBundle) { b.Source.Tree = "" }, "source tree"},
		{"an abbreviated source commit", func(b *PlanningInputBundle) { b.Source.Commit = "d9ae1d43" }, "source commit"},
		{"no acquisition argv", func(b *PlanningInputBundle) { b.Acquisition.Argv = nil }, "does not record how it was acquired"},
		{"no resolved executable", func(b *PlanningInputBundle) { b.Acquisition.Executables = nil }, "resolved executable"},
		{"an unbound environment", func(b *PlanningInputBundle) { b.Acquisition.Env = nil }, "does not record how it was acquired"},
		{"no comparability token", func(b *PlanningInputBundle) { b.Selection.Token = "" }, "comparability token"},
		{"no tie-break order", func(b *PlanningInputBundle) { b.Selection.TieBreak = "" }, "tie-break"},
		{"K of zero", func(b *PlanningInputBundle) { b.Selection.K = 0 }, "K is 0"},
		{"a parser with no digest", func(b *PlanningInputBundle) {
			b.Parsers = []ParserIdentity{{Name: "x", Version: "1"}}
		}, "no version or digest"},
		{"a parser identity the implementation does not match", func(b *PlanningInputBundle) {
			b.Parsers = append([]ParserIdentity(nil), b.Parsers...)
			b.Parsers[0].Digest = DigestBytes([]byte("invented parser bytes"))
		}, "the implementation that will run"},
		{"an incomplete parser inventory", func(b *PlanningInputBundle) {
			b.Parsers = ImplementedParserIdentities()[1:]
		}, "is not bound at all"},
		{"a parser this build does not implement", func(b *PlanningInputBundle) {
			b.Parsers = append(ImplementedParserIdentities(),
				ParserIdentity{Name: "invented-policy", Version: PlanImplementationVersion, Digest: SelfDigest()})
		}, "no such parser or policy"},
		{"an invented full-plan algorithm implementation", func(b *PlanningInputBundle) {
			b.Algorithms.FullPlan.Implementation = "some-other-implementation"
		}, "the implementation that will run"},
		{"a runnable listing with bytes but no names", func(b *PlanningInputBundle) {
			raw := []byte(`[{"name":"a","file":"x.spec.ts"}]`)
			b.Runnables = []RunnableSnapshot{{TargetID: "x.spec.ts", Bytes: raw, Digest: DigestBytes(raw)}}
		}, "no parsed names"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := testBundle()
			tc.edit(&b)
			err := b.Validate()
			if err == nil {
				t.Fatalf("the bundle validated")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestEverySnapshotBindsTheClosureOfItsOwnArgv is the F1/F6 regression.
//
// Each frozen listing must bind the exact argv, cwd, planning-relevant
// environment, resolved executable path, and complete tool/version/integrity
// closure THAT LISTING was taken by. Before this the discovery snapshot was
// checked for name, argv and cwd only: a snapshot with no environment
// validated, and the resolved-executable map — which lived on the bundle, not
// on the snapshot — could describe a program that had not run, or say
// `unresolved` and still be planned on.
//
// Every case below is an accepted bundle with exactly one clause of that
// closure removed or falsified. All of them used to validate.
func TestEverySnapshotBindsTheClosureOfItsOwnArgv(t *testing.T) {
	// withRunnable adds an otherwise-complete runnable listing, so the
	// runnable cases start from a bundle the validator accepts.
	withRunnable := func(b *PlanningInputBundle, edit func(*RunnableSnapshot)) {
		raw := []byte(`[{"name":"a","file":"x.spec.ts"}]`)
		r := RunnableSnapshot{
			TargetID: "x.spec.ts", Argv: []string{"vitest", "list", "x.spec.ts", "--json"},
			Cwd: "/repo", Env: map[string]string{},
			Executables: map[string]string{"vitest": "/repo/node_modules/.bin/vitest"},
			Tools:       testTools("vitest"),
			Names:       []string{"a"}, Bytes: raw, Digest: DigestBytes(raw),
		}
		edit(&r)
		b.Runnables = []RunnableSnapshot{r}
		b.AbsentInputs = nil
	}
	cases := []struct {
		name string
		edit func(*PlanningInputBundle)
		want string
	}{
		{"a discovery snapshot with no environment", func(b *PlanningInputBundle) {
			b.Discovery[0].Env = nil
		}, "does not record how it was acquired"},
		{"a discovery snapshot with no resolved executable", func(b *PlanningInputBundle) {
			b.Discovery[0].Executables = nil
		}, "binds no resolved executable path"},
		{"a discovery closure that resolves another program", func(b *PlanningInputBundle) {
			b.Discovery[0].Executables = map[string]string{"npx": "/usr/local/bin/npx"}
		}, "did not run"},
		{"a discovery executable that resolved to nothing", func(b *PlanningInputBundle) {
			b.Discovery[0].Executables = map[string]string{"vitest": ""}
		}, "resolves executable"},
		{"a discovery executable recorded as unresolved", func(b *PlanningInputBundle) {
			b.Discovery[0].Executables = map[string]string{"vitest": Unresolved}
		}, "could not resolve executable"},
		{"a discovery snapshot with no tool closure", func(b *PlanningInputBundle) {
			b.Discovery[0].Tools = nil
		}, "binds no tool version"},
		{"a discovery tool with no integrity", func(b *PlanningInputBundle) {
			b.Discovery[0].Tools = map[string]ToolIdentity{"node": {Version: "24.19.0"}}
		}, "no version or integrity"},
		{"a discovery tool with no version", func(b *PlanningInputBundle) {
			b.Discovery[0].Tools = map[string]ToolIdentity{"node": {Integrity: "sha256:node"}}
		}, "no version or integrity"},
		{"a discovery tool recorded as unresolved", func(b *PlanningInputBundle) {
			b.Discovery[0].Tools = map[string]ToolIdentity{"node": {Version: Unresolved, Integrity: "sha256:node"}}
		}, "could not resolve tool"},
		{"a runnable listing with no environment", func(b *PlanningInputBundle) {
			withRunnable(b, func(r *RunnableSnapshot) { r.Env = nil })
		}, "does not record how it was acquired"},
		{"a runnable closure that resolves another program", func(b *PlanningInputBundle) {
			withRunnable(b, func(r *RunnableSnapshot) {
				r.Executables = map[string]string{"npx": "/usr/local/bin/npx"}
			})
		}, "did not run"},
		{"a runnable tool recorded as unresolved", func(b *PlanningInputBundle) {
			withRunnable(b, func(r *RunnableSnapshot) {
				r.Tools = map[string]ToolIdentity{"vitest": {Version: "4.1.10", Integrity: Unresolved}}
			})
		}, "could not resolve tool"},
		{"an acquisition closure that resolves another program", func(b *PlanningInputBundle) {
			b.Acquisition.Executables = map[string]string{"npx": "/usr/local/bin/npx"}
		}, "did not run"},
		{"an acquisition tool with no integrity", func(b *PlanningInputBundle) {
			b.Acquisition.Tools = map[string]ToolIdentity{"testbucket": {Version: "0.2.2"}}
		}, "no version or integrity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := testBundle()
			// The starting point must be accepted, or the case below proves
			// nothing about the clause it removed.
			if err := b.Validate(); err != nil {
				t.Fatalf("the unedited fixture does not validate: %v", err)
			}
			tc.edit(&b)
			err := b.Validate()
			if err == nil {
				t.Fatalf("the bundle validated with %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestCompareArmsFindsEveryUnequalInvariant is the pair-equality rule: the two
// arms may differ ONLY in the enumerated candidate testbucket tuple. Anything
// else differing means the pair compared two different experiments.
func TestCompareArmsFindsEveryUnequalInvariant(t *testing.T) {
	reg := testRegistry()
	regDigest, err := reg.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	baseline := testManifest(testBundle(), regDigest)
	baseline.Role = "baseline"
	candidate := testManifest(testBundle(), regDigest)
	// The permitted difference: a different testbucket source, action content
	// and binary, together with the wrappers that ship with it. None of it may
	// be reported.
	candidateBinary := DigestBytes([]byte("candidate testbucket binary"))
	candidate.Source.BinaryDigest = candidateBinary
	candidate.Instrumentation.PhysicalBinary = candidateBinary
	candidate.Instrumentation.PeerBinary = candidateBinary
	candidate.Instrumentation.TraceBinary = candidateBinary
	candidate.Instrumentation.VerifierBinary = candidateBinary
	candidate.Actions["run-bucket"] = ActionIdentity{Commit: testTip, ContentDigest: "sha256:candidate-run"}
	if diffs := CompareArms(baseline, candidate); len(diffs) != 0 {
		t.Errorf("the permitted candidate tuple was reported as a difference: %v", diffs)
	}

	for _, tc := range []struct {
		name string
		edit func(*Stage1Manifest)
		want string
	}{
		{"a different runner image", func(m *Stage1Manifest) { m.Consumer.RunnerImage = "ubuntu-22.04@sha256:" + strings.Repeat("ef", 32) }, "runner_image"},
		{"a different K", func(m *Stage1Manifest) { m.Bundle.Selection.K = 4 }, "selection.k"},
		{"a different sweep count", func(m *Stage1Manifest) { m.Bundle.Selection.Count = 2 }, "selection.count"},
		{"a different store", func(m *Stage1Manifest) { m.Bundle.Store = NewRawSnapshot("s", nil, "/repo", []byte("other")) }, "store"},
		{"a different lockfile", func(m *Stage1Manifest) { m.Consumer.Lockfile = "sha256:other" }, "consumer.lockfile"},
		{"a different training lineage", func(m *Stage1Manifest) { m.TrainingLineage.ScorerDigest = "sha256:other" }, "training_lineage"},
		{"a different component registry", func(m *Stage1Manifest) { m.Registry = "sha256:other" }, "component_registry"},
		{"a different consumer commit", func(m *Stage1Manifest) { m.Consumer.Commit = strings.Repeat("b", 40) }, "consumer.commit"},
		{"a different source profile", func(m *Stage1Manifest) { m.SourceProfile.Integrities["vitest"] = "sha512-different" }, "source_profile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testManifest(testBundle(), regDigest)
			tc.edit(&c)
			diffs := CompareArms(baseline, c)
			if len(diffs) == 0 {
				t.Fatalf("the difference was not reported")
			}
			if !strings.Contains(strings.Join(diffs, "; "), tc.want) {
				t.Errorf("diffs %v do not mention %q", diffs, tc.want)
			}
		})
	}

	// And two arms with the same role are not a pair.
	same := testManifest(testBundle(), regDigest)
	if diffs := CompareArms(same, same); len(diffs) == 0 {
		t.Errorf("two candidate manifests were accepted as a baseline/candidate pair")
	}
}

// TestVerifierRefusesADuplicateLifecycle is the retry rule.
//
// The action stream is written by two processes, so a writer must be able to
// resume it — and that necessity is the hazard: a correctly chained SECOND
// lifecycle appended to the same stream would otherwise be widened into one
// apparent interval, first start to last end, and a retried action would read
// as a single long one.
func TestVerifierRefusesADuplicateLifecycle(t *testing.T) {
	dir := t.TempDir()
	s := newSynthRun(filepath.Join(dir, "records"))
	s.write(t, nil)

	// Append a second, correctly chained action lifecycle: exactly what a
	// retry that resumed the stream would produce.
	path := filepath.Join(s.dir, streamName(ProducerPhysical, LevelAction, 0))
	recs, err := ReadRecords(path)
	if err != nil {
		t.Fatal(err)
	}
	key, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWriter(path, ProducerPhysical, recs[0].ProducerID, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.Kind != "boundary" {
			continue
		}
		r.Instant.Mono += Nanos(60 * second)
		if _, err := w.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	v, err := VerifyDir(VerifyOptions{Dir: s.dir})
	if err != nil {
		t.Fatal(err)
	}
	if v.Complete {
		t.Errorf("a stream with two lifecycles verified as complete")
	}
	found := false
	for _, f := range v.Findings {
		if f.Code == "WT-020" && strings.Contains(f.Detail, "duplicate or a retry") {
			found = true
		}
	}
	if !found {
		t.Errorf("no WT-020 duplicate finding: %+v", v.Findings)
	}
}

// TestRawEvidenceIsRetained: a digest proves a record was not edited; it does
// not let anyone re-read what the kernel actually said. The contract asks for
// retained raw evidence, and a digest of discarded bytes is a receipt for
// evidence rather than evidence.
func TestRawEvidenceIsRetained(t *testing.T) {
	dir := t.TempDir()
	if _, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: dir, Cwd: dir, Timeout: 30 * time.Second,
		Argv: []string{"sh", "-c", "true"},
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	recs, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	boundaries, trees := 0, 0
	for _, r := range recs {
		switch {
		case r.Kind == "boundary" && r.Producer != ProducerPhysical:
			boundaries++
			if len(r.RawEventBytes) == 0 {
				t.Errorf("%s %s boundary retained no raw bytes, only a digest of them", r.Producer, r.Boundary)
			}
			if r.RawEventDigest == "" {
				t.Errorf("%s %s boundary has no raw evidence digest", r.Producer, r.Boundary)
			}
		case r.Kind == "process_tree":
			trees++
			if r.Note == "" {
				t.Errorf("the process-tree record carries no membership note")
			}
		}
	}
	if boundaries != 4 {
		t.Errorf("saw %d peer/trace boundary records, want 4", boundaries)
	}
	// THREE: the admission read taken with the containment frozen so it
	// observes exactly what was admitted, the last read taken while the
	// process was still observable, and the drained read after it empties.
	// One record could only ever be the last, and an empty close snapshot is
	// not evidence that anything was admitted.
	if trees != 3 {
		t.Errorf("saw %d process-tree records, want 3 (admitted, observed and drained)", trees)
	}
}

// TestStoreReceiptDistinguishesEveryRowState is the store-provenance rule.
//
// A measured zero is a measurement. Folding it into "missing" would let a real
// observation disappear into a gap, and the contract is explicit that
// observed_zero stays distinct from missing, failed, cancelled and malformed —
// and that a stale store or a restore-key fallback is not warm evidence at all.
func TestStoreReceiptDistinguishesEveryRowState(t *testing.T) {
	// The receipt is checked against the EXACT bytes it describes, at the
	// canonical planning instant — not against fields that merely look filled
	// in.
	storeBytes := storeBytesFor(3, 1, 1, nil)
	instant := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	base := func() StoreReceipt {
		return StoreReceipt{
			Digest: DigestBytes(storeBytes), Schema: 1, MigrationID: "store/v1", Token: "vitest",
			CacheKey: "k", RestoreMethod: "exact-key", StaleAt: "2099-01-01T00:00:00Z",
			Classifications: map[string]int{
				RowObservedPositive: 3, RowObservedZero: 1, RowMissing: 1, RowFailed: 1,
			},
			Rows: 6,
		}
	}
	check := func(r StoreReceipt, now time.Time) error {
		return r.Validate(storeBytes, 1, "vitest", now)
	}
	if err := check(base(), instant); err != nil {
		t.Fatalf("a complete store receipt was rejected: %v", err)
	}
	// Every state the contract names is admissible and separately counted.
	// The four RESIDENT states have to match the bytes; the four that describe
	// rows the ingest declined to admit cannot, because such a row is not in
	// the store to be counted.
	all := base()
	all.Classifications = map[string]int{}
	for i, state := range StoreRowStates {
		all.Classifications[state] = i + 1
	}
	allBytes := storeBytesFor(
		all.Classifications[RowObservedPositive],
		all.Classifications[RowObservedZero]+all.Classifications[RowNoTests],
		all.Classifications[RowMissing], nil)
	all.Digest = DigestBytes(allBytes)
	all.Rows = 0
	for _, n := range all.Classifications {
		all.Rows += n
	}
	if err := all.Validate(allBytes, 1, "vitest", instant); err != nil {
		t.Errorf("the full set of row states was rejected: %v", err)
	}

	for _, tc := range []struct {
		name string
		edit func(*StoreReceipt)
		now  time.Time
		want string
	}{
		{"no migration epoch", func(r *StoreReceipt) { r.MigrationID = "" }, instant, "migration id"},
		{"a restore-key fallback", func(r *StoreReceipt) { r.RestoreMethod = "restore-key-fallback" }, instant, "restore-key fallback"},
		{"a stale store at the canonical instant", func(r *StoreReceipt) { r.StaleAt = "2020-01-01T00:00:00Z" },
			instant, "not warm evidence"},
		{"staleness judged against no instant at all", func(r *StoreReceipt) {}, time.Time{}, "canonical planning instant"},
		{"a classification nobody defined", func(r *StoreReceipt) { r.Classifications["probably_fine"] = 1; r.Rows++ },
			instant, "unknown row classification"},
		{"counts that do not add up", func(r *StoreReceipt) { r.Rows = 99 }, instant, "count 6 rows but the store has 99"},
		{"a row count the frozen bytes do not support",
			func(r *StoreReceipt) { r.Classifications[RowObservedPositive] = 4; r.Rows++ },
			instant, "present in the store but the frozen bytes hold"},
		{"a coverage set the frozen bytes never recorded",
			func(r *StoreReceipt) { r.Coverage = []string{"tests/invented.spec.ts"} },
			instant, "the two sets are not the same"},
		{"a measured zero relabelled as a gap",
			func(r *StoreReceipt) { r.Classifications[RowObservedZero] = 0; r.Classifications[RowMissing] = 2 },
			instant, "carry no sample"},
		{"a digest for some other store", func(r *StoreReceipt) { r.Digest = "sha256:elsewhere" }, instant, "the bundle froze"},
		{"the wrong schema", func(r *StoreReceipt) { r.Schema = 99 }, instant, "frozen bytes are schema"},
		{"the wrong comparability token", func(r *StoreReceipt) { r.Token = "-race -count=100" }, instant, "measured under"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base()
			tc.edit(&r)
			err := check(r, tc.now)
			if err == nil {
				t.Fatalf("the receipt validated")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestCompareArmsCatchesEveryBundleAndInstrumentationDifference: the rule is
// stated as an exclusion, so it is implemented as one. Digesting the whole
// bundle and the whole instrumentation identity is what stops a field like
// file_parallelism or the raw-source taxonomy from going uncompared because
// nobody remembered to add it to a list.
func TestCompareArmsCatchesEveryBundleAndInstrumentationDifference(t *testing.T) {
	reg := testRegistry()
	regDigest, err := reg.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	baseline := testManifest(testBundle(), regDigest)
	baseline.Role = "baseline"

	for _, tc := range []struct {
		name string
		edit func(*Stage1Manifest)
	}{
		{"a different intra-bucket parallelism", func(m *Stage1Manifest) { m.Bundle.Render.FileParallelism = 4 }},
		{"a different records directory", func(m *Stage1Manifest) { m.Bundle.Render.WallDir = "/somewhere/else" }},
		{"a different events directory", func(m *Stage1Manifest) { m.Bundle.Render.EventsDir = "/other/events" }},
		{"different discovery bytes", func(m *Stage1Manifest) {
			m.Bundle.Discovery = []RawSnapshot{NewRawSnapshot("d", []string{"x"}, "/repo", []byte("[]"))}
		}},
		{"a different parser version", func(m *Stage1Manifest) {
			m.Bundle.Parsers[0].Version = "v9.9"
		}},
		{"a different digest implementation", func(m *Stage1Manifest) {
			m.Bundle.Algorithms.FullPlan.Implementation = "some-other-build"
		}},
		{"a different acquisition environment", func(m *Stage1Manifest) {
			m.Bundle.Acquisition.Env = map[string]string{"TB_DISCOVERY_EXCLUDE_PREFIXES": "other/"}
		}},
		{"a different absent-input claim", func(m *Stage1Manifest) {
			m.Bundle.AbsentInputs = append(m.Bundle.AbsentInputs, "something else")
		}},
		{"a different containment primitive", func(m *Stage1Manifest) {
			m.Instrumentation.ContainmentPolicy = "some other primitive"
		}},
		{"a different raw-source taxonomy", func(m *Stage1Manifest) {
			m.Instrumentation.RawSourceTaxonomy = []string{SourceContainment}
		}},
		{"a different store receipt", func(m *Stage1Manifest) { m.Store.MigrationID = "store/v2" }},
		{"a different allowed-difference matrix", func(m *Stage1Manifest) {
			m.AllowedDifferences = []string{"anything we like"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testManifest(testBundle(), regDigest)
			tc.edit(&c)
			if diffs := CompareArms(baseline, c); len(diffs) == 0 {
				t.Errorf("the difference was not reported")
			}
		})
	}
}

// TestStepAttemptIsIdentityNotAGate is the A_GH rule.
//
// GitHub reports step timestamps at one-second resolution, so this can never
// be a sub-second measurement — the contract says so, and the interval is
// widened by its declared precision before anything is compared. What it CAN
// do is say which step a ledger measured, and make the wrapper-install prefix
// that necessarily precedes AT_start visible rather than merely absent.
func TestStepAttemptIsIdentityNotAGate(t *testing.T) {
	dir := t.TempDir()
	s := newSynthRun(filepath.Join(dir, "records"))
	docs := writeFrozenDocs(t, dir, s)
	s.stage2 = docs.digest
	s.write(t, nil)

	opts := VerifyOptions{
		Dir: s.dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
		RegistryPath: docs.registry, AetaPath: docs.aeta, PcheckPath: docs.pcheck, ScorerPath: docs.scorer,
		TrainingSetPath: docs.trainingSet, Audit: docs.audit,
		ReplayPath: docs.replay, InvocationsPath: docs.invocations,
		StepAttemptPath: docs.stepAttempt,
		AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
	}
	v, err := VerifyDir(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Eligible {
		t.Fatalf("a run with a matching step attempt was not eligible: %+v", v.Findings)
	}
	// A_GH is longer than A, and the difference before AT_start is named.
	if v.ActionGHNs <= v.ActionNs {
		t.Errorf("A_GH %d ns is not longer than A %d ns", v.ActionGHNs, v.ActionNs)
	}
	if v.BootstrapGapNs <= 0 {
		t.Errorf("the pre-AT_start bootstrap was not accounted: %d ns", v.BootstrapGapNs)
	}
	// And it is a DIAGNOSTIC: no gate is named after it.
	for _, g := range v.Gates {
		if strings.Contains(g.Name, "a_gh") || strings.Contains(g.Name, "step") {
			t.Errorf("A_GH became a gate: %s", g.Name)
		}
	}

	for _, tc := range []struct {
		name string
		edit func(*StepAttemptDocument)
		opts func(*VerifyOptions)
		want string
	}{
		{name: "no step attempt at all", opts: func(o *VerifyOptions) { o.StepAttemptPath = "" },
			want: "not linked to the step that ran"},
		{name: "linked to another job", edit: func(d *StepAttemptDocument) { d.Attempt.Job = "some-other-job" },
			want: "the records name job"},
		{name: "linked to another attempt", edit: func(d *StepAttemptDocument) { d.Attempt.Attempt = "7" },
			want: "step attempt"},
		{name: "an envelope outside the step window", edit: func(d *StepAttemptDocument) {
			d.Attempt.StartedAt = "2099-01-01T00:00:00Z"
			d.Attempt.CompletedAt = "2099-01-01T00:01:00Z"
		}, want: "outside the step attempt's precision-widened interval"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := opts
			if tc.edit != nil {
				var doc StepAttemptDocument
				if err := ReadJSONFile(docs.stepAttempt, &doc); err != nil {
					t.Fatal(err)
				}
				tc.edit(&doc)
				path := filepath.Join(t.TempDir(), "step.json")
				if err := WriteJSONFile(path, doc); err != nil {
					t.Fatal(err)
				}
				o.StepAttemptPath = path
			}
			if tc.opts != nil {
				tc.opts(&o)
			}
			got, err := VerifyDir(o)
			if err != nil {
				t.Fatal(err)
			}
			if got.Eligible {
				t.Errorf("the run remained eligible")
			}
			var details []string
			for _, f := range got.Findings {
				details = append(details, f.Detail)
			}
			if !strings.Contains(strings.Join(details, "\n"), tc.want) {
				t.Errorf("no finding mentions %q:\n%s", tc.want, strings.Join(details, "\n"))
			}
		})
	}

	// A sub-second difference can never fail it: that is what "widened by the
	// declared precision" means, and it is why this is not a measurement.
	var doc StepAttemptDocument
	if err := ReadJSONFile(docs.stepAttempt, &doc); err != nil {
		t.Fatal(err)
	}
	shifted := doc
	start, _, err := doc.Attempt.Window()
	if err != nil {
		t.Fatal(err)
	}
	shifted.Attempt.StartedAt = start.Add(900 * time.Millisecond).Format(time.RFC3339Nano)
	path := filepath.Join(t.TempDir(), "shifted.json")
	if err := WriteJSONFile(path, shifted); err != nil {
		t.Fatal(err)
	}
	o := opts
	o.StepAttemptPath = path
	got, err := VerifyDir(o)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Eligible {
		t.Errorf("a sub-second shift inside the declared precision failed the check: %+v", got.Findings)
	}
}

// TestVerifierRefusesARecordThatNamesNoBinary covers the defensive branch
// directly. A Writer always fills the field, so a stream missing it did not
// come from this implementation — and a record that names no build cannot be
// tied to the one Stage 1 approved.
func TestVerifierRefusesARecordThatNamesNoBinary(t *testing.T) {
	v := &Verdict{}
	verifyProducerBinaries(v, InstrumentationIdentity{
		PhysicalBinary: synthBinary, PeerBinary: synthBinary, TraceBinary: synthBinary,
	}, []Record{{Producer: ProducerPhysical, Kind: "boundary"}})
	var details []string
	for _, f := range v.Findings {
		details = append(details, f.Detail)
	}
	if !strings.Contains(strings.Join(details, "\n"), "names no binary") {
		t.Errorf("a record naming no binary was accepted: %v", details)
	}
}

// TestProducerBinaryIsExactNotAPrefix is the identity rule.
//
// The binary digest used to live as a twelve-character fragment inside a
// display string, matched with a substring test. A twelve-hex prefix is
// findable, and a substring test is satisfiable by embedding the fragment
// anywhere at all — so an identity that a collision or an injected label could
// satisfy was not an identity. It is now its own field, compared for exact
// equality.
func TestProducerBinaryIsExactNotAPrefix(t *testing.T) {
	approved := synthBinary
	// A digest sharing the first twelve hex characters: what a prefix
	// collision buys an attacker.
	prefixTwin := Digest(string(approved)[:19] + strings.Repeat("0", len(string(approved))-19))
	if prefixTwin == approved {
		t.Fatalf("the twin is not distinct from the approved digest")
	}
	if string(prefixTwin)[:19] != string(approved)[:19] {
		t.Fatalf("the twin does not share the old short-digest window")
	}

	for _, tc := range []struct {
		name     string
		binary   Digest
		idPrefix string
		want     string
	}{
		{
			name:   "a digest sharing the old twelve-character window",
			binary: prefixTwin,
			want:   "not the",
		},
		{
			name: "the approved digest smuggled into the display name",
			// The old check searched the context string, so putting the
			// digest there was enough. The binary FIELD is what counts now,
			// and the contexts stay distinct so only the smuggling is on
			// trial.
			binary:   prefixTwin,
			idPrefix: "smuggled-" + string(approved) + "-",
			want:     "not the",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s := newSynthRun(filepath.Join(dir, "records"))
			s.producerBinary = tc.binary
			s.producerContextPrefix = tc.idPrefix
			docs := writeFrozenDocs(t, dir, s)
			s.stage2 = docs.digest
			s.write(t, nil)
			v, err := VerifyDir(VerifyOptions{
				Dir: s.dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
				RegistryPath: docs.registry, AetaPath: docs.aeta, PcheckPath: docs.pcheck, ScorerPath: docs.scorer,
				TrainingSetPath: docs.trainingSet, Audit: docs.audit,
				ReplayPath: docs.replay, InvocationsPath: docs.invocations,
				StepAttemptPath: docs.stepAttempt,
				AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
			})
			if err != nil {
				t.Fatal(err)
			}
			if v.Eligible {
				t.Errorf("records from an unapproved build were scored")
			}
			var details []string
			for _, f := range v.Findings {
				details = append(details, f.Detail)
			}
			if !strings.Contains(strings.Join(details, "\n"), tc.want) {
				t.Errorf("no finding mentions %q:\n%s", tc.want, strings.Join(details, "\n"))
			}
		})
	}
}

// A verdict from a host whose scorable clock reads a real epoch must still
// sign. Its instants are then above 2^53, which RFC 8785 canonicalisation
// refuses to render as a number — so an int64 endpoint would make the verdict
// unsignable, and every row from such a host silently uncountable. The
// endpoints are carried as strings for exactly this reason.
func TestVerdictSignsWithEpochScaleInstants(t *testing.T) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	// Well past 2^53 nanoseconds, i.e. any wall-clock epoch after 1970-04-15.
	const epoch = Nanos(1788243122402847000)
	v := Verdict{
		Schema:        SchemaVersion,
		RecordsDigest: "sha256:abc",
		Envelopes: []Envelope{{
			Level:    LevelAction,
			Physical: Interval{StartNs: epoch, EndNs: epoch + Nanos(90*second), OK: true},
			Peer:     Interval{StartNs: epoch + 1, EndNs: epoch + Nanos(90*second) - 1, OK: true},
			Trace:    Interval{StartNs: epoch + 2, EndNs: epoch + Nanos(90*second) - 2, OK: true},
		}},
		Phases: []Phase{{ComponentID: "action_containment_bootstrap", Parent: "action",
			StartNs: epoch, EndNs: epoch + Nanos(20*millisecond)}},
	}
	if err := v.Sign("ewj2-campaign", key); err != nil {
		t.Fatalf("a verdict measured on a real-epoch clock did not sign: %v", err)
	}

	// The exact value must survive the round trip, or the signature the
	// campaign checks would cover different numbers than the ones it reads.
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var got Verdict
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Envelopes[0].Physical.StartNs != epoch {
		t.Errorf("the action start came back as %d, not %d", got.Envelopes[0].Physical.StartNs, epoch)
	}
	d, err := got.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySigned(got.Signature, d, []string{PublicKeyOf(key)}); err != nil {
		t.Errorf("the round-tripped verdict no longer verifies: %v", err)
	}
}

// The whole point of a predeclared signer set: a party that can write the
// records directory must not be able to authenticate what it wrote. Each case
// is a forgery a self-authenticated verifier would have accepted, because the
// forger signs its own records with its own key and the old check verified
// exactly that.
func TestForgedEvidenceIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		// tamper acts on a complete, correctly attested directory.
		tamper func(t *testing.T, dir string, s *synthRun)
		want   string
	}{
		{
			name: "a stream is rewritten after the measurement closed",
			tamper: func(t *testing.T, dir string, s *synthRun) {
				// A consistent rewrite: chain, sequence numbers and signatures
				// all rebuilt, which is what an attacker who owns the
				// directory produces. Only the seal notices.
				path := filepath.Join(dir, streamName(ProducerPhysical, LevelAction, 0))
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "rewritten after the measurement closed",
		},
		{
			name: "a signer nobody declared appends a record",
			tamper: func(t *testing.T, dir string, s *synthRun) {
				key, err := NewSigningKey()
				if err != nil {
					t.Fatal(err)
				}
				w, err := NewWriter(filepath.Join(dir, streamName(ProducerPeer, LevelScript, 0)),
					ProducerPeer, "containment_peer#9.9", key)
				if err != nil {
					t.Fatal(err)
				}
				defer w.Close()
				if _, err := w.Append(Record{
					Kind: "boundary", Role: RolePeerScript, Level: LevelScript, Boundary: "start",
					Source: SourceContainment, Run: s.run(),
				}); err != nil {
					t.Fatal(err)
				}
			},
			want: "neither the roster declared nor the key log registered",
		},
		{
			name: "a key is registered after the seal",
			tamper: func(t *testing.T, dir string, s *synthRun) {
				key, err := NewSigningKey()
				if err != nil {
					t.Fatal(err)
				}
				if err := RegisterKey(dir, KeyLogEntry{
					Producer: ProducerPeer, Level: LevelInvocation, Seq: 0,
					PublicKey: PublicKeyOf(key), Binary: synthBinary,
				}); err != nil {
					t.Fatal(err)
				}
			},
			want: "a signer was added or removed after the measurement closed",
		},
		{
			name: "the roster is replaced by one the forger signed",
			tamper: func(t *testing.T, dir string, s *synthRun) {
				key, err := NewSigningKey()
				if err != nil {
					t.Fatal(err)
				}
				r, err := ReadRoster(dir)
				if err != nil {
					t.Fatal(err)
				}
				if err := r.Sign("ewj2-campaign", key); err != nil {
					t.Fatal(err)
				}
				b, err := json.MarshalIndent(r, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, rosterFile), b, 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "signer roster signature",
		},
		{
			name: "the whole directory is rebuilt with the forger's own keys",
			tamper: func(t *testing.T, dir string, s *synthRun) {
				// The strongest forgery available to a process that owns the
				// directory, and the one the old check could not tell from a
				// real run: every artefact internally consistent, signed
				// throughout, and attested by a key of the forger's own making.
				for _, name := range []string{rosterFile, sealFile} {
					if err := os.Remove(filepath.Join(dir, name)); err != nil {
						t.Fatal(err)
					}
				}
				forged := newSynthRun(dir)
				forged.attest(t)
			},
			want: "signer roster signature",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "records")
			s := newSynthRun(dir)
			docs := writeFrozenDocs(t, t.TempDir(), s)
			s.stage2 = docs.digest
			s.write(t, nil)
			tc.tamper(t, dir, s)

			v, err := VerifyDir(VerifyOptions{
				Dir: dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
				RegistryPath: docs.registry, AetaPath: docs.aeta, PcheckPath: docs.pcheck, ScorerPath: docs.scorer,
				TrainingSetPath: docs.trainingSet, Audit: docs.audit,
				ReplayPath: docs.replay, InvocationsPath: docs.invocations,
				StepAttemptPath: docs.stepAttempt,
				AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
			})
			if err != nil {
				t.Fatal(err)
			}
			if v.Eligible {
				t.Errorf("forged evidence was scored")
			}
			var details []string
			for _, f := range v.Findings {
				details = append(details, f.Detail)
			}
			if !strings.Contains(strings.Join(details, "\n"), tc.want) {
				t.Errorf("no finding mentions %q:\n%s", tc.want, strings.Join(details, "\n"))
			}
		})
	}
}

// A run whose Stage-1 manifest declares no signers cannot be scored: there is
// nothing to check the roster and the seal against, so they authenticate only
// themselves — which is the state this whole mechanism exists to leave.
func TestUndeclaredSignerSetIsNotScorable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "records")
	s := newSynthRun(dir)
	docsDir := t.TempDir()
	docs := writeFrozenDocs(t, docsDir, s)
	s.stage2 = docs.digest
	s.write(t, nil)

	var m Stage1Manifest
	if err := ReadJSONFile(docs.stage1, &m); err != nil {
		t.Fatal(err)
	}
	m.Instrumentation.Signers = nil
	key, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Sign("ewj2-campaign", key); err != nil {
		t.Fatal(err)
	}
	// A new path: these documents are identities, not outputs, so they are
	// written O_EXCL and a variant is a different document.
	stage1 := filepath.Join(docsDir, "stage1-no-signers.json")
	if err := WriteJSONFile(stage1, m); err != nil {
		t.Fatal(err)
	}

	v, err := VerifyDir(VerifyOptions{
		Dir: dir, Stage1Path: stage1, RegistryPath: docs.registry,
		AuthorityKeys: []string{PublicKeyOf(key)}, Authority: "ewj2-campaign",
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Eligible {
		t.Errorf("a run with no declared signer set was scored")
	}
	var details []string
	for _, f := range v.Findings {
		details = append(details, f.Detail)
	}
	if !strings.Contains(strings.Join(details, "\n"), "declares no record signers") {
		t.Errorf("no finding names the undeclared signer set:\n%s", strings.Join(details, "\n"))
	}
}

// "Independent" has to mean a different party, not a different document. A
// replay attestation signed by the key that authorised the plan is the
// planner re-checking its own work, and the contract's separate-party
// requirement is unenforced unless the verifier says so.
func TestReplaySignedByTheAuthorityIsNotIndependent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrange func(t *testing.T, m *Stage1Manifest, authority ed25519.PrivateKey)
		want    string
	}{
		{
			name: "no replay signer is declared at all",
			arrange: func(t *testing.T, m *Stage1Manifest, _ ed25519.PrivateKey) {
				m.Instrumentation.ReplaySigners = nil
			},
			want: "declares no independent replay signer",
		},
		{
			name: "the declared replay signer IS the authority",
			arrange: func(t *testing.T, m *Stage1Manifest, authority ed25519.PrivateKey) {
				m.Instrumentation.ReplaySigners = []string{PublicKeyOf(authority)}
			},
			want: "is also the campaign authority key",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "records")
			s := newSynthRun(dir)
			docsDir := t.TempDir()
			docs := writeFrozenDocs(t, docsDir, s)
			s.stage2 = docs.digest
			s.write(t, nil)

			var m Stage1Manifest
			if err := ReadJSONFile(docs.stage1, &m); err != nil {
				t.Fatal(err)
			}
			authority, err := NewSigningKey()
			if err != nil {
				t.Fatal(err)
			}
			tc.arrange(t, &m, authority)
			if err := m.Sign("ewj2-campaign", authority); err != nil {
				t.Fatal(err)
			}
			stage1 := filepath.Join(docsDir, "stage1-"+strings.ReplaceAll(tc.name, " ", "-")+".json")
			if err := WriteJSONFile(stage1, m); err != nil {
				t.Fatal(err)
			}

			v, err := VerifyDir(VerifyOptions{
				Dir: dir, Stage1Path: stage1, Stage2Path: docs.stage2,
				RegistryPath: docs.registry, ReplayPath: docs.replay,
				AuthorityKeys: []string{PublicKeyOf(authority)}, Authority: "ewj2-campaign",
			})
			if err != nil {
				t.Fatal(err)
			}
			if v.Eligible {
				t.Errorf("a replay that was not independent was scored")
			}
			var details []string
			for _, f := range v.Findings {
				details = append(details, f.Detail)
			}
			if !strings.Contains(strings.Join(details, "\n"), tc.want) {
				t.Errorf("no finding mentions %q:\n%s", tc.want, strings.Join(details, "\n"))
			}
		})
	}
}

// Two buckets of one plan share their Stage-2 receipt, membership digest,
// scorer and registry, so a per-bucket document built for one recomputes
// perfectly inside the other's job and applies the wrong forecast to the wrong
// work. Artifact naming in a workflow is a convention; only the verifier can
// make it a control.
func TestPerBucketDocumentsAreBoundToTheMeasuredBucket(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  func(docs frozenDocs) string
		edit func(m map[string]any)
		want string
	}{
		{
			name: "an invocation manifest for a different bucket",
			doc:  func(d frozenDocs) string { return d.invocations },
			edit: func(m map[string]any) { m["bucket_name"] = "bucket-7"; m["bucket"] = float64(7) },
			want: `the invocation manifest is for bucket "bucket-7"`,
		},
		{
			name: "an invocation manifest that names no bucket",
			doc:  func(d frozenDocs) string { return d.invocations },
			edit: func(m map[string]any) { m["bucket_name"] = "" },
			want: "the invocation manifest names no bucket",
		},
		{
			name: "a Pcheck projection for a different bucket",
			doc:  func(d frozenDocs) string { return d.pcheck },
			edit: func(m map[string]any) { m["bucket_name"] = "bucket-7" },
			want: `the Pcheck projection is for bucket "bucket-7"`,
		},
		{
			name: "a forecast for a different bucket",
			doc:  func(d frozenDocs) string { return d.aeta },
			edit: func(m map[string]any) { m["bucket_id"] = "bucket-7" },
			want: `the pre-action forecast is for bucket "bucket-7"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "records")
			s := newSynthRun(dir)
			docs := writeFrozenDocs(t, t.TempDir(), s)
			s.stage2 = docs.digest
			s.write(t, nil)
			editJSON(t, tc.doc(docs), tc.edit)

			v, err := VerifyDir(VerifyOptions{
				Dir: dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
				RegistryPath: docs.registry, AetaPath: docs.aeta, PcheckPath: docs.pcheck, ScorerPath: docs.scorer,
				TrainingSetPath: docs.trainingSet, Audit: docs.audit,
				ReplayPath: docs.replay, InvocationsPath: docs.invocations,
				StepAttemptPath: docs.stepAttempt,
				AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
			})
			if err != nil {
				t.Fatal(err)
			}
			if v.Eligible {
				t.Errorf("a document for another bucket was scored against this one")
			}
			var details []string
			for _, f := range v.Findings {
				details = append(details, f.Detail)
			}
			if !strings.Contains(strings.Join(details, "\n"), tc.want) {
				t.Errorf("no finding mentions %q:\n%s", tc.want, strings.Join(details, "\n"))
			}
		})
	}
}

// A wall-time envelope measures duration. A script that skipped half its
// targets produces a shorter, perfectly well-formed, fully attested envelope,
// and every gate in this package would pass it. Exact-run coverage is the only
// thing that says what ran, so a scorable row has to carry its answer.
func TestCoverageAuditIsAnEligibilityCondition(t *testing.T) {
	for _, tc := range []struct {
		name     string
		audit    AuditFunc
		terminal bool
		want     string
	}{
		{
			name:  "no audit at all",
			audit: nil,
			want:  "no coverage audit was supplied",
		},
		{
			name: "an audit that could not run",
			audit: func(string) (*AuditEvidence, error) {
				return nil, fmt.Errorf("no events were uploaded for this bucket")
			},
			want: "no events were uploaded for this bucket",
		},
		{
			name: "a bucket that never reported",
			audit: func(bucket string) (*AuditEvidence, error) {
				return &AuditEvidence{Bucket: bucket, Problems: []string{
					"tests/alpha.spec.ts: planned 1 invocation(s), reported 0",
				}}, nil
			},
			terminal: true,
			want:     "planned 1 invocation(s), reported 0",
		},
		{
			name: "a name slice that reached past its own filter",
			audit: func(bucket string) (*AuditEvidence, error) {
				return &AuditEvidence{Bucket: bucket, Problems: []string{
					"tests/alpha.spec.ts: renders fast reported but was in no -run slice",
				}}, nil
			},
			terminal: true,
			want:     "was in no -run slice",
		},
		{
			name: "another bucket's audit",
			audit: func(string) (*AuditEvidence, error) {
				return &AuditEvidence{Bucket: "bucket-7"}, nil
			},
			want: `covers bucket "bucket-7"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "records")
			s := newSynthRun(dir)
			docs := writeFrozenDocs(t, t.TempDir(), s)
			s.stage2 = docs.digest
			s.write(t, nil)

			v, err := VerifyDir(VerifyOptions{
				Dir: dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
				RegistryPath: docs.registry, AetaPath: docs.aeta,
				PcheckPath: docs.pcheck, ScorerPath: docs.scorer, Audit: tc.audit,
				ReplayPath: docs.replay, InvocationsPath: docs.invocations,
				StepAttemptPath: docs.stepAttempt,
				AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
			})
			if err != nil {
				t.Fatal(err)
			}
			if v.Eligible {
				t.Errorf("a row nobody proved ran its plan was scored")
			}
			// A failed audit ends the row: the frozen scope makes it terminal,
			// so it is not a threshold anything can be traded against.
			if tc.terminal && v.Complete {
				t.Errorf("a run that did not execute its plan was reported complete")
			}
			var details []string
			for _, f := range v.Findings {
				details = append(details, f.Detail)
			}
			if !strings.Contains(strings.Join(details, "\n"), tc.want) {
				t.Errorf("no finding mentions %q:\n%s", tc.want, strings.Join(details, "\n"))
			}
		})
	}
}

// A_GH is frozen as an identity/sanity diagnostic that "never enters balance,
// non-regression, prediction, or success calculation". Eligibility IS success
// calculation — a campaign's population is assembled from eligible rows — so a
// whole-second GitHub timestamp must never decide one.
//
// An earlier revision gated the pre-envelope gap on exactly that timestamp.
// This test pins the contract's rule instead: the gap is REPORTED, in full,
// and the row still stands on its physical records.
func TestTheStepDiagnosticNeverDecidesEligibility(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start time.Duration
		want  string
	}{
		{
			name:  "a large prefix before the envelope opened",
			start: -12 * time.Second,
			want:  "precedes AT_start",
		},
		{
			name:  "a prefix inside A_GH's own resolution",
			start: 0,
			want:  "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "records")
			s := newSynthRun(dir)
			docs := writeFrozenDocs(t, t.TempDir(), s)
			s.stage2 = docs.digest
			s.write(t, nil)
			editJSON(t, docs.stepAttempt, func(m map[string]any) {
				attempt := m["attempt"].(map[string]any)
				started, err := time.Parse(time.RFC3339, attempt["started_at"].(string))
				if err != nil {
					t.Fatal(err)
				}
				attempt["started_at"] = started.Add(tc.start).Format(time.RFC3339)
			})

			v, err := VerifyDir(VerifyOptions{
				Dir: dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
				RegistryPath: docs.registry, AetaPath: docs.aeta,
				PcheckPath: docs.pcheck, ScorerPath: docs.scorer,
				TrainingSetPath: docs.trainingSet, Audit: docs.audit,
				ReplayPath: docs.replay, InvocationsPath: docs.invocations,
				StepAttemptPath: docs.stepAttempt,
				AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
			})
			if err != nil {
				t.Fatal(err)
			}
			// The row stands either way: what GitHub reports about the step
			// cannot decide whether the physical envelope is scorable.
			if !v.Eligible {
				var details []string
				for _, f := range v.Findings {
					details = append(details, f.Severity+" "+f.Detail)
				}
				t.Errorf("a whole-second step timestamp made the row ineligible:\n%s", strings.Join(details, "\n"))
			}
			var notes []string
			for _, f := range v.Findings {
				if f.Severity == SeverityNote {
					notes = append(notes, f.Detail)
				}
			}
			joined := strings.Join(notes, "\n")
			if tc.want == "" {
				return
			}
			// Reported in full, as a diagnostic — visible, and not scored.
			if !strings.Contains(joined, tc.want) {
				t.Errorf("the gap was not reported as a diagnostic:\n%s", joined)
			}
		})
	}
}

// A per-bucket document that merely NAMES a Stage-2 digest proves only that
// its author knew the digest, which is public. The receipt is the one document
// that is signed and independently replayed, so the derived documents have to
// be bound into it — otherwise the atom, slice and forecast the bucket actually
// runs against are unbound files travelling beside the frozen plan.
func TestDerivedDocumentsMustBeBoundToStageTwo(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  func(docs frozenDocs) string
		edit func(m map[string]any)
		want string
	}{
		{
			name: "a substituted invocation manifest",
			doc:  func(d frozenDocs) string { return d.invocations },
			edit: func(m map[string]any) {
				// Every field the old checks looked at is untouched: the kind,
				// the Stage-2 string and the bucket all still agree. Only the
				// content differs, which is the whole substitution.
				inv := m["invocations"].([]any)
				m["invocations"] = inv[:1]
			},
			want: "the Stage-2 receipt binds invocations",
		},
		{
			name: "a substituted Pcheck projection",
			doc:  func(d frozenDocs) string { return d.pcheck },
			edit: func(m map[string]any) { m["scorer_id"] = "some-other-run" },
			want: "the Stage-2 receipt binds pcheck",
		},
		{
			name: "a substituted forecast",
			doc:  func(d frozenDocs) string { return d.aeta },
			edit: func(m map[string]any) { m["registry_digest"] = "sha256:elsewhere" },
			want: "the Stage-2 receipt binds aeta",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "records")
			s := newSynthRun(dir)
			docs := writeFrozenDocs(t, t.TempDir(), s)
			s.stage2 = docs.digest
			s.write(t, nil)
			editJSON(t, tc.doc(docs), tc.edit)

			v, err := VerifyDir(VerifyOptions{
				Dir: dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
				RegistryPath: docs.registry, AetaPath: docs.aeta,
				PcheckPath: docs.pcheck, ScorerPath: docs.scorer,
				TrainingSetPath: docs.trainingSet, Audit: docs.audit,
				ReplayPath: docs.replay, InvocationsPath: docs.invocations,
				StepAttemptPath: docs.stepAttempt,
				AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
			})
			if err != nil {
				t.Fatal(err)
			}
			if v.Eligible {
				t.Errorf("a derived document the frozen plan never bound was scored")
			}
			var details []string
			for _, f := range v.Findings {
				details = append(details, f.Detail)
			}
			if !strings.Contains(strings.Join(details, "\n"), tc.want) {
				t.Errorf("no finding mentions %q:\n%s", tc.want, strings.Join(details, "\n"))
			}
		})
	}

	// And a receipt that binds nothing at all: the state every plan produced
	// before this existed, which must not silently pass.
	t.Run("a receipt that binds no derived documents", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "records")
		s := newSynthRun(dir)
		docs := writeFrozenDocs(t, t.TempDir(), s)
		s.stage2 = docs.digest
		s.write(t, nil)
		editJSON(t, docs.stage2, func(m map[string]any) { delete(m, "derived_document_digests") })

		v, err := VerifyDir(VerifyOptions{
			Dir: dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
			RegistryPath: docs.registry, AetaPath: docs.aeta,
			PcheckPath: docs.pcheck, ScorerPath: docs.scorer,
			TrainingSetPath: docs.trainingSet, Audit: docs.audit,
			ReplayPath: docs.replay, InvocationsPath: docs.invocations,
			StepAttemptPath: docs.stepAttempt,
			AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
		})
		if err != nil {
			t.Fatal(err)
		}
		if v.Eligible {
			t.Errorf("a plan that bound none of its derived documents was scored")
		}
		var details []string
		for _, f := range v.Findings {
			details = append(details, f.Detail)
		}
		if !strings.Contains(strings.Join(details, "\n"), "binds no") {
			t.Errorf("no finding names the missing binding:\n%s", strings.Join(details, "\n"))
		}
	})
}

// The coverage audit compares what RAN to what was PLANNED, and the plan
// reaches the verifier as a separate artifact. A plan substituted after Stage 2
// to describe only the work that actually happened produces a complete audit
// while the Stage-2 receipt, its sidecars, the invocation manifest and every
// record stay valid — the invocation manifest says what was invoked and cannot
// say what was left out.
//
// So the audit's plan is bound to the receipt, and a mismatch is TERMINAL: an
// audit against an unauthorised plan is not a weaker audit but a different one.
func TestTheAuditPlanMustBeTheOneStageTwoFroze(t *testing.T) {
	for _, tc := range []struct {
		name     string
		audit    func(docs frozenDocs) AuditFunc
		terminal bool
		want     string
	}{
		{
			name: "a plan substituted after Stage 2",
			audit: func(docs frozenDocs) AuditFunc {
				return func(bucket string) (*AuditEvidence, error) {
					// A clean audit, with no problems at all — which is
					// exactly what the substitution buys.
					return &AuditEvidence{
						Bucket: bucket, PlanDigest: "sha256:the-plan-that-describes-only-what-ran",
						Report: "PASS — every planned package reported exactly the invocations the plan scheduled",
					}, nil
				}
			},
			terminal: true,
			want:     "but Stage 2 froze",
		},
		{
			name: "an audit that reports no plan at all",
			audit: func(docs frozenDocs) AuditFunc {
				return func(bucket string) (*AuditEvidence, error) {
					return &AuditEvidence{Bucket: bucket}, nil
				}
			},
			terminal: true,
			want:     "reports no full-plan digest",
		},
		{
			name: "an audit with no Stage-2 receipt to bind against",
			audit: func(docs frozenDocs) AuditFunc {
				return func(bucket string) (*AuditEvidence, error) {
					return &AuditEvidence{Bucket: bucket, PlanDigest: "sha256:anything"}, nil
				}
			},
			want: "no Stage-2 receipt to bind its plan to",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "records")
			s := newSynthRun(dir)
			docs := writeFrozenDocs(t, t.TempDir(), s)
			s.stage2 = docs.digest
			s.write(t, nil)

			opt := VerifyOptions{
				Dir: dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
				RegistryPath: docs.registry, AetaPath: docs.aeta,
				PcheckPath: docs.pcheck, ScorerPath: docs.scorer, Audit: tc.audit(docs),
				ReplayPath: docs.replay, InvocationsPath: docs.invocations,
				StepAttemptPath: docs.stepAttempt,
				AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
			}
			if tc.want == "no Stage-2 receipt to bind its plan to" {
				opt.Stage2Path = ""
			}
			v, err := VerifyDir(opt)
			if err != nil {
				t.Fatal(err)
			}
			if v.Eligible {
				t.Errorf("a row audited against an unauthorised plan was scored")
			}
			// Audit failure is terminal in the frozen scope, so an audit
			// against the wrong plan cannot be traded against anything.
			if tc.terminal && v.Complete {
				t.Errorf("an audit against an unauthorised plan left the row complete")
			}
			var details []string
			for _, f := range v.Findings {
				details = append(details, f.Detail)
			}
			if !strings.Contains(strings.Join(details, "\n"), tc.want) {
				t.Errorf("no finding mentions %q:\n%s", tc.want, strings.Join(details, "\n"))
			}
		})
	}
}

// The contract puts an owner-authority signature on the planning inputs BEFORE
// the plan exists. A post-run verifier can refuse a row, but it cannot un-run
// an action or restore an approval that never happened — so the approval has
// to be evidenced by the plan itself.
//
// The difficulty is that a detached signature is outside the Stage-1 digest,
// correctly: an unsigned manifest and the same manifest signed afterwards have
// the same digest, so the Stage-2 receipt's Stage1Digest cannot tell them
// apart. The receipt therefore records the approval the planner saw, and this
// is what catches a plan that ran first and was authorised later.
func TestAPlanMustHaveBeenAuthorisedBeforeItExisted(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(t *testing.T, docs frozenDocs)
		want string
	}{
		{
			// The exact attack: plan from an unsigned manifest, then sign it.
			// Stage1Digest still matches, every other binding still holds, and
			// only the absent approval says the order was wrong.
			name: "the manifest was signed after the plan was derived",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.stage2, func(m map[string]any) { delete(m, "stage1_approval") })
			},
			want: "records no pre-plan authority approval",
		},
		{
			name: "the receipt records an approval the manifest does not carry",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.stage2, func(m map[string]any) {
					m["stage1_approval"] = map[string]any{
						"authority": "ewj2-campaign", "key_id": "beef", "signature_digest": "sha256:invented",
					}
				})
			},
			want: "but the supplied Stage-1 manifest is signed by",
		},
		{
			name: "the manifest is unsigned at verification time",
			edit: func(t *testing.T, docs frozenDocs) {
				editJSON(t, docs.stage1, func(m map[string]any) { delete(m, "signature") })
			},
			want: "the Stage-1 manifest is unsigned",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "records")
			s := newSynthRun(dir)
			docs := writeFrozenDocs(t, t.TempDir(), s)
			s.stage2 = docs.digest
			s.write(t, nil)
			tc.edit(t, docs)

			v, err := VerifyDir(VerifyOptions{
				Dir: dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
				RegistryPath: docs.registry, AetaPath: docs.aeta,
				PcheckPath: docs.pcheck, ScorerPath: docs.scorer,
				TrainingSetPath: docs.trainingSet, Audit: docs.audit,
				ReplayPath: docs.replay, InvocationsPath: docs.invocations,
				StepAttemptPath: docs.stepAttempt,
				AuthorityKeys:   []string{docs.authority}, Authority: "ewj2-campaign",
			})
			if err != nil {
				t.Fatal(err)
			}
			if v.Eligible {
				t.Errorf("a plan derived from inputs nobody had approved was scored")
			}
			var details []string
			for _, f := range v.Findings {
				details = append(details, f.Detail)
			}
			if !strings.Contains(strings.Join(details, "\n"), tc.want) {
				t.Errorf("no finding mentions %q:\n%s", tc.want, strings.Join(details, "\n"))
			}
		})
	}
}

// RequireApproval is the pre-plan gate itself, separate from Validate: a
// manifest is VALIDATED while it is being built, before it can be signed, so
// the two questions cannot be the same method.
func TestRequireApprovalIsSeparateFromValidate(t *testing.T) {
	key, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	base := func() Stage1Manifest {
		m := testManifest(testBundle(), "sha256:registry")
		m.Schedule = CampaignSchedule{}
		return m
	}

	// A well-formed unsigned manifest still validates — that is what lets
	// `wall stage1` build one before signing it.
	unsigned := base()
	if err := unsigned.RequireApproval([]string{PublicKeyOf(key)}, "ewj2-campaign"); err == nil {
		t.Errorf("an unsigned manifest was treated as approved")
	} else if !strings.Contains(err.Error(), "is unsigned") {
		t.Errorf("error %q does not say it is unsigned", err)
	}

	for _, tc := range []struct {
		name      string
		signWith  ed25519.PrivateKey
		authority string
		keys      func() []string
		want      string
	}{
		{
			name: "no predeclared key", signWith: key, authority: "ewj2-campaign",
			keys: func() []string { return nil }, want: "no authority key was predeclared",
		},
		{
			name: "signed by an undeclared key", signWith: other, authority: "ewj2-campaign",
			keys: func() []string { return []string{PublicKeyOf(key)} }, want: "authority signature",
		},
		{
			name: "approved by a different environment", signWith: key, authority: "some-other-environment",
			keys: func() []string { return []string{PublicKeyOf(key)} }, want: "not the required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := base()
			if err := m.Sign(tc.authority, tc.signWith); err != nil {
				t.Fatal(err)
			}
			if err := m.RequireApproval(tc.keys(), "ewj2-campaign"); err == nil {
				t.Fatalf("an unapproved manifest passed")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}

	// And the approved case, so the gate is not simply refusing everything.
	good := base()
	if err := good.Sign("ewj2-campaign", key); err != nil {
		t.Fatal(err)
	}
	if err := good.RequireApproval([]string{PublicKeyOf(key)}, "ewj2-campaign"); err != nil {
		t.Errorf("a properly approved manifest was refused: %v", err)
	}
}

// A SCORED ROW MUST NAME THE RUN THAT PRODUCED IT.
//
// verifyRunIdentity asks whether every record AGREES about the identity, and
// blanks agree with blanks perfectly. A row whose every record, roster entry
// and seal repeats an empty workflow attempt was therefore scorable: nothing
// said which attempt, repository, job, step, derived plan or component
// registry produced it, and nothing asked.
//
// That is not a cosmetic gap. The contract requires the action wrapper to link
// one-to-one to an external run-bucket step attempt, and the retained
// population to be ten runs and eighty rows with no retry or replacement.
// Neither can be checked against evidence that does not say which attempt it
// came from, so an underbound row makes the no-retry population unverifiable
// rather than merely under-described.
//
// The producer cannot self-certify this by usually setting its flags: the
// verifier is the trust boundary, so the verifier establishes it.
func TestAScoredRowMustNameTheRunThatProducedIt(t *testing.T) {
	for _, tc := range []struct {
		field string
		blank func(*RunIdentity)
	}{
		{"workflow attempt", func(r *RunIdentity) { r.AttemptID = "" }},
		{"repository", func(r *RunIdentity) { r.Repository = "" }},
		{"workflow run", func(r *RunIdentity) { r.WorkflowRun = "" }},
		{"job", func(r *RunIdentity) { r.Job = "" }},
		{"step", func(r *RunIdentity) { r.Step = "" }},
		{"step attempt", func(r *RunIdentity) { r.StepAttempt = "" }},
		{"Stage-2 digest", func(r *RunIdentity) { r.Stage2 = "" }},
		{"component registry digest", func(r *RunIdentity) { r.ComponentRegistry = "" }},
		{"campaign identity", func(r *RunIdentity) { r.CampaignID = "" }},
		{"run identity", func(r *RunIdentity) { r.RunID = "" }},
		{"verifier identity", func(r *RunIdentity) { r.VerifierID = "" }},
	} {
		t.Run("a row naming no "+tc.field+" is not scorable", func(t *testing.T) {
			dir := t.TempDir()
			s := newSynthRun(filepath.Join(dir, "records"))
			docs := writeFrozenDocs(t, dir, s)
			s.stage2 = docs.digest
			// UNIFORMLY blank, in every record, so the records agree with one
			// another and only the presence rule can refuse the row.
			s.write(t, func(_ Level, _ int, _ Producer, _ string, r *Record) {
				tc.blank(&r.Run)
			})

			v, err := VerifyDir(VerifyOptions{
				Dir: s.dir, Stage1Path: docs.stage1, Stage2Path: docs.stage2,
				RegistryPath: docs.registry, AetaPath: docs.aeta, PcheckPath: docs.pcheck, ScorerPath: docs.scorer,
				TrainingSetPath: docs.trainingSet, Audit: docs.audit,
				ReplayPath: docs.replay, InvocationsPath: docs.invocations,
				AuthorityKeys: []string{docs.authority}, Authority: "ewj2-campaign",
			})
			if err != nil {
				t.Fatal(err)
			}
			if v.Eligible {
				t.Fatalf("a row whose every record agrees about having no %s was scored", tc.field)
			}
			var named bool
			for _, f := range v.Findings {
				if strings.Contains(f.Detail, "names no "+tc.field) {
					named = true
				}
			}
			if !named {
				t.Errorf("the row was refused, but no finding says it names no %s: %+v", tc.field, v.Findings)
			}
		})
	}
}

// AND THE STEP-ATTEMPT CROSS-CHECK CANNOT BE BYPASSED BY OMISSION.
//
// The comparison used to require BOTH sides to be present, so a recorded
// identity that named nothing skipped it: the GitHub document could state the
// repository, workflow run, job, step and attempt, the records could state
// none of them, and the check reported no problem. That is precisely the case
// the cross-check exists for — a ledger that cannot be linked to the step it
// claims to come from — and it was the case that passed.
func TestTheStepAttemptCrossCheckRefusesAnAbsentRecordedIdentity(t *testing.T) {
	api := StepAttempt{
		Repository: "example/mandel", WorkflowRun: "run-1",
		Job: "test", Step: "run-bucket", Attempt: "1",
	}
	for _, tc := range []struct {
		field string
		blank func(*RunIdentity)
	}{
		{"repository", func(r *RunIdentity) { r.Repository = "" }},
		{"workflow run", func(r *RunIdentity) { r.WorkflowRun = "" }},
		{"job", func(r *RunIdentity) { r.Job = "" }},
		{"step", func(r *RunIdentity) { r.Step = "" }},
		{"step attempt", func(r *RunIdentity) { r.StepAttempt = "" }},
	} {
		t.Run("records omitting the "+tc.field+" the step attempt supplies are refused", func(t *testing.T) {
			run := RunIdentity{
				Repository: "example/mandel", WorkflowRun: "run-1",
				Job: "test", Step: "run-bucket", StepAttempt: "1",
			}
			tc.blank(&run)
			problems := api.CheckIdentity(run, Record{}, Record{})
			var named bool
			for _, p := range problems {
				if strings.Contains(p, "the records name none") {
					named = true
				}
			}
			if !named {
				t.Errorf("a recorded identity omitting the %s was accepted against a step attempt that supplies it: %v", tc.field, problems)
			}
		})
	}

	// AND A MATCHING PAIR IS STILL ACCEPTED, so the rule has not become "any
	// cross-check fails".
	t.Run("a complete matching identity raises nothing", func(t *testing.T) {
		run := RunIdentity{
			Repository: "example/mandel", WorkflowRun: "run-1",
			Job: "test", Step: "run-bucket", StepAttempt: "1",
		}
		for _, p := range api.CheckIdentity(run, Record{}, Record{}) {
			if strings.Contains(p, "the records name") {
				t.Errorf("a complete matching identity was faulted: %s", p)
			}
		}
	})
}
