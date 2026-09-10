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

// releaseAsset is the archive name the installer derives for a given tag.
func releaseAsset(tag string) string {
	return fmt.Sprintf("testbucket_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH)
}

// stageRelease writes a release archive plus its checksums.txt into a staging
// directory the stubbed curl serves from.
func stageRelease(t *testing.T, root, tag string, members map[string]string) string {
	t.Helper()
	stage := filepath.Join(root, "release")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	asset := releaseAsset(tag)
	path := filepath.Join(stage, asset)
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
	for _, closer := range []func() error{tw.Close, gz.Close, f.Close} {
		if err := closer(); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sums := fmt.Sprintf("%x  %s\n", sha256.Sum256(b), asset)
	if err := os.WriteFile(filepath.Join(stage, "checksums.txt"), []byte(sums), 0o644); err != nil {
		t.Fatal(err)
	}
	return stage
}

// runReleaseInstaller drives the installer's RELEASE path with a stubbed curl
// that serves the staged directory, and reports its exit status and output.
//
// The candidate delivery path this harness replaces is gone: it downloaded a
// pre-publication build artifact and is the unpublished-candidate authority
// chain the component map removes. The archive validation it carried is not
// candidate-specific and now runs here, which is what these tests exercise.
func runReleaseInstaller(t *testing.T, script, root, stage, tag string) (int, string, string) {
	t.Helper()
	fakeBin := filepath.Join(root, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	// -o <dest> ... <url>: serve the staged file whose name matches dest.
	curl := "#!/bin/sh\ndest=\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"-o\" ]; then shift; dest=\"$1\"; fi\n  shift\ndone\n" +
		"[ -n \"$dest\" ] || exit 1\nname=$(basename \"$dest\")\n" +
		"[ -f \"$TB_TEST_STAGE/$name\" ] || exit 22\n/bin/cp \"$TB_TEST_STAGE/$name\" \"$dest\"\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "curl"), []byte(curl), 0o755); err != nil {
		t.Fatal(err)
	}
	bindir := filepath.Join(root, "bin")
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TB_TEST_STAGE="+stage,
		"TB_BINDIR="+bindir,
		"TB_REPO=example/testbucket",
		"TB_VERSION="+tag,
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

const installerScript = "../../.github/actions/install-testbucket.sh"

// TestTheReleaseInstallerInstallsOnlyTheArchivesOwnBinary is the retained
// installer kernel: what the archive contains is what runs.
//
// These refusals were written on the removed candidate path, where a digest
// named the archive in advance. They are not candidate-specific — an archive
// with a second executable, a symlinked member or no member at all is
// ambiguous however it was delivered — so they now run on the one delivery
// path that remains.
func TestTheReleaseInstallerInstallsOnlyTheArchivesOwnBinary(t *testing.T) {
	const tag = "v9.9.9"
	const good = "#!/bin/sh\nprintf 'RELEASED\\n'\n"

	t.Run("an ordinary release archive installs its testbucket member", func(t *testing.T) {
		root := t.TempDir()
		stage := stageRelease(t, root, tag, map[string]string{"testbucket": good})
		code, out, bin := runReleaseInstaller(t, installerScript, root, stage, tag)
		if code != 0 {
			t.Fatalf("a well-formed release archive did not install (exit %d):\n%s", code, out)
		}
		b, err := os.ReadFile(bin)
		if err != nil {
			t.Fatalf("nothing was installed: %v", err)
		}
		if string(b) != good {
			t.Error("the installed bytes are not the archive's testbucket member")
		}
	})

	t.Run("a second executable member is refused", func(t *testing.T) {
		root := t.TempDir()
		stage := stageRelease(t, root, tag, map[string]string{
			"testbucket": good,
			"helper":     "#!/bin/sh\nprintf 'OTHER\\n'\n",
		})
		code, out, bin := runReleaseInstaller(t, installerScript, root, stage, tag)
		if code == 0 {
			t.Fatalf("an archive carrying two executables installed:\n%s", out)
		}
		if !strings.Contains(out, "carries one binary") {
			t.Errorf("the refusal does not say why:\n%s", out)
		}
		if _, err := os.Stat(bin); err == nil {
			t.Error("the installer refused and installed a binary anyway")
		}
	})

	t.Run("an archive with no testbucket member is refused", func(t *testing.T) {
		root := t.TempDir()
		stage := stageRelease(t, root, tag, map[string]string{"notes.txt": "nothing here"})
		code, out, _ := runReleaseInstaller(t, installerScript, root, stage, tag)
		if code == 0 {
			t.Fatalf("an archive with no testbucket member installed:\n%s", out)
		}
		if !strings.Contains(out, "no testbucket member") {
			t.Errorf("the refusal does not name the missing member:\n%s", out)
		}
	})

	t.Run("a traversing member name is refused", func(t *testing.T) {
		root := t.TempDir()
		stage := stageRelease(t, root, tag, map[string]string{
			"testbucket":     good,
			"../escape.conf": "written outside the extraction root",
		})
		code, out, _ := runReleaseInstaller(t, installerScript, root, stage, tag)
		if code == 0 {
			t.Fatalf("an archive with a traversing path installed:\n%s", out)
		}
		if !strings.Contains(out, "traversing path") {
			t.Errorf("the refusal does not name the traversal:\n%s", out)
		}
	})

	t.Run("a mismatched checksum is refused before extraction", func(t *testing.T) {
		root := t.TempDir()
		stage := stageRelease(t, root, tag, map[string]string{"testbucket": good})
		sums := filepath.Join(stage, "checksums.txt")
		if err := os.WriteFile(sums,
			[]byte(strings.Repeat("0", 64)+"  "+releaseAsset(tag)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out, bin := runReleaseInstaller(t, installerScript, root, stage, tag)
		if code == 0 {
			t.Fatalf("an archive that does not match its checksum installed:\n%s", out)
		}
		if _, err := os.Stat(bin); err == nil {
			t.Error("the installer refused and installed a binary anyway")
		}
	})
}

// TestTheCandidateDeliveryPathIsGone is the removal itself.
//
// `candidate:<run-id>/<artifact>@sha256:<digest>` installed a pre-publication
// build artifact so a scored arm could run a binary no release names. That is
// the unpublished-candidate authority chain the component map removes, and a
// caller that still passes one must be told, not silently served a release.
func TestTheCandidateDeliveryPathIsGone(t *testing.T) {
	b, err := os.ReadFile(installerScript)
	if err != nil {
		t.Fatal(err)
	}
	sh := string(b)
	for _, gone := range []string{
		`TB_VERSION" | grep -q '^candidate:'`,
		"cand_digest",
		"cand_artifact",
	} {
		if strings.Contains(sh, gone) {
			t.Errorf("the candidate delivery path is still wired: %q", gone)
		}
	}
	if strings.Contains(sh, `${TB_CANDIDATE_BINARY_DIGEST:-}`) {
		t.Error("TB_CANDIDATE_BINARY_DIGEST is still read; it is the attested pre-publication delivery")
	}

	root := t.TempDir()
	stage := stageRelease(t, root, "v9.9.9", map[string]string{"testbucket": "#!/bin/sh\n"})
	code, out, _ := runReleaseInstaller(t, installerScript, root, stage,
		"candidate:123/candidate-build-"+runtime.GOOS+"_"+runtime.GOARCH+"@sha256:"+strings.Repeat("a", 64))
	if code == 0 {
		t.Fatalf("a candidate pin was accepted:\n%s", out)
	}
	if !strings.Contains(out, "invalid --version") {
		t.Errorf("a candidate pin is not refused as an unknown version:\n%s", out)
	}
}

// stageLeadingIrregularRelease is leadingIrregularArchive's release-path
// sibling: the same archive — a directory entry FIRST, then the binary, then
// enough filler to overflow a pipe buffer — staged as a published release with
// its checksums, so the enumeration is exercised where it now runs.
func stageLeadingIrregularRelease(t *testing.T, root, tag string, filler int) string {
	t.Helper()
	stage := filepath.Join(root, "release")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	asset := releaseAsset(tag)
	path := filepath.Join(stage, asset)
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
	const binary = "#!/bin/sh\nprintf 'RELEASED\\n'\n"
	write(&tar.Header{Name: "testbucket", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(binary))}, binary)
	for i := 0; i < filler; i++ {
		name := fmt.Sprintf("filler/%06d-%s.txt", i, strings.Repeat("n", 60))
		write(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: 1}, "x")
	}
	for _, closer := range []func() error{tw.Close, gz.Close, f.Close} {
		if err := closer(); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "checksums.txt"),
		[]byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(b), asset)), 0o644); err != nil {
		t.Fatal(err)
	}
	return stage
}
