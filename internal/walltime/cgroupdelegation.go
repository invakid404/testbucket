package walltime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CheckCgroupDelegation reports whether this process can actually MIGRATE into
// containments created under root — which is a different question from whether
// it owns root, and the difference is what made the published setup procedure
// unusable.
//
// On cgroup-v2, moving a process from one cgroup to another requires write
// access to the DESTINATION's `cgroup.procs` and to the `cgroup.procs` of the
// COMMON ANCESTOR of the source and the destination. The setup this project
// published created `/sys/fs/cgroup/testbucket`, gave it to the runner user
// and stopped there. The runner's own cgroup is somewhere else entirely — under
// the service slice, or the job's container root — so the common ancestor of
// the two is the ROOT cgroup, whose `cgroup.procs` is owned by root. Ownership
// of the destination is therefore not sufficient for the very first migration,
// and `wall begin` failed at
//
//	admit the action root .../tb-action-…/cgroup.procs: permission denied
//
// with nothing in the message to say which permission was missing or where.
//
// A delegated subtree is one whose ancestor the credential may write. The
// durable way to get one is to have the supervisor delegate it: a systemd
// service with `Delegate=yes` has its own cgroup subtree chowned to the
// service user, so the runner and every containment it creates live inside one
// delegated ancestor. The portable way, from a setup step, is to put the root
// UNDERNEATH the runner's own cgroup and give that cgroup's membership file to
// the runner user, which makes the runner's own cgroup the common ancestor.
//
// This returns nil when root is empty: an unmeasured run has nothing to
// delegate and is unaffected.
func CheckCgroupDelegation(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(root, "cgroup.procs")); err != nil {
		return fmt.Errorf("%s is not a cgroup-v2 directory (no cgroup.procs): %w", root, err)
	}
	self, err := selfCgroupDir()
	if err != nil {
		return err
	}
	ancestor := commonAncestorPath(self, root)
	procs := filepath.Join(ancestor, "cgroup.procs")
	f, err := os.OpenFile(procs, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf(
			"this process is in cgroup %s and would create containments under %s, whose common ancestor is %s — and %s is not writable by this credential (%v).\n"+
				"cgroup-v2 migration needs the COMMON ANCESTOR's membership file, not just the destination: owning %s is not enough to move even the action root into it.\n"+
				"Delegate a subtree instead: give the runner service `Delegate=yes` so its own cgroup subtree is delegated, or place the root under this process's own cgroup (%s) and give that cgroup.procs to this credential",
			self, root, ancestor, procs, err, root, self)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", procs, err)
	}
	// And the root itself must accept a new containment. Creating one and
	// removing it is the only honest test: the mode bits say what should
	// happen, the kernel says what does.
	probe := filepath.Join(root, fmt.Sprintf(".tb-delegation-check-%d", os.Getpid()))
	if err := os.Mkdir(probe, 0o755); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists; remove it and re-check the delegation", probe)
		}
		return fmt.Errorf("this credential cannot create a containment under %s: %w", root, err)
	}
	if err := os.Remove(probe); err != nil {
		return fmt.Errorf("this credential created %s but cannot remove it, so containments would accumulate: %w", probe, err)
	}
	return nil
}

// selfCgroupDir resolves this process's own cgroup directory: the cgroup-v2
// mount point from /proc/self/mountinfo, joined with the unified path from
// /proc/self/cgroup.
func selfCgroupDir() (string, error) {
	rel, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", fmt.Errorf("read this process's cgroup: %w", err)
	}
	path := ""
	for _, line := range strings.Split(string(rel), "\n") {
		// The unified hierarchy is the "0::" line; v1 controller lines are not
		// what any of this is about.
		if strings.HasPrefix(line, "0::") {
			path = strings.TrimPrefix(line, "0::")
			break
		}
	}
	if path == "" {
		return "", fmt.Errorf("this process is not in a cgroup-v2 unified hierarchy, so no subtree can be delegated to it")
	}
	mount, err := cgroup2MountPoint()
	if err != nil {
		return "", err
	}
	return filepath.Join(mount, path), nil
}

// cgroup2MountPoint reads the mount point of the unified hierarchy.
func cgroup2MountPoint() (string, error) {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", fmt.Errorf("read the mount table: %w", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		// mountinfo: … - <fstype> <source> <options>
		sep := strings.Index(line, " - ")
		if sep < 0 {
			continue
		}
		fields := strings.Fields(line[sep+3:])
		if len(fields) == 0 || fields[0] != "cgroup2" {
			continue
		}
		head := strings.Fields(line[:sep])
		if len(head) < 5 {
			continue
		}
		return head[4], nil
	}
	return "", fmt.Errorf("no cgroup-v2 filesystem is mounted, so there is no subtree to delegate")
}

// commonAncestorPath returns the deepest directory that is a prefix of both.
func commonAncestorPath(a, b string) string {
	as := strings.Split(filepath.Clean(a), string(filepath.Separator))
	bs := strings.Split(filepath.Clean(b), string(filepath.Separator))
	var shared []string
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			break
		}
		shared = append(shared, as[i])
	}
	out := strings.Join(shared, string(filepath.Separator))
	if out == "" {
		return string(filepath.Separator)
	}
	return out
}
