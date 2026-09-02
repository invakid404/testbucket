package walltime

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// membershipControl establishes who may write this containment's
// `cgroup.procs`, by READING the filesystem rather than by assertion.
//
// On cgroup-v2 `cgroup.procs` is the process-migration control: a process with
// write access to it, and to the file in the destination cgroup, may move
// processes within the delegated subtree. The measured workload must not have
// that access, or the nested membership history the envelope rests on is
// something the workload can rewrite while it runs.
//
// Three answers, and only the first is scorable:
//
//   - supervisor-owned: the file belongs to a uid the workload does not run
//     as, and no group or other write bit widens it;
//   - workload-writable: the workload owns it, or the mode grants it — this is
//     what the documented `chown -R "$(id -u)"` setup produces, because the
//     wrapper and the measured script run as the same runner uid;
//   - unknown: the file could not be read, so nothing was established.
func membershipControl(dir string) string {
	info, err := os.Stat(filepath.Join(dir, "cgroup.procs"))
	if err != nil {
		return MembershipUnknown
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return MembershipUnknown
	}
	// Group and other write bits are treated as workload-writable without
	// asking which groups the workload is in. Establishing that would mean
	// enumerating the workload's supplementary groups and trusting the answer;
	// refusing a widened mode outright is both simpler and the fail-closed
	// direction.
	perm := info.Mode().Perm()
	return membershipModelFor(MembershipFacts{
		OwnerUID: sys.Uid, OwnerGID: sys.Gid,
		GroupWritable: perm&0o020 != 0, OtherWritable: perm&0o002 != 0,
		SelfUID:      os.Getuid(),
		WorkloadUIDs: declaredWorkloadUIDs(),
		WorkloadGIDs: declaredWorkloadGIDs(),
	})
}

// declaredWorkloadGIDs is every group the declared workload account belongs
// to, read from /etc/group and the account's own primary group.
//
// It is read rather than declared because the question the boundary turns on
// is whether the WORKLOAD is in the group that may write the delegated
// subtree, and a caller-supplied answer to that is a caller-supplied boundary.
func declaredWorkloadGIDs() []int {
	user := strings.TrimSpace(os.Getenv(WorkloadUserEnv))
	if user == "" {
		return nil
	}
	var out []int
	if b, err := os.ReadFile("/etc/passwd"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Split(line, ":")
			if len(f) > 3 && f[0] == user {
				if gid, err := strconv.Atoi(f[3]); err == nil {
					out = append(out, gid)
				}
			}
		}
	}
	b, err := os.ReadFile("/etc/group")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 4 {
			continue
		}
		gid, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		for _, member := range strings.Split(f[3], ",") {
			if strings.TrimSpace(member) == user {
				out = append(out, gid)
			}
		}
	}
	return out
}

// declaredWorkloadUIDs is the caller's statement about which OTHER credentials
// the measured workload may run as. It never removes this process's own uid
// from consideration; see membershipModelFor.
func declaredWorkloadUIDs() []int {
	var out []int
	for _, field := range strings.Split(os.Getenv(WorkloadUIDEnv), ",") {
		if uid, err := strconv.Atoi(strings.TrimSpace(field)); err == nil {
			out = append(out, uid)
		}
	}
	return out
}
