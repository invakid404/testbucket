package walltime

import (
	"bufio"
	"net"
	"os"
	"strings"
	"time"
)

// supervisorSocket is the supervisor this wrapper must ask, or "" when it is
// running without one.
func supervisorSocket() string { return strings.TrimSpace(os.Getenv(SupervisorSocketEnv)) }

// askSupervisor sends one request and returns the reply.
//
// One request, one reply, one connection. The wrapper holds no capability of
// its own here — it asks a process running under a credential it does not have
// — so a wrapper the measured workload started can obtain exactly the
// operations the supervisor's policy permits and nothing else.
func askSupervisor(req SupervisorRequest) (SupervisorReply, error) {
	socket := supervisorSocket()
	if socket == "" {
		return SupervisorReply{}, os.ErrNotExist
	}
	conn, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return SupervisorReply{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	b, err := EncodeSupervisorRequest(req)
	if err != nil {
		return SupervisorReply{}, err
	}
	if _, err := conn.Write(b); err != nil {
		return SupervisorReply{}, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return SupervisorReply{}, err
	}
	return DecodeSupervisorReply(line)
}

// supervisedContainment asks the supervisor to create one, returning false
// when no supervisor is configured so the caller falls back to its own
// unprivileged creation — which is recorded honestly as workload-writable and
// refused for scoring.
func supervisedContainment(name string, parent *ContainmentIdentity, run RunIdentity) (Containment, bool, error) {
	if supervisorSocket() == "" {
		return nil, false, nil
	}
	req := SupervisorRequest{Kind: SupervisorCreateContainment, Run: run, Name: name}
	if parent != nil {
		req.Parent = parent.ID
	}
	rep, err := askSupervisor(req)
	if err != nil {
		return nil, true, err
	}
	if rep.Containment == nil {
		return nil, true, os.ErrInvalid
	}
	cont, err := AttachContainment(*rep.Containment)
	if err != nil {
		return nil, true, err
	}
	return &supervisedProxy{Containment: cont, run: run, id: *rep.Containment}, true, nil
}

// supervisedProxy is a containment whose PRIVILEGED operations go back to the
// supervisor. Reads are local — the containment is readable by design, and a
// producer must take its own observations — while admission and destruction
// are the supervisor's, because they are the operations a workload holding
// them could use to rewrite its own membership history.
type supervisedProxy struct {
	Containment
	run RunIdentity
	id  ContainmentIdentity
}

func (s *supervisedProxy) Identity() ContainmentIdentity { return s.id }

func (s *supervisedProxy) Admit(pid int) error {
	_, err := askSupervisor(SupervisorRequest{
		Kind: SupervisorAdmit, Run: s.run, Containment: s.id.ID, PID: pid,
	})
	return err
}

func (s *supervisedProxy) Destroy() error {
	_, err := askSupervisor(SupervisorRequest{
		Kind: SupervisorDestroy, Run: s.run, Containment: s.id.ID,
	})
	return err
}

// supervisedRegisterKey asks the supervisor to countersign one producer
// registration and appends the result.
//
// This is the whole of F1's repair. The run key is the capability that makes a
// lower-level producer admissible, and a wrapper the measured workload started
// must never hold it — so the wrapper sends the entry it wants registered and
// the supervisor, which does hold the key, decides. It signs each role once,
// so a second party claiming a role that already has an attributable holder is
// refused rather than recorded.
func supervisedRegisterKey(dir string, e KeyLogEntry, run RunIdentity) (bool, error) {
	if supervisorSocket() == "" {
		return false, nil
	}
	rep, err := askSupervisor(SupervisorRequest{Kind: SupervisorAuthorizeKey, Run: run, Entry: &e})
	if err != nil {
		return true, err
	}
	if rep.Entry == nil {
		return true, os.ErrInvalid
	}
	return true, appendKeyLogEntry(dir, *rep.Entry)
}
