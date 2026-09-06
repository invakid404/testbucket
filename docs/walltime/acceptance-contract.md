# Acceptance contract — testbucket practical wall-time balancing (PWT-13)

**Frozen:** 2026-09-07 UTC

**No line count or SHA-256 of any document in this set is quoted in this contract (S-10).** Earlier
revisions carried them for the adjudication, the adversary report, and prior contract revisions;
every one went stale the moment its file was edited, and three were already stale when the current
review found them. Companion documents are referenced by **path and verdict**, and their bytes are
checked at read time by `TestScopeArtifactConsistency`, which also fails on any self-referential
count or digest reintroduced here.

**Adjudication (`scope-adjudication.md`):** `NEEDS_SCOPE_REVISION`, on the ground that the governing
artifacts were **absent from the tree** — they lived in `/tmp` and no `jj`-only reviewer could see
them. That is fixed. The package is this contract, `scope.md`, `assumption-ledger.md`, `salvage-map.md`,
`source-to-claim-map.md`, `salvage-audit.md`, and `component-map.json` are in the working copy at
`docs/walltime/`, and the pinned consumer fixture is at `testdata/consumers/`. Every adjudicator
finding is closed in actual files; `scope.md` §23 maps each to its file.

**Owner resolution (recovery_gate), recorded verbatim in force:** five determinations govern this
revision and override any earlier wording — threat model (§0.1), B/C execution order (§0.2),
campaign gate framing (§0.3), thresholds and error gates (§0.4), and implementation delta labels
(§0.5). They are owner decisions, not review findings, and are not reopened by a reviewer.

**Revision: eighth — the specification tail repair S-1…S-10.** The current `scope-adversary.md`
returns `NEEDS_SCOPE_REVISION` on ten specification defects. All ten are closed here and in
`scope.md`, mapped in `scope.md` §20. The four that change this contract's text:

| # | Change here |
|---|---|
| S-1 | §0.9, §5, and §6.4 restate the model in **integer nanoseconds** with `scale` dimensionless and the single `/1e9` at the display boundary |
| S-2 | §0.4 is corrected: coefficients are fitted **only** from pre-campaign rows; C rows compute validation metrics only; a precommitted formula evaluated against observed rows is not retuning |
| S-5 | §0.2 and §10 bind a declared, recorded, per-pair-compared `cache_state`, and state the B/C claim as *sole intentional and configured difference* rather than physical identity |
| S-9 | §0.2 and §10 require authenticated **sequential** within-pair order, `completed_at(first) ≤ started_at(second)`, and separate it from launch order |

S-3, S-4, S-6, S-7, S-8, and S-10 are closed by the field registry (`scope.md` §2A), the delta
registry (`scope.md` §21.0), this contract's §15.3, §15.1a/§15.1b, §17.3 and §20.6a, and
`testdata/consumers/SOURCE.md`; this contract's references follow them.

**Prior revisions** repaired PWT-6's findings, the fourth and fifth adversarial reports' 34 findings
and 9 closure items — including the **Ledger sufficiency** analysis (`scope.md` §6.3 here), the sharpened
`N3`/`N1` requiring this contract to *affirmatively allow* pre-treatment reporter data rather than
claim an outcome-free allocation surface (§2, §6.2), and baml-rest's isolation stated as *no
ordinary source path* rather than an "immutable pin" (§15.3 here, ledger A-06) — and the SR-1…SR-10
set.
None of that is reopened here.

**State:** NEEDS_EVIDENCE — specification only; no implementation and no campaign evidence exist.
**Threat model: non-hostile.** §12 states the concrete measurement that would reopen that choice.
**Historical:** EWJ-2R13 — design evidence only; recovered by digest, not by path (ledger A-01).
**VCS:** `jj` only. No Git CLI. No source, remote, or GitHub mutation performed.

---

## 0. Owner resolutions

These five are settled by the owner. Everything later in this contract conforms to them.

### 0.1 Threat model — trusted / non-hostile CI

The runner, the test process, and the artifact channel are trusted. Hostile-runner containment and
multi-signer proof machinery are **out of scope**, and remain so **unless a measured timing-bias
result from an actual hostile environment reopens them**. No protected environments, no cgroup
isolation, and no observer chains are required by this product. A hypothetical attacker, a
preference for more signatures, or an unmeasured concern does not reopen it.

### 0.2 B/C execution order — fixed, precommitted, counterbalanced

The order is a **fixed precommitted counterbalanced sequence**, declared before the first run. It
is **not randomized**, and no document, schema, log, or comment in this product claims
randomization, a seed-derived draw, or a reproducible shuffle.

The declared sequence for the five pairs is:

| Pair | Order |
|---:|---|
| 1 | B → C |
| 2 | C → B |
| 3 | B → C |
| 4 | C → B |
| 5 | B → C |

Counterbalancing alternates which arm runs first so that ordering and drift within a pair do not
sit systematically on one arm. It is a control, not a draw.

**Within a pair the two arms run sequentially, not merely in declared launch order (S-9).**
Counterbalancing controls within-pair drift only if the arms do not overlap. The campaign therefore
requires **authenticated `completed_at(first) ≤ started_at(second)`** in the declared direction, both
instants from the Actions API. `started_at(first) < started_at(second)` alone is **launch order**;
it is not sufficient and no document attributes drift control to it. §19.5 and §17.18
state it; `TestPairsRunSequentiallyByAuthenticatedCompletion` is the test.

**The sole intentional and configured difference between B and C is the planner mode.** Binary
bytes, action tree digests, store bytes, expanded unit set, and the declared `cache_state` of
§10.5.0 are identical and asserted. That is a claim about **intent and configuration**, not
about physical identity: two separate hosted runners are not the same machine, and page-cache
warmth, filesystem state, and runner-instance variation are unbound. They are controlled by pairing
and counterbalancing and named as a limitation, not asserted away (S-5).

### 0.3 Campaign gate framing — an engineering release gate

Five matched K=8 pairs, ten runs, eighty rows, across at least three UTC dates is an **engineering
release gate only**. It is **not a statistical significance test** and makes **no generalization
claim beyond the measured sample**. No document in this product states or implies significance,
power, a null hypothesis, a p-value, or an inferential conclusion. Results are reported as what was
measured on those eighty rows.

### 0.4 Thresholds and error gates — precommitted, never retuned

**Thresholds are precommitted before any run and cannot be retuned from pilot or campaign
outcomes.** Every value in §10.4 is fixed before the first run of the pilot or the campaign, and no
observed result may change one.

**Where the coefficients come from — corrected (S-2).** An earlier revision of this section said the
coefficients "are evaluated at run time from the actual C rows; that is fitting". That was wrong and
contradicted the pre-campaign cutoff, the frozen model digest, and the held-out validation this same
contract requires. The corrected rule:

| | Fitted from | Evaluated on | Frozen |
|---|---|---|---|
| the four coefficients `fixed_ns`, `scale`, `whole_invocation_overhead_ns`, `per_slice_overhead_ns` | **only** qualifying **pre-campaign** ring rows — those excluded from the campaign run set and window by §15.1b | — | before the earliest authenticated campaign start; the `model_parameters_digest` is identical in both arms |
| the campaign's C rows | **never** — a C row entering a fit fails the campaign | residuals, MAE, worst error, and `mean(A)` for the calibration gates | — |

**Fitting and threshold evaluation are different acts, and neither is retuning.** A threshold's
**formula and percentage are precommitted** in §10.4; its **numerical right-hand side is evaluated
from the observed C rows**, because that is what a relative gate is — `5.0 % × mean(A)` cannot be a
constant before `mean(A)` is observed. Evaluating a frozen formula against observed data is not
deriving a threshold from an outcome. What is forbidden, and what §10.4 and `scope.md` test 27
reject, is **changing** a percentage or bound after seeing a result, or **refitting** a coefficient
on campaign rows. The earlier wording — "none is derived at run time" — would have rejected the
relative gates this contract requires, and is replaced by the distinction above.

Calibration is gated on both an absolute and a relative bound so that "extremely close" is
scale-independent. Exact values are in §10.4; every one is fixed before run 1.

Acceptance tests: `TestCampaignRowsNeverRefitTheModel` (`scope.md` test 61) and
`TestThresholdsAreFrozenBeforeRunOne` (test 27).

### 0.5 Implementation delta labels

The properties listed in the **ordered machine-readable delta registry at `scope.md` §21.0** are
**known R54 implementation gaps**, not properties the current source has. Each is labelled as such
and carries an exact acceptance test the implementation node must pass before validation.

**No count of gaps appears in this contract (S-10).** The earlier revision said "three" here while
`scope.md` §21 carried four gap sections and fourteen delta IDs, and several IDs pointed at tests of
unrelated behaviour. The `scope.md` §21.0 registry is now the single ordered machine source; counts are
**derived** from it.

**The delta projection contract — one account, stated here and referenced everywhere (R17-F2).**
An earlier revision gave three incompatible accounts: this section said only ID set/order,
acceptance-test symbols and gap resolution are compared; §22 said every delta field and every gap
`Target` is compared; and `component-map.json` repeated the narrower one. There is one account, and
it is this:

| Surface | Compared how |
|---|---|
| `ID-n` set and order | **exact**, against the registry |
| the prose register table | **byte-exact on all four columns** — `id`, `title`, `r54_status`, and the acceptance-test symbols, in registry order |
| each gap body's `Target` row | **string-equal** to the concatenated `target` values of the registry rows whose `gap_section` is that body — no appended prose, no substring match |
| each gap body's acceptance-test set | **set-equal** to the union of `acceptance_tests` over those same rows |
| `gap_section` | every value resolves to a real section, and every gap body is referenced by at least one row |
| `dependency` | resolves, no self-edge, and the graph is **acyclic** — proved by traversal, not assumed |
| `overlay_files` | non-empty, **disjoint across rows** so per-row R54 attribution is real, and every path is a `_test.go` path |
| `required_symbols` | non-empty, shaped `package.Symbol`, and **disjoint across rows** for the same reason |

**The delta registry's key set is closed, and every key has a declared grammar (R20-F2).** A row of
`scope.md` §21.0 carries these keys and no others; a key outside this table fails the authority test,
and so does a value outside its grammar. This is the delta registry's equivalent of the field
registry's no-prose rule: the delta domain is prose-bearing — a gap has to be named — so the
guarantee is a closed schema rather than an absence of words.

| Key | Grammar |
|---|---|
| `id` | `ID-` followed by the row's ordinal; the rows are ordered by it |
| `title` | the delta's name. Prose **within the delta domain**: it names what is missing, and states no rule, threshold, equation or value |
| `target` | one or more `contract §N` or `field-registry <member>` pointers, separated by `,` or `;`, and nothing else. A section number is `N`, `N.N`, `N.N.N`, any of those with a trailing letter, and any of those with a trailing `-<digit>` — the four shapes this contract's own headings use, so `§19.9c-1` is a pointer and not prose (R24-F3) |
| `r54_status` | what the R54 tree lacks or does today. Prose **within the delta domain**, and historical by construction |
| `acceptance_tests` | Go test symbols, `Test…`-shaped |
| `overlay_files` | `_test.go` paths |
| `required_symbols` | dotted `package.Symbol` paths |
| `gap_section` | a `21.x` section number, or null |
| `dependency` | an `ID-n` of another row, or null |
| `provenance` | exactly `{tags, see}`, as in the field registry |

**A delta row is source work, not evidence (R8-D8, moved here by R20-F2).** A row's
`acceptance_tests` name tests an implementer can run offline against the tree. The *existence* of an
external orchestrator, and its having completed a run, are **evidence** rather than source deltas: no
unit test can assert them, they are gated by §20.6b's AG-2 and AG-4, and they are deliberately not
`acceptance_tests` entries on any row. An earlier revision recorded that distinction in a free-text
`note` on one registry row, which is exactly the place a machine authority may not carry it.

**What is deliberately not compared, and why.** The gap bodies' `file:line` detail and their prose
about *why* a gap exists are hand-authored: no renderer exists, and the YAML does not hold them.
**Overlay-file existence and symbol resolution are downstream gates, not present ones** — those
files and symbols do not exist in the R54 tree by construction, which is what makes them gaps; test
28's overlay procedure resolves them at overlay time. Claiming to check them now would be the same
over-claim this section is correcting.

### 0.5a Implementation order — normative, and stated here (R21-F1)

The delta registry's `dependency` edges order the **deltas**. This section orders the **salvage
work** that carries them, which is a different constraint and was previously stated only in a
`DERIVATION` companion:

1. **Freeze the compatibility floor and make the specification reviewable.** Add §20's assertions
   and the in-tree fixture artifacts **before** anything is deleted.
2. **Extract the measurement kernel** out of `stage.go`'s orbit first. `Digest`, canonical JSON,
   `Instant`/`Clock` and the observation structs live in or near that orbit, and Palloc, Aeta,
   `record.go` and `verify.go` all depend on them, so deleting `stage.go` first breaks the package.
3. **Extract the process-group run/wait/signal/drain path** before deleting `contain_linux.go`.
4. **Close the loop** while reporter ingestion is still in place.
5. **Wire the planner** and add matrix-semantics tests before touching `MatrixJSON`.
6. **Port the retained test files**, then delete the proof tests and proof files.
7. **Reduce the interfaces**, then run the dogfood Vitest lane end to end.

**No ground-up rewrite.** Every item is a deletion, an extraction, or a wiring change. This ordering
is not reordered by a companion, and no companion restates it.

### 0.6 Review criterion for the next adversarial visit

A fresh reviewer must distinguish **current-source gaps** from **specification defects**. Reject
only if the specification's target or test is missing, contradictory, internally unsafe, or
physically infeasible — **not** because R54 source does not yet implement it. The gaps in §0.5 and
`scope.md` §21 are declared, targeted, and tested; they are not defects of this specification.

### 0.7 PD-1 — Additive compatibility

Every v0.2.2 legacy field **name, value, ordering, and executable script byte is preserved**.
`est_basis`, the `A_eta` display metadata (`a_eta_ns` and `wall_est_seconds`), and
`expanded_unit_set_digest` are **additive**: they are
new fields alongside the old ones and replace nothing.

- A **canonical v0.2.2-field legacy projection** — the matrix and plan document restricted to the
  field set v0.2.2 emitted, in v0.2.2 order — must be **byte-identical** for a consumer that has not
  opted in.
- An unopted invocation renders the **same script bytes**.
- Any earlier recommendation to *replace* `est_seconds` with `wall_est_seconds` is **superseded**;
  see `salvage-audit.md` and `component-map.json`, where each such row now carries an explicit
  superseded status.

Acceptance test: `TestReporterBasisPreservesLegacyProjectionAndDeclaresBasis` — canonical projection
of all v0.2.2 fields compared byte-for-byte, new fields asserted separately, and an unopted
invocation's script bytes unchanged.

### 0.8 PD-2 — Cold-start behaviour: two ordered phases, four normative outcomes

Mode selection and scored admission are **two separate sequential phases**, not one table of
mutually exclusive cases. Saying they were mutually exclusive was wrong: `{scored: true, basis
omitted, store missing}` matches both "emit a reporter cold plan" and "reject every cold plan".
Ordering resolves it.

**Phase 1 — mode selection.** Decides *what plan would be produced*, from the basis request and
store availability.

**Phase 2 — scored admission (higher priority).** If `scored: true`, a veto rejects any cold plan or
forced fallback **regardless of what phase 1 chose**.

The four normative outcomes:

| | Input | Outcome |
|---|---|---|
| (a) | basis omitted/default **+** missing or unknown-schema store | **reporter cold plan**, loudly labelled — *if not scored* |
| (b) | explicit `--est-basis reporter` **+** missing store | **reporter cold plan**, loudly labelled — *if not scored* |
| (c) | explicit `--est-basis wall` **+** model missing, `insufficient`, or `degraded` | **hard error, no matrix**, for every failure subtype, **both scored and unscored** |
| (d) | `scored: true` **+** any cold plan or fallback from (a) or (b) | **reject, no matrix** — phase 2 overrides phase 1 |

Outcomes (a) and (b) are conditional on phase 2 not vetoing; (d) *is* that veto. They are ordered
outcomes, not disjoint cases, and no document calls this table exhaustive or mutually exclusive.

### 0.9 PD-3 — Nonlinear partition objective

The `whole_file_count ≥ 1` admission rule is **not** adopted. Instead the objective itself is
corrected: a bucket's marginal cost includes **both** an indicator term for the single whole-file
invocation and a per-slice term.

```
reporter_ewma_ns[u] = round_half_up(reporter_ewma_seconds[u] × 1e9)     # int64
reporter_sum_ns(b)  = Σ_{u ∈ b} reporter_ewma_ns[u]                     # int64, frozen at plan time

A_eta_ns(b) = fixed_ns
            + round_half_up( scale × reporter_sum_ns(b) )
            + I(∃ whole-file unit ∈ b) × whole_invocation_overhead_ns
            + per_slice_overhead_ns × slice_count(b)
```

**One unit system, and one division (S-1).** Every quantity above is **integer nanoseconds**;
`scale` is **dimensionless** because both sides of its multiplication are nanoseconds. The only
conversion to seconds in the product is at the display boundary, applied to the **complete**
expression:

```
est_seconds(b) = round1( A_eta_ns(b) / 1e9 )
```

The superseded form fitted a **seconds-valued** predictor against a **nanosecond-valued** response
and then divided three of the four coefficients by 1e9 while leaving `scale × Σ(base seconds)`
unconverted — a literal implementation of it is wrong by a factor of 1e9. No term is divided
individually, no predictor is in seconds, and no intermediate crosses the unit boundary.

**The optimized objective is exactly this expression** — the same one displayed. It must hold for
whole-only, slice-only, and mixed buckets. Deterministic tie-breaking and core/adapter separation
are preserved. **§6.5 is the algorithm.** `scope.md` §7.2 works the K=2 counterexample in integer
nanoseconds, and `scope.md` §9.2 explains why the algorithm has the shape §6.5 gives it; neither
states a rule (R17-F1).

Acceptance tests: `TestAllocationObjectiveEqualsAetaForWholeAndSliceTopologies`, including
whole-only, slice-only, mixed, and the counterexample; and
`TestWallModelIsIntegerNanosecondsEndToEnd` (`scope.md` test 60), whose fixture answer differs by
1e9 under the superseded expression.

### 0.10 Authority — one normative root, two machine registries, and a reviewed map (owner F3)

**This contract is the sole normative prose root of the package.** A requirement exists only if this
contract states it, or if one of the two machine registries below states it within its own domain.
No other document creates, modifies, qualifies, relaxes or reinterprets a requirement.

**The hierarchy, in five lines.**

1. The owner chooses the product boundaries and the non-goals.
2. **One compact contract — this one — owns every measurable requirement.**
3. Source acceptance tests **prove** that contract. They cannot redefine it.
4. Fixture hashes, reports, receipts and campaign records are **evidence only**.
5. An owner choice cannot waive a factual unit, identity, cache or safety contradiction.

**The two machine registries.** Both live in `scope.md` §2A and `scope.md` §21.0, and each is the sole authority
for its own domain and for nothing else:

| Block | Domain |
|---|---|
| `field-registry v2` | wire paths, roles and memberships of the artifacts this product adds or makes model-relevant |
| `implementation-delta-registry v1` | the implementation deltas against R54: what is missing, what proves it, and where |

Their grammars are §0.5's. This contract does not restate what those blocks say, and nothing else in
the package has authority at all.

**Everything else is a companion, and `source-to-claim-map.md` says which is which.** That map is
short and reviewed: it records, per document, whether the document is derivation, evidence or
history, and which contract section owns the claim it discusses. It is read by a person, not parsed
as a grammar.

**What was here, and why it is gone (owner F3).** Earlier revisions grew a multi-document authority
grammar in this section: a machine-readable authority map with a closed artifact inventory, a
declared `operative_modals` vocabulary, an `owned_subjects` identifier index, a parsed
`companion_schema` for the JSON companion, and `TestSpecificationAuthorityIsSingular` as a **release
gate** over all of it. The owner has withdrawn that machinery. It improved neither the measured
interval, nor the model, nor the consumer's rendered estimate, nor the campaign comparison; it was
self-referential scope-about-scope of the same kind as the hostile-runner proof infrastructure §12
already excludes, and it is withdrawn for the same reason. What remains is what a reader needs:
one normative root, two registries with declared grammars, and a reviewed map.

**What did not change.** Every rule those checks were protecting is still stated, in its own section,
and is still proved by a product acceptance test in §22. The registry projections are still compared
— by `TestFieldRegistryCoversEverySerializedPath` (test 63) and
`TestArmsDifferOnlyByPlannerMode` — because those comparisons are about the product's serialized
bytes, not about which document is allowed to speak.

**Precedence.** This contract's body, then the two machine registries within their own domains.

## 1. Vocabulary

| Symbol | Name | Definition |
|---|---|---|
| `V[j]` | **Exec-envelope interval** | one rendered invocation, from the wrapper's first clock read inside `Exec` — taken before it creates the signing key, writer, spec identity, containment, controller, or observers — through the root child's completion and reap, same-PGID signal and group drain, observer close, containment destroy, and the closing read |
| `VB` | **Exec-envelope interval of the bucket script** | the complete generated script as one owned child, same bracketing |
| `A` | **instrumented run-bucket interval** | the first clock read inside `wall begin` to the last clock read inside `wall end` |
| `J` | whole-job wall time | **never measured, never estimated, never claimed** |

`V[j]` is neither a pure Vitest timer nor a whole-wrapper-process lifetime. Outside it, and named:
the `wall exec` process startup, its CLI dispatch and flag parsing, and the clock object's
construction all precede the first reading; the generated JSON spec-file write precedes the
wrapper entirely. Inside it, and named: for Mandel, `pnpm` → `tsx` → the façade → the façade's
offline preflight → `pnpm exec vitest` → Vitest initialization, import, transform, environment
setup, workers, hooks, test bodies, reporters, shutdown → façade cleanup, plus wrapper, observer,
and containment overhead.

`A` is **not** "the complete action" and not every nanosecond of a composite action. §3.2 names
what sits outside the clock pair at both ends, inside the very same step.

**Units, once (S-1, corrected by D-1).** `V[j]`, `VB`, `A`, every component span, the three
overhead/intercept coefficients, and every prediction are **integer nanoseconds**. **`scale` is the
one exception**: it is a dimensionless `float64`, because both sides of its multiplication are
nanoseconds — saying "every model coefficient is integer nanoseconds" contradicted that and is
withdrawn. The product of `scale` and `reporter_sum_ns` is returned to int64 by `round_half_up`
inside the objective, so `A_eta_ns` is integer.

**Seconds appear only in displays, and §5.1 is the inventory (D-1, R11-D8, R15-F1).** An earlier
revision named `scope.md` §9.2a the single authority *and* declared the inventory moved into §5.1,
which left one domain with two owners. There is one: **§5.1**. What this section fixes is the
*rule*, and §5.1 carries the enumeration:

- **every `wall`-basis seconds value is `round1` of a complete `A_eta_ns`**, divided exactly once;
- the **reporter-basis** surfaces (`PlanUnit`, the reporter `PlanSummary` values) are **legacy
  seconds** — the v0.2.2 EWMA quantities PD-1 freezes — and are *not* converted nanosecond
  quantities;
- `PlanSummary.ImbalancePct` is a **percentage**, not a duration, and is not part of the inventory;
- `matrix.wall_est_seconds` is emitted **iff the basis is `reporter` and a fitted model with status
  `ok` exists**, and is **absent** under `wall` basis; and
- the observation's echoed `est_seconds` is an audit copy of whichever value the plan displayed.

`TestEveryExposedEstimateDeclaresItsQuantity` walks §5.1's rows.



| Estimate name | Meaning |
|---|---|
| **reporter-work estimate** | the sum of a bucket's units' rolling reporter EWMA weights. It ranks and packs work. It omits repeated process/façade/preflight cost, action setup, wrapper overhead, and fixed action cost, and it is not a makespan at all once file parallelism exceeds one. **Not** a wall-time forecast; no accuracy claim may be made with it. |
| **`A_eta_ns`** | the calibrated forecast of `A` from §6, in **integer nanoseconds**. The only quantity permitted to be called a wall-time estimate. Its display rounding is `est_seconds = round1(A_eta_ns / 1e9)`. **Where it is serialized (D-1):** on **plan buckets** and on **observations**, in both cases **conditionally** — present iff the plan was built under `est_basis: wall`, absent under a reporter cold plan, which has no model and therefore no prediction. **Matrix entries do not carry `a_eta_ns`**; a consumer reading the matrix gets `est_seconds` and, under reporter basis with a fitted model, the additive `wall_est_seconds`. A validator recomputes `est_seconds` from `a_eta_ns` wherever both are present. |

### 1.1 Numeric domain — checked integers, rounding, and terminal arithmetic outcomes (R13-D5)

**This section is the single normative authority for arithmetic semantics.** Every other document
evaluates these rules and none restates them. An earlier revision fixed the *algebra* of the model
without fixing its *domain*: two conforming implementations could wrap, trap, or widen on the same
legal inputs and disagree, and no artifact said which was correct.

**The domain.** Every **serialized** `*_ns` quantity, every ring value, and every residual is a
**signed 64-bit integer count of nanoseconds**, range `[-2^63, 2^63 - 1]`. `scale` is the one
non-integer quantity: a `float64`.

**That sentence does not reach the campaign comparison values (R18-F4).** An earlier revision made
it quantify over "every campaign aggregate", which contradicted §1.2 on the same page: `DA` and
`RA_i` are **dimensionless ratios**, not nanosecond counts at all, and `mean(A)` or an
even-cardinality median can be a **non-integral rational** number of nanoseconds. None of them is a
signed-`int64` nanosecond count, and none is serialized by this package. In the role in which they
are **compared**, `DA`, `RA_i`, `mean(A)`, and every median of `DA`, `RA_i` or `TA` are exact
rationals over arbitrary-precision integers, and **§1.2 is their sole domain**. `TA` and `Amax` are
exact integers because they are a sum and a maximum of integer nanosecond counts, but when either
enters a comparison it enters it under §1.2, not under this sentence.

**Accumulation is exact and checked; wrapping and saturation are forbidden.** Every sum, product,
difference, and absolute value below is evaluated so that the **exact mathematical result** is
computed first and only then admitted to `int64`:

| Site | Accumulator | Narrowing |
|---|---|---|
| `reporter_sum_ns(b) = Σ_u reporter_ewma_ns[u]` | exact integer, width ≥ 128 bits | to `int64`, range-checked |
| the four-term `A_eta_ns(b)` sum of §0.9 | exact integer, width ≥ 128 bits | to `int64`, range-checked |
| `r_i = A_eta_ns(row_i) − elapsed_ns(row_i)` and `\|r_i\|` | exact integer, width ≥ 128 bits | to `int64`, range-checked |
| `Σ_i \|r_i\|` over the ring, `n ≤ W = 240` | exact integer, width ≥ 128 bits | **not** narrowed — it is a numerator |
| `residual_mae_ns = round_half_up( Σ\|r_i\| / n )` and `residual_p90_ns` = the **nearest-rank** element at 1-based index `ceil(0.90 × n)`, no interpolation, both from the **deployed** rounded coefficients | the **exact rational** mean, never a float | to `int64`, range-checked |
| campaign `TA = Σ_b A`, `Amax`, `mean(A)`, medians, `DA`, `RA_i` | `TA` and `Amax` are exact integers; `mean(A)`, the medians, `DA` and `RA_i` are exact rationals. Comparisons are by **cross-multiplication**, never by division | **arbitrary precision — §1.2 is the domain of every one of them when compared.** There is no `int64` narrowing site here: none of these quantities is serialized |

For the **nanosecond accumulation sites above the campaign row**, an implementation **must** use
either a signed integer type of width ≥ 128 bits or checked `int64` operations that report overflow.
**Two's-complement wrap-around and saturating arithmetic are non-conforming**, and
`TestWallArithmeticIsCheckedAndFailsClosed` rejects both. **That 128-bit floor does not extend to
the campaign row; §1.2 governs it, and a fixed 128-bit implementation of campaign comparisons is
non-conforming.**

**The `W = 240` worst case is defined, not undefined.** For a legal ring of `n = 240` rows whose
residual magnitudes are each `2^63 - 1`, the exact sum is `2 213 609 288 845 146 193 680` — which
exceeds `int64` and is therefore **never narrowed** — and the exact rational mean is
`2^63 - 1`, so `residual_mae_ns = 9223372036854775807`. Naive signed-`int64` accumulation would
yield `-240` and is the behaviour this rule outlaws. The 128-bit bound is sufficient by
construction: `240 × (2^63 - 1) < 2^71`.

### 1.2 Campaign rational comparison — arbitrary precision, with the proof (R14-F3)

**Campaign gate comparisons are evaluated in arbitrary-precision integers. A fixed-width
implementation is non-conforming, at any width.** An earlier revision permitted "a signed integer
type of width ≥ 128 bits" for *every* site including the campaign row, and that permission is
**withdrawn**: a legal campaign can produce a cross-multiplication that a signed 128-bit type
cannot represent, so two implementations both satisfying the old rule — signed int128 and arbitrary
precision — would disagree on a legal campaign, one of them silently.

**The witness, and it is legal.** Let `M = 2^63 − 1`. Two eight-bucket scored runs with these `A`
values are legal in every respect — eight complete buckets, every value a positive `int64`
nanosecond count:

```text
P = [1, 1, 1, M−1, M,   M,   M,   M  ]
Q = [1, 1, 1, M−2, M−1, M−1, M−1, M−1]
```

With the even-`n` median of §10 (the arithmetic mean of the two middle values) and
`DA = Amax / median(A)`:

```text
DA(P) = 2M     / (2M−1)      = 18446744073709551614 / 18446744073709551613
DA(Q) = 2(M−1) / (2M−3)      = 18446744073709551612 / 18446744073709551611
```

Both fractions are already in lowest terms and share no cross factor, so the mandated
cross-multiplication must form both products in full:

```text
DA(P) numerator × DA(Q) denominator = 340282366920938463334247398915801350154
DA(Q) numerator × DA(P) denominator = 340282366920938463334247398915801350156
signed int128 maximum                = 170141183460469231731687303715884105727
```

Each product needs 128 **magnitude** bits and therefore overflows signed int128, while the two
differ by **2** — so the comparison cannot be salvaged by any truncation, and `DA(P) < DA(Q)` is
decided entirely inside the bits that a signed 128-bit type loses. This is **not** an `int64`
narrowing site, so `E_NS_OVERFLOW` never fires; and failing the comparison instead would make a
legal campaign unevaluable, which is not an available answer.

**The rule.** Every campaign quantity that is compared as a rational — `DA` and its medians, `RA_i`
and its median, `TA` and its median, `mean(A)`, and both relative calibration bounds — is held as an
exact rational over arbitrary-precision integers, and every comparison is a cross-multiplication of
arbitrary-precision integers. No division, no floating point, and no fixed-width integer appears in
any of them. Serialization is unaffected: only quantities that are nanosecond counts are written,
and those are `int64` as §1.1 requires.

**Why not a proved fixed width.** `TA`-median cross-products already need 134 magnitude bits on
legal inputs, and each new derived comparison would need its own re-proof; a width that is correct
today becomes a silent defect the first time a gate is added. Arbitrary precision has no such
failure mode and is the reason it is required rather than recommended.
`TestWallArithmeticIsCheckedAndFailsClosed` carries this witness and **fails any implementation
that evaluates it in a fixed-width type**.

### 1.3 Rounding and terminal outcomes

**Rounding, defined for every real argument.**

| Operator | Definition |
|---|---|
| `round_half_up(x)` | the integer nearest `x`; on an exact tie, the integer of **larger magnitude** (away from zero). `x` must be finite and the result must lie in `int64` |
| `round1(x)` | the multiple of `0.1` nearest `x`; on an exact tie, the multiple of **larger magnitude**. Display only |

Every quantity to which `round_half_up` is applied in this product is non-negative, so the tie rule
is a totality statement rather than a reachable branch; it is pinned so that no implementation has
to choose.

**Terminal outcomes — every one is fail-closed and named.** There is no partial result, no default,
and no silent substitution. `E_*` names are the reported condition, not a serialized field.

| Condition | Outcome |
|---|---|
| any exact result outside `int64` at a narrowing site above | `E_NS_OVERFLOW`: the containing operation **fails**, exits non-zero naming the site and the operands, and writes **no** plan, matrix, model, observation, or calibration-evidence document |
| a non-finite intermediate (`NaN`, `±Inf`) anywhere — including `scale × reporter_sum_ns` and a stored `scale` that fails to parse finite | `E_NON_FINITE`: same fail-closed outcome |
| `round_half_up`/`round1` applied to a non-finite argument, or a rounded value outside `int64` | `E_CONVERSION_RANGE`: same fail-closed outcome |
| the Golub–Reinsch SVD of §6.6 exhausts its `75 × min(rows, 4)` iteration cap without meeting its stopping test | `E_RANK_NON_CONVERGENT`: `sigma_max` is **not produced**, the rank test has **no** result, and the containing operation fails closed — `plan --calibrate` writes no calibration-evidence document, `plan --est-basis wall` emits no matrix, and a fit stores no coefficients and leaves the model untouched. A rank is never guessed and never defaulted to deficient |
| the Lawson–Hanson NNLS of §6.8 reaches its `3 × 4 = 12` outer-iteration cap before `max(w) ≤ tol_nnls` | the solve is **not accepted**: no coefficients are stored, the model takes status **`insufficient`** with the named subtype `nnls_budget_exhausted`, and §7's rule 1 then makes an explicit `wall` plan a hard error. This is an ordinary unfitted model, not an arithmetic fault, and it is the **only** cap in this product that resolves to a status rather than to an `E_*` failure |

**Why the two caps resolve differently.** A missing model is a state the product already carries
and displays (`insufficient`, §0.8); a rank test that did not converge is a computation whose answer
is unknown, and reporting it as "deficient" would silently deny a warm model that the corpus may in
fact support. The first is a status; the second is a failure.

Acceptance tests: `TestWallArithmeticIsCheckedAndFailsClosed` (test 68) and
`TestSolverBudgetsHaveTerminalOutcomes` (test 69); both are registered in the test plan.

## 2. Product outcome

Balance a K-way Vitest matrix so each bucket's `A` is as nearly equal as the available signal
allows, and make the number the planner displays mean that same quantity.

**Precisely scoped.** The wall model changes the **partition weight** — which units land in which
bucket. It does **not** change the **unit topology**: whether a file is split at all, into how many
count-shards, and which runnable names share a slice are decided by `expandUnits` from the timing
store *before* any allocation score is applied, in both bases. §6 states this and the campaign
freezes it identically across arms. A product claim that the wall model chooses the work items
would be false.

**The allocation surface is not outcome-free, and is not claimed to be.** It is deliberately built
on historical, pre-treatment reporter outcomes — for the packing weight *and* for the unit topology
(§6.2). The claim this contract makes is narrower and checkable: no **current-run or
post-assignment** outcome reaches allocation. A stronger phrasing — "outcome-free allocation",
"no outcome-derived influence", or "mechanically pre-campaign" — would be false and appears
nowhere.

`A` is never `J`. §3.2 lists the exclusions by name; none may become an allocation label, an
estimate component, or a gate input.

## 3. Exact measurement boundary

### 3.1 Inside `A`

```
1  open envelope   `testbucket wall begin`   ← A_start: first clock read inside the process
2  setup           `testbucket wall run  -- bash -eo pipefail -c "$SETUP"`
3  run bucket      `testbucket wall exec --level script -- bash <bucket.sh>`      → VB
                       each script line is TWO commands joined by `&&`:
                         printf '%s' <spec-json> > <dir>/spec-<bucket>-<seq>.json
                         testbucket wall exec --level invocation -- <argv>        → V[j]
4  close envelope  `testbucket wall end`     ← A_end: last clock read inside the process
```

| Field | Interval |
|---|---|
| `setup_ns` | step 2's owned child, start to reap |
| `script_ns` | `VB` |
| `invocations[j].elapsed_ns` | `V[j]`, emission order |
| `script_overhead_ns` | `script_ns − Σ_j V[j]` — the spec-file writes, `wall exec` startup and flag parsing, inter-invocation gaps, script-level wrapper cost |
| `wrapper_ns` | `A − setup_ns − script_ns` — bootstrap, inter-step wrapper work, wait/reap, epilogue |

Invariants: `A ≥ setup_ns + script_ns`; `script_ns ≥ Σ_j V[j]`; all values `≥ 0`.

### 3.2 Outside `A`, by name

**Before `A_start`, inside the same step:** GitHub expression evaluation and composite-step
dispatch; the step shell's startup and command substitution; the `testbucket` binary's process
startup and CLI dispatch; and the boot-identity read `NewSystemClock` performs before the first
monotonic reading (`internal/walltime/action.go:176-177` → `internal/walltime/clock.go:70,109-112`).

**After `A_end`, inside the same step:** the closing boundary record's own write, the writer close,
the directory seal, the `wall end` process exit, and the step's shell tail
(`internal/walltime/action.go:903-920`). Containment destroy and handoff removal deliberately
precede the closing read and are inside `A`.

**Outside the action entirely:** queue and scheduling; runner allocation and image boot;
`actions/checkout`; `actions/setup-node` and `setup-go`; `pnpm install` / `npm ci`; fixture-binary
provisioning; testbucket acquisition or build; artifact upload; the plan job; the record job; every
sibling job; the workflow total.

**Outside `V[j]` but inside `VB`:** the per-invocation JSON spec-file write
(`internal/runner/vitestrunner/render.go:236-242`), and the `wall exec` process startup, CLI
dispatch, flag parsing, and clock construction that precede `Exec`'s first reading. All are
accounted in `script_overhead_ns`.

### 3.3 Clock and containment

`CLOCK_MONOTONIC`, a fresh read per endpoint, direct Linux syscall path, nanoseconds carried as
JSON strings. Boot identity at both endpoints; a change invalidates the observation. The wrapper
owns a process group and, before the closing read, performs exactly three things:

1. **waits and reaps the root child** it started — the only process it is the parent of;
2. **signals the process group** by negative PGID, `TERM` first, then `KILL` after a bounded
   interval;
3. **drains the group** — probes for the PGID's continued existence and does not take the closing
   reading while any member remains.

**It does not reap descendants.** A normal parent can `waitpid` its own child; it cannot reap
arbitrary grandchildren unless it becomes a child subreaper, and this product does not. Every claim
of "reap descendants", "reap grandchildren", or "descendant reap" is removed as infeasible as
written.

**Stated limitation.** A descendant that calls `setsid` or double-forks **leaves the process group**
and is therefore neither signalled nor drained; the closing reading may be taken while it still
runs. On platforms without a subreaper — and on non-Linux generally — group-level drain cannot be
guaranteed at all, and the wrapper records that it could not confirm an empty group rather than
implying it did. The promise is: root wait and reap, same-PGID signal with bounded escalation, and
empty-group drain where the platform supports probing it. No gate assumes more. §12 names the measurement
that would justify restoring stronger containment.

## 4. Scope of the change

1. Vitest only. Go's default rendered script bytes, canonical token, events, and audit semantics
   are unchanged. The claim is **current-pin isolation plus default execution neutrality**, not
   zero Go-facing change: shared core, store, and invocation schemas change additively, disclosed.
2. Closed by default. A consumer that does not opt in gets the v0.2.2 plan, matrix, script bytes,
   and store behaviour.
3. Salvage, not rewrite.
4. Distribution hardening ships separately; the measurement change does not depend on it.

## 5. The estimate a consumer reads

`est_seconds` stays a JSON **number**, seconds, exactly one decimal (half-up, as `round1`
computes it), present on every matrix entry, never a string, never null, never absent. Its meaning
is fixed by a declared `est_basis` on the plan document and every matrix entry:

| `est_basis` | `est_seconds` is | Allocation weight |
|---|---|---|
| `reporter` (default) | the **reporter-work estimate** — byte-identical to v0.2.2 | the same reporter EWMA weights |
| `wall` | **`round1(A_eta_ns / 1e9)`** — the bucket-level nonlinear objective, converted once at the display boundary | `A_eta_ns` itself, in integer nanoseconds; Stage 1 uses linearized nanosecond seeds derived from the frozen four-parameter model, purely as a search device |

**Units (S-1, D-1).** The model, the objective, the ring row, the response, and the residuals are
**integer nanoseconds** throughout, with `scale` the one dimensionless float. `est_seconds` is **not**
the only seconds-valued surface: §5.1 enumerates every one of them. The exact integer value is serialized as `a_eta_ns` on **plan buckets and observations**,
conditionally on wall basis and **never on matrix entries**, so a validator can recompute
`est_seconds` from it wherever both appear.

**The displayed number and the optimized number are always the same quantity.**
`wall_est_seconds` may be emitted as a shadow diagnostic in `reporter` basis; it is never the
allocation weight there.

**There is no per-unit wall estimate (SR-5).** The `I(∃ whole-file)` term is a property of a
bucket's composition and has no unique per-unit decomposition, so the unit-level display retains
reporter-basis values under both bases. The Stage-1 seed scores are a deterministic search device,
never an estimate and never displayed.

**`est_basis: wall` requires `file_parallelism == 1`**: the model is valid only for a serial
bucket.

Every user-facing description names its basis. The mislabels this product repairs, each with the
source site that carries it, are **§16.4** of this contract; nothing outside this contract is part of
it (R16-F1: this paragraph used to incorporate §16.4, which is now a redirect and which the
withdrawal of `ANNEX` had already made impossible).

### 5.1 The seconds-valued surface inventory — complete, and stated here (R14-F1)

**This is the whole inventory.** It was previously reached only by following a citation out of this
contract; it is here now, and `scope.md` §9.2a is a derivation explaining where each row comes
from. No other document maintains a second list.

| Surface | `reporter` basis | `wall` basis | Origin |
|---|---|---|---|
| `PlanUnit.est_seconds` | reporter EWMA work for that unit | **unchanged** — reporter EWMA work, labelled as such | **legacy seconds** (v0.2.2 EWMA); *not* a converted nanosecond quantity |
| `PlanSummary.MeasuredSeconds` / `EstimatedSeconds` / `TotalSeconds` / `MeanSeconds` | reporter sums | **unchanged** — reporter sums, labelled as such | **legacy seconds** |
| `PlanSummary.IdealSeconds` / `MakespanSeconds` / `LightestSeconds` | derived from reporter bucket sums | derived from `A_eta_ns` bucket values | legacy seconds under reporter; **`round1(ns/1e9)`** under wall |
| `PlanBucket.est_seconds` | reporter sum over the bucket's units | `round1(A_eta_ns(b)/1e9)` | legacy under reporter; converted under wall |
| `matrix.est_seconds` | reporter bucket sum | `round1(A_eta_ns(b)/1e9)` | legacy under reporter; converted under wall |
| `matrix.wall_est_seconds` | `round1(A_eta_ns(b)/1e9)` — the **additive shadow** | **absent** | converted; see the presence rule below |
| observation `est_seconds` (echoed) | the plan's displayed value for that bucket | the plan's displayed value | echo of whichever of the two above applied; audit only |
| run-bucket banner `estimated Ns` | reporter bucket sum | `round1(A_eta_ns(b)/1e9)` | legacy under reporter; converted under wall |

**The one presence rule for `wall_est_seconds`.** It is emitted **iff the basis is `reporter` and a
fitted model with status `ok` exists**. Under `wall` basis it is **absent**, because `est_seconds`
already *is* the model's value. It is a shadow diagnostic and never reaches `AllocationScore`.

**`PlanSummary.ImbalancePct` is not in this inventory.** It is a percentage, not a duration.

**Not every seconds display is a converted nanosecond quantity.** The reporter-basis rows are
**legacy seconds** — the v0.2.2 EWMA surfaces PD-1 freezes. The claim that holds is narrower and
exact: **every `wall`-basis seconds value is `round1` of a complete `A_eta_ns`**, divided once.

**Where the exact integer is serialized.** `a_eta_ns` appears on **plan buckets** and on
**observations**, present iff the plan was built under `est_basis: wall`. **Matrix entries carry no
`a_eta_ns`.**

**There is no wall-basis per-unit estimate.** `A_eta_ns` carries the `I(any_whole_file)` indicator,
which has no unique per-unit decomposition, so unit-level display keeps reporter-basis values under
both bases. A bucket's `est_seconds` under wall basis is therefore **not** the sum of its units'
`est_seconds`, and the plan report says so on the same screen.

`TestEveryExposedEstimateDeclaresItsQuantity` walks every row above.

## 6. Learning loop, model, and what it controls

```
plan (basis-aware) → run bucket under the wrapper → one observation per bucket
        ↑                                                         │
        └────── fit on the default branch ← audit + ingest ───────┘
```

### 6.1 What the model controls

**The partition weight, and nothing else.** `BuildPlan` calls `expandUnits` with the timing store at
`internal/core/plan.go:179` and applies `AllocationScore` only at `:193-194`. `est_basis` selects
the weight Karmarkar–Karp packs; it does not select the items.

### 6.2 What selects the items — three stored reporter outcomes, named

| Decision | Store field | Site |
|---|---|---|
| whether a unit is a whale at all | `UnitStat.Seconds` against `total/K` | `internal/core/units.go:145,179` |
| how many count-shards or slices | `UnitStat.SplitInto`, via `clampShards` | `internal/core/units.go:145,179` |
| which runnable names share a slice | `UnitStat.Tests` per-runnable EWMA | `internal/core/units.go:211,222,242` |

All three are reporter-derived and identical in both bases. This contract **allows** them
explicitly: they are historical, file-keyed, and observed before treatment assignment, which is
what makes them ordinary pre-treatment covariates rather than leakage. What it forbids is
current-run and post-assignment outcome. The campaign holds topology constant across arms by
asserting `expanded_unit_set_digest` (§10).

### 6.3 What the ledger is sufficient for — and what it is not

At **invocation and instrumented-action granularity** the observation ledger is sufficient for a
trusted paired comparison — and the claim is scoped to the fields the schema of §13
actually retains:

| Claimed | Retained field |
|---|---|
| what ran | `invocations[].argv_digest`, `.selector`, `.units`, `.atoms` |
| where it ran | `invocations[].cwd_digest`, `process_group_id` |
| which plan it belongs to | `plan_digest`, `profile`, `expanded_unit_set_digest` |
| which orchestration, binary, and workload produced it | `head_sha`, `candidate_sha`, `workload_commit` — three separate fields (S-6) |
| what cache state it ran under | `cache_state` — every leaf §10.5.0 classes as `declaration` or `outcome`, and no other (S-5, R13-D2, R16-F2) |
| how it ended | `terminal`, `exit_code`, `invocations[].exit_code` |
| the two boundaries | `started_mono_ns`, `ended_mono_ns`, per-invocation pair, `boot_id_*` |

`cwd_digest` and `process_group_id` are **added to the schema** by this revision precisely so the
claim matches what is stored; QC6 and QC7a check them. Nothing outside this table is claimed.

At **unit granularity it is not sufficient**. A whole-file Vitest invocation may cover many planned
units while its stream carries one aggregate `V`, and no signature over a supplied claim creates
the missing decomposition. The ledger therefore supports invocation- and action-level
**evaluation**, not file-specific **training labels**, unless invocations are made single-unit or
an attribution method is separately validated.

This product uses it only for the former: the model is fitted at bucket level, and the per-unit
signal comes from the reporter EWMA. Any surviving selected-work structure must require **singleton
equality** with its unit id — a non-empty list is not a unit label.

### 6.4 Field roles and the model

**REGRESSORS — the four PD-3 design columns, and nothing else** (SR-6; roles defined once in `scope.md` §2A):

| Column | Predictor, frozen at plan time | Unit | Coefficient unit |
|---:|---|---|---|
| 1 | constant `1` | — | integer ns |
| 2 | `reporter_sum_ns` = `Σ_u reporter_ewma_ns[u]` over the bucket's units, as the store stood at plan time | **integer ns** | **dimensionless** |
| 3 | `I(any_whole_file)` ∈ {0,1}, the indicator `whole_file_count > 0 ? 1 : 0` (R19-F2: stated here, not in the registry) | — | integer ns |
| 4 | `slice_count` | count | integer ns |

**Response:** the observation's top-level `elapsed_ns`, which is `A`.

**Not inputs, not responses — diagnostic/audit only:** `setup_ns`, `script_ns` (=`VB`),
`wrapper_ns`, `script_overhead_ns`, `invocations[].elapsed_ns` (=`V[j]`), `invocation_count`, and
`whole_file_count`. `whole_file_count` is retained for audit; the model uses the **indicator**
`I(any_whole_file)`, not the count. **Scoping key:** `comparability_key_digest` (§15.3).

**Model**, per execution profile: the PD-3 objective **is stated once, in §0.9**, and this
section does not restate it. `A_eta_ns(b)` and `est_seconds(b)` mean exactly what §0.9 defines,
evaluated in the checked-integer domain of §1.1.

**That one expression is both the displayed value and the optimized objective.** It is not additive
over units — the indicator depends on bucket composition — so the partition is produced by a
deterministic two-stage procedure: a Karmarkar–Karp seed on the additive part
(`scale·ewma`, plus `per_slice_overhead_ns` for slice units) through the existing
`PlanOptions.AllocationScore` seam, then a bounded, strictly-improving refinement evaluated against
`A_eta_ns` itself, in integer nanoseconds. **The algorithm is §6.5.** `scope.md` §7.2 explains the worked K=2 counterexample behind it.

**Historical timing is a pre-treatment covariate, not leakage — under mandatory conditions.** A
per-file duration is an ordinary predictor only when its cutoff provably precedes campaign
execution and its exclusion domains are recorded.

**These are operator-side campaign gates, evaluated after dispatch, and they are not the in-CI
selection rule (R13-D1).** The **sole** mechanism that selects the in-CI fitting population is
`campaign_id → trainable`, stated once in §6.4b and implemented as §15.1b describes; the
gates below are how the operator *verifies*, with authenticated instants the workflow cannot
obtain, that the mechanism did its job. They are **required**, never optional: the fitted model's
`fitted_at` must strictly precede the first arm's authenticated start; the population the fit
consumed must contain zero rows from the campaign's harness run universe and zero rows inside the
authenticated campaign window; and the manifest must name every exclusion domain, including
`excluded_run_ids` and `excluded_window`. A missing field fails the campaign rather than being
skipped. No in-CI component is required to evaluate `excluded_run_ids` or the authenticated window,
and none may be specified as doing so — those inputs do not exist when the config is frozen. Rows produced by the candidate binary outside that window are **retained** — they are the
warm corpus (S-6).

**No manufactured per-unit labels.** The renderer merges every whole-file unit of a bucket into one
invocation. No code copies that `V` onto one unit, divides it across units, or accepts a
multi-unit selected-work claim as a single-unit label. The per-unit signal is the reporter EWMA;
wall observations teach only `scale` and the overheads. A dedicated per-unit calibration — one
invocation per scheduling unit — is a named, costed, deferred alternative (`scope.md` §7.6).

**Acknowledged identifiability limit.** The model learns a global scale and two overheads. It
cannot distinguish two same-shaped files with different import graphs, transform volumes, setup
dependencies, or cache behaviour, and does not claim to. That is why the six-feature frozen scorer
is deleted rather than retained: six coarse shape features could not identify those costs either.

### 6.4a Which planner route may affect experimental topology

Two routes exist and they are **not** symmetric. This contract binds them:

| Route | May affect unit topology? | Rule |
|---|---|---|
| ordinary EWMA planner — `BuildPlan` → `expandUnits(live, store)` | **Yes.** It is where whale status, shard width, and slice membership are decided (§6.2). | Permitted, and it is the *same* store bytes in both arms, so topology is a held constant, not a treatment. |
| runtime-provenance path — `internal/walltime/palloc.go` scorer | **No.** It receives already-expanded units and returns a weight. | It must never reach `expandUnits`, never read a store row, and never alter unit identity or composition. |

The experimental treatment is confined to the weight the second route returns. Any change that
lets the scorer influence expansion voids the isolation and is a defect.

### 6.4b Allowed features and the training cutoff — bound here, not left to policy

**Allowed features, exhaustively.** Nothing outside this list may enter the fitted model:

**Plan-time predictors — the only regressors (PD-3's four columns):**

```
1                                      reporter_sum_ns(bucket)      # integer nanoseconds
I(any_whole_file)(bucket)              slice_count(bucket)
```

`reporter_sum_ns` is `Σ_u round_half_up(reporter_ewma_seconds[u] × 1e9)`, summed in int64 and frozen
at plan time. A seconds-valued column 2 against a nanosecond-valued response is forbidden (S-1).

**Response — one quantity, and it is `A`:** the observation's **top-level `elapsed_ns`, which is
`A`**, the instrumented run-bucket action interval. Nothing else is a response.

`invocations[j].elapsed_ns` is `V[j]` — one Vitest invocation. `script_ns` is `VB` — the bucket
script. `A`, `VB`, and `V[j]` are **distinct quantities, never aliases**. Fitting `V` would exclude
the action setup and wrapper time that `fixed_ns` exists to learn, which is exactly why the response
is `A`.

**Diagnostic / audit values — recorded, reported, and checked, but never regressors and never
responses.** The four frozen PD-3 columns above are **exhaustive**:

```
setup_ns   script_ns (= VB)   wrapper_ns   script_overhead_ns
invocations[].elapsed_ns (= V[j])          invocation_count   whole_file_count
```

**Scoping key:** `comparability_key_digest` (§15.3).

**Training cutoff.** The fitted model's `fitted_at` and the store's `updated_at` must be strictly
earlier than the **earliest authenticated start** of any campaign run. The comparison uses
authenticated Actions API instants (§9), never a self-declared field.

**Pre-treatment and hold-out verification.** Verified, not asserted:

1. The store artifact's SHA-256 is frozen in the manifest before run 1 and re-verified after
   download; both arms restore those exact bytes.
2. **The in-CI mechanism is `trainable`, written at append, and it is fail-closed (R9-D2,
   R13-D1).** The frozen config declares one opaque `campaign_id`; `run-bucket` stamps it onto every
   observation the pilot or campaign produces; `ingest` appends every qualifying row as a diagnostic
   and sets `trainable` at append — `false` when the id matches, and a **rejection** when the id is
   absent or different while a config is supplied, so a miswired arm cannot stay on the trainable
   path. **`trainable` is the single carrier**: the same append also stamps `false` on a row the
   supplied config marks foreign by workload or by orchestration commit, because those operands are
   present at append too (§15.1b class A). `campaign_id` remains the primary and the only
   fail-closed one. **The fitter then evaluates exactly one predicate, `trainable == true`, and no
   other** — a fit that evaluates a second selection predicate is a defect, not a belt-and-braces
   check. This is durable: it is a property of the stored row, not of ambient time, so a delayed or
   retried ingest cannot refit on a campaign row.

   The observation ring is then enumerated **as an operator-side cross-check after dispatch**, and
   the campaign fails if any row used for fitting carries a `run_id` in the harness universe or an
   **authenticated** `run_started_at` inside the campaign window (§15.1b, §18.2). Those
   predicates **cannot** be the in-CI mechanism, because the run IDs they name do not exist when the
   config is frozen; they exist to catch a `trainable` marking that disagrees with what ran. All are
   mandatory; a validator that cannot evaluate the window predicate fails the campaign rather than
   skipping it. **Corrected (S-6):** the earlier "a `head_sha` equal to the
   candidate or orchestration commit" overloaded one field with three identities and contradicted
   warming — comparability pins `testbucket_sha256`, so the warm corpus is necessarily produced by
   the candidate binary. The row now carries `head_sha` (orchestration), `candidate_sha`
   (testbucket build), and `workload_commit` (consumer checkout) as three separate fields, and
   campaign exclusion is the in-CI `campaign_id` stamping of §6.4b and §15.1b, materialized into
   `trainable: false` at append; the run-id and window predicates above remain the operator-side
   cross-check only.
3. Every exclusion domain is **named** in the manifest. A domain merely absent from a list is not
   excluded.
4. Held-out scoring: the calibration gates of §10.4 are evaluated on the campaign's C rows, which
   by (1)–(3) are disjoint from every row the model was fitted on.

A missing item among (1)–(4) fails the campaign (item 11 of this section).


### 6.5 Allocation algorithm — both stages, stated here (R14-F1)

**This is the operative algorithm.** It was previously owned by `scope.md` §9.2; that section is now
a derivation carrying the worked counterexample and the design rationale. Every quantity below is
integer nanoseconds evaluated in the domain of §1.1; `A_eta_ns` is §0.9's objective and is not
restated.

```
units = expandUnits(live, store)          # SAME IN BOTH BASES - store-derived

if est_basis == reporter:
        weight(u)           = store_ewma(u)               # or mean weight when unmeasured
        est_seconds(bucket) = round1(sum weight(u))       # the REPORTER-WORK ESTIMATE
        # byte-identical to v0.2.2

if est_basis == wall:
        require model status "ok" and file_parallelism == 1    # else FAIL (section 7)

        # EVERYTHING BELOW IS INTEGER NANOSECONDS. No term is ever divided here.
        base_ns(u) = round_half_up(store_ewma_seconds(u) * 1e9)   # or the mean weight, same conversion

        # cost_ns(b) is the OBJECTIVE and the DISPLAYED value - one expression, evaluated once.
        cost_ns(b) = A_eta_ns(b)                          # section 0.9

        # Stage 1 - the EXISTING karmarkarKarp (internal/core/partition.go), NOT LPT.
        # Packs the ADDITIVE part only, still in nanoseconds. A slice unit's seed carries
        # per_slice_overhead_ns, so THE SEED IS MODEL-SPECIFIC - two fitted models generally
        # produce two different KK seeds.
        seed_ns(u) = round_half_up(scale*base_ns(u))                          # whole-file unit
        seed_ns(u) = round_half_up(scale*base_ns(u)) + per_slice_overhead_ns  # name-slice unit
        partition  = karmarkarKarp({seed_ns(u)}, K)       # via PlanOptions.AllocationScore

        # Stage 2 - deterministic refinement against the TRUE objective.
        # TWO neighborhoods. REFINE_PASSES bounds them TOGETHER, not each.
        REFINE_PASSES = 8
        for pass in 1..REFINE_PASSES:
            moved = false

            # (2a) SINGLE-UNIT MOVES -- THE SINGLE NORMATIVE SCHEDULE
            #      Enumeration order: units by unit_id asc, then buckets by bucket_index asc.
            #      RESTART POLICY: NONE. An accepted move does NOT restart the pass; enumeration
            #      CONTINUES through the remaining buckets and the remaining units, re-reading the
            #      moved unit's new home. There is exactly one schedule; an implementation that
            #      restarts on the first accepted move is a DIFFERENT algorithm and non-conformant.
            for u in units sorted by unit_id asc:
                for t in buckets sorted by bucket_index asc:
                    if t == bucket(u): continue
                    if KEY(plan | u moved to t) < KEY(plan):    # STRICT on the TUPLE
                        move u to t; moved = true

            # (2b) WHOLE<->SLICE PAIR SWAPS
            #      ENTRY CONDITION: only when (2a) accepted nothing this pass.
            #      enumeration order: whole units by unit_id asc, then slice units by unit_id asc
            if not moved:
                for w in whole_file_units sorted by unit_id asc:
                    for v in slice_units sorted by unit_id asc:
                        if bucket(w) == bucket(v): continue
                        if KEY(plan | w<->v swapped) < KEY(plan):   # STRICT on the TUPLE
                            swap w and v; moved = true
                            break out of both loops        # take the FIRST improving swap,
                                                           # then start the next pass

            if not moved: break        # no accepted transition in this pass -- the loop stops.

        # ACCEPTANCE KEY - what both neighborhoods compare.
        #   KEY(plan) = ( max_b cost_ns(b) , canonical_bucket_vector(plan) )
        #   canonical_bucket_vector = each bucket's unit_id list sorted asc,
        #                             the K lists compared in bucket_index order,
        #                             element-wise as byte strings.
        # Compared LEXICOGRAPHICALLY as a 2-tuple; a move or swap is accepted only on a
        # STRICT DECREASE OF THE WHOLE TUPLE.

        # DISPLAY BOUNDARY - the ONLY division by 1e9 in the product.
        a_eta_ns(bucket)    = cost_ns(bucket)             # serialized as plan.buckets[].a_eta_ns
        est_seconds(bucket) = round1( a_eta_ns(bucket) / 1e9 )
```

**What is guaranteed, and what is not.** A **deterministic output after a bounded schedule** — and
nothing more. The loop stops on a quiet pass **or** on the `REFINE_PASSES` cap, and the cap may
leave improving transitions unexplored. This is **not** a fixed point, **not** convergence, and
**not** an optimum, and no document may call it any of those.

`invocation_count(bucket)` is exact post-render:
`(1 if any whole-file unit else 0) + slice_count`.

**Core/adapter separation, stated here (R17-F1).** Core never learns what a `vitest run` is. The
adapter declares the shape of a bucket through one neutral hook —
`InvocationShape(units) → {whole_invocations, slice_invocations}` — with Vitest returning
`{I(any non-slice unit), slice_count}` and Go returning `{0, len(units)}`, since Go renders one
invocation per unit. `cost_ns(b)` is computed in core from the frozen model parameters and that
shape, and from nothing else. `fixed_ns` stays out of the per-unit seed: every non-empty bucket pays
it identically, so it cannot change the partition, only the reported number; the two overhead terms
do change it, which is the point of PD-3.

**Unit discipline in this algorithm.** `base_ns`, `seed_ns`, `cost_ns` and `a_eta_ns` are int64
nanoseconds, `scale` is dimensionless, and the single `/ 1e9` appears once, on the complete
`a_eta_ns`, in the last line. Stage-2 comparisons are therefore integer comparisons: refinement is
exactly reproducible and no rounding can flip a strict improvement.

### 6.6 Rank admission — the frozen criterion, stated here (R14-F1)

A wall-basis plan is admitted **only if `rank(X) == 4`** for
`X = [1, reporter_sum_ns, I(any_whole_file), slice_count]` built from the qualifying rows of the
current execution profile, evaluated by rank-revealing QR against this frozen tolerance:

```
tol = sigma_max(X) * 4 * 2.220446049250313e-16
```

`4` is the column count and the constant is `float64` machine epsilon. A column is **rank-deficient
when its QR pivot is strictly less than `tol`**.

| Aspect | Frozen choice |
|---|---|
| `sigma_max(X)` | the largest singular value from a **Golub–Reinsch SVD** — Householder bidiagonalisation then implicit-shift QR on the bidiagonal, iterating until `\|e_i\| ≤ 2.220446049250313e-16 × (\|d_i\| + \|d_{i+1}\|)`, capped at `75 × min(rows, 4)` iterations; the classical LAPACK `dgesvd` path, never a randomised or truncated estimator |
| the QR | Householder with **column pivoting**; pivot is the column of maximum remaining norm, ties to the **lowest column index** |
| `rank(X)` | the count of pivots `≥ tol` |
| `deficient_columns` | original column indices whose pivot fell below `tol`, ascending |
| serialization | `sigma_max`, `tolerance`, `min_pivot` as shortest round-tripping decimal strings (Go `'g', -1, 64`) |
| **cap exhaustion** | `E_RANK_NON_CONVERGENT` per §1.3 — `sigma_max` is not produced, **no rank is inferred**, and no artifact is written |

Row and run counts are necessary and never sufficient: `MIN_ROWS` and `MIN_RUNS` do not imply
identifiability, and the earlier topology-specific support rules are withdrawn as mathematically
underspecified.

### 6.7 Calibration outcomes and proposer totality — stated here (R14-F1)

`--calibrate` is a **planning mode**: it implies `scored: false`, emits no campaign matrix, and never
writes the store. `N = --calibration-max-plans` is an integer `N ≥ 1`, default `4`, with no maximum.

**The layout function `L(U, K, i)` is PARTIAL** — for some `(U, K, i)` no layout exists. **The
proposer is TOTAL**: for every legal `(U, K, N)` it returns exactly one of

```
SUFFICIENT   |   STRUCTURALLY_INFEASIBLE   |   NOT_FOUND_WITHIN_BUDGET
```

and no fourth value. When `L(i)` is undefined the proposer records `layouts_tried = i − 1`, stops,
and reports `NOT_FOUND_WITHIN_BUDGET` with `generator_exhausted: true`. A bounded miss is **never**
promoted to structural infeasibility; `STRUCTURALLY_INFEASIBLE` is reserved for a whole-universe
proof that names an actually-zero column.

**Totality is a statement about those three outcomes only.** The proposer may instead terminate
**fail-closed** with a named condition of §1.3 — `E_RANK_NON_CONVERGENT`, `E_NS_OVERFLOW`,
`E_NON_FINITE`, `E_CONVERSION_RANGE` — writing **no** calibration-evidence document. It never
returns silently and never reports an incomplete computation as one of the three.

A topology proposal proves rank *reachability* for the layouts it enumerated, or structural
impossibility. It **cannot** manufacture executions, and the warm-up execution protocol cannot prove
rank. Neither substitutes for the other, and `MIN_ROWS`/`MIN_RUNS`/rank are re-checked against the
real ring on every wall plan.

### 6.8 Fitting procedure — the pinned solver, stated here (R14-F1)

**This is the procedure.** It was previously owned by `scope.md` §7.4; that section is now an annex
carrying the published witness and the reasoning. Selection is §6.4b's `trainable == true` and no
other predicate.

1. **Order** the selected rows for arithmetic reproducibility by
   `(head_sha, run_id, run_attempt, bucket_index)`. This is a **summation order**, never a definition
   of recency.
2. **Check rank sufficiency (§6.6) before fitting.** On failure the model status is `insufficient`
   and no coefficients are produced.
3. **Solve** non-negative least squares over §6.4b's four integer-nanosecond columns against the
   observation's top-level `elapsed_ns`, in `float64`. The solver is pinned, because full column rank
   gives a unique mathematical optimum but **not** identical floating-point bytes:

   | Aspect | Frozen choice |
   |---|---|
   | algorithm | **Lawson–Hanson active-set NNLS**, the classical formulation; no other family is permitted |
   | initial state | passive set empty, `b = 0`, all four columns in the active set |
   | column selection | the index of **maximum dual** `w = Xᵀ(y − Xb)`; on an exact tie, the **lowest column index** |
   | inner unconstrained solve | the normal equations over the passive set, by **Cholesky with columns in ascending index order**; summation over rows in step 1's order |
   | infeasibility step | the classical `α = min` ratio test; on an exact tie the **lowest column index** leaves the passive set |
   | stopping | `max(w) ≤ tol_nnls` with `tol_nnls = 10 × 4 × 2.220446049250313e-16 × ‖Xᵀy‖∞`, or after `3 × 4 = 12` outer iterations, whichever comes first |
   | **budget exhaustion** | if the 12-iteration cap fires **before** `max(w) ≤ tol_nnls` the solve is **not accepted**: no coefficients are stored, the model takes status `insufficient` with subtype `nnls_budget_exhausted`, and an explicit wall plan is a hard error. A partially converged vector is **never** deployed |
   | `scale` serialization | shortest round-tripping decimal (Go `strconv.FormatFloat(v, 'g', -1, 64)`); the model's only non-integer field |

4. **Round** the three nanosecond coefficients to `int64` by `round_half_up`; keep `scale` as
   `float64`. **Residuals and the `degraded` decision are computed from the ROUNDED coefficients** —
   from exactly the model that will plan — so a stored `residual_mae_ns` always describes the
   deployed predictor. Record `rows_used`, `runs_used`, `fitted_at`, `residual_mae_ns`,
   `residual_p90_ns`, and the four coefficients.
5. **Residual aggregates**, over the same rows in step 1's order, with
   `r_i = A_eta_ns(row_i) − elapsed_ns(row_i)`, every accumulation in §1.1's checked domain:

   | Field | Definition |
   |---|---|
   | `residual_mae_ns` | `round_half_up( Σ\|r_i\| / n )` — the exact rational mean, converted by the **same** `round_half_up` the coefficients use. No other rounding is permitted |
   | `residual_p90_ns` | **nearest-rank**: sort `\|r_i\|` ascending, take the element at 1-based index `ceil(0.90 × n)`. **No interpolation**, so the value is always an observed residual |

6. **`model_parameters_digest`** is SHA-256 over the UTF-8 canonical JSON of exactly
   `{"fixed_ns":<int>,"scale":"<shortest round-trip decimal>","whole_invocation_overhead_ns":<int>,"per_slice_overhead_ns":<int>}`
   — those four keys, in that order, no whitespace, integers as bare decimal digits, and `scale` as a
   **quoted string** so no float formatter can vary the bytes.

**Model status vocabulary.** `ok`; `insufficient` (below `MIN_ROWS`, below `MIN_RUNS`,
rank-insufficient per §6.6, or `nnls_budget_exhausted`); `degraded`
(`residual_mae_ns > MODEL_MAE_CEILING`). Only `ok` proceeds under wall basis, and a failure subtype
is always named — **carried in the registered store leaf `wall.failure_subtype`** (§15.1c), so two
conforming implementations serialize the same bytes for the same outcome (R23-F2). §15.1c spells the
subtype for **every** one of those causes, including the two count causes, and fixes the precedence
when more than one holds (R24-F2).

## 7. Fail-closed behaviour

1. `est_basis: wall` with no model, or status `insufficient` or `degraded` → the plan **fails**.
2. A unit the allocation score cannot score → the plan fails.
3. `est_basis: wall` with `file_parallelism > 1` → the plan fails.
4. `--wall-dir` with `--runner go` → error.
5. An observation is ingested only if **every** applicable check of **§7.1** passes.
6. Failed, cancelled, timed-out, or malformed observations are retained as diagnostics and never
   train.
7. Exactly one observation per `(head_sha, run_id, run_attempt, bucket_name)`; a duplicate rejects
   all of them.
8. An unknown store schema cold-starts loudly and is never reinterpreted.
9. A execution-profile change resets wall observations and the model, loudly.
10. A missing mandatory campaign field — cutoff, exclusion domain, orchestration identity, workload
    identity — fails the campaign; it is never skipped.
11. A missing or unverifiable item among §6.4b (1)–(4) fails the campaign.
12. A pair whose authenticated instants contradict the declared counterbalanced **sequential** order
    fails the campaign: the predicate is authenticated `completed_at(first) ≤ started_at(second)` in
    the declared direction, so two **overlapping** arms fail even when their starts are ordered
    correctly (§0.2, §10). An empty or unparseable instant is a failure, never a skip.
13. An absent row is **absent** — never imputed, never zero, never silently dropped.
14. **Pre-campaign warm-up (SR-10, S-7).** Explicit wall mode is admitted only when the profile's
    history satisfies **all four** of: ≥ 24 accepted rows; ≥ 3 distinct `(run_id, run_attempt)`;
    design rank 4 under the frozen tolerance of **§6.6**; and **at least one `i_any_whole_file = 0`
    row**. The campaign cannot supply these — campaign rows never train — so non-campaign
    calibration runs under the same execution-profile key must produce them. A topology proposal
    (§6.7) proves rank reachability or structural impossibility and **cannot** manufacture
    executions; a warm-up execution protocol populates the ring and proves nothing about rank on its
    own, and it must additionally demonstrate that **three distinct real runs reproduce one
    `comparability_key_digest`**. Until all four conditions hold, explicit wall mode is a hard error
    with no matrix, and this relaxes neither PD-2 nor PD-3.
15. **Scored cache state (S-5, R8-D4, R14-F2).** A scored run produces an observation only if
    **QC14a** passed on its own runner, and that observation is ingested only if **QC14b** passes at
    record time; §10.5.6 states both and the boundary between them. The tuple must be one of
    §10.5.1's — which includes the legitimate `exact-key` **miss**, where `matched_key` is empty —
    with no prefix-fallback match, `transform_cache_mode == "disabled"`, and an executed MongoDB
    binary digest equal to the declared `expected_mongo_binary_sha256`. A pair whose arms disagree
    **index-wise on any of the `bc_inv` cache leaves** is retained and **not scored**. Under
    §19.2b's exact-key mode an index-wise difference in `dependency_cache_hit`,
    `dependency_cache_matched_key` or `dependency_cache_disposition` is likewise **retained and not
    scored** — a *post-start* outcome, so §19.8 applies and it is never rescheduled (R15-F2, R15-F3).
    An earlier revision said such a difference "does not unscore" the pair; that is withdrawn.
16. **Separated provenance identities (S-6).** An observation is ingested only if QC15 passes:
    `head_sha` (orchestration), `candidate_sha` (testbucket build), and `workload_commit` (consumer
    checkout) are three separately present, well-formed values. A row that collapses them is
    rejected rather than silently overloaded.
17. **Scored plan admission inputs (S-4, D-4).** A scored plan is refused unless: `scored` is
    explicitly set, since no other input implies it; `runner-class` and
    `runs-on-label` are present and non-empty (**AD-8**); the run's `cache_state` is declared
    (**AD-9**); and `candidate-sha` and `workload-commit` are present (**AD-10**) so the observation
    can carry the three separate identities QC15 requires. All five are **plan** inputs, so every
    one of AD-8…AD-10 is enforceable at the component that must refuse before emitting a matrix.

### 7.1 Ingest qualification checks — QC1…QC17, stated here (R14-F1, owner F1)

**These are the checks.** Earlier drafts placed them in the companion and reached them only by
following a citation out of this contract; they are stated here, and the companion section is a
derivation that records each check's producer and history. An observation is ingested only if **every** applicable
check passes. Field names are the field registry's; this table does not restate the registry.

| # | Where it runs | Check |
|---|---|---|
| QC1 | record | `schema` recognised |
| QC2 | record | `repository`, `head_sha`, `run_id`, `run_attempt`, `job_id`, `bucket_name`, `plan_digest`, `comparability_key_digest`, and `profile.est_basis` present and non-empty. `est_basis` is read from the profile object; there is no top-level field |
| QC3 | record | `plan_digest` equals the digest of the shard plan this record job read once |
| QC4 | record | `bucket_name` resolves in that plan and `bucket_index` agrees |
| QC5 | record | `unit_ids` equals the plan's unit set for that bucket, exactly |
| QC6 | record | invocation membership and order equal the plan's rendered invocations; each `argv_digest` matches |
| QC7 | record | the §3.1 interval invariants hold; no endpoint copied between records |
| QC7a | record | `cwd_digest` matches and `process_group_id` is well-formed |
| QC8 | record | `boot_id_start == boot_id_end` |
| QC9 | record | `terminal == "passed"`, `exit_code == 0`, every invocation `exit_code == 0` |
| QC10 | record | the reporter-event coverage audit for that bucket passes |
| QC11 | record | exactly one observation for `(head_sha, run_id, run_attempt, bucket_name)`; a duplicate rejects all |
| QC12 | record | `comparability_key_digest` agrees with the plan document; `observed_runs_on_label` equals the key's `runner_image_label`; `profile.file_parallelism == 1` for any row that will train |
| QC13 | record | `profile` is **byte-identical** to the plan's canonical block — no field added, dropped, reordered, or retyped. Mandatory for every wall-basis observation and every campaign row; it is in no optional list |
| **QC14a** | **bucket runner, inside `run-bucket`, before artifact upload** | the on-runner half of the cache contract — §10.5.6 |
| **QC14b** | **record, at ingest** | the transferable half of the cache contract — §10.5.6 |
| QC15 | record | `head_sha`, `candidate_sha`, and `workload_commit` are each present, well-formed, and distinct fields; a row carrying one value in all three, or omitting one, is rejected rather than silently overloaded |
| QC17 | record | the row's `runtime_profile_digest` equals the plan document's `runtime_profile_declared_digest` (§15.3a). On mismatch the row is **rejected** and the failure names the **first differing constituent** by field number; the row never enters history, and a scored run's pair is **retained, unscored and non-passing** under §19.8's post-start rule — never voided and never rescheduled (owner F1, R15-F3) |
| QC16 | record | `realtime_start` is present and parses as an RFC 3339 UTC instant — absent or unparseable is **rejected**, never defaulted — and the row's `intrinsic_id` `(repository, run_id, run_attempt, job_id, bucket_index, plan_digest)` is complete and **not already present in the ring**, so the recency key is a total order |

**"QC1–QC17" names this set including QC7a, QC14a and QC14b.** **`QC14` remains the name of the
cache-state check as a whole**; what R14-F2 removed is the idea that it runs in **one** place. Where
a document must say *where* the check happens it names the half — `QC14a` on the bucket runner,
`QC14b` at ingest — and where only the subject matters, `QC14` still names the pair.

## 8. Compatibility that must not regress

1. **v0.2.2 exact-path atoms.** Conservative over-grouping: when two paths cannot be told apart,
   co-schedule them, so an over-match never crosses an invocation boundary. Every file enters the
   argv as a `./x` path token; an atom is never split across invocations or buckets. Byte-identity
   with Vitest 4.1.10's internal matcher is **not** claimed.
2. **Audit and coverage.** The plan-time never-drop-a-test gate, record-time coverage audit, and
   single-read per-bucket audit are retained. A wall row is learnable only if its bucket audit
   passes.
3. **baml-rest (Go).** Its sole gate at `ff3012b1` has **no ordinary source path from this
   branch**: its action source is commit-pinned at `551d49ce`, and its installer *requests* the
   `v0.1.1` release tag. Those two halves have different strengths and are not conflated — the
   action tree is content-immutable; the binary is a name resolved at run time, so a moved release
   tag is the one way the isolation could weaken, and that is a deliberate owner release action
   visible in the release history, not a consequence of this work. Within testbucket, K=6, `-race -count=100`, `-p=1`, count
   shards, `needs_node`, Go events and audit, and default rendered Go bytes are unchanged. A future
   upgrade needs its own compatibility run: `--file-parallelism` did not exist at v0.1.1.
   baml-rest is **not** required to adopt this Vitest-only work.
4. **Mandel unit-only safety.** K=8, `count=1`, `file_parallelism=1`, the façade,
   `TB_DISCOVERY_EXCLUDE_PREFIXES=shared/f/lib/cases/`, the façade's offline seal, the misc lane,
   and the fail-closed aggregate gate are unchanged. The unit-only predicate is **project-based**,
   not path-based: "no discovered path under `integration-tests/`" is wrong, because the legitimate
   `harness-unit` project lives there. **Mandel's own repository is not modified by the campaign**
   (§10).
5. **Matrix interface.** `est_seconds` numeric and one-decimal; `needs_node` unchanged; new fields
   additive.
6. **SemVer.** Additive fields and a store migration preserving reporter rows are a minor release.

## 9. Evidence

**Primary.** The `testbucket.wall-observation/v1` documents written by testbucket's own monotonic
wrappers.

**Corroborating.** The raw GitHub Actions job log and run/job/artifact metadata for the same
execution, bound to `head_sha`, `run_id`, `run_attempt`, `job_id`, `artifact_id`, and the SHA-256
the operator computes over the downloaded bytes at download time. **Every campaign date is an
authenticated date** from that metadata, normalised to UTC — never a self-reported or unsigned
field.

**Sufficiency.** For this non-hostile claim, exact content digests, terminal coverage, and retained
CI artifacts are sufficient provenance. Additional principals, immutable storage, or more observers
would strengthen tamper checks; they would not make an aggregate observation more granular and are
therefore no remedy for the unit-label gap of assumption A-35. Adopting them is a separate product decision
tied to an explicit hostile-runner threat model (§12).

**Claims permitted.** That the downloaded bytes hashed to a stated digest at a stated time; that
GitHub reported a stated run/job/commit identity and start time; that the elapsed value came from
`CLOCK_MONOTONIC` reads, with `V[j]` and `A` distinguished (SR-9): **`V[j]`** is bracketed by two
reads **in one process lifetime** around an owned, drained child; **`A`** is bracketed across **two
CLI invocations sharing one boot identity** — `wall begin` persists `A_start` and a boot
fingerprint, `wall end` verifies the same fingerprint and subtracts. A boot mismatch is a hard
error, and endpoints are never caller-supplied.

**Claims forbidden.** That artifacts or logs are immutable, permanent, tamper-evident, or
externally attested; that a signature proves an execution environment or reviewer approval; that a
process group proves containment against a detaching descendant; that the collision matcher is
byte-identical to upstream Vitest; that any measurement is the composite-action step duration, the
complete action, or the job duration; that the campaign order is randomized; that the campaign
establishes statistical significance, power, or any conclusion beyond the measured sample; that the in-tree
consumer snapshots prove the consumers' live state; that allocation is outcome-free, free of
outcome-derived influence, or mechanically pre-campaign beyond what §6.2 and §6.4's mandatory
fields establish.

## 10. Frozen campaign

**Two arms, one candidate, one bound difference.** `B` and `C` are the same testbucket commit,
composite actions, binary digest, instrumentation binary, orchestration identity, workload
identity, candidate identity, store bytes, discovered unit set, **expanded unit set**, declared
**runner class and `runs-on` label**, and declared **`cache_state`** (§10.5.0). The only
permitted difference is the bound `est_basis` field:

| Arm | `est_basis` |
|---|---|
| `B` | `reporter` — legacy reporter-weight allocation |
| `C` | `wall` — allocation from the fitted model |

Both arms run under measurement, so `A` is observed identically and the comparison is of
allocation weight, not instrumentation and not topology. The pair invariant tuple of
§19.2 is asserted mechanically; a pair failing it is **not scored**, and so is a pair
whose two arms disagree **index-wise on any of the `bc_inv` cache leaves** —
`dependency_cache_mode`, `dependency_cache_primary_key`, `transform_cache_mode`,
`mongo_binary_sha256`, `dependency_cache_producer` (§10.5.0, §17.19). `dependency_cache_matched_key`
and `dependency_cache_disposition` are recorded and QC14-checked per row, and are **not whole-run**
cross-arm invariants — but §19.2b compares them **index by index** across a scored pair's two arms,
and one unequal index leaves the pair unscored. An earlier revision said they are never compared
across arms at all, which contradicted §19.2b (R15-F2).

**Cache state is bound, and what is not bound is named (S-5).** Pinning the timing store's bytes
binds the *model input*; it does not bind the **runtime cache state** that changes the measured
process lifecycle. Every scored run therefore declares the declaration leaves of §10.5.0 and records the
outcome leaves of §10.5.0, produced as §10.5.3 requires; a prefix-fallback match fails the row, and the executed
MongoDB binary is bound to the hashed path by §10.5.5.
Ordinary page-cache and filesystem warmth remain **unbound** and are a stated paired-run limitation
controlled by counterbalancing — not a claim of physical identity, and requiring no cgroups,
protected environments, or immutable storage.

**Orchestration identity is separate from workload identity, and Mandel is not modified.**
Neither exact consumer uses this line: Mandel `d9ae1d43` pins v0.2.2, baml-rest `ff3012b1` pins
v0.1.1. Adopting in-repository would create a new Mandel commit and change the frozen workload
identity. Therefore the campaign runs from an **external digest-bound orchestrator** that checks
out Mandel at exactly `d9ae1d433bb45012c04d567879b66fc4bf6112c6` and drives the candidate
testbucket against it. The manifest records `orchestration_repo`/`orchestration_commit` and
`workload_repo`/`workload_commit` as **separate mandatory fields**, both equal across arms. If instead a new Mandel adoption revision is used, that commit becomes the
workload identity, and the consumer's unit-only safety audit **must be re-derived at that commit**
before any scored run; the audit is a project-based predicate over the consumer's Vitest projects.
How it was derived at the pinned commit is recorded in §20.4.

Until the campaign has run, "the practical wall-time product is exercised by an exact consumer" is
unproven and is not claimed.

**Exact profile, enforced at artifact validation, against the complete planned bucket set:**

```
runner_token      = vitest
K                 = 8
count             = 1
file_parallelism  = 1
one authenticated terminal row for EVERY bucket identity (index and name) in the eight-bucket plan
plan_digest       identical across all eight observations of one arm-run
```

Eight *distinct* rows are not sufficient: the rows must be the complete planned set, so a `K > 8`
plan cannot contribute a favourable eight.

**Store eligibility: warm-only**, delivered as a pinned artifact verified by digest after
download, never through a cache restore-key that can fall back by prefix with the matched key
uncaptured. A cold store cannot run wall basis at all.

**Population.** Five pre-declared matched pairs; **ten** workflow runs; eight buckets per run;
**80 `A` rows in total — 40 per arm**; at least **three distinct authenticated UTC dates**; span
between earliest and latest authenticated starts **≤ 14 days**. Any restatement as "80 per arm" is
false; the source comment that says so is a defect listed in §16.4.

**Order.** The fixed precommitted counterbalanced sequence of §0.2 — pairs 1, 3, 5 run B→C and
pairs 2, 4 run C→B — declared in the manifest before the first run. Not randomized; no seed, draw,
or shuffle appears anywhere.

**The declared order is verified against execution as a SEQUENTIAL order, not merely a launch
order (S-9).** Two properties were previously conflated:

| Property | Predicate | What it supports |
|---|---|---|
| launch order | `started_at(first) < started_at(second)` | which arm was dispatched first |
| **sequential order** | **`completed_at(first) ≤ started_at(second)`** | the arms did not overlap — the property counterbalancing needs to control within-pair drift |

Two jobs can satisfy launch order while running concurrently on different runners, which is not the
sequential execution the drift-control claim rests on. **The campaign requires sequential order.**
For each pair, both instants come from the Actions API (§9): the first arm's authenticated
`completed_at` must be less than or equal to the second arm's authenticated `started_at`, in the
direction the declared sequence states. A pair whose authenticated instants contradict the declared
sequential order — including an overlap — **fails the campaign**; an empty or unparseable instant is
a failure, never a skipped comparison. Where a document means only dispatch it says **launch order**
and makes no drift claim.

**Dates.** Each scheduled date is compared against the arm's **authenticated** run date, parsed as
`YYYY-MM-DD` in UTC. A scheduled date matching no authenticated run date fails the campaign.

**Statistics.** Per run `Amax = max_b A`, `TA = Σ_b A`, `DA = Amax / median_b(A)`; per pair
`RA_i = Amax[C_i]/Amax[B_i]`. Median for even *n* is the conventional arithmetic mean, computed in
exact integer nanoseconds with rational comparison. **No outlier deletion, no rounding allowance,
no retry or replacement after either arm starts a bucket script.**

| Gate | Requirement |
|---|---|
| balance / improvement | `median(RA) ≤ 0.95` and ≥ 4 of 5 pairs with `RA ≤ 1.00` |
| tail non-regression | every `RA_i ≤ 1.10` and `max_i Amax[C_i] ≤ 1.05 × max_i Amax[B_i]` |
| equality within a run | `median(DA[C]) ≤ 0.95 × median(DA[B])` |
| total cost | `median(TA[C]) ≤ 1.05 × median(TA[B])` |
| calibration, absolute (C's 40 rows, integer ns) | in integer nanoseconds: `MAE(\|A_eta_ns − A\|) ≤ 10.000 s` **and** every `\|A_eta_ns − A\| ≤ 20.000 s` |
| calibration, relative (C's 40 rows, integer ns) | in integer nanoseconds: `MAE(\|A_eta_ns − A\|) ≤ 5.0 % × mean(A)` **and** every `\|A_eta_ns − A\| ≤ 10.0 % × mean(A)`, where `mean(A)` is the arithmetic mean of the 40 observed `A` values. Formula and percentage precommitted; numerical bound evaluated from the observed C rows (§0.4) |
| profile | the complete-planned-bucket-set block holds for every arm-run |
| pair isolation | the §19.2 invariant tuple holds for every pair |
| provenance | cutoff precedes the first authenticated start; every exclusion domain named |
| dates | every scheduled date equals an authenticated run date; ≥3 distinct authenticated UTC dates; ≤14-day span |
| sequential order | for every pair, authenticated `completed_at(first) ≤ started_at(second)` in the declared direction (§0.2, §19.5) — **launch order alone does not satisfy this gate** (S-9) |
| cache state | every scored row passes QC14 and both arms of every pair agree **index-wise** on the `bc_inv` cache leaves (§10.5.0, §17.19) (S-5/D-5) |
| model freeze | the four coefficients were fitted only from rows the pre-campaign selection retained; **no campaign row entered a fit** (§0.4, §6.4b, §15.1b) (S-2) |
| integrity | every **scored** row passes QC1–QC17; every harness run the Actions API returns for the window appears in the **attempted** population with a disposition (§19.4a) |

**This table is the only gate table in the package (R13-A).** §19.7 formerly carried a
second copy, and `TestThresholdsAreFrozenBeforeRunOne` existed to stop the two from drifting — a
test made necessary only by the duplication. The duplicate is removed. That test now asserts the
stronger property: this table and §10.4 are the sole statement of the campaign gate set and its
numeric values, and **no other document states a gate label, threshold, or comparison operator**. A
gate restated elsewhere fails it. The three rows marked S-9, S-5, and S-2 were introduced by the
tail repair and live here, once.

**`integrity` says "every scored row", not "every row" (§19.4a).** Failed, cancelled, and malformed
observations are deliberately **retained as diagnostics** and by construction do not pass QC — a gate
demanding that *every* row pass QC1–QC17 would be unsatisfiable whenever a diagnostic row exists. The
scored population is what the gate governs; the attempted population is governed by the accounting
clause beside it.

**What this establishes.** An **engineering release gate** on one frozen workload, over exactly the
eighty rows measured (§0.3). It is not a statistical significance test, and it makes no claim about
any run, workload, or period outside the measured sample. A PASS is a statement about those ten
runs; a FAIL is a statement about those ten runs. §19.6 explains the engineering
motivation for the numbers, and §10.4 fixes every value before the first run.

### 10.4 Frozen threshold table — every value, fixed before the first run

**What is prohibited is selection, retuning, and refitting — not evaluation (§0.4, D-3).** No
constant below may be chosen, tuned, or adjusted from pilot or campaign outcomes, and no coefficient
may be refitted on campaign rows. The two relative calibration rows are frozen as *formula and
percentage*; their numerical right-hand side is **evaluated** from the observed C rows, because a
relative gate has no numeric value until `mean(A)` is observed. An earlier revision opened this
table with "no value below is computed from campaign outcomes" and then stated that evaluation two
paragraphs later — a contradiction on its face. Evaluating a frozen formula is not computing a
threshold from an outcome.

| Symbol | Value | Applies to |
|---|---|---|
| pairs | 5 | campaign population |
| runs | 10 | campaign population |
| rows | **80 total = 40 per arm** (never 80 per arm) | campaign population |
| complete buckets per run | 8, indices 0–7, each exactly once | `K` |
| distinct authenticated UTC dates | ≥ 3 | campaign window |
| campaign window | ≤ 14 days, earliest to latest authenticated start | campaign window |
| `median(RA)` | ≤ 0.95 | balance / improvement |
| pairs with `RA ≤ 1.00` | ≥ 4 of 5 | balance / improvement |
| every `RA_i` | ≤ 1.10 | tail non-regression |
| `max_i Amax[C_i]` | ≤ 1.05 × `max_i Amax[B_i]` | tail non-regression |
| `median(DA[C])` | ≤ 0.95 × `median(DA[B])` | equality within a run |
| `median(TA[C])` | ≤ 1.05 × `median(TA[B])` | total cost |
| calibration MAE, absolute | ≤ 10.000 s | C's 40 rows |
| calibration worst case, absolute | ≤ 20.000 s | C's 40 rows |
| calibration MAE, relative | ≤ 5.0 % × `mean(A)` | C's 40 rows |
| calibration worst case, relative | ≤ 10.0 % × `mean(A)` | C's 40 rows |
| `MIN_ROWS` | 24 | model sufficiency |
| `MIN_RUNS` | 3 | model sufficiency |
| `W` (observation ring) | 240 | model fitting |
| `MODEL_MAE_CEILING` | 20.000 s | model status `degraded` |
| platform voids permitted | ≤ 2; a third ends the campaign | platform failure |
| `file_parallelism` under wall basis | exactly 1 | validity condition |

Both calibration pairs are **conjunctive**: absolute *and* relative must hold. The absolute bound
binds when buckets are small; the relative bound binds when they are large, which is what makes
"extremely close" scale-independent rather than an artifact of this workload's size.

**Precommitted formula, evaluated bound (S-2).** For the two relative rows, the **formula and the
percentage** — `5.0 %` and `10.0 %` of `mean(A)` — are fixed in this table before run 1; the
**numerical right-hand side** is evaluated from the 40 observed C rows, in integer nanoseconds,
because a relative gate has no numeric value until `mean(A)` is observed. That evaluation is not
retuning and is not derivation of a threshold from an outcome. Changing a percentage or a bound
after seeing a result, or refitting a coefficient on campaign rows, **is** forbidden, and is what
`TestThresholdsAreFrozenBeforeRunOne` and `TestCampaignRowsNeverRefitTheModel` reject. All absolute
values in this table are fixed numbers.

**Intention-to-treat and platform failure.** Once either arm of a pair has started any bucket
script, every attempt in that pair is retained and counted. A run failing **before any bucket
script starts in either arm** is a **platform void**: retained with terminal `void` and a reason,
and the **same precommitted pair is rescheduled** — pair index, arm order, workload, mode, and every
invariant value unchanged. A rescheduled pair is never re-drawn and never re-selected. At most
**two** reschedules per campaign; a third void ends it as
`NEEDS_EVIDENCE`. A bucket failing **after** its script started is retained, unscored, and makes
its pair non-passing. `run_attempt > 1` never replaces attempt 1; every harness run in the window
is enumerated from the Actions API, and an unaccounted run **voids the campaign**.

**Decision.** PASS requires every gate in §7, §8, and §10. Any unmet gate is FAIL or
NEEDS_EVIDENCE.

### 10.5 Cache declaration — transport, outcome production, and binary binding (R13-D2)

**This section is the single normative authority for the `cache_state` transport and state
machine.** The companion derivations at `scope.md` §8.4 and `scope.md` §8.4a, and the
`component-map.json` record, note where each leaf is written and name the acceptance tests; neither
restates the tuple table or the transport protocol.

The declaration/outcome split of §10.5.0 was algebraically right and **physically unexecutable**: it
moved bytes by passing a pathname between jobs that share no filesystem, gave two outcomes no
producer, let one immutable
config record hold eight mutable outcome sets, and hashed a MongoDB binary that nothing bound to the
binary the tests actually execute. All four are fixed here.

#### 10.5.0 The `cache_state` leaves and their legal values — stated here (R14-F1)

Every scored run declares a cache mode and records what happened. **This table is the one canonical
shape of the `cache_state` block (R15-F2).** Every leaf the field registry registers under
`cache_state` appears here exactly once with exactly one class, and every leaf here is registered
there — the two sets are compared in both directions by `TestScoredCacheDeclarationTransportAndBinaryBinding`.

**No document states a leaf count, here or anywhere.** An earlier revision described the block as
"seven leaves, four plus three" while the registry carried more, and the two drifted the moment a
leaf was added; the classes below are named, the membership is derived, and the arithmetic is left
to the checker.

| Leaf | Class | Legal values |
|---|---|---|
| `dependency_cache_mode` | declaration | `disabled` \| `exact-key`. `disabled`: nothing is restored or saved, the run installs cold. `exact-key`: a cache is restored **only** on an exact primary-key match |
| `dependency_cache_primary_key` | declaration | the exact key requested; empty **iff** mode is `disabled` |
| `transform_cache_mode` | declaration | `disabled`, and no other value is legal — Vitest's transform cache directory is fresh per run |
| `dependency_cache_producer` | **declaration** | `none` \| `action` \| `caller` — which producer of §10.5.3 owns the restore. The caller chooses this **before dispatch**, exactly as it chooses `runs-on-label`, so it is declared, not produced: an earlier revision filed it with the outcomes, which let two arms of one pair run under different producers and still pass every invariant (owner F2, R14-F2). **`none` is the required value when, and only when, the mode is `disabled`**: no restore is attempted, so no producer runs (R15-F2) |
| `expected_mongo_binary_sha256` | declaration | the digest the pinned setup composite is expected to place |
| `dependency_cache_hit` | outcome | boolean, present **iff** `dependency_cache_producer == caller`, absent otherwise. It is the caller's restore result **serialized onto the row**, so QC14b can re-derive the presence pattern instead of being asked to check an unrecorded fact (R15-F2) |
| `dependency_cache_matched_key` | outcome | the key actually matched; empty **iff** nothing matched — mode `disabled`, or disposition `miss` |
| `dependency_cache_disposition` | outcome | `disabled` \| `exact-hit` \| `miss` |
| `mongo_binary_sha256` | outcome | SHA-256 of the MongoDB binary actually executed, as §10.5.5 defines that file |
| `mongo_binary_path` | outcome | the absolute path that digest was taken over, on the runner that executed it |
| `mongo_binary_verified_on_runner` | outcome | boolean; `true` **iff** QC14a passed on that runner (§10.5.6) |

**Declaration versus outcome, and what follows from the split.** The declaration leaves are knowable
before dispatch, are identical across every bucket job of a run, and are the only ones that reach
`cache_declaration_digest` and therefore the comparability key. The outcome leaves are produced by
the bucket job on its own runner and may legitimately differ between jobs.

`dependency_cache_matched_key` and `dependency_cache_disposition` vary legitimately between bucket
jobs of one run, so they are never compared as **whole-run** cross-arm invariants. Under §19.2b mode
(b) they are nevertheless compared **index by index** across the two arms of a scored pair, because a
per-index warmth difference lands inside `A`; an unequal index leaves the pair unscored and
non-passing under §19.8, never voided. Which leaves are whole-run cross-arm invariants is the
field registry's `bc_inv` membership; this contract does not reproduce that set, and §17.19 compares
exactly the leaves the registry places in it.

#### 10.5.1 The legal dependency tuples — stated once, here

| # | `dependency_cache_mode` | `dependency_cache_primary_key` | `dependency_cache_matched_key` | `dependency_cache_disposition` |
|---:|---|---|---|---|
| 1 | `exact-key` | non-empty | **equal to** `primary_key` | `exact-hit` |
| 2 | `exact-key` | non-empty | **empty** | `miss` |
| 3 | `disabled` | empty | empty | `disabled` |

Every other combination fails QC14, including the prefix-fallback tuple a `restore-keys:` list
produces — a **non-empty** `matched_key` differing from `primary_key`. A `miss` is legal and
representable. `transform_cache_mode` is `disabled` for every scored run, and no other value exists.

#### 10.5.2 Transport — bytes and a digest, never a shared pathname

Separate GitHub Actions jobs do not share a filesystem, so passing the same *path string* to the
plan job and to eight matrix jobs proves nothing about content. The declaration therefore moves as
**content plus digest**, and byte identity is **checked** rather than asserted — **on both hops**.

**Hop 0, caller → plan (R23-F4).** The same physical premise applies across the workflow-call
boundary: a caller's pathname is only a string inside the called workflow. An earlier revision made
`cache-declaration-file` a reusable-workflow input, which reproduced exactly the shape step 3 below
rejects. The inbound hop is therefore:

| # | Step | Normative requirement |
|---:|---|---|
| 0a | **Supply** | the caller passes the canonical declaration **as content** in the workflow input **`cache-declaration-json`**, and its SHA-256 in the workflow input **`cache-declaration-digest-expected`**. No pathname crosses the boundary |
| 0b | **Materialize** | the plan job writes `cache-declaration-json` verbatim to a **job-local** file and passes that local path to the `plan` action as `cache-declaration-file`. The plan action's input is unchanged; only its producer is now defined |
| 0c | **Verify** | the plan job recomputes SHA-256 over the materialized bytes and **fails the job before invoking `plan`** unless it equals `cache-declaration-digest-expected`. A perturbation of either the bytes or the expected digest fails here, independently |

**The two digests have distinct names and distinct provenance.**
`cache-declaration-digest-expected` is a **caller-supplied input**, consumed once at step 0c and
nowhere else. `cache-declaration-digest` is a **plan-job output**, produced at step 2 below over the
bytes the plan job verified. **Matrix jobs consume the plan-job outputs only** — never the caller
inputs — so exactly one value governs every bucket job of a run:

| # | Step | Normative requirement |
|---:|---|---|
| 1 | **Author** | the orchestrator writes the canonical declaration — every leaf §10.5.0 classes as `declaration`, **in the order that table lists them**, as canonical JSON with no whitespace — exactly once, before dispatch. §10.5.0 is the only enumeration; this step fixes the order and the encoding |
| 2 | **Publish** | the plan job exposes those bytes as the **job output `cache-declaration-json`** (the canonical single-line string) and their SHA-256 as the **job output `cache-declaration-digest`**. The reusable workflow re-exposes both as **workflow outputs** of the same names. Job/workflow outputs are the transport because they cross the job boundary with no additional permission and no artifact hop |
| 3 | **Materialize** | every matrix job writes `cache-declaration-json` verbatim to a job-local file and passes that local path to `run-bucket` as **`cache-declaration-file`**, together with the expected digest as **`cache-declaration-digest`**. The pathname is job-local by construction and is never claimed to be shared |
| 4 | **Verify** | `run-bucket` recomputes SHA-256 over the materialized bytes and **fails the job** unless it equals `cache-declaration-digest`. `plan` performs the same check against what it validated under AD-9 |
| 5 | **Copy** | `run-bucket` copies the verified **declaration** leaves verbatim into the observation's `cache_state` and appends the **outcome** leaves it produced (§10.5.0) |

`cache_declaration_digest` — comparability-key leaf 15 (§15.3) — is SHA-256 over those
same canonical bytes, so the key leaf, the workflow output, and the per-job verification are one
value. A declaration that reaches a bucket job altered fails at step 4 before any bucket script
starts; it can never reach QC14 as a silent difference.

#### 10.5.3 Outcome producers — declared, not assumed

`dependency_cache_matched_key` and `dependency_cache_disposition` are results of a **restore step**.
Exactly one of two producers is used, the workflow declares which, and there is no third option:

| Producer | Shape | When it applies |
|---|---|---|
| **A — action-owned restore** | the dependency-cache restore runs **inside** `run-bucket`, which reads its own step outputs | new orchestration written for this product |
| **B — caller-owned restore** | the job restores before invoking `run-bucket` and passes that step's `cache-matched-key` and `cache-hit` outputs in as the required inputs **`dependency-cache-matched-key`** and **`dependency-cache-hit`** | the pinned Mandel workflow, which restores before the action runs |

**The choice has a wire representation, and "exactly one" is validated (R14-F2).** Saying the
workflow "declares which" was not implementable while nothing carried the declaration and nothing
could tell a caller-owned **miss** (empty matched key, no hit) from **absent producer data** (empty
matched key because nothing was passed). Both are fixed by one required input and one presence rule:

| Input to `run-bucket` | Values | Presence rule |
|---|---|---|
| **`dependency-cache-producer`** | exactly `none`, `action`, or `caller`; any other value, or absence on a scored run, **fails the job** | always required for `scored: true`. **`none` is required when the declared mode is `disabled` and is illegal otherwise; `action` and `caller` are legal only under `exact-key`** (R15-F2) |
| **`dependency-cache-hit`** | `true` or `false` — **a tri-state is not permitted**, and empty is not `false` | **required non-empty iff** producer is `caller`; **must be absent** when producer is `action` or `none` |
| **`dependency-cache-matched-key`** | the matched key, or the empty string | **required (possibly empty) iff** producer is `caller`; **must be absent** when producer is `action` or `none` |

**Every legal mode now has a producer state, which it did not (R15-F2).** An earlier revision made
the producer input mandatory with only `action` and `caller` as values while the `disabled` tuple
said "producer not invoked" — so a `disabled` run had no legal value to write. `none` is that value,
and it is required exactly where no restore is attempted.

So an empty `dependency-cache-matched-key` **with** `dependency-cache-hit: false` is a caller-owned
miss, and an empty matched key **without** `dependency-cache-hit` is absent producer data. They are
distinguishable at the input, and — because the hit is serialized as
`cache_state.dependency_cache_hit` (§10.5.0) — they stay distinguishable on the row.

`run-bucket` **fails the job, before the bucket script starts**, when: the producer input is absent
or is not one of the values §10.5.3 declares; the producer value does not match the declared mode by the rule
above; producer is `caller` and either companion input is absent; producer is `action` or `none` and
**either** companion input is present — supplying two producers, or a producer under `disabled`, is
rejected rather than silently preferred; or producer is `caller` and `dependency-cache-hit` is
`true` while `dependency-cache-matched-key` is empty. The chosen producer **and the caller's hit**
are recorded as `cache_state.dependency_cache_producer` and `cache_state.dependency_cache_hit`, so
QC14b re-derives the presence pattern from the row alone rather than being asked to attest to an
input it never sees.

`run-bucket` then derives the disposition from the declared mode and the producer's result by this
table, and by no other rule:

| Declared mode | `producer` | Producer result | `matched_key` recorded | `disposition` |
|---|---|---|---|---|
| `disabled` | **`none`** | no restore attempted; both companion inputs absent | empty | `disabled` |
| `exact-key` | `action` or `caller` | hit **true**, matched **==** `primary_key` | the matched key | `exact-hit` |
| `exact-key` | `action` or `caller` | hit **false**, matched empty | empty | `miss` |
| `exact-key` | `action` or `caller` | hit **true**, matched **≠** `primary_key` | — | **row fails** (prefix fallback) |
| `exact-key` | `action` or `caller` | hit **true**, matched empty, **or** hit **false** with a non-empty matched key | — | **row fails** (incoherent producer result) |
| `disabled` | `action` or `caller` | — | — | **row fails** (a producer under a disabled cache) |
| `exact-key` | `none` | — | — | **row fails** (no producer under an active cache) |

A scored run declaring `exact-key` for which **neither** producer supplied a result **fails
closed** before the bucket script starts. The outcomes are never inferred from the filesystem, never
defaulted to `miss`, and never omitted.

#### 10.5.4 The frozen declaration and the per-row outcomes are different records

`manifest.cache_state` in the campaign **config** is the frozen declaration and carries **exactly the
declaration leaves of §10.5.0**. It is sealed at `frozen_at` and never mutated.

**The outcome leaves live on the observation, and nowhere else (R15-F2).** One set per bucket job, so
an arm-run carries eight and a pair sixteen. An earlier revision added "and the ring rows derived
from them", which the field registry contradicts: **the ring row registers no `cache_state` path at
all**, and it is right not to. The ring exists to hold the four design columns, the response, and the
identities a re-fit needs; the cache *declaration* already reaches it through
`comparability_key_digest`, which `cache_declaration_digest` is a leaf of, and the per-job *outcomes*
are diagnostics that no fit reads. The claim is withdrawn rather than the registry extended, and the
registry remains the authority for which artifact serializes what.

One immutable config record never holds many mutable outcome sets, and the field registry registers
the two shapes distinctly.

#### 10.5.5 The hashed binary is the executed binary

`mongo_binary_sha256` is SHA-256 of **the file named by the `MONGOMS_SYSTEM_BINARY` environment
variable as exported into the environment the façade inherits** — the same value the pinned façade
selects and validates before passing it to Vitest as `systemBinary`
(`testdata/consumers/mandel/scripts/run-unit-tests.ts`). It is **not** a path `run-bucket`
recomputes, re-derives, or discovers by search.

`run-bucket` additionally records that resolved absolute path as the diagnostic
`cache_state.mongo_binary_path`, so the audit trail names the file that was hashed. **QC14a fails
the job** — on the bucket runner, before upload — when `MONGOMS_SYSTEM_BINARY` is unset, empty, or
names a file that does not exist, or when the recorded digest is not the digest of the file at the
recorded path. §10.5.6 states why that check cannot be deferred to ingest.

`TestScoredCacheDeclarationTransportAndBinaryBinding` (`scope.md` test 70) injects a **decoy**:
two distinct MongoDB binaries with distinct digests, `MONGOMS_SYSTEM_BINARY` pointing at the
executed one and the decoy resident elsewhere in the workspace. The test fails unless the recorded
`mongo_binary_sha256` is the executed binary's, and asserts that a run hashing the decoy is rejected
**by QC14a, on the bucket runner,** even when the decoy digest equals
`expected_mongo_binary_sha256`.

#### 10.5.6 QC14a and QC14b — the check split across the job boundary (R14-F2)

An earlier revision defined one check, `QC14`, that required `mongo_binary_path` "to name an existing
file" and to rehash it. **That check cannot run where it was placed.** The observation is produced on
a *matrix* runner, uploaded, and ingested by the *record* job on a different runner. The recorded
absolute path names the executed binary on the matrix runner; a `stat` in the record job would
inspect an unrelated filesystem, and a coincidentally identical path there would bind nothing. The
check is therefore split at the boundary it actually crosses.

**QC14a — on the bucket runner, inside `run-bucket`, before the observation is uploaded.** This is
where the file exists, so this is where the binding is established:

| # | QC14a requires |
|---:|---|
| 1 | `MONGOMS_SYSTEM_BINARY` is set and non-empty in the environment the façade inherits |
| 2 | the file it names **exists and is readable** |
| 3 | `mongo_binary_sha256` is the SHA-256 of **that** file, and `mongo_binary_path` is its absolute path |
| 4 | `mongo_binary_sha256 == expected_mongo_binary_sha256` |
| 5 | the **input** presence rules of §10.5.3 hold — which inputs were supplied, and whether the producer value matches the declared mode — and the resulting tuple is one of §10.5.1's |
| 6 | the producer and, under `caller`, the hit are written onto the row as `cache_state.dependency_cache_producer` and `cache_state.dependency_cache_hit`, so what QC14a validated **travels** |

Any failure **fails the job** before upload. A failing run therefore produces no scored observation
at all, rather than an observation that a later job is asked to disbelieve.
`mongo_binary_verified_on_runner` is written `true` **only** on the path where **every requirement
in the table above** passed — all six, including the serialization step, so the witness the record
job later trusts is itself covered by the verdict it carries (R16-F2: this sentence said "all five"
while the table listed six, which left it ambiguous whether requirement 6 was inside the verdict). A
`run-bucket` that writes `true` without performing them is non-conformant.

**QC14b — in the record job, at ingest.** It validates exactly what travels with the observation, and
**nothing that requires the matrix runner's filesystem**:

| # | QC14b requires |
|---:|---|
| 1 | the assembled tuple is one of §10.5.1's, with `transform_cache_mode == "disabled"` |
| 2 | `expected_mongo_binary_sha256` and `mongo_binary_sha256` are both present, well-formed, and **equal** |
| 3 | the declaration leaves of §10.5.0 are byte-identical to the digest-verified declaration `plan` validated (§10.5.2) |
| 4 | `dependency_cache_producer` is present and legal, **and agrees with `dependency_cache_mode`** by §10.5.3's rule — `none` under `disabled`, `action` or `caller` under `exact-key` |
| 5 | `dependency_cache_hit` is **present iff** the producer is `caller` and **absent otherwise**, and — when present — is coherent with `dependency_cache_matched_key` and `dependency_cache_disposition` by §10.5.3's derivation table |
| 6 | `mongo_binary_path` is present, non-empty, and an **absolute** path — a **syntactic** check only |
| 7 | `mongo_binary_verified_on_runner == true` |

**Every one of those is decidable from the serialized row (R15-F2).** An earlier revision asked
QC14b to re-check *which inputs the caller supplied* while the row carried no witness of them —
a check no implementation could perform, and one this contract nonetheless required. The repair is
to serialize the witness: `dependency_cache_hit` is now a registered outcome leaf, so the
presence pattern QC14a validated at the input boundary is re-derivable at ingest instead of
asserted. What QC14b still **cannot** do is inspect the runner's filesystem, and it does not
pretend to.

**QC14b must not `stat`, open, or re-hash `mongo_binary_path`.** An implementation that does is
non-conformant: it would either fail every legitimate row or, worse, pass on an unrelated file of the
same name.

**What this does and does not establish, stated plainly.** Two different things travel here and they
are not equally strong. The producer pattern and the cache tuple are **serialized facts** that QC14b
re-derives from the row. The binding between the executed binary and the recorded digest is
**established on the runner that executed it** and is then *reported*, because the file exists
nowhere else.
Under the trusted-CI threat model of §0.1 a `run-bucket` that reports
`mongo_binary_verified_on_runner: true` is trusted exactly as far as every other self-reported
observation field — `elapsed_ns` included — and no further. This is a **reporting** boundary, not an
attestation, and no document may describe it as one.

**The rejected alternative, and why.** Uploading the MongoDB binary itself with each observation
would let the record job re-derive the digest from bytes it holds. It is rejected on cost: the binary
is on the order of a hundred megabytes and there are eight bucket jobs per arm-run, so a five-pair
campaign would move roughly ten gigabytes to strengthen a boundary the threat model already treats as
trusted. If the threat model is ever reopened, this is the mechanism to reinstate.

`TestScoredCacheDeclarationTransportAndBinaryBinding` (test 70) asserts the split directly: the
decoy-binary case fails **in QC14a on the bucket runner**, and a record-job ingest that stats or
rehashes `mongo_binary_path` **fails the test**.

## 11. Reviewability

This contract, `scope.md`, `assumption-ledger.md`, `salvage-map.md`, `source-to-claim-map.md`,
`salvage-audit.md`, and `component-map.json` ship **in the repository** at `docs/walltime/` as the
first step of implementation, after consecutive reviews correctly reported that the brief was not
`jj`-addressable. Snapshots of both consumers' relevant source, at named commits with per-file
digests, ship at `testdata/consumers/` in the same step.

The inventory is **enumerated from `jj status` and `jj file list -r @` at read time** by
`TestScopeArtifactConsistency`, never from a path count written in a document (S-10).

## 12. Explicitly out of contract, and the evidence that would reopen it

Out: protected-environment authority; multi-principal signatures, countersignatures, key rosters,
one-shot durable claims; object-lock or permanent evidence stores; triple physical/peer/trace
observer ceremony; cgroup-v2 delegation, distinct-UID execution, and hostile-descendant containment
as eligibility prerequisites; fleet image attestation; Stage-1/Stage-2 sealed derivation; replay
and build attestation; candidate release resolvers and digest pin files; exhaustive package-source
and release-chain closure; twelve ablations across four strata; the frozen Vitest 4.1.10 study pin.

The **mechanical descendant-drain** role the cgroup also served is *kept*, via owned-process-group
root wait and reap, same-PGID signal, and group drain (§3.3). What is dropped is hostile containment
as an eligibility gate.

**Re-entry is by measurement, not hypothesis (§0.1).** The governing rule is the owner's: a removed
control returns only when a **measured timing-bias result from an actual hostile environment**
reopens it. Instances that would qualify: detached descendants observed surviving process
completion and biasing `V` on this consumer's workload; measured clock instability; a demonstrated
artifact mutation; a demonstrated forged row. A hypothetical attacker, or a preference for more
signatures, is not evidence and does not reopen it.

NEEDS_EVIDENCE

---

<!-- R15-F1: sections 13-22 were carried here from the companion; the crosswalk is in 0.10. -->
<!-- Inside them a bare section reference is a section of THIS contract; the companion is
     always written `scope.md` §N and points to rationale or a machine registry. -->


## 13. Observation schema

One document per bucket, written atomically, uploaded under `if: always()`. Fields as in PWT-3 plus
the profile fields carried for artifact-level validation:

### 13.0 The canonical `profile` type (S4)

**One type, three consumers.** The plan emits it, every observation copies it **verbatim**, and the
validator compares the two. It is defined here and nowhere else.

```json
"profile": {
  "scored": true,
  "runner_token": "vitest",
  "k": 8,
  "count": 1,
  "file_parallelism": 1,
  "bucket_indices": [0,1,2,3,4,5,6,7],
  "est_basis": "reporter|wall",
  "store_sha256": "sha256:…",
  "expanded_unit_set_digest": "sha256:…"
}
```

Every field of the canonical profile is frozen at plan time; the block's membership is the field
registry's `profile` projection and is **not** counted here (R14-F4). `bucket_indices` is the
complete index set, not a count.

**`est_basis` has two distinct fields with two distinct roles (SR-5).** They are not a conflict;
naming them as one was.

| Field | Role (`scope.md` §2A) | Where | Meaning |
|---|---|---|---|
| `profile.est_basis` | `ADMISSION` | inside the canonical profile, on the plan and **every observation** | **the source of truth**, set when the observation is recorded; QC reads it from here |
| top-level `est_basis` | `OUTPUT_METADATA` | plan document and each matrix entry | **additive output** for display and consumer use (PD-1), *derived from* `profile.est_basis` and separately serialized |

An observation carries **only** `profile.est_basis`; there is no top-level observation field. The
plan and matrix carry **both** — the profile block for authority, the top-level field for
consumers, and they must agree (QC13 for the profile,
`TestEveryExposedEstimateDeclaresItsQuantity` for the pair).

An observation that alters any byte of the block it was given is rejected by **QC13**, which is
**mandatory for every wall-basis observation and every campaign row** and appears in no optional
list. That is what makes §19.3a's plan-time admission checkable at run time rather than merely
declared.

**Every path below is declared in the `scope.md` §2A wire-path registry, and every observation path in that
registry appears below.** `TestFieldRegistryCoversEverySerializedPath` compares the two sets in both
directions, so a field cannot be added to one without the other. There is **no loose top-level
`runner_token`**: the profile block is the only representation of the profile fields.

```json
{
  "schema": "testbucket.wall-observation/v1",
  "comparability_key_digest": "sha256:…",
  "repository": "owner/name",
  "head_sha": "<40 hex — the ORCHESTRATION head commit>",
  "candidate_sha": "<40 hex — the testbucket commit this binary was built from>",
  "workload_commit": "<40 hex — the consumer checkout this bucket executed>",
  "run_id": "…", "run_attempt": "1", "job_id": "…",
  "bucket_index": 0, "bucket_name": "bucket-0",
  "plan_digest": "sha256:…",
  "profile": { "…": "the canonical block of §13.0, copied verbatim from the plan" },
  "est_seconds": 412.7,
  "a_eta_ns": "412700000000",
  "process_group_id": "…",
  "actual_runner_name": "GitHub Actions 7",
  "observed_runs_on_label": "ubuntu-24.04",
  "unit_ids": ["src/a.spec.ts", "src/b.spec.ts"],
  "invocations": [
    {"seq": 0, "units": ["…"], "argv_digest": "sha256:…", "cwd_digest": "sha256:…",
     "selector": ["…"], "atoms": ["…"], "process_group_id": "…",
     "started_mono_ns": "…", "ended_mono_ns": "…", "elapsed_ns": "…", "exit_code": 0}
  ],
  "started_mono_ns": "…", "ended_mono_ns": "…", "elapsed_ns": "…",
  "setup_ns": "…", "script_ns": "…", "script_overhead_ns": "…", "wrapper_ns": "…",
  "boot_id_start": "…", "boot_id_end": "…",
  "realtime_start": "…", "realtime_end": "…",
  "campaign_id": "cmp-2026-09-mandel-d9ae1d43",
  "cache_state": {
    "dependency_cache_mode": "disabled|exact-key",
    "dependency_cache_primary_key": "…",
    "dependency_cache_matched_key": "…",
    "dependency_cache_disposition": "disabled|exact-hit|miss",
    "transform_cache_mode": "disabled",
    "dependency_cache_producer": "caller",
    "dependency_cache_hit": false,
    "mongo_binary_sha256": "sha256:…",
    "mongo_binary_path": "/tmp/mongodb-binaries/mongod-x64-ubuntu-7.0.14",
    "mongo_binary_verified_on_runner": true,
    "expected_mongo_binary_sha256": "sha256:…"
  },
  "cache_declaration_digest": "sha256:…",
  "runtime_profile": {
    "node_version": "…",
    "pnpm_version": "…",
    "vitest_version": "…",
    "testbucket_sha256": "sha256:…",
    "facade_command": "…",
    "lock_sha256": "sha256:…",
    "dependency_cache_mode": "disabled|exact-key"
  },
  "runtime_profile_digest": "sha256:…",
  "terminal": "passed", "exit_code": 0, "failure_reason": "",
  "limitations": [
    "V is the Exec-envelope interval: it contains wrapper and containment overhead, and excludes wall-exec CLI startup and the spec-file write",
    "invocation time includes the consumer's whole spawned command chain, not Vitest alone",
    "root child waited and reaped; process group signalled and drained where the platform permits probing. A setsid/double-forked descendant leaves the group and is neither signalled nor drained; descendants are never reaped.",
    "this is the instrumented run-bucket interval, not the complete action and not job wall time",
    "est_basis selects the partition weight only; unit topology is store-derived in both bases",
    "runner_image_label is a mutable name, not an image identity; actual_runner_name is a diagnostic and is not part of the comparability key",
    "cache_state records the declared and matched cache disposition; page-cache and filesystem warmth are not bound and remain a paired-run limitation",
    "runtime_profile records the versions and digests this bucket actually executed; QC17 compares it field-by-field against the plan document's declared object"
  ]
}
```

- `profile` is the canonical block of §13.0, copied **verbatim**; the profile fields are **not** loose
  top-level keys, so there is one representation and one place to change it. In particular the
  observation carries `profile.runner_token`, never a top-level `runner_token`.
- **Three identities, never one (S-6).** `head_sha` is the **orchestration** head commit as GitHub
  authenticated it; `candidate_sha` is the **testbucket** commit the executing binary was built
  from; `workload_commit` is the **consumer** checkout the bucket ran against. Each is recorded
  separately and none substitutes for another. §15.1a defines what each excludes.
- `a_eta_ns` is the integer-nanosecond prediction the plan made for this bucket, echoed so
  calibration residuals are computed in the same units as the response. `est_seconds` is its
  display rounding, `round1(a_eta_ns / 1e9)`, and is never a model input (S-1).
- `actual_runner_name` and `observed_runs_on_label` are **diagnostics** (S-4). The runner instance
  name is deliberately not a comparability leaf; the observed `runs-on` label is compared against
  the key's `runner_image_label` at admission (§15.3, QC12).
- `cache_state` is the scored cache-state block of §10.5.0, checked by **QC14a** and **QC14b**.
- `campaign_id` is **present and non-empty on this example because `profile.scored` is true** — §21
  requires a non-empty `campaign-id` for every scored run. An **ordinary unscored** observation
  **omits the field entirely**; it is never serialized empty (R10-D3).
- `cwd_digest` and `process_group_id` are defined precisely in §13.1; §18.0's claims are scoped to
  exactly what they carry.
- `plan_digest` is SHA-256 over the canonical bytes of the shard plan the record job read **once**.
- `realtime_*` are diagnostics; never a duration and **never a campaign date, cutoff, or window
  source** — those are always the authenticated Actions API instants of §18.2. `realtime_start` has
  exactly one further use: it is copied into the ring as `observed_start_realtime`, the recency
  stamp of §15.1b, which orders a training ring and decides nothing else (S-6).
- **All `*_ns` values are integer nanoseconds serialized as decimal strings.** No field in this
  document is in seconds except `est_seconds` (S-1).
- `limitations` is required and non-empty.

### 13.1 `cwd_digest` and `process_group_id` — exact definitions (F5)

**`cwd_digest`.** SHA-256 over the **executed absolute working-directory path**, as a UTF-8 string
with no trailing separator. Two recording points, deliberately different:

| Recorded | When | By |
|---|---|---|
| plan side | at **plan-creation** time, the absolute path the renderer resolved for that invocation's `Dir` | the planner, into the shard plan |
| observation side | at **execution** time, the absolute path the wrapper's child actually ran in | the wrapper, into the observation |

**QC7a compares the two.** They must be equal. A mismatch means the bucket ran somewhere other than
where it was planned, which invalidates the row. Hashing rather than storing the path keeps absolute
runner paths out of the artifact while remaining comparable.

**`process_group_id`.** The PGID of the group the wrapper created for the invocation, **as observed
by the testbucket process itself**. QC7a checks it is well-formed — a positive integer within the
platform's PID range — not merely non-empty.

**What this field does and does not support.** It records the process group **as the testbucket
process observed it**. It is **not** a containment guarantee and not proof of what ran inside the
group: a `setsid`/double-forked descendant leaves the group and is invisible to it (`scope.md` §4.3). §18.0 and
ledger A-35 state exactly this and no longer use the phrase "containment identity".

---


## 14. Ingest, audit, and the closed loop

### 14.1 Workflow wiring

The reusable workflow's `record` job gains one same-run artifact download and a
`wall-observations-dir` input. The `plan` job gains `runner-class` and `runs-on-label`; the
`run-bucket` job gains the same `runs-on-label` value plus `cache-declaration-file`, `candidate-sha`, and
`workload-commit` (§21). The dogfood
Vitest lane sets `wall-time-dir` and `est-basis`. The Go dogfood lane does not, and must not.

A **scored** run must supply `runner-class`, `runs-on-label`, `cache-declaration-file`, `candidate-sha`,
and `workload-commit`; an unscored run may omit them all and keeps every current default
(AD-8, AD-9, §19.3a).

### 14.2 CLI

`testbucket ingest --wall-observations <dir>`: read the shard plan once; parse each observation;
apply §7.1; fold reporter events into unit EWMAs as today; append qualifying rows to the store's
bounded ring under §15.1b; **refit under §6.8 if, and only if, the append changed the selected
trainable population** (§15.1b); print a per-observation accept/reject table with the exact reason
for each rejection.

**An append that does not change the selected population does not invoke the fitter (R22-F4).**
A `trainable: false` diagnostic never enters the selected population and, since §15.1b bounds the
two retention classes independently, never removes a row from it — so an accepted campaign or pilot
append leaves the selection identical and **the fitter is not called at all**. The stored model
record is then preserved **byte for byte, including `fitted_at`**: not recomputed to the same value,
but not written. An earlier revision made the refit unconditional, so a valid post-freeze diagnostic
append re-invoked the fitter over an unchanged population and could advance `fitted_at` — which
`model_parameters_digest` does not cover (§6.8 step 6) — while §19.5 requires the model frozen before
the first authenticated campaign start. Consumption, displacement, and now invocation are the three
paths by which a stored row could reach a fit, and all three are closed.


## 15. Store schema and migration

### 15.1 Schema 2

**The store is a registered artifact (R22-F2), with its own projection gate (R23-F1).** Its paths
are declared in the `scope.md` §2A field registry under `artifact: store`. The gate that compares
them is **`TestWallStoreSchemaStateMatrixAndMigration`** (§22), and the compared surface is the
**complete schema-2 surface**: this section's inventory, §15.1c's presence matrix, and §15.2's
`migrated_from`, in **both** directions. An earlier revision routed the comparison to test 63, which
is expressly the six-wholly-new-document projection and does not include the store — so the
migration marker and the failure subtype could be omitted invisibly. An earlier revision left the store out of the registry entirely while the
registry's declared domain — every path this product adds or makes model-relevant — plainly covered
it. Its scalars are `role_exempt`: the record holds fitted model **output**, not a model input, a
response, an admission check, or a comparability leaf.

`storeSchema` moves `1 → 2`: schema 1 plus an optional `wall` object holding `model_version`,
`comparability_key_digest`, `status`, `fitted_at`, `rows_used`, `runs_used`, `fixed_ns`, `scale`,
`whole_invocation_overhead_ns`, `per_slice_overhead_ns`, `residual_mae_ns`, `residual_p90_ns`,
`rank_support` (which coefficients the corpus identifies), and the bounded `observations` ring
(`W = 240`). **Exactly four coefficients are stored** — `fixed_ns`, `scale`,
`whole_invocation_overhead_ns`, `per_slice_overhead_ns`. There is no `per_invocation_ns` field and
no `degenerate_columns` field; both are superseded by PD-3 and F2 respectively. Everything outside `wall` keeps its schema-1 meaning byte-for-byte.

### 15.1a The bounded history ring row (SR-2, S-6)

**Derived from the `scope.md` §2A wire-path registry** (`artifact: ring_row`), one row per qualifying
observation. Every regressor value is **frozen at plan time**, so a later store update cannot
retroactively change what was fitted.

| Field | Role (`scope.md` §2A) | Frozen at | Purpose |
|---|---|---|---|
| `repository` | IDENTITY | run | part of `intrinsic_id`; without it §15.1b's key is not reconstructible from the stored row (R8-D3) |
| `job_id` | IDENTITY | run | part of `intrinsic_id` (R8-D3) |
| `head_sha` | IDENTITY | run | the **orchestration** head commit, as GitHub authenticated it |
| `candidate_sha` | IDENTITY | run | the **testbucket** commit the executing binary was built from |
| `workload_commit` | IDENTITY | run | the **consumer** checkout the bucket ran against |
| `run_id` | IDENTITY | run | authenticated workflow run |
| `run_attempt` | IDENTITY | run | authenticated attempt |
| `observed_start_realtime` | IDENTITY | ingest | the observation's own `realtime_start`, RFC 3339 UTC. **Self-reported, not authenticated**; it is the recency stamp of §15.1b and is used for nothing else |
| `run_started_at` | IDENTITY | operator annotation | the **authenticated** Actions API run start (§18.2), RFC 3339. **Optional in-CI**, because retrieval needs the operator's credentials and no scored workflow carries `actions: read`. Every campaign date, cutoff, and window gate uses this field |
| `trainable` | ADMISSION | ingest | the **single carrier of every in-CI exclusion** (R13-D1). `false` for any row carrying the config's `campaign_id` — pilot and campaign rows alike — and also for a row the supplied config marks foreign by workload or orchestration (§15.1b). Written **once at append** from the row's own bytes plus the supplied config, so no fit at any later time can consume it, and so the fitter needs no second predicate (R8-D2) |
| `ingest_seq` | DIAGNOSTIC | ingest | a strictly increasing per-store append counter, assigned once and never reused. **Read by nothing** — not retention, eviction, selection, or fitting (R8-D3) |
| `bucket_index` | ADMISSION | plan | bucket identity |
| `plan_digest` | ADMISSION | plan | **the canonical plan identity** |
| `store_sha256` | ADMISSION | plan | the store the plan was built from |
| `comparability_key_digest` | ADMISSION | plan | which history this row belongs to |
| `reporter_sum_ns` | REGRESSOR c2 | plan | design column 2, **integer nanoseconds** |
| `i_any_whole_file` | REGRESSOR c3 | plan | design column 3 |
| `slice_count` | REGRESSOR c4 | plan | design column 4 |
| `elapsed_ns` | RESPONSE | run | the sole regression response, `= A`, integer nanoseconds |
| `whole_file_count`, `invocation_count` | DIAGNOSTIC | plan | audit only; **never regressors** |
| `terminal` | ADMISSION | run | only `passed` trains |

**`plan_digest` is the one canonical plan-identity name.** `plan_id` is removed from every document
and from `component-map.json`; the registry marks `plan_digest` with `canonical_plan_identity: true`.

`reporter_sum_ns` is deliberately **not** recomputed at fit time: recomputing against a newer store
would fit the model to inputs the plan never saw.

### 15.1b Recency, eviction, and exclusion — defined before any solver order (S-6)

The earlier revision said "the most recent `W = 240`" and then gave a lexical sort by
`(head_sha, run_id, run_attempt, bucket_index)`. That tuple is a deterministic **summation order**;
it is not a definition of chronological recency, and it left eviction and ingestion arrival order
undefined, so two ingest orders could retain different populations while both satisfying the text.
Recency, eviction, and exclusion are defined here, and §6.8's sort is applied only afterwards.

**Chronological order — total, reproducible, and independent of arrival order.** Rows are ordered by
the **recency key**

```
recency(row) = (row.observed_start_realtime, row.intrinsic_id)

intrinsic_id  = (repository, run_id, run_attempt, job_id, bucket_index, plan_digest)
                compared as an ordered tuple of byte strings
```

compared lexicographically, `observed_start_realtime` as an RFC 3339 UTC instant.

**Why the tie key is row-intrinsic and not the append counter (D-6).** An earlier revision used
`(observed_start_realtime, ingest_seq)`, with `ingest_seq` assigned **at append**, and asserted that
`ingest_seq` could only break ties inside one run. That assertion is false and the counterexample is
direct: take `W + 1 = 241` valid rows from **distinct** runs that share one parseable
`observed_start_realtime`. Appending them in order evicts the first; appending the same rows in
reverse evicts the last. The retained populations differ, so retention depended on arrival order and
test 52's shuffle-invariance could not pass.

`intrinsic_id` is a function of the row's own immutable bytes, so it is identical under every
permutation of arrival. Two rows may not share one `intrinsic_id`: QC11 already rejects a duplicate
`(head_sha, run_id, run_attempt, bucket_name)`, and **QC16** extends that to the full intrinsic
tuple, so the recency key is a **total order** on any legal ring.

`ingest_seq` is **retained as an append diagnostic only**. It is recorded, reported, and never read
by retention, eviction, selection, or fitting. No authenticated in-CI clock and no hostile-runner
mechanism is required — the repair is a choice of tie key.

**Why the recency stamp is the self-reported instant, not the authenticated one.** Ingest runs
**inside** the record job. Authenticated Actions API instants are retrieved by the **operator, with
their own credentials, outside the workflow**, precisely so no scored workflow needs `actions: read`
(§18.2). A recency rule that required the authenticated instant at ingest would therefore not be
implementable by the component that has to apply it. `observed_start_realtime` is the observation's
own `realtime_start` — run-intrinsic, already recorded, and available with no credential — so the
rule is executable where it runs. Under the trusted-CI threat model (contract §0.1) a self-reported
ordering stamp is adequate for **ordering a training ring**; it is deliberately **not** adequate for
a campaign date, and §18.2's prohibition on `realtime_*` as a campaign date source is unchanged.

Two different ingest orders over the same set of observations produce the **same retained
population** and the **same evictions**, because both components of the recency key are properties
of the row rather than of when it arrived. `TestWallHistoryRecencyEvictionAndThreeIdentities`
carries the **241-row equal-timestamp permutation fixture**: 241 valid rows from distinct runs
sharing one `observed_start_realtime`, ingested in forward order and in reverse, asserting the same
240 rows are retained and the same row is evicted in both.

**Clock-skew limitation, stated rather than assumed away.** `observed_start_realtime` is
`CLOCK_REALTIME` on the runner, so two runs on differently-skewed runners can order slightly
differently than they truly ran. Three things bound the consequence: the ordering decides only
**which 240 rows are retained**, never a gate outcome; `intrinsic_id` makes every comparison total — `ingest_seq` is **not read** by any comparison (R10-D7) — so
skew can never produce a non-deterministic result from the same inputs, because ties resolve on
`intrinsic_id`; and a row whose `observed_start_realtime` is absent or unparseable is **rejected at
ingest by QC16**, never defaulted. Skew cannot smuggle a campaign row into training either, because
that exclusion is by **`campaign_id` → `trainable`**, written at append from the row's own bytes —
not by time and not by `run_id` (R10-D3).

**Eviction is per retention class, and a diagnostic never displaces a trainable row (R21-F4).**
A ring for one comparability key holds **two independently bounded classes**: the rows with
`trainable: true`, and the rows with `trainable: false`. On append, only the appended row's **own
class** is trimmed: if that class then exceeds `W = 240` rows, its rows with the **smallest**
`recency` are dropped until exactly `W` of that class remain. Nothing else evicts, no row is ever
dropped because a row of the *other* class arrived, and a row that fails QC is never appended in the
first place.

**Why the classes are separated.** An earlier revision bounded the ring as one undivided population.
That made the no-rollback claim of §19.9c false by a legal boundary witness: with exactly `W`
trainable rows retained, appending one non-trainable campaign diagnostic evicted the oldest
**trainable** row, so the fitted population fell from `W` to `W − 1` and
`model_parameters_digest` could change solely because a post-assignment campaign row arrived. The
fitter never read the campaign row — retention had already read it. Bounding each class on its own
restores the property this contract had already committed to: **a campaign or pilot row cannot
change a fit, by consumption or by displacement.**

**Selection for fitting — one predicate, no subtraction (R14-A3).** "The most recent `W` rows" means
exactly: the retained rows of the ring for the current comparability key, after that class's
eviction, whose `trainable` is `true`. **Nothing is subtracted from that set in CI.** The class-A
exclusions below are not a second step — they are *already* materialized into `trainable` at append,
which is why the fitter evaluates `trainable == true` and nothing else (§6.8 step 1). The class-B
exclusions never subtract in CI at all; they are operator-side cross-checks. An earlier revision
wrote this sentence as "…minus the exclusions below" while the very next rule said the fitter reads
one predicate, which stated the in-CI selection two ways. The `trainable: false` class is retained
for audit and is never selected.

**Exclusions — two classes, one in-CI carrier (R13-D1).** Every domain below is either *decidable
at append* from the row's own bytes plus the supplied campaign config, or it is not decidable in CI
at all. The first class is **materialized into `trainable` at append**; the second is
**operator-side only**. There is no third class, and the fitter evaluates **`trainable == true` and
nothing else** (§6.8 step 1).

| Class | Exclusion | Predicate on the decoded row | Why it lands where it does |
|---|---|---|---|
| **A — materialized into `trainable` at append, in CI** | campaign/pilot rows — the **primary** rule | the row's own `campaign_id` matches the supplied config's (§19.9c) ⇒ `trainable: false` | the campaign's and pilot's own rows are never training data. Needs no credential and no future run ID, so the record job can actually apply it (R9-D2) |
| **A** | foreign workload | `workload_commit != manifest.workload_commit` under a supplied config ⇒ `trainable: false` | a different consumer checkout is a different workload. Both operands are present at append, so this belongs to the append-time flag and **not** to a second fit-time predicate |
| **A** | foreign orchestration | `head_sha ∈ manifest.excluded_orchestration_commits` under a supplied config ⇒ `trainable: false` | named orchestration commits, when the operator declares any. Same reasoning: decidable at append |
| **B — operator-side cross-check, never in CI** | campaign runs | `run_id ∈ manifest.excluded_run_ids`, from the **attempts** document | catches a row whose `trainable` marking disagrees with what the Actions API says ran. It **cannot** run in CI: those run IDs do not exist when the config is frozen (R10-D3) |
| **B** | campaign window | `manifest.excluded_window.start ≤ ` **authenticated** ` run_started_at ≤ manifest.excluded_window.end` | catches a harness run the manifest failed to enumerate. It uses the **authenticated** instant and therefore runs where that instant is available (§18.2), as part of campaign validation, not inside the record job |

**With no campaign config supplied there are two cases, not one (R23-F3).** Absence of a config
does not imply absence of the optional `campaign_id` field from a decoded observation, and an earlier
revision took the vacuous branch for both:

| Decoded row, no config supplied | Outcome |
|---|---|
| carries **no** `campaign_id` and `profile.scored: false` | every class-A predicate is vacuous, and the row is appended `trainable: true`. This is the ordinary CI path |
| carries a `campaign_id`, **or** `profile.scored: true` | **rejected before append**, fail-closed. The row is not appended, not retained, and cannot reach a fit by any path |

A tagged row without its config is a **miswiring**, not an ordinary row: the identity that decides
its class is present while the artifact that classifies it is absent, so no in-CI component can
decide it. Rejecting it is the same fail-closed choice §19.9c already makes for a missing or
mismatched id **under** a supplied config, and it removes the fourth state entirely rather than
guessing at it. `profile.scored` is named alongside `campaign_id` because a scored run is required to
carry a campaign identity (§21), so a scored row without a config is miswired even if the identity
field itself was dropped.

**`candidate_sha` is deliberately not a blanket exclusion.** Comparability requires the same
`testbucket_sha256` for every row in one key (§15.3), so warming rows are *necessarily* produced by
the candidate binary. A rule that excluded every row at the candidate SHA would make the warm
corpus unreachable — the contradiction the earlier "zero rows at the candidate SHA" wording
created. What must be excluded is **campaign** rows, and the first two predicates above do exactly
that, from the row's own bytes. `candidate_sha` is recorded so a re-fit on an archived corpus can
*report* which binary produced each row and can reject a corpus that mixes binaries.

**One in-CI mechanism, and two operator-side cross-checks (R11-D1, R13-D1).** The **sole** rule an
ordinary in-CI fit applies is **`trainable == true`**, and `trainable` is written once at append by
the class-A predicates above — primarily `campaign_id`. No component in CI evaluates a second
selection predicate, and no document may specify one. The `run_id` and authenticated-window
predicates are **operator-side only** — the campaign validator applies them after dispatch, because
neither input exists when the config is frozen. An earlier revision said the `run_id` predicate is
what an in-CI fit "can and must apply"; that is **withdrawn**, because it demands run IDs that do not
yet exist and would drop diagnostics the state machine retains. A campaign whose validator cannot
evaluate the window predicate **fails** — it is never skipped (§17.15).

**What reads only the stored row, and what does not (R14-A3).** Everything the **record job** does —
the recency key, eviction, the class-A predicates, and the `trainable` flag they write — reads the
decoded stored row plus the supplied campaign config, and **no ambient workflow state**, so a re-fit
on an archived corpus reaches the same verdict as the original run. The class-**B** cross-checks do
**not** have that property and were never claimed to: they read the §19.9b attempts document and
authenticated Actions API instants, which is precisely why they run operator-side and never in CI. An
earlier revision put both under one "everything above" sentence, which read as though the
authenticated checks were row-local too.
`TestWallHistoryRecencyEvictionAndThreeIdentities` shuffles ingest order, drives the ring past `W`,
and asserts identical retained populations, identical evictions, that the three identity fields are
independently addressable, and that the recency stamp used is the **self-reported**
`observed_start_realtime` — so the rule is evaluable with no API credential — while every campaign
date, cutoff, and window comparison uses the **authenticated** `run_started_at`.

### 15.1c The store's status-indexed presence matrix (R23-F2)

`wall` is optional. When it is **present**, which of its leaves exist is decided by `status` and by
the terminal outcome that produced it — not by the leaf list of §15.1, which is an inventory rather
than a presence rule. §15.2 initialises `wall` with no history, and §6.8 produces
terminal outcomes in which **no coefficients are stored at all**. Which outcomes those are is read
off the matrix below — every row whose fit group is `all absent` — and is stated as a number nowhere,
here or in any other document (R12-F1). An earlier revision gave every child of `wall` cardinality
`one`, which no implementation could satisfy for exactly those two reasons.

This table is the **only** presence rule, and its rows **are** the exhaustive set of state
classes: the enumeration is the table itself, never a count written beside it (R12-F1).

The table is **total over every terminal state §6.8 can produce and over every child of `wall`**.
`status` and `failure_subtype` name the state; `model_version`, `comparability_key_digest` and
`observations` are **always present** once `wall` exists; the fit group — the four coefficients,
`fitted_at`, both residuals, `rows_used`, `runs_used` and `rank_support` — is present only where a
fit was accepted:

| State | `status` | `failure_subtype` | `model_version`, `comparability_key_digest` | the fit group | `observations` |
|---|---|---|---|---|---|
| **migrated** — §15.2 `1 → 2`, no history invented | `insufficient` | `migrated_no_history` | present | **all absent** | present, empty |
| **below-`MIN_ROWS`** — §6.8, corpus under the row minimum | `insufficient` | `rows_below_minimum` | present | **all absent** | present |
| **below-`MIN_RUNS`** — §6.8, corpus under the run minimum | `insufficient` | `runs_below_minimum` | present | **all absent** | present |
| **rank-insufficient** — §6.8 step 2 | `insufficient` | `rank_insufficient` | present | **all absent** | present |
| **budget-exhausted** — §6.8 NNLS cap | `insufficient` | `nnls_budget_exhausted` | present | **all absent** | present |
| **degraded** — accepted fit over the MAE ceiling | `degraded` | `mae_ceiling_exceeded` | present | **all present** | present |
| **ok** | `ok` | **absent** | present | **all present** | present |

**Precedence, when more than one insufficiency predicate holds (R24-F2).** The serialized subtype
**must** be the **first match** in this fixed order, so a corpus below both minima — or below a
minimum *and* rank-deficient — serializes one determinate value rather than an implementation's
choice:

1. `migrated_no_history` — the store was just migrated and carries no history at all;
2. `rows_below_minimum`;
3. `runs_below_minimum`;
4. `rank_insufficient`;
5. `nnls_budget_exhausted`.

The order follows the order in which the conditions can be decided: a migrated store has no rows to
count, counts are decidable before a design matrix is formed, rank needs the matrix, and the solver
cap can only fire after rank admits the design. An earlier revision called a subset of these classes
exhaustive while §6.8 named more `insufficient` causes than that subset spelled, gave no spelling for
the count causes, and left `model_version` and `comparability_key_digest` implicit (R24-F2); a later
one kept those written cardinalities beside a table that had already grown past them (R12-F1). No
count of states, of `insufficient` causes, or of no-coefficient rows is written anywhere now: the
matrix is the enumeration, and §22 test 71a derives its cases from the parsed rows.

`wall.failure_subtype` is **required for every non-`ok` status and absent for `ok`**. A coefficient,
`fitted_at`, or fit statistic appearing in **any** state whose fit group is `all absent` above is a
**failure**,
and so is a missing subtype under a non-`ok` status. `migrated_from` is present iff the store was
migrated. Every leaf named here is a registered `artifact: store` path (`scope.md` §2A), and
`TestWallStoreSchemaStateMatrixAndMigration` (§22) compares this matrix and that projection in **both**
directions.

#### 15.1d The wall model version — one current value, and what a bump means (R12-F2)

`wall.model_version` versions the **model semantics**. `storeSchema` versions the **store layout**.
They are independent integers and must never be conflated: schema 2 is the first layout that carries
a `wall` object at all, and this is the first model that object can hold.

**The current value is `1`.** Write it as the named constant `WALL_MODEL_VERSION`, whose value is
`1`. **Every** serialized `wall` object carries `model_version: 1` — in every row of §15.1c's matrix,
including `migrated`, where §15.2's `1 → 2` step writes it together with the other always-present
leaves. Two independently written literal fixtures for the same state therefore serialize the same
bytes for this leaf, which is what §15.1c's determinacy claim requires; an earlier revision fixed the
leaf's *presence* and left its *value* to the fixture author (R12-F2).

**What a bump means.** `WALL_MODEL_VERSION` changes when, and only when, one of these changes:

| Versioned by this leaf | Not versioned by it |
|---|---|
| the regressor set and column order of §0.9's design-column table | the store layout — that is `storeSchema` (§15.2) |
| the response `A` and its measurement boundary (§1.1) | the comparability key and its derivation (§15.3) |
| the integer-nanosecond unit system and the single rounding boundary (§0.9) | the observation document's own schema (§13) |
| the rank criterion (§6.6) and the fit/acceptance procedure (§6.8) | retention, eviction and `trainable` (§15.1b) |

**Reading is fail-closed, never a guess.** A `wall` object with **no** `model_version`, or with a
value the reader does not implement, is **refused** on §7's fail-closed path: the wall basis is
unavailable, the reason is named, and the reader **does not** treat an unknown version as the current
one, does not silently substitute a default, and does not fit. `observations` remain readable, because
their schema is versioned by `storeSchema` rather than by the model.

**A bump never silently reuses a fit.** Coefficients, `fitted_at`, both residuals, `rows_used`,
`runs_used` and `rank_support` written under one `model_version` are **not** valid under another. On a
bump the implementation either (a) declares an explicit migration that rewrites those leaves, or (b)
**drops the whole fit group**, retains `observations`, and lets the next `ingest` re-derive `status`
from the retained corpus under the new model. Path (b) needs **no new state class and no new
subtype**: the re-derivation lands the store in one of §15.1c's existing rows, so the matrix stays
total. What is forbidden is the third option — keeping the old fit group under a new version number.

### 15.2 Migration

- **1 → 2 forward**, in `ingest` only: `units`, `flags`, `updated_at`, `coverage`,
  `coverage_source` carried verbatim; `wall` initialised empty with status `insufficient`,
  `failure_subtype: migrated_no_history`, and the always-present leaves written —
  `model_version: WALL_MODEL_VERSION` (§15.1d), `comparability_key_digest`, and an empty
  `observations` container; `migrated_from: 1` recorded. **No wall history is invented.**
- **2 → 1 backward**: a schema-1 tool hits the existing unknown-schema path and cold-starts loudly.
- **Unknown schema**: cold start with a reason (existing behaviour, retained and tested).
- The plan path never migrates; only `ingest` writes schema 2.

### 15.3 Comparability — the execution-profile digest (F6)

The former key — `runner_token | os | arch | runner_image_label | node_version | testbucket_sha` —
was too weak in two specific ways: `runner_token` is the literal constant `vitest` for every Vitest
consumer and so carries no information, and a **mutable image label** such as `ubuntu-latest` is a
name, not an image identity, so two materially different images compared equal.

**Every leaf is sourced from the PLAN JOB (SR-3).** The key must be constructible **before the
matrix is emitted**, so every value comes from the job that runs the plan step — its runner context
and its environment. The eight matrix test-job runners do **not** exist at plan time and are never
key inputs.

**Plan job and test jobs are expected to share one runner label**, because they are configured in
one workflow. If they differ, that is a **misconfiguration to be detected and reported**, not
silently tolerated: the run-bucket action records its own runner label into the observation, and
QC compares it against the key's `runner_image_label`. A mismatch fails the row and names both
labels.

**`comparability_key_digest`** is SHA-256 over the canonical JSON of these fields, in this order,
each sourced as the `scope.md` §2A field registry records:

**Every leaf, with its exact producer.** "Producer" names the code path that sets the value; no leaf
is left to inference.

**Two leaves are repaired here (S-4).** The earlier key included `runner_name` — the plan job's
runner *instance* name — and claimed `runner_image_label` came "from the `runs-on` label resolved in
the plan job", which named no producer at all. Both are fixed:

| Repaired leaf | What was wrong | What replaces it |
|---|---|---|
| `runner_name` → **`runner_class`** | `${{ runner.name }}` on GitHub-hosted runners is a per-instance name with no stability contract across runs, so the key described a *plan-job instance* rather than the execution class whose `A` observations are fitted. `MIN_RUNS = 3` under one key had no route to stability. | an **explicit stable runner class string**, supplied as the required `runner-class` input to the `plan` action by the calling workflow or orchestrator (e.g. `github-hosted-ubuntu-24.04-2core`). It is chosen by the operator, constant across the runs they intend to compare, and recorded in the manifest. The actual instance name is retained as the **diagnostic** `actual_runner_name` (§13). |
| `runner_image_label` | no executable producer: a composite action can read `runner.os`/`runner.arch`, but no context yields the caller's resolved `runs-on` label | a **required `runs-on-label` input** passed explicitly from the calling workflow/orchestrator to **both** the `plan` and the `run-bucket` actions. `plan` puts it in the key; `run-bucket` records what it received as `observed_runs_on_label`; **QC12** compares them and fails the row on divergence, naming both values. |

| # | Field | Producer — the code path that sets it |
|---:|---|---|
| 1 | `runner_class` | `plan` composite action, **required input `runner-class`**; refused empty for a scored plan (AD-8) |
| 2 | `runner_image_label` | `plan` composite action, **required input `runs-on-label`**, passed by the caller; the same value is passed to `run-bucket`, which echoes it as `observed_runs_on_label` |
| 3 | `os` | `plan` action, from `${{ runner.os }}` |
| 4 | `arch` | `plan` action, from `${{ runner.arch }}` |
| 5 | `node_version` | `plan` action step, `node --version` in the plan job |
| 6 | `pnpm_version` | `plan` action step, `pnpm --version` in the plan job |
| 7 | `vitest_version` | `internal/runner/vitestrunner` acquisition provenance, resolved at discovery |
| 8 | `testbucket_sha256` | `install`/build step, SHA-256 of the binary placed on `PATH` |
| 9 | `working_dir` | `plan` action input `working-directory` |
| 10 | `facade_command` | `plan` action input `vitest-command` |
| 11 | `setup_command` | the **`plan`** action's own `setup-command` input (R8-D6/R9-D5). Naming `run-bucket` "echoed into the plan document" was backwards — the plan already exists by then, and every comparability leaf is plan-job-sourced. `run-bucket` receives the same value for execution; the key reads the plan's |
| 12 | `lock_sha256` | `plan` action step, SHA-256 of `pnpm-lock.yaml` in the working directory |
| 13 | `discovery_mode` | `plan` CLI flag `--vitest-discovery` |
| 14 | `exclusions` | `TB_DISCOVERY_EXCLUDE_PREFIXES` as read by the façade at discovery |
| 15 | `cache_declaration_digest` | `plan` composite action, SHA-256 over the canonical JSON of the **declaration leaves of §10.5.0**, in the order that table lists them, read from the required `cache-declaration-file` input. The **same** digest is published as the plan job's `cache-declaration-digest` output and re-checked by every `run-bucket` (contract §10.5.2), so the key leaf and the transport verification are one value |

**Why the cache declaration is a key leaf (R11-D4).** §10.5 states that dependency, transform, and
MongoDB binary state changes the measured preflight/import/transform lifecycle. Pairwise equality
protects B against C *inside* one pair; it does nothing to stop **pre-campaign fitting** from mixing
`disabled` runs with `exact-key` runs, different primary keys, or different Mongo binaries under one
model. Binding the declaration into the comparability key makes a change to any registered
declaration leaf **reset the wall history and the model**, exactly as a `lock_sha256` change does. The two per-job
**outcomes** — `dependency_cache_matched_key` and `dependency_cache_disposition` — are deliberately
**excluded** from the key: they vary legitimately between bucket jobs of one run, so keying on them
would reset history on ordinary cache behaviour.

**`runner_image_label` is the observable identity; no cryptographic image digest is required.**
Under the settled trusted-CI decision the label **is** the identity this product binds.
There is no producer for a cryptographic image digest — fleet attestation was removed by a settled
`PRODUCT_DECISION` — and **no document, test, or delta requires one**.

So the key binds the **mutable label string**. A label does **not** guarantee image identity the way
a digest would: `ubuntu-24.04` can be re-rolled underneath the same name, and two materially
different images can compare equal. Under the settled trusted-CI model (contract §0.1) that is
accepted, and a **label change is treated as a proxy for environment change** — it catches the
change the operator can see and does not pretend to catch the one they cannot. Obtaining a
cryptographic image digest would require reinstating fleet attestation, which the threat model
excludes; re-entry is by measurement (contract §12), not by wanting a stronger field.

**Deliberately outside the digest, with reasons.**

| Field | Why excluded |
|---|---|
| `actual_runner_name` | a hosted runner **instance** name has no stability contract across runs; including it made `MIN_RUNS = 3` under one key unreachable (S-4). It is recorded as a diagnostic on every observation and reported, never keyed |
| — | `lock_sha256` is **inside** the key (entry 12): a dependency change alters what is imported and executed, so rows across it are not comparable. The cost is accepted — a dependency PR resets wall history and the model re-warms. Ordinary planning is unaffected because it falls back to reporter basis (§0.8 a/b); only wall basis waits for `MIN_ROWS`/`MIN_RUNS` again. |
| `K` | topology enters the model as **frozen per-row columns** (`I(any_whole_file)`, `slice_count`, §15.1a), so a K change produces different rows rather than incomparable ones. The superseded rationale "the per-invocation term models K" is replaced by this PD-3 one. |
| `file_parallelism` | rows with `file_parallelism > 1` never train (QC12), so it cannot contaminate history |

**Two independent reset rules, unchanged in kind:**

| Key | Governs | Reset rule |
|---|---|---|
| `flags` — the canonical adapter token | reporter EWMAs | a token change clears `units` and `coverage` |
| `comparability_key_digest` | wall observations and the fitted model | a change clears `wall.observations`, sets status `insufficient`, and leaves reporter EWMAs untouched |

A reset is loud: the plan report names the field that changed and the row count discarded.

**Bounded warm-up is demonstrated, not asserted (S-4).** Before any document claims that a warm
model is reachable on this workload, **three distinct real runs must be shown to produce the
identical `comparability_key_digest`** under one declared `runner_class` and `runs-on-label`. That
is gate **W-3** of §17.3 and is the reason `MIN_RUNS = 3` is a reachable requirement rather than an
aspiration. `TestExecutionProfileIsPlanTimeAvailableAndLabelBound` proves the key is constructible
and stable given fixed inputs; the three-real-run demonstration is an execution gate (§20.6b AG-3),
not a document claim.


### 15.3a The executed runtime profile — the bucket proves what it ran (owner F1)

§15.3's comparability key is sourced entirely from the **plan job**, by construction: it must exist
before the matrix is emitted. That makes it a statement about what was *declared*, and the measured
observation previously recorded only `observed_runs_on_label` and `actual_runner_name`, with QC12
comparing the label alone. Nothing proved that the matrix job actually ran the planned Node, pnpm,
Vitest or testbucket binary. A copied plan profile and a caller-supplied `candidate_sha` prove a
declaration, not executed bytes — and those versions change imports, transforms, setup and test
execution **inside `A`**, so a history or a B/C pair could mix rows produced under different runtime
profiles.

**`runtime_profile_digest` is SHA-256 over the canonical JSON of exactly these seven fields, in this
order:**

| # | Field | Declared value — plan job | Executed value — bucket job |
|---:|---|---|---|
| 1 | `node_version` | `node --version` in the plan job (§15.3 leaf 5) | `node --version` on the runner that executed the bucket |
| 2 | `pnpm_version` | `pnpm --version` in the plan job (leaf 6) | `pnpm --version` on that same runner |
| 3 | `vitest_version` | the acquisition provenance resolved at discovery (leaf 7) | the version of the Vitest the façade actually invoked |
| 4 | `testbucket_sha256` | SHA-256 of the binary the install step placed on `PATH` (leaf 8) | SHA-256 of the testbucket binary on `PATH` **at the moment the bucket ran** |
| 5 | `facade_command` | the `plan` action's `vitest-command` input (leaf 10) | the façade command the bucket actually invoked |
| 6 | `lock_sha256` | SHA-256 of `pnpm-lock.yaml` in the plan job's working directory (leaf 12) | SHA-256 of `pnpm-lock.yaml` in the executed working directory |
| 7 | `dependency_cache_mode` | the declared mode of §10.5.0 | the mode actually in force for that bucket |

**Two objects, one algorithm, and both are serialized (R15-F1).** The **object** is what travels;
the digest is a convenience over it. The plan job writes the declared object and its digest into the
**plan document**, which is already transported to every bucket job:

```json
"runtime_profile_declared": {
  "node_version": "…", "pnpm_version": "…", "vitest_version": "…",
  "testbucket_sha256": "sha256:…", "facade_command": "…",
  "lock_sha256": "sha256:…", "dependency_cache_mode": "disabled|exact-key"
},
"runtime_profile_declared_digest": "sha256:…"
```

Every bucket job writes the same shape into its **observation**, as `runtime_profile` and
`runtime_profile_digest`, computed from its own executed values. The canonicalization is §0.9's:
the keys of the table above, in the order that table gives them, no whitespace, strings as JSON
strings.

**No new job output.** The declared object and digest ride in the plan document. §21 fixes the plan
job's output set at `matrix`, `cache-declaration-json` and `cache-declaration-digest`, and this rule
does **not** enlarge it — an earlier revision claimed a `runtime-profile-digest` job output as well,
which contradicted that exhaustive set (R15-F1).

**QC17 — the mismatch is a row failure, decided field by field.** QC17 compares the observation's
`runtime_profile` against the plan document's `runtime_profile_declared` **field by field, in the
declared order**, and the digests as a whole. It names the **first differing constituent** by field
number — which a digest comparison alone could never do, and which is why both objects are
serialized rather than only their digests. A digest difference with no field difference, or the
reverse, is itself a failure: the digest must be the one this section defines over that object. A failed row is **not appended**, never enters history, and — if the run is
scored — leaves the pair **retained, unscored and non-passing** under §19.8. QC17 is decided **after**
a bucket script has run, so it is a **post-start** outcome: never a `void-pre-start`, never a
replacement for an attempt, and never rescheduled (R15-F3).

**What this is not.** No signature, no attestation, no fleet roster, no runner identity proof and no
protected environment is required or implied. The bucket hashes files and reads version strings on
its own runner; a runner that lies is out of the trusted-CI boundary §3 already draws, and §12 keeps
it there. This rule detects **misconfiguration and drift**, which is the failure mode that actually
occurs when a workflow pins one Node in the plan job and another in the matrix job.

**Acceptance test.** `TestBucketRuntimeProfileMatchesPlan` (§22, test 73) mutates **each** of the
seven constituents in the bucket while leaving the plan document unchanged, and requires each
mutation to fail QC17, to keep the row out of history, and to leave a scored pair **retained,
unscored and non-passing** under §19.8 — never voided, never rescheduled.
It also asserts the equal case passes and that the digest is computed over the seven fields in the
declared order.

## 16. Planner, matrix semantics, and wording repairs

### 16.1 Mode selection

`testbucket plan --est-basis reporter|wall`, default `reporter`; pass-through `est-basis` on the
`plan` action and the reusable workflow. The plan document and every matrix entry carry `est_basis`
and `expanded_unit_set_digest`.

### 16.3 Matrix semantics

- `est_seconds` — JSON **number**, seconds, exactly one decimal, `round1` (half-up on
  non-negative), always present, never a string, never null. Meaning given by `est_basis`.
- `est_basis` — `"reporter"` or `"wall"`.
- `wall_est_seconds` — one decimal, present **iff the basis is `reporter` and a fitted model with
  status `ok` exists**, and **absent under `wall` basis**; §5.1 states that single presence predicate
  and this line defers to it (R12-D6). Under reporter basis it is a **shadow diagnostic** that never
  reaches `AllocationScore`. It is
  `round1(A_eta_ns/1e9)` computed from the same frozen coefficients.
- `a_eta_ns` — the exact integer-nanosecond objective value, serialized as a decimal string on each
  plan bucket. `est_seconds` under wall basis is exactly `round1(a_eta_ns / 1e9)`, and a validator
  recomputes one from the other (S-1).
- `needs_node`, `units`, `invocations`, `script`, `name`, `bucket` — unchanged.

**The displayed number and the optimized number are always the same quantity.**

### 16.4 Wording repairs — required, each with its site

| Site | Current text | Repair |
|---|---|---|
| `README.md:76-77` | "…which is what makes that sum **the job's actual wall time** rather than a proxy for it" | it is the reporter-work estimate; the job is never measured |
| `README.md:310-321` | the honest warning that reporter EWMA "cannot tell you how long the action took" | retained; it contradicts `:76` — fix the other |
| `README.md:443-444` | A_GH "accounts for the wrapper install that necessarily precedes AT_start" | the install is outside; A_GH accounts for no such thing |
| `.github/actions/run-bucket/action.yml:19` | `est-seconds`: "The bucket's time estimate in seconds" | name the basis: reporter-work estimate, or `round1(A_eta_ns/1e9)` |
| `internal/core/plan.go:400` | "loaded (recorded timing) … **measured wall-time** %s" | "recorded reporter work" |
| `internal/core/plan.go:448` | "execution model: -p=1, so a bucket's estimate is its **serial wall time**" | true only of the serial *reporter-work* sum; state the basis; do not emit when `file_parallelism > 1` |
| `internal/walltime/gates.go:568-569` | "eighty complete action observations **per arm**" | **false** — the constant is the campaign-wide total, split evenly between the two arms, at the values contract §10.4 fixes. This file is deleted by the removal plan; the replacement must not reproduce the error, and `TestCampaignDenominatorWording` (test 20) asserts the corrected wording |
| `internal/walltime/schedule.go:41` | "so the draw is **reproducible by a third party** rather than asserted" | unsupported: no code derives the order from the seed. This file is deleted by the removal plan; the replacement carries **no draw at all** — the order is the fixed counterbalanced sequence of §19.5, and the words *randomized*, *seed*, and *draw* do not appear (`TestCampaignOrderIsCounterbalancedNotRandomized` (test 18)) |

Anywhere "complete action", "whole wrapper", or "whole job" describes `A` or `V`, they become
"instrumented run-bucket interval" and "Exec-envelope interval". Until §16.4 lands, no
forecast-accuracy claim may be made with `est_seconds`.

The last two rows are in files this scope deletes. They are listed anyway because a deleted file's
wrong sentence tends to be re-typed into its replacement, and because a reviewer reading the tree
today will find them.

---

## 17. Fail-closed matrix

| # | Condition | Behaviour | Test |
|---|---|---|---|
| 17.1 | `--est-basis wall`, no model / `insufficient` / `degraded` | plan **fails**, naming the shortfall | required |
| 17.2 | `--est-basis wall` with `--file-parallelism > 1` | plan **fails** | required |
| 17.3 | `AllocationScore` cannot score a unit | plan fails | existing |
| 17.4 | `--wall-dir` with `--runner go` | error | existing |
| 17.5 | any of QC1–QC17 fails | row not ingested; retained and printed | required |
| 17.6 | duplicate observation for one key | **all** rejected | required |
| 17.7 | unknown store schema | cold start with a reason | existing |
| 17.8 | execution-profile change | wall rows and model cleared, loudly | required |
| 17.9 | bucket coverage audit fails | that row is not learnable; the run's ingest fails as today | existing |
| 17.10 | `verify-wall` required and failing | the bucket job fails | existing, simplified |
| 17.11 | observation absent at record time | the row is **absent**, reported as absent, never zero | required |
| 17.12 | campaign: profile block or pair invariant tuple violated | the arm-run is unscored; its pair non-passing | campaign harness |
| 17.13 | campaign: store artifact digest mismatch after download | run **void** before any bucket script starts | campaign harness |
| 17.14 | campaign: a scheduled date matches no authenticated run date | campaign **fails** | campaign harness |
| 17.15 | campaign: a mandatory field absent — cutoff, exclusion domain, orchestration identity, workload identity, `expanded_unit_set_digest` | campaign **fails**; never skipped | campaign harness |
| 17.16 | campaign: any workload-derived value disagrees with the value recomputed from the checkout at `workload_commit` — façade, config, lockfile, `package.json`, tree digest, façade argv, exclusion prefixes, or discovered path set | campaign **fails**; cross-arm equality does not substitute | campaign harness |
| 17.17 | plan: `scored: true` with any admission check **AD-1…AD-10** unmet — including AD-9's undeclared `cache_state` — | matrix **not emitted**; plan fails | required |
| 17.18 | campaign: a pair's authenticated instants contradict the declared counterbalanced **sequential** order of §19.5 — `completed_at(first) ≤ started_at(second)` — or any of those instants is empty/unparseable | campaign **fails** | campaign harness |
| 17.19 | campaign: a pair's two arms disagree **index-wise on any of the `bc_inv` cache leaves** (§10.5.0), or on the per-index `dependency_cache_hit`/`matched_key`/`disposition` §19.2b compares — or either arm's row fails QC14 | the pair is **not scored** and **non-passing**; both rows retained in the **attempted** population. This is a **post-start** outcome under §19.8: it is never a void and never rescheduled (R15-F2, R15-F3) | campaign harness |
| 17.20 | plan: `scored: true` with an empty `runner-class` or `runs-on-label` input (AD-8), or an empty `candidate-sha` or `workload-commit` (AD-10) | matrix **not emitted**; plan fails | required |
| 17.21 | ingest: an observation whose `head_sha`, `candidate_sha`, and `workload_commit` are not three separately present, well-formed values (QC15) | row not ingested | required |

---

### 17.3 Calibration and warm-up — a topology proposer and an execution protocol, kept apart (S-7)

Explicit wall mode needs a rank-4 corpus (§6.6) **and** at least `MIN_ROWS = 24` accepted rows
across at least `MIN_RUNS = 3` distinct runs. The campaign cannot bootstrap itself, because campaign
rows are excluded from training (§15.1b).

**Two different things, previously conflated.** A planner can decide, from a candidate unit
universe, whether some proposed partition *would* make the design full rank. It cannot manufacture
accepted executions. So the route to a warm profile is two artifacts, not one:

| | **Topology proposal** — `--calibrate` | **Warm-up execution protocol** — W-1…W-4 |
|---|---|---|
| Question | is there a partition of this universe whose rows would give `rank(X) == 4`? | does the ring actually hold ≥24 accepted rows over ≥3 runs under one stable key? |
| Produces | a `testbucket.calibration-evidence/v1` document | ring rows |
| Can prove | rank reachability for the topologies it enumerated, or structural impossibility | nothing about rank on its own |
| Cannot prove | that any run happened | that a proposed topology exists |

Neither substitutes for the other, and `MIN_ROWS`/`MIN_RUNS`/rank are re-checked against the **real**
ring on every wall plan.

#### 17.3a Topology proposal — `--calibrate`

```
testbucket plan --runner vitest --k 8 --count 1 --file-parallelism 1 \
                --calibrate --calibration-out <path> [--calibration-max-plans N]
```

`--calibrate` is a **planning mode**, not a scored mode: it implies `scored: false`, never emits a
campaign matrix, and never writes the store.

**`N` is defined for every legal value.** `N = --calibration-max-plans`, an integer with
`N ≥ 1`, default `4`. It bounds how many layouts the proposer evaluates. There is no maximum: the
layout generator `L` below is **partial**, but the **proposer** is total: any `N ≥ 1` is legal and
exactly one outcome is defined for all of them (R12-D4).

**What totality means, exactly, now that the arithmetic has terminal outcomes (R13-D5).** For every
legal `(U, K, N)` the proposer either **returns exactly one** of `SUFFICIENT`,
`STRUCTURALLY_INFEASIBLE`, or `NOT_FOUND_WITHIN_BUDGET`, **or** it terminates **fail-closed** with a
named arithmetic/convergence condition of contract §1.1 — `E_RANK_NON_CONVERGENT` when a rank
evaluation exhausts its iteration cap, `E_NS_OVERFLOW`/`E_NON_FINITE`/`E_CONVERSION_RANGE` when a
quantity leaves its domain — and writes **no** calibration-evidence document. It never returns a
fourth outcome value, never returns silently, and never reports a computation it did not complete as
one of the three. Totality is a statement about the three outcomes, not a promise that arithmetic
cannot fail.

**The layout generator `L(U, K, i)` — a PARTIAL layout function under a TOTAL proposer (R11-D5).**

`L` itself is **partial**: for some `(U, K, i)` no layout exists, and saying it was "total to exactly
`K` bucket lists" contradicted its own exhaustion rules. What is **total** is the *proposer*: for
**every** legal `(U, K, N)` it returns exactly one of `SUFFICIENT`, `STRUCTURALLY_INFEASIBLE`, or
`NOT_FOUND_WITHIN_BUDGET`. When `L(i)` is undefined the proposer records `layouts_tried = i - 1`,
stops, and reports `NOT_FOUND_WITHIN_BUDGET` with `generator_exhausted: true`.

An earlier revision described `L(3)` as isolating the heaviest whole-file unit and `L(i ≥ 5)` as
isolating the `(i − 4)`-th heaviest — so `L(5)` re-isolated the unit `L(3)` had already isolated and
`L(5) = L(4)`, which made test 59's "fails at `N = 4`, succeeds at `N = 5`" unsatisfiable by any
fixture. It also left residual placement, slot exhaustion, slice-split ordering, the one-slice case,
and empty buckets unspecified. `L` is defined as a **partial** function here, under a **total proposer** (R12-D4).

**Notation.** `W` = the whole-file units of `U` ordered by `(reporter_ewma_ns desc, unit_id asc)`;
`S` = the name-slice units of `U` ordered by `unit_id asc`. `L` returns **exactly `K` lists**,
indexed `0 … K−1`, whose union is `U` and whose pairwise intersections are empty.

| `i` | Layout — `isolate(n)` means "the first `n` units of `W`, each alone in its own bucket" |
|---:|---|
| 1 | the ordinary reporter partition (`expandUnits` plus the reporter-basis KK packing) |
| 2 | bucket 0 = **all** of `S`; the units of `W` distributed over buckets `1 … K−1` by KK on `reporter_ewma_ns` |
| 3 | `isolate(1)` into bucket 0; bucket 1 = all of `S`; the rest of `W` over buckets `2 … K−1` by KK |
| 4 | as `L(3)`, but `S` is split across buckets 1 and 2 — the first `ceil(size(S)/2)` of `S` in bucket 1, the remainder in bucket 2 — and the rest of `W` over buckets `3 … K−1` by KK |
| `i ≥ 5` | `isolate(i − 3)` into buckets `0 … i−4`; then `S` split across the next two buckets as in `L(4)`; then the rest of `W` over the remaining buckets by KK |

So `L(5)` isolates the **first two** whole units — genuinely new relative to `L(4)`, which isolated
one — and `L(i)` isolates `i − 3` of them. Every parameter the earlier text left open is fixed:

| Case | Rule |
|---|---|
| ties in `W` | broken by `unit_id` ascending; the order is total |
| residual placement | every unit not isolated and not in `S` is packed over the **remaining** buckets by the same deterministic KK the reporter basis uses |
| **isolation exhaustion** | `L(i)` isolates `i − 3` whole units. If `i − 3 > \|W\|` there are not enough whole units to isolate; the generator is **exhausted at `i`** and `L(i)` is **undefined**. It never pads a missing isolated unit and never silently isolates fewer (R10-D6) |
| slot exhaustion | `L(i)` needs `(i − 3) + 2` slots for isolation and slices, **plus one more whenever residual whole units remain** — units are left after isolating `i − 3` of them, so the requirement is `(i − 3) + 2 + (1 if any residual else 0)`. If that exceeds `K`, `L(i)` is **undefined** and the generator is **exhausted at `i`**. An earlier revision counted only the reserved slots and so published a layout with nowhere to put its residual (R8-D1) |
| **slice slots actually needed** | `L(4)` and `L(i ≥ 5)` reserve `min(2, \|S\|)` slice buckets, not always 2 — with `\|S\| = 1` only **one** slice slot is reserved, and the slot requirement becomes `(i − 3) + min(2, \|S\|) + (1 if residual else 0)`. This removes the one-slice contradiction R11-D5 found: at `K = 3`, `i = 4`, `\|S\| = 1` with residual wholes the old formula demanded `1 + 2 + 1 = 4` slots and declared exhaustion while the very next rule described a feasible three-slot layout. The requirement is now `1 + 1 + 1 = 3 ≤ K`, and the layout **exists** |
| exactly one slice unit | a single slice cannot be split, so it occupies the single reserved slice bucket whole; `slice_count` then varies through the residual packing rather than through a split |
| **minimum `K` for `L(2)` and `L(3)`** | `L(2)` needs `1 + (1 if \|W\| > 0 else 0)` slots and `L(3)` needs `1 + 1 + (1 if residual W else 0)`. If either exceeds `K`, that layout is **undefined** and the generator is exhausted at it, exactly as for `i ≥ 4`. No layout silently emits fewer than `K` bucket lists |
| no slice units | `slice_count` is identically zero over every partition, so **column 4** is dead; the universe is **structurally infeasible** (above) and the generator is not run |
| empty buckets | permitted, and produced when `U` has fewer than `K` units; an empty bucket contributes a row with `reporter_sum_ns = 0`, `I = 0`, `slice_count = 0`, and is a legitimate design row |

The proposer evaluates `L(1) … L(min(N, exhaustion point))` in order and stops at the first layout
whose added rows would make `rank(X) == 4` under the frozen tolerance of §6.6.

**Literal fixture for the budget boundary — recomputed, and it is `N = 3` / `N = 4` (R8-D1).**

An earlier revision published an `N = 4` / `N = 5` boundary whose matrices had **exact ranks 2 and
3, not 3 and 4**, and whose `L(5)` bucket list did not follow the generator: at `K = 4` that layout
consumes all four slots on two isolated whole units and two slice buckets, leaving **no slot for the
residual whole units**. Both errors are corrected, and the boundary moves to where the generator can
actually produce one.

**Universe:** `K = 4`; whole units `w1 = 40e9`, `w2 = 30e9`, `w3 = 20e9`, `w4 = 10e9`; slice units
`s1`, `s2`, `s3` at `5e9`. Reporter columns are shown in billions; scaling column 2 by `1e9` does
not change rank.

| Layout | Buckets, index 0 → K−1 | Design rows `[1, reporter_sum_ns, I, slice_count]` | rank |
|---|---|---|---:|
| `L(1)` | `{w1}` `{w2}` `{w3,s3}` `{w4,s1,s2}` | `[1,40,1,0]` `[1,30,1,0]` `[1,25,1,1]` `[1,20,1,2]` | **3** — every bucket holds a whole unit, so column 3 equals column 1 |
| `L(2)` | `{s1,s2,s3}` `{w1}` `{w2}` `{w3,w4}` | `[1,15,0,3]` `[1,40,1,0]` `[1,30,1,0]` `[1,30,1,0]` | **3** |
| `L(3)` | `{w1}` `{s1,s2,s3}` `{w2}` `{w3,w4}` | `[1,40,1,0]` `[1,15,0,3]` `[1,30,1,0]` `[1,30,1,0]` | **3** |
| `L(4)` | `{w1}` `{s1,s2}` `{s3}` `{w2,w3,w4}` | `[1,40,1,0]` `[1,10,0,2]` `[1,5,0,1]` `[1,60,1,0]` | **4** |
| `L(5)` | — | — | **exhausted**: `(5−3)+2 = 4` slots equals `K`, leaving none for residual `{w2,w3,w4}` |

Every bucket list is what `L(U, K, i)` produces for this universe under the rules above, residual
packing included, so the fixture witnesses the **generator** and not merely the rank test. `L(4)` is
the first layout reaching rank 4, so test 59 asserts **`not_found_within_budget` at `N = 3`** and
**`sufficient` at `N = 4`**, with `L(5)` reporting **exhaustion** rather than being evaluated. Each
rank was computed by exact rational elimination.

**Rank statements elsewhere say `≤ 3`, not `= 3`, unless a fixture establishes equality.** The table
above is the only place this document asserts an exact rank below 4.

**Three outcomes, each decidable, each truthful about what it proves.**

| Outcome | Condition | Emitted | Exit |
|---|---|---|---|
| **SUFFICIENT** | some evaluated layout `L(i)`, `i ≤ N`, yields `rank(X) == 4` under the frozen tolerance | the calibration evidence document, with `layouts_tried = i` | 0 |
| **STRUCTURALLY_INFEASIBLE** | a **proof** over the whole universe, not a search result, and **it names a different column in each direction (R8-D1)**: with **no name-slice unit**, `slice_count` is 0 in every bucket of every partition, so **column 4 is identically zero**; with **no whole-file unit**, `I(any_whole_file)` is 0 everywhere, so **column 3 is identically zero**. Either way `rank(X) ≤ 3` for **all** layouts, not merely those tried. An earlier revision blamed column 3 in both directions, which is false for an all-whole universe — empty buckets are permitted, so the indicator need not be constant there | an explicit structural-infeasibility error naming **the actually-zero column** and the reason | non-zero |
| **NOT_FOUND_WITHIN_BUDGET** | neither of the above within `N` layouts (including the case where the generator exhausted before `N`) | an explicit error naming the deficient columns, `layouts_tried`, `layout_budget`, and — when the generator exhausted — that it did | non-zero |

**The bounded negative outcome is named for what it is.** The earlier revision called it
`INFEASIBLE`, which asserted that the candidate universe *cannot* produce rank 4. Failing four
heuristic layouts does not prove that; a layout outside the list may succeed.
`NOT_FOUND_WITHIN_BUDGET` says exactly what happened, and `STRUCTURALLY_INFEASIBLE` is reserved for
the case where a genuine proof over the whole universe exists. Both are legitimate reportable
answers, and under both, explicit wall mode stays a hard error (§0.8 outcome (c), §7).

**Calibration evidence document**, written to `--calibration-out` and persisted for later runs:

```json
{
  "schema": "testbucket.calibration-evidence/v1",
  "outcome": "sufficient|structurally_infeasible|not_found_within_budget",
  "comparability_key_digest": "sha256:…",
  "proposed_plan_digests": ["sha256:…"],
  "layouts_tried": 3, "layout_budget": 4,
  "rank": 4,
  "sigma_max": "…", "tolerance": "…", "min_pivot": "…",
  "deficient_columns": [],
  "indicator_values_present": [0, 1],
  "distinct_slice_counts": [0, 2, 5],
  "generated_at": "…"
}
```

**What subsequent explicit wall mode does with it.** The document is **evidence, not authority**: a
later `--est-basis wall` plan still re-runs the §6.6 rank check against the **actual** ring
contents, and still requires `MIN_ROWS` and `MIN_RUNS`. The evidence records that a sufficient
corpus was reachable and by which topologies; it never substitutes for the checks, and a stale or
mismatched `comparability_key_digest` makes it inapplicable.

#### 17.3b Warm-up execution protocol — W-1…W-4

This is what actually populates the ring. It is execution, not planning, and no document claims it
can be satisfied by a planner.

| # | Gate |
|---|---|
| W-1 | a `--calibrate` run returns **SUFFICIENT** for the candidate universe under the intended `K`, recording the layouts that reach rank 4. The published boundary for this protocol is §17.3a's **`N = 3` not-found / `N = 4` sufficient** fixture, and `STRUCTURALLY_INFEASIBLE` names **column 4** for an all-whole universe and **column 3** for an all-slice one — the earlier N=4/N=5 and both-directions-column-3 statements are withdrawn (R10-D6) |
| W-2 | those layouts are executed as **non-scored** runs under the same comparability key, producing observations that pass QC1–QC17 |
| W-3 | **at least three distinct real runs** — distinct `(run_id, run_attempt)` — produce the **identical** `comparability_key_digest`, demonstrating that the repaired key (§15.3) is stable across runs rather than describing one plan-job instance |
| W-4 | the ring for that key holds **≥ `MIN_ROWS` = 24** accepted rows across those **≥ `MIN_RUNS` = 3** runs, with `rank(X) == 4` and at least one `i_any_whole_file = 0` row |

Only when W-1…W-4 all hold is explicit wall mode admitted. Until then it is a hard error with no
matrix, exactly as §0.8 outcome (c) says.

**Status of a calibration or warm-up run.** Same comparability key as the campaign,
`scored: false`, admitted to the ring under the ordinary QC criteria, excluded from the campaign's
scored population (§19.4a). It relaxes neither PD-2 nor PD-3.

**Acceptance test.** `TestCalibrationModeTerminatesSufficientOrNotFoundWithinBudget` — over the
**published §17.3a universe** it returns `not_found_within_budget` with
`layouts_tried == layout_budget` at **`N = 3`**, and `rank: 4` with `outcome: sufficient` at
**`N = 4`**, because `L(4)` is the first layout reaching rank 4 and `L(5)` reports exhaustion. It
returns `structurally_infeasible` naming **column 4** for an all-whole-file universe and **column 3**
for an all-slice one. It is defined and terminating at `N = 1`, at every `N` beyond the exhaustion
point, and for the **one-slice `K = 3`** case of §17.3a. It never emits a fourth outcome or a silent
partial success. (An earlier revision of this paragraph demanded column 3 in both directions and an
N=4 failure / N=5 success, reversing the table above; that is withdrawn — R11-D5.)

---

## 18. Evidence

### 18.0 Ledger sufficiency — what this evidence supports

**Sufficient, at invocation and instrumented-action granularity — and the claim is scoped to fields
the §13 schema actually retains.** Each claim below names its field; nothing outside the table is
claimed:

| Claimed | Retained field | Checked by |
|---|---|---|
| what ran | `invocations[].argv_digest`, `.selector`, `.units`, `.atoms` | QC6 |
| where it ran | `invocations[].cwd_digest` — the executed absolute cwd, hashed, compared to the plan's | QC7a |
| the process group **as the testbucket process observed it** — not a containment guarantee | `process_group_id` | QC7a |
| which plan it belongs to | `plan_digest`, `profile`, `expanded_unit_set_digest` | QC3, QC13 |
| how it ended | `terminal`, `exit_code`, `invocations[].exit_code` | QC9 |
| both boundaries | `started_mono_ns`/`ended_mono_ns`, per-invocation pair, `boot_id_*` | QC7, QC8 |

`cwd_digest` and `process_group_id` were **added to the schema** by this revision rather than
dropping the claim, because both are cheap to record and genuinely useful for tracing. "Containment
identity" as a broader notion is **not** claimed: the product owns a process group and says so (§3.3),
and no field asserts more than that.

**Not sufficient, at unit granularity.** A whole-file invocation may cover many planned units while
its stream carries one aggregate `V`, and no signature over a supplied claim creates the missing
decomposition. The ledger supports invocation- and action-level **evaluation**, not file-specific
**training labels**, unless invocations are made single-unit (`scope.md` §7.6) or an attribution method is
separately validated.

**What this product does with that.** It fits at bucket level and takes the per-unit signal from
the reporter EWMA (§6.2, §6.4b, and §0.9). It never derives a unit label from an aggregate. Adding principals,
immutable storage, or more observers would strengthen tamper checks and would **not** close the
unit-label gap, so none of them is adopted as a remedy for it (contract §9, `scope.md` §12).

### 18.1 Primary — testbucket's monotonic wrappers

The `testbucket.wall-observation/v1` documents of §13, with plan digest, expanded-unit-set digest,
bucket identity, unit set, invocation membership, and profile checked against a plan read once.

### 18.2 Corroborating — SHA/run/artifact-bound raw GitHub logs

| Field | Source |
|---|---|
| `repository`, `head_sha`, `run_id`, `run_attempt`, `run_started_at`, `conclusion` | `GET /repos/{o}/{r}/actions/runs/{run_id}` |
| `job_id`, job name, `started_at`, `completed_at`, `runner_name`, `labels`, agent version | `GET …/runs/{run_id}/attempts/{n}/jobs` |
| raw job log bytes and their SHA-256 | `GET …/actions/jobs/{job_id}/logs` |
| `artifact_id`, name, `size_in_bytes`, `created_at`, `expires_at` | `GET …/runs/{run_id}/artifacts` |
| artifact zip bytes and their SHA-256, plus each observation's SHA-256 | `GET …/actions/artifacts/{id}/zip` |

**Every campaign date and every cutoff comparison uses an authenticated timestamp** from this
metadata, normalised to UTC. A self-reported, unsigned, or optional field is never a date source.

Retrieval is the **operator's**, with their own credentials, outside the workflow — so no scored
workflow needs `actions: read`.

### 18.3 Non-claims

Not claimed: that artifacts or job logs are immutable, permanent, tamper-evident, or externally
attested; that a signature proves an execution environment or reviewer approval; that a process
group proves containment against a detaching descendant; that the collision matcher is
byte-identical to Vitest 4.1.10; that any measurement is the composite-action step duration, the
complete action, the whole wrapper lifetime, or the job duration; that a seed inside a digest
proves a draw; that the campaign order is randomized; that the campaign establishes significance,
power, or any conclusion beyond the measured sample; that the in-tree consumer snapshots prove the
consumers' live state; that the wall
model selects unit topology; that allocation is outcome-free, free of outcome-derived influence, or
mechanically pre-campaign beyond what §0.4 and §6.4b fix; that the ledger supports unit-level
training labels.

What *is* claimed, with `V[j]` and `A` stated as the **two different cases they are (SR-9)**:

- **`V[j]` is bounded by one process lifetime.** The testbucket process spawns the Vitest child,
  reads `V_start` before spawning, and reads `V_end` after child exit and group drain — **both
  reads in the same OS process**.
- **`A` is bounded across two CLI invocations sharing the same boot identity.** `wall begin`
  persists `A_start` plus a boot fingerprint; `wall end` reads `A_end`, **verifies the same
  fingerprint**, and computes `A = A_end − A_start`. `CLOCK_MONOTONIC` is boot-relative on Linux,
  so two readings are comparable exactly when the boot identity matches; **a boot mismatch is a
  hard error**, never a best-effort subtraction. Saying `A` is taken "in one process lifetime"
  was wrong and is removed.
- Endpoints are never caller-supplied: `wall end` uses the **persisted** start, not an argument.
- Bytes that hashed to a stated digest at a stated time; run and job identities and start times as
  GitHub reported them; conservative over-grouping of ambiguous file paths.

---

## 19. Campaign

### 19.1 Shape

One candidate, two planner modes, five matched pairs. Both arms run under measurement, so `A` is
observed identically and the comparison is of **allocation weight** with instrumentation *and unit
topology* as shared constants.

### 19.2 Pair invariant tuple — the bound treatment

Asserted per pair over both arms' plan documents, observations, and harness logs.

**`BC-INV` membership is declared by the field registry, and the table below is a checked projection
of it (R16-F1).** An earlier revision said the list was "declared once, here, and nowhere else" while
also saying the registry owns the membership — two ownership claims for one domain, with nothing
comparing them. There is one owner: the ordered `memberships.bc_inv` list in the `field-registry v2`
block of `scope.md` §2A, recursing into `memberships.tuple_leaves`.

**The table is a byte-exact projection, compared in both directions.** Row *n* is the *n*-th member
of `memberships.bc_inv`, in registry order, and the projection carries **no count** — the numbering
is positional, not a cardinality claim. `TestArmsDifferOnlyByPlannerMode` and the authority test both
fail if the two differ in membership or in order, which is what makes reproducing the list here safe
rather than a second authority. Every member names a **wire path**, so a mutation case addresses real
serialized bytes rather than an alias with no resolution rule; the `bc_*` alias entries of the
earlier revision are removed. `manifest.env_tuple` carries `tuple_leaves`, so entries and scalar
leaves are different quantities.

| # | Wire path — position *n* in `memberships.bc_inv` | Group |
|---:|---|---|
| 1 | `manifest.orchestration_repo` | identity |
| 2 | `manifest.orchestration_commit` | identity |
| 3 | `manifest.workload_repo` | identity |
| 4 | `manifest.workload_commit` | identity |
| 5 | `manifest.candidate_sha` | identity — the testbucket commit built for the run (S-6) |
| 6 | `manifest.testbucket_binary_sha256` | binary |
| 7 | `manifest.instrumentation_binary_sha256` | binary |
| 8 | `manifest.action_tree_digest.plan` | actions |
| 9 | `manifest.action_tree_digest.run-bucket` | actions |
| 10 | `manifest.action_tree_digest.record` | actions |
| 11 | `manifest.facade_sha256` | workload bytes |
| 12 | `manifest.facade_argv` | workload bytes |
| 13 | `manifest.vitest_config_sha256` | workload bytes |
| 14 | `manifest.lockfile_sha256` | workload bytes |
| 15 | `manifest.package_json_sha256` | workload bytes |
| 16 | `manifest.workload_tree_digest` | workload bytes |
| 17 | `manifest.discovery_exclude_prefixes` | discovery |
| 18 | `manifest.discovered_unit_set_digest` | discovery |
| 19 | `manifest.model_parameters_digest` | inputs |
| 20 | `manifest.runs_on_label` | environment |
| 21 | `manifest.runner_class` | environment — the stable class of §15.3 (S-4) |
| 22 | `manifest.runner_image_label` | environment |
| 23 | `profile.expanded_unit_set_digest` | topology |
| 24 | `profile.runner_token` | profile |
| 25 | `profile.k` | profile |
| 26 | `profile.count` | profile |
| 27 | `profile.file_parallelism` | profile |
| 28 | `profile.store_sha256` | inputs |
| 29 | `cache_state.dependency_cache_mode` | cache state (S-5) |
| 30 | `cache_state.dependency_cache_primary_key` | cache state (S-5) |
| 31 | `cache_state.dependency_cache_producer` | cache state — **configured before dispatch**, so it is an input, not an outcome (owner F2) |
| 32 | `cache_state.transform_cache_mode` | cache state (S-5) |
| 33 | `cache_state.mongo_binary_sha256` | cache state (S-5) |
| 34 | `manifest.env_tuple` — expanding through `tuple_leaves` into `node_version`, `pnpm_version`, `working_directory`, `setup_command`, `events_dir`, `timeout_minutes` | environment, compared as one ordered tuple; leaves compared individually |

**`dependency_cache_producer` is an invariant input, not an outcome (owner F2).** It is a workflow
input chosen **before dispatch**, exactly like `runs-on-label`: the caller decides whether it or the
action owns the restore. An earlier revision classified it with the job-produced outcome leaves and
left it out of `BC-INV`, so two arms could pass every pair invariant with `caller` against `action`
producers — a configured difference the contract simultaneously claimed did not exist. It is now
member 31, and a pair whose arms disagree on it is **not scored**.

**The only unequal input is `est_basis`, and §19.2b is what makes that true.**

**Derived outputs, not invariant inputs.** `plan_digest` and bucket composition are *consequences*
of the treatment and are expected to differ. They are excluded from `BC-INV` by construction and are
never counted among its entries.

**The mutation matrix is generated from the `scope.md` §2A field registry, not hand-maintained.**
`TestArmsDifferOnlyByPlannerMode` reads `memberships.bc_inv`, recurses into
`memberships.tuple_leaves` where present, resolves each member to its wire path in `wire_paths`, and
emits one equality check and one one-mutation rejection case per **scalar leaf**. The test source contains **no literal field count** — not "30", "28", "27", or any
number — and asserts that the generated case set is **non-empty** and **covers every recursive
scalar leaf**. It uses the repaired plan-time execution-profile key (SR-3) and asserts `est_basis`
is the sole unequal input while `plan_digest` and bucket composition are derived outputs.




**Classification is explicit.** `est_basis` is the **only unequal input field**. `plan_digest` and
bucket composition are **derived outputs** — consequences of the treatment — and are excluded from
`BC-INV` by construction, never counted among its entries, and never compared as invariants.

**Must differ, taking exactly the two values `{reporter, wall}`:** `est_basis`.

**Expected to differ as a consequence:** `plan_digest` and bucket composition — that *is* the
treatment.

`expanded_unit_set_digest` is the new one and it matters: unit topology is store-derived (`scope.md` §7.0), so
without asserting it across arms the treatment would silently include a topology difference if the
two arms ever saw different store bytes. A pair failing this assertion is **not scored** (§17.19),
and — because the assertion is evaluated on rows a bucket script produced — it is a **post-start**
outcome under §19.8: retained, non-passing, never voided (R15-F3).

### 19.2b Scored runs are cache-symmetric or cache-disabled (owner F2)

A dependency or transform cache that is warm in one arm and cold in the other changes the dependency
store and filesystem warmth that feeds imports and transforms **inside `A`**. Recording that
difference does not make the arms equal, and pairing and counterbalancing cannot repair a
within-pair asymmetry. A scored run therefore takes **exactly one** of two configurations, declared
before dispatch and asserted per pair:

| Mode | What is required | What is compared across the arms |
|---|---|---|
| **(a) disabled** — the default | `dependency_cache_mode` and `transform_cache_mode` are both `disabled` in **both** arms; `dependency_cache_producer` is `none`. Nothing is restored and nothing is saved, and every bucket installs cold | both mode leaves are `disabled` and equal, the producer is `none` in both arms, `dependency_cache_primary_key` and `dependency_cache_matched_key` are empty, and `dependency_cache_hit` is absent |
| **(b) exact-key symmetric** | `dependency_cache_mode` is `exact-key` in both arms, with the **same** `dependency_cache_primary_key` and the **same** `dependency_cache_producer` — all three are `BC-INV` members, so §17.19 already compares them | **index-wise equality**: for every cache index the run declares, `dependency_cache_hit`, `dependency_cache_matched_key` and `dependency_cache_disposition` are compared **between the two arms at the same index**. A single index differing **invalidates the pair** |

**An unequal pair is retained, unscored and non-passing — it is not a void (R15-F3).** Cache
equality is known only **after** a bucket script has run, so §19.8's post-start rule governs it: the
attempt is **retained in the attempted population** with its terminal state, is **excluded from the
scored and trainable populations**, makes its fixed pair **non-passing**, and is **never replaced,
re-drawn or rescheduled**. `void-pre-start` and the two-reschedule allowance are reserved for the
**platform-failure class** of §19.8 — a run that fails before either arm starts a bucket script. An
earlier revision routed this outcome to §17.12/§19.7 as a void, which contradicted the ITT rule it
cited. The pair is not repaired, re-weighted, or averaged away.

**Installation stays outside `A` in both modes**, so mode (a)'s cold install costs wall-clock in the
job and nothing in the measured interval. The executed MongoDB-binary check is retained in **both**
modes: QC14a still binds `mongo_binary_sha256` to the binary the bucket actually executed, and that
leaf remains a `BC-INV` member.

**Why (a) is the default.** It removes the entire class of within-pair warmth differences by
construction, and the only thing it costs is time outside the measured interval. Mode (b) exists for
a consumer that cannot afford cold installs, and it is strictly **more** to verify, not less: every
declared index must be shown equal, and one mismatch leaves the pair unscored and non-passing.

**What this withdraws.** An earlier revision let the arms differ in `dependency_cache_matched_key`,
`dependency_cache_disposition` and `dependency_cache_hit`, and treated recording those differences as
sufficient. Under that rule the true claim was "the same declared configuration with unbound runner
state", while this contract simultaneously asserted that `est_basis` is the sole material input
difference. Both cannot hold, and the campaign depends on the second, so the first is withdrawn
(owner F2).

**Acceptance test.** `TestScoredCacheIsSymmetric` (§22, test 72) drives both modes: a disabled pair
scores; an `exact-key` pair with index-wise equal outcomes scores; an `exact-key` pair with **one**
index differing in hit, matched key or disposition is **retained, unscored and non-passing** —
never voided, never rescheduled (§19.8); a pair whose arms
declare different `dependency_cache_producer` or different `dependency_cache_mode` fails §17.19
before it reaches this rule; and a scored run declaring any `transform_cache_mode` other than
`disabled` fails.

### 19.2a Workload binding — anchored to the pinned revision, not to supplied bytes

**Cross-arm equality is necessary and not sufficient.** Two arms agreeing proves they ran the *same*
partition; it cannot prove it was the *intended unit-only* partition. A self-consistent set of
workload inputs for some other partition would satisfy every equality in §19.2 while measuring the
wrong thing. Every workload-derived value is therefore **derived from the pinned revision**, never
accepted as a supplied value.

Before `B_1`, and again inside the campaign validator:

1. Resolve `workload_commit` by checking the workload repository out at that full SHA (§19.3).
2. Recompute from **that checkout**, not from the manifest: `facade_sha256`
   (`scripts/tb-vitest.ts`), `vitest_config_sha256` (`vitest.config.ts`), `lockfile_sha256`
   (`pnpm-lock.yaml`), `package_json_sha256`, and `workload_tree_digest` — the sorted list of
   `relative path`, file mode, and file SHA-256 over the tracked `*.test.ts` set plus those four
   files.
3. Compare each against the manifest's frozen value. **Any mismatch fails the campaign** (§17.16).
4. Derive `facade_argv` and `discovery_exclude_prefixes` from the **pinned revision's own workflow
   file**, not from the manifest, and compare.
5. Derive the expected discovered path set by running the pinned façade's discovery inside that
   checkout, and compare both its digest and its file count against `discovered_unit_set_digest`
   and the §20.4 static audit.

`workload_tree_digest`, `package_json_sha256`, and the derivation rule join the invariant tuple, so
the manifest can no longer assert a workload identity it did not obtain from version control.

### 19.3 Orchestration identity ≠ workload identity — Mandel is not modified

Adopting the experiment inside Mandel would create a new Mandel commit and change the frozen
workload identity, which the frozen profile pins at `d9ae1d43`. So the campaign does **not** modify
Mandel.

**Primary route — external digest-bound orchestrator.** A `workflow_dispatch` workflow in the
testbucket repository (or a scratch orchestration repository) that:

1. checks out Mandel at exactly `d9ae1d433bb45012c04d567879b66fc4bf6112c6` into `./workload`
   (`actions/checkout` with `repository` and `ref` pinned to the full SHA);
2. builds testbucket from the candidate SHA into a side directory, prints the binary's SHA-256, and
   puts it on `PATH` — no published release, no resolver, no moving reference;
3. provisions the workload under a **declared cache mode**, reusing Mandel's own composite from the
   checkout (`uses: ./workload/.github/actions/setup-case-test-mongo`) plus
   `pnpm install --frozen-lockfile`. **"The way Mandel does" is not a specification (S-5)** — the
   pinned workflow's own PNPM restore carries a `restore-keys:` prefix fallback, so copying it
   would leave the measured input unbound. The orchestrator therefore does all of:
   - declares `dependency_cache_mode` as either `disabled` (no restore, no save — installs cold) or
     `exact-key` (**no `restore-keys:` list**, so only an exact primary-key match can hit), and uses
     the **same** mode in both arms of every pair;
   - writes the **leaves §10.5.0 classes as `declaration`**, in the order that table lists them, as
     the canonical declaration. They are knowable **before dispatch**, and they reach every job by the
     **content-plus-digest protocol of §10.5.2**: the plan job publishes the canonical bytes and
     their SHA-256 as workflow outputs, each matrix job materializes them to a **job-local** file, and
     `run-bucket` re-verifies the digest before use. **R14-F2: this step previously said "the same
     immutable bytes are passed to `plan` and to every `run-bucket` job", which is the shared-path
     description the transport repair removed** — two jobs on two runners share no filesystem, so
     passing a path transports nothing and only a verified digest establishes byte identity. The
     leaves §10.5.0 classes as **`outcome`** are **not** in that declaration and cannot be: every one
     of them is a result of a bucket job's own restore, setup or input validation, on its own runner,
     after the matrix already exists (R11-D3, R16-F2: this step used to name three of them and omit
     the rest, which is why it now names none and cites the table);
   - points Vitest's transform cache at a **fresh per-run directory**, so `transform_cache_mode` is
     truthfully `disabled`.

   A prefix fallback — a **non-empty** matched key differing from the primary key — fails the row
   (QC14), while an `exact-key` **miss** is legal; a pair whose arms disagree **index-wise on any of
   the `bc_inv` cache leaves**, or on the per-index outcomes §19.2b compares, is **not scored**
   (§17.19, §19.2b). Ordinary page-cache, filesystem and runner-instance variation remains unbound
   and is a **named paired-run limitation** (§18.3), controlled by pairing and counterbalancing
   rather than asserted away;
4. invokes the composite actions with `working-directory: workload`, the frozen façade command, one
   caller-supplied `runs-on-label` passed to **both** `plan` and `run-bucket`, the declared
   `runner-class` passed to `plan` (§15.3, S-4), and — because AD-9 and AD-10 must refuse **before
   matrix emission** — the cache declaration, `candidate-sha` and `workload-commit` reaching **both
   `plan` and `run-bucket`** (R10-D5). The declaration reaches them as **content plus digest**, never
   as a shared path: `plan` receives `cache-declaration-file` in its own job and republishes the
   verified bytes as `cache-declaration-json` / `cache-declaration-digest`, and each matrix job
   materializes those bytes locally and passes the digest to `run-bucket` (contract §10.5.2, R14-F2).
   The job also passes **`dependency-cache-producer`**, and — under producer `caller` only —
   `dependency-cache-matched-key` and `dependency-cache-hit` (contract §10.5.3). `plan` enforces the
   admission rules; `run-bucket` writes the values into the observation so QC15's three identities are
   recordable (S-6). `campaign-id` is
   passed to `run-bucket` so every pilot and campaign observation carries it (R10-D3). `est-basis`,
   `scored` and `pair_index` are dispatch inputs.

The manifest records `orchestration_repo` / `orchestration_commit` and `workload_repo` /
`workload_commit` as **separate mandatory fields** (§17.15), both equal across arms (§19.2).

**Operational requirement, stated plainly:** the orchestrator needs read access to the Mandel
repository — an ordinary CI token or app installation, not a protected environment, not a signing
key, not an approval ceremony.

**Documented fallback.** If a new Mandel adoption revision is used instead, that commit becomes
`workload_commit`, and the static audit of §20.4 **must be re-derived at that revision** before
`B_1`; the `d9ae1d43` numbers may not be carried over.

The reusable workflow is not used either way: it runs `npm ci` at three sites and Mandel is a pnpm
project with no `package-lock.json`, and its `actions/checkout` uses carry no `repository`/`ref`,
which in a reusable workflow default to the caller's repository.

### 19.3a Plan admission — an observable check, not a CLI default (P1)

CLI defaults do not bind a scored run: `--k` defaults to 6, `--file-parallelism` may exceed 1, and
`count` is only enforced late when a Vitest plan is realized. A scored run is therefore **admitted**
by an observable record, not by the constants the binary happens to ship.

The plan document emits a `profile` block, and the plan **refuses to emit a scored matrix** unless
every field holds:

```json
"profile": {
  "scored": true,
  "runner_token": "vitest",
  "k": 8,
  "count": 1,
  "file_parallelism": 1,
  "bucket_indices": [0,1,2,3,4,5,6,7],
  "est_basis": "reporter|wall",
  "store_sha256": "sha256:…",
  "expanded_unit_set_digest": "sha256:…"
}
```

Admission rules, each fail-closed at plan time (§17.17):

| # | Check |
|---|---|
| AD-1 | `runner_token == "vitest"` — a scored run on any other adapter is refused |
| AD-2 | `k == 8` exactly; not "≥1", not the default |
| AD-3 | `count == 1` exactly, checked before discovery, not after realization |
| AD-4 | `file_parallelism == 1` — serial file execution |
| AD-5 | `bucket_indices` is exactly `[0..7]`, each once, and equals the rendered bucket set |
| AD-6 | `est_basis` is present and is one of the two permitted values |
| AD-7 | the block is copied verbatim onto every observation (§13), so the validator compares plan-time admission against run-time record |
| AD-8 | the `runner-class` and `runs-on-label` action inputs are **present and non-empty** (§15.3, S-4); a scored plan may not fall back to a default or to a runner-context value |
| AD-9 | `cache_state` is declared for the run and satisfies §10.5.0 before any bucket script starts (S-5) |
| AD-10 | `candidate-sha` and `workload-commit` are present and non-empty, so the observation can carry the three separate identities QC15 requires (S-6) |

`scored: false` runs are unaffected and keep every current default — this binds the campaign, not
ordinary consumers.

### 19.4 Exact profile, validated against the complete planned bucket set

```
runner_token      = vitest
K                 = 8
count             = 1
file_parallelism  = 1
one authenticated terminal row for EVERY bucket identity (index AND name) in the eight-bucket plan
plan_digest       identical across all eight observations of one arm-run
expanded_unit_set_digest identical across both arms of a pair
```

Eight *distinct* rows are not sufficient: they must be the **complete planned set**, matched by
identity against the plan document, so a `K > 8` plan cannot contribute a favourable eight. This is
what defaults, an eight-row denominator, and cross-arm equality do not give.

### 19.4a Two populations, never conflated (F8)

Counting scored runs and attempted runs as one number was a defect. They are defined separately and
used for different purposes.

| | **SCORED population** | **ATTEMPTED population** |
|---|---|---|
| Contents | the campaign population contract §10.4 fixes — arm-runs, rows per arm, and complete buckets per run — and nothing else | **every** Actions workflow attempt in the window — original, void, abandoned, rescheduled, and re-attempted |
| Used for | all effect, calibration, tail, dispersion, and cost gates (§19.7) | audit accounting and manifest completeness only |
| Never used for | audit completeness | any threshold or gate |

**Rules that follow, each stated so neither population absorbs the other:**

1. The **three-date and 14-day gates use scored-run start timestamps** — the authenticated starts of
   the 10 scored arm-runs, not of every attempt.
2. **Pre-start voids are excluded from outcome denominators and included in audit accounting.** A
   pair voided before either arm started a bucket script contributes zero scored rows and one or
   more attempted runs.
3. **Post-start failures stay in intention-to-treat.** Once either arm has started a bucket script
   the pair is counted; a failing bucket makes the pair **non-passing** and its rows are retained
   and unscored.
4. **Every run the Actions API returns for the window must appear in the manifest**, in the
   attempted population, with its disposition. A run present in the API and absent from the
   manifest **rejects the campaign**.
5. `run_attempt > 1` never erases or replaces attempt 1; both appear in the attempted population.

A campaign therefore reports two counts that are expected to differ — for example 12 attempted runs
and 10 scored runs after one two-arm pre-start void and its reschedule — and a reader is never asked
to reconcile them into one number.

### 19.5 Population, store, order, and dates

- **Scored:** the pair, arm, bucket and row population is contract §10.4's and is **not restated
  here** (R14-F1). What this section adds is the shape of the arithmetic: pairs × arms gives
  arm-runs, arm-runs × complete buckets gives rows, and rows divide evenly between the two arms. The
  "80 per arm" reading is the multiplication error §16.4 pins in the R54 source comment.
  **Attempted** is a separate, usually larger count (§19.4a).
- The **date-count and window gates are contract §10's**; what §19.4a rule 1 adds is *which*
  timestamps they read — the **scored** arm-runs' authenticated starts, never attempted runs'.
- **Store:** both arms restore the same pinned store artifact by artifact id, verified against a
  recorded SHA-256 after download; a mismatch voids the run before any bucket script starts
  (§17.13). No `actions/cache` restore-key in any scored path, because a restore-key can fall back
  by prefix and the matched key is not captured. Neither arm writes the store back. Warm-only
  (§0.8). Cutoff and exclusion domains per §6.4b are conditions of the campaign.
- **Order verification — sequential, not merely launch order (S-9).** Two distinct properties were
  previously conflated under one check:

  | Property | Predicate | What it supports |
  |---|---|---|
  | **launch order** | `started_at(first) < started_at(second)` | which arm was dispatched first |
  | **sequential order** | `completed_at(first) ≤ started_at(second)` | the arms did **not overlap** — the property counterbalancing needs to control within-pair drift |

  Two jobs can satisfy launch order while running concurrently on different runners, which is not
  the sequential execution the drift-control claim rests on. **The campaign requires sequential
  order.** For each pair, both instants come from the authenticated Actions API (§18.2): the first
  arm's `completed_at` must be less than or equal to the second arm's `started_at`, in the direction
  the declared sequence states. A contradiction, an overlap, or an empty or unparseable instant
  fails the campaign (§17.18, `TestPairsRunSequentiallyByAuthenticatedCompletion`). Where a document
  needs to speak only of dispatch it says **launch order** and makes no drift claim.
  `schedule.go` stores a seed and never derives an order from it; the replacement derives nothing
  and verifies everything.
- **Cache policy (S-5, R9-D4):** the `cache_state` block of §10.5.0 is declared before run 1 and
  recorded on every row; a pair whose arms disagree **index-wise on any leaf of the field
  registry's `BC-INV` membership** is not scored (§17.19). Under §19.2b's exact-key mode a
  per-index `dependency_cache_hit`, `dependency_cache_matched_key` or `dependency_cache_disposition`
  difference likewise leaves the pair unscored and non-passing — a **post-start** outcome under
  §19.8, never a void.
- **Model freeze (S-2):** the four coefficients and the store bytes are frozen before the first
  authenticated campaign start and are not refitted from campaign rows at any point.
- **Order (owner resolution, contract §0.2):** a **fixed precommitted counterbalanced sequence**,
  declared in the manifest before the first run — pairs 1, 3, 5 run `B → C`; pairs 2, 4 run
  `C → B`. Counterbalancing alternates which arm goes first so ordering and drift within a pair do
  not sit systematically on one arm; it is a control, not a draw. The campaign is **not
  randomized**, and no document, schema, log, or comment claims randomization, a seed-derived draw,
  or a reproducible shuffle (`TestCampaignOrderIsCounterbalancedNotRandomized` (test 18)).
- **Dates:** each scheduled date is compared against the arm's **authenticated** run date from the
  Actions API (§18.2), parsed as `YYYY-MM-DD` in UTC. A scheduled date matching no authenticated run
  date fails the campaign (§17.14).

### 19.6 Thresholds, effect size, and what the design can resolve

**Engineering motivation for the numbers.** The gate asks for a ≥5% reduction in per-run `Amax`.
Mandel's bucketed unit lane carries a 30-minute per-job timeout and replaced a ~29-minute serial
job, so 5% of a makespan in that range is minutes of developer-visible feedback per run, on every
PR and canary push — the smallest change worth shipping. This is the engineering reason the number
is 0.95; it is not an effect-size calculation, and no power argument is attached to it.

**What the design is (owner resolution, contract §0.3):** an **engineering release gate**, over
exactly the eighty rows measured. It is **not a statistical significance test**. No statement in
this scope, the contract, the code, the logs, or the campaign report asserts or implies
significance, power, a null hypothesis, a p-value, or an inferential conclusion (`TestNoInferentialClaimIsShipped` (test 26)).
Generalization beyond the measured sample is not claimed.

**What a PASS establishes:** on this frozen workload, over exactly the rows measured, every gate of
contract §10 held at the values contract §10.4 fixed before run 1 — tail non-regression, median
improvement, bounded total cost, improved per-run dispersion, and calibration of `A_eta_ns` against
`A` under both the absolute and the relative bound. **The thresholds themselves are not restated
here** (R14-F1); what this section adds is what the conjunction of them means and, below, what it
does not.

**How far the attribution goes, stated precisely (S-5, S-9).** The result is attributable to the
partition weight **to the extent that the declared and recorded inputs were equal**: unit topology
held constant by `expanded_unit_set_digest`, store bytes pinned by digest, the **`bc_inv` cache
leaves** declared and equal index-wise across each pair (§10.5.0), and the two arms executed
**sequentially** in a counterbalanced order (§19.5). What is **not** proved is runtime state
equality: page-cache warmth, filesystem state, and hosted-runner instance variation are unbound and
are controlled only by pairing and counterbalancing. The supportable sentence is "the sole
intentional and configured difference is `est_basis`", not "the two arms were physically identical".

**The cache diagnostics may differ only *within* an arm, never *across* one (R17-F1).** The per-job
outcomes `dependency_cache_hit`, `dependency_cache_matched_key` and `dependency_cache_disposition`
are permitted to differ **between different bucket indices of the same arm** — eight bucket jobs
restore independently, so one may hit while another misses inside a single arm-run, and that
variation does not weaken the attribution. **At the same bucket index across the B and C arms they
must be equal**, exactly as §19.2b's exact-key mode requires; under §19.2b's disabled mode they take
their prescribed absent/empty shape in both arms. An earlier revision of this paragraph said the two
diagnostics "are permitted to differ" without naming which axis, which read as a cross-arm
permission and contradicted §19.2b.

**A same-index cross-arm mismatch is a post-start outcome (R17-F1).** Cache equality is known only
after a bucket script has run, so §19.8's post-start rule governs it and §17.19 records the verdict:
the attempt is **retained** in the attempted population, **unscored**, and its fixed pair is
**non-passing**. It is **never** a `void-pre-start`, never replaces an attempt, and is never
rescheduled — those are reserved for the platform-failure class that fails before either arm starts
a bucket script.

**Pilot — plumbing only, with corrected arithmetic (S-9).** One pilot pair may run before the five
and is **not** counted in them. Its shape is

```
1 pair × 2 arms × 8 buckets = 16 rows
```

The factor of eight is **buckets**, not runs: a pilot pair is two arm-runs, each producing the
complete eight-bucket set. The earlier "1 pair × 8 runs × 2 modes" was wrong about the factor and
would have implied sixteen arm-runs.

**The pilot runs inside the fit freeze (D-3).** `manifest.frozen_at` precedes the pilot and
The pilot carries the config's `campaign_id`, so pilot observations are ingested as
diagnostics and **can never enter a fit** (§19.9c steps 3–4). An earlier revision froze the model
only before the *earliest campaign start*, which left pilot rows satisfying the written
"pre-campaign" predicate and able to refit — directly contradicting the sentence below. The freeze
now starts earlier than the pilot, so the two agree.

**What the 16-row pilot can and cannot establish.** It proves the harness end to end at pair scale:
the orchestrator checks out the workload at the frozen commit, the profile validates, the
`cache_state` declaration is recorded and equal across the pair, order verification runs, and
observations qualify. It **cannot** prove eighty-row bookkeeping — sixteen rows do not exercise the
five-pair denominators, the date-count gate, the window gate, the void/reschedule boundary, or the
`median(RA)` over five pairs. **Eighty-row bookkeeping is proven separately**, either by
`TestCampaignAttemptAccountingAndITT` over a synthetic 80-row population or by the real campaign
itself. The pilot's scope is plumbing only, and no document claims more. **It cannot change
anything.** Thresholds are frozen before the first campaign run and
cannot be retuned from pilot or campaign outcomes (owner resolution, contract §0.4); the pilot
produces no threshold, no design change, and no gate. Observed per-bucket and per-run spread may be
reported descriptively alongside the verdict; it is a description of what was measured, never an
argument about resolvable effects.

### 19.7 Statistics and gates

Per run `Amax`, `TA`, `DA`; per pair `RA_i`. Median for even *n* is the conventional arithmetic
mean, computed in exact integer nanoseconds with rational cross-product comparison.

**The gate set has exactly one home, and it is `acceptance-contract.md` §10 (R13-A).** An earlier
revision carried the same fourteen gates here and there, with a test whose whole purpose was to stop
the two copies from drifting — a test that exists only because the duplication does. The duplication
is removed: **this section states no gate, no threshold, and no comparison operator.** The
authoritative table is contract §10; the exact numeric values are contract §10.4; and the arithmetic
domain every one of them is evaluated in is contract §1.1.

What §19.7 owns is the *statistical shape* the gates read — `Amax`, `TA`, `DA`, `RA_i`, and the
even-`n` median convention above — and the two clarifications below, neither of which restates a
gate.

Both calibration gates are **conjunctive**: absolute *and* relative must hold. The absolute bound
binds when buckets are small, the relative bound when they are large, which makes "extremely close"
scale-independent rather than an artifact of this workload's size.

**Precommitted formula, evaluated bound (S-2).** `mean(A)` is the arithmetic mean of the 40 observed
`A` values in the C arm, in integer nanoseconds. The **formula and the percentage are precommitted**
and appear in contract §10.4 before run 1; the **numerical right-hand side is evaluated from the
observed C rows**, which is what a relative gate is. Evaluating a frozen formula against observed
data is not retuning a threshold, and the two are never described as the same act. What is forbidden
is changing either precommitted percentage — the values are contract §10.4's, and this section does
not restate them — or refitting a coefficient, after seeing an outcome. C rows compute residuals,
MAE, worst error, and `mean(A)` **only**; they never enter a fit.

**Every threshold is frozen before the first campaign run and cannot be retuned from pilot or
campaign outcomes** (owner resolution, contract §0.4). The authoritative list of exact numeric
values, including model sufficiency and void limits, is contract §10.4.

**What replaced "must not diverge" (R13-A).** `TestThresholdsAreFrozenBeforeRunOne` (test 27) no
longer compares two tables for equality, because there is no second table to compare. It now asserts
the stronger property: contract §10 carries the gate set, contract §10.4 carries every numeric
value, and **no other document in this package states a campaign gate label, threshold, or
comparison operator**. A gate restated anywhere else fails that test — which is the only way the two
copies could ever have drifted in the first place.

**No outlier deletion, no rounding allowance, and no retry or replacement after either arm starts a
bucket script.** B's reporter-work estimate
is not calibration-gated; its error is reported only as contrast.

### 19.8 Intention-to-treat, retry, and platform failure

- Once either arm of a pair has started **any** bucket script, every attempt in that pair is
  retained and counted.
- A run failing **before any bucket script starts in either arm** — runner allocation, checkout of
  either repository, `pnpm install`, fixture provisioning, a GitHub incident, concurrency
  cancellation, or a store-digest mismatch — is retained with terminal `void` and a reason, and the
  **same precommitted pair rescheduled** — pair index, arm order, workload, mode, and every `BC-INV`
  value unchanged. A rescheduled pair is never re-drawn, re-selected, or re-ordered; it is the same
  pair run again. **At most two reschedules per campaign;** a third void ends it as
  `NEEDS_EVIDENCE`.
- A bucket failing **after** its script started is retained with its terminal state, unscored, and
  makes its pair non-passing.
- `run_attempt > 1` never replaces attempt 1. The manifest enumerates **every** harness run in the
  window from the Actions API; an unaccounted run **voids the campaign**.

---

### 19.9 The campaign data model — two versioned artifacts, and who reads them (D-2, D-3)

The population rule of §6.4b and §15.1b is sound, but an earlier revision gave it **no data path**:
the manifest was said to be frozen before run 1 while `excluded_run_ids` must hold run IDs that
GitHub only allocates **after** dispatch; the ingest CLI and record action received no manifest or
exclusion input at all; and the authenticated-window check ran later, operator-side, **after** the
in-CI append/refit had already had its chance to fit. A rule no component can evaluate is not a
rule. It is split into two artifacts with different freeze times.

#### 19.9a `testbucket.campaign-config/v1` — frozen before the pilot, immutable thereafter

Everything knowable before dispatch: `schema`, `frozen_at`, the identity fields, the profile
constants, the declared counterbalanced sequence, the scheduled dates, `excluded_window.start/end`,
`excluded_orchestration_commits`, the pinned `store_artifact_id` + `store_sha256`, the four
frozen `model_parameters`, and `manifest.cache_state` — which carries the plan-time cache
**declaration** and nothing else, sealed at `frozen_at` and never mutated (contract §10.5.4). The
per-job cache **outcomes** are not in this document: they do not exist yet when it is frozen, and
one immutable record cannot hold eight mutable outcome sets (R13-D2). **`frozen_at` strictly
precedes the pilot**, not merely the campaign
(§19.6). Its digest is `model_parameters_digest`, asserted identical in both arms and unchanged from
before the pilot through campaign completion.

#### 19.9b `testbucket.campaign-attempts/v1` — appended after each dispatch

The ordinary post-dispatch ledger: one `manifest.attempts[]` record per Actions run the harness
starts, carrying `run_id`, `run_attempt`, `job_ids`, `pair_index`, `arm`, authenticated
`started_at`, and a `disposition` from `{scored, void-pre-start, rescheduled, post-start-failed,
unaccounted}`, plus the `manifest.pairs[]` tree with each arm's authenticated `started_at` and
`completed_at`. This is what makes "every Actions API run has a disposition", pre-start voids,
reschedules, ITT, bucket completeness, and the sequential-order predicate **serializable and
checkable** rather than asserted. It is written by the operator with ordinary API credentials — no
signing, no protected environment.

#### 19.9b-1 Which document owns which path (D-2, R9-D3; moved here by R21-F2)

The two campaign documents share one path tree, and the field registry's `owner` key says which of
them carries each path. The rule that key encodes is this, and it is stated here rather than in a
YAML comment:

- the **skeleton containers and their identity keys** — `manifest.pairs[]`, `pair_index`,
  `arms[]`, `arm` — are owned by **both**. §19.9a declares them before dispatch and §19.9b restates
  the same skeleton to hang execution values on, so every child path has a same-or-both-owned
  ancestor;
- `declared_order` and each arm's `est_basis` are **config-only declarations**;
- the **execution leaves** — `run_id`, `run_attempt`, `started_at`, `completed_at`, `bucket_count`,
  `scored` — are recorded in the **attempts** document after dispatch, at the same paths.

The restatement is deliberate: an owner-filtered inventory has to be emittable from either document
alone. An earlier revision owned the containers in `attempts` while owning two of their children in
`config`, which no such inventory could emit.

#### 19.9c How the exclusion actually reaches the component that fits (R8-D2)

An earlier revision had `ingest` reject rows whose `run_id` was "in the config's harness-run
universe" — but the config is frozen **before dispatch** and GitHub allocates those run IDs
**after**, so that universe can never be in the file ingest reads. It also filtered on the
self-reported `observed_start_realtime`, which §13 forbids as a window source, and it rejected pilot
rows before append while §19.6 and test 61 require those same rows retained as diagnostics. The
path is rebuilt so every step reads something that exists when it runs.

**The campaign identity travels with the observation, not with a list of future run IDs.** The
frozen config declares one opaque `campaign_id`; the orchestrator passes it to `run-bucket` as
`campaign-id`, and every observation the campaign produces carries it. Ingest then needs no
knowledge of run IDs at all.

| Step | Component | Input it receives | What it does |
|---|---|---|---|
| 1 | `run-bucket` | `campaign-id` from the orchestrator | stamps `campaign_id` onto every observation it writes; **no filtering** |
| 2 | `record` action | new inputs **`campaign-config-json`** and **`campaign-config-digest-expected`** | materializes and verifies the frozen bytes job-locally (§19.9c-1), then passes that local path to `ingest --campaign-config` |
| 3 | `ingest` CLI | new flag **`--campaign-config <path>`** | **appends every qualifying row to the ring as a diagnostic**, and sets `trainable` **fail-closed** (R9-D2): a row whose `campaign_id` **matches** the config's is `trainable: false`; a row carrying **any other** `campaign_id` is **rejected** as a miswired arm; and a row carrying **no** `campaign_id` while a config is supplied is likewise **rejected**. Only a row from a run with no campaign config at all — an ordinary CI run — is `trainable: true`. Nothing that is accepted is discarded, so §19.6's "retained as a diagnostic" and this rule agree |
| 4 | `ingest` CLI | the same artifact | **fits only over rows with `trainable: true`**, and under §14.2 does not fit at all when the append left that selection unchanged — which a `trainable: false` append always does. This is a durable property of the stored row, not a function of ambient `now`, so a delayed or retried ingest after the window closes still cannot train on a campaign or pilot row |
| 5 | operator validator | §19.9a + §19.9b | re-checks with **authenticated** instants and the real run IDs from the attempt ledger, and asserts `model_parameters_digest` is byte-identical at three points: at `frozen_at`, between pairs, and after the last scored run |

**Why this is durable where the earlier rule was not.** The old step 4 froze fitting only while
`now` fell inside `excluded_window`; a late ingest could refit afterwards. `trainable` is written
once, at append, from data the row carries, so the exclusion survives any ingest schedule. The
authenticated window remains the operator-side cross-check of step 5, which is the only place
authenticated instants are available (§18.2).

**Step 4 is what makes the sequence safe.** **There is no rollback protocol because there is nothing
to roll back.** The `trainable` flag is set at append from the row's own `campaign_id`, so no fit can
ever consume a campaign or pilot row, at any time, under any ingest schedule — and, since §15.1b
bounds the two retention classes independently, no campaign or pilot row can displace a trainable
row out of the fitted population either (R21-F4) — and, since §14.2 refits **only** when the append
changed the selected trainable population, an accepted diagnostic append does not invoke the fitter
at all (R22-F4). **Consumption, displacement, and invocation are the three ways a stored row can
reach a fit, and all three are closed.** An earlier revision of this sentence said there were only
two, which contradicted §14.2's own exhaustive count (R23-F5).

**The same append also materializes the two config-scoped domains (R13-D1).** When a config is
supplied, `ingest` additionally stamps `trainable: false` on a row whose `workload_commit` differs
from `manifest.workload_commit` or whose `head_sha` is in `manifest.excluded_orchestration_commits`
(§15.1b class A). `campaign_id` remains the **primary and fail-closed** rule — those two are ordinary
non-matching cases, not rejections — and the point of folding them in is that the fitter is then
left with **one** predicate, `trainable == true`, rather than a list it could apply inconsistently.

**The default is refusal, not admission (R9-D2).** An earlier revision marked a row non-trainable
only when its `campaign_id` *matched*, which left a miswired scored arm — wrong id, or none — on the
**trainable** path. Under a supplied `--campaign-config`, a mismatched or absent `campaign_id` is a
**rejection**, so the failure mode is a refused row rather than a silently poisoned fit.


**Run-id and authenticated-window predicates are operator-side cross-checks only.** They cannot be
the in-CI mechanism, because the run IDs they name do not exist when the config is frozen. §6.4b and
§15.1b describe them in exactly that role: the campaign validator re-derives them from the attempts
document after dispatch, and a disagreement with the `trainable` marking fails the campaign.

**The pilot carries the same `campaign_id`.** Pilot observations are therefore appended and
**retained as diagnostics** — visible, queryable, counted in the attempted population — while
`trainable: false` keeps them out of every fit. That is what §19.6's "the pilot cannot change
anything" always meant, and the two statements no longer contradict: the rows are kept *and* they
never train. The alternative the owner did not choose — permitting pilot training and withdrawing
the no-change claim — is recorded here as the rejected option.

**Acceptance test.** `TestCampaignRowsNeverRefitTheModel` (test 61) drives the whole state machine:
a **matching** campaign row is **appended and marked `trainable: false`** — retained, not rejected;
a row that bypasses step 3 still cannot alter a coefficient because of step 4; a pilot row is
ingested, retained, and never trains; and
`model_parameters_digest` is byte-identical before the pilot, between pairs, and after the last
scored run.

---

#### 19.9c-1 How the frozen config reaches the record job (R24-F3)

The same physical fact §10.5.2 states for the cache declaration applies here: separate Actions jobs
share no filesystem, so a caller pathname carries no content. An earlier revision named a
`campaign-config` input and passed it straight to a path-valued flag, which no record runner could
satisfy. The transport is therefore the same verified content-plus-digest pattern:

| # | Step | Normative requirement |
|---:|---|---|
| 1 | **Supply** | the orchestrator passes the frozen §19.9a document **as content** in `campaign-config-json`, and its SHA-256 in `campaign-config-digest-expected`. No pathname crosses the boundary |
| 2 | **Materialize** | the record job writes `campaign-config-json` verbatim to a **job-local** file |
| 3 | **Verify** | the record job recomputes SHA-256 over those bytes and **fails the job before invoking `ingest`** unless it equals `campaign-config-digest-expected`. Content and expected digest are perturbable independently, and either failure is a job failure |
| 4 | **Bind** | `ingest --campaign-config <local path>` reads the verified bytes, and the config's `manifest.campaign_id` is the value each observed row's `campaign_id` is compared against — a row whose id does not match the **verified** config is refused, never silently trainable |

**Required only for scored or pilot ingestion (R24-F3).** An earlier revision said the config is
required "when ingesting wall observations", which contradicted §15.1b's ordinary path: an unscored
run with no `campaign_id` ingests with **no config at all** and its rows are `trainable: true`. The
config is required **iff** the ingested set contains a scored or pilot row — that is, iff any row
carries a `campaign_id` or `profile.scored: true` — which is exactly the condition §15.1b rejects a
row for when the config is absent.

## 20. Preservation gates

### 20.1 v0.2.2 exact-path atoms

`assignFilterAtoms` unchanged: substring selection checked in both directions at the workspace root
and at every shared possible project-root suffix, conservative Unicode folding, union-find
transitive closure over collisions and existing atoms, each atom rendered once, members never
name-sliced apart. Every file enters the argv as a `./x` path token. `exact_paths_test.go` retained
verbatim.

**`exact_paths_test.go` is generic and does not bind the pinned universe.** The workload-specific
property — 42 pairs, zero Case crossings, transitive closure, no split atom over Mandel's 1,512
paths at `d9ae1d43` — is bound by
`TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm` (§20.6a, S-8), which runs the production
implementation over the full pinned path set rather than two illustrative examples.

**The property claimed is conservative over-grouping.** Byte-identity with Vitest 4.1.10's internal
matcher is not claimed (ledger A-21).

### 20.2 Audit and coverage

The plan-time never-drop-a-test gate, record-time `AuditCoverage`, and `ParseShardPlan` /
`PlannedCoverageForBucket` / `BucketIndexIn` single-read semantics are retained. QC10 makes a wall
row learnable only when its bucket audit passes.

### 20.3 baml-rest — no ordinary source path first, execution neutrality second

**Primary guarantee: no ordinary source path from this branch.** Stated with its two halves kept
apart, because they are not equally strong:

| Half | Mechanism | Strength |
|---|---|---|
| action source | `uses: …@551d49cea6a74a99706912fe18504f318bdba1cc` | a commit SHA — content-immutable |
| binary | `version: v0.1.1` passed as `TB_VERSION` to `install-testbucket.sh` | a **release-tag request** resolved at run time |

At v0.1.1 that input is documented as "the moving v1 alias, an exact vX.Y.Z, or `local`", so the
binary half is a *name*, not a digest. A moved release tag is therefore the one way this isolation
could weaken — a deliberate owner release action, visible in the release history, and not a
consequence of this work. Calling it an "immutable pin" would overstate the binary half; earlier
revisions of this scope did, and this one does not.

At `ff3012b1` that commit deleted the old monolithic baseline and the in-repo testbucket copy,
leaving the bucketed workflow as the only gate: K=6, `count=100`, race, serial `-p=1`, its module exclusions and node-prefix
handling, a guaranteed cold-start plan check, post-ingest next-plan validation, once-only gates,
and standalone `GOWORK=off` checks.

**Within testbucket**, `TestGoAdapterRendersTheBamlRestContract` and
`TestGoEventsAndAuditAreUnchanged` are retained.

**Go consumer safety contract (G1).** The surface that must not change, and the check owed on
**every repin**:

| Unchanged Go surface | Pinned by |
|---|---|
| rendered script bytes for a given bucket | RM-2 |
| canonical token `-race -count=100` | RM-3 |
| `-p=1`, `-timeout 20m`, count shards summing to the sweep | RM-3 |
| `go test -json` event parsing and `AuditCoverage` results | RM-4 |
| `needs_node` derivation | RM-1 |
| refusal of `--wall-dir` under `--runner go` | RM-11 |
| absence of `testbucket wall`, `spec-`, `--level invocation` in any Go script | RM-2 |

**Repin compatibility test — required before any consumer moves to a new testbucket revision.**
`TestGoConsumerSurfaceUnchangedAcrossRepin` renders the baml-rest-shaped bucket at both the
currently pinned revision and the proposed one and asserts every row above is byte-identical, and
that the proposed revision accepts the pinned revision's exact action input set with no newly
required input. A repin that changes any row is a **major** change for that consumer and needs its
own migration note. This is owed on every repin, not once.

**Do not broaden this to "no Go or core change."** Shared planning, store, and invocation schemas
changed additively. A future baml-rest upgrade needs its own compatibility run: `--file-parallelism`
did not exist at v0.1.1 and does at v0.2.2 (verified: 0 vs 3 occurrences in `cmd/testbucket/main.go`
at the two revisions). The supportable claim is **current-pin isolation plus default execution
neutrality**. baml-rest is not asked to adopt this Vitest-only work.

### 20.4 Mandel unit-only safety — project-based predicate, with a static audit

K=8, `count=1`, `file_parallelism=1`, the façade, `TB_DISCOVERY_EXCLUDE_PREFIXES=shared/f/lib/cases/`,
`MONGOMS_RUNTIME_DOWNLOAD=false`, the misc lane, and the fail-closed aggregate gate are unchanged.
`TestMandelVitestContract` is retained, including its assertion that measurement changes the
**script** and not the **invocations**.

Three projects: `unit` (excluding `**/integration-tests/**`, `packages/region-router/**`, and the
case-replset glob), `case-replset` (serial, real one-member replica set,
`shared/f/lib/cases/**/*.replset.test.ts`), and `harness-unit` (pure, no-infra,
`integration-tests/lib/purchase-order-recon-extraction/__tests__/**/*.test.ts` — 8 files). The
façade discovers via `globTestSpecifications()` across all projects without importing test code,
then drops paths under the excluded prefix; the misc lane runs that prefix.

```
allowed projects   = { unit, harness-unit }
forbidden projects = { case-replset }
forbidden paths    = shared/f/lib/cases/**
```

**"Zero discovered paths under `integration-tests/`" is wrong** — the legitimate `harness-unit`
project lives there.

**Static audit at `d9ae1d43`:** of 1,512 tracked `.test.ts` paths, 1,444 are in the root-config
union — **1,396 bucket files and 48 excluded Case files** — with **42 collision pairs inside the
bucket set and zero planned-filter-to-excluded-Case collisions**; all paths ASCII, so conservative
locale folding cannot hide a cross-boundary pair at that revision.

**Enforcement before `B_1`:** run the façade's discovery once; capture the raw `[{name,file}]`
bytes; record their SHA-256 and file count in the manifest; assert (a) every discovered file
resolves to an allowed project under the frozen config, (b) zero files match the excluded prefix,
(c) both arms' `discovered_unit_set_digest` and `expanded_unit_set_digest` are identical, (d) the
counts match the audit above, and **(e)** every workload input — façade, config, lockfile,
`package.json`, tree digest, façade argv, exclusion prefixes — equals the value **recomputed from
the checkout at `workload_commit`** (§19.2a). Steps (a)–(d) alone are satisfiable by a
self-consistent substituted partition; only (e) binds the run to the intended revision. **If the fallback of §19.3 is used**, the audit is re-derived at the
new workload commit and the `d9ae1d43` numbers are not carried over.

Offline safety is the **façade's**: `prepareOfflineUnitEnvV1` seals only the `run` shape; the
`list --filesOnly` shape imports no test code. testbucket must not weaken it and does not touch it.

### 20.5 Matrix compatibility

`TestMatrixSemanticsAreUnchanged` retained and extended: `est_seconds` numeric and one-decimal in
both bases; `needs_node` unchanged; new fields additive.

### 20.6 Consumer snapshots

```
testdata/consumers/mandel/
    vitest.config.ts                       full file
    package.json                           full file
    scripts/tb-vitest.ts                   full file  — the facade
    scripts/run-unit-tests.ts              full file  — facade helper (offline seal, preflight)
    scripts/cli-main-module.ts             full file  — facade helper (main-module guard)
    .github/actions/setup-case-test-mongo/action.yml   full file — setup composite
    .github/workflows/unit-tests-bucketed.yaml         full file
    pnpm-lock.excerpt.yaml                 excerpt    — full-file digest recorded in SOURCE.md
    tracked-test-paths.txt                 the membership input (jj file list at the pinned commit)
testdata/consumers/baml-rest/.github/workflows/unit-tests-bucketed.yml   full file
testdata/consumers/SOURCE.md               repo, commit SHA, per-file SHA-256, capture date
```

Eleven paths. The two façade helpers, the setup composite, and the tracked-path list are the ones
§20.6a's entrypoint and membership claims actually depend on, so they are listed here rather than
implied. `tracked-test-paths.txt` is additionally the input to the production suffix-atom regression
of §20.6a (S-8).

`TestPinnedConsumerStoredFixtureIntegrity` asserts each digest and the constants this scope depends
on; `TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm` asserts the collision property over the
full pinned path universe (§20.6a, S-8). **Honest framing:** snapshots at a named commit make the
claims checkable offline; they do not prove the consumers' live state.

### 20.6a Pinned consumer fixture — an acceptance input in this tree (T1, C1, M1)

`testdata/consumers/` now exists in the working copy and is an **acceptance input**, not a promise.
`testdata/consumers/SOURCE.md` records the pinned revisions, per-file SHA-256 digests, the frozen
profile constants read out of the fixture, the selected/excluded membership derived at the pinned
revision, and the collision regression fixture.

Membership derived from `mandel/tracked-test-paths.txt` (`jj file list -r d9ae1d43…`):

| Set | Count |
|---|---:|
| tracked `*.test.ts` | 1512 |
| excluded — `shared/f/lib/cases/**` | 48 |
| excluded — `integration-tests/**` outside `harness-unit` | 67 |
| excluded — `packages/region-router/**` | 1 |
| **bucket universe** | **1396** |
| `harness-unit`, under `integration-tests/`, legitimately selected | 8 |

**Two collision relations, both fixed, and the production one is what the regression binds (S-8).**

| Relation | Rule | selected↔selected pairs | selected→excluded-Case pairs |
|---|---|---:|---:|
| **root containment** — an illustrative subset | Vitest's `testFile.includes(filter)` on root-relative paths | **2** | **0** |
| **production** — what `assignFilterAtoms` actually computes | containment checked in both directions at the workspace root **and at every shared possible project-root suffix**, with conservative Unicode folding and union-find transitive closure | **42** | **0** |

The two root-containment pairs are `lib/attribution/resolveSupplierUserAttribution.test.ts` inside
its `shared/f/` twin and `lib/keto/organizations.test.ts` inside its `shared/f/` twin — the
keto/attribution pairs the v0.2.2 atom rule exists to co-schedule. They are **examples**, not the
property. The safety claim depends on the broader shared-project-root suffix rule, whose audited
result at this revision is **42 selected↔selected pairs and zero selected→excluded-Case pairs**.

**The regression must exercise the production algorithm over the full pinned universe.** Asserting
only the two root-containment examples leaves a regression in project-root suffix enumeration free
to pass while invalidating the workload safety claim.
`TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm` feeds **all 1,512 tracked `*.test.ts` paths**
from `testdata/consumers/mandel/tracked-test-paths.txt` — the 1,396 selected bucket paths, the 48
excluded Case paths, and the 68 otherwise-excluded paths — through the **production
`assignFilterAtoms` implementation** (`internal/runner/vitestrunner/collide.go`) and asserts:

| # | Assertion |
|---|---|
| 1 | exactly **42** selected↔selected collision pairs |
| 2 | exactly **0** selected-filter → excluded-Case collision pairs |
| 3 | **transitive closure**: the union-find atoms are closed under the collision relation — every pair in (1) has both members in one atom, and no atom contains two members with no collision path between them |
| 4 | **no atom is split** across an invocation boundary or a bucket boundary, for a K=8 plan over the selected set |
| 5 | every selected path enters the argv as a `./x` path token |

These values already pass at this snapshot; this is a **missing regression**, not a request to
change the algorithm. `TestPinnedConsumerStoredFixtureIntegrity` keeps its narrower job — digests
and the six membership counts — and no longer carries the collision claim alone.

**Fixture contract (F8/S6), stated once in `testdata/consumers/SOURCE.md`.**
The fixture is **direct entrypoint evidence, not an execution closure.** The earlier "full-file
execution closure" claim was **false**: `scripts/run-unit-tests.ts` imports `offline_replset.ts` and
`temporary_paths.ts` and executes `replset_preflight.ts`, and
`.github/actions/setup-case-test-mongo/action.yml` executes `replset_preflight.ts` and
`prepare-case-mongod.ts` — none of which is in the fixture. A snapshot that stops at the direct
entrypoints does not make the transitive preflight/setup graph reviewable, and no longer says it
does.

What the fixture **is**: the **direct entrypoints** this scope's constants are read from —
`package.json`, `vitest.config.ts`, `scripts/tb-vitest.ts`, `scripts/run-unit-tests.ts`,
`scripts/cli-main-module.ts`, `.github/actions/setup-case-test-mongo/action.yml`, and both
workflows — stored complete with whole-file digests; plus one **digest-only** file,
`pnpm-lock.yaml` (1.28 MB, not vendored), whose full-file digest is recorded while a stored excerpt
proves only the constants quoted from it. **No assertion claims an excerpt equals a full file.**

What supplies the **complete** closure for campaign purposes is the **workload commit identity**:
§19.2a recomputes every workload value from a checkout at `workload_commit`, which covers the
transitive graph the fixture does not.

**Acceptance tests.** `TestPinnedConsumerStoredFixtureIntegrity` validates **only the files actually
in the fixture** — full files against full-file digests, the excerpt only against its own excerpt
digest — and re-derives the six membership counts from `tracked-test-paths.txt`.
`TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm` binds the collision property to the
production algorithm over the full 1,512-path universe, as specified above.

### 20.6b Adoption gate (M1)

The workload claim is **not proved** until adoption happens, and the documents say so rather than
implying otherwise. Neither pinned consumer runs this line: Mandel `d9ae1d43` pins v0.2.2,
baml-rest `ff3012b1` pins `551d49ce`/`v0.1.1`.

The gate, all four required before any statement that a real consumer exercises the product:

| # | Gate |
|---|---|
| AG-1 | the fixture of §20.6a is present and `TestPinnedConsumerStoredFixtureIntegrity` passes |
| AG-2 | the external orchestrator of §19.3 runs against a checkout of `d9ae1d43`, and §19.2a's recomputation matches the fixture's digests |
| AG-3 | one full non-scored dry run produces eight qualifying observations for all of `0..7` |
| AG-4 | the campaign completes with its manifest accounting for every harness run in the window |

Until AG-1…AG-4 all hold, the product is described as **unadopted**, and no document claims
otherwise. baml-rest is exempt: the work is Vitest-only (§20.3).


---

## 21. Interfaces after simplification

**The interface rule is closed here, and nothing outside this contract defines it (R17-F1).** The
reusable workflow exposes the retained input set plus **exactly** the scored union stated below, and
**no other input and no secret at all**; the `plan` job's outputs are exactly `matrix`,
`cache-declaration-json` and `cache-declaration-digest` (§10.5.2 steps 2 and 0b; an earlier
revision said the output was reduced to `matrix` alone, which contradicted them — R23-F4);
`contents: read`
applies throughout; and `candidate-digest`, `preflight` and `candidate-resolver.sh` are deleted.
Because the permitted set is stated positively and exhaustively, the *removed* set is its complement
and needs no separate authority.

`component-map.json`'s removal inventory is **evidence of the simplification** — a record of which
inputs and secrets the R54 tree carried — and carries no requirement. An earlier revision said the
removed inputs and secrets "are enumerated in `component-map.json`" and that the "exact sets" live
there, which gave a `DERIVATION` companion operative authority outside either declared registry
domain (R10-D5, S-10 superseded on this point).

**Added inputs, and why each exists.** These are the executable half of the S-4 and S-5 repairs: a
rule that says a value is "passed explicitly from the orchestrator" is not implementable until the
input carrying it exists.

| Action | Added input | Required when | Purpose |
|---|---|---|---|
| `plan` | `est-basis` | always (defaults `reporter`) | mode selection (§16.1) |
| `plan` | **`runner-class`** | **required, non-empty, for `scored: true`** (AD-8) | the stable execution-class leaf of the comparability key (§15.3, S-4). It replaces `runner_name`, which had no cross-run stability contract |
| `plan` | **`runs-on-label`** | **required, non-empty, for `scored: true`** (AD-8) | the *producer* for the `runner_image_label` key leaf. No GitHub context yields the caller's resolved `runs-on` label, so the caller passes it (§15.3, S-4) |
| `run-bucket` | **`runs-on-label`** | **required, non-empty, for `scored: true`** | recorded into the observation as `observed_runs_on_label` and compared against the key's `runner_image_label` by **QC12**; a divergence is a reported misconfiguration, not silent tolerance |
| `run-bucket` | **`cache-declaration-file`** | **required for `scored: true`** (AD-9) | the job-local file the matrix job materialized from the `cache-declaration-json` workflow output — the **immutable plan-time declaration** of §10.5.0. `run-bucket` verifies its digest, copies it verbatim into the observation, and then **fills every leaf §10.5.0 classes as `outcome`** — from the declared restore producer, from its own input validation, and from `MONGOMS_SYSTEM_BINARY` (R16-F2: an earlier row named three of them, which omitted the producer, the hit witness, the path and the runner verdict). **QC14a** and **QC14b** validate the assembled block of contract §10.5.0 across the job boundary (contract §10.5.6); no count of its leaves is written here or anywhere (R15-F2) |
| `run-bucket` | **`cache-declaration-digest`** | **required for `scored: true`** (AD-9) | the SHA-256 the plan job published as a workflow output. `run-bucket` recomputes it over the materialized bytes and **fails the job on any difference**, which is what makes cross-job byte identity a checked property rather than an assertion about a shared pathname (contract §10.5.2, R13-D2) |
| `run-bucket` | **`dependency-cache-producer`** | **required for `scored: true`** | `none`, `action`, or `caller`, and nothing else — the wire representation of contract §10.5.3's "the workflow declares which". Without it the choice was stated but unrepresentable (R14-F2), and without **`none`** a `disabled` run had no legal value to write (R15-F2). It is recorded as `cache_state.dependency_cache_producer`, and the caller's result as `cache_state.dependency_cache_hit`, so QC14b re-derives the presence pattern from the row |
| `run-bucket` | **`dependency-cache-matched-key`**, **`dependency-cache-hit`** | **required, both, iff `dependency-cache-producer == caller`; both must be ABSENT under `action` and under `none`** | the restore step's own outputs, passed in when the *caller* owns the restore — the shape the pinned Mandel workflow has. `dependency-cache-hit` is `true` or `false` and never empty, so an empty matched key **with** a `false` hit is a caller-owned **miss** while an empty matched key **without** a hit is **absent producer data** — the discriminator R14-F2 found missing. Supplying either under producer `action` **fails the job**, so "both producers" is rejected rather than silently resolved |
| `run-bucket` | **`campaign-id`** | **required for `scored: true`, and for the pilot** | the frozen config's opaque campaign identity, stamped onto every observation so `ingest` can set `trainable: false` without knowing any run ID (R8-D2) |
| `run-bucket` | **`candidate-sha`** | **required for `scored: true`** | the testbucket commit the executing binary was built from, written into the observation as `candidate_sha`. Neither the runner context nor the binary carries it, so the caller that built the binary passes it (S-6, QC15) |
| `run-bucket` | **`workload-commit`** | **required for `scored: true`** | the consumer checkout the bucket ran against, written into the observation as `workload_commit`. The orchestrator checked the workload out at a full SHA (§19.3), so it is the only party that knows it (S-6, QC15) |
| `plan` | **`scored`** | always (defaults `false`) | **the producer for `profile.scored` (D-4).** Nothing else can derive it: basis does not imply it — scored **B** runs `reporter`, and an unscored warm-up may also run `reporter` — so it is an explicit boolean input. It gates PD-2's phase-2 veto and AD-8…AD-10 |
| `plan` | **`cache-declaration-file`** | **required for `scored: true`** (AD-9) | the **job-local** file the plan job materialized and verified at §10.5.2 steps 0b–0c. The **same immutable declaration leaves** §10.5.0 classes as such, which `run-bucket` also receives. `plan` validates only those and **refuses before emitting a matrix**; it never sees a matched key, a disposition, or an executed digest, because those do not exist yet (R11-D3). It publishes the canonical bytes and their digest as the job outputs `cache-declaration-json` and `cache-declaration-digest`, which is how they cross the job boundary (contract §10.5.2) |
| `plan` | **`candidate-sha`**, **`workload-commit`** | **required for `scored: true`** (AD-10) | likewise passed to `plan`, so AD-10 is enforceable at the component the rule names |
| `run-bucket` | **`shard-plan`** | **required for `scored: true`** | carries the canonical `profile` block, copied verbatim into the observation so **QC13 has a real byte path** (D-4) |
| `record` | **`wall-observations-dir`** | when wall observations are ingested | the record hop of §14.1 and §14.2 — the directory `ingest --wall-observations` reads |
| `record` | **`campaign-config-json`**, **`campaign-config-digest-expected`** | **required iff the ingested set contains a scored or pilot row** (R24-F3) | the frozen §19.9a document as **content** plus its expected SHA-256. §19.9c-1 materializes and verifies them job-locally, then passes the local path to `ingest --campaign-config` so `trainable` is set **at append** (D-3, R8-D2) |

**The reusable workflow exposes every scored input (R8-D6).** An earlier revision gave the `plan`
action six scored inputs while the reusable workflow exposed only three, so a caller could set
`scored: true` through the workflow and then fail AD-9/AD-10 with no way to supply what they need.
The workflow's input set is therefore **exactly** the union it must pass through — `est-basis`,
`scored`, `runner-class`, `runs-on-label`, `cache-declaration-json`, `cache-declaration-digest-expected`,
`dependency-cache-producer`, `dependency-cache-matched-key`, `dependency-cache-hit`,
`candidate-sha`, `workload-commit`,
`campaign-id`, `wall-observations-dir`, `campaign-config-json`, `campaign-config-digest-expected`.

**Four representations, each defined once (R13-F1).** An earlier revision said "the last two reach
the record job", a positional claim that silently pushed `wall-observations-dir` off the record job
when R24 split one config input into two (R12-F3). A later revision, now superseded, called those
accounts *ordered per-action partitions* and required them **equal in both directions** — a claim
none of them can satisfy, because they do not share a domain: `plan` materializes two caller inputs into one action
input, `run-bucket` receives three inputs the caller never supplies, and the scored-required
projection deliberately omits a defaulted and two conditional names. Equality between unlike
projections is not a stronger check; it is an untrue one. The four representations, their domains and
their ordering are therefore defined here, and only relations that exist are asserted:

| Symbol | What it is | Domain | Order |
|---|---|---|---|
| `U` | the reusable workflow's **caller-input union** — the list immediately above | caller inputs | **ordered**; this list is the order |
| `W[job]` | the **route relation** from `U` to jobs: which jobs each caller input reaches | caller inputs → jobs | each `W[job]` is an **ordered subsequence of `U`** |
| `A[action]` | the **added action inputs** of `plan`, `run-bucket` and `record` | action inputs | **ordered**, canonically, by the action table above — declared, not inferred |
| `S[action]` | the **scored-required** projection: which of `A[action]` a scored run's caller must supply | a subset of `A[action]` | inherits `A`'s order |

**`W` is a route, not a partition.** `runs-on-label`, `candidate-sha` and `workload-commit` each
reach **more than one** job, so the `W[job]` sets are **not disjoint** and no partition arithmetic
applies to them. What is required is exactly this: every `W[job]` is an ordered subsequence of `U`,
and every member of `U` reaches **at least one** job.

| Job | `W[job]` — the caller inputs it receives |
|---|---|
| **plan job** | `est-basis`, `scored`, `runner-class`, `runs-on-label`, `cache-declaration-json`, `cache-declaration-digest-expected`, `candidate-sha`, `workload-commit` |
| **bucket job** | `runs-on-label`, `dependency-cache-producer`, `dependency-cache-matched-key`, `dependency-cache-hit`, `candidate-sha`, `workload-commit`, `campaign-id` |
| **record job** | `wall-observations-dir`, `campaign-config-json`, `campaign-config-digest-expected` |

**The transport transformation `T(W, plan outputs) → A`.** `T` fixes **membership**; `A`'s order is
the canonical one declared above. It cannot fix order as well, because the inputs a job receives from
the **plan job's outputs** have no position in `U` at all:

| Action | `T` | Why |
|---|---|---|
| `plan` | `cache-declaration-json` and `cache-declaration-digest-expected` **collapse to** `cache-declaration-file`; every other `W[plan]` member is identity | §10.5.2 steps 0b–0c materialize the content and verify it against the expected digest inside the plan job |
| `run-bucket` | every `W[bucket]` member is identity, **plus** `cache-declaration-file`, `cache-declaration-digest` and `shard-plan` **injected from the plan job's outputs** | §10.5.2 step 3: matrix jobs consume the plan job's published bytes and digest, never a caller pathname; `shard-plan` is likewise produced by `plan` |
| `record` | identity on all of `W[record]` | the record job materializes and verifies the config per §19.9c-1 before invoking `ingest`; no name changes |

**`S` is `A` minus what a scored caller need not supply, and is never compared as `A`.** Exactly two
classes are excluded, by name: the **defaulted** input `est-basis`, and the **producer-conditional**
inputs `dependency-cache-matched-key` and `dependency-cache-hit`, which are required **iff**
`dependency-cache-producer == caller` and forbidden otherwise. `S[action]` is therefore the ordered
subsequence of `A[action]` that omits those three names, and nothing else. An earlier revision let
`S` stand in an equality with `A`; that comparison is now forbidden by name.

**Canonical `A`, in order.** `component-map.json`'s `action_interfaces` is the derived projection of
this and is compared against it **in order, in both directions**:

| Action | `A[action]`, canonically ordered |
|---|---|
| `plan` | `est-basis`, `runner-class`, `runs-on-label`, `scored`, `cache-declaration-file`, `candidate-sha`, `workload-commit` |
| `run-bucket` | `runs-on-label`, `cache-declaration-file`, `cache-declaration-digest`, `dependency-cache-producer`, `dependency-cache-matched-key`, `dependency-cache-hit`, `campaign-id`, `candidate-sha`, `workload-commit`, `shard-plan` |
| `record` | `wall-observations-dir`, `campaign-config-json`, `campaign-config-digest-expected` |

§22 test 71b asserts exactly three relations — `U ↔ W`, `T(W, plan outputs) ↔ A`, and
`A ↔ action_interfaces` — plus `S` against its two declared exclusion classes. No relation between
representations that do not share a domain is asserted anywhere, and no positional count appears in
any of them.
**The campaign config is part of the union (R23-F3):** the workflow can ingest a scored or pilot row,
§15.1b now rejects a tagged row whose config never arrived, and an interface that exposed the campaign
identity without it would make that rejection the *only* possible outcome for every scored run.
**The declaration crosses as content, not as a pathname (R23-F4):** the caller supplies the canonical
JSON and an expected digest, and §10.5.2 hop 0 materializes and verifies them inside the plan job. `component-map.json`'s
`workflow_interface` records the same set for the inventory's benefit. Exposing `scored` without the rest is a **defect the
interface test rejects**. The alternative the owner did not take — declaring the reusable workflow
unscored-only — is recorded here as the rejected option.

**The workflow also declares two outputs, and they are the transport (R13-D2).** The reusable
workflow re-exposes the plan job's `cache-declaration-json` and `cache-declaration-digest` as
**workflow outputs** of the same names, and every matrix job materializes the first to a job-local
file and passes the second to `run-bucket`. Before this, no declared interface carried the
declaration's *bytes* across a job boundary at all — only a pathname, which two jobs on two runners
do not share. `component-map.json`'s `workflow_interface` records both outputs, and
`TestScoredCacheDeclarationTransportAndBinaryBinding` (test 70) fails if either is absent or if
`run-bucket` accepts a declaration whose digest does not match.

**Every admission check now runs at a component that has its inputs (D-4).** AD-9 and AD-10 required
the *plan* to refuse before matrix emission while `cache-declaration-file`, `candidate-sha`, and
`workload-commit` were **only run-bucket inputs** — the plan could not evaluate its own rule. Each
of those inputs is now a plan input too, and `run-bucket` keeps them because it writes them into the
observation. One declaration, two consumers, one enforcement point each.

**Canonical profile bytes reach the observation (D-4).** QC13 requires every observation to carry
the plan's profile block **byte-for-byte**, but matrix entries carry no profile and `run-bucket`
received none, so there was no byte path. `run-bucket` gains **`shard-plan`** — the same shard-plan
artifact the record job reads — and copies `profile` out of it verbatim into the observation. The
plan writes the block once; `run-bucket` copies it; QC13 compares. No re-derivation anywhere.

**`setup_command`'s producer is the plan action's own input (D-4).** Describing it as a `run-bucket`
input "echoed into the plan document" was backwards — the plan already exists by then. Every
comparability leaf is sourced from the **plan** job, and `setup_command` is the `plan` action's
`setup-command` input. `run-bucket` receives the same value for execution; the key reads the plan's.

**One workflow expression drives both jobs and both actions (D-4).** Passing a declared
`runs-on-label` string to two actions proves only that the same string was copied twice. The calling
workflow must define the label **once** — as a workflow-level `env` value or a reusable-workflow
input — and use that **same expression** in *all four* places: `jobs.plan.runs-on`,
`jobs.<bucket>.runs-on`, the `plan` action's `runs-on-label`, and the `run-bucket` action's
`runs-on-label`. `TestExecutionProfileIsPlanTimeAvailableAndLabelBound` parses the workflow and
asserts the four references resolve to one definition; a literal label written separately in a
`runs-on:` selector **fails** the check even when its text happens to match. This is static
configuration binding, not fleet attestation.

**`candidate_sha` and `workload_commit` need inputs because nothing on the runner knows them.**
QC15 requires three separately present identities on every observation, and `head_sha` is the only
one GitHub supplies (`${{ github.sha }}`, the orchestration head). The other two are facts about what
the caller built and checked out, so a rule requiring them is not implementable until the inputs
carrying them exist — the same reasoning that adds `runs-on-label` for S-4.

`plan` and `run-bucket` must receive the **same** `runs-on-label` value from one caller expression;
that is what makes QC12's comparison meaningful rather than tautological. The actual runner instance
name is read by `run-bucket` from `${{ runner.name }}` into the diagnostic `actual_runner_name` and
is **never** an input and never a key leaf (`scope.md` §2A, S-4).

---

## 22. Test plan

1. `TestBoundaryInvariantsAndComponentSpans` — boundary invariants; `script_overhead_ns` and
   `wrapper_ns` computed and reported, all in integer nanoseconds.
2. `TestObservationRoundTrip` — `limitations` non-empty and naming the Exec-envelope, topology,
   runner-label, and cache-state limits.
3. `TestQualificationChecks` — QC1–QC17, one case each, asserting the exact rejection reason.
4. `TestDuplicateObservationRejectsAllRows` — a duplicate rejects **all** rows for the key.
5. `TestModelFittingIsDeterministicAndStatusAware` — determinism over a shuffled input set; clamping; rank-deficient column;
   `insufficient` and `degraded`; the fitted population is selected by §15.1b recency **before** the
   §6.8 summation sort, and the two are separately observable.
6. `TestExplicitWallBasisFailsClosed` — `--est-basis wall` fails with no model, `insufficient`,
   `degraded`, and `--file-parallelism 2`.
7. `TestAllocationUsesThePD3Objective` — allocation uses **contract §0.9's objective, evaluated in
   contract §1.1's domain**, and the test does not carry a second copy of the expression (R14-F1):
   it evaluates the contract's and asserts the whole-file overhead is charged **once per bucket that
   has any whole-file unit** and never per unit, that no term is divided before display, and that two
   plans over the same inputs are byte-identical.
8. `TestEstSecondsMatchesItsBasis` — `est_seconds` equals `round1(a_eta_ns/1e9)` in wall basis and the v0.2.2 reporter-work sum in
   reporter basis; `a_eta_ns` is serialized and the two agree exactly.
9. `TestWallEstSecondsIsShadowOnlyUnderReporterBasis` — never reaches `AllocationScore`.
10. `TestTopologyIsInvariantAcrossBases` — for one store and one live set, the expanded unit set is byte-identical
    under both bases, and only the partition differs.
11. `TestStoreMigrationPreservesReporterRows` — 1→2 preserves reporter rows and invents no wall
    history; 2→1 cold starts loudly.
12. `TestWallComparabilityProfileResetsHistory` — a key change clears wall rows only; `flags` change clears reporter rows; a `K`
    change clears neither.
13. `TestNoShippedStringOverstatesTheMeasurement` — §16.4's repaired strings asserted; no shipped
    string calls `A` the complete action or the job, or `V` the whole wrapper lifetime; and no
    shipped string claims an outcome-free allocation surface (see also item 24, same symbol).
14. `TestCampaignValidatorProfileAndPairInvariants` — profile (§19.4) and pair invariant (§19.2), including `K=9`, a bucket-set
    gap, a name/index mismatch, a differing `expanded_unit_set_digest`, and a mismatched-`est_basis`
    pair.
15. `TestCampaignProvenanceAndCutoff` — `fitted_at` after the first authenticated start fails; a
    ring containing a harness `run_id` or a row inside the excluded window fails; an unnamed
    exclusion domain fails.
16. `TestCampaignIdentitiesAreSeparateFields` — `orchestration_commit`, `workload_commit`, and
    `candidate_sha` are three separate required fields, each equal across arms, and none is
    derivable from another (S-6).
17. `TestCampaignDatesAreAuthenticated` — a scheduled date with no authenticated match fails.
18. `TestCampaignOrderIsCounterbalancedNotRandomized` — the manifest declares the fixed counterbalanced sequence (pairs 1,3,5 `B→C`;
    pairs 2,4 `C→B`) before run 1, the executed order matches it, and no shipped string contains
    *randomized*, *seed*, *draw*, or *shuffle* in a campaign-order context.
19. `TestPinnedConsumerStoredFixtureIntegrity` — **offline**: digests every stored fixture input
    (eight complete entrypoints, the lock excerpt, `tracked-test-paths.txt`) and re-derives the six
    membership counts. Reads no checkout and never the full `pnpm-lock.yaml` (R8-D8).
20. `TestCampaignDenominatorWording` — no shipped string says "eighty … per arm"; the constant is
    80 combined, 40 per arm; and no string says a 16-row pilot proves 80-row bookkeeping (S-9).
21. `TestBamlRestInputSetStillDrivesTheActions` — the v0.1.1 input set still drives them.
22. `TestMandelUnitOnlyPredicateIsProjectBased` — project-based classification accepts the 8 `harness-unit` files and rejects
    any `case-replset` file.
23. `TestSelectedWorkRequiresSingletonUnitEquality` — any surviving structure requires unit-list equality with its unit id;
    a multi-unit list is rejected as a label.
24. `TestNoShippedStringOverstatesTheMeasurement` (second case set) — no shipped string claims an
    outcome-free allocation surface, "no outcome-derived
    influence", or "mechanically pre-campaign"; the plan report names the three store fields that
    select topology.
25. `go test -race -count=1 ./...` green on Linux, in the OrbStack VM `tbtest`, not a container.
    **This round's reviewer explicitly claims no execution-test result**, so the suite's state is
    established by implementation, not inherited (ledger A-04).
26. `TestNoInferentialClaimIsShipped` — no shipped string in code, logs, schemas, or the campaign report asserts
    significance, power, a null hypothesis, a p-value, or a conclusion beyond the measured sample.
27. `TestThresholdsAreFrozenBeforeRunOne` — every value in contract §10.4 is a compile-time
    constant or a manifest field fixed before run 1. **It no longer compares two gate tables,
    because there is only one (R13-A).** The earlier form compared §19.7's copy against contract
    §10's on four dimensions — a test whose entire purpose was to stop two copies from drifting, and
    which therefore existed only because the duplication did. The duplicate is removed and the test
    asserts the stronger property: **contract §10 and §17.4 are the sole statement of the campaign
    gate set and its numeric values, and no other document in this package states a gate label, a
    threshold, or a comparison operator.** A gate restated anywhere else **fails** this test. It
    still rejects **retuning** — a changed percentage, bound, or refitted coefficient — and
    explicitly **permits** evaluating a frozen relative formula against observed C rows, which is
    what a relative gate is (S-2).
28. `TestImplementationGapTestsFailAgainstR54` — every gap in the `scope.md` §21.0 registry, enumerated from
    that machine-readable block rather than from a literal count, has an acceptance test that
    exists and fails against R54's current behaviour, so it cannot pass until the gap is actually
    closed (S-10).
29. `TestWorkloadBindingRecomputesFromPinnedCheckout` — a manifest whose `facade_sha256`, `vitest_config_sha256`, `lockfile_sha256`,
    `package_json_sha256`, `workload_tree_digest`, `facade_argv`, `discovery_exclude_prefixes`, or
    `discovered_unit_set_digest` disagrees with the value recomputed from the checkout at
    `workload_commit` is **rejected** — including the case where both arms agree with each other
    and both disagree with the pinned revision.
30. `TestPairsRunSequentiallyByAuthenticatedCompletion` — a pair whose authenticated instants
    contradict the declared counterbalanced **sequential** order is rejected; a pair whose arms
    merely satisfy launch order while overlapping (`completed_at(first) > started_at(second)`) is
    also rejected; and an empty or unparseable instant is a failure, not a skip (S-9).
31. `TestScoredPlanAdmission` — `scored: true` with `k != 8`, `count != 1`, `file_parallelism > 1`,
    a non-vitest runner, a bucket set that is not exactly `[0..7]`, an empty `runner-class` or
    `runs-on-label` (AD-8), an undeclared `cache_state` (AD-9), or an empty `candidate-sha` or
    `workload-commit` (AD-10) refuses to emit a matrix; the
    `profile` block is copied verbatim onto every observation.
32. Go repin: `TestGoConsumerSurfaceUnchangedAcrossRepin` — every row of the §20.3 surface table is
    byte-identical across the pinned and proposed revisions, and no newly required action input.
33. **PD-1** `TestReporterBasisPreservesLegacyProjectionAndDeclaresBasis`: the canonical
    v0.2.2-field projection is byte-identical for an unopted consumer; `est_basis`, the `A_eta_ns`
    and `wall_est_seconds` display
    metadata, and `expanded_unit_set_digest` are asserted separately as additive; an unopted
    invocation's script bytes are unchanged.
34. **PD-2** `TestColdStartModeSelection`: the four cases of contract §0.8, each distinct — cases 1
    and 2 plan, case 3 hard-errors with no matrix, case 4 rejects every cold or forced fallback.
35. **PD-3** `TestAllocationObjectiveEqualsAetaForWholeAndSliceTopologies`: optimized objective
    equals displayed `round1(a_eta_ns/1e9)` for whole-only, slice-only, and mixed buckets, and the `scope.md` §7.2 K=2
    counterexample partitions to makespan 200, not 250; refinement is deterministic across shuffled
    input order.
36. **S4** `TestObservationCopiesCanonicalProfileVerbatim`: a mutated, reordered, or retyped
    `profile` is rejected (QC13).
37. **S4** `TestWallHistoryFreezesPlanTimeFeatures`: `reporter_sum_ns` frozen at plan time and the topology counts
    are the plan-time values even after the store advances; a recomputed-at-fit-time value fails.
38. **S4** `TestLedgerClaimsMatchObservationSchema`: every claim in §18.0's table names a field the
    §13 schema retains, and no claim exceeds it.
39. **S6 / R8-D8** `TestPinnedConsumerCheckoutMatchesManifest`: **checkout-bound**, against a
    checkout at `d9ae1d43`; fails on a difference in full `package.json`, the **full
    `pnpm-lock.yaml`**, either helper, or the setup composite. Deliberately a **different symbol**
    from test 19's offline check — one reads stored bytes, the other requires a checkout, and one
    symbol cannot hold both contracts.
40. **S7** `TestCampaignVoidAndITTBoundary`: pre-start void; first and second permitted reschedules
    of the same precommitted pair; third void ends the campaign; post-start failure is retained and
    unscored; `run_attempt > 1` never replaces attempt 1; every run returned by the Actions API is
    accounted for.
41. **S8/F9/S-10** `TestScopeArtifactConsistency`: enumerates the `jj`-visible artifact inventory
    **from `jj file list -r @` and `jj status`, never from a number written in a document**, and
    parses the `scope.md` §2A field registry, the `scope.md` §21.0 delta registry, the canonical model parameter names,
    the action/workflow input set, the observation/profile/ring schemas, the `BC-INV` membership,
    the supersession table, and the ledger summary. **Fails on** an old parameter name
    (`per_invocation_ns`, `reporter_sum` where `reporter_sum_ns` is meant, `runner_name` as a key
    leaf, `comparability_key`, `degenerate_columns`, `plan_id`), a missing interface field
    (`est-basis`, `runner-class`, `runs-on-label`, `cache-declaration-file`, `candidate-sha`,
    `workload-commit`, `wall-observations-dir`), a duplicate or
    contradictory ledger status, a delta ID that appears out of order or without a resolvable test
    symbol, a section number that appears out of order, any recommendation that contradicts a frozen
    decision, and **any self-referential line count or SHA-256 of a document in this set** (S-10).
    **It also compares the `scope.md` §21.0 register table against the registry column for column (R14-F1)** —
    `id`, `title`, `r54_status`, and the acceptance-test symbols, in registry order — so the table is
    a checked projection rather than a second, drifting description of the same rows.
42. **F1** `TestWallModelAndPlannerUsePD3Objective`: fits a synthetic full-rank corpus with known
    four coefficients **in integer nanoseconds** and recovers them; reproduces identically after row
    shuffling; evaluates whole-only, slice-only, and mixed plans; and reproduces the `scope.md` §7.2 K=2
    counterexample at makespan **`200_000_000_000` ns, not `250_000_000_000` ns**, with every
    displayed `est_seconds` equal to `round1(a_eta_ns/1e9)` of the objective value used to accept
    each refinement move.
43. **F6/S-4/R11-D4** `TestWallComparabilityProfileResetsHistory`: mutating **each** comparability
    leaf enumerated from `memberships.comparability_key` clears wall rows and sets the model
    `insufficient` while reporter rows survive — **including the four inputs behind
    `cache_declaration_digest`**: a changed `dependency_cache_mode`, a changed
    `dependency_cache_primary_key`, a changed `transform_cache_mode`, and a changed
    `expected_mongo_binary_sha256` each reset history, while a changed
    `dependency_cache_matched_key` or `dependency_cache_disposition` **preserves** it, because those
    are per-job outcomes and not key inputs (R11-D4); a `K` change preserves history; a
    `runner_image_label` change resets history; a `runner_class` change resets history; and a change
    to `actual_runner_name` alone **preserves** history, proving the runner instance name is not a
    key leaf (S-4). **No image digest is constructed or required** — the label is the identity
    (§15.3).
44. **F7** `TestExecEnvelopeContainsFacadeVitestLifecycle`: ordered sentinels at façade entry,
    import/transform/setup, test body, reporter/shutdown, façade cleanup, root reap, and the closing
    read prove the documented prefix/suffix placement.
45. **F7** `TestProcessGroupDrainsBeforeEnd`: a cooperative same-group child outliving the root
    proves `TERM`→`KILL` escalation is bounded and that **no end timestamp is taken while the group
    exists**. No assertion claims descendant reaping.
46. **F8** `TestCampaignAttemptAccountingAndITT`: 10 scored runs / 80 rows with zero voids; a
    retained two-arm pre-start void plus reschedule giving **12 attempted and 10 scored**; two
    permitted void/reschedule events and rejection of a third; a post-start failure making its pair
    non-passing; `run_attempt > 1` unable to erase attempt 1; and rejection when any Actions-API run
    is absent from the manifest.
47. **F1** `TestWallModelUsesActionElapsedAsOnlyResponse`: build a row whose `A`, `VB`, `V[j]`, and
    component spans are deliberately unequal, fit it, and prove the response used is the top-level
    `elapsed_ns` (`A`) and no other quantity.
48. **F2** `TestScaleIsPredictiveOnly`: rejects causal/fraction phrasing — "accounts for", "fraction
    of", "causal" applied to `scale` — anywhere in a normative document.
49. **F2 / D-1** `TestEveryExposedEstimateDeclaresItsQuantity`: walks **plan, matrix, observation,
    `PlanSummary`, and the run-bucket banner** under **both** bases and proves each label, value, and
    the optimized objective agree; asserts every seconds-valued surface in §5.1's enumeration is
    present and no undeclared one exists; asserts `est_seconds == round1(a_eta_ns/1e9)` wherever both
    appear; asserts `a_eta_ns` is **present on plan buckets and observations under wall basis and
    absent under a reporter cold plan**, and **absent from matrix entries in both bases**; asserts
    **no wall-basis per-unit estimate exists** and that unit display stays reporter-labelled; and
    asserts the fitted `scale` is a dimensionless float while the other three coefficients are int64
    nanoseconds.
50. **F4** `TestWallModelRankAdmissionForWholeSliceAndMixedCandidates`: all-`I=1` with whole-only and
    slice-only candidates; all-`I=0` slice-only; `reporter_sum_ns`/`slice_count` collinearity despite
    multiple slice counts; full-rank mixed; and every bucket shape the refinement evaluator may
    score. This is the sole rank-admission test; no earlier rank test survives.
51. **SR-1 / R10-D2** `TestWallPlannerEscapesSingleMoveLocalMinimum`: asserts the mixed Stage-1
    seed at `250e9`; asserts **every single-unit move is non-improving** (`100/300`, `150/350`);
    performs the deterministic whole↔slice swap and obtains **makespan `200e9`**; asserts the
    displayed bucket estimate is the exact objective value used to accept the swap; and produces
    identical plan bytes for shuffled input. Three further cases pin the schedule itself:

    | Case | Asserts |
    |---|---|
    | **Stage 1 is KK, not LPT** | over the §22-published fixture universe, the seed equals the **generalized Karmarkar–Karp** result of `internal/core/partition.go` and **differs** from `longestProcessingTime` on the same weights, so an LPT substitution fails |
    | **no-restart schedule** | a fixture on which the normative *continue-through-the-pass* schedule and a *restart-on-first-accepted-move* variant reach **different canonical bucket vectors** at equal makespan; only the normative one is accepted |
    | **pass-cap stop** | a fixture where `REFINE_PASSES` is exhausted with an improving transition still available, asserting the planner **stops and returns deterministically** — and that no document calls that result a fixed point, a local optimum, or convergence |

52. **SR-2/S-6/R8-D3** `TestWallHistoryRecencyEvictionAndThreeIdentities`: round-trips **every field
    the recency key reads** — `repository`, `run_id`, `run_attempt`, `job_id`, `bucket_index`,
    `plan_digest`, `observed_start_realtime` — plus `head_sha`, `candidate_sha`, `workload_commit`,
    `run_started_at`, and `ingest_seq`, as **separately addressable** fields, and asserts the key is
    reconstructible **from the decoded ring row alone**, with no observation present (a row collapsing them is
    rejected by QC15); drives the ring past `W = 240` from two different ingest orders over the same
    observations and asserts the **same retained population and the same evictions** under the
    §15.1b recency key; asserts the §6.8 summation sort is applied only after selection and yields
    identical sorted bytes after shuffling; and asserts a row at the candidate SHA that is outside
    the campaign window is **retained**, so a warm corpus built by the candidate binary is reachable.

    **The two halves are asserted separately, and the boundary between them is itself an assertion
    (R13-D1).** An earlier version of this test excluded rows by `run_id ∈ excluded_run_ids` and by
    `run_started_at` inside the excluded window *and* asserted the decision came "from the decoded
    row with no ambient workflow state" — while test 61 says both predicates are operator-side and
    are not required in CI. No implementation satisfies both, so the test is split:

    | Half | Population it is driven over | Asserted |
    |---|---|---|
    | **ring / recency — in CI** | the decoded ring rows alone, with **no manifest and no API access** | retention, eviction, the recency key, the summation sort, and selection by **`trainable` only**. Supplying `excluded_run_ids` or a window to this half is a **failure**: the in-CI path must not read them |
    | **campaign validation — operator-side** | the same rows **plus** the attempts document and authenticated instants | `run_id ∈ excluded_run_ids` and `run_started_at` inside the window are evaluated here, and a disagreement with the rows' `trainable` marking **fails the campaign** |

    The in-CI half still decides everything it decides from the decoded row with no ambient workflow
    state; that claim is now made only about the half where it is true.
53. **SR-3/S-4** `TestExecutionProfileIsPlanTimeAvailableAndLabelBound`: constructs every key leaf
    **before matrix emission** from the `runner-class` and `runs-on-label` action inputs plus
    `runner.os`/`runner.arch` and the plan-job environment; reproduces the identical key inside a
    bucket job; proves the key is **byte-identical across three simulated runs whose
    `actual_runner_name` values all differ**, which is the stability the old `runner_name` leaf
    destroyed; proves changing the label, the class, or any other leaf resets history while a `K`
    change does not; and reports a plan/test `runs-on-label` mismatch as a misconfiguration (QC12)
    rather than tolerating it.
54. **SR-4 / R11-D6** `TestWallRankToleranceBoundary` — **literal matrices, published here** so the
    boundary is checkable without running the planner. Reporter columns in billions:

    | Fixture | Rows | `rank` | `deficient_columns` | Admission |
    |---|---|---:|---|---|
    | **full rank** | `[1,10,1,0]` `[1,20,1,0]` `[1,10,0,1]` `[1,30,0,2]` | 4 | `[]` | **accepted** |
    | **column 4 dead** | `[1,10,1,0]` `[1,20,1,0]` `[1,30,1,0]` `[1,40,1,0]` | 2 | `[3, 4]` | rejected |
    | **column 3 dead** | `[1,10,0,1]` `[1,20,0,2]` `[1,30,0,3]` `[1,40,0,4]` | 2 | `[3, 4]` | rejected |
    | **hidden collinearity** | `[1,1,0,0]` `[1,2,0,1]` `[1,2,1,0]` `[1,4,1,2]` | 3 | `[4]` | rejected — column 2 = column 1 + column 3 + column 4, which no prose heuristic catches |

    The test also asserts the pivot immediately **below** `tol` is rejected and immediately **above**
    is accepted, that `sigma_max`, `tolerance` and `min_pivot` serialize as shortest round-tripping
    decimals, and that admission bytes are identical after row shuffling.
55. **SR-6** `TestWallObservationFieldRolesAreFrozen`: rejects any design matrix other than the four
    ordered columns, rejects any response other than `A`, and proves that mutating a DIAGNOSTIC
    field changes **no coefficient and no prediction**.
56. **SR-9** `TestActionIntervalUsesPersistedSameBootMonotonicEndpoints`: runs begin and end as
    **separate processes**, proves end uses the persisted start, accepts a matching boot identity,
    rejects a changed one, and rejects caller-supplied endpoints.
57. **SR-10/S-7** `TestMandelWarmupPlanProducesQualifyingFullRankCorpus`: exercises W-1…W-4 of
    §17.3b — a `--calibrate` **SUFFICIENT** proposal, then ≥24 accepted observations across ≥3
    distinct `(run_id, run_attempt)` pairs whose `comparability_key_digest` values are byte-identical
    (W-3), including ≥1 `i_any_whole_file = 0` row, achieving rank 4, with the campaign run set and
    window excluded — and only then confirms explicit wall planning is admitted. The proposal alone
    never satisfies the gate; the test asserts that a corpus of one calibration document and zero
    ring rows is `insufficient`.
58. **S-7 / D-8 / R9-D1 / R10-D1** `TestWallObservationRoundTripClosesRecordLoop` — **the
    23 → 24 → 25 boundary, against a published in-tree oracle generated by the normative planner.**
    Three earlier revisions published oracles that were wrong: the first compared against artifacts
    that do not exist at 23 rows; the second named a corpus whose exact solution moves
    `per_slice_overhead_ns` **down** with no partition change; the third computed its plans with
    **LPT** while §6.5 specifies **Karmarkar–Karp**, and so published the wrong seed and the wrong
    row-24 bucket vector. The oracle is now
    **`testdata/walltime/test58-feedback-oracle.json`**, every value produced by the **exact
    generalized KK of `internal/core/partition.go`** plus the single bounded Stage-2 schedule, and
    the test compares those bytes.

    | Step | Asserted against the fixture |
    |---|---|
    | seed, 23 rows | **rank 3** — every row satisfies `I + slice_count == 1`, so column 1 = column 3 + column 4. Both gates fail (rank < 4 **and** rows < `MIN_ROWS`); explicit wall basis is a hard error with no matrix; **no coefficients exist** |
    | **23 → 24** | appending `[1,30,1,2] → A = 49` (a **mixed** bucket, `I + slice_count == 3`) takes the design to **rank 4**; the ring gains exactly one row with plan-time regressors frozen; status becomes `ok`; the **first literal model** equals `fit_at_24` = exact `[5, 1, 8, 3]` — `fixed_ns = 5e9`, `scale = 1.0` dimensionless, `whole = 8e9`, `per_slice = 3e9`, all non-negative. A **cross-basis** transition, not an update |
    | **24 → 25** | appending `[1,10,1,1] → A = 42` refits to `fit_at_25` = exact `[5, 37/53, 680/53, 399/53]`; three of four coefficients change |
    | **the plans** | over `K = 3`, `w1 = w2 = 5e9` whole and `s1 = s2 = 5e9` slice: **two model-specific KK seeds**, because a slice unit's seed weight carries `per_slice_overhead_ns`. At row 24 the seed is `[[w1,w2],[s1],[s2]]` and refinement gives **`[[w2],[w1],[s1,s2]]`** with costs `[18e9, 18e9, 21e9]`. At row 25 the seed is `[[s1],[s2],[w1,w2]]` and refinement leaves it, with costs `[16_018_867_925, 16_018_867_925, 24_811_320_754]` |
    | empty store | `insufficient`, **never** a refit |
    | **residual aggregates (R12-D7)** | over the 25 rows with the deployed row-25 coefficients, the residual vector is `+1 811 320 754` ×8, `−1 207 547 170` ×8, `0` ×7, `+4 830 188 679`, `−9 660 377 359`; `Σ\|r\| = 38 641 509 430`; **`residual_mae_ns = 1 545 660 377`** (`round_half_up` of `1 545 660 377.2`) and **`residual_p90_ns = 1 811 320 754`** (nearest rank, index `ceil(0.9×25) = 23`) |

    The transition is a real composition change — the fixture's `differs_as_multiset` is true — and
    cross-evaluation shows the row-25 model strictly prefers its own plan (`24_811_320_754` ns) over
    the row-24 composition (`27_037_735_849` ns). Because the seed is model-specific, the fixture
    publishes **both** seeds and their weights; a single shared seed was the R10-D1 error.

    Files: `internal/walltime/record.go`, `.github/actions/record/action.yml`,
    `cmd/testbucket/main.go` (`runIngest`), `internal/walltime/palloc.go` (`FitModel`),
    `internal/core/plan.go` (`BuildPlan`), `internal/core/partition.go` (`karmarkarKarp`), and the
    oracle `testdata/walltime/test58-feedback-oracle.json`.

59. **S-7 / R8-D1** `TestCalibrationModeTerminatesSufficientOrNotFoundWithinBudget`: over the
    **published §17.3a universe** (`K = 4`; `w1..w4 = 40/30/20/10e9`; `s1..s3 = 5e9`), `--calibrate`
    emits `not_found_within_budget` with `layouts_tried == layout_budget` at **`N = 3`**, and a
    `testbucket.calibration-evidence/v1` document with `outcome: sufficient` and `rank: 4` at
    **`N = 4`**, whose `L(4)` bucket list and design rows equal the published table exactly. `L(5)`
    reports **exhaustion** under the residual-aware slot rule rather than being evaluated. Also:
    `structurally_infeasible` naming **column 4** for an all-whole universe and **column 3** for an
    all-slice one; a defined outcome at `N = 1` and at any `N` beyond the exhaustion point; **the
    one-slice `K = 3` case** — `|S| = 1`, `i = 4`, residual whole units — where the repaired slot
    formula reserves `min(2, |S|) = 1` slice slot, needs `1 + 1 + 1 = 3 ≤ K`, and therefore returns a
    **layout** rather than exhaustion (R11-D5); and never a fourth outcome or a silent partial
    success.

60. **S-1** `TestWallModelIsIntegerNanosecondsEndToEnd`: every model quantity is integer
    nanoseconds and `scale` is dimensionless. Fits a corpus, plans a K=2 fixture, and asserts the
    objective, the ring row, the serialized `a_eta_ns`, and the residuals agree in nanoseconds; that
    `est_seconds == round1(a_eta_ns/1e9)` for every bucket; and that the **superseded mixed-unit
    expression** — three coefficients divided by 1e9 with `scale × Σ(base seconds)` left unconverted
    — produces a value differing from the correct one by a factor of 1e9 on this fixture, so the
    test cannot pass under it. Also asserts no serialized `*_ns` field is a float and no predictor
    is in seconds.

61. **S-2 / R10-D3** `TestCampaignRowsNeverRefitTheModel` — drives the whole `campaign_id →
    trainable` state machine, which is the **sole in-CI mechanism**:

    | Case | Asserted |
    |---|---|
    | **matching** id under a supplied config | row **appended and retained** with `trainable: false` — not rejected |
    | **missing** id under a supplied config | row **rejected** (fail-closed) |
    | **mismatched** id under a supplied config | row **rejected** (fail-closed) |
    | **no config**, no id | row appended with `trainable: true` — the ordinary CI path |
    | **no config, `campaign_id` PRESENT (R23-F3)** | row **rejected before append**: not appended, not retained. Selected row identities, model bytes, `fitted_at`, and the **fitter-call count (zero)** are unchanged |
    | **no config, `profile.scored: true`** | same rejection, on the same reasoning — a scored run is required to carry a campaign identity, so its absence is a miswiring |
    | **pilot** rows | carry the config's id, so retained and `trainable: false`; §19.6's "cannot change anything" and "retained as a diagnostic" are both true |
    | **delayed ingest** after the window closes | still cannot train — `trainable` is a stored property, not a function of ambient `now` |
    | the fit | consumes **only** `trainable: true` rows; a `trainable: false` row reaching a fit is a failure |
    | **foreign workload** under a supplied config | `workload_commit != manifest.workload_commit` ⇒ appended with `trainable: false` — materialized at append, never a second fit-time predicate (R13-D1) |
    | **foreign orchestration** under a supplied config | `head_sha ∈ manifest.excluded_orchestration_commits` ⇒ appended with `trainable: false`, same reasoning |
    | **displacement, not just consumption (R21-F4)** | an exactly full ring of `W` `trainable: true` rows, whose **oldest** row is high-leverage for the fit, then one and then many `trainable: false` appends: the selected row identities and `model_parameters_digest` are **byte-identical** before and after every append, and no trainable row is evicted |
    | **the fitter is not invoked at all (R22-F4)** | the fit is driven with an **instrumented fitter whose call count is asserted zero** across one and then many matching campaign/pilot appends; the stored model record is compared **byte for byte**, `fitted_at` included, before and after each append |
    | **the diagnostic class is bounded too** | appending more than `W` `trainable: false` rows evicts only `trainable: false` rows, in `recency` order, and never a `trainable: true` row |
    | **the fitter's predicate count** | the fit is driven with an instrumented selector that **fails if it evaluates any predicate other than `trainable == true`** — the executable form of "sole in-CI mechanism" (R13-D1) |
    | **an unverified config never reaches append (R24-F3)** | a config whose bytes disagree with `campaign-config-digest-expected`, and a byte-correct config under a perturbed expected digest, are each refused at §19.9c-1's verify step — **before** `ingest` runs — so no row is appended, no selected identity or model byte changes, and the fitter-call count stays zero. The two perturbations are asserted **independently** |
    | **a valid scored config, end to end (R24-F3)** | the record job materializes the verified bytes job-locally, `ingest --campaign-config` reads that local path, the matching row is retained **diagnostic-only**, and the selected identities, model bytes and `fitted_at` are byte-identical with **zero** fitter calls |
    | **operator-side only** | `run_id` and authenticated-window predicates are exercised in the validator, and the test asserts they are **not** required in CI, and that an in-CI fit given them **fails** rather than silently using them |

    `model_parameters_digest` is byte-identical before the pilot, between pairs, and after the last
    scored run. Complements test 27, which governs thresholds.

62. **S-5 / R8-D4 / R10-D4** `TestScoredCacheModeEqualityOrPairInvalid` — **positive fixtures
    first**, because earlier blanket predicates rejected behaviour the rule explicitly permits:

    | Case | Expected |
    |---|---|
    | `exact-key` **hit** — matched == primary, non-empty | **accepted** |
    | `exact-key` **miss** — matched **empty**, primary non-empty, disposition `miss` | **accepted** — a legal tuple, not a violation |
    | `disabled` — both keys empty, disposition `disabled` | **accepted** |
    | a pair whose arms differ in `dependency_cache_hit`, `dependency_cache_matched_key` or `dependency_cache_disposition` at **any** index, in exact-key mode | **not scored** and **non-passing**; both rows retained in the attempted population; **not** a void and **not** rescheduled (§19.2b, §19.8). The superseded "still scored" case is a **must-fail** fixture (R15-F2) |
    | **prefix fallback** — matched **non-empty** and ≠ primary | rejected by QC14 |
    | `transform_cache_mode != "disabled"` | rejected |
    | missing `mongo_binary_sha256` | rejected |
    | arms differing index-wise on any of the **`bc_inv` cache leaves** | pair **unscored**, both rows retained (§17.19) |

    Any other `cache_state` tuple is rejected: the legal tuples of contract §10.5.1 are
    exhaustive, and that table is the only place they are written.

63. **S-3 / D-2 / R8-D5** `TestFieldRegistryCoversEverySerializedPath`: for the **six wholly-new
    documents** — §13 (observation), §13.0 (profile), §15.1a (ring row), §17.3a (calibration evidence),
    §19.9a (campaign **config**), §19.9b (campaign **attempts**) — the registry path set equals the
    set the owning section serializes **in both directions**, with the campaign family partitioned by
    each path's `owner` key. Also asserts `manifest.schema` takes one of the two §19.9 identifiers
    and never a third. For **plan and matrix** it asserts only the forward direction — every
    registered path is really serialized — because those artifacts also carry the frozen v0.2.2
    legacy field set that **PD-1's canonical legacy projection owns** and test 33 byte-compares; the
    earlier "both directions for every artifact" claim was false and is withdrawn. Also asserts:
    every entry carries `artifact`, `type`, and `cardinality`; every `memberships` member resolves to
    a `wire_paths` path or a `roles` field; and no `alias_of` key survives.

    **The projections, named (R24-F1).** An earlier revision required "the four generated lists" without
    naming them or giving a procedure that identifies them, so two test authors could pick different
    fours and each claim to have implemented this test. A count cannot choose an algorithm. The
    projections are exactly these, each by parsed registry key, owner section, direction, and
    comparison — and **no other projection is compared**:

    | # | Parsed registry key | Owner section | Direction | Comparison |
    |---:|---|---|---|---|
    | 1 | `roles` where `role: REGRESSOR`, ordered by `column` | §0.9's design-column table | both | **order-equal** |
    | 2 | `memberships.comparability_key` | §15.3's producer table | both | **order-equal** |
    | 3 | `memberships.bc_inv` | §19.2's reading table | both | **order-equal** |
    | 4 | `wire_paths` grouped by `artifact`, for the six wholly-new documents | §13, §13.0, §15.1a, §17.3, §19.9a, §19.9b | both | **set-equal** |
    | 5 | `wire_paths` where `artifact: store` | §15.1 ∪ §15.1c ∪ §15.2 | both | **set-equal** — the gate is `TestWallStoreSchemaStateMatrixAndMigration`, not this test |
    | 6 | `wire_paths` where `artifact` is `plan` or `matrix_entry` | §16, §16.3, §15.3a | **forward only** | every registered path is serialized. §16 and §16.3 own the plan and matrix surfaces; **§15.3a owns every `plan.runtime_profile_declared*` path** — the object, its digest, and each constituent of §15.3a's field table — which no other section serializes (R16-F1-1). PD-1's canonical legacy projection owns the frozen field set and test 33 byte-compares it |

    **The registry contract, executably (R13-D7).** The earlier prose asked this test to prove
    `role_exempt` "contains no scalar a role also claims" while the machine block on the same page
    required five such overlaps — an assertion no implementation could satisfy. It is replaced by
    five decidable clauses, each computed from the parsed block and compared against a *declared*
    set, never against a transcribed list or a literal count:

    | # | Clause | Decision procedure |
    |---:|---|---|
    | 1 | **one role, at most** | no canonical field appears in `roles` twice; no scalar is claimed by two `roles` entries |
    | 2 | **total on non-exempt artifacts** | every scalar whose `artifact` is **not** in `role_exempt.artifacts` is claimed by exactly one `roles` entry. Zero missing |
    | 3 | **permissive on exempt artifacts** | a scalar of an exempt artifact carries **either** one role **or** none. Both are legal, and the test asserts **exactly** that: it computes the set of exempt scalars that *are* roled and requires it to equal `role_exempt.roled_anyway` — set equality in **both** directions, so neither a new overlap nor a stale name can hide |
    | 4 | **containers are derived** | the container set is computed as every registered path with `type: object` or `array_of_object`; each carries **no** role unless it appears in `container_roles`; each `container_roles` entry names a real container and a `checked_by` QC. The test **fails if any document transcribes the container list or its size**, which is how the earlier nine-name prose came to omit `manifest.model_parameters` |
    | 5 | **no count anywhere** | every named projection above and every membership are compared as **sets and orders**, never against a written number — and the set of projections is chosen by the table above, not by a count (R24-F1); a literal cardinality for any registry projection, in any document of this package, fails the test |

    Clauses 3 and 4 are the two the earlier prose got wrong, and each is stated in exactly one
    place — **this table** (R21-F2: an earlier revision put them in the machine block's YAML
    comments, which are not parsed values and so could not be the executable statement of anything).
    The table names the procedure rather than
    restating the rule.

64. **S-8** `TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm`: feeds all 1,512 tracked paths
    from `testdata/consumers/mandel/tracked-test-paths.txt` through the production
    `assignFilterAtoms` implementation and asserts 42 selected↔selected collision pairs, 0
    selected→excluded-Case pairs, transitive atom closure over the collision relation, that no atom
    is split across an invocation or bucket boundary in a K=8 plan, and that every selected path
    renders as a `./x` path token.

65. **S-9** `TestPairsRunSequentiallyByAuthenticatedCompletion`: for every pair, authenticated
    `completed_at(first) ≤ started_at(second)` in the declared direction; a pair that satisfies only
    launch order while the two arms overlap is **rejected**; an empty or unparseable instant is a
    failure, not a skip; and no shipped string attributes drift control to launch order alone.

66. **ID-3** `TestArmsDifferOnlyByPlannerMode`: the `BC-INV` mutation matrix, generated by walking
    `memberships.bc_inv` and recursing into `memberships.tuple_leaves` — see `scope.md` §21.2 for the
    assertions. The test source contains **no literal field count**.

67. **ID-21 / R10-D8** `TestConsumerAdoptionIdentitiesAreSeparateAndRecorded`: the snapshots exist
    and every recorded SHA-256 matches; the constants this scope depends on are still present; the
    manifest schema requires `orchestration_repo`, `orchestration_commit`, `workload_repo`,
    `workload_commit`, and `candidate_sha` non-empty and equal across arms; and **no VCS commit,
    push, tag, or tracked-source mutation targets the workload repository** — checkout, install and
    test execution in that working tree are expressly permitted, which is the same boundary §20.6b
    draws. **This test does not assert that an external orchestrator exists or that a run
    completed**: no unit test can, and that evidence is gate **AG-2/AG-4** of §20.6b (R10-D8).

68. **R13-D5** `TestWallArithmeticIsCheckedAndFailsClosed` — the checked-integer domain of contract
    §1.1, which owns these rules:

    | Case | Expected |
    |---|---|
    | a ring of `n = 240` rows whose residual magnitudes each equal `2^63 − 1` | `Σ\|r_i\|` is computed exactly as `2 213 609 288 845 146 193 680` in the ≥128-bit domain and **never narrowed**; the exact rational mean is `2^63 − 1`, so `residual_mae_ns = 9223372036854775807` |
    | the same case under naive signed-`int64` accumulation | yields `-240`; the test asserts the conforming path does **not** produce it, so wrapping implementations fail |
    | a saturating accumulator on the same case | fails — saturation is non-conforming, exactly as wrapping is |
    | `reporter_sum_ns`, the four-term objective sum, and `r_i` each pushed past `int64` at their narrowing site | `E_NS_OVERFLOW`: non-zero exit, the site and operands named, and **no** plan, matrix, model, observation, or calibration-evidence document written |
    | a stored `scale` that parses to `NaN` or `±Inf`, and a `scale × reporter_sum_ns` product that is non-finite | `E_NON_FINITE`, same fail-closed outcome |
    | `round_half_up` of a non-finite argument, and of a finite value whose rounded result exceeds `int64` | `E_CONVERSION_RANGE`, same fail-closed outcome |
    | `round_half_up` at an exact tie, at both signs | ties resolve **away from zero**; the test pins both directions even though every reachable argument in this product is non-negative |
    | campaign `TA`, `mean(A)`, medians and `RA_i` | evaluated as exact integers/rationals with **cross-multiplied** comparisons; a float-division implementation that disagrees at the last nanosecond fails |
    | **the `DA` cross-product witness (R14-F3)** | two legal eight-bucket runs `P = [1,1,1,M−1,M,M,M,M]` and `Q = [1,1,1,M−2,M−1,M−1,M−1,M−1]` with `M = 2^63 − 1` give `DA(P) = 2M/(2M−1)` and `DA(Q) = 2(M−1)/(2M−3)`. The mandated cross-multiplication forms `340282366920938463334247398915801350154` and `340282366920938463334247398915801350156` — each **128 magnitude bits, both above the signed-int128 maximum `170141183460469231731687303715884105727`**, and differing by **2**, so no truncation can decide it. The test asserts `DA(P) < DA(Q)` and **fails any implementation that evaluates the comparison in a fixed-width type**, which is exactly what contract §1.2 requires and what the old "≥ 128 bits" permission allowed to be wrong |
    | the same witness under signed int128 | **fails** — the products overflow, and `E_NS_OVERFLOW` does not cover it because this is not an `int64` narrowing site |

69. **R13-D5** `TestSolverBudgetsHaveTerminalOutcomes` — the two capped solvers, which resolve
    **differently and deliberately** (contract §1.1):

    | Case | Expected |
    |---|---|
    | a bidiagonal fixture on which the Golub–Reinsch stopping test is not met within `75 × min(rows, 4)` iterations | `E_RANK_NON_CONVERGENT`: `sigma_max` **absent**, no tolerance, **no rank inferred**, `--calibrate` writes **no** evidence document, `--est-basis wall` emits **no** matrix, and a fit leaves the stored model untouched |
    | the same fixture, under an implementation that reports the non-convergent case as rank-deficient | **fails** — a rank that was not computed is never reported as deficient |
    | a design on which Lawson–Hanson reaches the `12`-outer-iteration cap before `max(w) ≤ tol_nnls` | status `insufficient`, subtype `nnls_budget_exhausted`, **no coefficients stored**, and §0.8 (c) makes an explicit wall plan a hard error |
    | the same design, under an implementation that deploys the partially converged vector | **fails** |
    | both caps, across two conforming implementations | identical outcomes and identical serialized bytes, which is the property the caps existed to protect and previously did not state |

73. **owner F1** `TestBucketRuntimeProfileMatchesPlan` — the executed runtime profile is bound to the
    planned one, against contract §15.3a:

    | Case | Asserted |
    |---|---|
    | the equal case | plan and bucket compute `runtime_profile_digest` over the **same seven fields in the declared order**, the digests are equal, QC17 passes, and the row is appended |
    | each of the seven constituents, mutated **in the bucket only** | one case per constituent — `node_version`, `pnpm_version`, `vitest_version`, `testbucket_sha256`, `facade_command`, `lock_sha256`, `dependency_cache_mode` — each **fails QC17**, keeps the row out of history, and leaves a scored pair **retained, unscored and non-passing** under §19.8's post-start rule, never voided and never rescheduled (R15-F3) |
    | the QC17 failure message | names the **first differing constituent** by field number, so a drift is diagnosable without re-deriving the digest |
    | a bucket that copies the plan's declared digest instead of computing its own | **fails** — the test drives the executed values, not a forwarded string, which is the whole point of the check |
    | field order | permuting the seven fields changes the digest, so the canonical order of §15.3a is asserted rather than assumed |
    | signing, attestation, or a runner roster | **not required and not present**; the test uses only file digests and version strings read on the executing runner (§3, §12) |

72. **owner F2** `TestScoredCacheIsSymmetric` — a scored pair is cache-symmetric or cache-disabled,
    against contract §19.2b:

    | Case | Asserted |
    |---|---|
    | mode **(a) disabled**, both arms | scores. Both mode leaves are `disabled`, `dependency_cache_producer` is `none` in both, the primary and matched keys are empty, and `dependency_cache_hit` is absent |
    | mode **(b) exact-key**, index-wise equal | scores. Same primary key, same producer, and for **every** declared cache index the two arms agree on `dependency_cache_hit`, `dependency_cache_matched_key` and `dependency_cache_disposition` |
    | mode **(b)** with **one** index differing in hit, matched key or disposition | the pair is **retained, unscored and non-passing** under §19.8's post-start rule; both rows stay in the **attempted** population, the pair is **never voided and never rescheduled**, and it is never averaged away (R15-F3) |
    | arms declaring different `dependency_cache_producer`, or different `dependency_cache_mode` | **fails §17.19** before this rule is reached, because both are `BC-INV` members (owner F2) |
    | a scored run with `transform_cache_mode` other than `disabled` | **fails** |
    | mode (a) and mode (b) alike | QC14a still binds `mongo_binary_sha256` to the binary the bucket executed, and installation stays outside `A` |
    | the withdrawn rule | an arrangement that records differing hit/matched-key/disposition across the arms and scores anyway **fails**; recording a difference is not equalizing it |

70. **R13-D2** `TestScoredCacheDeclarationTransportAndBinaryBinding` — the physical half of the
    cache-declaration transport, against contract §10.5:

    | Case | Expected |
    |---|---|
    | **hop 0, caller → plan (R23-F4)**: the caller supplies `cache-declaration-json` plus `cache-declaration-digest-expected`; the plan job materializes job-locally and verifies | accepted, and the file `plan` validates is byte-identical to the caller's content |
    | **hop 0, caller bytes perturbed by one byte**, expected digest unchanged | the plan job **fails before invoking `plan`**; no matrix is emitted |
    | **hop 0, caller expected digest perturbed**, bytes unchanged | the plan job **fails before invoking `plan`**; no matrix is emitted. The two perturbations are asserted **independently** |
    | a workflow that passes a **pathname** across the workflow-call boundary instead of content | **fails** — the interface test asserts the two caller inputs exist, because a caller path is not a shared filesystem |
    | the values matrix jobs consume | **the plan-job outputs only** — a matrix job wired to the caller inputs **fails** |
    | the plan job publishes `cache-declaration-json` / `cache-declaration-digest`; a matrix job materializes and `run-bucket` verifies | accepted, and the observation's declaration leaves are byte-identical to what `plan` validated |
    | a matrix job whose materialized bytes differ from the published digest by one byte | the job **fails before any bucket script starts**; the row never reaches QC14 |
    | a workflow that passes only a **pathname** between the plan job and a matrix job | **fails** — the interface test asserts the two workflow outputs exist, because a shared path is not a shared filesystem |
    | producer **A** — restore inside `run-bucket`, `dependency-cache-producer: action`, both companion inputs **absent** — and producer **B** — `caller`, both companion inputs **present** | both accepted; the disposition is derived by contract §10.5.3's table and by no other rule; `cache_state.dependency_cache_producer` records which |
    | `exact-key` declared and **neither** producer supplied a result | fails closed; the outcome leaves are never defaulted to `miss` and never omitted |
    | **caller-owned miss vs absent producer data (R14-F2)** | `matched-key: ""` **with** `hit: false` is accepted as a `miss`; `matched-key: ""` **with no `hit` input at all** is **rejected** as absent producer data. The two were indistinguishable before this input existed, which is the finding |
    | **both producers (R14-F2)** | `dependency-cache-producer: action` while either companion input is present **fails the job** — supplying both is rejected, never silently resolved in favour of one |
    | `dependency-cache-hit` empty, or any third value | **fails** — it is `true` or `false`, and empty is not `false` |
    | **incoherent producer result** | `hit: true` with an empty matched key, and `hit: false` with a non-empty one, both fail the row |
    | **decoy binary** — two distinct MongoDB binaries, `MONGOMS_SYSTEM_BINARY` naming the executed one, the decoy resident elsewhere | the check happens **in QC14a, on the bucket runner**: `mongo_binary_sha256` is the **executed** binary's digest, `mongo_binary_path` names it, and a run that hashed the decoy **fails the job before upload** — even when the decoy digest equals `expected_mongo_binary_sha256` |
    | `MONGOMS_SYSTEM_BINARY` unset, empty, or naming a missing file | **QC14a fails the job**; no scored observation is produced |
    | **the record job's half (R14-F2)** | ingest runs QC14b over the uploaded row only. The test asserts QC14b **accepts** a well-formed row whose `mongo_binary_path` does not exist on the record runner — because that path names a file on another runner — and **fails the implementation** if ingest stats, opens, or re-hashes it |
    | `mongo_binary_verified_on_runner` absent or `false` | QC14b rejects the row: a row that did not pass QC14a on its own runner is not ingestible |
    | the campaign config's `manifest.cache_state` | carries **exactly** the leaves contract §10.5.0 classes as `declaration`, computed from that table rather than from a literal list; a config carrying an outcome leaf is rejected (contract §10.5.4) |
    | **the block's shape is derived, not written (R15-F2)** | the test computes the `cache_state` leaf set and its declaration/outcome partition **from the field registry and contract §10.5.0**, asserts the two agree in both directions, and **fails on any document that writes a leaf count** |
    | **disabled mode (R15-F2)** | `dependency-cache-producer: none` with both companion inputs absent is **accepted**; `none` under `exact-key`, or `action`/`caller` under `disabled`, **fail the job** — every legal mode has exactly one legal producer state |
    | **the serialized hit witness (R15-F2)** | `cache_state.dependency_cache_hit` is present **iff** the producer is `caller`; QC14b re-derives the presence pattern from the row and **fails the implementation** if it is asked to attest to an input the row does not carry |
    | **ring rows (R15-F2)** | the ring row carries **no** `cache_state` path; a ring schema that adds one, or a document that says the ring carries cache outcomes, **fails** |

71b. **R23-F3 / R23-F6 / R24-F3 / R12-F3 / R13-F1** `TestRecordInputReachesIngestFlags` — the record
    hop, end to end. It first parses §21's four representations — `U`, `W`, `A` and `S` — and asserts
    **only the relations §21 defines**, each in the direction that has an order. A superseded revision
    of this test required the accounts to be equal in both directions, which they cannot be: they do
    not share a domain (R13-F1). No positional or numeric claim about which inputs reach which job may
    appear in any of them:

    | Case | Asserted |
    |---|---|
    | `U ↔ W`, in order | every `W[job]` is an **ordered subsequence** of `U`, and every member of `U` reaches **at least one** job. An **order-only permutation** of any `W[job]` **fails** — the comparison is subsequence, never set (R13-F1) |
    | `W` is a route, not a partition | `runs-on-label`, `candidate-sha` and `workload-commit` reaching more than one job is **legal**; a test that treats the `W[job]` sets as disjoint **fails** (R13-F1) |
    | a union input routed nowhere, or a routed name outside `U` | **fails**, in that direction (R13-F1) |
    | `T(W, plan outputs) ↔ A`, as **membership** | `plan` collapses `cache-declaration-json` and `cache-declaration-digest-expected` into `cache-declaration-file`; `run-bucket` gains `cache-declaration-file`, `cache-declaration-digest` and `shard-plan` **from the plan job's outputs**; `record` is identity. Both directions: an `A` member no route or plan output supplies **fails**, and a routed name absent from `A` **fails** (R13-F1) |
    | direct caller-to-bucket declaration wiring, or a missing plan-output injection | **fails** — a bucket action reading a caller pathname, or an `A[run-bucket]` without the plan-produced trio, breaks `T` (R13-F1) |
    | `A ↔ action_interfaces`, **in order** | `component-map.json`'s `action_interfaces` equals canonical `A` per action, in order, both directions. An **order-only permutation** — `campaign-id` after `shard-plan`, say — **fails**; so does losing `est-basis` from `A[plan]` (R13-F1) |
    | `S` against its **exclusion classes only** | `S[action]` is `A[action]` minus the defaulted `est-basis` and the producer-conditional `dependency-cache-matched-key`/`dependency-cache-hit`, in `A`'s order. `S` compared **as** `A` **fails**; so does an `S` carrying an excluded name, or a `dependency-cache-*` pair treated as unconditional or tied to the wrong producer (R13-F1) |
    | an input **split into two** while a positional count is left unchanged | **fails** — routing is by name, so the split changes `U`, `W` and `A` (R12-F3) |
    | `wall-observations-dir` | reaches `ingest --wall-observations` |
    | a scored or pilot run | the record job materializes `campaign-config-json` job-locally, verifies it against `campaign-config-digest-expected`, and passes **that local path** to `ingest --campaign-config` |
    | **config content perturbed by one byte**, expected digest unchanged | the record job **fails before invoking `ingest`** |
    | **expected digest perturbed**, content unchanged | the record job **fails before invoking `ingest`**. The two perturbations are asserted **independently** |
    | a caller **pathname** in place of content | **fails** — a record-runner path is not a shared filesystem (§19.9c-1) |
    | a row whose `campaign_id` does not match the **verified** config | **refused**, never silently trainable |
    | **ordinary unscored ingest**: no `campaign_id`, `profile.scored: false`, **no config supplied** | rows appended `trainable: true`; the config inputs are **not** required (R24-F3) |
    | a workflow that exposes `campaign-id` without the config inputs | **fails**, because §15.1b would then reject every scored row it ingests |

71a. **R23-F1 / R23-F2 / R24-F2** `TestWallStoreSchemaStateMatrixAndMigration` — the store projection
    gate. It compares the complete schema-2 surface (§15.1's inventory, §15.1c's presence matrix,
    §15.2's `migrated_from`) against the `artifact: store` rows of `scope.md` §2A in **both**
    directions, and deserializes and re-serializes a literal document for **every** state §15.1c
    indexes. The state set, the subtype vocabulary and the no-fit set are read from the
    **parsed** matrix rows and compared as **sets**; no case is selected by a phrase or by a
    count, and no count is asserted anywhere (R12-F1). The cases below are that enumeration:

    | Case | Asserted |
    |---|---|
    | **migrated** | `migrated_from: 1`, `status: insufficient`, `failure_subtype: migrated_no_history`, `observations` present and empty, and **no invented history** |
    | **below-`MIN_ROWS`** | `status: insufficient`, `failure_subtype: rows_below_minimum`, and **no coefficient, `fitted_at`, or fit statistic present** |
    | **below-`MIN_RUNS`** | `status: insufficient`, `failure_subtype: runs_below_minimum`, same absence assertions |
    | **below both minima** | `failure_subtype: rows_below_minimum` — §15.1c's precedence, asserted rather than left to the implementation |
    | **below a minimum and rank-deficient** | `failure_subtype: rows_below_minimum` or `runs_below_minimum` per that same order, never `rank_insufficient` |
    | **rank-insufficient** | `status: insufficient`, `failure_subtype: rank_insufficient`, and **no coefficient, `fitted_at`, or fit statistic present** |
    | **budget-exhausted** | `status: insufficient`, `failure_subtype: nnls_budget_exhausted`, same absence assertions |
    | **degraded** | `status: degraded`, `failure_subtype: mae_ceiling_exceeded`, all coefficients and fit statistics present |
    | **ok** | `status: ok`, `failure_subtype` **absent**, all coefficients and fit statistics present |
    | a coefficient, `fitted_at`, or fit statistic in **any** row whose fit group is `all absent` | **fails**, and the case is instantiated **once per such row** — the rows are read from the parsed matrix, so a row added to §15.1c adds a case here without an edit (R12-F1) |
    | a missing `failure_subtype` under any non-`ok` status, or a present one under `ok` | **fails** |
    | a `failure_subtype` value outside §15.1c's spelled vocabulary | **fails** |
    | a missing `model_version`, `comparability_key_digest` or `observations` in **any** state where `wall` is present | **fails** — those three are always present |
    | `model_version` in **every** literal state document, `migrated` included | equals `WALL_MODEL_VERSION` = **1** exactly (§15.1d); the value is asserted, not merely present (R12-F2) |
    | the `1 → 2` migration output | carries `model_version: 1` alongside `comparability_key_digest` and the empty `observations` (R12-F2) |
    | a document whose `model_version` is absent, or is a value the reader does not implement | **fails closed** per §7 — no default is substituted and no fit is attempted (R12-F2) |
    | a document carrying a **different** `model_version` **together with** a fit group | **fails** — §15.1d forbids reusing coefficients, `fitted_at`, residuals, `rows_used`, `runs_used` or `rank_support` across a bump (R12-F2) |
    | a store path in §15.1/§15.1c/§15.2 that the registry omits, **or** an `artifact: store` row none of them names | **fails**, in that direction |

71. **Withdrawn (owner F3).** `TestSpecificationAuthorityIsSingular` was a release gate over a
    multi-document authority grammar — a closed artifact inventory, a declared modal vocabulary, an
    owned-identifier index, a parsed companion schema, and duplication thresholds. §0.10 records why
    the owner withdrew it: it proved nothing about the interval, the model, the rendered estimate or
    the campaign comparison, and it was scope-about-scope. **No product rule was carried only by that
    test.** Each rule it referenced is stated in its own section and proved by a product test above:
    the store surface by test 71a, the record hop by test 71b, the registry projections by test 63,
    the `BC-INV` projection by `TestArmsDifferOnlyByPlannerMode`, the delta register by test 41. The
    number is retired rather than reused, so a reference to "test 71" in a superseded document
    resolves to this entry.

**Retained v0.2.2 regressions, referenced by name in §20 and not re-specified here:**
`TestMandelVitestContract`, `TestMatrixSemanticsAreUnchanged`,
`TestGoAdapterRendersTheBamlRestContract`, `TestGoEventsAndAuditAreUnchanged`, and
`exact_paths_test.go`. They are preservation gates, not acceptance tests for a delta, so they carry
no `scope.md` §21.0 registry row.

---

