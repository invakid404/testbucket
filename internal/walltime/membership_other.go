//go:build !linux

package walltime

// membershipControl cannot be established off Linux: there is no cgroup-v2
// `cgroup.procs` to read, and such a host has no scorable containment anyway.
func membershipControl(string, WorkloadCredential) string { return MembershipUnknown }

// resolveWorkloadCredential has no accounts database to read here. The
// declaration is retained as the caller made it, and the run is unscorable for
// want of a containment primitive long before the credential matters.
func resolveWorkloadCredential(string) WorkloadCredential { return WorkloadCredential{UID: -1} }
