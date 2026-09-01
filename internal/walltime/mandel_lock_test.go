package walltime

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The frozen profile's real lock, and the shape that used to defeat the
// parser.
//
// Every lock test beside this one uses a small hand-written fixture, and those
// fixtures all happened to resolve each package exactly once. A real lockfile
// does not: `@ai-sdk/provider-utils` appears at 2.2.8 AND 3.0.30 in the frozen
// Mandel lock, and a closure keyed by package name could not represent that —
// it reported a conflict and refused the whole file. The consequence was not
// cosmetic: no Stage-1 source profile for the frozen target could be derived
// at all, so the completeness rule the previous round added was unreachable
// for the one lock it actually has to read.
//
// testdata/mandel-lock/pnpm-lock.yaml is a byte-exact excerpt of that file;
// see its README for provenance and for why these nodes were kept.

func mandelLockFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mandel-lock", "pnpm-lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestTheParserReadsTheFrozenMandelLockShape is the F3 regression.
func TestTheParserReadsTheFrozenMandelLockShape(t *testing.T) {
	closure, err := DeriveLockClosure(LockParserPNPM, mandelLockFixture(t))
	if err != nil {
		t.Fatalf("the production parser cannot derive the frozen profile's real lock shape: %v", err)
	}

	// The multi-version pair, both present and distinguishable. This is the
	// exact assertion the old name-keyed closure could not satisfy.
	for key, want := range map[string]string{
		"@ai-sdk/provider-utils@2.2.8":  "2.2.8",
		"@ai-sdk/provider-utils@3.0.30": "3.0.30",
	} {
		node, ok := closure[key]
		if !ok {
			t.Fatalf("the closure does not resolve %s; a name-keyed closure cannot hold two versions of one package", key)
		}
		if node.Version != want || node.Name != "@ai-sdk/provider-utils" {
			t.Errorf("%s resolved to name %q version %q", key, node.Name, node.Version)
		}
		if node.Integrity == "" {
			t.Errorf("%s carries no integrity", key)
		}
	}

	// Every node carries a complete identity: the lock's own key, a name, a
	// version and an integrity. A closure with holes is not a closure.
	for key, node := range closure {
		if node.Key != key || node.Name == "" || node.Version == "" || node.Integrity == "" {
			t.Errorf("node %q is incompletely resolved: %+v", key, node)
		}
	}

	// The frozen Vitest family, at the frozen version, from the real entries.
	family := map[string]bool{}
	for _, node := range closure {
		if !IsVitestPackage(node.Name) {
			continue
		}
		family[node.Name] = true
		if node.Version != RequiredVitest {
			t.Errorf("%s resolves at %s, not the frozen %s", node.Key, node.Version, RequiredVitest)
		}
	}
	for _, want := range []string{"vitest", "@vitest/runner", "@vitest/expect", "@vitest/mocker", "@vitest/snapshot", "@vitest/spy", "@vitest/utils"} {
		if !family[want] {
			t.Errorf("the frozen lock excerpt does not resolve %s", want)
		}
	}
}

// TestTheParserReadsTheWholeFrozenMandelLock runs the same derivation over the
// COMPLETE 1.25 MB lockfile, fetched live.
//
// The committed excerpt proves the shape; this proves the whole file, which is
// the thing a real Stage-1 profile would hand the parser. It is skipped rather
// than failed when the network or `gh` is unavailable — an offline machine is
// not a defect — so the excerpt above is the always-on coverage.
func TestTheParserReadsTheWholeFrozenMandelLock(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("no gh on PATH")
	}
	cmd := exec.Command("gh", "api", "-H", "Accept: application/vnd.github.raw+json",
		"repos/mandel-ai/mandel/contents/pnpm-lock.yaml?ref="+FrozenProfileCommit)
	lock, err := cmd.Output()
	if err != nil {
		t.Skipf("cannot fetch the frozen Mandel lockfile: %v", err)
	}
	closure, err := DeriveLockClosure(LockParserPNPM, lock)
	if err != nil {
		t.Fatalf("the production parser cannot derive the frozen profile's real lock closure: %v", err)
	}
	if len(closure) < 100 {
		t.Fatalf("the frozen Mandel lockfile derived only %d node(s)", len(closure))
	}
	sawVitest := false
	for _, node := range closure {
		if !IsVitestPackage(node.Name) {
			continue
		}
		sawVitest = true
		if node.Version != RequiredVitest {
			t.Errorf("%s resolves at %s, not the frozen %s", node.Key, node.Version, RequiredVitest)
		}
	}
	if !sawVitest {
		t.Error("the frozen Mandel lockfile resolves no vitest package")
	}
	t.Logf("derived %d resolved node(s) from the frozen Mandel lockfile", len(closure))
}
