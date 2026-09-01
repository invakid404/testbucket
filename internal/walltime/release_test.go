package walltime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// goreleaserManifest is the committed representative artifact manifest: four
// Binary rows, four Archives built from them, one Checksum.
func goreleaserManifest(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "goreleaser", "artifacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// buildDist materialises the files that manifest describes: each archive
// really contains a LICENSE, a README and the platform binary, so the
// derivation is exercised against real gzip/tar bytes rather than a stub.
// It returns the root and the digest of the linux/amd64 binary — the one a
// campaign arm would have been delivered.
func buildDist(t *testing.T) (string, Digest) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	var delivered Digest
	for _, p := range []struct{ goos, goarch, dir string }{
		{"linux", "amd64", "testbucket_linux_amd64_v1"},
		{"linux", "arm64", "testbucket_linux_arm64_v8.0"},
		{"darwin", "amd64", "testbucket_darwin_amd64_v1"},
		{"darwin", "arm64", "testbucket_darwin_arm64_v8.0"},
	} {
		binary := []byte("testbucket " + p.goos + "/" + p.goarch + " ELF")
		if p.goos == "linux" && p.goarch == "amd64" {
			delivered = DigestBytes(binary)
		}
		bindir := filepath.Join(root, "dist", p.dir)
		if err := os.MkdirAll(bindir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bindir, "testbucket"), binary, 0o755); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(zw)
		for _, f := range []struct {
			name string
			data []byte
		}{{"LICENSE", []byte("MIT")}, {"README.md", []byte("# testbucket")}, {"testbucket", binary}} {
			if err := tw.WriteHeader(&tar.Header{
				Name: f.name, Mode: 0o644, Size: int64(len(f.data)), Typeflag: tar.TypeReg,
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(f.data); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		name := "testbucket_0.3.0_" + p.goos + "_" + p.goarch + ".tar.gz"
		if err := os.WriteFile(filepath.Join(root, "dist", name), buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "checksums.txt"), []byte("...checksums..."), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, delivered
}

// TestTheGatedSetIsThePublishedSet is the F1 regression.
//
// The gate used to select goreleaser's Binary, Archive AND Checksum rows while
// the publisher globbed `dist/*.tar.gz` plus `dist/checksums.txt`. The two sets
// therefore differed by exactly the four raw platform binaries — gated, never
// uploaded — so the campaign's delivered-binary match could be satisfied by a
// file no consumer receives, while the assets people actually download were
// bound to nothing.
//
// There is now ONE derivation. This proves the set it produces contains no
// unpublished intermediate and every uploadable file.
func TestTheGatedSetIsThePublishedSet(t *testing.T) {
	root, _ := buildDist(t)
	m, err := DeriveReleaseManifest(root, goreleaserManifest(t))
	if err != nil {
		t.Fatalf("DeriveReleaseManifest: %v", err)
	}
	got := strings.Join(m.UploadNames(), "\n")
	want := strings.Join([]string{
		"checksums.txt",
		"testbucket_0.3.0_darwin_amd64.tar.gz",
		"testbucket_0.3.0_darwin_arm64.tar.gz",
		"testbucket_0.3.0_linux_amd64.tar.gz",
		"testbucket_0.3.0_linux_arm64.tar.gz",
	}, "\n")
	if got != want {
		t.Errorf("the publish set is\n%s\nwant\n%s", got, want)
	}
	// No raw goreleaser intermediate is in the set. This is the exact file
	// that used to be gated and never published.
	for _, a := range m.Assets {
		if strings.Contains(a.Path, "_v1/") || strings.Contains(a.Path, "_v8.0/") {
			t.Errorf("the publish set contains the raw intermediate %s, which nothing uploads", a.Path)
		}
	}
	if problems := m.Verify(root); len(problems) > 0 {
		t.Errorf("the derived manifest does not describe its own files: %v", problems)
	}
}

// TestTheDeliveredBinaryIsBoundToAPublishedAsset: the campaign binds the
// measured EXECUTABLE and a release ships ARCHIVES, so the two facts can only
// meet through the archive's contents. Hashing the members is what lets the
// gate say where the measured binary is actually published.
func TestTheDeliveredBinaryIsBoundToAPublishedAsset(t *testing.T) {
	root, delivered := buildDist(t)
	m, err := DeriveReleaseManifest(root, goreleaserManifest(t))
	if err != nil {
		t.Fatalf("DeriveReleaseManifest: %v", err)
	}
	asset, member, ok := m.Locate(delivered)
	if !ok {
		t.Fatal("the delivered linux/amd64 binary is not reachable from anything this release publishes")
	}
	if asset != "testbucket_0.3.0_linux_amd64.tar.gz" || member != "testbucket" {
		t.Errorf("the delivered binary was located at %s/%s", asset, member)
	}
	// A binary that was never built is not published, however plausible.
	if _, _, ok := m.Locate(DigestBytes([]byte("a later, differently built binary"))); ok {
		t.Error("a binary this release does not carry was reported as published")
	}
}

// TestAnEditedReleaseManifestIsRefused: the manifest is a document, so the gate
// re-derives every digest from the files it names. A hand-written manifest
// claiming the campaign's binary sits inside an archive that does not contain
// it must not pass.
func TestAnEditedReleaseManifestIsRefused(t *testing.T) {
	root, _ := buildDist(t)
	base, err := DeriveReleaseManifest(root, goreleaserManifest(t))
	if err != nil {
		t.Fatalf("DeriveReleaseManifest: %v", err)
	}
	if problems := base.Verify(root); len(problems) > 0 {
		t.Fatalf("the genuine manifest does not verify: %v", problems)
	}
	for _, tc := range []struct {
		name string
		edit func(*ReleaseManifest)
		want string
	}{
		{"a forged asset digest", func(m *ReleaseManifest) {
			m.Assets[0].Digest = DigestBytes([]byte("something else"))
		}, "hashes to"},
		{"a member the archive does not contain", func(m *ReleaseManifest) {
			for i := range m.Assets {
				if len(m.Assets[i].Contains) > 0 {
					m.Assets[i].Contains = append(m.Assets[i].Contains,
						ReleaseAssetMember{Name: "smuggled", Digest: DigestBytes([]byte("smuggled"))})
					return
				}
			}
		}, "which it does not contain"},
		{"a forged member digest", func(m *ReleaseManifest) {
			for i := range m.Assets {
				for j := range m.Assets[i].Contains {
					if m.Assets[i].Contains[j].Name == "testbucket" {
						m.Assets[i].Contains[j].Digest = DigestBytes([]byte("a later, differently built binary"))
						return
					}
				}
			}
		}, "not the recorded"},
		{"an asset that is not on disk", func(m *ReleaseManifest) {
			m.Assets[0].Path = "dist/never-built.tar.gz"
		}, "no such file"},
		{"the wrong kind", func(m *ReleaseManifest) { m.Kind = "tb.walltime.something-else/v1" }, "kind"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := *base
			m.Assets = append([]ReleaseAsset(nil), base.Assets...)
			for i := range m.Assets {
				m.Assets[i].Contains = append([]ReleaseAssetMember(nil), base.Assets[i].Contains...)
			}
			tc.edit(&m)
			problems := m.Verify(root)
			if len(problems) == 0 {
				t.Fatalf("Verify accepted %s", tc.name)
			}
			if !strings.Contains(strings.Join(problems, "; "), tc.want) {
				t.Errorf("no problem mentions %q:\n%s", tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}

// TestTheReleaseWorkflowUsesOneSelector is the workflow-level half of F1.
//
// The Go layer above cannot see a workflow that derives the set correctly and
// then uploads something else, and that is precisely the shape the defect took.
// This reads the committed workflow and requires that neither the gate nor the
// publisher selects files on its own: the manifest is derived once, and both
// steps refer to it.
func TestTheReleaseWorkflowUsesOneSelector(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yaml := string(b)

	// No independent file selection anywhere in the release job. A glob or a
	// second jq expression over artifacts.json is a second selector, and two
	// selectors can disagree.
	for _, forbidden := range []string{
		"dist/*.tar.gz",
		"dist/*",
		"artifacts.json",
	} {
		if strings.Contains(yaml, forbidden) {
			t.Errorf("release.yml selects release files with %q; the publish set must come from the release manifest alone", forbidden)
		}
	}
	// Both consumers name the same manifest.
	if n := strings.Count(yaml, "$TB_RELEASE_MANIFEST"); n < 2 {
		t.Errorf("release.yml refers to the release manifest %d time(s); the gate and the publisher must both read it", n)
	}
	if !strings.Contains(yaml, "wall release-manifest") {
		t.Error("release.yml never derives the canonical publish set with `wall release-manifest`")
	}
	if !strings.Contains(yaml, "--release-manifest") {
		t.Error("release.yml never hands the publish set to the campaign gate")
	}
	// And the campaign gate still precedes publication.
	gate := strings.Index(yaml, "--release-manifest")
	publish := strings.Index(yaml, "gh release create")
	if gate < 0 || publish < 0 || gate > publish {
		t.Error("the campaign gate does not precede publication")
	}
	// Publication must upload the manifest's own names, not a rediscovered set.
	uploads := regexp.MustCompile(`gh release (create|upload)[^\n]*`)
	for _, line := range uploads.FindAllString(yaml, -1) {
		if strings.Contains(line, "dist/") {
			t.Errorf("the publisher selects files itself: %s", line)
		}
	}
}
