package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// predecessorInstaller writes the installer exactly as the named revision
// carries it, so a control can drive the predecessor's own bytes rather than a
// reconstruction of them.
func predecessorInstaller(t *testing.T, rev string) string {
	t.Helper()
	// Either source is the predecessor's own bytes. `jj` is the direct route;
	// TB_PREDECESSOR_INSTALLER carries the same file to an environment that
	// has no jj — the Linux VM where this ambiguity is observable — and the
	// digest below is what makes the two the same thing rather than a
	// convenience.
	var raw []byte
	if p := strings.TrimSpace(os.Getenv("TB_PREDECESSOR_INSTALLER")); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("TB_PREDECESSOR_INSTALLER=%s: %v", p, err)
		}
		raw = b
	} else {
		out, err := exec.Command("jj", "file", "show", "-r", rev,
			".github/actions/install-testbucket.sh").Output()
		if err != nil {
			t.Skipf("cannot read the installer at %s and TB_PREDECESSOR_INSTALLER is unset: %v", rev, err)
		}
		raw = out
	}
	// The predecessor is identified by its bytes, not by where they came from.
	if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != predecessorInstallerDigest {
		t.Fatalf("the supplied predecessor installer digests to %s, not the %s recorded for %s", got, predecessorInstallerDigest, rev)
	}
	p := filepath.Join(t.TempDir(), "install-testbucket.sh")
	if err := os.WriteFile(p, raw, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// leadingIrregularArchive stages a candidate artifact whose archive enumerates
// a DIRECTORY first and a regular `testbucket` after it, with enough further
// entries that the listing exceeds a pipe buffer.
//
// The order and the size are both load-bearing. The enumeration pre-check
// reads the listing through a reader that stops at its first match, and a
// short listing is fully written before that reader exits — so the offending
// entry has to come first AND the listing has to be long enough for the
// producer to still be writing when the reader goes away.
func leadingIrregularArchive(t *testing.T, root string, filler int) (string, string) {
	t.Helper()
	artifact := filepath.Join(root, "artifact")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(artifact, "candidate.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	write := func(h *tar.Header, body string) {
		t.Helper()
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if body != "" {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// FIRST: the entry the script declares invalid.
	write(&tar.Header{Name: "leading-dir/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	const binary = "#!/bin/sh\nprintf 'PINNED\\n'\n"
	write(&tar.Header{Name: "testbucket", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(binary))}, binary)
	for i := 0; i < filler; i++ {
		name := fmt.Sprintf("filler/%06d-%s.txt", i, strings.Repeat("n", 60))
		write(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: 1}, "x")
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
	return artifact, fmt.Sprintf("%x", sha256.Sum256(b))
}

// runInstaller drives one installer script over a staged artifact with a
// stubbed `gh`, and reports its exit status and output.
func runInstaller(t *testing.T, script, root, artifact, version, binDigest string) (int, string, string) {
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
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"PATH="+fakeBin+":"+os.Getenv("PATH"),
		"TB_TEST_ARTIFACT="+artifact,
		"TB_BINDIR="+bindir,
		"TB_REPO=example/testbucket",
		"TB_VERSION="+version,
		"TB_CANDIDATE_BINARY_DIGEST="+binDigest,
		"GITHUB_PATH="+filepath.Join(root, "github-path"),
	)
	out, err := cmd.CombinedOutput()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	} else if err != nil {
		t.Fatalf("the installer produced no process state: %v", err)
	}
	return code, string(out), filepath.Join(bindir, "testbucket")
}

// THE ARCHIVE ENUMERATION IS EVALUATED COMPLETELY BEFORE ANYTHING INSTALLS.
//
// The pre-check was `tar … | grep -q …`. `grep -q` stops at its first match
// and exits, closing the pipe; `tar` is then killed by SIGPIPE and exits 141,
// and under `set -o pipefail` the PIPELINE's status is that 141 rather than
// grep's 0. The `if` therefore read false in exactly the case the check exists
// for — an offending FIRST entry — and the archive installed. A short listing
// hid it, because tar finishes writing before grep exits.
//
// The successor captures each listing in one command substitution and walks
// every line, so the outcome no longer depends on which line matched or on
// when a reader closed its input.
const leadingIrregularFiller = 4000

// predecessorInstallerDigest is the sha256 of `.github/actions/install-testbucket.sh`
// as EWJ-2R53 (9537850eed193b9dfe29dce272d43b87d1f4bbf9) carries it. The
// control is about those exact bytes, so the test refuses any other file
// offered in their place.
const predecessorInstallerDigest = "a73c0c5106e341f855af065ad1b110da4c482bd2658c5da7f3d19e12d7478937"

func TestTheArchiveEnumerationRefusesALeadingIrregularEntry(t *testing.T) {
	if runtime.GOOS != "linux" {
		// The producer only blocks on write when the listing exceeds the pipe
		// buffer, and the buffer and tar's own buffering differ by platform.
		// The successor is asserted everywhere; the predecessor control needs
		// the platform where the ambiguity is observable.
		t.Skip("the pipeline early-exit ambiguity is observable on Linux")
	}
	const good = "#!/bin/sh\nprintf 'PINNED\\n'\n"
	binDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(good)))

	t.Run("the successor refuses it and installs nothing", func(t *testing.T) {
		root := t.TempDir()
		artifact, digest := leadingIrregularArchive(t, root, leadingIrregularFiller)
		code, out, bin := runInstaller(t,
			filepath.Join("..", "..", ".github", "actions", "install-testbucket.sh"),
			root, artifact, candidateVersion(t, digest), binDigest)
		if code == 0 {
			t.Fatalf("an archive whose first entry is not a regular file installed (exit 0):\n%s", out)
		}
		if !strings.Contains(out, "not regular files") {
			t.Errorf("the refusal does not name the enumeration result:\n%s", out)
		}
		if _, err := os.Stat(bin); err == nil {
			t.Error("the installer refused and installed a binary anyway")
		}
	})

	t.Run("the predecessor admits it and exits 0", func(t *testing.T) {
		const predecessor = "9537850eed193b9dfe29dce272d43b87d1f4bbf9"
		script := predecessorInstaller(t, predecessor)
		root := t.TempDir()
		artifact, digest := leadingIrregularArchive(t, root, leadingIrregularFiller)
		code, out, bin := runInstaller(t, script, root, artifact,
			candidateVersion(t, digest), binDigest)
		if code != 0 {
			t.Fatalf("the predecessor control did not reproduce: %s exited %d, so this test would pass for the wrong reason\n%s", predecessor, code, out)
		}
		if _, err := os.Stat(bin); err != nil {
			t.Errorf("the predecessor exited 0 but installed nothing; the control is not the one described: %v", err)
		}
	})

	// AND A WELL-FORMED ARCHIVE STILL INSTALLS, so the successor is not simply
	// refusing everything.
	t.Run("a regular-file archive is unaffected", func(t *testing.T) {
		root := t.TempDir()
		artifact, digest := candidateArtifact(t, root, map[string]string{"testbucket": good}, nil, 1)
		code, out, bin := runInstaller(t,
			filepath.Join("..", "..", ".github", "actions", "install-testbucket.sh"),
			root, artifact, candidateVersion(t, digest), binDigest)
		if code != 0 {
			t.Fatalf("a well-formed pinned archive was refused (exit %d):\n%s", code, out)
		}
		ran, err := exec.Command(bin).Output()
		if err != nil || strings.TrimSpace(string(ran)) != "PINNED" {
			t.Errorf("the installed binary is not the pinned member: %q %v", string(ran), err)
		}
	})
}

// AND THE DECISION CONTAINS NO PIPELINE. The status of `a | b` under pipefail
// is not the status of the test the script means to perform, and a comment
// saying so is not what stops it from coming back.
func TestTheArchiveEnumerationUsesNoShortCircuitingPipeline(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "install-testbucket.sh"))
	if err != nil {
		t.Fatal(err)
	}
	sh := string(b)
	for _, forbidden := range []string{
		`tar -tzvf "$archive" | grep`,
		`tar -tzf "$archive" | grep`,
	} {
		if strings.Contains(sh, forbidden) {
			t.Errorf("the archive enumeration decision is made through %q; under pipefail the pipeline reports the producer's SIGPIPE rather than the test's own answer", forbidden)
		}
	}
	for _, want := range []string{
		`entry_listing=$(tar -tzvf "$archive")`,
		`name_listing=$(tar -tzf "$archive")`,
		"irregular_count",
		"unsafe_count",
	} {
		if !strings.Contains(sh, want) {
			t.Errorf("the fully-evaluating enumeration is missing %q", want)
		}
	}
	// The installed-byte checks that follow are untouched.
	for _, keep := range []string{
		"TB_CANDIDATE_BINARY_DIGEST",
		"the installed candidate binary digests to",
		"a candidate archive carries one binary",
		"not the precommitted",
	} {
		if !strings.Contains(sh, keep) {
			t.Errorf("an installed-byte identity check was lost: %q", keep)
		}
	}
}
