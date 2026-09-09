package consumers

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// checkoutEnv names the environment variable that points at a checkout of the
// pinned workload revision.
const checkoutEnv = "TB_MANDEL_CHECKOUT"

// TestPinnedConsumerCheckoutMatchesManifest is §22 test 39 (S6/R8-D8).
//
// It is CHECKOUT-BOUND, against a checkout at the pinned revision, and it is
// deliberately a DIFFERENT SYMBOL from test 19's offline check: one reads
// stored bytes and the other requires a checkout, and one symbol cannot hold
// both contracts. Test 19 must never read the full pnpm-lock.yaml; this one
// must.
//
// Without a checkout the test skips and names the gate, rather than passing
// vacuously or pretending to have verified a checkout it never saw. Supplying
// the checkout is gate AG-2 of §20.6b.
func TestPinnedConsumerCheckoutMatchesManifest(t *testing.T) {
	root := os.Getenv(checkoutEnv)
	if root == "" {
		t.Skipf("no workload checkout supplied: set %s to a checkout at %s. "+
			"This is gate AG-2 of §20.6b and cannot be satisfied offline; test 19 covers the stored bytes.",
			checkoutEnv, PinnedMandelCommit)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("%s=%s is not readable: %v", checkoutEnv, root, err)
	}

	digestOf := func(t *testing.T, rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s from the checkout: %v", rel, err)
		}
		sum := sha256.Sum256(b)
		return hex.EncodeToString(sum[:])
	}

	stored := StoredFixtureDigests()

	// The full files, against their full-file digests.
	t.Run("full files match the manifest", func(t *testing.T) {
		for _, pair := range []struct{ checkout, manifest string }{
			{"package.json", "mandel/package.json"},
			{"vitest.config.ts", "mandel/vitest.config.ts"},
			{"scripts/tb-vitest.ts", "mandel/scripts/tb-vitest.ts"},
			{"scripts/run-unit-tests.ts", "mandel/scripts/run-unit-tests.ts"},
			{"scripts/cli-main-module.ts", "mandel/scripts/cli-main-module.ts"},
			{".github/actions/setup-case-test-mongo/action.yml", "mandel/.github/actions/setup-case-test-mongo/action.yml"},
			{".github/workflows/unit-tests-bucketed.yaml", "mandel/.github/workflows/unit-tests-bucketed.yaml"},
		} {
			want, ok := stored[pair.manifest]
			if !ok {
				t.Fatalf("the manifest has no digest for %s", pair.manifest)
			}
			if got := digestOf(t, pair.checkout); got != want {
				t.Errorf("%s: checkout digest %s does not match the manifest's %s", pair.checkout, got, want)
			}
		}
	})

	// The FULL pnpm-lock.yaml — the one file test 19 is forbidden to read.
	t.Run("the full pnpm-lock.yaml matches its recorded digest", func(t *testing.T) {
		const wantLock = "c9381f491ac8a5a3954b70f761834c7a69dbef890bde673d269a732952be611c"
		if got := digestOf(t, "pnpm-lock.yaml"); got != wantLock {
			t.Errorf("full pnpm-lock.yaml digest = %s, want %s", got, wantLock)
		}
	})

	// The tracked path universe, recomputed from the checkout.
	t.Run("the membership re-derives from the checkout", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join("mandel", "tracked-test-paths.txt"))
		if err != nil {
			t.Fatal(err)
		}
		m := DeriveMembership(string(b))
		if m.Tracked != 1512 || m.BucketUniverse != 1396 {
			t.Fatalf("membership = %+v, want 1512 tracked / 1396 selected", m)
		}
		// Every selected path must exist in the checkout, which is the claim
		// the offline test cannot make.
		var missing int
		for _, line := range splitLines(string(b)) {
			if line == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, line)); err != nil {
				missing++
				if missing <= 3 {
					t.Errorf("tracked path %s is absent from the checkout", line)
				}
			}
		}
		if missing > 0 {
			t.Errorf("%d tracked paths are absent from the checkout at %s", missing, PinnedMandelCommit)
		}
	})
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, trimSpace(cur))
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, trimSpace(cur))
	return out
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
