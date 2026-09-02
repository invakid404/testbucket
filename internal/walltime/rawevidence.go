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
			checkRawEndpoint(v, label, side.who, "admission", side.in.start, true)
			checkRawEndpoint(v, label, side.who, "verified-empty", side.in.end, false)
		}
	}
}

// checkRawEndpoint verifies one boundary record against the bytes it retains.
//
// wantPopulated says which side of the lifecycle this is: admission observes a
// containment that HAS the child in it, and the closing read observes one the
// kernel reports empty. A record whose retained bytes say the opposite is not
// evidence for the interval it delimits.
func checkRawEndpoint(v *Verdict, label string, who Producer, what string, r Record, wantPopulated bool) {
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
			"%s retains bytes with no `populated` line; they are not a cgroup.events read", where))
		return
	}
	if populated != wantPopulated {
		state := map[bool]string{true: "populated", false: "empty"}
		v.add("WT-028", SeverityIneligible, fmt.Sprintf(
			"%s retains a cgroup.events read reporting %s, but a %s endpoint requires %s",
			where, state[populated], what, state[wantPopulated]))
		return
	}
	// AND THE MEMBERSHIP TAKEN WITH IT. A close that reports empty while its
	// own snapshot lists members contradicts itself; the snapshot exists so
	// "nothing was in it" can be checked rather than believed.
	if !populated && len(r.RawProcs) > 0 {
		v.add("WT-028", SeverityIneligible, fmt.Sprintf(
			"%s reports an empty containment while its own membership snapshot lists %d member(s): %v",
			where, len(r.RawProcs), r.RawProcs))
	}
	if populated && len(r.RawProcs) == 0 {
		v.add("WT-028", SeverityIneligible, fmt.Sprintf(
			"%s reports a populated containment but retains no membership snapshot, so what was admitted is unrecorded", where))
	}
}

// parseCgroupPopulated reads the `populated 0|1` line the kernel writes. It
// reports whether the line was found, so bytes that are not a cgroup.events
// read are distinguishable from bytes that are and say "empty" — the first is
// a missing observation, the second is an observation.
func parseCgroupPopulated(b []byte) (populated, found bool) {
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "populated" {
			return f[1] != "0", true
		}
	}
	return false, false
}
