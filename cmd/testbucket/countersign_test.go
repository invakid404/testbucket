package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// TestCountersigningIsAnIndependentCheck is the F7 regression on the command
// side.
//
// `wall attest` used to write the verifier id, the verifier's binary digest,
// the instant and `Result: verified` itself, sign the lot with the builder key,
// and then "verify" the result against that same key. Every party in that
// document was the builder. The verifier is now a separate command run by a
// separate job: it re-derives the artifact's digest from the bytes IT holds,
// authenticates the builder half against a predeclared key, and signs under
// its own.
func TestCountersigningIsAnIndependentCheck(t *testing.T) {
	dir := t.TempDir()
	subject := filepath.Join(dir, "testbucket-linux-amd64.tar.gz")
	if err := os.WriteFile(subject, []byte("the exact delivered bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	builderKey, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	verifierKey, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	const commit = "a7d3344c4e1deeae64e195f6d631c51a426827be"
	const builderID = "invakid404/testbucket/.github/workflows/release.yml@refs/tags/v0.3.0"
	attestation := filepath.Join(dir, "builder.json")

	t.Setenv(walltime.BuilderKeyEnv, walltime.EncodeKey(builderKey))
	if err := runWallAttest([]string{
		"--subject", subject, "--subject-name", "testbucket-linux-amd64.tar.gz",
		"--source-repository", "invakid404/testbucket", "--source-commit", commit,
		"--builder-id", builderID, "--issuer", "https://token.actions.githubusercontent.com",
		"--build-run", "1", "--build-attempt", "1", "--out", attestation,
	}); err != nil {
		t.Fatal(err)
	}
	// THE BUILDER HALF IS NOT A VERIFICATION. It states what was built; it
	// does not state that anybody checked it.
	var half walltime.BuildAttestation
	if err := walltime.ReadJSONFile(attestation, &half); err != nil {
		t.Fatal(err)
	}
	if half.Result != "" || half.VerifierID != "" || half.VerifierSignature != nil {
		t.Errorf("wall attest wrote the verifier's half itself: id=%q result=%q signed=%v",
			half.VerifierID, half.Result, half.VerifierSignature != nil)
	}
	if problems := half.Verify(half.SubjectDigest, commit, []string{walltime.PublicKeyOf(builderKey)}); len(problems) == 0 {
		t.Error("a builder-only attestation verified; nothing independent has checked that build")
	}

	// THE VERIFIER'S HALF, under the verifier's own key.
	out := filepath.Join(dir, "verified.json")
	t.Setenv(walltime.VerifierKeyEnv, walltime.EncodeKey(verifierKey))
	countersign := func(args ...string) error {
		return runWallCountersign(append([]string{
			"--attestation", attestation, "--subject", subject,
			"--verified-at", "2026-09-02T12:00:00Z",
			"--builder-key", walltime.PublicKeyOf(builderKey), "--out", out,
		}, args...))
	}
	if err := countersign("--verifier-id", "release-verification@example.org"); err != nil {
		t.Fatal(err)
	}
	if err := runWallVerifyAttestation([]string{
		"--attestation", out, "--subject", subject, "--source-commit", commit,
		"--key", walltime.PublicKeyOf(builderKey), "--key", walltime.PublicKeyOf(verifierKey),
	}); err != nil {
		t.Errorf("an independently countersigned delivery was refused: %v", err)
	}

	// AND THE REFUSALS, each of which is a way the old shape passed.
	t.Run("the builder as its own verifier", func(t *testing.T) {
		if err := countersign("--verifier-id", builderID); err == nil {
			t.Error("the builder countersigned its own build")
		}
	})
	t.Run("the builder's key as the verifier's", func(t *testing.T) {
		t.Setenv(walltime.VerifierKeyEnv, walltime.EncodeKey(builderKey))
		if err := countersign("--verifier-id", "release-verification@example.org"); err == nil {
			t.Error("one key signing both halves passed as two parties")
		}
	})
	t.Run("bytes the builder did not attest", func(t *testing.T) {
		other := filepath.Join(dir, "other.tar.gz")
		if err := os.WriteFile(other, []byte("different bytes entirely"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := runWallCountersign([]string{
			"--attestation", attestation, "--subject", other,
			"--verifier-id", "release-verification@example.org",
			"--verified-at", "2026-09-02T12:00:00Z",
			"--builder-key", walltime.PublicKeyOf(builderKey), "--out", out,
		})
		if err == nil || !strings.Contains(err.Error(), "different bytes") {
			t.Errorf("the verifier countersigned an artifact the builder never attested: %v", err)
		}
	})
	t.Run("a builder half signed by somebody else", func(t *testing.T) {
		stranger, err := walltime.NewSigningKey()
		if err != nil {
			t.Fatal(err)
		}
		if err := countersign("--verifier-id", "release-verification@example.org",
			"--builder-key", walltime.PublicKeyOf(stranger)); err == nil {
			t.Error("the verifier adopted a builder statement it could not authenticate")
		}
	})
	t.Run("the publish gate refuses one key", func(t *testing.T) {
		if err := runWallVerifyAttestation([]string{
			"--attestation", out, "--subject", subject, "--source-commit", commit,
			"--key", walltime.PublicKeyOf(builderKey),
		}); err == nil {
			t.Error("a delivery authenticated against one party's key was published")
		}
	})
}
