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

// processGroupsOf reads a process's real GID and supplementary groups from
// /proc/<pid>/status.
//
// It is the kernel's answer about the process that actually ran, which is what
// decides whether a group-writable containment excluded it. Reading /etc/group
// establishes what a file says; account resolution may not use that file at
// all.
func processGroupsOf(pid int) (int, []int) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return -1, nil
	}
	gid := -1
	var groups []int
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(line, "Gid:"):
			if f := strings.Fields(line); len(f) > 1 {
				if g, err := strconv.Atoi(f[1]); err == nil {
					gid = g
				}
			}
		case strings.HasPrefix(line, "Groups:"):
			for _, f := range strings.Fields(strings.TrimPrefix(line, "Groups:")) {
				if g, err := strconv.Atoi(f); err == nil {
					groups = append(groups, g)
				}
			}
		}
	}
	return gid, groups
}
