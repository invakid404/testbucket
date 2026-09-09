// Package consumers holds the pinned consumer fixture of contract §20.6 and
// the offline acceptance tests over it.
//
// The fixture is DIRECT ENTRYPOINT EVIDENCE, not an execution closure. An
// earlier framing claimed a "full-file execution closure", which was false:
// scripts/run-unit-tests.ts imports offline_replset.ts and temporary_paths.ts
// and executes replset_preflight.ts, and the setup composite executes
// replset_preflight.ts and prepare-case-mongod.ts — none of which is vendored.
// What supplies the complete closure for campaign purposes is the workload
// commit identity (§19.2a), which recomputes every value from a checkout.
package consumers

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// StoredFixtureDigests is the per-file SHA-256 manifest contract §20.6 records
// in SOURCE.md, for the files actually stored in the fixture.
//
// pnpm-lock.excerpt.yaml is keyed by its OWN excerpt digest. No assertion
// claims an excerpt equals a full file: the full pnpm-lock.yaml is 1.28 MB and
// deliberately not vendored, and its full-file digest is recorded in SOURCE.md
// as digest-only evidence.
func StoredFixtureDigests() map[string]string {
	return map[string]string{
		"mandel/package.json":                                     "b832872ff3775378d07fde267426cf1bdf2f989656f2a2c161824316ab4e20a9",
		"mandel/vitest.config.ts":                                 "90266bab9baafc40017fa3b7ee8defe63f5414d56ca86f27b4606ba8b504c563",
		"mandel/scripts/tb-vitest.ts":                             "74ba69cbda080f91dfa7fff9336a4c79256ebe50655dd1b71a86b164bd863134",
		"mandel/scripts/run-unit-tests.ts":                        "ac91910d570ddb8e4aa1ce603c49998653f4b0532f7271ff48d8ed471816e587",
		"mandel/scripts/cli-main-module.ts":                       "627fca02f6ca488678601984c2d09559d8a3efffa996665fb8fd35b13385dde1",
		"mandel/.github/actions/setup-case-test-mongo/action.yml": "57f86ee0d2fe140a35819e6b8ce5d772761729088979d4e9663ae7f144373b8e",
		"mandel/.github/workflows/unit-tests-bucketed.yaml":       "639d525c8227fe0ce99160333fc8aaf5fc9b4c4a3f08cc90a15c244db08db05b",
		"baml-rest/.github/workflows/unit-tests-bucketed.yml":     "c9f930125a243b2ec26563ed5d0b7f66ae3f820863e95760f2665104155d50ef",
		"mandel/pnpm-lock.excerpt.yaml":                           "149a0e3338a3c225c23f6eea38e7eecc872727348294e2533c3aebae713229ea",
		"mandel/tracked-test-paths.txt":                           "1119544350a94c56fd802c834c4dee6ff711c98c622d78288e55a202c76c85e6",
	}
}

// HarnessUnitInclude is the `harness-unit` project's include glob from the
// pinned vitest.config.ts. §20.4 requires a PROJECT-BASED predicate; see the
// membership test for why a path-substring predicate gets the wrong answer.
const HarnessUnitInclude = "integration-tests/lib/purchase-order-recon-extraction/__tests__/"

// Membership is the six-count partition of contract §20.6a.
type Membership struct {
	Tracked        int
	ExcludedCase   int
	ExcludedInteg  int
	ExcludedRegion int
	BucketUniverse int
	HarnessUnit    int
}

// DeriveMembership re-derives the six counts from the tracked path list. It
// reads no checkout: the list is `jj file list` output captured at the pinned
// revision.
func DeriveMembership(trackedList string) Membership {
	var m Membership
	for _, line := range strings.Split(trackedList, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasSuffix(line, ".test.ts") {
			continue
		}
		m.Tracked++
		switch {
		case strings.HasPrefix(line, "shared/f/lib/cases/"):
			m.ExcludedCase++
		case strings.HasPrefix(line, HarnessUnitInclude):
			// Under integration-tests/ but legitimately selected, because the
			// harness-unit PROJECT includes it.
			m.HarnessUnit++
			m.BucketUniverse++
		case strings.HasPrefix(line, "integration-tests/"):
			m.ExcludedInteg++
		case strings.HasPrefix(line, "packages/region-router/"):
			m.ExcludedRegion++
		default:
			m.BucketUniverse++
		}
	}
	return m
}

func fixtureRoot() string { return "." }

// TestPinnedConsumerStoredFixtureIntegrity is §22 test 19 (S-6/R8-D8).
//
// It is OFFLINE: it digests every stored fixture input and re-derives the six
// membership counts. It reads no checkout and never the full pnpm-lock.yaml —
// test 39 is the deliberately separate checkout-bound symbol, because one
// symbol cannot hold both contracts.
func TestPinnedConsumerStoredFixtureIntegrity(t *testing.T) {
	digests := StoredFixtureDigests()

	t.Run("every stored file matches its recorded digest", func(t *testing.T) {
		for rel, want := range digests {
			b, err := os.ReadFile(filepath.Join(fixtureRoot(), rel))
			if err != nil {
				t.Errorf("read %s: %v", rel, err)
				continue
			}
			sum := sha256.Sum256(b)
			if got := hex.EncodeToString(sum[:]); got != want {
				t.Errorf("%s digest = %s, want %s", rel, got, want)
			}
		}
	})

	t.Run("the fixture is the eleven paths §20.6 lists", func(t *testing.T) {
		// Ten stored files plus SOURCE.md itself.
		if _, err := os.Stat(filepath.Join(fixtureRoot(), "SOURCE.md")); err != nil {
			t.Fatalf("SOURCE.md missing: %v", err)
		}
		if len(digests) != 10 {
			t.Fatalf("the digest manifest covers %d stored files; §20.6 lists ten alongside SOURCE.md", len(digests))
		}
	})

	t.Run("SOURCE.md records every stored digest", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join(fixtureRoot(), "SOURCE.md"))
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for rel, want := range digests {
			if !strings.Contains(src, want) {
				t.Errorf("SOURCE.md does not record the digest of %s (%s)", rel, want)
			}
		}
	})

	t.Run("the six membership counts re-derive from the tracked path list", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join(fixtureRoot(), "mandel", "tracked-test-paths.txt"))
		if err != nil {
			t.Fatal(err)
		}
		m := DeriveMembership(string(b))
		want := Membership{
			Tracked:        1512,
			ExcludedCase:   48,
			ExcludedInteg:  67,
			ExcludedRegion: 1,
			BucketUniverse: 1396,
			HarnessUnit:    8,
		}
		if m != want {
			t.Fatalf("membership = %+v, want %+v", m, want)
		}
		// The partition is complete: every tracked path lands in exactly one class.
		if sum := m.BucketUniverse + m.ExcludedCase + m.ExcludedInteg + m.ExcludedRegion; sum != m.Tracked {
			t.Fatalf("the classes sum to %d, not the tracked %d", sum, m.Tracked)
		}
	})

	t.Run("the unit-only predicate is project-based, not path-substring", func(t *testing.T) {
		// §20.4 / §22 test 22. A substring predicate on "harness-unit" matches
		// NONE of the eight files — none of their paths contains that string —
		// so it would exclude all 75 integration-tests/ files and give a
		// bucket universe of 1388 instead of 1396. The include glob is the
		// only predicate that gets it right.
		b, err := os.ReadFile(filepath.Join(fixtureRoot(), "mandel", "tracked-test-paths.txt"))
		if err != nil {
			t.Fatal(err)
		}
		var substringHits, projectHits int
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasSuffix(line, ".test.ts") {
				continue
			}
			if strings.Contains(line, "harness-unit") {
				substringHits++
			}
			if strings.HasPrefix(line, HarnessUnitInclude) {
				projectHits++
			}
		}
		if substringHits != 0 {
			t.Fatalf("a path-substring predicate matched %d files; the fixture no longer demonstrates why it is wrong", substringHits)
		}
		if projectHits != 8 {
			t.Fatalf("the project include glob matched %d files, want 8", projectHits)
		}

		// And it rejects any case-replset file.
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "shared/f/lib/cases/") && strings.HasPrefix(line, HarnessUnitInclude) {
				t.Fatalf("a Case file was accepted by the unit-only predicate: %s", line)
			}
		}
	})

	t.Run("the vitest config declares the harness-unit include glob", func(t *testing.T) {
		// The predicate above is only trustworthy if it is the config's own.
		b, err := os.ReadFile(filepath.Join(fixtureRoot(), "mandel", "vitest.config.ts"))
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if !strings.Contains(src, "name: 'harness-unit'") {
			t.Fatal("the pinned vitest.config.ts no longer declares the harness-unit project")
		}
		if !strings.Contains(src, HarnessUnitInclude) {
			t.Fatalf("the pinned vitest.config.ts no longer carries the include glob %q", HarnessUnitInclude)
		}
	})

	t.Run("no assertion reads the full pnpm-lock.yaml", func(t *testing.T) {
		// The offline test must stop at the excerpt; the full lock is
		// digest-only evidence recorded in SOURCE.md.
		if _, err := os.Stat(filepath.Join(fixtureRoot(), "mandel", "pnpm-lock.yaml")); err == nil {
			t.Fatal("the full pnpm-lock.yaml is vendored; §20.6a keeps it digest-only")
		}
		for rel := range digests {
			if strings.HasSuffix(rel, "pnpm-lock.yaml") {
				t.Fatalf("the manifest keys the full lock file %q; only the excerpt is stored", rel)
			}
		}
	})
}
