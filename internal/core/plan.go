package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/invakid404/testbucket/internal/runner"
)

// PlanUnit is one scheduled unit as the plan artifact records it.
type PlanUnit struct {
	ID   string      `json:"id"`
	Kind runner.Kind `json:"kind"`
	// Packages is what this unit actually runs. It is spelled out rather than
	// left implicit in the ID because a module-atom unit covers several
	// targets, and a reader auditing the plan artifact should not have to
	// re-derive that.
	Packages []string `json:"packages"`
	// Run is a name-slice's runnable names, verbatim. It is spelled out here — not
	// left to be parsed back out of the ID's `pkg[a|b]` form — because a runnable
	// name may itself contain the '|' the ID joins on (a Vitest test title is
	// arbitrary text), so splitting the ID would corrupt the audit's name check.
	// Empty for every non-slice unit.
	Run       []string `json:"run,omitempty"`
	Seconds   float64  `json:"est_seconds"`
	Estimated bool     `json:"estimated,omitempty"`
}

// PlanBucket is one rendered lane: its units plus the concrete invocations and
// shell the CI job runs.
type PlanBucket struct {
	Index       int                 `json:"bucket"`
	Name        string              `json:"name"`
	Seconds     float64             `json:"est_seconds"`
	NeedsNode   bool                `json:"needs_node"`
	Units       []PlanUnit          `json:"units"`
	Invocations []runner.Invocation `json:"invocations"`
	Script      string              `json:"script"`
}

// PlanSummary is the loaded-vs-missing report. It exists so that a store that
// has silently expired, been keyed wrong, or drifted away from the tree shows
// up in the job log as numbers rather than as a mysteriously slow matrix three
// weeks later.
type PlanSummary struct {
	LivePackages     int      `json:"live_packages"`
	Loaded           int      `json:"loaded"`
	Missing          int      `json:"missing"`
	MissingPackages  []string `json:"missing_packages,omitempty"`
	MeasuredSeconds  float64  `json:"measured_seconds"`
	EstimatedSeconds float64  `json:"estimated_seconds"`
	MeanSeconds      float64  `json:"mean_seconds"`
	TotalSeconds     float64  `json:"total_seconds"`
	ScheduledUnits   int      `json:"scheduled_units"`
	StaleRows        []string `json:"stale_rows,omitempty"`
	DriftAdded       []string `json:"drift_added,omitempty"`
	DriftRemoved     []string `json:"drift_removed,omitempty"`
	ColdStart        bool     `json:"cold_start"`
	ColdStartReason  string   `json:"cold_start_reason,omitempty"`
	StoreAge         string   `json:"store_age,omitempty"`
	Stale            bool     `json:"stale,omitempty"`
	IdealSeconds     float64  `json:"ideal_seconds"`
	MakespanSeconds  float64  `json:"makespan_seconds"`
	LightestSeconds  float64  `json:"lightest_seconds"`
	ImbalancePct     float64  `json:"imbalance_pct"`
}

// PlanDocument is the whole plan: the matrix source, the human summary, and the
// per-bucket scripts. It is what --shard-plan writes and what `audit` reads
// back.
type PlanDocument struct {
	K         int          `json:"k"`
	Flags     string       `json:"flags"`
	Algorithm string       `json:"algorithm"`
	StorePath string       `json:"store"`
	UpdatedAt string       `json:"store_updated_at,omitempty"`
	Summary   PlanSummary  `json:"summary"`
	Buckets   []PlanBucket `json:"buckets"`
	Notes     []string     `json:"notes,omitempty"`
}

// PlanOptions is the framework-NEUTRAL plan configuration. Everything about how
// a bucket becomes a concrete command — flags, timeout, sweep rendering, setup
// detection — belongs to the adapter, not here; the core owns only the
// scheduling shape.
type PlanOptions struct {
	// K is the bucket count — the single knob.
	K int
	// StorePath is where the store lives (used only for the summary line).
	StorePath string
	// Count is the flake-sweep base repetitions: run the selection N times,
	// count-shards divide N. Neutral — the adapter decides how to render it.
	Count int
	// StaleAfter warns when the store was recorded longer ago than this.
	StaleAfter time.Duration
	// Now is the clock (staleness/age are derived from it).
	Now time.Time
	// Live is the discovered target set.
	Live []runner.LivePackage
	// Token is the adapter's opaque comparability token. The core treats it as
	// a key: the store cold-starts when it disagrees. It never inspects it.
	Token string
	// Runnables optionally overrides where the core resolves a name-sliced
	// target's runnable set. Production leaves it nil and the adapter's
	// Runnables method is used; it is an injection seam for tests that drive
	// the planner against a synthetic tree with no toolchain.
	Runnables runnableNamer
}

// Validate rejects settings that would emit an invalid or meaningless matrix. A
// non-positive Count is the sharp one: a sweep of zero repetitions schedules a
// complete, balanced, gate-passing matrix that runs no tests.
func (o PlanOptions) Validate() error {
	switch {
	case o.K < 1:
		return fmt.Errorf("--k must be >= 1, got %d", o.K)
	case o.Count < 1:
		return fmt.Errorf("--count must be >= 1, got %d (a sweep of zero repetitions runs nothing)", o.Count)
	case o.StaleAfter < 0:
		return fmt.Errorf("--stale-after must be >= 0, got %v", o.StaleAfter)
	}
	return nil
}

// BuildPlan is the whole planner as a near-pure function of (live tree, store,
// options). Its only outward call is to the runner adapter — to resolve the
// runnable names of a name-sliced target, to render each bucket into a command,
// and to validate each unit's command grammar. Everything else — weight
// assignment, KK partition, the structural coverage gate, the summary — is
// this package's own.
func BuildPlan(ctx context.Context, rnr runner.Runner, st *Store, reason string, opt PlanOptions) (*PlanDocument, error) {
	if err := opt.Validate(); err != nil {
		return nil, err
	}
	token := opt.Token

	coldStart := false
	coldReason := reason
	if st == nil {
		coldStart = true
		st = NewStore(token)
	} else if st.Flags != "" && st.Flags != token {
		// Weights measured under a different comparability token are not
		// comparable; using them would produce a confidently wrong split. This
		// is the guard against the "renamed job, silently bad split" trap.
		coldStart = true
		coldReason = fmt.Sprintf("store was recorded under flags %q but this plan runs %q", st.Flags, token)
		st = NewStore(token)
	}
	if coldReason != "" {
		coldStart = true
	}

	mean, measuredCount, _ := st.meanWeight(opt.Live)
	runnables := opt.Runnables
	if runnables == nil {
		runnables = func(p runner.LivePackage) ([]string, error) {
			return rnr.Runnables(ctx, p)
		}
	}
	ex, err := expandUnits(opt.Live, st, expandOptions{
		K:           opt.K,
		BaseCount:   opt.Count,
		MeanSeconds: mean,
		Runnables:   runnables,
	})
	if err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(ex.Units))
	byID := make(map[string]runner.Unit, len(ex.Units))
	for _, u := range ex.Units {
		items = append(items, itemOf(u))
		byID[u.ID] = u
	}
	groups := karmarkarKarp(items, opt.K)

	buckets := make([]runner.Bucket, opt.K)
	for i, g := range groups {
		b := runner.Bucket{Index: i}
		for _, it := range g {
			u := byID[it.ID]
			b.Units = append(b.Units, u)
			b.Seconds += u.Seconds
		}
		buckets[i] = b
	}

	// The gate runs on the FINAL buckets, after partitioning — the point is to
	// prove what will actually be executed, not what was intended. The
	// command-grammar half is the adapter's; the structural half is ours.
	if err := assertCoverage(gateInput{
		Live:         opt.Live,
		Buckets:      buckets,
		Runnables:    ex.Runnables,
		BaseCount:    opt.Count,
		ValidateUnit: rnr.ValidateUnit,
	}); err != nil {
		return nil, err
	}

	doc := &PlanDocument{
		K:         opt.K,
		Flags:     token,
		Algorithm: "karmarkar-karp",
		StorePath: storeName(opt.StorePath),
		UpdatedAt: st.UpdatedAt,
		Notes:     ex.Notes,
	}
	for _, b := range buckets {
		doc.Buckets = append(doc.Buckets, renderPlanBucket(b, rnr.Render(b)))
	}

	stale, staleOK := st.age(opt.Now)
	added, removed := st.coverageDrift(opt.Live)
	total := ex.MeasuredSeconds + ex.EstimatedSeconds
	ideal := total / float64(opt.K)
	maxSec, minSec := 0.0, 0.0
	for i, b := range buckets {
		if i == 0 || b.Seconds > maxSec {
			maxSec = b.Seconds
		}
		if i == 0 || b.Seconds < minSec {
			minSec = b.Seconds
		}
	}
	imbalance := 0.0
	if ideal > 0 {
		imbalance = (maxSec - ideal) / ideal * 100
	}

	doc.Summary = PlanSummary{
		LivePackages:     len(ex.Loaded) + len(ex.Missing),
		Loaded:           len(ex.Loaded),
		Missing:          len(ex.Missing),
		MissingPackages:  ex.Missing,
		MeasuredSeconds:  ex.MeasuredSeconds,
		EstimatedSeconds: ex.EstimatedSeconds,
		MeanSeconds:      mean,
		TotalSeconds:     total,
		ScheduledUnits:   len(ex.Units),
		StaleRows:        st.staleRows(opt.Live),
		DriftAdded:       added,
		DriftRemoved:     removed,
		ColdStart:        coldStart || measuredCount == 0,
		ColdStartReason:  coldReason,
		IdealSeconds:     ideal,
		MakespanSeconds:  maxSec,
		LightestSeconds:  minSec,
		ImbalancePct:     imbalance,
	}
	if staleOK {
		doc.Summary.StoreAge = stale.Round(time.Minute).String()
		doc.Summary.Stale = opt.StaleAfter > 0 && stale > opt.StaleAfter
	}
	if doc.Summary.ColdStart && doc.Summary.ColdStartReason == "" && measuredCount == 0 {
		doc.Summary.ColdStartReason = "store carries no measurement for any live package"
	}

	if doc.Summary.LivePackages == 0 {
		doc.Notes = append(doc.Notes, "no live package in the module set has test files — the matrix is empty")
	}
	empty := 0
	for _, b := range buckets {
		if len(b.Units) == 0 {
			empty++
		}
	}
	if empty > 0 {
		// Not an error — the matrix is still correct — but K buckets for fewer
		// than K units means paying a job's fixed overhead for nothing.
		doc.Notes = append(doc.Notes, fmt.Sprintf(
			"%d of %d buckets are empty: only %d schedulable units exist, so K=%d is more lanes than there is work",
			empty, opt.K, len(ex.Units), opt.K))
	}
	return doc, nil
}

// renderPlanBucket combines a bucket's units (this package's business) with the
// adapter's rendering of them (the concrete commands) into the artifact form.
func renderPlanBucket(b runner.Bucket, r runner.Rendered) PlanBucket {
	pb := PlanBucket{
		Index:       b.Index,
		Name:        fmt.Sprintf("bucket-%d", b.Index),
		Seconds:     b.Seconds,
		NeedsNode:   r.NeedsNode,
		Invocations: r.Invocations,
		Script:      r.Script,
	}
	for _, u := range b.Units {
		covered := make([]string, 0, len(u.Packages))
		for _, p := range u.Packages {
			covered = append(covered, p.ID)
		}
		pb.Units = append(pb.Units, PlanUnit{
			ID: u.ID, Kind: u.Kind, Packages: covered, Run: u.Run, Seconds: u.Seconds, Estimated: u.Estimate,
		})
	}
	return pb
}

// MatrixJSON renders the GitHub-Actions matrix, ready for
// `matrix: ${{ fromJSON(needs.plan.outputs.matrix) }}`.
func (d *PlanDocument) MatrixJSON() ([]byte, error) {
	type entry struct {
		Bucket      int                 `json:"bucket"`
		Name        string              `json:"name"`
		Seconds     float64             `json:"est_seconds"`
		NeedsNode   bool                `json:"needs_node"`
		Units       []string            `json:"units"`
		Invocations []runner.Invocation `json:"invocations"`
		Script      string              `json:"script"`
	}
	out := struct {
		Include []entry `json:"include"`
	}{}
	for _, b := range d.Buckets {
		e := entry{
			Bucket:      b.Index,
			Name:        b.Name,
			Seconds:     round1(b.Seconds),
			NeedsNode:   b.NeedsNode,
			Invocations: b.Invocations,
			Script:      b.Script,
		}
		for _, u := range b.Units {
			e.Units = append(e.Units, u.ID)
		}
		out.Include = append(out.Include, e)
	}
	return json.Marshal(out)
}

// WriteSummary prints the human report. It goes to the job log (stderr when the
// matrix is on stdout) precisely so that staleness is never silent: the
// loaded-vs-missing block is the whole early-warning system for a store that
// expired out of the CI cache — which is why a failure to write it is returned
// rather than dropped.
func (d *PlanDocument) WriteSummary(out io.Writer, shortenPrefix string) error {
	ew := &errWriter{w: out}
	w := io.Writer(ew)
	s := d.Summary
	fmt.Fprintf(w, "testbucket plan — K=%d, algorithm=%s, flags %q\n", d.K, d.Algorithm, d.Flags)
	fmt.Fprintf(w, "store: %s", d.StorePath)
	if d.UpdatedAt != "" {
		fmt.Fprintf(w, " (recorded %s", d.UpdatedAt)
		if s.StoreAge != "" {
			fmt.Fprintf(w, ", %s ago", s.StoreAge)
		}
		fmt.Fprint(w, ")")
	}
	fmt.Fprintln(w)

	if s.ColdStart {
		fmt.Fprintf(w, "\n*** COLD START: %s ***\n", firstNonEmpty(s.ColdStartReason, "no usable weights"))
		fmt.Fprintf(w, "    Every unweighted unit gets the mean weight (%.1fs). The matrix is valid and\n", s.MeanSeconds)
		fmt.Fprintf(w, "    complete, but only count-balanced until the next master `record` lands.\n")
	}
	if s.Stale {
		fmt.Fprintf(w, "\n*** STALE STORE: last recorded %s ago — the split is running on old timings. ***\n", s.StoreAge)
	}

	fmt.Fprintf(w, "\nloaded vs missing\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  live test packages\t%d\t\n", s.LivePackages)
	fmt.Fprintf(tw, "  loaded (recorded timing)\t%d\tmeasured wall-time %s\n", s.Loaded, humanSeconds(s.MeasuredSeconds))
	fmt.Fprintf(tw, "  missing (mean estimate)\t%d\testimated %s @ mean %.1fs\n", s.Missing, humanSeconds(s.EstimatedSeconds), s.MeanSeconds)
	fmt.Fprintf(tw, "  scheduled units\t%d\ttotal scheduled work %s\n", s.ScheduledUnits, humanSeconds(s.TotalSeconds))
	if len(s.StaleRows) > 0 {
		fmt.Fprintf(tw, "  store rows with no live package\t%d\t%s\n", len(s.StaleRows), truncList(s.StaleRows, 3))
	}
	if len(s.DriftAdded) > 0 || len(s.DriftRemoved) > 0 {
		fmt.Fprintf(tw, "  coverage drift vs store\t+%d / -%d\t%s\n", len(s.DriftAdded), len(s.DriftRemoved), truncList(append(append([]string{}, s.DriftAdded...), s.DriftRemoved...), 3))
	}
	_ = tw.Flush()
	if len(s.MissingPackages) > 0 {
		fmt.Fprintf(w, "  estimated packages: %s\n", truncList(s.MissingPackages, 8))
	}

	fmt.Fprintf(w, "\nbalance\n")
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  ideal (total/K)\t%s\n", humanSeconds(s.IdealSeconds))
	fmt.Fprintf(tw, "  makespan (heaviest)\t%s\n", humanSeconds(s.MakespanSeconds))
	fmt.Fprintf(tw, "  lightest\t%s\n", humanSeconds(s.LightestSeconds))
	fmt.Fprintf(tw, "  imbalance over ideal\t%.1f%%\n", s.ImbalancePct)
	_ = tw.Flush()

	fmt.Fprintf(w, "\nbuckets\n")
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  bucket\test\tnode\tunits\n")
	for _, b := range d.Buckets {
		names := make([]string, 0, len(b.Units))
		for _, u := range b.Units {
			names = append(names, displayID(u.ID, shortenPrefix))
		}
		node := "-"
		if b.NeedsNode {
			node = "node"
		}
		fmt.Fprintf(tw, "  %d\t%.1fs\t%s\t%s\n", b.Index, b.Seconds, node, truncList(names, 6))
	}
	_ = tw.Flush()

	if len(d.Notes) > 0 {
		fmt.Fprintf(w, "\nsplit notes\n")
		for _, n := range d.Notes {
			fmt.Fprintf(w, "  - %s\n", shortenID(n, shortenPrefix))
		}
	}
	_, _ = fmt.Fprintf(w, "\ncoverage gate: PASS — every live package, every runnable (test, example\n")
	_, _ = fmt.Fprintf(w, "or fuzz target) of every name-sliced package, and every count-shard of\n")
	_, _ = fmt.Fprintf(w, "every sharded package is assigned to exactly one bucket; each sharded\n")
	_, _ = fmt.Fprintf(w, "package's shards add back up to the requested -count.\n")
	_, _ = fmt.Fprintf(w, "execution model: -p=1, so a bucket's estimate is its serial wall time.\n")
	return ew.err
}
