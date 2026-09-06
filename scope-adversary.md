# Practical wall-time scope — fresh adversarial review

Date: 2026-09-07  
Reviewed working-copy snapshot before this report overwrite: @
(e2640236f53ef29e2e226e63f4de76e5b74c18f4, change osorptyz)  
R54 source revision: @- (8d27e9bccc537032615578748c80bed9300ec7bd)  
Baseline: v0.2.2 (693a19981fb6e0061d3fab62e59d75dc1c01ff3f)

## Overall verdict

**NEEDS_SCOPE_REVISION**

The narrow product remains practical: measure the instrumented run-bucket interval, learn one
bucket-level model, and change only Vitest allocation weight. The timer, exact consumer membership,
K=8/count=1/serial admission, Go isolation, and removal of the research proof stack all survive
adversarial review.

The current PWT-13 documents do not yet define a coherent implementation, however. The blocking
defects are:

1. the fit uses a seconds-valued reporter predictor and a nanoseconds-valued response, but the
   planner combines the fitted scale as though it were dimensionless;
2. acceptance-contract.md §0.4 says the model coefficients are fitted from the campaign C rows,
   contradicting the pre-campaign cutoff, frozen model digest, and held-out validation;
3. the 85-row canonical table still neither contains every wire field nor generates the schemas it
   says derive from it;
4. the comparability key includes a plan-runner instance name, has no executable producer for its
   runs-on label, and does not bind the dependency/runtime cache state that can change the measured
   process lifecycle;
5. history recency and eviction are undefined, while one head_sha is asked to represent three
   distinct identities;
6. the four-layout calibration search cannot prove INFEASIBLE, and the proposed one-observation
   round-trip test omits the corpus required by MIN_ROWS, MIN_RUNS, and rank 4;
7. the pilot arithmetic, threshold wording, and start-only order check do not support their stated
   campaign claims; and
8. the implementation-delta register is internally corrupted: it says three and thirteen while
   containing four detailed gaps and fourteen IDs, and several IDs point to tests of unrelated
   behavior.

No execution-test result is claimed. The review was source-, document-, ledger-, history-, and
fixture-based. The sole write is this requested report.

## Review basis

**SCOPE_PASS** means the specified product behavior and its acceptance condition are coherent,
even when R54 has not implemented them.

**NEEDS_SCOPE_REVISION** means the target or its test is contradictory, incomplete, unsafe, or
not capable of proving what it claims.

**PRODUCT_DECISION** records a settled boundary for which the reviewed evidence supplies no
measurement reason to add machinery.

**EXTERNAL_BLOCKED** is reserved for evidence that requires real CI executions or consumer
adoption; missing implementation alone is not called an external block.

Fresh Jujutsu checks established:

- v0.2.2 to R54 is 184 paths: 162 added, 22 modified, +53,295/−972;
- R54's parent to R54 is three paths, +328/−5;
- component-map.json partitions all 184 paths exactly once: KEEP 22, SIMPLIFY 49, REMOVE 112,
  UNCERTAIN 1;
- all complete fixture digests and the lock-excerpt digest match SOURCE.md;
- the exact Mandel membership input contains 1,512 tracked tests, 48 Case-prefix tests, 75
  integration paths including eight permitted harness-unit tests, one region-router test, and
  1,396 selected bucket tests;
- the production suffix-collision relation yields 42 selected↔selected pairs and zero
  selected-filter→excluded-Case pairs at the pinned revision.

## Requested falsification matrix

| Question | Verdict | Adversarial result |
|---|---|---|
| Does the timer contain the full Vitest/process lifecycle? | **SCOPE_PASS** | R54 reads V-start before child construction/spawn and V-end after the root child completes and is reaped, same-PGID signaling/drain, observer/controller close, and containment teardown. For Mandel the owned child chain is pnpm → tsx → façade → preflight → pnpm exec vitest, so config/import/transform/setup/hooks/tests/reporters/shutdown/façade cleanup precede root exit and are inside V. The JSON spec write and wall-exec CLI startup are outside V but inside VB and A. A detached setsid/double-fork descendant is the stated limitation. |
| Is A a full action or job timer? | **SCOPE_PASS** | No, and the target now says so. A starts after wall-begin process startup and ends before the closing record write/seal/process exit. Checkout, install, runner boot, queueing, plan, record, uploads, and sibling jobs are outside. The supported name is instrumented run-bucket interval. |
| Are import, transform, and setup learnable? | **SCOPE_PASS** for aggregate prediction; **EXTERNAL_BLOCKED** for adequacy evidence | Their aggregate contribution reaches the A response. The four-parameter model can learn only a global reporter association and bucket-shape overheads. It cannot identify file-specific import graphs, transform volumes, setup dependencies, cache effects, or causal fractions. A real warm corpus and held-out C rows must show that this coarse model is adequate. |
| Do estimates mean what the UI claims? | **NEEDS_SCOPE_REVISION** | The intended labels are good: per-unit values remain reporter work, while wall-basis bucket/matrix/banner values are A-eta and the optimized objective. The current units make that value numerically incoherent, so the UI contract is not implementable as written. |
| Do B and C differ only by planner mode? | **NEEDS_SCOPE_REVISION** | They can differ only in declared treatment inputs after the schema is repaired. Separate hosted runners are not physically identical, and current cache state is unbound. State the claim as “sole intentional/configured difference,” bind or disable dependency/runtime caches, and keep latent runner variation as a limitation controlled by pairing/counterbalancing. |
| Is cache/store provenance sufficient? | **SCOPE_PASS** for the timing-store artifact; **NEEDS_SCOPE_REVISION** for history identity and runtime caches | The scored timing store is one artifact id plus SHA-256, rechecked after download, never restored by prefix, and never written by either arm. That is strong. The ring's recency/exclusion identities and the PNPM/Mongo/runtime-cache state are not closed. |
| Is cold start handled safely? | **SCOPE_PASS** for mode selection; **NEEDS_SCOPE_REVISION** for the warm-up proof | Default or explicit reporter can cold-plan only when unscored; explicit wall hard-errors without an OK model; scored admission vetoes every cold fallback; campaign is warm-only. The calibration route cannot prove its claimed INFEASIBLE outcome or guarantee a reusable key. |
| Are suffix-collision atoms safe? | **SCOPE_PASS** at the exact pin; **NEEDS_SCOPE_REVISION** for the exact-consumer regression | The actual broader suffix relation has 42 selected pairs and zero selected→Case crossings, and union-find atoms preserve transitive co-scheduling. The named fixture test checks only the two root-containment examples and zero under the simpler root test; it must exercise the production suffix algorithm over the full pinned set. |
| Is K=8/count=1/serial behavior bound? | **SCOPE_PASS** | AD-1…AD-7 require Vitest, K=8, count=1, file_parallelism=1, and exactly indices 0…7 before a scored matrix is emitted, with the canonical profile copied to observations. The exact Mandel façade independently renders --no-file-parallelism. |
| Is Go neutral? | **SCOPE_PASS** | baml-rest is isolated by action commit 551d49ce… plus a runtime v0.1.1 release-tag request. Its exact lane remains K=6, -race -count=100, and -p=1. The contract correctly claims current-pin isolation plus default execution neutrality, not an immutable binary pin and not “no core change.” |
| Does whole-job data leak into fitting or scoring? | **SCOPE_PASS** for J; **NEEDS_SCOPE_REVISION** for campaign leakage | J is neither recorded nor a regressor/response. A is the sole response; V, VB, wrapper/setup spans, and counts are diagnostic. But contract §0.4 newly says coefficients are fitted from actual C rows, which leaks evaluation outcomes into the model and contradicts the frozen cutoff. |
| Is Mandel dispatch unit-only? | **SCOPE_PASS** | The predicate is project-based: unit plus eight pure harness-unit paths are admitted; case-replset, the 48 Case paths, 67 other integration paths, and region-router are outside the bucket universe. The broader collision audit finds no selected filter that can pull a Case path across the boundary. |
| Are campaign denominators, dates, and thresholds coherent? | **SCOPE_PASS** for the main denominator/date formulas; **NEEDS_SCOPE_REVISION** for pilot, order, fitting, and threshold prose | The scored campaign is five pairs × two arms × eight buckets = 80 rows total, 40 per arm, on at least three authenticated UTC dates within 14 days. Attempted and scored populations are separated. The pilot, start-only order, C-row fitting sentence, and “none derived at run time” test remain contradictory. |
| Is removed proof machinery needed for the product claim? | **PRODUCT_DECISION** | No. The aggregate trusted-CI claim needs monotonic endpoints, boot identity, root wait/reap, same-PGID signal/escalation/drain, atomic observations, content digests, exact membership, and coverage. It does not need protected approvals, signer rosters, object lock, triple observers, hostile cgroups, or unrelated release/study infrastructure. |
| Has the exact consumer claim been demonstrated? | **EXTERNAL_BLOCKED** | Neither pinned consumer runs this wall path. There is no qualifying warm corpus, exact façade lifecycle integration result, five-pair campaign, or adoption revision. |

## S-1 — The model mixes seconds and nanoseconds

**NEEDS_SCOPE_REVISION**

Scope §7.4 freezes reporter_sum as the sum of UnitStat.Seconds and fits it against top-level
elapsed_ns. Therefore:

- fixed_ns, whole_invocation_overhead_ns, and per_slice_overhead_ns are nanoseconds;
- reporter_sum is seconds; and
- fitted scale has units nanoseconds per second.

Scope §9.2 then computes a seconds-valued cost by dividing the three named nanosecond coefficients
by 1e9 but leaving scale × sum(base seconds) unconverted. That term is nanoseconds while its
neighbors and final est_seconds are seconds. Stage-1 seed arithmetic has the same mismatch. The
worked K=2 example instead treats scale=1 as dimensionless.

This is not a wording issue: a literal implementation can be wrong by 1e9.

Closure: choose one unit system once. A minimal repair is to convert reporter_sum to integer
nanoseconds before fitting so scale is dimensionless, retain all response/intercept/overhead
coefficients in nanoseconds, and divide the complete A-eta expression by 1e9 only at the display
boundary. The model, ring schema, planner, worked example, and test fixtures must state that same
choice. Add a unit-sensitive test whose answer would differ by 1e9 under the present formula.

## S-2 — Campaign rows are simultaneously held out and used to fit

**NEEDS_SCOPE_REVISION**

Acceptance-contract.md §0.4 now says the model coefficients and formulas are evaluated “from the
actual C rows” and calls that fitting. Elsewhere, the same contract and scope require:

- fitted_at and store.updated_at strictly before the earliest authenticated campaign start;
- one frozen store and model-parameter digest shared by B and C;
- zero campaign rows in the training ring; and
- C's 40 rows used as held-out calibration validation.

Those requirements cannot all hold if the coefficients are fit from C.

The threshold wording also remains self-contradictory. The 5% and 10% coefficients and their
formulas can be frozen, but the numerical right-hand sides depend on mean(A) computed from the 40
observed C rows. Contract §10.4 and scope test 27 currently say no value is derived from campaign
data, which would reject their own relative gates.

Closure: fit and freeze all four coefficients only from qualifying pre-campaign ring rows. C rows
may compute residuals, MAE, worst error, and mean(A) for validation, never refit. Say “formula and
percentage are precommitted; numerical bound is evaluated from observed C rows.” Update the
threshold test to reject retuning, not legitimate formula evaluation.

## S-3 — The canonical table still cannot generate the claimed schemas

**NEEDS_SCOPE_REVISION**

The YAML block has 85 name rows, but its narrowed promise still says it governs every field name,
each field's one role, and the artifact types in which fields appear. It does not.

Concrete counterexamples:

- the observation example contains schema, est_seconds, unit_ids, invocations, seq, units,
  argv_digest, selector, atoms, started_mono_ns, ended_mono_ns, failure_reason, and limitations,
  none of which has a canonical row;
- the example has a loose top-level runner_token while the following prose says profile fields are
  no longer loose top-level keys;
- profile.store_sha256 exists in the canonical profile but the table gives only store_sha256;
- most table rows carry no artifact/path metadata even though the table says it governs artifact
  membership;
- derived history_ring_row selects every RESPONSE, ADMISSION, and DIAGNOSTIC row, which would add
  repository, job_id, bucket_name, all profile leaves, cwd/process fields, boot fields, realtime
  fields, component spans, and residuals; §8.1a's actual ring table contains only a subset;
- derived plan_observation similarly selects runtime terminal and exit fields for a plan;
- BC aliases such as bc_k and bc_store_sha256 do not name the serialized profile.k and
  store_sha256 paths used by §13.2; alias resolution is unspecified; and
- node_version, pnpm_version, and setup_command are semantically both comparability leaves and
  env_tuple leaves, contradicting “one field, one role” even though the tuple hides the duplicate.

Closure: model wire paths, primary semantic roles, and cross-cutting memberships as separate
dimensions. Every actual serialized path needs an artifact/type/cardinality declaration. Generated
lists must be compared byte-for-byte with the explicit observation, profile, ring, plan, matrix,
and manifest schemas. If the table is intended only as a role registry, remove the stronger
artifact/schema-generation claims.

## S-4 — The comparability key prevents or misstates warm reuse

**NEEDS_SCOPE_REVISION**

The key includes runner_name from the plan job. The exact consumer uses GitHub-hosted
ubuntu-latest jobs and provides no stable runner-name contract across runs. The eight measured
bucket jobs are different runners and their names are deliberately not key inputs. Consequently
the key describes a plan-job instance rather than the execution class whose A observations are
being fitted, and MIN_RUNS=3 under one key has no specified route to stability.

runner_image_label also lacks the promised executable producer. A composite action can read
runner.os and runner.arch, but the documents do not define an input or context that yields the
caller's resolved runs-on label. Saying “from the runs-on label resolved in the plan job” is not a
producer. The exact workflow contains a literal label, but the general action contract does not
pass it.

The trusted-CI decision correctly rejects a cryptographic image digest. That does not justify an
unstable instance identity or an imaginary label source.

Closure:

- remove runner_name from the comparability digest or replace it with an explicit stable runner
  class; retain actual runner names as diagnostics;
- pass the configured runs-on label explicitly from the orchestrator/workflow to plan and bucket
  actions, then compare it at observation admission;
- demonstrate three distinct real runs reusing the same key before claiming bounded warm-up; and
- keep mutable-image limitations explicit without adding fleet attestation.

## S-5 — Timing-store bytes are pinned; runtime cache state is not

**NEEDS_SCOPE_REVISION**

The timing-store design itself passes: artifact id and SHA-256 are frozen and rechecked, no scored
restore-key fallback is allowed, and neither arm writes it.

That does not make all measured inputs equal. The pinned Mandel workflow restores its PNPM store
with a prefix fallback. Its setup composite restores the MongoDB binary by an exact key, but the
campaign tuple records neither cache mode/hit nor the executed MongoDB binary digest. The external
orchestrator says to provision “the way Mandel does” without freezing whether the dependency cache
is disabled, exact-hit, fallback-hit, or cold. Vitest transform state and filesystem warmth are
also latent contributors inside the spawned lifecycle.

Cross-arm lockfile equality proves dependency intent, not runtime cache state. Two arms with the
same BC-INV bytes may therefore differ in a measured preflight/import/transform input.

Closure: for the scored campaign, either disable dependency and transform caches, or declare a
cache mode and record the primary key, matched key/hit disposition, and relevant executed binary
digest. Compare those fields per pair or mark a pair invalid. Ordinary page-cache noise can remain
a paired-run limitation; this does not require cgroups, protected environments, or immutable
storage.

The B/C claim should be “the sole intentional and configured difference is est_basis,” not literal
physical identity of separate hosted runners.

## S-6 — Ring recency and provenance identity are not reproducible

**NEEDS_SCOPE_REVISION**

The fit says “most recent W=240” and then lexically sorts by
(head_sha, run_id, run_attempt, bucket_index). That tuple gives deterministic solver order, not a
definition of chronological recency or eviction. The ring row has no authenticated run-start
instant, and ingestion arrival order is not frozen. Two ingest orders can therefore retain
different populations while both satisfy the text.

One head_sha is also asked to exclude the B candidate, C candidate, orchestration commit, and
campaign universe. Under the external orchestrator those are distinct identities:

- the workflow run has an orchestration repository/commit;
- Mandel is checked out at a workload commit; and
- testbucket is built from a candidate commit/binary.

The row stores none of the latter two. If head_sha means orchestration SHA, it cannot prove
candidate/workload exclusion. If it means candidate SHA, it no longer agrees with authenticated
workflow provenance. Moreover, comparability requires the same testbucket binary digest for warm
rows, so a blanket “zero rows at the candidate SHA” conflicts with warming the candidate if those
identities are conflated.

Closure: store the three identities separately, plus an authenticated observation/run time or a
canonical append sequence. Define selection of the newest W rows and eviction before defining the
solver sort. Exclude campaign run IDs and time window directly; do not overload head_sha.

## S-7 — Calibration has no truthful negative outcome

**NEEDS_SCOPE_REVISION**

Calibration tries at most four heuristic layouts and offers only SUFFICIENT or INFEASIBLE. Failure
of four layouts does not prove that the candidate universe cannot produce rank 4. A layout outside
the list may do so. The optional maximum N is also undefined for N greater than four, while N less
than four can stop before the layout claimed to establish independence.

The evidence document projects rows/runs, but wall admission requires at least 24 accepted
observations across at least three actual runs. A planner can prove predictor rank for proposed
topologies; it cannot manufacture accepted executions.

The new feedback-loop acceptance test has the same omission. One observation cannot by itself
satisfy MIN_ROWS=24, MIN_RUNS=3, and rank 4. A coefficient change also does not generally imply a
partition change without a deliberately chosen boundary fixture.

Closure:

- rename the bounded negative outcome NOT_FOUND_WITHIN_BUDGET, or provide a genuinely exhaustive
  structural infeasibility proof;
- define behavior for every legal N;
- separate topology-rank proposal from the three-run execution protocol that populates the ring;
  and
- seed the round-trip test with 23 qualifying rows over at least three runs and a specified
  boundary fixture, then add the 24th row and prove the chosen next partition changes. If testing
  an empty store, expect insufficient rather than a refit.

The ordinary cold-start rules remain valid independently of this repair.

## S-8 — Exact suffix safety is true but under-tested

**NEEDS_SCOPE_REVISION** for the acceptance test; **SCOPE_PASS** for the pinned data.

SOURCE.md correctly distinguishes two root-relative containment examples from the broader
production algorithm and notes the independently audited 42-pair superset. The product safety
claim depends on the broader shared-project-root suffix rule, not just the two examples.

TestPinnedConsumerEntrypointDigestsMatch currently promises the two named pairs and zero
root-boundary collisions. The retained generic exact_paths_test.go does not bind the full
d9ae1d43 path universe. A regression in project-root suffix enumeration could therefore pass both
tests while invalidating the workload-specific claim.

Closure: feed all pinned selected and Case paths through the actual assignFilterAtoms collision
implementation; assert 42 selected↔selected collision pairs, zero selected↔Case pairs, transitive
atom closure, and that no atom is split across invocation or bucket boundaries. The values already
pass in this snapshot; this is a missing regression, not a demand to change the algorithm.

## S-9 — Campaign core is sound, but its pilot and order claims are not

**NEEDS_SCOPE_REVISION**

The main accounting is coherent:

- five pairs;
- ten scored arm-runs;
- eight complete buckets per run;
- 80 scored rows total, 40 per arm;
- scored and attempted populations separated;
- at least three authenticated UTC dates; and
- earliest-to-latest scored start span no more than 14 days.

Four active defects remain.

First, the pilot says “1 pair × 8 runs × 2 modes = 16 rows.” The factor is eight buckets, not eight
runs. It then says that the 16-row pilot proves “eighty-row bookkeeping,” which it cannot.

Second, B→C is validated only by start(B) < start(C). The jobs may overlap. That is not the
sequential within-pair order used to justify drift counterbalancing. Either require authenticated
completion(first) ≤ authenticated start(second), or call it launch order and remove the sequential
drift-control claim.

Third, the C-row fitting and relative-threshold contradictions of S-2 directly affect campaign
validity.

Fourth, the claimed attribution to partition weight is too strong until S-5 binds cache mode.
Counterbalancing controls systematic first-arm order; it does not prove runtime state equality.

Closure: correct the pilot to one pair × two arms × eight buckets, restrict it to 16-row plumbing,
test 80-row bookkeeping synthetically or in the real campaign, define sequential versus launch
order, freeze the pre-campaign model, and bind the campaign cache policy. No inferential statistics
or larger campaign are required.

## S-10 — The delta and consistency ledger cannot be executed as written

**NEEDS_SCOPE_REVISION**

The current artifacts make mutually false inventory claims:

- contract §0.5, scope §21, DEC-24, and test 28 say there are three R54 gaps;
- §21 contains four detailed gap sections: estimate semantics, B/C isolation, timing feedback, and
  consumer adoption;
- §21.0 says “Thirteen deltas (ID-1…ID-13)” but contains ID-1 through ID-14;
- ID-14 appears before ID-13 and §21.4 before §21.3.

Several acceptance mappings do not test their row:

- ID-6's nonlinear objective points to profile-copy and key-reset tests;
- ID-7's cold-start error points to the allocation-objective test;
- ID-8's fixture points to the campaign-void test;
- ID-10's rank refusal points to the façade lifecycle test;
- ID-12's process-group semantics points to campaign accounting and response-selection tests; and
- ID-13's attempted/scored accounting points to the scale-wording test.

Automated-looking substitutions have also produced fragments such as “the named §16 tests–39,”
truncated quoted test names, and unrelated consumer tests in §23. Scope repeats a
“Deliberately outside the digest” heading; the contract repeats “Statistics.” Current report
line-count/hash claims in scope, contract, and ledger were stale even before this overwrite.

TestScopeArtifactConsistency is therefore specified as passing over documents that visibly fail
its advertised checks. No execution result exists to override that observation.

Closure: maintain one ordered machine-readable registry of gap ID, target, source status, and exact
test symbol; generate the prose tables from it; make “known gap count” derived; and remove
self-referential “current report” line counts/hashes. Restore every mangled reference to one real
test. The consistency test must fail on this snapshot and pass only after those repairs.

## R54 source adjudication

### Timer and lifecycle

**SCOPE_PASS**

The useful R54 kernel is real. internal/walltime/exec.go takes the opening monotonic reading before
spawn-related construction, waits for the root, runs containment/group cleanup, closes observers
and controller, destroys containment, and only then takes the closing reading. action.go persists
the A start in wall begin and reads the A end in a later wall end under one boot identity.

This supports V/VB/A at the documented boundaries. It does not support “complete action,” whole
wrapper-process lifetime, whole job, hostile detached-child containment, or per-file attribution.

### Product implementation

R54 does not implement the proposed product, but that fact is an implementation delta rather than
by itself a scope rejection:

1. core.BuildPlan can use AllocationScore to alter packing, while PlanBucket.Seconds, matrix
   est_seconds, and summaries remain sums of reporter weights.
2. R54 palloc is a sealed six-feature per-unit model trained on physical invocation V, not the
   proposed four-column bucket model trained on A.
3. the reusable workflow uploads wall artifacts, but its record job downloads timing events and
   the shard plan only; normal ingest folds RunSummary.PackageSeconds and never appends a wall ring.
4. there is no est_basis, canonical practical profile, warm/scored admission path, schema-2 ring,
   practical refit, or plain five-pair validator.
5. neither exact consumer selects the R54 wall path.

The repaired delta registry should express these as a small coherent dependency chain:
observation → record download → qualifying ring → pre-campaign refit → next-plan objective/display
→ scored admission/campaign validation → exact consumer adoption. Each test must target its own
hop.

## Ledger adjudication

**NEEDS_SCOPE_REVISION**

The ledger is valuable where it records uncertainty and source facts, but its terminal state is no
longer trustworthy.

- A-15 says an inadequate reporter regressor may drift to scale≈1 and still yield a “better ETA,”
  or wall refusal still ships a better estimate. Neither follows: scale≈1 is not evidence of
  accuracy, and a refused wall model leaves the legacy reporter estimate.
- A-20 says “scale is” the measurement of whether the reporter covers import/transform/setup. That
  contradicts the contract's correct statement that scale is only a predictive association and
  no causal/component fraction.
- A-42 records an old SCOPE_PASS as VERIFIED, while A-47 declares repairs verified that the active
  canonical table and register do not actually satisfy.
- the summary says only external adoption remains, omitting the current internal contradictions.

Closure: retain A-15/A-20 as empirical unknowns, remove the promised better-ETA branch, describe
scale only as association, and move prior review outcomes/repair claims to historical status unless
their current acceptance tests pass.

## Salvage adjudication

**SCOPE_PASS**

The salvage direction and dependency order are sound:

- keep clock/nanosecond/canonical-digest/audit/store/allocation primitives;
- simplify exec, action, record, palloc, the actions, and the reusable workflow;
- extract the four shared stage.go types before deleting the research graph;
- extract the process-group run/wait/signal/drain path before deleting cgroup implementation;
- close the observation→record→ring→refit path while reporter ingest is still present; and
- retain exact-path atoms, coverage, Go render/event behavior, cancellation, and installation
  hardening.

The old additive per-invocation recommendation in salvage-audit.md is visibly marked SUPERSEDED by
PD-3, so it is not treated as live guidance. The 22/49/112/1 component classification is complete.
What still needs revision is the active product specification and its tests, not the high-level
salvage choice.

Extracting shared types or group-lifecycle code does not justify preserving the proof semantics
that currently happen to surround them.

## Exact-consumer adjudication

### Mandel

**SCOPE_PASS** for identity, membership, and execution shape.

At d9ae1d43 the exact workflow pins testbucket v0.2.2, K=8, count=1, façade command
pnpm exec tsx scripts/tb-vitest.ts, and the Case exclusion prefix. The renderer supplies
--no-file-parallelism. The permitted project set is unit plus harness-unit; the Case project and
other integration/region-router paths do not enter the bucket universe.

The fixture is honestly labelled direct-entrypoint evidence rather than a transitive execution
closure. Campaign execution must recompute from a checkout at workload_commit, which is the right
way to bind the omitted transitive setup/preflight files.

Cache provenance and the production suffix regression remain S-5 and S-8.

### baml-rest

**SCOPE_PASS**

At ff3012b1 the consumer calls action source 551d49ce… and asks its installer for v0.1.1. Its
contract is K=6, race, count=100, serial -p=1, existing events/audit semantics, and no wall
invocations. Calling the binary pin immutable would be false; the current documents correctly do
not. A future repin owes the byte-level render/action-input compatibility test.

## Scope-creep adjudication

**PRODUCT_DECISION**

Reject protected environments, multi-principal signatures, countersignatures, key rosters,
object-lock/permanent evidence, triple observers, cgroup eligibility, fleet attestation, and
unrelated release/study infrastructure for this claim.

None of those mechanisms:

- extends A to whole-job time;
- turns grouped V into a file label;
- identifies import or transform cost;
- repairs model units;
- stabilizes the comparability key;
- binds cache state;
- fixes campaign accounting; or
- closes the ordinary record/refit loop.

Minimal provenance in trusted CI is adequate: exact content digests, authenticated run/job
identity and times, terminal coverage, monotonic/boot identity, plan membership, atomic records,
and retained artifacts.

Concrete re-entry conditions only:

- measured detached descendants survive root completion and materially bias V or A → evaluate a
  subreaper or cgroup containment;
- demonstrated artifact mutation or forged rows defeats the stated audit → evaluate stronger
  storage or signatures;
- a real hostile-runner requirement replaces the trusted-CI threat model → revisit principals and
  attestation.

Wanting a stronger certificate is not measurement necessity.

## External gates

**EXTERNAL_BLOCKED**

After S-1 through S-10 are repaired and the implementation tests pass, the product claim still
requires evidence that documents cannot manufacture:

1. a real three-run, at-least-24-row warm corpus under one stable comparability key, with rank 4
   and the degradation ceiling met;
2. an exact façade sentinel integration proving import/transform/setup, test, reporter shutdown,
   façade cleanup, root reap, and closing-read order;
3. the five matched pairs: ten scored runs, 80 rows total/40 per arm, at least three authenticated
   UTC dates within 14 days, with every frozen balance, tail, cost, calibration, provenance,
   cache-policy, and integrity gate applied; and
4. a recorded exact consumer adoption/orchestration revision.

Until then the accurate state is: specification needs revision, R54 supplies salvageable
measurement machinery but not the practical product, and no consumer-effect claim has been
measured.
