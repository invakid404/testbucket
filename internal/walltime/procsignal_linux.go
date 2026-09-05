//go:build linux

package walltime

import "syscall"

// pidfd_open and pidfd_send_signal. The numbers are the same on every Linux
// architecture this project builds for, and an older kernel answers ENOSYS,
// which the caller treats as "no pidfd here" rather than as a failure.
const (
	sysPidfdOpen       = 434
	sysPidfdSendSignal = 424
)

// signalByIdentity signals a process THROUGH A HANDLE rather than through its
// number, and reports whether it did.
//
// Comparing a start identity and then calling kill(2) leaves a gap: between
// the comparison and the signal the process may exit and the kernel may hand
// its pid to something else, so the check says one thing and the signal
// reaches another. The window is small and the consequence — killing the
// runner's unrelated work — is not.
//
// pidfd_open pins THIS process: the descriptor keeps referring to it even
// after the pid is released, and never to a later process that inherits the
// number. So the identity is re-read AFTER the pin, and a pid recycled before
// the pin is detected rather than acted on. From there the signal goes to the
// descriptor, which cannot be redirected by anything the pid does.
//
// A false return means the caller should fall back: the kernel is too old, or
// the process is already gone, or its identity did not survive the check.
func signalByIdentity(pid int, start string, sig syscall.Signal) bool {
	if pid <= 0 || start == "" {
		return false
	}
	fd, _, errno := syscall.Syscall(sysPidfdOpen, uintptr(pid), 0, 0)
	if errno != 0 {
		return false
	}
	defer syscall.Close(int(fd))
	// VERIFY AFTER PINNING. Reading the identity now, with the descriptor
	// already held, is what makes this an atomic capability rather than a
	// faster version of the same race.
	if processStartID(pid) != start {
		return false
	}
	_, _, errno = syscall.Syscall6(sysPidfdSendSignal, fd, uintptr(sig), 0, 0, 0, 0)
	return errno == 0
}
