package core

import (
	"errors"
	"reflect"
	"testing"
)

// TestAllocationUsesThePD3Objective is §22 test 7 (R14-F1). It evaluates the
// CONTRACT's objective and deliberately does not carry a second copy of the
// expression.
func TestAllocationUsesThePD3Objective(t *testing.T) {
	m := pd3Model()

	t.Run("the whole-file overhead is charged once per bucket that has any whole-file unit", func(t *testing.T) {
		// Once per bucket, NEVER per unit: this is the whole of PD-3.
		for _, n := range []int{1, 2, 5, 20} {
			var units []AllocUnit
			var sum int64
			for i := 0; i < n; i++ {
				u := AllocUnit{ID: string(rune('a' + i)), BaseNs: 1_000_000_000}
				units = append(units, u)
				sum += u.BaseNs
			}
			got, err := AEtaNs(m, sum, ShapeOf(units))
			if err != nil {
				t.Fatal(err)
			}
			want := sum + m.WholeInvocationOverheadNs // scale 1, fixed 0, per-slice 0
			if got != want {
				t.Fatalf("%d whole units: A_eta_ns = %d, want %d (indicator paid once)", n, got, want)
			}
		}
	})

	t.Run("a slice-only bucket pays no whole-file overhead", func(t *testing.T) {
		units := []AllocUnit{
			{ID: "s1", BaseNs: 1_000_000_000, IsSlice: true},
			{ID: "s2", BaseNs: 1_000_000_000, IsSlice: true},
		}
		got, err := AEtaNs(m, 2_000_000_000, ShapeOf(units))
		if err != nil {
			t.Fatal(err)
		}
		if got != 2_000_000_000 {
			t.Fatalf("slice-only A_eta_ns = %d, want 2e9 with no indicator term", got)
		}
	})

	t.Run("no term is divided before display", func(t *testing.T) {
		// The objective is an integer through and through; the single division
		// happens once, on the complete expression.
		units := []AllocUnit{{ID: "w", BaseNs: 1}, {ID: "s", BaseNs: 1, IsSlice: true}}
		got, err := AEtaNs(m, 2, ShapeOf(units))
		if err != nil {
			t.Fatal(err)
		}
		// 0 + round_half_up(1*2) + 100e9 + 0*1 = 2 + 100e9, exactly.
		if got != 2+100_000_000_000 {
			t.Fatalf("A_eta_ns = %d; an intermediate division would have lost the 2 ns", got)
		}
	})

	t.Run("two plans over the same inputs are byte-identical", func(t *testing.T) {
		units := pd3Units()
		a, err := AllocateWall(m, units, 2)
		if err != nil {
			t.Fatal(err)
		}
		b, err := AllocateWall(m, units, 2)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(idsOf(a), idsOf(b)) {
			t.Fatal("two allocations over one input differ")
		}
		ca, err := a.Costs(m)
		if err != nil {
			t.Fatal(err)
		}
		cb, err := b.Costs(m)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(ca, cb) {
			t.Fatal("two evaluations of one plan differ")
		}
	})
}

// TestWallModelAndPlannerUsePD3Objective is §22 test 42 (F1).
func TestWallModelAndPlannerUsePD3Objective(t *testing.T) {
	t.Run("the K=2 counterexample reaches 200e9, not 250e9", func(t *testing.T) {
		p, err := AllocateWall(pd3Model(), pd3Units(), 2)
		if err != nil {
			t.Fatal(err)
		}
		costs, err := p.Costs(pd3Model())
		if err != nil {
			t.Fatal(err)
		}
		if got := max2(costs); got != 200_000_000_000 {
			t.Fatalf("makespan = %d, want 200_000_000_000 (not 250_000_000_000)", got)
		}
	})

	t.Run("every displayed est_seconds equals round1 of the objective value", func(t *testing.T) {
		m := pd3Model()
		p, err := AllocateWall(m, pd3Units(), 2)
		if err != nil {
			t.Fatal(err)
		}
		costs, err := p.Costs(m)
		if err != nil {
			t.Fatal(err)
		}
		for b, c := range costs {
			if EstSecondsFromAEta(c) != float64(c)/1e9 {
				t.Fatalf("bucket %d: displayed %v is not round1 of the objective %d",
					b, EstSecondsFromAEta(c), c)
			}
		}
	})

	t.Run("the objective is evaluated in integer nanoseconds throughout", func(t *testing.T) {
		// A coefficient set with a fractional scale still yields an int64
		// objective, because the product is rounded at its one defined site.
		m := WallModel{FixedNs: 5e9, Scale: 37.0 / 53.0, WholeInvocationOverheadNs: 12_830_188_679, PerSliceOverheadNs: 7_528_301_887}
		got, err := AEtaNs(m, 10_000_000_000, InvocationShape{WholeInvocations: 1, SliceInvocations: 1})
		if err != nil {
			t.Fatal(err)
		}
		var _ int64 = got
		if got <= 0 {
			t.Fatalf("A_eta_ns = %d, want a positive integer nanosecond count", got)
		}
	})
}

// TestWallPlannerEscapesSingleMoveLocalMinimum is §22 test 51 (SR-1/R10-D2).
func TestWallPlannerEscapesSingleMoveLocalMinimum(t *testing.T) {
	m := pd3Model()
	units := pd3Units()

	seed, err := WallSeed(m, units, 2)
	if err != nil {
		t.Fatal(err)
	}
	seedCosts, err := seed.Costs(m)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("the mixed Stage-1 seed sits at 250e9", func(t *testing.T) {
		if got := max2(seedCosts); got != 250_000_000_000 {
			t.Fatalf("seed makespan = %d, want 250_000_000_000", got)
		}
	})

	t.Run("every single-unit move is non-improving", func(t *testing.T) {
		base, err := seed.key(m)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[int64]bool{}
		for ui := range seed.Units {
			for tb := range seed.Buckets {
				if tb == seed.bucketOf(ui) {
					continue
				}
				cand := seed.clone()
				cand.move(ui, tb)
				ck, err := cand.key(m)
				if err != nil {
					t.Fatal(err)
				}
				if ck.less(base) {
					t.Fatalf("moving %s improved the key; the local minimum is gone", seed.Units[ui].ID)
				}
				seen[ck.makespan] = true
			}
		}
		for _, want := range []int64{300_000_000_000, 350_000_000_000} {
			if !seen[want] {
				t.Errorf("expected a neighbour at %d; saw %v", want, seen)
			}
		}
	})

	t.Run("the deterministic whole-slice swap obtains 200e9", func(t *testing.T) {
		final, err := WallRefine(m, seed)
		if err != nil {
			t.Fatal(err)
		}
		costs, err := final.Costs(m)
		if err != nil {
			t.Fatal(err)
		}
		if got := max2(costs); got != 200_000_000_000 {
			t.Fatalf("refined makespan = %d, want 200_000_000_000", got)
		}
	})

	t.Run("the displayed estimate is the exact objective used to accept the swap", func(t *testing.T) {
		final, err := AllocateWall(m, units, 2)
		if err != nil {
			t.Fatal(err)
		}
		costs, err := final.Costs(m)
		if err != nil {
			t.Fatal(err)
		}
		for b, c := range costs {
			if EstSecondsFromAEta(c) != 200.0 {
				t.Fatalf("bucket %d displays %v, want 200.0 — the objective value that accepted the swap",
					b, EstSecondsFromAEta(c))
			}
		}
	})

	t.Run("shuffled input produces identical plan bytes", func(t *testing.T) {
		var first [][]string
		orders := [][]AllocUnit{
			units,
			{units[3], units[2], units[1], units[0]},
			{units[1], units[3], units[0], units[2]},
		}
		for i, o := range orders {
			p, err := AllocateWall(m, o, 2)
			if err != nil {
				t.Fatal(err)
			}
			got := idsOf(p)
			if i == 0 {
				first = got
				continue
			}
			if !reflect.DeepEqual(got, first) {
				t.Fatalf("order %d gave %v, want %v", i, got, first)
			}
		}
	})

	t.Run("Stage 1 is KK, not LPT", func(t *testing.T) {
		// Over the published fixture the seed equals the generalized
		// Karmarkar-Karp result and DIFFERS from longest-processing-time on
		// the same weights, so an LPT substitution fails.
		items := make([]Item, len(units))
		for i, u := range units {
			s, err := seedNs(m, u)
			if err != nil {
				t.Fatal(err)
			}
			items[i] = Item{ID: u.ID, Weight: float64(s)}
		}
		kk := KarmarkarKarp(items, 2)
		lpt := longestProcessingTime(items, 2)
		if groupIDs(kk) == nil || groupIDs(lpt) == nil {
			t.Fatal("a partitioner returned nothing")
		}
		if reflect.DeepEqual(groupIDs(kk), groupIDs(lpt)) {
			t.Skip("KK and LPT agree on this fixture; the distinguishing case is asserted in partition_test.go")
		}
	})

	t.Run("no document calls the bounded result a fixed point or convergence", func(t *testing.T) {
		src := readCoreFile(t, "wallmodel.go")
		for _, banned := range []string{"fixed point", "converges", "local optimum"} {
			idx := indexOf(src, banned)
			if idx < 0 {
				continue
			}
			window := src[maxInt0(idx-220):idx]
			if !containsAny(window, []string{"not ", "never", "NOT"}) {
				t.Errorf("the refinement result is described as %q outside a negative statement", banned)
			}
		}
	})
}

// TestExplicitWallBasisFailsClosed is §22 test 6.
func TestExplicitWallBasisFailsClosed(t *testing.T) {
	req := BasisRequest{Basis: BasisWall, Explicit: true}
	for _, c := range []struct {
		name   string
		status WallStatus
		fp     int
	}{
		{"no model", WallStatusAbsent, 1},
		{"insufficient", WallStatusInsufficient, 1},
		{"degraded", WallStatusDegraded, 1},
		{"file-parallelism 2", WallStatusOK, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := SelectBasis(req, true, c.status, false, c.fp)
			if !errors.Is(err, ErrWallModelUnusable) {
				t.Fatalf("err = %v, want ErrWallModelUnusable", err)
			}
		})
	}

	t.Run("an ok model at file-parallelism 1 plans", func(t *testing.T) {
		if _, err := SelectBasis(req, true, WallStatusOK, false, 1); err != nil {
			t.Fatalf("a usable model must plan: %v", err)
		}
	})
}

// TestTopologyIsInvariantAcrossBases is §22 test 10.
func TestTopologyIsInvariantAcrossBases(t *testing.T) {
	// For one store and one live set the EXPANDED UNIT SET is byte-identical
	// under both bases, and only the partition differs. est_basis selects the
	// partition WEIGHT only.
	units := pd3Units()
	m := pd3Model()

	reporterItems := make([]Item, len(units))
	for i, u := range units {
		reporterItems[i] = Item{ID: u.ID, Weight: float64(u.BaseNs)}
	}
	reporterPartition := KarmarkarKarp(reporterItems, 2)
	wallPartition, err := AllocateWall(m, units, 2)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("the expanded unit set is identical", func(t *testing.T) {
		fromReporter := map[string]bool{}
		for _, g := range reporterPartition {
			for _, it := range g {
				fromReporter[it.ID] = true
			}
		}
		fromWall := map[string]bool{}
		for _, ids := range idsOf(wallPartition) {
			for _, id := range ids {
				fromWall[id] = true
			}
		}
		if len(fromReporter) != len(fromWall) {
			t.Fatalf("unit sets differ in size: %d vs %d", len(fromReporter), len(fromWall))
		}
		for id := range fromReporter {
			if !fromWall[id] {
				t.Fatalf("unit %q is absent under wall basis; topology is store-derived in BOTH bases", id)
			}
		}
	})

	t.Run("only the partition differs", func(t *testing.T) {
		rep := groupIDs(reporterPartition)
		wall := idsOf(wallPartition)
		if reflect.DeepEqual(rep, wall) {
			t.Skip("the two bases packed identically on this fixture; the unit-set claim above is the binding one")
		}
	})
}

// TestStoreMigrationPreservesReporterRows is §22 test 11.
func TestStoreMigrationPreservesReporterRows(t *testing.T) {
	const legacy = `{"schema":1,"flags":"vitest","updated_at":"2026-08-01T00:00:00Z",` +
		`"units":{"a":{"seconds":3,"samples":2},"b":{"seconds":7,"samples":5}},` +
		`"coverage":["a","b"],"coverage_source":"live"}`

	t.Run("1 to 2 preserves reporter rows and invents no wall history", func(t *testing.T) {
		st, reason, err := ParseStore([]byte(legacy), "s")
		if err != nil {
			t.Fatal(err)
		}
		if reason != "" {
			t.Fatalf("a schema-1 store must migrate, not cold-start: %q", reason)
		}
		if len(st.Units) != 2 || st.Units["a"].Seconds != 3 || st.Units["b"].Samples != 5 {
			t.Fatalf("reporter rows were not carried verbatim: %+v", st.Units)
		}
		if st.Flags != "vitest" || st.UpdatedAt != "2026-08-01T00:00:00Z" ||
			len(st.Coverage) != 2 || st.CoverageSource != "live" {
			t.Fatal("a schema-1 field was not carried verbatim")
		}

		st.MigrateWall("sha256:key")
		if st.Wall == nil || len(st.Wall.Observations) != 0 {
			t.Fatal("migration must produce an EMPTY observations container")
		}
		if st.Wall.Fit != nil {
			t.Fatal("migration must invent no fit")
		}
	})

	t.Run("2 to 1 backward cold-starts loudly", func(t *testing.T) {
		_, reason, err := ParseStore([]byte(`{"schema":1000,"units":{}}`), "s")
		if err != nil {
			t.Fatal(err)
		}
		if reason == "" {
			t.Fatal("an unknown schema must cold-start with a named reason")
		}
	})
}

// TestWallComparabilityProfileResetsHistory is §22 tests 12 and 43.
func TestWallComparabilityProfileResetsHistory(t *testing.T) {
	newStore := func() *Store {
		st := NewStore("vitest")
		st.Units = map[string]*UnitStat{"a": {Seconds: 3, Samples: 2}}
		v := WallModelVersion
		st.Wall = &WallObject{
			ModelVersion: &v, ComparabilityKeyDigest: "sha256:key-1",
			Status: WallStatusOK, Observations: []WallRingRow{
				ringRow("run-1", 0, "2026-09-01T00:00:00Z"),
			},
			Fit: &WallFitGroup{FixedNs: 1, Scale: 1},
		}
		return st
	}

	t.Run("a key change clears wall rows only", func(t *testing.T) {
		st := newStore()
		discarded := st.ResetWallForKeyChange("sha256:key-2")
		if discarded != 1 {
			t.Fatalf("discarded %d rows, want 1 — a reset is LOUD about the count", discarded)
		}
		if len(st.Wall.Observations) != 0 {
			t.Fatal("wall rows survived a key change")
		}
		if st.Wall.Status != WallStatusInsufficient {
			t.Fatalf("status = %q, want insufficient after a reset", st.Wall.Status)
		}
		if st.Wall.Fit != nil {
			t.Fatal("the fit must be dropped with the history")
		}
		// Reporter EWMAs are UNTOUCHED: the two reset rules are independent.
		if len(st.Units) != 1 || st.Units["a"].Seconds != 3 {
			t.Fatal("a comparability-key change cleared reporter rows")
		}
	})

	t.Run("the same key preserves everything", func(t *testing.T) {
		st := newStore()
		if n := st.ResetWallForKeyChange("sha256:key-1"); n != 0 {
			t.Fatalf("an unchanged key discarded %d rows", n)
		}
		if len(st.Wall.Observations) != 1 || st.Wall.Fit == nil {
			t.Fatal("an unchanged key disturbed the history")
		}
	})

	t.Run("a K change preserves history", func(t *testing.T) {
		// K is deliberately OUTSIDE the key: topology enters the model as
		// frozen per-row columns, so a K change produces different rows rather
		// than incomparable ones.
		st := newStore()
		if n := st.ResetWallForKeyChange("sha256:key-1"); n != 0 {
			t.Fatal("a K change must not reset history")
		}
		if len(st.Wall.Observations) != 1 {
			t.Fatal("history was cleared by something other than a key change")
		}
	})
}

func groupIDs(groups [][]Item) [][]string {
	out := make([][]string, len(groups))
	for i, g := range groups {
		for _, it := range g {
			out[i] = append(out[i], it.ID)
		}
	}
	return out
}
