package walltime

import (
	"os"
	"strconv"
	"sync"
	"syscall"
)

// processGroup is the UNSCORED containment fallback. It records everything a
// cgroup containment would, and its identity says plainly that it is a process
// group: the verifier refuses to score any lifecycle it delimits, so a run on
// a laptop or on a runner with no delegated cgroup tree produces a complete,
// honest, ineligible receipt instead of a plausible number.
//
// It is not a "degraded mode" in the usual sense. It cannot see a descendant
// that left the group, which is precisely the escape the scored primitive
// exists to detect, so its emptiness answer is advisory and marked as such.
type processGroup struct {
	mu     sync.Mutex
	pgid   int
	reason string
	ident  ContainmentIdentity
}

func newProcessGroupContainment(name, reason string) (Containment, error) {
	self := os.Getpid()
	return &processGroup{
		reason: reason,
		ident: ContainmentIdentity{
			Primitive: PrimitiveProcessGroup,
			ID:        name,
			BootID:    bootIdentity(),
			RootPID:   self,
			RootStart: processStartID(self),
		},
	}, nil
}

func (p *processGroup) Identity() ContainmentIdentity { return p.ident }

// Admit records the process group of the child. There is nothing to write:
// membership is established by the child's own setpgid at spawn.
func (p *processGroup) Admit(pid int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pgid = pid
	p.ident.RootPID = pid
	p.ident.RootStart = processStartID(pid)
	return nil
}

// Procs cannot enumerate a process group portably, so it reports nothing
// rather than guessing. The verifier treats an unenumerable membership as
// unscorable.
func (p *processGroup) Procs() ([]int, error) { return nil, nil }

func (p *processGroup) Observe(observer string) (RawEvent, bool, error) {
	p.mu.Lock()
	pgid := p.pgid
	p.mu.Unlock()
	populated := false
	if pgid != 0 {
		// signal 0 probes for existence without delivering anything.
		populated = syscall.Kill(-pgid, 0) == nil
	}
	id := newRawEventID(observer)
	state := "populated=" + strconv.FormatBool(populated) + ";pgid=" + strconv.Itoa(pgid)
	return RawEvent{
		ID:    id,
		Bytes: []byte(state),
		// No membership snapshot: a process group cannot be enumerated
		// portably, and Procs stays nil so the absence is visible rather than
		// presented as an empty containment. Such a run is unscorable anyway.
		Digest: DigestBytes([]byte(id + "\x00" + state)),
		// A process-group probe is a process-lifecycle observation, not a
		// containment event; naming it honestly is what lets the verifier see
		// that no containment evidence exists.
		Source: SourceProcessLifecycle,
	}, populated, nil
}

func (p *processGroup) Signal(sig syscall.Signal) error {
	p.mu.Lock()
	pgid := p.pgid
	p.mu.Unlock()
	if pgid == 0 {
		return nil
	}
	if err := syscall.Kill(-pgid, sig); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}

func (p *processGroup) Destroy() error { return nil }

// Reason explains why the scored primitive was unavailable. It is carried into
// the receipt so an ineligible run says why.
func (p *processGroup) Reason() string { return p.reason }
