package walltime

import (
	"os"
	"path/filepath"
	"testing"
)

// ringRowSource returns the ring row's declaration, so test 70 can assert the
// ring registers no cache_state path.
func ringRowSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "core", "store_wall.go"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
