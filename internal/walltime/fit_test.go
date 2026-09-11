package walltime

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestAdmitWallRingRecheckstheRealRing is §6.7's "MIN_ROWS/MIN_RUNS/rank are
// re-checked against the real ring on every wall plan", as a control.
//
// The plan path admitted wall allocation from the STORED status and coefficients
// alone. The three cases below are the three ways a store can carry a complete
// `ok` fit group over a population that no longer supports it.
func TestAdmitWallRingRechecksTheRealRing(t *testing.T) {
	// A population that does support a fit: MIN_ROWS rows over MIN_RUNS runs,
	// with both topology columns varying so rank reaches 4.
	good := func() []FitRow {
		var out []FitRow
		for i := 0; i < MinRows; i++ {
			out = append(out, FitRow{
				ReporterSumNs: int64(1_000_000_000 * (i + 1)),
				IAnyWholeFile: i % 2,
				SliceCount:    i % 5,
				ElapsedNs:     int64(2_000_000_000 * (i + 1)),
				HeadSHA:       "head",
				RunID:         fmt.Sprintf("run-%d", i%MinRuns),
				RunAttempt:    "1",
				BucketIndex:   i,
			})
		}
		return out
	}

	t.Run("a supporting population is admitted", func(t *testing.T) {
		if err := AdmitWallRing(good()); err != nil {
			t.Fatalf("a population of %d rows over %d runs must be admitted: %v", MinRows, MinRuns, err)
		}
	})

	t.Run("an emptied ring is refused", func(t *testing.T) {
		// THE EXACT DRIFT: status ok and a complete fit group over an
		// observations array that is now empty.
		err := AdmitWallRing(nil)
		if err == nil || !errors.Is(err, ErrRingAdmission) {
			t.Fatalf("an empty ring must be refused, got %v", err)
		}
		if !strings.Contains(err.Error(), "MIN_ROWS") {
			t.Errorf("the refusal must name MIN_ROWS, got %q", err)
		}
	})

	t.Run("rows below the minimum are refused", func(t *testing.T) {
		rows := good()[:MinRows-1]
		if err := AdmitWallRing(rows); err == nil || !errors.Is(err, ErrRingAdmission) {
			t.Fatalf("%d rows must be refused, got %v", len(rows), err)
		}
	})

	t.Run("runs below the minimum are refused with enough rows", func(t *testing.T) {
		rows := good()
		for i := range rows {
			rows[i].RunID = "run-0"
		}
		err := AdmitWallRing(rows)
		if err == nil || !errors.Is(err, ErrRingAdmission) {
			t.Fatalf("one run must be refused, got %v", err)
		}
		if !strings.Contains(err.Error(), "MIN_RUNS") {
			t.Errorf("the refusal must name MIN_RUNS, got %q", err)
		}
	})

	t.Run("a rank-deficient population is refused with enough rows and runs", func(t *testing.T) {
		// Counts are necessary and never sufficient — §6.6's whole point. Every
		// row carries the same topology, so columns 3 and 4 are constant.
		rows := good()
		for i := range rows {
			rows[i].IAnyWholeFile, rows[i].SliceCount = 1, 0
		}
		err := AdmitWallRing(rows)
		if err == nil || !errors.Is(err, ErrRingAdmission) {
			t.Fatalf("a rank-deficient ring must be refused, got %v", err)
		}
		if !strings.Contains(err.Error(), "rank") {
			t.Errorf("the refusal must name rank, got %q", err)
		}
	})
}
