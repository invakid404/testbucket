package gorunner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/runner"
)

func writeLive(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "live.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadLivePackagesAcceptsBothSchemas(t *testing.T) {
	// The --live schema changed when identity/grouping moved to the neutral
	// id/atom fields. A file written by the older tool — import_path + atomic —
	// must still load with a POPULATED identity, or every package collides on an
	// empty id and, at ingest, the whole timing store is pruned. This pins that
	// both schemas normalise to the same neutral LivePackage.
	want := runner.LivePackage{
		ID: "example.test/repo/ext/common", Atom: "ext/common", HasTests: true,
		Dir: "ext/common", Module: "ext/common", Mode: runner.ModeOff,
	}

	oldSchema := `[
	  {"import_path":"example.test/repo/ext/common","dir":"ext/common","module":"ext/common","mode":"off","atomic":true,"has_tests":true}
	]`
	newSchema := `[
	  {"id":"example.test/repo/ext/common","atom":"ext/common","has_tests":true,"dir":"ext/common","module":"ext/common","mode":"off"}
	]`

	for name, body := range map[string]string{"old-schema": oldSchema, "new-schema": newSchema} {
		t.Run(name, func(t *testing.T) {
			got, err := LoadLivePackages(writeLive(t, body))
			if err != nil {
				t.Fatalf("LoadLivePackages: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d entries, want 1", len(got))
			}
			if got[0] != want {
				t.Errorf("%s normalised to %+v, want %+v", name, got[0], want)
			}
		})
	}

	// A GOWORK=off entry from the old schema that set only `atomic` (no atom, no
	// off-mode) must still derive its co-scheduling key from the module.
	oldAtomicOnly := `[{"import_path":"m/a","module":"m","mode":"off","atomic":true,"has_tests":true}]`
	got, err := LoadLivePackages(writeLive(t, oldAtomicOnly))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Atom != "m" {
		t.Errorf("atomic old-schema entry got Atom=%q, want the module %q", got[0].Atom, "m")
	}
}

func TestLoadLivePackagesRejectsEmptyIdentity(t *testing.T) {
	// An entry with no resolvable identity (neither id nor import_path) is
	// malformed: proceeding would give it an empty id that collides with every
	// other empty one and prunes the store at ingest. It must be a loud error,
	// never a silently-empty id.
	cases := map[string]string{
		"neither id nor import_path": `[{"module":".","mode":"work","has_tests":true}]`,
		"explicitly empty id":        `[{"id":"","has_tests":true}]`,
		"one good, one empty":        `[{"id":"pkg/a","has_tests":true},{"dir":"b","has_tests":true}]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadLivePackages(writeLive(t, body)); err == nil {
				t.Fatal("an entry with no identity was accepted")
			} else if !strings.Contains(err.Error(), "no identity") {
				t.Errorf("error does not explain the missing identity: %v", err)
			}
		})
	}
}
