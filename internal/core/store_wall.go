package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// W is the bounded history ring size of contract §15.1.
const W = 240

// WallFailureSubtype is the spelled vocabulary of contract §15.1c. A value
// outside it is a failure, so the type is a closed set rather than a free
// string.
type WallFailureSubtype string

const (
	SubtypeMigratedNoHistory   WallFailureSubtype = "migrated_no_history"
	SubtypeRowsBelowMinimum    WallFailureSubtype = "rows_below_minimum"
	SubtypeRunsBelowMinimum    WallFailureSubtype = "runs_below_minimum"
	SubtypeRankInsufficient    WallFailureSubtype = "rank_insufficient"
	SubtypeNNLSBudgetExhausted WallFailureSubtype = "nnls_budget_exhausted"
	SubtypeMAECeilingExceeded  WallFailureSubtype = "mae_ceiling_exceeded"
)

// wallStatusPrecedenceOrder is contract §15.1c's fixed precedence, applied when
// more than one insufficiency predicate holds. The serialized subtype MUST be
// the first match, so a corpus below both minima — or below a minimum AND
// rank-deficient — serializes one determinate value rather than an
// implementation's choice.
//
// The order follows the order in which the conditions can be DECIDED: a
// migrated store has no rows to count, counts are decidable before a design
// matrix is formed, rank needs the matrix, and the solver cap can only fire
// after rank admits the design.
var wallStatusPrecedenceOrder = []WallFailureSubtype{
	SubtypeMigratedNoHistory,
	SubtypeRowsBelowMinimum,
	SubtypeRunsBelowMinimum,
	SubtypeRankInsufficient,
	SubtypeNNLSBudgetExhausted,
}

// WallStatusPrecedence resolves a set of simultaneously-holding insufficiency
// predicates to the one subtype §15.1c requires to be serialized.
func WallStatusPrecedence(holding map[WallFailureSubtype]bool) (WallFailureSubtype, bool) {
	for _, s := range wallStatusPrecedenceOrder {
		if holding[s] {
			return s, true
		}
	}
	return "", false
}

// WallStatusPrecedenceOrder exposes the order so a test can enumerate it
// rather than transcribing it.
func WallStatusPrecedenceOrder() []WallFailureSubtype {
	return append([]WallFailureSubtype(nil), wallStatusPrecedenceOrder...)
}

// WallRingRow is the bounded history ring row of contract §15.1a, one row per
// qualifying observation.
//
// Every REGRESSOR value is frozen at PLAN time, so a later store update cannot
// retroactively change what was fitted. In particular `reporter_sum_ns` is
// deliberately not recomputed at fit time: recomputing against a newer store
// would fit the model to inputs the plan never saw.
type WallRingRow struct {
	// Identity, frozen at run. All five identity SHAs are separate fields and
	// none is derivable from another (S-6).
	Repository     string `json:"repository"`
	JobID          string `json:"job_id"`
	HeadSHA        string `json:"head_sha"`
	CandidateSHA   string `json:"candidate_sha"`
	WorkloadCommit string `json:"workload_commit"`
	RunID          string `json:"run_id"`
	RunAttempt     string `json:"run_attempt"`

	// ObservedStartRealtime is the observation's own realtime_start, RFC 3339
	// UTC. SELF-REPORTED, not authenticated: it is the recency stamp of
	// §15.1b and is used for nothing else.
	ObservedStartRealtime string `json:"observed_start_realtime"`
	// RunStartedAt is the AUTHENTICATED Actions API run start. Optional in-CI,
	// because retrieval needs the operator's credentials and no scored
	// workflow carries `actions: read`. Every campaign date, cutoff and window
	// gate uses this field.
	RunStartedAt string `json:"run_started_at,omitempty"`

	// Trainable is the SINGLE CARRIER of every in-CI exclusion (R13-D1),
	// written once at append from the row's own bytes plus the supplied
	// config. Writing it at append is what lets the fitter select on one
	// predicate and lets no later fit re-derive an exclusion.
	Trainable bool `json:"trainable"`
	// IngestSeq is an append diagnostic only: recorded, reported, and READ BY
	// NOTHING — not retention, eviction, selection or fitting (R8-D3).
	IngestSeq int64 `json:"ingest_seq"`

	// Admission, frozen at plan.
	BucketIndex            int    `json:"bucket_index"`
	PlanDigest             string `json:"plan_digest"`
	StoreSHA256            string `json:"store_sha256"`
	ComparabilityKeyDigest string `json:"comparability_key_digest"`

	// The three design regressors, frozen at plan, integer nanoseconds.
	// Column 1 is the intercept and is not stored.
	ReporterSumNs int64 `json:"reporter_sum_ns,string"`
	IAnyWholeFile int   `json:"i_any_whole_file"`
	SliceCount    int   `json:"slice_count"`

	// ElapsedNs is the SOLE regression response, = A, integer nanoseconds.
	ElapsedNs int64 `json:"elapsed_ns,string"`

	// Diagnostics: audit only, NEVER regressors.
	WholeFileCount  int `json:"whole_file_count"`
	InvocationCount int `json:"invocation_count"`

	// Terminal admission: only `passed` trains.
	Terminal string `json:"terminal"`
}

// IntrinsicID is contract §15.1b's row-intrinsic tuple:
//
//	(repository, run_id, run_attempt, job_id, bucket_index, plan_digest)
//
// It is a function of the row's own immutable bytes, so it is identical under
// every permutation of arrival order.
func (r WallRingRow) IntrinsicID() [6]string {
	return [6]string{
		r.Repository, r.RunID, r.RunAttempt, r.JobID,
		fmt.Sprintf("%d", r.BucketIndex), r.PlanDigest,
	}
}

// RingRecencyKey is contract §15.1b's recency key:
//
//	recency(row) = (row.observed_start_realtime, row.intrinsic_id)
//
// compared lexicographically, with the instant as an RFC 3339 UTC string.
//
// The tie key is ROW-INTRINSIC and not the append counter, and that is a
// correctness requirement rather than a preference. An earlier revision used
// (observed_start_realtime, ingest_seq) with ingest_seq assigned at append: take
// W+1 = 241 valid rows from DISTINCT runs sharing one parseable
// observed_start_realtime, and appending them forward evicts the first while
// appending them reversed evicts the last. Retention then depended on arrival
// order and shuffle-invariance could not hold.
type RingRecencyKey struct {
	ObservedStartRealtime string
	Intrinsic             [6]string
}

// RingRecencyKeyOf builds the recency key for a row.
func RingRecencyKeyOf(r WallRingRow) RingRecencyKey {
	return RingRecencyKey{ObservedStartRealtime: r.ObservedStartRealtime, Intrinsic: r.IntrinsicID()}
}

// Less orders two recency keys, oldest first.
func (a RingRecencyKey) Less(b RingRecencyKey) bool {
	if a.ObservedStartRealtime != b.ObservedStartRealtime {
		return a.ObservedStartRealtime < b.ObservedStartRealtime
	}
	for i := range a.Intrinsic {
		if a.Intrinsic[i] != b.Intrinsic[i] {
			return a.Intrinsic[i] < b.Intrinsic[i]
		}
	}
	return false
}

// Equal reports whether two keys are the same row identity.
func (a RingRecencyKey) Equal(b RingRecencyKey) bool {
	return !a.Less(b) && !b.Less(a)
}

// WallFitGroup is the group of leaves §15.1c makes present only where a fit
// was accepted. It is a separate struct so "all absent" is representable as a
// nil pointer rather than as a convention about zero values — a zero
// coefficient is a legal fitted value and must not be confused with an absent
// one.
type WallFitGroup struct {
	FixedNs                   int64    `json:"fixed_ns,string"`
	Scale                     float64  `json:"scale"`
	WholeInvocationOverheadNs int64    `json:"whole_invocation_overhead_ns,string"`
	PerSliceOverheadNs        int64    `json:"per_slice_overhead_ns,string"`
	FittedAt                  string   `json:"fitted_at"`
	RowsUsed                  int      `json:"rows_used"`
	RunsUsed                  int      `json:"runs_used"`
	ResidualMAENs             int64    `json:"residual_mae_ns,string"`
	ResidualP90Ns             int64    `json:"residual_p90_ns,string"`
	RankSupport               []string `json:"rank_support"`
}

// WallObject is the optional `wall` object schema 2 adds. Everything outside
// it keeps its schema-1 meaning byte-for-byte.
//
// ModelVersion, ComparabilityKeyDigest and Observations are ALWAYS PRESENT
// once `wall` exists (§15.1c). The fit group is a pointer because it is
// present only where a fit was accepted.
type WallObject struct {
	// ModelVersion versions the MODEL SEMANTICS; storeSchema versions the
	// store LAYOUT. They are independent integers and must never be conflated.
	// Reading is fail-closed: an absent or unimplemented value is refused, not
	// defaulted (§15.1d).
	ModelVersion *int `json:"model_version"`
	// ComparabilityKeyDigest records which history these rows belong to. A
	// change clears Observations, sets status insufficient, and leaves the
	// reporter EWMAs untouched.
	ComparabilityKeyDigest string             `json:"comparability_key_digest"`
	Status                 WallStatus         `json:"status"`
	FailureSubtype         WallFailureSubtype `json:"failure_subtype,omitempty"`
	Fit                    *WallFitGroup      `json:"fit,omitempty"`
	// Observations is the bounded ring (W = 240). Always present once `wall`
	// exists, and empty rather than absent on a fresh migration.
	Observations []WallRingRow `json:"observations"`
	// MigratedFrom is present iff the store was migrated. It is NOT serialized
	// here: the registry puts `migrated_from` at the store ROOT, and this
	// field is where the migration is recorded on the way there.
	MigratedFrom *int `json:"-"`
}

// Validate enforces contract §15.1c's presence matrix. It is the store-side
// half of TestWallStoreSchemaStateMatrixAndMigration: the matrix is the only
// presence rule and its rows are the exhaustive state set.
func (w *WallObject) Validate() error {
	// The three always-present leaves.
	if w.ModelVersion == nil {
		return fmt.Errorf("wall object has no model_version; §15.1d refuses an absent version rather than defaulting it")
	}
	if *w.ModelVersion != WallModelVersion {
		return fmt.Errorf("wall object model_version is %d, this reader implements %d; refusing rather than reinterpreting",
			*w.ModelVersion, WallModelVersion)
	}
	if w.ComparabilityKeyDigest == "" {
		return fmt.Errorf("wall object has no comparability_key_digest; it is always present once wall exists")
	}
	if w.Observations == nil {
		return fmt.Errorf("wall object has no observations container; it is always present once wall exists, empty if there is no history")
	}

	// failure_subtype is required for every non-ok status and absent for ok.
	if w.Status == WallStatusOK {
		if w.FailureSubtype != "" {
			return fmt.Errorf("status ok carries failure_subtype %q; it must be absent", w.FailureSubtype)
		}
	} else {
		if w.FailureSubtype == "" {
			return fmt.Errorf("status %q carries no failure_subtype; it is required for every non-ok status", w.Status)
		}
		if !validSubtype(w.FailureSubtype) {
			return fmt.Errorf("failure_subtype %q is outside §15.1c's spelled vocabulary", w.FailureSubtype)
		}
	}

	// The fit group is present exactly where the matrix says a fit was
	// accepted: status ok or degraded.
	fitExpected := w.Status == WallStatusOK || w.Status == WallStatusDegraded
	if fitExpected && w.Fit == nil {
		return fmt.Errorf("status %q requires the fit group to be present", w.Status)
	}
	if !fitExpected && w.Fit != nil {
		return fmt.Errorf("status %q with failure_subtype %q must carry no coefficient, fitted_at or fit statistic",
			w.Status, w.FailureSubtype)
	}
	// §15.1d: a bump never silently reuses a fit. A different model_version
	// together with a fit group is forbidden — that case is unreachable here
	// because the version check above already refused it, which is the point.
	trainableRows, diagnosticRows := 0, 0
	for _, row := range w.Observations {
		if row.Trainable {
			trainableRows++
		} else {
			diagnosticRows++
		}
	}
	// PER CLASS, not combined: §15.1a bounds each `trainable` class at W
	// independently, so the two together may legitimately reach 2W.
	if trainableRows > W {
		return fmt.Errorf("observations ring holds %d trainable rows, above W = %d", trainableRows, W)
	}
	if diagnosticRows > W {
		return fmt.Errorf("observations ring holds %d non-trainable rows, above W = %d", diagnosticRows, W)
	}
	return nil
}

func validSubtype(s WallFailureSubtype) bool {
	switch s {
	case SubtypeMigratedNoHistory, SubtypeRowsBelowMinimum, SubtypeRunsBelowMinimum,
		SubtypeRankInsufficient, SubtypeNNLSBudgetExhausted, SubtypeMAECeilingExceeded:
		return true
	}
	return false
}

// NewMigratedWall is contract §15.2's `1 → 2` forward step. `wall` is
// initialised empty with status insufficient and subtype migrated_no_history,
// the always-present leaves are written, and migrated_from is recorded.
//
// NO WALL HISTORY IS INVENTED: Observations is an empty container, not a
// fabricated corpus.
func NewMigratedWall(comparabilityKeyDigest string) *WallObject {
	v := WallModelVersion
	from := 1
	return &WallObject{
		ModelVersion:           &v,
		ComparabilityKeyDigest: comparabilityKeyDigest,
		Status:                 WallStatusInsufficient,
		FailureSubtype:         SubtypeMigratedNoHistory,
		Observations:           []WallRingRow{},
		MigratedFrom:           &from,
	}
}

// AppendRow inserts a qualifying row and applies §15.1b's retention: the ring
// keeps the most recent W rows under the recency key, and eviction takes the
// OLDEST.
//
// Retention is a function of the rows themselves, so two different ingest
// orders over the same observations produce the same retained population and
// the same evictions.
//
// Eviction is class-aware: a trainable:false row is a bounded diagnostic and
// must never displace a trainable:true row, because the diagnostic class would
// otherwise be able to evict the corpus the model is fitted from.
func (w *WallObject) AppendRow(r WallRingRow) {
	w.Observations = append(w.Observations, r)
	w.evict()
}

// evict applies §15.1a's retention, which is PER CLASS.
//
// Each `trainable` class holds up to W rows INDEPENDENTLY, and appending to one
// trims only that one. The combined cap this used to apply had two consequences
// the contract does not permit: 240 trainable rows and one diagnostic row could
// not coexist, so the accepted total could never reach 480; and a diagnostic
// append pushed the population over W and evicted from a class it has nothing
// to do with, so an untrainable row could displace the corpus a fit reads.
func (w *WallObject) evict() {
	w.sortByRecency()
	trainable := make([]WallRingRow, 0, len(w.Observations))
	diagnostic := make([]WallRingRow, 0, len(w.Observations))
	for _, row := range w.Observations {
		if row.Trainable {
			trainable = append(trainable, row)
		} else {
			diagnostic = append(diagnostic, row)
		}
	}
	// Oldest first within a class, so dropping the head drops the oldest.
	if len(trainable) > W {
		trainable = trainable[len(trainable)-W:]
	}
	if len(diagnostic) > W {
		diagnostic = diagnostic[len(diagnostic)-W:]
	}
	w.Observations = append(trainable, diagnostic...)
	w.sortByRecency()
}

func (w *WallObject) sortByRecency() {
	sort.SliceStable(w.Observations, func(i, j int) bool {
		return RingRecencyKeyOf(w.Observations[i]).Less(RingRecencyKeyOf(w.Observations[j]))
	})
}

// TrainableRows selects the fit population. It evaluates exactly ONE predicate
// — `trainable == true` — which is the executable form of §15.1b's "single
// carrier of every in-CI exclusion". Any second predicate here would mean an
// exclusion was being re-derived at fit time instead of materialised at append.
func (w *WallObject) TrainableRows() []WallRingRow {
	out := make([]WallRingRow, 0, len(w.Observations))
	for _, r := range w.Observations {
		if r.Trainable {
			out = append(out, r)
		}
	}
	return out
}

// SummationSort is contract §6.8's deterministic summation order, applied ONLY
// after §15.1b's recency selection. The two are separately observable: recency
// decides WHICH rows are fitted, this decides in what order they are summed.
func SummationSort(rows []WallRingRow) []WallRingRow {
	out := append([]WallRingRow(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.HeadSHA != b.HeadSHA {
			return a.HeadSHA < b.HeadSHA
		}
		if a.RunID != b.RunID {
			return a.RunID < b.RunID
		}
		if a.RunAttempt != b.RunAttempt {
			return a.RunAttempt < b.RunAttempt
		}
		return a.BucketIndex < b.BucketIndex
	})
	return out
}

// DistinctRuns counts distinct (run_id, run_attempt) pairs, which is the
// MIN_RUNS population §7 rule 14 requires.
func DistinctRuns(rows []WallRingRow) int {
	seen := map[[2]string]bool{}
	for _, r := range rows {
		seen[[2]string{r.RunID, r.RunAttempt}] = true
	}
	return len(seen)
}

// QC16 extends QC11's duplicate rejection to the full intrinsic tuple, which is
// what makes the recency key a TOTAL order on any legal ring: two rows may not
// share one intrinsic_id.
func QC16(rows []WallRingRow, candidate WallRingRow) error {
	ck := candidate.IntrinsicID()
	for _, r := range rows {
		if r.IntrinsicID() == ck {
			return fmt.Errorf("QC16: intrinsic_id %v is already present; the recency key would not be a total order", ck)
		}
	}
	return nil
}

// nowRFC3339 renders an instant the way the ring stores one.
func nowRFC3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// QC15 is contract §7 rule 16: an observation is ingested only if `head_sha`
// (orchestration), `candidate_sha` (testbucket build) and `workload_commit`
// (consumer checkout) are three separately present, well-formed values.
//
// A row that collapses them is REJECTED rather than silently overloaded: one
// head_sha standing in for all three would make a candidate change invisible to
// the comparability key and to the campaign's pair invariant alike.
func QC15(r WallRingRow) error {
	for _, f := range []struct {
		name, value string
	}{
		{"head_sha", r.HeadSHA},
		{"candidate_sha", r.CandidateSHA},
		{"workload_commit", r.WorkloadCommit},
	} {
		if f.value == "" {
			return fmt.Errorf("QC15: %s is absent; the three provenance identities are separately required", f.name)
		}
	}
	if r.HeadSHA == r.CandidateSHA && r.CandidateSHA == r.WorkloadCommit {
		return fmt.Errorf("QC15: all three provenance identities are the same value %q; a row that collapses them is rejected", r.HeadSHA)
	}
	if r.Repository == "" || r.RunID == "" || r.RunAttempt == "" || r.JobID == "" {
		return fmt.Errorf("QC15: the intrinsic identity is incomplete, so the recency key would not be reconstructible from the row")
	}
	return nil
}

// THE REGISTERED WIRE FORMAT IS FLAT, and this is where the Go shape and the
// registry meet.
//
// The field registry declares `wall.fixed_ns`, `wall.scale`, `wall.fitted_at`
// and their peers directly under `wall`, with `migrated_from` at the store
// ROOT and `scale` as a STRING. The Go struct groups the fit into `Fit` for
// the same reason the contract groups it in prose — presence is all-or-nothing
// and a group makes that a single nil check — but the group is an internal
// convenience and must not reach the wire.
//
// It did. The store serialized `wall.fit.{...}`, wrote `wall.migrated_from`,
// and emitted `scale` as a JSON number. A consumer reading the registered
// paths found none of them, and `scale` as a number reintroduces exactly the
// float ambiguity the string form exists to remove: the shortest round-trip
// decimal is what makes two readers agree on the value they read.
type wallWire struct {
	ModelVersion           *int               `json:"model_version"`
	ComparabilityKeyDigest string             `json:"comparability_key_digest"`
	Status                 WallStatus         `json:"status"`
	FailureSubtype         WallFailureSubtype `json:"failure_subtype,omitempty"`

	FittedAt                  string   `json:"fitted_at,omitempty"`
	RowsUsed                  *int     `json:"rows_used,omitempty"`
	RunsUsed                  *int     `json:"runs_used,omitempty"`
	FixedNs                   string   `json:"fixed_ns,omitempty"`
	Scale                     string   `json:"scale,omitempty"`
	WholeInvocationOverheadNs string   `json:"whole_invocation_overhead_ns,omitempty"`
	PerSliceOverheadNs        string   `json:"per_slice_overhead_ns,omitempty"`
	ResidualMAENs             string   `json:"residual_mae_ns,omitempty"`
	ResidualP90Ns             string   `json:"residual_p90_ns,omitempty"`
	RankSupport               []string `json:"rank_support,omitempty"`

	Observations []WallRingRow `json:"observations"`
}

// MarshalJSON writes the registered flat surface.
func (w WallObject) MarshalJSON() ([]byte, error) {
	out := wallWire{
		ModelVersion:           w.ModelVersion,
		ComparabilityKeyDigest: w.ComparabilityKeyDigest,
		Status:                 w.Status,
		FailureSubtype:         w.FailureSubtype,
		Observations:           w.Observations,
	}
	if out.Observations == nil {
		// Always present once `wall` exists, and EMPTY rather than absent on a
		// fresh migration: the registry gives it cardinality one.
		out.Observations = []WallRingRow{}
	}
	if f := w.Fit; f != nil {
		rows, runs := f.RowsUsed, f.RunsUsed
		out.FittedAt = f.FittedAt
		out.RowsUsed, out.RunsUsed = &rows, &runs
		out.FixedNs = strconv.FormatInt(f.FixedNs, 10)
		out.Scale = strconv.FormatFloat(f.Scale, 'g', -1, 64)
		out.WholeInvocationOverheadNs = strconv.FormatInt(f.WholeInvocationOverheadNs, 10)
		out.PerSliceOverheadNs = strconv.FormatInt(f.PerSliceOverheadNs, 10)
		out.ResidualMAENs = strconv.FormatInt(f.ResidualMAENs, 10)
		out.ResidualP90Ns = strconv.FormatInt(f.ResidualP90Ns, 10)
		out.RankSupport = f.RankSupport
	}
	return json.Marshal(out)
}

// UnmarshalJSON reads the registered flat surface back into the grouped Go
// shape. The fit group is present exactly when the wire carries a fitted_at,
// which is the leaf whose presence the contract ties the group's to.
func (w *WallObject) UnmarshalJSON(b []byte) error {
	var in wallWire
	if err := json.Unmarshal(b, &in); err != nil {
		return err
	}
	*w = WallObject{
		ModelVersion:           in.ModelVersion,
		ComparabilityKeyDigest: in.ComparabilityKeyDigest,
		Status:                 in.Status,
		FailureSubtype:         in.FailureSubtype,
		Observations:           in.Observations,
	}
	if w.Observations == nil {
		w.Observations = []WallRingRow{}
	}
	if in.FittedAt == "" {
		return nil
	}
	fit := &WallFitGroup{FittedAt: in.FittedAt, RankSupport: in.RankSupport}
	if in.RowsUsed != nil {
		fit.RowsUsed = *in.RowsUsed
	}
	if in.RunsUsed != nil {
		fit.RunsUsed = *in.RunsUsed
	}
	for _, f := range []struct {
		name string
		src  string
		dst  *int64
	}{
		{"fixed_ns", in.FixedNs, &fit.FixedNs},
		{"whole_invocation_overhead_ns", in.WholeInvocationOverheadNs, &fit.WholeInvocationOverheadNs},
		{"per_slice_overhead_ns", in.PerSliceOverheadNs, &fit.PerSliceOverheadNs},
		{"residual_mae_ns", in.ResidualMAENs, &fit.ResidualMAENs},
		{"residual_p90_ns", in.ResidualP90Ns, &fit.ResidualP90Ns},
	} {
		if f.src == "" {
			continue
		}
		v, err := strconv.ParseInt(f.src, 10, 64)
		if err != nil {
			return fmt.Errorf("wall.%s: %w", f.name, err)
		}
		*f.dst = v
	}
	if in.Scale != "" {
		v, err := strconv.ParseFloat(in.Scale, 64)
		if err != nil {
			return fmt.Errorf("wall.scale: %w", err)
		}
		fit.Scale = v
	}
	w.Fit = fit
	return nil
}
