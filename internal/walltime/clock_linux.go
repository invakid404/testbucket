package walltime

import (
	"syscall"
	"unsafe"
)

// clockMonotonic is CLOCK_MONOTONIC, the clock the contract names. It is read
// through the raw syscall rather than through Go's time package because the
// value has to be an ABSOLUTE point on the host's boot timeline: the physical
// wrapper, the containment peer and the trace collector are separate
// processes, and their endpoints are compared to each other.
const clockMonotonic = 1

// readMonotonic takes one fresh clock_gettime(CLOCK_MONOTONIC) reading.
func readMonotonic() (int64, string) {
	var ts syscall.Timespec
	_, _, errno := syscall.Syscall(syscall.SYS_CLOCK_GETTIME, uintptr(clockMonotonic), uintptr(unsafe.Pointer(&ts)), 0)
	if errno != 0 {
		// A kernel that cannot answer clock_gettime is not a kernel we score
		// on; fall back to the process-relative reading, which the verifier
		// refuses.
		return processRelativeNanos(), ClockProcessRelative
	}
	return ts.Sec*1e9 + ts.Nsec, ClockMonotonic
}

// platformBootIdentity is unused on Linux: /proc/sys/kernel/random/boot_id is
// the authority there.
func platformBootIdentity() string { return "" }
