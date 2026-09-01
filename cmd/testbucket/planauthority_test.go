package main

import (
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
