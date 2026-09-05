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
	// VerifierSignature is the VERIFIER'S OWN signature over the same
	// attestation, and it is what makes the verifier a party rather than a
	// label.
	//
	// The document carried one signature — the builder's — beside a verifier
	// id, a verifier binary digest and `Result: verified`, all of them chosen
	// and signed by the builder. Requiring the two identity STRINGS to differ
	// only obliged a builder to pick two labels; nothing else in the document
	// came from anywhere but the builder, so the retained verification result
	// was the builder's statement that it had checked itself.
	//
	// A verification is somebody else re-deriving the subject and saying so
	// under their own key. That is this field, and Verify requires it.
	VerifierSignature *Signature `json:"verifier_signature,omitempty"`
}

// AttestationVerified is the only result that admits a delivery. Anything else
// — including a blank — is a build nobody vouched for.
const AttestationVerified = "verified"

// DigestOf is the attestation's canonical identity, excluding its signature.
func (a BuildAttestation) DigestOf() (Digest, error) {
	c := a
	c.Signature, c.VerifierSignature = nil, nil
	return DigestJSON(c)
}

// BuilderDigestOf is what the BUILDER signs: the half of the document the
// builder can honestly state — the subject, the source, and its own identity.
//
// It excludes the verifier's fields because the builder does not know them and
// must not author them. The builder used to sign a complete document in which
// the verifier id, the verifier's binary digest, the instant of the
// verification and its RESULT were all values the builder had chosen; the
// verifier is a separate party that fills those in afterwards, and a digest
// covering them could not have been signed before they existed.
func (a BuildAttestation) BuilderDigestOf() (Digest, error) {
	c := a
	c.Signature, c.VerifierSignature = nil, nil
	c.VerifierID, c.VerifierBinary, c.VerifierVersion = "", "", ""
	c.VerifiedAt, c.Result = "", ""
	return DigestJSON(c)
}

// VerifierDigestOf is what the VERIFIER signs: the whole document, INCLUDING
// the builder's signature. Countersigning the builder's signature is what ties
// the two statements together — the verifier says which builder statement it
// checked, not merely that it checked something with these fields.
func (a BuildAttestation) VerifierDigestOf() (Digest, error) {
	c := a
	c.VerifierSignature = nil
	return DigestJSON(c)
}

// Sign attaches the builder's detached signature over the builder half.
func (a *BuildAttestation) Sign(builder string, key ed25519.PrivateKey) error {
	d, err := a.BuilderDigestOf()
	if err != nil {
		return err
	}
	a.Signature = &Signature{Authority: builder, KeyID: PublicKeyOf(key), Digest: d, Value: SignApproval(builder, key, d)}
	return nil
}

// Countersign attaches the VERIFIER'S signature.
//
// It is a separate call because it is a separate party, holding a separate
// key, in a separate job that obtained the artifact for itself and re-derived
// its digest. It signs the whole document including the builder's signature,
// so a countersignature cannot be lifted onto a different build statement.
func (a *BuildAttestation) Countersign(verifier string, key ed25519.PrivateKey) error {
	d, err := a.VerifierDigestOf()
	if err != nil {
		return err
	}
	a.VerifierSignature = &Signature{Authority: verifier, KeyID: PublicKeyOf(key), Digest: d, Value: SignApproval(verifier, key, d)}
	return nil
}

// Verify checks the attestation against the delivery it claims to be about.
//
// binary and reviewTip are the manifest's own values, so what this answers is
// "does this attestation describe THIS delivery", not merely "is this
// attestation well formed". keys are the PREDECLARED public keys of both
// parties — the builder and the independent verifier: an attestation verified
// against whatever signed it is one anybody can mint.
func (a BuildAttestation) Verify(binary Digest, reviewTip string, keys []string) []string {
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
	// SELF-ATTESTATION IS NOT ATTESTATION.
	//
	// The release workflow passed the builder identity as the verifier
	// identity too, and `wall attest` then wrote `Result: verified`, named the
	// current binary as its own verifier, signed with the builder key and
	// checked only that same signature. A build vouching for itself is a
	// claim, not a verification, and the frozen contract makes it ineligible.
	//
	// The two identities must differ. That is a property of the DOCUMENT, so
	// it is checked here rather than left to whoever wires the workflow.
	if strings.TrimSpace(a.BuilderID) == strings.TrimSpace(a.VerifierID) {
		problems = append(problems, fmt.Sprintf(
			"the build attestation names %q as both the builder and the verifier; a build that vouches for itself has been checked by nobody",
			a.BuilderID))
	}
	if a.Signature == nil {
		problems = append(problems, "the build attestation is unsigned; an unattributable claim about a build is prose")
	}
	// AND THE VERIFIER SIGNED IT TOO.
	//
	// Without this the verifier existed only as a string the builder wrote
	// down, next to a result the builder also wrote down. A build is verified
	// when a second party obtains the artifact, checks it and signs what it
	// concluded; that signature is the evidence, and its absence means nobody
	// checked this build but the party that made it.
	if a.VerifierSignature == nil {
		problems = append(problems, fmt.Sprintf(
			"the build attestation carries no signature from verifier %q; the verifier id, the verifier binary and the retained result were all written and signed by the builder, so nothing independent has checked this build",
			a.VerifierID))
	}
	if len(problems) > 0 {
		return problems
	}
	if len(keys) == 0 {
		problems = append(problems, "no builder or verifier key was predeclared, so the attestation's own signatures would authenticate it and any self-generated key would vouch for any build")
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
	if a.VerifierSignature.Authority != a.VerifierID {
		problems = append(problems, fmt.Sprintf(
			"the build attestation's second signature is by authority %q but names verifier %q; a countersignature attributed to somebody who is not the verifier verifies nothing",
			a.VerifierSignature.Authority, a.VerifierID))
	}
	// TWO LABELS AND ONE KEY IS ONE PARTY. The identities differing is a
	// property of the text; the KEYS differing is the property that makes the
	// second signature independent evidence rather than the builder signing
	// twice.
	if a.Signature.KeyID == a.VerifierSignature.KeyID {
		problems = append(problems, fmt.Sprintf(
			"the builder and the verifier signed with the same key %s; two identities holding one key are one party, and a build checked by the party that built it has been checked by nobody",
			a.Signature.KeyID))
	}
	builderDigest, err := a.BuilderDigestOf()
	if err != nil {
		return append(problems, err.Error())
	}
	verifierDigest, err := a.VerifierDigestOf()
	if err != nil {
		return append(problems, err.Error())
	}
	if err := VerifySigned(a.Signature, builderDigest, keys); err != nil {
		problems = append(problems, fmt.Sprintf("build attestation signature: %v", err))
	}
	if err := VerifySigned(a.VerifierSignature, verifierDigest, keys); err != nil {
		problems = append(problems, fmt.Sprintf("build attestation verifier signature: %v", err))
	}
	return problems
}
