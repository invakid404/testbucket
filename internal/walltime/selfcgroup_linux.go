package walltime

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// SelfContainment is the cgroup-v2 containment THIS PROCESS is already in.
//
// It is the authenticated answer to "which containment encloses me", and it
// comes from the kernel rather than from a file. That distinction is the whole
// repair: an invocation wrapper is started by the measured script and is
// therefore inside the script's containment by inheritance, so it never needed
// the script to tell it where it was. The handoff file it used to trust lives
// in the records directory, which the measured script can write, and the
// production path had no run key to sign it with — so the one document
// deciding which containment an invocation would be measured inside was one
// the measured work could rewrite.
//
// A process cannot misreport its own cgroup to itself. It could still MOVE
// itself, which is the separate membership-control question WT-031 refuses to
// score without a real boundary.
func SelfContainment() (*ContainmentIdentity, bool) {
	rel, ok := selfCgroupPath()
	if !ok {
		return nil, false
	}
	dir, ok := absoluteCgroupPath(rel)
	if !ok {
		return nil, false
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, false
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, false
	}
	self := os.Getpid()
	ident := &ContainmentIdentity{
		Primitive: PrimitiveCgroup2,
		ID:        dir,
		Inode:     strconv.FormatUint(sys.Ino, 10),
		BootID:    bootIdentity(),
		RootPID:   self,
		RootStart: processStartID(self),
	}
	retainMembershipFacts(ident, dir)
	return ident, true
}

// selfCgroupPath reads the unified-hierarchy line of /proc/self/cgroup, which
// on cgroup-v2 is `0::/path`.
func selfCgroupPath() (string, bool) {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", false
	}
	return unifiedCgroupLine(string(b))
}

// unifiedCgroupLine reads the `0::/path` line of a /proc/<pid>/cgroup file.
func unifiedCgroupLine(text string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 && parts[0] == "0" && parts[1] == "" && strings.HasPrefix(parts[2], "/") {
			return parts[2], true
		}
	}
	return "", false
}

// absoluteCgroupPath joins the namespace-relative path onto the cgroup-v2
// mount. The delegated root this wrapper was given is the anchor when it
// contains the relative path's tail; otherwise the standard mount point is
// checked, and anything else fails closed rather than guessing at a directory
// to call a containment.
func absoluteCgroupPath(rel string) (string, bool) {
	rel = strings.TrimSuffix(rel, "/")
	if rel == "" || rel == "/" {
		return "", false
	}
	for _, mount := range []string{strings.TrimSpace(os.Getenv(cgroupRootEnv)), "/sys/fs/cgroup"} {
		if mount == "" {
			continue
		}
		// The delegated root may itself sit at a prefix of the relative path,
		// in which case the join must not repeat that prefix.
		candidates := []string{filepath.Join(mount, rel)}
		if i := strings.Index(rel, filepath.Base(mount)+"/"); i >= 0 {
			candidates = append(candidates, filepath.Join(mount, rel[i+len(filepath.Base(mount)):]))
		}
		for _, dir := range candidates {
			var st syscall.Statfs_t
			if err := syscall.Statfs(dir, &st); err != nil || st.Type != cgroup2SuperMagic {
				continue
			}
			return dir, true
		}
	}
	return "", false
}
