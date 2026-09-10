package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/invakid404/testbucket/internal/nsmath"
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

	// AEtaNs is the exact integer-nanosecond objective value of contract
	// §0.9, serialized as a decimal string. It is ADDITIVE (PD-1) and present
	// iff the plan was built under `est_basis: wall`; §5.1 puts it on plan
	// buckets and observations, and MATRIX ENTRIES CARRY NONE.
	//
	// Under wall basis est_seconds is exactly round1(a_eta_ns / 1e9), and a
	// validator recomputes one from the other.
	AEtaNs *Nanos `json:"a_eta_ns,omitempty"`

	// WallEstSeconds is §5.1's additive SHADOW, emitted iff the basis is
	// reporter AND a fitted model with status ok exists. It never reaches
	// AllocationScore.
	WallEstSeconds *float64 `json:"wall_est_seconds,omitempty"`
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

	// EstBasis declares what the displayed estimate MEANS (§16.1). The plan
	// document and every matrix entry carry it. It is additive: PD-1's
	// canonical v0.2.2-field projection excludes it, so an unopted consumer
	// still reads byte-identical legacy bytes.
	EstBasis EstBasis `json:"est_basis"`

	// ExpandedUnitSetDigest is additive too, and is byte-identical across the
	// two bases for one store and one live set — unit topology is
	// store-derived in both, so only the partition differs.
	ExpandedUnitSetDigest string `json:"expanded_unit_set_digest,omitempty"`

	// Profile is §13.0's canonical profile, carried opaquely: the plan states
	// the shape it was built under, and the observation copies it VERBATIM so
	// QC13 can compare the two without either side re-deriving it.
	Profile json.RawMessage `json:"profile,omitempty"`

	// ComparabilityKeyDigest is §15.3's key: which wall history this plan's
	// measurements may join. A row measured under a different key belongs to a
	// different population, so the plan says which one it is.
	ComparabilityKeyDigest string `json:"comparability_key_digest,omitempty"`

	// RuntimeProfileDeclared and its digest ride in the plan document, which
	// is already transported to every bucket job. §21 fixes the plan job's
	// output set at matrix, cache-declaration-json and cache-declaration-digest,
	// and §15.3a does not enlarge it.
	RuntimeProfileDeclared       map[string]string `json:"runtime_profile_declared,omitempty"`
	RuntimeProfileDeclaredDigest string            `json:"runtime_profile_declared_digest,omitempty"`

	// fileParallelism is the intra-bucket concurrency the plan was built
	// under. It is unexported and unserialized: it exists only so the human
	// report can suppress §16.4's execution-model line above 1, where the
	// sentence would be false. A serialized field would be a new wire path
	// with no registry entry.
	fileParallelism int
}

// Nanos is a nanosecond count serialized as a JSON STRING.
//
// The reason is the canonical digest: RFC 8785 renders numbers through
// ECMAScript double formatting, so an integer above 2^53 — which a nanosecond
// count routinely is — would round, and two readers could canonicalise the same
// plan to different bytes. Carrying it as a string keeps the value exact.
type Nanos int64

func (n Nanos) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(strconv.FormatInt(int64(n), 10))), nil
}

func (n *Nanos) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("nanoseconds %q: %w", s, err)
		}
		*n = Nanos(v)
		return nil
	}
	var v int64
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("nanoseconds %s: %w", b, err)
	}
	*n = Nanos(v)
	return nil
}

// LegacyProjectionFields is PD-1's canonical v0.2.2 field set for a matrix
// entry, in v0.2.2 order. The projection restricted to these names must be
// BYTE-IDENTICAL for a consumer that has not opted in; everything else this
// product adds is additive and asserted separately.
func LegacyProjectionFields() []string {
	return []string{"bucket", "name", "est_seconds", "needs_node", "units", "invocations", "script"}
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
	// AllocationScore optionally replaces the SCHEDULING score of each unit.
	//
	// It exists because the weight a partition packs to and the estimate a
	// human reads are two different numbers. The store's rolling EWMA is
	// reporter-derived, which makes it fine for an estimate and inadmissible as
	// a campaign's allocation input; a frozen pre-plan scorer is the opposite.
	// When this is set, KK packs by its value while every reported est_seconds
	// keeps summing the store weights, so the matrix a consumer reads is
	// unchanged.
	//
	// It returns an error rather than a fallback: a unit the scorer cannot
	// score must fail the plan, because silently packing it by a different
	// rule is exactly the leak the separation exists to prevent.
	AllocationScore func(u runner.Unit) (float64, error)
	// Runnables optionally overrides where the core resolves a name-sliced
	// target's runnable set. Production leaves it nil and the adapter's
	// Runnables method is used; it is an injection seam for tests that drive
	// the planner against a synthetic tree with no toolchain.
	Runnables runnableNamer
	// --- the practical basis, decided by the caller before planning ---

	// Basis is the DECIDED basis, not the requested one: §0.8's two ordered
	// phases run in the caller, which is where the store status and the scored
	// flag both are, and the decision arrives here already made. Empty means
	// reporter, so a caller that never opted in is unchanged.
	Basis EstBasis
	// WallModel is the fitted model the wall basis packs by. It is REQUIRED
	// when Basis is wall and must be absent otherwise: a wall plan built
	// without the model it claims would be a reporter plan wearing a label.
	WallModel *WallModel
	// Profile, ComparabilityKeyDigest, RuntimeProfileDeclared and its digest
	// are the practical metadata the plan document carries. They are computed
	// by the caller — the profile from the flags it parsed, the key and the
	// runtime profile from the environment it is running in — because none of
	// them is derivable from the live set and the store alone.
	Profile                      json.RawMessage
	ComparabilityKeyDigest       string
	ExpandedUnitSetDigest        string
	RuntimeProfileDeclared       map[string]string
	RuntimeProfileDeclaredDigest string

	// FileParallelism is the intra-bucket concurrency the buckets were
	// rendered under. Core does not use it to pack — the sum-of-weights model
	// is unchanged — but §16.4 requires the human report to SUPPRESS its
	// execution-model line above 1, where "a bucket's estimate is its serial
	// reporter-work sum" stops being true: a bucket then finishes nearer its
	// heaviest unit than its sum.
	FileParallelism int
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
		it := itemOf(u)
		if opt.AllocationScore != nil {
			score, err := opt.AllocationScore(u)
			if err != nil {
				return nil, fmt.Errorf("allocation score for %s: %w", u.ID, err)
			}
			it.Weight = score
		}
		items = append(items, it)
		byID[u.ID] = u
	}
	// THE PARTITION FOLLOWS THE BASIS.
	//
	// Under the reporter basis the split is the store's measured weights
	// through the existing KK, exactly as it has always been. Under the wall
	// basis it is §6.5's two-stage integer allocator over A_eta_ns, and the
	// SAME objective value is what the document then displays — one
	// expression, evaluated once, so the number a reader compares against the
	// balance is the number the balance was computed from.
	var buckets []runner.Bucket
	var wallCosts []int64
	if opt.Basis == BasisWall {
		if opt.WallModel == nil {
			return nil, fmt.Errorf("wall basis requires a fitted model; none was supplied")
		}
		alloc := make([]AllocUnit, 0, len(ex.Units))
		for _, u := range ex.Units {
			baseNs, err := reporterNsOf(u)
			if err != nil {
				return nil, fmt.Errorf("unit %s: %w", u.ID, err)
			}
			alloc = append(alloc, AllocUnit{
				ID:      u.ID,
				BaseNs:  baseNs,
				IsSlice: u.Kind == runner.KindRunSlice || u.Kind == runner.KindCountShard,
			})
		}
		part, err := AllocateWall(*opt.WallModel, alloc, opt.K)
		if err != nil {
			return nil, fmt.Errorf("wall allocation: %w", err)
		}
		wallCosts, err = part.Costs(*opt.WallModel)
		if err != nil {
			return nil, fmt.Errorf("wall objective: %w", err)
		}
		buckets = make([]runner.Bucket, opt.K)
		for i := range part.Buckets {
			b := runner.Bucket{Index: i}
			for _, ui := range part.Buckets[i] {
				u := byID[part.Units[ui].ID]
				b.Units = append(b.Units, u)
				b.Seconds += u.Seconds
			}
			buckets[i] = b
		}
	} else {
		groups := karmarkarKarp(items, opt.K)
		buckets = make([]runner.Bucket, opt.K)
		for i, g := range groups {
			b := runner.Bucket{Index: i}
			for _, it := range g {
				u := byID[it.ID]
				b.Units = append(b.Units, u)
				b.Seconds += u.Seconds
			}
			buckets[i] = b
		}
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

	basis := opt.Basis
	if basis == "" {
		basis = BasisReporter
	}
	doc := &PlanDocument{
		K:               opt.K,
		Flags:           token,
		Algorithm:       "karmarkar-karp",
		StorePath:       storeName(opt.StorePath),
		UpdatedAt:       st.UpdatedAt,
		Notes:           ex.Notes,
		fileParallelism: opt.FileParallelism,

		EstBasis:                     basis,
		Profile:                      opt.Profile,
		ComparabilityKeyDigest:       opt.ComparabilityKeyDigest,
		ExpandedUnitSetDigest:        opt.ExpandedUnitSetDigest,
		RuntimeProfileDeclared:       opt.RuntimeProfileDeclared,
		RuntimeProfileDeclaredDigest: opt.RuntimeProfileDeclaredDigest,
	}
	if basis == BasisWall {
		doc.Algorithm = "two-stage integer allocation over A_eta_ns (contract §6.5)"
	}
	if opt.AllocationScore != nil {
		// Say so out loud: a reader comparing bucket estimates against the
		// balance would otherwise wonder why the split does not follow them.
		doc.Algorithm = "karmarkar-karp (packed by the frozen allocation score)"
		doc.Notes = append(doc.Notes,
			"buckets were packed by the frozen pre-plan allocation score; est_seconds still reports the store's measured weights")
	}
	// §5.1's ADDITIVE SHADOW. Under the reporter basis, and only when a fitted
	// model with status ok exists, each bucket also carries what that model
	// would have predicted. The field was declared and copied to matrix rows
	// and NOTHING EVER ASSIGNED IT, so the B arm of a scored pair shipped the
	// wall-only field permanently empty. It never reaches AllocationScore and
	// never changes the partition: it is what the model says about a split the
	// model did not make.
	var shadow []int64
	if basis == BasisReporter && opt.WallModel != nil {
		shadow = make([]int64, len(buckets))
		for i, b := range buckets {
			members := make([]AllocUnit, 0, len(b.Units))
			weights := make([]int64, 0, len(b.Units))
			for _, u := range b.Units {
				ns, err := reporterNsOf(u)
				if err != nil {
					return nil, fmt.Errorf("unit %s: %w", u.ID, err)
				}
				members = append(members, AllocUnit{
					ID:      u.ID,
					BaseNs:  ns,
					IsSlice: u.Kind == runner.KindRunSlice || u.Kind == runner.KindCountShard,
				})
				weights = append(weights, ns)
			}
			sum, err := nsmath.SumNs("reporter_sum_ns", weights...)
			if err != nil {
				return nil, fmt.Errorf("bucket %d: %w", b.Index, err)
			}
			cost, err := AEtaNs(*opt.WallModel, sum, ShapeOf(members))
			if err != nil {
				return nil, fmt.Errorf("bucket %d wall shadow: %w", b.Index, err)
			}
			shadow[i] = cost
		}
	}

	for i, b := range buckets {
		pb := renderPlanBucket(b, rnr.Render(b))
		if basis == BasisWall && b.Index < len(wallCosts) {
			// §5.1: under the wall basis est_seconds is exactly
			// round1(a_eta_ns / 1e9), so the displayed number and the
			// optimised one cannot drift.
			ns := Nanos(wallCosts[b.Index])
			pb.AEtaNs = &ns
			pb.Seconds = nsmath.Round1Seconds(wallCosts[b.Index])
		}
		if shadow != nil && i < len(shadow) {
			sec := nsmath.Round1Seconds(shadow[i])
			pb.WallEstSeconds = &sec
		}
		doc.Buckets = append(doc.Buckets, pb)
	}

	stale, staleOK := st.age(opt.Now)
	added, removed := st.coverageDrift(opt.Live)
	total := ex.MeasuredSeconds + ex.EstimatedSeconds
	// THE BALANCE SUMMARY IS IN THE BASIS'S OWN QUANTITY.
	//
	// It read the reporter bucket sums under BOTH bases, so a wall plan whose
	// buckets each cost 31.3 s reported ideal, makespan and lightest of 23.0 s:
	// the split's balance was described in a quantity the split was not made
	// from. `total_seconds`, `measured_seconds` and `estimated_seconds` stay
	// reporter work, because that is what they name.
	balance := make([]float64, len(buckets))
	for i, b := range buckets {
		balance[i] = b.Seconds
	}
	if basis == BasisWall && len(wallCosts) == len(buckets) {
		for i := range buckets {
			balance[i] = nsmath.Round1Seconds(wallCosts[i])
		}
	}
	ideal := total / float64(opt.K)
	if basis == BasisWall {
		objective := 0.0
		for _, v := range balance {
			objective += v
		}
		ideal = objective / float64(opt.K)
	}
	maxSec, minSec := 0.0, 0.0
	for i, v := range balance {
		if i == 0 || v > maxSec {
			maxSec = v
		}
		if i == 0 || v < minSec {
			minSec = v
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
	// The v0.2.2 field set comes first, in v0.2.2 order, so PD-1's canonical
	// legacy projection is a prefix of this struct rather than a reordering of
	// it. The additive fields follow and are omitted when they do not apply.
	//
	// There is deliberately NO a_eta_ns here: §5.1 puts the exact integer on
	// plan buckets and observations, and matrix entries carry none.
	type entry struct {
		Bucket      int                 `json:"bucket"`
		Name        string              `json:"name"`
		Seconds     float64             `json:"est_seconds"`
		NeedsNode   bool                `json:"needs_node"`
		Units       []string            `json:"units"`
		Invocations []runner.Invocation `json:"invocations"`
		Script      string              `json:"script"`

		EstBasis       EstBasis `json:"est_basis"`
		WallEstSeconds *float64 `json:"wall_est_seconds,omitempty"`
	}
	out := struct {
		Include []entry `json:"include"`
	}{}
	for _, b := range d.Buckets {
		e := entry{
			Bucket:         b.Index,
			Name:           b.Name,
			Seconds:        round1(b.Seconds),
			NeedsNode:      b.NeedsNode,
			Invocations:    b.Invocations,
			Script:         b.Script,
			EstBasis:       d.EstBasis,
			WallEstSeconds: b.WallEstSeconds,
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
	fmt.Fprintf(tw, "  loaded (recorded timing)\t%d\trecorded reporter work %s\n", s.Loaded, humanSeconds(s.MeasuredSeconds))
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
	// §16.4: the old wording called this "serial wall time", which is true only
	// of the serial REPORTER-WORK sum. The basis is named, and the line is not
	// emitted at all when file_parallelism > 1, because a bucket then finishes
	// nearer its heaviest unit than its sum and the sentence would be false.
	//
	// It named the reporter sum under BOTH bases. A wall-basis estimate is the
	// model's A_eta prediction for the bucket, which is not a sum of anything
	// the store measured — describing it as one told a reader to compare it
	// against a quantity it was never derived from.
	if d.fileParallelism <= 1 {
		if d.EstBasis == BasisWall {
			_, _ = fmt.Fprintf(w, "execution model: -p=1; a bucket's estimate is the fitted model's predicted action interval (A_eta), not a reporter-work sum.\n")
		} else {
			_, _ = fmt.Fprintf(w, "execution model: -p=1, so a bucket's estimate is its serial reporter-work sum.\n")
		}
	}
	return ew.err
}

// reporterNsOf converts a unit's stored weight to the exact integer
// nanoseconds the wall objective consumes.
//
// The store keeps one-decimal SECONDS as a float64, and the objective is
// defined on integer nanoseconds, so a conversion is unavoidable at exactly
// this boundary. It goes through the exact rational domain rather than
// `seconds * 1e9` in float64: that product passes 2^53 at about 9e6 seconds,
// which is inside the range a long-running suite can reach, and above it two
// distinct stored weights would land on one nanosecond count.
func reporterNsOf(u runner.Unit) (int64, error) {
	q := new(big.Rat).SetFloat64(u.Seconds)
	if q == nil {
		return 0, fmt.Errorf("unit weight %v is not a finite number", u.Seconds)
	}
	q.Mul(q, new(big.Rat).SetInt64(1_000_000_000))
	return nsmath.RoundHalfUpRat("reporter_sum_ns", q)
}

// ExpandUnitsFor is the planner's unit expansion, exposed for the calibration
// proposer of contract §17.3a.
//
// It exists so the proposer searches over the units a plan would ACTUALLY
// schedule — the same whale expansion, the same cold-start weights, the same
// slicing — rather than a second derivation that could drift from the
// planner's. §6.5 makes that reuse the rule for the packer; the universe the
// packer runs over deserves the same treatment.
func ExpandUnitsFor(ctx context.Context, rnr runner.Runner, st *Store, opt PlanOptions) ([]runner.Unit, error) {
	if st == nil {
		st = NewStore(opt.Token)
	}
	runnables := opt.Runnables
	if runnables == nil {
		runnables = func(p runner.LivePackage) ([]string, error) {
			return rnr.Runnables(ctx, p)
		}
	}
	mean, _, _ := st.meanWeight(opt.Live)
	ex, err := expandUnits(opt.Live, st, expandOptions{
		K:           opt.K,
		BaseCount:   opt.Count,
		MeanSeconds: mean,
		Runnables:   runnables,
	})
	if err != nil {
		return nil, err
	}
	return ex.Units, nil
}

// ReporterNs is reporterNsOf, exposed for the same reason ExpandUnitsFor is:
// the design matrix a calibration proposal is built from must be in the exact
// integer domain the objective is evaluated in, converted the one way.
func ReporterNs(u runner.Unit) (int64, error) { return reporterNsOf(u) }

// ReporterNsFromSeconds converts a stored one-decimal seconds weight to exact
// integer nanoseconds, through the same checked rational path reporterNsOf
// uses. It is exported for the observation assembler, which needs the identical
// conversion so a plan and the observation of it cannot disagree by rounding.
func ReporterNsFromSeconds(sec float64) (int64, error) {
	return reporterNsOf(runner.Unit{Seconds: sec})
}
