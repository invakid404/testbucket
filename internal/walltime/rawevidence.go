package walltime

import (
	"fmt"
	"strings"
)

// verifyRawEvidence re-derives the conclusions the peer and trace records
// state, from the kernel bytes those records retain.
//
// The producer was already sound: it reads `cgroup.events`, snapshots
// `cgroup.procs` with the same observation, and derives the event digest from
// the observer's own id and those exact bytes. The verifier then never read
// any of it. `RawEventBytes` and `RawProcs` appeared only in the emitter, the
// record schema and tests; verification compared peer and trace ids and
// digests for INEQUALITY — proving two observers rather than one — and stopped
// there.
//
// So a signed stream could carry invented, distinct raw ids and digests, no
// bytes and no membership at all, and score. A signature authenticates a
// claim; it does not make the evidence behind the claim exist. These are the
// checks that make the retained bytes load-bearing:
//
//   - the bytes must be THERE, on every record that delimits a lifecycle;
//   - the digest must be the digest OF those bytes, under the observer's own
//     id, exactly as the producer derives it;
//   - the bytes must PARSE as cgroup.events and must say what the boundary
//     claims — populated at admission, empty at close;
//   - the membership snapshot must AGREE with that: an "empty" close whose
//     own snapshot lists members is a contradiction the record carries within
//     itself.
func verifyRawEvidence(v *Verdict, envs []Envelope) {
	for _, e := range envs {
		label := fmt.Sprintf("%s[%d]", e.Level, e.Seq)
		for _, side := range []struct {
			who Producer
			in  Interval
		}{{ProducerPeer, e.Peer}, {ProducerTrace, e.Trace}} {
			if !side.in.OK {
				continue
			}
			checkRawEndpoint(v, label, side.who, "admission", side.in.start)
			checkRawEndpoint(v, label, side.who, "verified-empty", side.in.end)
			// The two endpoints of ONE observer must be two observations.
			// Both legitimately report an empty containment, so equal bytes
			// are expected and prove nothing either way — but a shared raw
			// event id means one read was recorded twice, which the contract
			// forbids by requiring distinct admission and empty raw-event ids.
			if side.in.start.Source == SourceContainment && side.in.end.Source == SourceContainment &&
				side.in.start.RawEventID != "" && side.in.start.RawEventID == side.in.end.RawEventID {
				v.add("WT-028", SeverityIneligible, fmt.Sprintf(
					"%s %s delimits its lifecycle with one raw event id (%s) at both ends; the admission and the verified-empty read must be distinct observations, not one read filed twice",
					label, side.who, side.in.start.RawEventID))
			}
		}
	}
}

// checkRawEndpoint verifies one boundary record against the bytes it retains.
//
// BOTH endpoints must observe an EMPTY containment, and this is the production
// truth rather than a convenience.
//
// The contract puts peer and collector admission BEFORE every action-owned
// child, and the producer follows it exactly: `RunObserver` takes its
// admission read the moment the admit phase opens, and `Exec` only creates the
// child after both observers have written their start records. The containment
// is freshly made — `newCgroupUnder` refuses a directory that already exists —
// so at that instant nothing is in it and `cgroup.events` truthfully says
// `populated 0`. The contract states the same mapping: `cgroup_create/admit`
// (containment inode plus boot/PID-start identity) to `cgroup.events
// populated=0`.
//
// This check previously required the admission read to say `populated 1`. That
// is a state the production ordering forbids, so every genuine scored run was
// WT-028 ineligible and only a fixture describing an impossible sequence could
// pass. Requiring EMPTY is not a relaxation: it turns the admission read into
// the check for the contract's terminal "child before admission" — a
// containment that already has members when it is admitted is one whose
// lifecycle began before anybody was watching.
func checkRawEndpoint(v *Verdict, label string, who Producer, what string, r Record) {
	where := fmt.Sprintf("%s %s %s", label, who, what)

	// Only an independently observed OS class may delimit a lifecycle, and
	// only a containment read carries cgroup.events. A process-lifecycle
	// endpoint is admissible on an unscored host, where there is no cgroup to
	// read; it is the containment reads that must carry their bytes.
	if r.Source != SourceContainment {
		return
	}
	if len(r.RawEventBytes) == 0 {
		v.add("WT-028", SeverityIneligible, fmt.Sprintf(
			"%s retains no raw kernel bytes; the digest it carries is then a claim about evidence nobody kept", where))
		return
	}
	if r.RawEventID == "" {
		v.add("WT-028", SeverityIneligible, fmt.Sprintf("%s carries raw bytes under no raw event id", where))
		return
	}
	// THE PRODUCER'S OWN DERIVATION, rerun. The digest binds the observer's
	// id to the exact bytes, so two observers reading the same kernel file
	// still mint distinct evidence — and a digest that is not this one is a
	// number beside some bytes.
	if want := DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawEventBytes...)); r.RawEventDigest != want {
		v.add("WT-028", SeverityIneligible, fmt.Sprintf(
			"%s records raw event digest %s, but its own id and retained bytes derive %s", where, r.RawEventDigest, want))
		return
	}

	// THE BYTES THEMSELVES. cgroup.events is the authority on emptiness, so
	// the boundary's own claim is checked against what the kernel said rather
	// than against the wrapper's label for it.
	populated, ok := parseCgroupPopulated(r.RawEventBytes)
	if !ok {
		v.add("WT-028", SeverityIneligible, fmt.Sprintf(
			"%s retains bytes with no well-formed `populated 0|1` line; they are not a cgroup.events read this verifier will interpret", where))
		return
	}
	if populated {
		// Which endpoint it is changes what a populated read MEANS, and
		// neither meaning is admissible.
		switch what {
		case "admission":
			v.add("WT-028", SeverityIneligible, fmt.Sprintf(
				"%s retains a cgroup.events read reporting a POPULATED containment; the containment is created fresh and admitted before any child, so a member already inside it is the contract's child-before-admission — a lifecycle that began before this observer was watching", where))
		default:
			v.add("WT-028", SeverityIneligible, fmt.Sprintf(
				"%s retains a cgroup.events read reporting a POPULATED containment, so the kernel had not reported it empty when this endpoint was written; a returned root with a live descendant is exactly the escape this endpoint exists to catch", where))
		}
		return
	}
	// AND THE MEMBERSHIP TAKEN WITH IT.
	//
	// The snapshot must have HAPPENED. A nil membership and a successful read
	// of an empty containment used to serialise identically, so a producer
	// whose `cgroup.procs` read failed — or which never took one — emitted
	// evidence indistinguishable from proof that nothing was inside. The
	// contract asks for retained `cgroup.procs` snapshots at every level, and
	// "the field is absent" is not one.
	if r.RawProcs == nil {
		v.add("WT-028", SeverityIneligible, fmt.Sprintf(
			"%s retains no cgroup.procs membership snapshot; a read that never happened is not proof that the containment was empty", where))
		return
	}
	// And it must be the snapshot this observation took: its own bytes, under
	// this observation's own event id.
	if r.RawProcsDigest == "" {
		v.add("WT-028", SeverityIneligible, fmt.Sprintf(
			"%s lists a membership snapshot with no digest binding it to this observation", where))
		return
	}
	if want := DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawProcsBytes...)); r.RawProcsDigest != want {
		v.add("WT-028", SeverityIneligible, fmt.Sprintf(
			"%s records membership digest %s, but its own event id and retained cgroup.procs bytes derive %s",
			where, r.RawProcsDigest, want))
		return
	}
	// The listed pids must be the ones those bytes name, so the readable list
	// and the retained evidence cannot disagree.
	listed, ok := parseCgroupProcs(r.RawProcsBytes)
	if !ok {
		v.add("WT-028", SeverityIneligible, fmt.Sprintf(
			"%s retains cgroup.procs bytes that are not the kernel's grammar (%q); one decimal pid per line is what a membership snapshot is, and bytes nobody could have read are not proof that the containment was empty",
			where, string(r.RawProcsBytes)))
		return
	}
	if !sameInts(listed, r.RawProcs) {
		v.add("WT-028", SeverityIneligible, fmt.Sprintf(
			"%s lists members %v while its retained cgroup.procs bytes name %v", where, r.RawProcs, listed))
		return
	}
	// An endpoint that reports empty while its own snapshot lists members
	// contradicts itself; the snapshot exists so "nothing was in it" can be
	// checked rather than believed.
	if len(r.RawProcs) > 0 {
		v.add("WT-028", SeverityIneligible, fmt.Sprintf(
			"%s reports an empty containment while its own membership snapshot lists %d member(s): %v",
			where, len(r.RawProcs), r.RawProcs))
	}
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// parseCgroupPopulated reads the `populated 0|1` line the kernel writes.
//
// The grammar is EXACT. It used to read "anything that is not 0" as populated,
// which accepted `populated forged` — bytes no kernel produces — as a
// well-formed observation of a populated containment. The producer's own
// polling loop is deliberately permissive in the other direction, because
// there "unparseable" must mean "children may still be running"; a verifier
// adjudicating retained evidence after the fact has the opposite duty, and an
// uninterpretable byte string is not an observation at all.
//
// found reports whether a well-formed line was present, so bytes that are not
// a cgroup.events read stay distinguishable from bytes that are and say
// "empty" — the first is a missing observation, the second is an observation.
func parseCgroupPopulated(b []byte) (populated, found bool) {
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || f[0] != "populated" {
			continue
		}
		switch f[1] {
		case "0":
			return false, true
		case "1":
			return true, true
		default:
			// A `populated` line the kernel would never write. Refusing here
			// rather than continuing means a file carrying one malformed line
			// and one well-formed one is still refused.
			return false, false
		}
	}
	return false, false
}
