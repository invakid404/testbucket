package walltime

import (
	"bufio"
	"crypto/ed25519"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// SupervisorOptions configures the privileged half.
type SupervisorOptions struct {
	// Socket is the request socket. It is created with a mode that the
	// measured workload's credential cannot open.
	Socket string
	// Root is the delegated cgroup-v2 subtree this supervisor owns.
	Root string
	// Run is the single measurement it serves.
	Run RunIdentity
	// RunKey countersigns producer registrations. It never leaves this
	// process, which is the entire point: the wrapper that asks for a
	// signature runs in the measured workload's credential domain, and a key
	// delivered there is a key the workload holds.
	RunKey ed25519.PrivateKey
	// WorkloadUID is the credential the measured work runs as. It must NOT be
	// this process's own, or there is no boundary to enforce.
	WorkloadUID int
	// WrapperGID owns the socket, so the wrapper may ask and the workload may
	// not. Zero leaves the socket owner-only.
	WrapperGID int
}

// RunSupervisor serves privileged containment and signer operations for one
// measurement.
//
// It exists because of a fact about the runner rather than a preference: the
// wrapper and the measured workload used to share one credential, so every
// capability the evidence depends on — creating a containment whose membership
// the workload cannot rewrite, and countersigning the producer keys that sign
// the records — was a capability the workload also had. No arrangement of
// files or environment variables fixes that; the boundary has to be a
// different credential holding the capability, which is what this process is.
//
// It refuses to run without one. A supervisor sharing the workload's uid
// enforces nothing and would only make the absence harder to see.
func RunSupervisor(opt SupervisorOptions) error {
	if opt.WorkloadUID <= 0 {
		return fmt.Errorf("walltime: the supervisor needs the workload's uid; without it there is no credential boundary to enforce")
	}

	if opt.WorkloadUID == os.Getuid() {
		return fmt.Errorf("walltime: the supervisor runs as uid %d and the measured workload runs as the same uid; a supervisor that shares the workload's credential enforces nothing", opt.WorkloadUID)
	}
	if opt.RunKey == nil {
		return fmt.Errorf("walltime: the supervisor needs the run key; countersigning producer registrations is the capability it exists to hold")
	}
	if err := os.MkdirAll(filepath.Dir(opt.Socket), 0o755); err != nil {
		return err
	}
	_ = os.Remove(opt.Socket)
	ln, err := net.Listen("unix", opt.Socket)
	if err != nil {
		return fmt.Errorf("walltime: supervisor socket: %w", err)
	}
	defer ln.Close()
	// The workload's credential must not be able to open it. 0660 with the
	// wrapper's group is the boundary; without a group the socket stays
	// owner-only, which is stricter and still correct.
	mode := os.FileMode(0o600)
	if opt.WrapperGID > 0 {
		mode = 0o660
		if err := os.Chown(opt.Socket, os.Getuid(), opt.WrapperGID); err != nil {
			return fmt.Errorf("walltime: supervisor socket group: %w", err)
		}
	}
	if err := os.Chmod(opt.Socket, mode); err != nil {
		return fmt.Errorf("walltime: supervisor socket mode: %w", err)
	}

	policy := NewSupervisorPolicy(opt.Run, opt.Root, opt.WorkloadUID)
	var mu sync.Mutex
	conts := map[string]Containment{}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return nil
		}
		go func() {
			defer conn.Close()
			r := bufio.NewReader(conn)
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			mu.Lock()
			rep := serveSupervisorRequest(policy, conts, opt, line)
			mu.Unlock()
			if b, err := EncodeSupervisorReply(rep); err == nil {
				_, _ = conn.Write(b)
			}
		}()
	}
}

// serveSupervisorRequest answers one request under the policy.
func serveSupervisorRequest(policy *SupervisorPolicy, conts map[string]Containment, opt SupervisorOptions, line []byte) SupervisorReply {
	req, err := DecodeSupervisorRequest(line)
	if err != nil {
		return SupervisorReply{Error: err.Error()}
	}
	refuse := func(err error) SupervisorReply { return SupervisorReply{Error: err.Error()} }
	switch req.Kind {
	case SupervisorCreateContainment:
		if err := policy.CheckCreate(req); err != nil {
			return refuse(err)
		}
		var parent *ContainmentIdentity
		if req.Parent != "" {
			if c, ok := conts[req.Parent]; ok {
				id := c.Identity()
				parent = &id
			}
		}
		root := opt.Root
		if parent == nil {
			// A top-level containment goes directly under the delegated root
			// this supervisor owns, which the workload cannot write.
			if err := os.Setenv(cgroupRootEnv, root); err != nil {
				return refuse(err)
			}
		}
		cont, err := NewContainment(req.Name, parent)
		if err != nil {
			return refuse(err)
		}
		id := cont.Identity()
		conts[id.ID] = cont
		policy.Note(id.ID)
		return SupervisorReply{OK: true, Containment: &id}

	case SupervisorAdmit:
		if err := policy.CheckAdmit(req, cgroupOfPID(req.PID)); err != nil {
			return refuse(err)
		}
		cont, ok := conts[req.Containment]
		if !ok {
			return refuse(fmt.Errorf("containment %q is not open here", req.Containment))
		}
		if err := cont.Admit(req.PID); err != nil {
			return refuse(err)
		}
		return SupervisorReply{OK: true}

	case SupervisorAuthorizeKey:
		entry, err := policy.Authorize(req, opt.RunKey)
		if err != nil {
			return refuse(err)
		}
		return SupervisorReply{OK: true, Entry: entry}

	case SupervisorDestroy:
		if !policy.Owns(req.Containment) {
			return refuse(fmt.Errorf("containment %q was not created by this supervisor", req.Containment))
		}
		if cont, ok := conts[req.Containment]; ok {
			if err := cont.Destroy(); err != nil {
				return refuse(err)
			}
			delete(conts, req.Containment)
		}
		return SupervisorReply{OK: true}
	}
	return refuse(fmt.Errorf("unknown supervisor request %q", req.Kind))
}

// cgroupOfPID reads which cgroup a process is currently in, so an admission
// request can be refused when it would MOVE a process that is already inside
// one of this run's containments.
func cgroupOfPID(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cgroup")
	if err != nil {
		return ""
	}
	rel, ok := unifiedCgroupLine(string(b))
	if !ok {
		return ""
	}
	dir, ok := absoluteCgroupPath(rel)
	if !ok {
		return ""
	}
	return dir
}
