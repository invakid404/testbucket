package walltime

import (
	"reflect"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/core"
)

// corePacker is the production packer: the same generalized Karmarkar–Karp the
// reporter basis uses (§17.3a's residual-placement rule), reached through
// core's exported wrapper rather than reimplemented here.
func corePacker(units []CalibrationUnit, slots int) [][]CalibrationUnit {
	items := make([]core.Item, len(units))
	byID := make(map[string]CalibrationUnit, len(units))
	for i, u := range units {
		items[i] = core.Item{ID: u.ID, Weight: float64(u.BaseNs)}
		byID[u.ID] = u
	}
	groups := core.KarmarkarKarp(items, slots)
	out := make([][]CalibrationUnit, slots)
	for gi, g := range groups {
		if gi >= slots {
			break
		}
		for _, it := range g {
			out[gi] = append(out[gi], byID[it.ID])
		}
	}
	return out
}

// calibrationUniverse is contract §17.3a's published universe: K = 4, whole
// units w1..w4 at 40/30/20/10e9 and slice units s1..s3 at 5e9.
func calibrationUniverse() []CalibrationUnit {
	return []CalibrationUnit{
		{ID: "w1", BaseNs: 40e9}, {ID: "w2", BaseNs: 30e9},
		{ID: "w3", BaseNs: 20e9}, {ID: "w4", BaseNs: 10e9},
		{ID: "s1", BaseNs: 5e9, IsSlice: true},
		{ID: "s2", BaseNs: 5e9, IsSlice: true},
		{ID: "s3", BaseNs: 5e9, IsSlice: true},
	}
}

// TestCalibrationModeTerminatesSufficientOrNotFoundWithinBudget is §22 test 59
// (S-7/R8-D1) and the acceptance test the registry names for ID-18.
func TestCalibrationModeTerminatesSufficientOrNotFoundWithinBudget(t *testing.T) {
	u := calibrationUniverse()
	const k = 4

	t.Run("the published layout table reproduces", func(t *testing.T) {
		// Each row is what L(U, K, i) produces for this universe under
		// §17.3a's rules, residual packing included, so the fixture witnesses
		// the GENERATOR and not merely the rank test.
		cases := []struct {
			i     int
			rank  int
			want  [][]string
			undef bool
		}{
			{i: 1, rank: 3},
			{i: 2, rank: 3, want: [][]string{{"s1", "s2", "s3"}, {"w1"}, {"w2"}, {"w3", "w4"}}},
			{i: 3, rank: 3, want: [][]string{{"w1"}, {"s1", "s2", "s3"}, {"w2"}, {"w3", "w4"}}},
			{i: 4, rank: 4, want: [][]string{{"w1"}, {"s1", "s2"}, {"s3"}, {"w2", "w3", "w4"}}},
			{i: 5, undef: true},
		}
		for _, c := range cases {
			layout, err := Layout(u, k, c.i, corePacker)
			if c.undef {
				if err == nil {
					t.Errorf("L(%d) must be undefined: (5-3)+2 = 4 slots equals K, leaving none for the residual", c.i)
				}
				continue
			}
			if err != nil {
				t.Errorf("L(%d): %v", c.i, err)
				continue
			}
			if len(layout) != k {
				t.Errorf("L(%d) returned %d bucket lists, want exactly K = %d", c.i, len(layout), k)
			}
			if c.want != nil {
				if got := bucketIDs(layout); !reflect.DeepEqual(got, c.want) {
					t.Errorf("L(%d) buckets = %v, want %v", c.i, got, c.want)
				}
			}
			r, err := RankAdmission(designOfLayout(layout))
			if err != nil {
				t.Fatal(err)
			}
			if r.Rank != c.rank {
				t.Errorf("L(%d) rank = %d, want %d (rows %v)", c.i, r.Rank, c.rank, designOfLayout(layout))
			}
		}
	})

	t.Run("the union of every layout is the universe with empty intersections", func(t *testing.T) {
		for i := 1; i <= 4; i++ {
			layout, err := Layout(u, k, i, corePacker)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]int{}
			for _, b := range layout {
				for _, x := range b {
					seen[x.ID]++
				}
			}
			if len(seen) != len(u) {
				t.Errorf("L(%d) covers %d units, want %d", i, len(seen), len(u))
			}
			for id, n := range seen {
				if n != 1 {
					t.Errorf("L(%d) places %s in %d buckets; intersections must be empty", i, id, n)
				}
			}
		}
	})

	t.Run("N = 3 is not_found_within_budget and N = 4 is sufficient", func(t *testing.T) {
		ev, err := CalibrateProposer(u, k, 3, corePacker)
		if err != nil {
			t.Fatal(err)
		}
		if ev.Outcome != CalibrationNotFoundWithinBudget {
			t.Fatalf("N = 3 outcome = %q, want %q", ev.Outcome, CalibrationNotFoundWithinBudget)
		}
		if ev.LayoutsTried != ev.LayoutBudget || ev.LayoutsTried != 3 {
			t.Fatalf("N = 3: layouts_tried = %d, layout_budget = %d, want both 3", ev.LayoutsTried, ev.LayoutBudget)
		}
		if len(ev.DeficientColumns) == 0 {
			t.Error("a bounded miss must name the deficient columns")
		}
		if ev.GeneratorExhausted {
			t.Error("N = 3 stopped at the budget, not at generator exhaustion")
		}

		ev4, err := CalibrateProposer(u, k, 4, corePacker)
		if err != nil {
			t.Fatal(err)
		}
		if ev4.Outcome != CalibrationSufficient {
			t.Fatalf("N = 4 outcome = %q, want %q", ev4.Outcome, CalibrationSufficient)
		}
		if ev4.Rank != 4 {
			t.Fatalf("N = 4 rank = %d, want 4", ev4.Rank)
		}
		if ev4.LayoutsTried != 4 {
			t.Fatalf("N = 4 layouts_tried = %d, want 4", ev4.LayoutsTried)
		}
		if ev4.Schema != CalibrationEvidenceSchema {
			t.Fatalf("schema = %q, want %q", ev4.Schema, CalibrationEvidenceSchema)
		}
		want := [][]string{{"w1"}, {"s1", "s2"}, {"s3"}, {"w2", "w3", "w4"}}
		if !reflect.DeepEqual(ev4.Buckets, want) {
			t.Fatalf("L(4) buckets = %v, want %v", ev4.Buckets, want)
		}
	})

	t.Run("structural infeasibility names a DIFFERENT column in each direction", func(t *testing.T) {
		allWhole := []CalibrationUnit{{ID: "w1", BaseNs: 10e9}, {ID: "w2", BaseNs: 20e9}}
		ev, err := CalibrateProposer(allWhole, k, 4, corePacker)
		if err != nil {
			t.Fatal(err)
		}
		if ev.Outcome != CalibrationStructurallyInfeasible {
			t.Fatalf("all-whole outcome = %q, want structurally_infeasible", ev.Outcome)
		}
		if ev.ZeroColumn != 4 {
			t.Fatalf("all-whole names column %d, want 4 — an earlier revision blamed column 3 both ways, which is false here because empty buckets are permitted", ev.ZeroColumn)
		}

		allSlice := []CalibrationUnit{{ID: "s1", BaseNs: 5e9, IsSlice: true}, {ID: "s2", BaseNs: 5e9, IsSlice: true}}
		ev, err = CalibrateProposer(allSlice, k, 4, corePacker)
		if err != nil {
			t.Fatal(err)
		}
		if ev.Outcome != CalibrationStructurallyInfeasible {
			t.Fatalf("all-slice outcome = %q, want structurally_infeasible", ev.Outcome)
		}
		if ev.ZeroColumn != 3 {
			t.Fatalf("all-slice names column %d, want 3", ev.ZeroColumn)
		}
	})

	t.Run("a bounded miss is never promoted to structural infeasibility", func(t *testing.T) {
		// The universe has both kinds of unit, so no whole-universe proof is
		// available; a budget of 1 must report the bounded outcome.
		ev, err := CalibrateProposer(u, k, 1, corePacker)
		if err != nil {
			t.Fatal(err)
		}
		if ev.Outcome != CalibrationNotFoundWithinBudget {
			t.Fatalf("outcome = %q, want not_found_within_budget", ev.Outcome)
		}
		if ev.ZeroColumn != 0 {
			t.Error("a bounded miss must not name an actually-zero column")
		}
	})

	t.Run("the outcome name says what happened", func(t *testing.T) {
		// The earlier name INFEASIBLE asserted the universe CANNOT reach rank
		// 4, which failing a few heuristic layouts does not prove.
		if strings.Contains(string(CalibrationNotFoundWithinBudget), "infeasible") {
			t.Fatal("the bounded outcome must not be named infeasible")
		}
		src := readWalltimeSource(t, "calibrate.go")
		if strings.Contains(src, `"infeasible"`) {
			t.Error("a bare INFEASIBLE outcome value survives")
		}
	})

	t.Run("N is defined for every legal value and there is no fourth outcome", func(t *testing.T) {
		for _, n := range []int{1, 2, 3, 4, 5, 12, 99} {
			ev, err := CalibrateProposer(u, k, n, corePacker)
			if err != nil {
				t.Fatalf("N = %d: %v", n, err)
			}
			switch ev.Outcome {
			case CalibrationSufficient, CalibrationStructurallyInfeasible, CalibrationNotFoundWithinBudget:
			default:
				t.Fatalf("N = %d produced a fourth outcome %q", n, ev.Outcome)
			}
		}
		if _, err := CalibrateProposer(u, k, 0, corePacker); err == nil {
			t.Error("N = 0 is not a legal value and must be refused")
		}
	})

	t.Run("generator exhaustion beyond the sufficient point is reported as such", func(t *testing.T) {
		// With a budget past L(4) the proposer still stops at L(4) — the first
		// layout reaching rank 4 — so exhaustion is only reachable when no
		// earlier layout succeeds.
		noSuccess := []CalibrationUnit{
			{ID: "w1", BaseNs: 10e9},
			{ID: "s1", BaseNs: 5e9, IsSlice: true},
		}
		ev, err := CalibrateProposer(noSuccess, 2, 9, corePacker)
		if err != nil {
			t.Fatal(err)
		}
		if ev.Outcome != CalibrationNotFoundWithinBudget {
			t.Fatalf("outcome = %q, want not_found_within_budget", ev.Outcome)
		}
		if !ev.GeneratorExhausted {
			t.Error("the generator exhausted before the budget; that must be reported")
		}
		if ev.LayoutsTried >= ev.LayoutBudget {
			t.Errorf("layouts_tried = %d must be below the budget %d on exhaustion", ev.LayoutsTried, ev.LayoutBudget)
		}
	})

	t.Run("the one-slice K=3 case returns a layout rather than exhaustion", func(t *testing.T) {
		// R11-D5: |S| = 1, i = 4, residual whole units. The repaired slot
		// formula reserves min(2, |S|) = 1 slice slot, so the requirement is
		// 1 + 1 + 1 = 3 <= K and the layout EXISTS. The old formula demanded
		// 1 + 2 + 1 = 4 and declared exhaustion.
		oneSlice := []CalibrationUnit{
			{ID: "w1", BaseNs: 30e9}, {ID: "w2", BaseNs: 20e9}, {ID: "w3", BaseNs: 10e9},
			{ID: "s1", BaseNs: 5e9, IsSlice: true},
		}
		layout, err := Layout(oneSlice, 3, 4, corePacker)
		if err != nil {
			t.Fatalf("L(4) with |S| = 1 at K = 3 must exist: %v", err)
		}
		got := bucketIDs(layout)
		if len(got) != 3 {
			t.Fatalf("layout has %d buckets, want 3", len(got))
		}
		// The single slice occupies the one reserved slice bucket whole.
		if !reflect.DeepEqual(got[1], []string{"s1"}) {
			t.Fatalf("slice bucket = %v, want [s1] whole", got[1])
		}
		if !reflect.DeepEqual(got[0], []string{"w1"}) {
			t.Fatalf("isolated bucket = %v, want [w1]", got[0])
		}
	})

	t.Run("empty buckets are permitted and are legitimate design rows", func(t *testing.T) {
		few := []CalibrationUnit{
			{ID: "w1", BaseNs: 10e9},
			{ID: "s1", BaseNs: 5e9, IsSlice: true},
		}
		layout, err := Layout(few, 4, 2, corePacker)
		if err != nil {
			t.Fatal(err)
		}
		rows := designOfLayout(layout)
		if len(rows) != 4 {
			t.Fatalf("design has %d rows, want one per bucket", len(rows))
		}
		var empties int
		for _, r := range rows {
			if r[1] == 0 && r[2] == 0 && r[3] == 0 {
				empties++
			}
		}
		if empties == 0 {
			t.Fatal("expected at least one empty bucket to contribute a zero row")
		}
	})

	t.Run("isolation exhaustion never pads or silently isolates fewer", func(t *testing.T) {
		twoWhole := []CalibrationUnit{
			{ID: "w1", BaseNs: 10e9}, {ID: "w2", BaseNs: 5e9},
			{ID: "s1", BaseNs: 1e9, IsSlice: true}, {ID: "s2", BaseNs: 1e9, IsSlice: true},
		}
		// L(6) isolates 3 whole units but only 2 exist.
		if _, err := Layout(twoWhole, 8, 6, corePacker); err == nil {
			t.Fatal("L(6) must be undefined when the universe has fewer whole units than it isolates")
		}
	})
}
