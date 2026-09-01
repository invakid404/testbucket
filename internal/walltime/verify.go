package walltime

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// Finding severities. The distinction that matters is between a run that is
// STRUCTURALLY broken and one that is merely not SCORABLE: both are failures
// of eligibility, but only the first means the records themselves are wrong.
const (
	// SeverityTerminal means the records do not describe a complete
	// measurement: a broken chain, a missing endpoint, a copied reading.
	SeverityTerminal = "terminal"
	// SeverityIneligible means the measurement is complete but cannot be
	// scored: an unscorable clock, an unproven containment, an absent
	// prerequisite.
	SeverityIneligible = "ineligible"
	// SeverityNote records something a reader should know that does not
	// change the verdict.
	SeverityNote = "note"
)

// Finding is one defect, with the stable code the report and any downstream
// tooling key on.
type Finding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

// Phase is one span of a physical partition. Phases are DERIVED by the
// verifier from physical endpoints, never written by a wrapper: a producer
// that could name its own phases could also name away a gap.
type Phase struct {
	// ComponentID is the registry key, without any index: the numbering lives
	// in Index so a fixed template can cover a variable invocation count.
	ComponentID string `json:"component_id"`
	Index       int    `json:"index,omitempty"`
	Parent      string `json:"parent"`
	StartNs     int64  `json:"start_ns"`
	EndNs       int64  `json:"end_ns"`
}

// Duration is the phase's exclusive half-open length.
func (p Phase) Duration() int64 { return p.EndNs - p.StartNs }

// Name renders the phase for a report.
func (p Phase) Name() string {
	if p.Index > 0 || p.ComponentID == "invocation" || p.ComponentID == "between_invocation_gap" {
		return fmt.Sprintf("%s[%d]", p.ComponentID, p.Index)
	}
	return p.ComponentID
}

// Interval is one producer's bracketed span at one level.
type Interval struct {
	StartNs int64 `json:"start_ns"`
	EndNs   int64 `json:"end_ns"`
	OK      bool  `json:"complete"`
	// Start and End keep the records themselves so the verifier can compare
	// raw event ids, signers and sources without a second lookup.
	start, end Record
}

// Duration is the interval's length, or zero when it has no two endpoints. An
// incomplete interval is never given an inferred duration.
func (i Interval) Duration() int64 {
	if !i.OK {
		return 0
	}
	return i.EndNs - i.StartNs
}

// Envelope is one measured thing: the physical ledger plus its independent
// peer and trace.
type Envelope struct {
	Level       Level               `json:"level"`
	Seq         int                 `json:"seq"`
	Physical    Interval            `json:"physical"`
	Peer        Interval            `json:"peer"`
	Trace       Interval            `json:"trace"`
	Containment ContainmentIdentity `json:"containment"`
	Terminal    string              `json:"terminal"`
	Reason      string              `json:"reason,omitempty"`
	Spec        *SpecIdentity       `json:"spec,omitempty"`
	Desc        string              `json:"desc,omitempty"`
}

// Verdict is the whole verification result. It is JSON-serialisable because it
// is EVIDENCE: a campaign row is this document, not a log line someone read.
type Verdict struct {
	Schema string `json:"schema"`
	Dir    string `json:"dir"`
	// Complete means the records describe a well-formed measurement.
	Complete bool `json:"complete"`
	// Eligible means the measurement may be SCORED: complete, plus a scorable
	// clock and containment, signed records, Stage-1/Stage-2 binding, and
	// every applicable gate passing.
	Eligible     bool             `json:"eligible"`
	Envelopes    []Envelope       `json:"envelopes"`
	Phases       []Phase          `json:"phases"`
	Recon        []Reconciliation `json:"reconciliation"`
	Gates        []GateResult     `json:"gates"`
	Findings     []Finding        `json:"findings"`
	ActionNs     int64            `json:"action_ns"`
	ScriptNs     int64            `json:"script_ns"`
	InvocationNs []int64          `json:"invocation_ns,omitempty"`
}

// VerifyOptions selects what to verify and against which frozen documents.
type VerifyOptions struct {
	Dir string
	// Records lets a caller supply an already-loaded stream (a test, or a
	// verifier reading from an archive rather than a directory).
	Records    []Record
	Stage1Path string
	Stage2Path string
	AetaPath   string
	PcheckPath string
	// RegistryPath is the frozen Aeta component registry. Without it the
	// ETA-completeness gate cannot pass — which is the correct answer, not a
	// reason to skip the gate.
	RegistryPath string
	// AuthorityKeys are the PREDECLARED public keys of the protected campaign
	// environment. An empty set is not "accept any key": it means no authority
	// was declared, and the run is ineligible.
	AuthorityKeys []string
	// Authority, when set, is the protected environment name the manifest must
	// name.
	Authority string
}

// VerifyDir loads a records directory and verifies it.
func VerifyDir(opt VerifyOptions) (*Verdict, error) {
	recs := opt.Records
	if recs == nil {
		var err error
		recs, err = ReadDir(opt.Dir)
		if err != nil {
			return nil, fmt.Errorf("walltime: read records: %w", err)
		}
	}
	v := &Verdict{Schema: SchemaVersion, Dir: opt.Dir}
	if len(recs) == 0 {
		v.add("WT-004", SeverityTerminal, "no records: an absent measurement is not a zero-length one")
		return v, nil
	}

	streams := groupStreams(recs)
	verifyChains(v, streams)
	envelopes := buildEnvelopes(v, streams)
	v.Envelopes = envelopes
	verifyEndpoints(v, envelopes)
	verifyIndependence(v, envelopes)
	v.Phases = derivePartition(v, envelopes)
	v.Recon = reconcile(envelopes)
	summarise(v, envelopes)

	bound := verifyStageBinding(v, opt, recs)
	registry := loadRegistry(v, opt)
	if registry != nil && bound.stage1 != "" && bound.registry != "" {
		if d, err := registry.DigestOf(); err == nil && d != bound.registry {
			v.add("WT-017", SeverityIneligible,
				fmt.Sprintf("Stage 1 binds component registry %s but the supplied registry digests to %s", bound.registry, d))
		}
	}
	aeta := loadAeta(v, opt, registry, bound.stage2)
	if registry != nil {
		v.Findings = append(v.Findings, registry.CheckCompleteness(v.Phases, aeta)...)
	}
	evaluateGates(v, opt, aeta, bound)

	v.Complete = !v.has(SeverityTerminal)
	// Eligibility is a ROW question: are these records a scorable observation?
	// The campaign-scope gates in the table are decided by `wall campaign`
	// over the full frozen population, and a single run must never be able to
	// claim one of them.
	v.Eligible = v.Complete && !v.has(SeverityIneligible) && allGatesPass(RowScope(v.Gates))
	return v, nil
}

// add records a finding, collapsing an exact repeat. The same defect reached
// through several endpoints is one defect, and a report that says it six times
// buries the other five findings under it.
func (v *Verdict) add(code, severity, detail string) {
	for _, f := range v.Findings {
		if f.Code == code && f.Detail == detail {
			return
		}
	}
	v.Findings = append(v.Findings, Finding{Code: code, Severity: severity, Detail: detail})
}

func (v *Verdict) has(severity string) bool {
	for _, f := range v.Findings {
		if f.Severity == severity {
			return true
		}
	}
	return false
}

func allGatesPass(gates []GateResult) bool {
	if len(gates) == 0 {
		return false
	}
	for _, g := range gates {
		if !g.Pass {
			return false
		}
	}
	return true
}

// streamKey identifies one producer's stream at one level. Ordinal is the
// INVOCATION ordinal (Record.Seqno), never the record sequence: one stream
// holds many records, and grouping by the record sequence would shatter every
// stream into single-record fragments that can never close an interval.
type streamKey struct {
	Producer Producer
	Level    Level
	Ordinal  int
}

func groupStreams(recs []Record) map[streamKey][]Record {
	out := map[streamKey][]Record{}
	for _, r := range recs {
		k := streamKey{r.Producer, r.Level, r.Seqno}
		out[k] = append(out[k], r)
	}
	for k := range out {
		s := out[k]
		sort.SliceStable(s, func(i, j int) bool { return s[i].Seq < s[j].Seq })
	}
	return out
}

// verifyChains re-derives every record hash and follows the prev-hash links. A
// record that was rewritten after the fact cannot survive this, which is the
// point of writing the chain at all.
func verifyChains(v *Verdict, streams map[streamKey][]Record) {
	for _, key := range sortedKeys(streams) {
		recs := streams[key]
		var prev Digest
		for i, r := range recs {
			if r.Schema != SchemaVersion {
				v.add("WT-001", SeverityTerminal,
					fmt.Sprintf("%s/%s record %d has schema %q, want %q; a schema change is a new epoch, not a migration",
						key.Producer, key.Level, r.Seq, r.Schema, SchemaVersion))
			}
			want, err := r.computeHash()
			if err != nil {
				v.add("WT-002", SeverityTerminal, fmt.Sprintf("%s/%s record %d cannot be hashed: %v", key.Producer, key.Level, r.Seq, err))
				continue
			}
			if want != r.Hash {
				v.add("WT-002", SeverityTerminal,
					fmt.Sprintf("%s/%s record %d has been rewritten: it hashes to %s but claims %s", key.Producer, key.Level, r.Seq, want, r.Hash))
			}
			if r.PrevHash != prev {
				v.add("WT-002", SeverityTerminal,
					fmt.Sprintf("%s/%s record %d does not chain to its predecessor", key.Producer, key.Level, r.Seq))
			}
			if r.Seq != i {
				v.add("WT-002", SeverityTerminal,
					fmt.Sprintf("%s/%s record at position %d claims sequence %d", key.Producer, key.Level, i, r.Seq))
			}
			if err := VerifySignature(r); err != nil {
				v.add("WT-003", SeverityIneligible,
					fmt.Sprintf("%s/%s record %d: %v", key.Producer, key.Level, r.Seq, err))
			}
			prev = r.Hash
		}
	}
}

func sortedKeys(streams map[streamKey][]Record) []streamKey {
	keys := make([]streamKey, 0, len(streams))
	for k := range streams {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.Level != b.Level {
			return a.Level < b.Level
		}
		if a.Ordinal != b.Ordinal {
			return a.Ordinal < b.Ordinal
		}
		return a.Producer < b.Producer
	})
	return keys
}

// buildEnvelopes pairs the three producers at each level and sequence.
func buildEnvelopes(v *Verdict, streams map[streamKey][]Record) []Envelope {
	type envKey struct {
		Level Level
		Seq   int
	}
	byEnv := map[envKey]*Envelope{}
	for _, key := range sortedKeys(streams) {
		ek := envKey{key.Level, key.Ordinal}
		e, ok := byEnv[ek]
		if !ok {
			e = &Envelope{Level: key.Level, Seq: key.Ordinal}
			byEnv[ek] = e
		}
		iv := boundaryInterval(streams[key])
		switch key.Producer {
		case ProducerPhysical:
			e.Physical = iv
			for _, r := range streams[key] {
				if r.Containment.ID != "" {
					e.Containment = r.Containment
				}
				if r.Spec != nil {
					e.Spec = r.Spec
					e.Desc = r.Spec.Desc
				}
				if r.Terminal != "" {
					e.Terminal, e.Reason = r.Terminal, r.Reason
				}
			}
		case ProducerPeer:
			e.Peer = iv
		case ProducerTrace:
			e.Trace = iv
		}
	}
	out := make([]Envelope, 0, len(byEnv))
	for _, e := range byEnv {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return levelRank(out[i].Level) < levelRank(out[j].Level)
		}
		return out[i].Seq < out[j].Seq
	})
	for _, e := range out {
		for name, iv := range map[string]Interval{"physical": e.Physical, "peer": e.Peer, "trace": e.Trace} {
			if !iv.OK {
				v.add("WT-004", SeverityTerminal,
					fmt.Sprintf("%s[%d]: the %s ledger has no closed start/end pair; an interval with one endpoint is missing, not shorter", e.Level, e.Seq, name))
			}
		}
		if e.Terminal != "" && e.Terminal != TerminalPassed {
			v.add("WT-014", SeverityIneligible,
				fmt.Sprintf("%s[%d] terminated %s: %s (retained, never scored)", e.Level, e.Seq, e.Terminal, e.Reason))
		}
	}
	return out
}

func levelRank(l Level) int {
	switch l {
	case LevelAction:
		return 0
	case LevelScript:
		return 1
	default:
		return 2
	}
}

// boundaryInterval extracts the closed start/end pair of one stream.
func boundaryInterval(recs []Record) Interval {
	var iv Interval
	var haveStart, haveEnd bool
	for _, r := range recs {
		if r.Kind != "boundary" {
			continue
		}
		switch r.Boundary {
		case "start":
			if !haveStart {
				iv.StartNs, iv.start, haveStart = int64(r.Instant.Mono), r, true
			}
		case "end":
			iv.EndNs, iv.end, haveEnd = int64(r.Instant.Mono), r, true
		}
	}
	iv.OK = haveStart && haveEnd
	return iv
}

// verifyEndpoints enforces the contract's ordering chain, the clock and
// containment scorability, and the source taxonomy.
func verifyEndpoints(v *Verdict, envs []Envelope) {
	boot := ""
	for _, e := range envs {
		if !e.Physical.OK || !e.Peer.OK || !e.Trace.OK {
			continue
		}
		label := fmt.Sprintf("%s[%d]", e.Level, e.Seq)
		if !comparableEndpoints(e) {
			// Ordering cannot be CHECKED across readings that do not share an
			// epoch. Reporting a violation from incomparable numbers would be
			// a guess; the honest finding is that the clock cannot support the
			// check at all.
			v.add("WT-010", SeverityIneligible,
				fmt.Sprintf("%s: the three producers' readings do not share a clock epoch, so endpoint containment cannot be verified", label))
			continue
		}
		order := []struct {
			name string
			ns   int64
		}{
			{"physical start", e.Physical.StartNs},
			{"peer start", e.Peer.StartNs},
			{"trace start", e.Trace.StartNs},
			{"trace end", e.Trace.EndNs},
			{"peer end", e.Peer.EndNs},
			{"physical end", e.Physical.EndNs},
		}
		for i := 1; i < len(order); i++ {
			if order[i-1].ns > order[i].ns {
				v.add("WT-012", SeverityTerminal,
					fmt.Sprintf("%s: %s (%d) is after %s (%d); the endpoint containment the gate relies on does not hold",
						label, order[i-1].name, order[i-1].ns, order[i].name, order[i].ns))
			}
		}
		for _, r := range []Record{e.Physical.start, e.Physical.end, e.Peer.start, e.Peer.end, e.Trace.start, e.Trace.end} {
			if !r.Instant.Scorable() {
				v.add("WT-010", SeverityIneligible,
					fmt.Sprintf("%s: a %s endpoint was read from %s, which is not the clock the contract scores", label, r.Producer, r.Instant.ClockID))
			}
			if boot == "" {
				boot = r.Instant.BootID
			} else if r.Instant.BootID != boot && r.Instant.BootID != "" {
				v.add("WT-011", SeverityTerminal,
					fmt.Sprintf("%s: a %s endpoint carries boot identity %s, but the run's is %s", label, r.Producer, r.Instant.BootID, boot))
			}
		}
		// Only an independently observed containment or process-lifecycle event
		// may DELIMIT a peer or trace lifecycle.
		for _, r := range []Record{e.Peer.start, e.Peer.end, e.Trace.start, e.Trace.end} {
			if r.Source != SourceContainment && r.Source != SourceProcessLifecycle {
				v.add("WT-013", SeverityTerminal,
					fmt.Sprintf("%s: a %s endpoint is delimited by a %s event; an annotation may annotate a lifecycle but never bound one", label, r.Producer, r.Source))
			}
		}
		if !e.Containment.Scorable() {
			v.add("WT-009", SeverityIneligible,
				fmt.Sprintf("%s: containment is %q, which cannot prove that no descendant escaped", label, e.Containment.Primitive))
		}
		for _, r := range []Record{e.Peer.start, e.Peer.end, e.Trace.start, e.Trace.end} {
			if !r.Containment.Same(e.Containment) {
				v.add("WT-008", SeverityTerminal,
					fmt.Sprintf("%s: the %s ledger names containment %s/%s, not the physical %s/%s",
						label, r.Producer, r.Containment.ID, r.Containment.Inode, e.Containment.ID, e.Containment.Inode))
			}
		}
	}
}

// verifyIndependence proves the peer and the trace are two observers, not one
// observer written down twice. This is the check that a "cheap" implementation
// fails: copying an endpoint, sharing a process, or reusing a raw event id all
// land here.
func verifyIndependence(v *Verdict, envs []Envelope) {
	for _, e := range envs {
		if !e.Peer.OK || !e.Trace.OK {
			continue
		}
		label := fmt.Sprintf("%s[%d]", e.Level, e.Seq)
		pairs := []struct {
			what        string
			peer, trace Record
		}{
			{"admission", e.Peer.start, e.Trace.start},
			{"verified-empty", e.Peer.end, e.Trace.end},
		}
		for _, p := range pairs {
			if p.peer.RawEventID == p.trace.RawEventID {
				v.add("WT-006", SeverityTerminal,
					fmt.Sprintf("%s: the peer and the trace cite the same raw %s event id %q; that is one observation recorded twice", label, p.what, p.peer.RawEventID))
			}
			if p.peer.RawEventDigest == p.trace.RawEventDigest && p.peer.RawEventDigest != "" {
				v.add("WT-006", SeverityTerminal,
					fmt.Sprintf("%s: the peer and the trace cite identical raw %s evidence digests", label, p.what))
			}
			if p.peer.Instant.Mono == p.trace.Instant.Mono {
				v.add("WT-007", SeverityTerminal,
					fmt.Sprintf("%s: the peer and the trace report the same %s reading (%d ns); an endpoint was copied, not read", label, p.what, p.peer.Instant.Mono))
			}
			if p.peer.ProducerID == p.trace.ProducerID {
				v.add("WT-005", SeverityTerminal,
					fmt.Sprintf("%s: the peer and the trace share execution context %s; they are not independent observers", label, p.peer.ProducerID))
			}
			if p.peer.SignerID == p.trace.SignerID && p.peer.SignerID != "" {
				v.add("WT-005", SeverityIneligible,
					fmt.Sprintf("%s: the peer and the trace share signer %s", label, p.peer.SignerID))
			}
			if p.peer.Hash == p.trace.Hash {
				v.add("WT-005", SeverityTerminal, fmt.Sprintf("%s: the peer and the trace wrote an identical %s record", label, p.what))
			}
		}
		// The physical wrapper is a third producer; an endpoint it shares with
		// an observer is equally a copy.
		if e.Physical.OK {
			for _, o := range []Record{e.Peer.start, e.Trace.start} {
				if o.Instant.Mono == e.Physical.start.Instant.Mono {
					v.add("WT-007", SeverityTerminal,
						fmt.Sprintf("%s: the %s start reading equals the physical wrapper's; endpoints must be read, not shared", label, o.Producer))
				}
			}
		}
	}
}

// comparableEndpoints reports whether an envelope's six readings can be
// compared at all: same clock identity, same non-empty epoch identity.
func comparableEndpoints(e Envelope) bool {
	if !e.Physical.OK || !e.Peer.OK || !e.Trace.OK {
		return false
	}
	recs := []Record{e.Physical.start, e.Physical.end, e.Peer.start, e.Peer.end, e.Trace.start, e.Trace.end}
	clock, boot := recs[0].Instant.ClockID, recs[0].Instant.BootID
	if boot == "" {
		return false
	}
	for _, r := range recs[1:] {
		if r.Instant.ClockID != clock || r.Instant.BootID != boot {
			return false
		}
	}
	return true
}

// derivePartition builds the exclusive, ordered, gapless physical partition of
// each parent from PHYSICAL endpoints only. Peer and trace intervals are
// auxiliary; they appear here only as the boundary between a bootstrap phase
// and the work it precedes.
func derivePartition(v *Verdict, envs []Envelope) []Phase {
	var action, script *Envelope
	var invocations []Envelope
	for i := range envs {
		switch envs[i].Level {
		case LevelAction:
			action = &envs[i]
		case LevelScript:
			script = &envs[i]
		case LevelInvocation:
			invocations = append(invocations, envs[i])
		}
	}
	sort.Slice(invocations, func(i, j int) bool { return invocations[i].Physical.StartNs < invocations[j].Physical.StartNs })

	var phases []Phase
	if action != nil && comparableEndpoints(*action) {
		p := []Phase{
			{ComponentID: "action_containment_bootstrap", Parent: "action", StartNs: action.Physical.StartNs, EndNs: action.Peer.StartNs},
		}
		mid := action.Peer.EndNs
		if script != nil && script.Physical.OK {
			p = append(p,
				Phase{ComponentID: "action_prologue", Parent: "action", StartNs: action.Peer.StartNs, EndNs: script.Physical.StartNs},
				Phase{ComponentID: "bucket_script", Parent: "action", StartNs: script.Physical.StartNs, EndNs: script.Physical.EndNs},
				Phase{ComponentID: "action_epilogue_flush", Parent: "action", StartNs: script.Physical.EndNs, EndNs: mid},
			)
		} else {
			p = append(p, Phase{ComponentID: "action_prologue", Parent: "action", StartNs: action.Peer.StartNs, EndNs: mid})
		}
		p = append(p, Phase{ComponentID: "action_suffix", Parent: "action", StartNs: mid, EndNs: action.Physical.EndNs})
		checkPartition(v, "action", action.Physical, p)
		phases = append(phases, p...)
	}
	if script != nil && comparableEndpoints(*script) {
		p := []Phase{{ComponentID: "script_containment_bootstrap", Parent: "script", StartNs: script.Physical.StartNs, EndNs: script.Peer.StartNs}}
		cursor := script.Peer.StartNs
		for i, inv := range invocations {
			if !inv.Physical.OK {
				continue
			}
			p = append(p,
				Phase{ComponentID: "between_invocation_gap", Index: i, Parent: "script", StartNs: cursor, EndNs: inv.Physical.StartNs},
				Phase{ComponentID: "invocation", Index: i, Parent: "script", StartNs: inv.Physical.StartNs, EndNs: inv.Physical.EndNs},
			)
			cursor = inv.Physical.EndNs
		}
		p = append(p,
			Phase{ComponentID: "script_epilogue", Parent: "script", StartNs: cursor, EndNs: script.Peer.EndNs},
			Phase{ComponentID: "script_suffix", Parent: "script", StartNs: script.Peer.EndNs, EndNs: script.Physical.EndNs},
		)
		checkPartition(v, "script", script.Physical, p)
		phases = append(phases, p...)
	}
	for i, inv := range invocations {
		if !comparableEndpoints(inv) {
			continue
		}
		p := []Phase{
			{ComponentID: "invocation_bootstrap", Index: i, Parent: "invocation", StartNs: inv.Physical.StartNs, EndNs: inv.Peer.StartNs},
			{ComponentID: "invocation_containment", Index: i, Parent: "invocation", StartNs: inv.Peer.StartNs, EndNs: inv.Peer.EndNs},
			{ComponentID: "invocation_suffix", Index: i, Parent: "invocation", StartNs: inv.Peer.EndNs, EndNs: inv.Physical.EndNs},
		}
		checkPartition(v, fmt.Sprintf("invocation[%d]", i), inv.Physical, p)
		phases = append(phases, p...)
	}

	// Nesting: every V inside VB, VB inside A. A nested envelope that starts
	// before its parent is not a rounding error; it means the two ledgers are
	// not measuring the same run.
	if script != nil && action != nil && script.Physical.OK && action.Physical.OK {
		if script.Physical.StartNs < action.Physical.StartNs || script.Physical.EndNs > action.Physical.EndNs {
			v.add("WT-015", SeverityTerminal, "the bucket script envelope is not contained in the action envelope")
		}
	}
	for i, inv := range invocations {
		if script == nil || !script.Physical.OK || !inv.Physical.OK {
			continue
		}
		if inv.Physical.StartNs < script.Physical.StartNs || inv.Physical.EndNs > script.Physical.EndNs {
			v.add("WT-015", SeverityTerminal, fmt.Sprintf("invocation[%d] is not contained in the bucket script envelope", i))
		}
	}
	for i := 1; i < len(invocations); i++ {
		if invocations[i].Physical.OK && invocations[i-1].Physical.OK && invocations[i].Physical.StartNs < invocations[i-1].Physical.EndNs {
			v.add("WT-015", SeverityTerminal, fmt.Sprintf("invocation[%d] overlaps invocation[%d]; a serial bucket cannot have two invocations at once", i, i-1))
		}
	}
	return phases
}

// checkPartition proves the phases are ordered, non-overlapping and sum
// EXACTLY to the parent in integer nanoseconds. "Approximately" is not
// available here: an unaccounted nanosecond is an unaccounted phase.
func checkPartition(v *Verdict, label string, parent Interval, phases []Phase) {
	var total int64
	cursor := parent.StartNs
	for _, p := range phases {
		if p.StartNs != cursor {
			v.add("WT-016", SeverityTerminal,
				fmt.Sprintf("%s partition: %s starts at %d but the previous phase ended at %d", label, p.Name(), p.StartNs, cursor))
		}
		if p.EndNs < p.StartNs {
			v.add("WT-016", SeverityTerminal, fmt.Sprintf("%s partition: %s ends before it starts", label, p.Name()))
		}
		total += p.Duration()
		cursor = p.EndNs
	}
	if cursor != parent.EndNs {
		v.add("WT-016", SeverityTerminal,
			fmt.Sprintf("%s partition: the last phase ends at %d but the envelope ends at %d", label, cursor, parent.EndNs))
	}
	if total != parent.Duration() {
		v.add("WT-016", SeverityTerminal,
			fmt.Sprintf("%s partition: phases total %d ns but the envelope is %d ns", label, total, parent.Duration()))
	}
}

// reconcile computes the LIKE-FOR-LIKE trace-minus-peer deltas per level.
func reconcile(envs []Envelope) []Reconciliation {
	byLevel := map[Level]*Reconciliation{}
	for _, e := range envs {
		if !e.Peer.OK || !e.Trace.OK {
			continue
		}
		r, ok := byLevel[e.Level]
		if !ok {
			r = &Reconciliation{Level: e.Level}
			byLevel[e.Level] = r
		}
		r.Deltas = append(r.Deltas, e.Trace.Duration()-e.Peer.Duration())
	}
	out := make([]Reconciliation, 0, len(byLevel))
	for _, l := range []Level{LevelAction, LevelScript, LevelInvocation} {
		if r, ok := byLevel[l]; ok {
			out = append(out, *r)
		}
	}
	return out
}

func summarise(v *Verdict, envs []Envelope) {
	for _, e := range envs {
		switch e.Level {
		case LevelAction:
			v.ActionNs = e.Physical.Duration()
		case LevelScript:
			v.ScriptNs = e.Physical.Duration()
		case LevelInvocation:
			v.InvocationNs = append(v.InvocationNs, e.Physical.Duration())
		}
	}
}

// verifyStageBinding proves the records were produced under the frozen inputs
// and the single derived plan. A record that names no Stage-2 receipt might
// have measured a plan nobody authorised, so it is not scorable.
func verifyStageBinding(v *Verdict, opt VerifyOptions, recs []Record) boundIdentities {
	var bound boundIdentities
	var stage1 *Stage1Manifest
	var stage2 *Stage2Receipt
	if opt.Stage1Path != "" {
		var m Stage1Manifest
		if err := ReadJSONFile(opt.Stage1Path, &m); err != nil {
			v.add("WT-018", SeverityIneligible, fmt.Sprintf("stage-1 manifest: %v", err))
		} else if err := m.Validate(); err != nil {
			v.add("WT-018", SeverityIneligible, fmt.Sprintf("stage-1 manifest: %v", err))
		} else {
			stage1 = &m
		}
	} else {
		v.add("WT-018", SeverityIneligible, "no Stage-1 input manifest was supplied; the records are not bound to authorised inputs")
	}
	if opt.Stage2Path != "" {
		var r Stage2Receipt
		if err := ReadJSONFile(opt.Stage2Path, &r); err != nil {
			v.add("WT-018", SeverityIneligible, fmt.Sprintf("stage-2 receipt: %v", err))
		} else if err := r.Validate(); err != nil {
			v.add("WT-018", SeverityIneligible, fmt.Sprintf("stage-2 receipt: %v", err))
		} else {
			stage2 = &r
		}
	} else {
		v.add("WT-018", SeverityIneligible, "no Stage-2 derived-plan receipt was supplied; the records are not bound to one authorised plan")
	}
	if stage1 != nil {
		bound.registry = stage1.Registry
		bound.scorer = stage1.TrainingLineage.ScorerDigest
	}
	if stage2 != nil {
		bound.membership = stage2.MembershipDigest
		if d, err := stage2.DigestOf(); err == nil {
			bound.stage2 = d
		}
	}
	if stage1 != nil && stage2 != nil {
		d1, err := stage1.DigestOf()
		if err == nil {
			bound.stage1 = d1
		}
		if err == nil && stage2.Stage1Digest != d1 {
			v.add("WT-018", SeverityIneligible,
				fmt.Sprintf("the Stage-2 receipt names parent %s but the supplied Stage-1 manifest digests to %s", stage2.Stage1Digest, d1))
		}
		bd, err := stage1.Bundle.DigestOf()
		if err == nil && stage2.BundleDigest != bd {
			v.add("WT-018", SeverityIneligible,
				fmt.Sprintf("the Stage-2 receipt names input bundle %s but Stage 1 binds %s", stage2.BundleDigest, bd))
		}
		verifyAuthority(v, opt, stage1, d1)
	}
	// Every boundary record must carry the same Stage-1/Stage-2 identity.
	for _, r := range recs {
		if r.Kind != "boundary" {
			continue
		}
		if r.Run.Stage2 == "" {
			v.add("WT-018", SeverityIneligible,
				fmt.Sprintf("%s/%s record %d does not name a Stage-2 receipt", r.Producer, r.Level, r.Seq))
			break
		}
		if stage2 != nil {
			if d, err := stage2.DigestOf(); err == nil && r.Run.Stage2 != d {
				v.add("WT-018", SeverityIneligible,
					fmt.Sprintf("%s/%s record %d is bound to Stage-2 receipt %s, not the supplied %s", r.Producer, r.Level, r.Seq, r.Run.Stage2, d))
				break
			}
		}
	}
	if stage1 != nil {
		verifyProducerBinaries(v, stage1.Instrumentation, recs)
	}
	return bound
}

// verifyAuthority checks the Stage-1 signature against a PREDECLARED authority
// key.
//
// Passing an empty allowed-key set would accept any self-generated key, which
// is not an authority check at all: the point of the protected environment is
// that one identity, chosen before the campaign, authorises inputs. So a run
// with no predeclared key is ineligible — loudly — rather than trusting
// whatever signed the manifest.
func verifyAuthority(v *Verdict, opt VerifyOptions, m *Stage1Manifest, digest Digest) {
	if m.Signature == nil {
		v.add("WT-018", SeverityIneligible,
			"the Stage-1 manifest is unsigned; only the protected campaign authority may authorise inputs")
		return
	}
	if len(opt.AuthorityKeys) == 0 {
		v.add("WT-018", SeverityIneligible,
			fmt.Sprintf("the Stage-1 manifest is signed by %s but no authority key was predeclared to this verifier, so any self-generated key would pass", m.Signature.KeyID))
		return
	}
	if err := VerifySigned(m.Signature, digest, opt.AuthorityKeys); err != nil {
		v.add("WT-018", SeverityIneligible, fmt.Sprintf("stage-1 authority signature: %v", err))
		return
	}
	if m.Signature.Authority != opt.Authority && opt.Authority != "" {
		v.add("WT-018", SeverityIneligible,
			fmt.Sprintf("the Stage-1 manifest names authority %q, not the expected %q", m.Signature.Authority, opt.Authority))
	}
}

// verifyProducerBinaries binds each producer's records to the binary identity
// Stage 1 approved.
//
// The per-record signing keys are minted per run and cannot be predeclared —
// they do not exist when the manifest is signed. What CAN be bound is the
// binary allowed to mint them, and every ProducerID carries the digest of the
// executable that wrote the record, so the check is a real delivery binding
// rather than a field nobody reads.
func verifyProducerBinaries(v *Verdict, id InstrumentationIdentity, recs []Record) {
	want := map[Producer]Digest{
		ProducerPhysical: id.PhysicalBinary,
		ProducerPeer:     id.PeerBinary,
		ProducerTrace:    id.TraceBinary,
	}
	reported := map[Producer]map[string]bool{}
	for _, r := range recs {
		if r.ProducerID == "" {
			continue
		}
		if reported[r.Producer] == nil {
			reported[r.Producer] = map[string]bool{}
		}
		reported[r.Producer][r.ProducerID] = true
	}
	for producer, digest := range want {
		if digest == "" {
			v.add("WT-018", SeverityIneligible,
				fmt.Sprintf("Stage 1 binds no binary identity for the %s, so its records cannot be tied to an approved build", producer))
			continue
		}
		for context := range reported[producer] {
			if !strings.Contains(context, shortDigest(digest)) {
				v.add("WT-018", SeverityIneligible,
					fmt.Sprintf("a %s record was written by execution context %q, which is not the binary Stage 1 approved (%s)",
						producer, context, digest))
				break
			}
		}
	}
}

func loadRegistry(v *Verdict, opt VerifyOptions) *AetaRegistry {
	if opt.RegistryPath == "" {
		v.add("WT-017", SeverityIneligible, "no frozen Aeta component registry was supplied; ETA completeness cannot be proven")
		return nil
	}
	var r AetaRegistry
	if err := ReadJSONFile(opt.RegistryPath, &r); err != nil {
		v.add("WT-017", SeverityIneligible, fmt.Sprintf("aeta registry: %v", err))
		return nil
	}
	if err := r.Validate(); err != nil {
		v.add("WT-017", SeverityIneligible, fmt.Sprintf("aeta registry: %v", err))
		return nil
	}
	return &r
}

// loadAeta reads the pre-action forecast and RE-DERIVES it from the frozen
// template. A forecast that is merely well-formed proves nothing: the whole
// point of the two-stage freeze is that Stage 2 could only instantiate, so the
// verifier instantiates too and compares component by component.
func loadAeta(v *Verdict, opt VerifyOptions, registry *AetaRegistry, stage2 Digest) *AetaInstance {
	if opt.AetaPath == "" {
		return nil
	}
	var a AetaInstance
	if err := ReadJSONFile(opt.AetaPath, &a); err != nil {
		v.add("WT-017", SeverityIneligible, fmt.Sprintf("aeta instance: %v", err))
		return nil
	}
	if registry == nil {
		v.add("WT-017", SeverityIneligible,
			"a pre-action forecast was supplied with no frozen registry, so it cannot be re-derived and cannot be scored")
		return nil
	}
	for _, p := range a.Recompute(*registry) {
		v.add("WT-017", SeverityIneligible, "the pre-action forecast does not re-derive from the frozen template: "+p)
	}
	if stage2 != "" && a.Stage2 != stage2 {
		v.add("WT-017", SeverityIneligible,
			fmt.Sprintf("the pre-action forecast names Stage-2 %s, not the verified %s", a.Stage2, stage2))
	}
	if v.has(SeverityIneligible) {
		// Still return it: the gates should report the numbers the run
		// actually claimed, alongside the finding that says they are unbound.
		return &a
	}
	return &a
}

// evaluateGates runs every applicable frozen gate. A gate with no evidence
// does not disappear: it is reported with an empty population and does not
// pass.
// boundIdentities are the verified plan identities the derived documents must
// name. They come from the Stage-1/Stage-2 documents, not from the documents
// being checked.
type boundIdentities struct {
	stage1     Digest
	stage2     Digest
	membership Digest
	scorer     Digest
	registry   Digest
}

func evaluateGates(v *Verdict, opt VerifyOptions, aeta *AetaInstance, bound boundIdentities) {
	expected := map[Level]int{LevelAction: 1, LevelScript: 1}
	for _, r := range v.Recon {
		v.Gates = append(v.Gates, r.Gate(expected[r.Level]))
	}
	for _, l := range []Level{LevelAction, LevelScript, LevelInvocation} {
		found := false
		for _, r := range v.Recon {
			if r.Level == l {
				found = true
			}
		}
		if !found {
			v.Gates = append(v.Gates, GateResult{
				Name:     fmt.Sprintf("reconciliation:%s", l),
				Required: fmt.Sprintf("MAE <= %s and every |error| <= %s", dur(ReconMAELimit), dur(ReconMaxLimit)),
				Observed: "no like-for-like pair", Detail: "no peer/trace pair was recorded at this level",
			})
		}
	}
	if opt.PcheckPath != "" {
		v.Gates = append(v.Gates, predictorGates(v, opt, bound.stage2, bound.membership, bound.scorer)...)
	} else {
		v.Gates = append(v.Gates, GateResult{
			Name: "predictor:invocation-max", Scope: ScopeRow, Required: "<= " + dur(PcheckInvocationMaxLimit),
			Observed: "no projection", Detail: "no frozen Pcheck projection was supplied",
		})
	}
	if aeta != nil {
		// expected is 1 because this is a ROW: the individual-error, interval
		// and width rules are decided here, and the population mean is
		// reported campaign-scope, where it belongs.
		v.Gates = append(v.Gates, EvaluateAeta([]AetaSample{aeta.Sample(v.ActionNs)}, 1)...)
	} else {
		v.Gates = append(v.Gates, GateResult{
			Name: "aeta:point-max", Scope: ScopeRow, Required: "<= " + dur(AetaMaxLimit),
			Observed: "no forecast", Detail: "no pre-action Aeta instance was supplied",
		})
	}
	for i := range v.Gates {
		if v.Gates[i].Scope == ScopeCampaign {
			v.Gates[i].Pass = false
			v.Gates[i].Expected = ScoredActionRows
			v.Gates[i].Detail = firstNonEmptyStr(v.Gates[i].Detail,
				"campaign-scope: decided by `wall campaign` over the full frozen population, never by one row")
		}
	}
}

// predictorGates reads the audit projection, RECOMPUTES every prediction from
// the frozen values it carries, and binds it to the verified plan before
// comparing anything to an observation. A projection nobody can recompute is a
// number, and a number that names no plan is not an audit of this one.
func predictorGates(v *Verdict, opt VerifyOptions, stage2, membership, scorer Digest) []GateResult {
	var doc PcheckDocument
	if err := ReadJSONFile(opt.PcheckPath, &doc); err != nil {
		v.add("WT-019", SeverityIneligible, fmt.Sprintf("pcheck projection: %v", err))
		return nil
	}
	for _, p := range doc.Recompute() {
		v.add("WT-019", SeverityIneligible, "the Pcheck projection does not recompute: "+p)
	}
	if stage2 != "" && doc.Stage2 != stage2 {
		v.add("WT-019", SeverityIneligible,
			fmt.Sprintf("the Pcheck projection names Stage-2 %s, not the verified %s", doc.Stage2, stage2))
	}
	if membership != "" && doc.MembershipDigest != membership {
		v.add("WT-019", SeverityIneligible,
			fmt.Sprintf("the Pcheck projection covers membership %s but the verified plan rendered %s", doc.MembershipDigest, membership))
	}
	if scorer != "" && doc.ScorerDigest != scorer {
		v.add("WT-019", SeverityIneligible,
			fmt.Sprintf("the Pcheck projection came from scorer %s but Stage 1 binds %s", doc.ScorerDigest, scorer))
	}
	observed := map[int]int64{}
	for _, e := range v.Envelopes {
		if e.Level == LevelInvocation && e.Physical.OK {
			observed[e.Seq] = e.Physical.Duration()
		}
	}
	var samples []PredictorSample
	for _, inv := range doc.Invocations {
		got, ok := observed[inv.Seq]
		if !ok {
			v.add("WT-019", SeverityIneligible,
				fmt.Sprintf("pcheck projects invocation %d, which was never observed", inv.Seq))
			continue
		}
		samples = append(samples, PredictorSample{InvocationSeq: inv.Seq, BucketIndex: inv.BucketIndex, PredictedNs: inv.PredictedNs, ObservedNs: got})
	}
	if len(samples) != len(observed) {
		v.add("WT-019", SeverityIneligible,
			fmt.Sprintf("pcheck covers %d of the %d observed invocations; a skipped interval does not qualify", len(samples), len(observed)))
	}
	return EvaluatePredictor(samples)
}

// Write renders the verdict as the report a job log should show: the numbers
// first, then every finding, then the verdict itself.
func (v *Verdict) Write(out io.Writer) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "wall-time verification — %s\n\n", v.Dir)
	fmt.Fprintf(w, "envelope\tphysical\tpeer\ttrace\tterminal\n")
	for _, e := range v.Envelopes {
		fmt.Fprintf(w, "%s[%d]\t%s\t%s\t%s\t%s\n", e.Level, e.Seq,
			dur(e.Physical.Duration()), dur(e.Peer.Duration()), dur(e.Trace.Duration()), firstNonEmptyStr(e.Terminal, "-"))
	}
	if len(v.Phases) > 0 {
		fmt.Fprintf(w, "\nphysical partition\n")
		for _, p := range v.Phases {
			fmt.Fprintf(w, "  %s\t%s\t(%s)\n", p.Parent, p.Name(), dur(p.Duration()))
		}
	}
	fmt.Fprintf(w, "\ngate\tscope\trequired\tobserved\tn\tresult\n")
	for _, g := range v.Gates {
		result := "FAIL"
		switch {
		case g.Pass:
			result = "pass"
		case g.Scope == ScopeCampaign:
			// A campaign-scope gate is not failing here; it is simply not this
			// row's to decide, and printing FAIL would read as a defect.
			result = "campaign"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", g.Name, g.Scope, g.Required, g.Observed, g.Population, result)
		if g.Detail != "" {
			fmt.Fprintf(w, "\t\t%s\n", g.Detail)
		}
	}
	if len(v.Findings) > 0 {
		fmt.Fprintf(w, "\nfindings\n")
		for _, f := range v.Findings {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", f.Code, f.Severity, f.Detail)
		}
	}
	fmt.Fprintf(w, "\ncomplete: %v\teligible: %v\n", v.Complete, v.Eligible)
	if v.Eligible {
		fmt.Fprintf(w, "this row qualifies; the campaign-scope gates above are decided by `wall campaign` over the full frozen population.\n")
	} else {
		fmt.Fprintf(w, "this run contributes 0 scored rows; an absent measurement never fills a denominator.\n")
	}
	return w.Flush()
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
