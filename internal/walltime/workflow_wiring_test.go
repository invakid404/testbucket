package walltime

import (
	"encoding/json"
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

// TestActionInternalVerificationSteps gates Step E: the run-bucket and record
// actions must not merely DECLARE the §21 inputs, they must verify them.
//
// An input that arrives and is never checked is decoration: §10.5.2 step 4 and
// §19.9c-1 both turn on a recomputation happening inside the action, before the
// work it guards.
func TestActionInternalVerificationSteps(t *testing.T) {
	bucket := readRepoFile(t, filepath.Join(".github", "actions", "run-bucket", "action.yml"))
	record := readRepoFile(t, filepath.Join(".github", "actions", "record", "action.yml"))

	t.Run("run-bucket recomputes the declaration digest", func(t *testing.T) {
		if !strings.Contains(bucket, "Verify the cache declaration") {
			t.Fatal("run-bucket has no declaration-verification step")
		}
		if !strings.Contains(bucket, "sha256sum") {
			t.Error("run-bucket does not recompute a digest; byte identity would be an assertion, not a check")
		}
		// The verification must precede the bucket script.
		vi := strings.Index(bucket, "Verify the cache declaration")
		ri := strings.Index(bucket, "Run the bucket")
		if vi < 0 || ri < 0 || vi > ri {
			t.Error("the declaration is verified after the bucket runs; a mismatch must fail BEFORE any script starts")
		}
	})

	t.Run("run-bucket validates the producer state machine", func(t *testing.T) {
		if !strings.Contains(bucket, "Validate the dependency-cache producer") {
			t.Fatal("run-bucket has no producer-validation step")
		}
		// The rules that make an empty hit distinguishable from an absent one.
		for _, rule := range []string{
			"empty is not false",
			"two producers is rejected",
			"hit true with an empty matched key",
			"hit false with a non-empty matched key",
		} {
			if !strings.Contains(bucket, rule) {
				t.Errorf("the producer validation does not enforce %q", rule)
			}
		}
		// Every legal mode has exactly one legal producer state.
		if !strings.Contains(bucket, "mode disabled requires dependency-cache-producer none") {
			t.Error("disabled mode does not pin its producer to none")
		}
		if !strings.Contains(bucket, "exact-key requires a producer") {
			t.Error("exact-key mode does not reject producer none")
		}
	})

	t.Run("run-bucket runs QC14a over the EXECUTED binary", func(t *testing.T) {
		if !strings.Contains(bucket, "QC14a") {
			t.Fatal("run-bucket has no QC14a step")
		}
		if !strings.Contains(bucket, "MONGOMS_SYSTEM_BINARY") {
			t.Error("QC14a does not hash the file MONGOMS_SYSTEM_BINARY names, so a decoy could be hashed instead")
		}
		// It must run before upload, i.e. before the envelope closes.
		qi := strings.Index(bucket, "QC14a")
		ci := strings.Index(bucket, "Close the wall-time action envelope")
		if qi < 0 || ci < 0 || qi > ci {
			t.Error("QC14a must run on the bucket runner before artifact upload")
		}
	})

	t.Run("record verifies the campaign config before invoking ingest", func(t *testing.T) {
		if !strings.Contains(record, "Materialize and verify the campaign config") {
			t.Fatal("record has no config-verification step")
		}
		vi := strings.Index(record, "Materialize and verify the campaign config")
		ii := strings.Index(record, "testbucket ingest")
		if vi < 0 || ii < 0 || vi > ii {
			t.Error("the config is verified after ingest runs; §19.9c-1 requires it before")
		}
		if !strings.Contains(record, "printf '%s'") {
			t.Error("the config is written with something other than printf; an expanding shell would change the bytes the digest covers")
		}
	})

	t.Run("record passes the LOCAL verified path to ingest", func(t *testing.T) {
		if !strings.Contains(record, "--campaign-config \"$TB_CAMPAIGN_CONFIG\"") {
			t.Error("ingest does not receive the verified local path")
		}
		if !strings.Contains(record, "steps.campaign-config.outputs.file") {
			t.Error("the config path does not come from the verify step's own output")
		}
		if !strings.Contains(record, "--wall-observations \"$TB_WALL_OBSERVATIONS\"") {
			t.Error("ingest does not receive the observations directory")
		}
	})

	t.Run("record refuses a campaign id with no verified config", func(t *testing.T) {
		if !strings.Contains(record, "Refuse a campaign id with no verified config") {
			t.Fatal("record accepts campaign-id without the config inputs; §15.1b would reject every scored row")
		}
	})
}

// TestEveryActionShipsExactlyTheMapsInputSet compares each SIMPLIFY action's
// COMPLETE shipped input set against the governed component map, in both
// directions.
//
// The test above it checks only the ADDED-input projection — that every input
// §21 adds is declared. That is a one-directional check over a subset, so an
// action could declare every added input and still ship a dozen the map's
// `remove_inputs` list requires absent, which is exactly what happened: the
// install, plan and record actions kept 17 research-era inputs through two
// visits of "the SIMPLIFY surface is clean" while every §21 test stayed green.
//
// The map is the authority and it enumerates the sets exactly, so this reads
// them from the map rather than restating them here. A restatement would be
// one more thing that can agree with itself while the shipped YAML says
// something else.
func TestEveryActionShipsExactlyTheMapsInputSet(t *testing.T) {
	var cm struct {
		ActionInterfaces []struct {
			Action         string   `json:"action"`
			Classification string   `json:"classification"`
			KeepInputs     []string `json:"keep_inputs"`
			RemoveInputs   []string `json:"remove_inputs"`
			AddInputs      []string `json:"add_inputs"`
			AddedInputs    []string `json:"added_inputs"`
		} `json:"action_interfaces"`
	}
	if err := json.Unmarshal([]byte(readRepoFile(t, "docs/walltime/component-map.json")), &cm); err != nil {
		t.Fatalf("parse the component map: %v", err)
	}
	if len(cm.ActionInterfaces) == 0 {
		t.Fatal("the component map declares no action interfaces; this test is reading the wrong document")
	}

	checked := 0
	for _, iface := range cm.ActionInterfaces {
		path := filepath.Join(".github", "actions", iface.Action, "action.yml")
		if _, err := os.Stat(filepath.Join("..", "..", path)); err != nil {
			// A REMOVE-classified action is absent by design; the REMOVE sweep
			// is what asserts that, not this test.
			if iface.Classification == "REMOVE" {
				continue
			}
			t.Errorf("%s is classified %s but is not shipped", path, iface.Classification)
			continue
		}
		checked++

		declared := actionInputNames(t, path)
		want := map[string]bool{}
		for _, in := range iface.KeepInputs {
			want[in] = true
		}
		for _, in := range iface.AddInputs {
			want[in] = true
		}
		for _, in := range iface.AddedInputs {
			want[in] = true
		}
		remove := map[string]bool{}
		for _, in := range iface.RemoveInputs {
			remove[in] = true
		}

		for in := range want {
			if !declared[in] {
				t.Errorf("%s does not declare input %q, which the map keeps", path, in)
			}
		}
		for in := range declared {
			switch {
			case remove[in]:
				// Named explicitly, because "the map says to remove this" is a
				// sharper finding than "this is unexpected".
				t.Errorf("%s still declares input %q, which the map's remove_inputs requires absent", path, in)
			case !want[in]:
				t.Errorf("%s declares input %q, which the map's keep/add lists do not include", path, in)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no shipped action was compared; the paths this test reads have moved")
	}
}

// actionInputNames reads one composite action's top-level input names.
//
// It scans for two-space keys under `inputs:` rather than parsing YAML,
// because this package has no YAML dependency and the shape being read is
// fixed: `inputs:` at column 0, one key per input at column 2, and the next
// column-0 key ends the block. A malformed file fails the assertions above
// rather than being silently read as empty — hence the fatal on an empty set.
func actionInputNames(t *testing.T, rel string) map[string]bool {
	t.Helper()
	src := readRepoFile(t, rel)
	out := map[string]bool{}
	key := regexp.MustCompile(`^  ([a-z0-9][a-z0-9-]*):`)
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		if !inBlock {
			inBlock = line == "inputs:"
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#") {
			break // a new column-0 key: the inputs block is over
		}
		if m := key.FindStringSubmatch(line); m != nil {
			out[m[1]] = true
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s declares no inputs at all; the block this test reads has moved", rel)
	}
	return out
}
