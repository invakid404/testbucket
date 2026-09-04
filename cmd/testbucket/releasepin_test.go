package main

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pinnedReleaseDigests reads the repository-committed digest file.
func pinnedReleaseDigests(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", ".github", "actions", "released-binary-digests.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			t.Errorf("malformed pin line %q: want <tag>\\t<os>_<arch>\\tsha256:<hex>", line)
			continue
		}
		out[parts[0]+"/"+parts[1]] = parts[2]
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// A RELEASED BINARY IS CHECKED AGAINST A ROOT OUTSIDE THE RELEASE.
//
// The installer verified the platform archive against `checksums.txt` — and
// both are assets of the SAME release. `gh release upload --clobber` can
// replace them together, and the Releases API reports every current testbucket
// release as `immutable: false`, so an actor able to publish assets can swap
// the pair in one step and that check still passes. A tag is metadata, and the
// archive was being authenticated against metadata that moves with it.
func TestTheReleasedBinaryDigestsArePinnedInTheRepository(t *testing.T) {
	pins := pinnedReleaseDigests(t)
	if len(pins) == 0 {
		t.Fatal("no released binary digests are pinned, so every released install rests on co-mutable release metadata")
	}
	hex := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	for k, v := range pins {
		if !hex.MatchString(v) {
			t.Errorf("pin %s is %q, not sha256:<64 lower-case hex>", k, v)
		}
	}
	// Every platform this project releases must be pinned for the release the
	// moving default resolves to; a partial pin is a platform that installs
	// unchecked.
	for _, p := range []string{"linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"} {
		if _, ok := pins["v0.2.2/"+p]; !ok {
			t.Errorf("v0.2.2/%s is not pinned, and v0.2.2 is what the moving v0 default resolves to", p)
		}
	}

	// AND THE INSTALLER ACTUALLY USES THEM, before it installs anything.
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "install-testbucket.sh"))
	if err != nil {
		t.Fatal(err)
	}
	sh := string(b)
	use := strings.Index(sh, "released-binary-digests.tsv")
	inst := strings.Index(sh, `install -m 0755 "$work/testbucket" "$bin"`)
	if use < 0 {
		t.Fatal("the installer never reads the precommitted digests")
	}
	if inst < 0 {
		t.Fatal("the installer no longer installs a released binary; this test is looking at the wrong path")
	}
	if use > inst {
		t.Errorf("the precommitted digest is read at %d, after the binary is installed at %d", use, inst)
	}
	for _, want := range []string{
		"no precommitted binary digest for",
		"not the precommitted",
	} {
		if !strings.Contains(sh, want) {
			t.Errorf("the installer has no refusal for %q", want)
		}
	}
}

// AND A REPLACED BINARY IS REFUSED.
//
// This drives the shipped installer over a fake release whose archive and
// checksums.txt agree with each other — exactly what an actor replacing both
// assets produces — and requires the precommitted digest to catch it.
func TestATamperedReleasedBinaryIsRefused(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	root := t.TempDir()

	// A stand-in release server: a directory served over file:// through a
	// curl shim, so the real installer runs unmodified.
	rel := filepath.Join(root, "release")
	if err := os.MkdirAll(rel, 0o755); err != nil {
		t.Fatal(err)
	}
	const body = "#!/bin/sh\nprintf 'TAMPERED\\n'\n"
	binDir := filepath.Join(root, "stage")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "testbucket"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	asset := "testbucket_0.2.2_" + hostOSArch(t) + ".tar.gz"
	tarCmd := exec.Command("tar", "-czf", filepath.Join(rel, asset), "-C", binDir, "testbucket")
	if out, err := tarCmd.CombinedOutput(); err != nil {
		t.Fatalf("stage the archive: %v\n%s", err, out)
	}
	archive, err := os.ReadFile(filepath.Join(rel, asset))
	if err != nil {
		t.Fatal(err)
	}
	// The checksum AGREES with the tampered archive: replacing both together is
	// the whole point of the finding.
	if err := os.WriteFile(filepath.Join(rel, "checksums.txt"),
		[]byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), asset)), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := filepath.Join(root, "fake-bin")
	if err := os.MkdirAll(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	curl := "#!/bin/sh\nout=\nurl=\nwhile [ \"$#\" -gt 0 ]; do\n  case \"$1\" in\n    -o) shift; out=\"$1\" ;;\n    http*) url=\"$1\" ;;\n  esac\n  shift\ndone\n/bin/cp \"$TB_TEST_RELEASE_DIR/$(basename \"$url\")\" \"$out\"\n"
	if err := os.WriteFile(filepath.Join(fake, "curl"), []byte(curl), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", filepath.Join("..", "..", ".github", "actions", "install-testbucket.sh"))
	cmd.Env = append(os.Environ(),
		"PATH="+fake+":"+os.Getenv("PATH"),
		"TB_TEST_RELEASE_DIR="+rel,
		"TB_VERSION=v0.2.2",
		"TB_REPO=invakid404/testbucket",
		"TB_BINDIR="+filepath.Join(root, "bin"),
		"GITHUB_PATH="+filepath.Join(root, "github-path"),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a replaced release binary whose checksums.txt agreed with it was installed:\n%s", out)
	}
	if !strings.Contains(string(out), "not the precommitted") {
		t.Errorf("the refusal does not name the precommitted digest:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(root, "bin", "testbucket")); statErr == nil {
		t.Error("the tampered binary was installed anyway")
	}
}

func hostOSArch(t *testing.T) string {
	t.Helper()
	switch {
	case strings.Contains(strings.ToLower(os.Getenv("RUNNER_OS")), "linux"):
		return "linux_amd64"
	default:
		// The installer derives this from uname; mirror it the same way.
		u, err := exec.Command("uname", "-s").Output()
		if err != nil {
			t.Fatal(err)
		}
		a, err := exec.Command("uname", "-m").Output()
		if err != nil {
			t.Fatal(err)
		}
		osName := "linux"
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(u))), "darwin") {
			osName = "darwin"
		}
		arch := "amd64"
		switch strings.TrimSpace(string(a)) {
		case "arm64", "aarch64":
			arch = "arm64"
		}
		return osName + "_" + arch
	}
}
