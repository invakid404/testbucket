package vitestrunner

import (
	"encoding/json"
	"fmt"
	"io"

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
	Title    string   `json:"title"`
	FullName string   `json:"fullName"`
	Status   string   `json:"status"`
	Duration *float64 `json:"duration"` // ms; null for skipped/todo
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
			// Per-test (assertion) weights are deliberately NOT recorded: this
			// adapter buckets at FILE granularity, the natural Vitest unit, so a
			// whale spec runs whole rather than being name-sliced. Vitest
			// name-slicing (an anchored -t over escaped test names, with
			// duplicate-title and nested-describe handling) is a documented
			// follow-up; leaving TestSeconds empty keeps the core from ever
			// choosing a run-split the renderer does not yet implement.
		}
	}
	if len(sum.PackageSeconds) == 0 && len(sum.Failed) == 0 && len(sum.NoTests) == 0 {
		return nil, fmt.Errorf("no vitest results found (%d document(s), %d unparsable)", sum.Lines, sum.Malformed)
	}
	return sum, nil
}
