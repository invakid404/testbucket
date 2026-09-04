package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// capture runs f with os.Stdout and os.Stderr redirected, and returns what
// each received. It exists because the DIVISION between the two streams is the
// property under test, and nothing below the command can observe it.
func capture(t *testing.T, f func() error) (stdout, stderr string, err error) {
	t.Helper()
	outR, outW, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	errR, errW, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	done := make(chan [2]string, 1)
	go func() {
		o, _ := io.ReadAll(outR)
		e, _ := io.ReadAll(errR)
		done <- [2]string{string(o), string(e)}
	}()
	err = f()
	os.Stdout, os.Stderr = oldOut, oldErr
	_ = outW.Close()
	_ = errW.Close()
	got := <-done
	return got[0], got[1], err
}

// TestTheDelegateTheActionExportsIsExactlyOneDecodableValue is the F1
// regression, and it exercises the REAL command rather than a reconstruction
// of what it prints.
//
// `wall begin` wrote both the workflow masking directive and the key to
// stdout. The action captures stdout in a command substitution, so the runner
// never saw the directive it was meant to process, and the exported value
// became `::add-mask::<key>` followed by a bare line — which the
// environment-file parser rejects and no base64 decoder can read. No test
// exercised the command's actual streams, which is why it shipped.
func TestTheDelegateTheActionExportsIsExactlyOneDecodableValue(t *testing.T) {
	dir, err := os.MkdirTemp("", "tb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	wall := filepath.Join(dir, "wall")
	runKey, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(walltime.RunKeyEnv, walltime.EncodeKey(runKey))

	stdout, stderr, err := capture(t, func() error {
		return runWallBegin([]string{
			"--dir", wall, "--timeout", "8s",
			"--campaign-id", "ewj2", "--run-id", "export-1", "--bucket-id", "bucket-0",
			"--stage1", "sha256:e8bc163c82eee18733288c7d4ac636db3a6deb013ef2d37b68322be20edc45cc", "--stage2", "sha256:ad328846aa18b32a335816374511cac1063c704b8c57999e51da9f908290a7a4",
		})
	})
	if err != nil {
		t.Fatalf("wall begin: %v (stderr %s)", err, stderr)
	}
	t.Cleanup(func() {
		_, _, _ = capture(t, func() error { return runWallEnd([]string{"--dir", wall}) })
	})

	// EXACTLY ONE LINE ON STDOUT, and it is the key.
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 1 || lines[0] == "" {
		t.Fatalf("wall begin wrote %d stdout line(s) %q; the caller captures all of stdout as one value", len(lines), stdout)
	}
	// THE VALUE THE ACTION EXPORTS is what command substitution produces:
	// stdout with trailing newlines stripped.
	exported := strings.TrimRight(stdout, "\n")
	t.Setenv(walltime.SignerDelegateKeyEnv, exported)
	if _, err := walltime.DecodeKey(exported); err != nil {
		t.Errorf("the value run-bucket exports does not decode: %v", err)
	}

	// AND THE MASKING DIRECTIVE IS ON STDERR, where the runner still reads it
	// from the step log and command substitution cannot swallow it.
	if !strings.Contains(stderr, "::add-mask::"+exported) {
		t.Errorf("the masking directive did not reach stderr; it would be captured into the exported value instead of processed: %q", stderr)
	}
	if strings.Contains(stdout, "::add-mask::") {
		t.Error("the masking directive is on stdout, where the caller's command substitution swallows it")
	}
}

// TestAMultiLineDelegateIsRefusedByName: the reader says what is wrong rather
// than reporting a base64 error about a channel defect.
func TestAMultiLineDelegateIsRefusedByName(t *testing.T) {
	key, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	encoded := walltime.EncodeKey(key)
	t.Setenv(walltime.SignerDelegateKeyEnv, "::add-mask::"+encoded+"\n"+encoded)
	if _, err := walltime.LoadDelegateKeyForTest(); err == nil {
		t.Fatal("a two-line delegate value was accepted")
	} else if !strings.Contains(err.Error(), "holds 2 lines") {
		t.Errorf("the refusal does not name the shape: %v", err)
	}
	t.Setenv(walltime.SignerDelegateKeyEnv, encoded)
	if k, err := walltime.LoadDelegateKeyForTest(); err != nil || k == nil {
		t.Errorf("the single-line value the fixed command writes was refused: %v", err)
	}
}

// TestConsumerSetupNeverInheritsTheSignerDelegate is the F2 regression.
//
// The delegate was appended to $GITHUB_ENV, which puts it in the environment
// of every later step in the job — including the consumer-supplied setup
// command. That command runs through `wall run`, whose child was built with a
// nil `Cmd.Env` and therefore inherited everything. Caller-controlled code ran
// holding the capability that decides which keys may attest a run's evidence.
//
// Two independent boundaries now stop it, and this checks both.
func TestConsumerSetupNeverInheritsTheSignerDelegate(t *testing.T) {
	// 1. THE COMMAND. Without --wrapper-chain the child's environment is
	// scrubbed, so even a step that somehow held the capability cannot pass it
	// on to consumer code.
	b, err := os.ReadFile("wall.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, `wrapperChain := fs.Bool("wrapper-chain", false,`) {
		t.Error("wall run has no wrapper-chain distinction, so a consumer command and the wrapper chain get the same environment")
	}
	action, err := os.ReadFile(filepath.Join("..", "..", "internal", "walltime", "action.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(action), "if !o.WrapperChain {\n\t\tcmd.Env = scrubSecrets(nil)\n\t}") {
		t.Error("an action-owned child that is not the wrapper chain still inherits this process's environment")
	}

	// 2. THE WORKFLOW. The capability is a step OUTPUT, named by the one step
	// that needs it, rather than a job-global environment variable.
	yml, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "run-bucket", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(yml)
	for _, line := range strings.Split(text, "\n") {
		code, _, _ := strings.Cut(line, "#")
		if strings.Contains(code, "TB_WALL_SIGNER_DELEGATE_KEY=") && strings.Contains(code, "GITHUB_ENV") {
			t.Errorf("the delegate is exported job-wide: %s", strings.TrimSpace(line))
		}
	}
	if !strings.Contains(text, `printf 'signer-delegate=%s\n' "$delegate" >> "$GITHUB_OUTPUT"`) {
		t.Error("the delegate is not published as a narrowly scoped step output")
	}
	if strings.Count(text, "steps.begin.outputs.signer-delegate") != 1 {
		t.Errorf("%d steps name the delegate output; exactly one — the bucket step — may",
			strings.Count(text, "steps.begin.outputs.signer-delegate"))
	}
	// The setup step runs the consumer's command WITHOUT the wrapper-chain
	// flag, and the bucket step runs the wrapper chain WITH it.
	setup := strings.Index(text, `-- bash -eo pipefail -c "$TB_SETUP_COMMAND"`)
	bucket := strings.Index(text, `--wrapper-chain -- bash -euo pipefail -c '`)
	if setup < 0 || bucket < 0 {
		t.Fatalf("the two wall run call sites are not recognizable: setup=%d bucket=%d", setup, bucket)
	}
	if strings.Contains(text[setup-200:setup], "--wrapper-chain") {
		t.Error("the consumer setup command claims the wrapper chain's capabilities")
	}
}
