package core

import (
	"context"
	"github.com/invakid404/testbucket/internal/runner"
	"sort"
	"strings"
	"testing"
	"time"
)

// The tests in this package run entirely against a SYNTHETIC tree and a
// SYNTHETIC store. Nothing here shells out to the Go toolchain: the planner
// takes the live package set and the test-name resolver as data, so the
// whole mechanism — including the coverage gate that is its reason for
// existing — is exercised without a build, a network, or a CI run.
//
// The shape below is a Go monorepo's, scaled to the design's §2.4 estimates: one
// ~900s whale (netpkg/streamer), a second ~420s dominator
// (internal/engine), a smooth long tail, one GOWORK=off module that must be
// packed whole, and one package with no test files at all.

const repoPrefix = "example.test/repo/"

func livePkg(dir, module, mode string, hasTests bool) runner.LivePackage {
	imp := repoPrefix + dir
	if dir == "." {
		imp = strings.TrimSuffix(repoPrefix, "/")
	}
	atom := ""
	if mode == runner.ModeOff {
		atom = module
	}
	return runner.LivePackage{
		ID:       imp,
		Atom:     atom,
		HasTests: hasTests,
		Dir:      dir,
		Module:   module,
		Mode:     mode,
	}
}

func syntheticLive() []runner.LivePackage {
	live := []runner.LivePackage{
		livePkg("netpkg/streamer", "netpkg", runner.ModeWork, true),
		livePkg("netpkg/builder", "netpkg", runner.ModeWork, true),
		livePkg("netpkg/sse", "netpkg", runner.ModeWork, true),
		livePkg("internal/engine", ".", runner.ModeWork, true),
		livePkg("internal/body", ".", runner.ModeWork, true),
		livePkg("pool", "pool", runner.ModeWork, true),
		livePkg("worker", "worker", runner.ModeWork, true),
		livePkg("cmd/serve", ".", runner.ModeWork, true),
		livePkg("client", "client", runner.ModeWork, true),
		livePkg("internal/schema", ".", runner.ModeWork, true),
		// No test files: not bucketed, not gated — but the moment it gains
		// one, `go list` reports HasTests and it is scheduled.
		livePkg("cmd/embed", ".", runner.ModeWork, false),
		// GOWORK=off module: its packages must ride together.
		livePkg("adapters/common", "adapters/common", runner.ModeOff, true),
		livePkg("adapters/common/codegen", "adapters/common", runner.ModeOff, true),
	}
	sort.Slice(live, func(i, j int) bool { return live[i].ID < live[j].ID })
	return live
}

// syntheticWeights are the measured seconds the synthetic store carries.
var syntheticWeights = map[string]float64{
	"netpkg/streamer":         900,
	"netpkg/builder":          20,
	"netpkg/sse":              15,
	"internal/engine":         420,
	"internal/body":           160,
	"pool":                    120,
	"worker":                  110,
	"cmd/serve":               95,
	"client":                  80,
	"internal/schema":         25,
	"adapters/common":         12,
	"adapters/common/codegen": 44,
}

// syntheticStore builds a warm store. omit lists package dirs to leave
// unmeasured, so a test can model "these packages are new".
func syntheticStore(omit ...string) *Store {
	skip := map[string]bool{}
	for _, o := range omit {
		skip[o] = true
	}
	st := NewStore(canonicalFlags(true, 100))
	st.UpdatedAt = time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	for dir, sec := range syntheticWeights {
		if skip[dir] {
			continue
		}
		st.Units[repoPrefix+dir] = &UnitStat{Seconds: sec, Samples: 12}
	}
	for _, p := range syntheticLive() {
		if p.HasTests && st.Units[p.ID] != nil {
			st.Coverage = append(st.Coverage, p.ID)
		}
	}
	sort.Strings(st.Coverage)
	st.CoverageSource = "go-list"
	return st
}

// harpoon flags a package the way `ingest` would once it crosses the whale
// threshold.
func harpoon(st *Store, dir, policy string, into int, tests map[string]float64) {
	row := st.Units[repoPrefix+dir]
	if row == nil {
		row = &UnitStat{}
		st.Units[repoPrefix+dir] = row
	}
	row.Split = policy
	row.SplitInto = into
	row.Tests = tests
}

// syntheticRunnables stands in for `go test -list`. Packages absent from the
// map report nothing runnable, which is exactly the corruption the coverage
// gate must catch rather than paper over.
//
// Fixtures deliberately mix Test, Example and Fuzz names: `go test -run`
// selects all three, so a universe of only Test* would let the suite prove
// coverage of something narrower than the emitted command executes.
func syntheticRunnables(names map[string][]string) runnableNamer {
	return func(p runner.LivePackage) ([]string, error) {
		return names[p.ID], nil
	}
}

func defaultPlanOptions(live []runner.LivePackage) PlanOptions {
	return PlanOptions{
		K:          6,
		StorePath:  "test-timings.json",
		Count:      100,
		StaleAfter: 14 * 24 * time.Hour,
		Now:        time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC),
		Live:       live,
		Token:      canonicalFlags(true, 100),
	}
}

// mustPlan fails the test if the plan does not build; the coverage gate is
// supposed to pass on every well-formed input. It keeps Token in step with
// Count — the adapter derives its comparability token from the run's -count —
// so a test that changes Count does not accidentally trip the flag-mismatch
// cold start. It drives the shared goRunner (race, -count=100, -timeout 20m).
func mustPlan(t *testing.T, st *Store, reason string, opt PlanOptions) *PlanDocument {
	t.Helper()
	return mustPlanWith(t, goRunner, st, reason, opt)
}

// mustPlanWith is mustPlan against a specific adapter, for the few tests that
// need a non-default render configuration (events capture, say).
func mustPlanWith(t *testing.T, rnr runner.Runner, st *Store, reason string, opt PlanOptions) *PlanDocument {
	t.Helper()
	opt.Token = canonicalFlags(true, opt.Count)
	doc, err := BuildPlan(context.Background(), rnr, st, reason, opt)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	return doc
}

// scheduledPackages counts how many units cover each import path.
func scheduledPackages(doc *PlanDocument) map[string]int {
	out := map[string]int{}
	for _, b := range doc.Buckets {
		for _, u := range b.Units {
			for _, imp := range u.Packages {
				out[imp]++
			}
		}
	}
	return out
}
