package walltime

import (
	"strings"
	"testing"
)

// TestBoundaryInvariantsAndComponentSpans is §22 test 1: the boundary
// invariants, with script_overhead_ns and wrapper_ns computed and reported, all
// in integer nanoseconds.
func TestBoundaryInvariantsAndComponentSpans(t *testing.T) {
	t.Run("the component spans are derived from the readings", func(t *testing.T) {
		const (
			a      int64 = 50_000_000_000
			setup  int64 = 10_000_000_000
			script int64 = 30_000_000_000
		)
		vs := []int64{12_000_000_000, 15_000_000_000}
		got, err := ComputeComponentSpans(a, setup, script, vs)
		if err != nil {
			t.Fatal(err)
		}
		// script_overhead_ns = script_ns − Σ V[j]
		if want := script - (vs[0] + vs[1]); got.ScriptOverheadNs != want {
			t.Errorf("script_overhead_ns = %d, want %d", got.ScriptOverheadNs, want)
		}
		// wrapper_ns = A − setup_ns − script_ns
		if want := a - setup - script; got.WrapperNs != want {
			t.Errorf("wrapper_ns = %d, want %d", got.WrapperNs, want)
		}
		// Every value is an integer nanosecond count.
		var _ int64 = got.ScriptOverheadNs
		var _ int64 = got.WrapperNs
	})

	t.Run("A >= setup_ns + script_ns", func(t *testing.T) {
		_, err := ComputeComponentSpans(10_000_000_000, 6_000_000_000, 6_000_000_000, nil)
		if err == nil {
			t.Fatal("an envelope shorter than its parts was accepted")
		}
		if !strings.Contains(err.Error(), "setup_ns + script_ns") {
			t.Fatalf("the rejection must name the invariant, got %v", err)
		}
	})

	t.Run("script_ns >= the sum of invocation intervals", func(t *testing.T) {
		_, err := ComputeComponentSpans(50_000_000_000, 0, 10_000_000_000,
			[]int64{6_000_000_000, 6_000_000_000})
		if err == nil {
			t.Fatal("a script shorter than its invocations was accepted")
		}
		if !strings.Contains(err.Error(), "sum of invocation intervals") {
			t.Fatalf("the rejection must name the invariant, got %v", err)
		}
	})

	t.Run("no value may be negative", func(t *testing.T) {
		for name, args := range map[string][3]int64{
			"elapsed": {-1, 0, 0},
			"setup":   {10, -1, 0},
			"script":  {10, 0, -1},
		} {
			if _, err := ComputeComponentSpans(args[0], args[1], args[2], nil); err == nil {
				t.Errorf("a negative %s was accepted", name)
			}
		}
	})

	t.Run("the boundary permits equality at both invariants", func(t *testing.T) {
		// A == setup + script and script == Σ V are legal: the invariants are
		// non-strict, and a zero overhead is a real measurement.
		got, err := ComputeComponentSpans(20_000_000_000, 5_000_000_000, 15_000_000_000,
			[]int64{15_000_000_000})
		if err != nil {
			t.Fatalf("the equality case must be legal: %v", err)
		}
		if got.ScriptOverheadNs != 0 || got.WrapperNs != 0 {
			t.Fatalf("spans = %+v, want both zero at the boundary", got)
		}
	})
}

// TestActionIntervalUsesPersistedSameBootMonotonicEndpoints is §22 test 56
// (SR-9): begin and end run as SEPARATE PROCESSES, end uses the persisted
// start, a matching boot identity is accepted, a changed one is rejected, and
// caller-supplied endpoints are rejected.
func TestActionIntervalUsesPersistedSameBootMonotonicEndpoints(t *testing.T) {
	dir := t.TempDir()
	start := Instant{ClockID: "CLOCK_MONOTONIC", Mono: 1_000_000_000, Realtime: "2026-09-01T00:00:00Z", BootID: "boot-a"}
	if err := WriteIntervalHandoff(dir, start); err != nil {
		t.Fatal(err)
	}

	t.Run("end uses the persisted start", func(t *testing.T) {
		// The closing process never saw the opening reading in memory: it
		// reads it back from disk, which is what makes begin and end two
		// separate step processes rather than one.
		end := Instant{ClockID: "CLOCK_MONOTONIC", Mono: 21_000_000_000, BootID: "boot-a"}
		got, err := CloseInterval(dir, end, nil)
		if err != nil {
			t.Fatal(err)
		}
		if want := int64(20_000_000_000); got != want {
			t.Fatalf("elapsed_ns = %d, want %d", got, want)
		}
	})

	t.Run("a matching boot identity is accepted", func(t *testing.T) {
		end := Instant{Mono: 2_000_000_000, BootID: "boot-a"}
		if _, err := CloseInterval(dir, end, nil); err != nil {
			t.Fatalf("a matching boot identity was rejected: %v", err)
		}
	})

	t.Run("a changed boot identity is rejected", func(t *testing.T) {
		end := Instant{Mono: 21_000_000_000, BootID: "boot-b"}
		_, err := CloseInterval(dir, end, nil)
		if err == nil {
			t.Fatal("a changed boot identity was accepted; the readings are on different timelines")
		}
		if !strings.Contains(err.Error(), "different timelines") {
			t.Fatalf("the rejection must say why, got %v", err)
		}
	})

	t.Run("an absent boot identity is rejected", func(t *testing.T) {
		if _, err := CloseInterval(dir, Instant{Mono: 21_000_000_000}, nil); err == nil {
			t.Fatal("a closing reading with no boot identity was accepted")
		}
	})

	t.Run("caller-supplied endpoints are rejected", func(t *testing.T) {
		supplied := int64(0)
		end := Instant{Mono: 21_000_000_000, BootID: "boot-a"}
		_, err := CloseInterval(dir, end, &supplied)
		if err == nil {
			t.Fatal("a caller-supplied start was accepted; an interval a caller can name is not a measurement")
		}
		if !strings.Contains(err.Error(), "refused") {
			t.Fatalf("the rejection must be explicit, got %v", err)
		}
	})

	t.Run("a closing reading before the opening one is rejected", func(t *testing.T) {
		end := Instant{Mono: 500_000_000, BootID: "boot-a"}
		if _, err := CloseInterval(dir, end, nil); err == nil {
			t.Fatal("a negative interval was accepted")
		}
	})

	t.Run("a missing handoff fails closed", func(t *testing.T) {
		empty := t.TempDir()
		if _, err := CloseInterval(empty, Instant{Mono: 1, BootID: "boot-a"}, nil); err == nil {
			t.Fatal("closing with no persisted opening reading was accepted")
		}
	})

	t.Run("the opening reading must carry a boot identity to be persisted", func(t *testing.T) {
		if err := WriteIntervalHandoff(t.TempDir(), Instant{Mono: 1}); err == nil {
			t.Fatal("a handoff with no boot identity was written")
		}
	})
}
