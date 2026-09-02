package walltime

import (
	"fmt"
	"path/filepath"
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
		actionReading(e, "observed", 81001, 81500),
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

// TestTheActionCloserStandsOutsideTheContainmentItDrains is the F1 regression.
//
// `wall end` joined the action containment and then called
// enforceContainmentEmpty on it. That waits for the containment to become
// empty and SIGKILLs every member at the deadline — so a closer that is itself
// a member can never see it empty and kills itself, and the drained read, the
// end boundary, the seal and the cleanup it claims to perform never happen.
// The opening reading is the opposite case: `wall begin` created the
// containment and joined it, so a reader that had not joined would be
// reporting on a container it was never in.
func TestTheActionCloserStandsOutsideTheContainmentItDrains(t *testing.T) {
	e := actionEnvelope()
	v := &Verdict{}
	verifyProcessTree(v, []Envelope{e}, []Record{
		actionReading(e, "start", 81000, 81000),
		// The closer reads a containment that still holds action work, and is
		// not among it.
		actionReading(e, "observed", 81001, 81500),
		actionReading(e, "end", 81002),
	})
	if len(v.Findings) != 0 {
		t.Errorf("a closer standing outside its own drain produced findings: %+v", v.Findings)
	}

	// A CLOSER INSIDE ITS OWN DRAIN is terminal.
	inside := &Verdict{}
	verifyProcessTree(inside, []Envelope{e}, []Record{
		actionReading(e, "start", 81000, 81000),
		actionReading(e, "observed", 81001, 81001, 81500),
		actionReading(e, "end", 81002),
	})
	if len(findingsMentioning(inside, "WT-033", "inside its own drain")) == 0 {
		t.Errorf("a closer that made itself a member of the containment it drains was accepted: %+v", inside.Findings)
	}

	// AND AN OPENING READING FROM OUTSIDE is still refused: `wall begin`
	// creates the containment and joins it before it reads.
	outside := &Verdict{}
	verifyProcessTree(outside, []Envelope{e}, []Record{
		actionReading(e, "start", 81000, 81500),
		actionReading(e, "observed", 81001, 81500),
		actionReading(e, "end", 81002),
	})
	if len(findingsMentioning(outside, "WT-033", "does not contain")) == 0 {
		t.Errorf("an opening reading taken from outside the containment was accepted: %+v", outside.Findings)
	}

	// AND THE PRODUCTION CLOSER DOES NOT JOIN. The wrapper cannot produce the
	// shape above any more, which is the half a verifier rule cannot state.
	body := productionFunc(t, "action.go", "func EndAction(")
	if strings.Contains(body, "joinContainment(st.Containment, os.Getpid())") {
		t.Error("EndAction still joins the containment it is about to drain; it can only time out and cgroup.kill itself")
	}
	if !strings.Contains(body, "enforceContainmentEmpty(cont, deadline)") {
		t.Error("EndAction no longer drains the containment")
	}
	// The OPENING step still joins, because its reading must come from inside.
	begin := productionFunc(t, "action.go", "func BeginAction(")
	if !strings.Contains(begin, "joinContainment(cont.Identity(), os.Getpid())") {
		t.Error("BeginAction no longer joins the containment it reads")
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
		r.Kind, r.Boundary = "action_child", "observed"
		r.Note = "action-owned child: npm ci"
		if edit != nil {
			edit(&r)
		}
		return r
	}
	// The proof committed BEFORE the child existed: it names no process,
	// because there was not one yet, and states what the containment held.
	before := func() Record {
		r := actionReading(e, "", 0, 81000)
		r.Kind, r.Boundary = "action_child", "before"
		r.Proc = ProcIdentity{}
		r.Note = "action-owned child about to start: npm ci"
		return r
	}
	base := []Record{
		actionReading(e, "start", 81000, 81000, 82000),
		actionReading(e, "observed", 81001, 82000),
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
			verifyProcessTree(v, []Envelope{e}, append(append([]Record{}, base...), before(), tc.rec))
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
		actionReading(world, "observed", 81001, 81500),
		actionReading(world, "end", 81002),
		func() Record { r := before(); r.Containment.Mode = 0o777; return r }(),
		wc,
	})
	if len(findingsMentioning(wv, "WT-033", "could write this containment's cgroup.procs")) == 0 {
		t.Errorf("a world-writable action containment was accepted: %+v", wv.Findings)
	}

	// A complete one passes, so the control was added rather than the path
	// broken.
	v := &Verdict{}
	verifyProcessTree(v, []Envelope{e}, append(append([]Record{}, base...), before(), child(nil)))
	if len(v.Findings) != 0 {
		t.Errorf("a complete action-owned child produced findings: %+v", v.Findings)
	}

	// AND A CHILD WITH NO PROOF BEFORE IT is refused, which is the shape the
	// best-effort writer produced whenever anything failed.
	unproved := &Verdict{}
	verifyProcessTree(unproved, []Envelope{e}, append(append([]Record{}, base...), child(nil)))
	if len(findingsMentioning(unproved, "WT-033", "containment proof BEFORE every action-owned child")) == 0 {
		t.Errorf("an action-owned child that ran before anything was committed about it was accepted: %+v", unproved.Findings)
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
				actionReading(e, "observed", 81001, 81500),
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
		actionReading(e, "observed", 81001, 81500),
		actionReading(e, "end", 81002),
	})
	if len(findingsMentioning(v, "WT-033", "could write this containment's cgroup.procs")) == 0 {
		t.Errorf("an action record retaining no workload credential, whose observing wrapper owns the containment, was accepted: %+v", v.Findings)
	}
}

// TestTheActionChildRecordCarriesItsOwnProof: the verifier can only consume
// evidence the producer retains, and the record used to carry a pid and a note.
func TestTheActionChildRecordCarriesItsOwnProof(t *testing.T) {
	body := productionFunc(t, "action.go", "func (c *actionChild) append(")
	for _, want := range []string{
		"c.cont.Observe(string(ProducerPhysical))",
		"rec.RawProcs, rec.RawProcsBytes, rec.RawProcsDigest",
		"processGroupsOf(pid)",
		"UID: processUIDOf(pid)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the action-child record does not retain %q, so nothing downstream can rederive that the child was ever inside the action containment", want)
		}
	}
	// AND NOTHING IS SWALLOWED. Every failure along the way used to be
	// discarded, so a child could run with no retained proof and nothing said
	// so.
	if strings.Contains(body, "if err != nil {\n\t\treturn nil") {
		t.Error("the action-child ledger still discards failures")
	}
	// THE PROOF PRECEDES THE CHILD. The record was written after the spawn,
	// so nothing committed preceded the execution it is proof of.
	run := productionFunc(t, "action.go", "func RunInAction(")
	open := strings.Index(run, "openActionChild(dir, st, argv)")
	start := strings.Index(run, "cmd.Start()")
	if open < 0 || start < 0 {
		t.Fatalf("the action-child production path is not recognizable: open=%d start=%d", open, start)
	}
	if open > start {
		t.Errorf("the containment proof at %d is retained after the child starts at %d", open, start)
	}
}

// TestTwoLedgersDoNotShareOneChain is the reader half of F5.
//
// A hash chain is a property of ONE WRITER'S FILE. The reader grouped records
// by the producer/level/sequence identity they claim instead, so the
// action-child side file — which defaulted to the main action ledger's
// sequence number while starting its own chain — was merged with it and the
// second half "did not chain to its predecessor". Two intact chains were
// reported as one broken one.
func TestTwoLedgersDoNotShareOneChain(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, kind string) {
		w, err := NewWriter(filepath.Join(dir, name), ProducerPhysical, ProducerID(ProducerPhysical), mustSigningKey())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Append(Record{Kind: kind, Level: LevelAction, Seqno: 0}); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	write(streamName(ProducerPhysical, LevelAction, 0), "boundary")
	write("physical_wrapper.action-child.jsonl", "action_child")

	recs, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := &Verdict{}
	verifyChains(v, groupStreams(recs))
	for _, f := range v.Findings {
		if f.Code == "WT-002" {
			t.Errorf("two intact chains were reported as one broken chain: %s", f.Detail)
		}
	}
	// The real problem — two ledgers claiming one stream identity — is
	// reported as itself, so nothing is lost by grouping per file.
	if len(findingsMentioning(v, "WT-020", "claim the stream identity")) == 0 {
		t.Errorf("two ledgers claiming one stream identity went unreported: %+v", v.Findings)
	}

	// And production no longer produces that collision: each action-owned
	// child takes the next sequence number, so it is its own stream.
	body := productionFunc(t, "action.go", "func openActionChild(")
	if !strings.Contains(body, "actionChildSeq(dir) + 1") {
		t.Error("the action-child ledger does not take a sequence number distinct from the action envelope's")
	}
}
