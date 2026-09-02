package walltime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"syscall"
)

// Membership-control models: WHO may write the containment's `cgroup.procs`,
// which on cgroup-v2 is the process-migration control.
//
// The contract requires a scored containment whose membership the workload
// cannot modify. Stage 1 used to simply assert that property in a sentence
// while the documented setup gave the delegated subtree to the runner uid —
// the same uid the measured workload runs as — so the workload held exactly
// the capability the sentence denied it. It could migrate itself or its
// descendants between the action, script, invocation and sibling containments
// and defeat the nested membership history the whole envelope rests on.
//
// So the model is now a MEASURED FACT carried on the containment identity, and
// a run whose workload shares the migration capability is recorded in full and
// reported ineligible rather than scored. A wrapper cannot grant itself a
// privilege it does not have; what it can do is refuse to claim a property it
// cannot demonstrate.
const (
	// MembershipSupervisorOwned means `cgroup.procs` is owned by a credential
	// the measured workload does not have, and is not writable by group or
	// other.
	MembershipSupervisorOwned = "supervisor-owned"
	// MembershipWorkloadWritable means the workload can write `cgroup.procs`
	// — because it owns it, or because the mode grants it. Unscorable.
	MembershipWorkloadWritable = "workload-writable"
	// MembershipUnknown means this platform cannot establish the model. Also
	// unscorable: an unestablished boundary is not a boundary.
	MembershipUnknown = "unknown"
)

// WorkloadUIDEnv names the uid the MEASURED WORKLOAD runs as, when the caller
// runs it under a credential other than the wrapper's.
//
// It is what makes a real boundary expressible: with the workload on a
// different uid from the delegated subtree's owner, the wrapper can migrate
// processes and the workload cannot. Unset means the workload shares this
// process's credential, which is the shape that cannot be scored.
const WorkloadUIDEnv = "TB_WALL_WORKLOAD_UID"

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

// RawEvent is one producer's OWN observation of a containment transition. Two
// producers watching the same transition each take their own read and mint
// their own event id: the contract requires the peer and the trace to agree
// about the lifecycle without either copying the other's evidence.
type RawEvent struct {
	// ID is unique to this observation — never shared between producers.
	ID string
	// Digest covers the raw bytes read together with the observer identity, so
	// two independent reads of the same kernel file still yield distinct
	// evidence digests.
	Digest Digest
	// Source is the taxonomy class; only SourceContainment and
	// SourceProcessLifecycle may delimit a lifecycle.
	Source string
	// Bytes is the EXACT kernel output this observation was derived from, and
	// it is retained rather than hashed away. A digest proves a record was not
	// edited; it does not let anyone else re-read what the kernel actually
	// said. The contract asks for retained raw evidence, and a digest of
	// discarded bytes is not evidence, it is a receipt for evidence.
	Bytes []byte
	// Procs is the containment membership snapshot taken with the same read:
	// which processes were in the containment when this transition was
	// observed. "Populated: no" with a membership list is checkable; a boolean
	// is a claim.
	Procs []int
	// ProcsBytes is the EXACT `cgroup.procs` output the snapshot was read
	// from, and ProcsDigest binds those bytes to this observation's own event
	// id. A snapshot with no retained bytes is a claim; these make an empty
	// containment's snapshot as checkable as a populated one.
	ProcsBytes  []byte
	ProcsDigest Digest
}

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
	Procs() ([]int, error)
	// Observe takes one fresh raw read of the containment state and returns
	// both the observation and whether the containment is populated. observer
	// distinguishes the producers so their event ids can never collide.
	Observe(observer string) (RawEvent, bool, error)
	// Signal forwards a signal to every member.
	Signal(sig syscall.Signal) error
	// Destroy removes the containment after it is verified empty.
	Destroy() error
}

// NewContainment creates a dedicated containment for one level.
//
// parent, when set, is the enclosing containment this one is created INSIDE.
// That nesting is not decoration: an invocation containment created beside the
// script containment instead of under it would take the invocation's processes
// out of the script's lifecycle, and the trace would then be bracketing work
// that had left the interval it claims to measure.
//
// A host with no delegated cgroup-v2 tree gets the unscored process-group
// fallback rather than an error: the run still produces a full receipt, and
// the verifier is what refuses to score it.
func NewContainment(name string, parent *ContainmentIdentity) (Containment, error) {
	return newContainment(name, parent)
}

// newRawEventID mints an observation id that no other producer can reproduce.
func newRawEventID(observer string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A failure here would make two observations indistinguishable, which
		// is exactly the substitution the verifier must be able to detect.
		panic(fmt.Sprintf("walltime: raw event id: %v", err))
	}
	return observer + ":" + hex.EncodeToString(b[:])
}

// AttachContainment binds an ALREADY CREATED containment by its identity. It
// is how an independent observer watches the same lifecycle the physical
// wrapper created without being handed the wrapper's object — and how a
// mismatched inode (a containment that was destroyed and re-created under the
// same path) is caught rather than silently observed.
// newContainmentEvent builds the raw event for ONE `cgroup.events` read.
//
// The derivation lives here, portably, because three places must agree about
// it exactly: the Linux producer that reads the kernel file, the verifier that
// re-derives the digest from the retained bytes, and the tests that prove the
// two agree. When the producer owned a private copy of this arithmetic, a test
// could only restate it — and restating it is how a verifier came to demand a
// containment state the producer can never observe.
func newContainmentEvent(observer string, b, procsBytes []byte) RawEvent {
	id := newRawEventID(observer)
	return RawEvent{
		ID:     id,
		Digest: DigestBytes(append([]byte(id+"\x00"), b...)),
		Source: SourceContainment,
		Bytes:  b,
		// The membership snapshot is EVIDENCE, on the same footing as the
		// events read: its exact bytes and a digest binding them to this
		// observer's event id. A successful read of an empty containment
		// therefore still carries something nobody can write without having
		// taken it, which is what distinguishes it from a read that never
		// happened.
		Procs:       parseCgroupProcs(procsBytes),
		ProcsBytes:  procsBytes,
		ProcsDigest: DigestBytes(append([]byte(id+"\x00"), procsBytes...)),
	}
}

// parseCgroupProcs reads a `cgroup.procs` file into the pids it lists.
//
// It returns a NON-NIL slice for a successful read, empty file included. That
// is the whole point: nil means no snapshot was taken, and an empty
// containment must not be indistinguishable from an unread one.
func parseCgroupProcs(b []byte) []int {
	out := []int{}
	for _, line := range strings.Fields(string(b)) {
		if pid, err := strconv.Atoi(line); err == nil {
			out = append(out, pid)
		}
	}
	return out
}

func AttachContainment(ident ContainmentIdentity) (Containment, error) {
	switch ident.Primitive {
	case PrimitiveProcessGroup:
		return &processGroup{pgid: ident.RootPID, ident: ident, reason: "attached"}, nil
	case PrimitiveCgroup2:
		return attachCgroup2(ident)
	default:
		return nil, fmt.Errorf("walltime: unknown containment primitive %q", ident.Primitive)
	}
}
