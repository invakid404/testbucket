package walltime

import "syscall"

// containmentSysProc gives the child its own process group, which is what the
// wrapper signals and drains. A descendant that calls setsid or double-forks
// leaves it; that is a stated limitation of the measurement, recorded rather
// than claimed away.
func containmentSysProc(cont Containment) (*syscall.SysProcAttr, func(), error) {
	return &syscall.SysProcAttr{Setpgid: true}, func() {}, nil
}

func postSpawnAdmit(cont Containment, pid int) error { return cont.Admit(pid) }

// joinContainment has nothing to join: a process group is joined at spawn.
func joinContainment(ident ContainmentIdentity, pid int) error { return nil }

func processGroupOf(pid int) int {
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return 0
	}
	return pgid
}

// processParentOf cannot be read portably; the caller falls back to the parent
// it knows it is.
func processParentOf(int) int { return 0 }
