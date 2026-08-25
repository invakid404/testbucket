package gorunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/invakid404/testbucket/internal/runner"
)

// testToolchain is the subprocess runner the toolchain-backed tests use. Each
// `go` invocation gets its own deadline, so a hung one fails that single
// command instead of blocking the suite.
func testToolchain() toolchain { return toolchain{timeout: 2 * time.Minute} }

// sampleExcludes is a neutral exclusion set exercising every shape excluded()
// must handle: a glob whose `*` does not cross `/`, plain literals, and nesting
// at arbitrary depth. The tool ships with NO default exclusions; a consumer
// supplies a set like this one.
var sampleExcludes = []string{
	"ext/dep_*",
	"thirdparty/pinned",
	"cgomod",
	"internal/native/prep",
}

// TestListRunnableNamesAgainstTheRealToolchain is the one test here that shells
// out. Everything else runs against a synthetic tree, but the runnable universe
// is defined by `go test -list`'s own behaviour, and that is exactly what P0-1
// got wrong: `-list '^Test'` hides Examples and Fuzz targets that `-run` would
// have selected. Asserting the filter in isolation cannot catch a wrong regexp
// handed to the toolchain, so this builds a throwaway module and asks the real
// `go`.
func TestListRunnableNamesAgainstTheRealToolchain(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a test binary; skipped under -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}

	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/runnables\n\ngo 1.27\n")
	write("x.go", "package runnables\n\nimport \"fmt\"\n\n// Greet prints a greeting.\nfunc Greet() { fmt.Println(\"hi\") }\n")
	write("x_test.go", `package runnables

import "testing"

func TestGreet(t *testing.T) { Greet() }

func BenchmarkGreet(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Greet()
	}
}

func FuzzGreet(f *testing.F) {
	f.Fuzz(func(t *testing.T, s string) { _ = s })
}

func ExampleGreet() {
	Greet()
	// Output: hi
}

// ExampleGreet_second has no Output comment, so the test binary never
// registers it and -run cannot select it.
func ExampleGreet_second() { Greet() }
`)

	p := runner.LivePackage{
		ID:     "example.com/runnables",
		Dir:    ".",
		Module: ".",
		Mode:   runner.ModeOff, // resolve against this module alone, no go.work

		HasTests: true,
	}
	got, err := listRunnableNames(context.Background(), testToolchain(), dir, p)
	if err != nil {
		t.Fatalf("listRunnableNames: %v", err)
	}

	// Exactly what `go test -run` can select: the test, the fuzz target and the
	// runnable example. Not the benchmark (-bench selects those), and not the
	// example the binary never registers.
	want := []string{"ExampleGreet", "FuzzGreet", "TestGreet"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runnable universe = %v, want %v", got, want)
	}
}

func TestExcludedMatchesWholePathElementsAtAnyDepth(t *testing.T) {
	// The module set is a coverage boundary: a module wrongly excluded here
	// takes every test in it out of the plan, and the coverage gate cannot
	// notice, because it only ever sees the packages `go list` returned. So
	// both directions matter, and both are pinned.
	patterns := sampleExcludes
	cases := []struct {
		rel  string
		want bool
		why  string
	}{
		// --- must be excluded ---
		{"ext/dep_v0", true, "the glob's direct match"},
		{"ext/dep_v0/adapter", true, "one level under the glob"},
		{"ext/dep_v9/internal/tool", true, "TWO levels under the glob — `*` does not cross `/`, so a one-level pattern would miss this"},
		{"ext/dep_v1/a/b/c/d", true, "arbitrary depth under the glob"},
		{"thirdparty/pinned", true, "literal exclusion"},
		{"thirdparty/pinned/internal/x", true, "nested under a literal exclusion"},
		{"cgomod", true, "literal exclusion"},
		{"cgomod/deep/nest", true, "nested under a literal exclusion"},
		{"internal/native/prep", true, "literal exclusion"},
		{"internal/native/prep/sub", true, "nested under a literal exclusion"},

		// --- must NOT be excluded: each is a real module or a name that only
		// LOOKS like an excluded one ---
		{".", false, "the root module"},
		{"ext/common", false, "ext/common is IN the set; the glob must not reach it"},
		{"ext/common/codegen", false, "and neither must its packages"},
		{"ext", false, "the bare parent of the excluded deps is not itself excluded"},
		{"netpkg", false, "an ordinary module"},
		{"netpkg/streamer", false, "an ordinary package"},
		{"thirdparty", false, "the parent of an excluded module stays in the set"},
		{"thirdpartyfoo", false, "prefix collision must not exclude a different module"},
		{"cgomodfoo", false, "prefix collision: a pattern matches whole path elements, not text prefixes"},
		{"cgomod-tools", false, "another prefix collision"},
		{"internal/native", false, "the parent of an excluded module stays in the set"},
		{"internal/native/prep2", false, "sibling whose name extends an excluded one"},
		{"pool", false, ""},
		{"worker", false, ""},
		{"workerplugin", false, ""},
		{"introspected", false, ""},
	}
	for _, tc := range cases {
		got := excluded(tc.rel, patterns)
		if got != tc.want {
			verb := "was excluded"
			if !got {
				verb = "was NOT excluded"
			}
			t.Errorf("%s %s, want excluded=%v — %s", tc.rel, verb, tc.want, tc.why)
		}
	}

	// An exclusion pattern is normalised the same way the candidate dir is. A
	// perfectly reasonable spelling that silently matched nothing would be the
	// worst outcome for a knob whose only job is to scope the module set — the
	// user believes a module is excluded and it quietly is not.
	spellings := []struct {
		pat  string
		rel  string
		want bool
		why  string
	}{
		{"./cgomod", "cgomod", true, "a leading ./ is how most people type a repo-relative path"},
		{"./cgomod", "cgomod/deep", true, "and it must still nest"},
		{"cgomod//x", "cgomod/x", true, "a doubled separator"},
		{"ext/./dep_*", "ext/dep_v1", true, "a /./ segment inside a glob pattern"},
		{"ext/./dep_*", "ext/dep_v1/a/b", true, "and it must still nest at depth"},
		{"cgomod/", "cgomod", true, "a trailing slash was already handled"},
		{"  cgomod  ", "cgomod", true, "surrounding whitespace was already handled"},
		// Normalising must not widen a pattern into matching more.
		{"./cgomod", "cgomodfoo", false, "normalisation must not turn a pattern into a text prefix"},
		{"ext/./dep_*", "ext/common", false, "normalisation must not reach a sibling"},
	}
	for _, tc := range spellings {
		if got := excluded(tc.rel, []string{tc.pat}); got != tc.want {
			t.Errorf("excluded(%q, [%q]) = %v, want %v — %s", tc.rel, tc.pat, got, tc.want, tc.why)
		}
	}
}

func TestDiscoverModulesKeepsEveryLiveModuleAndDropsOnlyExcludedOnes(t *testing.T) {
	// The end-to-end form of the same guarantee, on a hermetic tree: a
	// workspace with members, one GOWORK=off module that must stay, the
	// excluded modules INCLUDING one nested two levels down, a prefix-colliding
	// sibling, and a go.mod fixture under testdata.
	if testing.Short() {
		t.Skip("runs `go work edit -json`; skipped under -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}

	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mod := func(dir, name string) {
		t.Helper()
		write(filepath.ToSlash(filepath.Join(dir, "go.mod")), "module "+name+"\n\ngo 1.27\n")
	}

	write("go.work", "go 1.27\n\nuse (\n\t.\n\t./netpkg\n\t./pool\n)\n")
	mod(".", "example.com/root")
	mod("netpkg", "example.com/root/netpkg")
	mod("pool", "example.com/root/pool")
	// Out of the workspace but IN the module set: must be discovered as an
	// atomic GOWORK=off module.
	mod("ext/common", "example.com/root/ext/common")
	// Excluded by the glob, at three different depths.
	mod("ext/dep_v1", "example.com/root/ext/dep_v1")
	mod("ext/dep_v1/internal/tool", "example.com/root/ext/dep_v1/internal/tool")
	mod("ext/dep_v2/a/b/c", "example.com/root/ext/dep_v2/a/b/c")
	// Excluded by literal patterns.
	mod("cgomod", "example.com/root/cgomod")
	mod("thirdparty/pinned", "example.com/root/thirdparty/pinned")
	// Prefix collision: must survive.
	mod("cgomodfoo", "example.com/root/cgomodfoo")
	// A fixture module under testdata: data, not a module of this repo.
	mod("internal/schema/testdata/broken", "example.com/fixture")

	mods, err := discoverModules(context.Background(), testToolchain(), root, sampleExcludes)
	if err != nil {
		t.Fatalf("discoverModules: %v", err)
	}
	got := map[string]moduleSpec{}
	for _, m := range mods {
		got[m.Dir] = m
	}

	want := map[string]moduleSpec{
		".":          {Dir: ".", Mode: runner.ModeWork, Atomic: false},
		"netpkg":     {Dir: "netpkg", Mode: runner.ModeWork, Atomic: false},
		"pool":       {Dir: "pool", Mode: runner.ModeWork, Atomic: false},
		"ext/common": {Dir: "ext/common", Mode: runner.ModeOff, Atomic: true},
		"cgomodfoo":  {Dir: "cgomodfoo", Mode: runner.ModeOff, Atomic: true},
	}
	for dir, spec := range want {
		g, ok := got[dir]
		if !ok {
			t.Errorf("module %s was DROPPED from the module set; every test in it would silently stop running", dir)
			continue
		}
		if g != spec {
			t.Errorf("module %s = %+v, want %+v", dir, g, spec)
		}
	}
	for dir := range got {
		if _, ok := want[dir]; !ok {
			t.Errorf("module %s was discovered but should be outside the module set", dir)
		}
	}
	if len(got) != len(want) {
		t.Errorf("discovered %d modules, want %d: %v", len(got), len(want), runner.SortedKeys(got))
	}
}

func TestWorkspaceMembersDistinguishesAbsenceFromFailure(t *testing.T) {
	// A stat failure that is not "absent" must not be read as "there is no
	// workspace": that would flip every module to GOWORK=off and pack each as a
	// whole-module atom, silently rescheduling the tree on the back of an I/O
	// error.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}
	root := t.TempDir()

	// No go.work at all: a legitimate shape, no error, no members.
	members, err := workspaceMembers(context.Background(), testToolchain(), root)
	if err != nil {
		t.Fatalf("a missing go.work must not be an error: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("members = %v, want none", members)
	}

	// A DANGLING SYMLINK is the ambiguous case, and the one that used to slip
	// through: os.Stat follows links, so a go.work pointing at nothing reports
	// the missing TARGET as ENOENT and looks exactly like "no workspace here".
	// A broken workspace must be loud — silently treating it as absent flips
	// every module to GOWORK=off/atomic and reschedules the whole tree, and
	// discovery runs before the final plan is ever gated, so nothing downstream
	// would catch it.
	dangling := t.TempDir()
	if err := os.Symlink(filepath.Join(dangling, "no-such-file"), filepath.Join(dangling, "go.work")); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	// Sanity: confirm the fixture really is the ambiguous shape — Stat says
	// "does not exist" while the directory entry is right there.
	if _, err := os.Stat(filepath.Join(dangling, "go.work")); !os.IsNotExist(err) {
		t.Fatalf("fixture is not a dangling symlink: stat err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dangling, "go.work")); err != nil {
		t.Fatalf("fixture has no directory entry: lstat err = %v", err)
	}
	members, err = workspaceMembers(context.Background(), testToolchain(), dangling)
	if err == nil {
		t.Errorf("a dangling go.work symlink was reported as an empty workspace (%v); every module would silently become GOWORK=off/atomic", members)
	} else if !strings.Contains(err.Error(), "dangling symlink") {
		t.Errorf("error does not name the cause: %v", err)
	}

	// A symlink that RESOLVES is a normal workspace and must still work.
	linked := t.TempDir()
	real := filepath.Join(linked, "real.work")
	if err := os.WriteFile(real, []byte("go 1.27\n\nuse (\n\t.\n)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, "go.mod"), []byte("module example.com/linked\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(linked, "go.work")); err != nil {
		t.Fatal(err)
	}
	if members, err := workspaceMembers(context.Background(), testToolchain(), linked); err != nil {
		t.Errorf("a resolvable go.work symlink was rejected: %v", err)
	} else if !members["."] {
		t.Errorf("members = %v, want the root module", members)
	}

	// go.work present but unreadable as a directory entry: `go work edit` fails
	// and the error must surface rather than degrade to "no members".
	if err := os.Mkdir(filepath.Join(root, "go.work"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaceMembers(context.Background(), testToolchain(), root); err == nil {
		t.Error("an unusable go.work was reported as an empty workspace")
	}
}
