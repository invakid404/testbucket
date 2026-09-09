package walltime

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// qcNamesFromContract parses §7.1's table so the QC case set is enumerated from
// the contract rather than transcribed from it.
func qcNamesFromContract(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "walltime", "acceptance-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := regexp.MustCompile(`### 7\.1 `).FindStringIndex(src)
	if start == nil {
		t.Fatal("contract §7.1 not found")
	}
	end := regexp.MustCompile(`## 8\. `).FindStringIndex(src[start[0]:])
	if end == nil {
		t.Fatal("contract §8 not found; cannot bound §7.1")
	}
	seg := src[start[0] : start[0]+end[0]]
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`\|\s*\*{0,2}(QC\d+[ab]?)\*{0,2}\s*\|`).FindAllStringSubmatch(seg, -1) {
		seen[m[1]] = true
	}
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
