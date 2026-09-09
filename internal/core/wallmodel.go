package core

import (
	"sort"

	"github.com/invakid404/testbucket/internal/nsmath"
)

// WallModelVersion is the model-version leaf of contract §15.1d. A reader
// refuses an absent or unknown value rather than defaulting it, and a writer
// drops the fit group across a bump.
const WallModelVersion = 1

// REFINE_PASSES bounds the two Stage-2 neighborhoods TOGETHER, not each
// (§6.5).
const RefinePasses = 8

// WallModel is the four-parameter model of contract §0.9. Three coefficients
// are integer nanoseconds; `Scale` is the one non-integer quantity and is
// DIMENSIONLESS, because both sides of its multiplication are nanoseconds.
type WallModel struct {
	FixedNs                   int64   `json:"fixed_ns,string"`
	Scale                     float64 `json:"scale"`
	WholeInvocationOverheadNs int64   `json:"whole_invocation_overhead_ns,string"`
	PerSliceOverheadNs        int64   `json:"per_slice_overhead_ns,string"`
}

// InvocationShape is the one neutral hook of contract §6.5's core/adapter
// separation. Core never learns what a `vitest run` is: the adapter declares
// the shape of a bucket and core computes the objective from the frozen model
// parameters and that shape, and from nothing else.
//
// Vitest returns {I(any non-slice unit), slice_count}; Go returns
// {0, len(units)}, since Go renders one invocation per unit.
type InvocationShape struct {
	WholeInvocations int
	SliceInvocations int
}

// AEtaNs is contract §0.9's objective, and it is BOTH the optimized and the
// displayed quantity — one expression, evaluated once.
//
//	A_eta_ns(b) = fixed_ns
//	            + round_half_up( scale × reporter_sum_ns(b) )
//	            + I(∃ whole-file unit ∈ b) × whole_invocation_overhead_ns
//	            + per_slice_overhead_ns × slice_count(b)
//
// The whole-file overhead is an INDICATOR: it is charged once per bucket that
// has any whole-file unit, never per unit. No term is divided here; the single
// division by 1e9 happens at the display boundary on the complete expression.
func AEtaNs(m WallModel, reporterSumNs int64, shape InvocationShape) (int64, error) {
	scaled, err := nsmath.MulScale("scale_x_reporter_sum_ns", m.Scale, reporterSumNs)
	if err != nil {
		return 0, err
	}
	indicator := int64(0)
	if shape.WholeInvocations > 0 {
		indicator = 1
	}
	slicePart, err := nsmath.SumNs("per_slice_overhead_ns_x_slice_count",
		repeatNs(m.PerSliceOverheadNs, shape.SliceInvocations)...)
	if err != nil {
		return 0, err
	}
	return nsmath.SumNs("a_eta_ns", m.FixedNs, scaled, indicator*m.WholeInvocationOverheadNs, slicePart)
}

// repeatNs expands `coefficient × count` into a term list so the product is
// accumulated in the exact domain rather than multiplied in int64, where a
// large per-slice coefficient and a large slice count would wrap.
func repeatNs(v int64, n int) []int64 {
	if n <= 0 {
		return nil
	}
	out := make([]int64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// EstSecondsFromAEta renders §0.9's display boundary. It is the ONLY division
// by 1e9 in the product, applied to the complete expression.
func EstSecondsFromAEta(aEtaNs int64) float64 { return nsmath.Round1Seconds(aEtaNs) }

// AllocUnit is a schedulable unit as the wall allocator sees it: an id for the
// deterministic enumeration order, its nanosecond base weight, and whether it
// is a name-slice.
type AllocUnit struct {
	ID      string
	BaseNs  int64
	IsSlice bool
}

// ShapeOf reports the invocation shape of a set of units under contract §6.5's
// Vitest rule: one whole-file invocation iff any non-slice unit is present,
// plus one per slice.
func ShapeOf(units []AllocUnit) InvocationShape {
	var s InvocationShape
	for _, u := range units {
		if u.IsSlice {
			s.SliceInvocations++
			continue
		}
		s.WholeInvocations = 1
	}
	return s
}

// seedNs is Stage 1's per-unit weight (§6.5). It packs the ADDITIVE part only.
// A slice unit's seed carries per_slice_overhead_ns, so THE SEED IS
// MODEL-SPECIFIC: two fitted models generally produce two different KK seeds.
//
// fixed_ns deliberately stays out of the seed: every non-empty bucket pays it
// identically, so it cannot change the partition, only the reported number.
func seedNs(m WallModel, u AllocUnit) (int64, error) {
	scaled, err := nsmath.MulScale("scale_x_base_ns", m.Scale, u.BaseNs)
	if err != nil {
		return 0, err
	}
	if !u.IsSlice {
		return scaled, nil
	}
	return nsmath.SumNs("seed_ns", scaled, m.PerSliceOverheadNs)
}

// WallPartition is an assignment of units to K buckets.
type WallPartition struct {
	Units   []AllocUnit
	Buckets [][]int // bucket index -> unit indices
}

// bucketCost evaluates the true objective for one bucket.
func (p WallPartition) bucketCost(m WallModel, b int) (int64, error) {
	members := make([]AllocUnit, 0, len(p.Buckets[b]))
	weights := make([]int64, 0, len(p.Buckets[b]))
	for _, ui := range p.Buckets[b] {
		members = append(members, p.Units[ui])
		weights = append(weights, p.Units[ui].BaseNs)
	}
	if len(members) == 0 {
		// An empty bucket runs nothing, so it costs nothing: fixed_ns is paid
		// by every NON-EMPTY bucket (§6.5).
		return 0, nil
	}
	sum, err := nsmath.SumNs("reporter_sum_ns", weights...)
	if err != nil {
		return 0, err
	}
	return AEtaNs(m, sum, ShapeOf(members))
}

// Costs returns every bucket's objective value, in bucket order.
func (p WallPartition) Costs(m WallModel) ([]int64, error) {
	out := make([]int64, len(p.Buckets))
	for b := range p.Buckets {
		c, err := p.bucketCost(m, b)
		if err != nil {
			return nil, err
		}
		out[b] = c
	}
	return out, nil
}

// key is contract §6.5's acceptance key:
//
//	KEY(plan) = ( max_b cost_ns(b) , canonical_bucket_vector(plan) )
//
// compared lexicographically as a 2-tuple. A transition is accepted only on a
// STRICT DECREASE OF THE WHOLE TUPLE, which is what makes the schedule
// deterministic without a tie-breaking heuristic.
type key struct {
	makespan int64
	vector   []string
}

func (p WallPartition) key(m WallModel) (key, error) {
	costs, err := p.Costs(m)
	if err != nil {
		return key{}, err
	}
	var mx int64
	for _, c := range costs {
		if c > mx {
			mx = c
		}
	}
	return key{makespan: mx, vector: p.canonicalVector()}, nil
}

// canonicalVector is each bucket's unit_id list sorted ascending, the K lists
// taken in bucket_index order, compared element-wise as byte strings.
func (p WallPartition) canonicalVector() []string {
	out := make([]string, 0, len(p.Units)+len(p.Buckets))
	for b := range p.Buckets {
		ids := make([]string, 0, len(p.Buckets[b]))
		for _, ui := range p.Buckets[b] {
			ids = append(ids, p.Units[ui].ID)
		}
		sort.Strings(ids)
		out = append(out, ids...)
		// A separator keeps two different groupings of the same ids distinct.
		out = append(out, "\x00")
	}
	return out
}

// less reports whether a sorts strictly before b as the 2-tuple.
func (a key) less(b key) bool {
	if a.makespan != b.makespan {
		return a.makespan < b.makespan
	}
	for i := 0; i < len(a.vector) && i < len(b.vector); i++ {
		if a.vector[i] != b.vector[i] {
			return a.vector[i] < b.vector[i]
		}
	}
	return len(a.vector) < len(b.vector)
}

// bucketOf returns the bucket holding unit ui.
func (p WallPartition) bucketOf(ui int) int {
	for b := range p.Buckets {
		for _, x := range p.Buckets[b] {
			if x == ui {
				return b
			}
		}
	}
	return -1
}

func (p WallPartition) clone() WallPartition {
	q := WallPartition{Units: p.Units, Buckets: make([][]int, len(p.Buckets))}
	for b := range p.Buckets {
		q.Buckets[b] = append([]int(nil), p.Buckets[b]...)
	}
	return q
}

func (p *WallPartition) move(ui, to int) {
	from := p.bucketOf(ui)
	if from == to || from < 0 {
		return
	}
	out := p.Buckets[from][:0]
	for _, x := range p.Buckets[from] {
		if x != ui {
			out = append(out, x)
		}
	}
	p.Buckets[from] = out
	p.Buckets[to] = append(p.Buckets[to], ui)
}

func (p *WallPartition) swap(a, b int) {
	ba, bb := p.bucketOf(a), p.bucketOf(b)
	if ba < 0 || bb < 0 || ba == bb {
		return
	}
	p.move(a, bb)
	p.move(b, ba)
}

// WallSeed builds Stage 1 of contract §6.5: the EXISTING generalized
// Karmarkar–Karp of partition.go over the model-specific seed weights, NOT
// longest-processing-time.
func WallSeed(m WallModel, units []AllocUnit, k int) (WallPartition, error) {
	items := make([]Item, len(units))
	for i, u := range units {
		s, err := seedNs(m, u)
		if err != nil {
			return WallPartition{}, err
		}
		// Item weights are float64 in the v0.2.2 partitioner; the seed is an
		// exact integer nanosecond count and stays exactly representable well
		// past any realistic workload, so KK sees the same ordering the
		// integer domain would give it.
		items[i] = Item{ID: u.ID, Weight: float64(s)}
	}
	groups := karmarkarKarp(items, k)

	byID := make(map[string]int, len(units))
	for i, u := range units {
		byID[u.ID] = i
	}
	p := WallPartition{Units: units, Buckets: make([][]int, k)}
	for b := range groups {
		if b >= k {
			break
		}
		for _, it := range groups[b] {
			p.Buckets[b] = append(p.Buckets[b], byID[it.ID])
		}
	}
	return p, nil
}

// WallRefine is Stage 2 of contract §6.5: deterministic refinement against the
// TRUE objective, over two neighborhoods that RefinePasses bounds together.
//
// The restart policy is NONE. An accepted single-unit move does not restart the
// pass: enumeration continues through the remaining buckets and the remaining
// units, re-reading the moved unit's new home. An implementation that restarts
// on the first accepted move is a DIFFERENT algorithm and non-conformant.
//
// What this guarantees is a deterministic output after a bounded schedule, and
// nothing more. The loop stops on a quiet pass or on the cap, and the cap may
// leave improving transitions unexplored — it is not a fixed point, not
// convergence and not an optimum.
func WallRefine(m WallModel, p WallPartition) (WallPartition, error) {
	order := make([]int, len(p.Units))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return p.Units[order[a]].ID < p.Units[order[b]].ID
	})

	cur, err := p.key(m)
	if err != nil {
		return WallPartition{}, err
	}

	for pass := 0; pass < RefinePasses; pass++ {
		moved := false

		// (2a) single-unit moves — the single normative schedule.
		for _, ui := range order {
			for t := range p.Buckets {
				if t == p.bucketOf(ui) {
					continue
				}
				cand := p.clone()
				cand.move(ui, t)
				ck, err := cand.key(m)
				if err != nil {
					return WallPartition{}, err
				}
				if ck.less(cur) {
					p, cur, moved = cand, ck, true
				}
			}
		}

		// (2b) whole<->slice pair swaps, entered only when (2a) accepted
		// nothing this pass. Take the FIRST improving swap, then start the
		// next pass.
		if !moved {
			var wholes, slices []int
			for _, ui := range order {
				if p.Units[ui].IsSlice {
					slices = append(slices, ui)
				} else {
					wholes = append(wholes, ui)
				}
			}
		swaps:
			for _, w := range wholes {
				for _, v := range slices {
					if p.bucketOf(w) == p.bucketOf(v) {
						continue
					}
					cand := p.clone()
					cand.swap(w, v)
					ck, err := cand.key(m)
					if err != nil {
						return WallPartition{}, err
					}
					if ck.less(cur) {
						p, cur, moved = cand, ck, true
						break swaps
					}
				}
			}
		}

		if !moved {
			break
		}
	}

	// Normalize member order so the output bytes are a function of the
	// assignment and not of the enumeration that produced it.
	for b := range p.Buckets {
		ids := p.Buckets[b]
		sort.SliceStable(ids, func(a, c int) bool { return p.Units[ids[a]].ID < p.Units[ids[c]].ID })
	}
	return p, nil
}

// AllocateWall runs both stages of contract §6.5.
func AllocateWall(m WallModel, units []AllocUnit, k int) (WallPartition, error) {
	seed, err := WallSeed(m, units, k)
	if err != nil {
		return WallPartition{}, err
	}
	return WallRefine(m, seed)
}
