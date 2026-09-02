package walltime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// freshCgroupEvents is what the kernel writes in a cgroup.events file for a
// cgroup with nothing in it. It is the exact content the production admission
// read observes, because the containment is created by newCgroupUnder — which
// refuses a directory that already exists — and both observers are admitted
// before Exec creates any child.
const freshCgroupEvents = "populated 0\nfrozen 0\n"

// TestTheVerifierAcceptsWhatTheProducerCanActuallyObserve is the F1
// regression, and it is the one that matters: it joins the production ORDERING
// to the semantic verifier instead of to a fixture.
//
// The previous check demanded `populated 1` at admission. Production cannot
// produce that state — RunObserver takes its admission read the moment the
// admit phase opens, and Exec creates the child only after both start records
// exist — so every genuine scored run was WT-028 ineligible while the fixture,
// which had been edited to describe the impossible sequence, passed. A test
// that restates the verifier's own expectation cannot catch that; this one
// derives the record from the producer's own arithmetic and the ordering from
// the production source.
func TestTheVerifierAcceptsWhatTheProducerCanActuallyObserve(t *testing.T) {
	// 1. THE ORDERING, read from the production source. If a future change
	// admits the observers after the child, the admission read stops being an
	// empty-containment observation and this whole check must be revisited.
	body := productionFunc(t, "exec.go", "func Exec(")
	peer := strings.Index(body, "peer.admit(deadline)")
	trace := strings.Index(body, "trace.admit(deadline)")
	child := strings.Index(body, "runChild(opt, cont, deadline)")
	if peer < 0 || trace < 0 || child < 0 {
		t.Fatalf("production ordering is no longer recognisable: peer=%d trace=%d child=%d", peer, trace, child)
	}
	if peer > child || trace > child {
		t.Fatalf("a child is created before an observer is admitted (peer=%d trace=%d child=%d); the contract puts peer admission before every action-owned child",
			peer, trace, child)
	}

	// 2. THE RECORD, built through the producer's own derivation from the
	// bytes a fresh containment yields — not restated here.
	for _, endpoint := range []string{"admission", "verified-empty"} {
		// An empty cgroup.procs: the exact bytes a fresh containment yields.
		ev := newContainmentEvent("containment_peer", []byte(freshCgroupEvents), []byte(""))
		rec := Record{
			Kind: "boundary", Boundary: "start", Source: ev.Source,
			RawEventID: ev.ID, RawEventDigest: ev.Digest, RawEventBytes: ev.Bytes,
			RawProcs: ev.Procs, RawProcsBytes: ev.ProcsBytes, RawProcsDigest: ev.ProcsDigest,
		}
		v := &Verdict{}
		checkRawEndpoint(v, "script[0]", ProducerPeer, endpoint, rec)
		if len(v.Findings) > 0 {
			t.Errorf("the verifier refuses the %s record the producer actually writes: %+v", endpoint, v.Findings)
		}
	}
}

// TestAPopulatedAdmissionIsChildBeforeAdmission states the inverse invariant
// so the correction cannot be read as "the check was simply dropped". A
// containment that already has members when it is admitted is the contract's
// terminal child-before-admission, and it is refused under that name.
func TestAPopulatedAdmissionIsChildBeforeAdmission(t *testing.T) {
	ev := newContainmentEvent("containment_peer", []byte("populated 1\nfrozen 0\n"), []byte("4242\n"))
	rec := Record{
		Kind: "boundary", Source: ev.Source,
		RawEventID: ev.ID, RawEventDigest: ev.Digest, RawEventBytes: ev.Bytes,
		RawProcs: ev.Procs, RawProcsBytes: ev.ProcsBytes, RawProcsDigest: ev.ProcsDigest,
	}
	v := &Verdict{}
	checkRawEndpoint(v, "script[0]", ProducerPeer, "admission", rec)
	if len(findingsMentioning(v, "WT-028", "child-before-admission")) == 0 {
		t.Errorf("a populated admission was not refused as child-before-admission: %+v", v.Findings)
	}
}

// TestTheProducerAndVerifierShareOneEventDerivation: the digest binds the
// observer id to the exact bytes, and a producer with a private copy of that
// arithmetic can drift from the verifier silently. They are the same function,
// and this fails if either grows its own.
func TestTheProducerAndVerifierShareOneEventDerivation(t *testing.T) {
	linux, err := os.ReadFile("contain_linux.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(linux), "newContainmentEvent(observer, b, procs)") {
		t.Error("the Linux producer no longer builds its raw event through the shared derivation")
	}
	ev := newContainmentEvent("containment_trace", []byte(freshCgroupEvents), []byte(""))
	if want := DigestBytes(append([]byte(ev.ID+"\x00"), ev.Bytes...)); ev.Digest != want {
		t.Errorf("the shared derivation produced %s, but re-deriving it the way the verifier does gives %s", ev.Digest, want)
	}
	if ev.Source != SourceContainment {
		t.Errorf("a cgroup.events read declared source %q; only a containment read may delimit a lifecycle", ev.Source)
	}
}

// productionFunc returns one function's source text, so a test can assert an
// ordering the type system cannot express.
func productionFunc(t *testing.T, file, decl string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, decl)
	if start < 0 {
		t.Fatalf("%s no longer defines %q", file, decl)
	}
	end := strings.Index(s[start+1:], "\nfunc ")
	if end < 0 {
		t.Fatalf("could not delimit %q in %s", decl, file)
	}
	return s[start : start+1+end]
}
