# Independent scope adjudication

Review date: 2026-09-07  
Decision basis: the current working-copy revision (`@`) and R54 parent
`8d27e9bccc537032615578748c80bed9300ec7bd`, read through Jujutsu. No
author's conclusion is adopted by this review.

## Decision

The scope is **not admissible**. The missing governing artifacts alone prevent
a coherent, testable product claim, and the source still contains several of
the gaps that `scope-adversary.md` calls implementation deltas. Under the
requested admission rule, a declared future fix is not closure: the claim,
observable, acceptance check, and implementation path must agree in actual
files.

This is **NEEDS_SCOPE_REVISION**, rather than `EXTERNAL_BLOCKED`. A real
consumer campaign is also absent, but external evidence cannot be the primary
blocker while the specification that would judge it is absent and internally
incomplete.

## Artifact check

At intake, `jj status` and `jj file list -r @` showed one new scope artifact:
`scope-adversary.md`. They did not show any of the following:

| Required material | Result | Consequence |
|---|---|---|
| `acceptance-contract.md` | Absent | No authoritative claim, observable, pass/fail rule, or evidence format. |
| `scope.md` | Absent | No bounded product surface, consumer scope, or explicit non-goals. |
| `assumption-ledger.md` | Absent | No owner-approved assumptions about the CI trust model, consumer behaviour, cold/warm state, or timing meaning. |
| Salvage map | No separately tracked map | No keep/remove/defer decision connects the large current machinery to the claimed product. |
| `scope-adversary.md` | Present only as the working-copy addition | It is useful input, but it cannot substitute for the four governing artifacts or close the source gaps it acknowledges. |

The report's conclusion is therefore not independently reproducible as a
contract decision. In particular, its statement that current-source absence is
not a scope defect conflicts with this review's required standard: all
adversarial findings must be closed in actual files before approval.

## R54 path is not narrow enough to assume away

The Jujutsu diff from the stated predecessor
`9537850eed193b9dfe29dce272d43b87d1f4bbf9` to R54 changes 17 files
(614 additions, 25 deletions), not only an archive installer and its tests. It
includes `.github/workflows/bucketed-reusable.yml`, `README.md`,
`cmd/testbucket/wall.go`, `cmd/testbucket/wallattestrunner.go`,
`internal/walltime/record.go`, `internal/walltime/runnerattest.go`, and
`internal/walltime/stage.go`, as well as installer and test changes. The R1
statement in `scope-adversary.md` that this is an installer-only `+328/-5`
change is therefore false for the named predecessor/successor pair.

That matters to scope: a safe implementation path must identify which R54
surfaces are part of the measured product, which are compatibility-sensitive,
and which are excluded. No current artifact supplies that mapping.

## Required observable contract

The current source can support a precisely named *invocation envelope*, but it
does not yet make that a governed user-facing contract.

`internal/walltime/exec.go` takes its first monotonic reading before wrapper
setup and child spawn, then takes its closing reading after the child wait,
drain, observer/controller close, containment teardown, and other wrapper
cleanup. `internal/runner/vitestrunner/render.go` writes a spec and invokes
`testbucket wall exec` afterwards. Thus the observable available today is not
pure Vitest time, GitHub action time, or job time. It is a wrapper-qualified
invocation envelope; the spec-file write and `wall exec` startup occur outside
it, while wrapper work within the envelope occurs inside it.

Before implementation proceeds, the contract must name this metric, its exact
start and end events, inclusions, exclusions, terminal-state handling, clock,
and unit of observation. If the product calls it a full-Vitest label, the
truthful name must make clear whether it means the complete spawned
façade/Vitest lifecycle plus wrapper teardown, rather than claiming a
separately observable Vitest-only duration. The same document must say whether
the action interval is a distinct metric and must prohibit presenting either
as GitHub job duration.

## Material unresolved defects

| Adversary finding | Independent result | Actual-file evidence and required closure |
|---|---|---|
| T1 | Open | The code defines an `Exec` envelope, but no acceptance contract names its boundary or disambiguates it from pure Vitest/action/job time. Add the exact observable definition and an integration fixture that proves the façade lifecycle lies inside it. |
| L1 | Open | `vitestrunner/render.go` combines `wholeUnits`/`wholeFiles` into one invocation, so one observed V can cover several units. `TrainingLabel` has one `UnitID`. Require one-unit calibration invocations or an explicit group-level model; prohibit copying an aggregate V onto each unit. |
| L2 | External evidence absent | No tracked sealed calibration/holdout corpus or adopted consumer campaign establishes learnability. Define the pre-treatment split, label unit, held-out population, absolute and relative gates first; then collect the evidence. |
| E1 | Open | `internal/core/store.go` defines rolling EWMA unit weights, while `run-bucket/action.yml` prints `estimated ...s`. The `--palloc-scorer` help in `cmd/testbucket/main.go` explicitly says `est_seconds` continues to report store weights. It is not an action ETA and need not be a makespan. Relabel it as a historical work estimate or replace it with a validated action-ETA value and matching acceptance test. |
| BC1 | Open | The current CLI selects a scorer through `--palloc-scorer`; it has no single bound `allocation_mode` field or negative manifest-diff admission test. Define B and C as byte-identical except for that one field, with the same binary, action revision, instrumentation, workload, store, environment, and schedule. |
| S1 | Open | There is no minimal trusted-CI ledger in an acceptance artifact. Current source instead contains extensive signing, replay, delivery, and authority mechanisms. State the minimal identities required for the practical claim and explicitly defer any stronger provenance mechanism not needed for it. |
| S2 | Open | Ordinary `core.LoadStore` supports cold start, but no governing artifact declares whether scored efficacy is warm-only, cold-only, or stratified. State the rule and require the corresponding no-store/invalid-store behaviour without mixing the populations. |
| C1 | Open | `vitestrunner/collide.go` may implement conservative collision handling, but there is no pinned-consumer profile and acceptance fixture binding that behaviour to the claimed workload. Add the exact consumer revision, config/lock identities, selected and excluded membership, and collision regression fixture. |
| P1 | Open | The regular plan path is generic (`--k` defaults to 6 and file parallelism may exceed 1). Constants alone do not bind a scored run to Vitest, K=8, count=1, serial execution, and the complete 0…7 bucket set. Make those observable admission checks. |
| G1 | Open | `--wall-dir` is correctly refused for the Go runner, but no consumer-safety contract or exact pinned regression evidence proves the claimed Go consumer remains unchanged. Specify the unchanged Go surface and the compatibility test required on every repin. |
| N1 | Open | `palloc.go` has a useful runtime-provenance prohibition, while the ordinary planner still uses timing-store EWMA. The scope never states which route may affect experimental topology or how pre-treatment/holdout exclusion is verified. Bind the allowed features and training cutoff in the acceptance contract. |
| M1 | Open | The asserted Mandel revision, façade, lock, exclusions, and misc lane are not represented as an acceptance input in this tree, and the adversary report admits R54 is not adopted there. Add an exact pinned consumer fixture and an adoption gate before treating the workload claim as proved. |
| D1 | Open | `gates.go` executes an 80-combined-row population, but its campaign comment says “eighty complete action observations per arm.” The denominator is contradictory in actual files. Correct it and freeze five pairs, ten runs, 80 total rows/40 per arm, eight complete buckets per run, three UTC dates, and the 14-day window in the contract. |
| D2 | Open | `schedule.go` stores a seed and ordered references but does not derive a counterbalanced arm order from that seed; date comparison skips an empty observed instant. Define the exact predeclared B/C sequence and require actual authenticated starts to match it. Do not call it randomized unless a reproducible randomization algorithm is implemented and used. |
| D3 | Open | `gates.go` implements Aeta absolute gates (10 s MAE and 20 s maximum) but no relative-error gate. Freeze all forecast, tail, cost, and population thresholds in the acceptance contract and implement every one before evidence can pass. |
| R1 | Open | The adversary's claimed R54 diff scope is factually wrong, as above. Supply a source-to-claim map and a focused regression matrix instead of treating unrelated R54 changes as invisible. |
| Q1 | Open | No salvage map exists. `ablation.go`, `campaign.go`, `stage.go`, `record.go`, `action.go`, and `exec.go` show a broad research-grade protocol: mandatory ablation strata, protected authorities, signatures, replay, cgroup ownership, observers, and durable claims. For each component, justify it as necessary for this narrow claim or mark it removed/deferred. |
| Q2 | Product decision not recorded | The trusted/non-hostile CI threat model is only asserted in the adversary report, not entered in an assumption ledger. The owner must explicitly choose that model and state that hostile tests/runners, detached-child security guarantees, artifact tampering, and multi-principal provenance are out of scope unless separately justified. |

No row above is closed merely because the adversary describes it as an
implementation delta. Several are directly acknowledged there as currently
absent (`est_seconds` truthfulness, one-field B/C isolation, exact consumer
adoption, strict profile enforcement, relative gates, and real evidence).

## Minimum coherent implementation path

The revised scope should authorize this small, inspectable path from R54:

1. Freeze one exact Vitest consumer revision, command/façade, config and lock,
   selected/excluded work, K=8, count=1, serial file execution, and full bucket
   coverage. Add a separate unchanged-Go compatibility check.
2. Emit the named invocation-envelope record with the boundary above. Associate
   it with one scheduled unit only, or use a declared group model whose
   prediction and acceptance gates operate at that same group level.
3. Train only on pre-treatment labels; freeze the feature schema, cutoff,
   exclusion set, model, and held-out scoring procedure. Demonstrate the
   frozen absolute and relative forecast gates on the held-out consumer data.
4. Either truthfully relabel current `est_seconds` as historical test-work or
   replace it with the validated action ETA. The displayed text and measured
   target must be identical in meaning.
5. Run a reproducible fixed counterbalanced B/C campaign with a single
   `allocation_mode` difference. Preserve the complete population and raw
   observations; fail on any other manifest difference, missing row, wrong
   profile, wrong date/order, or terminal failure.
6. Keep only machinery required to execute, identify, and compare that trusted
   experiment. Do not make cgroups, multiple signing roles, replay systems,
   release closure, mandatory ablations, or hostile-principal infrastructure
   product eligibility requirements without a documented necessity tied to the
   claim.

Once those documents exist and the listed source/evidence checks are closed,
the missing real campaign can be assessed as `EXTERNAL_BLOCKED` or passed on
its merits. It is not yet meaningful to promote the current scope to that
stage.

NEEDS_SCOPE_REVISION
