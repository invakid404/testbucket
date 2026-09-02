package main

import (
	"strings"
	"testing"
)

// TestTheEligibleGuardTestsTheResolvedVerifierKey is the A1 regression.
//
// The guard already refuses an eligible request whose RUN key does not resolve,
// for a reason it states plainly: fail-later is the one thing a pre-action
// control exists to prevent. The verifier key had no such predicate, and it
// needs one for exactly the same reason and with a worse consequence.
//
// `verify-wall` runs the human `wall verify --require eligible` first and the
// step's exit status comes from it. The JSON invocation that follows
// deliberately emits an UNSIGNED verdict when the key is empty — that is the
// honest behaviour, since the alternative is signing under a name nobody holds
// — and nothing about the step's success reflects it. So a scored arm with an
// unresolved verifier secret consumed the whole measurement, finished green,
// and uploaded a verdict no campaign can count. It is refused only when `wall
// campaign` authenticates it, long after the runner time was spent.
//
// The property is an EQUIVALENCE over the two expressions as the workflow
// actually spells them: the guard says `yes` exactly when the verifier key
// `verify-wall` is handed is nonempty.
func TestTheEligibleGuardTestsTheResolvedVerifierKey(t *testing.T) {
	text := readWorkflow(t)
	guardExpr := soleWorkflowExpression(t, text, "TB_VERIFIER_KEY_SET:")
	deliveredExpr := soleWorkflowExpression(t, text, "verifier-key:")

	const key = "AAAA-the-verifier-key"
	for _, tc := range []struct {
		name     string
		secrets  map[string]string
		selector string
		wantKey  string
	}{
		{name: "the declared verifier-key secret is supplied",
			secrets: map[string]string{"verifier-key": key}, wantKey: key},
		{name: "neither form is supplied",
			secrets: map[string]string{}, wantKey: ""},
		{name: "the selector names a secret this workflow can read",
			secrets: map[string]string{"TB_VERIFIER_KEY": key}, selector: "TB_VERIFIER_KEY", wantKey: key},

		// THE DEFECT. An external caller may pass `verifier-key-secret`
		// freely; nothing makes the name resolve on this side.
		{name: "the selector names a secret that maps to nothing here",
			secrets: map[string]string{}, selector: "TB_VERIFIER_KEY", wantKey: ""},
		{name: "the selector names a secret that exists and is empty",
			secrets: map[string]string{"TB_VERIFIER_KEY": ""}, selector: "TB_VERIFIER_KEY", wantKey: ""},
		{name: "the declared secret carries it despite an unreadable selector",
			secrets: map[string]string{"verifier-key": key}, selector: "TB_VERIFIER_KEY", wantKey: key},
	} {
		t.Run(tc.name, func(t *testing.T) {
			secrets := map[string]string{}
			for k, v := range tc.secrets {
				secrets[k] = v
			}
			ctx := map[string]map[string]string{
				"secrets": secrets,
				"inputs":  {"verifier-key-secret": tc.selector},
			}

			delivered, err := evalActionsExpression(deliveredExpr, ctx)
			if err != nil {
				t.Fatalf("the `verifier-key:` expression given to verify-wall could not be evaluated: %v\n  %s", err, deliveredExpr)
			}
			if delivered != tc.wantKey {
				t.Fatalf("verify-wall would be handed %q, want %q — this test's model of the workflow is wrong, fix it before trusting the guard assertion below", delivered, tc.wantKey)
			}

			got, err := evalActionsExpression(guardExpr, ctx)
			if err != nil {
				t.Fatalf("the TB_VERIFIER_KEY_SET expression could not be evaluated: %v\n  %s", err, guardExpr)
			}
			if got != "yes" && got != "no" {
				t.Fatalf("TB_VERIFIER_KEY_SET evaluated to %q; the guard compares it against `yes` and treats everything else as absent", got)
			}
			want := "no"
			if delivered != "" {
				want = "yes"
			}
			if got != want {
				if want == "no" {
					t.Errorf("TB_VERIFIER_KEY_SET is %q while verify-wall would be handed an EMPTY verifier key. The whole measurement runs, the step finishes green, and the verdict it uploads is unsigned — refused only when the campaign gate authenticates it, long after the time was spent.", got)
					return
				}
				t.Errorf("TB_VERIFIER_KEY_SET is %q while verify-wall would be handed a verifier key; a caller that supplied one correctly is refused", got)
			}
		})
	}
}

// TestTheEligibleGuardRefusesAnUnresolvedVerifierKey: the predicate must
// actually be consumed. An expression nothing reads refuses nothing.
func TestTheEligibleGuardRefusesAnUnresolvedVerifierKey(t *testing.T) {
	var guard string
	for _, step := range workflowJobSteps(t, "test") {
		if strings.Contains(step.body, "verify-require: eligible") {
			guard = step.body
			break
		}
	}
	if guard == "" {
		t.Fatal("the test job has no eligible guard")
	}
	if !strings.Contains(guard, `[ "$TB_VERIFIER_KEY_SET" != "yes" ]`) {
		t.Error("the eligible guard never tests the resolved verifier key, so a scored arm can run the whole measurement and finish green while producing an unsigned verdict")
	}
	if !strings.Contains(guard, "needs a verifier key that RESOLVES here") {
		t.Error("the guard does not say why an unresolved verifier key refuses the request")
	}
}
