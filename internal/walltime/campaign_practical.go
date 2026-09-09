package walltime

import (
	"fmt"
	"sort"
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
// est_basis is deliberately NOT a member: it is the treatment. Everything else
// the registry places in `bc_inv` is held equal, and the campaign validator
// rejects a pair in which any one of them differs.
type BCInvariantTuple struct {
	OrchestrationCommit    string
	WorkloadCommit         string
	CandidateSHA           string
	ExpandedUnitSetDigest  string
	ComparabilityKeyDigest string
	StoreSHA256            string
	K                      int
	Count                  int
	FileParallelism        int
	RunnerClass            string
	RunsOnLabel            string
	// The cache subset is the bc_inv DECLARATION leaves, never matched_key or
	// disposition — those are per-job outcomes (S-5/R9-D4).
	DependencyCacheMode       string
	DependencyCachePrimaryKey string
	DependencyCacheProducer   string
	TransformCacheMode        string
	ExpectedMongoBinarySHA256 string
}

// bcInvariantLeaves enumerates the tuple's scalar leaves by name, so the
// mutation matrix can be generated by walking the membership rather than from a
// literal list. No test carries a field count.
func bcInvariantLeaves(t BCInvariantTuple) map[string]any {
	return map[string]any{
		"orchestration_commit":         t.OrchestrationCommit,
		"workload_commit":              t.WorkloadCommit,
		"candidate_sha":                t.CandidateSHA,
		"expanded_unit_set_digest":     t.ExpandedUnitSetDigest,
		"comparability_key_digest":     t.ComparabilityKeyDigest,
		"store_sha256":                 t.StoreSHA256,
		"k":                            t.K,
		"count":                        t.Count,
		"file_parallelism":             t.FileParallelism,
		"runner_class":                 t.RunnerClass,
		"runs_on_label":                t.RunsOnLabel,
		"dependency_cache_mode":        t.DependencyCacheMode,
		"dependency_cache_primary_key": t.DependencyCachePrimaryKey,
		"dependency_cache_producer":    t.DependencyCacheProducer,
		"transform_cache_mode":         t.TransformCacheMode,
		"expected_mongo_binary_sha256": t.ExpectedMongoBinarySHA256,
	}
}

// BCInvariantLeafNames returns the membership, enumerated by recursion over the
// tuple rather than by a written list.
func BCInvariantLeafNames() []string {
	var out []string
	for k := range bcInvariantLeaves(BCInvariantTuple{}) {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ValidatePairInvariant rejects a pair in which any BC-INV leaf differs. It
// names the first differing leaf, so a rejected pair is diagnosable.
func ValidatePairInvariant(armB, armC BCInvariantTuple) error {
	lb, lc := bcInvariantLeaves(armB), bcInvariantLeaves(armC)
	for _, name := range BCInvariantLeafNames() {
		if lb[name] != lc[name] {
			return fmt.Errorf("§19.2: BC-INV leaf %q differs across the arms (%v vs %v); the treatment is the planner mode and nothing else",
				name, lb[name], lc[name])
		}
	}
	return nil
}
