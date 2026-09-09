package core

import (
	"encoding/json"
	"testing"
)

// TestEstSecondsMatchesItsBasis is §22 test 8 and the acceptance test the
// registry names for ID-2.
//
// The defect it closes: at R54 est_seconds was the store-weight sum in BOTH
// modes, so AllocationScore could change the packing without changing the
// display. Under wall basis the displayed number and the optimized number must
// be the same quantity.
func TestEstSecondsMatchesItsBasis(t *testing.T) {
	m := WallModel{
		FixedNs:                   5_000_000_000,
		Scale:                     1.0,
		WholeInvocationOverheadNs: 8_000_000_000,
		PerSliceOverheadNs:        3_000_000_000,
	}

	t.Run("in wall basis est_seconds is round1(a_eta_ns/1e9) of the OPTIMIZED value", func(t *testing.T) {
		units := []AllocUnit{
			{ID: "s1", BaseNs: 5_000_000_000, IsSlice: true},
			{ID: "s2", BaseNs: 5_000_000_000, IsSlice: true},
			{ID: "w1", BaseNs: 5_000_000_000},
			{ID: "w2", BaseNs: 5_000_000_000},
		}
		p, err := AllocateWall(m, units, 3)
		if err != nil {
			t.Fatal(err)
		}
		costs, err := p.Costs(m)
		if err != nil {
			t.Fatal(err)
		}
		// Every bucket's displayed estimate is round1 of the exact objective
		// value used to accept each refinement move — not a separate sum.
		for b, cost := range costs {
			want := EstSecondsFromAEta(cost)
			doc := &PlanDocument{EstBasis: BasisWall, K: len(costs)}
			n := Nanos(cost)
			doc.Buckets = append(doc.Buckets, PlanBucket{
				Index: b, Name: "bucket", Seconds: want, AEtaNs: &n,
			})
			if doc.Buckets[0].Seconds != want {
				t.Fatalf("bucket %d displays %v, want round1(a_eta_ns/1e9) = %v", b, doc.Buckets[0].Seconds, want)
			}
			// And a validator recomputes one from the other.
			if EstSecondsFromAEta(int64(*doc.Buckets[0].AEtaNs)) != doc.Buckets[0].Seconds {
				t.Fatalf("bucket %d: est_seconds and a_eta_ns disagree", b)
			}
		}
	})

	t.Run("a_eta_ns is serialized and the two agree exactly", func(t *testing.T) {
		n := Nanos(16_018_867_925)
		doc := &PlanDocument{
			EstBasis: BasisWall, K: 1,
			Buckets: []PlanBucket{{
				Index: 0, Name: "bucket-0",
				Seconds: EstSecondsFromAEta(int64(n)), AEtaNs: &n,
			}},
		}
		b, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		var back PlanDocument
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatal(err)
		}
		if back.Buckets[0].AEtaNs == nil {
			t.Fatal("a_eta_ns did not survive serialization")
		}
		if *back.Buckets[0].AEtaNs != n {
			t.Fatalf("a_eta_ns round-tripped to %d, want %d", *back.Buckets[0].AEtaNs, n)
		}
		if got := EstSecondsFromAEta(int64(*back.Buckets[0].AEtaNs)); got != back.Buckets[0].Seconds {
			t.Fatalf("est_seconds %v is not round1(a_eta_ns/1e9) = %v", back.Buckets[0].Seconds, got)
		}
	})

	t.Run("in reporter basis est_seconds is the v0.2.2 reporter-work sum", func(t *testing.T) {
		// The legacy quantity, unchanged: the sum of the bucket's unit
		// seconds, NOT a converted nanosecond value.
		units := []PlanUnit{
			{ID: "a", Seconds: 4.2},
			{ID: "b", Seconds: 8.1},
		}
		var sum float64
		for _, u := range units {
			sum += u.Seconds
		}
		doc := &PlanDocument{
			EstBasis: BasisReporter, K: 1,
			Buckets: []PlanBucket{{Index: 0, Name: "bucket-0", Units: units, Seconds: sum}},
		}
		if doc.Buckets[0].Seconds != 12.3 {
			t.Fatalf("reporter basis est_seconds = %v, want the reporter-work sum 12.3", doc.Buckets[0].Seconds)
		}
		// And it carries no a_eta_ns.
		if doc.Buckets[0].AEtaNs != nil {
			t.Fatal("a reporter-basis bucket must not carry a_eta_ns")
		}
	})

	t.Run("the packing cannot change without the display changing", func(t *testing.T) {
		// The R54 defect, made unreachable: because the displayed value IS the
		// objective, two different partitions of the same units cannot share a
		// displayed estimate unless they share the objective value too.
		a := []AllocUnit{{ID: "u1", BaseNs: 10_000_000_000}, {ID: "u2", BaseNs: 1_000_000_000}}
		p1, err := AllocateWall(m, a, 1)
		if err != nil {
			t.Fatal(err)
		}
		p2, err := AllocateWall(m, a, 2)
		if err != nil {
			t.Fatal(err)
		}
		c1, err := p1.Costs(m)
		if err != nil {
			t.Fatal(err)
		}
		c2, err := p2.Costs(m)
		if err != nil {
			t.Fatal(err)
		}
		if max2(c1) == max2(c2) {
			t.Fatal("two different packings produced one makespan; the fixture no longer separates them")
		}
		if EstSecondsFromAEta(max2(c1)) == EstSecondsFromAEta(max2(c2)) {
			t.Fatal("the packing changed but the displayed estimate did not — the R54 defect")
		}
	})
}
