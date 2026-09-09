package walltime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// bcInvMembershipFromRegistry parses memberships.bc_inv and
// memberships.tuple_leaves out of the field registry, so the mutation matrix
// is generated from the registry rather than from a Go declaration.
func bcInvMembershipFromRegistry(t *testing.T) (membership []string, tupleLeaves map[string][]string) {
	t.Helper()
	block := parseRegistryBlock(t, "# field-registry v2")

	i := strings.Index(block, "\n  bc_inv:")
	if i < 0 {
		t.Fatal("memberships.bc_inv not found in the registry")
	}
	seg := block[i:]
	started := false
	for _, line := range strings.Split(seg, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip the "bc_inv:" header itself; the first draft broke on it and
		// parsed an empty membership.
		if !started {
			if trimmed == "bc_inv:" {
				started = true
			}
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") {
			break
		}
		v := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		// Strip a trailing comment.
		if j := strings.Index(v, "#"); j >= 0 {
			v = strings.TrimSpace(v[:j])
		}
		membership = append(membership, v)
	}

	tupleLeaves = map[string][]string{}
	if j := strings.Index(block, "tuple_leaves:"); j >= 0 {
		tseg := block[j:]
		var current string
		key := regexp.MustCompile(`^([a-z_]+\.[a-z_]+):$`)
		for _, line := range strings.Split(tseg, "\n")[1:] {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if m := key.FindStringSubmatch(trimmed); m != nil {
				current = m[1]
				continue
			}
			if strings.HasPrefix(trimmed, "- ") && current != "" {
				tupleLeaves[current] = append(tupleLeaves[current],
					strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
				continue
			}
			if current != "" && !strings.HasPrefix(trimmed, "- ") {
				break
			}
		}
	}
	return membership, tupleLeaves
}

// allGoSource concatenates every non-test Go file, for the symbol-resolution
// half of test 28.
func allGoSource(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, dir := range []string{"internal", "cmd", "testdata"} {
		err := filepath.Walk(filepath.Join("..", "..", dir), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") {
				return nil
			}
			c, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			b.Write(c)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}
