package core

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
)

// PlannedCoverage is what the plan promised, per target.
type PlannedCoverage struct {
	// Invocations is how many separate runner calls should report this target:
	// 1 for a whole target or atom, S for a sharded or sliced one.
	Invocations map[string]int
	// Runnables is the union of the name-slice names across a target's slices,
	// present only for name-sliced targets.
	Runnables map[string][]string
	Units     int
}

// LoadPlannedCoverage reads the plan artifact and derives, per target, what a
// complete run of it should look like.
func LoadPlannedCoverage(path string) (*PlannedCoverage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read shard plan: %w", err)
	}
	var doc PlanDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse shard plan %s: %w", path, err)
	}
	out := &PlannedCoverage{Invocations: map[string]int{}, Runnables: map[string][]string{}}
	for _, b := range doc.Buckets {
		for _, u := range b.Units {
			out.Units++
			for _, p := range u.Packages {
				out.Invocations[p]++
			}
			if u.Kind == runner.KindRunSlice && len(u.Packages) == 1 {
				// The names are in the unit ID (pkg[A|B|C]) — the same text the
				// emitted name regex is built from.
				if open := strings.Index(u.ID, "["); open >= 0 && strings.HasSuffix(u.ID, "]") {
					names := strings.Split(u.ID[open+1:len(u.ID)-1], "|")
					out.Runnables[u.Packages[0]] = append(out.Runnables[u.Packages[0]], names...)
				}
			}
		}
	}
	return out, nil
}

// AuditCoverage compares the plan against what the events show actually ran.
//
// The coverage gate inside `plan` proves the MATRIX is complete before anything
// runs. This proves the RUN was: it is the after-the-fact half, and it catches
// what the gate structurally cannot — a bucket whose job never produced events,
// an artifact that failed to upload, a shard that died before reporting.
func AuditCoverage(out io.Writer, planned *PlannedCoverage, sum *runner.RunSummary) error {
	// A passing audit that could not deliver its report is not a pass: propagate
	// the first write error on the success path, the way plan and ingest do.
	ew := &errWriter{w: out}
	w := io.Writer(ew)
	observed := map[string]int{}
	for pkg, n := range sum.PackageRuns {
		observed[pkg] += n
	}
	for pkg := range sum.Failed {
		if _, ok := sum.PackageRuns[pkg]; !ok {
			// A target that only failed still ran; it reported a result.
			observed[pkg]++
		}
	}

	var missing, short, extra, unplanned []string
	for _, pkg := range runner.SortedKeys(planned.Invocations) {
		want := planned.Invocations[pkg]
		got := observed[pkg]
		switch {
		case got == 0:
			missing = append(missing, fmt.Sprintf("%s (planned %d invocation(s), reported none)", pkg, want))
		case got < want:
			short = append(short, fmt.Sprintf("%s reported %d of %d planned invocation(s)", pkg, got, want))
		case got > want:
			extra = append(extra, fmt.Sprintf("%s reported %d invocation(s), %d were planned", pkg, got, want))
		}
	}
	for _, pkg := range runner.SortedKeys(observed) {
		if _, ok := planned.Invocations[pkg]; !ok {
			unplanned = append(unplanned, fmt.Sprintf("%s reported %d result(s) but was in no bucket", pkg, observed[pkg]))
		}
	}

	// Name-sliced targets: the slices' names must be exactly the top-level
	// runnables the target actually reported. A name planned but never reported
	// means a slice did not run it; one reported but never planned means the
	// name filter reached past its slice.
	var sliceGaps []string
	for _, pkg := range runner.SortedKeys(planned.Runnables) {
		want := map[string]bool{}
		for _, n := range planned.Runnables[pkg] {
			want[n] = true
		}
		got := sum.TestSeconds[pkg]
		for _, n := range runner.SortedKeys(want) {
			if _, ok := got[n]; !ok {
				sliceGaps = append(sliceGaps, fmt.Sprintf("%s: %s was in a -run slice but never reported", pkg, n))
			}
		}
		for _, n := range runner.SortedKeys(got) {
			if !want[n] {
				sliceGaps = append(sliceGaps, fmt.Sprintf("%s: %s reported but was in no -run slice", pkg, n))
			}
		}
	}

	fmt.Fprintf(w, "testbucket audit — %d planned unit(s) over %d package(s)\n", planned.Units, len(planned.Invocations))
	fmt.Fprintf(w, "  packages that reported a result   %d\n", len(observed))
	fmt.Fprintf(w, "  run-sliced packages name-checked  %d\n", len(planned.Runnables))

	problems := len(missing) + len(short) + len(extra) + len(unplanned) + len(sliceGaps)
	if problems == 0 {
		fmt.Fprintf(w, "\nPASS — every planned package reported exactly the invocations the plan\n")
		fmt.Fprintf(w, "scheduled for it, counting a count-shard group as the one logical package\n")
		fmt.Fprintf(w, "it is, and every -run slice's names are accounted for.\n")
		return ew.err
	}

	var b strings.Builder
	b.WriteString("coverage audit FAILED: the run did not execute what the plan scheduled")
	for _, group := range []struct {
		label string
		items []string
	}{
		{"package(s) that never reported — a bucket produced no events", missing},
		{"package(s) that reported fewer invocations than planned — a shard or slice is missing", short},
		{"package(s) that reported more invocations than planned", extra},
		{"package(s) that ran but were in no bucket", unplanned},
		{"-run slice discrepancies", sliceGaps},
	} {
		if len(group.items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n  %s:", group.label)
		for _, it := range group.items {
			fmt.Fprintf(&b, "\n    - %s", it)
		}
	}
	b.WriteString("\n\nThe plan's coverage gate proves the MATRIX is complete before anything\n" +
		"runs. This proves the RUN was. A bucket that produced no events is\n" +
		"invisible to the gate and is exactly what this catches.")
	fmt.Fprintln(w)
	return fmt.Errorf("%s", b.String())
}
