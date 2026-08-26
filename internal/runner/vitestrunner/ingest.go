package vitestrunner

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
)

// vitestReport is the shape of `vitest run --reporter=json` output — the
// Jest-compatible JSON reporter. One object is written per run (per bucket, when
// captured with --outputFile), so ParseTimings decodes each reader as one whole
// document rather than as a line stream.
type vitestReport struct {
	TestResults []vitestFile `json:"testResults"`
}

type vitestFile struct {
	Name             string            `json:"name"` // absolute test-file path
	Status           string            `json:"status"`
	StartTime        float64           `json:"startTime"` // epoch ms
	EndTime          float64           `json:"endTime"`
	AssertionResults []vitestAssertion `json:"assertionResults"`
}

type vitestAssertion struct {
	Title string `json:"title"`
	// AncestorTitles is the enclosing describe path, outermost first. Joined with
	// Title under nameSep it reconstructs the exact `vitest list --json` name, so
	// the per-test rows this records key on the SAME identity the planner's
	// Runnables universe does. FullName (the reporter's own space-joined form) is
	// deliberately not used as the key: it is what -t matches but it is lossier
	// (a describe boundary and an in-title space become indistinguishable), and
	// keying on it would not line up with `vitest list`.
	AncestorTitles []string `json:"ancestorTitles"`
	FullName       string   `json:"fullName"`
	Status         string   `json:"status"`
	Duration       *float64 `json:"duration"` // ms; null for skipped/todo
}

// assertionID reconstructs the neutral `" > "`-joined per-test identity from a
// reporter assertion — the same string `vitest list --json` reports as `name`.
func assertionID(a vitestAssertion) string {
	path := make([]string, 0, len(a.AncestorTitles)+1)
	path = append(path, a.AncestorTitles...)
	path = append(path, a.Title)
	return strings.Join(path, nameSep)
}

// ParseTimings folds one or more Vitest JSON-reporter documents into the neutral
// RunSummary the store ingests: a file's weight is its wall time, keyed by the
// same root-relative id Discover produces, and each top-level test's duration is
// a per-runnable weight for name-slicing. A FAILED file contributes no fresh
// weight (its wall measures the failure, not the work), exactly as the Go
// adapter drops a failed package.
func (r *Runner) ParseTimings(readers ...io.Reader) (*runner.RunSummary, error) {
	sum := runner.NewRunSummary()
	for _, rd := range readers {
		data, err := io.ReadAll(rd)
		if err != nil {
			return nil, fmt.Errorf("read vitest report: %w", err)
		}
		sum.Lines++
		var rep vitestReport
		if err := json.Unmarshal(data, &rep); err != nil {
			sum.Malformed++
			continue
		}
		for _, f := range rep.TestResults {
			if f.Name == "" {
				continue
			}
			id := relID(r.root, f.Name)
			sum.Events++
			switch {
			case f.Status == "failed":
				sum.Failed[id] = true
				continue
			case len(f.AssertionResults) == 0:
				// A file that registered no test — nothing to weigh, nothing to
				// schedule beyond noting it ran.
				sum.NoTests[id] = true
				continue
			}
			wall := (f.EndTime - f.StartTime) / 1000
			if !runner.PlausibleSeconds(wall) || wall < 0 {
				// A corrupt or absent timestamp pair: reject rather than poison
				// the weight, as the Go adapter does with an implausible Elapsed.
				sum.Implausible++
				continue
			}
			sum.PackageSeconds[id] += wall
			sum.PackageRuns[id]++
			recordTestSeconds(sum, id, f.AssertionResults)
		}
	}
	if len(sum.PackageSeconds) == 0 && len(sum.Failed) == 0 && len(sum.NoTests) == 0 {
		return nil, fmt.Errorf("no vitest results found (%d document(s), %d unparsable)", sum.Lines, sum.Malformed)
	}
	return sum, nil
}

// recordTestSeconds folds one file's per-test durations into the summary so the
// planner can name-slice it — the Vitest twin of the Go adapter's top-level
// `pass` events.
//
// It refuses to record a file whose test ids are AMBIGUOUS (two full names that
// collapse to the same space-form -t would match together). Such a file cannot
// be name-sliced without running a test in two slices, so leaving its per-test
// rows out demotes it to a whole-file unit — the safe fallback — instead of
// persisting a picture the plan would then have to refuse. The rare case is
// caught here (steady state) and again in Runnables (a name added since the last
// record), so the plan never has to hard-fail on it.
func recordTestSeconds(sum *runner.RunSummary, file string, assertions []vitestAssertion) {
	ids := make([]string, 0, len(assertions))
	for _, a := range assertions {
		ids = append(ids, assertionID(a))
	}
	if len(ambiguous(ids)) > 0 {
		// Record no per-test rows for an ambiguous file AND raise the explicit
		// unsliceable signal. Without the signal, a file that was split=run on a
		// PRIOR record keeps its stale per-test map (ingest only prunes when fresh
		// rows arrive), stays split=run, and then fails the next plan closed when
		// Runnables refuses the collision. The signal makes ingest demote it to a
		// whole-file unit instead — a recoverable fallback, not a hard failure.
		sum.Unsliceable[file] = true
		return
	}
	for _, a := range assertions {
		if a.Status != "passed" || a.Duration == nil {
			// Only a passed test contributes a weight; a skipped/todo test has a
			// null duration and no work to measure. It is still in the `Runnables`
			// universe (via `vitest list`), so leaving it unweighed here just hands
			// it the slicer's residual average — never drops it.
			continue
		}
		sec := *a.Duration / 1000
		if sec <= 0 || !runner.PlausibleSeconds(sec) {
			continue
		}
		if sum.TestSeconds[file] == nil {
			sum.TestSeconds[file] = map[string]float64{}
		}
		sum.TestSeconds[file][assertionID(a)] += sec
	}
}
