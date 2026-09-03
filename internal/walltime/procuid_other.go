//go:build !linux

package walltime

// processUIDOf cannot be read portably, and a host without /proc has no
// scorable containment either.
func processUIDOf(int) int { return -1 }

// processGroupsOf cannot be read portably.
func processGroupsOf(int) (int, []int) { return -1, nil }

// processIsZombie cannot be answered portably; a caller that needs the
// distinction reaps the process instead, which is what reapExitedChild does.
func processIsZombie(int) bool { return false }
