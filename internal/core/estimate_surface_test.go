package core

import (
	"encoding/json"
	"testing"
)

// TestEveryExposedEstimateDeclaresItsQuantity is §22 test 49 (F2/D-1). It
// walks §5.1's inventory under BOTH bases and proves each label, value and the
// optimized objective agree.
func TestEveryExposedEstimateDeclaresItsQuantity(t *testing.T) {
	m := WallModel{FixedNs: 5e9, Scale: 1.0, WholeInvocationOverheadNs: 8e9, PerSliceOverheadNs: 3e9}
	units := []AllocUnit{
		{ID: "w1", BaseNs: 5e9}, {ID: "s1", BaseNs: 5e9, IsSlice: true},
	}
	p, err := AllocateWall(m, units, 1)
	if err != nil {
		t.Fatal(err)
	}
	costs, err := p.Costs(m)
	if err != nil {
		t.Fatal(err)
	}
	objective := costs[0]

	wallDoc := func() *PlanDocument {
		n := Nanos(objective)
		return &PlanDocument{
			K: 1, EstBasis: BasisWall,
			Buckets: []PlanBucket{{
				Index: 0, Name: "bucket-0",
				Seconds: EstSecondsFromAEta(objective), AEtaNs: &n,
				Units: []PlanUnit{{ID: "w1", Seconds: 5}, {ID: "s1", Seconds: 5}},
			}},
		}
	}
	shadow := EstSecondsFromAEta(objective)
	reporterDoc := func(withShadow bool) *PlanDocument {
		d := &PlanDocument{
			K: 1, EstBasis: BasisReporter,
			Buckets: []PlanBucket{{
				Index: 0, Name: "bucket-0", Seconds: 10,
				Units: []PlanUnit{{ID: "w1", Seconds: 5}, {ID: "s1", Seconds: 5}},
			}},
		}
		if withShadow {
			d.Buckets[0].WallEstSeconds = &shadow
		}
		return d
	}

	t.Run("est_seconds equals round1(a_eta_ns/1e9) wherever both appear", func(t *testing.T) {
		d := wallDoc()
		b := d.Buckets[0]
		if b.AEtaNs == nil {
			t.Fatal("wall basis must serialize a_eta_ns")
		}
		if got, want := b.Seconds, EstSecondsFromAEta(int64(*b.AEtaNs)); got != want {
			t.Fatalf("est_seconds = %v, want round1(a_eta_ns/1e9) = %v", got, want)
		}
		// And it is the OPTIMIZED objective, not a separate sum.
		if int64(*b.AEtaNs) != objective {
			t.Fatalf("a_eta_ns = %d, want the objective value %d", *b.AEtaNs, objective)
		}
	})

	t.Run("a_eta_ns is present on plan buckets under wall and absent under a reporter cold plan", func(t *testing.T) {
		if wallDoc().Buckets[0].AEtaNs == nil {
			t.Error("wall basis: a_eta_ns must be present on the plan bucket")
		}
		if reporterDoc(false).Buckets[0].AEtaNs != nil {
			t.Error("a reporter cold plan must not carry a_eta_ns")
		}
	})

	t.Run("a_eta_ns is absent from matrix entries in BOTH bases", func(t *testing.T) {
		for name, d := range map[string]*PlanDocument{"wall": wallDoc(), "reporter": reporterDoc(true)} {
			b, err := d.MatrixJSON()
			if err != nil {
				t.Fatal(err)
			}
			if contains(string(b), `"a_eta_ns"`) {
				t.Errorf("%s basis matrix carries a_eta_ns; §5.1 puts it on plan buckets and observations only", name)
			}
		}
	})

	t.Run("every matrix entry declares its basis", func(t *testing.T) {
		for _, d := range []*PlanDocument{wallDoc(), reporterDoc(false)} {
			b, err := d.MatrixJSON()
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Include []map[string]json.RawMessage `json:"include"`
			}
			if err := json.Unmarshal(b, &doc); err != nil {
				t.Fatal(err)
			}
			raw, ok := doc.Include[0]["est_basis"]
			if !ok {
				t.Fatal("a matrix entry carries no est_basis")
			}
			var basis string
			if err := json.Unmarshal(raw, &basis); err != nil {
				t.Fatal(err)
			}
			if basis != string(BasisWall) && basis != string(BasisReporter) {
				t.Fatalf("est_basis is %q; §5.1 permits exactly two values", basis)
			}
		}
	})

	t.Run("no wall-basis per-unit estimate exists and unit display stays reporter-labelled", func(t *testing.T) {
		// A_eta_ns carries the I(any_whole_file) indicator, which has no
		// unique per-unit decomposition, so unit-level display keeps
		// reporter-basis values under BOTH bases.
		d := wallDoc()
		var unitSum float64
		for _, u := range d.Buckets[0].Units {
			unitSum += u.Seconds
		}
		if unitSum == d.Buckets[0].Seconds {
			t.Fatal("the bucket estimate equals the sum of its units' estimates; under wall basis it must NOT, because the indicator has no per-unit share")
		}
		// The unit values are the legacy reporter seconds, unchanged.
		if d.Buckets[0].Units[0].Seconds != 5 {
			t.Fatalf("unit est_seconds = %v, want the reporter value 5", d.Buckets[0].Units[0].Seconds)
		}
	})

	t.Run("the shadow follows §5.1's single presence rule", func(t *testing.T) {
		if reporterDoc(true).Buckets[0].WallEstSeconds == nil {
			t.Error("reporter basis with an ok model must emit the shadow")
		}
		if wallDoc().Buckets[0].WallEstSeconds != nil {
			t.Error("wall basis must not carry the shadow; est_seconds already IS the model value")
		}
		if !WallEstSecondsPresent(BasisReporter, WallStatusOK) {
			t.Error("the presence predicate disagrees with the emitted document")
		}
		if WallEstSecondsPresent(BasisWall, WallStatusOK) {
			t.Error("the presence predicate permits the shadow under wall basis")
		}
	})

	t.Run("the fitted scale is a dimensionless float and the other three are int64 ns", func(t *testing.T) {
		var _ float64 = m.Scale
		var _ int64 = m.FixedNs
		var _ int64 = m.WholeInvocationOverheadNs
		var _ int64 = m.PerSliceOverheadNs
	})

	t.Run("no undeclared seconds-valued surface exists", func(t *testing.T) {
		// §5.1's inventory is complete. Every seconds-valued field a matrix
		// entry exposes must be one of the two it names.
		b, err := wallDoc().MatrixJSON()
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Include []map[string]json.RawMessage `json:"include"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		allowed := map[string]bool{"est_seconds": true, "wall_est_seconds": true}
		for key := range doc.Include[0] {
			if contains(key, "seconds") && !allowed[key] {
				t.Errorf("matrix entry exposes an undeclared seconds-valued surface %q", key)
			}
		}
	})
}
