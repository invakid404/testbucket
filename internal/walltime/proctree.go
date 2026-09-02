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
	for _, r := range recs {
		if r.Kind == "process_tree" {
			trees[envelopeKey(r.Level, r.Seqno)] = append(trees[envelopeKey(r.Level, r.Seqno)], r)
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
	// EXACT CARDINALITY. At admission the containment holds the admitted child
	// and nothing else: the wrapper does not join it, and the child has not run
	// long enough to fork. More members than that is an unknown descendant
	// already present, which the contract makes terminal.
	if len(r.RawProcs) != 1 {
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
			"%s: a process-tree record names containment %s/%s, not this envelope's %s/%s",
			label, r.Containment.ID, r.Containment.Inode, e.Containment.ID, e.Containment.Inode))
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
