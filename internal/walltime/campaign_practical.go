package walltime

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Campaign population constants of contract §10.4 that gates.go does not
// already carry. None may be chosen, tuned or adjusted from pilot or campaign
// outcomes, and CampaignPairs / BucketsPerRun / CampaignDates / CampaignWindow
// are reused from gates.go rather than restated here.
const (
	// CampaignRuns is pairs × arms.
	CampaignRuns = 10
	// CampaignRowsPerArm is the per-arm half of §10.4's "80 total = 40 per
	// arm, never 80 per arm" — the multiplication error §16.4 pins.
	CampaignRowsPerArm = 40
	// MaxRescheduleEvents is §22 test 40's bound: two permitted void/reschedule
	// events, and a third ends the campaign.
	MaxRescheduleEvents = 2
)

// Arm names the two planner modes a pair compares. B and C are THE SAME BINARY
// in two planner modes; the treatment is the mode, not an implementation tuple.
type Arm string

const (
	ArmB Arm = "B"
	ArmC Arm = "C"
)

// Direction is the within-pair order a pair runs in.
type Direction struct{ First, Second Arm }

// CounterbalancedSequence is contract §0.2/§19.5's FIXED PRECOMMITTED sequence,
// declared in the manifest before run 1: pairs 1, 3, 5 run B→C; pairs 2, 4 run
// C→B.
//
// Counterbalancing alternates which arm goes first so ordering and drift within
// a pair do not sit systematically on one arm. It is a CONTROL, not a draw: the
// campaign is not randomized, and no seed, shuffle or reproducible draw appears
// anywhere in this path.
func CounterbalancedSequence() []Direction {
	out := make([]Direction, CampaignPairs)
	for i := range out {
		if (i+1)%2 == 1 { // pairs 1, 3, 5
			out[i] = Direction{First: ArmB, Second: ArmC}
			continue
		}
		out[i] = Direction{First: ArmC, Second: ArmB} // pairs 2, 4
	}
	return out
}

// ArmRun is one authenticated arm execution as the Actions API reports it.
type ArmRun struct {
	Pair        int
	Arm         Arm
	RunID       string
	RunAttempt  int
	StartedAt   string // authenticated, RFC 3339
	CompletedAt string // authenticated, RFC 3339
	// StartedBucketScript records whether either arm reached a bucket script,
	// which is the pre-start/post-start boundary §19.8 turns on.
	StartedBucketScript bool
	// Disposition is the manifest's account of this attempt.
	Disposition string
}

// Attempt dispositions of §19.4a and §19.8.
const (
	DispositionScored       = "scored"
	DispositionVoidPreStart = "void-pre-start"
	DispositionRescheduled  = "rescheduled"
	DispositionAbandoned    = "abandoned"
	// DispositionRetainedUnscored is the POST-START outcome: retained in the
	// attempted population, excluded from scored and training, marking its
	// fixed pair non-passing. It is never a void and never rescheduled.
	DispositionRetainedUnscored = "retained-unscored"
)

// CampaignAttempts is the attempts document of §19.9b: every Actions workflow
// attempt in the window, with its disposition.
type CampaignAttempts struct {
	Runs []ArmRun
}

// ScoredVsAttempted is contract §19.4a's two populations, never conflated.
// Counting them as one number was the defect this type exists to prevent, and
// a reader is never asked to reconcile them.
type ScoredVsAttempted struct {
	Scored    int
	Attempted int
	// ScoredRuns are the runs whose starts the three-date and 14-day gates
	// read — the scored arm-runs, never every attempt.
	ScoredRuns []ArmRun
}

// Populations partitions the attempts. The scored population is §10.4's fixed
// campaign population and nothing else; the attempted population is every
// attempt in the window, used for audit accounting and manifest completeness
// only and for no threshold or gate.
func (a CampaignAttempts) Populations() ScoredVsAttempted {
	out := ScoredVsAttempted{Attempted: len(a.Runs)}
	for _, r := range a.Runs {
		if r.Disposition == DispositionScored {
			out.Scored++
			out.ScoredRuns = append(out.ScoredRuns, r)
		}
	}
	return out
}

// ValidateITT enforces §19.8's intention-to-treat rules and §19.4a's accounting.
//
// Only platform failures BEFORE either arm starts a bucket script are voidable
// and reschedulable. Post-start outcomes are retained in the attempted
// population, excluded from scored and training, mark their fixed pair
// non-passing, and cannot replace or reschedule an attempt.
func (a CampaignAttempts) ValidateITT() error {
	reschedules := 0
	byPair := map[int][]ArmRun{}
	for _, r := range a.Runs {
		byPair[r.Pair] = append(byPair[r.Pair], r)

		switch r.Disposition {
		case DispositionVoidPreStart:
			if r.StartedBucketScript {
				return fmt.Errorf("ITT: run %s is marked void-pre-start but started a bucket script; a post-start outcome is never a void (§19.8)",
					r.RunID)
			}
		case DispositionRescheduled:
			if r.StartedBucketScript {
				return fmt.Errorf("ITT: run %s is marked rescheduled but started a bucket script; a post-start outcome is never rescheduled",
					r.RunID)
			}
			reschedules++
		case DispositionRetainedUnscored:
			if !r.StartedBucketScript {
				return fmt.Errorf("ITT: run %s is marked retained-unscored but never started a bucket script; that is a pre-start void",
					r.RunID)
			}
		case DispositionScored, DispositionAbandoned:
		default:
			return fmt.Errorf("ITT: run %s carries unknown disposition %q", r.RunID, r.Disposition)
		}
	}

	if reschedules > MaxRescheduleEvents {
		return fmt.Errorf("ITT: %d void/reschedule events, above the permitted %d; the third ends the campaign",
			reschedules, MaxRescheduleEvents)
	}

	// run_attempt > 1 never erases or replaces attempt 1: both appear.
	for pair, runs := range byPair {
		seen := map[string]map[int]bool{}
		for _, r := range runs {
			if seen[r.RunID] == nil {
				seen[r.RunID] = map[int]bool{}
			}
			seen[r.RunID][r.RunAttempt] = true
		}
		for runID, attempts := range seen {
			if attempts[2] && !attempts[1] {
				return fmt.Errorf("ITT: pair %d run %s carries attempt 2 without attempt 1; a re-attempt never replaces the original",
					pair, runID)
			}
		}
	}
	return nil
}

// AccountForAPIRuns is §19.4a rule 4: every run the Actions API returns for the
// window must appear in the manifest, in the attempted population, with its
// disposition. A run present in the API and absent from the manifest REJECTS
// the campaign.
func (a CampaignAttempts) AccountForAPIRuns(apiRunIDs []string) error {
	inManifest := map[string]bool{}
	for _, r := range a.Runs {
		inManifest[r.RunID] = true
	}
	var missing []string
	for _, id := range apiRunIDs {
		if !inManifest[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("campaign accounting: %d Actions-API run(s) absent from the manifest: %v", len(missing), missing)
	}
	return nil
}

// SequentialOrder is contract §19.5's order verification (ID-20). It is
// SEQUENTIAL order, not launch order:
//
//	sequential: completed_at(first) ≤ started_at(second)   — the arms did not overlap
//	launch:     started_at(first)  <  started_at(second)   — which arm was dispatched first
//
// Two jobs can satisfy launch order while running CONCURRENTLY on different
// runners, which is not the sequential execution the drift-control claim rests
// on. Both instants come from the authenticated Actions API; an empty or
// unparseable instant is a FAILURE, never a skip.
func SequentialOrder(pair int, declared Direction, runs []ArmRun) error {
	var first, second *ArmRun
	for i := range runs {
		r := &runs[i]
		switch r.Arm {
		case declared.First:
			first = r
		case declared.Second:
			second = r
		}
	}
	if first == nil || second == nil {
		return fmt.Errorf("pair %d: the declared direction %s→%s is not covered by the pair's runs",
			pair, declared.First, declared.Second)
	}

	fc, err := parseAuthenticatedInstant(first.CompletedAt)
	if err != nil {
		return fmt.Errorf("pair %d arm %s completed_at: %w", pair, first.Arm, err)
	}
	ss, err := parseAuthenticatedInstant(second.StartedAt)
	if err != nil {
		return fmt.Errorf("pair %d arm %s started_at: %w", pair, second.Arm, err)
	}
	fs, err := parseAuthenticatedInstant(first.StartedAt)
	if err != nil {
		return fmt.Errorf("pair %d arm %s started_at: %w", pair, first.Arm, err)
	}

	if !fs.Before(ss) && !fs.Equal(ss) {
		return fmt.Errorf("pair %d: declared first arm %s started after second arm %s; the executed order contradicts the declared sequence",
			pair, first.Arm, second.Arm)
	}
	if fc.After(ss) {
		return fmt.Errorf("pair %d: arms OVERLAP — %s completed at %s, after %s started at %s; launch order alone does not satisfy this gate",
			pair, first.Arm, first.CompletedAt, second.Arm, second.StartedAt)
	}
	return nil
}

// parseAuthenticatedInstant reads an authenticated RFC 3339 instant. An empty or
// unparseable value is a failure, not a skip — §17.18 is explicit that the
// earlier "skip empty instants" behaviour is withdrawn.
func parseAuthenticatedInstant(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("instant is empty; an empty instant is a failure, never a skip")
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("instant %q is unparseable: %w", s, err)
	}
	return t.UTC(), nil
}

// AuthenticatedDates is §19.5/§17.14: each scheduled date is compared against
// the arm's AUTHENTICATED run date from the Actions API, parsed as YYYY-MM-DD
// in UTC. A scheduled date matching no authenticated run date fails.
//
// realtime_* is never a campaign date source; only these instants are.
func AuthenticatedDates(scheduled []string, scoredRuns []ArmRun) error {
	authenticated := map[string]bool{}
	var days []time.Time
	for _, r := range scoredRuns {
		t, err := parseAuthenticatedInstant(r.StartedAt)
		if err != nil {
			return fmt.Errorf("run %s: %w", r.RunID, err)
		}
		authenticated[t.Format("2006-01-02")] = true
		days = append(days, t)
	}
	for _, s := range scheduled {
		if !authenticated[s] {
			return fmt.Errorf("scheduled date %s matches no authenticated run date", s)
		}
	}
	if len(authenticated) < CampaignDates {
		return fmt.Errorf("campaign spans %d distinct authenticated UTC dates, below the required %d",
			len(authenticated), CampaignDates)
	}
	if len(days) > 0 {
		sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
		span := days[len(days)-1].Sub(days[0])
		if span > CampaignWindow {
			return fmt.Errorf("campaign window spans %v, above the permitted %v", span, CampaignWindow)
		}
	}
	return nil
}

// BCInvariantTuple is the bound treatment of contract §19.2 (ID-3): the values
// that must be IDENTICAL across the two arms of a pair, so the only observable
// difference is the planner mode.
//
// It is a MAP KEYED BY REGISTRY PATH rather than a struct of named fields, and
// that is deliberate. §19.2 compares exactly the leaves the field registry
// places in `memberships.bc_inv`, and §22 test 66 generates its mutation
// matrix by walking that membership and recursing into `tuple_leaves`. A struct
// would make the Go type the authority and let the registry grow past it
// silently — which is what happened: a first cut modelled sixteen leaves
// against a membership carrying thirty-four, and the missing eighteen would
// have been compared by nothing.
//
// est_basis is deliberately NOT a member: it is the treatment.
type BCInvariantTuple struct {
	// Leaves maps each bc_inv registry path to its value for one arm.
	Leaves map[string]string
}

// NewBCInvariantTuple builds a tuple over exactly the supplied membership,
// refusing any path the membership does not contain and any member the caller
// left out. Both directions matter: an extra leaf would be compared without
// authority, and a missing one would not be compared at all.
func NewBCInvariantTuple(membership []string, values map[string]string) (BCInvariantTuple, error) {
	want := map[string]bool{}
	for _, m := range membership {
		want[m] = true
	}
	for k := range values {
		if !want[k] {
			return BCInvariantTuple{}, fmt.Errorf("§19.2: %q is not a bc_inv member; the registry is the authority for the membership", k)
		}
	}
	var missing []string
	for m := range want {
		if _, ok := values[m]; !ok {
			missing = append(missing, m)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return BCInvariantTuple{}, fmt.Errorf("§19.2: bc_inv member(s) %v carry no value; every member is compared", missing)
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = v
	}
	return BCInvariantTuple{Leaves: out}, nil
}

// BCInvariantLeafNames returns the tuple's leaves in a deterministic order.
func (t BCInvariantTuple) BCInvariantLeafNames() []string {
	var out []string
	for k := range t.Leaves {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ExpandMembership resolves a bc_inv membership against its tuple_leaves,
// replacing every container by its leaves. §22 test 66 walks the EXPANDED
// membership, so a container compared as one opaque value would hide a
// difference in any single leaf.
func ExpandMembership(membership []string, tupleLeaves map[string][]string) []string {
	var out []string
	for _, m := range membership {
		if leaves, ok := tupleLeaves[m]; ok {
			out = append(out, leaves...)
			continue
		}
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// ValidatePairInvariant rejects a pair in which any BC-INV leaf differs. It
// names the first differing leaf, so a rejected pair is diagnosable.
//
// It also refuses two arms whose membership differs at all: comparing only the
// intersection would let an arm drop a leaf and pass.
func ValidatePairInvariant(armB, armC BCInvariantTuple) error {
	nb, nc := armB.BCInvariantLeafNames(), armC.BCInvariantLeafNames()
	if len(nb) != len(nc) {
		return fmt.Errorf("§19.2: the arms carry %d and %d bc_inv leaves; the membership is the same for both", len(nb), len(nc))
	}
	for i := range nb {
		if nb[i] != nc[i] {
			return fmt.Errorf("§19.2: the arms disagree on the bc_inv membership at %q vs %q", nb[i], nc[i])
		}
	}
	for _, name := range nb {
		if armB.Leaves[name] != armC.Leaves[name] {
			return fmt.Errorf("§19.2: BC-INV leaf %q differs across the arms (%q vs %q); the treatment is the planner mode and nothing else",
				name, armB.Leaves[name], armC.Leaves[name])
		}
	}
	return nil
}

// ArmRunProfile is the §19.4 exact profile of one arm-run: the complete planned
// bucket set, matched by identity against the plan document.
//
// Eight DISTINCT rows are not sufficient. They must be the complete planned
// set, so a K > 8 plan cannot contribute a favourable eight — which is exactly
// what defaults, an eight-row denominator and cross-arm equality fail to give.
type ArmRunProfile struct {
	RunnerToken           string
	K                     int
	Count                 int
	FileParallelism       int
	PlanDigest            string
	ExpandedUnitSetDigest string
	// Rows is one authenticated terminal row per bucket identity.
	Rows []ArmRunRow
}

// ArmRunRow is one bucket's identity and terminal state within an arm-run.
type ArmRunRow struct {
	BucketIndex int
	BucketName  string
	Terminal    string
	PlanDigest  string
}

// ValidateArmRunProfile enforces §19.4 against the complete planned bucket set.
func ValidateArmRunProfile(p ArmRunProfile, plannedNames map[int]string) error {
	if p.RunnerToken != "vitest" {
		return fmt.Errorf("§19.4: runner_token is %q, must be vitest", p.RunnerToken)
	}
	if p.K != BucketsPerRun {
		return fmt.Errorf("§19.4: K is %d, must be %d", p.K, BucketsPerRun)
	}
	if p.Count != 1 {
		return fmt.Errorf("§19.4: count is %d, must be 1", p.Count)
	}
	if p.FileParallelism != 1 {
		return fmt.Errorf("§19.4: file_parallelism is %d, must be 1", p.FileParallelism)
	}
	if len(plannedNames) != BucketsPerRun {
		return fmt.Errorf("§19.4: the plan declares %d buckets, not %d; a K > 8 plan cannot contribute a favourable eight",
			len(plannedNames), BucketsPerRun)
	}

	seen := map[int]bool{}
	for _, r := range p.Rows {
		want, ok := plannedNames[r.BucketIndex]
		if !ok {
			return fmt.Errorf("§19.4: bucket index %d is not in the eight-bucket plan", r.BucketIndex)
		}
		if r.BucketName != want {
			return fmt.Errorf("§19.4: bucket %d is named %q, the plan names it %q",
				r.BucketIndex, r.BucketName, want)
		}
		if seen[r.BucketIndex] {
			return fmt.Errorf("§19.4: bucket index %d appears twice", r.BucketIndex)
		}
		seen[r.BucketIndex] = true
		if r.Terminal != "passed" {
			return fmt.Errorf("§19.4: bucket %d terminal is %q; every row must be authenticated terminal", r.BucketIndex, r.Terminal)
		}
		// plan_digest identical across all eight observations of one arm-run.
		if r.PlanDigest != p.PlanDigest {
			return fmt.Errorf("§19.4: bucket %d carries plan_digest %q, the arm-run's is %q",
				r.BucketIndex, r.PlanDigest, p.PlanDigest)
		}
	}
	// The COMPLETE planned set, not merely eight distinct rows.
	for idx := range plannedNames {
		if !seen[idx] {
			return fmt.Errorf("§19.4: bucket index %d of the plan has no row; the set must be complete", idx)
		}
	}
	return nil
}

// ValidatePairProfile checks the two arms of one pair against §19.4 and §19.2:
// expanded_unit_set_digest is identical across both arms, so unit topology is
// not part of the treatment.
func ValidatePairProfile(armB, armC ArmRunProfile, plannedNames map[int]string) error {
	if err := ValidateArmRunProfile(armB, plannedNames); err != nil {
		return fmt.Errorf("arm B: %w", err)
	}
	if err := ValidateArmRunProfile(armC, plannedNames); err != nil {
		return fmt.Errorf("arm C: %w", err)
	}
	if armB.ExpandedUnitSetDigest != armC.ExpandedUnitSetDigest {
		return fmt.Errorf("§19.2: expanded_unit_set_digest differs across the arms (%q vs %q); unit topology is not part of the treatment",
			armB.ExpandedUnitSetDigest, armC.ExpandedUnitSetDigest)
	}
	return nil
}

// CampaignProvenance is §19.5's model-freeze and exclusion evidence.
type CampaignProvenance struct {
	// FittedAt is when the deployed model was fitted. It must PRECEDE the
	// first authenticated campaign start.
	FittedAt string
	// FirstAuthenticatedStart is the earliest scored arm-run start.
	FirstAuthenticatedStart string
	// ExcludedRunIDs and the window are operator-side predicates: they are
	// evaluated HERE and never in CI, and a disagreement with the rows'
	// stored `trainable` marking fails the campaign.
	ExcludedRunIDs      []string
	ExcludedWindowStart string
	ExcludedWindowEnd   string
	// ExclusionDomains names each exclusion domain. An unnamed domain fails.
	ExclusionDomains []string
}

// ValidateProvenance is §19.5/§17.14's cutoff and exclusion check.
func ValidateProvenance(p CampaignProvenance, ringRunIDs []string, ringStarts map[string]string) error {
	fitted, err := parseAuthenticatedInstant(p.FittedAt)
	if err != nil {
		return fmt.Errorf("fitted_at: %w", err)
	}
	first, err := parseAuthenticatedInstant(p.FirstAuthenticatedStart)
	if err != nil {
		return fmt.Errorf("first authenticated start: %w", err)
	}
	// The model is frozen BEFORE the first authenticated campaign start.
	if !fitted.Before(first) {
		return fmt.Errorf("§19.5: fitted_at %s does not precede the first authenticated start %s; the model must be frozen before the campaign begins",
			p.FittedAt, p.FirstAuthenticatedStart)
	}
	// Every exclusion domain must be NAMED; a missing one fails rather than
	// being skipped (§7 rule 10).
	if len(p.ExclusionDomains) == 0 {
		return fmt.Errorf("§19.5: no exclusion domain is named; a missing mandatory campaign field fails the campaign")
	}
	for i, d := range p.ExclusionDomains {
		if strings.TrimSpace(d) == "" {
			return fmt.Errorf("§19.5: exclusion domain %d is unnamed", i)
		}
	}
	// A ring containing a harness run_id fails.
	excluded := map[string]bool{}
	for _, id := range p.ExcludedRunIDs {
		excluded[id] = true
	}
	for _, id := range ringRunIDs {
		if excluded[id] {
			return fmt.Errorf("§19.5: the ring contains harness run_id %q, which the manifest excludes", id)
		}
	}
	// THE WINDOW IS MANDATORY, AND BOTH ENDPOINTS ARE.
	//
	// The comparison ran only `if start != "" && end != ""`, so omitting either
	// endpoint skipped the exclusion entirely and the validator returned success —
	// on inputs whose fitted_at, first authenticated start and exclusion domains
	// were all valid. §19.5's window is evidence that a named set of runs is
	// outside the population; half a window is not half the evidence, it is none.
	if p.ExcludedWindowStart == "" || p.ExcludedWindowEnd == "" {
		return fmt.Errorf("§19.5: the excluded window is %s..%s; both endpoints are required, and an absent one skips the exclusion rather than satisfying it",
			emptyAsAbsent(p.ExcludedWindowStart), emptyAsAbsent(p.ExcludedWindowEnd))
	}
	ws, err := parseAuthenticatedInstant(p.ExcludedWindowStart)
	if err != nil {
		return fmt.Errorf("excluded_window.start: %w", err)
	}
	we, err := parseAuthenticatedInstant(p.ExcludedWindowEnd)
	if err != nil {
		return fmt.Errorf("excluded_window.end: %w", err)
	}
	if we.Before(ws) {
		return fmt.Errorf("§19.5: the excluded window ends at %s, before it starts at %s",
			p.ExcludedWindowEnd, p.ExcludedWindowStart)
	}
	// EVERY RING RUN NEEDS AN AUTHENTICATED TIMESTAMP, not just the ones the
	// caller happened to supply one for.
	//
	// The loop iterated `ringStarts`, so a run present in the ring and absent from
	// that map was never compared against the window at all — the population the
	// check is about decided how much of itself got checked. §19.9 makes every
	// campaign date, cutoff and window gate read the AUTHENTICATED start, so a run
	// without one cannot be shown to be outside the window.
	for _, id := range ringRunIDs {
		stamp, ok := ringStarts[id]
		if !ok || strings.TrimSpace(stamp) == "" {
			return fmt.Errorf("§19.9: ring run %q carries no authenticated run_started_at, so it cannot be shown to be outside the excluded window",
				id)
		}
		at, err := parseAuthenticatedInstant(stamp)
		if err != nil {
			return fmt.Errorf("ring row %s run_started_at: %w", id, err)
		}
		if !at.Before(ws) && !at.After(we) {
			return fmt.Errorf("§19.5: ring row %s started at %s, inside the excluded window %s..%s",
				id, stamp, p.ExcludedWindowStart, p.ExcludedWindowEnd)
		}
	}
	return nil
}

// emptyAsAbsent renders a missing endpoint readably, so a failure distinguishes
// "absent" from "present and empty-looking".
func emptyAsAbsent(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(absent)"
	}
	return s
}
