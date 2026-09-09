package core

import (
	"reflect"
	"testing"
)

// pd3Model is the coefficient set scope.md §7.2 publishes for the K=2
// counterexample: scale = 1 dimensionless, fixed_ns = 0, whole overhead 100 s,
// per-slice overhead 0.
func pd3Model() WallModel {
	return WallModel{
		FixedNs:                   0,
		Scale:                     1,
		WholeInvocationOverheadNs: 100_000_000_000,
		PerSliceOverheadNs:        0,
	}
}

// pd3Units is §7.2's input set: two whole-file units at 50 s and two name
// slices at 100 s.
func pd3Units() []AllocUnit {
	return []AllocUnit{
		{ID: "s1", BaseNs: 100_000_000_000, IsSlice: true},
		{ID: "s2", BaseNs: 100_000_000_000, IsSlice: true},
		{ID: "w1", BaseNs: 50_000_000_000, IsSlice: false},
		{ID: "w2", BaseNs: 50_000_000_000, IsSlice: false},
	}
}

func idsOf(p WallPartition) [][]string {
	out := make([][]string, len(p.Buckets))
	for b := range p.Buckets {
		for _, ui := range p.Buckets[b] {
			out[b] = append(out[b], p.Units[ui].ID)
		}
	}
	return out
}

// TestAllocationObjectiveEqualsAetaForWholeAndSliceTopologies is §22 test 35
// (PD-3): the optimized objective equals the displayed round1(a_eta_ns/1e9)
// for whole-only, slice-only and mixed buckets, and §7.2's K=2 counterexample
// partitions to makespan 200, not 250.
func TestAllocationObjectiveEqualsAetaForWholeAndSliceTopologies(t *testing.T) {
	m := pd3Model()

	t.Run("the four-term objective on each topology", func(t *testing.T) {
		cases := []struct {
			name  string
			units []AllocUnit
			want  int64
		}{
			// whole-only: indicator paid once, no slice term.
			{"whole-only", []AllocUnit{
				{ID: "w1", BaseNs: 50_000_000_000},
				{ID: "w2", BaseNs: 50_000_000_000},
			}, 200_000_000_000},
			// slice-only: no indicator at all.
			{"slice-only", []AllocUnit{
				{ID: "s1", BaseNs: 100_000_000_000, IsSlice: true},
				{ID: "s2", BaseNs: 100_000_000_000, IsSlice: true},
			}, 200_000_000_000},
			// mixed: indicator once, plus one slice term (which is 0 here).
			{"mixed", []AllocUnit{
				{ID: "s1", BaseNs: 100_000_000_000, IsSlice: true},
				{ID: "w1", BaseNs: 50_000_000_000},
			}, 250_000_000_000},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				var w []int64
				for _, u := range c.units {
					w = append(w, u.BaseNs)
				}
				sum, err := sumNsForTest(w)
				if err != nil {
					t.Fatal(err)
				}
				got, err := AEtaNs(m, sum, ShapeOf(c.units))
				if err != nil {
					t.Fatal(err)
				}
				if got != c.want {
					t.Fatalf("A_eta_ns = %d, want %d", got, c.want)
				}
				// The displayed value is round1 of the SAME expression.
				if d := EstSecondsFromAEta(got); d != float64(c.want)/1e9 {
					t.Fatalf("est_seconds = %v, want %v", d, float64(c.want)/1e9)
				}
			})
		}
	})

	t.Run("the whole-file overhead is charged once per bucket, never per unit", func(t *testing.T) {
		// Four whole-file units in one bucket still pay the indicator once.
		units := []AllocUnit{
			{ID: "a", BaseNs: 1}, {ID: "b", BaseNs: 1},
			{ID: "c", BaseNs: 1}, {ID: "d", BaseNs: 1},
		}
		got, err := AEtaNs(m, 4, ShapeOf(units))
		if err != nil {
			t.Fatal(err)
		}
		if want := int64(4 + 100_000_000_000); got != want {
			t.Fatalf("A_eta_ns = %d, want %d (indicator paid once)", got, want)
		}
	})

	t.Run("the K=2 counterexample partitions to 200e9, not 250e9", func(t *testing.T) {
		units := pd3Units()

		// Stage 1 alone reproduces the additive-packing trap §7.2 names.
		seed, err := WallSeed(m, units, 2)
		if err != nil {
			t.Fatal(err)
		}
		seedCosts, err := seed.Costs(m)
		if err != nil {
			t.Fatal(err)
		}
		seedMax := max2(seedCosts)
		if seedMax != 250_000_000_000 {
			t.Fatalf("Stage-1 seed makespan = %d, want 250_000_000_000 (the mixed trap)", seedMax)
		}

		// Stage 2 escapes it with the whole<->slice swap.
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
		// Both buckets cost exactly 200e9: one slice-only, one whole-only.
		for b, c := range costs {
			if c != 200_000_000_000 {
				t.Fatalf("bucket %d costs %d, want 200_000_000_000; grouping %v", b, c, idsOf(final))
			}
		}
		// Topology is segregated, which is the point of PD-3.
		got := idsOf(final)
		want := [][]string{{"s1", "s2"}, {"w1", "w2"}}
		if !reflect.DeepEqual(got, want) && !reflect.DeepEqual(got, [][]string{{"w1", "w2"}, {"s1", "s2"}}) {
			t.Fatalf("grouping = %v, want the topology-segregated split", got)
		}
	})

	t.Run("every single-unit move from the seed is non-improving", func(t *testing.T) {
		// §22 test 51's 100/300 and 150/350 witnesses: the seed sits in a
		// single-move local minimum, which is why neighborhood (2b) exists.
		units := pd3Units()
		seed, err := WallSeed(m, units, 2)
		if err != nil {
			t.Fatal(err)
		}
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
					t.Fatalf("single-unit move of %s improved the key; the local minimum is gone",
						seed.Units[ui].ID)
				}
				seen[ck.makespan] = true
			}
		}
		for _, want := range []int64{300_000_000_000, 350_000_000_000} {
			if !seen[want] {
				t.Errorf("expected a single-move neighbour at makespan %d; saw %v", want, seen)
			}
		}
	})

	t.Run("refinement is deterministic across shuffled input order", func(t *testing.T) {
		orders := [][]AllocUnit{
			pd3Units(),
			{pd3Units()[3], pd3Units()[1], pd3Units()[2], pd3Units()[0]},
			{pd3Units()[2], pd3Units()[3], pd3Units()[0], pd3Units()[1]},
		}
		var first [][]string
		for i, us := range orders {
			p, err := AllocateWall(m, us, 2)
			if err != nil {
				t.Fatal(err)
			}
			got := idsOf(p)
			if i == 0 {
				first = got
				continue
			}
			if !reflect.DeepEqual(got, first) {
				t.Fatalf("shuffled input order %d gave %v, want %v", i, got, first)
			}
		}
	})
}

func max2(v []int64) int64 {
	var m int64
	for _, x := range v {
		if x > m {
			m = x
		}
	}
	return m
}
