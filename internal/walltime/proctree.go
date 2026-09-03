package walltime

import (
	"fmt"
	"strings"
)

// verifyProcessTree requires the measured child's OWN process identity.
//
// The containment identity answers "which container", and WT-029 makes its
// root process identity required and agreed. Neither says anything about the
// process that was actually measured: `runChild` writes a `process_tree`
// record carrying the child's pid, its start identity, its process group and
// its parent, and nothing read it. A signed, otherwise valid run with ZERO
// process-tree records scored — so the row proved a containment existed and
// never proved what ran inside it.
//
// The contract binds PID/start identity and membership at every level, and
// makes reparenting, a session or PGID change, and an unknown descendant
// terminal. None of those is decidable without the child's identity, so this
// requires it to be present, complete, and consistent with the containment it
// was measured in.
func verifyProcessTree(v *Verdict, envs []Envelope, recs []Record) {
	trees := map[string][]Record{}
	children := map[string][]Record{}
	for _, r := range recs {
		switch r.Kind {
		case "process_tree":
			trees[envelopeKey(r.Level, r.Seqno)] = append(trees[envelopeKey(r.Level, r.Seqno)], r)
		case "action_child":
			// KEYED BY LEVEL, NOT BY SEQNO. Each action-owned child writes its
			// own stream with its own sequence number so its chain is not
			// merged with the action ledger's; the envelope it belongs to is
			// still the action envelope, which is the one this level has.
			children[string(r.Level)] = append(children[string(r.Level)], r)
		}
	}
	for _, e := range envs {
		if e.Containment.Primitive != PrimitiveCgroup2 {
			continue
		}
		label := fmt.Sprintf("%s[%d]", e.Level, e.Seq)
		found := trees[envelopeKey(e.Level, e.Seq)]
		if len(found) == 0 {
			v.add("WT-033", SeverityIneligible, fmt.Sprintf(
				"%s retains no process-tree record, so nothing states the pid, start identity, process group or parent of the process that was measured; the envelope proves a containment existed and not what ran inside it", label))
			continue
		}
		var admitted, observed, drained []Record
		for _, r := range found {
			checkProcessTree(v, label, e, r)
			switch r.Boundary {
			case "start":
				admitted = append(admitted, r)
			case "observed":
				observed = append(observed, r)
			case "end":
				drained = append(drained, r)
			}
		}
		// BOTH ENDS, and they answer different questions.
		//
		// The wrapper used to write ONE process-tree record, after the
		// containment had been drained and the child reaped — so its
		// membership was empty by construction, and an empty snapshot is proof
		// that nothing escaped, never proof that the measured process was ever
		// inside. The admitted read is the historical membership; the drained
		// read is the closure. Neither substitutes for the other.
		if len(admitted) == 0 {
			v.add("WT-033", SeverityIneligible, fmt.Sprintf(
				"%s retains no process-tree record taken while the measured process was in the containment; an empty snapshot read after the drain says nothing escaped, not that anything was ever admitted", label))
		}
		if len(drained) == 0 {
			v.add("WT-033", SeverityIneligible, fmt.Sprintf(
				"%s retains no process-tree record taken after the containment drained, so nothing states that it ended empty", label))
		}
		if len(observed) == 0 {
			v.add("WT-033", SeverityIneligible, fmt.Sprintf(
				"%s retains no process-tree record of the measured process as it was LAST OBSERVED; the admission read alone cannot show a reparent, a session change, a process-group change or a descendant that lived and exited inside the interval", label))
		}
		for _, r := range admitted {
			checkAdmittedMembership(v, label, r)
		}
		for _, r := range observed {
			requireMembershipEvidence(v, label+" observed process tree", r)
		}
		for _, r := range drained {
			checkDrainedMembership(v, label, r)
		}
		// THE IDENTITY MUST NOT HAVE MOVED.
		//
		// The wrapper wrote one sample into two records, so they could not
		// disagree and every transition the contract makes terminal was
		// unobservable. The closing identity is now an independent sample, and
		// this is what makes that sampling load-bearing: a measured process
		// that changed parent, session, process group or start identity
		// between admission and its last observation is not the process the
		// envelope admitted.
		//
		// AT THE ACTION LEVEL IT IS THE CONTAINMENT THAT SPANS, and the
		// records are readings taken by four different step processes. Running
		// the transition check there compared two wrappers that were never
		// meant to be the same process and made every production action row
		// terminal; what that level must show instead is one unbroken
		// containment read from inside it, plus the children it owned.
		if e.Level == LevelAction {
			checkActionSpan(v, label, e, found)
			checkActionChildren(v, label, e, children[string(e.Level)])
			continue
		}
		for _, first := range admitted {
			for _, later := range append(append([]Record{}, observed...), drained...) {
				checkIdentityTransition(v, label, first.Proc, later.Proc, later.Boundary)
			}
		}
	}
}

// checkAdmittedMembership adjudicates the record taken WHILE the measured
// process was inside: the historical membership the contract asks for.
func checkAdmittedMembership(v *Verdict, label string, r Record) {
	where := label + " admitted process tree"
	if !requireMembershipEvidence(v, where, r) {
		return
	}
	if len(r.RawProcs) == 0 {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s reports an EMPTY containment at the moment the measured process was supposed to be inside it; the admission read is the only evidence that anything was ever admitted", where))
		return
	}
	var inside bool
	for _, pid := range r.RawProcs {
		if pid == r.Proc.PID {
			inside = true
		}
	}
	if !inside {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s lists members %v, none of which is the measured process %d", where, r.RawProcs, r.Proc.PID))
	}
	// EXACT CARDINALITY, WHERE THERE IS AN ADMITTED CHILD. At admission a
	// script or invocation containment holds that child and nothing else: the
	// wrapper does not join it, and the child has not run long enough to fork.
	// More members than that is an unknown descendant already present, which
	// the contract makes terminal.
	//
	// The ACTION containment is not that shape and never was. `wall begin`
	// joins the containment it created and admits the containment peer and the
	// trace collector into it before it reads — three members by construction,
	// on every production action — so requiring one here made the level
	// terminal for doing exactly what the contract tells it to do. What that
	// level must show is that the reading was taken from inside, which
	// checkActionSpan requires of every action record.
	if r.Level != LevelAction && len(r.RawProcs) != 1 {
		v.add("WT-033", SeverityTerminal, fmt.Sprintf(
			"%s lists %d members (%v) at admission; the containment is created fresh and holds exactly the admitted child, so anything else is a process nobody accounted for",
			where, len(r.RawProcs), r.RawProcs))
	}
}

// checkDrainedMembership adjudicates the closing read: the containment ended
// empty, and that is retained rather than asserted.
func checkDrainedMembership(v *Verdict, label string, r Record) {
	where := label + " drained process tree"
	if !requireMembershipEvidence(v, where, r) {
		return
	}
	if len(r.RawProcs) > 0 {
		v.add("WT-033", SeverityTerminal, fmt.Sprintf(
			"%s still lists %d member(s) %v after the drain; a descendant that outlived the measured root is the escape this endpoint exists to catch", where, len(r.RawProcs), r.RawProcs))
	}
}

// requireMembershipEvidence rederives the retained kernel evidence a
// process-tree record carries, exactly as the peer and trace endpoints are
// rederived. A record whose membership is a list with nothing behind it is a
// claim, and the physical producer's record used to carry no raw bytes or
// digest at all.
func requireMembershipEvidence(v *Verdict, where string, r Record) bool {
	if r.RawProcs == nil {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s retains no cgroup.procs membership snapshot; a read that never happened is not an observation", where))
		return false
	}
	if r.RawEventID == "" || r.RawProcsDigest == "" {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s lists a membership snapshot with no raw event id or digest binding it to an observation", where))
		return false
	}
	// THE EVENT BYTES TOO. The record carries a cgroup.events read beside its
	// membership, and only the membership digest was rederived — so event
	// bytes could be replaced while the digest stayed bound to the originals
	// and nothing noticed.
	if r.RawEventDigest == "" {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s carries no raw event digest, so the containment state it retains is a claim", where))
		return false
	}
	if want := DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawEventBytes...)); r.RawEventDigest != want {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s records raw event digest %s, but its own id and retained event bytes derive %s", where, r.RawEventDigest, want))
		return false
	}
	if want := DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawProcsBytes...)); r.RawProcsDigest != want {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s records membership digest %s, but its own event id and retained cgroup.procs bytes derive %s", where, r.RawProcsDigest, want))
		return false
	}
	listed, ok := parseCgroupProcs(r.RawProcsBytes)
	if !ok {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s retains cgroup.procs bytes that are not the kernel's grammar (%q)", where, string(r.RawProcsBytes)))
		return false
	}
	if !sameInts(listed, r.RawProcs) {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s lists members %v while its retained cgroup.procs bytes name %v", where, r.RawProcs, listed))
		return false
	}
	return true
}

// checkProcessTree adjudicates one process-tree record.
func checkProcessTree(v *Verdict, label string, e Envelope, r Record) {
	// The record has to be about THIS containment, or it describes some other
	// envelope's process.
	if !r.Containment.Same(e.Containment) {
		v.add("WT-033", SeverityTerminal, fmt.Sprintf(
			"%s: a process-tree record names a containment whose %s", label, r.Containment.Differs(e.Containment)))
		return
	}
	// WHO WROTE IT. The record is the PHYSICAL wrapper's account of the process
	// it spawned, delimited by an independently observed lifecycle event. A
	// trace collector's record, or one sourced from a wrapper annotation, is
	// not that account — and both used to be accepted without comment.
	if r.Producer != ProducerPhysical {
		v.add("WT-033", SeverityTerminal, fmt.Sprintf(
			"%s: a process-tree record was written by %s; the physical wrapper is the party that spawned the measured process and the only one able to state its identity", label, r.Producer))
		return
	}
	if r.Source != SourceProcessLifecycle {
		v.add("WT-033", SeverityTerminal, fmt.Sprintf(
			"%s: a process-tree record declares source %q; only an independently observed %s record may state a process lifecycle, and an annotation may annotate one but never constitute it",
			label, r.Source, SourceProcessLifecycle))
		return
	}
	if r.Boundary != "start" && r.Boundary != "observed" && r.Boundary != "end" {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s: a process-tree record names boundary %q; it is the admission read, the last observed read or the drained read, and an unlabelled one belongs to none of them", label, r.Boundary))
		return
	}
	p := r.Proc
	if p.PID <= 0 {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s: the process-tree record names no measured pid", label))
		return
	}
	// THE START IDENTITY. A pid is reused; a pid with its start time is not,
	// and this is the field the producer used to read after the wait had
	// already reaped the child — so on every normal Linux completion it was
	// empty and nothing noticed.
	if strings.TrimSpace(p.StartID) == "" {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s: the process-tree record names measured pid %d with no start identity; a pid alone is a number the kernel reuses, and it can only be read while the process is alive",
			label, p.PID))
	}
	if p.ParentPID <= 0 {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s: the process-tree record for pid %d names no parent, so reparenting is undecidable", label, p.PID))
	}
	if p.PGID <= 0 {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s: the process-tree record for pid %d names no process group, so a PGID change is undecidable", label, p.PID))
	}
	// THE MEMBERSHIP DECISION, REDERIVED — AT EVERY LEVEL.
	//
	// The producer wrote MembershipControl as a string and the verifier
	// believed it — a non-reproducible summary of the one property eligibility
	// turns on, about a cgroup that no longer exists by the time anyone reads
	// the records. The inputs are retained now, so the rule runs again here.
	//
	// It runs BEFORE the action level returns. It used to run after, so an
	// action containment could be world-writable, assert supervisor ownership
	// and never be rechecked — the level whose containment spans the whole
	// envelope was the one level whose membership nobody rederived.
	rederiveMembership(v, label, e, r)
	// THE ACTION LEVEL HAS NO MEASURED CHILD, and the rules below are about
	// one.
	//
	// A script or invocation envelope wraps a command: the wrapper creates the
	// containment and admits a child into it, so the measured process must be
	// neither the containment's own root nor running as the credential that
	// owns it. The ACTION envelope wraps a GitHub job step sequence. Its
	// containment is created by `wall begin`, which then joins it, and the
	// setup and bucket steps are separate step processes that join it too —
	// no single process is its child and none spans it.
	//
	// Applying the child rules there made the producer and the verifier
	// contradict each other: `BeginAction` recorded the creating wrapper as
	// the root, and this function then made that record terminal, so the
	// production action path could never verify. The action record says which
	// process TOOK the reading and what the containment held at that moment;
	// what spans the action is the containment, and the membership reads are
	// the evidence for it.
	if r.Level == LevelAction {
		if p.UID < 0 {
			v.add("WT-033", SeverityIneligible, fmt.Sprintf(
				"%s: the process-tree record does not state the credential the observing wrapper ran under", label))
		}
		return
	}

	// THE CREDENTIAL SEPARATION, OBSERVED.
	//
	// A containment whose `cgroup.procs` is owned by the credential the
	// measured process ran under is a containment that process could have
	// rewritten — and on cgroup-v2 that file is the migration control. This is
	// the boundary itself rather than a declaration of it: the owner comes off
	// the filesystem and the process's uid comes out of /proc, and they must
	// differ.
	if e.Containment.Primitive == PrimitiveCgroup2 && e.Containment.OwnerUID >= 0 {
		switch {
		case p.UID < 0:
			v.add("WT-033", SeverityIneligible, fmt.Sprintf(
				"%s: the process-tree record does not state the credential the measured process ran under, so nothing shows it could not write its own containment", label))
		case p.UID == e.Containment.OwnerUID:
			v.add("WT-033", SeverityIneligible, fmt.Sprintf(
				"%s: the measured process ran as uid %d, which is the credential owning this containment's cgroup.procs; on cgroup-v2 that file is the process-migration control, so the measured work could have rewritten the membership this envelope records",
				label, p.UID))
		}
	}
	// AND THE DECLARED WORKLOAD IS THE ONE THAT RAN.
	//
	// The invocation child is the process the workload account exists for. A
	// containment retaining one workload credential while the process measured
	// inside it ran as another means the rule above was rerun against a party
	// that was not there — a declaration standing in for the boundary rather
	// than describing it.
	if r.Level == LevelInvocation && e.Containment.WorkloadUID > 0 && p.UID >= 0 &&
		p.UID != e.Containment.WorkloadUID {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s: the containment retains workload credential uid %d, but the process measured inside it ran as uid %d; the membership rule was rerun against a credential that did not run",
			label, e.Containment.WorkloadUID, p.UID))
	}
	if p.SessionID <= 0 {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s: the process-tree record for pid %d names no session, and the contract makes a session change terminal — undecidable is not the same as absent", label, p.PID))
	}
	// The measured child is not the containment's own root process: the
	// wrapper creates the containment and the child is admitted into it. A
	// record claiming they are the same process is describing the wrapper
	// measuring itself.
	if p.PID == e.Containment.RootPID && p.StartID != "" && p.StartID == e.Containment.RootStart {
		v.add("WT-033", SeverityTerminal, fmt.Sprintf(
			"%s: the measured process %d/%s is the containment's own root process; the wrapper creates the containment and admits the child into it, so these cannot be the same process",
			label, p.PID, p.StartID))
	}
}

// rederiveMembership reruns the membership rule over the facts the record
// retained, and reports when the producer's conclusion is not what the rule
// derives — or when what it derives is a containment the workload could write.
//
// THE SUBJECT IS THE WORKLOAD, not whichever process took the reading.
//
// The question the contract asks is "could the measured workload rewrite the
// membership history this envelope rests on", and at the action level there is
// no measured child whose uid could stand in for the workload's — which is
// why the rederivation used to be skipped there entirely. The workload
// credential is retained on the containment now, so the same question has an
// answer at every level, and it is the same question at every level.
//
// At the invocation level the measured process IS the workload, so its own
// credential joins the subject: the retained declaration and the credential
// the kernel reported must both fail to reach the file.
func rederiveMembership(v *Verdict, label string, e Envelope, r Record) {
	c := e.Containment
	if c.Primitive != PrimitiveCgroup2 || c.Mode == 0 {
		// A containment that retained no mode retained no inputs; Scorable
		// refuses it outright rather than letting an omission skip the rule.
		return
	}
	p := r.Proc
	uids, gids, subject := membershipSubject(c, p, r.Level)
	rederived := membershipModelFor(MembershipFacts{
		OwnerUID: uint32(c.OwnerUID), OwnerGID: uint32(c.OwnerGID),
		GroupWritable: c.Mode&0o020 != 0, OtherWritable: c.Mode&0o002 != 0,
		SelfUID: p.UID,
		// The RETAINED resolved account, so the rerun makes the same
		// distinction the producer did between an account that exists and a
		// number a caller wrote down.
		WorkloadUID:  c.WorkloadUID,
		WorkloadUIDs: uids, WorkloadGIDs: gids,
	})
	if rederived != c.MembershipControl {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s: the containment records membership control %q, but rerunning the rule over its retained owner %d:%d, mode %04o and %s derives %q; a producer's summary of the property eligibility turns on is not the property",
			label, c.MembershipControl, c.OwnerUID, c.OwnerGID, c.Mode, subject, rederived))
	}
	if rederived != MembershipSupervisorOwned {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s: rerun over the retained facts, %s could write this containment's cgroup.procs (%s)", label, subject, rederived))
	}
}

// membershipSubject is the credential the rule is rerun against, and the words
// that name it in a finding.
func membershipSubject(c ContainmentIdentity, p ProcIdentity, level Level) ([]int, []int, string) {
	var uids, gids []int
	var described string
	// A retained workload uid is a POSITIVE one. Zero is root, and a workload
	// running as root holds every capability this boundary exists to deny, so
	// it can never be the credential a scored row rests on; reading the zero
	// value as a declaration would let an identity that retained nothing be
	// rerun against uid 0 and pass.
	if c.WorkloadUID > 0 {
		uids, gids = append(uids, c.WorkloadUID), append(gids, c.WorkloadGIDs...)
		described = fmt.Sprintf("the retained workload credential %d%v", c.WorkloadUID, c.WorkloadGIDs)
	}
	// AND THE CREDENTIAL THE KERNEL REPORTED FOR THE PROCESS THAT RAN.
	//
	// The retained declaration says which account the wrapper resolved; the
	// process identity says which credential actually ran. Both belong in the
	// subject at EVERY level where something was measured, and the script
	// level was deliberately excluded — so a script containment made
	// group-writable by the measured script's own group could be labelled
	// supervisor-owned using an unrelated account's groups, which is the one
	// arrangement the rule exists to refuse. A subject that omits the
	// measured process is a rule about somebody who was not there.
	if p.UID >= 0 && (level == LevelInvocation || level == LevelScript || len(uids) == 0) {
		own := append(append([]int{}, p.Groups...), p.GID)
		uids = append(uids, p.UID)
		gids = append(gids, own...)
		measured := fmt.Sprintf("the measured process's own credential %d%v", p.UID, own)
		if described == "" {
			described = measured
		} else {
			described += " together with " + measured
		}
	}
	return uids, gids, described
}

// checkActionSpan adjudicates the action level's process-tree records, whose
// subject is the CONTAINMENT rather than a process.
//
// The universal admitted-to-observed comparison could not hold here and was
// never about this level: `wall begin`, the setup step, the bucket step and
// `wall end` are separate step processes that each join the same containment,
// so a different pid at the closing read is the DESIGN, and reporting it as
// "a different pid is a different process" made every production action row
// terminal by construction.
//
// What must hold instead is what the action actually claims: one containment,
// unbroken across the interval, with every reading taken from inside it by a
// process that says which process it was.
func checkActionSpan(v *Verdict, label string, e Envelope, found []Record) {
	for _, r := range found {
		if !r.Containment.SameRoot(e.Containment) {
			v.add("WT-033", SeverityTerminal, fmt.Sprintf(
				"%s: an action process-tree record names a containment created for root %d/%s, not this envelope's %d/%s; what spans an action is its containment, so two readings of different containments cannot describe one action",
				label, r.Containment.RootPID, r.Containment.RootStart, e.Containment.RootPID, e.Containment.RootStart))
			continue
		}
		// WHO TOOK THE READING, AND WHERE THEY STOOD.
		//
		// With no measured child, this is the only thing tying an action
		// record to a process. The two ends stand in OPPOSITE places, and both
		// placements are required:
		//
		//   - the OPENING reading is taken by `wall begin`, which joins the
		//     containment it created; a reader that had not joined would be
		//     reporting on a container it was never in;
		//   - the CLOSING readings are taken by `wall end`, which must NOT be
		//     a member. It drains the containment and then SIGKILLs whatever
		//     is left, so a closer inside its own drain can never see it empty
		//     and kills itself at the deadline. A closing reading whose own
		//     membership contains the reader is a drain that measured itself.
		if r.RawProcs == nil {
			continue
		}
		var inside bool
		for _, pid := range r.RawProcs {
			if pid == r.Proc.PID {
				inside = true
			}
		}
		switch {
		case r.Boundary == "start" && !inside:
			v.add("WT-033", SeverityIneligible, fmt.Sprintf(
				"%s: the opening action reading was taken by pid %d, which its own membership snapshot %v does not contain; a wrapper that had not joined the containment is reporting on one it was never in",
				label, r.Proc.PID, r.RawProcs))
		case r.Boundary != "start" && inside:
			v.add("WT-033", SeverityTerminal, fmt.Sprintf(
				"%s: the %s action reading was taken by pid %d, which is itself a member of the containment being drained (%v); a closer inside its own drain cannot observe it empty and kills itself at the deadline, so no drained read, end boundary or seal can follow",
				label, r.Boundary, r.Proc.PID, r.RawProcs))
		}
	}
}

// checkActionChildren adjudicates the action-owned children.
//
// `RunInAction` retains one record per child, and NOTHING READ THEM. The
// contract asks for containment proof before every action-owned child, and the
// producer of exactly that proof was disconnected from eligibility: a setup
// command that started, forked and vanished left a record no verdict consulted.
func checkActionChildren(v *Verdict, label string, e Envelope, children []Record) {
	var observed int
	for _, r := range children {
		where := fmt.Sprintf("%s action child", label)
		if r.Boundary == "before" {
			// The reading committed BEFORE the child existed. It states what
			// the containment held at that moment and names no process,
			// because there was not one yet; the containment proof the
			// contract asks for precedes the execution rather than describing
			// it afterwards.
			if !r.Containment.Same(e.Containment) {
				v.add("WT-033", SeverityTerminal, fmt.Sprintf(
					"%s: a pre-spawn action-child record names a containment whose %s", where, r.Containment.Differs(e.Containment)))
				continue
			}
			requireMembershipEvidence(v, where+" pre-spawn reading", r)
			continue
		}
		observed++
		if !r.Containment.Same(e.Containment) {
			v.add("WT-033", SeverityTerminal, fmt.Sprintf(
				"%s: an action-child record names a containment whose %s", where, r.Containment.Differs(e.Containment)))
			continue
		}
		if r.Producer != ProducerPhysical || r.Source != SourceProcessLifecycle {
			v.add("WT-033", SeverityTerminal, fmt.Sprintf(
				"%s: an action-child record was written by %s with source %q; only the wrapper that spawned the child, reporting an observed lifecycle, can state its identity",
				where, r.Producer, r.Source))
			continue
		}
		p := r.Proc
		if p.PID <= 0 || strings.TrimSpace(p.StartID) == "" || p.PGID <= 0 || p.SessionID <= 0 || p.ParentPID <= 0 {
			v.add("WT-033", SeverityIneligible, fmt.Sprintf(
				"%s: pid %d is retained without the complete start/session/group/parent identity, so nothing states which process the action owned",
				where, p.PID))
		}
		if !requireMembershipEvidence(v, where, r) {
			continue
		}
		var inside bool
		for _, pid := range r.RawProcs {
			if pid == p.PID {
				inside = true
			}
		}
		if !inside {
			v.add("WT-033", SeverityTerminal, fmt.Sprintf(
				"%s: pid %d is not in the membership %v read while it was running; the contract asks for containment proof BEFORE every action-owned child, and a child outside the action containment is work the peer and the trace never bracketed",
				where, p.PID, r.RawProcs))
		}
		rederiveMembership(v, where, e, r)
	}
	// EVERY OBSERVED CHILD HAS A PROOF THAT PRECEDED IT. A missing pre-spawn
	// record is a child that ran before anything was committed about it, which
	// is the shape the best-effort writer produced whenever anything failed.
	if before := len(children) - observed; observed > before {
		v.add("WT-033", SeverityIneligible, fmt.Sprintf(
			"%s: %d action-owned child observation(s) but %d containment proof(s) committed before them; the contract asks for containment proof BEFORE every action-owned child",
			label, observed, before))
	}
}

// checkIdentityTransition compares the admission identity with a later one.
//
// Each field names a terminal case in the contract: a changed parent is a
// reparent, a changed session or process group is the escape a session leader
// can perform, and a changed start identity means the pid is not the process
// that was admitted at all.
func checkIdentityTransition(v *Verdict, label string, admitted, later ProcIdentity, boundary string) {
	if later.PID == 0 && later.StartID == "" {
		// Nothing was observed later; the absence is already reported by the
		// record's own completeness check.
		return
	}
	where := fmt.Sprintf("%s %s process tree", label, boundary)
	for _, f := range []struct {
		what        string
		was, is     string
		consequence string
	}{
		{"process id", fmt.Sprint(admitted.PID), fmt.Sprint(later.PID),
			"a different pid is a different process, not a later view of this one"},
		{"start identity", admitted.StartID, later.StartID,
			"a pid whose start identity moved is a reused number, not the process that was admitted"},
		{"parent", fmt.Sprint(admitted.ParentPID), fmt.Sprint(later.ParentPID),
			"the contract makes reparenting terminal"},
		{"session", fmt.Sprint(admitted.SessionID), fmt.Sprint(later.SessionID),
			"the contract makes a session change terminal"},
		{"process group", fmt.Sprint(admitted.PGID), fmt.Sprint(later.PGID),
			"the contract makes a PGID change terminal"},
	} {
		if f.was != f.is {
			v.add("WT-033", SeverityTerminal, fmt.Sprintf(
				"%s: the measured process was admitted with %s %s and last observed with %s; %s",
				where, f.what, f.was, f.is, f.consequence))
		}
	}
}

func envelopeKey(level Level, seq int) string { return fmt.Sprintf("%s#%d", level, seq) }
