//go:build !linux

package walltime

import "syscall"

// signalByIdentity has no portable equivalent: there is no way here to pin a
// process and then confirm what was pinned. Reporting false keeps the caller
// on the path that requires a retained OS handle, which is the only ownership
// this platform can actually establish.
func signalByIdentity(int, string, syscall.Signal) bool { return false }
