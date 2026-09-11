package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// TestAFailedActionReportsItsExitCodeAndReason is §13's "how it ended".
//
// `wall end` records a terminal and a reason and no process block, because an
// action is not a process. Assembly read the absent action process's exit code
// anyway, so an action whose script exited 7 — with the physical record
// carrying exit_kind failed, exit_code 7 and reason "exit status 7" — was
// serialized as `"terminal": "failed", "exit_code": 0, "failure_reason": ""`.
// The observation reported a failure it could not describe, and the two fields
// §13 names as the retained "how it ended" surface were both empty.
//
// QC9 already keeps a failed-terminal row out of training, so this is an
// observation-truth defect rather than a training one — which is exactly why
// nothing else caught it.
func TestAFailedActionReportsItsExitCodeAndReason(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	records := filepath.Join(dir, "records")
	if err := os.MkdirAll(records, 0o755); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	// runExpectingFailure is `wall exec` over a command that exits non-zero.
	// The wrapper's own status IS the measured command's, so a non-zero exit
	// here is the point rather than a failure of the test.
	runExpectingFailure := func(want int, args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		err := cmd.Run()
		var ee *exec.ExitError
		if err == nil {
			t.Fatalf("%v exited 0; the measured command's status must propagate", args)
		}
		if !asExitError(err, &ee) || ee.ExitCode() != want {
			t.Fatalf("%v exited %v, want %d\n%s", args, err, want, stderr.String())
		}
	}

	run("wall", "begin", "--dir", records, "--bucket-id", "bucket-0")
	run("wall", "run", "--dir", records, "--", "sh", "-c", "true")
	// The planned invocation SUCCEEDS; the script around it then fails. This
	// is the shape that made the loss visible: a green invocation inside a red
	// script, so nothing but the script's own record carries the status.
	// THE SELECTION IDENTITIES THE RENDERER PASSES, which the assembler needs to
	// produce a consistent document: it takes the LISTS from the plan and the
	// DIGESTS from the record, so a spec with no unit digest yields an invocation
	// whose units contradict the digest naming them. The real renderer always
	// writes both through its spec file.
	inner := bin + " wall exec --dir " + records + " --level invocation" +
		" --bucket-id bucket-0 --cwd " + dir +
		" --selector ./f0.test.ts --unit-digest " +
		string(walltime.DigestJSONOrEmpty([]string{"f0.test.ts"})) +
		" -- sh -c true"
	runExpectingFailure(7, "wall", "exec", "--dir", records, "--level", "script",
		"--bucket-id", "bucket-0", "--cwd", dir, "--", "sh", "-c", inner+"; exit 7")
	run("wall", "end", "--dir", records, "--terminal", "failed", "--reason", "the bucket script failed")

	plan, _ := writePlanDeclaring(t, dir, "bucket-0", 0, []string{"sh", "-c", "true"}, dir,
		observedProfileOf(t, bin))
	obsFile := filepath.Join(dir, "observation.json")
	cmd := exec.Command(bin, "wall", "assemble-observation",
		"--dir", records, "--shard-plan", plan, "--bucket-name", "bucket-0",
		"--out", obsFile, "--runs-on-label", "ubuntu-latest",
		"--repository", "owner/name", "--run-id", "run-9", "--attempt-id", "1",
		"--job", "job-1", "--head-sha", strings.Repeat("1", 40),
		"--candidate-sha", strings.Repeat("2", 40),
		"--workload-commit", strings.Repeat("3", 40))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("assemble-observation failed: %v\n%s", err, stderr.String())
	}

	var obs struct {
		Terminal      string `json:"terminal"`
		ExitCode      int    `json:"exit_code"`
		FailureReason string `json:"failure_reason"`
		Invocations   []struct {
			ExitCode int `json:"exit_code"`
		} `json:"invocations"`
	}
	b, err := os.ReadFile(obsFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &obs); err != nil {
		t.Fatal(err)
	}
	if obs.Terminal != "failed" {
		t.Errorf("terminal is %q, want failed", obs.Terminal)
	}
	if obs.ExitCode != 7 {
		t.Errorf("exit_code is %d, want the 7 the script exited with", obs.ExitCode)
	}
	if obs.FailureReason == "" {
		t.Error("failure_reason is empty on a failed action; §13 retains it as part of how it ended")
	}
	// The invocation really did pass, and that stays true: the two levels
	// report their own outcomes.
	if len(obs.Invocations) != 1 || obs.Invocations[0].ExitCode != 0 {
		t.Errorf("invocation exit codes are %v, want the one that passed", obs.Invocations)
	}
}

// asExitError narrows an *exec.ExitError without pulling errors.As's generic
// shape into every call site.
func asExitError(err error, out **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*out = ee
	}
	return ok
}
