package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The reusable workflow refuses an ELIGIBLE request it cannot pre-flight
// before anything else in the bucket job runs. One of its preconditions is a
// run key, because `wall begin` signs the signer roster with it and `wall end`
// signs the closing seal, and both are WT-023 ineligible when unsigned.
//
// There are two ways a caller supplies that key: the declared `run-key` secret
// (the only form available to a DIRECT EXTERNAL caller), or `run-key-secret`
// naming a secret this workflow can read (the `secrets: inherit` path). The
// second is a SELECTOR, not a value — an external caller can name a secret
// that maps to nothing here, and then the name is nonempty while the value the
// `run-bucket` action would receive is empty.
//
// A guard that tested the NAME therefore passed a request that had no key:
// pre-flight ran, the action envelope opened, the tests executed, and only the
// verifier afterwards refused the row. That is fail-later, and the whole point
// of this step is to be the pre-action control. So the property under test is
// an EQUIVALENCE, asserted over the two expressions as the file actually spells
// them: the guard says `yes` exactly when the run key `run-bucket` is handed is
// nonempty.
func TestTheEligibleGuardTestsTheRunKeyValueAndNotItsName(t *testing.T) {
	text := readWorkflow(t)
	guardExpr := soleWorkflowExpression(t, text, "TB_RUN_KEY_SET:")
	deliveredExpr := soleWorkflowExpression(t, text, "run-key:")

	const key = "AAAA-the-run-key"
	cases := []struct {
		name string
		// `secrets` as this workflow can actually read it: the declared
		// `run-key` lives under that name, an inherited one under its own.
		secrets  map[string]string
		selector string // the `run-key-secret` input
		wantKey  string // what `run-bucket` is handed
	}{
		{
			name:    "the declared run-key secret is supplied",
			secrets: map[string]string{"run-key": key},
			wantKey: key,
		},
		{
			name:    "neither form is supplied",
			secrets: map[string]string{},
			wantKey: "",
		},
		{
			name:     "the selector names a secret this workflow can read",
			secrets:  map[string]string{"TB_RUN_KEY": key},
			selector: "TB_RUN_KEY",
			wantKey:  key,
		},
		{
			// The regression. An external caller may pass `run-key-secret`
			// freely; nothing makes the name resolve on this side.
			name:     "the selector names a secret that maps to nothing here",
			secrets:  map[string]string{},
			selector: "TB_RUN_KEY",
			wantKey:  "",
		},
		{
			name:     "the selector names a secret that exists and is empty",
			secrets:  map[string]string{"TB_RUN_KEY": ""},
			selector: "TB_RUN_KEY",
			wantKey:  "",
		},
		{
			name:     "the declared secret carries the run despite an unreadable selector",
			secrets:  map[string]string{"run-key": key},
			selector: "TB_RUN_KEY",
			wantKey:  key,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			secrets := map[string]string{}
			for k, v := range tc.secrets {
				secrets[k] = v
			}
			ctx := map[string]map[string]string{
				"secrets": secrets,
				"inputs":  {"run-key-secret": tc.selector},
			}

			delivered, err := evalActionsExpression(deliveredExpr, ctx)
			if err != nil {
				t.Fatalf("the `run-key:` expression given to run-bucket could not be evaluated: %v\n  %s", err, deliveredExpr)
			}
			if delivered != tc.wantKey {
				t.Fatalf("run-bucket would be handed %q, want %q — this test's model of the workflow is wrong, fix it before trusting the guard assertion below", delivered, tc.wantKey)
			}

			got, err := evalActionsExpression(guardExpr, ctx)
			if err != nil {
				t.Fatalf("the TB_RUN_KEY_SET expression could not be evaluated: %v\n  %s", err, guardExpr)
			}
			if got != "yes" && got != "no" {
				t.Fatalf("TB_RUN_KEY_SET evaluated to %q; the guard compares it against `yes` and treats everything else as absent", got)
			}
			want := "no"
			if delivered != "" {
				want = "yes"
			}
			if got != want {
				if want == "no" {
					t.Errorf("TB_RUN_KEY_SET is %q while run-bucket would be handed an EMPTY run key. The request passes the pre-flight guard, opens the action envelope and runs the tests, and is only refused WT-023 afterwards — the fail-later this step exists to prevent. Derive the guard from the same expression run-bucket is given.", got)
					return
				}
				t.Errorf("TB_RUN_KEY_SET is %q while run-bucket would be handed a run key; a caller that supplied one correctly is refused", got)
			}
		})
	}
}

// A refusal is only a pre-action control if the refused job cannot proceed past
// it. The guard must be the FIRST step of the bucket job, and no step from
// there through the measured bucket may run on `always()` — one that did would
// execute over exactly the request the guard just refused.
func TestARefusedEligibleRequestNeverReachesTheEnvelope(t *testing.T) {
	steps := workflowJobSteps(t, "test")

	const guard = "Refuse an eligible request that cannot be pre-flighted"
	if len(steps) == 0 || steps[0].name != guard {
		got := "no steps at all"
		if len(steps) > 0 {
			got = fmt.Sprintf("%q", steps[0].name)
		}
		t.Fatalf("the first step of the bucket job is %s, not the %q guard; anything placed before it runs for a request the guard is about to refuse", got, guard)
	}

	last := 0
	for _, want := range []string{"Pre-flight the frozen plan", "Install testbucket for the measured action", "Run bucket "} {
		at := -1
		for i, s := range steps {
			if strings.HasPrefix(s.name, want) {
				at = i
				break
			}
		}
		if at < 0 {
			t.Fatalf("the bucket job has no %q step; this test cannot prove the guard precedes it", want)
		}
		if at <= 0 {
			t.Errorf("step %q does not follow the guard", want)
		}
		if at > last {
			last = at
		}
	}

	for _, s := range steps[1 : last+1] {
		if strings.Contains(s.body, "always()") {
			t.Errorf("step %q runs on always(), so it would still execute after the guard refused the request. Nothing from the guard through the measured bucket may.", s.name)
		}
	}
}

func readWorkflow(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "bucketed-reusable.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// soleWorkflowExpression returns the single `${{ … }}` body on the one line
// whose key is prefix. Two matching lines is an error rather than a choice: the
// test would otherwise silently assert about whichever came first.
func soleWorkflowExpression(t *testing.T, text, prefix string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), prefix) || !strings.Contains(line, "${{") {
			continue
		}
		open := strings.Index(line, "${{")
		close := strings.LastIndex(line, "}}")
		if close < open {
			t.Fatalf("the %s expression is not closed on its own line: %s", prefix, line)
		}
		found = append(found, strings.TrimSpace(line[open+3:close]))
	}
	switch len(found) {
	case 1:
		return found[0]
	case 0:
		t.Fatalf("the workflow has no %s expression; this test asserts about a line that no longer exists", prefix)
	}
	t.Fatalf("the workflow has %d %s expressions; this test cannot tell which one is the guard's counterpart", len(found), prefix)
	return ""
}

type wfStep struct{ name, body string }

// workflowJobSteps returns one job's steps in file order. Like the composite
// action scan in runkey_test.go this reads the YAML as text: the repository has
// no external dependencies, and a hand-rolled scan of the exact property under
// test is better than adding a YAML parser to assert one thing.
func workflowJobSteps(t *testing.T, job string) []wfStep {
	t.Helper()
	lines := strings.Split(readWorkflow(t), "\n")

	start := -1
	for i, l := range lines {
		if l == "  "+job+":" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("the workflow has no job %q", job)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(lines[i], "  ") && !strings.HasPrefix(lines[i], "   ") && strings.HasSuffix(trimmed, ":") {
			end = i
			break
		}
	}

	const marker = "      - "
	var steps []wfStep
	for i := start; i < end; i++ {
		if !strings.HasPrefix(lines[i], marker) {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(lines[i], marker))
		if rest, ok := strings.CutPrefix(name, "name: "); ok {
			name = strings.TrimSpace(rest)
		}
		j := i + 1
		for ; j < end; j++ {
			if strings.HasPrefix(lines[j], marker) {
				break
			}
		}
		steps = append(steps, wfStep{name: name, body: strings.Join(lines[i:j], "\n")})
	}
	if len(steps) == 0 {
		t.Fatalf("job %q has no steps at the expected indentation", job)
	}
	return steps
}
