package walltime

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ReleaseManifestKind identifies the canonical publish set.
const ReleaseManifestKind = "tb.walltime.release-manifest/v1"

// ReleaseManifest is THE publish set: the exact files a release will upload,
// derived once and used by everything downstream.
//
// It exists because the gate and the uploader used to select their files
// independently. The gate hashed every Binary, Archive and Checksum row of
// goreleaser's manifest; the uploader globbed `dist/*.tar.gz` plus
// `dist/checksums.txt`. The two sets were therefore not the same set, and the
// difference was exactly the raw platform binaries — which meant a raw file
// that is never uploaded could satisfy the campaign's delivered-binary match
// while the assets people actually download were bound to nothing.
//
// One derivation, one array, two consumers. A release cannot gate one set and
// publish another because there is only one set.
type ReleaseManifest struct {
	Kind string `json:"kind"`
	// Assets are the files that will be uploaded, sorted by upload name so the
	// document is stable across derivations.
	Assets []ReleaseAsset `json:"assets"`
}

// ReleaseAsset is one uploaded file.
type ReleaseAsset struct {
	// Name is what the asset is published as; Path is where it was hashed
	// from.
	Name   string `json:"name"`
	Path   string `json:"path"`
	Digest Digest `json:"digest"`
	// Contains is what an archive holds, member by member.
	//
	// The campaign binds the MEASURED EXECUTABLE, and a release publishes
	// archives. Without the members those two facts can never meet: the
	// binary the campaign ran is not itself an asset, so a gate that compared
	// only asset digests could never match it — and a gate that was handed the
	// raw binary as if it were an asset matched something nobody publishes.
	// Hashing the members binds the executable to the archive that actually
	// carries it.
	Contains []ReleaseAssetMember `json:"contains,omitempty"`
}

// ReleaseAssetMember is one file inside an archive.
type ReleaseAssetMember struct {
	Name   string `json:"name"`
	Digest Digest `json:"digest"`
}

// GoreleaserArtifact is the subset of a goreleaser `dist/artifacts.json` row
// this needs. Reading goreleaser's own manifest rather than globbing is
// deliberate: a glob that missed a produced file would gate a subset of what
// is published, which is the defect one layer down.
type GoreleaserArtifact struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Name string `json:"name"`
}

// PublishedArtifactTypes are the goreleaser artifact types a release uploads.
//
// A raw `Binary` is deliberately NOT one of them. Goreleaser produces it as an
// intermediate on the way to the archive; nothing uploads it, and treating it
// as a publishable asset is what let an unpublished file satisfy the gate.
var PublishedArtifactTypes = []string{"Archive", "Checksum", "Signature", "Checksum Signature"}

func isPublishedType(t string) bool {
	for _, p := range PublishedArtifactTypes {
		if t == p {
			return true
		}
	}
	return false
}

// DeriveReleaseManifest reads goreleaser's own artifact manifest, selects the
// files a release uploads, and hashes each of them — plus, for archives, every
// regular file inside.
//
// root is the directory `dist` sits in, so every path in the manifest stays
// relative to it and the document is reproducible from a different checkout.
func DeriveReleaseManifest(root string, artifactsJSON []byte) (*ReleaseManifest, error) {
	var rows []GoreleaserArtifact
	if err := json.Unmarshal(artifactsJSON, &rows); err != nil {
		return nil, fmt.Errorf("parse the goreleaser artifact manifest: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("the goreleaser artifact manifest is empty; there is nothing to publish or to gate")
	}
	m := &ReleaseManifest{Kind: ReleaseManifestKind}
	for _, row := range rows {
		if !isPublishedType(row.Type) {
			continue
		}
		name := row.Name
		if strings.TrimSpace(name) == "" {
			name = filepath.Base(row.Path)
		}
		asset := ReleaseAsset{Name: name, Path: row.Path}
		digest, err := FileDigest(filepath.Join(root, row.Path))
		if err != nil {
			return nil, fmt.Errorf("hash the published asset %s: %w", row.Path, err)
		}
		asset.Digest = digest
		if isTarGz(row.Path) {
			members, err := tarGzMembers(filepath.Join(root, row.Path))
			if err != nil {
				return nil, fmt.Errorf("hash the members of %s: %w", row.Path, err)
			}
			asset.Contains = members
		}
		m.Assets = append(m.Assets, asset)
	}
	if len(m.Assets) == 0 {
		return nil, fmt.Errorf("the goreleaser artifact manifest names no publishable asset (types: %s)", strings.Join(PublishedArtifactTypes, ", "))
	}
	sort.Slice(m.Assets, func(i, j int) bool { return m.Assets[i].Name < m.Assets[j].Name })
	return m, nil
}

func isTarGz(path string) bool {
	return strings.HasSuffix(path, ".tar.gz") || strings.HasSuffix(path, ".tgz")
}

// tarGzMembers hashes every regular file in a gzipped tar, sorted by name.
func tarGzMembers(path string) ([]ReleaseAssetMember, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var out []ReleaseAssetMember
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		sum := sha256.New()
		if _, err := io.Copy(sum, tr); err != nil {
			return nil, err
		}
		out = append(out, ReleaseAssetMember{
			Name: h.Name, Digest: Digest("sha256:" + hex.EncodeToString(sum.Sum(nil))),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Verify re-derives every digest in the manifest from the files on disk.
//
// The manifest is a document, and a document can be edited. The gate reads it
// and then checks it against the bytes it describes, so a hand-written manifest
// claiming the campaign's binary is inside an archive that does not contain it
// is refused rather than believed.
func (m ReleaseManifest) Verify(root string) []string {
	var problems []string
	if m.Kind != ReleaseManifestKind {
		problems = append(problems, fmt.Sprintf("release manifest kind %q, want %q", m.Kind, ReleaseManifestKind))
	}
	if len(m.Assets) == 0 {
		return append(problems, "the release manifest names no asset, so there is nothing to publish or to gate")
	}
	seen := map[string]bool{}
	for _, a := range m.Assets {
		if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.Path) == "" {
			problems = append(problems, fmt.Sprintf("an asset is missing its name or path: %+v", a))
			continue
		}
		if seen[a.Name] {
			problems = append(problems, fmt.Sprintf("two assets would be published as %q", a.Name))
		}
		seen[a.Name] = true
		digest, err := FileDigest(filepath.Join(root, a.Path))
		if err != nil {
			problems = append(problems, fmt.Sprintf("asset %s: %v", a.Name, err))
			continue
		}
		if digest != a.Digest {
			problems = append(problems, fmt.Sprintf("asset %s is recorded as %s but hashes to %s", a.Name, a.Digest, digest))
			continue
		}
		if len(a.Contains) == 0 {
			continue
		}
		members, err := tarGzMembers(filepath.Join(root, a.Path))
		if err != nil {
			problems = append(problems, fmt.Sprintf("asset %s: %v", a.Name, err))
			continue
		}
		have := map[string]Digest{}
		for _, mem := range members {
			have[mem.Name] = mem.Digest
		}
		for _, mem := range a.Contains {
			got, ok := have[mem.Name]
			if !ok {
				problems = append(problems, fmt.Sprintf("asset %s is recorded as containing %s, which it does not contain", a.Name, mem.Name))
				continue
			}
			if got != mem.Digest {
				problems = append(problems, fmt.Sprintf("asset %s contains %s as %s, not the recorded %s", a.Name, mem.Name, got, mem.Digest))
			}
		}
	}
	return problems
}

// UploadNames is the exact list a publisher uploads, in manifest order. The
// publisher reads this rather than selecting files itself, which is what makes
// "gated" and "published" the same set by construction.
func (m ReleaseManifest) UploadNames() []string {
	out := make([]string, 0, len(m.Assets))
	for _, a := range m.Assets {
		out = append(out, a.Name)
	}
	return out
}

// Locate finds a digest among the published assets, or inside one of them, and
// says where it was found. An empty asset name means it was not published at
// all.
func (m ReleaseManifest) Locate(d Digest) (asset string, member string, found bool) {
	for _, a := range m.Assets {
		if a.Digest == d {
			return a.Name, "", true
		}
	}
	for _, a := range m.Assets {
		for _, mem := range a.Contains {
			if mem.Digest == d {
				return a.Name, mem.Name, true
			}
		}
	}
	return "", "", false
}
