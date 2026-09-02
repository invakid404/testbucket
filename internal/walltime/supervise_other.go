//go:build !linux

package walltime

// unifiedCgroupLine and absoluteCgroupPath are Linux-only concepts; off Linux
// there is no unified hierarchy to read a process's containment out of, and a
// host without one has no scorable containment either.
func unifiedCgroupLine(string) (string, bool) { return "", false }

func absoluteCgroupPath(string) (string, bool) { return "", false }
