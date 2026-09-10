package main

import (
	"os"
	"strings"
	"testing"
)

// THE CANDIDATE INSTALLER PROOF IS REMOVED, and this file records what
// replaced it.
//
// It drove `candidate:<run-id>/<artifact>@sha256:<digest>`: an immutable
// pre-publication build artifact downloaded through `gh run download`,
// re-digested, and installed so a scored arm could run a binary no release
// names. That is the unpublished-candidate authority chain the component map
// classifies REMOVE, and the map's own note for this component is to keep
// ordinary local/release installation, checksum checking and exhaustive
// archive-member validation while removing candidate authority and the
// candidate end-to-end proof tests.
//
// The archive-member validation was written inside that path and was never
// candidate-specific, so it moved to the release path rather than being
// deleted with it. `releaseinstall_test.go` exercises it there:
// TestTheReleaseInstallerInstallsOnlyTheArchivesOwnBinary covers the fixed
// member, the second-executable refusal, the traversal refusal and the
// checksum refusal, and TestTheCandidateDeliveryPathIsGone covers the removal.
//
// What remains here is the assertion that the surface is gone from the shipped
// action interfaces as well as from the script — a removal that leaves the
// inputs behind is a removal a caller cannot see.
func TestNoActionStillOffersACandidateDelivery(t *testing.T) {
	for _, path := range []string{
		"../../.github/actions/install/action.yml",
		"../../.github/actions/run-bucket/action.yml",
		"../../.github/actions/plan/action.yml",
		"../../.github/actions/record/action.yml",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		yml := string(b)
		for _, gone := range []string{
			"candidate-binary-digest",
			"TB_CANDIDATE_BINARY_DIGEST",
			"release-pins-ref",
		} {
			if strings.Contains(yml, gone) {
				t.Errorf("%s still offers %q; the candidate authority chain is REMOVE-classified", path, gone)
			}
		}
	}
}
