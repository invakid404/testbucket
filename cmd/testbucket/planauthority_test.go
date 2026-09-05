package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// TestTheExactAuthorityPrecedesPlanning is the F1 regression.
//
// The contract requires the protected environment's approval BEFORE either
// role plans. The exact-label guard added last round lives in the bucket job,
// which runs after the plan job has already derived the matrix and the Stage-2
// material — so the frozen planner could run under a manifest approved by some
// other environment and a downstream refusal could not un-derive its output.
//
// This asserts the ordering the contract states: the guard comes first, in the
// job that plans.
func TestTheExactAuthorityPrecedesPlanning(t *testing.T) {
	steps := workflowJobSteps(t, "plan")
	guard, plan := -1, -1
	for i, step := range steps {
		switch step.name {
		case "Refuse a frozen plan without the exact protected authority":
			guard = i
		case "Plan the buckets":
			plan = i
		}
	}
	if plan < 0 {
		t.Fatal("the plan job has no planning step")
	}
	if guard < 0 {
		t.Fatal("the plan job has no exact-authority guard, so the frozen planner can run under a manifest approved by another protected environment")
	}
	if guard > plan {
		t.Errorf("the authority guard is step %d and planning is step %d; approval must precede planning, not follow it", guard, plan)
	}
	if !strings.Contains(steps[guard].body, walltime.CampaignAuthority) {
		t.Errorf("the plan-job guard does not require the exact protected authority %q", walltime.CampaignAuthority)
	}
}

// TestEligiblePlanningRequiresTheFrozenInputsBeforeItPlans is the F1
// regression, and it is about the INPUTS rather than the authority.
//
// An authority cannot approve inputs that do not exist. The guard checked the
// exact label and the key, but not `frozen-inputs-artifact` — so an eligible
// request that omitted it passed, the download step was skipped, `Plan the
// buckets` received an empty bundle and stage1, and the ORDINARY live planner
// derived a matrix from the working tree and the restored store. The matrix job
// refuses afterwards, but that only stops AT_start; it cannot make the plan
// that already happened a frozen one.
func TestEligiblePlanningRequiresTheFrozenInputsBeforeItPlans(t *testing.T) {
	steps := workflowJobSteps(t, "plan")
	guard, plan := -1, -1
	for i, step := range steps {
		switch step.name {
		case "Refuse a frozen plan without the exact protected authority":
			guard = i
		case "Plan the buckets":
			plan = i
		}
	}
	if guard < 0 || plan < 0 || guard >= plan {
		t.Fatalf("could not establish a pre-plan guard: guard=%d plan=%d", guard, plan)
	}
	body := steps[guard].body
	// The artifact must be READ by the guard...
	if !strings.Contains(body, "TB_FROZEN_INPUTS: ${{ inputs.frozen-inputs-artifact }}") {
		t.Error("the pre-plan guard never reads frozen-inputs-artifact, so an eligible request can reach the live planner without one")
	}
	// ...and its absence must be a refusal, not a note.
	if !strings.Contains(body, `if [ -z "$TB_FROZEN_INPUTS" ]`) {
		t.Error("the pre-plan guard does not refuse an empty frozen-inputs-artifact")
	}
	// The download the plan depends on is conditional on the same input, which
	// is why its absence silently selects the live planner rather than failing.
	for i, step := range steps {
		if step.name != "Download the frozen planning inputs" {
			continue
		}
		if i > guard {
			t.Errorf("the frozen-input download is step %d and the guard is step %d; the guard must precede the plan, and the download it gates on must not follow it", i, guard)
		}
	}
}

// TestTheScoredExampleSuppliesTheFrozenInputs: the documented scored
// invocation is the one most likely to be copied, and it named only the
// documents artifact — describing a fail-closed path that in fact planned live
// first.
func TestTheScoredExampleSuppliesTheFrozenInputs(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	at := strings.Index(text, "verify-require: eligible")
	if at < 0 {
		t.Fatal("the README has no scored example")
	}
	// The example block runs to the end of its fenced snippet.
	end := strings.Index(text[at:], "```")
	if end < 0 {
		t.Fatal("the scored example is not a fenced block")
	}
	example := text[at : at+end]
	for _, want := range []string{"frozen-inputs-artifact:", "frozen-documents-artifact:"} {
		if !strings.Contains(example, want) {
			t.Errorf("the documented scored example omits %s; a reader copying it would plan live before being refused", want)
		}
	}
}

// TestAnEmptyExpectedAuthorityIsNotAWildcard is the other half of F1.
//
// `RequireApproval` compared the label only when the caller supplied one, so
// the single call that mattered — the frozen planner's — could omit it and
// every label passed. A caller that cannot say which protected environment
// must have approved is not in a position to accept the approval.
func TestAnEmptyExpectedAuthorityIsNotAWildcard(t *testing.T) {
	key, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	m := walltime.Stage1Manifest{Kind: walltime.Stage1Kind}
	if err := m.Sign("some-other-environment", key); err != nil {
		t.Fatal(err)
	}
	keys := []string{walltime.PublicKeyOf(key)}

	for _, tc := range []struct {
		name, authority, want string
	}{
		{"an empty expected authority", "", "no expected authority was named"},
		{"a blank expected authority", "   ", "no expected authority was named"},
		{"the wrong protected environment", walltime.CampaignAuthority, "not the required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := m.RequireApproval(keys, tc.authority)
			if err == nil {
				t.Fatalf("RequireApproval accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
	// And the genuine label is accepted, so the check refuses the wrong
	// approval rather than every approval.
	if err := m.RequireApproval(keys, "some-other-environment"); err != nil {
		t.Errorf("the genuine approval was refused: %v", err)
	}
}

// TestTheFrozenPlannerRequiresTheAuthorityLabel: the CLI is the last place the
// omission could survive, and `plan --wall-bundle` now refuses without it.
func TestTheFrozenPlannerRequiresTheAuthorityLabel(t *testing.T) {
	err := planFromBundle(frozenPlanOptions{
		bundlePath:    "bundle.json",
		stage1Path:    "stage1.json",
		authorityKeys: []string{"aa"},
		authority:     "",
	})
	if err == nil {
		t.Fatal("the frozen planner accepted a bundle with no expected authority")
	}
	if !strings.Contains(err.Error(), "--wall-authority is required") {
		t.Errorf("error %q does not require the authority label", err)
	}
}
