package consumers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// PinnedMandelCommit is the frozen workload identity of contract §19.3. The
// campaign does NOT modify Mandel: adopting the experiment inside it would
// create a new Mandel commit and change exactly this value.
const PinnedMandelCommit = "d9ae1d433bb45012c04d567879b66fc4bf6112c6"

// AdoptionIdentities is the identity set §19.3/§20.6b requires a campaign
// manifest to carry. The three provenance identities are SEPARATE and none is
// derivable from another (S-6): orchestration is the workflow that dispatched
// the run, workload is the consumer checkout that executed, and candidate is
// the testbucket build under test.
type AdoptionIdentities struct {
	OrchestrationRepo   string
	OrchestrationCommit string
	WorkloadRepo        string
	WorkloadCommit      string
	CandidateSHA        string
}

// requiredIdentityFields is the manifest's non-empty requirement, enumerated so
// the test walks it rather than transcribing it.
func (a AdoptionIdentities) fields() map[string]string {
	return map[string]string{
		"orchestration_repo":   a.OrchestrationRepo,
		"orchestration_commit": a.OrchestrationCommit,
		"workload_repo":        a.WorkloadRepo,
		"workload_commit":      a.WorkloadCommit,
		"candidate_sha":        a.CandidateSHA,
	}
}

// Validate requires every identity field non-empty. A missing mandatory
// campaign identity fails the campaign and is never skipped (§7 rule 10).
func (a AdoptionIdentities) Validate() error {
	for name, v := range a.fields() {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("campaign manifest: %s is empty; a missing mandatory identity fails the campaign", name)
		}
	}
	// Orchestration identity is not workload identity: collapsing them would
	// make a Mandel modification invisible to the frozen profile.
	if a.OrchestrationRepo == a.WorkloadRepo && a.OrchestrationCommit == a.WorkloadCommit {
		return fmt.Errorf("campaign manifest: orchestration and workload identities are the same; §19.3 keeps them distinct")
	}
	return nil
}

// EqualAcrossArms requires the identity set to be equal in both arms of a pair.
// Each is a BC-INV member: the treatment is the planner mode, so a differing
// identity would confound it.
func EqualAcrossArms(armB, armC AdoptionIdentities) error {
	lb, lc := armB.fields(), armC.fields()
	for name := range lb {
		if lb[name] != lc[name] {
			return fmt.Errorf("campaign manifest: identity %s differs across the arms (%q vs %q)", name, lb[name], lc[name])
		}
	}
	return nil
}

// TestConsumerAdoptionIdentitiesAreSeparateAndRecorded is §22 test 67
// (ID-21/R10-D8).
//
// It does NOT assert that an external orchestrator exists or that a run
// completed: no unit test can, and that evidence is gate AG-2/AG-4 of §20.6b.
func TestConsumerAdoptionIdentitiesAreSeparateAndRecorded(t *testing.T) {
	t.Run("the snapshots exist and every recorded digest matches", func(t *testing.T) {
		for rel, want := range StoredFixtureDigests() {
			b, err := os.ReadFile(rel)
			if err != nil {
				t.Errorf("snapshot %s missing: %v", rel, err)
				continue
			}
			sum := sha256.Sum256(b)
			if got := hex.EncodeToString(sum[:]); got != want {
				t.Errorf("snapshot %s digest = %s, want %s", rel, got, want)
			}
		}
	})

	t.Run("the constants this scope depends on are still present", func(t *testing.T) {
		src, err := os.ReadFile(filepath.Join("SOURCE.md"))
		if err != nil {
			t.Fatal(err)
		}
		s := string(src)
		if !strings.Contains(s, PinnedMandelCommit[:8]) {
			t.Errorf("SOURCE.md no longer records the pinned Mandel commit %s", PinnedMandelCommit)
		}
		// The harness-unit include glob the membership predicate reads.
		cfg, err := os.ReadFile(filepath.Join("mandel", "vitest.config.ts"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(cfg), HarnessUnitInclude) {
			t.Errorf("the pinned config no longer carries the include glob %q", HarnessUnitInclude)
		}
	})

	t.Run("the manifest requires every identity non-empty", func(t *testing.T) {
		full := AdoptionIdentities{
			OrchestrationRepo: "owner/testbucket", OrchestrationCommit: "o1",
			WorkloadRepo: "owner/mandel", WorkloadCommit: PinnedMandelCommit,
			CandidateSHA: "c1",
		}
		if err := full.Validate(); err != nil {
			t.Fatalf("a complete identity set was rejected: %v", err)
		}
		// One case per field, walked from the enumeration.
		for name := range full.fields() {
			bad := full
			switch name {
			case "orchestration_repo":
				bad.OrchestrationRepo = ""
			case "orchestration_commit":
				bad.OrchestrationCommit = ""
			case "workload_repo":
				bad.WorkloadRepo = ""
			case "workload_commit":
				bad.WorkloadCommit = ""
			case "candidate_sha":
				bad.CandidateSHA = ""
			default:
				t.Fatalf("field %q has no emptying case; the identity set grew without the test", name)
			}
			if err := bad.Validate(); err == nil {
				t.Errorf("an empty %s was accepted", name)
			}
		}
	})

	t.Run("the identities are separate and none substitutes for another", func(t *testing.T) {
		collapsed := AdoptionIdentities{
			OrchestrationRepo: "owner/mandel", OrchestrationCommit: PinnedMandelCommit,
			WorkloadRepo: "owner/mandel", WorkloadCommit: PinnedMandelCommit,
			CandidateSHA: "c1",
		}
		if err := collapsed.Validate(); err == nil {
			t.Fatal("a manifest collapsing orchestration into workload identity was accepted")
		}
	})

	t.Run("every identity must be equal across the arms", func(t *testing.T) {
		base := AdoptionIdentities{
			OrchestrationRepo: "owner/testbucket", OrchestrationCommit: "o1",
			WorkloadRepo: "owner/mandel", WorkloadCommit: PinnedMandelCommit,
			CandidateSHA: "c1",
		}
		if err := EqualAcrossArms(base, base); err != nil {
			t.Fatalf("identical arms were rejected: %v", err)
		}
		for name := range base.fields() {
			other := base
			switch name {
			case "orchestration_repo":
				other.OrchestrationRepo = "x"
			case "orchestration_commit":
				other.OrchestrationCommit = "x"
			case "workload_repo":
				other.WorkloadRepo = "x"
			case "workload_commit":
				other.WorkloadCommit = "x"
			case "candidate_sha":
				other.CandidateSHA = "x"
			}
			if err := EqualAcrossArms(base, other); err == nil {
				t.Errorf("a differing %s across the arms was accepted", name)
			}
		}
	})

	t.Run("no VCS mutation targets the workload repository", func(t *testing.T) {
		// §20.6b's boundary: checkout, install and test execution in that
		// working tree are expressly PERMITTED; commit, push, tag and
		// tracked-source mutation are not. The check reads our own
		// orchestration surface, because that is the only place we could
		// write such a step.
		root := filepath.Join("..", "..", ".github")
		var offenders []string
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			text := string(b)
			for _, line := range strings.Split(text, "\n") {
				l := strings.ToLower(line)
				if !strings.Contains(l, "workload") {
					continue
				}
				for _, mutation := range []string{"git push", "git commit", "git tag", "jj commit", "jj push", "jj bookmark"} {
					if strings.Contains(l, mutation) {
						offenders = append(offenders, fmt.Sprintf("%s: %s", p, strings.TrimSpace(line)))
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(offenders) > 0 {
			t.Errorf("VCS mutation targeting the workload repository:\n  %s", strings.Join(offenders, "\n  "))
		}
	})

	t.Run("this test makes no claim that an orchestrator ran", func(t *testing.T) {
		// AG-2 and AG-4 of §20.6b are execution gates. Recording that here
		// keeps the fixture honest about what it does and does not prove.
		if PinnedMandelCommit == "" {
			t.Fatal("the pinned workload commit is the only thing this test binds")
		}
	})
}

// TestWorkloadBindingRecomputesFromPinnedCheckout is §22 test 29. It is the
// VALIDATOR half: a manifest whose recomputed values disagree with the checkout
// is rejected, including the case where both arms agree with each other and
// both disagree with the pinned revision.
//
// The checkout-bound half is gate AG-2 and cannot run offline. What runs here
// is the comparison — and its "recomputed" side is DERIVED, every value of it,
// from the pinned fixture and from the digests SOURCE.md records. It used to be
// eight literals, four of them placeholders (`sha256:tree`, `sha256:units`,
// `pnpm tb-vitest`, `integration-tests/`), compared against themselves. Two of
// those were not merely synthetic but WRONG: the fixture's own workflow says
// the façade is `pnpm exec tsx scripts/tb-vitest.ts` and the discovery
// exclusion is `shared/f/lib/cases/`. A manifest carrying the fixture's real
// values would have been rejected and one carrying the placeholders accepted,
// which is this test's own purpose pointing the wrong way.
func TestWorkloadBindingRecomputesFromPinnedCheckout(t *testing.T) {
	// The recomputable values §19.2a names.
	type binding struct {
		FacadeSHA256             string
		VitestConfigSHA256       string
		LockfileSHA256           string
		PackageJSONSHA256        string
		WorkloadTreeDigest       string
		FacadeArgv               string
		DiscoveryExcludePrefixes string
		DiscoveredUnitSetDigest  string
	}
	fields := func(b binding) map[string]string {
		return map[string]string{
			"facade_sha256":              b.FacadeSHA256,
			"vitest_config_sha256":       b.VitestConfigSHA256,
			"lockfile_sha256":            b.LockfileSHA256,
			"package_json_sha256":        b.PackageJSONSHA256,
			"workload_tree_digest":       b.WorkloadTreeDigest,
			"facade_argv":                b.FacadeArgv,
			"discovery_exclude_prefixes": b.DiscoveryExcludePrefixes,
			"discovered_unit_set_digest": b.DiscoveredUnitSetDigest,
		}
	}

	recomputed := binding{
		// Hashed from the vendored bytes rather than read from the digest
		// table: the table is checked against these same files elsewhere, and
		// a value taken from it would make this test agree with a
		// transcription.
		FacadeSHA256:       fileDigest(t, "mandel", "scripts", "tb-vitest.ts"),
		VitestConfigSHA256: fileDigest(t, "mandel", "vitest.config.ts"),
		PackageJSONSHA256:  fileDigest(t, "mandel", "package.json"),
		// The full pnpm-lock.yaml is NOT vendored — SOURCE.md is explicit that
		// its digest is a CHECKOUT assertion rather than an offline result. So
		// it is read from the record that makes the assertion, and a drift in
		// that record fails here.
		LockfileSHA256: sourceRecordedDigest(t, "mandel/pnpm-lock.yaml"),
		// The workload tree digest is likewise a checkout value. What is
		// derivable offline is the digest over the pinned fixture's OWN file
		// set, which is what this repository knows of that tree: change any
		// vendored byte and this moves.
		WorkloadTreeDigest: pinnedFixtureTreeDigest(t),
		// Read from the fixture's own workflow, which SOURCE.md names as where
		// these constants are read from rather than asserted.
		FacadeArgv:               fixtureEnvValue(t, "TESTBUCKET_VITEST_COMMAND"),
		DiscoveryExcludePrefixes: fixtureEnvValue(t, "TB_DISCOVERY_EXCLUDE_PREFIXES"),
		// Derived from tracked-test-paths.txt through §20.6a's membership
		// partition — the same input the fixture's other regressions use.
		DiscoveredUnitSetDigest: discoveredUnitSetDigest(t),
	}

	// The derivations must produce the fixture's real values, not merely
	// something. Stated explicitly, so a broken derivation cannot quietly make
	// every comparison below trivial.
	t.Run("every recomputed value is the fixture's own", func(t *testing.T) {
		stored := StoredFixtureDigests()
		for path, got := range map[string]string{
			"mandel/scripts/tb-vitest.ts": recomputed.FacadeSHA256,
			"mandel/vitest.config.ts":     recomputed.VitestConfigSHA256,
			"mandel/package.json":         recomputed.PackageJSONSHA256,
		} {
			if stored[path] != got {
				t.Errorf("%s hashes to %s, but the pinned digest is %s", path, got, stored[path])
			}
		}
		if want := "c9381f491ac8a5a3954b70f761834c7a69dbef890bde673d269a732952be611c"; recomputed.LockfileSHA256 != want {
			t.Errorf("the lockfile digest read from SOURCE.md is %q, want the recorded %q", recomputed.LockfileSHA256, want)
		}
		if want := "pnpm exec tsx scripts/tb-vitest.ts"; recomputed.FacadeArgv != want {
			t.Errorf("facade argv read from the fixture = %q, want %q", recomputed.FacadeArgv, want)
		}
		if want := "shared/f/lib/cases/"; recomputed.DiscoveryExcludePrefixes != want {
			t.Errorf("discovery exclude prefixes read from the fixture = %q, want %q", recomputed.DiscoveryExcludePrefixes, want)
		}
		for name, v := range map[string]string{
			"workload_tree_digest":       recomputed.WorkloadTreeDigest,
			"discovered_unit_set_digest": recomputed.DiscoveredUnitSetDigest,
		} {
			if !strings.HasPrefix(v, "sha256:") || len(v) != len("sha256:")+64 {
				t.Errorf("%s = %q, which is not a derived sha256 digest", name, v)
			}
		}
	})

	validate := func(declared, recomputed binding) error {
		dl, rl := fields(declared), fields(recomputed)
		for name := range rl {
			if dl[name] != rl[name] {
				return fmt.Errorf("workload binding: %s disagrees with the value recomputed from the checkout at workload_commit (%q vs %q)",
					name, dl[name], rl[name])
			}
		}
		return nil
	}

	t.Run("a manifest agreeing with the recomputation is accepted", func(t *testing.T) {
		if err := validate(recomputed, recomputed); err != nil {
			t.Fatalf("an agreeing manifest was rejected: %v", err)
		}
	})

	t.Run("a disagreement at any recomputable value is rejected", func(t *testing.T) {
		for name := range fields(recomputed) {
			bad := recomputed
			switch name {
			case "facade_sha256":
				bad.FacadeSHA256 = "x"
			case "vitest_config_sha256":
				bad.VitestConfigSHA256 = "x"
			case "lockfile_sha256":
				bad.LockfileSHA256 = "x"
			case "package_json_sha256":
				bad.PackageJSONSHA256 = "x"
			case "workload_tree_digest":
				bad.WorkloadTreeDigest = "x"
			case "facade_argv":
				bad.FacadeArgv = "x"
			case "discovery_exclude_prefixes":
				bad.DiscoveryExcludePrefixes = "x"
			case "discovered_unit_set_digest":
				bad.DiscoveredUnitSetDigest = "x"
			default:
				t.Fatalf("recomputable value %q has no mutation case", name)
			}
			if err := validate(bad, recomputed); err == nil {
				t.Errorf("a manifest disagreeing at %s was accepted", name)
			}
		}
	})

	t.Run("the placeholders this test used to recompute are now rejected", func(t *testing.T) {
		// The exact four values the earlier revision compared against itself.
		// Each must now be refused, because each disagrees with the fixture.
		for name, placeholder := range map[string]string{
			"workload_tree_digest":       "sha256:tree",
			"discovered_unit_set_digest": "sha256:units",
			"facade_argv":                "pnpm tb-vitest",
			"discovery_exclude_prefixes": "integration-tests/",
		} {
			manifest := recomputed
			switch name {
			case "workload_tree_digest":
				manifest.WorkloadTreeDigest = placeholder
			case "discovered_unit_set_digest":
				manifest.DiscoveredUnitSetDigest = placeholder
			case "facade_argv":
				manifest.FacadeArgv = placeholder
			case "discovery_exclude_prefixes":
				manifest.DiscoveryExcludePrefixes = placeholder
			}
			if err := validate(manifest, recomputed); err == nil {
				t.Errorf("the placeholder %s = %q was accepted against the pinned fixture", name, placeholder)
			}
		}
	})

	t.Run("both arms agreeing with each other and both wrong is still rejected", func(t *testing.T) {
		// The case §22 test 29 calls out: cross-arm equality does not
		// establish agreement with the pinned revision.
		wrong := recomputed
		wrong.FacadeSHA256 = "agreed-but-wrong"
		if err := EqualAcrossArms(AdoptionIdentities{}, AdoptionIdentities{}); err != nil {
			t.Fatal(err)
		}
		if err := validate(wrong, recomputed); err == nil {
			t.Fatal("two arms agreeing with each other and disagreeing with the pinned revision were accepted")
		}
	})
}

// fileDigest hashes one vendored fixture file.
func fileDigest(t *testing.T, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatalf("read fixture %v: %v", parts, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// sourceRecordedDigest reads a digest SOURCE.md records for a path it does not
// vendor. Those rows are checkout assertions, so the record IS the value
// offline, and a drift in the record fails the comparison rather than passing
// silently.
func sourceRecordedDigest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile("SOURCE.md")
	if err != nil {
		t.Fatalf("read SOURCE.md: %v", err)
	}
	re := regexp.MustCompile("(?m)^\\| `" + regexp.QuoteMeta(path) + "` \\| `([0-9a-f]{64})`")
	m := re.FindStringSubmatch(string(b))
	if m == nil {
		t.Fatalf("SOURCE.md records no full-file digest for %s", path)
	}
	return m[1]
}

// fixtureEnvValue reads one workflow-level env constant out of the pinned
// consumer's own workflow.
func fixtureEnvValue(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("mandel", ".github", "workflows", "unit-tests-bucketed.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read the pinned mandel workflow: %v", err)
	}
	re := regexp.MustCompile("(?m)^  " + regexp.QuoteMeta(name) + ": *(.*?) *(?:#.*)?$")
	m := re.FindStringSubmatch(string(b))
	if m == nil {
		t.Fatalf("the pinned workflow declares no %s", name)
	}
	return strings.Trim(strings.TrimSpace(m[1]), "'\"")
}

// pinnedFixtureTreeDigest is the canonical digest over the pinned fixture's own
// file set: every stored path with its full digest, in sorted order.
//
// The real workload_tree_digest is a checkout value AG-2 recomputes from the
// tree at workload_commit, and nothing offline can produce it. This produces
// the same KIND of value over the bytes this repository actually pins, so a
// vendored file that changes moves it — which is what makes it a derivation
// rather than a placeholder.
func pinnedFixtureTreeDigest(t *testing.T) string {
	t.Helper()
	stored := StoredFixtureDigests()
	paths := make([]string, 0, len(stored))
	for p := range stored {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	rows := make([][2]string, 0, len(paths))
	for _, p := range paths {
		rows = append(rows, [2]string{p, stored[p]})
	}
	d, err := walltime.DigestJSON(rows)
	if err != nil {
		t.Fatalf("digest the pinned fixture tree: %v", err)
	}
	return string(d)
}

// discoveredUnitSetDigest is the canonical digest over the discovered path
// set: the selected bucket universe at the pinned revision, derived from
// tracked-test-paths.txt through §20.6a's membership partition.
func discoveredUnitSetDigest(t *testing.T) string {
	t.Helper()
	selected := selectedFixturePaths(t)
	if len(selected) != 1396 {
		t.Fatalf("the discovered unit set has %d paths, want the pinned 1396", len(selected))
	}
	// The partition below is the one DeriveMembership COUNTS with. Keeping one
	// predicate in two shapes is exactly how a count regression and this
	// digest come to disagree, so the two are required to agree here.
	tracked, err := os.ReadFile(filepath.Join("mandel", "tracked-test-paths.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := DeriveMembership(string(tracked)).BucketUniverse; len(selected) != want {
		t.Fatalf("the selected path list has %d entries but the membership partition counts %d", len(selected), want)
	}
	d, err := walltime.DigestJSON(selected)
	if err != nil {
		t.Fatalf("digest the discovered unit set: %v", err)
	}
	return string(d)
}

// selectedFixturePaths is §20.6a's membership partition, returning the selected
// paths themselves rather than only their count. Its caller requires it to
// agree with DeriveMembership, which counts under the same rules.
func selectedFixturePaths(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("mandel", "tracked-test-paths.txt"))
	if err != nil {
		t.Fatalf("read the pinned path list: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasSuffix(line, ".test.ts") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "shared/f/lib/cases/"):
		case strings.HasPrefix(line, HarnessUnitInclude):
			out = append(out, line)
		case strings.HasPrefix(line, "integration-tests/"):
		case strings.HasPrefix(line, "packages/region-router/"):
		default:
			out = append(out, line)
		}
	}
	return out
}
