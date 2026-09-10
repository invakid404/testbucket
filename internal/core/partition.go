// Package core is testbucket's language-agnostic engine: the Karmarkar-Karp
// k-way partitioner, the rolling timing store (load / EWMA-merge / persist),
// whale detection and the count-shard vs name-slice unit model, the
// never-drop-a-test coverage gate, and the plan + audit logic — plus the
// single K knob and the cold-start mean weight.
//
// It knows nothing about `go`, `go test`, `go list`, GOWORK or any toolchain.
// Everything framework-specific — discovery, timing ingest, invocation
// rendering, command-grammar validation — reaches it through the
// runner.Runner seam. That is what lets a second framework adapter (e.g.
// Vitest) reuse this package unchanged; see internal/runner and the Go adapter
// in internal/runner/gorunner.
package core

import (
	"math/big"
	"sort"
)

// Item is one weighted thing to place into a bucket. The partitioners know
// nothing about tests or CI — they take (items, k) and return k disjoint
// groups whose union is exactly the input. That is deliberate: the balancer is
// the project-agnostic half of this tool.
type Item struct {
	ID     string
	Weight float64
	// WeightNs is the weight in EXACT integer nanoseconds, and HasNs says it
	// is the one to use. The wall basis seeds KK with integer nanosecond
	// weights, and casting those to float64 collapses distinct values above
	// 2^53; carrying the integer means the partition stays an exact function
	// of the seeds §6.5 defines it on.
	WeightNs int64
	HasNs    bool
}

// sortItems orders items heaviest-first, breaking ties by ID so the whole
// pipeline is a pure function of (store, K). A re-run must never reshuffle the
// buckets: a reviewer comparing two plans should see a diff only when the
// timings actually moved.
func sortItems(items []Item) []Item {
	out := append([]Item(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		if c := exactWeight(out[i]).Cmp(exactWeight(out[j])); c != 0 {
			return c > 0
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// kkPart is one bucket-in-progress inside a Karmarkar-Karp tuple.
//
// load is the NORMALISED load: after every merge the smallest part is
// subtracted from all k parts, because KK only ever needs the differences
// between parts, not their absolute sums. The true loads are recomputed from
// the item sets at the end.
type kkPart struct {
	load  *big.Rat
	items []Item
}

// tag is the smallest item ID in the part, or an empty-part sentinel. It is
// the deterministic tie-break: loads collide constantly (every part starts at
// 0), and without a total order the merge order — and therefore the whole
// plan — would depend on map/slice happenstance.
func (p kkPart) tag() string {
	best := ""
	for _, it := range p.items {
		if best == "" || it.ID < best {
			best = it.ID
		}
	}
	if best == "" {
		// Sorts after every real ID, so empty parts trail equal-load parts.
		return "￿"
	}
	return best
}

type kkTuple struct {
	parts []kkPart
}

// diff is the tuple's spread — the quantity KK greedily attacks by always
// combining the two most-spread tuples.
func (t kkTuple) diff() *big.Rat {
	if len(t.parts) == 0 {
		return new(big.Rat)
	}
	return new(big.Rat).Sub(t.parts[0].load, t.parts[len(t.parts)-1].load)
}

func (t kkTuple) tag() string {
	best := ""
	for _, p := range t.parts {
		tg := p.tag()
		if tg == "￿" {
			continue
		}
		if best == "" || tg < best {
			best = tg
		}
	}
	if best == "" {
		return "￿"
	}
	return best
}

// sortParts orders a tuple's parts heaviest-first with a deterministic
// tie-break.
func sortParts(parts []kkPart) {
	sort.SliceStable(parts, func(i, j int) bool {
		if c := parts[i].load.Cmp(parts[j].load); c != 0 {
			return c > 0
		}
		return parts[i].tag() < parts[j].tag()
	})
}

// karmarkarKarp partitions items into k groups using the largest-differencing
// method, generalised to k-way.
//
// Each item starts as its own tuple (one loaded part, k-1 empty). Repeatedly:
// take the two tuples with the largest spread, and merge them by pairing the
// heaviest part of one with the lightest part of the other — the k-way
// generalisation of "put the two biggest numbers on opposite sides". Then
// re-normalise. When one tuple is left, its parts are the buckets.
//
// KK shares LPT's (4/3 - 1/(3k)) worst-case bound but is markedly better on
// average, which is why the design starts here rather than at LPT: the plan is
// regenerated on every CI run, so a tighter partition is bought once and paid
// for never.
func karmarkarKarp(items []Item, k int) [][]Item {
	if k <= 0 {
		return nil
	}
	buckets := make([][]Item, k)
	if len(items) == 0 {
		return buckets
	}

	tuples := make([]kkTuple, 0, len(items))
	for _, it := range sortItems(items) {
		parts := make([]kkPart, k)
		parts[0] = kkPart{load: exactWeight(it), items: []Item{it}}
		for e := 1; e < k; e++ {
			parts[e].load = new(big.Rat)
		}
		tuples = append(tuples, kkTuple{parts: parts})
	}

	for len(tuples) > 1 {
		i, j := twoLargest(tuples)
		a, b := tuples[i], tuples[j]
		merged := mergeTuples(a, b, k)
		// Remove the two consumed tuples (higher index first) and append the
		// merged one. Order within the slice never matters: selection is by
		// (diff, tag), both of which are total.
		hi, lo := i, j
		if hi < lo {
			hi, lo = lo, hi
		}
		tuples = append(tuples[:hi], tuples[hi+1:]...)
		tuples = append(tuples[:lo], tuples[lo+1:]...)
		tuples = append(tuples, merged)
	}

	final := tuples[0]
	// Recompute true loads: the running `load` values are normalised
	// differences, not sums.
	loaded := make([]kkPart, k)
	for idx, p := range final.parts {
		total := new(big.Rat)
		for _, it := range p.items {
			total.Add(total, exactWeight(it))
		}
		loaded[idx] = kkPart{load: total, items: p.items}
	}
	sortParts(loaded)
	for idx := range loaded {
		buckets[idx] = sortItems(loaded[idx].items)
	}
	return buckets
}

// twoLargest returns the indices of the two most-spread tuples, ties broken by
// tuple tag so the choice is a pure function of the input.
func twoLargest(tuples []kkTuple) (int, int) {
	first, second := -1, -1
	better := func(cand, cur int) bool {
		if cur < 0 {
			return true
		}
		dc, dr := tuples[cand].diff(), tuples[cur].diff()
		if c := dc.Cmp(dr); c != 0 {
			return c > 0
		}
		return tuples[cand].tag() < tuples[cur].tag()
	}
	for idx := range tuples {
		if better(idx, first) {
			second = first
			first = idx
			continue
		}
		if better(idx, second) {
			second = idx
		}
	}
	return first, second
}

// mergeTuples performs the k-way differencing step: a's parts descending are
// paired with b's parts ascending, so the heaviest meets the lightest.
func mergeTuples(a, b kkTuple, k int) kkTuple {
	ap := append([]kkPart(nil), a.parts...)
	bp := append([]kkPart(nil), b.parts...)
	sortParts(ap)
	sortParts(bp)

	parts := make([]kkPart, k)
	for i := 0; i < k; i++ {
		other := bp[k-1-i]
		items := make([]Item, 0, len(ap[i].items)+len(other.items))
		items = append(items, ap[i].items...)
		items = append(items, other.items...)
		parts[i] = kkPart{load: new(big.Rat).Add(ap[i].load, other.load), items: items}
	}
	sortParts(parts)
	// Normalise: only inter-part differences carry information from here on.
	base := new(big.Rat).Set(parts[k-1].load)
	for i := range parts {
		parts[i].load = new(big.Rat).Sub(parts[i].load, base)
	}
	return kkTuple{parts: parts}
}

// longestProcessingTime is the classic greedy baseline: heaviest item to the
// currently lightest bucket. It is retained as the reference the KK partition
// is measured against in the tests — the design proposed starting here, the
// owner decision upgraded to KK, and keeping LPT one function away means that
// decision stays checkable rather than assumed.
func longestProcessingTime(items []Item, k int) [][]Item {
	if k <= 0 {
		return nil
	}
	buckets := make([][]Item, k)
	loads := make([]float64, k)
	for _, it := range sortItems(items) {
		best := 0
		for i := 1; i < k; i++ {
			if loads[i] < loads[best] {
				best = i
			}
		}
		buckets[best] = append(buckets[best], it)
		loads[best] += it.Weight
	}
	for i := range buckets {
		buckets[i] = sortItems(buckets[i])
	}
	return buckets
}

// makespan is the load of the heaviest bucket: the wall-time the matrix
// actually costs.
func makespan(buckets [][]Item) float64 {
	heaviest := 0.0
	for _, b := range buckets {
		total := 0.0
		for _, it := range b {
			total += it.Weight
		}
		if total > heaviest {
			heaviest = total
		}
	}
	return heaviest
}

// exactWeight is the item's weight in the EXACT rational domain.
//
// KK's loads used to be float64, and the wall seed handed it int64 nanosecond
// weights cast to float64. Above 2^53 two distinct seeds collapse onto one
// value, so the partition stopped being an exact function of the integer
// seeds §6.5 requires — and the collapse is silent, changing deterministic
// tie behaviour with nothing to observe.
//
// A rational is exact for both bases: an int64 nanosecond count is exact by
// construction, and a float64 reporter weight converts exactly to the rational
// it already is. The comparisons and the merge arithmetic are therefore exact
// in both, which is what §6.5's "Stage-2 comparisons are integer comparisons"
// asks for and what the seed feeding them has to be as well.
func exactWeight(it Item) *big.Rat {
	if it.HasNs {
		return new(big.Rat).SetInt64(it.WeightNs)
	}
	if q := new(big.Rat).SetFloat64(it.Weight); q != nil {
		return q
	}
	// A non-finite weight cannot order anything; it sorts as zero and the
	// caller's own validation is what refuses it.
	return new(big.Rat)
}
