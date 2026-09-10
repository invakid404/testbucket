package walltime

import "syscall"

// Containment primitives. Only PrimitiveCgroup2 can delimit a SCORED
// lifecycle: it is the one primitive here whose membership the workload cannot
// modify and whose emptiness the kernel reports as an event rather than as a
// guess.
const (
	// PrimitiveCgroup2 is a dedicated cgroup-v2 subtree.
	PrimitiveCgroup2 = "cgroup2"
	// PrimitiveProcessGroup is the diagnostic fallback for a platform or a
	// runner with no delegated cgroup tree. A workload can leave a process
	// group at will, so a lifecycle delimited by one is NEVER scored — it
	// exists so a developer run still produces an honest, complete, ineligible
	// receipt.
	PrimitiveProcessGroup = "process_group_unscored"
)

// Containment is the level-owned process container. The physical wrapper
// creates it, admits the child before the child can run, and the verifier —
// never the wrapper — decides when it is empty.
type Containment interface {
	// Identity is the stable containment identity every producer must name.
	Identity() ContainmentIdentity
	// Admit places a process in the containment. It is called BEFORE the child
	// is allowed to execute; a child that starts first is unaccounted, which is
	// terminal.
	Admit(pid int) error
	// Procs snapshots current membership.
	//
	// Observe and Freeze are gone with the cgroup-v2 primitive: they existed
	// so two independent observers could each take their own raw kernel read
	// of a containment neither could migrate out of, and there are no
	// independent observers.
	Procs() ([]int, error)
	// Signal forwards a signal to every member.
	Signal(sig syscall.Signal) error
	// Destroy removes the containment after it is verified empty.
	Destroy() error
}

func NewContainment(name string, parent *ContainmentIdentity) (Containment, error) {
	return newContainment(name, parent)
}
