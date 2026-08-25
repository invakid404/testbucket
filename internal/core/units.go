package core

import (
	"fmt"
	"sort"
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
)

// itemOf is the weighted item a unit contributes to the partitioner.
func itemOf(u runner.Unit) Item { return Item{ID: u.ID, Weight: u.Seconds} }

// countShardID / runSliceID / moduleAtomID render the unit grammar: `pkg`,
// `pkg#shardI`, `pkg[TestA|TestB]`, plus `mod:<dir>` for a whole-atom unit.
// IDs are stable identifiers, not display strings — the human summary shortens
// them.
func countShardID(importPath string, shard int) string {
	return fmt.Sprintf("%s#shard%d", importPath, shard)
}

func runSliceID(importPath string, names []string) string {
	return fmt.Sprintf("%s[%s]", importPath, strings.Join(names, "|"))
}

func moduleAtomID(moduleDir string) string { return "mod:" + moduleDir }

// runnableNamer resolves a target's complete top-level RUNNABLE set — every
// name the emitted name filter can select: tests, examples and fuzz targets
// alike. It is an injected dependency (the CLI wires it to runner.Runnables)
// so the whole planner is testable against a synthetic tree.
type runnableNamer func(p runner.LivePackage) ([]string, error)

type expandOptions struct {
	// K is the bucket count, used only to bound how far a whale may be split.
	K int
	// BaseCount is the flake sweep count the un-split unit uses; count-shards
	// divide it.
	BaseCount int
	// MeanSeconds is the cold-start weight handed to any live target the store
	// has no measurement for. Never zero: an unweighted unit would sink to
	// whichever bucket happens to be lightest and, worse, would make the
	// plan's estimates lie.
	MeanSeconds float64
	Runnables   runnableNamer
}

// expansion is the result of turning the live target set plus the store into
// schedulable units.
type expansion struct {
	Units []runner.Unit
	// Runnables records the live runnable-name list used for each name-sliced
	// target, so the coverage gate can check the slices against the same
	// universe the slicer saw.
	Runnables map[string][]string
	Notes     []string
	// Loaded / Missing count TARGETS (not units) whose weight came from a real
	// measurement vs the cold-start mean.
	Loaded           []string
	Missing          []string
	MeasuredSeconds  float64
	EstimatedSeconds float64
}

// expandUnits turns the live target set into the units `plan` partitions.
//
// The traversal is over the LIVE set, never over the store. That ordering is
// the structural half of the never-drop-a-test invariant: a target the store
// has never heard of still gets a unit (on the mean weight), and a store row
// with no live target simply never gets looked at.
func expandUnits(live []runner.LivePackage, st *Store, opt expandOptions) (*expansion, error) {
	ex := &expansion{Runnables: map[string][]string{}}

	testable := make([]runner.LivePackage, 0, len(live))
	for _, p := range live {
		if p.HasTests {
			testable = append(testable, p)
		}
	}
	sort.Slice(testable, func(i, j int) bool { return testable[i].ID < testable[j].ID })

	atoms, loose := moduleAtoms(testable)

	weightOf := func(p runner.LivePackage) (float64, bool) {
		if row := st.Units[p.ID]; row.measured() {
			return row.Seconds, false
		}
		return opt.MeanSeconds, true
	}
	account := func(p runner.LivePackage, sec float64, estimated bool) {
		if estimated {
			ex.Missing = append(ex.Missing, p.ID)
			ex.EstimatedSeconds += sec
			return
		}
		ex.Loaded = append(ex.Loaded, p.ID)
		ex.MeasuredSeconds += sec
	}

	// Whole-atom units first, so their notes read before the per-target ones
	// in the summary.
	for _, atomKey := range runner.SortedKeys(atoms) {
		pkgs := atoms[atomKey]
		total := 0.0
		estimated := false
		for _, p := range pkgs {
			sec, est := weightOf(p)
			account(p, sec, est)
			total += sec
			estimated = estimated || est
			if st.Units[p.ID].splitPolicy() != splitNone {
				ex.Notes = append(ex.Notes, fmt.Sprintf(
					"split of %s suppressed: atom group %s must be packed whole (mode=%s)",
					p.ID, atomKey, pkgs[0].Mode))
			}
		}
		ex.Units = append(ex.Units, runner.Unit{
			ID:       moduleAtomID(atomKey),
			Kind:     runner.KindModuleAtom,
			Seconds:  total,
			Estimate: estimated,
			Packages: pkgs,
			Module:   atomKey,
			Mode:     pkgs[0].Mode,
			Count:    opt.BaseCount,
		})
	}

	for _, p := range loose {
		sec, estimated := weightOf(p)
		account(p, sec, estimated)
		row := st.Units[p.ID]

		policy := row.splitPolicy()
		if opt.K < 2 && policy != splitNone {
			// With a single bucket there is nothing to balance, and each extra
			// slice pays another compile of the target. Splitting here would be
			// strictly worse than not.
			ex.Notes = append(ex.Notes, fmt.Sprintf("split of %s suppressed: K=%d leaves nothing to balance", p.ID, opt.K))
			policy = splitNone
		}

		switch policy {
		case splitCount:
			shards := clampShards(row.SplitInto, opt.K)
			per := sec / float64(shards)
			count := ceilDiv(opt.BaseCount, shards)
			for i := 1; i <= shards; i++ {
				ex.Units = append(ex.Units, runner.Unit{
					ID:       countShardID(p.ID, i),
					Kind:     runner.KindCountShard,
					Seconds:  per,
					Estimate: estimated,
					Packages: []runner.LivePackage{p},
					Module:   p.Module,
					Mode:     p.Mode,
					Count:    count,
					Shard:    i,
					Shards:   shards,
				})
			}
			ex.Notes = append(ex.Notes, fmt.Sprintf(
				"count-shard %s into %d x -count=%d (aggregate %d >= %d)",
				p.ID, shards, count, count*shards, opt.BaseCount))

		case splitRun:
			if opt.Runnables == nil {
				return nil, fmt.Errorf("%s is flagged split=run but no runnable-name resolver is configured", p.ID)
			}
			names, err := opt.Runnables(p)
			if err != nil {
				// Loud, not silent: falling back to a whole-target run here
				// would quietly undo the harpoon and blow the makespan without
				// anyone noticing.
				return nil, fmt.Errorf("resolve runnable names for %s (flagged split=run): %w", p.ID, err)
			}
			names = dedupeSorted(names)
			ex.Runnables[p.ID] = names
			slices := sliceByName(p, names, row, sec, clampShards(row.SplitInto, opt.K), opt.BaseCount, estimated)
			ex.Units = append(ex.Units, slices...)
			ex.Notes = append(ex.Notes, fmt.Sprintf(
				"run-slice %s into %d slices over %d live runnables (tests, examples and fuzz targets)",
				p.ID, len(slices), len(names)))

		default:
			ex.Units = append(ex.Units, runner.Unit{
				ID:       p.ID,
				Kind:     runner.KindPackage,
				Seconds:  sec,
				Estimate: estimated,
				Packages: []runner.LivePackage{p},
				Module:   p.Module,
				Mode:     p.Mode,
				Count:    opt.BaseCount,
			})
		}
	}

	sort.Slice(ex.Units, func(i, j int) bool { return ex.Units[i].ID < ex.Units[j].ID })
	sort.Strings(ex.Loaded)
	sort.Strings(ex.Missing)
	return ex, nil
}

// sliceByName packs a target's live runnables — tests, examples and fuzz
// targets alike — into up to `shards` name slices, weighting each name by its
// recorded per-name time and giving names the store has never seen the target's
// residual per-name average. Unrecorded names are packed exactly like recorded
// ones: that is what keeps a brand-new test (or a newly added Example) inside a
// harpooned whale from vanishing.
func sliceByName(p runner.LivePackage, names []string, row *UnitStat, pkgSeconds float64, shards, baseCount int, estimated bool) []runner.Unit {
	if len(names) == 0 {
		// Deliberately returns nothing rather than an empty filter (which
		// would match everything and duplicate the target across slices). The
		// coverage gate is the backstop that turns this into a loud failure,
		// because discovery says this target HAS tests.
		return nil
	}
	known := 0.0
	knownCount := 0
	for _, n := range names {
		if w, ok := row.Tests[n]; ok && w > 0 {
			known += w
			knownCount++
		}
	}
	unknownCount := len(names) - knownCount
	perUnknown := 0.0
	switch {
	case unknownCount == 0:
	case pkgSeconds-known > 0:
		perUnknown = (pkgSeconds - known) / float64(unknownCount)
	case knownCount > 0:
		perUnknown = known / float64(knownCount)
	default:
		perUnknown = pkgSeconds / float64(len(names))
	}

	items := make([]Item, 0, len(names))
	for _, n := range names {
		w := perUnknown
		if v, ok := row.Tests[n]; ok && v > 0 {
			w = v
		}
		items = append(items, Item{ID: n, Weight: w})
	}

	groups := karmarkarKarp(items, shards)
	units := make([]runner.Unit, 0, shards)
	for _, g := range groups {
		if len(g) == 0 {
			// Fewer live tests than requested slices; an empty slice would be
			// an invocation that runs nothing.
			continue
		}
		sliceNames := make([]string, 0, len(g))
		total := 0.0
		for _, it := range g {
			sliceNames = append(sliceNames, it.ID)
			total += it.Weight
		}
		sort.Strings(sliceNames)
		units = append(units, runner.Unit{
			ID:       runSliceID(p.ID, sliceNames),
			Kind:     runner.KindRunSlice,
			Seconds:  total,
			Estimate: estimated,
			Packages: []runner.LivePackage{p},
			Module:   p.Module,
			Mode:     p.Mode,
			Count:    baseCount,
			Run:      sliceNames,
		})
	}
	return units
}

// moduleAtoms splits the live set into targets that must be packed whole (a
// non-empty AtomKey) and targets that may mix freely.
//
// This is the "co-scheduling boundary is a SOFT factor" rule: honoured only
// where correctness needs it. For the Go adapter, a module that resolves with
// GOWORK=off cannot share an invocation with workspace-mode packages
// (different build list), so its packages ride together; everything inside the
// workspace packs purely for balance, across module lines. The core never
// learns any of that — it only groups by the key the adapter set.
func moduleAtoms(live []runner.LivePackage) (map[string][]runner.LivePackage, []runner.LivePackage) {
	atoms := map[string][]runner.LivePackage{}
	var loose []runner.LivePackage
	for _, p := range live {
		if key := p.AtomKey(); key != "" {
			atoms[key] = append(atoms[key], p)
			continue
		}
		loose = append(loose, p)
	}
	return atoms, loose
}

// clampShards bounds a whale's slice count: at least 2 (a "split" into one is
// not a split) and never more than the bucket count, since slices beyond K
// only add compile cost without adding parallelism. Callers must not reach it
// with k < 2; expandUnits suppresses splitting entirely there.
func clampShards(want, k int) int {
	if want < 2 {
		want = 2
	}
	if k >= 2 && want > k {
		return k
	}
	return want
}

func ceilDiv(a, b int) int {
	if b <= 0 {
		return a
	}
	if a <= 0 {
		return 1
	}
	n := (a + b - 1) / b
	if n < 1 {
		return 1
	}
	return n
}

// dedupeSorted sorts and de-duplicates a resolver's names. A duplicate would
// otherwise be packed into two slices and run twice.
func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return runner.Dedupe(out)
}
