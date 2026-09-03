package walltime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
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

// WorkloadUserEnv names the account the measured workload runs as, so the
// wrapper can establish which groups it belongs to.
//
// The group write bit on a delegated subtree is not by itself a hole: a
// subtree owned by root and writable by the WRAPPER's group is the boundary a
// supervised run rests on. It is a hole exactly when the workload is in that
// group, and answering that needs the workload's identity.
const WorkloadUserEnv = "TB_WALL_WORKLOAD_USER"

// WorkloadUIDEnv names ADDITIONAL uids the measured workload may run as, when
// the caller runs it under credentials other than the wrapper's.
//
// It cannot establish the boundary, only widen who is known to lack it. A
// declaration is a caller-controlled string, and a string cannot make the
// workload somebody else: the declaration used to be the only uid compared
// against the owner, so naming any other uid minted `supervisor-owned` for a
// file the wrapper itself owned — a privilege nobody held. So this process's
// own uid still counts as the workload's ALONGSIDE anything declared here.
//
// What DOES move the boundary is a resolved account, not a number:
// TB_WALL_WORKLOAD_USER names an account the wrapper opens and drops to, and
// only when that succeeds does the measured workload genuinely run as someone
// else and this process's uid stop counting as its own. See membershipModelFor,
// which is where the two cases are actually distinguished.
//
// A genuinely scored deployment needs the delegated subtree owned by a
// credential the wrapper does not run as, which in turn needs a privileged
// supervisor to create and admit the nested containments on the wrapper's
// behalf. This wrapper does not have one, so on a same-uid host every row is
// recorded in full and reported ineligible. That refusal is the honest state,
// and it is what this constant exists to make visible rather than to paper
// over.
const WorkloadUIDEnv = "TB_WALL_WORKLOAD_UID"

// ScriptUserEnv names the account the measured BUCKET SCRIPT runs as.
//
// It is a second account because the script and the test code are different
// parties. The script body is harness-generated: it writes the invocation
// specs and then starts the nested `wall exec` wrappers, which ASK the
// script-level wrapper to create, admit and measure each invocation. The
// script creates no containment itself — that is what keeps its own
// containment owned by the wrapper, and a measured process that owned its
// containment is exactly what the membership rule refuses.
//
// Empty means the script level does not drop, is recorded in full and is
// reported ineligible for want of the boundary — the same refusal an
// undeclared workload account earns.
const ScriptUserEnv = "TB_WALL_SCRIPT_USER"

// WorkloadCredential is the DECLARED workload account, resolved to facts.
//
// UID is the account's own uid (-1 when none was declared or it could not be
// resolved); UIDs is that uid together with any additional ones the caller
// declared; GIDs is its primary group and every supplementary group it belongs
// to. They are resolved once, retained on the containment identity, and the
// membership rule is rerun by the verifier over the retained copy.
type WorkloadCredential struct {
	UID  int
	UIDs []int
	GIDs []int
}

// membershipModelFor decides who may write a containment's `cgroup.procs`,
// from facts a caller has already read off the filesystem.
//
// It lives here, portably and separately from the reading, because it is the
// part that was wrong and the part worth testing on any host: the decision
// used to compare the owner against a caller-supplied string and nothing else,
// so declaring any uid other than the owner's returned supervisor-owned for a
// file the wrapper itself owned.
//
// selfUID counts as the workload's credential UNLESS a workload account was
// actually resolved. A bare declaration only WIDENS the set known to lack the
// boundary and never replaces the wrapper's own uid; a resolved account —
// TB_WALL_WORKLOAD_USER opened and dropped to — does replace it, because in
// that deployment the measured child really does run as someone else. The
// distinction is the whole point: it is what separates a boundary a caller
// asserted from one the host established.
func membershipModelFor(f MembershipFacts) string {
	// World-writable is writable by the workload whoever it is.
	if f.OtherWritable {
		return MembershipWorkloadWritable
	}
	// THE WORKLOAD'S OWN CREDENTIAL, AND WHAT MAY ESTABLISH IT.
	//
	// A RESOLVED ACCOUNT replaces this process's uid: TB_WALL_WORKLOAD_USER is
	// a name looked up in the accounts database, so a uid that comes back from
	// that lookup and differs from this process's is a second credential that
	// demonstrably exists. That is the arrangement a scored row needs — the
	// wrapper owns the containment and the measured work runs as somebody
	// else — and counting the wrapper as the workload would refuse it.
	//
	// A DECLARED NUMBER may only widen. TB_WALL_WORKLOAD_UID is a bare integer
	// list from the caller and nothing verifies it, so treating it as a
	// replacement let a caller mint the boundary by naming any uid at all: a
	// file this very process owns then reported supervisor-owned, which is the
	// one answer that scores. Self stays in the set whenever no account was
	// resolved, so a declaration can add credentials known to lack the
	// boundary and can never remove the one credential we know has it.
	// A RESOLVED uid is a POSITIVE one, which is also the convention the
	// retained containment identity uses. Zero is root, and a measured
	// workload running as root holds every capability this boundary exists to
	// deny, so it can never be the credential a scored row rests on; reading
	// the zero value as a resolution would let a fact nobody established
	// replace the one credential we know about.
	workload := append([]int(nil), f.WorkloadUIDs...)
	if f.WorkloadUID <= 0 {
		workload = append(workload, f.SelfUID)
	}
	if len(workload) == 0 {
		workload = []int{f.SelfUID}
	}
	for _, uid := range workload {
		if f.OwnerUID == uint32(uid) {
			return MembershipWorkloadWritable
		}
	}
	// GROUP-WRITABLE IS FINE ONLY IF THE WORKLOAD IS NOT IN THAT GROUP.
	//
	// A delegated subtree owned by root and writable by the wrapper's group is
	// exactly the boundary a supervised run establishes: the wrapper may
	// create, freeze, admit and destroy, and the workload may not. Refusing
	// every group-writable mode outright would refuse the only arrangement in
	// which a scored row can exist; refusing it when the workload is IN the
	// group is the check that means something.
	if f.GroupWritable {
		if len(f.WorkloadGIDs) == 0 {
			// Nothing was established about the workload's groups, so the
			// group write bit cannot be shown to exclude it.
			return MembershipWorkloadWritable
		}
		for _, gid := range f.WorkloadGIDs {
			if f.OwnerGID == uint32(gid) {
				return MembershipWorkloadWritable
			}
		}
	}
	return MembershipSupervisorOwned
}

// MembershipFacts is what a caller reads off the filesystem and the process
// credentials, separated from the decision so the rule can be exercised on any
// host.
type MembershipFacts struct {
	OwnerUID, OwnerGID           uint32
	GroupWritable, OtherWritable bool
	// SelfUID is this process's credential.
	SelfUID int
	// WorkloadUID is the uid of an account RESOLVED from the accounts
	// database, or a non-positive value when none was — which is also the
	// safe zero value, so a caller that does not set it gets the conservative
	// answer rather than an unestablished boundary.
	//
	// It is what separates a second credential that demonstrably exists from a
	// caller's bare assertion that one does. Only the first may replace this
	// process's uid in the decision below; a declared number may widen the set
	// and never narrow it.
	WorkloadUID int
	// WorkloadUIDs and WorkloadGIDs are the measured workload's credential and
	// its groups, as the caller declared them.
	WorkloadUIDs []int
	WorkloadGIDs []int
}

// cgroupRootEnv names the delegated cgroup-v2 subtree testbucket may create
// containments under. It is required: guessing a path and writing to it is how
// an action ends up moving processes it does not own.
const cgroupRootEnv = "TB_WALL_CGROUP_ROOT"

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
	// Freeze suspends or resumes every member.
	//
	// It is what makes the admission observation RACE-FREE. Clone-into-cgroup
	// puts the child in the containment at birth, but the child runs from its
	// first instruction — so a membership read taken afterwards races a child
	// that has already forked, and "exactly one member" was an assertion the
	// protocol did not establish. A containment frozen before the spawn holds
	// a child that cannot execute, so the read observes what was admitted
	// rather than what happened to be there.
	Freeze(frozen bool) error
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
// NewContainmentFor creates a containment for a named run.
//
// The privilege boundary is not a service this asks: it is WHICH CREDENTIAL
// this process runs as. The wrapper runs as a credential that may write the
// delegated subtree; the measured workload runs as one that may not, and is
// spawned under it. `membershipControl` reads which is true and the verifier
// refuses to score a containment the workload could have written.
func NewContainmentFor(name string, parent *ContainmentIdentity, run RunIdentity) (Containment, error) {
	return NewContainment(name, parent)
}

// NewContainmentAt creates a containment for one LEVEL, so the membership
// facts retained on it describe the party that level measures.
//
// The level is not decoration. The membership rule asks whether the measured
// party can write this containment's `cgroup.procs`, and there are two
// measured parties: the script account at the script level and the workload
// account at the invocation level. Resolving the invocation account for a
// SCRIPT containment answered the question about a process that was never in
// it — and did so immediately after delegating that containment to the script
// account's group, which is exactly the credential the answer omitted.
func NewContainmentAt(level Level, name string, parent *ContainmentIdentity) (Containment, error) {
	c, err := newContainment(name, parent)
	if err != nil {
		return nil, err
	}
	retainLevelMembershipFacts(c, level)
	return c, nil
}

// measuredAccountFor is the account the party measured at one level runs as.
// The action level has no measured child, so the workload account — the
// untrusted party whose containment this ultimately is — remains the subject.
func measuredAccountFor(level Level) string {
	if level == LevelScript {
		if user := strings.TrimSpace(os.Getenv(ScriptUserEnv)); user != "" {
			return user
		}
	}
	return strings.TrimSpace(os.Getenv(WorkloadUserEnv))
}

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
	// A producer that read bytes it cannot parse retains them and reports no
	// membership: the verifier then refuses the endpoint rather than reading
	// an unparseable file as an empty containment.
	procs, ok := parseCgroupProcs(procsBytes)
	if !ok {
		procs = nil
	}
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
		Procs:       procs,
		ProcsBytes:  procsBytes,
		ProcsDigest: DigestBytes(append([]byte(id+"\x00"), procsBytes...)),
	}
}

// parseCgroupProcs reads a `cgroup.procs` file into the pids it lists.
//
// It returns a NON-NIL slice for a successful read, empty file included. That
// is the whole point: nil means no snapshot was taken, and an empty
// containment must not be indistinguishable from an unread one.
//
// The grammar is EXACT, and ok reports whether the bytes are that grammar.
// This used to tokenise with strings.Fields and silently drop everything
// strconv.Atoi refused, so `not-a-pid\n` parsed to an empty list — and since
// the verifier compared the record's readable list using the same permissive
// parser, bytes no kernel ever wrote passed as proof that the containment was
// empty. cgroup-v2 writes one decimal pid per line and nothing else; anything
// else is not a membership snapshot, and a verifier adjudicating retained
// evidence after the fact must say so rather than guess.
func parseCgroupProcs(b []byte) ([]int, bool) {
	out := []int{}
	text := string(b)
	if text == "" {
		return out, true
	}
	// A non-empty file is newline-terminated, one pid per line.
	if !strings.HasSuffix(text, "\n") {
		return nil, false
	}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		pid, err := strconv.Atoi(line)
		if err != nil || pid <= 0 || line != strconv.Itoa(pid) {
			// Rejecting `007` and ` 7` as well as `not-a-pid`: the kernel
			// writes the canonical decimal, and accepting variants would make
			// the retained bytes something other than what was read.
			return nil, false
		}
		out = append(out, pid)
	}
	return out, true
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

// evidenceDirDelegationFor is the group and mode an evidence directory carries
// once a script account is declared, separated from resolving that account so
// the decision can be exercised on any host.
//
// rwxrws--T: the owner and the delegated group may CREATE; setgid makes new
// files carry the directory's group; the sticky bit means only a file's owner
// may remove or rename it. The wrapper's ledgers stay 0644 and wrapper-owned,
// so the account that may add files beside them can neither modify nor replace
// them — which is the difference between a delegation and a hole.
func evidenceDirDelegationFor(gids []int) (int, os.FileMode) {
	if len(gids) == 0 {
		return -1, 0o755
	}
	return gids[0], os.FileMode(0o770) | os.ModeSetgid | os.ModeSticky
}
