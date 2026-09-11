package walltime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTheSetupCommandRunsWithThisStepsEnvironment is R08's behavioural half.
//
// The run-bucket action's public `setup-command` input promised that a
// measured setup command ran with "both the wall-time capabilities and the
// GitHub file-command channels" stripped from its environment, and the
// `--wrapper-chain` help promised the same in the other direction. Neither was
// true: the machinery that filtered the environment went with the
// hostile-runner model §0.1 places out of scope, and `RunInActionWith` hands
// the child `os.Environ()`. A consumer reading that description would give the
// seam code it believed could not reach $GITHUB_ENV.
//
// The descriptions now say what happens, and this is what holds them to it. It
// asserts the RETAINED behaviour rather than the removed promise on purpose:
// if the filtering is ever reintroduced this fails, and whoever reintroduces
// it has to come back to the text — which is the failure mode that produced
// the finding, run in reverse.
func TestTheSetupCommandRunsWithThisStepsEnvironment(t *testing.T) {
	dir := t.TempDir()
	seen := filepath.Join(dir, "seen")

	// A file-command channel and a wall-time-shaped variable: the two classes
	// the description used to promise were removed.
	t.Setenv("GITHUB_OUTPUT", filepath.Join(dir, "github-output"))
	t.Setenv("TB_WALL_CAPABILITY", "carried")

	if _, err := BeginAction(dir, RunIdentity{BucketID: "b1", RunID: "run-1"}, 30*time.Second); err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	code, err := RunInAction(dir, []string{"sh", "-c", `printf '%s|%s' "${GITHUB_OUTPUT:-}" "${TB_WALL_CAPABILITY:-}" > "$0"`, seen}, dir, nil, nil)
	if err != nil || code != 0 {
		t.Fatalf("RunInAction: code=%d err=%v", code, err)
	}
	if _, err := EndAction(dir, TerminalPassed, ""); err != nil {
		t.Fatalf("EndAction: %v", err)
	}

	b, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("the setup command wrote nothing: %v", err)
	}
	got := strings.Split(string(b), "|")
	if len(got) != 2 {
		t.Fatalf("the setup command reported %q, want two fields", string(b))
	}
	if got[0] != os.Getenv("GITHUB_OUTPUT") {
		t.Errorf("the setup command saw GITHUB_OUTPUT=%q, this process has %q; "+
			"the documented behaviour is that it inherits this step's environment unchanged", got[0], os.Getenv("GITHUB_OUTPUT"))
	}
	if got[1] != "carried" {
		t.Errorf("the setup command saw TB_WALL_CAPABILITY=%q, want %q", got[1], "carried")
	}
}
