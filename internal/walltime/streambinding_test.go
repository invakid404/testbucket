package walltime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sealedSynthRun writes the package's fully bound positive fixture, lets a
// caller rearrange the ledgers on disk, and only THEN takes the closing seal —
// which is what `wall end` does: it seals whatever files the measured step
// left in the evidence directory.
//
// The ordering is the whole point. Everything a record commits to — its own
// hash, its chain, its signature — is committed before the rearrangement, and
// the seal is taken after it. So each of those remains internally valid and
// the seal covers the rearranged layout faithfully. If nothing compares a
// row's stated identity with the file it was read from, the whole set passes.
func sealedSynthRun(t *testing.T, rearrange func(dir string)) (*Verdict, *synthRun) {
	t.Helper()
	dir := t.TempDir()
	s := newSynthRun(filepath.Join(dir, "records"))
	docs := writeFrozenDocs(t, dir, s)
	s.stage2 = docs.digest

	first, last := s.invocations[0], s.invocations[len(s.invocations)-1]
	scriptStart, scriptEnd := first[0]-100_000_000, last[1]+80_000_000
	actionStart, actionEnd := scriptStart-500_000_000, scriptEnd+300_000_000
	s.writeLevel(t, LevelAction, 0, actionStart, actionEnd, nil, nil)
	s.writeLevel(t, LevelScript, 0, scriptStart, scriptEnd, nil, nil)
	for i, inv := range s.invocations {
		spec := s.spec(i)
		s.writeLevel(t, LevelInvocation, i, inv[0], inv[1], &spec, nil)
	}
	if rearrange != nil {
		rearrange(s.dir)
	}
	s.attest(t)

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
	return v, s
}

func renameLedger(t *testing.T, dir, from, to string) {
	t.Helper()
	if err := os.Rename(filepath.Join(dir, from), filepath.Join(dir, to)); err != nil {
		t.Fatal(err)
	}
}

// TestALedgerMustBeTheOneItsRowsName is the F1 regression.
//
// A record signs its own contents and never the name of the file it sits in,
// and the verifier never compared the two. So an evidence set could be written,
// signed, authorized and sealed correctly, and then have two of its ledgers
// exchanged: every hash, chain, signature, roster entry and key-log
// authorization stays valid, the closing seal covers the exchanged layout
// faithfully, and the verifier reports a containment peer's interval as the
// physical wrapper's — complete, eligible, no findings.
func TestALedgerMustBeTheOneItsRowsName(t *testing.T) {
	// THE POSITIVE CONTROL FIRST, so a refusal below means the rearrangement
	// and not the fixture.
	clean, _ := sealedSynthRun(t, nil)
	if !clean.Complete || !clean.Eligible || len(clean.Findings) != 0 {
		t.Fatalf("the untouched fixture is not fully eligible: complete=%v eligible=%v findings=%+v",
			clean.Complete, clean.Eligible, clean.Findings)
	}

	for _, tc := range []struct {
		name      string
		rearrange func(t *testing.T, dir string)
		want      string
	}{
		// THE PRODUCER DIMENSION: the physical wrapper's ledger and its
		// containment peer's, exchanged. This is the swap that made a peer's
		// rows read as the wrapper's.
		{"two producers exchanged", func(t *testing.T, dir string) {
			physical := streamName(ProducerPhysical, LevelScript, 0)
			peer := streamName(ProducerPeer, LevelScript, 0)
			renameLedger(t, dir, physical, physical+".swap")
			renameLedger(t, dir, peer, physical)
			renameLedger(t, dir, physical+".swap", peer)
		}, "claiming producer containment_peer"},

		// THE SEQUENCE DIMENSION: an invocation ledger moved to another
		// invocation's name. Its rows still say which invocation they measured.
		{"an invocation ledger under another sequence", func(t *testing.T, dir string) {
			renameLedger(t, dir,
				streamName(ProducerTrace, LevelInvocation, 0),
				streamName(ProducerTrace, LevelInvocation, 7))
		}, "invocation[0] were read from the ledger"},

		// THE LEVEL DIMENSION: a script ledger under an action name.
		{"a script ledger under an action name", func(t *testing.T, dir string) {
			renameLedger(t, dir,
				streamName(ProducerTrace, LevelScript, 0),
				streamName(ProducerTrace, LevelAction, 9))
		}, "claiming producer trace_collector at script[0]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, _ := sealedSynthRun(t, func(dir string) { tc.rearrange(t, dir) })
			if v.Complete || v.Eligible {
				t.Errorf("a rearranged ledger set was accepted: complete=%v eligible=%v findings=%+v",
					v.Complete, v.Eligible, v.Findings)
			}
			if len(findingsMentioning(v, "WT-034", tc.want)) == 0 {
				t.Errorf("no WT-034 names the mismatch (%q): %+v", tc.want, v.Findings)
			}
			// And the refusal is TERMINAL: a row read from a ledger that is
			// not its own is a claim about a different producer, not a weaker
			// claim about the measurement.
			var terminal bool
			for _, f := range v.Findings {
				if f.Code == "WT-034" && f.Severity == SeverityTerminal {
					terminal = true
				}
			}
			if !terminal {
				t.Errorf("the producer/ledger mismatch is not terminal: %+v", v.Findings)
			}
		})
	}
}

// TestTheLedgerBindingIsCheckedWhereTheChainsAre: the comparison must run on
// the production verification path, not only where a test calls it.
func TestTheLedgerBindingIsCheckedWhereTheChainsAre(t *testing.T) {
	body := productionFunc(t, "verify.go", "func verifyChains(")
	if !strings.Contains(body, "verifyStreamNamesBindTheirRows(v, streams)") {
		t.Error("the ledger-name binding is not checked where every stream is already being verified")
	}
	check := productionFunc(t, "verify.go", "func verifyStreamNamesBindTheirRows(")
	if !strings.Contains(check, "streamName(key.Producer, key.Level, key.Ordinal)") {
		t.Error("the expected ledger name is not derived from the same function that writes it")
	}
	// A record a caller built in memory has no source ledger to disagree with,
	// and must not be refused for having none.
	v := &Verdict{}
	verifyStreamNamesBindTheirRows(v, groupStreams([]Record{
		{Kind: "boundary", Producer: ProducerPeer, Level: LevelScript, Seqno: 0},
	}))
	if len(v.Findings) != 0 {
		t.Errorf("records with no source ledger were refused: %+v", v.Findings)
	}
	// And a correctly named one passes.
	ok := Record{Kind: "boundary", Producer: ProducerPeer, Level: LevelScript, Seqno: 0}
	ok.stream = streamName(ProducerPeer, LevelScript, 0)
	v = &Verdict{}
	verifyStreamNamesBindTheirRows(v, groupStreams([]Record{ok}))
	if len(v.Findings) != 0 {
		t.Errorf("a record read from its own ledger was refused: %+v", v.Findings)
	}
}
