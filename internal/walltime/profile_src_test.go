package walltime

import (
	"os"
	"testing"
)

// p73Source returns the runtime-profile implementation source, so test 73 can
// assert the check uses no signing, attestation or roster machinery.
func p73Source(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("profile.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
