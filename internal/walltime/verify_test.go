package walltime

import (
	"path/filepath"
	"strings"
	"testing"
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
	digest   Digest
}

func writeFrozenDocs(t *testing.T, dir string, s *synthRun) frozenDocs {
	t.Helper()
	bundle := testBundle()
	bd, err := bundle.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	m := testManifest(bundle)
	key, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Sign("ewj2-campaign", key); err != nil {
		t.Fatal(err)
	}
	m1, err := m.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	receipt := testReceipt(m1, bd)
	r2, err := receipt.DigestOf()
	if err != nil {
		t.Fatal(err)
	}

	reg := testRegistry()
	aeta, err := reg.Instantiate(AetaInputs{BucketID: "b1", PallocSeconds: 5.18, Invocations: len(s.invocations), Stage2: r2})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	pcheck := &PcheckDocument{Kind: PcheckKind, Stage2: r2, ScorerID: "synthetic"}
	for i, inv := range s.invocations {
		pcheck.Invocations = append(pcheck.Invocations, PcheckInvocation{
			Seq: i, BucketIndex: 1, Units: []string{"u"}, PredictedNs: inv[1] - inv[0] + 250_000_000,
		})
	}

	docs := frozenDocs{
		stage1:   filepath.Join(dir, "stage1.json"),
		stage2:   filepath.Join(dir, "stage2.json"),
		registry: filepath.Join(dir, "registry.json"),
		aeta:     filepath.Join(dir, "aeta.json"),
		pcheck:   filepath.Join(dir, "pcheck.json"),
		digest:   r2,
	}
	for path, v := range map[string]any{
		docs.stage1: m, docs.stage2: receipt, docs.registry: reg, docs.aeta: aeta, docs.pcheck: pcheck,
	} {
		if err := WriteJSONFile(path, v); err != nil {
			t.Fatal(err)
		}
	}
	return docs
}

func testBundle() PlanningInputBundle {
	var b PlanningInputBundle
	b.Kind = BundleKind
	b.Clock = ClockPolicy{
		Policy: "frozen_canonical_instant", Instant: "2026-08-31T12:00:00Z",
		Precision: "1ns", TimeZone: "UTC", PermittedSources: []string{"stage1_bundle"},
		StaleThreshold: "336h0m0s",
	}
	b.Discovery = []RawSnapshot{NewRawSnapshot("vitest-list", []string{"vitest", "list", "--filesOnly", "--json"}, "/repo", []byte(`[{"file":"/repo/t0.spec.ts"}]`))}
	b.Store = NewRawSnapshot("test-timings.json", nil, "/repo", []byte(`{"flags":"vitest","units":{}}`))
	b.Source.Repository = "example/mandel"
	b.Source.Commit = "d9ae1d433bb45012c04d567879b66fc4bf6112c6"
	b.Source.Tree = "sha256:tree"
	b.Acquisition.Argv = []string{"testbucket", "wall", "bundle"}
	b.Acquisition.Cwd = "/repo"
	b.Acquisition.Env = map[string]string{"TB_DISCOVERY_EXCLUDE_PREFIXES": "shared/f/lib/cases/"}
	b.Acquisition.Executables = map[string]string{"node": "/usr/bin/node"}
	b.Acquisition.Tools = map[string]string{"node": "24.19.0"}
	b.Parsers = []ParserIdentity{{Name: "vitest-discovery", Version: "v0.2.2", Digest: "sha256:parser"}}
	b.Algorithms.FullPlan = AlgorithmIdentity{Name: FullPlanDigestAlgorithm, Canonicalizer: CanonAlgorithm, Implementation: "testbucket"}
	b.Algorithms.SemanticPlan = AlgorithmIdentity{Name: SemanticPlanDigestAlgorithm, Canonicalizer: CanonAlgorithm, Implementation: "testbucket"}
	b.Selection.K = 8
	b.Selection.Count = 1
	b.Selection.Token = "vitest"
	b.Selection.Runner = "vitest"
	b.Selection.Renderer = "vitest/v0.2.2"
	b.Selection.TieBreak = "unit_id_ascending"
	b.AbsentInputs = []string{"runnable_snapshots(no name-sliced unit)"}
	return b
}

func testManifest(b PlanningInputBundle) Stage1Manifest {
	m := Stage1Manifest{Kind: Stage1Kind, Role: "candidate", Bundle: b}
	m.Actions = map[string]ActionIdentity{
		"plan":       {Commit: "693a1998", ContentDigest: "sha256:plan"},
		"run-bucket": {Commit: "693a1998", ContentDigest: "sha256:run"},
		"record":     {Commit: "693a1998", ContentDigest: "sha256:record"},
	}
	m.Source.ReviewTip = "693a19981fb6e0061d3fab62e59d75dc1c01ff3f"
	m.Source.BinaryDigest = "sha256:binary"
	m.Source.BuildAttestation = "attestation"
	m.SourceProfile = SourceProfileReceipt{
		Repository: "example/mandel", Commit: "d9ae1d43", Facade: "sha256:facade",
		Config: "sha256:config", Lockfile: "sha256:lock",
		ParserID:    ParserIdentity{Name: "pnpm-lock", Version: "9", Digest: "sha256:lockparser"},
		Packages:    map[string]string{"vitest": RequiredVitest, "@vitest/runner": RequiredVitest},
		Integrities: map[string]string{"vitest": "sha512-a", "@vitest/runner": "sha512-b"},
	}
	m.Instrumentation = InstrumentationIdentity{
		Schema: SchemaVersion, PhysicalBinary: "sha256:tb", PeerBinary: "sha256:tb",
		TraceBinary: "sha256:tb", VerifierBinary: "sha256:tb",
		ContainmentPolicy: "cgroup2-dedicated-subtree", ChildAdmission: "clone-into-cgroup",
		EndpointOrder: "physical<=peer<=trace", CancellationPolicy: "signal-containment-wait-reap",
		RawSourceTaxonomy: []string{SourceContainment, SourceProcessLifecycle, SourceReporter, SourceWrapper},
	}
	m.AllowedDifferences = []string{"testbucket source/action/binary tuple"}
	m.Registry = "sha256:registry"
	return m
}

func testReceipt(stage1, bundle Digest) Stage2Receipt {
	r := Stage2Receipt{
		Kind: Stage2Kind, Stage1Digest: stage1, BundleDigest: bundle,
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
	c := func(id, parent string, class ComponentClass, included bool, point int64) Component {
		return Component{ID: id, Parent: parent, Owner: "testbucket", Class: class, Included: included,
			Formula: FormulaConstant, Inputs: []string{"stage1"}, PointNs: point,
			IntervalNs: 100 * millisecond}
	}
	return AetaRegistry{
		Kind: RegistryKind, Version: "1",
		Components: []Component{
			c("action_containment_bootstrap", "action", ClassActionOnly, true, 20*millisecond),
			c("action_prologue", "action", ClassActionOnly, true, 480*millisecond),
			{ID: "bucket_script", Parent: "action", Owner: "testbucket", Class: ClassPalloc, Included: true,
				Formula: FormulaPallocSum, Inputs: []string{"palloc"}, IntervalNs: 2 * second},
			c("action_epilogue_flush", "action", ClassActionOnly, true, 285*millisecond),
			c("action_suffix", "action", ClassActionOnly, true, 15*millisecond),
			c("script_containment_bootstrap", "script", ClassActionOnly, false, 20*millisecond),
			c("between_invocation_gap", "script", ClassActionOnly, false, 80*millisecond),
			{ID: "invocation", Parent: "script", Owner: "testbucket", Class: ClassPalloc, Included: false,
				Formula: FormulaPallocSum, Inputs: []string{"palloc"}},
			c("script_epilogue", "script", ClassActionOnly, false, 65*millisecond),
			c("script_suffix", "script", ClassActionOnly, false, 15*millisecond),
			c("invocation_bootstrap", "invocation", ClassActionOnly, false, 20*millisecond),
			{ID: "invocation_containment", Parent: "invocation", Owner: "testbucket", Class: ClassPalloc, Included: false,
				Formula: FormulaPallocSum, Inputs: []string{"palloc"}},
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
	for _, g := range v.Gates {
		if !g.Pass {
			t.Errorf("gate %s failed: required %s, observed %s (%s)", g.Name, g.Required, g.Observed, g.Detail)
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
