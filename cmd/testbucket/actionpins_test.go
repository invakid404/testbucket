package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// usesRef captures every `uses:` reference in a workflow or composite action.
var usesRef = regexp.MustCompile(`(?m)^\s*(?:-\s*)?uses:\s*(\S+)`)

// fullSHARef is a 40-hex commit pin.
var fullSHARef = regexp.MustCompile(`^[0-9a-f]{40}$`)

// TestEveryThirdPartyActionIsPinnedToAFullSHA is the F7 regression.
//
// A tag is a MOVING POINTER. `actions/checkout@v7.0.1` resolves to whatever
// commit that tag names when the workflow runs, and whoever can move the tag
// can change what executes inside a measured envelope — in a job that holds
// the run key, the authority key and the delegated cgroup subtree. A full
// commit SHA is the only immutable form of an action reference, which is what
// the frozen contract requires and what GitHub's own secure-use guidance says.
//
// Forty-one such references shipped as mutable tags. This reads every one of
// them out of the tree so the next addition cannot quietly be a tag.
func TestEveryThirdPartyActionIsPinnedToAFullSHA(t *testing.T) {
	root := filepath.Join("..", "..", ".github")
	var checked int
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".yml" && ext != ".yaml" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range usesRef.FindAllStringSubmatch(string(b), -1) {
			ref := m[1]
			// A SAME-REPOSITORY reference binds to the caller's own ref, so it
			// is already exactly as immutable as the run that resolved it —
			// there is no third party who could move it.
			if strings.HasPrefix(ref, "$/") || strings.HasPrefix(ref, "./") {
				continue
			}
			checked++
			at := strings.LastIndex(ref, "@")
			if at < 0 {
				t.Errorf("%s: `uses: %s` names no version at all", path, ref)
				continue
			}
			if !fullSHARef.MatchString(ref[at+1:]) {
				t.Errorf("%s: `uses: %s` is pinned to a moving reference; whoever can move it changes what runs inside a measured envelope holding the run key, the authority key and the delegated cgroup subtree. Pin the full 40-hex commit SHA and keep the tag as a trailing comment",
					path, ref)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("the scan found no third-party `uses:` reference at all; it is not reading the tree")
	}
}

// TestEveryPinnedActionKeepsItsTagAsMetadata: the SHA is the identity and the
// tag is the thing a human reads. Dropping the comment makes the pin
// unmaintainable, which is how pins rot back into tags.
func TestEveryPinnedActionKeepsItsTagAsMetadata(t *testing.T) {
	root := filepath.Join("..", "..", ".github")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".yml" && ext != ".yaml" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(b), "\n") {
			m := usesRef.FindStringSubmatch(line)
			if m == nil || strings.HasPrefix(m[1], "$/") || strings.HasPrefix(m[1], "./") {
				continue
			}
			at := strings.LastIndex(m[1], "@")
			if at < 0 || !fullSHARef.MatchString(m[1][at+1:]) {
				continue
			}
			if !strings.Contains(line, "# v") {
				t.Errorf("%s: %q pins a SHA with no tag comment; the SHA is the identity and the tag is what makes it maintainable", path, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
