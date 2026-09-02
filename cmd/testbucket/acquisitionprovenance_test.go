package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// TestPlanningEnvIsTheEnvironmentTheAcquisitionRanWith is the F6 regression.
//
// Two defects, one cause. The subprocesses ran with a nil `Cmd.Env` and
// inherited everything, and the bundle recorded exact values only for an
// allow-list — binding every other inherited variable by the DIGEST of its
// value. A digest says that a variable had some value; nobody can rerun
// `vitest list` from a hash, so the plan was derived under an environment the
// bundle could not reconstruct, and this test used to assert that a digest was
// sufficient.
//
// The record and the executed environment are now built from one read, so they
// cannot differ, and the only values withheld are the wall-time secret and
// capability keys — withheld from the SUBPROCESS as well, so nothing is
// claimed to have run with a value that was not there.
func TestPlanningEnvIsTheEnvironmentTheAcquisitionRanWith(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("NODE_OPTIONS", "--max-old-space-size=4096")
	t.Setenv("TB_ARBITRARY_PLANNING_INPUT", "exact-value-needed-for-replay")
	t.Setenv(walltime.RunKeyEnv, "a-signing-key")

	record, env := planningEnvironment()
	for _, name := range []string{"PATH", "NODE_OPTIONS", "TB_ARBITRARY_PLANNING_INPUT"} {
		if record[name] != os.Getenv(name) {
			t.Errorf("the retained environment records %s as %q, not the exact value %q the acquisition ran under; a replay cannot reconstruct a subprocess from a description of it",
				name, record[name], os.Getenv(name))
		}
	}
	// THE RECORD IS THE ENVIRONMENT. Every KEY=VALUE handed to the subprocess
	// is in the record with the same value, and nothing in the record claims a
	// variable the subprocess did not get.
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	for k, v := range record {
		if strings.HasPrefix(k, "digest:") {
			continue
		}
		if have, ok := got[k]; !ok || have != v {
			t.Errorf("the record binds %s=%q but the subprocess environment has %q (present=%v)", k, v, have, ok)
		}
	}
	for k, v := range got {
		if record[k] != v {
			t.Errorf("the subprocess ran with %s=%q, which the record does not bind", k, v)
		}
	}
	// THE SECRET AND CAPABILITY KEYS are withheld from both, and named by
	// digest so their presence and any change to them is still visible.
	for _, secret := range walltime.WallTimeSecretEnv {
		if _, leaked := got[secret]; leaked && os.Getenv(secret) != "" {
			t.Errorf("the acquisition subprocess was handed %s", secret)
		}
	}
	if record[walltime.RunKeyEnv] == "a-signing-key" {
		t.Error("planningEnv wrote a signing key into a bundle meant to be published")
	}
	if record["digest:"+walltime.RunKeyEnv] == "" {
		t.Errorf("planningEnv omits %s entirely; the recorded set is not the set the process held", walltime.RunKeyEnv)
	}
	for k, v := range record {
		if v == "a-signing-key" {
			t.Errorf("planningEnv leaked a signing key under %q", k)
		}
	}
}

// TestTheAcquisitionSubprocessRunsWithTheRetainedEnvironment: the record is
// only replayable if the subprocess was actually given it.
func TestTheAcquisitionSubprocessRunsWithTheRetainedEnvironment(t *testing.T) {
	b, err := os.ReadFile("wallplan.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Env: planningEnvArgs(),") {
		t.Error("the runner is not given the retained environment, so the acquisition inherits whatever is ambient and the bundle describes something else")
	}
	exec, err := os.ReadFile(filepath.Join("..", "..", "internal", "runner", "vitestrunner", "exec.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exec), "cmd.Env = t.env") {
		t.Error("the acquisition subprocess still runs with a nil Cmd.Env")
	}
	if !strings.Contains(string(exec), "cmd.Path") {
		t.Error("the acquisition does not retain the executable exec.Command actually resolved")
	}
}

// TestTheAcquisitionRootIsTheCanonicalAbsolutePath is part of F6.
//
// `--root` defaults to `.`, and the bundle recorded the caller's spelling as
// the cwd every subprocess ran from. "." names a different directory from
// every other working directory in the world, so a replay could not know where
// discovery had run.
func TestTheAcquisitionRootIsTheCanonicalAbsolutePath(t *testing.T) {
	b, err := os.ReadFile("wallplan.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(b)
	if !strings.Contains(source, "acquiredRoot := rnr.Root()") {
		t.Error("the bundle does not record the canonical root the runner actually used")
	}
	if strings.Contains(source, "Root: *root, Runner: \"vitest\"") {
		t.Error("the bundle still records the caller's spelling of --root as the acquisition cwd")
	}
	if !strings.Contains(source, "Resolve: closureResolver(acquiredRoot, observedPaths)") {
		t.Error("the executable closure is resolved against a root other than the one that ran")
	}
}

// TestTheDiscoveryArgvComesFromTheOperationThatRanIt is part of F6.
//
// The bundle rebuilt its discovery argv from the same flags a second time. The
// two helpers agree today and are not one observed value: a change to how the
// invocation is assembled would make the bundle describe a command nobody
// issued, and nothing would notice.
func TestTheDiscoveryArgvComesFromTheOperationThatRanIt(t *testing.T) {
	b, err := os.ReadFile("wallplan.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(b)
	if !strings.Contains(source, "if seen := rnr.Discovered(); seen != nil {") {
		t.Error("the bundle does not take the discovery argv from the operation that issued it")
	}
	if strings.Contains(source, "DiscoveryArgv: discoveryArgv(") {
		t.Error("the bundle still reconstructs its own discovery argv")
	}
}

// TestTheResolvedExecutableIsAbsolute is part of F6: a relative
// `node_modules/.bin/...` names a different program in every working
// directory, which is an executable identity that is not an identity.
func TestTheResolvedExecutableIsAbsolute(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "vitest"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(mustWD(t), root)
	if err != nil {
		t.Skip("the temp dir is not relative to the working directory on this host")
	}
	got, err := delegatedProgram(rel, []string{"npx", "vitest", "list"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.path != "" && !filepath.IsAbs(got.path) {
		t.Errorf("the resolved executable is %q, which names a different program in every working directory", got.path)
	}
}

func mustWD(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}
