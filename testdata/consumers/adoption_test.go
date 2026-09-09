package consumers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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

// TestWorkloadBindingRecomputesFromPinnedCheckout is §22 test 29.
//
// §19.2a is explicit that every workload-derived value is recomputed FROM THE
// CHECKOUT at `workload_commit` and never accepted as a supplied value: two
// arms agreeing proves they ran the same partition, not that it was the
// intended one. So this reads a real checkout.
//
// It has been wrong twice. First it compared eight literals against
// themselves, two of which disagreed with the fixture. Then it recomputed from
// the VENDORED fixture and from digests SOURCE.md records — closer, but
// `pinnedFixtureTreeDigest` hashed ten stored rows rather than the pinned
// workload tree, the discovered set was read from a stored path list rather
// than derived, and the test never looked at `TB_MANDEL_CHECKOUT` at all. It
// passed when that variable named a directory that did not exist.
//
// Skipping and passing are different outcomes. With no checkout named there is
// nothing to recompute from and the test SKIPS. With one named, every value
// below is computed from it, and a value that cannot be computed is a FAILURE
// rather than a reason to report success.
func TestWorkloadBindingRecomputesFromPinnedCheckout(t *testing.T) {
	root := pinnedCheckoutRoot(t)

	// --- §19.2a step 2: the four named files, hashed from the checkout -----
	facade := checkoutFileDigest(t, root, filepath.Join("scripts", "tb-vitest.ts"))
	config := checkoutFileDigest(t, root, "vitest.config.ts")
	lockfile := checkoutFileDigest(t, root, "pnpm-lock.yaml")
	packageJSON := checkoutFileDigest(t, root, "package.json")

	// --- §19.2a step 2: the tree digest -----------------------------------
	tracked := trackedTestPaths(t, root)
	tree := workloadTreeDigest(t, root, tracked)

	// --- §19.2a step 4: argv and exclusions from the checkout's OWN workflow
	facadeArgv := checkoutWorkflowEnv(t, root, "TESTBUCKET_VITEST_COMMAND")
	excludePrefixes := checkoutWorkflowEnv(t, root, "TB_DISCOVERY_EXCLUDE_PREFIXES")

	// --- §19.2a step 5: the discovered path set ----------------------------
	discovered := discoveredPathSet(t, root, tracked)
	discoveredDigest := digestOf(t, discovered)

	recomputed := workloadBinding{
		FacadeSHA256:             facade,
		VitestConfigSHA256:       config,
		LockfileSHA256:           lockfile,
		PackageJSONSHA256:        packageJSON,
		WorkloadTreeDigest:       tree,
		FacadeArgv:               facadeArgv,
		DiscoveryExcludePrefixes: excludePrefixes,
		DiscoveredUnitSetDigest:  discoveredDigest,
	}

	t.Logf("recomputed from %s at %s:\n  tracked *.test.ts %d, discovered %d\n  facade_argv %q\n  exclude_prefixes %q\n  tree %s\n  discovered_unit_set %s",
		root, PinnedMandelCommit, len(tracked), len(discovered),
		facadeArgv, excludePrefixes, tree, discoveredDigest)

	// The recomputation is bound to the PINNED revision, not merely to some
	// checkout: these counts and digests are §20.6a's and SOURCE.md's for
	// `d9ae1d43`. A checkout at another revision fails here rather than
	// producing a self-consistent binding for the wrong tree.
	t.Run("the checkout is the pinned revision", func(t *testing.T) {
		if len(tracked) != 1512 {
			t.Errorf("the checkout has %d tracked *.test.ts files; the pinned revision has 1512", len(tracked))
		}
		if len(discovered) != 1396 {
			t.Errorf("the checkout discovers %d unit-scope paths; the pinned revision has 1396", len(discovered))
		}
		stored := StoredFixtureDigests()
		for name, got := range map[string]string{
			"mandel/scripts/tb-vitest.ts": facade,
			"mandel/vitest.config.ts":     config,
			"mandel/package.json":         packageJSON,
		} {
			if stored[name] != got {
				t.Errorf("%s in the checkout hashes to %s, but the pinned digest is %s", name, got, stored[name])
			}
		}
		// The full lockfile is NOT vendored, so SOURCE.md's row for it is a
		// checkout assertion. This is the check that makes it one.
		if want := sourceRecordedDigest(t, "mandel/pnpm-lock.yaml"); lockfile != want {
			t.Errorf("pnpm-lock.yaml in the checkout hashes to %s, but SOURCE.md records %s", lockfile, want)
		}
	})

	// --- §19.2a step 3: the comparison -------------------------------------
	validate := func(declared, recomputed workloadBinding) error {
		dl, rl := declared.fields(), recomputed.fields()
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
		for name := range recomputed.fields() {
			bad := recomputed
			if !bad.set(name, "x") {
				t.Fatalf("recomputable value %q has no mutation case", name)
			}
			if err := validate(bad, recomputed); err == nil {
				t.Errorf("a manifest disagreeing at %s was accepted", name)
			}
		}
	})

	t.Run("the placeholders earlier revisions recomputed are rejected", func(t *testing.T) {
		// The four values the first revision of this test compared against
		// itself. Each must be refused against the checkout.
		for name, placeholder := range map[string]string{
			"workload_tree_digest":       "sha256:tree",
			"discovered_unit_set_digest": "sha256:units",
			"facade_argv":                "pnpm tb-vitest",
			"discovery_exclude_prefixes": "integration-tests/",
		} {
			bad := recomputed
			if !bad.set(name, placeholder) {
				t.Fatalf("no mutation case for %q", name)
			}
			if err := validate(bad, recomputed); err == nil {
				t.Errorf("the placeholder %s = %q was accepted against the checkout", name, placeholder)
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

// workloadBinding is the recomputable set §19.2a names.
type workloadBinding struct {
	FacadeSHA256             string
	VitestConfigSHA256       string
	LockfileSHA256           string
	PackageJSONSHA256        string
	WorkloadTreeDigest       string
	FacadeArgv               string
	DiscoveryExcludePrefixes string
	DiscoveredUnitSetDigest  string
}

func (b workloadBinding) fields() map[string]string {
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

// set assigns one field by its manifest name, reporting whether the name is
// one this struct models. A switch that silently ignored an unknown name would
// make the mutation loop above pass by mutating nothing.
func (b *workloadBinding) set(name, v string) bool {
	switch name {
	case "facade_sha256":
		b.FacadeSHA256 = v
	case "vitest_config_sha256":
		b.VitestConfigSHA256 = v
	case "lockfile_sha256":
		b.LockfileSHA256 = v
	case "package_json_sha256":
		b.PackageJSONSHA256 = v
	case "workload_tree_digest":
		b.WorkloadTreeDigest = v
	case "facade_argv":
		b.FacadeArgv = v
	case "discovery_exclude_prefixes":
		b.DiscoveryExcludePrefixes = v
	case "discovered_unit_set_digest":
		b.DiscoveredUnitSetDigest = v
	default:
		return false
	}
	return true
}

// pinnedCheckoutRoot resolves the checkout to recompute from.
//
// UNSET is a skip: there is nothing to recompute from, and saying so is honest.
// SET BUT ABSENT is a FAILURE, not a skip — setting the variable is a request
// for the checkout-bound check, and pointing it at nothing is a
// misconfiguration that must not be reported as success. That exact case is
// how an earlier revision of this test passed while reading no checkout.
func pinnedCheckoutRoot(t *testing.T) string {
	t.Helper()
	root := strings.TrimSpace(os.Getenv("TB_MANDEL_CHECKOUT"))
	if root == "" {
		t.Skip("TB_MANDEL_CHECKOUT is unset: §19.2a recomputes from a checkout at workload_commit, and there is none to read")
	}
	st, err := os.Stat(root)
	if err != nil {
		t.Fatalf("TB_MANDEL_CHECKOUT=%q cannot be read: %v; a named checkout that is not there is a misconfiguration, not an absence of one", root, err)
	}
	if !st.IsDir() {
		t.Fatalf("TB_MANDEL_CHECKOUT=%q is not a directory", root)
	}
	return root
}

// checkoutFileDigest hashes one file inside the checkout, failing rather than
// defaulting when it is absent.
func checkoutFileDigest(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("§19.2a recomputes %s from the checkout, and it cannot be read: %v", rel, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// trackedTestPaths enumerates the checkout's tracked `*.test.ts` set.
//
// TRACKED, not "present": a stray file in a working copy is not part of the
// pinned revision, and a tree digest over whatever happens to be on disk would
// bind to the developer's directory rather than to version control. The
// enumeration therefore comes from the VCS at the pinned commit, and a
// checkout that cannot answer that question fails.
func trackedTestPaths(t *testing.T, root string) []string {
	t.Helper()
	out, err := runIn(root, "jj", "file", "list", "-r", PinnedMandelCommit)
	if err != nil {
		// A checkout made with `actions/checkout` is a git tree, which is the
		// primary route §19.3 describes.
		out, err = runIn(root, "git", "ls-tree", "-r", "--name-only", PinnedMandelCommit)
		if err != nil {
			t.Fatalf("neither jj nor git could list the tracked files of %s at %s; §19.2a needs the tracked set, not the working copy: %v",
				root, PinnedMandelCommit, err)
		}
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ".test.ts") {
			paths = append(paths, line)
		}
	}
	if len(paths) == 0 {
		t.Fatalf("the checkout at %s lists no tracked *.test.ts files at %s", root, PinnedMandelCommit)
	}
	sort.Strings(paths)
	return paths
}

// workloadTreeDigest is §19.2a's tree digest: the sorted list of relative
// path, file mode and file SHA-256 over the tracked `*.test.ts` set plus the
// four named files.
func workloadTreeDigest(t *testing.T, root string, tracked []string) string {
	t.Helper()
	rows := make([][3]string, 0, len(tracked)+4)
	add := func(rel string) {
		full := filepath.Join(root, rel)
		info, err := os.Lstat(full)
		if err != nil {
			t.Fatalf("the tree digest covers %s, which cannot be stat'ed: %v", rel, err)
		}
		b, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("the tree digest covers %s, which cannot be read: %v", rel, err)
		}
		sum := sha256.Sum256(b)
		rows = append(rows, [3]string{rel, fmt.Sprintf("%04o", info.Mode().Perm()), hex.EncodeToString(sum[:])})
	}
	for _, rel := range tracked {
		add(rel)
	}
	for _, rel := range []string{
		filepath.Join("scripts", "tb-vitest.ts"), "vitest.config.ts", "pnpm-lock.yaml", "package.json",
	} {
		add(rel)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return digestOf(t, rows)
}

// checkoutWorkflowEnv reads one workflow-level env constant from the
// checkout's OWN workflow file, which is where §19.2a step 4 says facade_argv
// and the exclusion prefixes come from.
func checkoutWorkflowEnv(t *testing.T, root, name string) string {
	t.Helper()
	rel := filepath.Join(".github", "workflows", "unit-tests-bucketed.yaml")
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("§19.2a derives %s from %s in the checkout, which cannot be read: %v", name, rel, err)
	}
	re := regexp.MustCompile("(?m)^  " + regexp.QuoteMeta(name) + ": *(.*?) *(?:#.*)?$")
	m := re.FindStringSubmatch(string(b))
	if m == nil {
		t.Fatalf("the checkout's own workflow declares no %s", name)
	}
	v := strings.Trim(strings.TrimSpace(m[1]), "'\"")
	if v == "" {
		t.Fatalf("the checkout's own workflow declares %s as empty", name)
	}
	return v
}

// discoveredPathSet derives the unit-scope discovered set from the checkout.
//
// §19.2a step 5 compares the discovered set's digest AND its file count
// against the §20.4 static audit, so the audit is the derivation and the
// façade run is its confirmation. The rules come from the checkout's own
// `vitest.config.ts`, read as globs rather than restated here — §20.4 is
// explicit that membership is PROJECT-based, and a path-substring predicate
// gets the wrong answer by excluding the eight pure harness-unit files that
// legitimately live under `integration-tests/`.
//
// When the checkout carries an installed toolchain, the pinned façade's own
// discovery is run and required to agree. When it does not, running it would
// mean acquiring dependencies, which is outside what this suite may do; the
// static audit stands alone and says so.
func discoveredPathSet(t *testing.T, root string, tracked []string) []string {
	t.Helper()
	harnessInclude, excluded := checkoutProjectRules(t, root)

	var selected []string
	for _, p := range tracked {
		if strings.HasPrefix(p, harnessInclude) {
			selected = append(selected, p)
			continue
		}
		skip := false
		for _, ex := range excluded {
			if strings.HasPrefix(p, ex) {
				skip = true
				break
			}
		}
		if !skip {
			selected = append(selected, p)
		}
	}
	sort.Strings(selected)

	if fromFacade, ok := facadeDiscovery(t, root); ok {
		if !slices.Equal(fromFacade, selected) {
			t.Fatalf("the pinned façade discovered %d paths and the §20.4 static audit derives %d; they must agree",
				len(fromFacade), len(selected))
		}
	}
	return selected
}

// checkoutProjectRules reads the harness-unit include and the excluded
// prefixes out of the checkout's own vitest.config.ts.
func checkoutProjectRules(t *testing.T, root string) (harnessInclude string, excluded []string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "vitest.config.ts"))
	if err != nil {
		t.Fatalf("§20.4's partition is PROJECT-based and its rules live in vitest.config.ts, which cannot be read: %v", err)
	}
	src := string(b)
	// The harness-unit project's include glob, taken as its literal directory
	// prefix.
	m := regexp.MustCompile(`(integration-tests/[A-Za-z0-9_./-]*?/__tests__/)`).FindStringSubmatch(src)
	if m == nil {
		t.Fatal("the checkout's vitest.config.ts declares no harness-unit include under integration-tests/")
	}
	harnessInclude = m[1]
	for _, want := range []string{"integration-tests/", "packages/region-router/", "shared/f/lib/cases/"} {
		if !strings.Contains(src, want) {
			t.Fatalf("the checkout's vitest.config.ts does not mention the %q partition rule", want)
		}
		excluded = append(excluded, want)
	}
	return harnessInclude, excluded
}

// facadeDiscovery runs the pinned façade's own discovery inside the checkout,
// returning its file set. The second result is false when the checkout has no
// installed toolchain to run it with.
//
// THE MANDEL SAFETY GUARD RUNS FIRST. Nothing here may execute a Mandel suite,
// reach real infrastructure or touch a Mongo URL, so the invocation is
// discovery in `--filesOnly` form — which resolves files by glob WITHOUT
// importing the module graph — and the resulting set is checked for forbidden
// selections BEFORE it is used. Zero FORBIDDEN integration selections is the
// governed condition: the eight pure `harness-unit` files under
// `integration-tests/` are intentionally selected, and §20.4 says excluding
// that whole tree is wrong.
func facadeDiscovery(t *testing.T, root string) ([]string, bool) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, "node_modules", ".bin")); err != nil {
		t.Logf("the checkout at %s has no installed toolchain, so the pinned façade's discovery was not run: acquiring dependencies is outside this suite's envelope. The §20.4 static audit stands on its own.", root)
		return nil, false
	}
	argv := strings.Fields(checkoutWorkflowEnv(t, root, "TESTBUCKET_VITEST_COMMAND"))
	if len(argv) == 0 {
		t.Fatal("the checkout's façade argv is empty")
	}
	args := append(append([]string(nil), argv[1:]...), "list", "--filesOnly", "--json")
	out, err := runIn(root, argv[0], args...)
	if err != nil {
		t.Fatalf("the pinned façade's discovery failed inside the checkout: %v", err)
	}
	var rows []struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("the façade's discovery output does not parse: %v", err)
	}
	var files []string
	for _, r := range rows {
		files = append(files, strings.TrimPrefix(strings.TrimPrefix(r.File, root), "/"))
	}
	sort.Strings(files)

	// The guard, on the set the façade actually selected.
	harnessInclude, _ := checkoutProjectRules(t, root)
	forbidden, cases := 0, 0
	for _, f := range files {
		if strings.HasPrefix(f, "integration-tests/") && !strings.HasPrefix(f, harnessInclude) {
			forbidden++
		}
		if strings.HasPrefix(f, "shared/f/lib/cases/") {
			cases++
		}
	}
	if forbidden != 0 || cases != 0 {
		t.Fatalf("the façade's discovery selected %d forbidden integration path(s) and %d Case path(s); this suite may not proceed with either",
			forbidden, cases)
	}
	return files, true
}

// runIn runs a command inside the checkout, read-only. The checkout is never
// modified: §19.3 is explicit that the campaign does not modify Mandel, and a
// test that wrote to it would change the very identity it is binding to.
func runIn(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// digestOf is the canonical digest the campaign compares, taken through the
// production digester so a manifest and a recomputation cannot differ by
// serialisation.
func digestOf(t *testing.T, v any) string {
	t.Helper()
	d, err := walltime.DigestJSON(v)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	return string(d)
}

// sourceRecordedDigest reads a digest SOURCE.md records for a path it does not
// vendor. That row is a CHECKOUT assertion, and the checkout is what this test
// compares it against.
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
