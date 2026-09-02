package walltime

import (
	"os"
	"strconv"
	"strings"
)

// processUIDOf is the real uid a process is running under, read from
// /proc/<pid>/status.
//
// It is read rather than taken from the caller because the whole boundary
// turns on it: a containment owned by one credential and a measured process
// running under another is the separation itself, and a declaration of that
// separation is not the separation.
func processUIDOf(pid int) int {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		f := strings.Fields(line)
		// Uid: real effective saved fs — the REAL uid is what the process is,
		// whatever it may have become for one operation.
		if len(f) > 1 {
			if uid, err := strconv.Atoi(f[1]); err == nil {
				return uid
			}
		}
	}
	return -1
}
