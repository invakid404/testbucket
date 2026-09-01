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
