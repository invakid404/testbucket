//go:build unix

package walltime

import (
	"errors"
	"syscall"
	"time"
)

const (
	sigTERM = syscall.SIGTERM
	sigKILL = syscall.SIGKILL
)

// groupProbeSupported reports whether the platform can be asked whether a
// process group still has members. On unix, signal 0 answers exactly that.
func groupProbeSupported() bool { return true }

// groupExists probes for the group's continued existence with the null signal.
// ESRCH means no member remains; EPERM means a member remains that we may not
// signal, which is still a member.
func groupExists(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	if err == nil {
		return true
	}
	return !errors.Is(err, syscall.ESRCH)
}

func signalGroup(pgid int, sig syscall.Signal) error {
	// The negative PGID is what makes this a GROUP signal rather than a signal
	// to one process.
	return syscall.Kill(-pgid, sig)
}

func isNoSuchProcess(err error) bool { return errors.Is(err, syscall.ESRCH) }

func monotonicNow() time.Time                  { return time.Now() }
func monotonicSince(t time.Time) time.Duration { return time.Since(t) }
