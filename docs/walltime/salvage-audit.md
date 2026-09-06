# R54 practical wall-time salvage audit

Status: **SALVAGE_READY**  
Target: R54 `8d27e9bccc537032615578748c80bed9300ec7bd`  
Baseline: `v0.2.2` and `master@origin`, both `693a19981fb6e0061d3fab62e59d75dc1c01ff3f`  
Mode: read-only repository audit; `jj` was the only VCS used

## Outcome

R54 should not be merged or preserved wholesale as the implementation of practical wall-time balancing. It adds a useful measurement and compatibility substrate, but the shipped plan → test → record loop still learns Vitest reporter timing, not complete run-bucket execution time. Its wall model is an opt-in, externally supplied, signed research surface whose labels target invocation `V`, while the product target is the complete run-bucket execution span `A`. The ordinary planner does not use that scorer, the record job does not download or ingest the wall records, and the dogfood Vitest lane does not enable wall measurement.

The practical salvage is well bounded: keep 22 changed files, simplify 49, remove 112, and leave one syntax/lint file uncertain. The exact 184-path classification, component dependencies, schemas, action inputs, and workflow interface are machine-readable in `component-map.json`; the inventory was checked to contain all 184 `jj diff --summary` paths exactly once.

This audit is **not externally blocked**. The prior handoff's protected-environment, campaign-harness, release-resolver, branch-protection, signer, archive, and fleet-attestation blockers apply to the historical research campaign, not to the practical product salvage.

## Scope and evidence

- The working copy was clean and its parent was exactly R54: `tvlupxsw 8d27e9bc`; the empty working-copy commit was `f13c7c28`.
- `v0.2.2` and the remote master bookmark resolve to `693a19981fb6e0061d3fab62e59d75dc1c01ff3f`. The local `master` bookmark is stale at the initial commit `c2ae47`; it was not used as the comparison base.
- The exact R54 delta is 184 files, 53,295 insertions, and 972 deletions.
- The handoff SHA-256 is `cfc27b4997ed4b9d7456989cfcbb9cb559ff01507d8e2411157f411bb3806fe3`; `/tmp/claude-501/.../scratchpad/acceptance-contract.md` is the normalized `/tmp` spelling of the recovered contract path below.
- The requested frozen contract no longer existed at its original temporary path. A surviving byte-identical copy was recovered at `/private/tmp/claude-501/-Users-inva-Coding-testbucket-equal-wall-time/9c1a590b-31fc-4751-832b-457ddbb28893/scratchpad/acceptance-contract.md`; its SHA-256 is `2f23ef5cca314a805b5d49f674040445e8823f2f5cdf342591c2e00a2805c412`, matching `/tmp/testbucket-ewj2-campaign/source-handoff.md:19`.
- The contract was used as historical design evidence. Its product outcome is explicit at `acceptance-contract.md:5`, and its distinction between physical action `A`, whole job `J`, and legacy reporter estimate `E` is at `:25`. Its protected authority, triple observers, cgroup, sealed training, and campaign population are historical mechanisms, not present product requirements (`:29-33`, `:39-61`, `:73-94`, `:108-125`).
- The handoff says no campaign population was collected (`source-handoff.md:39-51`) and lists campaign-only source/external blockers (`:55-96`). Those facts argue against retaining proof infrastructure merely because it exists; they do not block a small production feedback loop.

## Product truth at R54

The original practical loop should be:

```text
plan from prior wall history
        │
        ▼
balance units by predicted run-bucket execution cost
        │
        ▼
run setup + complete Vitest bucket process lifecycle
        │
        ▼
write/upload one complete monotonic bucket observation
        │
        ▼
audit exact plan membership + runner coverage
        │
        ▼
learn/update wall model on the default branch
        └───────────────────────────────► next plan
```

R54 breaks the feedback edge after upload. The wall artifact is uploaded at `.github/workflows/bucketed-reusable.yml:1087-1094`, but the record job downloads only timing events and the shard plan at `:1144-1155`, then invokes the normal record action at `:1165-1179`. That action audits reporter event files and calls `testbucket ingest` at `.github/actions/record/action.yml:91-152`. `runIngest` parses runner reports at `cmd/testbucket/main.go:565-568`; `ApplyIngest` updates rows from `RunSummary.PackageSeconds` at `internal/core/ingest.go:204-223`.

Consequently, `internal/core/plan.go:400` calling the existing store values “measured wall-time” is misleading for Vitest: those values are reporter durations, not the complete measured run-bucket span.

### Critical product gaps

1. **No wall-time learning loop.** No ordinary record path consumes a wall observation. `PhysicalVReceipt`, `SelectedWorkDocument`, and `TopologyValidationReceipt` are declared at `internal/walltime/labelevidence.go:30-105`, but production search finds no constructor that emits them. `wall train` consumes a prebuilt receipt set (`cmd/testbucket/wall.go:557-588`); normal CI cannot create that set.

2. **Wrong target.** Palloc explicitly learns historical physical invocation `V`, not action `A` (`internal/walltime/palloc.go:12-19`, `:127-160`). The complete action forecast is separately instantiated by Aeta (`internal/walltime/aeta.go:243-303`) and never becomes the partition weight.

3. **Default behavior is unchanged.** `--wall-dir` is empty by default and promises v0.2.2 bytes (`cmd/testbucket/main.go:342`). The ordinary planner at `:380-433` supplies no `AllocationScore`; only the frozen bundle path at `:349-364` can do so. The Vitest dogfood lane at `.github/workflows/bucketed.yml:86-99` supplies no `wall-time-dir`.

4. **Predictions cannot distinguish ordinary files well.** The runtime feature schema contains only `atom_size`, `file_count`, `is_slice`, `path_depth`, `runnable_count`, and `slice_share` (`internal/planbind/features.go:13-28`, vectors at `:62-100`). It intentionally excludes timing/history. Many one-file units therefore have identical feature vectors despite very different execution costs.

5. **Displayed ETA and allocation diverge.** `AllocationScore` can replace KK's packing weight (`internal/core/plan.go:107-120`, `:189-203`), but bucket `Seconds` continues summing legacy unit weights (`:205-213`), and the plan states that `est_seconds` remains the reporter estimate (`:237-243`). Matrix serialization exposes only that legacy number (`:338-365`); run-bucket displays it at `.github/actions/run-bucket/action.yml:515-528` and `:645`.

6. **Invocation attribution is unresolved.** Vitest merges every whole-file unit in a bucket into one invocation (`internal/runner/vitestrunner/render.go:37-42`, `:59-96`), while `TrainingLabel` and its receipt need one `UnitID` (`internal/walltime/palloc.go:131-160`, `internal/walltime/labelevidence.go:174-183`). R54 has exact membership, which is useful, but no defensible rule for turning the grouped wall duration into individual labels.

7. **The useful span is narrower than its “complete action” claim.** The monotonic open is the first explicit composite step at `.github/actions/run-bucket/action.yml:395-468`; setup, bucket execution, and close follow at `:479-513`, `:515-688`, and `:690-720`. This is a good run-bucket execution span. It cannot include GitHub expression/step-shell startup before `wall begin`, record/seal work after the end reading (`internal/walltime/action.go:897-920`), or the deliberately external installer (`.github/actions/run-bucket/action.yml:374-403`). The product schema should name the observed interval honestly instead of claiming literal composite-action wall time.

## What is worth keeping

### Monotonic timing and full lifecycle

- `SystemClock.Now` performs a fresh monotonic read (`internal/walltime/clock.go:59-84`), Linux uses `clock_gettime(CLOCK_MONOTONIC)` directly (`internal/walltime/clock_linux.go:8-25`), and `Nanos` avoids JSON's >2^53 precision loss (`internal/walltime/nanos.go:9-40`). Keep this.
- The process execution path starts a concrete argv, passes stdout/stderr through, waits for completion, preserves exit status, forwards cancellation, escalates TERM to KILL, and exercises cleanup/reap behavior (`internal/walltime/exec.go:352-640`; regressions at `internal/walltime/exec_test.go:159`, `:353`, `:507`, and `internal/walltime/cancel_test.go:50`, `:164`, `:217`, `:254`). Keep the behavior, not the cgroup/observer proof shell around it.
- A portable process group can bound the owned child tree and signal it as a group (`internal/walltime/contain_pgroup.go:11-19`, `:92-105`). It cannot prove that a hostile descendant did not call `setsid`; the practical product should state that limitation instead of carrying Linux cgroup eligibility machinery.

### Wall-time allocation seam

- `core.PlanOptions.AllocationScore` is a clean runner-neutral injection point (`internal/core/plan.go:86-125`, applied at `:189-203`). Keep it.
- `runner.Invocation.Units`, `Selector`, and `Atoms` preserve exactly what one invocation covers (`internal/runner/types.go:126-151`). Keep them.
- `PcheckFor`'s post-render use of the renderer's exact membership is a useful idea (`internal/planbind/features.go:165-194`), although the signed Pcheck document should go. A lightweight consistency check can ensure the additive score projected over invocations equals the bucket score.
- Deterministic model scoring, a nonnegative/floor policy, and bucket-total error tests are useful fragments of Palloc/gates (`internal/walltime/palloc.go`, `internal/walltime/gates_test.go:39`, `:323`). Replace sealed provenance with ordinary versioned history.

### Exact paths, coverage, and consumer safety

- Vitest renders actual argv, path tokens such as `./x`, reporter flags, selector, units, and atoms (`internal/runner/vitestrunner/render.go:49-105`, `:116-179`, `:214-261`). Keep those identities while removing Stage/proof coupling.
- Executable resolution and subprocess provenance describe what actually ran (`internal/runner/vitestrunner/exec.go:39-69`, `:85-155`; regression at `toolpath_test.go:18`). Keep argv/path/cwd; narrow full environment capture to fields that affect execution/model comparability.
- The per-bucket audit parses the shard plan once, resolves the exact named bucket, and derives expected coverage (`internal/core/audit_bucket.go:24-106`). The verifier bridge audits event files against that same parsed plan (`cmd/testbucket/wall.go:1113-1199`). Keep a small version.
- Go neutrality is directly tested: K=6, `-race -count=100`, unchanged Go output, Go event/audit compatibility, Vitest opt-in, and numeric one-decimal matrix semantics (`internal/core/consumer_contract_test.go:24-184`). Keep these tests.
- Third-party GitHub actions are pinned to full SHAs in CI and workflows (`.github/workflows/ci.yml:27-118`; `cmd/testbucket/actionpins_test.go:28-101`). Keep this independent supply-chain safety.
- The installer now enumerates all archive members without a short-circuiting `tar | grep -q` under `pipefail` (`cmd/testbucket/archiveenumeration_test.go:140-245`). Keep that narrow hardening while removing candidate/release authority.
- `ParseStore` gives a byte parser with the same semantics as file loading (`internal/core/store.go:142-161`). Keep it; it is useful for tests, cache inputs, and future schema migration even without frozen replay.

## Recommended practical contract

Define the measured product quantity as:

> `bucket_exec_elapsed_ns`: `CLOCK_MONOTONIC` elapsed from the first run-bucket-owned timing read before the consumer setup command through termination of the complete generated Vitest script, forwarding/cancellation, wait/reap of the owned process group, and final wrapper cleanup needed to know the process lifecycle is over.

It explicitly excludes queue time, checkout, plan and record jobs, artifact transfer, unrelated job setup, and the separate installer. Those are whole-job concerns; contract §3.2 keeps them outside `A`, and this audit records the reason.

Use one small atomic observation per bucket:

```json
{
  "schema": "testbucket.wall-observation/v1",
  "runner_token": "vitest|...",
  "run_id": "...",
  "attempt_id": "...",
  "bucket_index": 0,
  "bucket_name": "bucket-0",
  "plan_digest": "sha256:...",
  "unit_ids": ["src/a.spec.ts", "src/b.spec.ts"],
  "invocations": [{"units": ["src/a.spec.ts", "src/b.spec.ts"], "argv_digest": "sha256:..."}],
  "started_mono_ns": "...",
  "ended_mono_ns": "...",
  "elapsed_ns": "...",
  "terminal": "passed",
  "exit_code": 0,
  "failure_reason": ""
}
```

Write it atomically and upload it under `if: always()`. Retain failed/cancelled/malformed observations as diagnostics, but train only after all of these pass: schema, monotonic endpoints, terminal success, exact plan/bucket identity, invocation/unit membership, and existing runner-event coverage.

`# STATUS: SUPERSEDED BY S-3, S-5 AND S-6` — the observation sketch above predates the field
registry. The normative observation schema is §13, whose wire paths are declared in `scope.md` §2A
and checked in both directions by `TestFieldRegistryCoversEverySerializedPath`; it additionally
carries `candidate_sha` and `workload_commit` beside `head_sha` (S-6), the `cache_state` block
(S-5), and the diagnostics `actual_runner_name` / `observed_runs_on_label` (S-4), and it has **no**
loose top-level `runner_token`. The qualification list is `acceptance-contract.md` §7.1's, not the six checks named above.

# STATUS: SUPERSEDED BY PD-3 AND S-1 — HISTORICAL, AND THIS FILE STATES NO LIVE MODEL

> **SUPERSEDED by owner decision PD-3** (`acceptance-contract.md` §0.9) **and by specification
> repair S-1** (`scope.md` §7.2). The additive `per_invocation_overhead * invocation_count` model
> below is **not adopted**, and neither is its unit system.
>
> **The live model is `acceptance-contract.md` §0.9, and it is not reproduced anywhere in this
> file (R13-D4).** An earlier revision of this note wrote out a four-term equation, labelled it
> "the live model", and **omitted the `round_half_up`** the live equation requires — while a second
> note a few lines below wrote out the *corrected* equation and also called it the live model. This
> file therefore contradicted itself about the one expression the whole product optimizes and
> displays. Both copies are withdrawn. What survives here is only *why* the historical form was
> rejected: the additive form treated the whole-file invocation overhead as a constant every bucket
> pays, which is false for a slice-only bucket, so the displayed estimate and the optimized
> partition could disagree; and the mixed seconds/nanoseconds arithmetic the recommendation below
> implies is wrong by a factor of 1e9 (S-1). Retained below as the original recommendation only.

`# STATUS: SUPERSEDED BY PD-3 AND S-1/D-1` — the block below is the ORIGINAL recommendation, it is
**not** the live model, and this file does not say what the live model is. That statement has one
home: `acceptance-contract.md` §0.9, evaluated in the checked-integer domain of contract §1.1
(R10-D9, R13-D4).

A practical first model can learn complete action labels without pretending grouped invocation time belongs to one file:

```text
A_hat(bucket) = fixed_setup
              + scale * sum(legacy per-unit reporter EWMA)
              + per_invocation_overhead * invocation_count

# SUPERSEDED BY PD-3 AND S-1/D-1. HISTORICAL SKETCH ONLY.
# The live objective, its round_half_up, and the single display division are stated once, in
# acceptance-contract.md 0.9, and evaluated in the checked-integer domain of contract 1.1.
# R13-D4: this comment used to reproduce that equation. Two copies of one equation lived in this
# file and they did not agree; the copies are gone and the pointer replaces them.
```

Fit/calibrate `fixed_setup`, `scale`, and `per_invocation_overhead` from prior successful default-branch bucket observations. The additive allocation score for a unit is its scaled reporter EWMA plus its share of invocation overhead; the fixed term affects ETA but not partition choice because every nonempty bucket pays it. This immediately predicts the complete run-bucket span while retaining useful per-file signal. It also handles the current grouped whole-file invocation honestly. More expressive unit residuals can be added later only when the observation design provides enough identifiability.

# STATUS: SUPERSEDED BY PD-1

> **SUPERSEDED by owner decision PD-1** (`acceptance-contract.md` §0.7). The replacement approach
> below is **not adopted**. PD-1 freezes additive compatibility: every v0.2.2 legacy field name,
> value, ordering, and script byte is preserved, and `est_basis` / `A_eta` display metadata /
> `expanded_unit_set_digest` are **additive**. `est_seconds` is not replaced by `wall_est_seconds`;
> its meaning is declared by `est_basis`, and a canonical v0.2.2-field projection stays
> byte-identical for an unopted consumer. Retained below as the original recommendation only.

~~Expose the result as a new numeric `wall_est_seconds` field, leaving legacy one-decimal `est_seconds` intact until a deliberate versioned interface change. Use `wall_est_seconds`/its unrounded internal value for allocation and display both during migration. This preserves consumers while making the optimized quantity visible.~~

## Classification summary

| Classification | Changed files | Decision |
|---|---:|---|
| KEEP | 22 | Directly useful substrate or regression protection. |
| SIMPLIFY | 49 | Extract the practical kernel; remove proof dependencies. |
| REMOVE | 112 | Campaign/protected-authority/multi-signer/permanent-storage/triple-observer/cgroup/release-study only. |
| UNCERTAIN | 1 | `$/.github/actions` lint suppression; validate externally or replace. |

### Actions and support files

| Component | Class | Evidence | Dependency impact |
|---|---|---|---|
| `.github/actions/candidate-digest/action.yml` | REMOVE | authority purpose at `:9-11`; inputs at `:16-39` | Only authenticates prepublication campaign bytes; remove with resolver/Stage 1. |
| `.github/actions/install/action.yml` | SIMPLIFY | inputs at `:12-49` | Keep `version` and ordinary local/release install; remove candidate digest and later-commit pin input. |
| `.github/actions/plan/action.yml` | SIMPLIFY | normal inputs `:8-112`; frozen/authority inputs `:115-205` | Keep plan/store/Vitest/wall-model interface; delete bundle, stage, claim, authority, registry, scorer proof. |
| `.github/actions/preflight/action.yml` | REMOVE | purpose `:3-7`; authority inputs `:25-40` | Independent signed replay exists only for research eligibility. |
| `.github/actions/record/action.yml` | SIMPLIFY | audit and reporter ingest `:91-152` | Preserve audit-first behavior and make it ingest wall observations; delete candidate pin inputs. |
| `.github/actions/run-bucket/action.yml` | SIMPLIFY | open `:395-468`, setup `:479-513`, run `:515-688`, close `:690-720` | This becomes the small monotonic lifecycle wrapper; remove cgroup/users/keys/stages/scored/observers. |
| `.github/actions/verify-wall/action.yml` | SIMPLIFY | inputs `:9-181` | Keep observation and exact coverage validation; remove signed eligibility surface. |
| `.github/actions/install-testbucket.sh` | SIMPLIFY | permanent release pins `:379-455` | Keep safe extraction and ordinary checksum verification; remove campaign candidate and permanent external pin roots. |
| `.github/actions/candidate-resolver.sh` | REMOVE | campaign authority purpose `:6` | Candidate publication cycle only. |
| `.github/actions/released-binary-digests.tsv` | REMOVE | file purpose `:1` | Permanent release-proof store only. |

The complete action-input KEEP/REMOVE sets and proposed `wall-observations-dir` input are in `component-map.json` under `action_interfaces`.

### Workflows

| Workflow | Class | Evidence | Dependency impact |
|---|---|---|---|
| `.github/workflows/bucketed-reusable.yml` | SIMPLIFY | call interface `:57-425`; wall upload `:1087`; record path `:1107-1189` | Keep plan/test/default-branch record. Add wall artifact download/ingest; remove campaign, cgroup, Stage, signer, candidate, A_GH surfaces. |
| `.github/workflows/bucketed.yml` | SIMPLIFY | Go lane `:58-76`; Vitest lane `:86-99` | Preserve both consumer lanes and actually exercise Vitest wall learning; Go remains unwrapped. |
| `.github/workflows/ci.yml` | KEEP | build/test `:20-61`, Vitest integration `:67-93`, release dry-run `:95-121` | Useful ordinary CI with full-SHA third-party pins. |
| `.github/workflows/pin-release.yml` | REMOVE | pin store update `:61-90` | Permanent release-proof maintenance only. |
| `.github/workflows/release.yml` | REMOVE | R54 adds actions-read/build-attestation/campaign gates around `:218-848` | Revert R54's proof rewrite and retain the simpler baseline release workflow. |

The reusable workflow interface is 39 inputs and three secrets; its `plan` job exposes two outputs. Keep the ordinary runner/plan/toolchain/working-directory/setup/wall-dir/runs-on inputs and the plan job's matrix output. Remove candidate delivery, cgroup/users, campaign/stage/registry/verifier/authority/key/signer/frozen-artifact inputs, all three new secrets, and the plan job's candidate digest output. The exact names are in `component-map.json.workflow_interface`.

### Changed Go packages

| Package | Production-file inventory | Class | Dependency impact |
|---|---|---|---|
| `cmd/testbucket` | `main.go`, `wall.go`, `wallplan.go`; proof-only `checkdelegation.go`, `stage1binary.go`, `wallattestrunner.go`, `wallhold.go`, `wallstage1.go` | SIMPLIFY / REMOVE proof-only files | Retain compact measure/verify/ingest and acquisition identity; delete campaign command graph. |
| `internal/core` | `audit_bucket.go`, `plan.go`, `store.go` | KEEP | Runner-neutral allocator injection, plan/matrix schema, exact bucket audit, byte store parsing. |
| `internal/planbind` | `features.go`, `planbind.go` | SIMPLIFY | Keep allocation/membership bridge; replace frozen Stage/scorer contract with ordinary versioned wall history. |
| `internal/runner` | `types.go` | KEEP | `Invocation.Units/Selector/Atoms` are exact attribution and audit data. |
| `internal/runner/gorunner` | `render.go` | KEEP | Data-only invocation identity; rendered Go bytes stay unchanged. |
| `internal/runner/vitestrunner` | `discover.go`, `exec.go`, `render.go`, `vitestrunner.go` | SIMPLIFY; KEEP the exact-executable regression | Preserve actual argv/path/cwd, exact path tokens, membership, event reporter, deadlines, and wrapper hook while dropping broad frozen/proof state. |
| `internal/walltime` | 58 new production files | KEEP 11 primitive files; SIMPLIFY 11 mixed files; REMOVE 36 proof-only files | Extract a small timing/lifecycle/model package and remove most of the research protocol. Exact file membership is below and in the JSON map. |

`internal/walltime` production disposition:

- **KEEP:** `audit.go`, `canon.go`, `clock.go`, `clock_fallback.go`, `clock_linux.go`, `clock_other.go`, `nanos.go`, `procsignal_linux.go`, `procsignal_other.go`, `spec.go`.
- **SIMPLIFY:** `action.go`, `aeta.go`, `contain.go`, `contain_other.go`, `contain_pgroup.go`, `doc.go`, `exec.go`, `gates.go`, `palloc.go`, `record.go`, `spawn_other.go`, `verify.go`.
- **REMOVE:** `ablation.go`, `ablationderived.go`, `actionhold.go`, `agh.go`, `attest.go`, `campaign.go`, `cgroupdelegation.go`, `contain_linux.go`, `controller.go`, `delegation.go`, `detach_unix.go`, `dirdigest.go`, `hierarchy.go`, `identity.go`, `idsampler.go`, `labelevidence.go`, `lockclosure.go`, `membership_linux.go`, `membership_other.go`, `observe.go`, `peercred_linux.go`, `peercred_other.go`, `planidentity.go`, `proctree.go`, `procuid_linux.go`, `procuid_other.go`, `rawevidence.go`, `release.go`, `roster.go`, `runnerattest.go`, `schedule.go`, `selfcgroup_linux.go`, `selfcgroup_other.go`, `spawn_linux.go`, `stage.go`.

### Configuration and fixtures

| Files | Class | Evidence and impact |
|---|---|---|
| `.github/actionlint.yaml` | UNCERTAIN | Suppresses errors for `$/.github/actions` (`:1-24`). Kept where external consumer execution proves the syntax; otherwise remove with the syntax. |
| `README.md` | SIMPLIFY | Keep practical timing/compatibility documentation; remove the research protocol beginning around `:320`. |
| `.goreleaser.yaml`, `testdata/goreleaser/{README.md,artifacts.json}` | REMOVE | R54 release-attestation fixture/change only; restore baseline release config. |
| `testdata/mandel-lock/{README.md,pnpm-lock.yaml}` | REMOVE | Historical sealed Mandel source-profile proof only. |
| `testdata/vitest-sample/{package.json,package-lock.json}` | REMOVE R54 change | The 4.1.10 downgrade/freeze serves the historical campaign (`acceptance-contract.md:11`, `:27`); restore normal fixture maintenance/baseline 4.1.11. |

## Schema inventory

Every exported changed/new schema is classified here; exact source arrays and intended replacements are in `component-map.json.schemas`.

| Schema family | Types | Class | Dependency impact |
|---|---|---|---|
| Plan/matrix (`internal/core/plan.go:14-126`) | `PlanUnit`, `PlanBucket`, `PlanSummary`, `PlanDocument`, `PlanOptions` | KEEP | Keep `Run` and `AllocationScore`. `# STATUS: SUPERSEDED BY PD-1` — the advice to add `wall_est_seconds` instead of changing legacy `est_seconds` is superseded: `est_seconds` is not replaced, `est_basis` declares its meaning, new fields are additive. |
| Runner invocation (`internal/runner/types.go:126`) | `Invocation` with new `Units`, `Selector`, `Atoms` | KEEP | Exact scheduling/selection identity shared by Go and Vitest. |
| Vitest acquisition (`exec.go:60`, `discover.go:243`, `vitestrunner.go:95-178`) | `ExecProvenance`, `DiscoveryProvenance`, `ResolvedProgram`, `Options`, `FrozenInputs` | SIMPLIFY | Keep actual argv/path/cwd and wall hook; remove Stage replay and broad env closure. |
| Plan binding (`internal/planbind/features.go:32-117`, `planbind.go:44-507`) | `FeatureBuilder`, `Allocator`, `AcquireOptions`, `Result`, `PlanOptions`, `SemanticPlan`, `SemanticBucket`, `SemanticUnit` | SIMPLIFY | Retain allocation bridge/semantic membership; replace frozen acquisition protocol. |
| Measurement primitives (`clock.go:33-64`, `nanos.go:16`, `spec.go:18-79`, `audit.go:13-46`) | `Instant`, `Clock`, `SystemClock`, `Nanos`, `InvocationSpec`, `InvocationIdentity`, `InvocationManifest`, `AuditEvidence`, `AuditFunc`, `Digest` | KEEP | Foundation for precise, exact, auditable observations. |
| Lifecycle ledger (`action.go:20`, `exec.go:82`, `record.go:21-451`, `verify.go:31-181`) | `ActionState`, `RunInActionOptions`, `ExecOptions`, `Level`, `Role`, `Producer`, `RunIdentity`, `ContainmentIdentity`, `ProcIdentity`, `Record`, `SpecIdentity`, `Writer`, `Finding`, `Phase`, `Interval`, `Envelope`, `Verdict`, `VerifyOptions` | SIMPLIFY | Collapse into one small bucket observation and optional invocation rows; preserve terminal/exit/lifecycle semantics. |
| Cgroup containment (`contain.go:98-267`) | `WorkloadCredential`, `MembershipFacts`, `RawEvent`, `Containment` | REMOVE | Evidence/control schema for cgroup and privilege claims; use an internal process-group runner instead. |
| Predictor/ETA (`palloc.go:62-716`, `aeta.go:24-189`, `gates.go:89-358`) | `Feature`, `FeatureVector`, `TrainingLabel`, `TrainingLineageID`, `TrainingReceiptSet`, `Scorer`, `PcheckInvocation`, `PcheckDocument`, `ComponentClass`, `Component`, `AetaRegistry`, `AetaInputs`, `InstantiatedComponent`, `AetaInstance`, `GateResult`, `PredictorSample`, `AetaSample` | SIMPLIFY | Replace sealed lineage/registry with online wall-history/model schema; retain deterministic scoring and quality metrics. |
| Campaign gates (`gates.go:104`, `:358`, `:424`) | `Reconciliation`, `CampaignRun`, `CampaignPair` | REMOVE | Five-pair/triple-observer research arithmetic only. |
| Stage/authority/replay (`stage.go:47-2257`, `attest.go:28`, `runnerattest.go:33`, `lockclosure.go:55`) | `AlgorithmIdentity`, `ToolIdentity`, `RawSnapshot`, `RunnableSnapshot`, `ClockPolicy`, `ParserIdentity`, `PlanningInputBundle`, `Signature`, `Stage1Manifest`, `ActionIdentity`, `SourceProfileReceipt`, `StoreFacts`, `StoreReceipt`, `InstrumentationIdentity`, `InputAccess`, `Stage2Receipt`, `Stage1Approval`, `ReplayAttestation`, `PlannerClaimReceipt`, `BuildAttestation`, `RunnerAttestation`, `LockedPackage` | REMOVE | Protected authority, exact implementation proof, fleet/build attestation, replay, and permanent planner claims only. |
| Multi-signer/observer (`delegation.go:47`, `roster.go:39`, `observe.go:66`, `controller.go:40`) | `SignerDelegation`, `DelegationScope`, `RosterEntry`, `Roster`, `StreamDigest`, `Seal`, `KeyLogEntry`, `ObserverConfig`, `InvocationController` | REMOVE | Per-producer keys, peer/trace processes, sealed streams, and nesting authority only. |
| Training evidence (`labelevidence.go:30-125`) | `PhysicalVReceipt`, `SelectedWorkDocument`, `TopologyValidationReceipt`, `LabelEvidence` | REMOVE | No normal producer; exists solely to prove sealed offline label causality/topology. |
| Campaign/ablation/schedule (`ablation.go:45`, `ablationderived.go:18`, `agh.go:22`, `campaign.go:19-108`, `schedule.go:13-37`) | `CampaignAblationRef`, `AblationDerived`, `StepAttempt`, `StepAttemptDocument`, `CampaignArm`, `CampaignPairRef`, `CampaignIndex`, `CampaignRelease`, `CampaignLoader`, `FileCampaignLoader`, `ScheduledPair`, `CampaignSchedule` | REMOVE | Frozen research population and API observer only. |
| Release manifest (`release.go:33-69`) | `ReleaseManifest`, `ReleaseAsset`, `ReleaseAssetMember`, `GoreleaserArtifact` | REMOVE | Candidate publication proof, unrelated to timing prediction. |

## Test inventory

There are 84 changed test files: six KEEP, 19 SIMPLIFY, and 59 REMOVE. “SIMPLIFY” means retain the named practical assertion against the new small API, not preserve the current proof fixture.

### KEEP (6)

- `cmd/testbucket/actionpins_test.go:28` — immutable third-party action references.
- `cmd/testbucket/auditplan_test.go:19` — exact plan digest, substitution detection, and one-read audit (`:63`, `:146`).
- `internal/core/consumer_contract_test.go:24` — Go neutrality, audit/events, Vitest opt-in, and matrix compatibility (`:61`, `:91`, `:152`).
- `internal/runner/vitestrunner/toolpath_test.go:18` — retained resolved executable equals the one run.
- `internal/walltime/cancel_test.go:50` — escalation, deadline, and descendant cleanup (`:164`, `:217`, `:254`).
- `internal/walltime/canon_test.go:12` — canonical JSON/digest and exact nanosecond round-trip (`:58`, `:79`, `:93`).

### SIMPLIFY (19)

- `cmd/testbucket/acquisitionprovenance_test.go:26` — retain canonical root/actual argv/resolved path cases (`:108`, `:131`, `:148`); drop full frozen environment proof.
- `cmd/testbucket/actionsexpr_test.go:250` — retain only tests for expressions remaining in the simplified workflow.
- `cmd/testbucket/archiveenumeration_test.go:158` — retain exhaustive safe-member check and no-short-circuit regression (`:222`).
- `cmd/testbucket/candidateinstall_test.go:115` — keep safe pinned-member mechanics; delete candidate authority E2E at `:281`.
- `cmd/testbucket/main_test.go:13` — keep adapter count, Vitest-only measurement, discovery timeout/argv behavior (`:49`, `:65`, `:88`).
- `cmd/testbucket/planningsnapshot_test.go:19` — narrow to actual acquisition identity; remove sealed package closure at `:89`.
- `cmd/testbucket/workflowpermissions_test.go:100` — retain least-privilege checks for the smaller workflow; remove A_GH/candidate permission expectations.
- `internal/planbind/features_test.go:34` — retain allocation-score separation/determinism; replace frozen-provenance feature assertions.
- `internal/planbind/planbind_test.go:110` — retain stable semantic membership and exact invocation checks; remove Stage replay/one-shot proof.
- `internal/runner/vitestrunner/vitestrunner_test.go:31` — retain parser, exact rendering, events, seriality, and validation regressions; remove FrozenInputs-only cases.
- `internal/walltime/actione2e_test.go:25` — rewrite as a single complete observation lifecycle.
- `internal/walltime/actionspan_test.go:55` — keep span/owned-child/cleanup intent; remove multiple wrappers/ledgers.
- `internal/walltime/aeta_test.go:35` — replace registry proof with complete bucket ETA model tests.
- `internal/walltime/beginrollback_test.go:25` — retain cleanup of resources started before a begin failure.
- `internal/walltime/exec_test.go:23` — retain child failure, output pass-through, action span, full-call bracketing, bootstrap failure; delete three-ledger/signer/observer cases (`:159`, `:353`, `:507`).
- `internal/walltime/gates_test.go:13` — retain predictor/Aeta/bucket-total error helpers (`:39`, `:72`, `:323`); delete reconciliation and campaign decision tests.
- `internal/walltime/palloc_test.go:98` — retain deterministic scorer/floor/invalid-model behavior; replace sealed training admission.
- `internal/walltime/synth_test.go:1` — reduce to a small observation/verifier fixture.
- `internal/walltime/verify_test.go:795` — retain complete/missing/malformed/coverage/terminal checks; delete authority/stage/signer/observer eligibility matrix.

### REMOVE (59)

These are entirely coupled to historical protected-authority, candidate-release, permanent-claim, multi-signer, triple-observer, cgroup, fleet/source-profile, or campaign-proof requirements. The first test declaration is given as line evidence (or the file role where it is a helper).

- Candidate/release proof: `cmd/testbucket/aliasfreeze_test.go:26`, `candidatedelivery_test.go:111`, `candidateresolver_test.go:27`, `releasepin_test.go:51`; `internal/walltime/release_test.go:98`, `releasetrust_test.go:43`.
- Protected preflight/authority/replay: `cmd/testbucket/planauthority_test.go:22`, `preflight_test.go:19`, `preflightverifier_test.go:104`, `replay_attest_test.go:20`, `plannerclaimrollback_test.go:26`, `wallstage1_test.go:282`; `internal/planbind/planidentity_test.go:19`; `internal/walltime/replaybinding_test.go:18`, `stage2identity_test.go:21`, `stage2match_test.go:71`.
- Signers, keys, delegation, and signed identities: `cmd/testbucket/countersign_test.go:22`, `delegateexport_test.go:52`, `eligibleguard_test.go:30`, `runkey_test.go:25`, `signercontract_test.go:23`, `verdictidentity_test.go:23`, `verifierkeyguard_test.go:27`; `internal/walltime/delegation_test.go:20`, `identitygates_test.go:16`, `roles_test.go:16`, `secretenv_test.go:35`, `selfattest_test.go:18`, `signerauthority_test.go:19`, `signercanonical_test.go:26`, `streambinding_test.go:71`, `handoff_test.go:24`, `evidencedir_test.go:21`, `filechannel_test.go:28`.
- Cgroup/credential/triple-observer/process proof: `cmd/testbucket/cgrouplifecycle_test.go:37`, `nestedexec_test.go:85`, `main_test_dispatch_test.go:24`; `internal/walltime/cgroupdelegation_test.go:19`, `containmentidentity_test.go:20`, `controller_test.go:35`, `credentialboundary_test.go:24`, `hierarchy_test.go:18`, `observerabandon_test.go:80`, `observerexit_test.go:25`, `observerfail_test.go:25`, `proctree_test.go:20`, `producercontract_test.go:29`, `rawevidence_test.go:49`.
- Frozen source/profile/training/campaign research: `cmd/testbucket/closure_test.go:30`, `derivedproducer_test.go:24`; `internal/runner/vitestrunner/frozen_version_test.go:43`; `internal/walltime/ablation_test.go:19`, `campaign_test.go:436`, `labelevidence_test.go:19`, `lockclosure_test.go:23`, `mandel_lock_test.go:36`, `runnerattest_test.go:21`, `runnerimage_test.go:20`, `topologygrammar_test.go:15`.

## Code used only for historical claims

This is the explicit claim-to-code inventory requested. Mixed files are not called “only”; they are classified SIMPLIFY above.

### Protected-authority only

- Candidate/preflight/release actions and workflows: `.github/actions/candidate-digest/action.yml`, `.github/actions/candidate-resolver.sh`, `.github/actions/preflight/action.yml`, `.github/actions/released-binary-digests.tsv`, `.github/workflows/pin-release.yml`, and R54's proof additions to `.github/workflows/release.yml`.
- CLI: `cmd/testbucket/stage1binary.go`, `wallstage1.go`, `wallattestrunner.go`, plus authority/preflight/replay/verdict/key/candidate/release tests listed above.
- Package: `internal/walltime/attest.go`, `delegation.go`, `dirdigest.go`, `identity.go`, `release.go`, `runnerattest.go`, `stage.go`, and their authority/identity/replay tests.
- Evidence: contract `:29-35`; handoff says the protected environment it presumed did not exist at `source-handoff.md:62-64`.

### Multi-signer only

- `internal/walltime/delegation.go`, `identity.go`, `roster.go`, signed portions of `record.go`/`verify.go`, and signer/delegation/roster/file-channel tests.
- CLI/action inputs `run-key`, `record-signer`, `verifier-key`, authority keys, and signer-delegate plumbing in `run-bucket`/`verify-wall`/reusable workflow.
- Evidence: per-producer key intent at `internal/walltime/identity.go:53-61`; roster schemas at `internal/walltime/roster.go:39-148`; the historical handoff itself says key disjointness was still not enforced at `:74-75`.

### Permanent-storage only

- `PlannerClaimReceipt` and one-shot claim handling in `internal/walltime/stage.go:2257`, `cmd/testbucket/wallplan.go`, and `plannerclaimrollback_test.go`/`wallstage1_test.go`.
- Permanent release pins in `.github/actions/released-binary-digests.tsv`, installer `:379-455`, and `pin-release.yml`.
- Raw evidence/campaign archive expectations in campaign/stage/replay schemas and workflows.
- Evidence: the handoff says no immutable raw archive existed (`source-handoff.md:89-90`). A normal timing cache/artifact with retention and schema migration is sufficient for product learning; it need not prove permanence.

### Triple-observer only

- `internal/walltime/observe.go`, `detach_unix.go`, `rawevidence.go`, observer portions of `action.go`/`exec.go`/`record.go`/`verify.go`, and observer/producer/reconciliation tests.
- CPA/CPB/CPV and VTA/VTB/VT logic, independent raw event IDs, stream rosters/seals, and `A_GH` API observer.
- Evidence: historical contract `:25`, `:39-43`, `:49-71`; `BeginAction` starts two observer processes at `internal/walltime/action.go:151-158` and `:328-333`.

### Cgroup only

- `cmd/testbucket/checkdelegation.go`, `wallhold.go`, cgroup lifecycle/nested execution tests.
- `internal/walltime/actionhold.go`, `cgroupdelegation.go`, `contain_linux.go`, `controller.go`, `hierarchy.go`, `idsampler.go`, `membership_*`, `peercred_*`, `proctree.go`, `procuid_*`, `selfcgroup_*`, `spawn_linux.go`, and corresponding tests.
- Workflow inputs and provisioning for `cgroup-root`, `workload-user`, `script-user`, and `workload-uid` (`.github/actions/run-bucket/action.yml:94-300`; `.github/workflows/bucketed-reusable.yml:213-267`).
- Evidence: historical contract demands a scored cgroup-v2 primitive at `:47`; the practical product needs owned-process completion and honest limitations, not proof that no hostile descendant escaped.

### Research-proof only

- `internal/walltime/ablation.go`, `ablationderived.go`, `agh.go`, `campaign.go`, `labelevidence.go`, `lockclosure.go`, `planidentity.go`, `schedule.go`, most of `stage.go`, `runnerattest.go`, release manifest/attestation code, sealed Mandel/goreleaser fixtures, and all their tests.
- Frozen Palloc training lineage, Pcheck signed documents, Aeta registry completeness, five-pair decision gates, 12 ablations/four strata, 80 physical rows, random schedule, ITT/void rules, fleet/source-profile/package-closure attestations, and publication gating.
- Evidence: contract `:73-94`, `:96-125`; handoff confirms all population counters were zero at `:39-51` and no runnable campaign harness existed at `:59-60`.

## Dependency-aware salvage order

1. Freeze the compatibility floor: keep `runner.Invocation` identity, exact Vitest path/argv behavior, core allocation seam, one-read audit, `ParseStore`, action SHA pins, safe archive enumeration, and consumer-contract tests.
2. Extract `internal/walltime` to a small monotonic begin/run/end implementation. Start before setup, execute the generated bucket script under a process group, forward TERM/INT, escalate on deadline, wait/reap, then take the end reading and atomically write the observation.
3. Wire wall observations into the reusable workflow's record job. Download both event and wall artifacts; verify exact plan/bucket/membership/coverage; learn only successful complete rows.
4. `# STATUS: SUPERSEDED BY PD-1, PD-3 AND S-1/S-6` — Add a versioned wall-history/model schema. ~~Initially calibrate complete bucket `A` from reporter work plus fixed and per-invocation overhead. Supply the additive unit score through `AllocationScore` and add `wall_est_seconds` to plan/matrix/log output.~~ Superseded: the model is the four-parameter PD-3 form with an `I(any_whole_file)` indicator, not a per-invocation overhead; every quantity is **integer nanoseconds** with `scale` dimensionless and one division of the complete `A_eta_ns` at display (S-1); the ring row carries three separate identities plus `run_started_at` and `ingest_seq`, with recency and eviction defined before any solver sort (S-6, §15.1a–contract §15.1b); there is no per-unit wall score; and `wall_est_seconds` is an **additive** shadow field, never a replacement for `est_seconds`.
5. Port the 25 KEEP/SIMPLIFY practical test files before deleting proof packages. This protects lifecycle, exact paths, coverage, Go neutrality, and consumer schema while imports are cut.
6. Remove Stage, campaign, authority, signer, observer, cgroup, release-proof, frozen-study fixtures, and their 59 tests. Then reduce action/workflow inputs and permissions.
7. Exercise the reusable workflow from a real external consumer. Resolve `$/.github/actions/...` before relying on it; the lint suppression is the sole UNCERTAIN component.

Dependency consequences of doing this out of order are material: deleting `stage.go` first breaks most of the package because Palloc/Aeta/record/verify share `Digest`, `Signature`, and identity types; extract `Digest`/canonical JSON and the practical observation structs first. Deleting cgroup containment before extracting the process-group run/wait/signal path loses cancellation behavior. Changing `est_seconds` in place before adding consumer tests risks an unannounced interface break. Removing reporter ingestion before the wall loop is operational creates a cold-start-only product.

## Residual uncertainty

- `$/.github/actions/{plan,run-bucket,record}` is guarded by `.github/actionlint.yaml`, but the historical evidence does not demonstrate an external consumer invocation. This is a validation task, not a reason to preserve the research stack.
- A process group cannot prove containment against deliberately escaping descendants. The practical contract should promise completion of the owned command/process group and truthful failure/cancellation handling, not cgroup-grade hostile containment.
- Bucket-level wall labels alone do not identify arbitrary per-file latent costs (contract §0.9). `# STATUS: SUPERSEDED BY PD-3 AND S-1` — ~~The proposed calibrated additive model is intentionally modest and uses existing per-file reporter EWMAs as the allocation basis while learning the complete-action scale/overheads.~~ **Superseded twice over (D-13):** the live model is PD-3's **four-parameter nonlinear** objective, not a "calibrated additive model"; it is fitted in **integer nanoseconds** against `A`, the **instrumented run-bucket interval**, which the contract forbids calling a "complete action" (§9 non-claims, `scope.md` §4.2). The surviving true statement is the modest one: per-file reporter EWMAs remain the allocation basis, and the model learns only a global scale and two bucket-shape overheads. More granular wall attribution requires either controlled invocation topology or enough varied bucket observations; contract §0.9 does not invent it by dividing a grouped duration blindly.

## Verification notes

- Repository files were not edited.
- `component-map.json` parses as valid JSON.
- Its component inventory contains 184 paths and 184 unique paths.
- A sorted comparison against `jj diff --summary` at the exact baseline/target produced no missing or extra path.
- Classification totals are KEEP 22, SIMPLIFY 49, REMOVE 112, UNCERTAIN 1.
- `go test ./...` completed successfully: 1,419 tests passed in seven packages.

SALVAGE_READY
