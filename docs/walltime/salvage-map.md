# Component salvage map (adjudicator Q1)

Read-only, `jj` only, 2026-09-07 UTC. Q1 asked for a component-level decision on six named files:
each is justified as necessary for this narrow claim, or marked removed/deferred. Sizes are
measured at R54 `8d27e9bccc537032615578748c80bed9300ec7bd`.

The narrow claim is: *balance a K-way Vitest matrix by the instrumented run-bucket interval, and
make the displayed estimate mean that same quantity.* A component is **necessary** just when the
claim cannot be produced, identified, or compared without it.

| File | Lines | Exported | Decision |
|---|---:|---:|---|
| `internal/walltime/exec.go` | 1964 | 5 | **SIMPLIFY — necessary kernel** |
| `internal/walltime/action.go` | 955 | 7 | **SIMPLIFY — necessary kernel** |
| `internal/walltime/record.go` | 621 | 13 | **SIMPLIFY — necessary, reduced to one document** |
| `internal/walltime/stage.go` | 2423 | 34 | **REMOVE — extract 4 types first** |
| `internal/walltime/campaign.go` | 773 | 8 | **REMOVE — replaced by a manifest + validator** |
| `internal/walltime/ablation.go` | 480 | 2 | **REMOVE — deferred, no re-entry planned** |

---

## exec.go — SIMPLIFY, necessary

**Why necessary.** It *is* the observable. Its first monotonic reading precedes wrapper setup and
child spawn; its closing reading follows root child wait and reap, same-PGID signal with bounded
escalation, and group drain — §3.3's three steps, and nothing after them. That bracket is the
`Exec` envelope `V` the contract names, and no other component can produce it.

**Keep:** the two readings and their ordering; concrete argv spawn with stdout/stderr passthrough;
exit-status preservation; cancellation forwarding; **root wait and reap** of the one child it
parented; **same-PGID signal** (TERM→KILL escalation on deadline); and **group drain** — probing
that the PGID no longer exists before the closing read. It does **not** reap descendants: a
non-subreaper parent cannot `waitpid` a grandchild (`scope.md` §4.3). Regressions in `exec_test.go` and
`cancel_test.go`.

**Remove from it:** the three-ledger split, signer and key handling, observer startup and admission
handshakes, cgroup admission and freeze/thaw, and peer/trace record emission. These are ~⅔ of the
file and serve the hostile-runner model that §0.1 of the contract places out of scope.

**Risk if reordered:** the process-group run/wait/signal/drain path is extracted *before*
`contain_linux.go` is deleted, or cancellation and escalation behaviour is lost silently.

## action.go — SIMPLIFY, necessary

**Why necessary.** It defines the second observable, the instrumented run-bucket interval `A`:
`BeginAction` takes the opening reading, `EndAction` the closing one after the action-state handoff
removal and after the measured process group has been drained. The contract's boundary — including
what sits outside it at both ends — is stated against this file's actual ordering.

**Keep:** begin/end readings and their placement relative to teardown; the action-state handoff
between steps; terminal-state and reason recording; the rollback path that cleans up resources
started before a failed begin.

**Remove from it:** observer process startup (`BeginAction` currently launches two), peer/trace
brackets, signing, and the sealed-directory step. The closing record write stays *outside* the
interval by construction and is named as such in §3.2 rather than hidden.

## record.go — SIMPLIFY, necessary but much smaller

**Why necessary.** Something has to serialize an observation and let a verifier reject an incomplete
or mismatched one. That is the evidence the claim rests on.

**Keep:** one `testbucket.wall-observation/v1` document per bucket; atomic write; canonical JSON
and digest; nanoseconds as strings; terminal state, exit code, failure reason.

**Remove from it:** the multi-role ledger (`Role`, `Producer`, `Level` fan-out), hash-chained
append-only streams, signer identity, and the key log. Thirteen exported types collapse to one
document plus its invocation rows.

## stage.go — REMOVE, but extract four types first

**Why not necessary.** Stage-1/Stage-2 sealed derivation, protected-authority signatures, planning
input bundles, replay attestation, and durable one-shot planner claims all serve authority and
non-repudiation. The claim needs neither: under a trusted-CI model (contract §0.1) an ordinary
content digest and a retained CI artifact are sufficient provenance.

**Why this file cannot simply be deleted.** Several types the rest of the package depends on live in
or near its orbit; the ordering that follows from that is **contract §0.5a**, and this entry
restates none of it.

**Deferred, not forbidden:** if the threat model ever changes by measurement (contract §12), a
signing layer would be re-introduced *around* the observation document, not by restoring this file.

## campaign.go — REMOVE, replaced by a manifest and a validator

**Why not necessary as written.** It encodes a research population — signed arm loading, index
authentication, verdict sets — but cannot detect an omitted or substituted attempt, which is the
one property a campaign actually needs. The scope replaces it with a plain manifest plus a
validator that enumerates every harness run from the Actions API and fails on an unaccounted one.

**What the replacement keeps:** exactly the arithmetic that was already correct — the campaign
population, date-count, and window values are frozen in `acceptance-contract.md` §10.4 and are
**not restated here** (R14-F1), so deleting this file cannot lose them and cannot contradict them
either.

## ablation.go — REMOVE, deferred with no planned re-entry

**Why not necessary.** Twelve controlled ablations across four topology strata answer "which
mechanism caused the improvement". This claim asks only "is C's makespan better than B's on this
workload", which the paired campaign answers directly. The ablations are a research obligation the
narrow product does not carry.

**Deferred.** If a future question needs mechanism attribution, ablations are re-authored against
whatever the design is then — not restored from this file, whose strata assume the removed
campaign/stage machinery.

---

## Aggregate effect

| | Lines at R54 | Disposition |
|---|---:|---|
| kept and simplified (`exec`, `action`, `record`) | 3,540 | reduced to a begin/run/exec/end lifecycle, one document, one verifier |
| removed (`stage`, `campaign`, `ablation`) | 3,676 | with four types extracted from `stage.go` first |

No component above is retained "because it exists". Each kept one is tied to producing,
identifying, or comparing the measured quantity; each removed one is tied to an authority,
non-repudiation, or mechanism-attribution obligation the narrow claim does not make.
