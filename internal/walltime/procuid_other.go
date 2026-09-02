//go:build !linux

package walltime

// processUIDOf cannot be read portably, and a host without /proc has no
// scorable containment either.
func processUIDOf(int) int { return -1 }
