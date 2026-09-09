package walltime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/invakid404/testbucket/internal/nsmath"
)

// ComponentSpans is contract §3.1's derived accounting. Both derived values are
// computed rather than reported, so neither can be asserted independently of
// the readings it comes from.
type ComponentSpans struct {
	// ScriptOverheadNs is script_ns − Σ_j V[j]: the spec-file writes, the
	// `wall exec` startup and flag parsing, inter-invocation gaps, and
	// script-level wrapper cost.
	ScriptOverheadNs int64
	// WrapperNs is A − setup_ns − script_ns: bootstrap, inter-step wrapper
	// work, wait/reap and epilogue.
	WrapperNs int64
}

// ComputeComponentSpans derives §3.1's two component spans in the checked
// domain and enforces the invariants that make them meaningful.
//
// They are DERIVED, not recorded: a reported overhead could disagree with the
// readings it claims to summarise, and then no invariant would bind.
func ComputeComponentSpans(elapsedNs, setupNs, scriptNs int64, invocationNs []int64) (ComponentSpans, error) {
	for name, v := range map[string]int64{"elapsed_ns": elapsedNs, "setup_ns": setupNs, "script_ns": scriptNs} {
		if v < 0 {
			return ComponentSpans{}, fmt.Errorf("§3.1: %s is %d; all values must be >= 0", name, v)
		}
	}
	sumV, err := nsmath.SumNs("sum_invocation_ns", invocationNs...)
	if err != nil {
		return ComponentSpans{}, err
	}
	// A >= setup_ns + script_ns
	floor, err := nsmath.SumNs("setup_plus_script", setupNs, scriptNs)
	if err != nil {
		return ComponentSpans{}, err
	}
	if elapsedNs < floor {
		return ComponentSpans{}, fmt.Errorf("§3.1: A (%d) < setup_ns + script_ns (%d)", elapsedNs, floor)
	}
	// script_ns >= Σ_j V[j]
	if scriptNs < sumV {
		return ComponentSpans{}, fmt.Errorf("§3.1: script_ns (%d) < the sum of invocation intervals (%d)", scriptNs, sumV)
	}
	overhead, err := nsmath.SumNs("script_overhead_ns", scriptNs, -sumV)
	if err != nil {
		return ComponentSpans{}, err
	}
	wrapper, err := nsmath.SumNs("wrapper_ns", elapsedNs, -setupNs, -scriptNs)
	if err != nil {
		return ComponentSpans{}, err
	}
	return ComponentSpans{ScriptOverheadNs: overhead, WrapperNs: wrapper}, nil
}

// intervalHandoffSchema names the persisted begin/end handoff.
const intervalHandoffSchema = "testbucket.wall-interval-handoff/v1"

// IntervalHandoff is what `wall begin` persists so `wall end` — a SEPARATE
// PROCESS in a sibling step — can close the interval against the same timeline.
//
// It exists because no process spans an action: begin and end are two step
// processes, so the opening reading has to survive between them, and it has to
// carry the boot identity that makes the two readings comparable at all.
type IntervalHandoff struct {
	Schema string  `json:"schema"`
	Start  Instant `json:"start"`
}

// WriteIntervalHandoff persists the opening reading.
func WriteIntervalHandoff(dir string, start Instant) error {
	if start.BootID == "" {
		return fmt.Errorf("interval handoff: the opening reading carries no boot identity")
	}
	b, err := json.Marshal(IntervalHandoff{Schema: intervalHandoffSchema, Start: start})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "wall-interval-handoff.json"), b, 0o644)
}

// CloseInterval reads the PERSISTED opening reading and closes the interval
// against it.
//
// Three refusals, each for a different failure:
//
//   - a caller-supplied endpoint is rejected outright, because an interval a
//     caller can name is not a measurement;
//   - a changed boot identity is rejected, because two readings from different
//     monotonic epochs are on different timelines and their difference is not
//     a duration;
//   - a closing reading before the persisted opening one is rejected, since a
//     negative interval is never a legal value.
func CloseInterval(dir string, end Instant, callerSuppliedStartNs *int64) (int64, error) {
	if callerSuppliedStartNs != nil {
		return 0, fmt.Errorf("interval close: a caller-supplied start endpoint is refused; the opening reading comes from the persisted handoff")
	}
	b, err := os.ReadFile(filepath.Join(dir, "wall-interval-handoff.json"))
	if err != nil {
		return 0, fmt.Errorf("interval close: no persisted opening reading: %w", err)
	}
	var h IntervalHandoff
	if err := json.Unmarshal(b, &h); err != nil {
		return 0, fmt.Errorf("interval close: handoff does not parse: %w", err)
	}
	if h.Schema != intervalHandoffSchema {
		return 0, fmt.Errorf("interval close: handoff schema %q is not %q", h.Schema, intervalHandoffSchema)
	}
	if end.BootID == "" {
		return 0, fmt.Errorf("interval close: the closing reading carries no boot identity")
	}
	if h.Start.BootID != end.BootID {
		return 0, fmt.Errorf("interval close: boot identity changed from %q to %q; the two readings are on different timelines",
			h.Start.BootID, end.BootID)
	}
	if int64(end.Mono) < int64(h.Start.Mono) {
		return 0, fmt.Errorf("interval close: the closing reading %d precedes the persisted opening reading %d",
			end.Mono, h.Start.Mono)
	}
	return nsmath.SumNs("elapsed_ns", int64(end.Mono), -int64(h.Start.Mono))
}
