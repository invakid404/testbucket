package core

import (
	"fmt"
	"io"
	"math"
	"sort"
	"time"

	"github.com/invakid404/testbucket/internal/runner"
)

type IngestOptions struct {
	Alpha float64
	// Token is the adapter's opaque comparability token the measured run used.
	// Weights are only comparable within one token; a change resets the store.
	Token string
	Count int
	// WhaleK is the K the split threshold is derived from: a target is a whale
	// once it alone exceeds total/K, because at that point it — not the total
	// work — sets the makespan.
	WhaleK int
	// WhaleSeconds overrides the derived threshold with an absolute one.
	WhaleSeconds float64
	// MinShardSeconds is the floor on a slice's wall time. Every extra slice is
	// a whole extra CI job paying checkout + setup + compile, so slicing a unit
	// into pieces smaller than that spends a job to save less than the job
	// costs. It is the "diminishing returns" half of the K curve, enforced per
	// unit.
	MinShardSeconds float64
	Now             time.Time
	// Live is the authoritative target set at record time, when available. It
	// is what lets `plan` report drift, and what licenses pruning rows for
	// targets that no longer exist.
	Live              []runner.LivePackage
	LiveAuthoritative bool
}

// countShardFloor is a LOWER BOUND on what one count-shard of a target costs,
// not an estimate of it.
//
// Each shard is a SEPARATE binary, so every per-binary fixed cost — init,
// TestMain, fixture construction — is paid S times over rather than divided.
// Dividing the target's wall time by S therefore describes the best case a
// count-shard could possibly achieve.
//
// The part of that fixed cost this can see is the wall time NOT attributable to
// any named runnable. The part it cannot see is fixed work that happens INSIDE
// a named test behind a sync.Once: its cost is billed to the test that happens
// to trigger it, so it looks per-iteration from here. Sharding that target six
// ways really does repeat that work six times, and no reduction over the
// timing stream alone can tell you so.
//
// The bound is used deliberately rather than apologetically: it is compared
// against the heaviest name below, and understating the count-shard cost makes
// the "name-slicing wins" branch HARDER to reach. The policy therefore errs
// toward count-sharding, which is the direction the divisibility measurement
// concluded is right.
func countShardFloor(pkgSeconds, namedSeconds float64, shards int) float64 {
	if shards < 1 {
		return pkgSeconds
	}
	fixed := pkgSeconds - namedSeconds
	if fixed < 0 {
		// Named time can exceed the target's wall time when tests run in
		// parallel inside the binary; there is no observable fixed term then.
		fixed = 0
	}
	return fixed + (pkgSeconds-fixed)/float64(shards)
}

// chooseSplitPolicy decides how a whale is harpooned, and it is the one place
// the two mechanisms are compared on equal terms.
//
// Count-sharding divides ITERATIONS: S shards of -count=base/S each, costing at
// least seconds/S (see countShardFloor for why "at least"), whatever the
// target's internal shape. Name-slicing divides the RUNNABLE LIST, so its
// makespan can never fall below the single heaviest name — pack the other 200
// tests however you like, the slice holding the dominant one still has to run
// it.
//
// So a run split is only worth choosing when that dominant name would not
// itself be slower than a count-shard.
//
// Named coverage is still required: without per-test weights for most of the
// target the slicer would be packing blind. It is just no longer sufficient.
func chooseSplitPolicy(pkgSeconds, namedSeconds, heaviestSeconds float64, namedCount, runShards, countShards int) (policy, cause, reason string) {
	// Count-sharding is only on the table if the sweep has iterations to
	// divide; name-slicing is bounded only by runShards, because disjoint name
	// sets each keep the full -count.
	countViable := countShards >= 2

	switch {
	case pkgSeconds <= 0 || runShards < 2:
		return splitCount, causeNoPicture, "no usable per-test picture"
	case namedCount < 2:
		return splitCount, causeTooFewNames, "fewer than two named runnables to slice"
	case namedSeconds/pkgSeconds < runUpgradeFraction:
		return splitCount, causeLowCoverage, fmt.Sprintf("named runnables explain only %.0f%% of the package (need %.0f%%)",
			namedSeconds/pkgSeconds*100, runUpgradeFraction*100)
	case !countViable:
		// The target IS name-divisible and count-sharding cannot divide this
		// sweep at all. Name-slicing wins by default rather than by comparison:
		// there is nothing to compare against.
		return splitRun, causeNameDivisible, fmt.Sprintf(
			"name-divisible, and a count-shard cannot divide this sweep (heaviest runnable %.1fs)", heaviestSeconds)
	}

	// The decisive comparison, made against the width count-sharding could
	// ACTUALLY use — not the width name-slicing would use.
	floor := countShardFloor(pkgSeconds, namedSeconds, countShards)
	if heaviestSeconds > floor {
		return splitCount, causeDominated, fmt.Sprintf("dominated by one runnable (%.1fs, above the %.1fs a %d-way count-shard costs at best)",
			heaviestSeconds, floor, countShards)
	}
	return splitRun, causeNameDivisible, fmt.Sprintf("name-divisible: heaviest runnable %.1fs fits under the %.1fs best case for a %d-way count-shard",
		heaviestSeconds, floor, countShards)
}

// Why a target got the policy it did. Returned alongside the prose reason so
// callers can branch on the DECISION rather than pattern-match the wording:
// count-sharding is chosen for four quite different reasons, and only one of
// them is "a single runnable dominates". Reporting the others under that
// heading would state something false about the target.
const (
	causeDominated     = "dominated"
	causeLowCoverage   = "low-named-coverage"
	causeTooFewNames   = "too-few-names"
	causeNoPicture     = "no-per-test-picture"
	causeNameDivisible = "name-divisible"
)

// runUpgradeFraction is how much of a whale's wall time must be attributable to
// named top-level runnables before `ingest` promotes it from count-sharding to
// the finer name slicing. Below it, the per-test picture is too incomplete for
// name slices to balance well and count-sharding stays the safer harpoon.
const runUpgradeFraction = 0.5

type IngestReport struct {
	Updated         []string
	New             []string
	SkippedFail     []string
	Pruned          []string
	Whales          []string
	Unflagged       []string
	TotalSeconds    float64
	Threshold       float64
	Alpha           float64
	Events          int
	Malformed       int
	Coverage        int
	CoverageFrom    string
	FlagsReset      string
	Subtests        int
	Implausible     int
	PartialCaptures []string
	Dominated       []string
}

// validate rejects settings that would silently corrupt the store rather than
// fail. An out-of-range alpha is the dangerous one: 0 makes the store stop
// learning forever and a negative one drives weights negative, and neither
// shows up as anything but a mysteriously bad split months later.
func (o IngestOptions) Validate() error {
	switch {
	case o.Count < 1:
		return fmt.Errorf("--count must be >= 1, got %d", o.Count)
	case math.IsNaN(o.Alpha) || o.Alpha <= 0 || o.Alpha > 1:
		return fmt.Errorf("--ewma must be in (0,1], got %v", o.Alpha)
	case o.WhaleK < 1:
		return fmt.Errorf("--whale-k must be >= 1, got %d", o.WhaleK)
	case math.IsNaN(o.WhaleSeconds) || math.IsInf(o.WhaleSeconds, 0) || o.WhaleSeconds < 0:
		return fmt.Errorf("--whale-seconds must be a finite value >= 0, got %v", o.WhaleSeconds)
	case math.IsNaN(o.MinShardSeconds) || math.IsInf(o.MinShardSeconds, 0) || o.MinShardSeconds < 0:
		return fmt.Errorf("--min-shard-seconds must be a finite value >= 0, got %v", o.MinShardSeconds)
	}
	return nil
}

// applyIngest merges a batch of measurements into the store and re-derives the
// split policy. It is the entire self-optimizing half of the loop: every master
// run rewrites the weights that shape the next PR's matrix.
func ApplyIngest(st *Store, sum *runner.RunSummary, opt IngestOptions) (*IngestReport, error) {
	if err := opt.Validate(); err != nil {
		return nil, err
	}
	token := opt.Token
	rep := &IngestReport{
		Alpha: opt.Alpha, Events: sum.Events, Malformed: sum.Malformed,
		Subtests: sum.Subtests, Implausible: sum.Implausible,
	}

	if st.Flags != "" && st.Flags != token {
		// Weights from a different comparability token cannot be blended.
		rep.FlagsReset = fmt.Sprintf("%s -> %s", st.Flags, token)
		st.Units = map[string]*UnitStat{}
		st.Coverage = nil
	}
	st.Flags = token
	if st.Units == nil {
		st.Units = map[string]*UnitStat{}
	}

	for _, pkg := range runner.SortedKeys(sum.PackageSeconds) {
		measured := sum.PackageSeconds[pkg]
		if sum.Failed[pkg] {
			rep.SkippedFail = append(rep.SkippedFail, pkg)
			continue
		}
		if measured <= 0 {
			continue
		}
		row := st.Units[pkg]
		if row == nil {
			row = &UnitStat{}
			st.Units[pkg] = row
			rep.New = append(rep.New, pkg)
		} else {
			rep.Updated = append(rep.Updated, pkg)
		}
		row.Seconds = ewma(row.Seconds, row.Samples, measured, opt.Alpha)
		row.Samples++
	}
	for _, pkg := range runner.SortedKeys(sum.Failed) {
		if _, ok := sum.PackageSeconds[pkg]; !ok {
			rep.SkippedFail = append(rep.SkippedFail, pkg)
		}
	}
	sort.Strings(rep.SkippedFail)
	rep.SkippedFail = runner.Dedupe(rep.SkippedFail)

	// Prune rows for targets that no longer exist, but only when the live set
	// is authoritative. Pruning off an event batch alone would delete every
	// target that simply was not part of this batch.
	if opt.LiveAuthoritative {
		liveSet := map[string]bool{}
		for _, p := range opt.Live {
			if p.HasTests {
				liveSet[p.ID] = true
			}
		}
		for _, pkg := range runner.SortedKeys(st.Units) {
			if !liveSet[pkg] {
				delete(st.Units, pkg)
				rep.Pruned = append(rep.Pruned, pkg)
			}
		}
		// NEVER-DROP for timing data: an authoritative ingest must not silently
		// empty a non-empty store. If pruning removed every row, the live set
		// matched none of the recorded (and just-measured) packages, which means
		// its identities are broken — empty or mis-keyed. Persisting the emptied
		// store would erase every timing weight and reset coverage to nothing on
		// a run that reported real results. Fail loudly, and write nothing.
		if len(rep.Pruned) > 0 && len(st.Units) == 0 {
			return nil, fmt.Errorf(
				"ingest refused: the authoritative live set matched none of the %d recorded package(s), which would empty the store and drop every timing row — the live set is almost certainly wrong (empty or mis-keyed identities); nothing was written",
				len(rep.Pruned))
		}
	}

	// Reduced over sorted keys and in integer microseconds: map iteration order
	// is randomised per process and float addition is not associative, so a
	// plain `for range st.Units` sum can land on either side of total/K for
	// byte-identical inputs — and the whale threshold derived from it decides
	// whether a target is split at all.
	measured := make([]float64, 0, len(st.Units))
	for _, pkg := range runner.SortedKeys(st.Units) {
		if row := st.Units[pkg]; row.measured() {
			measured = append(measured, row.Seconds)
		}
	}
	rep.TotalSeconds = runner.SumSeconds(measured)
	rep.Threshold = whaleThreshold(rep.TotalSeconds, opt)

	for _, pkg := range runner.SortedKeys(st.Units) {
		row := st.Units[pkg]
		// The split policy in force when this batch was CAPTURED, not the one
		// about to be derived: it says how many invocations were supposed to
		// report this target, which is what makes a batch judgeable as complete
		// or partial.
		capturedPolicy, capturedInto := row.splitPolicy(), row.SplitInto
		if !row.measured() || rep.Threshold <= 0 || row.Seconds <= rep.Threshold {
			if row.splitPolicy() != splitNone {
				rep.Unflagged = append(rep.Unflagged, pkg)
			}
			row.Split = ""
			row.SplitInto = 0
			// The recorded justification goes with the policy it justified.
			// Left behind, it would be persisted next to "policy none x0" and
			// `testbucket whales` would print a decision that no longer holds.
			row.SplitReason = ""
			// Per-test rows exist only to serve a split; drop them with it so
			// the store does not accrete a per-test index of the tree.
			row.Tests = nil
			continue
		}
		// TWO candidate widths, because the two mechanisms are bounded by
		// different things and sharing one number silently couples them.
		//
		// runShards is what a name-slice could use: K, because K slices of
		// pkg/K fit any bucket by construction, narrowed only by whether the
		// target has the wall time to make slicing worth its fixed cost.
		// Deriving it from total/K instead would make a target's policy a
		// function of the whole tree's size, so an unrelated target getting
		// slower elsewhere could flip a whale's mechanism.
		runShards := clampShards(opt.WhaleK, opt.WhaleK)
		if opt.MinShardSeconds > 0 {
			if affordable := int(row.Seconds / opt.MinShardSeconds); affordable < runShards {
				runShards = affordable
			}
		}

		// countShards is what a COUNT-shard could actually use, and it carries
		// one extra bound the run width must NOT inherit: a count-shard divides
		// ITERATIONS, so it cannot divide more finely than the sweep has
		// iterations to give. Requiring at least two per shard is the
		// count-dimension twin of MinShardSeconds — below it the rounding in
		// -count=ceil(base/S) stops being rounding and starts being
		// duplication. At -count=1, ceil(1/6) is 1, so a six-way "split" reruns
		// the whole target six times.
		//
		// A name-slice has no such limit: its slices are DISJOINT name sets,
		// each still running the full -count, so a six-way name split at
		// -count=1 neither multiplies a test nor weakens the sweep. Applying
		// the iteration cap to both widths is what an earlier version of this
		// did, and it forced perfectly name-divisible whales to run whole at
		// low counts.
		countShards := runShards
		if affordable := opt.Count / 2; affordable < countShards {
			countShards = affordable
		}

		// Fold this batch's per-test weights in before deciding whether the
		// target can be name-sliced, so a target that becomes a whale on the
		// same run it was measured can go straight to name slicing.
		//
		// A FAILED target contributes nothing here, for the same reason it
		// contributes no target weight: a run that aborted under -race or hit
		// its -timeout reports pass events only for the tests that finished
		// before it died, and blending that partial picture in would quietly
		// bias the slices toward whatever ran first.
		if fresh := sum.TestSeconds[pkg]; len(fresh) > 0 && !sum.Failed[pkg] {
			if row.Tests == nil {
				row.Tests = map[string]float64{}
			}
			for _, name := range runner.SortedKeys(fresh) {
				sec := fresh[name]
				if sec <= 0 {
					// A sub-millisecond test reports 0.00; a zero row carries no
					// weight information and is treated as unknown by the slicer
					// anyway, so storing it would only grow the store.
					continue
				}
				row.Tests[name] = ewma(row.Tests[name], boolToInt(row.Tests[name] > 0), sec, opt.Alpha)
			}
			// A test that no longer reports has been renamed or deleted; keeping
			// its weight would misdirect a future slice.
			//
			// But prune ONLY when this batch actually covered the target. A
			// weight is smoothed by EWMA and recovers from one bad run; a
			// deletion is not, so a partial capture — a missing bucket artifact,
			// a cancelled job, one slice of several that never uploaded — would
			// erase the per-test picture that name slicing depends on and
			// silently demote the target back to count-sharding on the next
			// plan.
			if batchCoveredPackage(sum.PackageRuns[pkg], capturedPolicy, capturedInto) {
				for name := range row.Tests {
					if _, ok := fresh[name]; !ok {
						delete(row.Tests, name)
					}
				}
			} else {
				rep.PartialCaptures = append(rep.PartialCaptures, fmt.Sprintf(
					"%s reported %d of %d expected invocations; per-test rows updated but not pruned",
					pkg, sum.PackageRuns[pkg], expectedRuns(capturedPolicy, capturedInto)))
			}
		}

		// Same fixed-order, integer reduction: this sum decides count-shard
		// versus name slicing, and the two are not interchangeable.
		perTest := make([]float64, 0, len(row.Tests))
		heaviestName, heaviest := "", 0.0
		for _, name := range runner.SortedKeys(row.Tests) {
			w := row.Tests[name]
			perTest = append(perTest, w)
			if w > heaviest {
				heaviestName, heaviest = name, w
			}
		}
		named := runner.SumSeconds(perTest)
		policy, cause, reason := chooseSplitPolicy(row.Seconds, named, heaviest, len(row.Tests), runShards, countShards)
		// The chosen mechanism decides which of the two widths applies.
		width := runShards
		if policy == splitCount {
			width = countShards
		}
		if width < 2 {
			// Over the relative threshold but too small for the chosen
			// mechanism to pay for itself — in wall time, or in iterations to
			// divide. Leave it whole.
			if row.splitPolicy() != splitNone {
				rep.Unflagged = append(rep.Unflagged, pkg)
			}
			row.Split = ""
			row.SplitInto = 0
			row.SplitReason = ""
			row.Tests = nil
			continue
		}
		row.Split, row.SplitInto, row.SplitReason = policy, width, reason
		// Only the dominance branch may be reported as dominance. Count is also
		// chosen when there are too few names to slice, or when named tests
		// explain too little of the target — and a 30s runnable in a 900s target
		// does not dominate anything, so filing it under that heading would
		// print a claim that is simply untrue.
		if cause == causeDominated && heaviestName != "" {
			rep.Dominated = append(rep.Dominated, fmt.Sprintf(
				"%s: %s alone is %.0f%% of the package (%.1fs of %.1fs) — a -run split cannot finish faster than it",
				pkg, heaviestName, heaviest/row.Seconds*100, heaviest, row.Seconds))
		}
		rep.Whales = append(rep.Whales, fmt.Sprintf("%s %.1fs -> split=%s x%d (%s)", pkg, row.Seconds, row.Split, row.SplitInto, row.SplitReason))
	}

	// Record the coverage snapshot `plan` diffs against.
	cov := map[string]bool{}
	src := "go-test-json"
	if opt.LiveAuthoritative {
		src = "go-list"
		for _, p := range opt.Live {
			if p.HasTests {
				cov[p.ID] = true
			}
		}
	} else {
		for pkg := range sum.PackageSeconds {
			cov[pkg] = true
		}
		for pkg := range sum.Failed {
			cov[pkg] = true
		}
	}
	st.Coverage = runner.SortedKeys(cov)
	st.CoverageSource = src
	rep.Coverage = len(st.Coverage)
	rep.CoverageFrom = src
	st.stamp(opt.Now)
	return rep, nil
}

// expectedRuns is how many target-level pass events a complete capture of this
// target should contain under the split policy that produced it: one per shard
// or slice, and one for an un-split target.
func expectedRuns(policy string, into int) int {
	if policy == splitNone || into < 2 {
		return 1
	}
	return into
}

// batchCoveredPackage reports whether the batch looks like a complete capture
// of the target. More invocations than expected is fine (a re-run, or a policy
// that shrank between plan and record); fewer is the partial case that must not
// drive deletions.
func batchCoveredPackage(runs int, policy string, into int) bool {
	return runs >= expectedRuns(policy, into)
}

// whaleThreshold is total/K — the point above which a single target alone sets
// the makespan and no value of K can help until it is split.
func whaleThreshold(total float64, opt IngestOptions) float64 {
	if opt.WhaleSeconds > 0 {
		return opt.WhaleSeconds
	}
	if opt.WhaleK <= 0 {
		return 0
	}
	return total / float64(opt.WhaleK)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (r *IngestReport) Write(out io.Writer, prefix string) error {
	ew := &errWriter{w: out}
	w := io.Writer(ew)
	_, _ = fmt.Fprintf(w, "testbucket ingest — %d events (%d unparsable lines), alpha=%.2f\n", r.Events, r.Malformed, r.Alpha)
	if r.Implausible > 0 {
		_, _ = fmt.Fprintf(w, "*** %d event(s) carried an implausible Elapsed and were discarded — the capture looks corrupt. ***\n", r.Implausible)
	}
	if r.Subtests > 0 {
		_, _ = fmt.Fprintf(w, "  subtest events seen %d (not weighed: already inside their parent's elapsed)\n", r.Subtests)
	}
	if r.FlagsReset != "" {
		_, _ = fmt.Fprintf(w, "*** FLAG SET CHANGED (%s): previous weights discarded, store cold-starts. ***\n", r.FlagsReset)
	}
	_, _ = fmt.Fprintf(w, "  packages updated   %d\n", len(r.Updated))
	_, _ = fmt.Fprintf(w, "  packages new       %d%s\n", len(r.New), listSuffix(r.New, prefix))
	if len(r.SkippedFail) > 0 {
		_, _ = fmt.Fprintf(w, "  failed (no fresh weight, prior kept) %d%s\n", len(r.SkippedFail), listSuffix(r.SkippedFail, prefix))
	}
	if len(r.Pruned) > 0 {
		_, _ = fmt.Fprintf(w, "  rows pruned (package gone) %d%s\n", len(r.Pruned), listSuffix(r.Pruned, prefix))
	}
	_, _ = fmt.Fprintf(w, "  total measured work %s\n", humanSeconds(r.TotalSeconds))
	_, _ = fmt.Fprintf(w, "  whale threshold     %.1fs\n", r.Threshold)
	if len(r.Whales) > 0 {
		_, _ = fmt.Fprintf(w, "  split candidates:\n")
		for _, wl := range r.Whales {
			_, _ = fmt.Fprintf(w, "    - %s\n", shortenID(wl, prefix))
		}
	}
	if len(r.PartialCaptures) > 0 {
		_, _ = fmt.Fprintf(w, "  partial captures (per-test rows kept, not pruned):\n")
		for _, pc := range r.PartialCaptures {
			_, _ = fmt.Fprintf(w, "    - %s\n", shortenID(pc, prefix))
		}
	}
	if len(r.Dominated) > 0 {
		_, _ = fmt.Fprintf(w, "  count-sharded because one runnable dominates:\n")
		for _, d := range r.Dominated {
			_, _ = fmt.Fprintf(w, "    - %s\n", shortenID(d, prefix))
		}
	}
	if len(r.Unflagged) > 0 {
		_, _ = fmt.Fprintf(w, "  no longer whales   %d%s\n", len(r.Unflagged), listSuffix(r.Unflagged, prefix))
	}
	_, _ = fmt.Fprintf(w, "  coverage recorded  %d packages (source: %s)\n", r.Coverage, r.CoverageFrom)
	return ew.err
}

func listSuffix(items []string, prefix string) string {
	if len(items) == 0 {
		return ""
	}
	short := make([]string, 0, len(items))
	for _, i := range items {
		short = append(short, shortenID(i, prefix))
	}
	return "  " + truncList(short, 5)
}
