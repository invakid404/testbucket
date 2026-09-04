package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE v0 FREEZE IS EXECUTABLE, NOT DOCUMENTARY.
//
// The reusable workflow's jobs declare their own least-privilege tokens, which
// requires a caller to grant `actions: read` as well as `contents: read`. That
// is breaking for a caller, and the compatibility contract handles it by
// versioning the boundary: the reusable workflow and the README both promise
// `v0` stays at v0.2.2 so an existing `@v0` consumer receives the requirement
// only after a deliberate re-pin.
//
// The release workflow's alias job would have broken that promise on the first
// successful v0.3.x publish — it derives the alias from the tag and force-moves
// it to the highest published release of that major. The absence of v0.3.0
// meant the bug had not fired, not that the source preserved the boundary.
//
// The two halves are asserted together here, because a promise in one file and
// a force-move in another is exactly how they drifted apart.
func TestTheReleaseWorkflowDoesNotMoveTheFrozenV0Alias(t *testing.T) {
	release, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(release)

	// The alias job still exists and still force-moves an alias — this test is
	// about which majors it may move, not about removing the mechanism.
	move := strings.Index(body, `git tag -f "$major" "$target"`)
	if move < 0 {
		t.Fatal("the alias job no longer moves a major alias; this test is looking at the wrong workflow")
	}
	guard := strings.Index(body, `if [ "$major" = "v0" ]; then`)
	if guard < 0 {
		t.Fatal("the alias job has no v0 freeze, so the first v0.3.x publish would move v0 and deliver the held-back caller-permission requirement to every @v0 consumer")
	}
	if guard > move {
		t.Errorf("the v0 freeze at %d comes AFTER the alias is moved at %d", guard, move)
	}
	// It must EXIT, not fall through.
	after := body[guard : guard+900]
	if !strings.Contains(after, "exit 0") {
		t.Error("the v0 branch does not exit, so it falls through to the force-move it is meant to prevent")
	}
	if !strings.Contains(after, "FROZEN") || !strings.Contains(after, "v0.2.2") {
		t.Error("the v0 branch does not say what it is freezing or where; a guard nobody can read is a guard somebody deletes")
	}

	// AND THE PROMISE IT KEEPS IS STILL WRITTEN DOWN, in both places a
	// consumer reads.
	for _, f := range []struct{ path, want string }{
		{filepath.Join("..", "..", ".github", "workflows", "bucketed-reusable.yml"), "stays pinned at v0.2.2"},
		{filepath.Join("..", "..", "README.md"), "pinned at v0.2.2"},
	} {
		b, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), f.want) {
			t.Errorf("%s no longer states the v0 hold (looked for %q); the freeze and the promise must move together", filepath.Base(f.path), f.want)
		}
	}
}
