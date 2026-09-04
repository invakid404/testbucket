package walltime

import (
	"fmt"
	"time"
)

// A_GH is the GitHub Run-bucket step's own elapsed time, in whole seconds.
//
// It is a DIAGNOSTIC and never a measurement: GitHub reports step timestamps
// at one-second resolution, and the contract is explicit that it can never
// enter balance, non-regression, prediction or success. What it is good for is
// identity — proving that the action envelope this ledger describes is the
// step GitHub thinks ran — and for making visible the one action-step
// operation that necessarily precedes AT_start, since the wrapper cannot read
// a clock before its own binary exists.
//
// The check is resolution-aware by construction. The API interval is widened
// by its declared precision before being compared, so a sub-second difference
// can never fail it; only a wrong step, a missing link, or a gap larger than
// the precision plus the measured envelope can.
type StepAttempt struct {
	// Repository, WorkflowRun, Job, Step and Attempt identify the one step
	// attempt this action ran as.
	Repository  string `json:"repository"`
	WorkflowRun string `json:"workflow_run"`
	Job         string `json:"job"`
	Step        string `json:"step"`
	Attempt     string `json:"attempt"`
	// StartedAt and CompletedAt are the API's own timestamps.
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	// Precision is the API's declared timestamp resolution, e.g. "1s".
	Precision string `json:"precision"`
}

// AGHKind identifies the step-attempt diagnostic document.
const AGHKind = "tb.walltime.step-attempt/v1"

// StepAttemptDocument is the retained diagnostic.
type StepAttemptDocument struct {
	Kind    string      `json:"kind"`
	Attempt StepAttempt `json:"attempt"`
}

// ElapsedNs is A_GH: the whole-second step elapsed, in nanoseconds.
func (s StepAttempt) ElapsedNs() (int64, error) {
	start, end, err := s.Window()
	if err != nil {
		return 0, err
	}
	return end.Sub(start).Nanoseconds(), nil
}

// Window parses the API interval.
func (s StepAttempt) Window() (time.Time, time.Time, error) {
	start, err := time.Parse(time.RFC3339, s.StartedAt)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("step attempt started_at %q: %w", s.StartedAt, err)
	}
	end, err := time.Parse(time.RFC3339, s.CompletedAt)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("step attempt completed_at %q: %w", s.CompletedAt, err)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("step attempt completed at %s, before it started at %s", s.CompletedAt, s.StartedAt)
	}
	return start, end, nil
}

// CheckIdentity compares the step attempt to the action envelope's own
// realtime brackets, and reports what disagrees.
//
// It is an identity and sanity check, NEVER a sub-second gate: the API
// interval is widened by its declared precision on both sides before anything
// is compared. What it can catch is a ledger linked to the wrong step, a
// missing link, or an envelope that lies outside the step it claims to be.
func (s StepAttempt) CheckIdentity(run RunIdentity, start, end Record) []string {
	var problems []string
	// ABSENCE IS NOT AGREEMENT.
	//
	// Both sides used to have to be present before they were compared, so a
	// recorded identity that named nothing skipped the comparison entirely:
	// the API document could state the repository, workflow run, job, step and
	// attempt, the records could state none of them, and this returned no
	// problem at all. That is the one case the cross-check exists for — a
	// ledger that cannot be linked to the step it claims to come from — and it
	// was the case that passed.
	//
	// When the API supplies a value, the records must supply it too, and it
	// must match. When the API itself is silent there is nothing to compare
	// against and the row's own presence rule (see requiredIdentityFields)
	// still requires the value to exist.
	for _, f := range []struct{ name, recorded, api string }{
		{"repository", run.Repository, s.Repository},
		{"workflow run", run.WorkflowRun, s.WorkflowRun},
		{"job", run.Job, s.Job},
		{"step", run.Step, s.Step},
		{"step attempt", run.StepAttempt, s.Attempt},
	} {
		if f.api == "" {
			continue
		}
		if f.recorded == "" {
			problems = append(problems, fmt.Sprintf(
				"the step attempt names %s %q and the records name none; a row that omits the identity the step attempt supplies cannot be linked to it", f.name, f.api))
			continue
		}
		if f.api != f.recorded {
			problems = append(problems, fmt.Sprintf("the records name %s %q, the step attempt %q", f.name, f.recorded, f.api))
		}
	}
	window, err := time.ParseDuration(firstNonEmptyStr(s.Precision, "1s"))
	if err != nil {
		return append(problems, fmt.Sprintf("step attempt precision %q: %v", s.Precision, err))
	}
	apiStart, apiEnd, err := s.Window()
	if err != nil {
		return append(problems, err.Error())
	}
	// Widen by the declared precision, then require each envelope endpoint's
	// realtime bracket to fall inside.
	lo, hi := apiStart.Add(-window), apiEnd.Add(window)
	for _, e := range []struct {
		name string
		rec  Record
	}{{"AT_start", start}, {"AT_end", end}} {
		before, after, err := e.rec.Instant.RealtimeBracket()
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", e.name, err))
			continue
		}
		if before.Before(lo) || after.After(hi) {
			problems = append(problems, fmt.Sprintf(
				"%s brackets [%s, %s] fall outside the step attempt's precision-widened interval [%s, %s]",
				e.name, before.Format(time.RFC3339Nano), after.Format(time.RFC3339Nano),
				lo.Format(time.RFC3339Nano), hi.Format(time.RFC3339Nano)))
		}
	}
	return problems
}

// BootstrapGapNs is the action-step time that necessarily precedes AT_start:
// installing the wrapper binary, because there is no wrapper to read a clock
// until it exists.
//
// It is derived from the step attempt rather than measured, so it is reported
// at the API's resolution and is explicitly not part of A. Naming it is the
// point: an unmeasurable prefix that nobody reports is indistinguishable from
// one that does not exist.
func (s StepAttempt) BootstrapGapNs(start Record) (int64, error) {
	apiStart, _, err := s.Window()
	if err != nil {
		return 0, err
	}
	before, _, err := start.Instant.RealtimeBracket()
	if err != nil {
		return 0, err
	}
	gap := before.Sub(apiStart)
	if gap < 0 {
		return 0, nil
	}
	return gap.Nanoseconds(), nil
}
