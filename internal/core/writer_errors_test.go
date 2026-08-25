package core

import (
	"bytes"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/runner"
)

// The P3 contract: a report that could not be delivered must not read as a
// success. whales and a PASSING audit propagate the first writer error, the way
// plan and ingest already do. (failingWriter is defined in plan_test.go.)

func TestWhaleReportPropagatesWriteFailure(t *testing.T) {
	st := syntheticStore()
	harpoon(st, "netpkg/streamer", splitCount, 6, map[string]float64{"TestA": 400, "TestB": 100})

	for _, budget := range []int{0, 20, 200} {
		if err := WriteWhaleReport(&failingWriter{budget: budget}, st, 6, 8, true); err == nil {
			t.Errorf("budget=%d: a failed whales write was reported as success", budget)
		}
	}
	var ok bytes.Buffer
	if err := WriteWhaleReport(&ok, st, 6, 8, true); err != nil {
		t.Errorf("a healthy whales write returned an error: %v", err)
	}
	if ok.Len() == 0 {
		t.Error("the healthy whales write produced nothing")
	}
}

func TestAuditPropagatesWriteFailureOnPass(t *testing.T) {
	// A run that matches its plan: the audit passes, but if it could not write
	// its PASS report the caller must still see a failure.
	planned := &PlannedCoverage{
		Units:       1,
		Invocations: map[string]int{"pkg/a": 1},
		Runnables:   map[string][]string{},
	}
	sum, err := parseEvents(stream(event("pass", "pkg/a", "", 10)))
	if err != nil {
		t.Fatal(err)
	}

	for _, budget := range []int{0, 15} {
		if err := AuditCoverage(&failingWriter{budget: budget}, planned, sum); err == nil {
			t.Errorf("budget=%d: a passing audit that could not write its report returned nil", budget)
		}
	}
	var ok bytes.Buffer
	if err := AuditCoverage(&ok, planned, sum); err != nil {
		t.Errorf("a healthy passing audit returned an error: %v", err)
	}
}

func TestIngestRefusesToEmptyANonEmptyStore(t *testing.T) {
	// Defense-in-depth for the never-drop principle applied to TIMING DATA: an
	// authoritative ingest whose live set matches NONE of the recorded packages
	// (e.g. every identity came back empty or mis-keyed) must fail loudly rather
	// than silently prune every row and persist an empty store.
	events, err := parseEvents(stream(event("pass", repoPrefix+"pool", "", 120)))
	if err != nil {
		t.Fatal(err)
	}
	st := syntheticStore() // 12 measured rows
	if len(st.Units) == 0 {
		t.Fatal("fixture store is empty")
	}

	opt := defaultIngestOptions()
	opt.Token = canonicalFlags(true, 100)
	opt.LiveAuthoritative = true
	// A live set that matches nothing in the store — the shape a broken --live
	// (empty identities) collapses to.
	opt.Live = []runner.LivePackage{livePkg("ghost/nowhere", ".", runner.ModeWork, true)}

	if _, err := ApplyIngest(st, events, opt); err == nil {
		t.Fatal("ingest emptied a non-empty store without error")
	} else if !strings.Contains(err.Error(), "matched none") {
		t.Errorf("error does not explain the empty-store refusal: %v", err)
	}

	// The healthy direction: a live set that DOES match keeps the store and does
	// not trip the guard.
	st2 := syntheticStore()
	events2, err := parseEvents(stream(event("pass", repoPrefix+"pool", "", 120)))
	if err != nil {
		t.Fatal(err)
	}
	opt2 := defaultIngestOptions()
	opt2.LiveAuthoritative = true
	opt2.Live = syntheticLive()
	if _, err := ApplyIngest(st2, events2, opt2); err != nil {
		t.Fatalf("a healthy authoritative ingest was refused: %v", err)
	}
	if len(st2.Units) == 0 {
		t.Fatal("a healthy ingest still emptied the store")
	}
}
