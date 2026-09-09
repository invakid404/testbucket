package core

import (
	"errors"
	"testing"
)

// TestColdStartModeSelection is §22 test 34 (PD-2) and the acceptance test the
// registry names for ID-1 and ID-7: the four cases of contract §0.8, each
// distinct — cases 1 and 2 plan, case 3 hard-errors with no matrix, case 4
// rejects every cold or forced fallback.
func TestColdStartModeSelection(t *testing.T) {
	t.Run("(a) basis omitted + missing store plans, loudly labelled", func(t *testing.T) {
		d, err := SelectBasis(BasisRequest{Basis: BasisReporter}, false, WallStatusAbsent, false, 1)
		if err != nil {
			t.Fatalf("outcome (a) must plan, got %v", err)
		}
		if d.Basis != BasisReporter || !d.ColdStart {
			t.Fatalf("outcome (a) = %+v, want a reporter cold plan", d)
		}
		if d.Reason == "" {
			t.Fatal("a cold plan must be loudly labelled; reason is empty")
		}
	})

	t.Run("(b) explicit reporter + missing store plans, loudly labelled", func(t *testing.T) {
		d, err := SelectBasis(BasisRequest{Basis: BasisReporter, Explicit: true}, false, WallStatusAbsent, false, 1)
		if err != nil {
			t.Fatalf("outcome (b) must plan, got %v", err)
		}
		if d.Basis != BasisReporter || !d.ColdStart {
			t.Fatalf("outcome (b) = %+v, want a reporter cold plan", d)
		}
		if d.Reason == "" {
			t.Fatal("a cold plan must be loudly labelled; reason is empty")
		}
	})

	t.Run("(a) and (b) are distinct rows", func(t *testing.T) {
		a, _ := SelectBasis(BasisRequest{Basis: BasisReporter}, false, WallStatusAbsent, false, 1)
		b, _ := SelectBasis(BasisRequest{Basis: BasisReporter, Explicit: true}, false, WallStatusAbsent, false, 1)
		if a.Reason == b.Reason {
			t.Fatal("outcomes (a) and (b) must be distinguishable; both reported the same reason")
		}
	})

	t.Run("(c) explicit wall hard-errors for EVERY failure subtype", func(t *testing.T) {
		// §0.8 (c) covers model missing, insufficient and degraded, both
		// scored and unscored, with no matrix.
		for _, status := range []WallStatus{WallStatusAbsent, WallStatusInsufficient, WallStatusDegraded} {
			for _, scored := range []bool{false, true} {
				_, err := SelectBasis(BasisRequest{Basis: BasisWall, Explicit: true}, true, status, scored, 1)
				if !errors.Is(err, ErrWallModelUnusable) {
					t.Fatalf("status %q scored=%v: err = %v, want ErrWallModelUnusable", statusName(status), scored, err)
				}
			}
		}
	})

	t.Run("(c) explicit wall with file_parallelism > 1 fails", func(t *testing.T) {
		_, err := SelectBasis(BasisRequest{Basis: BasisWall, Explicit: true}, true, WallStatusOK, false, 2)
		if !errors.Is(err, ErrWallModelUnusable) {
			t.Fatalf("err = %v, want ErrWallModelUnusable (§7 rule 3)", err)
		}
	})

	t.Run("(c) explicit wall with an ok model plans", func(t *testing.T) {
		d, err := SelectBasis(BasisRequest{Basis: BasisWall, Explicit: true}, true, WallStatusOK, true, 1)
		if err != nil {
			t.Fatalf("wall basis with an ok model must plan, got %v", err)
		}
		if d.Basis != BasisWall || d.ColdStart {
			t.Fatalf("decision = %+v, want a warm wall plan", d)
		}
	})

	t.Run("(d) scored vetoes a cold plan that phase 1 chose", func(t *testing.T) {
		// This is the case that proves the phases are ORDERED rather than
		// exclusive: {scored, basis omitted, store missing} matches both (a)
		// and (d), and (d) wins.
		_, err := SelectBasis(BasisRequest{Basis: BasisReporter}, false, WallStatusAbsent, true, 1)
		if !errors.Is(err, ErrScoredColdPlan) {
			t.Fatalf("err = %v, want ErrScoredColdPlan", err)
		}
		// And the unscored form of the very same inputs plans.
		if _, err := SelectBasis(BasisRequest{Basis: BasisReporter}, false, WallStatusAbsent, false, 1); err != nil {
			t.Fatalf("the same inputs unscored must plan, got %v", err)
		}
	})

	t.Run("(d) scored vetoes an explicit reporter cold plan too", func(t *testing.T) {
		_, err := SelectBasis(BasisRequest{Basis: BasisReporter, Explicit: true}, false, WallStatusAbsent, true, 1)
		if !errors.Is(err, ErrScoredColdPlan) {
			t.Fatalf("err = %v, want ErrScoredColdPlan", err)
		}
	})

	t.Run("a warm reporter plan is not a cold start", func(t *testing.T) {
		d, err := SelectBasis(BasisRequest{Basis: BasisReporter}, true, WallStatusOK, true, 1)
		if err != nil {
			t.Fatal(err)
		}
		if d.ColdStart {
			t.Fatal("a warm reporter plan must not be labelled cold")
		}
	})
}

// TestWallEstSecondsIsShadowOnlyUnderReporterBasis is §22 test 9 and §5.1's
// single presence rule for the additive shadow.
func TestWallEstSecondsIsShadowOnlyUnderReporterBasis(t *testing.T) {
	cases := []struct {
		basis  EstBasis
		status WallStatus
		want   bool
	}{
		{BasisReporter, WallStatusOK, true},
		{BasisReporter, WallStatusDegraded, false},
		{BasisReporter, WallStatusInsufficient, false},
		{BasisReporter, WallStatusAbsent, false},
		// Under wall basis it is ABSENT: est_seconds already IS the model value.
		{BasisWall, WallStatusOK, false},
	}
	for _, c := range cases {
		if got := WallEstSecondsPresent(c.basis, c.status); got != c.want {
			t.Errorf("WallEstSecondsPresent(%q, %q) = %v, want %v", c.basis, statusName(c.status), got, c.want)
		}
	}
}
