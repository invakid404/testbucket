package walltime

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// hasFinding reports whether the verdict carries a finding with this code.
func hasFinding(v *Verdict, code string) bool {
	for _, f := range v.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

// TestVerifierRefusesAnEmptyDirectory is the sharpest fail-closed case: no
// records at all is not a zero-length measurement.
func TestVerifierRefusesAnEmptyDirectory(t *testing.T) {
	v, err := VerifyDir(VerifyOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if v.Complete || v.Eligible {
		t.Errorf("an empty directory verified as complete=%v eligible=%v", v.Complete, v.Eligible)
	}
	if !hasFinding(v, "WT-004") {
		t.Errorf("an empty directory left no WT-004 finding; findings = %+v", v.Findings)
	}
}

// TestVerifierRefusesAnIncompleteStream is the incomplete-record refusal: an
// interval that was opened and never closed is not a measurement of anything.
//
// It is the shape a killed runner leaves behind, and it is the one a verifier
// must not round up: the opening reading on its own would otherwise be read as
// a run whose duration happened to be unavailable.
func TestVerifierRefusesAnIncompleteStream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "physical-invocation-00.jsonl")
	w, err := NewWriter(path, ProducerPhysical, "physical")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(Record{
		Kind: "boundary", Level: LevelInvocation, Boundary: "start",
		Source: SourceWrapper, Instant: NewSystemClock().Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	v, err := VerifyDir(VerifyOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(v, "WT-003") {
		t.Errorf("an unclosed interval left no WT-003 finding; findings = %+v", v.Findings)
	}
	if v.Complete || v.Eligible {
		t.Errorf("an unclosed interval verified as complete=%v eligible=%v", v.Complete, v.Eligible)
	}
}

// TestVerifierRefusesAMalformedRecord: a stream that cannot be decoded is an
// ERROR, never an empty record set.
//
// Both would leave a verdict with nothing in it, and the difference is the
// whole point — "there is no evidence here" and "the evidence is unreadable"
// call for different responses, and silently treating the second as the first
// would let a corrupted stream verify as an absent one.
func TestVerifierRefusesAMalformedRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "physical-invocation-00.jsonl")
	if err := os.WriteFile(path, []byte("{not json at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyDir(VerifyOptions{Dir: dir}); err == nil {
		t.Error("a malformed stream verified without an error")
	}
}

// TestACompleteRunIsCompleteButNotYetScorable is the positive control the
// refusals need: without it every refusal above would pass against a verifier
// that refused everything.
//
// It also fixes the boundary between the two levels. A well-formed passed run
// is COMPLETE — the records describe a real measurement — and it is not
// SCORABLE, because nothing has yet checked that it ran the work the plan gave
// it. "Nobody checked" and "it passed" must not reach the same verdict, so the
// absent prerequisites are findings rather than skips.
func TestACompleteRunIsCompleteButNotYetScorable(t *testing.T) {
	dir := t.TempDir()
	if _, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: dir, Cwd: dir, Timeout: 30 * time.Second,
		Run:  RunIdentity{BucketID: "b1", Stage2: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
		Argv: []string{"sh", "-c", "true"},
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	v, err := VerifyDir(VerifyOptions{Dir: dir})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	if !v.Complete {
		t.Errorf("a well-formed passed run is not complete; findings = %+v", v.Findings)
	}
	if v.Eligible {
		t.Error("a run with no invocation manifest and no coverage audit was scored")
	}
	for _, want := range []string{"WT-021", "WT-024"} {
		if !hasFinding(v, want) {
			t.Errorf("no %s finding names the absent prerequisite; findings = %+v", want, v.Findings)
		}
	}
	if v.RecordsDigest == "" {
		t.Error("the verdict names no records digest: a row that cannot name its evidence is a number in a file")
	}
	// The reported duration comes from the records the verifier read, not from
	// anything a producer asserted beside them.
	if len(v.InvocationNs) != 1 || v.InvocationNs[0] <= 0 {
		t.Errorf("invocation durations = %v, want one positive span", v.InvocationNs)
	}
}

// TestARunThatMeetsEveryPrerequisiteIsScorable closes the other side: with the
// plan it was rendered from and an audit of what it ran, the same records ARE
// scorable. Without this the eligibility rules could all be satisfied by
// refusing everything.
func TestARunThatMeetsEveryPrerequisiteIsScorable(t *testing.T) {
	dir := t.TempDir()
	run := RunIdentity{BucketID: "b1", Stage2: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}
	opt := ExecOptions{
		Level: LevelInvocation, Dir: dir, Cwd: dir, Timeout: 30 * time.Second,
		Run:        run,
		Argv:       []string{"sh", "-c", "true"},
		Selector:   []string{"./a.test.ts"},
		UnitDigest: mustDigest([]string{"a.test.ts"}),
		AtomDigest: mustDigest([]string{"suffix:test.ts"}),
	}
	if _, err := Exec(opt); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	// The manifest is built from the SAME identities the plan would have
	// rendered, which is what the comparison is for: a manifest written from
	// the records would agree with them by construction and check nothing.
	manifest := InvocationManifest{
		Kind: InvocationManifestKind, BucketName: "b1", BucketIndex: 0,
		Invocations: []InvocationIdentity{{
			Seq: 0, Cwd: dir,
			ArgvDigest:     mustDigest(opt.Argv),
			SelectorDigest: mustDigest(opt.Selector),
			UnitDigest:     opt.UnitDigest,
			AtomDigest:     opt.AtomDigest,
			Units:          []string{"a.test.ts"},
		}},
	}
	planned := func(string) (*InvocationManifest, error) { return &manifest, nil }
	audit := func(bucket string) (*AuditEvidence, error) {
		return &AuditEvidence{Bucket: bucket, Planned: 1, Reported: 1, Report: "1 of 1 planned target ran"}, nil
	}
	v, err := VerifyDir(VerifyOptions{Dir: dir, Invocations: planned, Audit: audit})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	if !v.Complete || !v.Eligible {
		t.Errorf("a run meeting every prerequisite verified as complete=%v eligible=%v; findings = %+v",
			v.Complete, v.Eligible, v.Findings)
	}

	// And the membership is EXACT: one changed selector makes the same records
	// ineligible, because they no longer ran what the plan rendered.
	manifest.Invocations[0].SelectorDigest = mustDigest([]string{"./b.test.ts"})
	v, err = VerifyDir(VerifyOptions{Dir: dir, Invocations: planned, Audit: audit})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	if v.Eligible {
		t.Error("a run that applied a different test selection than the plan rendered was scored")
	}
	if !hasFinding(v, "WT-021") {
		t.Errorf("no WT-021 finding names the membership mismatch; findings = %+v", v.Findings)
	}

	// A manifest for a DIFFERENT bucket is refused too: it would otherwise
	// verify identically against any bucket of the same plan.
	manifest.Invocations[0].SelectorDigest = mustDigest(opt.Selector)
	manifest.BucketName = "b2"
	v, err = VerifyDir(VerifyOptions{Dir: dir, Invocations: planned, Audit: audit})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	if !hasFinding(v, "WT-025") {
		t.Errorf("no WT-025 finding names the wrong bucket; findings = %+v", v.Findings)
	}

	// A failing coverage audit is TERMINAL, not merely unscorable: a run that
	// did not execute its plan is not a measurement of that plan.
	manifest.BucketName = "b1"
	failing := func(bucket string) (*AuditEvidence, error) {
		return &AuditEvidence{Bucket: bucket, Planned: 2, Reported: 1,
			Problems: []string{"a.test.ts ran; b.test.ts did not"}}, nil
	}
	v, err = VerifyDir(VerifyOptions{Dir: dir, Invocations: planned, Audit: failing})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	if v.Complete || v.Eligible {
		t.Errorf("a bucket that skipped half its plan verified as complete=%v eligible=%v", v.Complete, v.Eligible)
	}
}

// TestTheSchemaIsAnEpochNotAMigration: this binary cannot know what a later
// schema means, and a verifier that guessed would be reporting on records it
// did not understand.
func TestTheSchemaIsAnEpochNotAMigration(t *testing.T) {
	dir := t.TempDir()
	if _, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: dir, Cwd: dir, Timeout: 30 * time.Second,
		Run:  RunIdentity{BucketID: "b1"},
		Argv: []string{"sh", "-c", "true"},
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	recs, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) == 0 {
		t.Fatal("no records")
	}
	recs[0].Schema = "tb.walltime/v2"
	v, err := VerifyDir(VerifyOptions{Dir: dir, Records: recs})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	if !hasFinding(v, "WT-001") {
		t.Errorf("a record from another schema epoch left no WT-001 finding; findings = %+v", v.Findings)
	}
	if v.Complete {
		t.Error("records from a schema this binary does not implement verified as complete")
	}
}
