//go:build unix

package walltime

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup puts the child in its OWN process group.
//
// That is what makes the same-PGID signal of contract §3.3 possible at all: a
// child sharing the wrapper's group could not be signalled without signalling
// the wrapper, and a negative-PGID signal would reach the runner's own work.
func ownProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// childProcessGroup reads the group the child actually landed in, rather than
// assuming it equals the child's pid. They normally coincide under Setpgid,
// but the drain signals whatever the kernel reports.
func childProcessGroup(cmd *exec.Cmd) (int, error) {
	if cmd.Process == nil {
		return 0, errNoChildProcess
	}
	return syscall.Getpgid(cmd.Process.Pid)
}
