package walltime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// TestWorkflowExposesTheScoredUnion asserts the LIVE workflow and action YAML
// against the §21 model, rather than only asserting the model against itself.
//
// The interface test next door proves the four representations relate
// correctly; this one proves the shipped bytes implement them. Both are needed:
// a correct model wired to nothing is not an interface.
func TestWorkflowExposesTheScoredUnion(t *testing.T) {
	wf := readRepoFile(t, ".github/workflows/bucketed-reusable.yml")

	t.Run("the reusable workflow exposes every union name", func(t *testing.T) {
		// R8-D6: an earlier revision gave the plan action six scored inputs
		// while the workflow exposed only three, so a caller could set
		// scored: true and then fail AD-9/AD-10 with no way to supply what it
		// needs.
		for _, name := range WorkflowInputUnion() {
			// A workflow_call input is a six-space key under `inputs:`.
			re := regexp.MustCompile(`(?m)^      ` + regexp.QuoteMeta(name) + `:\s*$`)
			if !re.MatchString(wf) {
				t.Errorf("the reusable workflow does not expose union input %q", name)
			}
		}
	})

	t.Run("each action receives the inputs A declares for it", func(t *testing.T) {
		for action, inputs := range AddedActionInputs() {
			src := readRepoFile(t, filepath.Join(".github", "actions", action, "action.yml"))
			for _, in := range inputs {
				re := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(in) + `:\s*$`)
				if !re.MatchString(src) {
					t.Errorf("action %q does not declare input %q", action, in)
				}
			}
		}
	})

	t.Run("matrix jobs consume the plan job's outputs, never the caller inputs", func(t *testing.T) {
		// §10.5.2 step 3, and the decisive property of the transport repair:
		// exactly one value governs every bucket job of a run.
		bucket := bucketStepBlock(t, wf)
		if !strings.Contains(bucket, "needs.plan.outputs.cache-declaration-digest") {
			t.Error("run-bucket's cache-declaration-digest does not come from the plan job's outputs")
		}
		for _, caller := range []string{"inputs.cache-declaration-json", "inputs.cache-declaration-digest-expected"} {
			if strings.Contains(bucket, caller) {
				t.Errorf("run-bucket is wired to the caller input %q; a matrix job must consume the plan job's outputs only", caller)
			}
		}
		// And the file it verifies is job-local, never a caller pathname.
		if !strings.Contains(bucket, "steps.bucket-cache-declaration.outputs.file") {
			t.Error("run-bucket's cache-declaration-file is not the job-local materialized path")
		}
	})

	t.Run("the plan job verifies the caller declaration before invoking plan", func(t *testing.T) {
		// §10.5.2 steps 0b-0c. A pathname cannot cross the workflow-call
		// boundary, so the content is materialized and the digest recomputed
		// over what was actually written.
		if !strings.Contains(wf, "Materialize and verify the cache declaration") {
			t.Fatal("the plan job has no materialize-and-verify step")
		}
		idxVerify := strings.Index(wf, "Materialize and verify the cache declaration")
		idxPlan := strings.Index(wf, "name: Plan the buckets")
		if idxVerify < 0 || idxPlan < 0 || idxVerify > idxPlan {
			t.Fatal("the verify step must precede the plan step, so a mismatch fails before any matrix is emitted")
		}
	})

	t.Run("the plan job publishes both declaration outputs", func(t *testing.T) {
		for _, out := range []string{"cache-declaration-json", "cache-declaration-digest"} {
			if !strings.Contains(wf, out+": ${{ steps.plan.outputs."+out+" }}") {
				t.Errorf("the plan job does not publish %q as a job output", out)
			}
		}
	})

	t.Run("the plan job's output set stays exactly §21's three plus the retained pair", func(t *testing.T) {
		// §21 fixes the plan job's outputs at matrix, cache-declaration-json
		// and cache-declaration-digest; candidate-binary-digest is a retained
		// R54 output, not a new one, and §15.3a explicitly does NOT add a
		// runtime-profile-digest output.
		if strings.Contains(wf, "runtime-profile-digest:") {
			t.Error("a runtime-profile-digest job output was added; §15.3a says the declared object rides in the plan document and does not enlarge the output set")
		}
	})

	t.Run("the record job receives the observations directory and the config pair", func(t *testing.T) {
		rec := recordStepBlock(t, wf)
		for _, in := range []string{"wall-observations-dir", "campaign-config-json", "campaign-config-digest-expected"} {
			if !strings.Contains(rec, in+":") {
				t.Errorf("the record job does not pass %q", in)
			}
		}
	})
}

// bucketStepBlock returns the run-bucket invocation's `with:` block.
func bucketStepBlock(t *testing.T, wf string) string {
	t.Helper()
	i := strings.Index(wf, "/.github/actions/run-bucket")
	if i < 0 {
		t.Fatal("no run-bucket invocation found")
	}
	end := i + 4000
	if end > len(wf) {
		end = len(wf)
	}
	return wf[i:end]
}

func recordStepBlock(t *testing.T, wf string) string {
	t.Helper()
	i := strings.Index(wf, "/.github/actions/record")
	if i < 0 {
		t.Fatal("no record invocation found")
	}
	end := i + 2000
	if end > len(wf) {
		end = len(wf)
	}
	return wf[i:end]
}
