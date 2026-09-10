package walltime

import (
	"bufio"
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
	// LevelSetup is the action-owned per-bucket setup command.
	//
	// It has no envelope of its own -- it is not a measured product outcome --
	// but §3.1's floor is `A >= setup_ns + script_ns`, and a term of that
	// inequality has to come from somewhere. Without a recorded interval
	// `setup_ns` was underivable from the produced bytes, so the floor could
	// only ever be checked against a number supplied beside them.
	LevelSetup Level = "setup"
)

// THE THREE-LEDGER ROLE MODEL IS REMOVED.
//
// Role named which of nine ledgers a record belonged to — AT/VB/V for the
// physical envelope, CPA/CPB/CPV for the containment peer, VTA/VTB/VT for the
// trace collector. The peer and the trace are gone, so there is one ledger and
// a level already names what it measures.

// Producer is which of the three independent producers wrote a record. A peer
// and a trace that share a producer are not independent, and the verifier says
// so.
type Producer string

const (
	// ProducerPhysical is the ONLY producer. The containment peer and the
	// trace collector went with the multi-ledger model: they existed to
	// reconcile the physical envelope against two independent observers, and
	// nothing observes independently any more.
	ProducerPhysical Producer = "physical_wrapper"
)

// Source taxonomy: what kind of event an endpoint was derived from. The
// containment and reporter-annotation classes went with the observers that
// produced them.
const (
	SourceProcessLifecycle = "os_process_lifecycle"
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
	// Stage1, Stage2, ComponentRegistry and VerifierID are gone with the
	// authority model: they bound a record to frozen planning inputs, a
	// single authorised derived plan, an Aeta registry template and a
	// delivery-bound verifier identity, none of which exists. What identifies
	// a record now is the run that produced it.
	// RunnerName, RunnerOS and RunnerArch are gone. They existed to be
	// COMPARED against a fleet's signed statement of what it had booted: an
	// independently observed host identity is what stops one valid statement
	// naming `runner-a` from being replayed across every job of a run. There
	// is no fleet statement and no signature to check it against, and the
	// runner a bucket actually ran on is already a registered §13 diagnostic
	// on the observation — `actual_runner_name` — so repeating it on every
	// record answered nothing the observation does not.
}

// ProcIdentity is the process-tree fact a record carries.
type ProcIdentity struct {
	PID     int    `json:"pid,omitempty"`
	PGID    int    `json:"pgid,omitempty"`
	StartID string `json:"start_id,omitempty"`
	// UID, GID and Groups are gone with the credential boundary they measured.
	// They recorded the credential and group vector the measured process
	// actually ran under, so a verifier could decide whether a containment
	// owned by one credential had excluded a process running as another. There
	// is no second credential: §3's trusted-CI boundary does not ask whether
	// the workload could have rewritten its own containment.
	ParentPID int    `json:"ppid,omitempty"`
	ExitKind  string `json:"exit_kind,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Signal    string `json:"signal,omitempty"`
}

// Record is one append-only JSONL line.
type Record struct {
	Schema   string   `json:"schema"`
	Seq      int      `json:"seq"`
	Kind     string   `json:"kind"`
	Level    Level    `json:"level,omitempty"`
	Boundary string   `json:"boundary,omitempty"`
	Producer Producer `json:"producer"`
	// ProducerID is the execution context of the writer: which process, on
	// which host.
	ProducerID string `json:"producer_id"`
	// Source is the taxonomy class of the underlying event.
	Source string `json:"source"`
	// Seqno is the stable ordinal of an invocation within its bucket script.
	Seqno int `json:"invocation_seq,omitempty"`

	Run  RunIdentity  `json:"run"`
	Proc ProcIdentity `json:"proc,omitzero"`
	// The containment identity is gone. It named a cgroup-v2 subtree; as a
	// value it serialized `{}` on every record once that implementation was
	// removed, and as a pointer it serialized nothing while leaving the
	// proof-era schema constructible. The process group the measured tree ran
	// under is already in Proc.PGID, which is a fact the wrapper reads rather
	// than a container it claims to own.
	Instant Instant `json:"instant"`

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

	// stream is the file this record was read from. It is stamped by the
	// reader and is never serialized: a per-file property is
	// a property of one writer's FILE, and grouping by the identity a file
	// CLAIMS merged two intact chains into one broken one.
	stream string
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

// Writer appends hash-chained records to one producer's JSONL stream.
//
// Every Append fsyncs before returning. That is deliberately the slow choice:
// a record whose purpose is to prove that a child had not started yet is
// worthless if it is still in a page cache when the machine is cancelled.
type Writer struct {
	mu       sync.Mutex
	f        *os.File
	seq      int
	producer Producer
	id       string
}

// NewWriter opens (creating) the append-only stream.
//
// It used to take an ed25519 signing key and hash-chain every record. Both are
// gone with the multi-ledger model: production always passed nil, and inside
// the trusted-CI boundary §3 draws, a chain the writer recomputes and a
// signature made with a key the writer holds establish nothing an editor of
// the file could not reproduce. The producer-binary digest and the signer
// identity went with them.
func NewWriter(path string, p Producer, producerID string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("walltime: records dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("walltime: open records %s: %w", path, err)
	}
	w := &Writer{f: f, producer: p, id: producerID}
	// Resume an existing stream rather than restarting its sequence: the
	// action level writes its start and end from two different processes.
	if seq, err := tailSeq(path); err == nil {
		w.seq = seq
	}
	return w, nil
}

// Append stamps and durably writes one record.
func (w *Writer) Append(r Record) (Record, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	r.Schema = SchemaVersion
	r.Producer = w.producer
	r.ProducerID = w.id
	r.Seq = w.seq
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
	return r, nil
}

// Close flushes and closes the stream.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}

// tailSeq reads an existing stream to recover its next sequence number.
func tailSeq(path string) (int, error) {
	recs, err := ReadRecords(path)
	if err != nil || len(recs) == 0 {
		return 0, err
	}
	return recs[len(recs)-1].Seq + 1, nil
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
	recs, err := decodeRecords(f)
	if err != nil {
		return nil, err
	}
	// WHICH FILE EACH RECORD CAME FROM.
	//
	// A hash chain is a property of ONE WRITER'S FILE, and the reader grouped
	// records by the producer/level/sequence identity they claim instead. Two
	// files claiming one identity — which is what a side stream defaulting to
	// the main stream's sequence number produced — were merged into a single
	// group whose second half chained to nothing, and the reader reported a
	// broken chain when what it had was two intact chains. The stream a record
	// was read from is not part of the record it signs, so it is stamped here
	// and never serialized.
	name := filepath.Base(path)
	for i := range recs {
		recs[i].stream = name
	}
	return recs, nil
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
// streamFile is the ledger a record was read from. It is empty for a record a
// caller built in memory, which groups such records together exactly as before.
func (r Record) streamFile() string { return r.stream }

func ReadDir(dir string) ([]Record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		// The key-log exclusion that stood here is gone with the key log
		// itself: there is no longer a .jsonl file in this directory that is
		// not a record stream.
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
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
