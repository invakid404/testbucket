package walltime

import (
	"crypto/ed25519"
	"fmt"
	"strings"
)

// BuildAttestationKind identifies the builder's statement about one artifact.
const BuildAttestationKind = "tb.walltime.build-attestation/v1"

// BuildAttestation is the builder's signed statement that an exact artifact
// was built from an exact source.
//
// It replaced a plain string. `Stage1Manifest.Source.BuildAttestation` used to
// be caller-authored prose that validation checked only for non-emptiness, so
// `trust-me-this-binary-was-built-from-the-tip` satisfied the frozen
// requirement that the delivered binary be reproducibly built or attested from
// the reviewed tip. There was no subject, no builder, no issuer, no signature,
// no verifier identity and no retained verification result — the five things
// the contract asks to be retained and the one thing that makes them
// checkable.
//
// Every field here is compared against something else: the subject against the
// manifest's own binary digest, the source against its reviewed tip, the
// signature against a predeclared builder key, and the result against the only
// value that admits a delivery.
type BuildAttestation struct {
	Kind string `json:"kind"`
	// SubjectName and SubjectDigest are the exact artifact attested. The
	// digest is compared with the binary Stage 1 delivers, so an attestation
	// for some other build cannot travel beside it.
	SubjectName   string `json:"subject_name"`
	SubjectDigest Digest `json:"subject_digest"`
	// SourceRepository and SourceCommit are what it was built FROM. The commit
	// is compared with the reviewed tip.
	SourceRepository string `json:"source_repository"`
	SourceCommit     string `json:"source_commit"`
	// BuilderID is the workload that produced it, and Issuer the identity that
	// vouches for that workload — for GitHub Actions, the workflow ref and the
	// OIDC issuer respectively.
	BuilderID string `json:"builder_id"`
	Issuer    string `json:"issuer"`
	// BuildRun and BuildAttempt bind the attestation to the run that made it,
	// which is what lets a reader go and look. They are REQUIRED: the contract
	// asks the verification result to be bound to the GitHub run and attempt,
	// and an optional empty string binds it to nothing.
	BuildRun     string `json:"build_run"`
	BuildAttempt string `json:"build_attempt"`
	// VerifierID, VerifierBinary and VerifierVersion are WHO checked it, and
	// Result is what they concluded. The contract asks for the verification
	// result to be retained; retaining it is only meaningful if the identity
	// that produced it is retained too.
	VerifierID      string     `json:"verifier_id"`
	VerifierBinary  Digest     `json:"verifier_binary"`
	VerifierVersion string     `json:"verifier_version"`
	VerifiedAt      string     `json:"verified_at"`
	Result          string     `json:"result"`
	Signature       *Signature `json:"signature,omitempty"`
}

// AttestationVerified is the only result that admits a delivery. Anything else
// — including a blank — is a build nobody vouched for.
const AttestationVerified = "verified"

// DigestOf is the attestation's canonical identity, excluding its signature.
func (a BuildAttestation) DigestOf() (Digest, error) {
	c := a
	c.Signature = nil
	return DigestJSON(c)
}

// Sign attaches the builder's detached signature.
func (a *BuildAttestation) Sign(builder string, key ed25519.PrivateKey) error {
	d, err := a.DigestOf()
	if err != nil {
		return err
	}
	a.Signature = &Signature{Authority: builder, KeyID: PublicKeyOf(key), Digest: d, Value: SignApproval(builder, key, d)}
	return nil
}

// Verify checks the attestation against the delivery it claims to be about.
//
// binary and reviewTip are the manifest's own values, so what this answers is
// "does this attestation describe THIS delivery", not merely "is this
// attestation well formed". builderKeys are PREDECLARED: an attestation
// verified against whatever signed it is one anybody can mint.
func (a BuildAttestation) Verify(binary Digest, reviewTip string, builderKeys []string) []string {
	var problems []string
	if a.Kind != BuildAttestationKind {
		problems = append(problems, fmt.Sprintf("build attestation kind %q, want %q", a.Kind, BuildAttestationKind))
	}
	for _, f := range []struct{ what, value string }{
		{"subject name", a.SubjectName},
		{"subject digest", string(a.SubjectDigest)},
		{"source repository", a.SourceRepository},
		{"source commit", a.SourceCommit},
		{"builder id", a.BuilderID},
		{"issuer", a.Issuer},
		{"verifier id", a.VerifierID},
		{"verifier binary", string(a.VerifierBinary)},
		{"verifier version", a.VerifierVersion},
		{"verification instant", a.VerifiedAt},
		{"result", a.Result},
		{"build run", a.BuildRun},
		{"build attempt", a.BuildAttempt},
	} {
		if strings.TrimSpace(f.value) == "" {
			problems = append(problems, "the build attestation records no "+f.what)
		}
	}
	if len(problems) > 0 {
		return problems
	}
	if _, err := parseInstant(a.VerifiedAt); err != nil {
		problems = append(problems, fmt.Sprintf("the build attestation's verification instant: %v", err))
	}
	if a.Result != AttestationVerified {
		problems = append(problems, fmt.Sprintf("the build attestation's retained result is %q, not %q", a.Result, AttestationVerified))
	}
	if err := requireFullSHA("the attested source commit", a.SourceCommit); err != nil {
		problems = append(problems, err.Error())
	}
	if binary != "" && a.SubjectDigest != binary {
		problems = append(problems, fmt.Sprintf("the build attestation attests %s, but the delivered binary is %s", a.SubjectDigest, binary))
	}
	if reviewTip != "" && a.SourceCommit != reviewTip {
		problems = append(problems, fmt.Sprintf("the build attestation attests a build from %s, but the reviewed tip is %s", a.SourceCommit, reviewTip))
	}
	if a.Signature == nil {
		problems = append(problems, "the build attestation is unsigned; an unattributable claim about a build is prose")
		return problems
	}
	if len(builderKeys) == 0 {
		problems = append(problems, "no builder key was predeclared, so the attestation's own signature would authenticate it and any self-generated key would vouch for any build")
		return problems
	}
	// The SIGNER must be the builder it names. Signature.Authority is the only
	// signer identity a signature carries, and it is now bound into the signed
	// bytes; leaving it unequal to BuilderID would make the retained builder
	// identity an unchecked string beside an authenticated one.
	if a.Signature.Authority != a.BuilderID {
		problems = append(problems, fmt.Sprintf(
			"the build attestation is signed by authority %q but names builder %q; the retained builder identity must be the identity that signed",
			a.Signature.Authority, a.BuilderID))
	}
	d, err := a.DigestOf()
	if err != nil {
		return append(problems, err.Error())
	}
	if err := VerifySigned(a.Signature, d, builderKeys); err != nil {
		problems = append(problems, fmt.Sprintf("build attestation signature: %v", err))
	}
	return problems
}
