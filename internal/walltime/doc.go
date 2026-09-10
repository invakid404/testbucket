// Package walltime measures the instrumented run-bucket interval and turns it
// into something a later plan can learn from.
//
// The lifecycle is three nested intervals and one document. `wall begin` and
// `wall end` bracket the ACTION interval A across the step processes that make
// it up; `wall exec` measures the generated bucket SCRIPT and each rendered
// INVOCATION inside it; `wall verify` decides whether what was recorded is a
// complete measurement; and `wall assemble-observation` produces the single
// atomic per-bucket observation that `ingest` qualifies, appends to the
// bounded history ring, and refits the wall model from.
//
// A is the instrumented run-bucket interval. It is narrower than the job and
// narrower than the action as a whole: acquisition and install are outside it,
// because a wrapper cannot read a clock before it exists.
//
// Three rules shape every type here, and each of them is fail-closed:
//
//   - ONLY TESTBUCKET MAKES A QUALIFYING TIMER. A reporter timestamp, a
//     GitHub step duration, or a shell `date` is an annotation. Every endpoint
//     is a fresh clock_gettime(CLOCK_MONOTONIC) read taken by the producer
//     that records it, and a platform that cannot supply one is INELIGIBLE
//     rather than approximated (see clock.go).
//   - A MISSING RECORD IS NEVER AN ESTIMATE. An unclosed wrapper, an escaped
//     descendant or a copied endpoint makes the row terminal — it does not
//     fill a denominator, train a model, or relax a gate (see verify.go).
//   - TEARDOWN IS PART OF THE MEASUREMENT. The closing read is taken after the
//     root child is reaped and its process group has been signalled and
//     drained, so an interval never ends while the work it measures might
//     still be running (see exec.go).
//
// What this package is NOT, deliberately: there are no containment peers, no
// independent trace collectors, no signed or hash-chained ledgers, no Stage-1
// or Stage-2 receipts, and no frozen gates. Those belonged to a research
// authority model that the practical contract replaces with §3's trusted-CI
// boundary; the tombstones in aeta.go, palloc.go and record.go say what each
// of them was.
//
// The package is Vitest-agnostic and Go-agnostic: it wraps an argv, not a test
// framework. Nothing in internal/core imports it, so the neutral core/adapter
// seam is unchanged and the Go adapter's rendered bytes are untouched.
package walltime

// SchemaVersion is the record/receipt schema identity. Every record carries it
// and the verifier refuses a stream that mixes versions: a schema change is a
// new measurement epoch, not a migration of old rows.
const SchemaVersion = "tb.walltime/v1"
