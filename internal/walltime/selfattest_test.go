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
	verifierKey := mustSigningKey()
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
		// Countersigned by the verifier it names, under the verifier's OWN
		// key: that second signature is what makes the verifier a party
		// rather than a label the builder chose.
		if err := a.Countersign(verifier, verifierKey); err != nil {
			t.Fatal(err)
		}
		return a
	}

	keys := []string{PublicKeyOf(key), PublicKeyOf(verifierKey)}
	self := build(builder)
	problems := self.Verify("sha256:aaaa", "295c48aa98120245334e9cc0928b3c6c313c750d", keys)
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "both the builder and the verifier") {
		t.Errorf("a self-attested build was accepted: %v", problems)
	}

	// And an independently verified one still passes, so the rule was added
	// rather than the path broken.
	independent := build("release-verification@example.org")
	if problems := independent.Verify("sha256:aaaa", "295c48aa98120245334e9cc0928b3c6c313c750d", keys); len(problems) != 0 {
		t.Errorf("an independently verified build was refused: %v", problems)
	}

	// TWO LABELS OVER ONE KEY IS ONE PARTY. Requiring the identity strings to
	// differ only obliged a builder to pick two words; what makes the second
	// signature evidence is that somebody else made it.
	oneKey := independent
	if err := oneKey.Countersign(oneKey.VerifierID, key); err != nil {
		t.Fatal(err)
	}
	if problems := oneKey.Verify("sha256:aaaa", "295c48aa98120245334e9cc0928b3c6c313c750d", keys); len(problems) == 0 {
		t.Error("a builder that signed both halves under one key passed as independent verification")
	}

	// AND THE COUNTERSIGNATURE MUST BE THE VERIFIER'S. A second signature
	// attributed to somebody who is not the named verifier verifies nothing.
	misattributed := independent
	if err := misattributed.Countersign("somebody-else@example.org", verifierKey); err != nil {
		t.Fatal(err)
	}
	if problems := misattributed.Verify("sha256:aaaa", "295c48aa98120245334e9cc0928b3c6c313c750d", keys); len(problems) == 0 {
		t.Error("a countersignature by a party the document does not name passed")
	}

	// A BUILDER-ONLY SIGNATURE, which is exactly what the release path
	// produced: every verifier field written and signed by the builder.
	builderOnly := independent
	builderOnly.VerifierSignature = nil
	if problems := builderOnly.Verify("sha256:aaaa", "295c48aa98120245334e9cc0928b3c6c313c750d", keys); len(problems) == 0 {
		t.Error("an attestation with no verifier signature passed as verified")
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
	// THE VERIFIER IS A SEPARATE JOB WITH A SEPARATE KEY.
	//
	// An identity string is not a party. The builder job must not hold the
	// verifier key at all, and the countersignature must be produced by a job
	// that obtained the artifact for itself.
	if !strings.Contains(yml, "wall countersign") {
		t.Error("nothing in the release workflow countersigns the builder's attestation")
	}
	builder, verify, publish := strings.Index(yml, "\n  release:"), strings.Index(yml, "\n  verify:"), strings.Index(yml, "\n  publish:")
	if builder < 0 || verify < 0 || publish < 0 {
		t.Fatalf("the release workflow no longer has separate build/verify/publish jobs: release=%d verify=%d publish=%d", builder, verify, publish)
	}
	if !strings.Contains(yml[verify:publish], "TB_WALL_VERIFIER_KEY: ${{ secrets.TB_WALL_VERIFIER_KEY }}") {
		t.Error("the verify job does not sign under its own key")
	}
	// The KEY ITSELF, not a mention of its name: the comment above the verify
	// job names it, and what matters is which step is given the secret.
	if strings.Contains(yml[builder:verify], "TB_WALL_VERIFIER_KEY: ${{") {
		t.Error("the builder job holds the verifier key; one party with two keys is still one party")
	}
	if strings.Contains(yml[verify:publish], "TB_WALL_BUILDER_KEY: ${{") {
		t.Error("the verify job holds the builder key")
	}
	// And it refuses before signing anything if that identity is absent or is
	// the builder, rather than emitting an attestation nothing will accept.
	if !strings.Contains(yml, `[ "${TB_VERIFIER_ID:-}" = "$TB_BUILDER_ID" ]`) {
		t.Error("the release workflow does not refuse a verifier identity equal to the builder")
	}
	// AND THE ATTESTATION IS A RELEASE INPUT. The files were produced and
	// consulted by nothing, so they gated no delivery even on their own terms.
	if !strings.Contains(yml[publish:], "wall verify-attestation") {
		t.Error("the publish job does not refuse an asset whose attestation fails to verify; the attestations gate nothing")
	}
	if !strings.Contains(yml, "needs: verify") {
		t.Error("publication does not depend on the verify job, so it can publish an unverified build")
	}
}
