package walltime

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// HoldActionChild is the action-owned child's barrier, and it is INTERNAL:
// nothing but the wrapper starts it.
//
// A containment proof read after `Start` is a race a short-lived command wins:
// the child is gone before `cgroup.procs` can be read, its pid is missing from
// the membership, and the verifier reports a terminal WT-033 about a child
// that was in fact inside the containment all along. Held here, the child is
// alive while the reading is taken.
//
// It reads exactly one byte and then EXECS the command in place, so the pid
// whose membership was read is the pid that runs it. An EOF means the wrapper
// gave up before releasing the barrier, and then the command must not run at
// all: a child whose containment could not be retained is a child the contract
// refuses, the same rule the measured admission protocol applies one level
// down.
func HoldActionChild(argv []string) error {
	if len(argv) > 0 && argv[0] == "--" {
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return fmt.Errorf("walltime: the action-child barrier was given no command; it is started by the wrapper, not by hand")
	}
	gate := os.NewFile(uintptr(ActionChildHoldFD), "action-child-barrier")
	if gate == nil {
		return fmt.Errorf("walltime: no barrier on fd %d; the action-child barrier is started by the wrapper, not by hand", ActionChildHoldFD)
	}
	var b [1]byte
	if _, err := io.ReadFull(gate, b[:]); err != nil {
		return fmt.Errorf("walltime: the wrapper never released the action-child barrier, so no containment proof was retained and this command must not run: %w", err)
	}
	if err := gate.Close(); err != nil {
		return fmt.Errorf("walltime: close the action-child barrier: %w", err)
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("walltime: the action-owned child's command: %w", err)
	}
	return syscall.Exec(path, argv, os.Environ())
}
