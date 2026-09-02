package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanningEnvBindsTheWholeInheritedEnvironment is part of F6.
//
// Discovery and runnable subprocesses run with a nil `Cmd.Env` and therefore
// inherit everything. The bundle recorded five allow-listed variables, so
// `PATH`, `NODE_OPTIONS`, package-manager configuration and proxy settings —
// every one of which can change which tool runs and how — were neither frozen
// nor eliminated. An allow-list records what the author thought mattered
// rather than what the plan was derived under.
//
// Publishing every value would publish secrets, so the remainder is bound by
// the digest of its value: the SET is complete, a change to any of it is
// visible, and nothing secret is written into a document meant to be
// published.
func TestPlanningEnvBindsTheWholeInheritedEnvironment(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("NODE_OPTIONS", "--max-old-space-size=4096")
	t.Setenv("TB_TEST_SECRET_TOKEN", "hunter2")

	env := planningEnv()
	for _, name := range []string{"PATH", "NODE_OPTIONS"} {
		if env[name] == "" {
			t.Errorf("planningEnv does not record %s, which selects which tool runs", name)
		}
	}
	// Everything else is present by name, bound by digest, and never by value.
	digested, ok := env["digest:TB_TEST_SECRET_TOKEN"]
	if !ok {
		t.Error("planningEnv omits an inherited variable entirely; the recorded set is not the set the subprocess inherited")
	}
	if digested == "hunter2" {
		t.Error("planningEnv wrote a secret's value into a bundle meant to be published")
	}
	for k, v := range env {
		if v == "hunter2" {
			t.Errorf("planningEnv leaked a secret value under %q", k)
		}
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
	if !strings.Contains(source, "Resolve: closureResolver(acquiredRoot)") {
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
