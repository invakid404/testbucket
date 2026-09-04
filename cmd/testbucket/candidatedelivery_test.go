package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// publicActions are the six composite actions that put a testbucket binary on
// a runner. Every one of them is a supported delivery route, so every one of
// them has to be able to deliver a CANDIDATE.
var publicActions = []string{"plan", "preflight", "install", "run-bucket", "verify-wall", "record"}

// installStepEnv returns the environment the named action's installer step
// actually exports, with `${{ inputs.… }}` resolved against what a caller
// passes in `with:`.
//
// It is read out of the shipped YAML rather than restated here, which is the
// point of the test: a value the action does not thread through is absent from
// this map and the installer then runs without it, exactly as it would on a
// runner.
func installStepEnv(t *testing.T, action string, with map[string]string, runnerTemp string) map[string]string {
	t.Helper()
	path := filepath.Join("..", "..", ".github", "actions", action, "action.yml")
	var body string
	for name, b := range actionSteps(t, path) {
		if strings.Contains(b, "install-testbucket.sh") {
			if body != "" {
				t.Fatalf("%s runs the shared installer from more than one step; this test cannot tell which delivers", path)
			}
			body = b
			_ = name
		}
	}
	if body == "" {
		t.Fatalf("%s never runs the shared installer, so it cannot be a delivery route", path)
	}
	env := map[string]string{}
	inEnv := false
	line := regexp.MustCompile(`^        ([A-Za-z_][A-Za-z0-9_]*): (.*)$`)
	inputRef := regexp.MustCompile(`\$\{\{ inputs\.([a-z0-9-]+) \}\}`)
	for _, l := range strings.Split(body, "\n") {
		if strings.TrimSpace(l) == "env:" {
			inEnv = true
			continue
		}
		if !inEnv {
			continue
		}
		m := line.FindStringSubmatch(l)
		if m == nil {
			break
		}
		v := inputRef.ReplaceAllStringFunc(m[2], func(ref string) string {
			return with[inputRef.FindStringSubmatch(ref)[1]]
		})
		v = strings.ReplaceAll(v, "${{ runner.temp }}", runnerTemp)
		v = strings.ReplaceAll(v, "${{ github.token }}", "")
		env[m[1]] = v
	}
	return env
}

// runInstallerWithEnv drives the REAL installer with exactly the environment an
// action's step exports, and a stubbed `gh` that hands over a staged artifact.
func runInstallerWithEnv(t *testing.T, root, artifact string, env map[string]string) (string, string, error) {
	t.Helper()
	fakeBin := filepath.Join(root, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	gh := "#!/bin/sh\nout=\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"--dir\" ]; then shift; out=\"$1\"; fi\n  shift\ndone\n/bin/mkdir -p \"$out\"\n/bin/cp -R \"$TB_TEST_ARTIFACT/.\" \"$out/\"\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join("..", "..", ".github", "actions", "install-testbucket.sh"))
	cmd.Env = append(os.Environ(),
		"PATH="+fakeBin+":"+os.Getenv("PATH"),
		"TB_TEST_ARTIFACT="+artifact,
		"GITHUB_PATH="+filepath.Join(root, "github-path"),
	)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	return filepath.Join(env["TB_BINDIR"], "testbucket"), string(out), err
}

// THE MANDATORY CHECK HAS TO BE REACHABLE THROUGH THE SUPPORTED ROUTE.
//
// The installer refuses a `candidate:` pin unless TB_CANDIDATE_BINARY_DIGEST
// names the binary Stage 1 authorised. That rule was correct and unusable: the
// six composite actions that run the installer exposed no input carrying it
// and set no such environment variable, so the advertised delivery path could
// not supply the value its own installer demands. Every candidate install
// through a public action failed, and the only way to get one through was an
// undocumented ambient variable that a reusable-workflow caller cannot even
// set.
//
// This drives the SHIPPED YAML of each action the way a caller does — resolving
// its installer step's env from the action's declared inputs — and then runs
// the real installer with it. An action that stops threading the value fails
// here rather than on a runner.
func TestEveryPublicActionDeliversTheAttestedCandidateBinary(t *testing.T) {
	const good = "#!/bin/sh\nprintf 'PINNED\\n'\n"
	binDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(good)))

	for _, action := range publicActions {
		t.Run(action+" installs the candidate its caller attested", func(t *testing.T) {
			root := t.TempDir()
			artifact, digest := candidateArtifact(t, root, map[string]string{"testbucket": good}, nil, 1)
			env := installStepEnv(t, action, map[string]string{
				"version":                 candidateVersion(t, digest),
				"candidate-binary-digest": binDigest,
			}, root)
			if got := env["TB_CANDIDATE_BINARY_DIGEST"]; got != binDigest {
				t.Fatalf("the %s action exports TB_CANDIDATE_BINARY_DIGEST=%q for a caller that attested %s; the installer's mandatory check has no supported way to be satisfied", action, got, binDigest)
			}
			bin, out, err := runInstallerWithEnv(t, root, artifact, env)
			if err != nil {
				t.Fatalf("the %s action could not deliver an attested candidate: %v\n%s", action, err, out)
			}
			ran, err := exec.Command(bin).Output()
			if err != nil {
				t.Fatalf("the delivered binary does not run: %v", err)
			}
			if strings.TrimSpace(string(ran)) != "PINNED" {
				t.Errorf("the %s action installed %q, not the pinned archive's member", action, strings.TrimSpace(string(ran)))
			}
		})

		// AND THE ROUTE STAYS FAIL-CLOSED. Threading the input through must not
		// have turned the check into one a caller can decline.
		t.Run(action+" still refuses an unattested candidate", func(t *testing.T) {
			root := t.TempDir()
			artifact, digest := candidateArtifact(t, root, map[string]string{"testbucket": good}, nil, 1)
			env := installStepEnv(t, action, map[string]string{
				"version":                 candidateVersion(t, digest),
				"candidate-binary-digest": "",
			}, root)
			_, out, err := runInstallerWithEnv(t, root, artifact, env)
			if err == nil {
				t.Fatalf("the %s action installed a candidate nobody attested:\n%s", action, out)
			}
			if !strings.Contains(out, "TB_CANDIDATE_BINARY_DIGEST") {
				t.Errorf("the refusal does not name the missing attested digest:\n%s", out)
			}
		})

		// A digest supplied beside a PUBLISHED version is a wiring mistake: the
		// candidate path is the only one that checks it, so accepting it
		// silently would leave a caller believing in a check nothing runs.
		t.Run(action+" refuses an attested digest for a released version", func(t *testing.T) {
			root := t.TempDir()
			artifact, _ := candidateArtifact(t, root, map[string]string{"testbucket": good}, nil, 1)
			env := installStepEnv(t, action, map[string]string{
				"version":                 "v0.2.2",
				"candidate-binary-digest": binDigest,
			}, root)
			_, out, err := runInstallerWithEnv(t, root, artifact, env)
			if err == nil {
				t.Fatalf("the %s action accepted a candidate binary digest for a released version:\n%s", action, out)
			}
			if !strings.Contains(out, "not a candidate pin") {
				t.Errorf("the refusal does not explain the mismatch:\n%s", out)
			}
		})
	}
}

// AND THE REUSABLE WORKFLOW IS A DELIVERY CONTRACT TOO.
//
// A direct composite caller can set an ambient environment variable; a caller
// of a reusable workflow cannot. Its `with:` inputs are the entire supported
// contract, so a value the workflow does not declare and thread is a value the
// advertised scored route cannot provide at all.
//
// The workflow does not take the digest on trust either: the plan job derives
// it from the signed Stage-1 manifest with a PUBLISHED testbucket and refuses a
// caller-supplied value that disagrees, so what every downstream job installs
// is the binary the campaign authority approved.
func TestTheReusableWorkflowDeliversTheAttestedCandidateBinaryDigest(t *testing.T) {
	path := filepath.Join("..", "..", ".github", "workflows", "bucketed-reusable.yml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)

	for _, want := range []struct{ what, text string }{
		{"a typed candidate-binary-digest input", "\n      candidate-binary-digest:\n"},
		{"a typed candidate-resolver-version input", "\n      candidate-resolver-version:\n"},
		{"a plan-job output carrying the derived digest", "\n      candidate-binary-digest: ${{ steps.candidate-binary.outputs.digest }}\n"},
		{"a derivation step", "- name: Resolve the attested candidate binary digest"},
		{"the derivation reading the signed Stage-1 manifest", "wall stage1-binary"},
		{"the derivation refusing a disagreeing caller value", "but Stage 1 authorises"},
		{"a PUBLISHED resolver, not the candidate itself", "version: ${{ inputs.candidate-resolver-version }}"},
		{"the resolver refusing to be a candidate", "a candidate cannot authenticate its own approval"},
	} {
		if !strings.Contains(body, want.text) {
			t.Errorf("the reusable workflow has no %s (looked for %q)", want.what, want.text)
		}
	}

	// EVERY call that installs must carry the value. Finding the calls by the
	// version they pass means a new job cannot be added without answering this.
	const versionLine = "          version: ${{ inputs.testbucket-version }}\n"
	const digestLine = "          candidate-binary-digest: "
	calls := strings.Count(body, versionLine)
	if calls == 0 {
		t.Fatal("no job in the reusable workflow installs testbucket; this test is looking at the wrong contract")
	}
	for i, part := range strings.Split(body, versionLine)[1:] {
		next, _, _ := strings.Cut(part, "\n")
		if !strings.HasPrefix(versionLine+next+"\n", versionLine+digestLine) {
			t.Errorf("action call %d installs `testbucket-version` without threading candidate-binary-digest; a candidate pin through that call cannot satisfy the installer", i+1)
		}
	}
	if got := strings.Count(body, digestLine+"${{ needs.plan.outputs.candidate-binary-digest }}\n"); got != calls-1 {
		t.Errorf("%d of %d downstream calls consume the plan job's derived digest; a job that resolves its own could install different bytes from the rest of the run", got, calls-1)
	}
}

// WHAT THE DIGEST IS DERIVED FROM.
//
// `wall stage1-binary` is the mechanical derivation the delivery route needs:
// it prints the binary a SIGNED Stage-1 manifest authorises, and refuses
// everything short of that. Typing a digest into the workflow would name the
// bytes the caller wanted; this names the bytes the campaign authority
// approved, having verified the builder's attestation and the verifier's
// countersignature on the way.
func TestWallStage1BinaryPrintsOnlyAnAuthorisedBinary(t *testing.T) {
	args := stage1Inputs(t, "--campaign-schedule", writeSchedule(t))
	if err := runWallStage1(args); err != nil {
		t.Fatalf("could not author the manifest under test: %v", err)
	}
	m := readManifest(t, args)
	var out string
	for i, a := range args {
		if a == "--out" && i+1 < len(args) {
			out = args[i+1]
		}
	}
	key, err := walltime.DecodeKey(os.Getenv(walltime.AuthorityKeyEnv))
	if err != nil {
		t.Fatal(err)
	}
	pub := walltime.PublicKeyOf(key)

	stdout, _, err := capture(t, func() error {
		return runWallStage1Binary([]string{"--file", out, "--authority", m.Signature.Authority, "--authority-key", pub})
	})
	if err != nil {
		t.Fatalf("the authorised binary could not be derived: %v", err)
	}
	if got := strings.TrimSpace(stdout); got != string(m.Source.BinaryDigest) {
		t.Fatalf("derived %q, but the manifest authorises %s", got, m.Source.BinaryDigest)
	}

	// AND THE REFUSALS. Each removes exactly one thing that makes the printed
	// value evidence rather than a reading.
	for _, tc := range []struct {
		name, want string
		args       []string
	}{
		{
			name: "no predeclared authority key",
			args: []string{"--file", out, "--authority", m.Signature.Authority},
			want: "checked against the key it carries",
		},
		{
			name: "a key nobody predeclared",
			args: []string{"--file", out, "--authority", m.Signature.Authority, "--authority-key", walltime.PublicKeyOf(mustKey(t))},
			want: "authority signature",
		},
		{
			name: "an authority label the manifest does not name",
			args: []string{"--file", out, "--authority", "some-other-environment", "--authority-key", pub},
			want: "not the expected",
		},
		{
			name: "no manifest at all",
			args: []string{"--authority", m.Signature.Authority, "--authority-key", pub},
			want: "needs --file",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := capture(t, func() error { return runWallStage1Binary(tc.args) }); err == nil {
				t.Fatalf("a binary digest was derived with %s", tc.name)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not say why: %v", err)
			}
		})
	}

	// A manifest whose delivered binary nobody attested is not a source of
	// truth about that binary, so the derivation refuses it rather than
	// reading the field out anyway.
	t.Run("a manifest whose build attestation does not hold", func(t *testing.T) {
		var raw map[string]any
		if err := walltime.ReadJSONFile(out, &raw); err != nil {
			t.Fatal(err)
		}
		src, _ := raw["source"].(map[string]any)
		att, _ := src["build_attestation"].(map[string]any)
		delete(att, "verifier_signature")
		broken := filepath.Join(t.TempDir(), "stage1.json")
		if err := walltime.WriteJSONFile(broken, raw); err != nil {
			t.Fatal(err)
		}
		_, _, err := capture(t, func() error {
			return runWallStage1Binary([]string{"--file", broken, "--authority", m.Signature.Authority, "--authority-key", pub})
		})
		if err == nil {
			t.Fatal("a binary nobody countersigned was derived as authorised")
		}
		if !strings.Contains(err.Error(), "does not authorise a delivery") {
			t.Errorf("the refusal does not name the failure: %v", err)
		}
	})
}

func mustKey(t *testing.T) []byte {
	t.Helper()
	k, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}
