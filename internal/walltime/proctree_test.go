package walltime

import (
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
			if p == ProducerPhysical && boundary == "process_tree" {
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
		if p == ProducerPhysical && boundary == "process_tree" {
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
		Kind: "process_tree", Containment: other,
		Proc: ProcIdentity{PID: 70001, PGID: 70001, StartID: "8810", ParentPID: 4242},
	})
	if len(findingsMentioning(v, "WT-033", "not this envelope's")) == 0 {
		t.Errorf("a process-tree record for another containment was accepted: %+v", v.Findings)
	}
	// And the matching one is accepted.
	ok := &Verdict{}
	checkProcessTree(ok, "script[0]", e, Record{
		Kind: "process_tree", Containment: e.Containment,
		Proc: ProcIdentity{PID: 70001, PGID: 70001, StartID: "8810", ParentPID: 4242},
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
		if p == ProducerPhysical && boundary == "process_tree" {
			r.RawProcs = []int{999999}
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
