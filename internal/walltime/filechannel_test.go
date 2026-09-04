package walltime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// AN OBSERVER MUST NOT HOLD THE CHANNEL THE DELEGATE ARRIVES ON.
//
// `wall begin` mints the signer delegate, returns it on stdout, and the
// composite step then appends it to $GITHUB_OUTPUT so exactly the measured
// step can name it. The two action observers are started BEFORE that append
// and are deliberately detached, so they outlive the step — and they inherited
// $GITHUB_OUTPUT.
//
// Scrubbing the delegate VALUE while leaving an observer holding the path of
// the file it is about to be written to secures nothing: the observer keeps
// same-uid read access to the exact channel for the whole measured window, and
// an observer that can obtain the delegate can authorize a lower signer and
// vouch for itself — the one thing the delegation's scope exists to prevent.
//
// This drives the production launch path, not scrubSecrets in isolation: the
// question is what the detached observers are actually started with.
func TestDetachedObserversDoNotInheritTheGitHubFileChannels(t *testing.T) {
	for _, name := range WallTimeSecretEnv {
		t.Setenv(name, "secret-for-"+name)
	}
	t.Setenv(RunKeyEnv, EncodeKey(mustSigningKey()))

	// The channels as a runner really sets them: real paths, which is what
	// makes them readable rather than merely named.
	channels := t.TempDir()
	for _, name := range GitHubFileCommandEnv {
		p := filepath.Join(channels, strings.ToLower(name))
		if err := os.WriteFile(p, []byte("pre-existing\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv(name, p)
	}
	// Ambient values an observer legitimately needs must survive: this is a
	// denylist, and a test that only checked removals would pass on one that
	// removed everything.
	t.Setenv("TB_WALLTIME_AMBIENT_MUST_SURVIVE", "yes")

	original := ObserverLauncher
	var launched []*exec.Cmd
	ObserverLauncher = func(args []string) (*exec.Cmd, error) {
		cmd, err := original(args)
		if err == nil {
			launched = append(launched, cmd)
		}
		return cmd, err
	}
	t.Cleanup(func() { ObserverLauncher = original })

	dir := t.TempDir()
	run := RunIdentity{
		CampaignID: "ewj2", RunID: "file-channels", BucketID: "bucket-0",
		Stage2: "sha256:5e585fd3fab5cb85a941179b4df835cef988f0281af9f47878024f539c302df5",
	}
	if _, err := BeginAction(dir, run, 20*time.Second); err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	if len(launched) != 2 {
		t.Fatalf("BeginAction launched %d observer(s), want the action peer and trace", len(launched))
	}
	for i, cmd := range launched {
		if cmd.SysProcAttr == nil {
			t.Errorf("action observer %d is not on the detached production path, which is what makes an inherited channel durable", i)
		}
		if cmd.Env == nil {
			t.Fatalf("action observer %d has a nil Env and would inherit every channel", i)
		}
		got := map[string]string{}
		for _, kv := range cmd.Env {
			k, v, _ := strings.Cut(kv, "=")
			got[k] = v
		}
		for _, name := range GitHubFileCommandEnv {
			if v, present := got[name]; present {
				t.Errorf("action observer %d inherits %s=%q; it outlives the step and can read the file the signer delegate is appended to", i, name, v)
			}
		}
		for _, name := range WallTimeSecretEnv {
			if v, present := got[name]; present {
				t.Errorf("action observer %d inherits the capability %s=%q", i, name, v)
			}
		}
		if got["TB_WALLTIME_AMBIENT_MUST_SURVIVE"] != "yes" {
			t.Errorf("action observer %d lost an ambient variable it legitimately needs; the scrub is a denylist, not an allowlist", i)
		}
	}
}

// AND THE MEASURED WORK DOES NOT HOLD THEM EITHER.
//
// `scrubSecrets` is what every non-wrapper-chain child is launched with: the
// measured child of an envelope, and the consumer-supplied setup command that
// runs through `wall run`. Neither may write the channel that carries the
// delegate to the measured step, or read the one it arrives on.
func TestTheScrubRemovesEveryCapabilityAndEveryFileChannel(t *testing.T) {
	var env []string
	for _, name := range append(append([]string{}, WallTimeSecretEnv...), GitHubFileCommandEnv...) {
		env = append(env, name+"=/should/not/travel")
	}
	env = append(env, "PATH=/usr/bin", "HOME=/home/runner", "TB_WALL_CGROUP_ROOT=/sys/fs/cgroup/x")

	got := map[string]string{}
	for _, kv := range scrubSecrets(env) {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	for _, name := range append(append([]string{}, WallTimeSecretEnv...), GitHubFileCommandEnv...) {
		if _, present := got[name]; present {
			t.Errorf("scrubSecrets kept %s", name)
		}
	}
	for _, name := range []string{"PATH", "HOME", "TB_WALL_CGROUP_ROOT"} {
		if _, present := got[name]; !present {
			t.Errorf("scrubSecrets removed %s, which every child legitimately needs", name)
		}
	}

	// The channels are named exactly, not by prefix: scrubbing every
	// GITHUB_-prefixed variable would take GITHUB_REPOSITORY, GITHUB_RUN_ID
	// and the rest of the identity the records bind.
	keep := []string{"GITHUB_REPOSITORY", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_JOB", "GITHUB_WORKSPACE"}
	var ident []string
	for _, k := range keep {
		ident = append(ident, k+"=value")
	}
	kept := map[string]bool{}
	for _, kv := range scrubSecrets(ident) {
		k, _, _ := strings.Cut(kv, "=")
		kept[k] = true
	}
	for _, k := range keep {
		if !kept[k] {
			t.Errorf("scrubSecrets removed %s, which is run identity rather than a writable channel", k)
		}
	}
}

// AND THE HANDOFF IS UNCHANGED. The composite step appends the delegate in its
// OWN shell, which is not a scrubbed child, so removing the channel from the
// observers does not remove it from the step that legitimately writes it.
func TestTheDelegateHandoffStillUsesTheStepOutput(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "run-bucket", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yml := string(b)
	if !strings.Contains(yml, `printf 'signer-delegate=%s\n' "$delegate" >> "$GITHUB_OUTPUT"`) {
		t.Error("the opening step no longer hands the delegate over as a step output")
	}
	if strings.Contains(yml, "TB_WALL_SIGNER_DELEGATE_KEY=") && strings.Contains(yml, "GITHUB_ENV") {
		t.Error("the delegate is being written to GITHUB_ENV, which puts it in every later step of the job")
	}
	// And the consumer is told what a measured setup command no longer has.
	if !strings.Contains(yml, "GITHUB_STEP_SUMMARY") {
		t.Error("the setup-command contract does not say the GitHub file channels are stripped under measurement")
	}
}
