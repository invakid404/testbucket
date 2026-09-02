package walltime

import (
	"bufio"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Level is a nesting level of the measurement. The three levels are measured
// independently and their gates are evaluated separately: an aggregate that
// mixes levels does not qualify.
type Level string

const (
	// LevelAction is the complete testbucket-controlled Run-bucket action, the
	// product outcome A.
	LevelAction Level = "action"
	// LevelScript is the complete generated bucket script, VB.
	LevelScript Level = "script"
	// LevelInvocation is one exact rendered invocation and its waited process
	// tree, V.
	LevelInvocation Level = "invocation"
)

// Role is the ledger a record belongs to. The three ledgers at each level are
// deliberately distinct types rather than one interval with a flag: the
// physical envelope is the product, the peer is its reconciliation partner,
// and the trace is an independent reconstruction. Mixing them is the failure
// mode the whole schema exists to prevent.
type Role string

const (
	RolePhysicalAction     Role = "AT"
	RolePhysicalScript     Role = "VB"
	RolePhysicalInvocation Role = "V"
	RolePeerAction         Role = "CPA"
	RolePeerScript         Role = "CPB"
	RolePeerInvocation     Role = "CPV"
	RoleTraceAction        Role = "VTA"
	RoleTraceScript        Role = "VTB"
	RoleTraceInvocation    Role = "VT"
)

// Producer is which of the three independent producers wrote a record. A peer
// and a trace that share a producer are not independent, and the verifier says
// so.
type Producer string

const (
	ProducerPhysical Producer = "physical_wrapper"
	ProducerPeer     Producer = "containment_peer"
	ProducerTrace    Producer = "trace_collector"
)

// Source taxonomy. Only an independently observed os_containment or
// os_process_lifecycle event may DELIMIT a peer or trace lifecycle; a wrapper
// or reporter event may annotate one but can never supply an endpoint.
const (
	SourceContainment      = "os_containment"
	SourceProcessLifecycle = "os_process_lifecycle"
	SourceReporter         = "reporter_annotation"
	SourceWrapper          = "wrapper_annotation"
)

// Terminal states. Every one of them is retained: an incomplete row stays in
// the ledger with its reason and never becomes a duration.
const (
	TerminalPassed        = "passed"
	TerminalFailed        = "failed"
	TerminalSignalled     = "signalled"
	TerminalCancelled     = "cancelled"
	TerminalSpawnError    = "spawn_error"
	TerminalWrapperError  = "wrapper_error"
	TerminalCrashUnclosed = "crash_unclosed"
)

// RoleFor maps a producer and level to the ledger role, so the three producers
// cannot disagree about what they are writing.
func RoleFor(p Producer, l Level) (Role, error) {
	switch p {
	case ProducerPhysical:
		switch l {
		case LevelAction:
			return RolePhysicalAction, nil
		case LevelScript:
			return RolePhysicalScript, nil
		case LevelInvocation:
			return RolePhysicalInvocation, nil
		}
	case ProducerPeer:
		switch l {
		case LevelAction:
			return RolePeerAction, nil
		case LevelScript:
			return RolePeerScript, nil
		case LevelInvocation:
			return RolePeerInvocation, nil
		}
	case ProducerTrace:
		switch l {
		case LevelAction:
			return RoleTraceAction, nil
		case LevelScript:
			return RoleTraceScript, nil
		case LevelInvocation:
			return RoleTraceInvocation, nil
		}
	}
	return "", fmt.Errorf("walltime: no role for producer %q at level %q", p, l)
}

// RunIdentity is the campaign/delivery keying every record carries. It is
// repeated on every line on purpose: a record must be independently
// attributable without trusting a file name or a directory layout.
type RunIdentity struct {
	CampaignID  string `json:"campaign_id,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	AttemptID   string `json:"attempt_id,omitempty"`
	BucketID    string `json:"bucket_id,omitempty"`
	Repository  string `json:"repository,omitempty"`
	WorkflowRun string `json:"workflow_run,omitempty"`
	Job         string `json:"job,omitempty"`
	Step        string `json:"step,omitempty"`
	StepAttempt string `json:"step_attempt,omitempty"`
	// Stage1 and Stage2 bind the record to the frozen planning inputs and the
	// single derived plan. A record that names no Stage-2 receipt cannot be
	// scored: it might have measured a plan nobody authorised.
	Stage1 Digest `json:"stage1_digest,omitempty"`
	Stage2 Digest `json:"stage2_digest,omitempty"`
	// ComponentRegistry is the Aeta registry template digest in force.
	ComponentRegistry Digest `json:"component_registry_digest,omitempty"`
	// VerifierID is the delivery-bound verifier identity the run expects.
	VerifierID string `json:"verifier_id,omitempty"`
}

// ContainmentIdentity is the stable containment the physical wrapper, its peer
// and the trace must all name. Inode and PID-start are strings: they are
// identities, not quantities, and an inode can exceed the exact float64 range
// that the canonical digest allows.
type ContainmentIdentity struct {
	// Primitive is the containment mechanism, e.g. "cgroup2". Anything other
	// than a real containment primitive is unscorable by construction.
	Primitive string `json:"primitive"`
	// ID is the containment path/name.
	ID string `json:"id"`
	// Inode is the containment inode, which distinguishes a re-created
	// containment that reused a path.
	Inode string `json:"inode,omitempty"`
	// BootID ties the identity to one boot.
	BootID string `json:"boot_id,omitempty"`
	// RootPID and RootStart are the process-start identity of the root the
	// containment was created for: a PID alone is reusable, a PID plus its
	// start time is not.
	RootPID   int    `json:"root_pid,omitempty"`
	RootStart string `json:"root_start,omitempty"`
	// OwnerUID is the credential that owns this containment's `cgroup.procs`.
	// The verifier compares it against the measured process's own uid: they
	// must differ, or the thing being measured could have rewritten its own
	// membership.
	OwnerUID int `json:"owner_uid,omitempty"`
	// OwnerGID and Mode are the rest of the facts the membership decision was
	// made from, retained so a VERIFIER CAN REDERIVE IT.
	//
	// The producer's MembershipControl string is a summary, and the cgroup is
	// gone by the time anyone reads the records — so a verifier that trusted
	// the string was trusting a non-reproducible producer conclusion about the
	// one property eligibility turns on. These are the inputs; the rule is
	// membershipModelFor, and it can be run again.
	OwnerGID int    `json:"owner_gid,omitempty"`
	Mode     uint32 `json:"mode,omitempty"`
	// WorkloadUID and WorkloadGIDs are the credential the MEASURED WORKLOAD
	// runs as, resolved once when the containment was created and retained
	// here.
	//
	// The membership decision asks one question — can the workload rewrite
	// this containment's `cgroup.procs`? — and the producer used to answer it
	// by reading `/etc/passwd` and `/etc/group` at decision time and writing
	// down the conclusion. Nobody could rerun that: the accounts file is not
	// part of the evidence, the cgroup is gone, and at the action level there
	// is no measured child whose own uid could stand in for the workload's.
	// These are the inputs, retained beside the owner and the mode, so the
	// rule runs again over the same facts the producer used.
	//
	// WorkloadUID is -1 when no workload account was declared, which is the
	// single-credential host: the wrapper and the measured work share a
	// credential, and no boundary exists to rederive.
	WorkloadUID  int   `json:"workload_uid,omitempty"`
	WorkloadGIDs []int `json:"workload_gids,omitempty"`
	// MembershipControl is WHO may write this containment's `cgroup.procs` —
	// the process-migration control on cgroup-v2 — established by reading the
	// filesystem rather than asserted by Stage 1. See the Membership*
	// constants. A containment the measured workload can migrate itself out
	// of cannot prove the membership history the envelope is built on.
	MembershipControl string `json:"membership_control,omitempty"`
}

// Scorable reports whether this containment can delimit a scored lifecycle.
//
// The ROOT PROCESS IDENTITY is required, not merely documented. RootPID plus
// RootStart is what the schema says closes PID reuse, and a containment
// identity that omits it cannot say which process the containment was made
// for: the path and inode survive a reboot's worth of pid recycling, and
// "some process with this number" is not an identity. It used to be checked
// for none of that, so a run whose every record omitted the start identity
// scored exactly as well as one that carried it.
func (c ContainmentIdentity) Scorable() bool {
	return c.Primitive == PrimitiveCgroup2 && c.ID != "" && c.Inode != "" && c.BootID != "" &&
		c.RootPID > 0 && strings.TrimSpace(c.RootStart) != "" &&
		c.MembershipControl == MembershipSupervisorOwned &&
		// AND THE FACTS THE MEMBERSHIP DECISION WAS MADE FROM.
		//
		// MembershipControl is a producer's summary of the one property
		// eligibility turns on. The verifier reruns the rule — but only over
		// records that retained its inputs, and the mode was optional: a
		// signed identity could omit it, assert supervisor ownership, skip
		// the rederivation it gates and stay scorable. A conclusion whose
		// inputs were not retained is not rederivable, and a property nobody
		// can recheck is a claim.
		c.OwnerUID >= 0 && c.OwnerGID >= 0 && c.Mode != 0
}

// SameRoot reports whether two identities name the same root process.
//
// It is separate from Same because the two answer different questions and
// carry different consequences: Same asks whether this is the same stable
// containment, while this asks whether the producers agree about the process
// it was created for. Disagreement there means the observers watched
// containments made for different processes, which is unscorable rather than
// malformed.
func (c ContainmentIdentity) SameRoot(o ContainmentIdentity) bool {
	return c.RootPID == o.RootPID && c.RootStart == o.RootStart
}

// Same reports whether two records name the same stable containment. Identity
// is the pair (id, inode) plus boot: a path that was destroyed and re-created
// is a DIFFERENT containment even though its name is unchanged.
func (c ContainmentIdentity) Same(o ContainmentIdentity) bool {
	return c.Primitive == o.Primitive && c.ID == o.ID && c.Inode == o.Inode && c.BootID == o.BootID &&
		// AND THE SAME MEMBERSHIP FACTS.
		//
		// Two records naming one path, inode and boot were treated as the same
		// containment however they described its ownership — so a record could
		// keep the identity and restate the owner, the mode or the conclusion,
		// and the disagreement was invisible. These fields are what the
		// membership rule is rerun over; producers that disagree about them
		// did not observe the same containment.
		c.OwnerUID == o.OwnerUID && c.OwnerGID == o.OwnerGID && c.Mode == o.Mode &&
		c.MembershipControl == o.MembershipControl &&
		c.WorkloadUID == o.WorkloadUID && sameInts(c.WorkloadGIDs, o.WorkloadGIDs)
}

// Differs names the first field on which two identities disagree, in words a
// reader can act on.
//
// The mismatch message used to print the path and the inode, which are exactly
// the fields that are equal when the disagreement is about the owner, the mode
// or the membership conclusion — so a record that restated the ownership of a
// containment produced a finding saying it named "X, not this envelope's X".
func (c ContainmentIdentity) Differs(o ContainmentIdentity) string {
	for _, f := range []struct {
		what    string
		a, b    any
		differs bool
	}{
		{"primitive", c.Primitive, o.Primitive, c.Primitive != o.Primitive},
		{"path", c.ID, o.ID, c.ID != o.ID},
		{"inode", c.Inode, o.Inode, c.Inode != o.Inode},
		{"boot", c.BootID, o.BootID, c.BootID != o.BootID},
		{"cgroup.procs owner uid", c.OwnerUID, o.OwnerUID, c.OwnerUID != o.OwnerUID},
		{"cgroup.procs owner gid", c.OwnerGID, o.OwnerGID, c.OwnerGID != o.OwnerGID},
		{"cgroup.procs mode", c.Mode, o.Mode, c.Mode != o.Mode},
		{"membership control", c.MembershipControl, o.MembershipControl, c.MembershipControl != o.MembershipControl},
		{"retained workload uid", c.WorkloadUID, o.WorkloadUID, c.WorkloadUID != o.WorkloadUID},
		{"retained workload groups", c.WorkloadGIDs, o.WorkloadGIDs, !sameInts(c.WorkloadGIDs, o.WorkloadGIDs)},
	} {
		if f.differs {
			return fmt.Sprintf("%s %v, not this envelope's %v", f.what, f.a, f.b)
		}
	}
	return ""
}

// ProcIdentity is the process-tree fact a record carries.
type ProcIdentity struct {
	PID     int    `json:"pid,omitempty"`
	PGID    int    `json:"pgid,omitempty"`
	StartID string `json:"start_id,omitempty"`
	// SessionID is the child's session, read while it is alive. The contract
	// makes a session or PGID change terminal, and neither is decidable from a
	// record that never carried the session.
	SessionID int `json:"sid,omitempty"`
	// UID is the credential the measured process actually ran under, read from
	// the kernel rather than declared. It is what turns the workload account
	// from a caller's assertion into a fact: a containment owned by one
	// credential and a measured process running under another is the boundary
	// itself, observed.
	UID int `json:"uid,omitempty"`
	// GID and Groups are the process's ACTUAL group vector, read from the
	// launched process rather than resolved from /etc files. Account
	// resolution may go through NSS, LDAP or SSSD, so parsing /etc/group
	// establishes what those files say and not what the process received. The
	// kernel's own answer is what decides whether a group-writable containment
	// excluded this process.
	GID       int    `json:"gid,omitempty"`
	Groups    []int  `json:"groups,omitempty"`
	ParentPID int    `json:"ppid,omitempty"`
	ExitKind  string `json:"exit_kind,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Signal    string `json:"signal,omitempty"`
}

// Record is one append-only JSONL line. Hash and Signature are excluded from
// the hashed payload; everything else is in it, so a rewritten field breaks
// the chain.
type Record struct {
	Schema   string   `json:"schema"`
	Seq      int      `json:"seq"`
	Kind     string   `json:"kind"`
	Role     Role     `json:"role,omitempty"`
	Level    Level    `json:"level,omitempty"`
	Boundary string   `json:"boundary,omitempty"`
	Producer Producer `json:"producer"`
	// ProducerID is the execution context of the writer: which process, on
	// which host. Peer and trace records that share it are not independent.
	ProducerID string `json:"producer_id"`
	// ProducerBinary is the FULL SHA-256 of the executable that wrote this
	// record. It is its own field rather than a fragment inside ProducerID
	// because the verifier must compare it for exact equality against the
	// binary Stage 1 approved: a substring match over a truncated digest is
	// satisfiable by a prefix collision, and an identity that a collision can
	// satisfy is not an identity.
	ProducerBinary Digest `json:"producer_binary,omitempty"`
	// Source is the taxonomy class of the underlying event.
	Source string `json:"source"`
	// RawEventID and RawEventDigest identify the raw event this endpoint was
	// derived from. A peer and its trace observe the SAME lifecycle through
	// DIFFERENT raw reads, so these must differ even though the containment
	// identity matches.
	RawEventID     string `json:"raw_event_id,omitempty"`
	RawEventDigest Digest `json:"raw_event_digest,omitempty"`
	// RawEventBytes is the exact kernel output the endpoint was derived from,
	// retained so a later reader can re-derive the conclusion rather than
	// trust it. RawProcs is the containment membership snapshot taken with the
	// same read.
	RawEventBytes []byte `json:"raw_event_bytes,omitempty"`
	// RawProcs is the containment membership snapshot taken WITH that read.
	//
	// It deliberately carries no `omitempty`. A successful read of an empty
	// containment and a read that never happened used to serialise
	// identically — both absent — so a producer whose `cgroup.procs` read
	// failed emitted evidence indistinguishable from one that proved the
	// containment empty. Without omitempty a taken snapshot is `[]` and an
	// absent one is `null`, and the verifier can tell them apart.
	//
	// RawProcsBytes and RawProcsDigest are that snapshot's own retained
	// evidence, derived exactly as the event's are: the digest binds this
	// observer's event id to the exact `cgroup.procs` bytes, so an empty read
	// still has a digest nobody can produce without having taken it.
	RawProcs       []int  `json:"raw_procs"`
	RawProcsBytes  []byte `json:"raw_procs_bytes,omitempty"`
	RawProcsDigest Digest `json:"raw_procs_digest,omitempty"`
	// Phase names a trace phase (invocation lifecycle, inter-invocation gap,
	// script epilogue).
	Phase string `json:"phase,omitempty"`
	// Seqno is the stable ordinal of an invocation within its bucket script.
	Seqno int `json:"invocation_seq,omitempty"`

	Run         RunIdentity         `json:"run"`
	Containment ContainmentIdentity `json:"containment"`
	Proc        ProcIdentity        `json:"proc,omitzero"`
	Instant     Instant             `json:"instant"`

	// Spec is the invocation identity: serialised argv, cwd and selector
	// digests. It is what makes "this V measured that invocation" checkable
	// rather than assumed.
	Spec *SpecIdentity `json:"spec,omitzero"`

	// Terminal is set on a terminal record; Reason explains a missing or
	// abnormal closure and is retained forever.
	Terminal string `json:"terminal,omitempty"`
	Reason   string `json:"reason,omitempty"`

	// Note carries wrapper/reporter annotations. An annotation may never
	// delimit a scored interval; it exists so that discarded context is still
	// recorded rather than lost.
	Note string `json:"note,omitempty"`

	PrevHash  Digest `json:"prev_hash"`
	Hash      Digest `json:"hash"`
	SignerID  string `json:"signer_id,omitempty"`
	Signature string `json:"signature,omitempty"`
}

// SpecIdentity is the digest-bound identity of an invocation: what was run,
// from where, selecting what.
type SpecIdentity struct {
	ArgvDigest     Digest `json:"argv_digest"`
	Cwd            string `json:"cwd"`
	SelectorDigest Digest `json:"selector_digest,omitempty"`
	UnitDigest     Digest `json:"unit_digest,omitempty"`
	AtomDigest     Digest `json:"atom_digest,omitempty"`
	Desc           string `json:"desc,omitempty"`
}

// payload is the record minus the fields that authenticate it. Hashing this
// (rather than the marshalled line) is what makes the chain independent of
// field order and of encoding/json's future output choices.
func (r Record) payload() Record {
	c := r
	c.Hash, c.Signature = "", ""
	return c
}

// computeHash chains this record to its predecessor.
func (r Record) computeHash() (Digest, error) { return DigestJSON(r.payload()) }

// Writer appends hash-chained records to one producer's JSONL stream.
//
// Every Append fsyncs before returning. That is deliberately the slow choice:
// a record whose purpose is to prove that a child had not started yet is
// worthless if it is still in a page cache when the machine is cancelled.
type Writer struct {
	mu       sync.Mutex
	f        *os.File
	seq      int
	prev     Digest
	producer Producer
	id       string
	// binary is the full digest of the executable writing this stream. Every
	// record carries it so the verifier can tie the stream to the build Stage 1
	// approved without parsing it out of a display string.
	binary Digest
	key    ed25519.PrivateKey
	signer string
}

// NewWriter opens (creating) the append-only stream for one producer.
// producerID is the writer's execution-context identity; signing key may be
// nil, in which case records are hash-chained but unsigned and the verifier
// will refuse to SCORE them (it still verifies their structure).
func NewWriter(path string, p Producer, producerID string, key ed25519.PrivateKey) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("walltime: records dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("walltime: open records %s: %w", path, err)
	}
	w := &Writer{f: f, producer: p, id: producerID, binary: SelfDigest(), key: key}
	if key != nil {
		w.signer = hex.EncodeToString(key.Public().(ed25519.PublicKey))
	}
	// Resume an existing stream rather than restarting its sequence: the
	// action level writes its start and end from two different processes.
	if seq, prev, err := tailChain(path); err == nil {
		w.seq, w.prev = seq, prev
	}
	return w, nil
}

// Append stamps, chains, signs and durably writes one record.
func (w *Writer) Append(r Record) (Record, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	r.Schema = SchemaVersion
	r.Producer = w.producer
	r.ProducerID = w.id
	if r.ProducerBinary == "" {
		r.ProducerBinary = w.binary
	}
	r.Seq = w.seq
	r.PrevHash = w.prev
	r.SignerID = w.signer
	h, err := r.computeHash()
	if err != nil {
		return Record{}, err
	}
	r.Hash = h
	if w.key != nil {
		r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(w.key, []byte(h)))
	}
	line, err := json.Marshal(r)
	if err != nil {
		return Record{}, fmt.Errorf("walltime: marshal record: %w", err)
	}
	if _, err := w.f.Write(append(line, '\n')); err != nil {
		return Record{}, fmt.Errorf("walltime: write record: %w", err)
	}
	if err := w.f.Sync(); err != nil {
		return Record{}, fmt.Errorf("walltime: sync record: %w", err)
	}
	w.seq++
	w.prev = h
	return r, nil
}

// Close flushes and closes the stream.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}

// tailChain reads an existing stream to recover its sequence and last hash.
func tailChain(path string) (int, Digest, error) {
	recs, err := ReadRecords(path)
	if err != nil || len(recs) == 0 {
		return 0, "", err
	}
	last := recs[len(recs)-1]
	return last.Seq + 1, last.Hash, nil
}

// ReadRecords parses one stream. It does NOT verify the chain — VerifyChain
// does, and keeping them apart lets the verifier report a broken chain as a
// finding rather than as a read error.
func ReadRecords(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeRecords(f)
}

func decodeRecords(r io.Reader) ([]Record, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var out []Record
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("walltime: malformed record: %w", err)
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// ReadDir loads every record stream in a directory, sorted by file name so the
// result is deterministic.
func ReadDir(dir string) ([]Record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		// The key log shares the .jsonl suffix but is not a record stream:
		// reading it as one would report every line as a malformed record.
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") || e.Name() == keyLogFile {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var out []Record
	for _, n := range names {
		recs, err := ReadRecords(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
	}
	return out, nil
}
