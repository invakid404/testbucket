package walltime

import (
	"fmt"
	"strconv"
	"time"

	"github.com/invakid404/testbucket/internal/nsmath"
)

// PlanBucketRef is what the record job knows about one bucket of the shard plan
// it read once.
type PlanBucketRef struct {
	Index       int
	UnitIDs     []string
	ArgvDigests []Digest
	CwdDigests  []Digest
	// SelectorDigests, UnitDigests and AtomDigests are the per-invocation
	// selection identities DERIVED FROM THE PLAN, one per rendered invocation.
	//
	// Without them QC6 could compare only the invocation count, the sequence and
	// the argv digest — so a row with a correct bucket-wide unit_ids and correct
	// argv digests, but wrong or entirely absent per-invocation membership, passed
	// the check whose stated subject is membership.
	SelectorDigests []Digest
	UnitDigests     []Digest
	AtomDigests     []Digest
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
	// CacheDeclarationDigest is the digest of the §10.5.0 declaration the PLAN
	// job validated against its expected digest (§10.5.2 step 0b). QC14b
	// requirement 3 compares the row's declaration against it; without it the
	// requirement was unimplementable, and an internally coherent row naming a
	// different exact key passed.
	CacheDeclarationDigest Digest
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

// QualifyObservation runs contract §7.1's QC1…QC18 in order and returns the
// FIRST failure, named by check. An observation is ingested only if every
// applicable check passes.
//
// The checks run in table order because several depend on earlier ones: QC4
// needs the bucket to resolve before QC5 can compare its unit set, QC17
// compares against a plan document QC3 has already bound, and QC18 compares
// against the bucket QC4 resolved.
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
		// MEMBERSHIP, which is what QC6 is named for. The argv digest says which
		// command ran; these say which SELECTION it carried — the unit set, the
		// selector including any name filter, and the atoms co-scheduled with it.
		// Two slices of one file differ in exactly the selector and in nothing
		// else an argv comparison can see.
		for _, d := range []struct {
			what             string
			observed, wanted Digest
			plan             []Digest
		}{
			{"selector_digest", inv.SelectorDigest, digestAt(ref.SelectorDigests, i), ref.SelectorDigests},
			{"unit_digest", inv.UnitDigest, digestAt(ref.UnitDigests, i), ref.UnitDigests},
			{"atom_digest", inv.AtomDigest, digestAt(ref.AtomDigests, i), ref.AtomDigests},
		} {
			if len(d.plan) == 0 {
				// The plan carried no such identity for ANY invocation, so there
				// is nothing to compare against. That is a plan-context defect
				// rather than a row defect, and it must not read as a pass.
				return fmt.Errorf("QC6: the plan context carries no %s for bucket %q, so invocation membership cannot be compared",
					d.what, o.BucketName)
			}
			if d.observed != d.wanted {
				return fmt.Errorf("QC6: invocation %d %s %s does not match the plan's %s; the measured selection is not the one the plan rendered",
					i, d.what, emptyDigestName(d.observed), emptyDigestName(d.wanted))
			}
		}

		// AND THE CARRIED LISTS ARE THE VALUES THOSE DIGESTS NAME.
		//
		// The three digests above are now compared against the plan, which proves
		// what the invocation SELECTED. The row also carries the lists themselves
		// — `units`, `selector`, `atoms` — and nothing proved they are the lists
		// the digests stand for. An imported document could set `units: null`,
		// `selector: ["wrong.test.ts"]` or `atoms: ["wrong-atom"]` with every
		// digest intact and be admitted: the audit surface §18.0 names would then
		// attribute the interval to a selection the row itself contradicts.
		//
		// Copying the lists from the plan in the assembler is not a check. It
		// makes the shipped path consistent and says nothing about bytes that
		// arrive from anywhere else, which is the whole reason ingest has a
		// qualifier rather than a trust boundary.
		//
		// The digest is recomputed with DigestJSONOrEmpty — the SAME function both
		// the wrapper and the plan side use, including its empty-list convention —
		// so a legitimately empty list and its empty digest agree, and a nil list
		// under a non-empty digest does not.
		for _, l := range []struct {
			what   string
			list   []string
			digest Digest
		}{
			{"units", inv.Units, inv.UnitDigest},
			{"selector", inv.Selector, inv.SelectorDigest},
			{"atoms", inv.Atoms, inv.AtomDigest},
		} {
			if got := DigestJSONOrEmpty(l.list); got != l.digest {
				return fmt.Errorf("QC6: invocation %d carries %s %v, which digests to %s, and its %s_digest is %s; "+
					"the list and the digest that names it must be one value",
					i, l.what, l.list, emptyDigestName(got), l.what, emptyDigestName(l.digest))
			}
		}
	}

	// QC7 — §3.1's interval invariants; no endpoint copied between records.
	if err := intervalInvariants(o); err != nil {
		return err
	}

	// QC7a — cwd_digest matches and process_group_id is WELL-FORMED.
	//
	// §13.1 says well-formed means "a positive integer within the platform's PID
	// range — not merely non-empty", and non-empty was all this checked. `0` and
	// `abc` both passed; `0` is the worse of the two, because in every
	// negative-PGID signal API it names the CALLER's group rather than a bucket's.
	if err := wellFormedPGID("process_group_id", o.ProcessGroupID); err != nil {
		return err
	}
	for i, inv := range o.Invocations {
		if i < len(ref.CwdDigests) && inv.CwdDigest != ref.CwdDigests[i] {
			return fmt.Errorf("QC7a: invocation %d cwd_digest %s does not match the plan's %s",
				i, inv.CwdDigest, ref.CwdDigests[i])
		}
		if err := wellFormedPGID(fmt.Sprintf("invocation %d process_group_id", i), inv.ProcessGroupID); err != nil {
			return err
		}
	}

	// QC8 — the endpoints are two readings of ONE monotonic timeline: the same
	// boot identity, and a clock domain that may delimit a scored interval.
	//
	// Only the boot identity was compared. The non-Linux backend reads the host
	// REALTIME clock under an honest name and a fixed non-empty boot marker, so
	// its two readings always carry the same marker and QC8 passed — and assembly
	// had already dropped clock_id, so nothing else downstream could tell the
	// difference. A row from a backend explicitly intended to be diagnostic could
	// qualify and train. An NTP step moves that clock; §13.1's interval is
	// CLOCK_MONOTONIC's or it is not an interval.
	if o.BootIDStart != o.BootIDEnd {
		return fmt.Errorf("QC8: boot_id_start %q != boot_id_end %q; the endpoints are not on one timeline",
			o.BootIDStart, o.BootIDEnd)
	}
	// A SCORED ROW IS REFUSED OUTRIGHT; an ordinary one is admitted as the
	// diagnostic it is and kept out of the fit by §15.1b's trainable decision
	// (see TrainableAtAppend). The distinction is the contract's own: a scored
	// campaign arm measured on a clock that cannot delimit a scored interval is a
	// miswiring, while a developer's local run on a platform without a raw
	// monotonic clock is exactly what the fallback exists for.
	if prof.Scored && o.ClockID != ClockMonotonic {
		return fmt.Errorf("QC8: a scored row's endpoints were read from clock %q; §13.1 admits %s only",
			o.ClockID, ClockMonotonic)
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
	//
	// AN ABSENT VERDICT IS A REFUSAL. The guard was `CoverageAuditPasses != nil
	// && !passes[name]`, so a nil map — or a map that simply had no entry for this
	// bucket — admitted the row. "Nobody checked" and "it passed" must never reach
	// the same conclusion, and §14 makes an absent prerequisite a finding rather
	// than a skip.
	pass, have := plan.CoverageAuditPasses[o.BucketName]
	switch {
	case !have:
		return fmt.Errorf("QC10: no reporter-event coverage verdict was supplied for bucket %q; an absent audit is not a passing one",
			o.BucketName)
	case !pass:
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
		// §10.5.6 REQUIREMENT 3, which had no implementation: the declaration
		// leaves must be the digest-verified declaration the plan validated.
		if err := QC14bAgainstPlan(o.CacheState, o.CacheDeclarationDigest, plan.CacheDeclarationDigest); err != nil {
			return err
		}
	}

	// QC15 — three separately present, well-formed identity fields, distinct
	// unless the plan declared a same-repository workload.
	if err := qc15Observation(o, prof); err != nil {
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
	//
	// `ref` is QC4's — the bucket it resolved, which is why reaching here at
	// all means the plan has one. An additional lookup guarded by `ok` read as
	// though QC18 were conditional on the bucket existing, and it is not.
	//
	// THE DISPLAYED ESTIMATE IS COMPARED UNCONDITIONALLY, zero included.
	// Guarding the comparison on `ref.EstSeconds != 0` made zero an absence
	// sentinel on a field that has no presence bit and is always assigned from
	// the parsed plan bucket — so a bucket the plan honestly displayed `0` for,
	// which §17.3a admits as a legitimate design row, would accept a row
	// displaying any estimate whatever. Absence of a displayed estimate is not
	// representable here because a plan bucket always has one; zero is a value.
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
	if o.EstSeconds != ref.EstSeconds {
		return fmt.Errorf("QC18: the row displays est_seconds %v, the plan displayed %v",
			o.EstSeconds, ref.EstSeconds)
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

	// EVERY DERIVED SPAN IS THE SPAN ITS OWN ENDPOINTS DESCRIBE.
	//
	// The checks above are bounds and orderings: nonnegative, A >= setup+script,
	// script >= ΣV, each pair ordered. None of them compares a DURATION against
	// the two readings it was supposedly computed from — so a row could report
	// endpoints 1000 and 2000 and an elapsed_ns of anything at all, including a
	// value that satisfies every bound. §3.1's terms are derived quantities, and
	// a derived quantity nobody recomputes is a claim.
	//
	// The residual identities are the same shape. `wrapper_ns` is what the
	// envelope holds beyond setup and script, and `script_overhead_ns` what the
	// script holds beyond its invocations; both were carried and never checked.
	//
	// The arithmetic is nsmath's throughout: a difference that cannot be
	// represented is a NAMED failure, never a smaller number.
	span, err := nsmath.SumNs("envelope_span", int64(o.EndedMonoNs), -int64(o.StartedMonoNs))
	if err != nil {
		return fmt.Errorf("QC7: %w", err)
	}
	if int64(o.ElapsedNs) != span {
		return fmt.Errorf("QC7: elapsed_ns is %d and its endpoints span %d (%d..%d); a derived duration is the difference of the readings it came from",
			o.ElapsedNs, span, o.StartedMonoNs, o.EndedMonoNs)
	}
	for i, inv := range o.Invocations {
		got, err := nsmath.SumNs("invocation_span", int64(inv.EndedMonoNs), -int64(inv.StartedMonoNs))
		if err != nil {
			return fmt.Errorf("QC7: invocation %d: %w", i, err)
		}
		if int64(inv.ElapsedNs) != got {
			return fmt.Errorf("QC7: invocation %d elapsed_ns is %d and its endpoints span %d (%d..%d)",
				i, inv.ElapsedNs, got, inv.StartedMonoNs, inv.EndedMonoNs)
		}
	}
	wrapper, err := nsmath.SumNs("wrapper_residual", int64(o.ElapsedNs), -int64(o.SetupNs), -int64(o.ScriptNs))
	if err != nil {
		return fmt.Errorf("QC7: %w", err)
	}
	if int64(o.WrapperNs) != wrapper {
		return fmt.Errorf("QC7: wrapper_ns is %d and A - setup_ns - script_ns is %d; the residual is what the envelope holds beyond its two named spans",
			o.WrapperNs, wrapper)
	}
	overhead, err := nsmath.SumNs("script_overhead_residual", int64(o.ScriptNs), -sumV)
	if err != nil {
		return fmt.Errorf("QC7: %w", err)
	}
	if int64(o.ScriptOverheadNs) != overhead {
		return fmt.Errorf("QC7: script_overhead_ns is %d and script_ns - the sum of invocation intervals is %d; the residual is what the script holds beyond the calls it made",
			o.ScriptOverheadNs, overhead)
	}
	return nil
}

// CommitSHALen is the declared domain of §13's three provenance identities:
// 40 lowercase hex characters, which is what the record schema spells for
// head_sha, candidate_sha and workload_commit.
const CommitSHALen = 40

// wellFormedCommitSHA checks the DECLARED DOMAIN, not merely presence.
//
// QC15 compared three non-empty strings and their equality, so `a`, `b`, `c`
// passed it — three distinct values, none of them a commit. Presence and
// distinctness are the properties QC15 was written for; they are not the
// property that makes a retained row auditable later, which is that the identity
// can be resolved in the repository it names.
func wellFormedCommitSHA(name, v string) error {
	if v == "" {
		return fmt.Errorf("QC15: %s is absent; the three provenance identities are separately required", name)
	}
	if len(v) != CommitSHALen {
		return fmt.Errorf("QC15: %s is %d characters; §13 declares %d hex characters",
			name, len(v), CommitSHALen)
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("QC15: %s carries %q at offset %d; §13 declares lowercase hex",
				name, string(c), i)
		}
	}
	return nil
}

// wellFormedPGID is §13.1's "positive integer within the platform's PID range".
//
// The ceiling is the largest value any supported platform can allocate — Linux's
// own /proc/sys/kernel/pid_max maximum; Darwin's limit is far lower. It is an
// upper bound on a SYNTAX check, not a claim about this machine: the field
// records what the testbucket process observed, and a value outside every
// platform's range was never observed anywhere.
const maxPlatformPID = 1 << 22

func wellFormedPGID(name, v string) error {
	if v == "" {
		return fmt.Errorf("QC7a: %s is absent", name)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("QC7a: %s is %q, not an integer; §13.1 declares a positive integer in the platform's PID range", name, v)
	}
	if n < 1 || n > maxPlatformPID {
		return fmt.Errorf("QC7a: %s is %d, outside the platform's PID range (1..%d); 0 names the caller's own group, never a bucket's",
			name, n, maxPlatformPID)
	}
	return nil
}

// qc15Observation is QC15 over an observation: the three provenance identities
// are separately present, well-formed, and distinct UNLESS the plan declared the
// workload to be the orchestration checkout. Omitting one is always a rejection;
// so is collapsing all three, except for that one declared, unscored shape.
func qc15Observation(o Observation, prof CanonicalProfile) error {
	for _, f := range []struct{ name, value string }{
		{"head_sha", o.HeadSHA},
		{"candidate_sha", o.CandidateSHA},
		{"workload_commit", o.WorkloadCommit},
	} {
		if err := wellFormedCommitSHA(f.name, f.value); err != nil {
			return err
		}
	}
	if o.HeadSHA != o.CandidateSHA || o.CandidateSHA != o.WorkloadCommit {
		// Three values, at least two of them different: nothing is collapsed and
		// the rule has nothing to say. Partial equality has always been legal —
		// a `local` build's source can equal the orchestration head while the
		// workload is somewhere else — and still is.
		return nil
	}

	// ALL THREE ARE ONE VALUE. Whether that is a defect depends on whether the
	// three identities genuinely are one commit, and only the PLAN can say so.
	//
	// S-6's defect was one `head_sha` overloaded into three fields, and for a
	// campaign arm or an external-consumer run that is still exactly what this
	// shape means: the arm measures a pinned external workload with a pinned
	// binary, so its three identities are separately bound by construction.
	//
	// A SAME-REPOSITORY DOGFOOD IS NOT THAT DEFECT. When the project runs its own
	// suite from its own checkout with a `local` build, the orchestration head,
	// the source that build compiled and the workload checkout ARE one commit.
	// There is no truthful distinct value to put in the other two fields, and
	// inventing one would be the overloading this rule exists to stop, written
	// backwards. So the equality is admitted exactly when the plan declared the
	// workload to be the orchestration checkout, and the row is NOT scored.
	switch {
	case prof.Scored:
		return fmt.Errorf("QC15: a scored row carries %q in all three provenance identities; "+
			"a scored arm's identities are separately bound and the same-repository carve-out is not available to it",
			o.HeadSHA)
	case !prof.SameRepositoryWorkload:
		return fmt.Errorf("QC15: all three provenance identities carry %q and the plan declares no same-repository workload; "+
			"a row that collapses them is rejected rather than silently overloaded",
			o.HeadSHA)
	case !prof.ScoredIsExplicit():
		// EXPLICITLY UNSCORED, which is what the carve-out is for.
		//
		// Go decodes an absent `scored` and a `scored: null` into the same `false`
		// a present `false` produces, so a block that simply does not say would
		// have collected an exception reserved for a declared shape — and QC13's
		// byte-identity cannot close that, because it only makes the plan and the
		// row agree, and two blocks agree trivially on a field neither contains.
		// "Not scored" and "does not say" are different facts.
		return fmt.Errorf("QC15: all three provenance identities carry %q under a same-repository declaration, "+
			"but profile.scored is absent or null rather than an explicit JSON false; the carve-out is for an "+
			"explicitly unscored row and a block that does not say is not one",
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

// digestAt reads one per-invocation digest, returning the empty digest past the
// end rather than panicking: a row with more invocations than the plan rendered
// is already refused by QC6's count check, and a helper that panics would turn a
// refusal into a crash.
func digestAt(ds []Digest, i int) Digest {
	if i < len(ds) {
		return ds[i]
	}
	return ""
}

// emptyDigestName renders an absent digest readably, so a failure message
// distinguishes "the row carried none" from "the row carried a different one".
func emptyDigestName(d Digest) string {
	if d == "" {
		return "(absent)"
	}
	return string(d)
}
