package vitestrunner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestTheRetainedExecutableIsTheOneThatRan is the F8 regression.
//
// `exec.Command` resolves a bare name on PATH, but a head that CONTAINS A
// SLASH is left exactly as written and interpreted by the kernel against the
// child's working directory. The provenance stored that string, so `./tool`
// was retained as `./tool`: a replay from any other directory follows it to
// different bytes or to nothing, and a closure hashed from it hashes the wrong
// file.
func TestTheRetainedExecutableIsTheOneThatRan(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf '[]\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, head := range []string{"./tool", tool} {
		n := nodetool{command: []string{head}, env: []string{"PATH=/usr/bin:/bin"}}
		var p ExecProvenance
		if _, err := n.runWith(context.Background(), dir, &p); err != nil {
			t.Fatalf("%s: %v", head, err)
		}
		if !filepath.IsAbs(p.Path) {
			t.Errorf("head %q retained a relative executable %q; a replay cannot follow it", head, p.Path)
		}
		if p.Path != tool {
			t.Errorf("head %q retained %q, not the file that ran (%s)", head, p.Path, tool)
		}
		// The argv keeps what was WRITTEN — that is the command line — while
		// the path says what it resolved to.
		if len(p.Argv) == 0 || p.Argv[0] != head {
			t.Errorf("head %q lost its argv: %v", head, p.Argv)
		}
	}

	// A PATH lookup still yields the resolved absolute program.
	n := nodetool{command: []string{"sh"}, env: []string{"PATH=" + os.Getenv("PATH")}}
	var p ExecProvenance
	if _, err := n.runWith(context.Background(), dir, &p, "-c", "printf '[]'"); err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(p.Path) {
		t.Errorf("a PATH-resolved head retained %q", p.Path)
	}

	// And the resolution is the kernel's: relative to the directory the child
	// is given, not to the wrapper's own cwd.
	if got := absoluteExecutable("./tool", dir); got != tool {
		t.Errorf("absoluteExecutable resolved ./tool in %s to %q", dir, got)
	}
	if got := absoluteExecutable("/bin/sh", dir); got != "/bin/sh" {
		t.Errorf("an absolute head was rewritten to %q", got)
	}
}
