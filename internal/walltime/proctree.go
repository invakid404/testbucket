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
		for _, r := range found {
			checkProcessTree(v, label, e, r)
		}
	}
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
			"%s: the process-tree record for pid %d names no process group, so a session or PGID change is undecidable", label, p.PID))
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
	// The membership snapshot taken with it must contain the process it
	// describes, unless the record itself reports the child never ran.
	if len(r.RawProcs) > 0 && p.ExitKind != TerminalSpawnError {
		var inside bool
		for _, pid := range r.RawProcs {
			if pid == p.PID {
				inside = true
			}
		}
		if !inside {
			v.add("WT-033", SeverityIneligible, fmt.Sprintf(
				"%s: the process-tree record lists members %v, none of which is the measured process %d", label, r.RawProcs, p.PID))
		}
	}
}

func envelopeKey(level Level, seq int) string { return fmt.Sprintf("%s#%d", level, seq) }
