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
	// Instants, for the same reason as Interval's.
	StartNs Nanos `json:"start_ns"`
	EndNs   Nanos `json:"end_ns"`
}

// Duration is the phase's exclusive half-open length.
func (p Phase) Duration() int64 { return int64(p.EndNs - p.StartNs) }

// Name renders the phase for a report.
func (p Phase) Name() string {
	if p.Index > 0 || p.ComponentID == "invocation" || p.ComponentID == "between_invocation_gap" {
		return fmt.Sprintf("%s[%d]", p.ComponentID, p.Index)
	}
	return p.ComponentID
}

// Interval is one producer's bracketed span at one level.
type Interval struct {
	// Instants are Nanos, not int64: an epoch-nanosecond count is above 2^53,
	// and a verdict is CANONICALISED before it is signed. Carried as a number
	// it would either round or refuse to canonicalise at all — and it refused,
	// on the first host whose scorable clock reads a real epoch.
	StartNs Nanos `json:"start_ns"`
	EndNs   Nanos `json:"end_ns"`
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
	return int64(i.EndNs - i.StartNs)
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
	// StepAttemptPath is the GitHub step-attempt diagnostic (A_GH). It is
	// never a gate — GitHub reports seconds — but the contract requires it for
	// identity sanity, and it is what makes the unmeasurable binary-install
	// prefix visible instead of merely absent.
	StepAttemptPath string
	// InvocationsPath is the per-bucket invocation manifest: what the
	// authorised plan rendered. Without it a measured Spec is an assertion
	// travelling beside the plan rather than a claim checked against it.
	InvocationsPath string
	// ReplayPath is the independent Stage-2 replay attestation. Without it the
	// records are bound to a receipt nobody re-derived, so the run cannot be
	// scored: comparing the planner's account of its own output to itself
	// proves nothing.
	ReplayPath string
	// AuthorityKeys are the PREDECLARED public keys of the protected campaign
	// environment. An empty set is not "accept any key": it means no authority
	// was declared, and the run is ineligible.
	AuthorityKeys []string
	// ScorerPath is the frozen scorer the Pcheck projection claims. Without it
	// the projection is only checked against its own arithmetic, which a
	// substituted allocation map satisfies.
	ScorerPath string
	// TrainingSetPath is the EXACT sealed training receipt set the scorer was
	// fitted from. Without it the offline surface is checked only by its own
	// digest — a string the scorer supplies about itself — so the run is
	// ineligible rather than trusted. With it the verifier revalidates the
	// set under the authority Stage 1 declared and REFITS the scorer, which is
	// the only check that separates a model built from this evidence from one
	// that merely cites it.
	TrainingSetPath string
	// Audit runs the exact-run coverage audit for the measured bucket. A nil
	// Audit is not "no audit needed": it makes the row ineligible, because a
	// row nobody audited cannot be shown to have run its plan.
	Audit AuditFunc
	// Authority, when set, is the protected environment name the manifest must
	// name.
	Authority string
	// SignerKeys are the PREDECLARED public keys allowed to sign the roster
	// and the closing seal — the run keys. They come from the Stage-1
	// manifest, so a run whose signer set nobody declared is ineligible
	// rather than trusted.
	SignerKeys []string
}

// runIdentityDiff names the FIRST field two identities disagree about, or "".
// Every field is compared: a check that looked at three of them would accept a
// record that agreed about the campaign and the run while naming another
// attempt, job, step, plan or verifier.
func runIdentityDiff(want, got RunIdentity) string {
	for _, f := range []struct {
		name      string
		want, got string
	}{
		{"campaign_id", want.CampaignID, got.CampaignID},
		{"run_id", want.RunID, got.RunID},
		{"attempt_id", want.AttemptID, got.AttemptID},
		{"bucket_id", want.BucketID, got.BucketID},
		{"repository", want.Repository, got.Repository},
		{"workflow_run", want.WorkflowRun, got.WorkflowRun},
		{"job", want.Job, got.Job},
		{"step", want.Step, got.Step},
		{"step_attempt", want.StepAttempt, got.StepAttempt},
		{"stage1_digest", string(want.Stage1), string(got.Stage1)},
		{"stage2_digest", string(want.Stage2), string(got.Stage2)},
		{"component_registry_digest", string(want.ComponentRegistry), string(got.ComponentRegistry)},
		{"verifier_id", want.VerifierID, got.VerifierID},
	} {
		if f.want != f.got {
			return fmt.Sprintf("%s is %q, not %q", f.name, f.got, f.want)
		}
	}
	return ""
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
	// File is the ledger the records were read from.
	//
	// A hash chain is a property of ONE WRITER'S FILE. Grouping by the
	// identity a file claims merged two files that claimed the same
	// producer/level/sequence into one group, and the second file's intact
	// chain then "did not chain to its predecessor" — a terminal finding about
	// the reader rather than about the evidence. Two files claiming one stream
	// identity is a real problem, and it is reported as itself below.
	File string
}

func groupStreams(recs []Record) map[streamKey][]Record {
	out := map[streamKey][]Record{}
	for _, r := range recs {
		k := streamKey{r.Producer, r.Level, r.Seqno, r.streamFile()}
		out[k] = append(out[k], r)
	}
	for k := range out {
		s := out[k]
		sort.SliceStable(s, func(i, j int) bool { return s[i].Seq < s[j].Seq })
	}
	return out
}

func mapOfKeys(m map[string]map[string]bool) map[string]string {
	out := map[string]string{}
	for k := range m {
		out[k] = k
	}
	return out
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

// isActionChildStream reports whether a ledger is an action-owned child's
// rather than an envelope's. Such a ledger carries only `action_child`
// records: the pre-spawn containment proof and the child's own identity.
func isActionChildStream(recs []Record) bool {
	if len(recs) == 0 {
		return false
	}
	for _, r := range recs {
		if r.Kind != "action_child" {
			return false
		}
	}
	return true
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
				iv.StartNs, iv.start, haveStart = Nanos(r.Instant.Mono), r, true
			}
		case "end":
			iv.EndNs, iv.end, haveEnd = Nanos(r.Instant.Mono), r, true
		}
	}
	iv.OK = haveStart && haveEnd
	return iv
}

// containmentControlOrUnknown names an unset membership-control fact rather
// than printing an empty string: a record written before this was established
// says nothing about the model, which is itself the answer.
func containmentControlOrUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return MembershipUnknown + " (the record states none)"
	}
	return s
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

// evaluateGates runs every applicable frozen gate. A gate with no evidence
// does not disappear: it is reported with an empty population and does not
// pass.
// boundIdentities are the verified plan identities the derived documents must
// name. They come from the Stage-1/Stage-2 documents, not from the documents
// being checked.
type boundIdentities struct {
	// signers are the run-key public keys Stage 1 declared: the only keys
	// whose roster and seal this verifier will accept.
	signers []string
	// replaySigners are the keys allowed to attest an independent replay.
	replaySigners []string
	// planStage2 is the receipt's PLAN identity — its Stage-2 digest without
	// the binding over the documents it derived. Derived documents cite it
	// rather than the full receipt digest, because the full digest covers a
	// binding taken over the documents themselves.
	planStage2 Digest
	stage1     Digest
	stage2     Digest
	membership Digest
	scorer     Digest
	registry   Digest
	// lineage is the whole training lineage Stage 1 bound, and trainingKeys
	// the authority that may have sealed the set it names. Both are needed to
	// reprove the offline surface rather than read its own account of itself.
	lineage      TrainingLineageID
	trainingKeys []string
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// BootstrapGapResolution is A_GH's own reporting resolution: GitHub reports
// step timestamps in whole SECONDS, so a gap up to one tick is
// indistinguishable from zero and cannot be attributed to anything.
const BootstrapGapResolution = int64(second)

// --- reporting kernel -------------------------------------------------------
//
// Verdict and its three practical methods are RETAINED. The reduction that
// removed the rest of this file cascaded from Verdict.Sign, which needed the
// ed25519 signer the removal plan deletes — but the verdict document itself is
// the practical reporting surface: audit.go (KEEP) writes findings into it, and
// other packages consume it. Only Sign is gone, with the signing model.
// Verdict is the whole verification result. It is JSON-serialisable because it
// is EVIDENCE: a campaign row is this document, not a log line someone read.
type Verdict struct {
	Schema string `json:"schema"`
	Dir    string `json:"dir"`
	// RecordsDigest binds this verdict to the EXACT records it was derived
	// from, and VerifierBinary to the build that derived it. Without both, a
	// campaign row is a JSON file asserting its own eligibility — and a forged
	// one is indistinguishable from a real one.
	RecordsDigest  Digest `json:"records_digest"`
	VerifierBinary Digest `json:"verifier_binary"`
	// Samples are the row's own gate observations, retained so the campaign
	// can compute the population-wide means the contract requires. A row that
	// discards them leaves an 80-row MAE uncomputable, and 80 individually
	// acceptable rows can still miss it.
	AetaSample      *AetaSample       `json:"aeta_sample,omitempty"`
	PredictorSample []PredictorSample `json:"predictor_samples,omitempty"`
	// Audit is the exact-run coverage evidence for this bucket. A verdict
	// without it says only that the measurement was well formed, never that
	// the measured work was the work the plan scheduled.
	Audit *AuditEvidence `json:"audit,omitempty"`
	// Run is the campaign/delivery identity the records carried. A campaign
	// assembles its population from verdicts, and a verdict that did not say
	// which campaign, run and plan it belongs to could be counted into any of
	// them.
	Run RunIdentity `json:"run"`
	// StartedAt and Terminal are the row's AUTHENTICATED outcome, derived from
	// the action envelope's own records rather than asserted alongside them.
	//
	// A campaign decides the three-UTC-date rule, the fourteen-day window and
	// intention-to-treat retention from these. Taken from the campaign index
	// they are unsigned text, and an index that claimed schedule-shaped dates
	// and "passed" over a genuine signed row set would satisfy all three
	// without any of the facts being authenticated. Here they are covered by
	// the verdict's signature and by the records digest behind it.
	//
	// StartedAt is the START of the action envelope's realtime bracket, in
	// UTC. The bracket is what the wrapper actually observed around its
	// monotonic reading; it is not A_GH, which the contract forbids from
	// entering any success calculation.
	StartedAt string `json:"started_at,omitempty"`
	Terminal  string `json:"terminal,omitempty"`
	// Complete means the records describe a well-formed measurement.
	Complete bool `json:"complete"`
	// Eligible means the measurement may be SCORED: complete, plus a scorable
	// clock and containment, signed records, Stage-1/Stage-2 binding, and
	// every applicable gate passing.
	Eligible  bool             `json:"eligible"`
	Envelopes []Envelope       `json:"envelopes"`
	Phases    []Phase          `json:"phases"`
	Recon     []Reconciliation `json:"reconciliation"`
	Gates     []GateResult     `json:"gates"`
	Findings  []Finding        `json:"findings"`
	ActionNs  int64            `json:"action_ns"`
	// ActionGHNs is A_GH: the GitHub step's own whole-second elapsed. It is a
	// DIAGNOSTIC and never enters a gate, a balance or a prediction; it is
	// recorded so a reader can see the action envelope against the step
	// GitHub thinks ran.
	ActionGHNs int64 `json:"action_gh_ns,omitempty"`
	// BootstrapGapNs is the action-step time before AT_start.
	//
	// It is DERIVED FROM A_GH, so it is reported and never gated. The contract
	// makes A_GH an identity/sanity diagnostic that never enters balance,
	// non-regression, prediction, or success calculation, and eligibility is a
	// success calculation — a campaign's population is assembled from eligible
	// rows. Anything here that changed a verdict would put a whole-second
	// GitHub timestamp into the result.
	//
	// Under measurement the wrapper is installed by the CALLER, before the
	// measured action starts, so `wall begin` is the action's first owned
	// operation and this gap should be nothing but the runner's own step
	// startup. That ORDERING is the control; this number is how a reader sees
	// it, not how it is enforced.
	BootstrapGapNs int64   `json:"bootstrap_gap_ns,omitempty"`
	ScriptNs       int64   `json:"script_ns"`
	InvocationNs   []int64 `json:"invocation_ns,omitempty"`
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
	if v.ActionGHNs > 0 {
		// A_GH and the bootstrap gap are printed together with A so the
		// relationship is visible: the step is longer than the envelope, and
		// the difference is named rather than left for a reader to wonder about.
		fmt.Fprintf(w, "\nGitHub step (A_GH, whole seconds, diagnostic only)\t%s\n", dur(v.ActionGHNs))
		fmt.Fprintf(w, "  of which before AT_start (wrapper install)\t%s\n", dur(v.BootstrapGapNs))
	}
	fmt.Fprintf(w, "\ncomplete: %v\teligible: %v\n", v.Complete, v.Eligible)
	if v.Eligible {
		fmt.Fprintf(w, "this row qualifies; the campaign-scope gates above are decided by `wall campaign` over the full frozen population.\n")
	} else {
		fmt.Fprintf(w, "this run contributes 0 scored rows; an absent measurement never fills a denominator.\n")
	}
	return w.Flush()
}

// VerifyDir reads a records directory and decides whether the run it describes
// is ELIGIBLE.
//
// SIMPLIFIED per salvage-map: the practical question is whether this run is a
// complete, passing measurement, and that is answered from the records' own
// terminal states. What is gone is the proof layer that used to answer it —
// chain verification, envelope independence, raw cgroup evidence, containment
// hierarchy and process-tree reconstruction — all of which served the
// hostile-runner model §0.1 places out of scope.
//
// The two properties the retained cancellation regressions turn on are kept
// exactly: an absent measurement is not a zero-length one, and a run that was
// cancelled or left an escaped descendant is RETAINED but never scored.
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
	// The exact records this verdict describes. A row that cannot name the
	// evidence behind it is a number in a file.
	if d, err := DigestJSON(recs); err == nil {
		v.RecordsDigest = d
	}
	if len(recs) == 0 {
		v.add("WT-004", SeverityTerminal, "no records: an absent measurement is not a zero-length one")
		return v, nil
	}

	// Eligibility is affirmative and fails closed: it requires a closing
	// boundary that reached `passed`, so a cancelled run, a crash, an escaped
	// descendant or a truncated stream all leave it false without needing a
	// rule of their own.
	sawEnd := false
	for _, r := range recs {
		if r.Terminal != "" && r.Terminal != TerminalPassed {
			v.add("WT-002", SeverityIneligible,
				fmt.Sprintf("a record reached terminal state %q: %s", r.Terminal, r.Reason))
		}
		if r.Kind == "boundary" && r.Boundary == "end" {
			sawEnd = true
			if r.Level == LevelAction || r.Level == LevelScript || r.Level == LevelInvocation {
				v.Run = r.Run
			}
		}
	}
	if !sawEnd {
		v.add("WT-003", SeverityTerminal, "no closing boundary record: the interval was never closed")
	}
	// Complete and Eligible are the two-level split the report renders, and
	// both are affirmative. COMPLETE is about the evidence: the records
	// describe a well-formed measurement, whatever it turned out to be — so a
	// run that failed cleanly is complete and not eligible, while a truncated
	// stream is neither. Leaving Complete unset made the report say
	// `complete: false` over a perfectly well-formed measurement.
	v.Complete = !v.has(SeverityTerminal)
	v.Eligible = v.Complete && !v.has(SeverityIneligible)
	return v, nil
}
