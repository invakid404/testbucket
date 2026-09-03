package walltime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func releaseWorkflow(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// releaseWorkflowCode is the workflow with its comment lines removed, so an
// assertion about what a job DOES is not satisfied or defeated by prose
// describing what it used to do.
func releaseWorkflowCode(t *testing.T) string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(releaseWorkflow(t), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// TestTheReleaseArtifactIsUploadedFromOneRoot is the F9 regression.
//
// upload-artifact roots a multi-path upload at the LEAST COMMON ANCESTOR of
// its inputs. `dist` lives in the workspace and the manifest and attestations
// under RUNNER_TEMP, so on a hosted runner that ancestor was /home/runner/work
// and the download produced `testbucket/testbucket/dist` and
// `_temp/release-manifest.json` — while the verify and publish jobs both read
// a flat `$TB_BUILD/dist` and `$TB_BUILD/release-manifest.json`. The release
// stopped there, before any independent check or campaign gate ran.
func TestTheReleaseArtifactIsUploadedFromOneRoot(t *testing.T) {
	s := releaseWorkflow(t)
	upload := strings.Index(s, "name: release-build")
	if upload < 0 {
		t.Fatal("the release-build upload is not recognizable")
	}
	// ONE path, so that path is the artifact root.
	block := s[upload:]
	if lines := strings.SplitN(block, "\n", 8); len(lines) > 7 {
		block = strings.Join(lines[:7], "\n")
	}
	if strings.Count(block, "path: ") != 1 || !strings.Contains(block, "path: ${{ runner.temp }}/release-staging") {
		t.Errorf("the release artifact is not uploaded from a single staged root:\n%s", block)
	}
	for _, mixed := range []string{"            dist\n", "            ${{ runner.temp }}/release-manifest.json\n"} {
		if strings.Contains(block, mixed) {
			t.Errorf("the upload still mixes workspace and runner-temp paths, so its root is their common ancestor")
		}
	}
	// And the staging step puts each consumer-addressed name at that root.
	stage := s[strings.Index(s, "Stage the built artifacts under one root"):]
	for _, want := range []string{`"$TB_STAGING/dist"`, `"$TB_STAGING/release-manifest.json"`, `"$TB_STAGING/attestations"`} {
		if !strings.Contains(stage, want) {
			t.Errorf("the staging step does not place %s", want)
		}
	}
	// The consumers address exactly that flat layout.
	for _, want := range []string{`"$TB_BUILD/release-manifest.json"`, `"$TB_BUILD/attestations/$name.attestation.json"`, `"$TB_BUILD/$a"`} {
		if !strings.Contains(s, want) {
			t.Errorf("no consumer reads %s; the producer and the consumer must agree on the layout", want)
		}
	}
}

// TestTheReleaseAdjudicatorIsNotTheCandidate is the F10 regression.
//
// The verify job built `testbucket-verifier` from the release candidate's own
// checkout and handed that binary the verifier private key; the publish job
// built its gate the same way. A candidate that decides whether it is
// acceptable, signs the answer, and reports its own hash as the "verifier
// binary" has verified nothing.
func TestTheReleaseAdjudicatorIsNotTheCandidate(t *testing.T) {
	s := releaseWorkflowCode(t)
	verifyAt, publishAt := strings.Index(s, "\n  verify:\n"), strings.Index(s, "\n  publish:\n")
	if verifyAt < 0 || publishAt <= verifyAt {
		t.Fatal("the release jobs are not recognizable")
	}
	for name, job := range map[string]string{"verify": s[verifyAt:publishAt], "publish": s[publishAt:]} {
		// NOTHING IS BUILT FROM THE CANDIDATE'S OWN CHECKOUT. The bootstrap
		// route does build — from a SEPARATE tree checked out at its own
		// pinned commit — so what matters is where the build runs, not that a
		// build exists.
		for _, line := range strings.Split(job, "\n") {
			if !strings.Contains(line, "go build") {
				continue
			}
			if !strings.Contains(line, "$RUNNER_TEMP/trusted-src") {
				t.Errorf("the %s job builds its adjudicator outside the pinned bootstrap tree: %s", name, strings.TrimSpace(line))
			}
		}
		if !strings.Contains(job, "TB_RELEASE_TRUSTED_VERIFIER_TAG") || !strings.Contains(job, "TB_RELEASE_TRUSTED_VERIFIER_SHA256") {
			t.Errorf("the %s job does not offer a separately pinned, digest-checked released adjudicator", name)
		}
		// THE BOOTSTRAP ROUTE IS REACHABLE, not merely mentioned. No published
		// release carries the `wall` adjudication subcommands and this step
		// refuses the tag being released, so without a route that builds from
		// a separately pinned COMMIT the first wall-capable release has no
		// adjudicator it could ever obtain.
		if !strings.Contains(job, `elif [ -n "${TB_TRUSTED_VERIFIER_COMMIT:-}" ]; then`) {
			t.Errorf("the %s job has no reachable bootstrap branch", name)
		}
		if !strings.Contains(job, "TB_RELEASE_TRUSTED_VERIFIER_COMMIT") {
			t.Errorf("the %s job never reads the bootstrap pin", name)
		}
		if !strings.Contains(job, `grep -Eq '^[0-9a-f]{40}$'`) {
			t.Errorf("the %s job does not require the bootstrap pin to be an exact full-length commit", name)
		}
		if !strings.Contains(job, "sha256sum -c -") {
			t.Errorf("the %s job runs a downloaded adjudicator without checking the pinned digest first", name)
		}
		// THE ASSET NAME IS THE ONE THAT IS PUBLISHED. goreleaser names
		// archives {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }};
		// a `uname`-built name matches no asset at any tag, so the step could
		// never obtain an adjudicator at all.
		if !strings.Contains(job, `asset="testbucket_${version}_${os}_${arch}.tar.gz"`) {
			t.Errorf("the %s job's asset selector does not match the published archive naming", name)
		}
		// EXACT STABLE SemVer, by regex. A shell glob accepted `v1x.2y.3junk`
		// and `v1.2.3-rc.1`.
		if !strings.Contains(job, `grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'`) {
			t.Errorf("the %s job accepts a prerelease or a malformed tag as its trusted adjudicator", name)
		}
		if strings.Contains(job, "v[0-9]*.[0-9]*.[0-9]*)") {
			t.Errorf("the %s job still guards its tag with a permissive shell glob", name)
		}
		if !strings.Contains(job, `"$TB_TRUSTED_VERIFIER_TAG" = "${GITHUB_REF_NAME:-}"`) {
			t.Errorf("the %s job would accept the tag being released as its own adjudicator", name)
		}
		if !strings.Contains(job, `"$TB_TRUSTED_VERIFIER_COMMIT" = "$TB_RELEASE_SHA"`) {
			t.Errorf("the %s job would bootstrap its adjudicator from the commit being released", name)
		}
		if !strings.Contains(job, "exit 1") {
			t.Errorf("the %s job does not fail closed when no pin is set", name)
		}
	}
	// The BUILDER still builds from the candidate — it is the candidate's own
	// artifacts it is producing — and it must not hold either adjudication.
	builder := s[:verifyAt]
	if strings.Contains(builder, "wall countersign") || strings.Contains(builder, "wall verify-attestation") {
		t.Error("the builder job adjudicates its own delivery")
	}
}

// TestTheScoredAndTrustedTagGuardsAreStrict is the F9 regression.
//
// `case "$v" in v[0-9]*.[0-9]*.[0-9]*)` reads like a version check and is not
// one: in a shell pattern `*` is unrestricted, so it also matched
// `v1x.2y.3junk` and `v1.2.3-rc.1`. A prerelease is a build the project has
// not stood behind, and the trusted-adjudicator path could select one.
func TestTheScoredAndTrustedTagGuardsAreStrict(t *testing.T) {
	for _, file := range []string{
		filepath.Join("..", "..", ".github", "workflows", "release.yml"),
		filepath.Join("..", "..", ".github", "actions", "run-bucket", "action.yml"),
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if strings.Contains(s, "v[0-9]*.[0-9]*.[0-9]*)") {
			t.Errorf("%s still guards an exact-tag decision with a permissive shell glob", filepath.Base(file))
		}
		if !strings.Contains(s, `^v[0-9]+\.[0-9]+\.[0-9]+$`) {
			t.Errorf("%s does not use the strict stable-SemVer expression", filepath.Base(file))
		}
	}
}

// TestTheEventDirectoryResolvesTheScriptsPrimaryGroup is the F7 regression.
//
// The Go boundary resolves the script account's real primary GID; the action
// shell reinterpreted the ACCOUNT NAME as a group name. `useradd` often
// creates a like-named group, but a valid account can have any primary group —
// and then `chgrp` fails and the measured script cannot write one event file.
func TestTheEventDirectoryResolvesTheScriptsPrimaryGroup(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "run-bucket", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	// The CODE, not the comment recording why it changed.
	for _, line := range strings.Split(s, "\n") {
		code, _, _ := strings.Cut(line, "#")
		if strings.Contains(code, `chgrp "$TB_WALL_SCRIPT_USER"`) {
			t.Errorf("the event directory is chgrp'd to the account NAME, which is not necessarily a group name: %s", strings.TrimSpace(line))
		}
	}
	if !strings.Contains(s, `script_gid="$(id -g "$TB_WALL_SCRIPT_USER")"`) || !strings.Contains(s, `chgrp "$script_gid"`) {
		t.Error("the event directory's group is not resolved from the account's real primary GID")
	}
}
