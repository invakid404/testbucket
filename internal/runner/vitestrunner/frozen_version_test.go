package vitestrunner_test

// The real adapter fixture must be the version the frozen contract names.
//
// Everything else in this package is executable coverage of the Vitest
// lifecycle — façade, list, run, reporter — and that coverage is only evidence
// about the version it actually ran. The fixture pinned 4.1.11 while the frozen
// source-inventory epoch is 4.1.10, so the synthetic source-profile fixtures
// asserted one version and the executable path exercised another: exact 4.1.10
// lifecycle behaviour was never established by the suite that claimed to
// establish it.
//
// These tests are the guard against that drifting again. They read the
// committed manifest and lockfile — the bytes CI installs from — rather than
// the tree on disk, so they fail on the commit that changes the pin instead of
// on whichever machine happens to have installed something else.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

func fixtureFile(t *testing.T, name string) []byte {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "vitest-sample", name))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestTheRealFixturePinsTheFrozenVitestVersion reads the committed manifest.
func TestTheRealFixturePinsTheFrozenVitestVersion(t *testing.T) {
	var pkg struct {
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(fixtureFile(t, "package.json"), &pkg); err != nil {
		t.Fatal(err)
	}
	if got := pkg.DevDependencies["vitest"]; got != walltime.RequiredVitest {
		t.Errorf("testdata/vitest-sample pins vitest %q; the frozen source-inventory epoch is %q, so the real adapter coverage would establish behaviour for a version the contract does not admit",
			got, walltime.RequiredVitest)
	}
}

// TestTheRealFixtureLockResolvesTheFrozenVitestClosure reads the lockfile CI
// installs from, through the same derivation the source-profile receipt uses.
// A manifest pin is a request; the lock is what `npm ci` actually installs.
func TestTheRealFixtureLockResolvesTheFrozenVitestClosure(t *testing.T) {
	closure, err := walltime.DeriveLockClosure(walltime.LockParserNPM, fixtureFile(t, "package-lock.json"))
	if err != nil {
		t.Fatalf("derive the fixture lock closure: %v", err)
	}
	seen := 0
	for name, pkg := range closure {
		if !walltime.IsVitestPackage(name) {
			continue
		}
		seen++
		if pkg.Version != walltime.RequiredVitest {
			t.Errorf("the fixture lock resolves %s at %s, not the frozen %s", name, pkg.Version, walltime.RequiredVitest)
		}
		if pkg.Integrity == "" {
			t.Errorf("the fixture lock records no integrity for %s", name)
		}
	}
	if seen == 0 {
		t.Fatal("the fixture lock resolves no vitest package at all")
	}
	if _, ok := closure["@vitest/runner"]; !ok {
		t.Error("the fixture lock does not resolve @vitest/runner, which the façade loads")
	}
}

// TestTheInstalledVitestIsTheFrozenVersion closes the last gap: the manifest
// and the lock are bytes, and what the adapter actually drove is a process.
// It is skipped, not failed, when the fixture is not installed — that is the
// same gate every other real-adapter test here uses.
func TestTheInstalledVitestIsTheFrozenVersion(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "vitest-sample"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("no npx on PATH")
	}
	if _, err := os.Stat(filepath.Join(root, "node_modules")); err != nil {
		t.Skip("vitest sample not installed (run `npm install` in testdata/vitest-sample)")
	}
	cmd := exec.Command("npx", "--no-install", "vitest", "--version")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ask the installed vitest for its version: %v", err)
	}
	// `vitest/4.1.10 darwin-arm64 node-v26.7.0`
	got := strings.TrimSpace(string(out))
	if !strings.HasPrefix(got, "vitest/"+walltime.RequiredVitest+" ") && got != "vitest/"+walltime.RequiredVitest {
		t.Errorf("the installed fixture reports %q; the real adapter coverage must run the frozen %s", got, walltime.RequiredVitest)
	}
}
