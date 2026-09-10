package walltime

import (
	"fmt"
	"github.com/invakid404/testbucket/internal/nsmath"
	"time"
)

// PlanBucketRef is what the record job knows about one bucket of the shard plan
// it read once.
type PlanBucketRef struct {
	Index       int
	UnitIDs     []string
	ArgvDigests []Digest
	CwdDigests  []Digest
	// EstSeconds is the estimate the plan DISPLAYED for this bucket, and
	// AEtaNs the objective it was optimized against, present iff the plan was
	// built under the wall basis.
	//
	// Neither was carried before, so no check could compare an observation's
	// estimate against the plan's — the row echoed a number and the gate took
	// its word for it. QC18 below is what makes the echo checkable.
	EstSeconds float64
	AEtaNs     *Nanos
}

// PlanContext is the plan-side half of the qualification checks: the shard plan
// the record job read ONCE (QC3), plus the plan document's canonical profile
// block and declared runtime profile.
type PlanContext struct {
	PlanDigest                   Digest
	Buckets                      map[string]PlanBucketRef
	ProfileBlock                 ProfileBlock
	ComparabilityKeyDigest       Digest
	RunnerImageLabel             string
	RuntimeProfileDeclared       RuntimeProfile
	RuntimeProfileDeclaredDigest Digest
	// CoverageAuditPasses is the reporter-event coverage verdict for the
	// bucket (QC10). It is supplied because the audit is core's, not this
	// package's: a wall-time envelope records how long something took and can
	// never record what it skipped.
	CoverageAuditPasses map[string]bool
}

// RingFacts is the ring-side half: what is already present, so QC11 and QC16
// can reject a duplicate without this package depending on the store layout.
type RingFacts struct {
	// SeenObservationKeys holds (head_sha, run_id, run_attempt, bucket_name).
	SeenObservationKeys map[[4]string]bool
	// SeenIntrinsicIDs holds the full §15.1b tuple.
	SeenIntrinsicIDs map[[6]string]bool
}

// ObservationKey is QC11's uniqueness tuple.
func ObservationKey(o Observation) [4]string {
	return [4]string{o.HeadSHA, o.RunID, o.RunAttempt, o.BucketName}
}

// IntrinsicID is QC16's tuple, and it is a function of the row's own immutable
// bytes so the recency key is a total order under every arrival permutation.
func IntrinsicID(o Observation) [6]string {
	return [6]string{o.Repository, o.RunID, o.RunAttempt, o.JobID,
		fmt.Sprintf("%d", o.BucketIndex), string(o.PlanDigest)}
}

// QualifyObservation runs contract §7.1's QC1…QC17 in order and returns the
// FIRST failure, named by check. An observation is ingested only if every
// applicable check passes.
//
// The checks run in table order because several depend on earlier ones: QC4
// needs the bucket to resolve before QC5 can compare its unit set, and QC17
// compares against a plan document QC3 has already bound.
func QualifyObservation(o Observation, plan PlanContext, ring RingFacts) error {
	// QC1 — schema recognised.
	if o.Schema != ObservationSchema {
		return fmt.Errorf("QC1: schema %q is not %q", o.Schema, ObservationSchema)
	}

	// QC2 — the required identity and admission fields, non-empty. est_basis
	// is read from the PROFILE object; there is no top-level field.
	prof, err := o.Profile.Parse()
	if err != nil {
		return fmt.Errorf("QC2: profile does not parse: %w", err)
	}
	for _, f := range []struct{ name, value string }{
		{"repository", o.Repository},
		{"head_sha", o.HeadSHA},
		{"run_id", o.RunID},
		{"run_attempt", o.RunAttempt},
		{"job_id", o.JobID},
		{"bucket_name", o.BucketName},
		{"plan_digest", string(o.PlanDigest)},
		{"comparability_key_digest", string(o.ComparabilityKeyDigest)},
		{"profile.est_basis", string(prof.EstBasis)},
	} {
		if f.value == "" {
			return fmt.Errorf("QC2: %s is absent or empty", f.name)
		}
	}

	// QC3 — plan_digest equals the digest of the shard plan this record job
	// read once.
	if o.PlanDigest != plan.PlanDigest {
		return fmt.Errorf("QC3: plan_digest %s does not equal the shard plan the record job read (%s)",
			o.PlanDigest, plan.PlanDigest)
	}

	// QC4 — bucket_name resolves in that plan and bucket_index agrees.
	ref, ok := plan.Buckets[o.BucketName]
	if !ok {
		return fmt.Errorf("QC4: bucket_name %q does not resolve in the plan", o.BucketName)
	}
	if ref.Index != o.BucketIndex {
		return fmt.Errorf("QC4: bucket_index %d disagrees with the plan's %d for %q",
			o.BucketIndex, ref.Index, o.BucketName)
	}

	// QC5 — unit_ids equals the plan's unit set for that bucket, EXACTLY.
	if !sameStringSlice(o.UnitIDs, ref.UnitIDs) {
		return fmt.Errorf("QC5: unit_ids %v is not exactly the plan's unit set %v", o.UnitIDs, ref.UnitIDs)
	}

	// QC6 — invocation membership and order equal the plan's rendered
	// invocations; each argv_digest matches.
	if len(o.Invocations) != len(ref.ArgvDigests) {
		return fmt.Errorf("QC6: %d invocations recorded, the plan rendered %d",
			len(o.Invocations), len(ref.ArgvDigests))
	}
	for i, inv := range o.Invocations {
		if inv.Seq != i {
			return fmt.Errorf("QC6: invocation %d carries seq %d; membership and order must equal the plan's", i, inv.Seq)
		}
		if inv.ArgvDigest != ref.ArgvDigests[i] {
			return fmt.Errorf("QC6: invocation %d argv_digest %s does not match the plan's %s",
				i, inv.ArgvDigest, ref.ArgvDigests[i])
		}
	}

	// QC7 — §3.1's interval invariants; no endpoint copied between records.
	if err := intervalInvariants(o); err != nil {
		return err
	}

	// QC7a — cwd_digest matches and process_group_id is well-formed.
	if o.ProcessGroupID == "" {
		return fmt.Errorf("QC7a: process_group_id is absent")
	}
	for i, inv := range o.Invocations {
		if i < len(ref.CwdDigests) && inv.CwdDigest != ref.CwdDigests[i] {
			return fmt.Errorf("QC7a: invocation %d cwd_digest %s does not match the plan's %s",
				i, inv.CwdDigest, ref.CwdDigests[i])
		}
		if inv.ProcessGroupID == "" {
			return fmt.Errorf("QC7a: invocation %d has no process_group_id", i)
		}
	}

	// QC8 — boot identity unchanged across the interval. A change means the
	// two monotonic readings are not on one timeline.
	if o.BootIDStart != o.BootIDEnd {
		return fmt.Errorf("QC8: boot_id_start %q != boot_id_end %q; the endpoints are not on one timeline",
			o.BootIDStart, o.BootIDEnd)
	}

	// QC9 — terminal and every exit code.
	if o.Terminal != "passed" {
		return fmt.Errorf("QC9: terminal is %q, not passed", o.Terminal)
	}
	if o.ExitCode != 0 {
		return fmt.Errorf("QC9: exit_code is %d, not 0", o.ExitCode)
	}
	for i, inv := range o.Invocations {
		if inv.ExitCode != 0 {
			return fmt.Errorf("QC9: invocation %d exit_code is %d, not 0", i, inv.ExitCode)
		}
	}

	// QC10 — the reporter-event coverage audit for that bucket.
	if plan.CoverageAuditPasses != nil && !plan.CoverageAuditPasses[o.BucketName] {
		return fmt.Errorf("QC10: the reporter-event coverage audit failed for bucket %q", o.BucketName)
	}

	// QC11 — exactly one observation per (head_sha, run_id, run_attempt,
	// bucket_name); a duplicate rejects ALL rows for the key.
	if ring.SeenObservationKeys[ObservationKey(o)] {
		return fmt.Errorf("QC11: a duplicate observation exists for %v; a duplicate rejects ALL rows for the key",
			ObservationKey(o))
	}

	// QC12 — comparability agreement, the label, and file parallelism.
	if o.ComparabilityKeyDigest != plan.ComparabilityKeyDigest {
		return fmt.Errorf("QC12: comparability_key_digest %s disagrees with the plan document's %s",
			o.ComparabilityKeyDigest, plan.ComparabilityKeyDigest)
	}
	if err := QC12(plan.RunnerImageLabel, o.ObservedRunsOnLabel, prof.FileParallelism); err != nil {
		return err
	}

	// QC13 — the profile block byte-identical to the plan's. Mandatory for
	// every wall-basis observation and every campaign row, and in no optional
	// list.
	if err := QC13(plan.ProfileBlock, o.Profile); err != nil {
		return err
	}

	// QC14b — the transferable half of the cache contract. The on-runner half
	// (QC14a) already ran inside run-bucket, before upload.
	if prof.Scored {
		if err := QC14b(o.CacheState); err != nil {
			return err
		}
	}

	// QC15 — three separately present, well-formed, DISTINCT identity fields.
	if err := qc15Observation(o); err != nil {
		return err
	}

	// QC17 — the executed runtime profile against the plan's declared one.
	if err := QC17(plan.RuntimeProfileDeclared, o.RuntimeProfile,
		plan.RuntimeProfileDeclaredDigest, o.RuntimeProfileDigest); err != nil {
		return err
	}

	// QC18 — the row's estimate is the PLAN'S estimate.
	//
	// §5.1 makes est_seconds and a_eta_ns echoes of what the plan decided, and
	// nothing compared them: an observation could report any objective at all
	// so long as its own two fields agreed with each other, which they do by
	// construction because the assembler derives one from the other. An echo
	// nobody checks is a field, not evidence.
	if ref, ok := plan.Buckets[o.BucketName]; ok {
		switch {
		case ref.AEtaNs == nil && o.AEtaNs != nil:
			return fmt.Errorf("QC18: the row reports a_eta_ns %d for a bucket the plan optimized no objective for",
				int64(*o.AEtaNs))
		case ref.AEtaNs != nil && o.AEtaNs == nil:
			return fmt.Errorf("QC18: the plan optimized a_eta_ns %d for %s and the row reports none",
				int64(*ref.AEtaNs), o.BucketName)
		case ref.AEtaNs != nil && int64(*o.AEtaNs) != int64(*ref.AEtaNs):
			return fmt.Errorf("QC18: the row reports a_eta_ns %d, the plan optimized %d",
				int64(*o.AEtaNs), int64(*ref.AEtaNs))
		}
		if ref.EstSeconds != 0 && o.EstSeconds != ref.EstSeconds {
			return fmt.Errorf("QC18: the row displays est_seconds %v, the plan displayed %v",
				o.EstSeconds, ref.EstSeconds)
		}
	}

	// QC16 — the recency stamp parses, and the intrinsic id is not already in
	// the ring, so the recency key is a total order on any legal ring.
	if o.RealtimeStart == "" {
		return fmt.Errorf("QC16: realtime_start is absent; it is never defaulted")
	}
	if _, err := time.Parse(time.RFC3339, o.RealtimeStart); err != nil {
		return fmt.Errorf("QC16: realtime_start %q does not parse as an RFC 3339 UTC instant", o.RealtimeStart)
	}
	if ring.SeenIntrinsicIDs[IntrinsicID(o)] {
		return fmt.Errorf("QC16: intrinsic_id %v is already present in the ring; the recency key would not be a total order",
			IntrinsicID(o))
	}
	return nil
}

// intervalInvariants is QC7: contract §3.1's invariants, in the checked domain.
//
//	A >= setup_ns + script_ns
//	script_ns >= Σ_j V[j]
//	every value >= 0
func intervalInvariants(o Observation) error {
	vals := map[string]int64{
		"elapsed_ns":         int64(o.ElapsedNs),
		"setup_ns":           int64(o.SetupNs),
		"script_ns":          int64(o.ScriptNs),
		"script_overhead_ns": int64(o.ScriptOverheadNs),
		"wrapper_ns":         int64(o.WrapperNs),
	}
	for name, v := range vals {
		if v < 0 {
			return fmt.Errorf("QC7: %s is %d; every interval value must be >= 0", name, v)
		}
	}
	// THE SUMS ARE CHECKED, because an overflow here turns an IMPOSSIBLE
	// observation into one that passes. `setup_ns + script_ns` wrapping past
	// MaxInt64 yields a negative left-hand side, and `A < negative` is false —
	// so the row satisfies §3.1's floor by arithmetic accident. The whole
	// point of the checked domain is that a value which cannot be represented
	// is a named failure, never a smaller number.
	floor, err := nsmath.SumNs("setup_ns_plus_script_ns", int64(o.SetupNs), int64(o.ScriptNs))
	if err != nil {
		return fmt.Errorf("QC7: %w", err)
	}
	if int64(o.ElapsedNs) < floor {
		return fmt.Errorf("QC7: A (%d) < setup_ns (%d) + script_ns (%d)",
			o.ElapsedNs, o.SetupNs, o.ScriptNs)
	}
	terms := make([]int64, 0, len(o.Invocations))
	for i, inv := range o.Invocations {
		if int64(inv.ElapsedNs) < 0 {
			return fmt.Errorf("QC7: invocation %d elapsed_ns is negative", i)
		}
		if int64(inv.EndedMonoNs) < int64(inv.StartedMonoNs) {
			return fmt.Errorf("QC7: invocation %d ends before it starts", i)
		}
		terms = append(terms, int64(inv.ElapsedNs))
	}
	sumV, err := nsmath.SumNs("sum_invocation_elapsed_ns", terms...)
	if err != nil {
		return fmt.Errorf("QC7: %w", err)
	}
	if int64(o.ScriptNs) < sumV {
		return fmt.Errorf("QC7: script_ns (%d) < the sum of invocation intervals (%d)", o.ScriptNs, sumV)
	}
	if int64(o.EndedMonoNs) < int64(o.StartedMonoNs) {
		return fmt.Errorf("QC7: the closing reading precedes the opening one")
	}
	// No endpoint copied between records: an invocation may not share the
	// envelope's own endpoints, which is what a copied value looks like.
	for i, inv := range o.Invocations {
		if inv.StartedMonoNs == o.StartedMonoNs && inv.EndedMonoNs == o.EndedMonoNs {
			return fmt.Errorf("QC7: invocation %d reuses the envelope's endpoints; no endpoint is copied between records", i)
		}
	}
	return nil
}

// qc15Observation is QC15 over an observation: the three provenance identities
// are separately present, well-formed and DISTINCT. A row carrying one value in
// all three, or omitting one, is rejected rather than silently overloaded.
func qc15Observation(o Observation) error {
	for _, f := range []struct{ name, value string }{
		{"head_sha", o.HeadSHA},
		{"candidate_sha", o.CandidateSHA},
		{"workload_commit", o.WorkloadCommit},
	} {
		if f.value == "" {
			return fmt.Errorf("QC15: %s is absent; the three provenance identities are separately required", f.name)
		}
	}
	if o.HeadSHA == o.CandidateSHA && o.CandidateSHA == o.WorkloadCommit {
		return fmt.Errorf("QC15: all three provenance identities carry %q; a row that collapses them is rejected rather than silently overloaded",
			o.HeadSHA)
	}
	return nil
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// RejectDuplicateKeys implements QC11's "a duplicate rejects ALL" rule: when
// two observations share (head_sha, run_id, run_attempt, bucket_name), EVERY
// row for that key is rejected, not merely the later arrival.
//
// Rejecting only the second would let arrival order decide which of two
// contradictory measurements entered history, and there is no ground for
// preferring either.
func RejectDuplicateKeys(obs []Observation) []Observation {
	counts := map[[4]string]int{}
	for _, o := range obs {
		counts[ObservationKey(o)]++
	}
	var out []Observation
	for _, o := range obs {
		if counts[ObservationKey(o)] > 1 {
			out = append(out, o)
		}
	}
	return out
}
