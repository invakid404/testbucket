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
	w, err := NewWriter(path, ProducerPhysical, "physical", nil)
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

// TestACompleteRunIsCompleteAndScorable is the positive control the refusals
// need: without it every refusal above would pass against a verifier that
// refused everything.
func TestACompleteRunIsCompleteAndScorable(t *testing.T) {
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
	if !v.Complete || !v.Eligible {
		t.Errorf("a well-formed passed run verified as complete=%v eligible=%v; findings = %+v",
			v.Complete, v.Eligible, v.Findings)
	}
	if v.RecordsDigest == "" {
		t.Error("the verdict names no records digest: a row that cannot name its evidence is a number in a file")
	}
}
