package walltime

import (
	"crypto/ed25519"
	"fmt"
	"strings"
)

// RunnerAttestationKind identifies a fleet's signed statement about the host a
// job actually ran on.
const RunnerAttestationKind = "tb.walltime.runner-attestation/v1"

// RunnerAttestation binds an asserted runner image to the host that EXECUTED a
// job, under a key the Stage-1 manifest predeclares.
//
// WHY IT HAS TO EXIST. The manifest binds a runner image and the pair equality
// check compares that binding, but `--runner-image` was caller-supplied text
// copied into the manifest and compared only with itself. Nothing tied it to
// the machine that ran anything: two arms could execute on different hosts
// while both signed manifests claimed one identity, and a digest-shaped
// self-hosted scheduler label could name an image the host is not. Refusing an
// alias made the assertion unambiguous; it did not make it true.
//
// GitHub's runner context does not report the selected image digest, so a
// hosted lane cannot produce this and is UNSUPPORTED for a scored arm — which
// is the honest outcome, and the one the manifest now enforces. A fleet that
// provisions its own runners can: it knows which image it booted, it can name
// the runner that will execute the job, and it holds a key the campaign
// authority predeclares. That is the attested binding.
//
// What it is NOT: a claim the wrapper can make about itself. The signer is the
// fleet, and the fleet is not the job.
type RunnerAttestation struct {
	Kind string `json:"kind"`
	// Image is the exact image the fleet booted this runner from. It must
	// equal the manifest's Consumer.RunnerImage.
	Image string `json:"image"`
	// Runner is the identity of the host as the runner itself reports it —
	// $RUNNER_NAME — so the statement names one machine rather than a fleet.
	Runner string `json:"runner"`
	// OS and Arch are the platform the fleet booted, compared with what the
	// run actually reports.
	OS   string `json:"os"`
	Arch string `json:"arch"`
	// Repository, WorkflowRun, RunAttempt, Job and Bucket scope the statement
	// to ONE ROW. A statement scoped only to a run is good for every matrix
	// job in it: one host attestation naming `runner-a` could be replayed
	// across jobs and buckets that never touched `runner-a`, and every
	// signature check would still pass. A fleet attests one row on one host.
	Repository  string `json:"repository"`
	WorkflowRun string `json:"workflow_run"`
	RunAttempt  string `json:"run_attempt"`
	Job         string `json:"job"`
	Bucket      string `json:"bucket"`
	// AttestedAt is when the fleet made the statement.
	AttestedAt string     `json:"attested_at"`
	Signature  *Signature `json:"signature,omitempty"`
}

// SubjectOf is the digest the fleet signs: every field except the signature.
func (a RunnerAttestation) SubjectOf() (Digest, error) {
	a.Signature = nil
	return DigestJSON(a)
}

// Sign attaches the fleet's signature.
func (a *RunnerAttestation) Sign(fleet string, key ed25519.PrivateKey) error {
	a.Kind = RunnerAttestationKind
	d, err := a.SubjectOf()
	if err != nil {
		return err
	}
	a.Signature = &Signature{
		Authority: fleet, KeyID: PublicKeyOf(key), Digest: d,
		Value: SignApproval(fleet, key, d),
	}
	return nil
}

// Verify checks the attestation against the image the manifest asserts, the
// run it claims to be about, and the keys the manifest predeclares.
//
// Every comparison is equality on the exact strings. A fleet that boots a
// different image, a statement about another run, or a signature by a key
// nobody predeclared are all the same answer: this does not attest THIS job.
func (a RunnerAttestation) Verify(image string, run RunIdentity, keys []string) error {
	if err := a.VerifyDocument(image, keys); err != nil {
		return err
	}
	// AND IT IS ABOUT THIS RUN. The manifest is signed before any run exists,
	// so this half is checked where the records are: the verifier holds both.
	// THE ROW, AND THE HOST THE ROW SAW.
	//
	// Repository/run/attempt scope a statement to a run, and a run is a matrix
	// of jobs: one statement naming `runner-a` was good for every job and
	// bucket in it, none of which need have executed on `runner-a`. Job and
	// bucket narrow it to one row; the three runner fields are the row's OWN
	// observation of the machine it is running on, read from the runner's
	// environment by the wrapper rather than taken from the statement being
	// checked. The fleet says what it booted; the row says what it is on; this
	// requires them to be the same host.
	for _, f := range []struct{ what, got, want string }{
		{"repository", a.Repository, run.Repository},
		{"workflow run", a.WorkflowRun, run.WorkflowRun},
		{"run attempt", a.RunAttempt, run.AttemptID},
		{"job", a.Job, run.Job},
		{"bucket", a.Bucket, run.BucketID},
		{"runner name", a.Runner, run.RunnerName},
		{"runner os", a.OS, run.RunnerOS},
		{"runner arch", a.Arch, run.RunnerArch},
	} {
		if strings.TrimSpace(f.want) == "" {
			return fmt.Errorf("runner attestation cannot be bound to this row: the records record no %s, so nothing independent says which host ran it", f.what)
		}
		if f.got != f.want {
			return fmt.Errorf("the runner attestation is for %s %q, but this row records %q", f.what, f.got, f.want)
		}
	}
	return nil
}

// VerifyDocument checks everything that does not need a run: the shape, the
// image it names, and the fleet signature against the predeclared keys.
func (a RunnerAttestation) VerifyDocument(image string, keys []string) error {
	if a.Kind != RunnerAttestationKind {
		return fmt.Errorf("runner attestation kind %q, want %q", a.Kind, RunnerAttestationKind)
	}
	if err := requireSet(map[string]string{
		"the attested image":   a.Image,
		"the runner identity":  a.Runner,
		"the runner os":        a.OS,
		"the runner arch":      a.Arch,
		"the repository":       a.Repository,
		"the workflow run":     a.WorkflowRun,
		"the run attempt":      a.RunAttempt,
		"the job":              a.Job,
		"the bucket":           a.Bucket,
		"the attestation time": a.AttestedAt,
	}); err != nil {
		return fmt.Errorf("runner attestation %w", err)
	}
	if _, err := parseInstant(a.AttestedAt); err != nil {
		return fmt.Errorf("runner attestation: %w", err)
	}
	if !isImmutableImage(a.Image) {
		return fmt.Errorf("runner attestation names image %q, which is not an immutable identity", a.Image)
	}
	if a.Image != image {
		return fmt.Errorf("the fleet attests image %s but the manifest binds %s", a.Image, image)
	}
	if len(keys) == 0 {
		return fmt.Errorf("no runner authority key is predeclared, so the attestation would be authenticated by whatever signed it and any self-generated key could vouch for any host")
	}
	if a.Signature == nil {
		return fmt.Errorf("the runner attestation is unsigned; an unattributable statement about a host is prose")
	}
	d, err := a.SubjectOf()
	if err != nil {
		return err
	}
	return VerifySigned(a.Signature, d, keys)
}
