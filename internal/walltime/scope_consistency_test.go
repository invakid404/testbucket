package walltime

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func repoRoot() string { return filepath.Join("..", "..") }

// TestScopeArtifactConsistency is §22 test 41 (S8/F9/S-10).
//
// The artifact inventory is enumerated from `jj file list -r @` and `jj status`
// — NEVER from a number written in a document — and the registries are parsed
// rather than transcribed.
func TestScopeArtifactConsistency(t *testing.T) {
	t.Run("the artifact inventory comes from jj, not from a written number", func(t *testing.T) {
		cmd := exec.Command("jj", "--no-pager", "file", "list", "-r", "@")
		cmd.Dir = repoRoot()
		out, err := cmd.Output()
		if err != nil {
			t.Skipf("jj unavailable: %v", err)
		}
		var pkg []string
		for _, l := range strings.Split(string(out), "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "docs/walltime/") {
				pkg = append(pkg, l)
			}
		}
		if len(pkg) == 0 {
			t.Fatal("jj listed no docs/walltime/ artifacts")
		}
		// Every governed artifact must be present in the listing.
		for _, want := range []string{
			"docs/walltime/acceptance-contract.md",
			"docs/walltime/scope.md",
			"docs/walltime/component-map.json",
			"docs/walltime/source-to-claim-map.md",
		} {
			var found bool
			for _, p := range pkg {
				if p == want {
					found = true
				}
			}
			if !found {
				t.Errorf("%s is absent from the jj listing", want)
			}
		}
	})

	t.Run("no document states a self-referential line count or SHA-256", func(t *testing.T) {
		// S-10: a document that quotes its own length or digest cannot stay
		// true across an edit to itself.
		for _, name := range []string{"acceptance-contract.md", "scope.md", "source-to-claim-map.md"} {
			b, err := os.ReadFile(filepath.Join(repoRoot(), "docs", "walltime", name))
			if err != nil {
				t.Fatal(err)
			}
			src := string(b)
			selfLines := regexp.MustCompile(`(?i)this (document|file|contract|section) (is|has|carries) \d+ lines`)
			if m := selfLines.FindString(src); m != "" {
				t.Errorf("%s states its own line count: %q", name, m)
			}
			// A SHA-256 of a document in this set, quoted inside that set.
			for _, other := range []string{"acceptance-contract.md", "scope.md"} {
				re := regexp.MustCompile(other + "`?[^\\n]{0,80}`[0-9a-f]{64}`")
				if m := re.FindString(src); m != "" {
					t.Errorf("%s quotes a SHA-256 of %s: %q", name, other, m)
				}
			}
		}
	})

	t.Run("the delta registry has no out-of-order ID and every test resolves", func(t *testing.T) {
		block := parseRegistryBlock(t, "# implementation-delta-registry v1")
		ids := regexp.MustCompile(`- id: (ID-\d+)`).FindAllStringSubmatch(block, -1)
		if len(ids) == 0 {
			t.Fatal("parsed no delta rows")
		}
		last := 0
		for _, m := range ids {
			var n int
			if _, err := fmtSscanInt(strings.TrimPrefix(m[1], "ID-"), &n); err != nil {
				t.Fatalf("unparseable delta id %q", m[1])
			}
			if n != last+1 {
				t.Errorf("delta id %s appears out of order (previous was ID-%d)", m[1], last)
			}
			last = n
		}

		// Every acceptance_test symbol must resolve to a real Go test.
		symbols := regexp.MustCompile(`acceptance_tests: \[([^\]]+)\]`).FindAllStringSubmatch(block, -1)
		declared := map[string]bool{}
		for _, m := range symbols {
			for _, s := range strings.Split(m[1], ",") {
				if s = strings.TrimSpace(s); s != "" {
					declared[s] = true
				}
			}
		}
		have := goTestSymbols(t)
		for sym := range declared {
			if !have[sym] {
				t.Errorf("delta registry names acceptance test %q, which does not exist", sym)
			}
		}
	})

	t.Run("no superseded parameter name survives", func(t *testing.T) {
		// §22 test 41's fail list, checked against the SHIPPED source rather
		// than the documents, since the documents legitimately name a retired
		// term in order to retire it.
		banned := map[string]string{
			"per_invocation_ns":   "superseded by PD-3",
			"degenerate_columns":  "superseded by F2",
			"comparability_key\"": "the leaf is comparability_key_digest",
			"plan_id":             "the canonical name is plan_digest",
		}
		files := shippedFiles(t)
		for name, src := range files {
			if !strings.HasSuffix(name, ".go") {
				continue
			}
			for bad, why := range banned {
				for i, line := range strings.Split(src, "\n") {
					if !strings.Contains(line, bad) {
						continue
					}
					// A retired name may appear only in the sentence that
					// retires it. "there is no per_invocation_ns field" is the
					// statement of its absence, not a use of it.
					lower := strings.ToLower(line)
					if strings.Contains(lower, "there is no") || strings.Contains(lower, "no ") &&
						strings.Contains(lower, "field") || strings.Contains(lower, "supersede") ||
						strings.Contains(lower, "removed") || strings.Contains(lower, "never") {
						continue
					}
					t.Errorf("%s:%d carries the superseded name %q (%s): %s",
						name, i+1, bad, why, strings.TrimSpace(line))
				}
			}
		}
	})

	t.Run("every interface field the contract requires is present", func(t *testing.T) {
		union := map[string]bool{}
		for _, n := range WorkflowInputUnion() {
			union[n] = true
		}
		for _, need := range []string{
			"est-basis", "runner-class", "runs-on-label",
			"candidate-sha", "workload-commit", "wall-observations-dir",
		} {
			if !union[need] {
				t.Errorf("the workflow union is missing the required interface field %q", need)
			}
		}
		// cache-declaration-file is an ACTION input, reached by materializing
		// the two caller inputs, so it is checked there rather than in U.
		var found bool
		for _, in := range AddedActionInputs()[ActionPlan] {
			if in == "cache-declaration-file" {
				found = true
			}
		}
		if !found {
			t.Error("the plan action is missing cache-declaration-file")
		}
	})

	t.Run("the withdrawn authority test does not exist", func(t *testing.T) {
		// §22 test 71 withdrew TestSpecificationAuthorityIsSingular. The
		// number is retired rather than reused, and re-introducing the symbol
		// would resurrect the scope-about-scope machinery §0.10 removed.
		if goTestSymbols(t)["TestSpecificationAuthorityIsSingular"] {
			t.Error("TestSpecificationAuthorityIsSingular exists; §22 test 71 withdrew it")
		}
	})

	t.Run("no recommendation contradicts a frozen decision", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join(repoRoot(), "docs", "walltime", "scope.md"))
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		// The excluded machinery of §12 must not be recommended anywhere.
		for _, excluded := range []string{
			"recommend restoring signing", "recommend fleet attestation",
			"recommend a protected environment", "recommend object lock",
		} {
			if strings.Contains(strings.ToLower(src), excluded) {
				t.Errorf("scope.md recommends excluded machinery: %q", excluded)
			}
		}
	})
}

// goTestSymbols returns every Go test function name in the tree.
func goTestSymbols(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, dir := range []string{"internal", "cmd", "testdata"} {
		err := filepath.Walk(filepath.Join(repoRoot(), dir), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, "_test.go") {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			for _, m := range regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)`).FindAllStringSubmatch(string(b), -1) {
				out[m[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}
