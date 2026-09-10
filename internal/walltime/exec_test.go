package walltime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestExecPropagatesChildFailure is failure propagation: the wrapper's own exit
// status is the measured command's, and a failed run is not scorable.
//
// A wrapper that reported its own success would make every red test run look
// green to the step that called it.
func TestExecPropagatesChildFailure(t *testing.T) {
	dir := t.TempDir()
	code, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: dir, Argv: []string{"sh", "-c", "exit 3"}, Cwd: dir,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
	v, err := VerifyDir(VerifyOptions{Dir: dir})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	if !hasFinding(v, "WT-014") {
		t.Errorf("a failed child left no WT-014 finding; findings = %+v", v.Findings)
	}
	if v.Eligible {
		t.Errorf("a failed run must not be scorable")
	}
}

// TestExecPassesTheChildOutputThrough is a regression test with a scar: a
// wrapper that swallows the test log is worse than no wrapper, and a nil
// *os.File stored in an io.Writer is a non-nil interface holding a nil
// pointer, so the obvious nil check does not catch it.
func TestExecPassesTheChildOutputThrough(t *testing.T) {
	dir := t.TempDir()
	out, err := os.CreateTemp(dir, "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if _, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: dir, Cwd: dir, Timeout: 30 * time.Second,
		Argv:   []string{"sh", "-c", "echo the-test-log; echo the-error-log >&2"},
		Stdout: out, Stderr: out,
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	b, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"the-test-log", "the-error-log"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the child's output did not reach the caller: wanted %q in %q", want, b)
		}
	}
}

// TestEnvelopeBracketsTheWholeCall: the recorded interval must lie INSIDE the
// call that produced it. An envelope that starts before the caller entered or
// ends after it returned is measuring something other than the call.
func TestEnvelopeBracketsTheWholeCall(t *testing.T) {
	dir := t.TempDir()
	clock := NewSystemClock()
	before := clock.Now()
	opt := ExecOptions{
		Level: LevelInvocation, Dir: dir, Cwd: dir, Timeout: 30 * time.Second,
		Argv: []string{"sh", "-c", "true"},
	}
	if _, err := Exec(opt); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	after := clock.Now()
	recs, err := ReadRecords(filepath.Join(dir, execStreamName(opt)))
	if err != nil {
		t.Fatal(err)
	}
	var start, end Nanos
	for _, r := range recs {
		switch r.Boundary {
		case "start":
			start = r.Instant.Mono
		case "end":
			end = r.Instant.Mono
		}
	}
	if start < before.Mono || end > after.Mono {
		t.Errorf("the envelope [%d,%d] is not inside the call [%d,%d]", start, end, before.Mono, after.Mono)
	}
}

// TestRunInActionRunsTheAdmittedCommand is the property the composite action's
// bucket step depends on: the command runs, and its status is the step's.
//
// The bucket step takes its own exit status from here, so a swallowed failure
// would turn a red bucket into a green job.
func TestRunInActionRunsTheAdmittedCommand(t *testing.T) {
	dir := t.TempDir()
	run := RunIdentity{BucketID: "b1", Stage2: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}
	if _, err := BeginAction(dir, run, 30*time.Second); err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	t.Cleanup(func() { _, _ = EndAction(dir, TerminalPassed, "") })

	marker := filepath.Join(dir, "child-ran")
	code, err := RunInAction(dir, []string{"sh", "-c", "touch " + marker}, dir, nil, nil)
	if err != nil {
		t.Fatalf("RunInAction: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the admitted command did not run: %v", err)
	}

	// And it propagates a failure: the bucket step takes its status from here.
	code, err = RunInAction(dir, []string{"sh", "-c", "exit 7"}, dir, nil, nil)
	if err != nil {
		t.Fatalf("RunInAction: %v", err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

// TestTheRecordedCwdIsTheExecutedAbsoluteDirectory is §13.1's cwd identity.
//
// The plan renders a repo-root-relative directory and every side used to hash
// that string. QC7a therefore compared two copies of the same relative text
// and passed no matter which absolute root each job resolved it under — the
// check existed and could not fail for the reason it was written for.
func TestTheRecordedCwdIsTheExecutedAbsoluteDirectory(t *testing.T) {
	target := t.TempDir()
	records := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, target)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(rel) {
		t.Skipf("no relative path from %s to %s", wd, target)
	}

	if _, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: records, Argv: []string{"sh", "-c", "true"},
		Cwd: rel, Timeout: 30 * time.Second,
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	recs, err := ReadDir(records)
	if err != nil {
		t.Fatal(err)
	}
	var seen int
	for _, r := range recs {
		if r.Spec == nil {
			continue
		}
		seen++
		if !filepath.IsAbs(r.Spec.Cwd) {
			t.Errorf("the record carries cwd %q, which is the relative string the plan rendered", r.Spec.Cwd)
		}
		if r.Spec.Cwd != filepath.Clean(target) {
			t.Errorf("the record carries cwd %q, want the directory the command ran in, %q",
				r.Spec.Cwd, filepath.Clean(target))
		}
	}
	if seen == 0 {
		t.Fatal("the run recorded no spec identity at all")
	}
}

// TestAbsCwdNormalizes covers the two forms a rendered dir arrives in: the
// empty string the repo root is spelled as, and a path with traversal in it.
// Both have to reduce to one value, because the digest is of the text.
func TestAbsCwdNormalizes(t *testing.T) {
	if AbsCwd("") != AbsCwd(".") {
		t.Errorf("AbsCwd(%q) = %q but AbsCwd(%q) = %q", "", AbsCwd(""), ".", AbsCwd("."))
	}
	if got, want := AbsCwd("a/../b"), AbsCwd("b"); got != want {
		t.Errorf("AbsCwd(%q) = %q, want %q", "a/../b", got, want)
	}
	if !filepath.IsAbs(AbsCwd("b")) {
		t.Errorf("AbsCwd(%q) = %q, which is not absolute", "b", AbsCwd("b"))
	}
}
