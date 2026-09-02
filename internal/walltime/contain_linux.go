package walltime

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// cgroup2SuperMagic identifies a cgroup-v2 mount. A directory that merely
// looks like one (a tmpfs a test made) must not be able to pose as scoreable
// containment.
const cgroup2SuperMagic = 0x63677270

type cgroup2 struct {
	dir   string
	ident ContainmentIdentity
}

func newContainment(name string, parent *ContainmentIdentity) (Containment, error) {
	// A nested level is created inside its parent, so the child's processes
	// stay inside the parent's lifecycle.
	if parent != nil && parent.Primitive == PrimitiveCgroup2 {
		return newCgroupUnder(parent.ID, name)
	}
	root := strings.TrimSpace(os.Getenv(cgroupRootEnv))
	if root == "" {
		return newProcessGroupContainment(name, fmt.Sprintf("%s is unset: no delegated cgroup-v2 subtree", cgroupRootEnv))
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return newProcessGroupContainment(name, fmt.Sprintf("statfs %s: %v", root, err))
	}
	if st.Type != cgroup2SuperMagic {
		return newProcessGroupContainment(name, fmt.Sprintf("%s is not a cgroup-v2 mount", root))
	}
	return newCgroupUnder(root, name)
}

// newCgroupUnder creates one containment directory under an existing
// cgroup-v2 directory and reads back its inode, which is what makes the
// identity survive a path being reused.
func newCgroupUnder(root, name string) (Containment, error) {
	dir := filepath.Join(root, name)
	// The containment must be FRESH. An existing directory has an unknown
	// history — a previous run's members, a previous run's lifecycle — and
	// observing it would attribute someone else's processes to this envelope.
	// This is a hard failure, not a fallback to the unscored primitive: a name
	// collision here means two runs believe they own the same containment.
	if err := os.Mkdir(dir, 0o755); err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("walltime: containment %s already exists; a lifecycle must begin in a fresh containment, not one with an unknown history", dir)
		}
		return newProcessGroupContainment(name, fmt.Sprintf("mkdir %s: %v", dir, err))
	}
	info, err := os.Stat(dir)
	if err != nil {
		return newProcessGroupContainment(name, fmt.Sprintf("stat %s: %v", dir, err))
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return newProcessGroupContainment(name, "cannot read containment inode")
	}
	self := os.Getpid()
	return &cgroup2{
		dir: dir,
		ident: ContainmentIdentity{
			Primitive: PrimitiveCgroup2,
			ID:        dir,
			Inode:     strconv.FormatUint(sys.Ino, 10),
			BootID:    bootIdentity(),
			RootPID:   self,
			RootStart: processStartID(self),
			// Established by reading the filesystem, not asserted. A
			// containment whose membership the workload can rewrite cannot
			// prove the nested history the envelope is built on.
			MembershipControl: membershipControl(dir),
		},
	}, nil
}

func (c *cgroup2) Identity() ContainmentIdentity { return c.ident }

func (c *cgroup2) Admit(pid int) error {
	return os.WriteFile(filepath.Join(c.dir, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0o644)
}

// Freeze writes cgroup.freeze, the kernel's own suspend primitive for a
// subtree. A child cloned into a frozen containment is created stopped, which
// is what lets the admission read observe exactly what was admitted.
func (c *cgroup2) Freeze(frozen bool) error {
	value := "0"
	if frozen {
		value = "1"
	}
	if err := os.WriteFile(filepath.Join(c.dir, "cgroup.freeze"), []byte(value), 0o644); err != nil {
		return fmt.Errorf("cgroup.freeze=%s: %w", value, err)
	}
	return nil
}

func (c *cgroup2) Procs() ([]int, error) {
	b, err := os.ReadFile(filepath.Join(c.dir, "cgroup.procs"))
	if err != nil {
		return nil, err
	}
	var out []int
	for _, line := range strings.Fields(string(b)) {
		if pid, err := strconv.Atoi(line); err == nil {
			out = append(out, pid)
		}
	}
	return out, nil
}

// Observe reads cgroup.events fresh. The returned digest covers the exact
// bytes the kernel produced together with this observer's own event id, so the
// peer's evidence and the trace's evidence of one transition are never the
// same value even though they describe the same lifecycle.
func (c *cgroup2) Observe(observer string) (RawEvent, bool, error) {
	b, err := os.ReadFile(filepath.Join(c.dir, "cgroup.events"))
	if err != nil {
		return RawEvent{}, false, err
	}
	// The membership snapshot is taken with the same observation, so
	// "unpopulated" comes with the list that was empty rather than as a bare
	// boolean.
	//
	// A READ ERROR IS FATAL HERE. It used to be discarded, which made a failed
	// membership read produce evidence identical to a successful read of an
	// empty containment — the one thing this snapshot exists to distinguish.
	// The events file is still the authority on emptiness; what the caller may
	// not do is claim a snapshot it never obtained.
	procs, err := os.ReadFile(filepath.Join(c.dir, "cgroup.procs"))
	if err != nil {
		return RawEvent{}, false, fmt.Errorf("read the containment membership snapshot: %w", err)
	}
	return newContainmentEvent(observer, b, procs), cgroupPopulated(b), nil
}

// cgroupPopulated parses the `populated 0|1` line of cgroup.events. An
// unparseable file reads as POPULATED: the safe answer is "children may still
// be running", never "we are done".
func cgroupPopulated(b []byte) bool {
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "populated" {
			return f[1] != "0"
		}
	}
	return true
}

func (c *cgroup2) Signal(sig syscall.Signal) error {
	// cgroup.kill (kernel >= 5.14) kills the whole subtree atomically, with no
	// window for a descendant to fork out from under the enumeration.
	if sig == syscall.SIGKILL {
		if err := os.WriteFile(filepath.Join(c.dir, "cgroup.kill"), []byte("1"), 0o644); err == nil {
			return nil
		}
	}
	pids, err := c.Procs()
	if err != nil {
		return err
	}
	for _, pid := range pids {
		if err := syscall.Kill(pid, sig); err != nil && err != syscall.ESRCH {
			return err
		}
	}
	return nil
}

func (c *cgroup2) Destroy() error {
	if err := os.Remove(c.dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// processStartID reads a PID's start time from /proc, which is what makes a
// PID an identity rather than a reusable number.
func processStartID(pid int) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	// The comm field can contain spaces and parentheses; everything after the
	// LAST ')' is fixed-position, and starttime is field 22 overall.
	i := strings.LastIndexByte(string(b), ')')
	if i < 0 {
		return ""
	}
	fields := strings.Fields(string(b[i+1:]))
	// fields[0] is state (field 3); starttime is field 22 => index 19.
	if len(fields) < 20 {
		return ""
	}
	return fields[19]
}

// attachCgroup2 binds an existing cgroup-v2 containment, refusing one whose
// inode no longer matches: a path that was recreated is a different
// containment, and observing it would attribute another lifecycle's emptiness
// to this one.
func attachCgroup2(ident ContainmentIdentity) (Containment, error) {
	info, err := os.Stat(ident.ID)
	if err != nil {
		return nil, fmt.Errorf("walltime: attach containment %s: %w", ident.ID, err)
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, fmt.Errorf("walltime: attach containment %s: no inode", ident.ID)
	}
	if got := strconv.FormatUint(sys.Ino, 10); got != ident.Inode {
		return nil, fmt.Errorf("walltime: containment %s inode is %s, expected %s", ident.ID, got, ident.Inode)
	}
	return &cgroup2{dir: ident.ID, ident: ident}, nil
}
