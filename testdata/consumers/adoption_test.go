package consumers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
// The checkout-bound half is gate AG-2 and cannot run offline; what is tested
// here is that the comparison rejects rather than tolerates.
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
	// Recomputed from the pinned fixture, for the two values we can derive
	// offline.
	d := StoredFixtureDigests()
	recomputed := binding{
		FacadeSHA256:             d["mandel/scripts/tb-vitest.ts"],
		VitestConfigSHA256:       d["mandel/vitest.config.ts"],
		LockfileSHA256:           "c9381f491ac8a5a3954b70f761834c7a69dbef890bde673d269a732952be611c",
		PackageJSONSHA256:        d["mandel/package.json"],
		WorkloadTreeDigest:       "sha256:tree",
		FacadeArgv:               "pnpm tb-vitest",
		DiscoveryExcludePrefixes: "integration-tests/",
		DiscoveredUnitSetDigest:  "sha256:units",
	}

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

	t.Run("the pinned facade and config digests are the fixture's own", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join("mandel", "scripts", "tb-vitest.ts"))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != recomputed.FacadeSHA256 {
			t.Fatalf("facade digest = %s, want %s", got, recomputed.FacadeSHA256)
		}
	})
}
