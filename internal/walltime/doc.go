// Package walltime implements the EWJ-2R13 complete-action wall-time
// measurement architecture: the testbucket-owned PHYSICAL envelopes (AT, VB,
// V), their separately recorded CONTAINMENT PEERS (CPA, CPB, CPV), the
// INDEPENDENT TRACE (VTA, VTB, VT), the frozen two-stage planning input
// binding, and the verifier that decides eligibility from those records alone.
//
// Three rules shape every type here, and each of them is fail-closed:
//
//   - ONLY TESTBUCKET MAKES A QUALIFYING TIMER. A reporter timestamp, a
//     GitHub step duration, or a shell `date` is an annotation. Every scored
//     endpoint is a fresh clock_gettime(CLOCK_MONOTONIC) read taken by this
//     package, and a platform that cannot supply one is INELIGIBLE rather
//     than approximated (see clock.go, contain.go).
//   - A MISSING RECORD IS NEVER AN ESTIMATE. An unclosed wrapper, an escaped
//     descendant, a copied endpoint, or an unbound planning input makes the
//     row terminal — it does not fill a denominator, train a score, or relax
//     a gate (see verify.go).
//   - THE PHYSICAL LEDGER OWNS ALL THE TIME. Peer and trace intervals are
//     auxiliary; they never shorten an envelope, never substitute for a
//     physical phase, and never become forecast input. The reconciliation
//     gate therefore compares like for like — trace against its own peer —
//     and never the shorter containment lifecycle against the longer
//     physical envelope (see gates.go).
//
// The package is Vitest-agnostic and Go-agnostic: it wraps an argv, not a test
// framework. Nothing in internal/core imports it, so the neutral core/adapter
// seam is unchanged and the Go adapter's rendered bytes are untouched.
package walltime

// SchemaVersion is the record/receipt schema identity. Every record carries it
// and the verifier refuses a stream that mixes versions: a schema change is a
// new measurement epoch, not a migration of old rows.
const SchemaVersion = "tb.walltime/v1"
