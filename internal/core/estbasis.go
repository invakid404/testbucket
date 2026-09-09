package core

import (
	"errors"
	"fmt"
)

// EstBasis names the weight the partition was built from. Contract §16.1 makes
// `reporter` the default and requires the plan document and every matrix entry
// to carry the value.
type EstBasis string

const (
	BasisReporter EstBasis = "reporter"
	BasisWall     EstBasis = "wall"
)

func (b EstBasis) Valid() bool { return b == BasisReporter || b == BasisWall }

// WallStatus is the stored model's status (§15.1). It is read, never defaulted:
// contract §7 rule 1 makes an explicit wall plan a hard error under every
// status but `ok`.
type WallStatus string

const (
	WallStatusOK           WallStatus = "ok"
	WallStatusDegraded     WallStatus = "degraded"
	WallStatusInsufficient WallStatus = "insufficient"
	// WallStatusAbsent is the in-memory marker for "the store carries no wall
	// object at all". It is not a serialized value.
	WallStatusAbsent WallStatus = ""
)

// ErrWallModelUnusable is contract §0.8 outcome (c): an explicit
// `--est-basis wall` request that cannot be served because the model is
// missing, `insufficient` or `degraded`. It is a hard error with no matrix, for
// every failure subtype, both scored and unscored.
var ErrWallModelUnusable = errors.New("wall model unusable: --est-basis wall requires a fitted model with status ok")

// ErrScoredColdPlan is contract §0.8 outcome (d): the phase-2 veto. A scored
// run rejects any cold plan or forced fallback regardless of what phase 1
// chose.
var ErrScoredColdPlan = errors.New("scored run rejects a cold plan or a forced fallback")

// BasisRequest is what the caller asked for. `Explicit` distinguishes an
// omitted flag from `--est-basis reporter` typed out, because §0.8's outcomes
// (a) and (b) are different rows even though they reach the same plan.
type BasisRequest struct {
	Basis    EstBasis
	Explicit bool
}

// BasisDecision is the outcome of the two ordered phases.
type BasisDecision struct {
	Basis EstBasis
	// ColdStart is true when the plan is a reporter cold plan, which §0.8
	// requires to be LOUDLY labelled rather than silently served.
	ColdStart bool
	Reason    string
}

// SelectBasis implements contract §0.8's two ordered phases.
//
// Mode selection and scored admission are two SEPARATE SEQUENTIAL phases, not
// one table of mutually exclusive cases. Treating them as exclusive was wrong:
// {scored: true, basis omitted, store missing} matches both "emit a reporter
// cold plan" and "reject every cold plan". Ordering resolves it — phase 2 is a
// veto with higher priority, and outcome (d) IS that veto.
func SelectBasis(req BasisRequest, storePresent bool, status WallStatus, scored bool, fileParallelism int) (BasisDecision, error) {
	// --- Phase 1: mode selection. What plan WOULD be produced. ---
	var d BasisDecision
	switch {
	case req.Basis == BasisWall && req.Explicit:
		// Outcome (c). Checked before file parallelism so the reported reason
		// is the model's state when both are wrong.
		if status != WallStatusOK {
			return BasisDecision{}, fmt.Errorf("%w (status %q)", ErrWallModelUnusable, statusName(status))
		}
		// §7 rule 3.
		if fileParallelism > 1 {
			return BasisDecision{}, fmt.Errorf("%w: file_parallelism is %d, wall basis requires 1",
				ErrWallModelUnusable, fileParallelism)
		}
		d = BasisDecision{Basis: BasisWall}

	case !storePresent:
		// Outcomes (a) and (b): both reach a reporter cold plan, loudly
		// labelled. They are distinct rows because (b) was asked for
		// explicitly.
		d = BasisDecision{Basis: BasisReporter, ColdStart: true}
		if req.Explicit {
			d.Reason = "explicit --est-basis reporter with no usable store: reporter cold plan"
		} else {
			d.Reason = "no usable store and no basis requested: reporter cold plan"
		}

	default:
		d = BasisDecision{Basis: BasisReporter}
	}

	// --- Phase 2: scored admission, HIGHER PRIORITY. ---
	if scored && (d.ColdStart || d.Basis != req.Basis && req.Explicit) {
		return BasisDecision{}, fmt.Errorf("%w: %s", ErrScoredColdPlan, d.Reason)
	}
	return d, nil
}

// statusName renders the absent status readably, since "" would otherwise make
// the failure message ambiguous between absent and empty.
func statusName(s WallStatus) string {
	if s == WallStatusAbsent {
		return "absent"
	}
	return string(s)
}

// WallEstSecondsPresent is contract §5.1's ONE presence rule for the additive
// `wall_est_seconds` shadow: emitted iff the basis is `reporter` AND a fitted
// model with status `ok` exists. Under `wall` basis it is ABSENT, because
// `est_seconds` already IS the model's value.
//
// It is a shadow diagnostic and never reaches AllocationScore.
func WallEstSecondsPresent(basis EstBasis, status WallStatus) bool {
	return basis == BasisReporter && status == WallStatusOK
}
