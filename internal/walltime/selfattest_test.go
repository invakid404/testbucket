package walltime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestASelfAttestedBuildIsRefused is the F8 regression.
//
// The release workflow passed the builder identity as the verifier identity
// too. `wall attest` then wrote `Result: verified`, named the current binary as
// its own verifier, signed with the builder key, and verified only that same
// signature — and `BuildAttestation.Verify` never asked whether the two
// identities differed. A build that vouches for itself has been checked by
// nobody, and the frozen contract makes self-attestation ineligible.
func TestASelfAttestedBuildIsRefused(t *testing.T) {
	key := mustSigningKey()
	const builder = "invakid404/testbucket/.github/workflows/release.yml@refs/tags/v0.3.0"
	build := func(verifier string) BuildAttestation {
		a := BuildAttestation{
			Kind: BuildAttestationKind, SubjectName: "testbucket-linux-amd64",
			SubjectDigest:    "sha256:aaaa",
			SourceRepository: "invakid404/testbucket",
			SourceCommit:     "295c48aa98120245334e9cc0928b3c6c313c750d",
			BuilderID:        builder,
			Issuer:           "https://token.actions.githubusercontent.com",
			BuildRun:         "1", BuildAttempt: "1",
			VerifierID: verifier, VerifierBinary: "sha256:bbbb", VerifierVersion: "v0.3.0",
			VerifiedAt: "2026-09-02T00:00:00Z", Result: AttestationVerified,
		}
		if err := a.Sign(builder, key); err != nil {
			t.Fatal(err)
		}
		return a
	}

	self := build(builder)
	problems := self.Verify("sha256:aaaa", "295c48aa98120245334e9cc0928b3c6c313c750d", []string{PublicKeyOf(key)})
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "both the builder and the verifier") {
		t.Errorf("a self-attested build was accepted: %v", problems)
	}

	// And an independently verified one still passes, so the rule was added
	// rather than the path broken.
	independent := build("release-verification@example.org")
	if problems := independent.Verify("sha256:aaaa", "295c48aa98120245334e9cc0928b3c6c313c750d", []string{PublicKeyOf(key)}); len(problems) != 0 {
		t.Errorf("an independently verified build was refused: %v", problems)
	}
}

// TestTheReleaseWorkflowSuppliesAnIndependentVerifier: the document rule is
// only half of it. The workflow passed one identity into both flags, so it
// could never have produced an attestation the rule accepts.
func TestTheReleaseWorkflowSuppliesAnIndependentVerifier(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yml := string(b)
	if strings.Contains(yml, `--verifier-id "$TB_BUILDER_ID"`) {
		t.Error("the release workflow still passes the builder identity as the verifier; the attestation it produces is a build vouching for itself")
	}
	if !strings.Contains(yml, `--verifier-id "$TB_VERIFIER_ID"`) {
		t.Error("the release workflow does not pass an independent verifier identity")
	}
	// And it refuses before building anything if that identity is absent or
	// is the builder, rather than emitting an attestation nothing will accept.
	if !strings.Contains(yml, `[ "${TB_VERIFIER_ID:-}" = "$TB_BUILDER_ID" ]`) {
		t.Error("the release workflow does not refuse a verifier identity equal to the builder")
	}
}
