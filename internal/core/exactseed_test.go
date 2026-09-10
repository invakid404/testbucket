package core

import (
	"math"
	"testing"
)

// TestKKSeedsAboveTwoPow53StayDistinct is the F6 regression.
//
// The wall seed used to reach KK as `float64(seedNs)`. float64 has 53 bits of
// mantissa, so above 2^53 consecutive integers are no longer representable and
// two distinct nanosecond seeds collapse onto one value. The partition then
// stops being a function of the integer seeds §6.5 defines it on, silently:
// nothing is wrong with the output except that a different pair of inputs
// would have produced it.
func TestKKSeedsAboveTwoPow53StayDistinct(t *testing.T) {
	const twoPow53 = int64(1) << 53
	// Three consecutive integers straddling the boundary. In float64 the first
	// two are the SAME value.
	a, b, c := twoPow53, twoPow53+1, twoPow53+2
	if float64(a) != float64(b) {
		t.Fatalf("this platform distinguishes %d and %d in float64; the test's premise is gone", a, b)
	}

	items := []Item{
		{ID: "a", WeightNs: a, HasNs: true},
		{ID: "b", WeightNs: b, HasNs: true},
		{ID: "c", WeightNs: c, HasNs: true},
	}
	// The exact domain must order them c > b > a, which the float domain
	// cannot: it sees c > (a == b) and falls back to the ID tie-break.
	sorted := sortItems(items)
	want := []string{"c", "b", "a"}
	for i, id := range want {
		if sorted[i].ID != id {
			t.Errorf("sortItems[%d] = %q, want %q: the seeds are not being compared exactly (got %v)",
				i, sorted[i].ID, id, itemIDs(sorted))
			break
		}
	}

	// And the exact weights themselves must survive the round trip.
	for _, it := range items {
		got, _ := exactWeight(it).Float64()
		if int64(got) != it.WeightNs && exactWeight(it).IsInt() {
			num := exactWeight(it).Num()
			if !num.IsInt64() || num.Int64() != it.WeightNs {
				t.Errorf("exactWeight(%s) lost the value %d", it.ID, it.WeightNs)
			}
		}
	}
}

// TestKKNearMaxInt64DoesNotWrap: a part's load is the SUM of its items, and a
// sum near MaxInt64 is where an int64 accumulator would wrap into a negative
// load and invert every comparison after it. The rational domain has no such
// boundary.
func TestKKNearMaxInt64DoesNotWrap(t *testing.T) {
	big1 := int64(math.MaxInt64) / 2
	items := []Item{
		{ID: "a", WeightNs: big1, HasNs: true},
		{ID: "b", WeightNs: big1, HasNs: true},
		{ID: "c", WeightNs: big1, HasNs: true},
		{ID: "d", WeightNs: 1, HasNs: true},
	}
	buckets := karmarkarKarp(items, 2)
	if len(buckets) != 2 {
		t.Fatalf("got %d buckets, want 2", len(buckets))
	}
	seen := map[string]bool{}
	for _, b := range buckets {
		for _, it := range b {
			if seen[it.ID] {
				t.Errorf("item %s was placed twice", it.ID)
			}
			seen[it.ID] = true
		}
	}
	if len(seen) != len(items) {
		t.Errorf("placed %d of %d items; a wrapped load drops or duplicates work", len(seen), len(items))
	}
	// Two loads of MaxInt64/2 sum past MaxInt64 in int64. The partition must
	// still be sane: the heaviest bucket holds at most three of them.
	for i, b := range buckets {
		if len(b) > 3 {
			t.Errorf("bucket %d holds %d of 4 items; the balance collapsed", i, len(b))
		}
	}
}

// TestReporterWeightsAreUnchangedByTheExactDomain: the reporter basis packs by
// float seconds and its behaviour must not move. A rational built from a
// float64 is exactly that float64, so the ordering is identical.
func TestReporterWeightsAreUnchangedByTheExactDomain(t *testing.T) {
	items := []Item{
		{ID: "a", Weight: 1.5}, {ID: "b", Weight: 2.5},
		{ID: "c", Weight: 0.1}, {ID: "d", Weight: 2.5},
	}
	sorted := sortItems(items)
	want := []string{"b", "d", "a", "c"} // heaviest first, ties by ID
	for i, id := range want {
		if sorted[i].ID != id {
			t.Errorf("reporter ordering changed: got %v, want %v", itemIDs(sorted), want)
			break
		}
	}
}

func itemIDs(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}
