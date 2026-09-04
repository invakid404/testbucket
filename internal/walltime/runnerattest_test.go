package walltime

import (
	"strings"
	"testing"
)

// A SCORED ARM'S RUNNER IMAGE IS ATTESTED BY THE FLEET, OR THE ARM IS REFUSED.
//
// `--runner-image` was caller-supplied text copied into the manifest and
// compared only with itself. Nothing tied it to a machine: two arms could
// execute on different hosts while both signed manifests claimed one identity,
// and a digest-shaped self-hosted scheduler label could name an image the host
// is not. Refusing an alias made the assertion unambiguous; it did not make it
// true.
//
// GitHub's runner context does not report the selected image, so a hosted lane
// cannot produce this and is UNSUPPORTED for a scored arm. That is what these
// refusals say, and saying it is the point: the alternative was scoring on an
// assertion.
func TestAScoredArmRequiresAFleetAttestationOfItsRunner(t *testing.T) {
	image := "ubuntu-24.04@sha256:" + strings.Repeat("cd", 32)
	run := RunIdentity{Repository: "example/mandel", WorkflowRun: "run-1", AttemptID: "1"}
	good := testRunnerAttestation(image, run.Repository, run.WorkflowRun, run.AttemptID)
	if err := good.Verify(image, run, testRunnerKeys()); err != nil {
		t.Fatalf("a genuine fleet attestation for this run was refused: %v", err)
	}

	for _, tc := range []struct {
		name string
		edit func(*RunnerAttestation)
		keys []string
		want string
	}{
		{
			name: "no predeclared fleet key",
			keys: nil,
			want: "no runner authority key is predeclared",
		},
		{
			name: "a key the campaign never declared",
			keys: []string{PublicKeyOf(mustSigningKey())},
			want: "not an authorised authority key",
		},
		{
			name: "the fleet attests a different image",
			edit: func(a *RunnerAttestation) {
				a.Image = "ubuntu-24.04@sha256:" + strings.Repeat("ab", 32)
				_ = a.Sign("ewj2-fleet", testFleetAuthority)
			},
			want: "but the manifest binds",
		},
		{
			name: "an image that is not immutable",
			edit: func(a *RunnerAttestation) {
				a.Image = "ubuntu-latest"
				_ = a.Sign("ewj2-fleet", testFleetAuthority)
			},
			want: "not an immutable identity",
		},
		{
			name: "a statement about another run",
			edit: func(a *RunnerAttestation) {
				a.WorkflowRun = "run-2"
				_ = a.Sign("ewj2-fleet", testFleetAuthority)
			},
			want: "not this run's",
		},
		{
			name: "a statement about another attempt",
			edit: func(a *RunnerAttestation) {
				a.RunAttempt = "2"
				_ = a.Sign("ewj2-fleet", testFleetAuthority)
			},
			want: "not this run's",
		},
		{
			name: "an unsigned statement",
			edit: func(a *RunnerAttestation) { a.Signature = nil },
			want: "unsigned",
		},
		{
			name: "a body edited after signing",
			edit: func(a *RunnerAttestation) { a.Runner = "some-other-host" },
			want: "signature covers",
		},
		{
			name: "no host identity at all",
			edit: func(a *RunnerAttestation) {
				a.Runner = ""
				_ = a.Sign("ewj2-fleet", testFleetAuthority)
			},
			want: "runner identity",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := *testRunnerAttestation(image, run.Repository, run.WorkflowRun, run.AttemptID)
			if tc.edit != nil {
				tc.edit(&a)
			}
			keys := testRunnerKeys()
			if tc.name == "no predeclared fleet key" {
				keys = nil
			} else if tc.keys != nil {
				keys = tc.keys
			}
			err := a.Verify(image, run, keys)
			if err == nil {
				t.Fatalf("%s was accepted as a fleet attestation of this run's host", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not say why (want %q): %v", tc.want, err)
			}
		})
	}
}

// AND THE MANIFEST REFUSES A SCORED ARM WITHOUT ONE, while an ablation — which
// is not a pair — is exempt.
func TestTheManifestRefusesAScoredArmWithNoRunnerAttestation(t *testing.T) {
	b := PlanningInputBundle{}
	m := testManifest(b, Digest("sha256:"+strings.Repeat("0", 64)))

	m.RunnerAttestation = nil
	err := m.Validate()
	if err == nil {
		t.Fatal("a scored arm validated with no fleet attestation of its runner")
	}
	if !strings.Contains(err.Error(), "unsupported for scoring") {
		t.Errorf("the refusal does not say the lane is unsupported: %v", err)
	}

	// An ablation is exempt: it is not one arm of a pair, so there is no
	// cross-host comparison for an unattested image to corrupt.
	m.AblationStratum = AblationStrata[0]
	if err := m.Validate(); err != nil && strings.Contains(err.Error(), "unsupported for scoring") {
		t.Errorf("an ablation was refused for want of a runner attestation: %v", err)
	}

	// AND A KEY SET WITHOUT A STATEMENT IS NOT A STATEMENT.
	m.AblationStratum = ""
	m.RunnerAuthorityKeys = testRunnerKeys()
	m.RunnerAttestation = nil
	if err := m.Validate(); err == nil {
		t.Error("predeclaring a fleet key was accepted as evidence that a fleet said anything")
	}
}
