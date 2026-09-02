//go:build !linux

package walltime

// SelfContainment cannot be answered off Linux: there is no cgroup-v2
// hierarchy to read this process's own membership out of, and such a host has
// no scorable containment either.
func SelfContainment() (*ContainmentIdentity, bool) { return nil, false }
