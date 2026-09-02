package walltime

import (
	"fmt"
	"strings"
	"testing"
)

// TestTheMeasuredProcessIdentityIsRequired is the F2 regression for the
// verifier half.
//
// WT-029 requires the CONTAINMENT's root process identity, which
// newCgroupUnder fills in from the wrapper's own pid. Nothing asked about the
// process that was actually measured: `runChild` writes a process_tree record
// carrying the child's pid, start identity, process group and parent, and no
// check read it. A signed, otherwise valid run with zero process-tree records
// scored — so the row proved a containment existed and never proved what ran
// inside it.
func TestTheMeasuredProcessIdentityIsRequired(t *testing.T) {
	v := verifySynth(t, nil, func(s *synthRun) { s.processTree = false })
	if len(findingsMentioning(v, "WT-033", "retains no process-tree record")) == 0 {
		t.Fatalf("a run with no process-tree evidence raised no WT-033: %+v", v.Findings)
	}
	if v.Eligible {
		t.Error("a run that never recorded what it measured scored")
	}
}

// TestTheProcessTreeRecordMustBeComplete: each field the contract makes
// decidable — reparenting, a session or PGID change, pid reuse — needs the
// fact it is decided from.
func TestTheProcessTreeRecordMustBeComplete(t *testing.T) {
	tree := func(edit func(*ProcIdentity)) mutation {
		return func(_ Level, _ int, p Producer, boundary string, r *Record) {
			if p == ProducerPhysical && strings.HasPrefix(boundary, "process_tree") {
				edit(&r.Proc)
			}
		}
	}
	for _, tc := range []struct {
		name string
		edit mutation
		want string
	}{
		// THE DEFECT the live sample repairs: on a normal Linux completion the
		// producer read /proc/<pid> after the wait had reaped the child, so
		// this field came back empty on exactly the runs that succeed.
		{"no start identity", tree(func(p *ProcIdentity) { p.StartID = "" }), "no start identity"},
		{"no measured pid", tree(func(p *ProcIdentity) { p.PID = 0 }), "names no measured pid"},
		{"no parent", tree(func(p *ProcIdentity) { p.ParentPID = 0 }), "names no parent"},
		{"no process group", tree(func(p *ProcIdentity) { p.PGID = 0 }), "names no process group"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := verifySynth(t, tc.edit, nil)
			if len(findingsMentioning(v, "WT-033", tc.want)) == 0 {
				t.Errorf("no WT-033 finding mentions %q: %+v", tc.want, v.Findings)
			}
			if v.Eligible {
				t.Errorf("a run with %s scored", tc.name)
			}
		})
	}
}

// TestTheMeasuredProcessIsNotTheContainmentRoot: the wrapper creates the
// containment and admits the child into it, so a record claiming they are one
// process is the wrapper describing itself as the measured work.
func TestTheMeasuredProcessIsNotTheContainmentRoot(t *testing.T) {
	v := verifySynth(t, func(_ Level, _ int, p Producer, boundary string, r *Record) {
		if p == ProducerPhysical && strings.HasPrefix(boundary, "process_tree") {
			r.Proc.PID = r.Containment.RootPID
			r.Proc.StartID = r.Containment.RootStart
		}
	}, nil)
	if len(findingsMentioning(v, "WT-033", "containment's own root process")) == 0 {
		t.Errorf("a wrapper measuring itself raised no WT-033: %+v", v.Findings)
	}
}

// TestAProcessTreeRecordIsBoundToItsEnvelope: a record about another
// envelope's containment does not describe this one's process.
//
// Asserted against checkProcessTree directly. Mutating the record inside a
// whole synthetic run also moves the envelope's own derived containment, so
// the run is refused by the ledger-agreement check before this one is
// reached — and a case that fires a different rule proves a different thing.
func TestAProcessTreeRecordIsBoundToItsEnvelope(t *testing.T) {
	e := Envelope{Level: LevelScript, Seq: 0, Containment: ContainmentIdentity{
		Primitive: PrimitiveCgroup2, ID: "/sys/fs/cgroup/testbucket/tb-action-0/tb-script-0",
		Inode: "900001", BootID: "boot-1", RootPID: 4242, RootStart: "778899",
		MembershipControl: MembershipSupervisorOwned,
	}}
	other := e.Containment
	other.Inode = "999999"
	v := &Verdict{}
	checkProcessTree(v, "script[0]", e, Record{
		Kind: "process_tree", Boundary: "start", Producer: ProducerPhysical,
		Source: SourceProcessLifecycle, Containment: other,
		Proc: ProcIdentity{PID: 70001, PGID: 70001, SessionID: 70001, StartID: "8810", ParentPID: 4242},
	})
	if len(findingsMentioning(v, "WT-033", "not this envelope's")) == 0 {
		t.Errorf("a process-tree record for another containment was accepted: %+v", v.Findings)
	}
	// And the matching one is accepted.
	ok := &Verdict{}
	checkProcessTree(ok, "script[0]", e, Record{
		Kind: "process_tree", Boundary: "start", Producer: ProducerPhysical,
		Source: SourceProcessLifecycle, Containment: e.Containment,
		Proc: ProcIdentity{PID: 70001, PGID: 70001, SessionID: 70001, StartID: "8810", ParentPID: 4242},
	})
	if len(ok.Findings) > 0 {
		t.Errorf("a well-formed process-tree record was refused: %+v", ok.Findings)
	}
}

// TestTheMembershipBesideTheTreeNamesTheMeasuredProcess: a snapshot listing
// members none of which is the process the record describes is describing
// somebody else's containment.
func TestTheMembershipBesideTheTreeNamesTheMeasuredProcess(t *testing.T) {
	v := verifySynth(t, func(_ Level, _ int, p Producer, boundary string, r *Record) {
		if p == ProducerPhysical && boundary == "process_tree:start" {
			setMembership(r, "999999\n")
		}
	}, nil)
	if len(findingsMentioning(v, "WT-033", "none of which is the measured process")) == 0 {
		t.Errorf("a membership snapshot without the measured process was accepted: %+v", v.Findings)
	}
}

// TestTheChildIdentityIsSampledBeforeTheReap is the F2 regression for the
// PRODUCER half, and it is the one that matters: the verifier can only require
// a fact the producer is able to record.
//
// PGID and start id are read from /proc/<pid>, and they used to be read after
// awaitChild — which waits for cmd.Wait, and cmd.Wait reaps. On a normal Linux
// completion the entry is gone by then, so the identity that closes pid reuse
// came back empty on exactly the runs that succeed. The order is asserted from
// the production source, because it is an ordering the type system cannot
// express and a passing run on a host whose /proc behaves differently would
// hide it.
func TestTheChildIdentityIsSampledBeforeTheReap(t *testing.T) {
	body := productionFunc(t, "exec.go", "func runChild(")
	sample := indexOfAll(body, "StartID:   processStartID(cmd.Process.Pid)")
	if len(sample) == 0 {
		t.Fatal("runChild no longer samples the child's start identity")
	}
	wait := indexOf(body, "go func() { done <- cmd.Wait() }()")
	await := indexOf(body, "awaitChild(cont, sigs, done, deadline)")
	if wait < 0 || await < 0 {
		t.Fatalf("runChild's wait is no longer recognisable: wait=%d await=%d", wait, await)
	}
	if sample[0] > wait || sample[0] > await {
		t.Errorf("the child's start identity is first sampled at %d, after the wait at %d/%d; cmd.Wait reaps, and /proc/<pid> is gone once it has",
			sample[0], wait, await)
	}
	admit := indexOf(body, "postSpawnAdmit(cont, cmd.Process.Pid)")
	if admit < 0 || sample[0] < admit {
		t.Errorf("the sample at %d does not follow admission at %d; the facts are taken between admission and the wait", sample[0], admit)
	}
}

func indexOf(hay, needle string) int { return strings.Index(hay, needle) }

func indexOfAll(hay, needle string) []int {
	var out []int
	for at, off := 0, 0; ; {
		at = strings.Index(hay[off:], needle)
		if at < 0 {
			return out
		}
		out = append(out, off+at)
		off += at + len(needle)
	}
}

// TestAnEmptyCloseSnapshotIsNotMembershipProof is the F2 regression.
//
// The wrapper wrote ONE process-tree record, after enforceContainmentEmpty had
// drained the containment and the child had been reaped — so its membership
// was empty by construction, and the verifier only looked at membership when
// the list was non-empty. An empty close snapshot is proof that nothing
// escaped; it is not proof that the measured process was ever inside, and the
// fixture emitted exactly that shape, which is why the committed positive
// tests could not see the gap.
func TestAnEmptyCloseSnapshotIsNotMembershipProof(t *testing.T) {
	// The defect shape: only the drained read survives.
	v := verifySynth(t, func(_ Level, _ int, p Producer, boundary string, r *Record) {
		if p == ProducerPhysical && boundary == "process_tree:start" {
			setMembership(r, "")
			r.RawEventBytes = []byte("populated 0\nfrozen 0\n")
			r.RawEventDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawEventBytes...))
		}
	}, nil)
	if len(findingsMentioning(v, "WT-033", "reports an EMPTY containment at the moment")) == 0 {
		t.Errorf("an empty admission snapshot was accepted as membership proof: %+v", v.Findings)
	}
	if v.Eligible {
		t.Error("a run that never showed the measured process inside its containment scored")
	}
}

// TestBothProcessTreeReadsAreRequired: the admission read and the drained read
// answer different questions, and neither substitutes for the other.
func TestBothProcessTreeReadsAreRequired(t *testing.T) {
	for _, tc := range []struct {
		name string
		drop string
		want string
	}{
		{"no admission read", "start", "taken while the measured process was in the containment"},
		{"no drained read", "end", "taken after the containment drained"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := verifySynth(t, nil, func(s *synthRun) { s.dropProcessTree = tc.drop })
			if len(findingsMentioning(v, "WT-033", tc.want)) == 0 {
				t.Errorf("dropping the %q read raised no WT-033: %+v", tc.drop, v.Findings)
			}
			if v.Eligible {
				t.Errorf("a run with %s scored", tc.name)
			}
		})
	}
}

// TestAProcessTreeRecordMustComeFromThePhysicalProducer: the record is the
// physical wrapper's account of the process it spawned, delimited by an
// independently observed lifecycle event. A trace collector's record, or one
// sourced from a wrapper annotation, is not that account — and both were
// accepted without comment.
func TestAProcessTreeRecordMustComeFromThePhysicalProducer(t *testing.T) {
	e := Envelope{Level: LevelScript, Seq: 0, Containment: ContainmentIdentity{
		Primitive: PrimitiveCgroup2, ID: "/sys/fs/cgroup/tb/script", Inode: "42",
		BootID: "boot", RootPID: 100, RootStart: "1000", MembershipControl: MembershipSupervisorOwned,
	}}
	base := Record{
		Kind: "process_tree", Boundary: "start", Producer: ProducerPhysical, Role: RolePhysicalScript,
		Source: SourceProcessLifecycle, Level: LevelScript, Seqno: 0, Containment: e.Containment,
		Proc:     ProcIdentity{PID: 200, PGID: 200, SessionID: 200, StartID: "2000", ParentPID: 100},
		RawProcs: []int{},
	}
	for _, tc := range []struct {
		name string
		edit func(*Record)
		want string
	}{
		{"a trace collector's record", func(r *Record) { r.Producer = ProducerTrace; r.Role = RoleTraceScript }, "written by"},
		{"a wrapper annotation", func(r *Record) { r.Source = SourceWrapper }, "declares source"},
		{"a reporter annotation", func(r *Record) { r.Source = SourceReporter }, "declares source"},
		{"an unlabelled boundary", func(r *Record) { r.Boundary = "" }, "belongs to none of them"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.edit(&r)
			v := &Verdict{}
			checkProcessTree(v, "script[0]", e, r)
			if len(findingsMentioning(v, "WT-033", tc.want)) == 0 {
				t.Errorf("%s was accepted: %+v", tc.name, v.Findings)
			}
		})
	}
}

// TestTheProcessTreeMembershipIsRetainedEvidence: the physical record carries
// the same raw kernel evidence a peer or trace endpoint does. It used to carry
// a bare list with nothing behind it.
func TestTheProcessTreeMembershipIsRetainedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit mutation
		want string
	}{
		{"no retained bytes or digest", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPhysical && strings.HasPrefix(b, "process_tree") {
				r.RawProcsDigest, r.RawProcsBytes = "", nil
			}
		}, "no raw event id or digest"},

		{"a digest over other bytes", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPhysical && b == "process_tree:start" {
				r.RawProcsDigest = DigestBytes(append([]byte("someone-else\x00"), r.RawProcsBytes...))
			}
		}, "derive"},

		{"bytes outside the kernel grammar", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPhysical && b == "process_tree:start" {
				r.RawProcsBytes = []byte("not-a-pid\n")
				r.RawProcsDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawProcsBytes...))
			}
		}, "not the kernel's grammar"},

		{"a list its own bytes do not name", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPhysical && b == "process_tree:start" {
				r.RawProcs = append(r.RawProcs, 999999)
			}
		}, "retained cgroup.procs bytes name"},

		// More than the admitted child at admission is a process nobody
		// accounted for, which the contract makes terminal.
		{"an unknown descendant already present at admission", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPhysical && b == "process_tree:start" {
				setMembership(r, fmt.Sprintf("%d\n999999\n", r.Proc.PID))
			}
		}, "anything else is a process nobody accounted for"},

		{"a descendant still alive after the drain", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPhysical && b == "process_tree:end" {
				setMembership(r, "999999\n")
			}
		}, "outlived the measured root"},

		{"no session identity", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPhysical && strings.HasPrefix(b, "process_tree") {
				r.Proc.SessionID = 0
			}
		}, "names no session"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := verifySynth(t, tc.edit, nil)
			if len(findingsMentioning(v, "WT-033", tc.want)) == 0 {
				t.Errorf("no WT-033 finding mentions %q: %+v", tc.want, v.Findings)
			}
			if v.Eligible {
				t.Errorf("a run with %s scored", tc.name)
			}
		})
	}
}

// TestAnIdentityTransitionBetweenTheReadsIsTerminal is the F2 regression for
// the independent closing sample.
//
// `runChild` sampled the child's identity once, at admission, and wrote that
// same struct into both records — so two records carried one sample, could not
// disagree, and every transition the contract makes terminal was unobservable
// by construction. The closing identity is now an independent sample taken
// while the process was still alive, and this is what makes that sampling
// load-bearing.
func TestAnIdentityTransitionBetweenTheReadsIsTerminal(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*ProcIdentity)
		want string
	}{
		{"a reparent", func(p *ProcIdentity) { p.ParentPID++ }, "reparenting terminal"},
		{"a session change", func(p *ProcIdentity) { p.SessionID++ }, "session change terminal"},
		{"a process-group change", func(p *ProcIdentity) { p.PGID++ }, "PGID change terminal"},
		{"a start-identity change", func(p *ProcIdentity) { p.StartID += "-moved" }, "reused number"},
		{"a different pid entirely", func(p *ProcIdentity) { p.PID++ }, "a different pid is a different process"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, boundary := range []string{"process_tree:observed", "process_tree:end"} {
				v := verifySynth(t, func(_ Level, _ int, p Producer, b string, r *Record) {
					if p == ProducerPhysical && b == boundary {
						tc.edit(&r.Proc)
					}
				}, nil)
				if len(findingsMentioning(v, "WT-033", tc.want)) == 0 {
					t.Errorf("%s at %s raised no WT-033: %+v", tc.name, boundary, v.Findings)
				}
				if v.Eligible {
					t.Errorf("a run with %s at %s scored", tc.name, boundary)
				}
			}
		})
	}
}

// TestTheProcessTreeEventBytesAreRederived: the record carries a cgroup.events
// read beside its membership, and only the membership digest was rederived —
// so event bytes could be replaced while the digest stayed bound to the
// originals and nothing noticed.
func TestTheProcessTreeEventBytesAreRederived(t *testing.T) {
	v := verifySynth(t, func(_ Level, _ int, p Producer, b string, r *Record) {
		if p == ProducerPhysical && strings.HasPrefix(b, "process_tree:") {
			r.RawEventBytes = []byte("invented event bytes\n")
		}
	}, nil)
	if len(findingsMentioning(v, "WT-033", "retained event bytes derive")) == 0 {
		t.Errorf("event bytes that do not derive the retained digest were accepted: %+v", v.Findings)
	}
	if v.Eligible {
		t.Error("a run whose process-tree event bytes were replaced scored")
	}
	// And an absent event digest is not a pass either.
	v = verifySynth(t, func(_ Level, _ int, p Producer, b string, r *Record) {
		if p == ProducerPhysical && b == "process_tree:start" {
			r.RawEventDigest = ""
		}
	}, nil)
	if len(findingsMentioning(v, "WT-033", "no raw event digest")) == 0 {
		t.Errorf("a process-tree record with no event digest was accepted: %+v", v.Findings)
	}
}

// TestTheAdmissionReadIsTakenUnderAFreeze is the race-free half of F2.
//
// Clone-into-cgroup puts the child in the containment at birth, but the child
// runs from its first instruction — so a membership read taken afterwards
// races a child that may already have forked, and "exactly one member" was an
// assertion the protocol never established. The containment is frozen before
// the spawn, so the child is created stopped and the read observes what was
// admitted. Asserted from the production source, because it is an ordering the
// type system cannot express.
func TestTheAdmissionReadIsTakenUnderAFreeze(t *testing.T) {
	body := productionFunc(t, "exec.go", "func runChild(")
	freeze := strings.Index(body, "cont.Freeze(true)")
	start := strings.Index(body, "cmd.Start()")
	read := strings.Index(body, `retainProcessTree(w, opt, clock, cont, proc, "start")`)
	thaw := strings.Index(body, "cont.Freeze(false); err != nil")
	if freeze < 0 || start < 0 || read < 0 || thaw < 0 {
		t.Fatalf("the admission protocol is no longer recognisable: freeze=%d start=%d read=%d thaw=%d", freeze, start, read, thaw)
	}
	if !(freeze < start && start < read && read < thaw) {
		t.Errorf("the admission read is not taken under the freeze: freeze=%d start=%d read=%d thaw=%d; a child that is running when the read is taken may already have forked",
			freeze, start, read, thaw)
	}
}
