package walltime

import (
	"strconv"
	"strings"
	"testing"
)

// setMembership rewrites a record's membership snapshot COHERENTLY — bytes,
// parsed list and digest together — so a case exercises the invariant it
// names rather than tripping the digest check on its way there.
func setMembership(r *Record, procs string) {
	r.RawProcsBytes = []byte(procs)
	parsed, ok := parseCgroupProcs(r.RawProcsBytes)
	if !ok {
		// A COHERENT malformed tuple: bytes, an empty non-nil list and a
		// matching digest. This is the shape the grammar check exists for —
		// leaving the list nil instead would trip the "no snapshot was taken"
		// refusal first and the case would pass for the wrong reason.
		parsed = []int{}
	}
	r.RawProcs = parsed
	r.RawProcsDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawProcsBytes...))
}

// findingsMentioning collects every finding whose code and detail match, so a
// case can assert on the reason rather than on a count.
func findingsMentioning(v *Verdict, code, substr string) []string {
	var out []string
	for _, f := range v.Findings {
		if f.Code == code && strings.Contains(f.Detail, substr) {
			out = append(out, f.Detail)
		}
	}
	return out
}

// TestTheRetainedKernelBytesAreActuallyVerified is the F4 regression.
//
// The producer read `cgroup.events`, snapshotted `cgroup.procs` with the same
// observation, and derived the event digest from its own id and those exact
// bytes. The verifier then never read any of it: it compared peer and trace
// ids and digests for INEQUALITY — which proves two observers, not two
// observations — and stopped. So a correctly signed stream could carry
// invented ids, digests of nothing, no bytes and no membership, and be scored.
//
// Every case below is a signed, otherwise-valid run with exactly one thing
// wrong with its retained evidence. All of them used to be eligible.
func TestTheRetainedKernelBytesAreActuallyVerified(t *testing.T) {
	admission := func(boundary string) bool { return boundary == "start" }
	for _, tc := range []struct {
		name string
		edit mutation
		want string
	}{
		// THE DEFECT, exactly as it stood: an id, a digest derived from that
		// id alone, and no kernel bytes at all.
		{"no retained bytes at all", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p != ProducerPhysical {
				r.RawEventBytes, r.RawProcs = nil, nil
				r.RawEventDigest = DigestBytes([]byte(r.RawEventID))
			}
		}, "retains no raw kernel bytes"},

		{"bytes the recorded digest is not the digest of", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p == ProducerPeer {
				r.RawEventBytes = append(append([]byte(nil), r.RawEventBytes...), ' ')
			}
		}, "derive"},

		{"a digest minted over some other observer's id", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p == ProducerTrace {
				r.RawEventDigest = DigestBytes(append([]byte("someone-else\x00"), r.RawEventBytes...))
			}
		}, "derive"},

		{"bytes that are not a cgroup.events read", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p != ProducerPhysical {
				r.RawEventBytes = []byte("frozen 0\n")
				r.RawEventDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawEventBytes...))
			}
		}, "well-formed `populated 0|1` line"},

		// PRODUCTION TRUTH, both ways round. The containment is created
		// fresh and admitted before any child, so a populated admission is
		// the contract's child-before-admission, and a populated close is an
		// escaped descendant. Neither is admissible, and the previous
		// verifier had the admission case exactly inverted — it DEMANDED the
		// state production forbids.
		{"an admission whose kernel read says populated", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p != ProducerPhysical && admission(b) {
				r.RawEventBytes = []byte("populated 1\nfrozen 0\n")
				r.RawProcs = []int{synthAdmittedPID}
				r.RawEventDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawEventBytes...))
			}
		}, "child-before-admission"},

		{"a close whose kernel read says populated", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p != ProducerPhysical && !admission(b) {
				r.RawEventBytes = []byte("populated 1\nfrozen 0\n")
				r.RawProcs = []int{synthAdmittedPID}
				r.RawEventDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawEventBytes...))
			}
		}, "had not reported it empty"},

		// A `populated` value no kernel writes. It used to read as POPULATED,
		// because anything that was not literally "0" did.
		{"a populated value outside the kernel grammar", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p != ProducerPhysical {
				r.RawEventBytes = []byte("populated forged\nfrozen 0\n")
				r.RawEventDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawEventBytes...))
			}
		}, "well-formed `populated 0|1` line"},

		{"a numeric populated value the grammar does not allow", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p == ProducerTrace {
				r.RawEventBytes = []byte("populated 2\nfrozen 0\n")
				r.RawEventDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawEventBytes...))
			}
		}, "well-formed `populated 0|1` line"},

		// The record contradicting ITSELF: the kernel line says empty, the
		// membership snapshot taken with it lists a member. True at either
		// end, since production observes an empty containment at both.
		// A COHERENT snapshot — bytes, list and digest all agreeing — that
		// lists a member while the kernel line says empty. The record
		// contradicts itself, at either end.
		{"an empty admission whose own snapshot lists members", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p != ProducerPhysical && admission(b) {
				setMembership(r, "4242\n")
			}
		}, "membership snapshot lists"},

		{"an empty close whose own snapshot lists members", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p != ProducerPhysical && !admission(b) {
				setMembership(r, "4242\n")
			}
		}, "membership snapshot lists"},

		// THE F2 DEFECT: no snapshot at all. A nil membership and a
		// successful read of an empty containment used to serialise
		// identically, so a failed read proved emptiness.
		{"no membership snapshot at any observer endpoint", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p != ProducerPhysical {
				r.RawProcs = nil
			}
		}, "retains no cgroup.procs membership snapshot"},

		{"a membership snapshot with no digest", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p == ProducerPeer {
				r.RawProcsDigest = ""
			}
		}, "no digest binding it to this observation"},

		{"a membership digest over some other observation", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p == ProducerTrace {
				r.RawProcsDigest = DigestBytes(append([]byte("someone-else\x00"), r.RawProcsBytes...))
			}
		}, "derive"},

		// The readable list and the retained bytes disagreeing: one of them
		// was edited after the observation.
		{"a member list its own retained bytes do not name", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p == ProducerPeer {
				r.RawProcs = []int{synthAdmittedPID}
			}
		}, "retained cgroup.procs bytes name"},

		// ONE read filed twice. Both endpoints legitimately report an empty
		// containment, so equal bytes prove nothing — but the contract
		// requires distinct admission and empty raw-event ids, and a shared
		// id means one observation was recorded as two.
		{"one raw event id at both ends of a lifecycle", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPeer {
				r.RawEventID = "peer:one-read-filed-twice"
				r.RawEventDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawEventBytes...))
			}
		}, "not one read filed twice"},

		{"bytes carried under no raw event id", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p == ProducerPeer {
				r.RawEventID = ""
				r.RawEventDigest = DigestBytes(append([]byte("\x00"), r.RawEventBytes...))
			}
		}, "under no raw event id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := verifySynth(t, tc.edit, nil)
			got := findingsMentioning(v, "WT-028", tc.want)
			if len(got) == 0 {
				var codes []string
				for _, f := range v.Findings {
					codes = append(codes, f.Code+": "+f.Detail)
				}
				t.Fatalf("no WT-028 finding mentions %q; eligible=%v findings=%v", tc.want, v.Eligible, codes)
			}
			if v.Eligible {
				t.Errorf("a run whose retained evidence is %s scored", tc.name)
			}
		})
	}
}

// TestAnUnmutatedSynthRunKeepsItsRawEvidence is the positive control for the
// table above: the fixture emits evidence the new checks accept, so every
// failure there is the mutation and not the harness.
func TestAnUnmutatedSynthRunKeepsItsRawEvidence(t *testing.T) {
	v := verifySynth(t, nil, nil)
	for _, f := range v.Findings {
		if f.Code == "WT-028" {
			t.Errorf("an untouched run raised %s: %s", f.Code, f.Detail)
		}
	}
	if !v.Eligible {
		t.Errorf("the untouched fixture is not eligible; the raw-evidence checks refuse a sound run")
	}
}

// TestParseCgroupPopulatedDistinguishesAbsenceFromEmptiness: "no populated
// line" is a missing observation and "populated 0" is an observation. Folding
// them together would let bytes that are not a cgroup.events read at all be
// accepted as evidence that a containment was empty.
func TestParseCgroupPopulatedDistinguishesAbsenceFromEmptiness(t *testing.T) {
	for _, tc := range []struct {
		in         string
		pop, found bool
	}{
		{"populated 1\nfrozen 0\n", true, true},
		{"populated 0\nfrozen 0\n", false, true},
		{"frozen 0\n", false, false},
		{"", false, false},
		{"populated\n", false, false},
		{"frozen 0\npopulated 1\n", true, true},
	} {
		pop, found := parseCgroupPopulated([]byte(tc.in))
		if pop != tc.pop || found != tc.found {
			t.Errorf("parseCgroupPopulated(%q) = (%v, %v), want (%v, %v)", tc.in, pop, found, tc.pop, tc.found)
		}
	}
}

// TestTheRetainedMembershipBytesMustBeTheKernelGrammar is the F6 regression.
//
// parseCgroupProcs tokenised with strings.Fields and silently discarded every
// token strconv.Atoi refused, so `not-a-pid\n` parsed to an empty list — and
// because the verifier compared the record's readable list with that same
// permissive parser, bytes no kernel ever wrote passed as proof that the
// containment was empty. A coherent tuple of malformed bytes, an empty list
// and a matching digest scored.
func TestTheRetainedMembershipBytesMustBeTheKernelGrammar(t *testing.T) {
	for _, bytes := range []string{
		"not-a-pid\n",
		"4242",    // unterminated
		"0\n",     // not a pid
		"-1\n",    // not a pid
		"007\n",   // not the canonical decimal the kernel writes
		" 42\n",   // padded
		"42 43\n", // two on one line
		"42\n\n",  // a blank line
		"42\nx\n", // one good line does not redeem the file
	} {
		t.Run(strconv.Quote(bytes), func(t *testing.T) {
			v := verifySynth(t, func(_ Level, _ int, p Producer, _ string, r *Record) {
				if p != ProducerPhysical {
					setMembership(r, bytes)
				}
			}, nil)
			if len(findingsMentioning(v, "WT-028", "not the kernel's grammar")) == 0 {
				t.Errorf("bytes %q raised no WT-028: %+v", bytes, v.Findings)
			}
			if v.Eligible {
				t.Errorf("a run whose membership snapshot is %q scored", bytes)
			}
		})
	}
}

// TestTheKernelGrammarAcceptsWhatTheKernelWrites is the positive control: the
// strict parser must not refuse a real snapshot, or the table above would be
// passing for the wrong reason.
func TestTheKernelGrammarAcceptsWhatTheKernelWrites(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []int
	}{
		{"", []int{}},
		{"42\n", []int{42}},
		{"42\n43\n99999\n", []int{42, 43, 99999}},
	} {
		got, ok := parseCgroupProcs([]byte(tc.in))
		if !ok {
			t.Errorf("parseCgroupProcs(%q) refused a snapshot the kernel writes", tc.in)
			continue
		}
		if !sameInts(got, tc.want) {
			t.Errorf("parseCgroupProcs(%q) = %v, want %v", tc.in, got, tc.want)
		}
		if got == nil {
			t.Errorf("parseCgroupProcs(%q) returned nil, which means no snapshot was taken", tc.in)
		}
	}
}
