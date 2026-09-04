package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
			"candidate:123/candidate-build@sha256:"+digest)
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
			"candidate:123/candidate-build@sha256:"+digest)
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
			"candidate:123/candidate-build@sha256:"+digest)
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
			"candidate:123/candidate-build@sha256:"+digest)
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
			"candidate:123/candidate-build@sha256:"+strings.Repeat("a", 64))
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
			"candidate:123/candidate-build@sha256:"+digest,
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
			"candidate:123/candidate-build@sha256:"+digest2,
			"TB_CANDIDATE_BINARY_DIGEST=sha256:"+want); err != nil {
			t.Errorf("the attested binary was refused: %v\n%s", err, out)
		}
	})
}
