package walltime

import (
	"encoding/json"
	"fmt"
	"os"
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
	// authority is the PREDECLARED public key of the protected environment.
	// The verifier refuses to treat a signature as authority approval without
	// one, so the fixture has to carry it the way a campaign would.
	authority string
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

	aeta, err := reg.Instantiate(AetaInputs{BucketID: "b1", PallocSeconds: 5.18, Invocations: len(s.invocations), Stage2: r2})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	// The projection is built the way production builds it, so the verifier's
	// recomputation is exercised against a real document rather than a
	// hand-written one that happens to agree.
	palloc := map[string]float64{}
	var invocations []PcheckInvocation
	for i, inv := range s.invocations {
		unit := fmt.Sprintf("t%d.spec.ts", i)
		palloc[unit] = float64(inv[1]-inv[0]+250_000_000) / float64(second)
		invocations = append(invocations, PcheckInvocation{Seq: i, BucketIndex: 1, Units: []string{unit}})
	}
	pcheck, err := BuildPcheck(r2, receipt.MembershipDigest, testScorer(), palloc, invocations)
	if err != nil {
		t.Fatal(err)
	}

	docs := frozenDocs{
		stage1:    filepath.Join(dir, "stage1.json"),
		stage2:    filepath.Join(dir, "stage2.json"),
		registry:  filepath.Join(dir, "registry.json"),
		aeta:      filepath.Join(dir, "aeta.json"),
		pcheck:    filepath.Join(dir, "pcheck.json"),
		digest:    r2,
		authority: m.Signature.KeyID,
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

// testScorer is the frozen scorer the synthetic Pcheck came from; Stage 1
// binds its digest through the training lineage.
func testScorer() Scorer {
	return Scorer{
		Kind: ScorerKind, ID: "synthetic", Version: "1",
		FeatureSchema: []string{"runnable_count"},
		Coefficients:  map[string]float64{"runnable_count": 1},
		Intercept:     1, Floor: 0.1,
		Lineage: TrainingLineageID{
			ReceiptSetDigest: "sha256:sealed", Cutoff: "2026-08-30T00:00:00Z",
			Epoch: "vitest-4.1.10", ScorerID: "synthetic",
			Algorithm: "ridge-least-squares", TieBreak: "unit_id_ascending",
		},
	}
}

func testManifest(b PlanningInputBundle, registry Digest) Stage1Manifest {
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
		Schema: SchemaVersion, PhysicalBinary: synthBinary, PeerBinary: synthBinary,
		TraceBinary: synthBinary, VerifierBinary: synthBinary,
		ContainmentPolicy: "cgroup2-dedicated-subtree", ChildAdmission: "clone-into-cgroup",
		EndpointOrder: "physical<=peer<=trace", CancellationPolicy: "signal-containment-wait-reap",
		RawSourceTaxonomy: []string{SourceContainment, SourceProcessLifecycle, SourceReporter, SourceWrapper},
	}
	m.AllowedDifferences = []string{"testbucket source/action/binary tuple"}
	m.Registry = registry
	sc := testScorer()
	m.TrainingLineage = sc.Lineage
	if d, err := sc.DigestOf(); err == nil {
		m.TrainingLineage.ScorerDigest = d
	}
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
		AuthorityKeys: []string{docs.authority}, Authority: "ewj2-campaign",
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
				RegistryPath: docs.registry, AetaPath: docs.aeta, PcheckPath: docs.pcheck,
				AuthorityKeys: []string{docs.authority}, Authority: "ewj2-campaign",
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
		RegistryPath: docs.registry, AetaPath: docs.aeta, PcheckPath: docs.pcheck,
		AuthorityKeys: []string{docs.authority},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Eligible {
		t.Errorf("records from an unapproved build were scored")
	}
	found := false
	for _, f := range v.Findings {
		if strings.Contains(f.Detail, "not the binary Stage 1 approved") {
			found = true
		}
	}
	if !found {
		t.Errorf("no binary-identity finding: %+v", v.Findings)
	}
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
