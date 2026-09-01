package walltime

import (
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
	clockID              string
	// sharedObserverContext writes the peer and the trace from ONE execution
	// context with one key — the shape a single-process implementation
	// produces, and the one the independence check exists to catch.
	sharedObserverContext bool
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
		clockID:              ClockMonotonic,
	}
}

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
		spec := &SpecIdentity{
			ArgvDigest: mustDigest([]string{"vitest", "run", fmt.Sprintf("./t%d.spec.ts", i)}),
			Cwd:        "/repo",
			Desc:       fmt.Sprintf("t%d.spec.ts", i),
		}
		s.writeLevel(t, LevelInvocation, i, inv[0], inv[1], spec, mutate)
	}
}

// writeLevel emits the three ledgers of one envelope with the contract's
// ordering built in.
func (s *synthRun) writeLevel(t *testing.T, level Level, seq int, start, end int64, spec *SpecIdentity, mutate mutation) {
	t.Helper()
	ident := ContainmentIdentity{
		Primitive: s.containmentPrimitive,
		ID:        fmt.Sprintf("/sys/fs/cgroup/testbucket/tb-%s-%d", level, seq),
		Inode:     fmt.Sprintf("%d", 900000+seq+levelRank(level)*10),
		BootID:    synthBoot,
		RootPID:   4242 + seq,
		RootStart: "778899",
	}
	peerStart, peerEnd := start+s.bootstrapNs, end-s.suffixNs
	traceStart, traceEnd := peerStart+s.peerLeadNs, peerEnd-s.peerLeadNs
	run := RunIdentity{BucketID: "b1", RunID: "r1", Stage2: s.stage2, Stage1: "sha256:0000000000000000000000000000000000000000000000000000000000000001"}

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
		context := fmt.Sprintf("%s@synthetic#%d", producer, levelRank(level)*100+seq)
		if s.sharedObserverContext && producer != ProducerPhysical {
			key = sharedKey
			context = fmt.Sprintf("observer@synthetic#%d", levelRank(level)*100+seq)
		}
		w, err := NewWriter(filepath.Join(s.dir, streamName(producer, level, seq)), producer, context, key)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range byProducer[producer] {
			role, err := RoleFor(producer, level)
			if err != nil {
				t.Fatal(err)
			}
			rec := Record{
				Kind: "boundary", Role: role, Level: level, Boundary: p.boundary,
				Source: p.source, Seqno: seq, Run: run, Containment: ident,
				Instant: Instant{ClockID: s.clockID, Mono: Nanos(p.ns), BootID: synthBoot,
					Realtime: time.Unix(0, p.ns).UTC().Format(time.RFC3339Nano) + "/" + time.Unix(0, p.ns+1000).UTC().Format(time.RFC3339Nano)},
				Spec: spec,
			}
			if producer != ProducerPhysical {
				rec.RawEventID = fmt.Sprintf("%s:%s:%s:%d", producer, level, p.boundary, seq)
				rec.RawEventDigest = DigestBytes([]byte(rec.RawEventID))
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
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// actionNs is the complete physical action the fixture describes.
func (s *synthRun) actionNs() int64 {
	first, last := s.invocations[0], s.invocations[len(s.invocations)-1]
	return (last[1] + 80_000_000 + 300_000_000) - (first[0] - 100_000_000 - 500_000_000)
}
