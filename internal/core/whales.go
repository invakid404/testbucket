package core

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/invakid404/testbucket/internal/runner"
)

// WriteWhaleReport reports the per-runnable distribution behind each split
// decision.
//
// It exists so the divisibility question — is a whale's wall time spread across
// its tests, or concentrated in one that no name split can escape? — can be
// re-derived from any store by anyone, rather than taken on trust from a
// one-off local measurement. Point it at the store artifact a master run
// uploaded and it answers the question on CI's own numbers.
//
// It returns the first write error so a whales report that could not be
// delivered (a full disk, a closed pipe) fails loudly rather than exiting 0 with
// a truncated report — the same discipline plan and ingest already follow.
func WriteWhaleReport(out io.Writer, st *Store, k, top int, all bool) error {
	ew := &errWriter{w: out}
	w := io.Writer(ew)
	measured := make([]float64, 0, len(st.Units))
	for _, pkg := range runner.SortedKeys(st.Units) {
		if row := st.Units[pkg]; row.measured() {
			measured = append(measured, row.Seconds)
		}
	}
	total := runner.SumSeconds(measured)
	threshold := total / float64(k)

	fmt.Fprintf(w, "testbucket whales — K=%d, flags %q\n", k, st.Flags)
	fmt.Fprintf(w, "store: %s (recorded %s)\n", firstNonEmpty(st.UpdatedAt, "<no timestamp>"), firstNonEmpty(st.CoverageSource, "unknown source"))
	fmt.Fprintf(w, "total measured work %s; split threshold (total/K) %.1fs\n", humanSeconds(total), threshold)

	shown := 0
	for _, pkg := range runner.SortedKeys(st.Units) {
		row := st.Units[pkg]
		if !row.measured() {
			continue
		}
		if !all && row.splitPolicy() == splitNone && row.Seconds <= threshold {
			continue
		}
		if len(row.Tests) == 0 && !all {
			continue
		}
		shown++

		names := runner.SortedKeys(row.Tests)
		sort.SliceStable(names, func(i, j int) bool { return row.Tests[names[i]] > row.Tests[names[j]] })
		named := 0.0
		for _, n := range names {
			named += row.Tests[n]
		}
		heaviest, heaviestName := 0.0, ""
		if len(names) > 0 {
			heaviestName, heaviest = names[0], row.Tests[names[0]]
		}

		shards := row.SplitInto
		if shards < 2 {
			shards = 2
		}
		fmt.Fprintf(w, "\n%s\n", pkg)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "  package wall time\t%s\t\n", humanSeconds(row.Seconds))
		fmt.Fprintf(tw, "  named runnables\t%d\t%.1f%% of the package (%s)\n", len(names), pct(named, row.Seconds), humanSeconds(named))
		if heaviestName != "" {
			fmt.Fprintf(tw, "  heaviest runnable\t%.1fs\t%.1f%% of the package — %s\n", heaviest, pct(heaviest, row.Seconds), heaviestName)
		}
		fmt.Fprintf(tw, "  policy\t%s x%d\t%s\n", firstNonEmpty(row.Split, splitNone), row.SplitInto, row.SplitReason)
		tw.Flush()

		// The comparison the policy actually turns on. Count-sharding divides
		// iterations, so its cost is independent of the internal shape;
		// name-slicing divides the test list, so it can never finish faster
		// than its single heaviest name.
		fmt.Fprintf(w, "  mechanism comparison at S=%d:\n", shards)
		fmt.Fprintf(w, "    count-shard  >= %7.1fs per shard  (a LOWER BOUND: each shard is a separate\n",
			countShardFloor(row.Seconds, named, shards))
		fmt.Fprintf(w, "                                       binary, so per-binary fixed work repeats S times)\n")
		if heaviestName != "" {
			fmt.Fprintf(w, "    -run slice      %7.1fs floor     (the heaviest single runnable; no S beats it)\n", heaviest)
		}

		if len(names) > 0 {
			// Clamped at both ends, not just the top: WriteWhaleReport is called
			// directly by tests and could be reached with a negative limit,
			// where names[:limit] panics rather than reporting.
			limit := top
			if limit < 0 {
				limit = 0
			}
			if limit > len(names) {
				limit = len(names)
			}
			fmt.Fprintf(w, "  heaviest %d of %d runnables:\n", limit, len(names))
			for _, n := range names[:limit] {
				fmt.Fprintf(w, "    %8.1fs  %5.1f%%  %s\n", row.Tests[n], pct(row.Tests[n], row.Seconds), n)
			}
		}
	}
	if shown == 0 {
		fmt.Fprintf(w, "\nno package carries per-test rows yet — nothing has crossed the split threshold.\n")
	}
	return ew.err
}

func pct(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return part / whole * 100
}
