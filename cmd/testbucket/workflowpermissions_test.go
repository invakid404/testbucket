package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// workflowJobs returns each top-level job in a workflow, keyed by job id, with
// its declared `permissions:` block. A job with no block maps to nil, which is
// the case the test is here to refuse: nil is not "no permissions", it is
// "whatever the caller's repository default happens to be".
//
// Hand-rolled, like the other workflow contracts in this package: the module
// has no dependencies, and a scan of the exact property under test is better
// than a YAML parser pulled in to assert one thing.
func workflowJobs(t *testing.T, path string) map[string]map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	start := -1
	for i, l := range lines {
		if l == "jobs:" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s declares no jobs", path)
	}
	jobLine := regexp.MustCompile(`^  ([a-zA-Z0-9_-]+):\s*$`)
	scopeLine := regexp.MustCompile(`^      ([a-z-]+): ([a-z-]+)\s*$`)
	out := map[string]map[string]string{}
	job := ""
	inPerms := false
	for _, l := range lines[start:] {
		if m := jobLine.FindStringSubmatch(l); m != nil {
			job, inPerms = m[1], false
			if _, dup := out[job]; dup {
				t.Fatalf("%s declares job %q twice", path, job)
			}
			out[job] = nil
			continue
		}
		if job == "" {
			continue
		}
		if strings.TrimSpace(l) == "permissions:" && strings.HasPrefix(l, "    permissions:") {
			inPerms = true
			out[job] = map[string]string{}
			continue
		}
		if !inPerms {
			continue
		}
		if m := scopeLine.FindStringSubmatch(l); m != nil {
			out[job][m[1]] = m[2]
			continue
		}
		if strings.TrimSpace(l) != "" {
			inPerms = false
		}
	}
	return out
}

func showScopes(p map[string]string) string {
	if p == nil {
		return "<no permissions block: inherits the caller's default>"
	}
	var keys []string
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+": "+p[k])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// THE CALLED JOBS BOUND THEIR OWN TOKEN.
//
// They used to declare nothing, and a comment told the caller what to grant.
// That bounds nothing: with no declaration each job inherits whatever the
// caller's repository default is — on many repositories a write-scoped token —
// and the jobs move artifacts and read the Actions API with it. Documentation
// is not a permissions boundary.
//
// The declarations are asserted EXACTLY, not as a lower bound. A scope added
// here is a scope every consumer's token must carry, so widening one is a
// decision, not a detail.
func TestTheReusableWorkflowJobsDeclareLeastPrivilege(t *testing.T) {
	path := filepath.Join("..", "..", ".github", "workflows", "bucketed-reusable.yml")
	want := map[string]map[string]string{
		// Checkout, the timing-store cache and same-run artifacts.
		"plan": {"contents": "read"},
		// The one job that reads the Actions API: the A_GH collector reads
		// this job's own step timestamps back, and without it no invocation
		// can produce an eligible row.
		"test": {"contents": "read", "actions": "read"},
		// Checkout, same-run artifacts, and the store written back through the
		// cache rather than the repository.
		"record": {"contents": "read"},
	}
	got := workflowJobs(t, path)
	if len(got) != len(want) {
		t.Fatalf("the reusable workflow has %d job(s) and this test knows %d; a new job must state the token it needs", len(got), len(want))
	}
	for job, expect := range want {
		have, known := got[job]
		if !known {
			t.Errorf("the reusable workflow no longer has a %q job", job)
			continue
		}
		if have == nil {
			t.Errorf("job %q declares no permissions, so it runs on whatever the caller's repository default grants", job)
			continue
		}
		if showScopes(have) != showScopes(expect) {
			t.Errorf("job %q declares %s, want %s", job, showScopes(have), showScopes(expect))
		}
	}
}

// AND THE CALLERS IN THIS REPOSITORY GRANT WHAT THOSE JOBS DECLARE.
//
// A called workflow may only retain or reduce what its caller granted, so a
// job declaring `actions: read` fails at startup for a caller that grants
// less. That regression shipped once already. The declarations and the grants
// are two halves of one versioned migration, and this is what stops them
// drifting apart again.
func TestEveryCallerGrantsWhatTheCalledJobsDeclare(t *testing.T) {
	reusable := filepath.Join("..", "..", ".github", "workflows", "bucketed-reusable.yml")
	need := map[string]string{}
	for _, scopes := range workflowJobs(t, reusable) {
		for k, v := range scopes {
			need[k] = v
		}
	}
	if len(need) == 0 {
		t.Fatal("the reusable workflow declares no permissions at all; this test is looking at the wrong contract")
	}

	dir := filepath.Join("..", "..", ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	callers := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		body := string(b)
		if !strings.Contains(body, "bucketed-reusable.yml") || e.Name() == "bucketed-reusable.yml" {
			continue
		}
		for job, scopes := range workflowJobs(t, filepath.Join(dir, e.Name())) {
			// Only the jobs that actually call it.
			if !callsReusable(t, filepath.Join(dir, e.Name()), job) {
				continue
			}
			callers++
			if scopes == nil {
				t.Errorf("%s job %q calls the reusable workflow with no permissions block", e.Name(), job)
				continue
			}
			for scope, level := range need {
				if scopes[scope] != level {
					t.Errorf("%s job %q grants %s, but the called jobs declare %s: %s — the run fails at startup, because a called workflow may not elevate",
						e.Name(), job, showScopes(scopes), scope, level)
				}
			}
		}
	}
	if callers == 0 {
		t.Fatal("no workflow in this repository calls the reusable workflow; the migration half of this contract is unverified")
	}
}

// callsReusable reports whether one job's body has `uses:` pointing at the
// reusable workflow.
func callsReusable(t *testing.T, path, job string) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	jobLine := regexp.MustCompile(`^  ([a-zA-Z0-9_-]+):\s*$`)
	cur := ""
	for _, l := range lines {
		if m := jobLine.FindStringSubmatch(l); m != nil {
			cur = m[1]
			continue
		}
		if cur == job && strings.HasPrefix(strings.TrimSpace(l), "uses:") &&
			strings.Contains(l, "bucketed-reusable.yml") {
			return true
		}
	}
	return false
}
