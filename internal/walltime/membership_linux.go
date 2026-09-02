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
	if info.Mode().Perm()&0o022 != 0 {
		return MembershipWorkloadWritable
	}
	if uint32(workloadUID()) == sys.Uid {
		return MembershipWorkloadWritable
	}
	return MembershipSupervisorOwned
}

// workloadUID is the credential the measured workload runs as: the caller's
// declared one, or this process's own when the caller declares none — which
// means the workload shares the wrapper's credential.
func workloadUID() int {
	if v := strings.TrimSpace(os.Getenv(WorkloadUIDEnv)); v != "" {
		if uid, err := strconv.Atoi(v); err == nil {
			return uid
		}
	}
	return os.Getuid()
}
