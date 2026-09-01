package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// The audit's plan binding is only a control if a shard-plan artifact written
// by the planner canonicalises back to the very digest the Stage-2 receipt
// froze. If a round trip through the artifact changed the digest, the check
// would fire on every honest run — so it is proved by executing it, not by
// assuming JSON is lossless.
func TestShardPlanArtifactRoundTripsToItsFullPlanDigest(t *testing.T) {
	doc := &core.PlanDocument{
		K: 2, Flags: "vitest", Algorithm: "kk-lpt", StorePath: "test-timings.json",
		Buckets: []core.PlanBucket{
			{Index: 0, Name: "bucket-0", Units: []core.PlanUnit{
				{ID: "pkg/a", Kind: runner.KindPackage, Packages: []string{"pkg/a"}, Seconds: 1.5},
			}},
			{Index: 1, Name: "bucket-1", Units: []core.PlanUnit{
				{ID: "pkg/b[T1|T2]", Kind: runner.KindRunSlice, Packages: []string{"pkg/b"}, Run: []string{"T1", "T2"}, Seconds: 2.25},
			}},
		},
	}
	want, err := walltime.DigestJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shard-plan.json")
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fullPlanDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("the artifact digests to %s but the receipt froze %s", got, want)
	}
}

// The attack F1 describes, executed end to end against the REAL audit.
//
// After a valid planning run, an attacker replaces the shard-plan artifact
// with one describing only the tests that actually ran. Every other artifact
// stays exactly as valid as it was: the Stage-2 receipt, its sidecars, the
// invocation manifest and every record are untouched. The audit then reports
// complete coverage of a run that skipped half its work.
//
// The only thing that can tell the two plans apart is the full-plan digest, so
// this proves both halves: the substitution genuinely defeats the audit, and
// the digest the audit reports is what exposes it.
func TestASubstitutedShardPlanIsCaughtOnlyByItsFullPlanDigest(t *testing.T) {
	dir := t.TempDir()
	// The authorised plan: two targets in bucket-0.
	authorised := &core.PlanDocument{
		K: 1, Flags: "vitest", Algorithm: "kk-lpt", StorePath: "test-timings.json",
		Buckets: []core.PlanBucket{{Index: 0, Name: "bucket-0", Units: []core.PlanUnit{
			{ID: "pkg/a", Kind: runner.KindPackage, Packages: []string{"pkg/a"}, Seconds: 1.5},
			{ID: "pkg/b", Kind: runner.KindPackage, Packages: []string{"pkg/b"}, Seconds: 2.5},
		}}},
	}
	// The substitution: the same shape, minus the target that never ran.
	substituted := &core.PlanDocument{
		K: 1, Flags: "vitest", Algorithm: "kk-lpt", StorePath: "test-timings.json",
		Buckets: []core.PlanBucket{{Index: 0, Name: "bucket-0", Units: []core.PlanUnit{
			{ID: "pkg/a", Kind: runner.KindPackage, Packages: []string{"pkg/a"}, Seconds: 1.5},
		}}},
	}
	write := func(name string, doc *core.PlanDocument) string {
		path := filepath.Join(dir, name)
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	honestPath, substitutedPath := write("shard-plan.json", authorised), write("substituted.json", substituted)

	// Only pkg/a ran: the run is incomplete against the authorised plan.
	events := filepath.Join(dir, "events")
	if err := os.MkdirAll(events, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(events, "b0.jsonl"),
		[]byte(`{"Action":"pass","Package":"pkg/a","Elapsed":0.6}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Against the authorised plan the audit fails, as it must.
	honest, err := coverageAudit(honestPath, events, "go")("bucket-0")
	if err != nil {
		t.Fatal(err)
	}
	if len(honest.Problems) == 0 {
		t.Fatalf("an incomplete run audited clean against the plan it was given")
	}

	// Against the substituted plan it passes — the attack works, and no other
	// artifact is disturbed by it.
	forged, err := coverageAudit(substitutedPath, events, "go")("bucket-0")
	if err != nil {
		t.Fatal(err)
	}
	if len(forged.Problems) != 0 {
		t.Fatalf("the substituted plan did not produce a clean audit, so this test is not exercising the attack: %v", forged.Problems)
	}

	// What separates them is the full-plan digest, and nothing else.
	want, err := walltime.DigestJSON(authorised)
	if err != nil {
		t.Fatal(err)
	}
	if honest.PlanDigest != want {
		t.Errorf("the audit reports plan %s for the authorised document, which digests to %s", honest.PlanDigest, want)
	}
	if forged.PlanDigest == want {
		t.Errorf("the substituted plan reports the authorised digest, so the binding cannot tell them apart")
	}
}

// The TOCTOU the single read closes.
//
// The digest, the bucket lookup and the expected coverage all describe "the
// plan". Read from a path three times they can describe three plans: take the
// digest from the authorised file, swap in a narrowed one, and the audit
// reports a Stage-2-matching digest over a population that was never planned —
// the substitution wearing the digest that exists to catch it as a disguise.
//
// The plan is therefore read ONCE. This test mutates the file while the audit
// is mid-flight and requires the evidence to be self-consistent: whichever
// snapshot it saw, its digest and its coverage must be that same snapshot's.
func TestTheAuditPlanCannotChangeBetweenItsReads(t *testing.T) {
	dir := t.TempDir()
	authorised := &core.PlanDocument{
		K: 1, Flags: "vitest", Algorithm: "kk-lpt", StorePath: "test-timings.json",
		Buckets: []core.PlanBucket{{Index: 0, Name: "bucket-0", Units: []core.PlanUnit{
			{ID: "pkg/a", Kind: runner.KindPackage, Packages: []string{"pkg/a"}, Seconds: 1.5},
			{ID: "pkg/b", Kind: runner.KindPackage, Packages: []string{"pkg/b"}, Seconds: 2.5},
		}}},
	}
	narrowed := &core.PlanDocument{
		K: 1, Flags: "vitest", Algorithm: "kk-lpt", StorePath: "test-timings.json",
		Buckets: []core.PlanBucket{{Index: 0, Name: "bucket-0", Units: []core.PlanUnit{
			{ID: "pkg/a", Kind: runner.KindPackage, Packages: []string{"pkg/a"}, Seconds: 1.5},
		}}},
	}
	path := filepath.Join(dir, "shard-plan.json")
	write := func(doc *core.PlanDocument) {
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(authorised)

	// Only pkg/a ran, so the run is complete against the narrowed plan and
	// incomplete against the authorised one. The two snapshots disagree about
	// the verdict, which is what makes the mid-flight swap worth attempting.
	events := filepath.Join(dir, "events")
	if err := os.MkdirAll(events, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(events, "b0.jsonl"),
		[]byte(`{"Action":"pass","Package":"pkg/a","Elapsed":0.6}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	authorisedDigest, err := walltime.DigestJSON(authorised)
	if err != nil {
		t.Fatal(err)
	}
	narrowedDigest, err := walltime.DigestJSON(narrowed)
	if err != nil {
		t.Fatal(err)
	}

	// Hammer the file while the audit runs. One of the two snapshots wins each
	// time; what must never happen is a mixture of both.
	audit := coverageAudit(path, events, "go")
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				write(narrowed)
				write(authorised)
			}
		}
	}()
	defer func() { close(stop); <-done }()

	for i := 0; i < 200; i++ {
		ev, err := audit("bucket-0")
		if err != nil {
			// A torn read is a parse failure, which fails closed. That is a
			// correct outcome; an inconsistent success is not.
			continue
		}
		clean := len(ev.Problems) == 0
		switch ev.PlanDigest {
		case authorisedDigest:
			if clean {
				t.Fatalf("the audit reported the authorised plan's digest over a population that audited clean; only the narrowed plan can audit clean here")
			}
		case narrowedDigest:
			if !clean {
				t.Fatalf("the audit reported the narrowed plan's digest but found problems; only the authorised plan has unmet coverage here")
			}
		default:
			t.Fatalf("the audit reported plan %s, which is neither snapshot", ev.PlanDigest)
		}
	}
}
