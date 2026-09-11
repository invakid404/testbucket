package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestWallDecoderPreservesWirePresence is §15.1c's presence matrix applied where
// the wire facts still exist — in the decoder.
//
// Three decoder defaults erased them, and each turned a malformed store into a
// well-formed one BEFORE anything could refuse it, so Validate's corresponding
// checks were unreachable from any decoded store.
func TestWallDecoderPreservesWirePresence(t *testing.T) {
	const key = "sha256:key"
	fit := map[string]any{
		"fitted_at":                    "2026-09-01T00:00:00Z",
		"rows_used":                    24,
		"runs_used":                    3,
		"fixed_ns":                     "1000000000",
		"scale":                        "1",
		"whole_invocation_overhead_ns": "0",
		"per_slice_overhead_ns":        "0",
		"residual_mae_ns":              "0",
		"residual_p90_ns":              "0",
		"rank_support":                 []string{"fixed", "scale", "whole_invocation_overhead", "per_slice_overhead"},
	}
	base := func() map[string]any {
		return map[string]any{
			"model_version":            1,
			"comparability_key_digest": key,
			"status":                   "ok",
			"observations":             []any{},
		}
	}
	decode := func(t *testing.T, m map[string]any) (*WallObject, error) {
		t.Helper()
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		var w WallObject
		if err := json.Unmarshal(b, &w); err != nil {
			return nil, err
		}
		return &w, nil
	}

	t.Run("a complete record still decodes and validates", func(t *testing.T) {
		m := base()
		for k, v := range fit {
			m[k] = v
		}
		w, err := decode(t, m)
		if err != nil {
			t.Fatalf("a complete ok record must decode: %v", err)
		}
		if w.Fit == nil {
			t.Fatal("the fit group is absent after decoding a complete record")
		}
		if w.Fit.Scale != 1 {
			t.Errorf("scale decoded as %v, want 1", w.Fit.Scale)
		}
		if err := w.Validate(); err != nil {
			t.Fatalf("a complete ok record must validate: %v", err)
		}
	})

	t.Run("an absent observations container is not invented", func(t *testing.T) {
		m := base()
		delete(m, "observations")
		for k, v := range fit {
			m[k] = v
		}
		w, err := decode(t, m)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if w.Observations != nil {
			t.Fatal("the decoder invented an observations container; §15.1c makes it always present once wall exists, and absence is a refusal not an empty ring")
		}
		if err := w.Validate(); err == nil {
			t.Fatal("a record with no observations container must be refused")
		}
	})

	t.Run("a null observations container is not a container", func(t *testing.T) {
		m := base()
		m["observations"] = nil
		for k, v := range fit {
			m[k] = v
		}
		w, err := decode(t, m)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if err := w.Validate(); err == nil {
			t.Fatal("`observations: null` must be refused; null is not an empty container")
		}
	})

	t.Run("an absent coefficient is not a measured zero", func(t *testing.T) {
		// THE DEFECT: fitted_at present, scale absent. This decoded to a
		// complete all-zero fit and passed validation — a model predicting that
		// work takes no time, from a record that never said so.
		for _, missing := range []string{
			"scale", "fixed_ns", "whole_invocation_overhead_ns", "per_slice_overhead_ns",
			"residual_mae_ns", "residual_p90_ns", "rows_used", "runs_used", "rank_support",
		} {
			m := base()
			for k, v := range fit {
				m[k] = v
			}
			delete(m, missing)
			if _, err := decode(t, m); err == nil {
				t.Errorf("a fit group missing %q was accepted; §15.1c's group is all present or all absent", missing)
			} else if !strings.Contains(err.Error(), missing) {
				t.Errorf("the refusal for missing %q does not name it: %v", missing, err)
			}
		}
	})

	t.Run("a forbidden coefficient reaches the presence matrix", func(t *testing.T) {
		// THE DEFECT: status insufficient with a coefficient and no fitted_at.
		// The decoder returned before reading the coefficient, so the matrix
		// never saw the leaf it exists to reject.
		m := base()
		m["status"] = "insufficient"
		m["failure_subtype"] = "rows_below_minimum"
		m["fixed_ns"] = "1000000000"
		if _, err := decode(t, m); err == nil {
			t.Fatal("an insufficient record carrying a coefficient was accepted")
		}
	})

	t.Run("an empty fitted_at is not a fit", func(t *testing.T) {
		m := base()
		for k, v := range fit {
			m[k] = v
		}
		m["fitted_at"] = ""
		if _, err := decode(t, m); err == nil {
			t.Fatal(`"fitted_at": "" was accepted as the instant a fit was made`)
		}
	})

	t.Run("an empty coefficient string is a named failure", func(t *testing.T) {
		m := base()
		for k, v := range fit {
			m[k] = v
		}
		m["scale"] = ""
		_, err := decode(t, m)
		if err == nil {
			t.Fatal(`"scale": "" was accepted and silently became 0`)
		}
		if !strings.Contains(err.Error(), "scale") {
			t.Errorf("the failure does not name scale: %v", err)
		}
	})

	t.Run("an insufficient record with no fit group still decodes", func(t *testing.T) {
		m := base()
		m["status"] = "insufficient"
		m["failure_subtype"] = "migrated_no_history"
		w, err := decode(t, m)
		if err != nil {
			t.Fatalf("an all-absent fit group is §15.1c's own insufficient shape: %v", err)
		}
		if w.Fit != nil {
			t.Error("an insufficient record materialized a fit group")
		}
		if err := w.Validate(); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})
}
