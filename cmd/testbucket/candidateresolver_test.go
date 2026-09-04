package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// THE CANDIDATE MAY NOT RESOLVE ITS OWN APPROVAL.
//
// The resolver reads the signed Stage-1 manifest and prints the binary digest
// the campaign authority approved; the installer then refuses any candidate
// whose bytes do not match. That is only worth something if the party reading
// is not the party being checked.
//
// The first guard rejected values beginning with `candidate:` and said it
// prevented self-verification. It did not: `local` walked straight through it,
// and the shared installer treats `local` by BUILDING THE CHECKED-OUT TREE —
// which on a candidate run is the candidate. The candidate ran its own
// `wall stage1-binary` and vouched for itself, through the exact route the
// guard existed to close.
//
// This drives the shipped guard, one value at a time.
func TestOnlyAPublishedImmutableReleaseMayResolveACandidate(t *testing.T) {
	script := filepath.Join("..", "..", ".github", "actions", "candidate-resolver.sh")
	run := func(v string) (string, error) {
		cmd := exec.Command("bash", script)
		cmd.Env = append(os.Environ(), "TB_RESOLVER_VERSION="+v)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	for _, tc := range []struct{ value, why string }{
		{"", "nothing to resolve with, and silence is not a default"},
		{"local", "builds the checked-out tree, which on a candidate run is the candidate"},
		{"source", "the same build, spelled differently"},
		{"candidate:123/art-linux_amd64@sha256:" + strings.Repeat("a", 64), "a candidate cannot authenticate its own approval"},
		{"v0", "a moving alias: whoever may move it chooses the resolver"},
		{"v0.2", "a moving minor alias"},
		{"main", "a branch is not a release"},
		{"693a19981fb6e0061d3fab62e59d75dc1c01ff3f", "a commit has no published asset"},
		{"  local  ", "whitespace does not make it published"},
	} {
		t.Run("refuses "+fmt.Sprintf("%q", tc.value), func(t *testing.T) {
			out, err := run(tc.value)
			if err == nil {
				t.Fatalf("%q was accepted as a resolver (%s):\n%s", tc.value, tc.why, out)
			}
			if !strings.Contains(out, "candidate-resolver-version") {
				t.Errorf("the refusal does not name the input:\n%s", out)
			}
		})
	}

	for _, ok := range []string{"v0.2.2", "v1.0.0", "v10.20.30"} {
		t.Run("accepts "+ok, func(t *testing.T) {
			if out, err := run(ok); err != nil {
				t.Fatalf("an exact published release was refused: %v\n%s", err, out)
			}
		})
	}
}

// AND AN INCAPABLE PUBLISHED RELEASE IS REFUSED FOR THAT REASON.
//
// `v0` and `v0.2.2` both resolve to a revision with no wall-time
// implementation at all: it exits with `unknown subcommand "wall"`. The
// workflow defaulted to it, so the advertised route failed with a message
// about a subcommand rather than about the delivery — and no published release
// implements `wall stage1-binary` yet, which is a fact the route has to state
// rather than trip over.
func TestAnIncapableResolverIsRefusedByName(t *testing.T) {
	dir := t.TempDir()
	// A stand-in for a published release that predates the subcommand.
	old := filepath.Join(dir, "testbucket")
	if err := os.WriteFile(old, []byte("#!/bin/sh\necho 'unknown subcommand \"wall\"' >&2\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	probe := exec.Command(old, "wall", "stage1-binary", "-h")
	if err := probe.Run(); err == nil {
		t.Fatal("the stand-in for an incapable release reported that it can resolve")
	}

	// The shipped action asks exactly that question, and says what is missing.
	body, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "candidate-digest", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"$TB_RESOLVER" wall stage1-binary -h`) {
		t.Error("the resolver's capability is never probed, so an incapable release fails with a subcommand error instead of a delivery one")
	}
	for _, want := range []string{
		"has no 'wall stage1-binary'",
		"Until one is published there is no capable resolver",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal does not explain %q", want)
		}
	}
	// And the capable binary under test answers the same probe affirmatively,
	// so the check distinguishes the two rather than always refusing.
	bin := buildCLI(t)
	if err := exec.Command(bin, "wall", "stage1-binary", "-h").Run(); err != nil {
		t.Errorf("the candidate's own resolver fails the capability probe, so no release built from it could ever pass: %v", err)
	}
}
