package walltime

import (
	"crypto/ed25519"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// A synthetic run is how the verifier gets tested against a SCORABLE record
// set on a host that cannot produce one. Every field here is what a real
// cgroup-v2 Linux run would write; the readings are chosen rather than
// measured, which is exactly what makes each mutation below a controlled
// experiment.
//
// Building fixtures through the production Writer (not by hand-writing JSON)
// is deliberate: the hash chain, the signatures and the schema come from the
// shipped code, so a fixture cannot drift into agreeing with a verifier bug.

const (
	synthBoot   = "f1c0ffee-0000-4000-8000-000000000001"
	synthStage2 = "sha256:0000000000000000000000000000000000000000000000000000000000000002"
)

// synthBinary is the executable identity the synthetic producers claim. It is
// a real digest rather than a placeholder because Stage 1 binds it and the
// verifier ties every record's execution context back to it.
var synthBinary = DigestBytes([]byte("synthetic testbucket binary"))

// synthContext builds a producer execution context in the production format.
// The binary identity is a separate record field, not a fragment of this
// string, so a test cannot accidentally satisfy the binary check by shaping a
// display name.
func synthContext(p Producer, n int) string {
	return fmt.Sprintf("%s#%d.1", p, n)
}

// synthRun describes one bucket's timeline in nanoseconds from an arbitrary
// origin. Only the shape matters: A contains VB contains each V, and at every
// level the peer brackets the trace.
type synthRun struct {
	dir string
	// invocations is each invocation's [physical start, physical end].
	invocations [][2]int64
	// bootstrapNs is the physical prefix before containment admission at each
	// level; suffixNs is the physical work after verified-empty.
	bootstrapNs int64
	suffixNs    int64
	// peerLeadNs is how far the peer's admission read precedes the trace's,
	// and how far its empty read follows: this is the handshake latency a real
	// run has, and it is what the like-for-like gate measures.
	peerLeadNs int64
	// stage2 is the receipt digest every boundary record binds to.
	stage2 Digest
	// containmentPrimitive lets a test make the run unscorable.
	containmentPrimitive string
	// membershipControl is who may write the containment's cgroup.procs; a
	// test can move it to the shape a same-uid delegation produces.
	membershipControl string
	clockID           string
	// producerBinary is the executable identity every record claims. A test
	// can move it to prove the verifier compares the FULL digest.
	producerBinary Digest
	// producerContextPrefix decorates the display name, so a test can prove
	// that smuggling an approved digest into it changes nothing. It keeps the
	// per-producer suffix, so independence is not disturbed.
	producerContextPrefix string
	// sharedObserverContext writes the peer and the trace from ONE execution
	// context with one key — the shape a single-process implementation
	// produces, and the one the independence check exists to catch.
	sharedObserverContext bool
	// processTree emits the physical wrapper's process-tree record. A test can
	// clear it to model a run that retains no evidence of what it measured.
	processTree bool
	// unauthorizedLowerKeys registers script/invocation signers with no
	// run-key authorization: the shape a measured step can produce for itself.
	unauthorizedLowerKeys bool
	// runKey stands in for the key an Actions step holds and the measured
	// script does not: it signs the roster and the closing seal, exactly as
	// `wall begin` and `wall end` do.
	runKey ed25519.PrivateKey
	// rosterKeys collects the ACTION-level signing keys, which production
	// declares in the roster; everything below action level registers in the
	// key log instead, because it is minted after the roster is sealed.
	rosterKeys []RosterEntry
	// rosterRun and sealRun edit the identity a SIDECAR repeats, leaving the
	// records untouched. Both documents are still signed with the run key over
	// their own digests, so what a test built this way isolates is the
	// semantic invariant — the sidecar belongs to this measurement — rather
	// than signature or chain integrity.
	rosterRun func(*RunIdentity)
	sealRun   func(*RunIdentity)
}

// RunSigner is the public half a Stage-1 manifest must declare for this run's
// roster and seal to be attributable.
func (s *synthRun) RunSigner() string { return PublicKeyOf(s.runKey) }

// keyLogAuthorizer is the key that vouches for lower-level signers. A test can
// clear it to model the measured step registering its own keys, which is what
// the verifier must refuse.
func (s *synthRun) keyLogAuthorizer() ed25519.PrivateKey {
	if s.unauthorizedLowerKeys {
		return nil
	}
	return s.runKey
}

func newSynthRun(dir string) *synthRun {
	return &synthRun{
		dir:                  dir,
		invocations:          [][2]int64{{1_000_000_000, 4_000_000_000}, {4_500_000_000, 6_000_000_000}},
		bootstrapNs:          20_000_000,
		suffixNs:             15_000_000,
		peerLeadNs:           1_000_000,
		stage2:               synthStage2,
		containmentPrimitive: PrimitiveCgroup2,
		membershipControl:    MembershipSupervisorOwned,
		processTree:          true,
		clockID:              ClockMonotonic,
		producerBinary:       synthBinary,
		runKey:               mustSigningKey(),
	}
}

// mustSigningKey mints the fixture's run key. A failure here is a broken test
// environment, not a case under test.
func mustSigningKey() ed25519.PrivateKey {
	k, err := NewSigningKey()
	if err != nil {
		panic(err)
	}
	return k
}

// run is the identity every record, the roster and the seal share. One source
// so the fixture cannot drift the way production must not.
func (s *synthRun) run() RunIdentity {
	return RunIdentity{
		// bucket-1 rather than a fixture-only label: production passes the
		// plan's own bucket NAME as the run's bucket id, and every per-bucket
		// document is now checked against it, so a fixture that used a
		// different spelling would exercise the check against a shape no run
		// produces.
		CampaignID: "ewj2", BucketID: "bucket-1", RunID: "r1",
		Repository: "example/mandel", WorkflowRun: "run-1", Job: "test",
		Step: "run-bucket", StepAttempt: "1",
		// The delivery-bound verifier the row is measured against. The replay
		// attestation must name this same identity, or it is an independent
		// re-derivation of the right plan by somebody nobody bound to this row.
		VerifierID: "synthetic-verifier",
		Stage2:     s.stage2, Stage1: "sha256:0000000000000000000000000000000000000000000000000000000000000001",
	}
}

// spec is invocation i's identity, built once so the records and the
// invocation manifest that checks them cannot drift apart in the fixture the
// way they must not drift apart in production.
func (s *synthRun) spec(i int) SpecIdentity {
	argv := []string{"vitest", "run", fmt.Sprintf("./t%d.spec.ts", i)}
	selector := []string{fmt.Sprintf("t%d.spec.ts", i)}
	return SpecIdentity{
		ArgvDigest:     mustDigest(argv),
		Cwd:            "/repo",
		SelectorDigest: mustDigest(selector),
		UnitDigest:     mustDigest([]string{fmt.Sprintf("t%d.spec.ts", i)}),
		Desc:           fmt.Sprintf("t%d.spec.ts", i),
	}
}

// synthAdmittedPID is a containment member a MUTATION can plant. Production
// never records one at either endpoint: both reads observe an empty
// containment, and a member in the snapshot contradicts the read taken with
// it.
const synthAdmittedPID = 4242

// mutation is applied to a record just before it is written, so a test can
// break exactly one invariant.
type mutation func(level Level, seq int, producer Producer, boundary string, r *Record)

func (s *synthRun) write(t *testing.T, mutate mutation) {
	t.Helper()
	first, last := s.invocations[0], s.invocations[len(s.invocations)-1]
	scriptStart, scriptEnd := first[0]-100_000_000, last[1]+80_000_000
	actionStart, actionEnd := scriptStart-500_000_000, scriptEnd+300_000_000

	s.writeLevel(t, LevelAction, 0, actionStart, actionEnd, nil, mutate)
	s.writeLevel(t, LevelScript, 0, scriptStart, scriptEnd, nil, mutate)
	for i, inv := range s.invocations {
		spec := s.spec(i)
		s.writeLevel(t, LevelInvocation, i, inv[0], inv[1], &spec, mutate)
	}
	s.attest(t)
}

// attest writes the two run-key documents that bracket a real measurement: the
// roster `wall begin` seals before the measured script exists, and the seal
// `wall end` writes over the finished streams.
func (s *synthRun) attest(t *testing.T) {
	t.Helper()
	run := s.run()
	rosterID := run
	if s.rosterRun != nil {
		s.rosterRun(&rosterID)
	}
	roster := Roster{Kind: RosterKind, Run: rosterID, Entries: s.rosterKeys}
	if err := roster.Sign(run.CampaignID, s.runKey); err != nil {
		t.Fatal(err)
	}
	if err := WriteRoster(s.dir, roster); err != nil {
		t.Fatal(err)
	}
	rd, err := roster.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	streams, err := SealStreams(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	_, keyLog, err := ReadKeyLog(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	sealID := run
	if s.sealRun != nil {
		s.sealRun(&sealID)
	}
	seal := Seal{Kind: SealKind, Run: sealID, RosterDigest: rd, KeyLogDigest: keyLog, Streams: streams}
	if err := seal.Sign(run.CampaignID, s.runKey); err != nil {
		t.Fatal(err)
	}
	if err := WriteSeal(s.dir, seal); err != nil {
		t.Fatal(err)
	}
}

// synthContainmentPath is the nested containment path production builds: each
// level's directory inside its parent's. The action is the root of the tree,
// the script sits inside it, and every invocation sits inside the script.
func synthContainmentPath(level Level, seq int) string {
	const root = "/sys/fs/cgroup/testbucket"
	action := root + "/tb-action-0"
	switch level {
	case LevelAction:
		return action
	case LevelScript:
		return fmt.Sprintf("%s/tb-script-%d", action, seq)
	default:
		return fmt.Sprintf("%s/tb-script-0/tb-invocation-%03d", action, seq)
	}
}

// writeLevel emits the three ledgers of one envelope with the contract's
// ordering built in.
func (s *synthRun) writeLevel(t *testing.T, level Level, seq int, start, end int64, spec *SpecIdentity, mutate mutation) {
	t.Helper()
	ident := ContainmentIdentity{
		Primitive: s.containmentPrimitive,
		// NESTED, the way production creates them: newCgroupUnder makes each
		// level's containment a directory inside its parent's, so a script
		// lives under the action and an invocation under its script. The
		// fixture used to emit three SIBLINGS, which no wrapper produces and
		// which cannot exercise the hierarchy the verifier must prove.
		ID:        synthContainmentPath(level, seq),
		Inode:     fmt.Sprintf("%d", 900000+seq+levelRank(level)*10),
		BootID:    synthBoot,
		RootPID:   4242 + seq,
		RootStart: "778899",
		// The membership-control model production establishes by reading the
		// filesystem. A scored containment is one whose `cgroup.procs` the
		// measured workload cannot write.
		MembershipControl: s.membershipControl,
	}
	peerStart, peerEnd := start+s.bootstrapNs, end-s.suffixNs
	traceStart, traceEnd := peerStart+s.peerLeadNs, peerEnd-s.peerLeadNs
	run := s.run()

	type point struct {
		producer Producer
		boundary string
		ns       int64
		source   string
	}
	points := []point{
		{ProducerPhysical, "start", start, SourceWrapper},
		{ProducerPhysical, "end", end, SourceWrapper},
		{ProducerPeer, "start", peerStart, SourceContainment},
		{ProducerPeer, "end", peerEnd, SourceContainment},
		{ProducerTrace, "start", traceStart, SourceContainment},
		{ProducerTrace, "end", traceEnd, SourceContainment},
	}
	byProducer := map[Producer][]point{}
	for _, p := range points {
		byProducer[p.producer] = append(byProducer[p.producer], p)
	}
	sharedKey, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, producer := range []Producer{ProducerPhysical, ProducerPeer, ProducerTrace} {
		key, err := NewSigningKey()
		if err != nil {
			t.Fatal(err)
		}
		context := s.producerContextPrefix + synthContext(producer, levelRank(level)*100+seq)
		if s.sharedObserverContext && producer != ProducerPhysical {
			key = sharedKey
			context = synthContext("observer", levelRank(level)*100+seq)
		}
		w, err := NewWriter(filepath.Join(s.dir, streamName(producer, level, seq)), producer, context, key)
		if err != nil {
			t.Fatal(err)
		}
		// Production declares the action-level keys in the roster and
		// registers everything later in the key log; the fixture does the
		// same, or the verifier's signer check would be exercised against a
		// shape no real run produces.
		if level == LevelAction {
			s.rosterKeys = append(s.rosterKeys, RosterEntry{
				Producer: producer, Level: level, PublicKey: PublicKeyOf(key), Binary: s.producerBinary,
			})
		} else if err := RegisterKeyWith(s.dir, KeyLogEntry{
			Producer: producer, Level: level, Seq: seq,
			PublicKey: PublicKeyOf(key), Binary: s.producerBinary,
			// AUTHORIZED by the run key, which is the shape a producer path
			// holding a capability the measured work does not have produces.
			// A fixture that registered lower-level keys unauthorized would be
			// modelling the measured step attesting itself.
		}, s.keyLogAuthorizer()); err != nil {
			t.Fatal(err)
		}
		for _, p := range byProducer[producer] {
			role, err := RoleFor(producer, level)
			if err != nil {
				t.Fatal(err)
			}
			rec := Record{
				ProducerBinary: s.producerBinary,
				Kind:           "boundary", Role: role, Level: level, Boundary: p.boundary,
				Source: p.source, Seqno: seq, Run: run, Containment: ident,
				Instant: Instant{ClockID: s.clockID, Mono: Nanos(p.ns), BootID: synthBoot,
					Realtime: time.Unix(0, p.ns).UTC().Format(time.RFC3339Nano) + "/" + time.Unix(0, p.ns+1000).UTC().Format(time.RFC3339Nano)},
				Spec: spec,
			}
			if producer != ProducerPhysical {
				// The RETAINED KERNEL BYTES, in exactly the shape the Linux
				// producer emits at each endpoint.
				//
				// BOTH reads report an empty containment, because that is what
				// production observes. The admission read is taken the moment
				// the admit phase opens — before Exec creates the child, into
				// a containment newCgroupUnder just made — so the kernel says
				// `populated 0` and cgroup.procs is empty. The closing read is
				// awaitEmpty's own conclusion, which by construction only
				// returns once the kernel reports unpopulated.
				//
				// An earlier fixture said `populated 1` with a fake member at
				// admission. Nothing in production can produce that, and a
				// fixture describing an impossible sequence cannot establish
				// that a real run verifies — which is exactly how a verifier
				// that refused every genuine scored run passed its own tests.
				rec.RawEventID = fmt.Sprintf("%s:%s:%s:%d", producer, level, p.boundary, seq)
				rec.RawEventBytes = []byte("populated 0\nfrozen 0\n")
				rec.RawEventDigest = DigestBytes(append([]byte(rec.RawEventID+"\x00"), rec.RawEventBytes...))
				// The membership snapshot TAKEN WITH that read: an empty
				// cgroup.procs, its exact bytes, and the digest binding them
				// to this observation. The fixture used to leave RawProcs nil,
				// which is the shape a failed read produces — so it could not
				// tell a proved-empty containment from an unread one.
				rec.RawProcsBytes = []byte("")
				rec.RawProcs, _ = parseCgroupProcs(rec.RawProcsBytes)
				rec.RawProcsDigest = DigestBytes(append([]byte(rec.RawEventID+"\x00"), rec.RawProcsBytes...))
				rec.Phase = lifecyclePhase(level)
			}
			if p.boundary == "end" {
				rec.Terminal = TerminalPassed
			}
			if mutate != nil {
				mutate(level, seq, producer, p.boundary, &rec)
			}
			if _, err := w.Append(rec); err != nil {
				t.Fatal(err)
			}
		}
		// THE PROCESS TREE, which production's physical wrapper writes after
		// the observers close: the measured child's own pid, start identity,
		// process group and parent, with the membership snapshot taken beside
		// it. The fixture emitted none, so a verifier that never read one
		// passed its own tests while proving nothing about what actually ran.
		if producer == ProducerPhysical && s.processTree {
			tree := Record{
				ProducerBinary: s.producerBinary,
				Kind:           "process_tree", Role: roleOrPanic(ProducerPhysical, level), Level: level,
				Source: SourceProcessLifecycle, Seqno: seq, Run: run, Containment: ident,
				Proc: s.childProc(level, seq), RawProcs: []int{},
				Instant: Instant{ClockID: s.clockID, Mono: Nanos(end), BootID: synthBoot,
					Realtime: time.Unix(0, end).UTC().Format(time.RFC3339Nano) + "/" + time.Unix(0, end+1000).UTC().Format(time.RFC3339Nano)},
				Note: "cgroup.procs members at close: 0",
			}
			if mutate != nil {
				mutate(level, seq, ProducerPhysical, "process_tree", &tree)
			}
			if _, err := w.Append(tree); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// childProc is the measured child's identity: distinct from the containment's
// own root process, because the wrapper creates the containment and admits the
// child into it.
func (s *synthRun) childProc(level Level, seq int) ProcIdentity {
	return ProcIdentity{
		PID:       70000 + levelRank(level)*100 + seq,
		PGID:      70000 + levelRank(level)*100 + seq,
		StartID:   fmt.Sprintf("88%d%d", levelRank(level), seq),
		ParentPID: 4242 + seq,
		ExitCode:  0,
	}
}

// actionNs is the complete physical action the fixture describes.
func (s *synthRun) actionNs() int64 {
	first, last := s.invocations[0], s.invocations[len(s.invocations)-1]
	return (last[1] + 80_000_000 + 300_000_000) - (first[0] - 100_000_000 - 500_000_000)
}
