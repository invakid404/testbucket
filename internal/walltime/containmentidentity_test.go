package walltime

import (
	"os"
	"path/filepath"
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := full
			tc.edit(&c)
			if c.Scorable() {
				t.Errorf("a containment with %s is scorable", tc.name)
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
	if got := membershipControl(t.TempDir()); got != MembershipUnknown {
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
