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
