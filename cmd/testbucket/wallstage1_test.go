package main

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// stage1Inputs builds a complete, genuinely signed input set for `wall stage1`
// and returns the argv the command is invoked with.
//
// It exists because the command had never been exercised end to end. Its
// consumers required two fields — the predeclared verdict signers and, for an
// ablation, the authorised stratum — that the producer had no flag for and
// never assigned, so every otherwise complete invocation terminated at the
// manifest's own Validate call. A CLI whose success path nothing runs is a CLI
// whose success path can stop existing without anything saying so.
func stage1Inputs(t *testing.T, extra ...string) []string {
	t.Helper()
	dir := t.TempDir()

	authority, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(walltime.AuthorityKeyEnv, walltime.EncodeKey(authority))

	// The offline surface: a sealed training receipt set and the scorer FITTED
	// from it, so the manifest's refit reproduces the same model rather than
	// comparing a claim with itself.
	trainingKey, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	evidenceKey, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	set := trainingSet(t, trainingKey, evidenceKey)
	scorer, err := walltime.TrainScorer(set, "e2e", []string{walltime.PublicKeyOf(trainingKey)})
	if err != nil {
		t.Fatal(err)
	}

	// The delivered binary and the builder's signed statement about it.
	const tip = "693a19981fb6e0061d3fab62e59d75dc1c01ff3f"
	// THE DELIVERED BINARY IS THE RUNNING ONE. The manifest requires the
	// delivered digest to equal the approved physical/peer/trace/verifier
	// wrapper digests, which is exactly the production arrangement: the binary
	// being delivered is the binary that will do the measuring. Pointing at
	// the test executable keeps that true instead of relaxing the check.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binBytes, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	bin := self
	builderKey, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	att := walltime.BuildAttestation{
		Kind:             walltime.BuildAttestationKind,
		SubjectName:      "testbucket",
		SubjectDigest:    walltime.DigestBytes(binBytes),
		SourceRepository: "invakid404/testbucket",
		SourceCommit:     tip,
		BuilderID:        "invakid404/testbucket/.github/workflows/release.yml@refs/tags/v0.3.0",
		Issuer:           "https://token.actions.githubusercontent.com",
		BuildRun:         "33838202332",
		BuildAttempt:     "1",
		VerifierID:       "ewj2-verifier",
		VerifierBinary:   walltime.DigestBytes([]byte("verifier")),
		VerifierVersion:  "1",
		VerifiedAt:       "2026-09-01T00:00:00Z",
		Result:           walltime.AttestationVerified,
	}
	// Signed UNDER the builder identity it names: the retained builder must be
	// the identity that signed, or the string is signed but attributable to
	// nobody.
	if err := att.Sign(att.BuilderID, builderKey); err != nil {
		t.Fatal(err)
	}
	// The verifier signs the SAME attestation, which is what makes it a party
	// to the delivery rather than a string inside the builder's statement.
	verifierBuildKey, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := att.Countersign(att.VerifierID, verifierBuildKey); err != nil {
		t.Fatal(err)
	}

	for _, n := range []string{"plan", "run-bucket", "record"} {
		p := filepath.Join(dir, "actions", n)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "action.yml"), []byte("name: "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// One frozen store, described by the bundle and by the receipt.
	storeBytes := []byte(`{"schema":1,"flags":"vitest","coverage":["tests/alpha.spec.ts"],` +
		`"units":{"a":{"seconds":1.5,"samples":3},"b":{"seconds":0,"samples":2},"c":{"seconds":0,"samples":0}}}`)

	write := func(name string, v any) string {
		p := filepath.Join(dir, name)
		if err := walltime.WriteJSONFile(p, v); err != nil {
			t.Fatal(err)
		}
		return p
	}
	verdictKey, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	recordKey, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	replayKey, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}

	args := []string{
		"--bundle", write("bundle.json", bundleFor(storeBytes)),
		"--out", filepath.Join(dir, "stage1.json"),
		"--role", "candidate",
		"--actions-dir", filepath.Join(dir, "actions"),
		"--action-commit", tip,
		"--review-tip", tip,
		"--release-sha", tip,
		"--binary", bin,
		"--build-attestation", write("attestation.json", att),
		// Both parties are PREDECLARED: an attestation authenticated by
		// whatever signed it is one anybody can mint.
		"--builder-key", walltime.PublicKeyOf(builderKey),
		"--builder-key", walltime.PublicKeyOf(verifierBuildKey),
		"--source-profile", write("profile.json", sourceProfile(t)),
		// The store the bundle froze, described exactly. The classifications
		// are DERIVED from those same bytes rather than asserted beside them:
		// a measured zero is its own state, not a gap, and a receipt that
		// counted them by hand would be a second unchecked statement of what
		// the store says.
		"--store-receipt", write("store.json", storeReceipt(t, storeBytes)),
		"--scorer", write("scorer.json", scorer),
		"--training-set", write("training.json", set),
		"--training-authority-key", walltime.PublicKeyOf(trainingKey),
		"--registry", write("registry.json", walltime.AetaRegistry{
			Kind: walltime.RegistryKind, Version: "1",
			Components: []walltime.Component{{
				ID: "action_containment_bootstrap", Parent: "action", Owner: "testbucket",
				Class: walltime.ClassActionOnly, Included: true, Formula: walltime.FormulaConstant,
				PointNs: 20_000_000, IntervalNs: 10_000_000,
				// Every physical component carries its own limit. A fixture
				// that omitted it made an unbounded component the shape of a
				// good one.
				BoundNs: 500_000_000,
			}},
		}),
		"--runner-image", "ubuntu-24.04@sha256:" + strings.Repeat("a", 64),
		"--consumer-repository", walltime.FrozenProfileRepository,
		"--consumer-commit", walltime.FrozenProfileCommit,
		"--caller-workflow-sha", "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c",
		"--downstream-ref", "refs/heads/main",
		// The field Validate has always required and the producer never had a
		// flag for.
		"--verdict-signers", walltime.PublicKeyOf(verdictKey),
		"--record-signers", walltime.PublicKeyOf(recordKey),
		"--replay-signers", walltime.PublicKeyOf(replayKey),
	}
	return append(args, extra...)
}

// trainingSet seals one admissible label set, with the real bytes of every
// evidence document it references.
func trainingSet(t *testing.T, seal, evidence ed25519.PrivateKey) walltime.TrainingReceiptSet {
	t.Helper()
	const at = "2026-08-01T00:00:00Z"
	signed := func(doc any, put func(*walltime.Signature)) []byte {
		var d walltime.Digest
		var err error
		switch v := doc.(type) {
		case *walltime.SelectedWorkDocument:
			d, err = v.DigestOf()
		case *walltime.TopologyValidationReceipt:
			d, err = v.DigestOf()
		case *walltime.PhysicalVReceipt:
			d, err = v.DigestOf()
		}
		if err != nil {
			t.Fatal(err)
		}
		put(&walltime.Signature{
			Authority: "ewj2-observation", KeyID: walltime.PublicKeyOf(evidence),
			Digest: d, Value: walltime.SignApproval("ewj2-observation", evidence, d),
		})
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	label := func(id string, runnables float64, ns int64) walltime.TrainingLabel {
		work := &walltime.SelectedWorkDocument{Kind: walltime.SelectedWorkKind, UnitID: id, Units: []string{id + ".spec.ts"}}
		workBytes := signed(work, func(s *walltime.Signature) { work.Signature = s })
		workDigest := walltime.DigestBytes(workBytes)

		topo := &walltime.TopologyValidationReceipt{
			Kind: walltime.TopologyReceiptKind, UnitID: id, SelectedWorkDigest: workDigest,
			Validated: true, Validator: "ewj2-topology",
		}
		topoBytes := signed(topo, func(s *walltime.Signature) { topo.Signature = s })
		topoDigest := walltime.DigestBytes(topoBytes)

		rec := &walltime.PhysicalVReceipt{
			Kind: walltime.PhysicalVReceiptKind, ReceiptID: id, UnitID: id,
			Level: walltime.LevelInvocation, Producer: walltime.ProducerPhysical,
			Source: walltime.SourceContainment,
			Containment: walltime.ContainmentIdentity{
				Primitive: walltime.PrimitiveCgroup2, ID: "tb-" + id, Inode: "9" + id, BootID: "boot-1",
				RootPID: 4242, RootStart: "778899",
				OwnerUID: 1000, OwnerGID: 900, Mode: 0o770,
				WorkloadUID: 1001, WorkloadGIDs: []int{1001},
				MembershipControl: walltime.MembershipSupervisorOwned,
			},
			Terminal: walltime.TerminalPassed, ObservedAt: at, DurationNs: ns,
			SelectedWorkDigest: workDigest, TopologyReceipt: topoDigest,
		}
		recBytes := signed(rec, func(s *walltime.Signature) { rec.Signature = s })

		return walltime.TrainingLabel{
			ReceiptID: id, UnitID: id, Provenance: walltime.LabelProvenance,
			ReceiptHash: walltime.DigestBytes(recBytes), SelectedWorkDigest: workDigest,
			TopologyReceipt: topoDigest, ObservedAt: at, ObservedNs: ns,
			Features: []walltime.Feature{{
				Name: "runnable_count", Value: runnables, Provenance: walltime.ProvRunnableSnapshot,
			}},
			Evidence: &walltime.LabelEvidence{
				ReceiptBytes: recBytes, SelectedWorkBytes: workBytes, TopologyBytes: topoBytes,
			},
		}
	}
	set := walltime.TrainingReceiptSet{
		Kind: walltime.TrainingSetKind, Epoch: "vitest-4.1.10", Cutoff: "2026-08-30T00:00:00Z",
		FeatureSchema: []string{"runnable_count"},
		Algorithm:     "ridge-least-squares", Configuration: "lambda=0.01", Lambda: 0.01, Seed: 1,
		EvidenceSigners: []string{walltime.PublicKeyOf(evidence)},
		Labels: []walltime.TrainingLabel{
			label("h1", 1, 2_000_000_000), label("h2", 2, 3_000_000_000),
			label("h3", 3, 4_000_000_000), label("h4", 4, 5_000_000_000),
		},
	}
	if err := set.Seal("ewj2-training", seal); err != nil {
		t.Fatal(err)
	}
	return set
}

// THE SHIPPED COMMAND CAN AUTHOR THE DOCUMENTS THE CAMPAIGN NEEDS.
//
// `Stage1Manifest.Validate` refuses a manifest with no predeclared verdict
// signer, and the ablation gate refuses one that declares no stratum. Neither
// field had a flag and neither was ever assigned, so there was no production
// path to the Stage-1 document a scored arm needs, and none at all to the
// signed stratum each of the twelve mandatory ablations needs: the campaign
// could not be started through the shipped interface.
func TestWallStage1AuthorsTheDocumentsTheCampaignNeeds(t *testing.T) {
	t.Run("a scored campaign arm", func(t *testing.T) {
		args := stage1Inputs(t, "--campaign-schedule", writeSchedule(t))
		if err := runWallStage1(args); err != nil {
			t.Fatalf("wall stage1 could not author a scored arm: %v", err)
		}
		m := readManifest(t, args)
		if len(m.VerdictSigners) == 0 {
			t.Error("the signed manifest predeclares no verdict signer, which its own validator refuses")
		}
		if err := m.Validate(); err != nil {
			t.Errorf("the manifest the producer wrote is refused by its consumer: %v", err)
		}
	})

	// ALL FOUR STRATA, because the prerequisite is three ablations in each.
	for _, stratum := range walltime.AblationStrata {
		t.Run("an ablation in stratum "+stratum, func(t *testing.T) {
			args := stage1Inputs(t, "--ablation-stratum", stratum)
			if err := runWallStage1(args); err != nil {
				t.Fatalf("wall stage1 could not author a %s ablation: %v", stratum, err)
			}
			m := readManifest(t, args)
			if m.AblationStratum != stratum {
				t.Errorf("the signed manifest authorises stratum %q, want %q", m.AblationStratum, stratum)
			}
			if err := m.Validate(); err != nil {
				t.Errorf("the manifest the producer wrote is refused by its consumer: %v", err)
			}
		})
	}

	// AND THE MODE-AWARE REQUIREMENTS ARE ENFORCED WHERE THEY BELONG.
	t.Run("a scored arm without its campaign schedule is refused", func(t *testing.T) {
		if err := runWallStage1(stage1Inputs(t)); err == nil {
			t.Fatal("a scored arm was authored with no frozen pair order")
		} else if !strings.Contains(err.Error(), "--campaign-schedule") {
			t.Errorf("the refusal does not name the missing input: %v", err)
		}
	})
	t.Run("an invented stratum is refused at the flag", func(t *testing.T) {
		err := runWallStage1(stage1Inputs(t, "--ablation-stratum", "not-a-stratum"))
		if err == nil || !strings.Contains(err.Error(), "not one of the four") {
			t.Errorf("an invented stratum was accepted: %v", err)
		}
	})
}

// writeSchedule freezes the five predeclared pairs, which arm is which, the
// seed and the UTC date each pair runs on — the order the contract requires
// before the first candidate run.
func writeSchedule(t *testing.T) string {
	t.Helper()
	sc := walltime.CampaignSchedule{Kind: walltime.ScheduleKind, CampaignID: "ewj2", Seed: 20260901}
	dates := []string{"2026-09-01", "2026-09-02", "2026-09-03"}
	for i := 0; i < walltime.CampaignPairs; i++ {
		sc.Pairs = append(sc.Pairs, walltime.ScheduledPair{
			Index: i, BaselineRun: fmt.Sprintf("b%d", i), CandidateRun: fmt.Sprintf("c%d", i),
			Date: dates[i%len(dates)],
		})
	}
	p := filepath.Join(t.TempDir(), "schedule.json")
	if err := walltime.WriteJSONFile(p, sc); err != nil {
		t.Fatal(err)
	}
	return p
}

func readManifest(t *testing.T, args []string) walltime.Stage1Manifest {
	t.Helper()
	var out string
	for i, a := range args {
		if a == "--out" && i+1 < len(args) {
			out = args[i+1]
		}
	}
	var m walltime.Stage1Manifest
	if err := walltime.ReadJSONFile(out, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// sourceProfile is the complete, lock-derivable source profile.
//
// The closure is DERIVED from the repository's real lockfile through the same
// exported parser the verifier runs, rather than written out by hand: a
// hand-written closure is a second, unchecked statement of what the lock
// resolves, which is the class of divergence this round closed elsewhere.
func sourceProfile(t *testing.T) walltime.SourceProfileReceipt {
	t.Helper()
	lock, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mandel-lock", "pnpm-lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	closure, err := walltime.DeriveLockClosure(walltime.LockParserPNPM, lock)
	if err != nil {
		t.Fatal(err)
	}
	packages, integrities := map[string]string{}, map[string]string{}
	unpinned := map[string]string{}
	for key, p := range closure {
		packages[key] = p.Version
		if p.Integrity != "" {
			integrities[key] = p.Integrity
			continue
		}
		// A node the lock resolves with no integrity is not pinned. The
		// receipt must DECLARE it rather than let it pass unmentioned, and the
		// declaration names the tarball it resolves to.
		unpinned[key] = p.Tarball
	}
	facade, config := []byte("await import('vitest/node')\n"), []byte("export default {}\n")
	return walltime.SourceProfileReceipt{
		Repository: walltime.FrozenProfileRepository, Commit: walltime.FrozenProfileCommit,
		Facade: walltime.DigestBytes(facade), Config: walltime.DigestBytes(config),
		Lockfile:    walltime.DigestBytes(lock),
		FacadeBytes: facade, ConfigBytes: config, LockfileBytes: lock,
		ParserID: mustParser(t, walltime.LockParserPNPM),
		Packages: packages, Integrities: integrities, UnpinnedNodes: unpinned,
	}
}

func mustParser(t *testing.T, name string) walltime.ParserIdentity {
	t.Helper()
	id, err := walltime.LockParserIdentity(name)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// storeReceipt describes the frozen store bytes, with every count derived from
// those same bytes through the production parser.
func storeReceipt(t *testing.T, raw []byte) walltime.StoreReceipt {
	t.Helper()
	facts, err := walltime.DeriveStoreFacts(raw)
	if err != nil {
		t.Fatal(err)
	}
	return walltime.StoreReceipt{
		Digest: walltime.DigestBytes(raw), Schema: facts.Schema,
		MigrationID: "store/v1", Token: facts.Token,
		CacheKey: "testbucket-timings-e2e-v1", RestoreMethod: "exact-key",
		StaleAt: "2099-01-01T00:00:00Z",
		Classifications: map[string]int{
			walltime.RowObservedPositive: facts.Measured - facts.Zero,
			walltime.RowObservedZero:     facts.Zero,
			// A row with no sample is MISSING; "no tests" is a different
			// state the store records separately.
			walltime.RowMissing: facts.Unmeasured,
		},
		Rows: facts.Rows, Coverage: facts.Coverage,
	}
}

// bundleFor is the frozen planning-input bundle the manifest authorises: the
// clock, the discovery listing and its closure, the store, the plan-digest
// algorithm and parser identities this build implements, and the neutral
// selection configuration.
func bundleFor(storeBytes []byte) walltime.PlanningInputBundle {
	var b walltime.PlanningInputBundle
	b.Kind = walltime.BundleKind
	// Ambient time is prohibited: an unbound clock changes a plan without
	// changing an input.
	b.Clock = walltime.ClockPolicy{
		Policy: "frozen_canonical_instant", Instant: "2026-09-01T00:00:00Z",
		Precision: "1ns", TimeZone: "UTC", PermittedSources: []string{"stage1_bundle"},
		StaleThreshold: "336h0m0s",
	}
	// Every resolved tool, by version, path and integrity: an argv whose
	// closure is not bound is a frozen input whose provenance is invented.
	tools := func(head, path string) map[string]walltime.ToolIdentity {
		return map[string]walltime.ToolIdentity{
			head:   {Version: "4.1.10", Path: path, Integrity: "sha256:9f2e6d33a3717ee826353a404ba4618d1aeeb6879ad7936bce8ed5f46814924d"},
			"node": {Version: "24.19.0", Path: "/usr/bin/node", Integrity: "sha256:545ea538461003efdc8c81c244531b003f6f26cfccf6c0073b3239fdedf49446"},
		}
	}
	disc := walltime.NewRawSnapshot("vitest-list",
		[]string{"vitest", "list", "--filesOnly", "--json"}, "/repo",
		[]byte(`[{"file":"/repo/t0.spec.ts"}]`))
	disc.Env = map[string]string{"TB_DISCOVERY_EXCLUDE_PREFIXES": "shared/f/lib/cases/"}
	disc.Executables = map[string]string{"vitest": "/repo/node_modules/.bin/vitest"}
	disc.Tools = tools("vitest", "/repo/node_modules/.bin/vitest")
	b.Discovery = []walltime.RawSnapshot{disc}

	b.Store = walltime.NewRawSnapshot("test-timings.json", nil, "/repo", storeBytes)
	// The frozen acceptance contract profiles exactly one workload.
	b.Source.Repository = walltime.FrozenProfileRepository
	b.Source.Commit = walltime.FrozenProfileCommit
	b.Source.Tree = "sha256:dc9c5edb8b2d479e697b4b0b8ab874f32b325138598ce9e7b759eb8292110622"
	b.Acquisition.Argv = []string{"testbucket", "wall", "bundle"}
	b.Acquisition.Cwd = "/repo"
	b.Acquisition.Env = map[string]string{"TB_DISCOVERY_EXCLUDE_PREFIXES": "shared/f/lib/cases/"}
	b.Acquisition.Executables = map[string]string{"testbucket": "/usr/local/bin/testbucket"}
	b.Acquisition.Tools = tools("testbucket", "/usr/local/bin/testbucket")
	// The identities of the implementations that would actually run.
	b.Parsers = walltime.ImplementedParserIdentities()
	b.Algorithms.FullPlan = walltime.ImplementedFullPlanAlgorithm()
	b.Algorithms.SemanticPlan = walltime.ImplementedSemanticPlanAlgorithm()
	b.Selection.K = 8
	b.Selection.Count = 1
	// Weights are comparable only within one runner token, so the plan must
	// run the token the store was measured under.
	b.Selection.Token = "vitest"
	b.Selection.Runner = "vitest"
	b.Selection.Renderer = "vitest/v0.2.2"
	b.Selection.TieBreak = "unit_id_ascending"
	b.AbsentInputs = []string{"runnable_snapshots(no name-sliced unit)"}
	return b
}

// THE PLANNER RUNS ONCE, AND THE SECOND INVOCATION IS REFUSED BEFORE IT RUNS.
//
// The exactly-once rule used to be the O_EXCL write of the Stage-2 receipt at
// the END of the frozen plan path. By then the planner had executed and the
// derived documents and shard plan had been written — the shard plan through a
// truncating create — so a second invocation did the work, replaced outputs,
// and only then discovered the receipt it was not allowed to overwrite. That
// is an output-collision check; the contract asks for the second invocation to
// be rejected.
func TestThePlannerClaimRefusesASecondInvocationBeforeItRuns(t *testing.T) {
	// The machine store is redirected, so the suite never writes into the
	// developer's real state directory and one test cannot claim a derivation
	// out from under another.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	const stage1, bundle = walltime.Digest("sha256:ef24c98b6f6843d9d586189733598c533de9fa109464aa1d7045c667a4621b0f"), walltime.Digest("sha256:1e6ed65d77d6364eeaed5a745ba5c4985ae2b700dd85d7cf7f027bdf294a33fc")

	first, err := claimPlannerExecution(dir, stage1, bundle)
	if err != nil {
		t.Fatalf("the first derivation could not claim: %v", err)
	}

	// A SECOND INVOCATION OF THE SAME DERIVATION IS REFUSED. It is refused by
	// the claim, before any planning, and the refusal says which derivation
	// was already claimed rather than which file already existed.
	if _, err := claimPlannerExecution(dir, stage1, bundle); err == nil {
		t.Fatal("a second invocation of the same derivation was allowed to plan")
	} else if !strings.Contains(err.Error(), "already been derived") {
		t.Errorf("the refusal does not say the plan was already derived: %v", err)
	}

	// A DIFFERENT DERIVATION IS NOT BLOCKED: the claim is keyed by identity,
	// not by an output path, so another Stage-1 or another bundle proceeds.
	if _, err := claimPlannerExecution(dir, stage1, walltime.Digest("sha256:72cb9f157914e773011a1c32d55763ef237b88888dcaf36b3bb21ceec2eecc6a")); err != nil {
		t.Errorf("a different derivation was blocked by another's claim: %v", err)
	}
	if _, err := claimPlannerExecution(dir, walltime.Digest("sha256:ec3813d11a45b190e8e5501f714b71d3accfc991d91cce8d218a2c2bcb25f0a8"), bundle); err != nil {
		t.Errorf("a different derivation was blocked by another's claim: %v", err)
	}

	// A CLAIM WHOSE DERIVATION FAILED DOES NOT STAND. Otherwise a planner that
	// crashed would refuse the authorised run's own retry forever.
	first.release()
	if _, err := claimPlannerExecution(dir, stage1, bundle); err != nil {
		t.Errorf("a released claim still blocked its own derivation: %v", err)
	}

	// AND WITH NO CONFIGURED STORE THE MACHINE STORE STILL HOLDS THE CLAIM.
	//
	// This used to assert the opposite — that an unconfigured store simply let
	// the derivation through — because the claim lived in the caller's output
	// directory and an unset one meant no guard at all. The claim is now taken
	// in a store that does not move with the working directory, so a repeat on
	// the same machine is refused whether or not a durable store was
	// configured. Failing to configure one weakens the CROSS-RUNNER guarantee;
	// it no longer removes the local one.
	other := walltime.Digest("sha256:08c4f47566e38c4e5bd906fb164ec38525724222bcedae2bbab892f2ad584210")
	if _, err := claimPlannerExecution("", stage1, other); err != nil {
		t.Fatalf("the first derivation could not claim in the machine store: %v", err)
	}
	if _, err := claimPlannerExecution("", stage1, other); err == nil {
		t.Error("an unconfigured store let the same derivation run twice on one machine")
	}
}

// A SCORED DERIVATION REFUSES TO PLAN WITHOUT A DURABLE CLAIM.
//
// The machine store guards a second invocation on one machine and nothing
// more. A job rerun gets a fresh runner, so for an eligible or scored arm the
// store has to be one the deployment provides and every attempt resolves —
// and the refusal has to come before any planner work.
func TestAScoredDerivationRequiresADurableClaim(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv(plannerClaimStoreEnv, "")

	local, err := claimPlannerExecution("", walltime.Digest("sha256:043a718774c572bd8a25adbeb1bfcd5c0256ae11cecf9f9c3f925d0e52beaf89"), walltime.Digest("sha256:3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d"))
	if err != nil {
		t.Fatal(err)
	}
	if local.receipt.Durable {
		t.Error("a claim taken only in the machine store reported itself durable")
	}
	err = requireDurablePlannerClaim(local)
	if err == nil {
		t.Fatal("a scored derivation was allowed to plan on a machine-local claim")
	}

	if !strings.Contains(err.Error(), "DURABLE") {
		t.Errorf("the refusal does not say a durable claim is required: %v", err)
	}

	// AND A NIL CLAIM IS THE ABSENCE OF THE GUARD, not a pass. This returned
	// success, so the scored path was satisfied whenever the claim machinery
	// had not run at all.
	if err := requireDurablePlannerClaim(plannerClaim{}); err == nil {
		t.Error("the scored guard accepted a claim that was never taken")
	}

	// A PATHNAME ALONE DOES NOT CERTIFY DURABILITY. An arbitrary directory is
	// exactly what a fresh hosted runner also has.
	unattested, uerr := claimPlannerExecution(t.TempDir(), walltime.Digest("sha256:41242b9fae56fad4e6e77dfe33cb18d1c3fc583f988cf25ef9f2d9be0d440bbb"), walltime.Digest("sha256:76a8277347f52530e1cf979175a178980b3a180d176165c985d85f7e142f1eed"))
	if uerr != nil {
		t.Fatal(uerr)
	}
	if unattested.receipt.Durable {
		t.Error("an unattested directory self-certified as durable")
	}

	// WITH ONE ATTESTED, the scored path proceeds and the receipt says so — so
	// a reader of the Stage-2 receipt can tell which guarantee was given.
	//
	// The attestation, not the pathname, is what makes a store durable. A path
	// is something the planner can choose for itself, and the planner is the
	// party whose one-shot behaviour is being constrained; only the authority
	// that provisioned the store can say it is the same one every attempt of
	// the job resolves.
	store := t.TempDir()
	key, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	subject := walltime.PlannerClaimStoreSubject(store)
	t.Setenv(plannerClaimAttestationEnv, walltime.SignApproval(walltime.CampaignAuthority, key, subject))
	t.Setenv(plannerClaimAuthorityKeysEnv, walltime.PublicKeyOf(key))

	durable, err := claimPlannerExecution(store, walltime.Digest("sha256:ad328846aa18b32a335816374511cac1063c704b8c57999e51da9f908290a7a4"), walltime.Digest("sha256:4814d92093ac8a0f4a2163ab87dee509ba306a58f5888be0edcb2fcd0712028b"))
	if err != nil {
		t.Fatal(err)
	}
	if !durable.receipt.Durable {
		t.Error("a claim taken in a configured store did not report itself durable")
	}
	if err := requireDurablePlannerClaim(durable); err != nil {
		t.Errorf("a durable claim was refused: %v", err)
	}
	// It is held in BOTH stores, so the machine-local guard is not lost by
	// configuring an external one.
	if len(durable.paths) != 2 {
		t.Errorf("the claim was taken in %d store(s), want the machine store and the configured one", len(durable.paths))
	}
}

// A DERIVED ARTIFACT IS NOT OVERWRITTEN.
//
// The shard plan was written with a truncating create, so a second run could
// replace the output of the one authorised derivation before anything refused
// it.
func TestADerivedArtifactIsNotOverwritten(t *testing.T) {
	p := filepath.Join(t.TempDir(), "shard-plan.json")
	if err := writeJSONFile(p, map[string]string{"first": "derivation"}); err != nil {
		t.Fatal(err)
	}
	err := writeJSONFile(p, map[string]string{"second": "derivation"})
	if err == nil {
		t.Fatal("a derived artifact was overwritten by a second derivation")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("the refusal does not say the artifact already exists: %v", err)
	}
	// The first derivation's output is intact.
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "first") {
		t.Errorf("the first derivation's output was replaced: %s", b)
	}
}

// THE PUBLISHER STATES WHICH EVIDENCE IT IS GATING ON.
//
// Campaign evidence cannot live in the commit being released — an index that
// binds the release SHA cannot be part of the bytes that SHA is computed over —
// so it is produced outside that commit and carried to the publisher. Once it
// travels, "the file at this path" stops being an identity: a stale or
// substituted index at the same path is indistinguishable from the produced
// evidence. The digest is what makes the handoff verifiable, and it is required
// exactly where the gate authorises a delivery.
func TestThePublishGateRequiresAddressedCampaignEvidence(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "index.json")
	raw := []byte(`{"kind":"tb.walltime.campaign-index/v1","campaign_id":"ewj2"}`)
	if err := os.WriteFile(index, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	const sha = "0000000000000000000000000000000000000000"

	// A DELIVERY-AUTHORISING RUN WITHOUT A STATED DIGEST IS REFUSED, before any
	// evidence is read.
	t.Run("no digest with a release sha", func(t *testing.T) {
		err := runWallCampaign([]string{
			"--index", index, "--authority", "ewj2-campaign",
			"--authority-key", "aa", "--release-sha", sha,
		})
		if err == nil {
			t.Fatal("the gate authorised a delivery without saying which evidence it read")
		}
		if !strings.Contains(err.Error(), "--index-digest is required") {
			t.Errorf("the refusal does not require the digest: %v", err)
		}
	})

	// EVIDENCE THAT IS NOT THE ADDRESSED BYTES IS REFUSED, and the refusal
	// names where it was supposed to have come from.
	t.Run("evidence that does not match the stated digest", func(t *testing.T) {
		err := runWallCampaign([]string{
			"--index", index, "--authority", "ewj2-campaign",
			"--authority-key", "aa", "--release-sha", sha,
			"--index-digest", string(walltime.DigestBytes([]byte("a different index"))),
			"--index-origin", "run/12345/artifact/campaign-evidence",
		})
		if err == nil {
			t.Fatal("a substituted campaign index was evaluated as the produced evidence")
		}
		for _, want := range []string{"digests to", "run/12345/artifact/campaign-evidence"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal does not mention %q: %v", want, err)
			}
		}
	})

	// AND THE ADDRESSED BYTES ARE ACCEPTED FOR EVALUATION. It still fails the
	// campaign itself — the fixture is an empty index — but it gets past the
	// handoff, which is the thing that used to be impossible.
	t.Run("the addressed bytes reach the gate", func(t *testing.T) {
		err := runWallCampaign([]string{
			"--index", index, "--authority", "ewj2-campaign",
			"--authority-key", "aa", "--release-sha", sha,
			"--index-digest", string(walltime.DigestBytes(raw)),
		})
		if err != nil && strings.Contains(err.Error(), "digests to") {
			t.Errorf("the addressed evidence was refused as a mismatch: %v", err)
		}
		if err != nil && strings.Contains(err.Error(), "--index-digest is required") {
			t.Errorf("the addressed evidence was refused for a missing digest: %v", err)
		}
	})
}
