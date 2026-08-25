package core

import (
	"fmt"
	"io"

	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/runner/gorunner"
)

// goRunner is a real Go adapter shared by the core tests, used wherever the core
// needs adapter behaviour it does not own: rendering a bucket, validating a
// unit's command grammar, and parsing a `go test -json` stream. Render /
// ValidateUnit / ParseTimings are pure — no toolchain is touched — so the whole
// planner is exercised without a build. Run-slice tests inject their runnable
// names through PlanOptions.Runnables or expandOptions.Runnables rather than
// through Discover.
//
// It is configured with the render defaults the ported tests assume (race,
// -count=100, -timeout 20m, the "adapters" node prefix) — the config that used
// to live on the plan options and now, correctly, lives inside the adapter.
var goRunner = mustGoRunner(gorunner.Options{
	Race:         true,
	Count:        100,
	Timeout:      "20m",
	NodePrefixes: []string{"adapters"},
})

func mustGoRunner(opt gorunner.Options) *gorunner.Runner {
	r, err := gorunner.New(opt)
	if err != nil {
		panic(err)
	}
	return r
}

// eventsRunner is the shared render config plus an events directory, for the
// test that checks the emitted commands tee their `go test -json` streams.
func eventsRunner(dir string) *gorunner.Runner {
	return mustGoRunner(gorunner.Options{
		Race:         true,
		Count:        100,
		Timeout:      "20m",
		NodePrefixes: []string{"adapters"},
		EventsDir:    dir,
	})
}

// canonicalFlags builds the comparability token the ported tests key their
// synthetic stores on. It mirrors the Go adapter's own token so a store built
// here compares equal to one the adapter would stamp. It is a local test helper,
// not a call across the seam.
func canonicalFlags(race bool, count int) string {
	if race {
		return fmt.Sprintf("-race -count=%d", count)
	}
	return fmt.Sprintf("-count=%d", count)
}

// parseEvents parses a `go test -json` stream via the adapter, returning the
// same RunSummary the core ingests.
func parseEvents(readers ...io.Reader) (*runner.RunSummary, error) {
	return goRunner.ParseTimings(readers...)
}

// renderBucket renders a bucket via the shared adapter (using its default render
// config), exposed under its old name so the ported render probes read as they
// did in the source tree.
func renderBucket(b runner.Bucket) runner.Rendered {
	return goRunner.Render(b)
}
