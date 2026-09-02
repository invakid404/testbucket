//go:build !linux

package walltime

// membershipControl cannot be established off Linux: there is no cgroup-v2
// `cgroup.procs` to read, and such a host has no scorable containment anyway.
func membershipControl(string) string { return MembershipUnknown }
