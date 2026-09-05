package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// A FAILED ACQUISITION LEAVES NO CLAIM.
//
// The contract permits ONE planner execution, so a claim file is a permanent
// refusal to anyone who finds it. The path was registered for rollback only
// after the directory entry had been committed, so a failure to open, sync or
// close the store directory ran release() without the file it had just
// created: every earlier store was rolled back, the current one kept its
// claim, and the function reported failure. A later attempt then read that
// file as "already derived" and refused to plan at all — a permanent refusal
// produced by an acquisition that failed.
//
// The directory here is write-and-search but not readable, which is exactly
// that shape: O_EXCL creation succeeds, and opening the directory to sync it
// does not.
func TestAFailedPlannerClaimAcquisitionLeavesNoClaimBehind(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses the directory permission this failure is produced by")
	}
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv(plannerClaimStoreEnv, "")

	external := filepath.Join(t.TempDir(), "shared-claims")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write and search, but not readable: a file can be created inside, and
	// the directory cannot be opened to be synced.
	if err := os.Chmod(external, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(external, 0o755) })

	stage1 := walltime.DigestBytes([]byte("stage-1 manifest"))
	bundle := walltime.DigestBytes([]byte("planning bundle"))
	if _, err := claimPlannerExecution(external, stage1, bundle); err == nil {
		t.Fatal("a claim whose directory entry could not be committed was reported as taken")
	} else if !strings.Contains(err.Error(), "directory entry") {
		t.Fatalf("the failure is not the one this test produces: %v", err)
	}

	name := "planner-claim-" + walltime.PlannerClaimKey(stage1, bundle) + ".json"
	// Search permission is enough to stat a name inside the directory.
	if _, err := os.Stat(filepath.Join(external, name)); err == nil {
		t.Error("failed claim acquisition left its current-store file behind; a later attempt reads it as an already-derived plan and refuses for ever")
	} else if !os.IsNotExist(err) {
		t.Errorf("could not tell whether the claim file survived: %v", err)
	}
	machine := filepath.Join(state, "testbucket", "planner-claims", name)
	if _, err := os.Stat(machine); err == nil {
		t.Error("failed claim acquisition left an earlier store's file behind")
	} else if !os.IsNotExist(err) {
		t.Errorf("could not tell whether the earlier store's claim survived: %v", err)
	}

	// AND THE NEXT ATTEMPT CAN STILL PLAN. Rollback that leaves the tree
	// unclaimed is the whole point; a test that only checked the file was gone
	// would not notice a rollback that removed it from a different store.
	if err := os.Chmod(external, 0o755); err != nil {
		t.Fatal(err)
	}
	claim, err := claimPlannerExecution(external, stage1, bundle)
	if err != nil {
		t.Fatalf("a later attempt was refused after a failed acquisition: %v", err)
	}
	claim.release()
}
