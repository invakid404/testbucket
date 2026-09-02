package walltime

import (
	"sync"
	"time"
)

// identitySampler watches the measured child WHILE IT RUNS.
//
// The wrapper used to sample the child's identity once, at admission, and
// write that same struct into both the opening and the closing process-tree
// record. Two records carrying one sample cannot disagree, so reparenting, a
// session change, a process-group change and a start-identity change — every
// transition the contract makes terminal — were unobservable by construction.
//
// After `cmd.Wait` returns the child has been reaped and there is nothing left
// in `/proc` to read, so the last moment those facts exist is while the child
// is alive. This samples them there, and keeps the last successful read: the
// closing record then carries an identity that was observed rather than
// copied, and the verifier can compare the two.
//
// It samples the containment MEMBERSHIP with it, because that is where a
// descendant which existed during the interval and exited before the drain is
// visible at all. An empty final containment proves nothing escaped; it cannot
// reconstruct what was there.
type identitySampler struct {
	pid    int
	parent int
	cont   Containment
	stopCh chan struct{}
	done   chan struct{}

	mu       sync.Mutex
	identity ProcIdentity
	event    *RawEvent
}

func newIdentitySampler(pid int, cont Containment, parent int) *identitySampler {
	return &identitySampler{
		pid: pid, parent: parent, cont: cont,
		stopCh: make(chan struct{}), done: make(chan struct{}),
	}
}

func (s *identitySampler) start() {
	go func() {
		defer close(s.done)
		for {
			s.sample()
			select {
			case <-s.stopCh:
				// One final read: the child may have been alive until the
				// moment the wait returned, and that sample is the most
				// recent state anyone can retain.
				s.sample()
				return
			case <-time.After(pollInterval):
			}
		}
	}()
}

// sample takes one reading and keeps it only if the process still exists. A
// read of a departed process returns zeros, and overwriting a real observation
// with those would erase the evidence rather than update it.
func (s *identitySampler) sample() {
	start := processStartID(s.pid)
	pgid := processGroupOf(s.pid)
	if start == "" && pgid == 0 {
		return
	}
	id := ProcIdentity{
		PID:       s.pid,
		PGID:      pgid,
		StartID:   start,
		SessionID: processSessionOf(s.pid),
		ParentPID: s.parent,
	}
	ev, _, err := s.cont.Observe(string(ProducerPhysical))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identity = id
	if err == nil {
		copyOf := ev
		s.event = &copyOf
	}
}

// stop ends the sampler and returns the last observed identity and raw
// containment read. A zero identity means the child was never observed alive
// after admission, which is itself a fact the record carries.
func (s *identitySampler) stop() (ProcIdentity, *RawEvent) {
	close(s.stopCh)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.identity, s.event
}
