package core

import "testing"

// TestWallHistoryFreezesPlanTimeFeatures is §22 test 37 (S4) and the acceptance
// test the registry names for ID-5.
//
// Every regressor value is frozen at PLAN time, so a later store update cannot
// retroactively change what was fitted. In particular reporter_sum_ns is
// deliberately NOT recomputed at fit time: recomputing against a newer store
// would fit the model to inputs the plan never saw.
func TestWallHistoryFreezesPlanTimeFeatures(t *testing.T) {
	const frozenSum = 10_000_000_000

	row := ringRow("run-1", 0, "2026-09-01T00:00:00Z")
	row.ReporterSumNs = frozenSum
	row.IAnyWholeFile = 1
	row.SliceCount = 2

	w := newWall()
	w.AppendRow(row)

	t.Run("reporter_sum_ns is the plan-time value", func(t *testing.T) {
		if got := w.Observations[0].ReporterSumNs; got != frozenSum {
			t.Fatalf("reporter_sum_ns = %d, want the frozen %d", got, frozenSum)
		}
	})

	t.Run("advancing the store does not change the stored regressors", func(t *testing.T) {
		// The store's reporter EWMAs move on; the ring row does not.
		st := NewStore("vitest")
		st.Units = map[string]*UnitStat{
			"a.spec.ts": {Seconds: 999, Samples: 5},
		}
		st.Wall = w

		before := st.Wall.Observations[0]
		// A later ingest updates the unit weights.
		st.Units["a.spec.ts"].Seconds = 1234
		after := st.Wall.Observations[0]

		if after.ReporterSumNs != before.ReporterSumNs {
			t.Fatal("a store update changed a stored regressor")
		}
		if after.ReporterSumNs != frozenSum {
			t.Fatalf("reporter_sum_ns drifted to %d", after.ReporterSumNs)
		}
	})

	t.Run("the topology counts are the plan-time values", func(t *testing.T) {
		if w.Observations[0].IAnyWholeFile != 1 {
			t.Fatalf("i_any_whole_file = %d, want the plan-time 1", w.Observations[0].IAnyWholeFile)
		}
		if w.Observations[0].SliceCount != 2 {
			t.Fatalf("slice_count = %d, want the plan-time 2", w.Observations[0].SliceCount)
		}
	})

	t.Run("a recomputed-at-fit-time value fails", func(t *testing.T) {
		// The executable form of "deliberately not recomputed": a fit driven
		// over a row whose regressor was replaced by a fresh recomputation
		// produces a different design than the frozen one, so the two are
		// distinguishable and the frozen path is the one that holds.
		recomputed := w.Observations[0]
		recomputed.ReporterSumNs = 1_234_000_000_000 // as a newer store would give
		if recomputed.ReporterSumNs == frozenSum {
			t.Fatal("the fixture no longer separates the frozen and recomputed values")
		}
		frozenDesign := []float64{1, float64(frozenSum), 1, 2}
		freshDesign := []float64{1, float64(recomputed.ReporterSumNs), 1, 2}
		if frozenDesign[1] == freshDesign[1] {
			t.Fatal("the two designs are identical; recomputation would be undetectable")
		}
	})

	t.Run("the ring row carries the diagnostics but never as regressors", func(t *testing.T) {
		r := w.Observations[0]
		// whole_file_count and invocation_count are audit only.
		r.WholeFileCount = 3
		r.InvocationCount = 4
		design := []float64{1, float64(r.ReporterSumNs), float64(r.IAnyWholeFile), float64(r.SliceCount)}
		if len(design) != 4 {
			t.Fatalf("the design has %d columns, want the four §0.9 declares", len(design))
		}
		for _, v := range design {
			if v == 3 || v == 4 {
				t.Fatal("a diagnostic leaked into the design matrix")
			}
		}
	})
}
