package walltime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Helpers PORTED from the proof-test files the removal plan deletes, kept
// because the PRACTICAL regressions in exec_test.go and beginrollback_test.go
// still need them. §0.5a step 6 orders it this way — port the retained tests,
// then delete the proof tests — precisely so a kept regression does not
// disappear with the file its helper happened to live in.

// shortTempDir returns a temp dir with a SHORT path.
//
// It exists because a unix domain socket path has a hard length limit, and the
// default per-test temp directory on some platforms is long enough to exceed it
// on its own. Using it keeps a test from failing for a reason it is not about.
// Ported from the deleted controller_test.go.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// productionFunc returns one function's source text, so a test can assert an
// ORDERING the type system cannot express — which is what the begin-rollback
// regression needs: that cleanup of resources started before a failed begin
// happens in the right place. Ported from the deleted producercontract_test.go.
func productionFunc(t *testing.T, file, decl string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, decl)
	if start < 0 {
		t.Fatalf("%s no longer defines %q", file, decl)
	}
	end := strings.Index(s[start+1:], "\nfunc ")
	if end < 0 {
		t.Fatalf("could not delimit %q in %s", decl, file)
	}
	return s[start : start+1+end]
}

// findingsMentioning collects every finding whose code and detail match, so a
// case can assert on the REASON rather than on a count. Ported from the
// deleted rawevidence_test.go, because the retained lifecycle regressions
// assert on reasons and would otherwise have to count findings.
func findingsMentioning(v *Verdict, code, substr string) []string {
	var out []string
	for _, f := range v.Findings {
		if f.Code == code && strings.Contains(f.Detail, substr) {
			out = append(out, f.Detail)
		}
	}
	return out
}
