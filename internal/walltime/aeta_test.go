package walltime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheAetaRegistryProofIsNotDeclaredAnywhere is this file's whole subject.
//
// The registry proof's assertions were about a sealed component lineage rather
// than about A_eta, so when the proof went there was nothing here to test — and
// a test file with no tests is how a package keeps reporting `ok` while its
// subject rots. What this file can still check is that the surface it used to
// cover has not come back: the types were dead and EXPORTED, which is the state
// that lets an internal caller rebuild the thing that was removed.
//
// The objective itself is checked next door: the Aeta gate in gates_test.go
// decides the four-parameter interval, and that is A_eta's own behaviour.
func TestTheAetaRegistryProofIsNotDeclaredAnywhere(t *testing.T) {
	declarations := []string{
		"type ComponentClass ",
		"type Component struct",
		"type AetaInputs struct",
		"type InstantiatedComponent struct",
		"type AetaInstance struct",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var scanned int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		src := string(b)
		for _, decl := range declarations {
			if strings.Contains(src, "\n"+decl) {
				t.Errorf("%s declares %q; the Aeta registry proof is REMOVE-classified", name, decl)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no package source; this test is reading the wrong directory")
	}
	// And the registry's own wire kind is not DECLARED anywhere.
	//
	// A declaration, not a mention: this file and aeta.go both name the kind in
	// prose, because a removal that cannot be explained in the source is a
	// removal the next reader undoes. What is prohibited is binding the string
	// to an identifier again, which is what would put it back on a document.
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" {
			continue
		}
		b, _ := os.ReadFile(name)
		// Assembled from parts so this file does not match its own needle:
		// the literal spelled out here would be a declaration by the rule
		// above, and the scan would convict the scanner.
		needle := `= "tb.walltime.` + "aeta-registry" + `/v1"`
		if strings.Contains(string(b), needle) {
			t.Errorf("%s declares the aeta-registry wire kind", name)
		}
	}
}
