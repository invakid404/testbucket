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
		if strings.Contains(job, "go build ") {
			t.Errorf("the %s job builds its adjudicator from the candidate it judges", name)
		}
		if !strings.Contains(job, "TB_RELEASE_TRUSTED_VERIFIER_TAG") || !strings.Contains(job, "TB_RELEASE_TRUSTED_VERIFIER_SHA256") {
			t.Errorf("the %s job does not obtain a separately pinned, predeclared verifier", name)
		}
		if !strings.Contains(job, "sha256sum -c -") {
			t.Errorf("the %s job runs its adjudicator without checking the pinned digest first", name)
		}
		// The pin must be an EXACT tag: an alias the release process can move
		// is not a separate root of trust, and the tag being released is not
		// an independent one either.
		if !strings.Contains(job, `v[0-9]*.[0-9]*.[0-9]*) ;;`) {
			t.Errorf("the %s job accepts a moving alias as its trusted verifier", name)
		}
		if !strings.Contains(job, `"$TB_TRUSTED_VERIFIER_TAG" = "${GITHUB_REF_NAME:-}"`) {
			t.Errorf("the %s job would accept the tag being released as its own adjudicator", name)
		}
		if !strings.Contains(job, "exit 1") {
			t.Errorf("the %s job does not fail closed when the pin is absent", name)
		}
	}
	// The BUILDER still builds from the candidate — it is the candidate's own
	// artifacts it is producing — and it must not hold either adjudication.
	builder := s[:verifyAt]
	if strings.Contains(builder, "wall countersign") || strings.Contains(builder, "wall verify-attestation") {
		t.Error("the builder job adjudicates its own delivery")
	}
}
