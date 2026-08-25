package runner

import "math"

// RunSummary is one batch of a runner's timing output reduced to weights. The
// adapter produces it (for the Go adapter, from `go test -json`); the core
// ingests it into the store and audits a run against a plan with it.
//
// The vocabulary is package/test because the Go adapter is adapter #1; another
// adapter maps its own nouns onto the same fields (a Vitest file is a
// "package", a test case a "test").
type RunSummary struct {
	// PackageSeconds sums the target-level elapsed across every invocation in
	// the batch. Summing (rather than taking a max) is what makes a split
	// target reconstitute correctly: the S count-shards or name slices of one
	// target each report their own elapsed, and their sum is the whole-target
	// weight the next plan wants.
	//
	// A batch missing one bucket's artifact therefore under-measures the
	// targets that bucket held. EWMA keeps that from rewriting the split, and
	// the drop shows up in the next plan's measured wall-time.
	PackageSeconds map[string]float64
	PackageRuns    map[string]int
	// TestSeconds holds TOP-LEVEL runnable weights only. A parent's pass event
	// already reports the elapsed time of everything it ran, subtests
	// included, so folding child events in would count that time twice.
	TestSeconds map[string]map[string]float64
	// Failed targets contribute no fresh weight: a target that aborted under
	// the race detector or hit its -timeout reports a wall time that measures
	// the failure, not the work.
	Failed  map[string]bool
	NoTests map[string]bool
	Lines   int
	Events  int
	// Subtests counts child pass events seen and deliberately not weighed.
	Subtests int
	// Implausible counts events whose elapsed could not be believed.
	Implausible int
	Malformed   int
}

// NewRunSummary returns an empty summary with its maps initialised.
func NewRunSummary() *RunSummary {
	return &RunSummary{
		PackageSeconds: map[string]float64{},
		PackageRuns:    map[string]int{},
		TestSeconds:    map[string]map[string]float64{},
		Failed:         map[string]bool{},
		NoTests:        map[string]bool{},
	}
}

// SumSeconds adds durations in the order given and in integer microseconds, so
// the result is exactly reproducible: integer addition is associative where
// float addition is not, and callers always pass a value list built from
// sorted keys rather than a map range.
func SumSeconds(values []float64) float64 {
	var micros int64
	for _, v := range values {
		if !PlausibleSeconds(v) {
			// Non-finite values, and finite ones too large to survive the
			// microsecond conversion, are dropped rather than folded in.
			// Beyond MaxPlausibleSeconds `int64(math.Round(v*1e6))` is
			// implementation-defined in Go, which would defeat the exact
			// reproducibility this helper exists to provide and could turn the
			// total — and the whale threshold derived from it — negative.
			continue
		}
		micros += int64(math.Round(v * 1e6))
	}
	return float64(micros) / 1e6
}

// MaxPlausibleSeconds bounds any single duration this tool will believe. It is
// ~34 years: far above any real target, shard or batch, and far below the
// point where the microsecond conversion leaves int64 range.
const MaxPlausibleSeconds = 1 << 30

// PlausibleSeconds reports whether a duration read from an artifact can be
// used arithmetically. Elapsed comes from output that a corrupt or truncated
// upload can write, so it is untrusted input, not a program invariant.
func PlausibleSeconds(v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	return v >= -MaxPlausibleSeconds && v <= MaxPlausibleSeconds
}
