package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/invakid404/testbucket/internal/walltime"
)

// runnerKeyEnv is the FLEET'S signing key. It is an environment variable for
// the same reason every other signing key here is: a key on a command line is
// a key in the process table — and it is on the scrub denylist, so no observer
// or measured child inherits it.
const runnerKeyEnv = walltime.RunnerKeyEnv

// runWallAttestRunner produces the fleet's signed statement that a named host
// was booted from a named image, scoped to one run.
//
// It is run by whoever PROVISIONS the runners, not by the job: the fleet knows
// which image it booted and holds a key the campaign authority predeclares,
// and a job that attested its own host would be asserting exactly the thing
// that was already unverified. On a hosted lane nobody can produce this — the
// runner context does not report the selected image — and that lane is
// unsupported for a scored arm rather than scored on an assertion.
func runWallAttestRunner(args []string) error {
	fs := flag.NewFlagSet("wall attest-runner", flag.ExitOnError)
	image := fs.String("image", "", "the exact immutable image this host was booted from (required)")
	runner := fs.String("runner", os.Getenv("RUNNER_NAME"), "the host's own identity, as the runner reports it (defaults to $RUNNER_NAME)")
	osName := fs.String("os", os.Getenv("RUNNER_OS"), "the platform the fleet booted (defaults to $RUNNER_OS)")
	arch := fs.String("arch", os.Getenv("RUNNER_ARCH"), "the architecture the fleet booted (defaults to $RUNNER_ARCH)")
	repository := fs.String("repository", os.Getenv("GITHUB_REPOSITORY"), "the repository the run belongs to")
	workflowRun := fs.String("workflow-run", os.Getenv("GITHUB_RUN_ID"), "the workflow run this statement is scoped to")
	attempt := fs.String("run-attempt", os.Getenv("GITHUB_RUN_ATTEMPT"), "the run attempt this statement is scoped to")
	job := fs.String("job", os.Getenv("GITHUB_JOB"), "the matrix job this statement is scoped to. A statement scoped only to a run is good for every job in it")
	bucket := fs.String("bucket", "", "the bucket this statement is scoped to (required): a fleet attests one row on one host")
	at := fs.String("attested-at", "", "the instant the fleet made this statement, RFC3339 (required)")
	fleet := fs.String("fleet", "", "the fleet identity signing this statement (required)")
	out := fs.String("out", "", "write the signed attestation here (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for name, v := range map[string]string{
		"--image": *image, "--runner": *runner, "--os": *osName, "--arch": *arch,
		"--repository": *repository, "--workflow-run": *workflowRun,
		"--run-attempt": *attempt, "--job": *job, "--bucket": *bucket,
		"--attested-at": *at, "--fleet": *fleet, "--out": *out,
	} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("wall attest-runner needs %s: a statement missing any part of the host, the image or the run it is about attests nothing", name)
		}
	}
	raw := strings.TrimSpace(os.Getenv(runnerKeyEnv))
	if raw == "" {
		return fmt.Errorf("wall attest-runner needs %s, the fleet's signing key; it is read from the environment rather than a flag because a key on a command line is a key in the process table", runnerKeyEnv)
	}
	key, err := walltime.DecodeKey(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", runnerKeyEnv, err)
	}

	a := walltime.RunnerAttestation{
		Image: *image, Runner: *runner, OS: *osName, Arch: *arch,
		Repository: *repository, WorkflowRun: *workflowRun, RunAttempt: *attempt,
		Job: *job, Bucket: *bucket, AttestedAt: *at,
	}
	if err := a.Sign(*fleet, key); err != nil {
		return fmt.Errorf("sign the runner attestation: %w", err)
	}
	// REFUSED HERE IF IT WOULD BE REFUSED THERE: the producer applies the
	// consumer's own check, so a malformed statement fails where it is made
	// rather than at the arm that depended on it.
	if err := a.VerifyDocument(*image, []string{walltime.PublicKeyOf(key)}); err != nil {
		return fmt.Errorf("the attestation this would write does not verify: %w", err)
	}
	if err := walltime.WriteJSONFile(*out, a); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: %s attests %s booted from %s for %s run %s attempt %s job %s bucket %s\n",
		*fleet, *runner, *image, *repository, *workflowRun, *attempt, *job, *bucket)
	fmt.Fprintln(os.Stdout, walltime.PublicKeyOf(key))
	return nil
}
