package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/runner/vitestrunner"
)

// These are the acquisition-provenance regressions, and they are asserted on
// the RUNNER because that is where the capture lives.
//
// They used to scan the `wall bundle` builder's source text for the
// expressions that fed a planning-input bundle. That bundle bound a run to a
// signed Stage-1 manifest and is gone; the properties it needed are not, so
// they are checked here against the behaviour instead of against the
// spelling — which is the stronger form, since a rewrite that keeps the words
// and loses the property no longer passes.

// discoveringRunner builds a runner whose discovery subprocess is a verbatim
// stub. It is a plain shell script, not Vitest: what is under test is what the
// acquisition RETAINS about the subprocess it ran, and a stub makes the
// expected argv, cwd and executable exactly known.
func discoveringRunner(t *testing.T, root string) *vitestrunner.Runner {
	t.Helper()
	r, err := vitestrunner.New(vitestrunner.Options{
		Root: root,
		// `sh -c` with a body that prints one discovered file. The runner
		// appends nothing to a verbatim discovery command, so this argv is
		// exactly what must come back.
		DiscoveryCommand: []string{"sh", "-c", `printf '[{"file":"a.test.ts"}]\n'`},
		DiscoveryMode:    "glob",
	})
	if err != nil {
		t.Fatalf("vitestrunner.New: %v", err)
	}
	if _, err := r.Discover(context.Background()); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	return r
}

// TestTheAcquisitionSubprocessRunsWithTheRetainedEnvironment: the record is
// only replayable if the subprocess was actually given it.
//
// A nil Cmd.Env inherits everything and states nothing, so an acquisition that
// recorded an environment it had not applied would describe a process nobody
// could rerun.
func TestTheAcquisitionSubprocessRunsWithTheRetainedEnvironment(t *testing.T) {
	rnr := discoveringRunner(t, t.TempDir())
	seen := rnr.Discovered()
	if seen == nil {
		t.Fatal("the runner retained no discovery provenance")
	}
	if len(seen.Env) == 0 {
		t.Fatal("the acquisition retained no environment, so a replay cannot reconstruct the process")
	}
	// The retained environment is the one the subprocess ran with, not an
	// allow-list of interesting values: every variable this process holds has
	// to be in it.
	for _, kv := range os.Environ() {
		if !slices.Contains(seen.Env, kv) {
			t.Errorf("the retained environment is missing %q; it is a summary rather than the environment that ran", strings.SplitN(kv, "=", 2)[0])
			break
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
	dir := t.TempDir()
	// The caller's spelling is deliberately not canonical: a relative path
	// with a redundant segment, which is what `--root .` amounts to.
	t.Chdir(dir)

	rnr := discoveringRunner(t, "./.")
	root := rnr.Root()
	if !filepath.IsAbs(root) {
		t.Errorf("Root() = %q, which is not absolute: a replay cannot follow the caller's spelling", root)
	}
	if root != filepath.Clean(root) {
		t.Errorf("Root() = %q, which is not canonical", root)
	}
	// And the retained cwd is that same root — the directory the subprocess
	// actually ran from, not the spelling it was configured with.
	if seen := rnr.Discovered(); seen == nil || seen.Cwd != root {
		t.Errorf("the discovery cwd is %q, want the canonical root %q", seen.Cwd, root)
	}
}

// TestTheDiscoveryArgvComesFromTheOperationThatRanIt is part of F6.
//
// The bundle rebuilt its discovery argv from the same flags a second time. The
// two helpers agree today and are not one observed value: a change to how the
// invocation is assembled would make the bundle describe a command nobody
// issued, and nothing would notice.
func TestTheDiscoveryArgvComesFromTheOperationThatRanIt(t *testing.T) {
	rnr := discoveringRunner(t, t.TempDir())
	seen := rnr.Discovered()
	if seen == nil {
		t.Fatal("the runner retained no discovery provenance")
	}
	want := []string{"sh", "-c", `printf '[{"file":"a.test.ts"}]\n'`}
	if !reflect.DeepEqual(seen.Argv, want) {
		t.Errorf("the retained argv is %q, want the argv that ran %q", seen.Argv, want)
	}
	// THE RESOLVED EXECUTABLE, not the head of the command line. A bare name
	// is resolved on PATH, and a replay from a different directory following
	// the unresolved name reaches different bytes or nothing at all.
	if !filepath.IsAbs(seen.Path) {
		t.Errorf("the retained executable %q is not absolute", seen.Path)
	}
	if filepath.Base(seen.Path) != "sh" {
		t.Errorf("the retained executable %q is not the program that ran", seen.Path)
	}
}

// TestDiscoveryFromFrozenBytesRetainsNoInvocation: there is no subprocess to
// describe when the bytes came from a file, and inventing one would be worse
// than reporting none.
func TestDiscoveryFromFrozenBytesRetainsNoInvocation(t *testing.T) {
	rnr, err := vitestrunner.New(vitestrunner.Options{
		Root:          t.TempDir(),
		DiscoveryMode: "glob",
		Frozen:        &vitestrunner.FrozenInputs{Discovery: []byte(`[{"file":"a.test.ts"}]`)},
	})
	if err != nil {
		t.Fatalf("vitestrunner.New: %v", err)
	}
	if _, err := rnr.Discover(context.Background()); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if seen := rnr.Discovered(); seen != nil {
		t.Errorf("a frozen discovery reported an invocation it never issued: %+v", seen)
	}
}
