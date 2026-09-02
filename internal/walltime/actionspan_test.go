package walltime

import (
	"fmt"
	"strings"
	"testing"
)

// actionEnvelope is one action envelope and a helper for the readings its four
// step processes take.
func actionEnvelope() Envelope {
	return Envelope{Level: LevelAction, Seq: 0, Containment: ContainmentIdentity{
		Primitive: PrimitiveCgroup2, ID: "/sys/fs/cgroup/testbucket/tb-action-0",
		Inode: "900000", BootID: "boot-1", RootPID: 4242, RootStart: "778899",
		OwnerUID: 1000, OwnerGID: 900, Mode: 0o770,
		WorkloadUID: 1001, WorkloadGIDs: []int{1001},
		MembershipControl: MembershipSupervisorOwned,
	}}
}

// actionReading is a process-tree record for the action level, taken by pid
// from inside the containment.
func actionReading(e Envelope, boundary string, pid int, members ...int) Record {
	r := Record{
		Kind: "process_tree", Boundary: boundary, Producer: ProducerPhysical,
		Role: RolePhysicalAction, Level: LevelAction, Source: SourceProcessLifecycle,
		Containment: e.Containment,
		Proc: ProcIdentity{
			PID: pid, PGID: pid, SessionID: pid, StartID: fmt.Sprintf("%d0", pid),
			ParentPID: 7, UID: 1000, GID: 900, Groups: []int{900},
		},
	}
	var bytes string
	for _, m := range members {
		bytes += fmt.Sprintf("%d\n", m)
	}
	r.RawEventID = "physical:" + boundary
	r.RawEventBytes = []byte("populated 1\nfrozen 0\n")
	r.RawEventDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawEventBytes...))
	r.RawProcsBytes = []byte(bytes)
	r.RawProcs, _ = parseCgroupProcs(r.RawProcsBytes)
	r.RawProcsDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawProcsBytes...))
	return r
}

// TestActionReadingsAreTakenByDifferentWrappers is the F3 regression.
//
// `wall begin`, the setup step, the bucket step and `wall end` are SEPARATE
// step processes that each join the same containment. The verifier applied the
// measured-child transition rules to them, so the closing reading was reported
// as "a different pid is a different process" — terminal, on every production
// action, for doing exactly what the action protocol requires. No process spans
// an action; the CONTAINMENT does, and the readings are the evidence for it.
func TestActionReadingsAreTakenByDifferentWrappers(t *testing.T) {
	e := actionEnvelope()
	v := &Verdict{}
	verifyProcessTree(v, []Envelope{e}, []Record{
		actionReading(e, "start", 81000, 81000, 81500, 81600),
		actionReading(e, "observed", 81001, 81001),
		actionReading(e, "end", 81002),
	})
	for _, f := range v.Findings {
		if strings.Contains(f.Detail, "different pid is a different process") {
			t.Errorf("the action's own step processes were compared as one measured process: %s", f.Detail)
		}
		if strings.Contains(f.Detail, "members") && f.Severity == SeverityTerminal {
			t.Errorf("the action admission read was held to the one-member rule that belongs to a level with a measured child: %s", f.Detail)
		}
	}
	if len(v.Findings) != 0 {
		t.Errorf("a coherent action envelope produced findings: %+v", v.Findings)
	}
}

// TestAnActionReadingTakenFromOutsideIsRefused: with no measured child, the
// only thing tying an action record to a process is that the process was IN
// the containment it reported on.
//
// `wall end` took its closing reading without joining, so the record named a
// process its own membership snapshot did not contain — an observation of a
// containment the observer had never been inside.
func TestAnActionReadingTakenFromOutsideIsRefused(t *testing.T) {
	e := actionEnvelope()
	v := &Verdict{}
	verifyProcessTree(v, []Envelope{e}, []Record{
		actionReading(e, "start", 81000, 81000),
		actionReading(e, "observed", 81001, 81500),
		actionReading(e, "end", 81002),
	})
	if len(findingsMentioning(v, "WT-033", "does not contain")) == 0 {
		t.Errorf("a reading taken from outside the containment was accepted: %+v", v.Findings)
	}
	// And the production step joins before it reads, so the shape above cannot
	// be produced by the wrapper any more.
	body := productionFunc(t, "action.go", "func EndAction(")
	join := strings.Index(body, "joinContainment(st.Containment, os.Getpid())")
	read := strings.Index(body, `retainActionProcessTree(w, st.Run, clock, cont, "observed")`)
	if join < 0 || read < 0 || join > read {
		t.Errorf("EndAction does not join the action containment before taking its closing reading: join=%d read=%d", join, read)
	}
}

// TestActionOwnedChildrenAreVerified is the other half of F3.
//
// `RunInAction` emitted `action_child` records and the verifier consumed only
// `process_tree` ones, so the producer of the containment proof the contract
// asks for before every action-owned child was disconnected from eligibility
// entirely: a setup command that started, forked and vanished left a record no
// verdict ever read.
func TestActionOwnedChildrenAreVerified(t *testing.T) {
	e := actionEnvelope()
	child := func(edit func(*Record)) Record {
		r := actionReading(e, "", 82000, 82000, 81000)
		r.Kind, r.Boundary = "action_child", ""
		r.Note = "action-owned child: npm ci"
		if edit != nil {
			edit(&r)
		}
		return r
	}
	base := []Record{
		actionReading(e, "start", 81000, 81000),
		actionReading(e, "observed", 81001, 81001),
		actionReading(e, "end", 81002),
	}
	for _, tc := range []struct {
		name string
		rec  Record
		want string
	}{
		{"a child of another containment", child(func(r *Record) {
			r.Containment.Inode = "999999"
		}), "not this envelope's"},
		{"a child with no containment proof", child(func(r *Record) {
			r.RawProcs, r.RawProcsBytes, r.RawProcsDigest = nil, nil, ""
		}), "retains no cgroup.procs membership snapshot"},
		{"a child that was never inside", child(func(r *Record) {
			setMembership(r, "81000\n")
		}), "containment proof BEFORE every action-owned child"},
		{"a child with no start identity", child(func(r *Record) {
			r.Proc.StartID = ""
		}), "without the complete start/session/group/parent identity"},
		{"a child that restates the containment's ownership", child(func(r *Record) {
			r.Containment.OwnerUID = 4242
		}), "cgroup.procs owner uid 4242, not this envelope's 1000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := &Verdict{}
			verifyProcessTree(v, []Envelope{e}, append(append([]Record{}, base...), tc.rec))
			if len(findingsMentioning(v, "WT-033", tc.want)) == 0 {
				t.Errorf("%s was accepted: %+v", tc.name, v.Findings)
			}
		})
	}
	// AND THE MEMBERSHIP RULE IS RERUN OVER THE CHILD'S OWN RECORD. A
	// world-writable action containment is one anybody could migrate into,
	// whatever its producer concluded.
	world := actionEnvelope()
	world.Containment.Mode = 0o777
	wv := &Verdict{}
	wc := child(func(r *Record) { r.Containment.Mode = 0o777 })
	verifyProcessTree(wv, []Envelope{world}, []Record{
		actionReading(world, "start", 81000, 81000),
		actionReading(world, "observed", 81001, 81001),
		actionReading(world, "end", 81002),
		wc,
	})
	if len(findingsMentioning(wv, "WT-033", "could write this containment's cgroup.procs")) == 0 {
		t.Errorf("a world-writable action containment was accepted: %+v", wv.Findings)
	}

	// A complete one passes, so the control was added rather than the path
	// broken.
	v := &Verdict{}
	verifyProcessTree(v, []Envelope{e}, append(append([]Record{}, base...), child(nil)))
	if len(v.Findings) != 0 {
		t.Errorf("a complete action-owned child produced findings: %+v", v.Findings)
	}
}

// TestTheActionContainmentIsRederivedLikeEveryOther is the F4 regression at
// the level that used to escape it.
//
// checkProcessTree returned for the action level BEFORE the membership
// rederivation, so an action containment could be world-writable, assert
// supervisor ownership, and be believed — the one containment that spans the
// whole envelope was the one nobody rechecked. The rule now runs first, and it
// runs against the RETAINED workload credential, because at this level there
// is no measured child whose uid could stand in for the workload's.
func TestTheActionContainmentIsRederivedLikeEveryOther(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*ContainmentIdentity)
		want string
	}{
		{"world-writable", func(c *ContainmentIdentity) { c.Mode = 0o777 }, "could write this containment's cgroup.procs"},
		{"owned by the workload itself", func(c *ContainmentIdentity) { c.OwnerUID = c.WorkloadUID }, "could write this containment's cgroup.procs"},
		{"group-writable by a group the workload is in", func(c *ContainmentIdentity) {
			c.WorkloadGIDs = append(c.WorkloadGIDs, c.OwnerGID)
		}, "could write this containment's cgroup.procs"},
		{"a conclusion its own facts contradict", func(c *ContainmentIdentity) {
			c.Mode, c.MembershipControl = 0o777, MembershipSupervisorOwned
		}, "rerunning the rule over its retained owner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := actionEnvelope()
			tc.edit(&e.Containment)
			v := &Verdict{}
			verifyProcessTree(v, []Envelope{e}, []Record{
				actionReading(e, "start", 81000, 81000),
				actionReading(e, "observed", 81001, 81001),
				actionReading(e, "end", 81002),
			})
			if len(findingsMentioning(v, "WT-033", tc.want)) == 0 {
				t.Errorf("an action containment that is %s was accepted: %+v", tc.name, v.Findings)
			}
		})
	}
	// AND WITHOUT RETAINED WORKLOAD FACTS the rule still runs, against the
	// only credential the record does state — fail-closed rather than skipped.
	e := actionEnvelope()
	e.Containment.WorkloadUID, e.Containment.WorkloadGIDs = 0, nil
	e.Containment.OwnerUID = 1000
	v := &Verdict{}
	verifyProcessTree(v, []Envelope{e}, []Record{
		actionReading(e, "start", 81000, 81000),
		actionReading(e, "observed", 81001, 81001),
		actionReading(e, "end", 81002),
	})
	if len(findingsMentioning(v, "WT-033", "could write this containment's cgroup.procs")) == 0 {
		t.Errorf("an action record retaining no workload credential, whose observing wrapper owns the containment, was accepted: %+v", v.Findings)
	}
}

// TestTheActionChildRecordCarriesItsOwnProof: the verifier can only consume
// evidence the producer retains, and the record used to carry a pid and a note.
func TestTheActionChildRecordCarriesItsOwnProof(t *testing.T) {
	body := productionFunc(t, "action.go", "func retainActionChild(")
	for _, want := range []string{
		"cont.Observe(string(ProducerPhysical))",
		"rec.RawProcs, rec.RawProcsBytes, rec.RawProcsDigest",
		"processGroupsOf(pid)",
		"UID: processUIDOf(pid)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the action-child record does not retain %q, so nothing downstream can rederive that the child was ever inside the action containment", want)
		}
	}
}
