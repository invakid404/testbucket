package walltime

import (
	"crypto/sha256"
	"encoding/hex"
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

	t.Run("a campaign id with no verified config is refused before append", func(t *testing.T) {
		// This used to assert that the record action contains a step NAMED
		// "Refuse a campaign id with no verified config". That step's
		// condition read `inputs.campaign-id`, which the action does not
		// declare and the record job does not pass, so the condition was
		// always false — the test passed on a string while the guard it
		// claimed to cover could never fire.
		//
		// The property is real, and §15.1b enforces it where it can: against
		// the rows ingest actually parses. So it is asserted there, on
		// behaviour.
		obs := buildObservation(t, false, "campaign-1", "workload-1", "head-1", 0)
		trainable, accept, reason := TrainableAtAppend(obs, nil)
		if accept || trainable {
			t.Errorf("a row carrying campaign_id %q was accepted with no verified config (trainable=%v accept=%v)",
				obs.CampaignID, trainable, accept)
		}
		if !strings.Contains(reason, "campaign_id") {
			t.Errorf("the refusal does not name the campaign id: %q", reason)
		}

		// And the same row IS accepted once a config verifies it, so the
		// refusal above is the campaign identity being checked rather than
		// everything being refused.
		cfg, err := VerifyCampaignConfig(campaignConfigBytes(t, "campaign-1"), campaignConfigDigest(t, "campaign-1"))
		if err != nil {
			t.Fatalf("VerifyCampaignConfig: %v", err)
		}
		if _, accept, reason := TrainableAtAppend(obs, &cfg); !accept {
			t.Errorf("a row matching its verified config was refused: %s", reason)
		}
	})

	t.Run("no action condition reads an input the action does not declare", func(t *testing.T) {
		// The class of defect the step above was: a guard whose condition can
		// never be true, because the input it reads is not part of the
		// action's interface. It is invisible to every green test that checks
		// for the guard's presence rather than its reachability.
		cond := regexp.MustCompile(`(?m)^\s+if:\s*(.+)$`)
		ref := regexp.MustCompile(`inputs\.([a-z0-9][a-z0-9-]*)`)
		for _, action := range []string{"install", "plan", "record", "run-bucket", "verify-wall"} {
			rel := filepath.Join(".github", "actions", action, "action.yml")
			declared := actionInputNames(t, rel)
			for _, m := range cond.FindAllStringSubmatch(readRepoFile(t, rel), -1) {
				for _, r := range ref.FindAllStringSubmatch(m[1], -1) {
					if !declared[r[1]] {
						t.Errorf("%s has a condition reading undeclared input %q, so it can never fire: %s",
							rel, r[1], strings.TrimSpace(m[1]))
					}
				}
			}
		}
	})
}

// campaignConfigBytes is a minimal §19.9a document naming one campaign.
func campaignConfigBytes(t *testing.T, id string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"manifest": map[string]any{"campaign_id": id}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// campaignConfigDigest is the caller-side expected digest of those bytes, which
// §19.9c-1 verifies before ingest runs. It is the bare hex form the transport
// carries, not the `sha256:`-prefixed record form.
func campaignConfigDigest(t *testing.T, id string) string {
	t.Helper()
	sum := sha256.Sum256(campaignConfigBytes(t, id))
	return hex.EncodeToString(sum[:])
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

// TestNoCompositeShellReadsAnUnexportedVariable is the F3 regression.
//
// Every composite step runs `bash -euo pipefail`, so `set -u` terminates the
// step the first time it dereferences a variable nothing exported. The
// run-bucket action shipped with `"$TB_SCORED"` in its measured branch while
// declaring no `scored` input and exporting no such variable: every measured
// bucket died there, before the generated script ran, and no test noticed —
// the workflow tests scan this file for step names and phrases rather than
// checking what the shell actually reads.
//
// So this checks the invariant that broke: a variable the body dereferences
// without a `:-` default must be exported by that step, provided by the
// runner, or a shell builtin. It is a static check on purpose — the bodies
// create directories and start processes, and a test that executed them would
// be testing the runner rather than the wiring.
func TestNoCompositeShellReadsAnUnexportedVariable(t *testing.T) {
	// Provided by the Actions runner in every step's environment.
	runnerProvided := map[string]bool{
		"GITHUB_ENV": true, "GITHUB_OUTPUT": true, "GITHUB_PATH": true,
		"GITHUB_STEP_SUMMARY": true, "GITHUB_WORKSPACE": true, "GITHUB_REPOSITORY": true,
		"GITHUB_RUN_ID": true, "GITHUB_RUN_ATTEMPT": true, "GITHUB_JOB": true,
		"GITHUB_SHA": true, "GITHUB_REF": true, "GITHUB_REF_NAME": true,
		"GITHUB_ACTION_PATH": true, "GITHUB_EVENT_NAME": true, "GITHUB_TOKEN": true,
		"RUNNER_TEMP": true, "RUNNER_OS": true, "RUNNER_ARCH": true, "RUNNER_TOOL_CACHE": true,
		"HOME": true, "PATH": true, "PWD": true, "USER": true, "SHELL": true, "TMPDIR": true,
	}
	// Shell builtins and specials.
	shellProvided := map[string]bool{
		"PIPESTATUS": true, "BASH_SOURCE": true, "FUNCNAME": true, "LINENO": true,
		"RANDOM": true, "SECONDS": true, "OSTYPE": true, "IFS": true, "REPLY": true,
		"BASH_REMATCH": true, "PPID": true, "UID": true, "EUID": true, "HOSTNAME": true,
	}

	// A dereference of $NAME or ${NAME} that is NOT ${NAME:-...}, ${NAME:=...},
	// ${NAME:?...} or ${NAME+...} — those supply or demand their own value.
	deref := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
	guarded := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)[:+\-?=]`)

	checked := 0
	for _, action := range []string{"install", "plan", "record", "run-bucket", "verify-wall"} {
		rel := filepath.Join(".github", "actions", action, "action.yml")
		src := readRepoFile(t, rel)
		for _, step := range compositeShellSteps(t, src) {
			checked++
			body := stripShellComments(step.run)
			exported := map[string]bool{}
			for name := range step.env {
				exported[name] = true
			}
			// Assignments inside the body define their own names.
			for _, m := range regexp.MustCompile(`(?m)^\s*(?:local\s+|export\s+)?([A-Za-z_][A-Za-z0-9_]*)=`).FindAllStringSubmatch(body, -1) {
				exported[m[1]] = true
			}
			for _, m := range regexp.MustCompile(`(?m)for\s+([A-Za-z_][A-Za-z0-9_]*)\s+in`).FindAllStringSubmatch(body, -1) {
				exported[m[1]] = true
			}
			// `read` binds its own names, including in a `while read` loop.
			for _, m := range regexp.MustCompile(`\bread\s+(?:-[A-Za-z]+\s+)*([A-Za-z_][A-Za-z0-9_ ]*)`).FindAllStringSubmatch(body, -1) {
				for _, name := range strings.Fields(m[1]) {
					exported[name] = true
				}
			}
			for _, g := range guarded.FindAllStringSubmatch(body, -1) {
				exported[g[1]] = true
			}
			for _, m := range deref.FindAllStringSubmatch(body, -1) {
				name := m[1]
				if name == "" {
					name = m[2]
				}
				if exported[name] || runnerProvided[name] || shellProvided[name] {
					continue
				}
				t.Errorf("%s step %q dereferences $%s under `set -u`, and the step exports no such variable: the step dies there before it runs anything",
					rel, step.name, name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no composite shell step was checked; the structure this test reads has moved")
	}
}

// compositeShellStep is one `shell: bash` step of a composite action.
type compositeShellStep struct {
	name string
	run  string
	env  map[string]bool
}

// compositeShellSteps extracts each shell step's name, env keys and run body.
//
// It reads the YAML by structure rather than parsing it, because this package
// has no YAML dependency. The shape is fixed: steps are `    - name:` entries,
// `      env:` holds `        KEY: value` pairs, and `      run: |` opens a
// block scalar that runs to the next key at its own indent.
func compositeShellSteps(t *testing.T, src string) []compositeShellStep {
	t.Helper()
	var out []compositeShellStep
	lines := strings.Split(src, "\n")
	var cur *compositeShellStep
	inEnv, inRun := false, false
	var run []string
	flush := func() {
		if cur != nil && inRun {
			cur.run = strings.Join(run, "\n")
		}
		if cur != nil && cur.run != "" {
			out = append(out, *cur)
		}
		cur, inEnv, inRun, run = nil, false, false, nil
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "    - name:") || strings.HasPrefix(line, "    - uses:") {
			flush()
			name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- name:"))
			cur = &compositeShellStep{name: name, env: map[string]bool{}}
			continue
		}
		if cur == nil {
			continue
		}
		if inRun {
			// The run block ends at the next key at step-key indent.
			if line != "" && !strings.HasPrefix(line, "        ") && strings.HasPrefix(line, "      ") {
				inRun = false
				cur.run = strings.Join(run, "\n")
			} else {
				run = append(run, line)
				continue
			}
		}
		switch {
		case strings.HasPrefix(line, "      env:"):
			inEnv = true
		case strings.HasPrefix(line, "      run:"):
			inEnv, inRun = false, true
			run = nil
		case inEnv && strings.HasPrefix(line, "        "):
			if k, _, ok := strings.Cut(strings.TrimSpace(line), ":"); ok {
				cur.env[strings.TrimSpace(k)] = true
			}
		case strings.HasPrefix(line, "      "):
			inEnv = false
		}
	}
	flush()
	return out
}

// stripShellComments removes `#` comment text so a variable NAMED in a comment
// is not read as a dereference. The removed TB_SCORED gate is described in a
// comment that quotes it, and quoting a defect is not committing it.
func stripShellComments(body string) string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// TestTheRecordActionCarriesTheComparabilityKeyOnEveryIngest guards the
// v0.2.2 upgrade path.
//
// §15.3's key reaches ingest through the plan, because the component map gives
// this action no `comparability-key` input. When --wall-shard-plan was passed
// only inside the wall-observations branch, an ordinary reporter ingest over a
// restored schema-1 store had no key, could not run §15.2's forward migration
// and exited 1 — a break with no wall observations anywhere in the run.
func TestTheRecordActionCarriesTheComparabilityKeyOnEveryIngest(t *testing.T) {
	yml := readRepoFile(t, ".github/actions/record/action.yml")
	wallBranch := strings.Index(yml, `if [ -n "${TB_WALL_OBSERVATIONS:-}" ]`)
	if wallBranch < 0 {
		t.Fatal("the record action no longer has a wall-observations branch")
	}
	plan := strings.Index(yml, "--wall-shard-plan")
	if plan < 0 {
		t.Fatal("the record action never passes --wall-shard-plan, so ingest gets no comparability key")
	}
	if plan > wallBranch {
		t.Error("--wall-shard-plan is passed only inside the wall-observations branch; " +
			"a reporter-only ingest over a schema-1 store then has no key and cannot migrate")
	}
}

// TestTheReusableWorkflowSchedulesAndIdentifiesWithOneExpression is F2.
//
// Jobs ran on a `runs-on` input that defaulted to `ubuntu-latest` while plan,
// run and record identity came from a separate `runs-on-label` that defaulted
// to EMPTY — and QC12 rejects an empty observed label unconditionally. This
// repository's own default measured run therefore could not qualify or train,
// whatever it measured, and no test noticed because every test passes an
// explicit label.
//
// One input now does both, so the two cannot come apart again.
func TestTheReusableWorkflowSchedulesAndIdentifiesWithOneExpression(t *testing.T) {
	wf := readRepoFile(t, ".github/workflows/bucketed-reusable.yml")

	// Every job schedules on the identity expression.
	sched := regexp.MustCompile(`(?m)^    runs-on:\s*(.+)$`)
	found := sched.FindAllStringSubmatch(wf, -1)
	if len(found) == 0 {
		t.Fatal("the reusable workflow schedules no jobs")
	}
	for _, m := range found {
		if got := strings.TrimSpace(m[1]); got != "${{ inputs.runs-on-label }}" {
			t.Errorf("a job schedules on %q; scheduling and identity must be the one expression", got)
		}
	}
	// And there is no second scheduling input left to diverge from it.
	if regexp.MustCompile(`(?m)^      runs-on:\s*$`).MatchString(wf) {
		t.Error("a separate `runs-on` input still exists; two inputs is how the label came apart")
	}

	// THE DEFAULT PATH MUST PASS QC12. The label the workflow uses when a
	// caller supplies nothing is read out of the file and put through the
	// production check, rather than asserted to be non-empty.
	label := workflowInputDefault(t, wf, "runs-on-label")
	if label == "" {
		t.Fatal("runs-on-label has no default; the default measured run records an empty label and QC12 refuses it")
	}
	if err := QC12(label, label, 1); err != nil {
		t.Errorf("the default reusable-workflow path fails QC12: %v", err)
	}

	// The repository's own caller supplies it explicitly rather than relying on
	// the default, because a default nobody reads is how the empty value
	// survived.
	caller := readRepoFile(t, ".github/workflows/bucketed.yml")
	if n := strings.Count(caller, "runs-on-label:"); n < 2 {
		t.Errorf("bucketed.yml passes runs-on-label to %d of its two reusable-workflow jobs", n)
	}
}

// workflowInputDefault reads the `default:` of one workflow_call input.
func workflowInputDefault(t *testing.T, wf, name string) string {
	t.Helper()
	at := regexp.MustCompile(`(?m)^      ` + regexp.QuoteMeta(name) + `:\s*$`).FindStringIndex(wf)
	if at == nil {
		t.Fatalf("the workflow declares no %q input", name)
	}
	rest := wf[at[1]:]
	// The next input begins at the same indentation; stop there.
	if next := regexp.MustCompile(`(?m)^      [a-z0-9-]+:\s*$`).FindStringIndex(rest); next != nil {
		rest = rest[:next[0]]
	}
	m := regexp.MustCompile(`(?m)^        default:\s*(.*)$`).FindStringSubmatch(rest)
	if m == nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(m[1]), `"'`)
}

// TestTheBucketBannerNamesItsBasis is F4(d).
//
// The banner printed "estimated Ns" and N means two different quantities: the
// bucket's reporter-work sum under `reporter`, and round1(A_eta/1e9) of the
// fitted model's predicted action interval under `wall`. An operator reading
// the job log could not tell which, and the action received nothing that said.
func TestTheBucketBannerNamesItsBasis(t *testing.T) {
	action := readRepoFile(t, ".github/actions/run-bucket/action.yml")
	banner := regexp.MustCompile(`estimated \$\{BUCKET_EST[^"]*`)
	found := banner.FindAllString(action, -1)
	if len(found) == 0 {
		t.Fatal("run-bucket prints no estimate banner")
	}
	for _, b := range found {
		if !strings.Contains(b, "TB_EST_BASIS") {
			t.Errorf("the banner %q states a number without saying what quantity it is", b)
		}
	}
	if !strings.Contains(action, "TB_EST_BASIS: ${{ env.TB_EST_BASIS }}") {
		t.Error("the banner's basis reaches no step environment, so it can only ever read unset")
	}
	// The caller supplies it. The component map gives run-bucket no
	// `est-basis` input, so it travels through the job environment the way
	// QC12's label reaches the record action.
	wf := readRepoFile(t, ".github/workflows/bucketed-reusable.yml")
	if !strings.Contains(wf, "TB_EST_BASIS: ${{ matrix.est_basis }}") {
		t.Error("the reusable workflow does not pass the matrix entry's est_basis to the bucket step")
	}
}
