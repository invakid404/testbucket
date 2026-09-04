package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"github.com/invakid404/testbucket/internal/walltime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// candidateArtifact stages a downloadable candidate artifact and returns the
// artifact root and the digest of its archive.
//
// members are written into the archive; siblings are written BESIDE it, inside
// the artifact but outside the digest-pinned bytes. The distinction is the
// whole point: the digest authenticates the archive, so only what the archive
// contains may be installed.
func candidateArtifact(t *testing.T, root string, members, siblings map[string]string, archives int) (string, string) {
	t.Helper()
	artifact := filepath.Join(root, "artifact")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path string) string {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		for name, body := range members {
			mode := int64(0o644)
			if strings.HasPrefix(body, "#!") {
				mode = 0o755
			}
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
		for _, closeErr := range []error{tw.Close(), gz.Close(), f.Close()} {
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%x", sha256.Sum256(b))
	}
	digest := write(filepath.Join(artifact, "candidate.tar.gz"))
	for i := 1; i < archives; i++ {
		write(filepath.Join(artifact, fmt.Sprintf("candidate-%d.tar.gz", i)))
	}
	for name, body := range siblings {
		if err := os.WriteFile(filepath.Join(artifact, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return artifact, digest
}

// runCandidateInstaller drives the REAL installer script with a stubbed `gh`
// that copies a staged artifact, so what is exercised is the shipped shell.
// candidateVersion is the delivery identity for this runner: the artifact is
// published per platform, so its name carries the runner's own os_arch.
func candidateVersion(t *testing.T, digest string) string {
	t.Helper()
	osName := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("candidate:123/candidate-build-%s_%s@sha256:%s", osName, arch, digest)
}

func runCandidateInstaller(t *testing.T, root, artifact, version string, env ...string) (string, string, error) {
	t.Helper()
	fakeBin := filepath.Join(root, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	gh := "#!/bin/sh\nout=\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"--dir\" ]; then shift; out=\"$1\"; fi\n  shift\ndone\n/bin/mkdir -p \"$out\"\n/bin/cp -R \"$TB_TEST_ARTIFACT/.\" \"$out/\"\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatal(err)
	}
	bindir := filepath.Join(root, "bin")
	cmd := exec.Command("bash", filepath.Join("..", "..", ".github", "actions", "install-testbucket.sh"))
	cmd.Env = append(append(os.Environ(),
		"PATH="+fakeBin+":"+os.Getenv("PATH"),
		"TB_TEST_ARTIFACT="+artifact,
		"TB_BINDIR="+bindir,
		"TB_REPO=example/testbucket",
		"TB_VERSION="+version,
		"GITHUB_PATH="+filepath.Join(root, "github-path"),
	), env...)
	out, err := cmd.CombinedOutput()
	return filepath.Join(bindir, "testbucket"), string(out), err
}

// THE DIGEST GOVERNS THE BYTES THAT RUN.
//
// A candidate is trusted because its bytes were named in advance. The
// installer digested one archive and then searched the WHOLE download for
// something named testbucket, so the digest authenticated one file while an
// unrelated sibling was the thing that got installed — a scored arm could run
// bytes its own stated digest never covered.
func TestTheCandidateInstallerRunsOnlyThePinnedArchive(t *testing.T) {
	const good = "#!/bin/sh\nprintf 'PINNED\\n'\n"
	const evil = "#!/bin/sh\nprintf 'UNPINNED\\n'\n"

	// The installed-binary digest is MANDATORY, so every case that expects the
	// installer to get as far as installing must state it.
	attested := func(body string) string {
		return fmt.Sprintf("TB_CANDIDATE_BINARY_DIGEST=sha256:%x", sha256.Sum256([]byte(body)))
	}

	// THE PROPERTY: an executable sibling beside the archive is never chosen,
	// and the archive's own member is.
	t.Run("a sibling beside the pinned archive is not installed", func(t *testing.T) {
		root := t.TempDir()
		artifact, digest := candidateArtifact(t,
			root,
			map[string]string{"testbucket": good},
			map[string]string{"testbucket": evil},
			1)
		bin, out, err := runCandidateInstaller(t, root, artifact,
			candidateVersion(t, digest), attested(good))
		if err != nil {
			t.Fatalf("the installer refused a well-formed candidate: %v\n%s", err, out)
		}
		got, err := exec.Command(bin).Output()
		if err != nil {
			t.Fatalf("the installed file did not run: %v", err)
		}
		if strings.TrimSpace(string(got)) != "PINNED" {
			t.Errorf("the installer ran %q; the digest must govern the bytes that run", strings.TrimSpace(string(got)))
		}
	})

	// AND WITH NOTHING LEGITIMATE TO INSTALL IT REFUSES rather than falling
	// back to whatever else arrived. This is the shape of the validator's own
	// control: the pinned archive holds no executable, so refusal is the only
	// correct outcome.
	t.Run("an archive with no binary is refused, not fallen back from", func(t *testing.T) {
		root := t.TempDir()
		artifact, digest := candidateArtifact(t,
			root,
			map[string]string{"README": "no executable here\n"},
			map[string]string{"testbucket": evil},
			1)
		bin, out, err := runCandidateInstaller(t, root, artifact,
			candidateVersion(t, digest), attested(good))
		if err == nil {
			ran, _ := exec.Command(bin).Output()
			t.Fatalf("the installer accepted an artifact whose pinned archive holds no binary and installed %q\n%s",
				strings.TrimSpace(string(ran)), out)
		}
		if !strings.Contains(out, "no testbucket member") {
			t.Errorf("the refusal does not say the pinned archive holds no binary: %s", out)
		}
	})

	// AMBIGUITY IS REFUSED. Two archives means the digest names one of them
	// and the installer would be choosing which.
	t.Run("more than one archive is refused", func(t *testing.T) {
		root := t.TempDir()
		artifact, digest := candidateArtifact(t,
			root, map[string]string{"testbucket": good}, nil, 2)
		_, out, err := runCandidateInstaller(t, root, artifact,
			candidateVersion(t, digest), attested(good))
		if err == nil {
			t.Fatalf("an ambiguous multi-archive artifact was accepted\n%s", out)
		}
		if !strings.Contains(out, "exactly one") {
			t.Errorf("the refusal does not name the ambiguity: %s", out)
		}
	})

	// A SECOND EXECUTABLE INSIDE THE ARCHIVE is the same ambiguity one level
	// in, and is refused too.
	t.Run("a second executable inside the archive is refused", func(t *testing.T) {
		root := t.TempDir()
		artifact, digest := candidateArtifact(t, root,
			map[string]string{"testbucket": good, "helper": evil}, nil, 1)
		_, out, err := runCandidateInstaller(t, root, artifact,
			candidateVersion(t, digest), attested(good))
		if err == nil {
			t.Fatalf("an archive carrying a second executable was accepted\n%s", out)
		}
		if !strings.Contains(out, "one binary") {
			t.Errorf("the refusal does not name the extra executable: %s", out)
		}
	})

	// A WRONG ARCHIVE DIGEST IS REFUSED, which is the original guarantee.
	t.Run("an archive that is not the demanded bytes is refused", func(t *testing.T) {
		root := t.TempDir()
		artifact, _ := candidateArtifact(t, root, map[string]string{"testbucket": good}, nil, 1)
		_, out, err := runCandidateInstaller(t, root, artifact,
			candidateVersion(t, strings.Repeat("a", 64)), attested(good))
		if err == nil {
			t.Fatalf("an archive that is not the demanded bytes was accepted\n%s", out)
		}
		if !strings.Contains(out, "digests to") {
			t.Errorf("the refusal does not name the digest mismatch: %s", out)
		}
	})

	// AND THE INSTALLED BINARY IS RE-DIGESTED against what was attested. The
	// archive digest says which archive; this says which binary, and Stage 1
	// binds the second.
	t.Run("the installed binary must match the attested digest", func(t *testing.T) {
		root := t.TempDir()
		artifact, digest := candidateArtifact(t, root, map[string]string{"testbucket": good}, nil, 1)
		_, out, err := runCandidateInstaller(t, root, artifact,
			candidateVersion(t, digest),
			"TB_CANDIDATE_BINARY_DIGEST=sha256:"+strings.Repeat("b", 64))
		if err == nil {
			t.Fatalf("a binary that is not the attested one was installed\n%s", out)
		}
		if !strings.Contains(out, "not the attested") {
			t.Errorf("the refusal does not name the attested binary digest: %s", out)
		}

		root2 := t.TempDir()
		artifact2, digest2 := candidateArtifact(t, root2, map[string]string{"testbucket": good}, nil, 1)
		want := fmt.Sprintf("%x", sha256.Sum256([]byte(good)))
		if _, out, err := runCandidateInstaller(t, root2, artifact2,
			candidateVersion(t, digest2),
			"TB_CANDIDATE_BINARY_DIGEST=sha256:"+want); err != nil {
			t.Errorf("the attested binary was refused: %v\n%s", err, out)
		}
	})
}

// fixtureAuthorityKey stands in for the protected campaign authority, minted
// once so every fixture claim verifies against the same predeclared key.
var fixtureAuthorityKey = func() ed25519.PrivateKey {
	k, err := walltime.NewSigningKey()
	if err != nil {
		panic(err)
	}
	return k
}()

// fixtureClaim is the one-shot planner claim a derivation is performed under.
//
// It carries what the schema requires rather than what a fixture could simply
// assert: the CANONICAL key, which any checker recomputes from the two
// parents, and the authority's real signature over the store identity. A
// hand-authored `Durable: true` beside `Key: "fixture"` used to pass, so
// Stage 2 could not tell an earned claim from a declared one.
func fixtureClaim(stage1, bundle walltime.Digest) *walltime.PlannerClaimReceipt {
	const store = "authority/durable-claims"
	subject := walltime.PlannerClaimStoreSubject(store)
	return &walltime.PlannerClaimReceipt{
		Store: store, Durable: true,
		Key: walltime.PlannerClaimKey(stage1, bundle), Stage1: stage1, Bundle: bundle,
		Attestation:   walltime.SignApproval(walltime.CampaignAuthority, fixtureAuthorityKey, subject),
		AuthorityKeys: []string{walltime.PublicKeyOf(fixtureAuthorityKey)},
	}
}

// THE DELIVERY IS VERIFIED AT THE THING THAT EXECUTES.
//
// The archive digest says which archive was downloaded. It says nothing about
// which member runs, whether that member is a real file, or whether the
// archive was built for this platform at all — and each of those was a way for
// unverified bytes to reach execution.
func TestTheCandidateDeliveryIsVerifiedEndToEnd(t *testing.T) {
	const good = "#!/bin/sh\nprintf 'PINNED\\n'\n"
	attested := fmt.Sprintf("TB_CANDIDATE_BINARY_DIGEST=sha256:%x", sha256.Sum256([]byte(good)))

	// THE BINARY DIGEST IS MANDATORY. An optional check nothing supplies is
	// not a check: the delivery would be verified up to the archive and
	// unverified at the thing that runs.
	t.Run("a candidate with no attested binary digest is refused", func(t *testing.T) {
		root := t.TempDir()
		artifact, digest := candidateArtifact(t, root, map[string]string{"testbucket": good}, nil, 1)
		_, out, err := runCandidateInstaller(t, root, artifact, candidateVersion(t, digest))
		if err == nil {
			t.Fatalf("a candidate installed with no attested binary digest\n%s", out)
		}
		if !strings.Contains(out, "TB_CANDIDATE_BINARY_DIGEST") {
			t.Errorf("the refusal does not name the missing attestation: %s", out)
		}
	})

	// THE ARTIFACT NAMES THIS PLATFORM. One archive is not the right archive.
	t.Run("an artifact for another platform is refused", func(t *testing.T) {
		root := t.TempDir()
		artifact, digest := candidateArtifact(t, root, map[string]string{"testbucket": good}, nil, 1)
		_, out, err := runCandidateInstaller(t, root, artifact,
			"candidate:123/candidate-build-plan9_sparc@sha256:"+digest, attested)
		if err == nil {
			t.Fatalf("a candidate built for another platform was installed\n%s", out)
		}
		if !strings.Contains(out, "does not name this runner's platform") {
			t.Errorf("the refusal does not name the platform mismatch: %s", out)
		}
	})

	// A SYMLINK IS A NAME FOR BYTES THE DIGEST DID NOT COVER. `[ -f ]` follows
	// links while the ambiguity check did not, so a fixed-name symlink pointing
	// outside the extracted tree was installed and executed.
	t.Run("a symlink member is refused", func(t *testing.T) {
		root := t.TempDir()
		artifact := filepath.Join(root, "artifact")
		if err := os.MkdirAll(artifact, 0o755); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(root, "outside")
		if err := os.WriteFile(outside, []byte("#!/bin/sh\nprintf 'OUTSIDE\\n'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		archive := filepath.Join(artifact, "candidate.tar.gz")
		f, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeSymlink, Name: "testbucket", Linkname: outside, Mode: 0o777,
		}); err != nil {
			t.Fatal(err)
		}
		for _, closeErr := range []error{tw.Close(), gz.Close(), f.Close()} {
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		}
		b, err := os.ReadFile(archive)
		if err != nil {
			t.Fatal(err)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(b))
		bin, out, err := runCandidateInstaller(t, root, artifact, candidateVersion(t, digest), attested)
		if err == nil {
			ran, _ := exec.Command(bin).Output()
			t.Fatalf("a symlink member was installed and ran %q\n%s", strings.TrimSpace(string(ran)), out)
		}
		if !strings.Contains(out, "not a regular file") {
			t.Errorf("the refusal does not name the member type: %s", out)
		}
	})

	// A GROUP- OR OTHER-EXECUTABLE SIBLING inside the archive is executable
	// too, and the stated rule is one binary.
	t.Run("a second executable by any bit is refused", func(t *testing.T) {
		root := t.TempDir()
		artifact := filepath.Join(root, "artifact")
		if err := os.MkdirAll(artifact, 0o755); err != nil {
			t.Fatal(err)
		}
		archive := filepath.Join(artifact, "candidate.tar.gz")
		f, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		for _, m := range []struct {
			name string
			mode int64
			body string
		}{
			{"testbucket", 0o755, good},
			// executable by OTHER only: the old check looked at -perm -u+x.
			{"helper", 0o601, "#!/bin/sh\nprintf 'OTHER\\n'\n"},
		} {
			if err := tw.WriteHeader(&tar.Header{Name: m.name, Mode: m.mode, Size: int64(len(m.body))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(m.body)); err != nil {
				t.Fatal(err)
			}
		}
		for _, closeErr := range []error{tw.Close(), gz.Close(), f.Close()} {
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		}
		b, err := os.ReadFile(archive)
		if err != nil {
			t.Fatal(err)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(b))
		_, out, err := runCandidateInstaller(t, root, artifact, candidateVersion(t, digest), attested)
		if err == nil {
			t.Fatalf("an archive with an other-executable sibling was accepted\n%s", out)
		}
		if !strings.Contains(out, "one binary") {
			t.Errorf("the refusal does not name the extra executable: %s", out)
		}
	})
}
