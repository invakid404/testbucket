package walltime

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// THE QUESTION THE CHECK HAS TO ASK IS THE ONE THE KERNEL ASKS.
//
// The published setup created `/sys/fs/cgroup/testbucket`, gave it to the
// runner user, and was wrong: cgroup-v2 authorises a migration at the COMMON
// ANCESTOR of the source and destination, and for a root beside the runner's
// own cgroup that ancestor is the root cgroup. Owning the destination admits
// nothing, and the only symptom was `permission denied` on the action root.
func TestTheDelegationCheckRefusesARootNothingCanMigrateInto(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("cgroup-v2 delegation is a Linux property")
	}
	// Shaped like a cgroup — it has the membership file — but outside the
	// unified hierarchy, so the ancestor it shares with this process's own
	// cgroup has no membership file this credential can write. That is the
	// published setup's failure in miniature.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cgroup.procs"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := CheckCgroupDelegation(root)
	if err == nil {
		t.Fatal("a root whose common ancestor is unwritable was reported delegated; the first migration would fail with a bare permission error")
	}
	for _, want := range []string{"common ancestor", "Delegate=yes", root} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q, so it does not say what to fix:\n%v", want, err)
		}
	}
}

// AND A SUBTREE THAT REALLY IS DELEGATED PASSES.
//
// Hung off this process's OWN cgroup, the ancestor the migration is authorised
// at is that cgroup — which is what `Delegate=` gives a service, and what the
// published procedure now tells an operator to arrange.
func TestADelegatedSubtreeUnderTheOwnCgroupIsUsable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("cgroup-v2 delegation is a Linux property")
	}
	self, err := selfCgroupDir()
	if err != nil {
		t.Skipf("no cgroup-v2 unified hierarchy here: %v", err)
	}
	root := filepath.Join(self, fmt.Sprintf("tb-delegation-test-%d", os.Getpid()))
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Skipf("this environment does not delegate %s to the test credential: %v", self, err)
	}
	t.Cleanup(func() { _ = os.Remove(root) })
	if err := CheckCgroupDelegation(root); err != nil {
		t.Fatalf("a subtree under this process's own cgroup was refused: %v", err)
	}
}

// A root that is not a cgroup at all is refused for THAT reason, and an
// unmeasured run — which delegates nothing — is not an error.
func TestTheDelegationCheckSeparatesAbsenceFromMisconfiguration(t *testing.T) {
	if err := CheckCgroupDelegation("   "); err != nil {
		t.Errorf("an unmeasured run with no delegated subtree was refused: %v", err)
	}
	err := CheckCgroupDelegation(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a cgroup-v2 directory") {
		t.Errorf("an ordinary directory was not identified as one: %v", err)
	}
}

func TestTheCommonAncestorIsWhereTheMigrationIsAuthorised(t *testing.T) {
	for _, tc := range []struct{ a, b, want string }{
		{"/sys/fs/cgroup/system.slice/runner.service", "/sys/fs/cgroup/testbucket", "/sys/fs/cgroup"},
		{"/sys/fs/cgroup/user.slice/x", "/sys/fs/cgroup/user.slice/x/testbucket/tb-action-1", "/sys/fs/cgroup/user.slice/x"},
		{"/a", "/b", "/"},
		{"/sys/fs/cgroup", "/sys/fs/cgroup", "/sys/fs/cgroup"},
	} {
		if got := commonAncestorPath(tc.a, tc.b); got != tc.want {
			t.Errorf("commonAncestorPath(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}
