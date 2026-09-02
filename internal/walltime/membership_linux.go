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
	return membershipModelFor(sys.Uid, info.Mode().Perm()&0o022 != 0, os.Getuid(), declaredWorkloadUIDs())
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
