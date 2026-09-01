package walltime

import "fmt"

// AuditEvidence is the outcome of running the exact-run coverage audit over
// the bucket that was measured.
//
// A wall-time ledger records how long something took. It cannot record WHAT
// ran: a script that skipped half its targets produces a shorter, perfectly
// well-formed, fully attested envelope. The frozen scope makes audit failure
// terminal, so a scorable row has to carry the audit's answer, not merely be
// eligible for one to be run later on a branch it will never reach.
type AuditEvidence struct {
	// Bucket is the bucket the audit was scoped to, so a verdict cannot
	// inherit another bucket's coverage.
	Bucket string
	// Planned and Reported are what the plan scheduled for this bucket and
	// what the events show, kept for the report rather than for a gate.
	Planned  int
	Reported int
	// Problems is empty exactly when the run executed what the plan scheduled.
	Problems []string
	// Report is the audit's own human-readable output, echoed into the verdict
	// so a reader is not asked to trust a boolean.
	Report string
}

// AuditFunc runs the coverage audit for one bucket.
//
// It is injected rather than called directly because the audit belongs to the
// planner/adapter layer and this package deliberately imports neither: the
// measurement code must not be able to reach the code it measures. The CLI
// supplies the real implementation; a caller that supplies none gets a
// finding, never a pass.
type AuditFunc func(bucketID string) (*AuditEvidence, error)

// verifyAudit makes exact-run coverage an ELIGIBILITY condition.
//
// Absence is a finding, not a skip. "No audit was supplied" and "the audit
// passed" must never reach the same verdict, because the whole failure this
// closes is a row that was eligible without anyone having asked whether it ran
// the work it was given.
func verifyAudit(v *Verdict, opt VerifyOptions) {
	if opt.Audit == nil {
		v.add("WT-024", SeverityIneligible,
			"no coverage audit was supplied, so nothing checked that this bucket ran the targets, count-shards and name slices the authorised plan gave it; a wall-time envelope measures duration and cannot measure what was skipped")
		return
	}
	ev, err := opt.Audit(v.Run.BucketID)
	if err != nil {
		v.add("WT-024", SeverityIneligible, fmt.Sprintf("coverage audit: %v", err))
		return
	}
	if ev == nil {
		v.add("WT-024", SeverityIneligible, "the coverage audit produced no evidence")
		return
	}
	if ev.Bucket != "" && v.Run.BucketID != "" && ev.Bucket != v.Run.BucketID {
		v.add("WT-024", SeverityIneligible,
			fmt.Sprintf("the coverage audit covers bucket %q but bucket %q was measured", ev.Bucket, v.Run.BucketID))
	}
	v.Audit = ev
	// TERMINAL, not merely ineligible: the frozen scope says a failed audit
	// ends the row. A run that did not execute its plan is not a measurement
	// of that plan under any threshold.
	for _, p := range ev.Problems {
		v.add("WT-024", SeverityTerminal, "coverage audit: "+p)
	}
}
