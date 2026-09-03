package walltime

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestTheContainmentRootProcessIdentityIsRequiredAndCompared is the F1
// regression.
//
// ContainmentIdentity documents RootPID plus RootStart as the pair that closes
// pid reuse, and nothing asked for either: Scorable() checked the primitive,
// path, inode and boot, and Same() compared those same four. So a run whose
// every containment record omitted the start identity scored, and so did one
// where the trace named a containment created for a different process than the
// physical wrapper and the peer did.
func TestTheContainmentRootProcessIdentityIsRequiredAndCompared(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit mutation
		want string
	}{
		// THE DEFECT, both halves.
		{"no start identity anywhere", func(_ Level, _ int, _ Producer, _ string, r *Record) {
			r.Containment.RootStart = ""
		}, "a number the kernel reuses"},

		{"a trace watching a containment made for another process", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p == ProducerTrace {
				r.Containment.RootStart = "different-process-start"
			}
		}, "did not watch a containment made for the same process"},

		{"a peer naming a different root pid", func(_ Level, _ int, p Producer, _ string, r *Record) {
			if p == ProducerPeer {
				r.Containment.RootPID = 999999
			}
		}, "did not watch a containment made for the same process"},

		{"no root pid anywhere", func(_ Level, _ int, _ Producer, _ string, r *Record) {
			r.Containment.RootPID = 0
		}, "a number the kernel reuses"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := verifySynth(t, tc.edit, nil)
			if len(findingsMentioning(v, "WT-029", tc.want)) == 0 {
				t.Errorf("no WT-029 finding mentions %q; eligible=%v findings=%+v", tc.want, v.Eligible, v.Findings)
			}
			if v.Eligible {
				t.Errorf("a run with %s scored", tc.name)
			}
		})
	}
}

// TestScorableRequiresTheWholeIdentity states the predicate directly, because
// it is the one place a future field can be added to the struct and forgotten
// in the check.
func TestScorableRequiresTheWholeIdentity(t *testing.T) {
	full := ContainmentIdentity{
		Primitive: PrimitiveCgroup2, ID: "/sys/fs/cgroup/testbucket/tb-action-0",
		Inode: "900000", BootID: "boot-1", RootPID: 4242, RootStart: "778899",
		OwnerUID: 1000, OwnerGID: 900, Mode: 0o770,
		MembershipControl: MembershipSupervisorOwned,
	}
	if !full.Scorable() {
		t.Fatal("a complete containment identity is not scorable")
	}
	for _, tc := range []struct {
		name string
		edit func(*ContainmentIdentity)
	}{
		{"no primitive", func(c *ContainmentIdentity) { c.Primitive = "" }},
		{"a process group", func(c *ContainmentIdentity) { c.Primitive = PrimitiveProcessGroup }},
		{"no path", func(c *ContainmentIdentity) { c.ID = "" }},
		{"no inode", func(c *ContainmentIdentity) { c.Inode = "" }},
		{"no boot", func(c *ContainmentIdentity) { c.BootID = "" }},
		{"no root pid", func(c *ContainmentIdentity) { c.RootPID = 0 }},
		{"no root start", func(c *ContainmentIdentity) { c.RootStart = "  " }},
		{"a membership the workload can write", func(c *ContainmentIdentity) { c.MembershipControl = MembershipWorkloadWritable }},
		{"an unestablished membership model", func(c *ContainmentIdentity) { c.MembershipControl = "" }},
		// THE INPUTS TO THAT MODEL, which the verifier reruns the rule over.
		// The mode was optional, so an identity could omit it, assert
		// supervisor ownership, skip the rederivation that omission gates and
		// stay scorable — a conclusion with no retained inputs behind it.
		{"no retained mode", func(c *ContainmentIdentity) { c.Mode = 0 }},
		{"no retained owner uid", func(c *ContainmentIdentity) { c.OwnerUID = -1 }},
		{"no retained owner gid", func(c *ContainmentIdentity) { c.OwnerGID = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := full
			tc.edit(&c)
			if c.Scorable() {
				t.Errorf("a containment with %s is scorable", tc.name)
			}
		})
	}
	// SAME compares the membership facts too. Two records naming one path,
	// inode and boot were treated as one containment however they described
	// its ownership, so a producer could keep the identity and restate the
	// owner, the mode or the conclusion and nothing noticed.
	for _, tc := range []struct {
		name string
		edit func(*ContainmentIdentity)
	}{
		{"a different owner uid", func(c *ContainmentIdentity) { c.OwnerUID = 4242 }},
		{"a different owner gid", func(c *ContainmentIdentity) { c.OwnerGID = 4242 }},
		{"a different mode", func(c *ContainmentIdentity) { c.Mode = 0o777 }},
		{"a different membership conclusion", func(c *ContainmentIdentity) { c.MembershipControl = MembershipWorkloadWritable }},
		{"a different workload credential", func(c *ContainmentIdentity) { c.WorkloadUID = 31337 }},
		{"a different workload group vector", func(c *ContainmentIdentity) { c.WorkloadGIDs = []int{31337} }},
	} {
		t.Run("Same with "+tc.name, func(t *testing.T) {
			c := full
			tc.edit(&c)
			if full.Same(c) {
				t.Errorf("two identities differing by %s compared as the same containment", tc.name)
			}
		})
	}
	// SameRoot answers only about the root process, and says so.
	other := full
	other.Inode = "different"
	if !full.SameRoot(other) {
		t.Error("SameRoot disagreed about two identities with the same root process")
	}
	other.RootStart = "elsewhere"
	if full.SameRoot(other) {
		t.Error("SameRoot agreed about two different root processes")
	}
}

// TestAWorkloadWritableContainmentIsNotScored is the F4 regression.
//
// The composite action told callers to `chown -R "$(id -u)"` the delegated
// subtree, and the wrapper and the measured workload run as the same runner
// uid. On cgroup-v2 `cgroup.procs` IS the process-migration control, so the
// workload could move itself or a descendant between the action, script,
// invocation and sibling containments and rewrite the membership history the
// envelope is built on — while Stage 1 recorded "membership not modifiable by
// the workload". A manifest cannot make a boundary exist by describing one.
func TestAWorkloadWritableContainmentIsNotScored(t *testing.T) {
	for _, model := range []string{MembershipWorkloadWritable, MembershipUnknown, ""} {
		v := verifySynth(t, nil, func(s *synthRun) { s.membershipControl = model })
		if len(findingsMentioning(v, "WT-031", "process-migration control")) == 0 {
			t.Errorf("membership model %q raised no WT-031: %+v", model, v.Findings)
		}
		if v.Eligible {
			t.Errorf("a run whose containment membership model is %q scored", model)
		}
	}
}

// TestTheMembershipModelIsReadNotAsserted: the fact comes off the filesystem,
// and same-uid ownership — the shape the documented setup produces — is the
// unscorable one.
func TestTheMembershipModelIsReadNotAsserted(t *testing.T) {
	if got := membershipControl(t.TempDir(), resolveWorkloadCredential(os.Getenv(WorkloadUserEnv))); got != MembershipUnknown {
		t.Errorf("a directory with no cgroup.procs established model %q, want %q", got, MembershipUnknown)
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "testbucket", "wallstage1.go"))
	if err != nil {
		t.Fatal(err)
	}
	// CODE, not commentary. The phrase is expected to survive in the comment
	// explaining why it was removed; what must not survive is a manifest that
	// still states it as a property of the environment.
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if strings.Contains(line, "membership not modifiable by the workload") {
			t.Errorf("Stage 1 still asserts that the workload cannot modify membership: %s\nNothing established that property, and the documented setup gave the workload exactly that capability", strings.TrimSpace(line))
		}
	}
}

// TestADeclaredWorkloadUIDCannotMintTheBoundary is the F4 regression.
//
// membershipControl compared the owner of `cgroup.procs` against
// TB_WALL_WORKLOAD_UID and nothing else, so declaring any uid other than the
// owner's returned `supervisor-owned` for a file this very process owns. The
// declaration is a caller-controlled string standing in for a privilege nobody
// held: nothing in this wrapper changes credentials between itself and the
// measured child — not runChild, not RunInAction, not the workflow step — so
// the workload runs as exactly this uid.
func TestADeclaredWorkloadUIDCannotMintTheBoundary(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cgroup.procs"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, declared := range []string{
		strconv.Itoa(os.Getuid() + 1),
		strconv.Itoa(os.Getuid()+1) + ",65534",
		"",
		"not-a-uid",
	} {
		t.Setenv(WorkloadUIDEnv, declared)
		// NOT supervisor-owned is the invariant, on every platform: Linux
		// reads the owner and answers workload-writable, and a host with no
		// cgroup-v2 to read answers unknown. Both are unscorable; what must
		// never happen is a declaration producing the one answer that scores.
		if got := membershipControl(dir, resolveWorkloadCredential(os.Getenv(WorkloadUserEnv))); got == MembershipSupervisorOwned {
			t.Errorf("declaring workload uid %q made a file this process owns %q; a declaration cannot make the workload somebody else",
				declared, got)
		}
	}
	// The declaration may still WIDEN who lacks the boundary, never narrow it.
	t.Setenv(WorkloadUIDEnv, strconv.Itoa(os.Getuid()))
	if got := membershipControl(dir, resolveWorkloadCredential(os.Getenv(WorkloadUserEnv))); got == MembershipSupervisorOwned {
		t.Errorf("membershipControl=%q for a file owned by a declared workload uid", got)
	}
}

// TestTheMembershipDecisionCountsThisProcesssOwnCredential is the portable F4
// regression: the decision itself, on any host.
//
// The rule used to compare the owner against TB_WALL_WORKLOAD_UID and nothing
// else, so a caller declaring any uid other than the owner's minted
// `supervisor-owned` for a file the wrapper itself owned — a caller-controlled
// string standing in for a privilege nobody held.
func TestTheMembershipDecisionCountsThisProcesssOwnCredential(t *testing.T) {
	const self = 1000
	for _, tc := range []struct {
		name     string
		owner    uint32
		widened  bool
		resolved int
		declared []int
		want     string
	}{
		// The single-credential runner: nothing declared, so the workload is
		// taken to share this process's credential and owns what it could
		// write. This is the shape that cannot be scored.
		{"owned by this process, nothing declared", self, false, 0, nil, MembershipWorkloadWritable},

		// A RESOLVED workload account that differs from the owner is the
		// supervised shape, and the only thing that may replace this process's
		// uid in the decision. TB_WALL_WORKLOAD_USER is a NAME looked up in
		// the accounts database, so a uid that comes back and differs from
		// this process's is a second credential that demonstrably exists — and
		// it is not taken on trust either: the wrapper spawns the measured
		// command as that account, so a false one fails the measurement, and
		// the verifier separately requires the measured process's own uid to
		// differ from the containment owner's.
		{"owned by the wrapper with a distinct workload account resolved", self, false, self + 1, []int{self + 1}, MembershipSupervisorOwned},

		// A BARE DECLARED NUMBER MAY NOT. TB_WALL_WORKLOAD_UID is an integer
		// list from the caller that nothing verifies, and treating it as a
		// replacement let a caller mint the boundary by naming any uid at all:
		// a file this very process owns then reported supervisor-owned, which
		// is the one answer that scores. On Linux that is exactly what CI
		// caught; on a platform with no accounts database to read it was
		// invisible.
		{"owned by this process with a distinct uid merely declared", self, false, 0, []int{self + 1}, MembershipWorkloadWritable},
		{"owned by a declared workload uid among several", self, false, 0, []int{4, self}, MembershipWorkloadWritable},
		// Root is not a resolution: zero is the safe zero value, so a caller
		// that sets nothing gets the conservative answer.
		{"owned by this process with root named as the workload", self, false, 0, []int{self + 1}, MembershipWorkloadWritable},

		// A declaration WIDENS: another uid, declared as the workload's.
		{"owned by a declared workload uid", 4242, false, 0, []int{4242}, MembershipWorkloadWritable},
		{"owned by one of several declared uids", 4242, false, 0, []int{7, 4242}, MembershipWorkloadWritable},
		{"world-writable however it is owned", 4242, true, 0, []int{7}, MembershipWorkloadWritable},
		// And widening still applies alongside a resolved account.
		{"owned by a uid widened beside a resolved account", 4242, false, self + 1, []int{self + 1, 4242}, MembershipWorkloadWritable},

		// The only scorable shape.
		{"owned by a credential nobody here runs as", 4242, false, 0, []int{7}, MembershipSupervisorOwned},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := membershipModelFor(MembershipFacts{
				OwnerUID: tc.owner, OtherWritable: tc.widened,
				SelfUID: self, WorkloadUID: tc.resolved, WorkloadUIDs: tc.declared,
			})
			if got != tc.want {
				t.Errorf("membershipModelFor(owner=%d widened=%v self=%d resolved=%d declared=%v) = %q, want %q",
					tc.owner, tc.widened, self, tc.resolved, tc.declared, got, tc.want)
			}
		})
	}
}

// TestGroupWritableIsAHoleOnlyWhenTheWorkloadIsInTheGroup: a delegated subtree
// owned by root and writable by the WRAPPER's group is the boundary a
// supervised run rests on, so refusing every group-writable mode outright
// would refuse the only arrangement in which a scored row can exist. It is a
// hole exactly when the workload is in that group.
func TestGroupWritableIsAHoleOnlyWhenTheWorkloadIsInTheGroup(t *testing.T) {
	base := MembershipFacts{
		OwnerUID: 0, OwnerGID: 900, GroupWritable: true,
		SelfUID: 1000, WorkloadUIDs: []int{1001},
	}
	t.Run("the workload is not in the owning group", func(t *testing.T) {
		f := base
		f.WorkloadGIDs = []int{1001, 100}
		if got := membershipModelFor(f); got != MembershipSupervisorOwned {
			t.Errorf("a root-owned, wrapper-group-writable subtree read as %q", got)
		}
	})
	t.Run("the workload IS in the owning group", func(t *testing.T) {
		f := base
		f.WorkloadGIDs = []int{1001, 900}
		if got := membershipModelFor(f); got != MembershipWorkloadWritable {
			t.Errorf("a subtree writable by a group the workload is in read as %q", got)
		}
	})
	t.Run("nothing established about the workload's groups", func(t *testing.T) {
		f := base
		f.WorkloadGIDs = nil
		if got := membershipModelFor(f); got != MembershipWorkloadWritable {
			t.Errorf("a group write bit that could not be shown to exclude the workload read as %q", got)
		}
	})
}
