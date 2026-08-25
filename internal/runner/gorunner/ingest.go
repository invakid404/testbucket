package gorunner

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
)

// testEvent is the subset of `go test -json`'s TestEvent this tool reads. The
// stream is the native Go equivalent of the JUnit XML every other splitter
// consumes, which is why no third-party runner is needed.
type testEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"`
}

// parseEvents folds one or more NDJSON streams into a runner.RunSummary.
// Non-JSON lines are tolerated and counted, not fatal: a stray `go` warning on
// stdout must not cost a whole run's timings. A stream with no usable events at
// all IS fatal — that means the capture is broken and silently writing an
// unchanged store would hide it.
func parseEvents(readers ...io.Reader) (*runner.RunSummary, error) {
	sum := runner.NewRunSummary()
	for _, r := range readers {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			sum.Lines++
			if !strings.HasPrefix(line, "{") {
				sum.Malformed++
				continue
			}
			var ev testEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				sum.Malformed++
				continue
			}
			if ev.Package == "" {
				continue
			}
			sum.Events++
			if ev.Elapsed != 0 && !runner.PlausibleSeconds(ev.Elapsed) {
				// Reject rather than absorb: an implausible Elapsed is corrupt
				// data, and silently folding it in would poison the weight, the
				// whale threshold and every split derived from them. Counting it
				// keeps the corruption visible in the job log.
				sum.Implausible++
				continue
			}
			switch {
			case ev.Test == "" && ev.Action == "pass":
				sum.PackageSeconds[ev.Package] += ev.Elapsed
				sum.PackageRuns[ev.Package]++
			case ev.Test == "" && ev.Action == "fail":
				sum.Failed[ev.Package] = true
			case ev.Test == "" && ev.Action == "skip":
				// "no test files" — nothing to weigh, nothing to schedule.
				sum.NoTests[ev.Package] = true
			case ev.Test != "" && ev.Action == "pass":
				// -run slicing operates on top-level names, and a top-level pass
				// event's Elapsed ALREADY includes every subtest it ran. Adding
				// the "TestX/sub" events on top would inflate the parent — by a
				// lot, in a package that leans on t.Run — which in turn skews the
				// slice packing and can push a package over the run-upgrade
				// threshold on time that was only ever counted once in reality.
				if strings.ContainsRune(ev.Test, '/') {
					sum.Subtests++
					continue
				}
				if sum.TestSeconds[ev.Package] == nil {
					sum.TestSeconds[ev.Package] = map[string]float64{}
				}
				sum.TestSeconds[ev.Package][ev.Test] += ev.Elapsed
			}
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("read go test -json stream: %w", err)
		}
	}
	// "Usable" means package RESULTS, not merely well-formed lines: a capture
	// that recorded only `run`/`output` chatter is broken, and silently writing
	// an unchanged store would hide that indefinitely.
	if len(sum.PackageSeconds) == 0 && len(sum.Failed) == 0 && len(sum.NoTests) == 0 {
		return nil, fmt.Errorf("no `go test -json` package results found (%d lines, %d events, %d unparsable)",
			sum.Lines, sum.Events, sum.Malformed)
	}
	return sum, nil
}
