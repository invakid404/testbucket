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

// Envelope is one measured thing: the physical ledger's start/end pair.
//
// It carried a `peer` and a `trace` interval beside the physical one, and the
// report printed all three as columns. Nothing has produced a peer or a trace
// record since the observers were removed, so both columns were structurally
// empty — a report that shows a reader two blank quantities is telling them
// something was measured and not reported.
type Envelope struct {
	Level    Level         `json:"level"`
	Seq      int           `json:"seq"`
	Physical Interval      `json:"physical"`
	Terminal string        `json:"terminal"`
	Reason   string        `json:"reason,omitempty"`
	Spec     *SpecIdentity `json:"spec,omitempty"`
	Desc     string        `json:"desc,omitempty"`
}

// InvocationsFunc resolves the invocation manifest for one bucket. A caller
// that supplies none gets a finding, never a pass.
type InvocationsFunc func(bucketID string) (*InvocationManifest, error)

// VerifyOptions selects what to verify.
//
// It used to carry Stage1Path, Stage2Path, AetaPath, PcheckPath,
// RegistryPath, ReplayPath, AuthorityKeys, ScorerPath, TrainingSetPath,
// Authority and SignerKeys. VerifyDir runs only the practical checks and read
// NONE of them, so the interface accepted eleven controls it silently ignored
// — a caller could pass a replay attestation and an authority key set and
// believe something was being checked. They are gone with the model that
// defined them.
type VerifyOptions struct {
	Dir string
	// Records lets a caller supply an already-loaded stream (a test, or a
	// verifier reading from an archive rather than a directory).
	Records []Record
	// StepAttemptPath is the GitHub step-attempt diagnostic (A_GH). It is
	// never a gate — GitHub reports seconds — but it is what makes the
	// unmeasurable binary-install prefix visible instead of merely absent.
	StepAttemptPath string
	// Invocations resolves the per-bucket invocation manifest: what the
	// authorised plan rendered. Without it a measured Spec is an assertion
	// travelling beside the plan rather than a claim checked against it.
	//
	// It is INJECTED, and by bucket id, for the same two reasons the audit is.
	// Building the manifest means reading a plan document and rendering its
	// invocations, which belongs to the planner/adapter layer that this
	// package deliberately does not import — the measurement code must not be
	// able to reach the code it measures. And the bucket the manifest is for
	// is a fact the RECORDS carry, so it cannot be known before they are read.
	Invocations InvocationsFunc
	// Audit runs the exact-run coverage audit for the measured bucket. A nil
	// Audit is not "no audit needed": it makes the row ineligible, because a
	// row nobody audited cannot be shown to have run its plan.
	Audit AuditFunc
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

// evaluateGates runs every applicable frozen gate. A gate with no evidence
// does not disappear: it is reported with an empty population and does not
// pass.
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
	// Eligible means the measurement may be SCORED: complete, plus every
	// applicable gate passing.
	//
	// `reconciliation` is gone from this document with the peer/trace ledgers:
	// it reported like-for-like trace-minus-peer deltas, and nothing has
	// produced a peer or a trace record since the observers were removed.
	Eligible  bool         `json:"eligible"`
	Envelopes []Envelope   `json:"envelopes"`
	Phases    []Phase      `json:"phases"`
	Gates     []GateResult `json:"gates"`
	Findings  []Finding    `json:"findings"`
	ActionNs  int64        `json:"action_ns"`
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
	BootstrapGapNs int64 `json:"bootstrap_gap_ns,omitempty"`
	ScriptNs       int64 `json:"script_ns"`
	// SetupNs is the action-owned setup command's interval, derived from its
	// own boundary pair. §3.1's floor is `A >= setup_ns + script_ns`, so the
	// verdict carries the term rather than leaving it to be supplied.
	SetupNs      int64   `json:"setup_ns,omitempty"`
	InvocationNs []int64 `json:"invocation_ns,omitempty"`
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
	fmt.Fprintf(w, "envelope\tphysical\tterminal\n")
	for _, e := range v.Envelopes {
		fmt.Fprintf(w, "%s[%d]\t%s\t%s\n", e.Level, e.Seq,
			dur(e.Physical.Duration()), firstNonEmptyStr(e.Terminal, "-"))
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
// VerifyDir is the practical verifier: the six checks the simplified
// verification action retains, and nothing else.
//
// The removed checks went with their machinery — Stage-1/Stage-2 binding,
// replay attestation, the Aeta registry, the Pcheck projection, the scorer's
// training lineage, the signer roster and the closing seal all belonged to the
// protected-authority model. What is left is what a measurement can be checked
// against using only the records themselves and the plan that produced them:
//
//  1. SCHEMA — every record is of the epoch this binary understands;
//  2. TERMINAL STATE — nothing reached a state other than `passed`;
//  3. POSITIVE MONOTONIC DURATION — every interval closed, and closed after
//     it opened;
//  4. PLAN/BUCKET IDENTITY — the records name one run, and every artifact
//     compared against them is for the bucket that was measured;
//  5. EXACT MEMBERSHIP — each measured invocation ran the argv, selector, unit
//     and atom closure the authorised plan rendered;
//  6. EVENT COVERAGE — the bucket ran the work the plan gave it.
//
// COMPLETE and ELIGIBLE stay separate, and both are affirmative. Complete is
// about the evidence: the records describe a well-formed measurement, whatever
// it turned out to be — so a run that failed cleanly is complete and not
// eligible, while a truncated stream is neither. Eligible additionally
// requires every prerequisite a scorable row has, and an absent prerequisite
// is a finding rather than a skip: "nobody checked" and "it passed" must never
// reach the same verdict.
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

	verifySchema(v, recs)
	v.Envelopes = collectEnvelopes(v, recs)
	verifyRunIdentity(v, recs)
	verifyIntervals(v, v.Envelopes)
	verifyInvocationMembership(v, opt, v.Envelopes)
	verifyAudit(v, opt)
	summariseDurations(v, v.Envelopes)

	v.Complete = !v.has(SeverityTerminal)
	v.Eligible = v.Complete && !v.has(SeverityIneligible)
	return v, nil
}

// verifySchema is check 1. A schema change is a new epoch, not a migration:
// this binary cannot know what a later schema means, and guessing would make a
// verdict about records it did not understand.
func verifySchema(v *Verdict, recs []Record) {
	for _, r := range recs {
		if r.Schema != "" && r.Schema != SchemaVersion {
			v.add("WT-001", SeverityTerminal,
				fmt.Sprintf("%s/%s record %d has schema %q, want %q; a schema change is a new epoch, not a migration",
					r.Producer, r.Level, r.Seq, r.Schema, SchemaVersion))
		}
	}
}

// collectEnvelopes groups the records into one envelope per measured thing.
//
// A DUPLICATE OPENING is reported here rather than folded into a longer
// interval: two starts in one stream is a retry or a second lifecycle written
// over the first, and taking the widest pair would report the union of two
// runs as the duration of one.
func collectEnvelopes(v *Verdict, recs []Record) []Envelope {
	streams := groupStreams(recs)
	var out []Envelope
	for _, key := range sortedKeys(streams) {
		stream := streams[key]
		if isActionChildStream(stream) {
			continue
		}
		starts := 0
		for _, r := range stream {
			if r.Kind == "boundary" && r.Boundary == "start" {
				starts++
			}
		}
		if starts == 0 {
			continue
		}
		if starts > 1 {
			v.add("WT-020", SeverityTerminal,
				fmt.Sprintf("%s[%d] has %d start records; a second lifecycle in one stream is a duplicate or a retry, not a longer interval",
					key.Level, key.Ordinal, starts))
		}
		e := Envelope{Level: key.Level, Seq: key.Ordinal, Physical: boundaryInterval(stream)}
		for _, r := range stream {
			if r.Kind != "boundary" {
				continue
			}
			if r.Spec != nil {
				e.Spec = r.Spec
			}
			if r.Boundary == "end" {
				e.Terminal, e.Reason = r.Terminal, r.Reason
			}
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if levelRank(out[i].Level) != levelRank(out[j].Level) {
			return levelRank(out[i].Level) < levelRank(out[j].Level)
		}
		return out[i].Seq < out[j].Seq
	})
	return out
}

// verifyIntervals is checks 2 and 3.
func verifyIntervals(v *Verdict, envs []Envelope) {
	if len(envs) == 0 {
		v.add("WT-004", SeverityTerminal, "no envelope has an opening boundary: nothing here measures anything")
		return
	}
	closed := 0
	for _, e := range envs {
		label := fmt.Sprintf("%s[%d]", e.Level, e.Seq)
		// An interval with ONE endpoint is missing, not shorter. A run whose
		// closing record never arrived is the shape a killed runner leaves,
		// and inferring an end would turn it into a measurement.
		if !e.Physical.OK {
			v.add("WT-004", SeverityTerminal,
				fmt.Sprintf("%s: the ledger has no closed start/end pair; an interval with one endpoint is missing, not shorter", label))
		} else {
			closed++
			// POSITIVE and MONOTONIC. A reading that does not advance means
			// the two endpoints were not two reads of a running clock, so the
			// number derived from them is not a duration.
			if d := e.Physical.Duration(); d <= 0 {
				v.add("WT-012", SeverityTerminal,
					fmt.Sprintf("%s: the interval is %d ns (start %d, end %d); an endpoint pair that does not advance is not a duration",
						label, d, e.Physical.StartNs, e.Physical.EndNs))
			}
			if b := e.Physical.start.Instant.BootID; b != "" && b != e.Physical.end.Instant.BootID {
				v.add("WT-011", SeverityTerminal,
					fmt.Sprintf("%s: the endpoints carry boot identities %s and %s; two readings on different timelines are never compared",
						label, b, e.Physical.end.Instant.BootID))
			}
		}
		if e.Terminal != "" && e.Terminal != TerminalPassed {
			v.add("WT-014", SeverityIneligible,
				fmt.Sprintf("%s terminated %s: %s (retained, never scored)", label, e.Terminal, e.Reason))
		}
	}
	if closed == 0 {
		v.add("WT-003", SeverityTerminal, "no closing boundary record: the interval was never closed")
	}
}

// verifyRunIdentity is check 4's first half: the records agree about which run
// produced them.
//
// A row every record of which agrees about having NO identity is a row that
// cannot be attributed to the run that produced it, which is why an empty
// field is a finding rather than a match.
func verifyRunIdentity(v *Verdict, recs []Record) {
	seen := map[RunIdentity]bool{}
	for _, r := range recs {
		if r.Kind != "boundary" {
			continue
		}
		seen[r.Run] = true
		if r.Run.BucketID != "" || r.Run.RunID != "" {
			v.Run = r.Run
		}
		if r.Boundary == "end" && r.Level == LevelAction {
			v.Terminal = r.Terminal
			v.StartedAt = firstNonEmptyStr(v.StartedAt, r.Instant.Realtime)
		}
	}
	if len(seen) > 1 {
		var got []string
		for id := range seen {
			got = append(got, fmt.Sprintf("%+v", id))
		}
		sort.Strings(got)
		v.add("WT-026", SeverityIneligible,
			fmt.Sprintf("the records name %d different run identities: %s; one measurement belongs to one run",
				len(seen), strings.Join(got, " / ")))
	}
	if v.Run.BucketID == "" {
		v.add("WT-026", SeverityIneligible,
			"the records name no bucket; a row that cannot say which bucket it measured cannot be counted into one")
	}
}

// verifyInvocationMembership is checks 4's second half and 5.
//
// Without the manifest a measured Spec is an assertion travelling beside the
// plan rather than a claim checked against it: the verifier could confirm that
// a record names SOME argv and selector, but not that they are the ones the
// authorised plan rendered. Two legal name slices of one file have the same
// description and different units, so the comparison is over identities.
func verifyInvocationMembership(v *Verdict, opt VerifyOptions, envs []Envelope) {
	var measured []Envelope
	for _, e := range envs {
		if e.Level == LevelInvocation {
			measured = append(measured, e)
		}
	}
	if opt.Invocations == nil {
		if len(measured) > 0 {
			v.add("WT-021", SeverityIneligible,
				"no invocation manifest was supplied, so the measured argv, selector, unit membership and atom closure are not checked against the authorised plan")
		}
		return
	}
	m, err := opt.Invocations(v.Run.BucketID)
	if err != nil {
		v.add("WT-021", SeverityIneligible, fmt.Sprintf("invocation manifest: %v", err))
		return
	}
	if m == nil {
		v.add("WT-021", SeverityIneligible, "the invocation manifest lookup produced nothing")
		return
	}
	if m.Kind != InvocationManifestKind {
		v.add("WT-021", SeverityIneligible,
			fmt.Sprintf("invocation manifest kind %q, want %q", m.Kind, InvocationManifestKind))
		return
	}
	// PLAN/BUCKET IDENTITY: a manifest that names no bucket would verify
	// identically against any bucket of the same plan.
	switch {
	case m.BucketName == "":
		v.add("WT-025", SeverityIneligible,
			"the invocation manifest names no bucket, so it would verify identically against any bucket of this plan")
	case v.Run.BucketID != "" && m.BucketName != v.Run.BucketID:
		v.add("WT-025", SeverityIneligible,
			fmt.Sprintf("the invocation manifest is for bucket %q but bucket %q was measured", m.BucketName, v.Run.BucketID))
	}
	if len(m.Invocations) != len(measured) {
		v.add("WT-021", SeverityIneligible,
			fmt.Sprintf("the plan rendered %d invocation(s) but %d were measured", len(m.Invocations), len(measured)))
	}
	for _, e := range measured {
		planned, ok := m.Find(e.Seq)
		if !ok {
			v.add("WT-021", SeverityIneligible,
				fmt.Sprintf("invocation[%d] was measured but the authorised plan rendered no such invocation", e.Seq))
			continue
		}
		if e.Spec == nil {
			v.add("WT-021", SeverityIneligible,
				fmt.Sprintf("invocation[%d] carries no spec, so what it ran cannot be compared to what was planned", e.Seq))
			continue
		}
		for _, p := range planned.Compare(*e.Spec) {
			v.add("WT-021", SeverityIneligible, fmt.Sprintf("invocation[%d] %s", e.Seq, p))
		}
	}
}

// summariseDurations fills the reported spans from the envelopes the verifier
// derived, so the report's numbers come from the records rather than from
// anything a producer asserted alongside them.
func summariseDurations(v *Verdict, envs []Envelope) {
	for _, e := range envs {
		if !e.Physical.OK {
			continue
		}
		switch e.Level {
		case LevelAction:
			v.ActionNs = e.Physical.Duration()
		case LevelScript:
			v.ScriptNs = e.Physical.Duration()
		case LevelInvocation:
			v.InvocationNs = append(v.InvocationNs, e.Physical.Duration())
		case LevelSetup:
			v.SetupNs = e.Physical.Duration()
		}
	}
}
