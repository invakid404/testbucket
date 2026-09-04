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

// A RELEASE CANNOT CONTAIN ITS OWN DIGEST, AND DOES NOT HAVE TO.
//
// GoReleaser embeds the tag commit and its timestamp in the binary, so the
// bytes do not exist until the tag does and a pin committed beforehand would
// have to predict its own hash. The colocated file pins releases published
// before the commit it lives in; the newest release is pinned by the commit
// that comes AFTER it, named as a full 40-hex commit SHA — immutable, and
// therefore still a root the release cannot move.
func TestALaterCommitMaySupplyAReleasePin(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "install-testbucket.sh"))
	if err != nil {
		t.Fatal(err)
	}
	sh := string(b)
	for _, want := range []string{
		"TB_RELEASE_PINS_REF",
		"not a full 40-hex commit SHA",
		"A pin fetched through a branch or a tag is a pin whoever can move that ref controls.",
		"Two reviewed roots disagreeing about one release's bytes is not a tie to break.",
	} {
		if !strings.Contains(sh, want) {
			t.Errorf("the installer is missing %q", want)
		}
	}
	// Only a full commit SHA: a branch or tag would put the pin back under the
	// control of whoever can move that ref.
	if !strings.Contains(sh, `grep -Eq '^[0-9a-f]{40}$'`) {
		t.Error("the pins ref is not constrained to a full commit SHA")
	}

	// EVERY PUBLIC ACTION CAN CARRY IT, or the supported route cannot use it.
	for _, a := range publicActions {
		body, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", a, "action.yml"))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, "release-pins-ref:") {
			t.Errorf("the %s action has no release-pins-ref input", a)
		}
		if !strings.Contains(text, "TB_RELEASE_PINS_REF: ${{ inputs.release-pins-ref }}") {
			t.Errorf("the %s action never exports TB_RELEASE_PINS_REF", a)
		}
	}

	// AND THE SECOND PHASE EXISTS, proposes rather than commits, and holds a
	// read-only token.
	pin, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "pin-release.yml"))
	if err != nil {
		t.Fatal("the two-phase pin workflow is missing: a release published after the action's commit could never be pinned")
	}
	text := string(pin)
	for _, want := range []string{
		"contents: read",
		"in a reviewed change",
		"already pinned",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the pin workflow is missing %q", want)
		}
	}
	for _, forbidden := range []string{"git push", "gh pr create", "contents: write"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("the pin workflow contains %q; the pin's value is that a person reviewed it, so this job proposes and does not write", forbidden)
		}
	}
}
