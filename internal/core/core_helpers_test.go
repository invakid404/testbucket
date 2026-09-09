package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readCoreFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func indexOf(hay, needle string) int { return strings.Index(hay, needle) }

func maxInt0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func containsAny(hay string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(hay, n) {
			return true
		}
	}
	return false
}
