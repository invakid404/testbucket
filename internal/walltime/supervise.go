package walltime

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"strings"
)

// SupervisorSocketEnv names the supervisor's request socket.
//
// Its presence is what makes a scored row possible at all. Without it the
// wrapper creates its own containments and registers its own keys, which on a
// single-credential runner means the measured workload holds every capability
// the evidence rests on — so the row is recorded in full and refused.
const SupervisorSocketEnv = "TB_WALL_SUPERVISOR"

// Supervisor request kinds. Each is one privileged operation the wrapper
// cannot perform for itself and the workload must never perform at all.
const (
	// SupervisorCreateContainment asks for a fresh cgroup owned by the
	// supervisor's credential: the wrapper may read it and ask for admissions,
	// and no workload credential may write its cgroup.procs.
	SupervisorCreateContainment = "create-containment"
	// SupervisorAdmit asks for one process to be placed in a containment the
	// supervisor created.
	SupervisorAdmit = "admit"
	// SupervisorAuthorizeKey asks for a producer key to be countersigned into
	// the key log.
	SupervisorAuthorizeKey = "authorize-key"
	// SupervisorDestroy releases a containment after it is verified empty.
	SupervisorDestroy = "destroy"
)

// SupervisorRequest is one privileged operation.
type SupervisorRequest struct {
	Kind string `json:"kind"`
	// Run identifies the measurement this request belongs to. The supervisor
	// serves exactly one run and refuses any other, so a second measurement
	// cannot borrow the first one's authority.
	Run RunIdentity `json:"run"`
	// Name is the containment name for a create request.
	Name string `json:"name,omitempty"`
	// Parent is the containment a create request nests under. It must be one
	// this supervisor created; nesting under anything else would let a caller
	// place a lifecycle outside the tree the supervisor owns.
	Parent string `json:"parent,omitempty"`
	// Containment and PID are the admit request's target.
	Containment string `json:"containment,omitempty"`
	PID         int    `json:"pid,omitempty"`
	// Entry is the key-log registration to countersign.
	Entry *KeyLogEntry `json:"entry,omitempty"`
}

// SupervisorReply is the answer.
type SupervisorReply struct {
	OK          bool                 `json:"ok"`
	Error       string               `json:"error,omitempty"`
	Containment *ContainmentIdentity `json:"containment,omitempty"`
	Entry       *KeyLogEntry         `json:"entry,omitempty"`
}

// SupervisorPolicy is the decision half of the supervisor, separated from the
// privileged half so it can be exercised on any host.
//
// What the supervisor may do is not "whatever it is asked". It holds the run
// key and the authority to create containments, and the party asking is a
// wrapper the measured workload started — so the policy is what keeps a
// capability the workload cannot hold from becoming one it can simply request.
type SupervisorPolicy struct {
	// Run is the single measurement this supervisor serves.
	Run RunIdentity
	// Root is the delegated subtree it owns. Every containment it creates is
	// inside it, and a create request naming a parent outside it is refused.
	Root string
	// WorkloadUID is the credential the measured work runs as. It is recorded
	// so the supervisor can refuse to admit a process that is not a fresh
	// child of the requester, and so the boundary is stated rather than
	// assumed.
	WorkloadUID int
	// created is every containment this supervisor made, by path.
	created map[string]bool
	// authorized is every producer identity it has already countersigned. A
	// second registration for one role is a second producer claiming it.
	authorized map[string]bool
}

// NewSupervisorPolicy starts a policy for one run.
func NewSupervisorPolicy(run RunIdentity, root string, workloadUID int) *SupervisorPolicy {
	return &SupervisorPolicy{
		Run: run, Root: root, WorkloadUID: workloadUID,
		created: map[string]bool{}, authorized: map[string]bool{},
	}
}

// Note records a containment the supervisor created, so later requests can be
// checked against the tree it owns.
func (p *SupervisorPolicy) Note(path string) { p.created[path] = true }

// Owns reports whether this supervisor created a containment.
func (p *SupervisorPolicy) Owns(path string) bool { return p.created[path] }

// CheckCreate decides one containment-creation request.
func (p *SupervisorPolicy) CheckCreate(req SupervisorRequest) error {
	if err := p.checkRun(req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("a containment request names nothing")
	}
	if strings.ContainsAny(req.Name, "/.") {
		// A name is a leaf, never a path. Accepting one would let a caller
		// place a containment anywhere the supervisor can write.
		return fmt.Errorf("containment name %q is a path, not a name", req.Name)
	}
	if req.Parent == "" {
		return nil
	}
	if !p.created[req.Parent] {
		return fmt.Errorf("containment %q was not created by this supervisor, so nothing may be nested inside it", req.Parent)
	}
	return nil
}

// CheckAdmit decides one admission request.
//
// The supervisor admits into containments it made, for the run it serves, and
// nothing else. It is deliberately not a general migration service: on
// cgroup-v2 the ability to write `cgroup.procs` IS the migration control, and
// the reason the workload does not hold it is that a process able to move
// itself between containments can rewrite the membership history the whole
// envelope rests on.
func (p *SupervisorPolicy) CheckAdmit(req SupervisorRequest, currentCgroup string) error {
	if err := p.checkRun(req); err != nil {
		return err
	}
	if !p.created[req.Containment] {
		return fmt.Errorf("containment %q was not created by this supervisor", req.Containment)
	}
	if req.PID <= 0 {
		return fmt.Errorf("an admission request names no process")
	}
	// A process ALREADY inside one of this run's containments may not be
	// moved. That is the migration the boundary exists to prevent, and asking
	// for it politely is still asking for it.
	if currentCgroup != "" && p.created[currentCgroup] && currentCgroup != req.Containment {
		return fmt.Errorf("process %d is already in containment %q; moving it to %q would rewrite the membership history this envelope records",
			req.PID, currentCgroup, req.Containment)
	}
	return nil
}

// CheckAuthorize decides one key-log countersignature request.
//
// The supervisor holds the run key precisely so the measured workload cannot,
// and a producer identity it signs becomes admissible evidence. So it signs
// each role ONCE: a second registration for the same producer, level and
// sequence is a second party claiming a role that already has an attributable
// holder, which is the substitution the whole signer protocol exists to refuse.
func (p *SupervisorPolicy) CheckAuthorize(req SupervisorRequest) error {
	if err := p.checkRun(req); err != nil {
		return err
	}
	if req.Entry == nil {
		return fmt.Errorf("an authorization request carries no key-log entry")
	}
	e := *req.Entry
	if strings.TrimSpace(e.PublicKey) == "" {
		return fmt.Errorf("an authorization request names no public key")
	}
	if e.Level == LevelAction {
		// Action-level producers are declared in the roster the trusted
		// opening step seals. Countersigning one here would add a signer to a
		// set that is closed by design.
		return fmt.Errorf("action-level signers are predeclared in the roster and are not authorized at runtime")
	}
	role := keyLogAuthority(e)
	if p.authorized[role] {
		return fmt.Errorf("%s already has an authorized signer; a second is a different party claiming the same role", role)
	}
	return nil
}

// Authorize countersigns one entry and records the role as taken.
func (p *SupervisorPolicy) Authorize(req SupervisorRequest, runKey ed25519.PrivateKey) (*KeyLogEntry, error) {
	if err := p.CheckAuthorize(req); err != nil {
		return nil, err
	}
	e := *req.Entry
	if err := e.Authorize(keyLogAuthority(e), runKey); err != nil {
		return nil, err
	}
	p.authorized[keyLogAuthority(e)] = true
	return &e, nil
}

func (p *SupervisorPolicy) checkRun(req SupervisorRequest) error {
	if req.Run.CampaignID != p.Run.CampaignID || req.Run.RunID != p.Run.RunID || req.Run.BucketID != p.Run.BucketID {
		return fmt.Errorf("this supervisor serves run %s/%s/%s, not %s/%s/%s",
			p.Run.CampaignID, p.Run.RunID, p.Run.BucketID,
			req.Run.CampaignID, req.Run.RunID, req.Run.BucketID)
	}
	return nil
}

// EncodeSupervisorRequest and DecodeSupervisorReply are the wire form. It is
// newline-delimited JSON over a unix socket: one request, one reply, one
// connection, so a partial exchange cannot be replayed as a complete one.
func EncodeSupervisorRequest(req SupervisorRequest) ([]byte, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// DecodeSupervisorRequest reads one request.
func DecodeSupervisorRequest(line []byte) (SupervisorRequest, error) {
	var req SupervisorRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return req, fmt.Errorf("supervisor request: %w", err)
	}
	return req, nil
}

// EncodeSupervisorReply writes one reply.
func EncodeSupervisorReply(rep SupervisorReply) ([]byte, error) {
	b, err := json.Marshal(rep)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// DecodeSupervisorReply reads one reply.
func DecodeSupervisorReply(line []byte) (SupervisorReply, error) {
	var rep SupervisorReply
	if err := json.Unmarshal(line, &rep); err != nil {
		return rep, fmt.Errorf("supervisor reply: %w", err)
	}
	if !rep.OK {
		return rep, fmt.Errorf("supervisor refused: %s", rep.Error)
	}
	return rep, nil
}
