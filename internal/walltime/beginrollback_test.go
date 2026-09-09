package walltime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAFailedBeginLeavesNoHandoff is the reduced form of the begin-rollback
// regression.
//
// The original asserted that BeginAction ARMS a rollback list and that every
// error return between arming and disarming goes through it. That property was
// about resources BeginAction started before the handoff existed — two observer
// processes, a containment, a signer registration and a roster — and the
// removal plan deletes all of them. There is nothing left for a rollback to
// tear down, so asserting the mechanism would be asserting scaffolding.
//
// What the rollback was FOR still matters and is what this test now checks: a
// begin that fails must leave no handoff, so `wall end` cannot attach to a
// half-open envelope and report an interval that never opened. That is the
// observable consequence, and it holds regardless of how begin is implemented.
func TestAFailedBeginLeavesNoHandoff(t *testing.T) {
	t.Run("a successful begin leaves exactly one handoff", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := BeginAction(dir, RunIdentity{BucketID: "b1"}, DefaultTimeout); err != nil {
			t.Fatalf("BeginAction: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, actionStateFile)); err != nil {
			t.Fatalf("a successful begin left no handoff: %v", err)
		}
		if _, err := LoadActionState(dir); err != nil {
			t.Fatalf("the handoff does not load: %v", err)
		}
	})

	t.Run("a begin that cannot create its directory leaves no handoff", func(t *testing.T) {
		// A file where the records directory should be: MkdirAll fails, and
		// the function must return before writing anything.
		base := t.TempDir()
		blocked := filepath.Join(base, "records")
		if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := BeginAction(blocked, RunIdentity{BucketID: "b1"}, DefaultTimeout); err == nil {
			t.Fatal("BeginAction succeeded against a path that is a file")
		}
		if _, err := os.Stat(filepath.Join(blocked, actionStateFile)); err == nil {
			t.Fatal("a failed begin left a handoff behind")
		}
	})

	t.Run("EndAction refuses to attach with no handoff", func(t *testing.T) {
		// The consequence the rollback existed to guarantee: without a
		// handoff there is no interval to close, and end says so rather than
		// inventing one.
		if _, err := EndAction(t.TempDir(), TerminalPassed, ""); err == nil {
			t.Fatal("EndAction closed an envelope that was never opened")
		}
	})

	t.Run("the handoff is written last, after the opening reading", func(t *testing.T) {
		// Ordering, asserted from the production source: the reading must
		// precede the handoff write, or the handoff could name a start that
		// had not been taken.
		body := productionFunc(t, "action.go", "func BeginAction(")
		read := strings.Index(body, "start := clock.Now()")
		wrote := strings.Index(body, "actionStateFile")
		if read < 0 {
			t.Fatal("BeginAction no longer takes an opening reading")
		}
		if wrote < 0 {
			t.Fatal("BeginAction no longer writes the handoff")
		}
		if read > wrote {
			t.Fatal("BeginAction writes the handoff before taking the opening reading")
		}
	})
}
