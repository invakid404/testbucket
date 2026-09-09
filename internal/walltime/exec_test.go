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
	if !hasFinding(v, "WT-002") {
		t.Errorf("a failed child left no WT-002 finding; findings = %+v", v.Findings)
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
