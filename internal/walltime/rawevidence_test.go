package walltime

import (
	"strings"
	"testing"
)

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
		}, "no `populated` line"},

		// The bytes are real and self-consistent, and say the OPPOSITE of what
		// the boundary they delimit claims.
		{"an admission whose kernel read says empty", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p != ProducerPhysical && admission(b) {
				r.RawEventBytes = []byte("populated 0\nfrozen 0\n")
				r.RawProcs = nil
				r.RawEventDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawEventBytes...))
			}
		}, "requires populated"},

		{"a close whose kernel read says populated", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p != ProducerPhysical && !admission(b) {
				r.RawEventBytes = []byte("populated 1\nfrozen 0\n")
				r.RawProcs = []int{synthAdmittedPID}
				r.RawEventDigest = DigestBytes(append([]byte(r.RawEventID+"\x00"), r.RawEventBytes...))
			}
		}, "requires empty"},

		// The record contradicting ITSELF: the kernel line says empty, the
		// membership snapshot taken with it lists a member.
		{"an empty close whose own snapshot lists members", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p != ProducerPhysical && !admission(b) {
				r.RawProcs = []int{synthAdmittedPID}
			}
		}, "membership snapshot lists"},

		{"a populated admission with no membership recorded", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p != ProducerPhysical && admission(b) {
				r.RawProcs = nil
			}
		}, "retains no membership snapshot"},

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
