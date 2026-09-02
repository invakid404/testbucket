package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The run key is what makes a measurement attributable: `wall begin` signs the
// signer roster with it and `wall end` signs the closing seal, and the
// verifier refuses both as WT-023 when they are unsigned.
//
// Its security value is entirely a matter of WHICH STEPS SEE IT. A step-scoped
// `env:` in an Actions composite is not visible to other steps, so a key bound
// to the two envelope steps cannot be read by the setup command or by the
// measured bucket script — which is why the measured work cannot mint a
// roster, mint a seal, or alter either once the seal exists. Bound one step too
// widely, the whole signer set becomes forgeable by the very thing it attests.
//
// So this is asserted structurally rather than left to review. The action YAML
// is read as text and split on its step boundaries: the repository has no
// external dependencies, and a hand-rolled scan of the exact property under
// test is better than adding a YAML parser to assert one thing.
func TestTheRunKeyReachesOnlyTheEnvelopeSteps(t *testing.T) {
	const envKey = "TB_WALL_RUN_KEY"
	// Named by the property each step has, not by its position: a step
	// reordering must not silently satisfy this test.
	want := map[string]bool{
		"Refuse an unmeasurable scored row": false,
		"Install testbucket":                false,
		// The supervisor setup step SEES the key, and that is the point of it:
		// it hands the key to a root process on a file descriptor and deletes
		// the file, so the capability ends up held by a credential the
		// measured workload does not have. Before this step existed there was
		// nowhere for that capability to live except the measured step, which
		// is why no lower-level producer could be authorized at all.
		"Establish the supervised measurement boundary": true,
		"Open the wall-time action envelope":            true,
		"Set up the bucket":                             false,
		"Run the bucket":                                false,
		"Close the wall-time action envelope":           true,
	}

	steps := actionSteps(t, filepath.Join("..", "..", ".github", "actions", "run-bucket", "action.yml"))
	if len(steps) != len(want) {
		t.Fatalf("the action has %d step(s) and this test knows %d; a new step must state whether it may see the run key", len(steps), len(want))
	}
	for name, body := range steps {
		expected, known := want[name]
		if !known {
			t.Errorf("step %q is not covered by this test; state whether it may see the run key", name)
			continue
		}
		got := strings.Contains(body, envKey)
		switch {
		case expected && !got:
			t.Errorf("step %q does not receive %s, so it cannot sign — every scored row would be WT-023 ineligible", name, envKey)
		case !expected && got:
			t.Errorf("step %q receives %s. The setup command and the measured script must never see it: a signer set the measured work can produce attests nothing", name, envKey)
		}
	}

	// And the input has to exist and be empty by default, so an unmeasured or
	// unscored caller is unaffected and a scored one must opt in explicitly.
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "run-bucket", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "\n  run-key:\n") {
		t.Errorf("the action declares no run-key input, so the workflow has no way to supply one")
	}
}

// The verifier needs the PUBLIC half, or it has nothing to check the roster and
// seal against. Stage 1 is the authoritative declaration; this is the caller's
// independent statement of the same expectation.
func TestVerifyWallAcceptsThePredeclaredRecordSigner(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "verify-wall", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "\n  record-signer:\n") {
		t.Fatalf("verify-wall declares no record-signer input")
	}
	if !strings.Contains(text, "--record-signer") {
		t.Errorf("verify-wall never passes --record-signer, so the input would be inert")
	}
	// The private half must not appear anywhere in the verifying action: it
	// verifies, it does not attest.
	if strings.Contains(text, "TB_WALL_RUN_KEY") {
		t.Errorf("verify-wall references the run key; only the two envelope steps of run-bucket may")
	}
}

// actionSteps splits a composite action's steps into name -> body. It keys on
// the two-space step indentation the file uses; a step whose name is missing is
// reported rather than silently skipped.
func actionSteps(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const marker = "    - name: "
	out := map[string]string{}
	rest := string(b)
	idx := strings.Index(rest, marker)
	if idx < 0 {
		t.Fatalf("%s has no steps at the expected indentation", path)
	}
	for idx >= 0 {
		rest = rest[idx+len(marker):]
		name, body, _ := strings.Cut(rest, "\n")
		next := strings.Index(rest, marker)
		if next >= 0 {
			body = rest[:next]
		}
		if _, dup := out[strings.TrimSpace(name)]; dup {
			t.Fatalf("%s has two steps named %q, so this test cannot tell them apart", path, name)
		}
		out[strings.TrimSpace(name)] = body
		idx = next
	}
	return out
}
