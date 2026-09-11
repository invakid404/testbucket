package core

import (
	"errors"
	"testing"
)

// TestScoredAdmissionSeesTheColdStateItWillActuallyGet is §0.8 phase 2's veto
// against a scored cold plan, applied to the state BuildPlan will really build
// in rather than to "a store file parsed".
//
// SelectBasis was told `st != nil`. BuildPlan then discards a store recorded
// under another token and cold-starts, so a scored reporter run with a restored
// wrong-token store passed the veto and emitted a cold matrix — from a normal
// incompatible cache, not an exotic input.
func TestScoredAdmissionSeesTheColdStateItWillActuallyGet(t *testing.T) {
	const token = "go -race -count=1"

	cases := []struct {
		name       string
		st         *Store
		loadReason string
		wantUsable bool
	}{
		{"a store under this run's token is usable", &Store{Flags: token}, "", true},
		{"a store that has not committed to a token is usable", &Store{Flags: ""}, "", true},
		{"a store under another token is NOT usable", &Store{Flags: "go -count=100"}, "", false},
		{"an absent store is not usable", nil, "no store at x", false},
		{"a load reason makes a present store unusable", &Store{Flags: token}, "store x is empty", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			usable, reason := StoreUsableFor(c.st, c.loadReason, token)
			if usable != c.wantUsable {
				t.Fatalf("StoreUsableFor = %v, want %v (reason %q)", usable, c.wantUsable, reason)
			}
			if !usable && reason == "" {
				t.Error("an unusable store must say why; a silent cold start is the defect")
			}
		})
	}

	t.Run("a scored plan over a wrong-token store is refused, not cold-started", func(t *testing.T) {
		st := &Store{Flags: "go -count=100"}
		usable, _ := StoreUsableFor(st, "", token)
		if usable {
			t.Fatal("the fixture no longer models an incompatible store")
		}
		_, err := SelectBasis(BasisRequest{Basis: BasisReporter}, usable, WallStatusAbsent, true, 1)
		if !errors.Is(err, ErrScoredColdPlan) {
			t.Fatalf("a scored run that will cold-start must be refused, got %v", err)
		}
		// And the defect, stated as the thing that must no longer happen: the
		// old argument admitted it.
		if _, old := SelectBasis(BasisRequest{Basis: BasisReporter}, st != nil, WallStatusAbsent, true, 1); old == nil {
			t.Log("confirmed: passing `st != nil` admits the scored cold plan this test refuses")
		}
	})

	t.Run("BuildPlan and admission agree on the same predicate", func(t *testing.T) {
		// BuildPlan cold-starts exactly when StoreUsableFor says unusable. The
		// two were separate expressions; this is the control that keeps them one.
		for _, c := range cases {
			usable, _ := StoreUsableFor(c.st, c.loadReason, token)
			if usable != c.wantUsable {
				t.Errorf("%s: the shared predicate disagrees with its own table", c.name)
			}
		}
	})
}
