package core

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/invakid404/testbucket/internal/runner"
)

func demoPlan(basis EstBasis, withShadow bool) *PlanDocument {
	shadow := 18.4
	b := PlanBucket{
		Index: 0, Name: "bucket-0", Seconds: 12.3, NeedsNode: true,
		Units:       []PlanUnit{{ID: "a.spec.ts", Seconds: 12.3}},
		Invocations: []runner.Invocation{},
		Script:      "vitest run ./a.spec.ts",
	}
	if basis == BasisWall {
		n := Nanos(12_300_000_000)
		b.AEtaNs = &n
	}
	if withShadow {
		b.WallEstSeconds = &shadow
	}
	return &PlanDocument{
		K: 1, Flags: "vitest", Algorithm: "kk", StorePath: "s.json",
		Buckets: []PlanBucket{b}, EstBasis: basis,
	}
}

// legacyProjection restricts a matrix entry to PD-1's canonical v0.2.2 field
// set, in v0.2.2 order, which is the object test 33 byte-compares.
func legacyProjection(t *testing.T, matrix []byte) []byte {
	t.Helper()
	var doc struct {
		Include []map[string]json.RawMessage `json:"include"`
	}
	if err := json.Unmarshal(matrix, &doc); err != nil {
		t.Fatal(err)
	}
	var out []byte
	out = append(out, '[')
	for i, e := range doc.Include {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '{')
		for j, name := range LegacyProjectionFields() {
			v, ok := e[name]
			if !ok {
				t.Fatalf("legacy field %q is missing from the matrix entry", name)
			}
			if j > 0 {
				out = append(out, ',')
			}
			k, _ := json.Marshal(name)
			out = append(out, k...)
			out = append(out, ':')
			out = append(out, v...)
		}
		out = append(out, '}')
	}
	out = append(out, ']')
	return out
}

// TestReporterBasisPreservesLegacyProjectionAndDeclaresBasis is §22 test 33
// (PD-1): the canonical v0.2.2-field projection is byte-identical for an
// unopted consumer, and the new fields are asserted SEPARATELY as additive.
func TestReporterBasisPreservesLegacyProjectionAndDeclaresBasis(t *testing.T) {
	unopted := demoPlan(BasisReporter, false)
	unoptedMatrix, err := unopted.MatrixJSON()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("the canonical legacy projection is byte-identical across bases", func(t *testing.T) {
		wall := demoPlan(BasisWall, false)
		wallMatrix, err := wall.MatrixJSON()
		if err != nil {
			t.Fatal(err)
		}
		a := legacyProjection(t, unoptedMatrix)
		b := legacyProjection(t, wallMatrix)
		if string(a) != string(b) {
			t.Fatalf("the legacy projection differs between bases:\n reporter: %s\n wall:     %s", a, b)
		}
	})

	t.Run("the additive fields are asserted separately, never as part of the projection", func(t *testing.T) {
		proj := string(legacyProjection(t, unoptedMatrix))
		for _, additive := range []string{"est_basis", "wall_est_seconds", "a_eta_ns", "expanded_unit_set_digest"} {
			if contains(proj, `"`+additive+`"`) {
				t.Errorf("additive field %q leaked into the canonical legacy projection", additive)
			}
		}
		// And they ARE present on the full matrix, which is a different claim.
		var doc struct {
			Include []map[string]json.RawMessage `json:"include"`
		}
		if err := json.Unmarshal(unoptedMatrix, &doc); err != nil {
			t.Fatal(err)
		}
		if _, ok := doc.Include[0]["est_basis"]; !ok {
			t.Error("every matrix entry must carry est_basis (§16.1)")
		}
	})

	t.Run("no assertion claims the full matrix is byte-identical", func(t *testing.T) {
		// PD-1's additive fields make that false, and saying it would
		// contradict §16.1's requirement that every entry carry est_basis.
		wall := demoPlan(BasisWall, false)
		wallMatrix, err := wall.MatrixJSON()
		if err != nil {
			t.Fatal(err)
		}
		if string(unoptedMatrix) == string(wallMatrix) {
			t.Fatal("the two bases produced byte-identical FULL matrices; est_basis must differ")
		}
	})

	t.Run("matrix entries carry no a_eta_ns in either basis", func(t *testing.T) {
		for _, basis := range []EstBasis{BasisReporter, BasisWall} {
			m, err := demoPlan(basis, false).MatrixJSON()
			if err != nil {
				t.Fatal(err)
			}
			if contains(string(m), `"a_eta_ns"`) {
				t.Fatalf("%s basis matrix carries a_eta_ns; §5.1 puts it on plan buckets and observations only", basis)
			}
		}
	})

	t.Run("a_eta_ns is present on plan buckets under wall and absent under a reporter cold plan", func(t *testing.T) {
		wall := demoPlan(BasisWall, false)
		if wall.Buckets[0].AEtaNs == nil {
			t.Error("wall basis must serialize a_eta_ns on the plan bucket")
		}
		cold := demoPlan(BasisReporter, false)
		if cold.Buckets[0].AEtaNs != nil {
			t.Error("a reporter cold plan must not carry a_eta_ns")
		}
	})

	t.Run("wall_est_seconds is emitted only under reporter basis with an ok model", func(t *testing.T) {
		withShadow := demoPlan(BasisReporter, true)
		m, err := withShadow.MatrixJSON()
		if err != nil {
			t.Fatal(err)
		}
		if !contains(string(m), `"wall_est_seconds"`) {
			t.Error("the shadow must be emitted under reporter basis with an ok model")
		}
		// Under wall basis it is absent, because est_seconds already IS the
		// model's value.
		wall := demoPlan(BasisWall, false)
		mw, err := wall.MatrixJSON()
		if err != nil {
			t.Fatal(err)
		}
		if contains(string(mw), `"wall_est_seconds"`) {
			t.Error("wall basis must not carry the shadow")
		}
	})

	t.Run("a_eta_ns round-trips as an exact decimal string", func(t *testing.T) {
		// Above 2^53, so a number-typed field would lose the value.
		n := Nanos(9_007_199_254_740_993)
		b, err := json.Marshal(n)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != `"9007199254740993"` {
			t.Fatalf("a_eta_ns serialized as %s, want an exact quoted decimal", b)
		}
		var back Nanos
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatal(err)
		}
		if back != n {
			t.Fatalf("round trip gave %d, want %d", back, n)
		}
	})

	t.Run("est_seconds under wall basis is round1 of a_eta_ns", func(t *testing.T) {
		wall := demoPlan(BasisWall, false)
		got := EstSecondsFromAEta(int64(*wall.Buckets[0].AEtaNs))
		if got != 12.3 {
			t.Fatalf("round1(a_eta_ns/1e9) = %v, want 12.3", got)
		}
	})

	t.Run("the legacy field set is exactly v0.2.2's, in order", func(t *testing.T) {
		want := []string{"bucket", "name", "est_seconds", "needs_node", "units", "invocations", "script"}
		if !reflect.DeepEqual(LegacyProjectionFields(), want) {
			t.Fatalf("legacy projection = %v, want %v", LegacyProjectionFields(), want)
		}
	})
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
