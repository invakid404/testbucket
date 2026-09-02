package walltime

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// containmentSysProc builds the child's process attributes so the child is
// CREATED INSIDE the containment rather than moved into it after the fact.
// clone3's CLONE_INTO_CGROUP is what closes the window a move would leave: a
// child that forks between spawn and admission would otherwise be real,
// unaccounted, action-owned work.
func containmentSysProc(cont Containment) (*syscall.SysProcAttr, func(), error) {
	attr := &syscall.SysProcAttr{Setpgid: true}
	ident := cont.Identity()
	if ident.Primitive != PrimitiveCgroup2 {
		return attr, func() {}, nil
	}
	fd, err := syscall.Open(ident.ID, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open containment %s: %w", ident.ID, err)
	}
	attr.UseCgroupFD, attr.CgroupFD = true, fd
	return attr, func() { syscall.Close(fd) }, nil
}

// postSpawnAdmit is a no-op for a real containment (the child was created
// inside it) and records membership for the unscored fallback.
func postSpawnAdmit(cont Containment, pid int) error {
	if cont.Identity().Primitive == PrimitiveCgroup2 {
		return nil
	}
	return cont.Admit(pid)
}

// joinContainment moves THIS process into an enclosing containment before it
// does any work of its own, so a nested wrapper's overhead is inside the
// parent envelope's lifecycle rather than beside it.
func joinContainment(ident ContainmentIdentity, pid int) error {
	if ident.Primitive != PrimitiveCgroup2 {
		return nil
	}
	return os.WriteFile(filepath.Join(ident.ID, "cgroup.procs"), []byte(fmt.Sprint(pid)), 0o644)
}

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
	// Go's Linux syscall package exposes no Getsid wrapper, so the raw call is
	// made directly. errno != 0 means the process is gone, which is the same
	// answer as "no session could be read".
	sid, _, errno := syscall.RawSyscall(syscall.SYS_GETSID, uintptr(pid), 0, 0)
	if errno != 0 {
		return 0
	}
	return int(sid)
}
