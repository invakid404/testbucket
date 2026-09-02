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
//
// It takes the workload credential as an ARGUMENT rather than reading the
// accounts files itself. The facts are resolved once, retained on the
// containment identity, and this same rule is rerun by the verifier over the
// retained copy: a decision made from `/etc/group` at decision time is one
// nobody can reproduce from the records.
func membershipControl(dir string, w WorkloadCredential) string {
	facts, ok := containmentFacts(dir)
	if !ok {
		return MembershipUnknown
	}
	facts.SelfUID = os.Getuid()
	facts.WorkloadUIDs, facts.WorkloadGIDs = w.UIDs, w.GIDs
	return membershipModelFor(facts)
}

// containmentFacts reads the owner and mode of a containment's `cgroup.procs`
// — the inputs the membership rule runs over.
func containmentFacts(dir string) (MembershipFacts, bool) {
	info, err := os.Stat(filepath.Join(dir, "cgroup.procs"))
	if err != nil {
		return MembershipFacts{}, false
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return MembershipFacts{}, false
	}
	perm := info.Mode().Perm()
	return MembershipFacts{
		OwnerUID: sys.Uid, OwnerGID: sys.Gid,
		GroupWritable: perm&0o020 != 0, OtherWritable: perm&0o002 != 0,
	}, true
}

// resolveWorkloadCredential resolves the DECLARED workload account to the
// credential facts the membership rule needs: its uid, its primary group and
// every supplementary group it belongs to.
//
// It is read rather than declared because the question the boundary turns on
// is whether the WORKLOAD is in the group that may write the delegated
// subtree, and a caller-supplied answer to that is a caller-supplied boundary.
// It resolves the account's UID too: naming the account without resolving it
// left the uid set empty, so the rule fell back to this process's own
// credential and the declaration decided nothing.
func resolveWorkloadCredential(user string) WorkloadCredential {
	var w WorkloadCredential
	w.UID = -1
	user = strings.TrimSpace(user)
	// Additional uids the caller says the workload may also run as. They can
	// only WIDEN the set known to lack the boundary.
	for _, field := range strings.Split(os.Getenv(WorkloadUIDEnv), ",") {
		if uid, err := strconv.Atoi(strings.TrimSpace(field)); err == nil {
			w.UIDs = append(w.UIDs, uid)
		}
	}
	if user == "" {
		return w
	}
	if b, err := os.ReadFile("/etc/passwd"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Split(line, ":")
			if len(f) > 3 && f[0] == user {
				if uid, err := strconv.Atoi(f[2]); err == nil {
					w.UID = uid
					w.UIDs = append(w.UIDs, uid)
				}
				if gid, err := strconv.Atoi(f[3]); err == nil {
					w.GIDs = append(w.GIDs, gid)
				}
			}
		}
	}
	b, err := os.ReadFile("/etc/group")
	if err != nil {
		return w
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
				w.GIDs = append(w.GIDs, gid)
			}
		}
	}
	return w
}
