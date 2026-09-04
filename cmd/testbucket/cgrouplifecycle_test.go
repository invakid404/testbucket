package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// THE CLI LIFECYCLE, ON A REAL DELEGATED CGROUP.
//
// The in-process lifecycle tests move the TEST RUNNER into the action
// containment, so on a host where cgroup containment really works they are
// measuring a process that is also the test harness — and the harness dies
// with the containment. They prove the code path; they cannot prove the
// published setup procedure, because the thing they exercise is not the thing
// an operator runs.
//
// This drives the SHIPPED CLI the way the run-bucket action does — `wall
// begin`, an action-owned `wall run`, `wall end`, `wall verify`, each its own
// process — against whatever TB_WALL_CGROUP_ROOT names. It skips where no
// delegated subtree exists, and where one does it fails if the envelope cannot
// open, cannot contain an action-owned child, or does not close on a real
// cgroup-v2 containment.
//
// Exercise it with the procedure the action publishes:
//
//	own=$(awk -F: '$1 == "0" { print $3 }' /proc/self/cgroup)
//	sudo mkdir -p "/sys/fs/cgroup${own}/testbucket"
//	sudo chown "$(id -un)" "/sys/fs/cgroup${own}" "/sys/fs/cgroup${own}/cgroup.procs"
//	sudo chown -R "$(id -un)" "/sys/fs/cgroup${own}/testbucket"
//	TB_WALL_CGROUP_ROOT="/sys/fs/cgroup${own}/testbucket" go test ./cmd/testbucket/ -run TestTheCLIActionLifecycle
func TestTheCLIActionLifecycleRunsInARealDelegatedCgroup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("cgroup-v2 containment is a Linux property")
	}
	root := strings.TrimSpace(os.Getenv(walltime.CgroupRootEnv))
	if root == "" {
		t.Skipf("%s is unset: no delegated subtree to exercise the published procedure against", walltime.CgroupRootEnv)
	}
	if err := walltime.CheckCgroupDelegation(root); err != nil {
		t.Skipf("%s is not a usable delegation, so the CLI lifecycle cannot be exercised here: %v", root, err)
	}

	bin := filepath.Join(t.TempDir(), "testbucket")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the CLI under test: %v\n%s", err, out)
	}

	dir := filepath.Join(t.TempDir(), "wall")
	cli := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := cli("wall", "begin", "--dir", dir, "--bucket-id", "cli-lifecycle",
		"--campaign-id", "cli", "--run-id", "1", "--attempt-id", "1",
		"--repository", "invakid404/testbucket", "--workflow-run", "1",
		"--job", "cli", "--step", "run-bucket", "--step-attempt", "1"); err != nil {
		t.Fatalf("the action envelope would not open on a delegated cgroup: %v\n%s", err, out)
	}
	// An ACTION-OWNED child, admitted to the same containment the wrapper
	// joined. This is the migration the published setup has to make possible.
	if out, err := cli("wall", "run", "--dir", dir, "--", "/bin/sh", "-c", "exit 0"); err != nil {
		t.Fatalf("an action-owned command could not run inside the action containment: %v\n%s", err, out)
	}
	if out, err := cli("wall", "end", "--dir", dir, "--terminal", "passed"); err != nil {
		t.Fatalf("the action envelope would not close: %v\n%s", err, out)
	}

	// STDOUT ONLY for the verdict: `wall verify` writes an unsigned-verdict
	// warning to stderr, and a JSON document is not the concatenation of the
	// two.
	verify := exec.Command(bin, "wall", "verify", "--dir", dir, "--json", "--require", "complete")
	verify.Env = os.Environ()
	var problems strings.Builder
	verify.Stderr = &problems
	stdout, err := verify.Output()
	out := string(stdout)
	if err != nil {
		t.Fatalf("the records of a real cgroup lifecycle are not complete: %v\n%s\n%s", err, problems.String(), out)
	}
	var verdict struct {
		Complete bool `json:"complete"`
		Findings []struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		} `json:"findings"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &verdict); jsonErr != nil {
		t.Fatalf("the verdict is not JSON: %v\n%s", jsonErr, out)
	}
	if !verdict.Complete {
		t.Fatalf("the lifecycle did not produce complete records:\n%s", out)
	}

	// AND IT WAS REALLY A CGROUP. A process-group fallback would pass every
	// check above while proving nothing about containment, which is the whole
	// reason the delegation has to work.
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read the records directory: %v", readErr)
	}
	streams := ""
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		streams += string(b)
	}
	if streams == "" {
		t.Fatal("the lifecycle wrote no producer ledgers at all")
	}
	if !strings.Contains(streams, walltime.PrimitiveCgroup2) {
		t.Fatalf("the lifecycle ran on a %s fallback, not a cgroup-v2 containment; the delegated subtree was not used", walltime.PrimitiveProcessGroup)
	}
}
