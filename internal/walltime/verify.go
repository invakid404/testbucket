package walltime

import (
	"crypto/ed25519"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
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
	// Signature is the approved verifier's statement over this verdict.
	Signature *Signature `json:"signature,omitempty"`
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
	v := &Verdict{Schema: SchemaVersion, Dir: opt.Dir, VerifierBinary: SelfDigest()}
	// The exact records this verdict describes. A campaign row that cannot
	// name the evidence behind it is a number in a file.
	if d, err := DigestJSON(recs); err == nil {
		v.RecordsDigest = d
	}
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
	verifyRawEvidence(v, envelopes)
	verifyContainmentHierarchy(v, envelopes)
	verifyProcessTree(v, envelopes, recs)
	v.Phases = derivePartition(v, envelopes)
	v.Recon = reconcile(envelopes)
	summarise(v, envelopes)
	for _, r := range recs {
		// The action record's identity is the row's identity; a script or
		// invocation record inherits it, so the first one that names a
		// campaign is enough.
		if r.Level == LevelAction && r.Kind == "boundary" {
			v.Run = r.Run
			break
		}
	}
	verifyRunIdentity(v, recs)

	bound := verifyStageBinding(v, opt, recs)
	registry := loadRegistry(v, opt)
	if registry != nil && bound.stage1 != "" && bound.registry != "" {
		if d, err := registry.DigestOf(); err == nil && d != bound.registry {
			v.add("WT-017", SeverityIneligible,
				fmt.Sprintf("Stage 1 binds component registry %s but the supplied registry digests to %s", bound.registry, d))
		}
	}
	deriveOutcome(v, envelopes)
	verifySignerSet(v, opt, bound.signers, recs)
	verifyAudit(v, opt)
	verifyStepAttempt(v, opt, envelopes)
	verifyInvocationIdentity(v, opt, envelopes, bound.planStage2)
	aeta := loadAeta(v, opt, registry, bound.planStage2)
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

// verifyRunIdentity requires the REPEATED delivery identity to be the same on
// every record.
//
// RunIdentity is written onto every line on purpose: a record has to be
// attributable without trusting a file name or a directory layout. That only
// works if the repetition is checked. The chain, the signatures and the
// closing seal all check integrity — that nobody edited what was written — and
// a stream whose trace records name a different run is perfectly intact by all
// three: the writer signed it, the chain covers it, the seal fixes it. It is
// simply not one measurement.
//
// The Stage-2 binding check next door is not this check. It compares one field
// against one frozen document, so two streams could agree about which plan
// they measured while disagreeing about which run, bucket, attempt, job or
// step they belong to — which is exactly how a trace from another run gets
// scored beside a physical envelope from this one.
//
// The identity is taken from the action boundary, because that is the record
// the row's identity is defined by; with no action boundary the first record
// establishes it, so a directory with no action level is still checked for
// internal agreement rather than skipped.
func verifyRunIdentity(v *Verdict, recs []Record) {
	established, where := RunIdentity{}, ""
	for _, r := range recs {
		if r.Level == LevelAction && r.Kind == "boundary" {
			established, where = r.Run, fmt.Sprintf("the %s/%s boundary", r.Producer, r.Level)
			break
		}
	}
	if where == "" {
		established, where = recs[0].Run, fmt.Sprintf("the first %s/%s record", recs[0].Producer, recs[0].Level)
	}
	// The verifier identity must EXIST, not merely be repeated consistently.
	//
	// runIdentityDiff answers "do these records agree", and a row whose every
	// record, roster and seal repeats a blank verifier agrees with itself
	// perfectly. It was then scorable: no record named who verified it, and
	// nothing asked. Attribution is a property of the value, not of the
	// agreement.
	if strings.TrimSpace(established.VerifierID) == "" {
		v.add("WT-026", SeverityIneligible, fmt.Sprintf(
			"%s names no verifier identity; a row every record of which agrees about having no verifier is a row attributable to nobody", where))
	}
	for _, r := range recs {
		if diff := runIdentityDiff(established, r.Run); diff != "" {
			v.add("WT-026", SeverityTerminal, fmt.Sprintf(
				"%s/%s record %d repeats a different delivery identity than %s (%s); every record carries the full identity so that it can be attributed on its own, and two identities in one directory are two measurements",
				r.Producer, r.Level, r.Seq, where, diff))
			return
		}
	}
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

// boundaryCardinality is the rule that keeps a resumed stream honest.
//
// The action envelope is written by two processes — `wall begin` and `wall
// end` — so a writer must be able to resume an existing stream. That
// necessity is also a hazard: a correctly chained SECOND lifecycle appended to
// the same stream would be silently widened into one apparent interval, first
// start to last end, and a retry would read as a single long action. Exactly
// one start and exactly one end per stream; anything else is a duplicate or a
// retry, and both are terminal.
func checkBoundaryCardinality(v *Verdict, key streamKey, recs []Record) {
	starts, ends := 0, 0
	for _, r := range recs {
		if r.Kind != "boundary" {
			continue
		}
		switch r.Boundary {
		case "start":
			starts++
		case "end":
			ends++
		}
	}
	label := fmt.Sprintf("%s/%s[%d]", key.Producer, key.Level, key.Ordinal)
	if starts > 1 {
		v.add("WT-020", SeverityTerminal,
			fmt.Sprintf("%s has %d start records; a second lifecycle in one stream is a duplicate or a retry, not a longer interval", label, starts))
	}
	if ends > 1 {
		v.add("WT-020", SeverityTerminal,
			fmt.Sprintf("%s has %d end records; a second lifecycle in one stream is a duplicate or a retry, not a longer interval", label, ends))
	}
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

// verifyStreamIdentitiesAreUnique reports two LEDGERS claiming one stream
// identity.
//
// Chains are verified per file, because a chain is what one writer wrote. That
// makes two files claiming the same producer/level/sequence a distinct
// problem — the reader can no longer tell which of them is the stream that
// identity names — and it is reported as itself rather than as a spurious
// broken chain, which is what it used to surface as.
func verifyStreamIdentitiesAreUnique(v *Verdict, streams map[streamKey][]Record) {
	files := map[string]map[string]bool{}
	for k := range streams {
		if k.File == "" {
			continue
		}
		id := fmt.Sprintf("%s/%s#%d", k.Producer, k.Level, k.Ordinal)
		if files[id] == nil {
			files[id] = map[string]bool{}
		}
		files[id][k.File] = true
	}
	for _, id := range sortedStringKeys(mapOfKeys(files)) {
		if len(files[id]) < 2 {
			continue
		}
		names := make([]string, 0, len(files[id]))
		for n := range files[id] {
			names = append(names, n)
		}
		sort.Strings(names)
		v.add("WT-020", SeverityTerminal, fmt.Sprintf(
			"two ledgers claim the stream identity %s (%v); each is internally intact, and nothing says which of them is that stream",
			id, names))
	}
}

func mapOfKeys(m map[string]map[string]bool) map[string]string {
	out := map[string]string{}
	for k := range m {
		out[k] = k
	}
	return out
}

// verifyChains re-derives every record hash and follows the prev-hash links. A
// record that was rewritten after the fact cannot survive this, which is the
// point of writing the chain at all.
func verifyChains(v *Verdict, streams map[streamKey][]Record) {
	verifyStreamIdentitiesAreUnique(v, streams)
	for _, key := range sortedKeys(streams) {
		recs := streams[key]
		checkBoundaryCardinality(v, key, recs)
		checkProducerConsistency(v, key, recs)
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

// checkProducerConsistency refuses a stream written by two execution contexts.
//
// Streams are grouped by producer and level, not by ProducerID, because the
// action stream is legitimately written by two processes. But those two must
// be the same ROLE from the same binary; a stream carrying records from an
// unrelated context is two runs' evidence in one file.
func checkProducerConsistency(v *Verdict, key streamKey, recs []Record) {
	seen := map[string]bool{}
	for _, r := range recs {
		if r.ProducerID != "" {
			seen[r.ProducerID] = true
		}
	}
	if len(seen) <= 1 {
		return
	}
	// Two contexts are admissible only at the action level, where `wall begin`
	// and `wall end` are genuinely different processes. Even there, they must
	// agree on the binary — the part before the '#'.
	binaries := map[string]bool{}
	for id := range seen {
		if i := strings.IndexByte(id, '#'); i >= 0 {
			binaries[id[:i]] = true
		} else {
			binaries[id] = true
		}
	}
	if key.Level != LevelAction {
		v.add("WT-020", SeverityTerminal,
			fmt.Sprintf("%s/%s[%d] was written by %d execution contexts; only the action stream spans two processes",
				key.Producer, key.Level, key.Ordinal, len(seen)))
		return
	}
	if len(binaries) > 1 {
		v.add("WT-020", SeverityTerminal,
			fmt.Sprintf("%s/%s[%d] was written by %d different binaries", key.Producer, key.Level, key.Ordinal, len(binaries)))
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
				iv.StartNs, iv.start, haveStart = Nanos(r.Instant.Mono), r, true
			}
		case "end":
			iv.EndNs, iv.end, haveEnd = Nanos(r.Instant.Mono), r, true
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
			ns   Nanos
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
		// WHO MAY MOVE PROCESSES BETWEEN CONTAINMENTS.
		//
		// On cgroup-v2 `cgroup.procs` is the migration control, and the
		// documented setup gave the delegated subtree to the runner uid — the
		// same uid the measured workload runs as. The workload therefore held
		// exactly the capability Stage 1's ContainmentPolicy said it did not:
		// it could move itself or a descendant between the action, script,
		// invocation and sibling containments and rewrite the membership
		// history this envelope is built on. Stage 1 asserting otherwise made
		// it a false security claim rather than a control.
		//
		// It is now a fact the producer establishes by reading the filesystem,
		// and a run the workload could have migrated itself in is recorded in
		// full and reported ineligible rather than scored.
		if e.Containment.Primitive == PrimitiveCgroup2 && e.Containment.MembershipControl != MembershipSupervisorOwned {
			v.add("WT-031", SeverityIneligible, fmt.Sprintf(
				"%s: the containment's membership control is %q; on cgroup-v2 `cgroup.procs` is the process-migration control, so a workload that can write it can move itself between containments and the nested membership this envelope records proves nothing. A scored run needs the delegated subtree owned by a credential the measured workload does not run as (%s)",
				label, containmentControlOrUnknown(e.Containment.MembershipControl), WorkloadUIDEnv))
		}
		for _, r := range []Record{e.Peer.start, e.Peer.end, e.Trace.start, e.Trace.end} {
			if !r.Containment.Same(e.Containment) {
				v.add("WT-008", SeverityTerminal,
					fmt.Sprintf("%s: the %s ledger names containment %s/%s, not the physical %s/%s",
						label, r.Producer, r.Containment.ID, r.Containment.Inode, e.Containment.ID, e.Containment.Inode))
			}
		}
		// THE ROOT PROCESS IDENTITY, required and compared.
		//
		// The schema documents RootPID plus RootStart as the pair that closes
		// pid reuse, and nothing asked for it: a run whose every containment
		// record omitted the start identity scored, and so did one where the
		// trace named a different root process than the physical wrapper and
		// the peer. A path and an inode identify the containment; they say
		// nothing about which process it was made for, and two observers
		// watching containments created for different processes are not two
		// observers of one lifecycle.
		if e.Containment.Primitive == PrimitiveCgroup2 {
			for _, r := range []Record{e.Physical.start, e.Physical.end, e.Peer.start, e.Peer.end, e.Trace.start, e.Trace.end} {
				if r.Containment.RootPID <= 0 || strings.TrimSpace(r.Containment.RootStart) == "" {
					v.add("WT-029", SeverityIneligible, fmt.Sprintf(
						"%s: a %s endpoint names containment root process %d/%q; a pid without its start identity is a number the kernel reuses, and the contract binds PID-start identity at every level",
						label, r.Producer, r.Containment.RootPID, r.Containment.RootStart))
					continue
				}
				if !r.Containment.SameRoot(e.Containment) {
					v.add("WT-029", SeverityIneligible, fmt.Sprintf(
						"%s: the %s ledger names containment root process %d/%s, but the physical ledger names %d/%s; the observers did not watch a containment made for the same process",
						label, r.Producer, r.Containment.RootPID, r.Containment.RootStart,
						e.Containment.RootPID, e.Containment.RootStart))
				}
			}
		}
	}
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
			v.add("WT-018", SeverityIneligible, err.Error())
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
		bound.lineage = stage1.TrainingLineage
		bound.trainingKeys = stage1.TrainingAuthorityKeys
	}
	if stage2 != nil {
		bound.membership = stage2.MembershipDigest
		if d, err := stage2.DigestOf(); err == nil {
			bound.stage2 = d
		}
		if d, err := stage2.PlanDigestOf(); err == nil {
			bound.planStage2 = d
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
	if stage1 != nil && stage2 != nil {
		verifyPrePlanApproval(v, *stage1, *stage2)
	}
	if stage1 != nil {
		verifyProducerBinaries(v, stage1.Instrumentation, recs)
		// Stage 1 is where the run-key signer set is DECLARED. Taking it from
		// the manifest rather than from a verifier flag is the point: the same
		// signed document that approves the instrumentation says which keys
		// may attest to what that instrumentation produced.
		if len(stage1.Instrumentation.Signers) == 0 {
			v.add("WT-023", SeverityIneligible,
				"the Stage-1 manifest declares no record signers, so no roster or seal can be attributed and the record chain authenticates only itself")
		}
		bound.signers = stage1.Instrumentation.Signers
		bound.replaySigners = stage1.Instrumentation.ReplaySigners
	}
	verifyReplay(v, opt, stage1, stage2, bound)
	verifyTrainingSurface(v, opt, stage1, bound)
	return bound
}

// verifyTrainingSurface independently reproves the offline surface.
//
// The contract asks the verifier to prove the training set's
// signature/hash/cutoff/exclusion/causal/topology lineage and to recompute the
// frozen training and scorer identity as specified. None of that was happening:
// verification checked that the scorer's receipt-set digest string was
// non-empty, recomputed the runtime projections from the scorer, and stopped.
// An independently written scorer could therefore claim any digest, obtain a
// signed Stage-1 binding and allocate the entire campaign without the claimed
// set's bytes ever being read.
//
// Absence is a refusal, not a skip: a set nobody supplied cannot be checked,
// and a row whose allocation surface is unattributable is ineligible.
func verifyTrainingSurface(v *Verdict, opt VerifyOptions, stage1 *Stage1Manifest, bound boundIdentities) {
	if stage1 == nil {
		return
	}
	if len(bound.trainingKeys) == 0 {
		v.add("WT-027", SeverityIneligible,
			"the Stage-1 manifest predeclares no training authority key, so the sealed training set would authenticate itself and any self-generated key could seal a lineage")
		return
	}
	if opt.TrainingSetPath == "" {
		v.add("WT-027", SeverityIneligible,
			"no sealed training receipt set was supplied; the scorer's lineage is then a digest it states about itself, and its coefficients are attributable to no admissible evidence")
		return
	}
	if opt.ScorerPath == "" {
		v.add("WT-027", SeverityIneligible,
			"no frozen scorer was supplied, so the sealed training set cannot be refitted and compared to the model that allocated this run")
		return
	}
	var set TrainingReceiptSet
	if err := ReadJSONFile(opt.TrainingSetPath, &set); err != nil {
		v.add("WT-027", SeverityIneligible, fmt.Sprintf("sealed training receipt set: %v", err))
		return
	}
	var sc Scorer
	if err := ReadJSONFile(opt.ScorerPath, &sc); err != nil {
		v.add("WT-027", SeverityIneligible, fmt.Sprintf("frozen scorer: %v", err))
		return
	}
	for _, p := range VerifyTrainingSurface(set, bound.lineage, sc, bound.trainingKeys) {
		v.add("WT-027", SeverityIneligible, "the training surface does not independently verify: "+p)
	}
}

// verifyReplay requires an INDEPENDENT re-derivation of the plan.
//
// The Stage-2 receipt is the planner's own account of what it produced.
// Checking it against itself is not verification, and the contract is explicit
// that an independent verifier must rerun the frozen parsers over the frozen
// bytes and reject a changed plan before AT_start. A run with no such
// attestation is complete and unscorable, which is the honest answer.
func verifyReplay(v *Verdict, opt VerifyOptions, stage1 *Stage1Manifest, stage2 *Stage2Receipt, bound boundIdentities) {
	if opt.ReplayPath == "" {
		v.add("WT-018", SeverityIneligible,
			"no independent Stage-2 replay attestation was supplied; the records are bound to a plan receipt nobody re-derived")
		return
	}
	var a ReplayAttestation
	if err := ReadJSONFile(opt.ReplayPath, &a); err != nil {
		v.add("WT-018", SeverityIneligible, fmt.Sprintf("replay attestation: %v", err))
		return
	}
	if stage2 == nil {
		v.add("WT-018", SeverityIneligible, "a replay attestation was supplied with no Stage-2 receipt to check it against")
		return
	}
	var instr InstrumentationIdentity
	if stage1 != nil {
		instr = stage1.Instrumentation
	}
	// The verifier identity the MEASURED RECORDS carry. Without it the replay
	// document is checked in isolation and never joined to the row it attests.
	for _, p := range a.Verify(*stage2, bound.stage2, bound.stage1, instr, v.Run.VerifierID) {
		v.add("WT-018", SeverityIneligible, "the independent replay does not attest this plan: "+p)
	}
	// The attestation is a claim by a party; it has to be signed by one the
	// campaign declared, for the same reason the manifest does.
	if a.Signature == nil {
		v.add("WT-018", SeverityIneligible, "the replay attestation is unsigned")
		return
	}
	d, err := a.DigestOf()
	if err != nil {
		v.add("WT-018", SeverityIneligible, fmt.Sprintf("replay attestation: %v", err))
		return
	}
	// The replay signer set is DISTINCT from the authority's. An attestation
	// accepted from the key that authorised the plan is the planner checking
	// its own work — the exact thing an independent replay exists to rule out
	// — so a manifest that declares no separate replay signer leaves the
	// independence claim unenforced, and the honest answer is unscorable.
	replayKeys := bound.replaySigners
	if len(replayKeys) == 0 {
		v.add("WT-018", SeverityIneligible,
			"the Stage-1 manifest declares no independent replay signer, so the attestation would be accepted from the same party that authorised the plan and its independence is an assertion rather than a control")
		return
	}
	for _, rk := range replayKeys {
		for _, ak := range opt.AuthorityKeys {
			if rk == ak {
				v.add("WT-018", SeverityIneligible,
					fmt.Sprintf("replay signer %s is also the campaign authority key; a replay by the issuer of the plan is not an independent re-derivation", rk))
			}
		}
	}
	if err := VerifySigned(a.Signature, d, replayKeys); err != nil {
		v.add("WT-018", SeverityIneligible, fmt.Sprintf("replay attestation signature: %v", err))
	}
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
	// The EXPECTED LABEL IS REQUIRED. It used to be compared only when a
	// caller supplied one, which made omitting it a wildcard: the approved
	// keys were still enforced, but a key signs under whatever label it is
	// given, so a manifest approved by some other protected environment passed
	// whenever the caller said nothing. A verifier that cannot name the
	// environment that must have approved is not in a position to accept the
	// approval.
	if strings.TrimSpace(opt.Authority) == "" {
		v.add("WT-018", SeverityIneligible,
			"no expected protected authority was named to this verifier, so a manifest approved under any label would pass; the contract names exactly one environment that may approve Stage-1 inputs")
		return
	}
	if m.Signature.Authority != opt.Authority {
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
	// Collect the FULL digest each producer's records claim, and compare for
	// exact equality. A substring match over a truncated digest is satisfiable
	// by a prefix collision, and an identity a collision can satisfy is not an
	// identity.
	reported := map[Producer]map[Digest]bool{}
	for _, r := range recs {
		if reported[r.Producer] == nil {
			reported[r.Producer] = map[Digest]bool{}
		}
		reported[r.Producer][r.ProducerBinary] = true
	}
	for producer, digest := range want {
		if digest == "" {
			v.add("WT-018", SeverityIneligible,
				fmt.Sprintf("Stage 1 binds no binary identity for the %s, so its records cannot be tied to an approved build", producer))
			continue
		}
		for got := range reported[producer] {
			if got == "" {
				v.add("WT-018", SeverityIneligible,
					fmt.Sprintf("a %s record names no binary, so it cannot be tied to the build Stage 1 approved", producer))
				break
			}
			if got != digest {
				v.add("WT-018", SeverityIneligible,
					fmt.Sprintf("a %s record was written by binary %s, not the %s Stage 1 approved", producer, got, digest))
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

// verifyStepAttempt cross-checks the action envelope against the one GitHub
// step attempt it claims to be, and records A_GH and the bootstrap gap.
//
// The contract makes this diagnostic non-gating and still requires it: without
// it, a ledger cannot say WHICH step it measured, and the action-step time
// before AT_start — the wrapper's own installation — is invisible rather than
// merely unmeasurable.
func verifyStepAttempt(v *Verdict, opt VerifyOptions, envs []Envelope) {
	var action *Envelope
	for i := range envs {
		if envs[i].Level == LevelAction {
			action = &envs[i]
		}
	}
	if opt.StepAttemptPath == "" {
		if action != nil {
			v.add("WT-022", SeverityIneligible,
				"no GitHub step-attempt diagnostic was supplied, so the action envelope is not linked to the step that ran and the pre-AT_start bootstrap is unaccounted")
		}
		return
	}
	var doc StepAttemptDocument
	if err := ReadJSONFile(opt.StepAttemptPath, &doc); err != nil {
		v.add("WT-022", SeverityIneligible, fmt.Sprintf("step attempt: %v", err))
		return
	}
	if doc.Kind != AGHKind {
		v.add("WT-022", SeverityIneligible, fmt.Sprintf("step-attempt document kind %q, want %q", doc.Kind, AGHKind))
		return
	}
	if action == nil || !action.Physical.OK {
		v.add("WT-022", SeverityIneligible, "a step attempt was supplied but no complete action envelope was recorded")
		return
	}
	for _, p := range doc.Attempt.CheckIdentity(v.Run, action.Physical.start, action.Physical.end) {
		v.add("WT-022", SeverityIneligible, "step-attempt identity: "+p)
	}
	if ns, err := doc.Attempt.ElapsedNs(); err == nil {
		v.ActionGHNs = ns
	}
	if gap, err := doc.Attempt.BootstrapGapNs(action.Physical.start); err == nil {
		v.BootstrapGapNs = gap
		checkBootstrapGap(v, gap)
	}
}

// verifyInvocationIdentity compares every measured invocation against what the
// authorised plan rendered: the argv, the working directory, the selector, the
// unit membership and the atom closure.
//
// This is the check that makes the atom and slice controls MEASURED rather
// than merely planned. Two legal name slices of one file share a description
// and differ in their selector and unit membership; without this comparison a
// run could measure one and report the other, and every planner-side test
// would still be green.
func verifyInvocationIdentity(v *Verdict, opt VerifyOptions, envs []Envelope, stage2 Digest) {
	var measured []Envelope
	for _, e := range envs {
		if e.Level == LevelInvocation {
			measured = append(measured, e)
		}
	}
	if opt.InvocationsPath == "" {
		if len(measured) > 0 {
			v.add("WT-021", SeverityIneligible,
				"no invocation manifest was supplied, so the measured argv, selector, unit membership and atom closure are not checked against the authorised plan")
		}
		return
	}
	var m InvocationManifest
	if err := ReadJSONFile(opt.InvocationsPath, &m); err != nil {
		v.add("WT-021", SeverityIneligible, fmt.Sprintf("invocation manifest: %v", err))
		return
	}
	if m.Kind != InvocationManifestKind {
		v.add("WT-021", SeverityIneligible, fmt.Sprintf("invocation manifest kind %q, want %q", m.Kind, InvocationManifestKind))
		return
	}
	if stage2 != "" && m.Stage2 != stage2 {
		v.add("WT-021", SeverityIneligible,
			fmt.Sprintf("the invocation manifest names Stage-2 %s, not the verified %s", m.Stage2, stage2))
	}
	checkBucket(v, "invocation manifest", m.BucketName)
	checkSidecarBinding(v, opt, SidecarInvocations, m.BucketIndex, m)
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
	checkBucket(v, "pre-action forecast", a.BucketID)
	checkSidecarBinding(v, opt, SidecarAeta, a.Inputs.BucketIndex, a)
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
		v.Gates = append(v.Gates, predictorGates(v, opt, bound.planStage2, bound.membership, bound.scorer)...)
	} else {
		v.Gates = append(v.Gates, GateResult{
			Name: "predictor:invocation-max", Scope: ScopeRow, Required: "<= " + dur(PcheckInvocationMaxLimit),
			Observed: "no projection", Detail: "no frozen Pcheck projection was supplied",
		})
	}
	if aeta != nil {
		// expected is 1 because this is a ROW: the individual-error, interval
		// and width rules are decided here, and the population mean is
		// reported campaign-scope, where it belongs. The sample is RETAINED so
		// the campaign can compute that mean over all eighty rows — without it
		// eighty individually acceptable rows could still miss the aggregate
		// and nothing would notice.
		sample := aeta.Sample(v.ActionNs)
		v.AetaSample = &sample
		v.Gates = append(v.Gates, EvaluateAeta([]AetaSample{sample}, 1)...)
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
	// Recomputed against the FROZEN SCORER, not only against itself. Checking
	// a projection's own arithmetic catches an edited number; it cannot catch
	// a Palloc map that no scorer ever produced, because such a map is
	// perfectly self-consistent. The runtime surface is defined as
	// frozen_scorer(frozen_preplan_unit_feature_vector), so the verifier runs
	// exactly that and compares.
	if opt.ScorerPath == "" {
		v.add("WT-019", SeverityIneligible,
			"no frozen scorer was supplied, so the Pcheck projection can only be checked against its own arithmetic and its values are unattributable to any model")
		for _, p := range doc.Recompute() {
			v.add("WT-019", SeverityIneligible, "the Pcheck projection does not recompute: "+p)
		}
	} else {
		var sc Scorer
		if err := ReadJSONFile(opt.ScorerPath, &sc); err != nil {
			v.add("WT-019", SeverityIneligible, fmt.Sprintf("frozen scorer: %v", err))
		} else {
			if sc.Kind != ScorerKind {
				v.add("WT-019", SeverityIneligible, fmt.Sprintf("frozen scorer kind %q, want %q", sc.Kind, ScorerKind))
			}
			// The scorer's own lineage must name a sealed receipt set, or the
			// model is attributable to nothing on the offline side either.
			if sc.Lineage.ReceiptSetDigest == "" {
				v.add("WT-019", SeverityIneligible,
					"the frozen scorer names no sealed training receipt set, so its coefficients have no admissible lineage")
			}
			for _, p := range doc.RecomputeFrom(sc) {
				v.add("WT-019", SeverityIneligible, "the Pcheck projection does not recompute: "+p)
			}
		}
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
	checkBucket(v, "Pcheck projection", doc.BucketName)
	checkSidecarBinding(v, opt, SidecarPcheck, doc.BucketIndex, doc)
	for _, inv := range doc.Invocations {
		if inv.BucketIndex != doc.BucketIndex {
			v.add("WT-025", SeverityIneligible,
				fmt.Sprintf("the Pcheck projection is for bucket %d but its invocation %d is for bucket %d",
					doc.BucketIndex, inv.Seq, inv.BucketIndex))
			break
		}
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
	v.PredictorSample = samples
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

// DigestOf is the verdict's canonical identity, taken over everything except
// the signature that covers it. A campaign names its rows by this digest, so
// an index cannot cite one verdict file and score another.
func (v Verdict) DigestOf() (Digest, error) {
	c := v
	c.Signature = nil
	return DigestJSON(c)
}

// Sign attaches the verifier's statement. A campaign will not count a row
// without one: a verdict is a claim, and an unsigned claim names nobody.
func (v *Verdict) Sign(authority string, key ed25519.PrivateKey) error {
	d, err := v.DigestOf()
	if err != nil {
		return err
	}
	v.Signature = &Signature{
		Authority: authority, KeyID: PublicKeyOf(key), Digest: d, Value: SignApproval(authority, key, d),
	}
	return nil
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// checkBucket ties a per-bucket document to the bucket that was measured.
//
// Every other identity two buckets of one plan carry is the same: one Stage-2
// receipt, one membership digest, one scorer, one registry. A forecast or a
// projection built for bucket 3 therefore recomputes perfectly against its own
// frozen values inside bucket 1's job, and applies the wrong prediction to the
// wrong work. Artifact naming in a workflow is a convention; this is the
// control.
func checkBucket(v *Verdict, what, named string) {
	if v.Run.BucketID == "" {
		v.add("WT-025", SeverityIneligible,
			fmt.Sprintf("the records name no bucket, so the %s cannot be tied to the bucket that was measured", what))
		return
	}
	if named == "" {
		v.add("WT-025", SeverityIneligible,
			fmt.Sprintf("the %s names no bucket, so it would verify identically against any bucket of this plan", what))
		return
	}
	if named != v.Run.BucketID {
		v.add("WT-025", SeverityIneligible,
			fmt.Sprintf("the %s is for bucket %q but bucket %q was measured", what, named, v.Run.BucketID))
	}
}

// BootstrapGapResolution is A_GH's own reporting resolution: GitHub reports
// step timestamps in whole SECONDS, so a gap up to one tick is
// indistinguishable from zero and cannot be attributed to anything.
const BootstrapGapResolution = int64(second)

// checkBootstrapGap REPORTS action-step time before AT_start. It never changes
// the verdict.
//
// The acceptance contract is explicit: A_GH is "an integer-second
// identity/sanity diagnostic only and never enters balance, non-regression,
// prediction, or success calculation". Eligibility is success calculation —
// a campaign's population is assembled from eligible rows — so a finding that
// makes a row ineligible on the strength of a whole-second GitHub timestamp
// is A_GH entering the result, whatever the finding is called.
//
// An earlier revision gated this. That was wrong, and it is stated here rather
// than quietly reverted: the underlying concern is real — the contract also
// requires A to begin at the first action-owned operation, and nothing in the
// physical ledger can see before AT_start — but bounding the prefix by A_GH is
// not an available remedy, and making it one is an acceptance-contract
// decision rather than an implementation choice. The structural control
// remains: under measurement the wrapper is installed by the caller and
// `wall begin` is the action's first step, so there is no action-owned work
// left to precede the envelope.
func checkBootstrapGap(v *Verdict, gap int64) {
	if gap < 0 {
		v.add("WT-026", SeverityNote,
			fmt.Sprintf("diagnostic: AT_start precedes the start GitHub reports for the step by %s. A_GH is whole-second, so this is reported and not scored", dur(-gap)))
		return
	}
	if gap > BootstrapGapResolution {
		v.add("WT-026", SeverityNote,
			fmt.Sprintf("diagnostic: %s of action-step time precedes AT_start, beyond A_GH's own %s resolution. Under measurement the wrapper is installed by the caller and `wall begin` is the action's first step, so this should be runner startup; A_GH never enters a gate, so it is reported and not scored",
				dur(gap), dur(BootstrapGapResolution)))
	}
}

// checkSidecarBinding requires a derived per-bucket document to be the one the
// Stage-2 receipt bound.
//
// A document that merely names a Stage-2 digest proves only that its author
// knew the digest, which is public. The receipt is signed and independently
// replayed, so binding the document's digest INTO it is what makes the
// per-bucket handoff part of the frozen plan rather than a file travelling
// beside it.
func checkSidecarBinding(v *Verdict, opt VerifyOptions, kind string, bucket int, doc any) {
	if opt.Stage2Path == "" {
		return // already reported: without a receipt there is nothing to bind to
	}
	var r Stage2Receipt
	if err := ReadJSONFile(opt.Stage2Path, &r); err != nil {
		return // already reported by verifyStageBinding
	}
	if err := r.checkSidecar(kind, bucket, doc); err != nil {
		v.add("WT-021", SeverityIneligible, err.Error())
	}
}

// deriveOutcome reads the row's start instant and terminal state off the
// ACTION ENVELOPE'S OWN RECORDS, so a campaign never has to take either on
// assertion.
//
// The start is the beginning of the realtime bracket the wrapper observed
// around its AT_start monotonic reading. That bracket is wrapper-produced,
// hash-chained, signed and sealed — unlike the campaign index, which is a
// plain file. It is deliberately NOT the GitHub step's timestamp: the contract
// forbids A_GH from entering a success calculation, and the three-date and
// fourteen-day rules decide campaign membership.
func deriveOutcome(v *Verdict, envs []Envelope) {
	for _, e := range envs {
		if e.Level != LevelAction {
			continue
		}
		v.Terminal = e.Terminal
		if e.Physical.OK || e.Physical.start.Instant.Realtime != "" {
			if before, _, err := e.Physical.start.Instant.RealtimeBracket(); err == nil {
				v.StartedAt = before.UTC().Format(time.RFC3339Nano)
			}
		}
		return
	}
}

// verifyPrePlanApproval checks that the plan was authorised BEFORE it existed.
//
// The Stage-2 receipt names the Stage-1 manifest by digest, and that digest
// excludes the detached signature — correctly, but it means an unsigned
// manifest and the same manifest signed afterwards are indistinguishable by
// digest alone. A plan derived from an unauthorised manifest would then carry
// a Stage-1 reference that a later signature appears to satisfy.
//
// So the planner records the approval it saw, the independent replay
// re-derives it, and this compares that record to the signature the manifest
// actually carries now. A receipt with no approval is a plan that ran before
// anyone approved its inputs; a receipt whose approval names a different key
// or authority is a plan approved by someone else.
func verifyPrePlanApproval(v *Verdict, stage1 Stage1Manifest, stage2 Stage2Receipt) {
	if stage2.Stage1Approval == (Stage1Approval{}) {
		v.add("WT-018", SeverityIneligible,
			"the Stage-2 receipt records no pre-plan authority approval, so the plan may have been derived before anyone approved its inputs; a signature added afterwards leaves the Stage-1 digest unchanged and cannot restore an approval that did not happen")
		return
	}
	got, err := ApprovalOf(stage1)
	if err != nil {
		v.add("WT-018", SeverityIneligible,
			fmt.Sprintf("the Stage-2 receipt records a pre-plan approval but %v", err))
		return
	}
	if got != stage2.Stage1Approval {
		v.add("WT-018", SeverityIneligible,
			fmt.Sprintf("the plan was approved by %s/%s but the supplied Stage-1 manifest is signed by %s/%s",
				stage2.Stage1Approval.Authority, stage2.Stage1Approval.KeyID, got.Authority, got.KeyID))
	}
}
