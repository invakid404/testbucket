//go:build !linux

package walltime

import "syscall"

// containmentSysProc off Linux can only give the child its own process group.
// That is not containment — a descendant can leave at will — which is why the
// identity says PrimitiveProcessGroup and the verifier will not score it.
func containmentSysProc(cont Containment) (*syscall.SysProcAttr, func(), error) {
	return &syscall.SysProcAttr{Setpgid: true}, func() {}, nil
}

func postSpawnAdmit(cont Containment, pid int) error { return cont.Admit(pid) }

// joinContainment has nothing to join off Linux.
func joinContainment(ident ContainmentIdentity, pid int) error { return nil }

func processGroupOf(pid int) int {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return 0
	}
	return pgid
}

// processSessionOf is the child's session id, read while it is alive.
//
// The contract makes a session or PGID change terminal, and neither is
// decidable from a record that never carried the session. Like the start
// identity it is only readable before the child is reaped.
func processSessionOf(pid int) int {
	sid, err := syscall.Getsid(pid)
	if err != nil {
		return 0
	}
	return sid
}
