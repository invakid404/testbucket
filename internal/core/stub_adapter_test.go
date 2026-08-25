package core_test

// This is the discriminating proof that the runner-adapter seam is genuinely
// framework-agnostic: a MINIMAL non-Go adapter — Vitest-shaped — that
//
//   - implements runner.Runner,
//   - populates the neutral runner.LivePackage (ID + Atom, no Go fields),
//   - carries its OWN run configuration (a Vitest `testTimeout` of 10000 ms, a
//     retry count), never a Go duration or `go test -count` semantics,
//
// and drives a real core.BuildPlan through it — WITHOUT importing
// internal/runner/gorunner and WITHOUT touching internal/core. If this compiles
// and plans, a Phase-2 Vitest adapter drops in the same way. (It lives in
// package core_test — black-box — precisely so it cannot reach any unexported
// core or Go-adapter helper.)

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
)

// vitestRunner is a toy Vitest adapter. Everything Vitest-specific — the test
// timeout, the retry count, the `npx vitest` command — lives here; none of it
// crosses into the core.
type vitestRunner struct {
	testTimeoutMS int                 // a Vitest timeout, e.g. 10000 — NOT a Go duration
	retries       int                 // Vitest --retry; the adapter's own knob
	names         map[string][]string // per-file selectable test names (for run-slices)
}

func (v vitestRunner) Discover(ctx context.Context) ([]runner.LivePackage, error) {
	// Two projects, four spec files. Files within a project co-schedule (Atom =
	// project); the projects mix freely. No Go fields are set.
	return []runner.LivePackage{
		{ID: "web/login.spec.ts", Atom: "web", HasTests: true},
		{ID: "web/signup.spec.ts", Atom: "web", HasTests: true},
		{ID: "api/users.spec.ts", Atom: "api", HasTests: true},
		{ID: "api/orders.spec.ts", Atom: "api", HasTests: true},
	}, nil
}

func (v vitestRunner) Runnables(ctx context.Context, p runner.LivePackage) ([]string, error) {
	return v.names[p.ID], nil
}

func (v vitestRunner) ParseTimings(readers ...io.Reader) (*runner.RunSummary, error) {
	return runner.NewRunSummary(), nil
}

func (v vitestRunner) Render(b runner.Bucket) runner.Rendered {
	var lines []string
	for _, u := range b.Units {
		specs := make([]string, 0, len(u.Packages))
		for _, p := range u.Packages {
			specs = append(specs, p.ID)
		}
		// A Vitest command with the adapter's own testTimeout — a millisecond
		// integer, which would be "not a Go duration" if the core still owned it.
		line := "npx vitest run --testTimeout=" + itoa(v.testTimeoutMS)
		if v.retries > 0 {
			line += " --retry=" + itoa(v.retries)
		}
		if len(u.Run) > 0 {
			line += " -t " + shellRegex(u.Run)
		}
		line += " " + strings.Join(specs, " ")
		lines = append(lines, line)
	}
	return runner.Rendered{Script: strings.Join(lines, "\n")}
}

func (v vitestRunner) ValidateUnit(u runner.Unit, live map[string]runner.LivePackage, baseCount int) []string {
	// A minimal grammar check: every named target must be live and test-bearing.
	// A real Vitest adapter would also check its own -t regex grammar.
	var defects []string
	for _, p := range u.Packages {
		if lp, ok := live[p.ID]; !ok || !lp.HasTests {
			defects = append(defects, u.ID+" names "+p.ID+", not a live test file")
		}
	}
	return defects
}

func (v vitestRunner) CanonicalToken() string {
	return "vitest testTimeout=" + itoa(v.testTimeoutMS) + " retry=" + itoa(v.retries)
}

func TestNonGoAdapterPlansThroughTheCore(t *testing.T) {
	v := vitestRunner{testTimeoutMS: 10000, retries: 2}
	ctx := context.Background()

	live, err := v.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := core.BuildPlan(ctx, v, nil, "cold start (no store)", core.PlanOptions{
		K:     2,
		Count: 1, // a neutral sweep of 1 repetition; the adapter renders it
		Live:  live,
		Token: v.CanonicalToken(),
		Now:   fixedNow(),
	})
	if err != nil {
		t.Fatalf("a non-Go adapter could not plan through the core: %v", err)
	}

	// The never-drop gate passed on a purely non-Go tree: every spec is
	// scheduled exactly once across the two buckets.
	scheduled := map[string]int{}
	for _, b := range doc.Buckets {
		for _, u := range b.Units {
			for _, id := range u.Packages {
				scheduled[id]++
			}
		}
	}
	for _, f := range []string{"web/login.spec.ts", "web/signup.spec.ts", "api/users.spec.ts", "api/orders.spec.ts"} {
		if scheduled[f] != 1 {
			t.Errorf("%s scheduled %d times, want exactly 1", f, scheduled[f])
		}
	}
	if len(doc.Buckets) != 2 {
		t.Fatalf("got %d buckets, want K=2", len(doc.Buckets))
	}

	// The Vitest timeout — a bare 10000, which the old core would have rejected
	// as "not a Go duration" — reaches the emitted command untouched, because it
	// never passes through the core at all.
	joined := ""
	for _, b := range doc.Buckets {
		joined += b.Script + "\n"
	}
	if !strings.Contains(joined, "--testTimeout=10000") {
		t.Errorf("the adapter's Vitest timeout did not reach the command:\n%s", joined)
	}
	if !strings.Contains(joined, "npx vitest run") {
		t.Errorf("the emitted command is not a Vitest command:\n%s", joined)
	}
	// And the two projects co-scheduled as atoms: a project's specs share a
	// bucket, proving the neutral Atom key drove co-scheduling with no Go module
	// in sight.
	for _, b := range doc.Buckets {
		projects := map[string]bool{}
		for _, u := range b.Units {
			for _, id := range u.Packages {
				projects[strings.SplitN(id, "/", 2)[0]] = true
			}
		}
		if len(projects) > 1 {
			t.Errorf("bucket mixed projects %v; the Atom key did not co-schedule", projects)
		}
	}
}

// --- tiny local helpers, so the stub needs no Go-adapter code at all ---

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func shellRegex(names []string) string { return "'" + strings.Join(names, "|") + "'" }

func fixedNow() time.Time { return time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC) }
