package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/nsmath"
	"github.com/invakid404/testbucket/internal/walltime"
)

// THE PRODUCTION FEEDBACK LOOP.
//
// `ingest --wall-observations` used to read the rows, print the accept/reject
// table, and stop. Every piece of the loop existed and was tested — the §7.1
// qualification, the ring append, the conditional refit — and nothing in
// production called any of it, so no measured bucket could change a later
// plan. This file is the adapter that closes it: it is what makes `core.Store`
// a `walltime.RingStore` and what turns a qualifying observation into a ring
// row and a fit.

// wallRingStore adapts the timing store to the ring interface §14.2's hop
// writes through.
//
// Qualification happens HERE, inside Append, rather than beside it. The
// contract admits a row only if every applicable QC1…QC17 check passes, so a
// row that fails must never reach the ring at all — and putting the gate at
// the append boundary means there is no path into the ring that skips it.
type wallRingStore struct {
	st   *core.Store
	plan walltime.PlanContext
	ring walltime.RingFacts
	// frozen carries §15.1a's PLAN-FROZEN regressors per bucket. They are the
	// fit's inputs, and they belong to the plan rather than to the row: a
	// value re-derived from the observation is a value the measurement could
	// move.
	frozen map[string]planFrozen
	seq    int64
}

// planFrozen is the regressor triple a fit consumes, taken from the plan the
// bucket was fanned out from.
type planFrozen struct {
	// ReporterSumNs is the sum of the bucket's PLAN-TIME reporter weights. It
	// is not est_seconds: under the wall basis est_seconds is the model's
	// A_eta display, so using it would feed the model its own output.
	ReporterSumNs int64
	// WholeFiles and Slices are the invocation topology the PLAN rendered.
	// Inferring them from the observation's selectors made every invocation
	// whole and slice_count always zero, because the assembler left selectors
	// nil.
	WholeFiles int
	Slices     int
	// StoreSHA256 is the store state the plan was built from, carried in the
	// canonical profile.
	StoreSHA256 string
}

// SelectedIdentities is the SELECTED trainable population, in selection order.
// §14.2 refits if and only if this sequence changes, so it is the ring's own
// answer rather than a count.
func (w *wallRingStore) SelectedIdentities() []string {
	if w.st.Wall == nil {
		return nil
	}
	rows := w.st.Wall.TrainableRows()
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		id := r.IntrinsicID()
		out = append(out, strings.Join(id[:], "\x00"))
	}
	return out
}

// Append qualifies the observation and, only then, admits it to the ring.
func (w *wallRingStore) Append(obs walltime.Observation, trainable bool) error {
	if w.st.Wall == nil {
		return fmt.Errorf("the store carries no wall object; §15.2's migration runs before ingest appends")
	}
	if err := walltime.QualifyObservation(obs, w.plan, w.ring); err != nil {
		return err
	}
	// MARKED BEFORE THE ROW IS WRITTEN. The execution key is claimed the moment
	// this observation qualifies for it, so a second observation carrying it
	// cannot slip in behind a later failure — a key that was qualified for is
	// taken whether or not the row that qualified reached the ring.
	w.ring.SeenObservationKeys[walltime.ObservationKey(obs)] = true
	frozen, ok := w.frozen[obs.BucketName]
	if !ok {
		// Without this the map miss would hand back a zero planFrozen and the
		// row would train on reporter_sum_ns=0 — the silent shape the derived
		// regressors had.
		return fmt.Errorf("bucket %q is not in the shard plan, so its plan-frozen regressors are unknown", obs.BucketName)
	}
	row := ringRowOf(obs, trainable, w.nextSeq(), frozen)
	// QC15 and QC16 are the ring's own admission: the three provenance
	// identities, and a recency key that is a total order against what is
	// already stored.
	if err := core.QC15(row); err != nil {
		return err
	}
	if err := core.QC16(w.st.Wall.Observations, row); err != nil {
		return err
	}
	w.st.Wall.AppendRow(row)

	// The ring facts move with the ring: a second row in this same invocation
	// that duplicates an identity must be refused by the same check that
	// refuses one duplicating a stored row.
	w.ring.SeenIntrinsicIDs[row.IntrinsicID()] = true
	return nil
}

func (w *wallRingStore) nextSeq() int64 {
	w.seq++
	return w.seq
}

// ringRowOf projects an observation onto the ring row §15.1a stores.
//
// Three of the four model inputs — reporter_sum_ns, the whole-file indicator
// and the slice count — are PLAN-FROZEN (§15.1a): they describe the workload
// the bucket was asked to run, and they are read off the one parsed plan, not
// off the observation. Only the elapsed interval is measured. Deriving the
// regressors from the observation instead let the model regress on its own
// output and on selectors the assembler never populated.
func ringRowOf(obs walltime.Observation, trainable bool, seq int64, frozen planFrozen) core.WallRingRow {
	indicator := 0
	if frozen.WholeFiles > 0 {
		indicator = 1
	}
	return core.WallRingRow{
		Repository:     obs.Repository,
		JobID:          obs.JobID,
		HeadSHA:        obs.HeadSHA,
		CandidateSHA:   obs.CandidateSHA,
		WorkloadCommit: obs.WorkloadCommit,
		RunID:          obs.RunID,
		RunAttempt:     obs.RunAttempt,

		ObservedStartRealtime: obs.RealtimeStart,

		Trainable: trainable,
		IngestSeq: seq,

		BucketIndex:            obs.BucketIndex,
		PlanDigest:             string(obs.PlanDigest),
		ComparabilityKeyDigest: string(obs.ComparabilityKeyDigest),

		// THE PLAN'S VALUES, VERBATIM. Every one of these used to be derived
		// from the observation: reporter_sum_ns from est_seconds through a
		// float truncation, the topology from selectors the assembler never
		// set, and store_sha256 not at all.
		ReporterSumNs: frozen.ReporterSumNs,
		IAnyWholeFile: indicator,
		SliceCount:    frozen.Slices,
		StoreSHA256:   frozen.StoreSHA256,

		ElapsedNs: int64(obs.ElapsedNs),

		WholeFileCount:  frozen.WholeFiles,
		InvocationCount: len(obs.Invocations),

		Terminal: obs.Terminal,
	}
}

// wallFitter is §6.8's fit, run over the ring's SELECTED trainable population
// and written back into the store.
//
// It is a closure rather than a method so the hop can count its invocations:
// §14.2 requires the fitter to stay uncalled when the selected population did
// not change, and a call count is the only way to observe that.
func wallFitter(st *core.Store) walltime.Fitter {
	return func() error {
		if st.Wall == nil {
			return fmt.Errorf("refit: the store carries no wall object")
		}
		rows := st.Wall.TrainableRows()
		fitRows := make([]walltime.FitRow, 0, len(rows))
		for _, r := range rows {
			fitRows = append(fitRows, walltime.FitRow{
				ReporterSumNs: r.ReporterSumNs,
				IAnyWholeFile: r.IAnyWholeFile,
				SliceCount:    r.SliceCount,
				ElapsedNs:     r.ElapsedNs,
				HeadSHA:       r.HeadSHA,
				RunID:         r.RunID,
				RunAttempt:    r.RunAttempt,
				BucketIndex:   r.BucketIndex,
			})
		}
		res, err := walltime.FitModel(fitRows)
		if err != nil {
			return err
		}
		st.Wall.Status = core.WallStatus(res.Status)
		st.Wall.FailureSubtype = core.WallFailureSubtype(res.Subtype)
		if res.Status != walltime.FitOK {
			// A model that did not reach rank is recorded as INSUFFICIENT with
			// no coefficients: §15.1c makes presence status-indexed, so an
			// unusable model must not leave a fit group behind for a later
			// reader to pack by.
			st.Wall.Fit = nil
			return nil
		}
		st.Wall.Fit = &core.WallFitGroup{
			FixedNs:                   res.Model.FixedNs,
			Scale:                     res.Model.Scale,
			WholeInvocationOverheadNs: res.Model.WholeInvocationOverheadNs,
			PerSliceOverheadNs:        res.Model.PerSliceOverheadNs,
			FittedAt:                  time.Now().UTC().Format(time.RFC3339),
			RowsUsed:                  res.RowsUsed,
			RunsUsed:                  res.RunsUsed,
			ResidualMAENs:             res.ResidualMAENs,
			ResidualP90Ns:             res.ResidualP90Ns,
			// The columns the design actually supported, named so a reader
			// can see WHY a model was admitted rather than only that it was.
			RankSupport: rankSupportOf(res.Rank),
		}
		return nil
	}
}

// ringFactsOf builds the duplicate-identity facts QC11 and QC16 read, from the
// rows the store already holds.
//
// SeenObservationKeys used to be left EMPTY here, so QC11's uniqueness was
// forgotten the moment the store was saved: two observations of one execution
// key that differed only in job_id — a re-run of the same bucket under a new
// job — had distinct intrinsic ids, passed QC16, and both entered the ring as
// trainable rows, refitting the model twice for one execution.
//
// The row does not store bucket_name; it stores bucket_index, and §15.1a fixes
// that field set. The name is recovered from the PLAN being ingested against,
// which is the same plan those rows were fanned out from whenever the key can
// actually collide: an execution key pins head_sha, run_id and run_attempt, so
// two rows sharing it came from one run and therefore one plan, where name and
// index are a bijection. A row whose index the plan does not name contributes
// no key rather than a guessed one.
func ringFactsOf(st *core.Store, plan walltime.PlanContext) walltime.RingFacts {
	facts := walltime.RingFacts{
		SeenObservationKeys: map[[4]string]bool{},
		SeenIntrinsicIDs:    map[[6]string]bool{},
	}
	if st.Wall == nil {
		return facts
	}
	names := make(map[int]string, len(plan.Buckets))
	for name, ref := range plan.Buckets {
		names[ref.Index] = name
	}
	for _, r := range st.Wall.Observations {
		facts.SeenIntrinsicIDs[r.IntrinsicID()] = true
		if name, ok := names[r.BucketIndex]; ok {
			facts.SeenObservationKeys[[4]string{r.HeadSHA, r.RunID, r.RunAttempt, name}] = true
		}
	}
	return facts
}

// rankSupportOf names the columns §6.6's admission found support for: the four
// model columns minus the ones it reported deficient.
func rankSupportOf(r walltime.RankResult) []string {
	all := []string{"fixed", "scale", "whole_invocation_overhead", "per_slice_overhead"}
	deficient := map[int]bool{}
	for _, c := range r.DeficientColumns {
		deficient[c] = true
	}
	out := make([]string, 0, len(all))
	for i, name := range all {
		// DeficientColumns is 1-based, as the contract's tables index them.
		if deficient[i+1] {
			continue
		}
		out = append(out, name)
	}
	return out
}

// planContextOf builds the §7.1 plan context from the PLAN DOCUMENT.
//
// It reads the plan the run was fanned out from, not the observations being
// qualified. That distinction is the whole value of the gate: QC6 compares the
// recorded invocations against the ones the plan rendered, QC13 against the
// canonical profile the plan carried, QC17 against the runtime profile it
// declared. A context assembled from the rows would make every one of those
// checks compare a row against itself and pass unconditionally.
//
// It takes the ALREADY-PARSED document rather than the path, for the reason
// ParseShardPlan itself gives: a control that re-reads the artifact can have
// each read see a different file, and the key ingest migrates under would then
// be able to come from a plan other than the one the rows are qualified
// against.
func planContextOf(doc *core.PlanDocument, runsOnLabel string, st *core.Store) (walltime.PlanContext, map[string]planFrozen, error) {
	frozen := map[string]planFrozen{}
	ctx := walltime.PlanContext{
		Buckets:             map[string]walltime.PlanBucketRef{},
		CoverageAuditPasses: map[string]bool{},
		PlanDigest:          walltime.Digest(doc.ComparabilityKeyDigest),
	}
	if d, derr := walltime.DigestJSON(doc); derr == nil {
		ctx.PlanDigest = d
	}
	ctx.ComparabilityKeyDigest = walltime.Digest(doc.ComparabilityKeyDigest)
	// The label is the caller's, not the plan's: no context inside a composite
	// action yields the resolved runs-on value, so the plan job is passed it
	// and the record job is passed the same one. QC12 is the comparison that
	// makes the pair meaningful.
	ctx.RunnerImageLabel = runsOnLabel
	ctx.RuntimeProfileDeclaredDigest = walltime.Digest(doc.RuntimeProfileDeclaredDigest)
	ctx.RuntimeProfileDeclared = runtimeProfileFromMap(doc.RuntimeProfileDeclared)
	if len(doc.Profile) > 0 {
		var block walltime.ProfileBlock
		if err := json.Unmarshal(doc.Profile, &block); err == nil {
			ctx.ProfileBlock = block
		}
	}
	storeSHA := ""
	if len(doc.Profile) > 0 {
		var prof struct {
			StoreSHA256 string `json:"store_sha256"`
		}
		if err := json.Unmarshal(doc.Profile, &prof); err == nil {
			storeSHA = prof.StoreSHA256
		}
	}
	for _, b := range doc.Buckets {
		ref := walltime.PlanBucketRef{Index: b.Index}
		// §15.1a's PLAN-FROZEN regressors, computed once from the plan.
		f := planFrozen{StoreSHA256: storeSHA}
		var terms []int64
		for _, u := range b.Units {
			ns, err := core.ReporterNsFromSeconds(u.Seconds)
			if err != nil {
				return walltime.PlanContext{}, nil, fmt.Errorf("bucket %s unit %s: %w", b.Name, u.ID, err)
			}
			terms = append(terms, ns)
		}
		sum, err := nsmath.SumNs("plan_reporter_sum_ns", terms...)
		if err != nil {
			return walltime.PlanContext{}, nil, fmt.Errorf("bucket %s: %w", b.Name, err)
		}
		f.ReporterSumNs = sum
		for _, inv := range b.Invocations {
			if selectorHasNameFilter(inv.Selector) {
				f.Slices++
				continue
			}
			f.WholeFiles++
		}
		frozen[b.Name] = f
		for _, u := range b.Units {
			ref.UnitIDs = append(ref.UnitIDs, u.ID)
		}
		for _, inv := range b.Invocations {
			ref.ArgvDigests = append(ref.ArgvDigests, walltime.DigestJSONOrEmpty(inv.Args))
			// §13.1's cwd identity is the ABSOLUTE executed directory. Hashing
			// the plan's relative string let QC7a pass while the measuring job
			// and this one resolved it under different roots.
			ref.CwdDigests = append(ref.CwdDigests, walltime.DigestJSONOrEmpty(walltime.AbsCwd(inv.Dir)))
		}
		ctx.Buckets[b.Name] = ref
		// The reporter-event coverage verdict is core's, and the record job
		// runs `audit` before it ingests: a bucket that reached here has been
		// audited already, and QC10 reads that verdict rather than
		// re-deriving it from a wall envelope, which can measure how long
		// something took and never what it skipped.
		ctx.CoverageAuditPasses[b.Name] = true
	}
	return ctx, frozen, nil
}

// selectorHasNameFilter reports whether a rendered selector carries a name
// filter, which is what makes an invocation a name SLICE rather than a whole
// file. It reads the PLAN's selector, which is always populated; the
// observation's was not.
func selectorHasNameFilter(selector []string) bool {
	for _, s := range selector {
		if s == "-t" || s == "--testNamePattern" {
			return true
		}
	}
	return false
}

// runtimeProfileFromMap reads the plan's declared runtime object back into the
// typed profile QC17 compares field by field.
func runtimeProfileFromMap(m map[string]string) walltime.RuntimeProfile {
	return walltime.RuntimeProfile{
		NodeVersion:         m["node_version"],
		PnpmVersion:         m["pnpm_version"],
		VitestVersion:       m["vitest_version"],
		TestbucketSHA256:    m["testbucket_sha256"],
		FacadeCommand:       m["facade_command"],
		LockSHA256:          m["lock_sha256"],
		DependencyCacheMode: m["dependency_cache_mode"],
	}
}
