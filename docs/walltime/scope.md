# Scope — testbucket practical wall-time balancing (PWT-13)

Recorded: 2026-09-07 UTC · Mode: read-only specification. The recording conditions, the threat
model, the artifact inventory, and the rule against a document quoting a line count or SHA-256 of
anything in this set are stated in **contract §0 and contract §0.10** and are not restated
here. `TestScopeArtifactConsistency` (test 41) is the test that enforces them over these files.

**Revision 13 — specification tail repair S-1…S-10 (historical record).** Ten defects from the
then-current `scope-adversary.md` were closed as one batch; the rules they produced live in the
contract, and this entry records only which batch closed what. One integer-nanosecond unit system
with a single display-boundary division (now contract §0.9); pre-campaign-only fitting with C rows
used for validation metrics only (now contract §6.4b); §2A rebuilt as a wire-path / role /
membership registry with the schema-generation claim withdrawn in favour of contract §22 test 63's
named projections;
`runner_class` (now contract §15.3) and an explicit
`runs-on-label` producer replacing the plan-job runner instance name (§8.3); a declared, recorded,
per-pair-compared `cache_state` (§8.4); three separated provenance identities with a defined ring
recency and eviction rule (§8.1a, §8.1b); a truthful bounded calibration outcome over a **partial** layout function
under a **total proposer**, separated from the W-1…W-4 execution protocol (§10.3); a production-algorithm suffix-atom
regression over the full pinned 1,512-path universe (§14.6a); corrected pilot arithmetic and
authenticated **sequential** within-pair order (§13.5, §13.6); and one ordered machine-readable
delta registry from which §21's **counts, test symbols, and every cell of its register table** are
derived — the table is a byte-exact four-column projection test 41 compares (R14-F1), while the gap
bodies remain hand-authored and no renderer exists (§21.0, R10-D8, R11-D10). §20 maps each.

**Revision 7 — owner resolution.** Five owner determinations from `recovery_gate` were recorded in
**`acceptance-contract.md` §0**; what they are, and their standing relative to a reviewer, is that
section's to state. This revision applied them at §13.5 (counterbalanced order, randomization
removed), §13.6–§13.7 (engineering release gate, inferential language removed, absolute **and**
relative calibration), and the new §21 (labelled R54 gaps with acceptance tests) and §22 (review
criterion).

**Revision 6.** The fifth adversarial review (`dc606179…`) carries the same 34 findings again and
sharpens one: baml-rest's isolation is *no ordinary source path* — a commit-pinned action source
plus a `v0.1.1` release-tag request — not an "immutable pin". §14.3, §1, and ledger A-06 are
corrected; nothing else in the finding set changed.

**Revision 5.** The fourth adversarial review carries the **same 34 findings and 9 closure items**
as the third, and adds three things: a new **Ledger sufficiency** analysis (§11.0); a sharpened
`N3`/`N1` requiring this scope to *affirmatively allow* pre-treatment reporter data rather than
claim an outcome-free allocation surface (§7.0); and a **withdrawal of its own test-execution
claim** — it now states that no execution-test result is claimed, which moves the burden of running
the suite onto implementation (ledger A-04). §20 maps all 34 findings and all 9 closure items.
**Terminal verdict: `SCOPE_READY`** — see §19.

---

## 1. Identity

| | |
|---|---|
| Repository | `invakid404/testbucket`, workspace `/Users/inva/Coding/testbucket-equal-wall-time` |
| Baseline | `v0.2.2` = `v0` = `master@origin` = `693a19981fb6e0061d3fab62e59d75dc1c01ff3f` |
| Salvage source | R54 `8d27e9bccc537032615578748c80bed9300ec7bd` (PR #34, open, draft, unmerged) |
| R54 delta from baseline | 184 files (162 added, 22 modified), +53,295 / −972 |
| R54 delta from parent `9537850eed19` | 3 files, +328/−5 — archive-install hardening only (§12.6) |
| Working copy | parent exactly R54, no commit. Added path groups: `docs/walltime/` (the governing artifacts), `testdata/consumers/` (the pinned consumer fixture), **`testdata/walltime/`** (the feedback oracle `test58-feedback-oracle.json`), and the reviewer-authored inputs `scope-adversary.md` and `scope-adjudication.md`, which this round did not create, edit, or delete. The inventory is enumerated from `jj status` / `jj file list -r @` by test 41, never from a number written here (S-10, R11-D11) |
| Vitest workload | Mandel `d9ae1d433bb45012c04d567879b66fc4bf6112c6` — **pins v0.2.2 (`693a1998`)** |
| Go consumer | `invakid404/baml-rest` `ff3012b160aca29a920516ab7e03c772e948648a` — action source commit-pinned at `551d49ce`; installer requests release tag `v0.1.1` |

Neither exact consumer uses the wall-time line. That is simultaneously the strongest neutrality
guarantee available (§14.3) and the reason the campaign needs an external orchestrator that leaves
the workload identity untouched (§13.3).

## 2. Mandate and discipline

- **Narrow.** The product is bucket balancing by the instrumented run-bucket interval, through the
  partition weight only (§7.0).
- **Salvage.** The implementation order is contract §0.5a's.
- **Closed by default.** What a consumer that does not opt in receives is contract §4's.
- **Fail-closed.** Every absence is a refusal — contract §7 states the rule and contract §17 the matrix. A missing campaign field fails the
  campaign; it is never skipped.
- **Named honestly.** The four quantities and two estimate names of contract §1 are used
  consistently. §9.4 lists every current violation with its site.
- **No proof-system creep.** Nothing removed in §12.3 returns except on the measurement evidence
  of contract §12.

---

## 2A. Field registry — wire paths, roles, and memberships

**What this registry is, exactly (S-3).** The earlier revision claimed one 85-row name table
governed field names, each field's single role, the artifact types fields appear in, and that
several schemas were *generated* from it. It did none of those things: the observation example
carried thirteen serialized paths with no row, `profile.store_sha256` and `store_sha256` were one
row, `bc_*` aliases named nothing resolvable, and the "generated" ring row and plan observation
selected role sets far larger than the actual §8.1a and §5 documents. The schema-generation claim
is **withdrawn and replaced** by three separate, individually checkable dimensions:

| Dimension | Block | What it fixes | What it does **not** claim |
|---|---|---|---|
| **A. Wire paths** | `wire_paths` | every path this product **adds or makes model-relevant**, its artifact, its JSON type, its cardinality | byte layout, key order, file names, and the frozen v0.2.2 legacy plan/matrix fields PD-1 owns |
| **B. Roles** | `roles` | each canonical field's **one primary semantic role** | that a role implies an artifact |
| **C. Memberships** | `memberships` | cross-cutting ordered sets — comparability key, `BC-INV`, `env_tuple` | that a member is not also something else |

**One field has one primary role, and any number of membership references.** A field is a member of
a cross-cutting set by *referencing its wire path*, never by acquiring a second role. That is how
`node_version`, `pnpm_version`, and `setup_command` are simultaneously comparability-key leaves and
`env_tuple` leaves without violating "one field, one role": their role is `COMPARABILITY_KEY`, and
`env_tuple` **references** them. Alias entries are removed entirely; `BC-INV` names wire paths.

**What this registry covers, and what it does not (D-2).** The registry is an **additive subset**,
not a total serialization schema. It registers every path this product **adds or makes
model-relevant**; it deliberately does **not** register the v0.2.2 legacy serialization that PD-1
freezes — `PlanUnit`/`PlanBucket`/`PlanSummary` members, `name`, `script`, `units`, `invocations`,
`needs_node` and the rest of the legacy plan and matrix field sets. Those are governed by PD-1's
**canonical legacy projection**, which is byte-compared by
`TestReporterBasisPreservesLegacyProjectionAndDeclaresBasis` (test 33), and registering them here
would duplicate that authority in a second place — the exact drift this registry exists to stop.

So the earlier "matches each owning serialized artifact in both directions" claim is **withdrawn and
replaced** by a precise one:

| Claim | Status |
|---|---|
| every registered path is serialized by the section named in its `artifact` | **held**, checked by test 63 |
| every path **added by this product** to observation, profile, ring row, calibration evidence, campaign **config**, and campaign **attempts** is registered | **held** — those six documents are wholly new, so for them the correspondence *is* bidirectional. The `manifest` artifact tag covers the two campaign documents; each path's `owner` key says which one carries it (R8-D5) |
| every path serialized on plan and matrix entries is registered | **withdrawn** — plan and matrix carry the frozen v0.2.2 legacy field set, which PD-1 owns and test 33 compares |
| the registry generates the artifact schemas | **withdrawn** — which projections are generated, and how each is compared, is contract §22 test 63's named table (R24-F1) |

**Container and leaf convention.** A path whose `type` is `object` or `array_of_object` is a
**container**: it is registered so the artifact's shape is complete, and it normally carries **no**
primary role, because roles attach to the scalars a container holds. **Two containers are the
exception, and deliberately so:** `invocations` carries `invocation_membership` and `cache_state`
carries `cache_state`, because in both cases the admission-checked property *is the container's own
membership and shape* — QC6 compares the invocation list against the plan's rendered invocations,
and QC14 validates the cache block against the legal tuples of contract §10.5.1. A role on those
two is a statement
about the container itself, not a misplaced scalar role. The registry declares them in
`container_roles` so the exception is enumerated rather than inferred.

**The container set is derived, never transcribed (R13-D7).** A container is any registered path
whose `type` is `object` or `array_of_object`; the set is exactly what the machine block below
contains, and this prose quotes neither its members nor its size. An earlier revision transcribed
nine names by hand and omitted the registered `manifest.model_parameters` container — the drift a
transcription always eventually produces. Test 63 computes the set from `wire_paths` and compares it
against `container_roles`; **no document may restate it.**

**Roles and exemption — why contract §22 test 63 had to be permissive (R13-D7, restated for R21-F2).** An
earlier revision said test 63 proves `role_exempt` "contains no scalar a role also claims", while the
machine block on the same page required overlaps. No implementation could satisfy both, so that
sentence is **withdrawn**. What test 63 asserts — the coverage rule, the exempt-artifact rule, the
two-role prohibition, and the container rule — is stated in **contract §22**, and the artifacts and
the roled-anyway set are the block's own `role_exempt` keys below. This section states neither, and
no YAML comment in the block is the only statement of any of them.

**Which lists are generated from this registry, which sections they are compared against, and which
comparisons are bidirectional are contract §22's** — test 63 for the six wholly-new documents,
`TestWallStoreSchemaStateMatrixAndMigration` for the schema-2 store surface, and test 33 for the
legacy field set. An earlier revision stated a second, worded account here: it said only four lists
were generated and described the projection as covering six documents bidirectionally, which
disagreed with the `artifact: store` rows in the machine block immediately below (R23-F1). This
section states none of it.
**Why the unit question had to be settled once (S-1, corrected by D-1, restated for R19-F1).** An
earlier revision fitted a seconds predictor against a nanosecond response, so a literal
implementation was wrong by a factor of 1e9 and no document said which reading was correct. The
answer is **contract §0.9 and contract §1.1**: the unit of every registered quantity, the one
non-integer coefficient, the rounding step, and the single division at the display boundary are
theirs, and this section reproduces none of them. The registry below records each path's `unit` as
a field of the entry, which is registry structure rather than a restatement.

The one thing worth recording here is why the question recurred: the legacy reporter-basis surfaces
were v0.2.2 values frozen by PD-1 and were never part of the new unit system at all, so a sentence
written about "every display" was false the moment it was written. The inventory that resolves it is
**contract §5.1**, and §9.2a below explains where each of its rows comes from without restating it
(R12-D6, R14-F1).

```yaml
# field-registry v2 — machine-readable; TestFieldRegistryCoversEverySerializedPath parses this block.
# A. WIRE PATHS — domain: every path this product ADDS OR MAKES MODEL-RELEVANT; acceptance-contract.md 0.10 states it.
#    artifact ∈ {observation, profile, ring_row, plan, matrix_entry, manifest, calibration_evidence,
#                store}
#    cardinality ∈ {one, one_per_invocation, one_per_bucket, one_per_pair, one_per_arm,
#                    one_per_attempt, list, optional}
wire_paths:

  # ---- observation — acceptance-contract.md 13 ----
  - {path: "schema",                      artifact: observation, type: string,  cardinality: one}
  - {path: "comparability_key_digest",    artifact: observation, type: string,  cardinality: one}
  - {path: "repository",                  artifact: observation, type: string,  cardinality: one}
  - {path: "head_sha",                    artifact: observation, type: string,  cardinality: one}
  - {path: "candidate_sha",               artifact: observation, type: string,  cardinality: one}
  - {path: "workload_commit",             artifact: observation, type: string,  cardinality: one}
  - {path: "run_id",                      artifact: observation, type: string,  cardinality: one}
  - {path: "run_attempt",                 artifact: observation, type: string,  cardinality: one}
  - {path: "job_id",                      artifact: observation, type: string,  cardinality: one}
  - {path: "bucket_index",                artifact: observation, type: integer, cardinality: one}
  - {path: "bucket_name",                 artifact: observation, type: string,  cardinality: one}
  - {path: "plan_digest",                 artifact: observation, type: string,  cardinality: one}
  - {path: "profile",                     artifact: observation, type: object,  cardinality: one,
     provenance: {tags: [], see: ["acceptance-contract.md 13.0"]}}
  - {path: "est_seconds",                 artifact: observation, type: number,  cardinality: one,
     provenance: {tags: [], see: ["acceptance-contract.md 5.1"]}}
  - {path: "a_eta_ns",                    artifact: observation, type: string,  cardinality: optional,
     provenance: {tags: [R18-F3], see: ["acceptance-contract.md 13", "acceptance-contract.md 5.1"]}}
  - {path: "process_group_id",            artifact: observation, type: string,  cardinality: one}
  - {path: "actual_runner_name",          artifact: observation, type: string,  cardinality: one}
  - {path: "observed_runs_on_label",      artifact: observation, type: string,  cardinality: one}
  - {path: "unit_ids",                    artifact: observation, type: array_of_string, cardinality: one}
  - {path: "invocations",                 artifact: observation, type: array_of_object, cardinality: one}
  - {path: "invocations[].seq",           artifact: observation, type: integer, cardinality: one_per_invocation}
  - {path: "invocations[].units",         artifact: observation, type: array_of_string, cardinality: one_per_invocation}
  - {path: "invocations[].argv_digest",   artifact: observation, type: string,  cardinality: one_per_invocation}
  - {path: "invocations[].cwd_digest",    artifact: observation, type: string,  cardinality: one_per_invocation}
  - {path: "invocations[].selector",      artifact: observation, type: array_of_string, cardinality: one_per_invocation}
  - {path: "invocations[].atoms",         artifact: observation, type: array_of_string, cardinality: one_per_invocation}
  - {path: "invocations[].process_group_id", artifact: observation, type: string, cardinality: one_per_invocation}
  - {path: "invocations[].started_mono_ns",  artifact: observation, type: string, cardinality: one_per_invocation}
  - {path: "invocations[].ended_mono_ns",    artifact: observation, type: string, cardinality: one_per_invocation}
  - {path: "invocations[].elapsed_ns",       artifact: observation, type: string, cardinality: one_per_invocation}
  - {path: "invocations[].exit_code",        artifact: observation, type: integer, cardinality: one_per_invocation}
  - {path: "started_mono_ns",             artifact: observation, type: string,  cardinality: one}
  - {path: "ended_mono_ns",               artifact: observation, type: string,  cardinality: one}
  - {path: "elapsed_ns",                  artifact: observation, type: string,  cardinality: one}
  - {path: "setup_ns",                    artifact: observation, type: string,  cardinality: one}
  - {path: "script_ns",                   artifact: observation, type: string,  cardinality: one}
  - {path: "script_overhead_ns",          artifact: observation, type: string,  cardinality: one}
  - {path: "wrapper_ns",                  artifact: observation, type: string,  cardinality: one}
  - {path: "boot_id_start",               artifact: observation, type: string,  cardinality: one}
  - {path: "boot_id_end",                 artifact: observation, type: string,  cardinality: one}
  - {path: "realtime_start",              artifact: observation, type: string,  cardinality: one}
  - {path: "realtime_end",                artifact: observation, type: string,  cardinality: one}
  - {path: "terminal",                    artifact: observation, type: string,  cardinality: one}
  - {path: "exit_code",                   artifact: observation, type: integer, cardinality: one}
  - {path: "failure_reason",              artifact: observation, type: string,  cardinality: one}
  - {path: "limitations",                 artifact: observation, type: array_of_string, cardinality: one}
  - {path: "campaign_id",                 artifact: observation, type: string,  cardinality: optional,
     provenance: {tags: [R8-D2, R18-F3], see: ["acceptance-contract.md 19.9", "acceptance-contract.md 15.1b"]}}
  - {path: "cache_state",                 artifact: observation, type: object,  cardinality: one,
     provenance: {tags: [], see: ["acceptance-contract.md 10.5"]}}
  - {path: "cache_state.dependency_cache_mode",        artifact: observation, type: string, cardinality: one}
  - {path: "cache_state.dependency_cache_primary_key", artifact: observation, type: string, cardinality: one}
  - {path: "cache_state.dependency_cache_matched_key", artifact: observation, type: string, cardinality: one}
  - {path: "cache_state.dependency_cache_disposition", artifact: observation, type: string, cardinality: one}
  - {path: "cache_state.transform_cache_mode",         artifact: observation, type: string, cardinality: one}
  - {path: "cache_state.mongo_binary_sha256",          artifact: observation, type: string, cardinality: one,
     provenance: {tags: [R11-D3], see: ["acceptance-contract.md 10.5.0", "acceptance-contract.md 10.5.6"]}}
  - {path: "cache_state.dependency_cache_hit",         artifact: observation, type: boolean, cardinality: optional,
     provenance: {tags: [R15-F2, R18-F3], see: ["acceptance-contract.md 10.5.3"]}}
  - {path: "cache_state.dependency_cache_producer",    artifact: observation, type: string, cardinality: one,
     provenance: {tags: [R14-F2, R17-F2], see: ["acceptance-contract.md 10.5.0", "acceptance-contract.md 10.5.3"]}}
  - {path: "cache_state.mongo_binary_verified_on_runner", artifact: observation, type: boolean, cardinality: one,
     provenance: {tags: [R14-F2, R18-F3], see: ["acceptance-contract.md 10.5.6"]}}
  - {path: "cache_state.mongo_binary_path",            artifact: observation, type: string, cardinality: one,
     provenance: {tags: [R13-D2], see: ["acceptance-contract.md 10.5.5"]}}
  - {path: "cache_state.expected_mongo_binary_sha256", artifact: observation, type: string, cardinality: one,
     provenance: {tags: [R11-D3], see: ["acceptance-contract.md 10.5.0"]}}
  - {path: "runtime_profile_digest",       artifact: observation, type: string,  cardinality: one,
     provenance: {tags: [R14-A1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "runtime_profile.node_version",     artifact: observation, type: string, cardinality: one,
     provenance: {tags: [R14-A1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "runtime_profile.pnpm_version",     artifact: observation, type: string, cardinality: one,
     provenance: {tags: [R14-A1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "runtime_profile.vitest_version",   artifact: observation, type: string, cardinality: one,
     provenance: {tags: [R14-A1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "runtime_profile.testbucket_sha256", artifact: observation, type: string, cardinality: one,
     provenance: {tags: [R14-A1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "runtime_profile.facade_command",   artifact: observation, type: string, cardinality: one,
     provenance: {tags: [R14-A1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "runtime_profile.lock_sha256",      artifact: observation, type: string, cardinality: one,
     provenance: {tags: [R14-A1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "runtime_profile.dependency_cache_mode", artifact: observation, type: string, cardinality: one,
     provenance: {tags: [R14-A1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "runtime_profile",              artifact: observation, type: object,  cardinality: one,
     provenance: {tags: [R14-A1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "cache_declaration_digest",     artifact: observation, type: string,  cardinality: one,
     provenance: {tags: [R11-D4], see: ["acceptance-contract.md 10.5.0", "acceptance-contract.md 15.3"]}}

  # ---- profile — acceptance-contract.md 13.0 ----
  - {path: "profile.scored",              artifact: profile, type: boolean, cardinality: one}
  - {path: "profile.runner_token",        artifact: profile, type: string,  cardinality: one}
  - {path: "profile.k",                   artifact: profile, type: integer, cardinality: one}
  - {path: "profile.count",               artifact: profile, type: integer, cardinality: one}
  - {path: "profile.file_parallelism",    artifact: profile, type: integer, cardinality: one}
  - {path: "profile.bucket_indices",      artifact: profile, type: array_of_integer, cardinality: one}
  - {path: "profile.est_basis",           artifact: profile, type: string,  cardinality: one}
  - {path: "profile.store_sha256",        artifact: profile, type: string,  cardinality: one}
  - {path: "profile.expanded_unit_set_digest", artifact: profile, type: string, cardinality: one}

  # ---- ring_row — acceptance-contract.md 15.1a ----
  - {path: "ring.repository",             artifact: ring_row, type: string,  cardinality: one,
     provenance: {tags: [R8-D3], see: ["acceptance-contract.md 15.1b"]}}
  - {path: "ring.job_id",                 artifact: ring_row, type: string,  cardinality: one,
     provenance: {tags: [R8-D3], see: ["acceptance-contract.md 15.1b"]}}
  - {path: "ring.head_sha",               artifact: ring_row, type: string,  cardinality: one}
  - {path: "ring.candidate_sha",          artifact: ring_row, type: string,  cardinality: one}
  - {path: "ring.workload_commit",        artifact: ring_row, type: string,  cardinality: one}
  - {path: "ring.run_id",                 artifact: ring_row, type: string,  cardinality: one}
  - {path: "ring.run_attempt",            artifact: ring_row, type: string,  cardinality: one}
  - {path: "ring.observed_start_realtime", artifact: ring_row, type: string, cardinality: one,
     provenance: {tags: [], see: ["acceptance-contract.md 15.1b"]}}
  - {path: "ring.run_started_at",         artifact: ring_row, type: string,  cardinality: optional,
     provenance: {tags: [], see: ["acceptance-contract.md 15.1a", "acceptance-contract.md 18.2"]}}
  - {path: "ring.trainable",              artifact: ring_row, type: boolean, cardinality: one,
     provenance: {tags: [R8-D2], see: ["acceptance-contract.md 15.1b", "acceptance-contract.md 19.9c"]}}
  - {path: "ring.ingest_seq",             artifact: ring_row, type: integer, cardinality: one}
  - {path: "ring.bucket_index",           artifact: ring_row, type: integer, cardinality: one}
  - {path: "ring.plan_digest",            artifact: ring_row, type: string,  cardinality: one}
  - {path: "ring.store_sha256",           artifact: ring_row, type: string,  cardinality: one}
  - {path: "ring.comparability_key_digest", artifact: ring_row, type: string, cardinality: one}
  - {path: "ring.reporter_sum_ns",        artifact: ring_row, type: string,  cardinality: one}
  - {path: "ring.i_any_whole_file",       artifact: ring_row, type: integer, cardinality: one}
  - {path: "ring.slice_count",            artifact: ring_row, type: integer, cardinality: one}
  - {path: "ring.elapsed_ns",             artifact: ring_row, type: string,  cardinality: one}
  - {path: "ring.whole_file_count",       artifact: ring_row, type: integer, cardinality: one}
  - {path: "ring.invocation_count",       artifact: ring_row, type: integer, cardinality: one}
  - {path: "ring.terminal",               artifact: ring_row, type: string,  cardinality: one}

  # ---- plan — acceptance-contract.md 16 ----
  - {path: "plan.est_basis",              artifact: plan, type: string, cardinality: one}
  - {path: "plan.expanded_unit_set_digest", artifact: plan, type: string, cardinality: one}
  - {path: "plan.profile",                artifact: plan, type: object, cardinality: one, opaque: true,
     provenance: {tags: [R9-D3], see: ["acceptance-contract.md 13.0"]}}
  - {path: "plan.comparability_key_digest", artifact: plan, type: string, cardinality: one}
  - {path: "plan.runtime_profile_declared_digest", artifact: plan, type: string, cardinality: one,
     provenance: {tags: [R14-A1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "plan.runtime_profile_declared",  artifact: plan, type: object, cardinality: one,
     provenance: {tags: [R15-F1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "plan.runtime_profile_declared.node_version",     artifact: plan, type: string, cardinality: one,
     provenance: {tags: [R15-F1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "plan.runtime_profile_declared.pnpm_version",     artifact: plan, type: string, cardinality: one,
     provenance: {tags: [R15-F1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "plan.runtime_profile_declared.vitest_version",   artifact: plan, type: string, cardinality: one,
     provenance: {tags: [R15-F1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "plan.runtime_profile_declared.testbucket_sha256", artifact: plan, type: string, cardinality: one,
     provenance: {tags: [R15-F1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "plan.runtime_profile_declared.facade_command",   artifact: plan, type: string, cardinality: one,
     provenance: {tags: [R15-F1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "plan.runtime_profile_declared.lock_sha256",      artifact: plan, type: string, cardinality: one,
     provenance: {tags: [R15-F1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "plan.runtime_profile_declared.dependency_cache_mode", artifact: plan, type: string, cardinality: one,
     provenance: {tags: [R15-F1], see: ["acceptance-contract.md 15.3a"]}}
  - {path: "plan.buckets[].est_seconds",  artifact: plan, type: number, cardinality: one_per_bucket}
  - {path: "plan.buckets[].a_eta_ns",     artifact: plan, type: string, cardinality: optional,
     provenance: {tags: [R18-F3, D-1], see: ["acceptance-contract.md 5.1"]}}
  - {path: "plan.buckets[].cwd_digest",   artifact: plan, type: string, cardinality: one_per_bucket}

  # ---- matrix_entry — acceptance-contract.md 16.3 ----
  - {path: "matrix.est_seconds",          artifact: matrix_entry, type: number, cardinality: one_per_bucket}
  - {path: "matrix.est_basis",            artifact: matrix_entry, type: string, cardinality: one_per_bucket}
  - {path: "matrix.wall_est_seconds",     artifact: matrix_entry, type: number, cardinality: optional}
  - {path: "matrix.expanded_unit_set_digest", artifact: matrix_entry, type: string, cardinality: one_per_bucket}
  - {path: "matrix.needs_node",           artifact: matrix_entry, type: boolean, cardinality: one_per_bucket}

  # ---- manifest — acceptance-contract.md 19.9 ----
  - {path: "manifest.orchestration_repo",   artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.orchestration_commit", artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.workload_repo",        artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.workload_commit",      artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.candidate_sha",        artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.testbucket_binary_sha256", artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.instrumentation_binary_sha256", artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.action_tree_digest",   artifact: manifest, type: object, cardinality: one, owner: config}
  - {path: "manifest.action_tree_digest.plan",       artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.action_tree_digest.run-bucket", artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.action_tree_digest.record",     artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.facade_sha256",        artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.facade_argv",          artifact: manifest, type: array_of_string, cardinality: one, owner: config}
  - {path: "manifest.vitest_config_sha256", artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.lockfile_sha256",      artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.package_json_sha256",  artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.workload_tree_digest", artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.discovery_exclude_prefixes", artifact: manifest, type: array_of_string, cardinality: one, owner: config}
  - {path: "manifest.discovered_unit_set_digest", artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.model_parameters_digest",    artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.runs_on_label",        artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.runner_class",         artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.runner_image_label",   artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.cache_state",          artifact: manifest, type: object, cardinality: one, owner: config, opaque: true,
     provenance: {tags: [R13-D2, R16-F2, R18-F3], see: ["acceptance-contract.md 10.5"]}}
  - {path: "manifest.env_tuple",            artifact: manifest, type: object, cardinality: one, owner: config}
  - {path: "manifest.env_tuple.node_version",       artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.env_tuple.pnpm_version",       artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.env_tuple.working_directory",  artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.env_tuple.setup_command",      artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.env_tuple.events_dir",         artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.env_tuple.timeout_minutes",    artifact: manifest, type: integer, cardinality: one, owner: config}
  - {path: "manifest.campaign_id",          artifact: manifest, type: string, cardinality: one, owner: config,
     provenance: {tags: [R8-D2], see: ["acceptance-contract.md 19.9"]}}
  - {path: "manifest.schema",               artifact: manifest, type: string, cardinality: one,
     provenance: {tags: [R8-D5, R18-F3], see: ["acceptance-contract.md 19.9"]},
            owner: both}
  - {path: "manifest.frozen_at",            artifact: manifest, type: string, cardinality: one,
     provenance: {tags: [D-3], see: ["acceptance-contract.md 19.1"]}, owner: config}
  - {path: "manifest.store_artifact_id",    artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.store_sha256",         artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.model_parameters",     artifact: manifest, type: object, cardinality: one, owner: config}
  - {path: "manifest.model_parameters.fixed_ns",                     artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.model_parameters.scale",                        artifact: manifest, type: number, cardinality: one, owner: config}
  - {path: "manifest.model_parameters.whole_invocation_overhead_ns", artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.model_parameters.per_slice_overhead_ns",        artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.scheduled_dates",      artifact: manifest, type: array_of_string, cardinality: one, owner: config}
  - {path: "manifest.declared_order",       artifact: manifest, type: array_of_string, cardinality: one, owner: config}
  - {path: "manifest.excluded_run_ids",     artifact: manifest, type: array_of_string, cardinality: one,
     provenance: {tags: [D-3], see: ["acceptance-contract.md 19.9b"]}, owner: attempts}
  - {path: "manifest.excluded_orchestration_commits", artifact: manifest, type: array_of_string, cardinality: one, owner: config}
  - {path: "manifest.excluded_window",      artifact: manifest, type: object, cardinality: one, owner: config}
  - {path: "manifest.excluded_window.start", artifact: manifest, type: string, cardinality: one, owner: config}
  - {path: "manifest.excluded_window.end",   artifact: manifest, type: string, cardinality: one, owner: config}

  # ---- manifest pair/arm tree — acceptance-contract.md 19.9a ----
  # owner — acceptance-contract.md 19.9b-1 states the two-document ownership rule.
  - {path: "manifest.pairs",                  artifact: manifest, type: array_of_object, cardinality: one, owner: both}
  - {path: "manifest.pairs[].pair_index",     artifact: manifest, type: integer, cardinality: one_per_pair, owner: both}
  - {path: "manifest.pairs[].declared_order", artifact: manifest, type: string, cardinality: one_per_pair,
     provenance: {tags: [], see: ["acceptance-contract.md 0.2"]}, owner: config}
  - {path: "manifest.pairs[].arms",           artifact: manifest, type: array_of_object, cardinality: one_per_pair, owner: both}
  - {path: "manifest.pairs[].arms[].arm",          artifact: manifest, type: string,  cardinality: one_per_arm, owner: both}
  - {path: "manifest.pairs[].arms[].est_basis",    artifact: manifest, type: string,  cardinality: one_per_arm,
     provenance: {tags: [D-2], see: ["acceptance-contract.md 19.2"]}, owner: config}
  - {path: "manifest.pairs[].arms[].run_id",       artifact: manifest, type: string,  cardinality: one_per_arm, owner: attempts}
  - {path: "manifest.pairs[].arms[].run_attempt",  artifact: manifest, type: string,  cardinality: one_per_arm, owner: attempts}
  - {path: "manifest.pairs[].arms[].started_at",   artifact: manifest, type: string,  cardinality: one_per_arm,
     provenance: {tags: [], see: ["acceptance-contract.md 18.2"]}, owner: attempts}
  - {path: "manifest.pairs[].arms[].completed_at", artifact: manifest, type: string,  cardinality: one_per_arm,
     provenance: {tags: [S-9], see: ["acceptance-contract.md 19.5"]}, owner: attempts}
  - {path: "manifest.pairs[].arms[].bucket_count", artifact: manifest, type: integer, cardinality: one_per_arm, owner: attempts}
  - {path: "manifest.pairs[].arms[].scored",       artifact: manifest, type: boolean, cardinality: one_per_arm, owner: attempts}

  # ---- manifest attempts — acceptance-contract.md 19.9b ----
  - {path: "manifest.attempts",                 artifact: manifest, type: array_of_object, cardinality: one, owner: attempts}
  - {path: "manifest.attempts[].run_id",        artifact: manifest, type: string,  cardinality: one_per_attempt, owner: attempts}
  - {path: "manifest.attempts[].run_attempt",   artifact: manifest, type: string,  cardinality: one_per_attempt, owner: attempts}
  - {path: "manifest.attempts[].job_ids",       artifact: manifest, type: array_of_string, cardinality: one_per_attempt, owner: attempts}
  - {path: "manifest.attempts[].pair_index",    artifact: manifest, type: integer, cardinality: one_per_attempt, owner: attempts}
  - {path: "manifest.attempts[].arm",           artifact: manifest, type: string,  cardinality: one_per_attempt, owner: attempts}
  - {path: "manifest.attempts[].started_at",    artifact: manifest, type: string,  cardinality: one_per_attempt, owner: attempts}
  - {path: "manifest.attempts[].disposition",   artifact: manifest, type: string,  cardinality: one_per_attempt,
     provenance: {tags: [R17-F2, R18-F3], see: ["acceptance-contract.md 19.9b"]}, owner: attempts}

  # ---- calibration_evidence — acceptance-contract.md 17.3 ----
  - {path: "calib.schema",                  artifact: calibration_evidence, type: string, cardinality: one}
  - {path: "calib.outcome",                 artifact: calibration_evidence, type: string, cardinality: one}
  - {path: "calib.comparability_key_digest", artifact: calibration_evidence, type: string, cardinality: one}
  - {path: "calib.proposed_plan_digests",   artifact: calibration_evidence, type: array_of_string, cardinality: one}
  - {path: "calib.layouts_tried",           artifact: calibration_evidence, type: integer, cardinality: one}
  - {path: "calib.layout_budget",           artifact: calibration_evidence, type: integer, cardinality: one}
  - {path: "calib.rank",                    artifact: calibration_evidence, type: integer, cardinality: one}
  - {path: "calib.sigma_max",               artifact: calibration_evidence, type: string, cardinality: one}

  # ---- store — acceptance-contract.md 15.1 ----
  # presence by status — acceptance-contract.md 15.1c.
  - {path: "storeSchema",                   artifact: store, type: integer, cardinality: one,
     provenance: {tags: [R22-F2], see: ["acceptance-contract.md 15.1"]}}
  - {path: "migrated_from",                 artifact: store, type: integer, cardinality: optional,
     provenance: {tags: [R23-F1], see: ["acceptance-contract.md 15.2"]}}
  - {path: "wall",                          artifact: store, type: object, cardinality: optional,
     provenance: {tags: [R22-F2], see: ["acceptance-contract.md 15.1"]}}
  - {path: "wall.model_version",            artifact: store, type: integer, cardinality: one}
  - {path: "wall.comparability_key_digest", artifact: store, type: string,  cardinality: one}
  - {path: "wall.status",                   artifact: store, type: string,  cardinality: one}
  - {path: "wall.failure_subtype",          artifact: store, type: string,  cardinality: optional,
     provenance: {tags: [R23-F2], see: ["acceptance-contract.md 15.1c"]}}
  - {path: "wall.fitted_at",                artifact: store, type: string,  cardinality: optional}
  - {path: "wall.rows_used",                artifact: store, type: integer, cardinality: optional}
  - {path: "wall.runs_used",                artifact: store, type: integer, cardinality: optional}
  - {path: "wall.fixed_ns",                 artifact: store, type: string,  cardinality: optional}
  - {path: "wall.scale",                    artifact: store, type: string,  cardinality: optional}
  - {path: "wall.whole_invocation_overhead_ns", artifact: store, type: string, cardinality: optional}
  - {path: "wall.per_slice_overhead_ns",    artifact: store, type: string,  cardinality: optional}
  - {path: "wall.residual_mae_ns",          artifact: store, type: string,  cardinality: optional}
  - {path: "wall.residual_p90_ns",          artifact: store, type: string,  cardinality: optional}
  - {path: "wall.rank_support",             artifact: store, type: array_of_string, cardinality: optional}
  - {path: "wall.observations",             artifact: store, type: array_of_object, cardinality: one, opaque: true,
     provenance: {tags: [R22-F2], see: ["acceptance-contract.md 15.1a"]}}
  - {path: "calib.tolerance",               artifact: calibration_evidence, type: string, cardinality: one}
  - {path: "calib.min_pivot",               artifact: calibration_evidence, type: string, cardinality: one}
  - {path: "calib.deficient_columns",       artifact: calibration_evidence, type: array_of_string, cardinality: one}
  - {path: "calib.indicator_values_present", artifact: calibration_evidence, type: array_of_integer, cardinality: one}
  - {path: "calib.distinct_slice_counts",   artifact: calibration_evidence, type: array_of_integer, cardinality: one}
  - {path: "calib.generated_at",            artifact: calibration_evidence, type: string, cardinality: one}

# B. ROLES — acceptance-contract.md 22 test 63.
#    role ∈ {REGRESSOR, RESPONSE, ADMISSION, DIAGNOSTIC, COMPARABILITY_KEY, OUTPUT_METADATA} · IDENTITY
roles:

  # ---- REGRESSOR — acceptance-contract.md 0.9.
  - {field: constant,           role: REGRESSOR, column: 1, value: 1, wire: []}
  - {field: reporter_sum_ns,    role: REGRESSOR, column: 2, frozen_at: plan, unit: nanoseconds,
     wire: [ring.reporter_sum_ns],
     derivation_see: "acceptance-contract.md 0.9"}
  - {field: i_any_whole_file,   role: REGRESSOR, column: 3, frozen_at: plan,
     wire: [ring.i_any_whole_file], derivation_see: "acceptance-contract.md 6.4"}
  - {field: slice_count,        role: REGRESSOR, column: 4, frozen_at: plan,
     wire: [ring.slice_count]}

  # ---- RESPONSE — acceptance-contract.md 0.9.
  - {field: elapsed_ns,         role: RESPONSE, unit: nanoseconds, equals: A,
     wire: [elapsed_ns, ring.elapsed_ns]}

  # ---- IDENTITY — acceptance-contract.md 15.1a.
  - {field: head_sha,           role: IDENTITY, means: orchestration_head_commit,
     wire: [head_sha, ring.head_sha, manifest.orchestration_commit]}
  - {field: candidate_sha,      role: IDENTITY, means: testbucket_commit_built_for_the_run,
     wire: [candidate_sha, ring.candidate_sha, manifest.candidate_sha]}
  - {field: workload_commit,    role: IDENTITY, means: consumer_checkout_commit,
     wire: [workload_commit, ring.workload_commit, manifest.workload_commit]}
  - {field: run_id,             role: IDENTITY, wire: [run_id, ring.run_id]}
  - {field: run_attempt,        role: IDENTITY, wire: [run_attempt, ring.run_attempt]}
  - {field: observed_start_realtime, role: IDENTITY, source: observation_realtime_start,
     unit: rfc3339_utc, authenticated: false,
     wire: ["ring.observed_start_realtime"],
     provenance: {tags: [S-6], see: ["acceptance-contract.md 15.1b"]}}
  - {field: run_started_at,     role: IDENTITY, source: actions_api, unit: rfc3339_utc,
     authenticated: true, cardinality: optional,
     wire: ["ring.run_started_at"],
     provenance: {tags: [], see: ["acceptance-contract.md 18.2"]}}
  - {field: ingest_seq,         role: DIAGNOSTIC, means: canonical_append_sequence_per_store,
     wire: ["ring.ingest_seq"],
     provenance: {tags: [R8-D3], see: ["acceptance-contract.md 15.1b"]}}
  - {field: ring_repository,    role: IDENTITY, wire: ["ring.repository"]}
  - {field: ring_job_id,        role: IDENTITY, wire: ["ring.job_id"]}
  - {field: repository,         role: IDENTITY, wire: [repository]}
  - {field: job_id,             role: IDENTITY, wire: [job_id]}

  # ---- ADMISSION — acceptance-contract.md 13.
  - {field: bucket_index,       role: ADMISSION, wire: [bucket_index, ring.bucket_index]}
  - {field: bucket_name,        role: ADMISSION, wire: [bucket_name]}
  - {field: plan_digest,        role: ADMISSION, canonical_plan_identity: true,
     wire: [plan_digest, ring.plan_digest]}
  - {field: store_sha256,       role: ADMISSION,
     wire: [profile.store_sha256, ring.store_sha256]}
  - {field: comparability_key_digest, role: ADMISSION, derived_from_membership: comparability_key,
     wire: [comparability_key_digest, ring.comparability_key_digest, plan.comparability_key_digest]}
  - {field: profile_est_basis,  role: ADMISSION, authoritative: true, wire: [profile.est_basis]}
  - {field: profile_scored,     role: ADMISSION, wire: [profile.scored]}
  - {field: profile_runner_token, role: ADMISSION, wire: [profile.runner_token]}
  - {field: profile_k,          role: ADMISSION, wire: [profile.k]}
  - {field: profile_count,      role: ADMISSION, wire: [profile.count]}
  - {field: profile_file_parallelism, role: ADMISSION, wire: [profile.file_parallelism]}
  - {field: profile_bucket_indices,   role: ADMISSION, wire: [profile.bucket_indices]}
  - {field: expanded_unit_set_digest, role: ADMISSION,
     wire: [profile.expanded_unit_set_digest, plan.expanded_unit_set_digest, matrix.expanded_unit_set_digest]}
  - {field: cwd_digest,         role: ADMISSION, wire: ["invocations[].cwd_digest", "plan.buckets[].cwd_digest"]}
  - {field: process_group_id,   role: ADMISSION, wire: [process_group_id, "invocations[].process_group_id"]}
  - {field: boot_id_start,      role: ADMISSION, wire: [boot_id_start]}
  - {field: boot_id_end,        role: ADMISSION, wire: [boot_id_end]}
  - {field: terminal,           role: ADMISSION, wire: [terminal, ring.terminal]}
  - {field: exit_code,          role: ADMISSION, wire: [exit_code, "invocations[].exit_code"]}
  - {field: schema,             role: ADMISSION, wire: [schema, calib.schema]}
  - {field: unit_ids,           role: ADMISSION, wire: [unit_ids]}
  - {field: invocation_membership, role: ADMISSION,
     wire: [invocations, "invocations[].seq", "invocations[].units", "invocations[].argv_digest",
            "invocations[].selector", "invocations[].atoms"]}
  - {field: campaign_id,        role: ADMISSION, wire: ["campaign_id", "manifest.campaign_id"],
     provenance: {tags: [R8-D2], see: ["acceptance-contract.md 15.1b"]}}
  - {field: trainable,          role: ADMISSION, wire: ["ring.trainable"]}
  - {field: cache_declaration_digest, role: COMPARABILITY_KEY, source: plan_action_input,
     wire: ["cache_declaration_digest"],
     provenance: {tags: [R11-D4], see: ["acceptance-contract.md 15.3"]}}
  - {field: cache_state,        role: ADMISSION, checked_by: QC14,
     wire: [cache_state, cache_state.dependency_cache_mode, cache_state.dependency_cache_primary_key,
            cache_state.dependency_cache_matched_key, cache_state.dependency_cache_disposition,
            cache_state.transform_cache_mode, cache_state.mongo_binary_sha256,
            cache_state.mongo_binary_path, cache_state.mongo_binary_verified_on_runner,
            cache_state.dependency_cache_producer, cache_state.dependency_cache_hit,
            cache_state.expected_mongo_binary_sha256]}

  # ---- DIAGNOSTIC — acceptance-contract.md 13.
  - {field: setup_ns,           role: DIAGNOSTIC, unit: nanoseconds, wire: [setup_ns]}
  - {field: script_ns,          role: DIAGNOSTIC, unit: nanoseconds, equals: VB, wire: [script_ns]}
  - {field: wrapper_ns,         role: DIAGNOSTIC, unit: nanoseconds, wire: [wrapper_ns]}
  - {field: script_overhead_ns, role: DIAGNOSTIC, unit: nanoseconds, wire: [script_overhead_ns]}
  - {field: invocation_elapsed_ns, role: DIAGNOSTIC, unit: nanoseconds, equals: "V[j]",
     wire: ["invocations[].elapsed_ns"]}
  - {field: monotonic_endpoints, role: DIAGNOSTIC, unit: nanoseconds,
     wire: [started_mono_ns, ended_mono_ns, "invocations[].started_mono_ns", "invocations[].ended_mono_ns"]}
  - {field: invocation_count,   role: DIAGNOSTIC, wire: [ring.invocation_count]}
  - {field: whole_file_count,   role: DIAGNOSTIC, wire: [ring.whole_file_count],
     provenance: {tags: [], see: ["acceptance-contract.md 0.9"]}}
  - {field: realtime_start,     role: DIAGNOSTIC, wire: [realtime_start]}
  - {field: realtime_end,       role: DIAGNOSTIC, wire: [realtime_end]}
  - {field: failure_reason,     role: DIAGNOSTIC, wire: [failure_reason]}
  - {field: limitations,        role: DIAGNOSTIC, wire: [limitations]}
  - {field: actual_runner_name, role: DIAGNOSTIC, wire: [actual_runner_name],
     provenance: {tags: [S-4], see: ["acceptance-contract.md 15.3"]}}
  - {field: observed_runs_on_label, role: DIAGNOSTIC, wire: [observed_runs_on_label],
     provenance: {tags: [], see: ["acceptance-contract.md 15.3"]}}
  - {field: residual_mae_ns,    role: DIAGNOSTIC, unit: nanoseconds, wire: []}
  - {field: residual_p90_ns,    role: DIAGNOSTIC, unit: nanoseconds, wire: []}

  # ---- ADMISSION — acceptance-contract.md 15.3a.
  - {field: runtime_profile_digest, role: ADMISSION,
     wire: [runtime_profile_digest, plan.runtime_profile_declared_digest],
     provenance: {tags: [R15-F1], see: ["acceptance-contract.md 15.3a"]}}
  - {field: runtime_profile_cache_mode, role: ADMISSION,
     wire: [runtime_profile.dependency_cache_mode,
            plan.runtime_profile_declared.dependency_cache_mode],
     provenance: {tags: [R15-F1], see: ["acceptance-contract.md 15.3a"]}}

  # ---- COMPARABILITY_KEY — acceptance-contract.md 15.3.
  - {field: runner_class,       role: COMPARABILITY_KEY, source: plan_action_input}
  - {field: runner_image_label, role: COMPARABILITY_KEY, source: plan_action_input}
  - {field: os,                 role: COMPARABILITY_KEY, source: plan_job_context}
  - {field: arch,               role: COMPARABILITY_KEY, source: plan_job_context}
  - {field: node_version,       role: COMPARABILITY_KEY, source: plan_job_environment,
     wire: [runtime_profile.node_version, plan.runtime_profile_declared.node_version]}
  - {field: pnpm_version,       role: COMPARABILITY_KEY, source: plan_job_environment,
     wire: [runtime_profile.pnpm_version, plan.runtime_profile_declared.pnpm_version]}
  - {field: vitest_version,     role: COMPARABILITY_KEY, source: plan_job_environment,
     wire: [runtime_profile.vitest_version, plan.runtime_profile_declared.vitest_version]}
  - {field: testbucket_sha256,  role: COMPARABILITY_KEY, source: plan_job_environment,
     wire: [runtime_profile.testbucket_sha256, plan.runtime_profile_declared.testbucket_sha256]}
  - {field: working_dir,        role: COMPARABILITY_KEY, source: plan_job_environment}
  - {field: facade_command,     role: COMPARABILITY_KEY, source: plan_job_environment,
     wire: [runtime_profile.facade_command, plan.runtime_profile_declared.facade_command]}
  - {field: setup_command,      role: COMPARABILITY_KEY, source: plan_job_environment}
  - {field: lock_sha256,        role: COMPARABILITY_KEY, source: plan_job_environment,
     wire: [runtime_profile.lock_sha256, plan.runtime_profile_declared.lock_sha256]}
  - {field: discovery_mode,     role: COMPARABILITY_KEY, source: plan_job_environment}
  - {field: exclusions,         role: COMPARABILITY_KEY, source: plan_job_environment}

  # ---- OUTPUT_METADATA — acceptance-contract.md 5.1 (PD-1)
  - {field: est_basis_output,   role: OUTPUT_METADATA, derives_from: profile_est_basis,
     wire: [plan.est_basis, matrix.est_basis],
     provenance: {tags: [], see: ["acceptance-contract.md 13"]}}
  - {field: est_seconds,        role: OUTPUT_METADATA, unit: seconds_one_decimal,
     wire: [est_seconds, "plan.buckets[].est_seconds", matrix.est_seconds],
     provenance: {tags: [D-1], see: ["acceptance-contract.md 5.1"]}}
  - {field: a_eta_ns,           role: OUTPUT_METADATA, unit: nanoseconds,
     wire: [a_eta_ns, "plan.buckets[].a_eta_ns"],
     provenance: {tags: [], see: ["acceptance-contract.md 0.9"]}}
  - {field: wall_est_seconds,   role: OUTPUT_METADATA, unit: seconds_one_decimal,
     wire: [matrix.wall_est_seconds],
     provenance: {tags: [], see: ["acceptance-contract.md 5.1"]}}
  - {field: needs_node,         role: OUTPUT_METADATA, wire: [matrix.needs_node]}

# C. MEMBERSHIPS — acceptance-contract.md 22 test 63 states the membership rule.
memberships:

  # comparability_key — acceptance-contract.md 15.3.
  comparability_key:
    - runner_class
    - runner_image_label
    - os
    - arch
    - node_version
    - pnpm_version
    - vitest_version
    - testbucket_sha256
    - working_dir
    - facade_command
    - setup_command
    - lock_sha256
    - discovery_mode
    - exclusions
    - cache_declaration_digest

  # bc_inv — acceptance-contract.md 19.2.
  bc_inv:
    - manifest.orchestration_repo
    - manifest.orchestration_commit
    - manifest.workload_repo
    - manifest.workload_commit
    - manifest.candidate_sha
    - manifest.testbucket_binary_sha256
    - manifest.instrumentation_binary_sha256
    - manifest.action_tree_digest.plan
    - manifest.action_tree_digest.run-bucket
    - manifest.action_tree_digest.record
    - manifest.facade_sha256
    - manifest.facade_argv
    - manifest.vitest_config_sha256
    - manifest.lockfile_sha256
    - manifest.package_json_sha256
    - manifest.workload_tree_digest
    - manifest.discovery_exclude_prefixes
    - manifest.discovered_unit_set_digest
    - manifest.model_parameters_digest
    - manifest.runs_on_label
    - manifest.runner_class
    - manifest.runner_image_label
    - profile.expanded_unit_set_digest
    - profile.runner_token
    - profile.k
    - profile.count
    - profile.file_parallelism
    - profile.store_sha256
    - cache_state.dependency_cache_mode
    - cache_state.dependency_cache_primary_key
    - cache_state.dependency_cache_producer
    - cache_state.transform_cache_mode
    - cache_state.mongo_binary_sha256
    - manifest.env_tuple            # expands via tuple_leaves below

  # env_tuple — acceptance-contract.md 15.3.
  tuple_leaves:
    manifest.env_tuple:
      - manifest.env_tuple.node_version
      - manifest.env_tuple.pnpm_version
      - manifest.env_tuple.working_directory
      - manifest.env_tuple.setup_command
      - manifest.env_tuple.events_dir
      - manifest.env_tuple.timeout_minutes

# ROLE_EXEMPT — acceptance-contract.md 22 test 63 states the coverage rule (D-2).
# CONTAINER_ROLES — acceptance-contract.md 22 test 63 states the container rule (D-2).
container_roles:
  - {path: invocations, role_field: invocation_membership, checked_by: QC6}
  - {path: cache_state, role_field: cache_state,           checked_by: QC14}
  - {path: runtime_profile, role_field: runtime_profile,   checked_by: QC17}

role_exempt:
  artifacts: [manifest, calibration_evidence, store]
  roled_anyway: ["manifest.orchestration_commit", "manifest.workload_commit",
                 "manifest.candidate_sha", "manifest.campaign_id", "calib.schema"]
  provenance: {tags: [R18-F3], see: ["acceptance-contract.md 0.10"]}

exceptions:
  - {path: "manifest.pairs[].arms[].est_basis",
     provenance: {tags: [], see: ["acceptance-contract.md 19.2"]}, owner: config}
  - {paths: [plan_digest, bucket_composition],
     provenance: {tags: [], see: ["acceptance-contract.md 19.2"]}}
  - {path: "actual_runner_name",
     provenance: {tags: [S-4], see: ["acceptance-contract.md 15.3"]}}
```

**No count appears in this registry or anywhere else.** Leaf and entry counts are *derived* by the
same recursion the tests use — quoting either is what produced the drift this replaces.

## 3. Product definition

### 3.1 What is modelled

Given a live Vitest file set and K, produce a partition whose buckets have as nearly equal `A` as
the available signal allows, and report the displayed estimate that contract §5.1 defines for that
same `A`.

What one `V[j]` contains for Mandel — the whole spawned chain from `pnpm` down to façade cleanup —
and what `A` adds on top of it are named in **`acceptance-contract.md` §2** and are not listed
again here. The point this section makes is narrower: the modelled quantity is a *chain*, not a
Vitest timer, which is why a per-file attribution finer than the invocation is unavailable.

### 3.2 What is not modelled

`A` is not `J`, not the workflow makespan, not the composite-action step duration, and not "the
complete action". `V[j]` is neither a pure Vitest timer nor a whole-wrapper lifetime. §4.2 names
what sits outside each, at file:line. `J` is not measured at all; that absence is stated, not
presented as a control.

### 3.3 Why R54 does not already deliver this

R54 targets per-invocation `V` with externally supplied sealed receipts no ordinary run produces;
its record job never ingests a wall record; `--wall-dir` is empty by default and the dogfood Vitest
lane never sets it; and its allocation weight and displayed estimate can disagree. R54's own delta
over its parent is archive-install hardening contributing no measurement evidence (§12.6).

---

## 4. Exact measurement boundary

**Moved to `acceptance-contract.md` §3 (R18-F1).** The step sequence, the field/interval table, the
ingest invariants, the by-name exclusions, and the clock-and-containment rules were stated here and,
in nearly the same words, in the contract. The contract carries them. What remains below is the
reasoning and the corrections that produced them, and nothing else.

### 4.1 Why the boundary sits at the in-process reads

An interval is defensible when both endpoints are read by the process that owns the work between
them, and not otherwise. Anything timed from outside — a composite-step duration, a job duration, a workflow total —
folds in queueing, runner allocation and image boot, none of which a partition can influence, so a
balance claim built on it would be measuring the wrong thing. That is why the honest labels are
**instrumented run-bucket interval** for `A` and **Exec-envelope interval** for `V`, and why
"complete action", "whole wrapper" and "whole job" are used nowhere in this package. Where the reads
sit, and which field covers which interval, is contract §3.1's.

### 4.2 Why the exclusions are named individually

A boundary stated only as what is *inside* it invites a reader to assume the rest. Naming each
excluded region at file and line makes the omission checkable instead of implied, and it is what
lets `script_overhead_ns` and `wrapper_ns` be residuals with a stated composition rather than
unexplained slack. The list itself is contract §3.2's.

### 4.3 Clock and containment — the correction this section records

**An earlier revision claimed descendants are drained (R11-D9).** It said so in the paragraph
describing `V[j]` while the containment paragraph on the same page disclaimed exactly that. A parent
cannot `waitpid` a grandchild without becoming a child subreaper, which this product does not do, so
the stronger claim was infeasible as written; every "reap descendants" phrasing was removed and both
paragraphs now describe one end point. What the wrapper does promise, and the limitation that
remains when a descendant calls `setsid` or double-forks, are contract §3.3's to state.

**`J` is not measured at all.** That absence is recorded rather than presented as a control, and
contract §12 names the measurement that would justify restoring stronger containment.

---

## 5. Observation schema

**Moved to `acceptance-contract.md` §13 (R15-F1).** This section states no requirement; the contract carries it.

### 5.0 The canonical `profile` type (S4)

**Moved to `acceptance-contract.md` §13.0 (R15-F1).** This section states no requirement; the contract carries it.

### 5.1 `cwd_digest` and `process_group_id` — exact definitions (F5)

**Moved to `acceptance-contract.md` §13.1 (R15-F1).** This section states no requirement; the contract carries it.

## 6. Ingest, audit, and the closed loop

### 6.1 Workflow wiring

**Moved to `acceptance-contract.md` §14.1 (R15-F1).** This section states no requirement; the contract carries it.

### 6.2 CLI

**Moved to `acceptance-contract.md` §14.2 (R15-F1).** This section states no requirement; the contract carries it.

### 6.3 Qualification checks — where each check's inputs come from (annex)

**The checks themselves are `acceptance-contract.md` §7.1**, which states QC1…QC18 including QC7a,
QC14a and QC14b. This section states no check (R14-F1); it records, for each, the artifact the
inputs come from and the reason the check exists, which is the part a reader needs when
implementing ingest and which the contract does not carry.

| Check | Reads | Why it is there |
|---|---|---|
| QC1–QC2 | the observation's own bytes | a row that cannot be identified cannot be audited, and the basis field of contract §5.1 lives on the profile object so that a plan and its rows cannot disagree about basis |
| QC3–QC5 | the shard plan the record job reads **once**, plus the row | the plan is the only authority for a bucket's identity and unit set; re-reading it per row would let two plans coexist in one ingest |
| QC6 | the plan's rendered invocations | invocation membership is a container-level property, which is why the field registry gives `invocations` a container role |
| QC7 / QC7a | the interval fields of §4.1 and the definitions of §5.1 | the boundary invariants are what make `A` a measured interval rather than a reported number |
| QC8 | `boot_id_start` / `boot_id_end` | a reboot between the two clock reads invalidates the monotonic interval |
| QC9–QC10 | terminal state and the reporter-event coverage audit | contract §7.1 keeps a failed or partially observed bucket as a diagnostic and out of training |
| QC11 | the ring | duplicate rows would double-weight one execution |
| QC12–QC13 | the plan document and the profile block copied verbatim into the row | the comparability key and the canonical profile are the two things a re-fit on an archived corpus has to reconstruct |
| QC14a | the **bucket runner's** environment and filesystem | it is the only place the executed MongoDB binary exists — see §8.4a and contract §10.5.6 |
| QC14b | the observation as uploaded | it is all the record job has; contract §10.5.6 states why it must not touch the path |
| QC15 | the three identity fields of §8.1a | one `head_sha` carrying three identities is the defect S-6 removed |
| QC16 | `realtime_start` and the row-intrinsic tuple | contract §15.1b's recency key is a total order because of it |
| QC17 | the observation's `runtime_profile` object and the plan document's `runtime_profile_declared` | the plan says what the run was configured to execute; only the bucket knows what it did execute, and contract §15.3a compares the two field by field |
| QC18 | the plan document's displayed estimate and optimized objective for that bucket, and the row's echo of each | contract §5.1 makes the row's two estimate fields audit copies of what the plan decided, so the plan is the only thing that can falsify them; the assembler derives one of the row's fields from the other, which is why their agreeing with each other proves nothing about either |

What the ingest CLI prints, and in what form, is **contract §14.2**'s; this section maps each check
to the input it reads and says why that input is the right one.


## 7. The wall model

### 7.0 What the model controls — and what it does not

Unit topology is decided by stored reporter outcomes read before any weight is applied — which
decisions those are, and at which call sites, is **`acceptance-contract.md` §6.2**'s to enumerate.
The consequence this section draws from it is the one that matters for allocation: the basis field
of contract §5.1 **selects the partition weight, not the work items.** Unit topology is
reporter-derived and identical in both bases, enforced across arms by `expanded_unit_set_digest`
(§5, QC12, §13.2) and pinned by `TestTopologyIsInvariantAcrossBases` (test 10).

**This scope allows that explicitly rather than denying it.** Those three values are historical,
file-keyed, and observed before treatment assignment, which makes them ordinary pre-treatment
covariates. The checkable claim is the narrow one: **no current-run or post-assignment outcome
reaches allocation.** The broader phrasings — "outcome-free allocation", "no outcome-derived
influence", "mechanically pre-campaign" — are **false** and appear nowhere in this scope, the
contract, the code, or the logs (`TestNoShippedStringOverstatesTheMeasurement` (tests 13 and 24)). The scorer does not read the numeric EWMA directly, but
it scores units whose existence and composition those outcomes selected, and saying otherwise would
be the defect.

Two consequences follow and are accepted:

- The reachable improvement is bounded by the topology the reporter store produces. If a file's
  reporter weight understates its wall cost badly enough that it should have been split and was
  not, wall-basis allocation cannot fix it.
- Making topology depend on measured wall time is a separate, larger change (whale thresholds would have to be
  expressed in `A` terms) and is **out of scope** here. §7.6 records it beside the other deferred
  option.

### 7.1 Field roles — a NON-EXHAUSTIVE synopsis of the §2A registry (SR-6, R11-D7)

**This table is a reading aid, not the authority.** It shows the roles that matter to the model and
to ingest, with representative members; it does **not** enumerate every role of the registry and does **not**
list every member. The **authoritative, complete classification is §2A's `roles` block**, whose
seven roles are `REGRESSOR`, `RESPONSE`, `IDENTITY`, `DIAGNOSTIC`, `ADMISSION`, `COMPARABILITY_KEY`,
and `OUTPUT_METADATA`; **its size is derived from the block and is written nowhere** (R14-F4). Test
63 checks that block, not this synopsis. An earlier revision claimed
this table *derived* every field from the registry while omitting `COMPARABILITY_KEY` and
`OUTPUT_METADATA` entirely, plus many members of the roles it did show; the claim is withdrawn
rather than the table padded, because duplicating the registry in prose is what produces drift.

**The two roles this synopsis omits, named so the omission is explicit:** `COMPARABILITY_KEY` (the
ordered leaves of §8.3, enumerated by `memberships.comparability_key` and nowhere else) and
`OUTPUT_METADATA` (the additive plan/matrix display fields of §9.3).
Membership in a cross-cutting set (`comparability_key`, `bc_inv`, `env_tuple`) is not a role.

The synopsis table that stood here assigned fields to roles, which is the `roles` block of §2A's
own content and contract §0.9's for the design columns. It is removed rather than kept synchronised
(R19-F1); the two omitted roles named above are the only part of it that was not a projection.

| Role | Where the assignment lives |
|---|---|
| **REGRESSOR** | §2A `roles`, read by contract §0.9 as the ordered design columns |
| **RESPONSE** | §2A `roles`, read by contract §0.9 |
| **DIAGNOSTIC** | §2A `roles` |
| **IDENTITY** | §2A `roles` |
| **ADMISSION** | §2A `roles` |

**The count is DIAGNOSTIC and the indicator is the design column, and they are different facts.**
Which is which, and how the indicator is derived, are contract §6.4's; the point recorded here is
that the two are computed at different times — the count is an audit fact about a plan, the
indicator is a model input.
Feeding the count would change the model's shape, which contract §6.4b excludes.

**Historical timing is a pre-treatment covariate only under mandatory conditions.** The fitted
model's `fitted_at` and the store's `updated_at` strictly precede the first arm's **authenticated**
start; the fitting population contains **zero rows with `trainable: false`** — the single in-CI
mechanism, set at append from the row's own bytes against the supplied config, primarily its
`campaign_id` (§13.9c, §8.1b class A) — **and**, as an operator-side cross-check
after dispatch, zero rows whose `run_id` is in the campaign harness universe or whose authenticated
`run_started_at` falls inside the campaign window (§11.2); and every exclusion domain is **named** in
the manifest, never merely absent from a list — contract §6.4b makes all of them conditions of the
campaign, and none is skipped. The run-id and
window predicates of contract §15.1b **cannot** be the in-CI mechanism, because those run IDs do
not exist when the config is frozen (R9-D2) — they exist only to catch a `trainable` marking that disagrees with what
actually ran.

**Corrected here (S-6, and corrected again by R17-F1).** An earlier revision said "zero rows at the
candidate or orchestration commit — decided from each row's own `head_sha`". That overloaded one
field with three identities and contradicted warming: the comparability key pins one
`testbucket_sha256` for every row in a key, so the warm corpus is *necessarily* produced by the
candidate binary.

**A later revision of this paragraph then described the replacement wrongly**, saying the exclusion
is "by campaign run id and campaign window, decided from the row's own bytes". That is the design
the package rejected, and an implementer reading it would build it. The mechanism contract §6.4b and
contract §15.1b actually state is the one this scope explains at §13.9c: the frozen config's `campaign_id` is
stamped on every pilot and campaign observation and travels record-to-ingest; a matching row is
appended `trainable: false`; a missing or mismatched id under a supplied config is refused; the
class-A foreign workload and orchestration domains are materialized into the same flag at append;
and the fitter reads `trainable == true` and nothing else. **Run id and authenticated window are
operator-side post-dispatch cross-checks, not a row-byte exclusion primitive.** `candidate_sha` and
`workload_commit` are recorded separately so a corpus that mixes binaries or workloads can be
detected and reported.

There is **no curator and no sealed training set**: the model is fitted from the store in ordinary
CI, so these conditions are the whole mechanism. Contract §6.3 has any surviving selected-work structure require
**singleton equality** between its unit list and its unit id (§11.0).

### 7.2 Form

**One unit system, and one authority for the equation (S-1, R13-A, restated for R19-F1).** The unit
of every quantity in the model, the point at which the reporter EWMA is converted, and the
dimensionality of each coefficient are **contract §0.9**'s; this section states none of them. What it
records is why they had to be settled at all: an earlier revision left the domain implicit, and two
literal implementations of the same algebra could then disagree by a factor of 1e9.

**The objective `A_eta_ns(b)`, its `round_half_up`, and the display conversion
`est_seconds(b)` are defined once, normatively, in `acceptance-contract.md` §0.9, evaluated in the
checked-integer domain of contract §1.1. This section does not restate them.** An earlier revision
carried the same four-term equation in four places — here, §9.2, contract §0.9 and contract §6.4 —
and one of the copies (in `salvage-audit.md`) silently dropped the `round_half_up`. One equation,
one home, and every other site evaluates it by reference.

**Why the rounding of contract §0.9 is part of the equation and not an implementation note (D-1).** `scale` is a
dimensionless `float64`, so its product with `reporter_sum_ns` is generally fractional while every
other term and the result are int64 nanoseconds. Writing that product unrounded would make the objective of contract §0.9
simultaneously "integer" and fractional, and two literal implementations could disagree
on the last nanoseconds. Contract §1.1 fixes the rounding operator and the overflow behaviour of
every accumulation the objective performs.

The symbol-by-symbol unit table that stood here was a second copy of **contract §0.9**'s column
table and of **contract §5.1**'s display inventory. It is removed rather than kept synchronised.

**The division by 1e9 happens exactly once, on the complete `A_eta_ns`, at the display boundary** —
contract §0.9 states that conversion; it is not restated here.

Where the division happens, and that it happens once, are **contract §0.9**'s. The reason this
section records the question at all is the previous revision: it divided three coefficients by 1e9
while leaving the seconds-valued product unconverted, which made a literal implementation wrong by a
factor of
1e9; `TestWallModelIsIntegerNanosecondsEndToEnd` is a fixture whose answer differs by exactly that
factor under the old expression.

**Fitting arithmetic, in the domain of contract §1.1.** The design matrix and response are built from these integer values and
solved in `float64`; the four coefficients are then stored as `scale` (a `float64`) and three int64
nanosecond values obtained by `round_half_up` of the solved value. Evaluation is contract §0.9's
objective in contract §1.1's domain — **this section does not restate it** (R14-F1) — and because
every term is int64, two evaluations of the same plan produce identical bytes.

What each coefficient of **contract §0.9** absorbs, and when each is paid, is that section's to
state. The engineering reading this section adds is that the three overhead terms correspond to
distinct physical costs — action setup, a merged whole-file invocation, and a per-slice invocation —
which is why one flat per-invocation term could not represent them. For Mandel both overheads cover the same
chain: `pnpm`/`tsx`/façade/preflight startup plus Vitest initialization, config load, project
resolution, and reporter flush.

**Why the indicator, and not a flat per-invocation term (PD-3).** The previous form,
`per_invocation_ns × invocation_count`, treated the whole-file invocation's overhead as a constant
every bucket pays. That is false for a **slice-only** bucket, which has no whole-file invocation and
pays nothing for one. Because the term was assumed constant it was dropped from the allocation
weight, so the optimized objective and the displayed forecast disagreed exactly when topology was
mixed. Worked K=2 counterexample for the objective of contract §0.9. Every number below is **integer nanoseconds**; the seconds in
parentheses are the display rounding only. Coefficient values for the objective of contract §0.9:
`scale = 1` (dimensionless), `fixed_ns = 0`,
`whole_invocation_overhead_ns = 100_000_000_000` (100 s), `per_slice_overhead_ns = 0`:

The example's inputs, in the units contract §0.9 fixes:

| Units | per-unit reporter value | kind |
|---|---:|---|
| `w1`, `w2` | `50_000_000_000` (50 s) each | whole-file |
| `s1`, `s2` | `100_000_000_000` (100 s) each | name slice |

- Additive-weight packing sees `{100e9, 100e9, 50e9, 50e9}` and splits `{s1,w1} | {s2,w2}`, each
  `150e9`. True cost of each: `150e9 + 100e9 = 250e9`. **Makespan `250_000_000_000` ns (250.0 s).**
- Segregating topology gives `{s1,s2} | {w1,w2}`: the slice-only bucket costs `200e9 + 0 = 200e9`;
  the whole-only bucket costs `100e9 + 100e9 = 200e9`. **Makespan `200_000_000_000` ns (200.0 s).**

The additive form is 25 % worse here *and* would have displayed `150.0` while the bucket took
250 s. The corrected objective values both partitions truthfully and prefers the second. Note that
`scale = 1` is meaningful only because both sides of the multiplication are nanoseconds; under the
superseded seconds-predictor form `scale = 1` would have been 1 ns per second and the same example
would evaluate to `150e9 + 100e9` against a `150`-valued neighbour — the 1e9 error this repair
removes.

The `scale` of contract §0.9 is a **predictive association coefficient**: the fitted relationship between the reporter's
per-file sum and the observed `A`. Its value **does not measure a causal fraction** and does not say
"how much of the interval the reporter accounts for". Contract §6.4 excludes any such phrasing,
and `TestWallModelRankAdmissionForWholeSliceAndMixedCandidates` is the test.

**Acknowledged identifiability limit of the model of contract §0.9.** It learns a global scale and two overheads. It
cannot distinguish two same-shaped files with different import graphs, transform volumes, setup
dependencies, top-level side effects, or cache behaviour, and does not claim to. That is exactly
why the six-feature frozen scorer is removed (§12.2) rather than retained: those features could not
identify those costs either, and implying they could was the defect.

### 7.3 Validity condition

Contract §7 admits `est_basis: wall` only at `file_parallelism == 1`, and the reason is arithmetic:
with `--maxWorkers=N>1` a sum of file weights is not the invocation makespan at all.

### 7.4 Fitting — the PD-3 four-parameter model, and nothing else

**One model.** The design matrix, its column order, the coefficient carried by each column, the
response, and the unit of each are **contract §0.9**'s; the second copy that stood here is removed.
What this section records is the defect that made the question urgent: a design in which
column 2 is seconds while the response is nanoseconds is what
`TestWallModelIsIntegerNanosecondsEndToEnd` rejects.

The `[1, reporter_sum, invocation_count]` / `per_invocation_ns` design is **superseded by PD-3** and
appears nowhere.

**Three role-distinct quantities, named apart (F2).** Conflating them was a defect:

| Role | Quantity | Use |
|---|---|---|
| **plan-time predictors** | the ordered design columns of contract §0.9 | the only regressors; all frozen at plan time (§8.1a); contract §0.9 fixes their units |
| **response** | the observation's top-level elapsed time, as contract §2 defines it | the single regression target |
| **diagnostic / audit** | `setup_ns`, `script_ns` (=`VB`), `wrapper_ns`, `script_overhead_ns`, `invocations[].elapsed_ns` (=`V[j]`), `invocation_count`, `whole_file_count` | recorded, reported, checked by QC7. **Never regressors, never responses.** |

The reason the response is `A` rather than `V` is that the two are different quantities, and the
narrower one omits exactly the cost the first coefficient of contract §0.9 exists to learn. Contract §2 separates them; this
section only records why the choice went that way.

**Fitting procedure — the reasoning behind contract §6.8 (annex).** The ordered procedure, the
pinned Lawson–Hanson solver, the rounding step, the two residual definitions, the model-status
vocabulary, and `model_parameters_digest` are **`acceptance-contract.md` §6.8**. This section
restates none of them (R14-F1); it records why each was pinned and publishes the witness the
acceptance test uses.

**Why the solver is pinned at all.** Full column rank gives a unique *mathematical* optimum, not
identical *floating-point bytes*. Two conforming NNLS implementations can differ in the order they
add columns to the passive set, in how they break an exact dual tie, and in the order they sum rows
inside the inner solve — each of which can move the last few bits of a coefficient, and therefore
`model_parameters_digest`, which contract §19.2 has both campaign arms compare equal. Pinning the family, the tie
rules, and the summation order is what makes that digest reproducible rather than approximately
stable.

**Why residuals come from the rounded coefficients.** The deployed predictor is the rounded one, so
a `residual_mae_ns` computed from the raw solution would describe a model that never plans anything.
The `degraded` decision has the same reason: contract §6.8 gates the predictor that will actually be used.

**Why the non-integer coefficient of contract §0.9 is serialized as a quoted string.** A bare JSON
number lets a float formatter vary the bytes; quoting removes the formatter from the digest
entirely.

**Published witness — the row-25 corpus of `testdata/walltime/test58-feedback-oracle.json`.**
With the deployed row-25 coefficients `[5e9, 0.6981132075471698, 12830188679, 7528301887]`, the
five distinct row shapes give residuals (predicted − observed):

| Shape | count | residual (ns) |
|---|---:|---:|
| `[1,10,1,0] → 23e9` | 8 | `+1 811 320 754` |
| `[1,20,1,0] → 33e9` | 8 | `−1 207 547 170` |
| `[1,15,0,1] → 23e9` | 7 | `0` |
| `[1,30,1,2] → 49e9` | 1 | `+4 830 188 679` |
| `[1,10,1,1] → 42e9` | 1 | `−9 660 377 359` |

`Σ|r_i| = 38 641 509 430` over `n = 25`, so the exact mean is `1 545 660 377.2` and
`residual_mae_ns = 1 545 660 377`. Nearest-rank p90 takes index `ceil(0.9 × 25) = 23`, whose value is
`residual_p90_ns = 1 811 320 754`. Both are acceptance values for test 58.

**Why the fitting population needed an in-CI mechanism at all (S-2, R10-D3, restated for R19-F1).**
An earlier revision expressed the pre-campaign restriction as a pair of predicates over run IDs and
an authenticated time window. Both are unevaluable inside CI: the run IDs do not exist when the
config is frozen, and the authenticated instants need an API call the bucket job cannot make. A
restriction that no in-CI component can evaluate is not a restriction, and that is the defect this
entry records. Which mechanism replaced it, what the fitter reads, what the cutoff compares, and
what the campaign rows may be used for are **contract §6.4b, contract §15.1b, and contract §19.9c**;
this section states none of them. `TestCampaignRowsNeverRefitTheModel` is the test.

**The coefficient of contract §0.9's column 2 is predictive, not causal.** It is a fitted
association, not "the fraction of the Exec interval the reporter accounts for", and no document
says so. Import, transform, environment
setup, and hooks occur inside the measured target, which makes their **aggregate** effect
observable; it does **not** make them separately attributable, and file-specific import graph,
transform volume, setup dependency, top-level side effects, and cache behaviour remain **not
identified** by this model.

### 7.4a Rank admission — why this criterion and not the earlier ones (annex)

**The criterion is `acceptance-contract.md` §6.6**: `rank(X) == 4` under the frozen tolerance
`tol = sigma_max(X) * 4 * 2.220446049250313e-16`, with the Golub–Reinsch `sigma_max`, the pivoted
Householder QR, the serialization encoding, and the `E_RANK_NON_CONVERGENT` outcome when the SVD's
iteration cap is exhausted. This section restates none of it (R14-F1) and records why each of those
was pinned.

**Row and run counts are not identifiability.** The corpus minima of contract §0.4 are necessary
and never sufficient, which is why a rank test exists at all.

**The earlier topology-specific support rules were withdrawn as mathematically underspecified.** "A
slice-only candidate needs at least one `I=0` row" and "a candidate with slices needs two distinct
`slice_count` values" do not imply rank of the relevant columns: an all-`I=0` corpus is itself rank
deficient, and `reporter_sum_ns` can stay collinear with `slice_count` even when two slice counts
exist. Whole-only candidates had no rule at all. A rank test over the actual design matrix subsumes
all three cases and cannot be gamed by a topology that satisfies a proxy.

**Why the tolerance is frozen rather than left to a library.** Two conforming rank-revealing QR
implementations can pick different tolerances and therefore different boundary decisions on the same
corpus. Freezing the formula is not enough on its own: contract §6.6 also pins the norm feeding it, or
the same formula yields two thresholds. That is why `sigma_max` names a specific classical method
rather than "the largest singular value" — a randomised or truncated estimator answers a different
question near the boundary.

**Why non-convergence is a failure and not a status.** Reporting a rank test that did not converge
as *rank-deficient* would silently deny a warm model the corpus may in fact support, and the two are
not the same fact. The terminal outcome that keeps them apart is contract §1.3's.

**Upper bounds, not exact ranks (R10-D6).** Four identical `[1,10,1,0]` rows have rank 2, not 4: a
row count bounds rank from above and says nothing about it from below. Test 54 publishes literal
matrices with their exact ranks so the boundary is checkable without running the planner.

### 7.5 Cold start — two ordered phases, four normative outcomes (F3)

Earlier revisions called this an exhaustive, mutually exclusive table. That was wrong:
`{scored: true, basis omitted, store missing}` matches both "emit a reporter cold plan" and "reject
every cold plan". The two are not competing rows — they are **two sequential phases**, and ordering
resolves the overlap.

**Phase 1 — mode selection.** From the basis request and store availability, decide what plan would
be produced.

**Phase 2 — scored admission, higher priority.** A scored run's veto is evaluated after phase 1 and
overrides it.

The four outcomes themselves, their inputs, and the order in which they are evaluated are
**`acceptance-contract.md` §0.8**'s and are not reproduced here. The property worth recording is
structural: they are *ordered* rather than disjoint, because the scored-admission outcome
deliberately overlaps two of the others and wins. That is why neither "exhaustive" nor "mutually
exclusive" is claimed anywhere in this package, and why warm routes needed no separate case.
**Model status vocabulary.** The statuses, the corpus thresholds that separate them, and which of
them may proceed under wall basis are **contract §6.8**'s; no value is repeated here.

### 7.6 Deferred alternatives, named and costed

- **Per-unit physical labels** would need one invocation per scheduling unit — roughly 175 extra
  `pnpm`/`tsx`/façade/Vitest startups per bucket at Mandel's file count (1,396 files at K=8) instead of
  1. They cannot be approximated by copying a multi-unit `V` onto one unit or dividing it across
  units, which is why contract §0.9 models the bucket rather than the unit.
- **Wall-derived topology** (§7.0) would mean expressing whale thresholds and shard widths in `A`
  terms rather than reporter seconds, which changes `expandUnits`, the store's split policy, and the
  audit's expectations together.

Both are separate, measurable product decisions. Neither is inferred from current data.

---

## 8. Store schema and migration

### 8.1 Schema 2

**Moved to `acceptance-contract.md` §15.1 (R15-F1).** This section states no requirement; the contract carries it.

### 8.1a The bounded history ring row (SR-2, S-6)

**Moved to `acceptance-contract.md` §15.1a (R15-F1).** This section states no requirement; the contract carries it.

### 8.1b Recency, eviction, and exclusion — defined before any solver order (S-6)

**Moved to `acceptance-contract.md` §15.1b (R15-F1).** This section states no requirement; the contract carries it.

### 8.2 Migration

**Moved to `acceptance-contract.md` §15.2 (R15-F1).** This section states no requirement; the contract carries it.

### 8.3 Comparability — the execution-profile digest (F6)

**Moved to `acceptance-contract.md` §15.3 (R15-F1).** This section states no requirement; the contract carries it.

### 8.4 Scored cache state — declared, recorded, and compared (S-5)

The timing store is pinned by artifact id and SHA-256 and re-verified after download (§13.5), and
neither arm writes it. What that pins is the *model input* and nothing further. The runtime state
that also moves the measured process lifecycle is a separate matter: the pinned Mandel workflow restores its PNPM
store with a **prefix fallback**, the setup composite restores a MongoDB binary by key, and Vitest
keeps its own transform cache. Two arms with identical lockfile bytes can therefore differ in a
measured preflight/import/transform input. Cross-arm lockfile equality proves dependency *intent*,
not runtime cache *state*.

**Why the run declares a cache mode at all.** The leaves and their legal values are
**`acceptance-contract.md` §10.5.0**; this section is the derivation behind them and states no rule
of its own (R14-F1). What follows is the measurement argument: dependency-cache state, transform-cache
state, and the MongoDB binary all change the preflight/import/transform lifecycle that `A` measures,
so a run that does not pin them has not held its measured input constant — and pairwise equality
inside a pair does nothing to stop **pre-campaign fitting** from mixing `disabled` rows with
`exact-key` rows under one model. The per-job outcome leaves vary legitimately between bucket jobs,
which is exactly why they are recorded per row and are not comparability-key leaves.

#### 8.4a Declaration versus outcome — why the split exists (R11-D3)

An earlier revision had the orchestrator write **every** leaf into one `cache-declaration-file` and
pass that path to both `plan` and `run-bucket`. That is physically impossible: the outcome leaves
are results of a bucket job's own restore, setup and input validation, produced on a different
runner *after* the matrix exists, and a filesystem path in the plan job cannot transport them.

**The membership and the classes are `acceptance-contract.md` §10.5.0's, and this section neither
enumerates nor counts them (R16-F2).** An earlier version of this section wrote "The seven leaves"
and then listed four declarations and three outcomes — a table that was already wrong when the
producer, the hit witness, the hashed path and the runner verdict joined the block, and that
disagreed with the registry it claimed to derive from. What survives here is the *reasoning* for the
split, which the contract's table does not carry:

| Class | What makes a leaf belong to it | Consequence |
|---|---|---|
| **declaration** | knowable **before dispatch**, identical for every bucket job of a run, and therefore something the orchestrator can author once | it is the only class that reaches `cache_declaration_digest`, and through it the comparability key, so a change to any of them resets wall history |
| **outcome** | produced by a bucket job **on its own runner**, after the matrix exists | it may legitimately differ between jobs of one run, so it is recorded per observation and is never a comparability leaf; only the subset the registry's `bc_inv` names is compared across arms |

**Why the expectations had to travel as content rather than as a path (R13-D2).** An earlier
revision of this section described the declaration as an ordinary workflow-level artifact and said
the same path and content reach the plan job and every matrix job. **Separate Actions jobs share no
filesystem**, so a pathname string proves nothing about content, and no declared interface carried
the bytes at all. That is the reasoning this section keeps. The protocol that replaced the broken
description is **`acceptance-contract.md` §10.5.2**, and this section neither summarises nor
outlines it.

**Why the restore outcome leaves need a declared producer.** A restore result is meaningless until a
document says which component produced it, because "no result was supplied" and "a restore ran and
missed" are indistinguishable on the wire — that ambiguity is what the earlier draft left open.
**`acceptance-contract.md` §10.5.3** closes it. Which producer values exist, which mode each is
legal under, and what a scored run does when the producer and the mode disagree are its statements,
not this section's.

**Why the frozen declaration and the per-row outcomes are separate records (R13-D2).** One immutable
config record cannot hold many mutable outcome sets: the declaration is authored once, before
dispatch, while each bucket job produces its own outcome set on its own runner. An earlier sentence
here also claimed the ring row carries a cache path, which the field registry contradicts (R15-F2).
Where each half is written, and what is sealed when, is **`acceptance-contract.md` §10.5.4**'s to
say.

**Why the cache check is split across the job boundary.** Only the bucket runner can observe the
binary it actually executed, and only ingest can read a serialized row; one undivided check would
have to do one of those from the wrong side of the boundary. **`acceptance-contract.md` §10.5.6**
draws the line and states what each half covers.

**Acceptance tests.** `TestScoredCacheModeEqualityOrPairInvalid` includes a **two-bucket fixture in
which bucket 0 reports `exact-hit` and bucket 1 reports `miss`** under the *same* declaration — both
rows pass QC14, the arm is valid, and the pair remains **scored**. The plan job is never given
either outcome, and the test asserts the plan's validated input contains only the leaves
contract §10.5.0 classes as `declaration`. `TestScoredCacheDeclarationTransportAndBinaryBinding` (test 70) covers the physical half:
cross-job byte transport with digest verification, both declared restore producers, the fail-closed
case where neither producer supplied a result, and the **decoy-binary** case of contract §10.5.5.

**The state machine lives in the contract, not here (D-5, R13-A).** An earlier revision said
`matched_key` is empty *iff* the mode is `disabled`, while QC14 required `matched_key ==
primary_key` whenever the mode is `exact-key`. Those two rules make an **`exact-key` miss
unrepresentable**: nothing matched, so the field would have to be simultaneously empty and equal to
a non-empty key. The repair is the tuple table of **`acceptance-contract.md` §10.5.1**, which is the
**sole** statement of the transport state machine; this section names it and does not reproduce it.

What matters here is the consequence: the tuple the prefix fallback produces — a non-empty
`matched_key` differing from `primary_key` — is rejected by the QC rule of contract §10.5.6. That is
the condition the Mandel
workflow's own `restore-keys:` list would create, and forbidding it is the point of the rule; a
**miss is a legitimate, representable outcome**, never a violation.

**Why cardinality needed a decision at all (D-5).** Because the block is recorded per observation
rather than once per run, "both arms agree" had no meaning until a document said over what and in
which direction the comparison runs. **`acceptance-contract.md` §10.5** answers that within an
arm-run and **`acceptance-contract.md` §17.19** answers it across a pair. How many blocks a run or
a pair carries, which leaves are compared, and what a disagreement costs are theirs; this section
states none of it.

**Why the comparison is index-wise (R9-D4).** Bucket jobs restore independently, so one may hit
while another misses inside a single arm-run — which is why the comparison is per index and not per
run.

`acceptance-contract.md` §19.2b states what an unequal index means for a scored pair. An earlier
draft of this package said instead that the disposition leaves are never compared across arms at
all; that draft is **superseded** (R15-F2).

The orchestrator's "provision the way Mandel does" instruction is made concrete by the contract
sections named above rather than by anything written here.

**What remains a limitation, honestly.** Ordinary page-cache warmth, filesystem state, and the
runner's own image warmth are **not** bound; they stay a paired-run limitation controlled by
counterbalancing (contract §19.5) and are named in `limitations` on every observation. None of that needs
cgroups, protected environments, or immutable storage.

**Acceptance test.** `TestScoredCacheModeEqualityOrPairInvalid` is the test that binds this
section's reasoning to the rules of contract §10.5.6 and contract §17.19; the cases it must cover
are those sections' to enumerate. What it exists to prevent is the R11-D2 regression: an earlier
revision treated a per-job hit/miss difference as a scoring failure, and that is withdrawn.

## 9. Planner, allocation, matrix, and wording

### 9.1 Mode selection

**Moved to `acceptance-contract.md` §16.1 (R15-F1).** This section states no requirement; the contract carries it.

### 9.2 Allocation algorithm — the numeric witness behind it (derivation)

**The algorithm is `acceptance-contract.md` §6.5, and this section states nothing about it.** Both
stages, the Stage-2 enumeration schedule, the two neighbourhoods, the pass bound, the acceptance key,
the core/adapter hook, the unit discipline and the single display division are fixed there and
nowhere else. What remains here is the **worked K=2 witness** and a record of the drafting errors
that witness exposed — no acceptance rule, no enumeration order, no tie-break, and no bound.

**R17-F1: an earlier version of this section disclaimed stating rules and then stated them** — the
acceptance key, the pass-bound guarantee, "Stage 2 has exactly one schedule", and the enumeration and
first-improving choice, all in the present tense. Those sentences are removed rather than softened;
where a reader needs them, contract §6.5 has them.

**The witness the pair-swap neighbourhood exists for (S-2).** From the Stage-1 mixed seed
`{s1,w1} | {s2,w2}` at 250 each: moving a whole unit gives `100 / 300`; moving a slice gives
`150 / 350`. Every single-unit move raises the maximum, so single moves alone sit at 250 and never
reach the 200 the segregating layout achieves. Swapping `w1` with `s2` gives `{s1,s2} | {w1,w2}` =
`200 / 200`. That is the arithmetic reason contract §6.5 carries a second neighbourhood; the rule
itself is contract §6.5's.

**What the earlier drafts got wrong, recorded.** One draft accepted a move only on a strictly smaller
makespan, said equal candidates "never trigger" a change, **and** promised the lexicographically
smallest bucket vector among equal makespans. Those could not all hold: if the current plan was not
already that minimum, no permitted transition reached it. Another draft promised a fixed point or
convergence, which a pass-bounded schedule cannot deliver — exhausting the bound may leave improving
transitions unexplored, so the reachable property was only reproducibility. Contract §6.5 states what
replaced both.

**Why two stages, and why Stage 1 is the existing KK.** `cost_ns(b)` is not additive over units — the
indicator term depends on the bucket's composition, not on any one unit — so Karmarkar–Karp cannot
optimize it directly; Stage 1 seeds on the additive part and Stage 2 improves against the real
objective. Stage 1 is the generalized Karmarkar–Karp already in `internal/core/partition.go`
(largest-differencing, k-way, with its tag tie-breaks, normalisation and true-load-descending bucket
ordering), and **not** LPT, which that file keeps one function away as a measured baseline:
substituting LPT changes the seed and therefore the published plan bytes. Test 51 asserts the
distinction by fixture, and `TestWallPlannerEscapesSingleMoveLocalMinimum` carries an equal-makespan
fixture — two partitions with identical `max_b cost_ns` and different canonical vectors.

### 9.2a Seconds-valued surfaces — where each row of the inventory comes from (annex)

**The inventory is `acceptance-contract.md` §5.1**, which lists every seconds-valued surface, the
`wall_est_seconds` presence rule, and the exclusion of `ImbalancePct`. This section restates none of
it (R14-F1) and records why the list has two kinds of row.

**Two kinds of seconds, and conflating them was the defect.** The reporter-basis surfaces —
`PlanUnit`, the reporter `PlanSummary` values — are **legacy v0.2.2 seconds** frozen by PD-1. They
were never nanoseconds and are not converted from any. How the wall-basis surfaces are produced is
contract §5.1's. An earlier pair of lists diverged: one omitted two of the displays, counted a
dimensionless percentage as a duration, and claimed *every* seconds display is a divided nanosecond
quantity.
The repair was to state the narrower claim that is true and to keep one list; R14-F1 then moved that
list into the contract so there is no second one to diverge from.

**Why there is no wall-basis per-unit estimate.** The bucket-level objective carries a whole-file
indicator, and an indicator has no unique decomposition across the units that share the invocation
it indicates — there is no principled way to divide that overhead between them. What unit-level
display shows under each basis, and what the plan report states alongside it, are contract §5.1's.

### 9.3 Matrix semantics

**Moved to `acceptance-contract.md` §16.3 (R15-F1).** This section states no requirement; the contract carries it.

### 9.4 Wording repairs — each with its site

**Moved to `acceptance-contract.md` §16.4 (R15-F1).** This section states no requirement; the contract carries it.

## 10. Fail-closed matrix

**Moved to `acceptance-contract.md` §17 (R15-F1).** This section states no requirement; the contract carries it.

### 10.3 Calibration and warm-up — a topology proposer and an execution protocol, kept apart (S-7)

**Moved to `acceptance-contract.md` §17.3 (R15-F1).** This section states no requirement; the contract carries it.

#### 10.3a Topology proposal — `--calibrate`

**Moved to `acceptance-contract.md` §17.3a (R15-F1).** This section states no requirement; the contract carries it.

#### 10.3b Warm-up execution protocol — W-1…W-4

**Moved to `acceptance-contract.md` §17.3b (R15-F1).** This section states no requirement; the contract carries it.

## 11. Evidence

**Moved to `acceptance-contract.md` §18 (R15-F1).** This section states no requirement; the contract carries it.

### 11.0 Ledger sufficiency — what this evidence supports

**Moved to `acceptance-contract.md` §18.0 (R15-F1).** This section states no requirement; the contract carries it.

### 11.1 Primary — testbucket's monotonic wrappers

**Moved to `acceptance-contract.md` §18.1 (R15-F1).** This section states no requirement; the contract carries it.

### 11.2 Corroborating — SHA/run/artifact-bound raw GitHub logs

**Moved to `acceptance-contract.md` §18.2 (R15-F1).** This section states no requirement; the contract carries it.

### 11.3 Non-claims

**Moved to `acceptance-contract.md` §18.3 (R15-F1).** This section states no requirement; the contract carries it.

## 12. Keep / simplify / remove plan

**KEEP 22 · SIMPLIFY 49 · REMOVE 112 · UNCERTAIN 1**, each of the 184 changed paths exactly once,
verified against `jj diff --summary`.

### 12.1 KEEP — 22 files

Measurement primitives `internal/walltime/{clock.go, clock_linux.go, clock_fallback.go,
clock_other.go, nanos.go, canon.go, audit.go, spec.go, procsignal_linux.go, procsignal_other.go}`;
`internal/core/{plan.go, store.go, audit_bucket.go}` (changed additively — "keep" means keep the
change); `internal/runner/types.go`; `internal/runner/gorunner/render.go` (metadata added, default
rendered bytes unchanged); regressions `internal/core/consumer_contract_test.go`,
`internal/walltime/{cancel_test.go, canon_test.go}`,
`internal/runner/vitestrunner/toolpath_test.go`, `cmd/testbucket/{actionpins_test.go,
auditplan_test.go}`; `.github/workflows/ci.yml`.

### 12.2 SIMPLIFY — 49 files

`internal/walltime`: reduce `{action.go, exec.go, contain.go, contain_other.go, contain_pgroup.go,
record.go, verify.go, spawn_other.go, doc.go, gates.go, palloc.go, aeta.go}` to a
begin/run/exec/end lifecycle, one observation writer, one verifier, and §7's model.
`cmd/testbucket`: keep `wall begin|run|exec|end|verify`, `ingest --wall-observations`,
`plan --est-basis`; delete the campaign/stage/authority command graph. `internal/planbind`: keep
the allocation and membership bridge; **drop the six-feature runtime schema** — it cannot identify
file-specific import, transform, setup, or cache cost. `internal/runner/vitestrunner`: keep exact
argv, `./x` path tokens, atoms, selector, events reporter, discovery timeout, wall hook. Actions
and workflows reduced to `component-map.json`'s interfaces, plus `est-basis` and
`wall-observations-dir`. `README.md` per §9.4. The 19 test files in `salvage-audit.md`.

### 12.3 REMOVE — 112 files

Candidate delivery and release proof; protected authority, Stage 1/2, replay; multi-signer,
delegation, rosters; triple observers and raw evidence; cgroup delegation and credentials; campaign,
ablation, schedule, and label-evidence machinery; study fixtures; and their 59 tests — enumerated in
`salvage-audit.md` and `component-map.json`.

The cgroup's **mechanical descendant-drain** role is preserved by the owned-process-group wait,
root wait/reap, same-PGID signal, and group drain of §4.3. What is dropped is hostile containment as
an eligibility gate. Contract
§12 states the measurement that would restore it: detached descendants observed surviving
completion and biasing `V` on this consumer's workload.

### 12.4 UNCERTAIN — 1 file: retain `$/` and validate it

`.github/actionlint.yaml` suppresses lint for `$/.github/actions/...`. **Retain it.** `./` inside a
*called* reusable workflow resolves against the caller's workspace and would break external
callers; a hard-coded `owner/repo/…@<sha>` is impossible because `uses:` takes no expression. Risk
is bounded: both consumers compose the composite actions directly, and the campaign orchestrator
does too (§13.3). **Validation task:** call it at a full SHA from a scratch second repository,
confirm resolution, retain the raw job log recording the runner agent version. See ledger A-14.

### 12.5 Implementation order — contract §0.5a

The implementation order is **contract §0.5a**. This section states none of it.

### 12.6 Delivery split

R54's delta over its parent replaces `tar`-listing pipelines whose early `grep` exit produced
SIGPIPE/pipefail false negatives with complete enumeration before install: credible distribution
hardening that changes no timer, feature, label, allocation, estimate, consumer, or campaign
calculation.

- **PR-1 — distribution hardening.** `install-testbucket.sh`, `archiveenumeration_test.go`,
  `candidateinstall_test.go`.
- **PR-2 — practical wall-time balancing.** Everything else. It does not depend on PR-1.

### 12.7 In-tree artifacts

Three consecutive reviews correctly reported that no tracked file or `jj` description contained the
originating brief, "R54", "salvage audit", or "practical contract". The artifacts are now in the
working copy at these exact paths (S-8c):

```
docs/walltime/acceptance-contract.md
docs/walltime/scope.md
docs/walltime/assumption-ledger.md
docs/walltime/salvage-map.md
docs/walltime/source-to-claim-map.md
docs/walltime/salvage-audit.md
docs/walltime/component-map.json
testdata/consumers/SOURCE.md          # the fixture manifest lives WITH the fixture, never under docs/
```

`SOURCE.md` is the fixture manifest, and it lives beside the bytes it describes rather than under
`docs/walltime/`. What it records is contract §20.6a's to list.

---

## 13. Campaign

**Moved to `acceptance-contract.md` §19 (R15-F1).** This section states no requirement; the contract carries it.

### 13.1 Shape

**Moved to `acceptance-contract.md` §19.1 (R15-F1).** This section states no requirement; the contract carries it.

### 13.2 Pair invariant tuple — the bound treatment

**Moved to `acceptance-contract.md` §19.2 (R15-F1).** This section states no requirement; the contract carries it.

### 13.2a Workload binding — anchored to the pinned revision, not to supplied bytes

**Moved to `acceptance-contract.md` §19.2a (R15-F1).** This section states no requirement; the contract carries it.

### 13.3 Orchestration identity ≠ workload identity — Mandel is not modified

**Moved to `acceptance-contract.md` §19.3 (R15-F1).** This section states no requirement; the contract carries it.

### 13.3a Plan admission — an observable check, not a CLI default (P1)

**Moved to `acceptance-contract.md` §19.3a (R15-F1).** This section states no requirement; the contract carries it.

### 13.4 Exact profile, validated against the complete planned bucket set

**Moved to `acceptance-contract.md` §19.4 (R15-F1).** This section states no requirement; the contract carries it.

### 13.4a Two populations, never conflated (F8)

**Moved to `acceptance-contract.md` §19.4a (R15-F1).** This section states no requirement; the contract carries it.

### 13.5 Population, store, order, and dates

**Moved to `acceptance-contract.md` §19.5 (R15-F1).** This section states no requirement; the contract carries it.

### 13.6 Thresholds, effect size, and what the design can resolve

**Moved to `acceptance-contract.md` §19.6 (R15-F1).** This section states no requirement; the contract carries it.

### 13.7 Statistics and gates

**Moved to `acceptance-contract.md` §19.7 (R15-F1).** This section states no requirement; the contract carries it.

### 13.8 Intention-to-treat, retry, and platform failure

**Moved to `acceptance-contract.md` §19.8 (R15-F1).** This section states no requirement; the contract carries it.

### 13.9 The campaign data model — two versioned artifacts, and who reads them (D-2, D-3)

**Moved to `acceptance-contract.md` §19.9 (R15-F1).** This section states no requirement; the contract carries it.

#### 13.9a `testbucket.campaign-config/v1` — frozen before the pilot, immutable thereafter

**Moved to `acceptance-contract.md` §19.9a (R15-F1).** This section states no requirement; the contract carries it.

#### 13.9b `testbucket.campaign-attempts/v1` — appended after each dispatch

**Moved to `acceptance-contract.md` §19.9b (R15-F1).** This section states no requirement; the contract carries it.

#### 13.9c How the exclusion actually reaches the component that fits (R8-D2)

**Moved to `acceptance-contract.md` §19.9c (R15-F1).** This section states no requirement; the contract carries it.

## 14. Preservation gates

**Moved to `acceptance-contract.md` §20 (R15-F1).** This section states no requirement; the contract carries it.

### 14.1 v0.2.2 exact-path atoms

**Moved to `acceptance-contract.md` §20.1 (R15-F1).** This section states no requirement; the contract carries it.

### 14.2 Audit and coverage

**Moved to `acceptance-contract.md` §20.2 (R15-F1).** This section states no requirement; the contract carries it.

### 14.3 baml-rest — no ordinary source path first, execution neutrality second

**Moved to `acceptance-contract.md` §20.3 (R15-F1).** This section states no requirement; the contract carries it.

### 14.4 Mandel unit-only safety — project-based predicate, with a static audit

**Moved to `acceptance-contract.md` §20.4 (R15-F1).** This section states no requirement; the contract carries it.

### 14.5 Matrix compatibility

**Moved to `acceptance-contract.md` §20.5 (R15-F1).** This section states no requirement; the contract carries it.

### 14.6 Consumer snapshots

**Moved to `acceptance-contract.md` §20.6 (R15-F1).** This section states no requirement; the contract carries it.

### 14.6a Pinned consumer fixture — an acceptance input in this tree (T1, C1, M1)

**Moved to `acceptance-contract.md` §20.6a (R15-F1).** This section states no requirement; the contract carries it.

### 14.6b Adoption gate (M1)

**Moved to `acceptance-contract.md` §20.6b (R15-F1).** This section states no requirement; the contract carries it.

## 15. Interfaces after simplification

**Moved to `acceptance-contract.md` §21 (R15-F1).** This section states no requirement; the contract carries it.

## 16. Test plan

**Moved to `acceptance-contract.md` §22 (R15-F1).** This section states no requirement; the contract carries it.

## 17. Explicitly out of scope

Everything in contract §12, with its measurement-based re-entry criterion. Also: per-file wall
attribution finer than the grouped invocation; unit topology derived from measured wall time (§7.0,
§7.6); any change to the displayed estimate of contract §5.1 for a consumer that has not opted in; Go
wall-time measurement; any change to `est_seconds` for a consumer that has not opted in; a hostile
threat model; baml-rest adoption; modifying Mandel's repository; and any release, tag, merge, or
push.

---

## 18. Decisions taken in this scope

| # | Decision | Alternative rejected |
|---|---|---|
| DEC-1 | The displayed estimate keeps its shape and gains a basis field; displayed and optimized are one quantity. Contract §5.1 states both | reporting the reporter sum while packing by the wall score |
| DEC-2 | The per-unit regressor is the file-keyed reporter EWMA, scaled | six coarse shape features, or manufactured per-unit labels |
| DEC-3 | **PD-3.** Which coefficients sit in the seed weight and which sit in the objective is contract §0.9's; the decision recorded here is that they are not the same set | the superseded additive form, which treated the whole-file overhead as a constant and so displayed a number the partition did not optimize |
| DEC-4 | `K` and `file_parallelism` are excluded from the comparability key | discarding wall history on every K change |
| DEC-5 | Both campaign arms run under measurement | measuring only C |
| DEC-6 | An **external orchestrator** checks out Mandel at `d9ae1d43`; Mandel is not modified | an in-repository adoption workflow, which changes the frozen workload identity |
| DEC-7 | `$/` and its suppression are retained and validated externally | `./` or a hard-coded SHA |
| DEC-8 | Calibration is gated on C's 40 rows only | gating a reporter-work estimate against `A` |
| DEC-9 | A bounded reschedule allowance, whose bound is contract §10.4's | an unbounded rerun-until-green channel |
| DEC-10 | Wall basis is admitted only at `file_parallelism == 1` (contract §7) | an unenforced validity condition |
| DEC-11 | Campaign eligibility is warm-only | claiming both cold-start support and a warm-only requirement |
| DEC-12 | The store reaches both arms as a digest-verified pinned artifact | `actions/cache` restore-keys with an uncaptured matched key |
| DEC-13 | Distribution hardening ships as its own PR | bundling it into a claim it does not support |
| DEC-14 | Specification and consumer snapshots ship in-tree | leaving them in `/tmp` |
| DEC-15 | The pre-declared matched-pair design is kept and the campaign is framed as an engineering release gate over the measured sample (owner contract §0.3); the population itself is contract §10.4's | any inferential framing — significance, power, p-values |
| DEC-16 | Order is a fixed precommitted **counterbalanced** sequence (owner contract §0.2) | randomization, a seed-derived draw, or a reproducible shuffle |
| DEC-17 | Removed machinery returns only on named measurement evidence | a standing refusal with no exit, or re-import on hypothesis |
| DEC-18 | The product claim is scoped to the **partition weight**; unit topology stays store-derived and is held constant across arms | implying the wall model chooses the work items |
| DEC-19 | Cutoff and exclusion domains are named fields the campaign cannot omit (contract §6.4b) | optional fields rejected only when explicitly listed |
| DEC-20 | The allocation surface is stated as **built on pre-treatment reporter outcomes**, with current-run and post-assignment outcomes excluded (contract §6.4b) | claiming an outcome-free allocation surface, which the topology path falsifies |
| DEC-21 | The ledger is used for invocation/action **evaluation** only; unit labels come from the reporter EWMA | treating a signed aggregate `V` as a per-unit label, or adding observers to try to close the gap |
| DEC-22 | Calibration is gated on both an absolute and a relative criterion, conjunctively. The criteria and their values are contract §10.4's; no value is repeated here | an absolute-only gate, which changes meaning with bucket size |
| DEC-23 | Thresholds are precommitted rather than tuned, and the pilot validates the harness. The freeze rule and what it covers are contract §0.4's | a pilot that feeds back into thresholds or design |
| DEC-24 | Every R54 implementation gap is **labelled as a gap** and carries an acceptance test that must fail on current source (owner contract §0.5, §21). The set is enumerated by the §21.0 registry and **no count is quoted** in prose (S-10) | describing intended behaviour as though the source already had it, or quoting a gap count that drifts from the register |
| DEC-25 | Every workload-derived value is **recomputed from the checkout at `workload_commit`** and compared; cross-arm equality is necessary but never sufficient (§13.2a) | trusting supplied/self-consistent workload bytes, which a substituted partition satisfies |
| DEC-26 | **One unit system**, fixed by contract §0.9 and evaluated in the domain of contract §1.1. Neither the unit, the coefficient dimensionality, nor the division is restated here (S-1) | a seconds-valued predictor against a nanosecond response, which makes a literal implementation wrong by 1e9 |
| DEC-27 | Fitting is pre-campaign only, and evaluating a precommitted formula against observed rows is not retuning. Which rows may be fitted on, and what the campaign rows may be used for, are contract §6.4b's (S-2) | fitting from the C rows, which leaks evaluation outcomes into the model; or a "nothing derived at run time" rule that rejects the contract's own relative gates |
| DEC-28 | §2A is a **field registry** — wire paths, primary roles, and cross-cutting memberships as three separate dimensions. Which projections it generates, and how each is compared, are contract §22 test 63's (S-3) | a single name table claiming to govern artifact membership and generate schemas while doing neither |
| DEC-29 | The comparability key binds an explicit stable operator-supplied token rather than a runner instance name. Which leaf carries it, which name stays a diagnostic, and which inputs carry it are contract §15.3's (S-4) | keying on `${{ runner.name }}`, which has no cross-run stability contract and made a three-run minimum unreachable; or naming a label source that no code path can produce |
| DEC-30 | Every scored run declares and records its cache state, with prefix-fallback matches rejected and per-pair equality required over the registry's cross-arm-invariant membership only (§8.4, contract §10.5, contract §17.19, S-5/R9-D4). Which leaves those are, what a prefix-fallback match is, and which leaves stay per-row diagnostics are the contract's. Page-cache warmth stays an explicit unbound limitation | copying the pinned workflow's `restore-keys:` prefix fallback, which silently changes the installed tree and is never captured |
| DEC-31 | **Three separate provenance identities** rather than one overloaded field, and ring recency defined before any solver ordering. The fields, the recency key and the stamp it uses are contract §15.1a and contract §15.1b's (S-6) | overloading one field with three identities; defining "most recent W" by a lexical solver sort; or excluding every row at the candidate SHA, which makes warming unreachable |
| DEC-32 | A bounded search reports that it did not find a layout, not that none exists, and the proposer is total so every legal case terminates. The outcome vocabulary and the partial/total split are contract §6.7's (S-7/R12-D4) | reporting a bare infeasibility after four heuristic layouts, which asserts more than a bounded search can prove |
| DEC-33 | The exact-consumer collision claim is bound by running the **production `assignFilterAtoms`** over the full pinned 1,512-path universe, not by two illustrative root-containment examples (§14.6a, S-8) | asserting only the two named pairs, which a break in project-root suffix enumeration would pass |
| DEC-34 | Within-pair order is **sequential** and distinct from launch order, which carries no drift claim; the pilot proves plumbing only. The predicate and the pilot's arithmetic are contract §19.5 and contract §19.6's (S-9) | a start-only check that two overlapping jobs satisfy; or a pilot asserted to prove the full campaign's bookkeeping |
| DEC-35 | Implementation deltas live in **one ordered machine-readable registry** (§21.0). It is the source for the **counts**, the **test symbols**, and every cell of the register table, which test 41 compares column for column (R14-F1); the gap bodies remain hand-authored (R10-D8); **no count of gaps appears in prose**, and no document quotes a line count or SHA-256 of a document in this set (S-10) | a hand-maintained register whose stated counts, ID ordering, and test mappings drift from its own contents |

---

## 19. Verdict

The product is defined narrowly and completely. Revision 4 closed the three findings that were
genuinely new — partition weight versus unit topology (§7.0, `TestTopologyIsInvariantAcrossBases` (test 10)), mandatory cutoff and
exclusion domains (§7.1, contract §17.15, `TestCampaignProvenanceAndCutoff` (test 15)), and orchestration identity separated from workload
identity so Mandel is never modified (§13.3, `TestCampaignIdentitiesAreSeparateFields` (test 16)). Revision 5 closes what the fourth review
added on top of the same finding set: the allocation surface is now **affirmatively described** as
built on pre-treatment reporter outcomes rather than claimed outcome-free, with the three deciding
store fields named at file:line (§7.0, DEC-20, `TestNoShippedStringOverstatesTheMeasurement` (tests 13 and 24)); the ledger's sufficiency is split
explicitly into invocation/action **evaluation** — which it supports — and unit **training labels**,
which it does not (§11.0, DEC-21, `TestSelectedWorkRequiresSingletonUnitEquality` (test 23)); and the suite's green state is no longer inherited from
a reviewer, because this one claims no execution-test result (`go test -race -count=1 ./...` on Linux (test 25), ledger A-04).

Nothing here needs a protected environment, a signing key, a published release, a candidate
resolver, a self-hosted runner, branch protection, an immutable archive, or a repository setting
that does not exist. The only new operational requirement is read access to the Mandel repository
for the orchestrator — an ordinary CI credential. No removed proof machinery returns except on named
measurement evidence, and the cgroup's mechanical drain role is preserved rather than dropped.

One item is open work rather than a blocker: the `$/` reference has never been exercised from an
external caller (§12.4), which no consumer and no campaign path depends on. The specification and
the consumer fixture **are in the tree** (`docs/walltime/`, `testdata/consumers/`); what remains is
the campaign, gated by AG-1…AG-4 (§14.6b).

**Revision 13 (S-1…S-10 — the specification tail repair).** The ten normative choices this batch
makes are recorded as **DEC-26…DEC-35** in §18, each with the alternative it rejects, so the
decision record is complete rather than implicit in the prose. Every finding of the current
`scope-adversary.md` now has **one unambiguous normative repair and one executable acceptance
test**, mapped in §20 and listed in §16 as tests 52, 53, 58–65 alongside the amended 27, 28, 41,
43 and 57. The two structural changes are §2A — rebuilt from a name table that claimed to generate
schemas into a **wire-path / role / membership registry** whose four generated lists are each
byte-compared to the section that owns them — and §21.0, **one ordered machine-readable delta
registry** from which §21's **counts, test symbols, and every register-table cell** are derived —
the table is compared column for column (R14-F1) while the gap bodies stay hand-authored, and no
renderer exists (R11-D10). Nothing in this revision
reopens PD-1, PD-2, PD-3, the trusted-CI threat model, or the five owner resolutions, and no
previously passed boundary is weakened: the timer definitions, exact consumer membership,
K=8/count=1/serial admission, Go isolation, and the removal of the research proof stack are
unchanged.

**Revision 12 (SR-1…SR-10 + canonical schema table) — historical record; the rules it produced now
live in the contract, chiefly contract §0.9, contract §5.1 and contract §15.3.** The structural
change is §2A: one
machine-readable table assigning every field exactly one role, from which the model equation,
history row, plan/observation schema, BC-INV set, comparability key, mutation tests, and the
consistency test all derive. On top of it: a pair-swap neighborhood that actually reaches the K=2
witness (§9.2); sort/exclusion identity in the history row (§8.1a); a plan-time-only profile key
(§8.3); a frozen rank tolerance (§7.4a); two distinct `est_basis` roles (§5.0); a role taxonomy
replacing "learnable inputs" (§7.1); generated BC-INV cases (§13.2); the A-versus-V clock
distinction (§11.3); and a bounded warm-up protocol (§10.3).

**Revision 10 (specification repair F1–F9).** PD-1, PD-2, and PD-3 remain frozen and were not
reopened. Repaired here: the four-parameter PD-3 objective is now the only model anywhere,
including the store and both machine-readable maps (§7.4, §8.1, §8.3, DEC-3); rank sufficiency
refuses instead of zeroing a collinear column (§7.4a); PD-2's two escape hatches are deleted
(§7.5); PD-1 is expressed as a canonical **legacy projection** rather than full-matrix byte
identity (§21.1); `BC-INV`'s size is stated only in its declaration (§13.2); the comparability key
becomes a `comparability_key_digest` over plan-job-sourced leaves (§8.3); descendant-reap
promises are replaced by root-reap-only semantics with stated limits (§4.3); scored and attempted
populations are separated (§13.4a); and the artifact inventory is reconciled to 20 paths (§1,
§12.7, §14.6).

**Revision 9 (owner decisions PD-1…PD-3 and specification defects S4–S8).** The governing artifacts
are in the tree at `docs/walltime/` and the pinned consumer fixture at `testdata/consumers/`; that
is the current state throughout this document and the ledger. Added here: additive-compatibility
freezing (contract §0.7), the four cold-start cases (contract §0.8, §7.5), the nonlinear partition objective
with its worked K=2 counterexample (contract §0.9, §7.2, §9.2), the canonical `profile` type and bounded ring
row (§5.0, §8.1a), the named `BC-INV` list with a generated mutation matrix (§13.2), the two-class
consumer-fixture contract (§14.6a), reschedule-not-redraw language (§13.5, §13.8), and the
nine-row implementation-delta register (§21.0).

**Revision 8 (adjudication + re-review).** The fresh adversarial review returned **`SCOPE_PASS`**
(§20); its only non-pass rows are `L2` (the campaign has not run — `EXTERNAL_BLOCKED`) and `Q2`
(the threat model, an owner decision now recorded at ledger A-39). The governing artifacts are now in the tree at `docs/walltime/`,
with the pinned consumer fixture at `testdata/consumers/`; §23 maps all 11 adjudicator findings to
the file that closes each. New here: plan-time admission checks (§13.3a), the Go repin
compatibility contract (§14.3), the adoption gate (§14.6b), authenticated-start order verification
(§13.5), the planner-route and cutoff bindings (contract §6.4a–contract §6.4b), a component salvage map, and
a source-to-claim map correcting the installer-only premise with verified arithmetic.

This revision also repairs one genuinely new review finding, `M4`: cross-arm equality could not
distinguish the intended Mandel partition from a self-consistent substituted one, so §13.2a now
derives every workload input from the checkout at the pinned `workload_commit` and compares, with
contract §17.16 failing closed and `TestWorkloadBindingRecomputesFromPinnedCheckout` (test 29) covering the both-arms-wrong case.

It applies five owner resolutions rather than review findings: the threat model and its
single re-entry condition (contract §0.1); a fixed precommitted **counterbalanced** order with
randomization removed everywhere (contract §0.2, §13.5, `TestCampaignOrderIsCounterbalancedNotRandomized` (test 18)); the campaign reframed as an **engineering
release gate** with all inferential language deleted (contract §0.3, §13.6, `TestNoInferentialClaimIsShipped` (test 26)); every threshold frozen
before run 1, with calibration gated **absolutely and relatively** — 10 s / 20 s and 5 % / 10 % of
`mean(A)`, conjunctive — and every exact value tabulated in contract §10.4 (contract §0.4, §13.7, DEC-22,
DEC-23, `TestThresholdsAreFrozenBeforeRunOne` (test 27)); and the R54 implementation gaps labelled
as gaps, each with an acceptance test that must fail on current source, enumerated by the §21.0
registry with **no count quoted in prose** (contract §0.5, §21, DEC-24,
`TestImplementationGapTestsFailAgainstR54` (test 28)). §22 records the review criterion for the next
visit.

**`SCOPE_READY`**

---

## 20. Adversarial findings → repairs

**No document in this set quotes its own or another artifact's line count or SHA-256 (S-10).**
Every such figure went stale the moment the file it described was edited, and three of them were
already stale before the review that found them. Reports are referenced by **path and verdict**;
their bytes are checked by `TestScopeArtifactConsistency` (test 41) against the working copy at
read time, never against a number written here.

### 20.0a Prior review — `scope-adversary.md`, `NEEDS_SCOPE_REVISION` on S-1…S-10

PD-1, PD-2, PD-3, the trusted-CI model, and the five owner resolutions are accepted without
reopening. All ten defects are repaired here as specification; the **§2A field registry** (rebuilt
from a name table into a wire-path / role / membership registry) and the **§21.0 delta registry**
are the two structural changes the rest hang from.

| # | Defect | Normative repair | Acceptance test |
|---|---|---|---|
| S-1 | model mixed seconds and nanoseconds; a literal implementation could be wrong by 1e9 | §7.2 fixes **one unit system**: `reporter_sum_ns` in integer nanoseconds, `scale` dimensionless, every coefficient and the response in nanoseconds, and the **complete** `A_eta_ns` divided by 1e9 **once**, at the display boundary. §7.4, §8.1a, §9.2, §9.2a, §9.3 and the worked K=2 example all state the same choice; `a_eta_ns` is serialized alongside `est_seconds` | `TestWallModelIsIntegerNanosecondsEndToEnd` (test 60) |
| S-2 | C rows were both held out and used to fit; the threshold prose rejected its own relative gates | §7.4 fits **only** from rows excluded by §8.1b from the campaign run set and window; C rows compute residuals, MAE, worst error and `mean(A)` and **never refit**. §13.7 states that the **formula and percentage are precommitted while the numerical bound is evaluated from observed C rows**, and test 27 rejects retuning rather than formula evaluation | `TestCampaignRowsNeverRefitTheModel` (test 61); `TestThresholdsAreFrozenBeforeRunOne` (test 27) |
| S-3 | the 85-row table could not generate the schemas it claimed | §2A is rebuilt as three separate dimensions — **wire paths** (artifact, type, cardinality, every serialized path including the thirteen the observation example carried with no row), **roles** (one primary role each), and **memberships** (comparability key, `BC-INV`, `env_tuple`, by wire-path reference). Aliases are removed; `profile.store_sha256` and `store_sha256` are distinct paths; the loose top-level `runner_token` is deleted from §5; the generation claim is narrowed to four lists, each byte-compared to the section that owns it | `TestFieldRegistryCoversEverySerializedPath` (test 63) |
| S-4 | the key carried a plan-job runner **instance** name and an unproduced label | §8.3 removes `runner_name` from the digest and replaces it with **`runner_class`**, an explicit stable string supplied as a non-optional `plan` action input; `runner_image_label` gains a real producer — a **`runs-on-label`** input passed to `plan` **and** `run-bucket`, echoed as `observed_runs_on_label` and compared by QC12. The instance name survives as the diagnostic `actual_runner_name`. W-3 has three real runs reproduce one key before bounded warm-up is claimed | `TestExecutionProfileIsPlanTimeAvailableAndLabelBound` (test 53); `TestWallComparabilityProfileResetsHistory` (test 43) |
| S-5 | store bytes were pinned; runtime cache state was not | new §8.4 defines a scored **`cache_state`** block — dependency cache mode, primary key, matched key, disposition, transform-cache mode, executed MongoDB binary digest — forbids prefix-fallback matches (QC14), makes the **four** cross-arm-invariant leaves `BC-INV` members compared **index-wise** per pair (contract §17.19), and states page-cache warmth as a named paired-run limitation. Contract §0.2 and §10 now claim "the sole **intentional and configured** difference", not physical identity | `TestScoredCacheModeEqualityOrPairInvalid` (test 62) |
| S-6 | ring recency/eviction undefined; one `head_sha` carried three identities | §8.1a splits `head_sha` (orchestration), `candidate_sha` (testbucket build), and `workload_commit` (consumer checkout), and adds the self-reported `observed_start_realtime`, the optional authenticated `run_started_at`, and a per-store `ingest_seq` (**diagnostic only, never read**). New §8.1b defines the **recency key** `(observed_start_realtime, intrinsic_id)` where `intrinsic_id = (repository, run_id, run_attempt, job_id, bucket_index, plan_digest)` — the `ingest_seq` form was append-dependent and is **withdrawn** (R8-D3/R10-D7) — deliberately the *self-reported* stamp, because ingest runs in the record job and authenticated instants are operator-side (§11.2), so an authenticated recency rule would not be implementable where it runs — plus eviction at `W = 240`, and exclusion by **`campaign_id` → `trainable`** in CI, with run-id and authenticated-window predicates as **operator-side** cross-checks (R10-D3), all **before** §7.4's summation sort. It also explains why a blanket candidate-SHA exclusion would make warming unreachable, and states the clock-skew limitation | `TestWallHistoryRecencyEvictionAndThreeIdentities` (test 52); QC15 (test 3) |
| S-7 | calibration could not prove `INFEASIBLE`; the round-trip test omitted the corpus | §10.3a renames the bounded outcome **`NOT_FOUND_WITHIN_BUDGET`**, reserves `STRUCTURALLY_INFEASIBLE` for a real proof — **column 4** dead for an all-whole universe, **column 3** dead for an all-slice one (corrected by R8-D1/R10-D6), and makes `L` a **partial** layout function under a **total proposer**, so every legal `N ≥ 1` has a defined outcome (R12-D4). §10.3b separates the topology proposal from the W-1…W-4 execution protocol. Test 58 is seeded with **23 rows over ≥3 runs plus a named boundary fixture**, then adds the 24th | `TestCalibrationModeTerminatesSufficientOrNotFoundWithinBudget` (test 59); `TestWallObservationRoundTripClosesRecordLoop` (test 58) |
| S-8 | the exact-consumer collision claim rested on two examples | §14.6a distinguishes the illustrative root-containment relation (2 pairs) from the **production** shared-project-root suffix relation (42 pairs), and has the regression run the **production `assignFilterAtoms`** over all 1,512 pinned paths, asserting 42 pairs, 0 Case crossings, transitive closure, no split atom, and `./x` tokens | `TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm` (test 64) |
| S-9 | pilot arithmetic wrong; order check was start-only | §13.6 corrects the pilot to **1 pair × 2 arms × 8 buckets = 16 rows** and restricts it to plumbing, moving 80-row bookkeeping to a synthetic or real-campaign check. §13.5 defines **launch order** versus **sequential order** and pins authenticated `completed_at(first) ≤ started_at(second)`; §13.6 scopes the attribution claim to the inputs actually held equal | `TestPairsRunSequentiallyByAuthenticatedCompletion` (test 65 / test 30); `TestCampaignDenominatorWording` (test 20) |
| S-10 | the delta register was internally corrupted and its mappings pointed at unrelated tests | §21.0 is **one ordered machine-readable registry** (`id`, `title`, `target`, `r54_status`, `acceptance_tests`, `gap_section`, `dependency`) from which the **counts** and **test symbols** derive; the register table is a **byte-exact four-column projection** compared by test 41 (R14-F1) while the gap bodies stay hand-authored (R10-D8); every count is derived, the six wrong test mappings are corrected, §21.1–§21.4 appear once each in order, every mangled `the "…" test` reference is restored to a real symbol, and **no document quotes a line count or SHA-256 of a document in this set** | `TestScopeArtifactConsistency` (test 41); `TestImplementationGapTestsFailAgainstR54` (test 28) |

**`TestScopeArtifactConsistency` fails on the snapshot this revision replaced and passes only
after these repairs** — that is the point of the test, and the prior revision's claim that it passed
over documents visibly failing its advertised checks is itself corrected here.

### 20.0b Current review — `scope-tail-adversary-r7.md`, `NEEDS_SCOPE_REVISION` on D-1…D-13

A fresh independent review of the **repaired** package. Its numeric reproductions — the integer
objective, the K=2 witness, rank behaviour, the registry projections **as they stood at that
revision**, the full 42/0 production collision relation, the pilot/campaign arithmetic, and the
fixture digests — were re-verified here and **all reproduce exactly**. **R13-A: the registry
cardinalities that review published are HISTORICAL and are deliberately not reproduced here.** They
described an earlier registry, they are not the current one, and quoting a registry projection
outside the registry is the second-authority pattern this revision removes; every current
cardinality is derived from the machine block by test 63. Several defects are in
the S-1…S-10 repairs themselves.

| # | Defect | Normative repair | Acceptance test |
|---|---|---|---|
| D-1 | canonical equations omitted `round_half_up` while calling `A_eta_ns` integer; contract §1 called every coefficient integer although `scale` is float; "`est_seconds` is the only seconds field" was false; `a_eta_ns` placement disagreed across artifacts and was unconditional on plan buckets | every canonical equation now carries `round_half_up(scale × reporter_sum_ns)` (§7.2, §9.2, contract §0.9, contract §6.4); contract §1 states three int64 coefficients **plus `scale` as the one dimensionless float**; §9.2a is the **normative enumeration of every seconds display**; `a_eta_ns` is `cardinality: optional` on plan buckets and observations, present iff wall basis, and **absent from matrix entries** | `TestEveryExposedEstimateDeclaresItsQuantity` (test 49), extended to traverse plan, matrix, observation, summary and banner in both bases |
| D-2 | §2A claimed a total bidirectional artifact schema it could not be — plan/matrix omit the legacy field set, 48 scalars had no role, BC-INV's comment said "manifest" while members span three artifacts, and the manifest had no pair/arm/attempt tree | the claim is **narrowed to an additive subset**: bidirectional for the five wholly-new artifacts, forward-only for plan/matrix, with PD-1's legacy projection owning the rest. `role_exempt` (manifest, calibration) and `container_roles` make the role partition exact and enumerated. The manifest gains `schema`, `frozen_at`, store identity, frozen `model_parameters`, `excluded_window.start/end`, `excluded_orchestration_commits`, and full `pairs[].arms[]` and `attempts[]` trees; `est_basis` moves into the arm record | `TestFieldRegistryCoversEverySerializedPath` (test 63) |
| D-3 | the leakage rule had **no data path**: the manifest froze before run IDs existed, ingest received no exclusion input, and the operator-side window check ran after an in-CI fit could already have happened; the pilot preceded the freeze | new **§13.9** splits `campaign-config/v1` (frozen **before the pilot**) from `campaign-attempts/v1` (appended post-dispatch). `record` gains `campaign-config`; `ingest` **appends every qualifying row and retains it**, setting **`trainable:false`** on any row carrying the config's `campaign_id` — pilot rows included — and **rejecting** only a row whose `campaign_id` is **missing or mismatched** under a supplied config (fail-closed). The fitter reads only `trainable:true`. `run_id` and the authenticated window are **operator-side cross-checks**, never in-CI, because those IDs do not exist at freeze time (R12-D1) | `TestCampaignRowsNeverRefitTheModel` (test 61), driving matching / missing / mismatched / no-config / pilot / delayed-ingest |
| D-4 | `profile.scored` had no producer; AD-9/AD-10 were enforced by `plan` but their inputs went only to `run-bucket`; QC13 had no byte path for profile; `setup_command`'s producer was described backwards; one label string copied twice proved nothing | `plan` gains **`scored`** (explicit — basis cannot imply it, since scored B is `reporter`), plus `cache-declaration-file`, `candidate-sha`, `workload-commit` so AD-8…AD-10 are enforceable where they are stated; `run-bucket` gains **`shard-plan`** and copies the profile block verbatim; `setup_command`'s producer is the **plan** action's own input; and one workflow expression drives **all four** of `jobs.plan.runs-on`, `jobs.<bucket>.runs-on`, and both action inputs | `TestExecutionProfileIsPlanTimeAvailableAndLabelBound` (test 53); `TestScoredPlanAdmission` (test 31) |
| D-5 | an `exact-key` **miss** was unrepresentable — `matched_key` had to be both empty and equal to a non-empty primary key; the "six-leaf" invariant had five; no cardinality was defined | an **exhaustive three-tuple state machine** makes `miss` legal (`matched_key` empty) while still rejecting prefix fallback (non-empty and unequal); cardinality is **one block per bucket job**, eight per arm-run, compared **index-wise**; the cross-arm leaves are the BC-INV members `acceptance-contract.md` §19.2 projects. The clause that made `matched_key`/`disposition` per-row *and uncompared across arms* is superseded by contract §19.2b (R15-F2) | `TestScoredCacheModeEqualityOrPairInvalid` (test 62) |
| D-6 | ring retention was **arrival-order dependent**: 241 rows sharing one timestamp evict differently forward vs reversed, so test 52's shuffle-invariance could not pass | the recency tie key becomes the **row-intrinsic** `(repository, run_id, run_attempt, job_id, bucket_index, plan_digest)`; `ingest_seq` is demoted to an append diagnostic read by nothing; **QC16** rejects an unparseable `realtime_start` and a duplicate intrinsic identity | `TestWallHistoryRecencyEvictionAndThreeIdentities` (test 52), with the **241-row equal-timestamp permutation fixture** |
| D-7 | `L(5)` re-isolated the unit `L(3)` already isolated, so `L(5) = L(4)` and test 59's boundary was unsatisfiable; residual placement, slot exhaustion, slice ordering, one-slice and empty-bucket behaviour were undefined | `L(U, K, i)` is a **partial** layout function under a **total proposer**: `L(i)` isolates `i − 3` whole units, so `L(5)` isolates **two**; ties, residual KK placement, slot exhaustion, the one-slice case, the no-slice case, and empty buckets are each specified; **literal fixtures** are published with bucket lists, design rows and ranks — at the corrected `N = 3` / `N = 4` boundary (R11-D5) | `TestCalibrationModeTerminatesSufficientOrNotFoundWithinBudget` (test 59) |
| D-8 | the 23→24 test compared against a coefficient and a wall plan that **do not exist at 23** | **three** boundaries: 23 rejects; **24** creates the first literal model and plan, compared against published expected coefficients — a labelled **cross-basis** transition, not an update; **25** produces the coefficient-and-partition change on a published knife-edge fixture | `TestWallObservationRoundTripClosesRecordLoop` (test 58) |
| D-9 | Stage-2 accepted only strict makespan decreases yet promised the lexicographically smallest bucket vector — unreachable if the current plan is not already minimal | acceptance compares the **tuple** `(makespan, canonical_bucket_vector)` lexicographically and accepts a strict decrease **of the tuple**, so an equal-makespan rearrangement that lowers the vector is reachable | `TestWallPlannerEscapesSingleMoveLocalMinimum` (test 51), with an **equal-makespan fixture** |
| D-10 | the excerpt digest was abbreviated, `tracked-test-paths.txt` had none, and offline fixture integrity was conflated with checkout validation | both full digests recorded (`149a0e33…`, `1119544350…`, matching R7's independent computation); **two validations split** into an offline table and a checkout table with disjoint inputs and checks; `SOURCE.md`'s adoption link corrected to §21.4; `source-to-claim-map.md` RM-5/RM-6 now protect the **42-pair production relation**, not the two examples; §23 ADJ-T1/ADJ-C1 remapped to tests 39 and 64 | `TestPinnedConsumerCheckoutMatchesManifest` (39), `TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm` (64) |
| D-11 | secondary summaries omitted the rules the primaries carry | contract §7 item 12 carries the **sequential** predicate and rejects overlap; item 17 covers AD-8…**AD-10** and `scored`; contract §17.17 covers **AD-1…AD-10**; §20.2 gate 6 and §23 ADJ-P1 likewise; §20.2 gate 3's duplicated symbol is replaced by test 49 | `TestScoredPlanAdmission` (31), `TestPairsRunSequentiallyByAuthenticatedCompletion` (65) |
| D-12 | the registry could not generate the prose it claimed to generate; compound deltas had one test each; the dependency comment omitted six IDs; test 28 had no R54 overlay procedure | the generation claim is **narrowed to what is checkable** — id set, order, and acceptance-test symbols, with Delta/R54 cells then declared hand-authored prose (**superseded by R14-F1, which makes all four columns compared**); compound rows carry **`acceptance_tests` arrays** (ID-4, ID-9, ID-21); the dependency comment is replaced by the **authoritative edge list** with the narrative marked commentary; and the **red/green overlay** is defined (test-files-only `jj` workspace at R54; compile-and-fail *or* fail-to-compile are both red, pass means the row is wrong) | `TestScopeArtifactConsistency` (41), `TestImplementationGapTestsFailAgainstR54` (28) |
| D-13 | stale language across every companion | ledger A-32's candidate/orchestration exclusion corrected, A-37's "three gaps" derived, A-43's test numbers 34/35/36 → 33/34/35, A-44/A-45's superseded internals marked, A-46's "thirteen" withdrawn, A-38/A-44/A-45/A-46 headings moved VERIFIED → CORRECTED to match the summary, A-48.3's false registry claim corrected; `salvage-audit.md`'s "calibrated additive model … complete-action" marked superseded by PD-3 and S-1; `component-map.json`'s observation list 51 → 53, workflow inputs added, units claim corrected, descendant-reap promise replaced, historical go-test/clean status labelled | `TestScopeArtifactConsistency` (41) |

### 20.1 Previous review — SR-1…SR-10, repaired in the prior revision and retained

| # | Defect | Repair | Test |
|---|---|---|---|
| SR-1 | Stage 2 cannot reach the K=2 witness | the repair added neighbourhood (2b), whose entry condition, order, acceptance rule and bound are contract §6.5's and are not restated in this record (R17-F1); §9.2 keeps only the arithmetic witness, shared pass bound, tie-break | `TestWallPlannerEscapesSingleMoveLocalMinimum` |
| SR-2 | history row omits sort/exclusion identity | §8.1a adds `head_sha`, `run_attempt`, `bucket_index`; `plan_digest` is the one canonical plan identity; sorting and exclusion read the decoded row. **Extended by S-6:** three identities, `run_started_at`, `ingest_seq`, and the §8.1b recency/eviction rule | `TestWallHistoryRecencyEvictionAndThreeIdentities` |
| SR-3 | profile key not available at planning time | §8.3 sources every leaf from the **plan job**; a plan/test label mismatch is a reported misconfiguration. **Extended by S-4:** `runner_class` replaces `runner_name`, and `runs-on-label` is an explicit input | `TestExecutionProfileIsPlanTimeAvailableAndLabelBound` |
| SR-4 | rank admission had no numeric tolerance | the tolerance is frozen — its formula, the pinned `sigma_max` method, and the pivoted QR now live in contract §6.6, and this ledger row does not restate them (R14-F1); §7.4a records why each was pinned | `TestWallRankToleranceBoundary` |
| SR-5 | estimate schema contradicted the nonlinear model | §5.0 gives `est_basis` two distinct roles; contract §5 replaces "additive per-unit score" with the bucket-level objective plus Stage-1 linearized seeds | `TestEveryExposedEstimateDeclaresItsQuantity` |
| SR-6 | diagnostics mislabelled as model inputs | §7.1 is a role taxonomy derived from §2A; `whole_file_count` is DIAGNOSTIC, the regressor is the indicator | `TestWallObservationFieldRolesAreFrozen` |
| SR-7 | BC-INV test hard-coded a field count | cases generated from §2A `memberships.bc_inv`, recursive over `tuple_leaves`, each member a wire path; no literal count anywhere | `TestArmsDifferOnlyByPlannerMode` |
| SR-8 | artifacts failed their own consistency test | items fixed literally; ledger partitions disjoint and exhaustive. **S-10 finds this claim was premature** and rebuilds the register and the consistency test's own contract | `TestScopeArtifactConsistency` |
| SR-9 | action clock described as one-process | §11.3 states `V[j]` as one process lifetime and `A` as two CLI invocations sharing a boot identity | `TestActionIntervalUsesPersistedSameBootMonotonicEndpoints` |
| SR-10 | no specified route to a warm profile | §10.3 gives a bounded pre-campaign calibration protocol with W-1…W-4; contract §7 item 14 mirrors it. **Extended by S-7:** the proposer and the execution protocol are separated, and the bounded negative outcome is renamed | `TestMandelWarmupPlanProducesQualifyingFullRankCorpus` |

### 20.2 The reviewer's acceptance ledger, mapped

Its eight gates are "implementation/evidence gates, not requests to revise scope". Each already has
a home here:

| # | Reviewer gate | Where specified |
|---|---|---|
| 1 | integration fixture proving `V` encloses the façade/Vitest lifecycle, with the excluded prefix/suffix documented | §4.1–§4.2, `TestBoundaryInvariantsAndComponentSpans` (test 1); contract §3 |
| 2 | frozen scorer from pre-treatment labels, or a declared group model; pass held-out absolute and relative gates | §7.0–§7.2, contract §6.4b, contract §10.4; `TestModelFittingIsDeterministicAndStatusAware` (test 5) and `TestCampaignProvenanceAndCutoff` (test 15) |
| 3 | UI label and value agree — reporter work vs action ETA | §9.2–§9.4, `TestEstSecondsMatchesItsBasis` (test 8), `TestEveryExposedEstimateDeclaresItsQuantity` (test 49), `TestNoShippedStringOverstatesTheMeasurement` (tests 13 and 24) |
| 4 | a manifest mutation anywhere except the mode fails B/C admission | §13.2, contract §17.12, `TestCampaignValidatorProfileAndPairInvariants` (test 14) |
| 5 | exercise ordinary cold start and the warm-only campaign rule without mixing | §7.5, §13.5, `TestExplicitWallBasisFailsClosed` (test 6) and `TestStoreMigrationPreservesReporterRows` (test 11) |
| 6 | reject any scored plan not exactly Vitest/K=8/count=1/serial/0…7 | §13.3a **AD-1…AD-10** — adding the runner-class/runs-on-label inputs (AD-8), the declared `cache_state` (AD-9), and the candidate/workload identities (AD-10) — contract §17.17, contract §17.20, `TestScoredPlanAdmission` (test 31) |
| 7 | bind exact Mandel identities and adopt the wall path at an exact revision | §13.2a, §13.3, §14.6a, §14.6b; `TestWorkloadBindingRecomputesFromPinnedCheckout` (test 29) and `TestGoConsumerSurfaceUnchangedAcrossRepin` |
| 8 | collect the full 5-pair/10-run/80-row campaign on authenticated dates and apply every frozen gate | §13.5–§13.8, contract §10.4; `TestCampaignDatesAreAuthenticated` (test 17) and `TestPairsRunSequentiallyByAuthenticatedCompletion` (test 30) |

Gates 1–7 are implementation work this scope specifies and tests. Gate 8 is the campaign, which is
L2's `EXTERNAL_BLOCKED` and is closed by running it.

## 21. Known R54 implementation gaps, labelled, with acceptance tests

**Owner resolution contract §0.5.** The properties in the register below are **gaps in R54's current
implementation**, not properties the source already has. They are labelled as gaps wherever they
appear, and each carries an exact acceptance test the implementation node has to pass **before
validation**. Each test **fails against R54 as it stands**
(`TestImplementationGapTestsFailAgainstR54` (test 28)) — a test that already passes is not evidence
the gap was closed.

**No count of gaps appears in prose, here or anywhere else (S-10).** The earlier revision said
"three gaps" in four places while carrying four detailed gap sections, and "thirteen deltas
(ID-1…ID-13)" while listing fourteen IDs with ID-14 before ID-13 and §21.4 before §21.3. Counts are
now **derived** from the ordered registry block in §21.0 by the same parse the tests use.

### 21.0 The implementation-delta registry — one ordered machine-readable source

**This block is the only place a delta is declared (D-12).** An earlier revision said the prose table
and the §21.1–§21.4 bodies were "generated from it" while no renderer existed — the same defect S-10
found in the register it replaced. Both surfaces are now **checked projections** instead: the
register table is byte-exact on all four columns (R14-F1), and each gap body's `Target` row
reproduces the `target` field of every registry row it covers, verbatim (R16-F1). What the gap bodies
still add by hand is `file:line` detail and prose about *why* a gap exists — never a target and never
an acceptance-test set of their own:

| Surface | Status | What test 41 enforces |
|---|---|---|
| the registry block | **the single machine source** | parses; ids ordered and unique; every `gap_section` exists; every dependency resolves and is acyclic; every `acceptance_tests` symbol defined in §16 |
| the prose register table | **byte-exact projection (R14-F1)** | **all four columns** are compared row for row and in order: `ID`, `title`, `r54_status`, and the acceptance-test symbols. Nothing in it is uncompared prose any more |
| §21.1–§21.4 gap bodies | **checked projection plus hand-authored rationale (R16-F1)** | each exists, is referenced by at least one registry row's `gap_section`, its `Target` row equals the registry `target` of every row it covers verbatim, and it names only tests the registry lists for that gap |

Counts remain derived: `TestImplementationGapTestsFailAgainstR54` (test 28) enumerates `id` from the
block — never from a literal number — and checks that each named test exists and fails against R54.

**Compound deltas carry `acceptance_tests` arrays, not a single symbol (D-12).** A one-test mapping
under-covered targets that bundle several behaviours: ID-4 spans the canonical profile *and*
AD-1…AD-10, ID-9 spans fixed sequence *and* ITT *and* dates, ID-21 spans identities *and* the
external orchestrator. Each such row now lists every test its target needs.

**The R54 red/green overlay, with per-row attribution (D-12, corrected by R10-D8).** Test 28
checks that each acceptance test **fails** against R54, but the tests do not exist there, so "fails"
needs a procedure. The earlier one counted *any* compile failure as red for *every* row — and Go
compiles per package, so **one** unrelated missing symbol could mark every row red without
exercising a single row's behaviour. That is not attribution. The corrected procedure isolates each
row:

1. Create a `jj` workspace at R54. **Per registry row**, apply **only** the files named in that
   row's **`overlay_files`** — no product source and no other row's tests — into an otherwise
   untouched tree, and build **only** that package. `acceptance_tests` names *symbols*, which cannot
   be applied; `overlay_files` names the **paths** the overlay copies, and every row carries one
   (R11-D10). Each row's file is distinct, so no two rows share a Go file and the isolation is real.
2. Classify the outcome, with the reason belonging to the row:

   | Outcome | Verdict |
   |---|---|
   | compiles and **fails** an assertion | **red, attributed** — the strongest result |
   | **fails to compile**, and every missing symbol is in the row's **`required_symbols`** set, or in the `required_symbols` of a row reachable through its `dependency` edges (**transitive closure**, computed from the registry graph — not from prose) | **red, attributed** |
   | fails to compile on a symbol outside that computed closure | **NOT attributable** — test 28 fails and the registry row is corrected, because the evidence does not show what it claims |
   | **passes** | the gap does not exist; the registry row is wrong |

3. Attribution is therefore **mechanical**: `required_symbols` is a machine-readable per-row symbol set, and the admissible set for a row is the union over its transitive `dependency` closure. Test 28 records the outcome **and the attributing symbol** for every row, so a red result always
   names why *that* delta is missing rather than that the tree does not build.

Every row's `target` names a section of `acceptance-contract.md` or a machine-registry block (R17-F2: an earlier sentence said the targets are specified in this scope, which the R15 move made false), and every `acceptance_test` is feasible against a
tree that implements it. `dependency` records the chain the deltas form, so a test targets its own
hop and not another's.

```yaml
# implementation-delta-registry v1 — machine-readable; parsed by TestScopeArtifactConsistency
# TestImplementationGapTestsFailAgainstR54 also parses this block.
# Key set, per-key grammar and row ordering — acceptance-contract.md 0.5.
deltas:
  - id: ID-1
    title: "est_basis field on plan and every matrix entry"
    target: "contract §13.0, contract §16.1, contract §16.3"
    r54_status: "does not exist"
    acceptance_tests: [TestColdStartModeSelection]
    overlay_files: ["internal/core/plan_estbasis_test.go"]
    required_symbols: ["core.PlanDocument.EstBasis"]
    gap_section: "21.1"
    dependency: null
    provenance: {tags: [D-1], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 5.1"]}

  - id: ID-2
    title: "per-basis integration of the displayed estimate with the optimized objective"
    target: "contract §5.1, contract §6.5, contract §16.3"
    r54_status: "est_seconds is the store-weight sum in both modes; AllocationScore can change packing without changing the display"
    acceptance_tests: [TestEstSecondsMatchesItsBasis]
    overlay_files: ["internal/core/plan_aeta_display_test.go"]
    required_symbols: ["core.PlanBucket.AEtaNs"]
    gap_section: "21.1"
    dependency: ID-1
    provenance: {tags: [D-1], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 0.9"]}

  - id: ID-3
    title: "bound B/C mode field, with the BC-INV mutation matrix"
    target: "contract §19.2; field-registry `memberships.bc_inv`"
    r54_status: "scorer selected by --palloc-scorer; no bound mode field"
    acceptance_tests: [TestArmsDifferOnlyByPlannerMode]
    overlay_files: ["internal/walltime/campaign_bcinv_test.go"]
    required_symbols: ["walltime.BCInvariantTuple"]
    gap_section: "21.2"
    dependency: ID-1
    provenance: {tags: [R8-D5], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 19.2"]}

  - id: ID-4
    title: "canonical profile type emitted by the plan and copied verbatim onto every observation"
    target: "contract §13.0, contract §19.3a"
    r54_status: "profile fields are loose CLI constants"
    acceptance_tests: [TestObservationCopiesCanonicalProfileVerbatim, TestScoredPlanAdmission]
    overlay_files: ["internal/walltime/profile_test.go"]
    required_symbols: ["walltime.CanonicalProfile"]
    gap_section: null
    dependency: null
    provenance: {tags: [S-4], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 13.0"]}

  - id: ID-5
    title: "bounded ring row schema with plan-time frozen regressors"
    target: "contract §15.1a"
    r54_status: "no ring; no plan-time freezing"
    acceptance_tests: [TestWallHistoryFreezesPlanTimeFeatures]
    overlay_files: ["internal/core/store_ring_test.go"]
    required_symbols: ["core.Store.Wall.Observations"]
    gap_section: "21.3"
    dependency: ID-4
    provenance: {tags: [SR-2], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 15.1a"]}

  - id: ID-6
    title: "nonlinear four-parameter objective used for fit, refine, display, and validate"
    target: "contract §0.9, contract §6.5"
    r54_status: "additive per_invocation x count; displayed differs from optimized on mixed topology"
    acceptance_tests: [TestAllocationObjectiveEqualsAetaForWholeAndSliceTopologies]
    overlay_files: ["internal/core/partition_pd3_test.go"]
    required_symbols: ["core.AEtaNs"]
    gap_section: null
    dependency: ID-2
    provenance: {tags: [PD-3], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 0.9"]}

  - id: ID-7
    title: "explicit wall-basis cold-start outcome"
    target: "contract §0.8, contract §17.1"
    r54_status: "--est-basis does not exist, so case (c) cannot arise"
    acceptance_tests: [TestColdStartModeSelection]
    overlay_files: ["internal/core/plan_coldstart_test.go"]
    required_symbols: ["core.ErrWallModelUnusable"]
    gap_section: null
    dependency: ID-1
    provenance: {tags: [F3], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 0.8"]}

  - id: ID-8
    title: "pinned Mandel entrypoint fixture with per-file digests"
    target: "contract §20.6, contract §20.6a"
    r54_status: "no fixture; no per-file digests in tree"
    acceptance_tests: [TestPinnedConsumerStoredFixtureIntegrity, TestPinnedConsumerCheckoutMatchesManifest]
    overlay_files: ["testdata/consumers/fixture_test.go"]
    required_symbols: ["consumers.StoredFixtureDigests"]
    gap_section: "21.4"
    dependency: null
    provenance: {tags: [C1], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 20.6"]}

  - id: ID-9
    title: "campaign fixed-sequence, ITT, and authenticated-date checks"
    target: "contract §19.5, contract §19.8, contract §17.14, contract §17.18"
    r54_status: "schedule.go stores a seed and derives no order; date compare skips empty instants"
    acceptance_tests: [TestCampaignDatesAreAuthenticated, TestCampaignOrderIsCounterbalancedNotRandomized, TestCampaignAttemptAccountingAndITT]
    overlay_files: ["internal/walltime/campaign_dates_test.go"]
    required_symbols: ["walltime.CampaignAttempts"]
    gap_section: null
    dependency: ID-3
    provenance: {tags: [S-9], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 19.5"]}

  - id: ID-10
    title: "rank-sufficiency refusal instead of zeroing a collinear column"
    target: "contract §6.6"
    r54_status: "no rank check; a collinear column is zeroed and planning continues"
    acceptance_tests: [TestWallModelRankAdmissionForWholeSliceAndMixedCandidates]
    overlay_files: ["internal/walltime/palloc_rank_test.go"]
    required_symbols: ["walltime.RankAdmission"]
    gap_section: null
    dependency: ID-5
    provenance: {tags: [F3], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 6.6"]}

  - id: ID-11
    title: "comparability key digest over plan-job-sourced leaves, with runner_class not runner_name"
    target: "contract §15.3"
    r54_status: "weak runner_token|os|arch|label|node|sha tuple, built partly from data unavailable at plan time"
    acceptance_tests: [TestExecutionProfileIsPlanTimeAvailableAndLabelBound]
    overlay_files: ["internal/walltime/comparability_test.go"]
    required_symbols: ["walltime.ComparabilityKeyDigest"]
    gap_section: null
    dependency: ID-4
    provenance: {tags: [S-4], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 15.3"]}

  - id: ID-12
    title: "root-reap-only process-group semantics, with the setsid limitation stated"
    target: "contract §3.3"
    r54_status: "wording promises descendant reap, which a non-subreaper parent cannot do"
    acceptance_tests: [TestProcessGroupDrainsBeforeEnd]
    overlay_files: ["internal/walltime/contain_pgroup_test.go"]
    required_symbols: ["walltime.DrainGroup"]
    gap_section: null
    dependency: null
    provenance: {tags: [SR-9], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 3.3"]}

  - id: ID-13
    title: "scored and attempted populations counted separately"
    target: "contract §19.4a"
    r54_status: "one population; voids and attempts conflated with scored rows"
    acceptance_tests: [TestCampaignAttemptAccountingAndITT]
    overlay_files: ["internal/walltime/campaign_itt_test.go"]
    required_symbols: ["walltime.ScoredVsAttempted"]
    gap_section: null
    dependency: ID-9
    provenance: {tags: [D-3], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 19.4a"]}

  - id: ID-14
    title: "the timing feedback loop: observation to record to ring to refit to next plan"
    target: "contract §14.1, contract §14.2, contract §15.1b, contract §19.9c, contract §19.9c-1, contract §21, contract §6.8, contract §6.5"
    r54_status: "R54 uploads a wall artifact that nothing downloads; ingest has no --wall-observations; there is no ring, no refit, and no path by which a measured bucket changes the next split; the reusable workflow and the record action carry no campaign-config content or expected digest, nothing materializes or verifies those bytes job-locally, neither the ingest command wiring nor core ingest binds a verified config to an observed campaign_id, and no interface test relates the workflow union, its per-job route, the action inputs and the scored-caller projection under their own domains"
    acceptance_tests: [TestWallObservationRoundTripClosesRecordLoop, TestCampaignRowsNeverRefitTheModel,
                        TestRecordInputReachesIngestFlags]
    overlay_files: ["internal/walltime/feedback_loop_test.go", "internal/walltime/campaign_config_transport_test.go"]
    required_symbols: ["walltime.IngestWallObservations", "walltime.VerifiedCampaignConfig"]
    gap_section: "21.3"
    dependency: ID-5
    provenance: {tags: [D-2], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 14.1"]}

  - id: ID-15
    title: "integer-nanosecond model and single display-boundary division"
    target: "contract §0.9, contract §1.1, contract §6.8, contract §15.1a, contract §6.5"
    r54_status: "no wall model of this shape exists"
    acceptance_tests: [TestWallModelIsIntegerNanosecondsEndToEnd]
    overlay_files: ["internal/walltime/units_ns_test.go"]
    required_symbols: ["walltime.ReporterSumNs"]
    gap_section: null
    dependency: ID-6
    provenance: {tags: [S-1], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 0.9"]}

  - id: ID-16
    title: "scored cache-state declaration, recording, and per-pair comparison"
    target: "contract §10.5, contract §17.19"
    r54_status: "no cache_state block; the pinned workflow restores its PNPM store with a prefix fallback and nothing records the matched key; no interface carries the declaration bytes across either job boundary, the caller passes only a pathname, and nothing binds the hashed MongoDB binary to the executed one"
    acceptance_tests: [TestScoredCacheModeEqualityOrPairInvalid, TestScoredCacheDeclarationTransportAndBinaryBinding]
    overlay_files: ["internal/walltime/cachestate_test.go"]
    required_symbols: ["walltime.CacheState"]
    gap_section: null
    dependency: ID-4
    provenance: {tags: [S-5], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 10.5"]}

  - id: ID-17
    title: "three separated provenance identities with chronological ring recency and eviction"
    target: "contract §15.1a, contract §15.1b"
    r54_status: "no ring at all; one head_sha would otherwise carry candidate, workload, and orchestration identity"
    acceptance_tests: [TestWallHistoryRecencyEvictionAndThreeIdentities]
    overlay_files: ["internal/core/store_recency_test.go"]
    required_symbols: ["core.RingRecencyKey"]
    gap_section: null
    dependency: ID-5
    provenance: {tags: [S-6], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 15.1a"]}

  - id: ID-18
    title: "calibration mode with a truthful bounded-negative outcome; L partial under a total proposer"
    target: "contract §6.7, contract §17.3a, contract §17.3b"
    r54_status: "no calibration mode"
    acceptance_tests: [TestCalibrationModeTerminatesSufficientOrNotFoundWithinBudget]
    overlay_files: ["internal/walltime/calibrate_test.go"]
    required_symbols: ["walltime.CalibrateProposer"]
    gap_section: null
    dependency: ID-10
    provenance: {tags: [S-7], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 6.7"]}

  - id: ID-19
    title: "production-algorithm suffix-atom regression over the full pinned path universe"
    target: "contract §20.1, contract §20.6a"
    r54_status: "the retained generic exact_paths_test.go does not bind the pinned 1,512-path universe; no test runs assignFilterAtoms over it"
    acceptance_tests: [TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm]
    overlay_files: ["internal/runner/vitestrunner/pinned_atoms_test.go"]
    required_symbols: ["vitestrunner.AssignFilterAtoms"]
    gap_section: null
    dependency: ID-8
    provenance: {tags: [S-8], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 20.1"]}

  - id: ID-20
    title: "sequential within-pair order verified by authenticated completion and start"
    target: "contract §19.5, contract §17.18"
    r54_status: "no order verification derives from authenticated instants at all"
    acceptance_tests: [TestPairsRunSequentiallyByAuthenticatedCompletion]
    overlay_files: ["internal/walltime/campaign_order_test.go"]
    required_symbols: ["walltime.SequentialOrder"]
    gap_section: null
    dependency: ID-9
    provenance: {tags: [S-9], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 19.5"]}

  - id: ID-21
    title: "consumer adoption SOURCE delta: separate orchestration/workload/candidate identities"
    target: "contract §19.3, contract §20.6b"
    r54_status: "no snapshots and no separate identity fields; the reusable workflow cannot run Mandel"
    acceptance_tests: [TestConsumerAdoptionIdentitiesAreSeparateAndRecorded, TestWorkloadBindingRecomputesFromPinnedCheckout]
    overlay_files: ["testdata/consumers/adoption_test.go"]
    required_symbols: ["consumers.AdoptionIdentities"]
    gap_section: "21.4"
    dependency: ID-8
    provenance: {tags: [R8-D8], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 20.6b"]}
  - id: ID-22
    title: "schema-2 store state matrix, failure subtype carrier, and migration marker"
    target: "contract §15.1, contract §15.1c, contract §15.1d, contract §15.2, contract §6.8"
    r54_status: "the store has no wall object at all, so no status, no subtype carrier and no migration marker exist; store.go writes neither the always-present model-version and comparability-key-digest leaves nor a literal low-row or low-run subtype, the migration path stamps no status, and ingest applies no deterministic precedence when several insufficiency predicates hold at once; nothing carries a wall model version, so no reader refuses an absent or unknown one and no writer drops a fit group across a bump"
    acceptance_tests: [TestWallStoreSchemaStateMatrixAndMigration]
    overlay_files: ["internal/core/store_schema2_test.go", "internal/core/store_status_precedence_test.go", "internal/core/store_model_version_test.go"]
    required_symbols: ["core.Store.Wall.FailureSubtype", "core.WallStatusPrecedence", "core.WallModelVersion"]
    gap_section: "21.3"
    dependency: ID-5
    provenance: {tags: [R23-F2], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 15.1c"]}

  - id: ID-23
    title: "the executed runtime profile: a bucket-side digest bound to the plan's declared one"
    target: "contract §15.3a, contract §7.1, contract §17.12"
    r54_status: "the comparability leaves are read from the plan job only; a bucket records its runner label and instance name and nothing else, so no digest is computed over the Node, pnpm, Vitest, testbucket binary, facade command, lock or cache mode it actually ran, and no check compares one against the plan"
    acceptance_tests: [TestBucketRuntimeProfileMatchesPlan]
    overlay_files: ["internal/walltime/runtime_profile_test.go"]
    required_symbols: ["walltime.RuntimeProfileDigest"]
    gap_section: null
    dependency: ID-11
    provenance: {tags: [R14-A1], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 15.3a"]}

  - id: ID-24
    title: "scored cache symmetry: one declared configuration across both arms of a pair"
    target: "contract §19.2b, contract §10.5.0, contract §17.19"
    r54_status: "the pinned workflow restores its PNPM store with a prefix fallback, nothing records the matched key, and no rule ties the two arms of a pair to one cache configuration or to equal per-index outcomes"
    acceptance_tests: [TestScoredCacheIsSymmetric]
    overlay_files: ["internal/walltime/cache_symmetry_test.go"]
    required_symbols: ["walltime.ScoredCacheSymmetry"]
    gap_section: null
    dependency: ID-16
    provenance: {tags: [R14-A2], see: ["acceptance-contract.md 0.5", "acceptance-contract.md 19.2b"]}

# Dependency graph — the rows are the graph; acceptance-contract.md 0.5 states the rule.
```

**Register table — a byte-exact projection of the block above (R14-F1).** It is **no longer
hand-authored prose.** Every cell is copied verbatim from the registry: the `Delta` column is each
row's `title`, the `R54 today` column is its `r54_status`, and the acceptance-test column is its
`acceptance_tests`, in registry order. Test 41 compares **all four columns**, not just the IDs and
test symbols, so a paraphrase that drifts from the registry now fails rather than being permitted.
The earlier arrangement declared the Delta and R54 cells uncompared prose, which meant the register
had a second, unchecked projection of itself — the drift this registry exists to stop.

| ID | Delta (`title`) | R54 today (`r54_status`) | Acceptance test(s) |
|---|---|---|---|
| ID-1 | est_basis field on plan and every matrix entry | does not exist | `TestColdStartModeSelection` |
| ID-2 | per-basis integration of the displayed estimate with the optimized objective | est_seconds is the store-weight sum in both modes; AllocationScore can change packing without changing the display | `TestEstSecondsMatchesItsBasis` |
| ID-3 | bound B/C mode field, with the BC-INV mutation matrix | scorer selected by --palloc-scorer; no bound mode field | `TestArmsDifferOnlyByPlannerMode` |
| ID-4 | canonical profile type emitted by the plan and copied verbatim onto every observation | profile fields are loose CLI constants | `TestObservationCopiesCanonicalProfileVerbatim`, `TestScoredPlanAdmission` |
| ID-5 | bounded ring row schema with plan-time frozen regressors | no ring; no plan-time freezing | `TestWallHistoryFreezesPlanTimeFeatures` |
| ID-6 | nonlinear four-parameter objective used for fit, refine, display, and validate | additive per_invocation x count; displayed differs from optimized on mixed topology | `TestAllocationObjectiveEqualsAetaForWholeAndSliceTopologies` |
| ID-7 | explicit wall-basis cold-start outcome | --est-basis does not exist, so case (c) cannot arise | `TestColdStartModeSelection` |
| ID-8 | pinned Mandel entrypoint fixture with per-file digests | no fixture; no per-file digests in tree | `TestPinnedConsumerStoredFixtureIntegrity`, `TestPinnedConsumerCheckoutMatchesManifest` |
| ID-9 | campaign fixed-sequence, ITT, and authenticated-date checks | schedule.go stores a seed and derives no order; date compare skips empty instants | `TestCampaignDatesAreAuthenticated`, `TestCampaignOrderIsCounterbalancedNotRandomized`, `TestCampaignAttemptAccountingAndITT` |
| ID-10 | rank-sufficiency refusal instead of zeroing a collinear column | no rank check; a collinear column is zeroed and planning continues | `TestWallModelRankAdmissionForWholeSliceAndMixedCandidates` |
| ID-11 | comparability key digest over plan-job-sourced leaves, with runner_class not runner_name | weak runner_token\|os\|arch\|label\|node\|sha tuple, built partly from data unavailable at plan time | `TestExecutionProfileIsPlanTimeAvailableAndLabelBound` |
| ID-12 | root-reap-only process-group semantics, with the setsid limitation stated | wording promises descendant reap, which a non-subreaper parent cannot do | `TestProcessGroupDrainsBeforeEnd` |
| ID-13 | scored and attempted populations counted separately | one population; voids and attempts conflated with scored rows | `TestCampaignAttemptAccountingAndITT` |
| ID-14 | the timing feedback loop: observation to record to ring to refit to next plan | R54 uploads a wall artifact that nothing downloads; ingest has no --wall-observations; there is no ring, no refit, and no path by which a measured bucket changes the next split; the reusable workflow and the record action carry no campaign-config content or expected digest, nothing materializes or verifies those bytes job-locally, neither the ingest command wiring nor core ingest binds a verified config to an observed campaign_id, and no interface test relates the workflow union, its per-job route, the action inputs and the scored-caller projection under their own domains | `TestWallObservationRoundTripClosesRecordLoop`, `TestCampaignRowsNeverRefitTheModel`, `TestRecordInputReachesIngestFlags` |
| ID-15 | integer-nanosecond model and single display-boundary division | no wall model of this shape exists | `TestWallModelIsIntegerNanosecondsEndToEnd` |
| ID-16 | scored cache-state declaration, recording, and per-pair comparison | no cache_state block; the pinned workflow restores its PNPM store with a prefix fallback and nothing records the matched key; no interface carries the declaration bytes across either job boundary, the caller passes only a pathname, and nothing binds the hashed MongoDB binary to the executed one | `TestScoredCacheModeEqualityOrPairInvalid`, `TestScoredCacheDeclarationTransportAndBinaryBinding` |
| ID-17 | three separated provenance identities with chronological ring recency and eviction | no ring at all; one head_sha would otherwise carry candidate, workload, and orchestration identity | `TestWallHistoryRecencyEvictionAndThreeIdentities` |
| ID-18 | calibration mode with a truthful bounded-negative outcome; L partial under a total proposer | no calibration mode | `TestCalibrationModeTerminatesSufficientOrNotFoundWithinBudget` |
| ID-19 | production-algorithm suffix-atom regression over the full pinned path universe | the retained generic exact_paths_test.go does not bind the pinned 1,512-path universe; no test runs assignFilterAtoms over it | `TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm` |
| ID-20 | sequential within-pair order verified by authenticated completion and start | no order verification derives from authenticated instants at all | `TestPairsRunSequentiallyByAuthenticatedCompletion` |
| ID-21 | consumer adoption SOURCE delta: separate orchestration/workload/candidate identities | no snapshots and no separate identity fields; the reusable workflow cannot run Mandel | `TestConsumerAdoptionIdentitiesAreSeparateAndRecorded`, `TestWorkloadBindingRecomputesFromPinnedCheckout` |
| ID-22 | schema-2 store state matrix, failure subtype carrier, and migration marker | the store has no wall object at all, so no status, no subtype carrier and no migration marker exist; store.go writes neither the always-present model-version and comparability-key-digest leaves nor a literal low-row or low-run subtype, the migration path stamps no status, and ingest applies no deterministic precedence when several insufficiency predicates hold at once; nothing carries a wall model version, so no reader refuses an absent or unknown one and no writer drops a fit group across a bump | `TestWallStoreSchemaStateMatrixAndMigration` |
| ID-23 | the executed runtime profile: a bucket-side digest bound to the plan's declared one | the comparability leaves are read from the plan job only; a bucket records its runner label and instance name and nothing else, so no digest is computed over the Node, pnpm, Vitest, testbucket binary, facade command, lock or cache mode it actually ran, and no check compares one against the plan | `TestBucketRuntimeProfileMatchesPlan` |
| ID-24 | scored cache symmetry: one declared configuration across both arms of a pair | the pinned workflow restores its PNPM store with a prefix fallback, nothing records the matched key, and no rule ties the two arms of a pair to one cache configuration or to equal per-index outcomes | `TestScoredCacheIsSymmetric` |

**Every acceptance test above targets its own hop.** The earlier revision mapped ID-6 to
profile-copy and key-reset tests, ID-7 to the allocation-objective test, ID-8 to the campaign-void
test, ID-10 to the façade-lifecycle test, ID-12 to campaign-accounting and response-selection tests,
and ID-13 to the scale-wording test — six rows pointing at tests of unrelated behaviour. Those
mappings are corrected above and are re-checked mechanically by test 41.

### 21.1 Gap — estimate semantics (ID-1, ID-2)

| | |
|---|---|
| **Label** | The displayed estimate of contract §5.1 is today a **reporter-work weight**, not an action wall-time forecast. |
| **Current source** | `internal/core/plan.go` sums store EWMA weights into the displayed estimate of contract §5.1 and keeps doing so when `AllocationScore` changes packing. `plan.go:400` calls it "measured wall-time"; `plan.go:448` calls it "serial wall time"; `README.md:76-77` calls the sum "the job's actual wall time"; `run-bucket/action.yml:19` calls it "the bucket's time estimate". There is no `est_basis` field. |
| **Target** | **ID-1**: contract §13.0, contract §16.1, contract §16.3 — **ID-2**: contract §5.1, contract §6.5, contract §16.3 |
| **Acceptance tests** | For the surfaces of contract §5.1: TestReporterBasisPreservesLegacyProjectionAndDeclaresBasis (test 33) and `TestEstSecondsMatchesItsBasis` (test 8) — asserting (1) the **canonical legacy projection** is byte-for-byte identical for an unopted consumer, on both the Vitest and the Go adapter; (2) **every rendered script byte** is unchanged for an unopted invocation, compared separately from the projection; (3) the additive fields are present and asserted **separately**, never as part of the projection; (4) in `wall` basis `est_seconds` equals `round1(a_eta_ns/1e9)` of the model's objective value; (5) no shipped string describes reporter-basis `est_seconds` as action or job wall time (§9.4). **No assertion claims the full matrix is byte-identical** — that would contradict PD-1's additive fields. Registry union adds `TestColdStartModeSelection`. |

### 21.2 Gap — B/C isolation (ID-3)

| | |
|---|---|
| **Label** | B and C are intended as **the same binary in two planner modes**; what the campaign actually compares is still a **whole-delivery comparison**, in which every non-mode field is held equal by construction and asserted, rather than proven impossible to differ. |
| **Current source** | There is no allocation-mode field. `Stage1Manifest.InvariantTuple` omits the candidate action/source/binary tuple and the instrumentation binary digests, and both frozen arms receive the same scorer path — so the observable treatment is an implementation tuple, not a mode toggle. |
| **Target** | **ID-3**: contract §19.2; field-registry `memberships.bc_inv` |
| **Acceptance test** | `TestArmsDifferOnlyByPlannerMode` (test 14 case set) — asserts (1) two plans over one store, one live set, and one binary, differing only in `--est-basis`, produce identical values at every wire path of the registry's cross-arm-invariant membership, enumerated by recursion rather than by a literal list, so the test contains no field count; (2) the basis field differs and takes only the two values contract §5.1 permits; (3) `expanded_unit_set_digest` is byte-identical across the two, so unit topology is not part of the treatment; (4) the campaign validator **rejects** a synthetic pair in which any one other invariant field differs, one case per **`BC-INV` scalar leaf**, which for cache state is the `bc_inv` cache subset and not `matched_key` or `disposition` (S-5/R9-D4), plus `candidate_sha` (S-6). **Fails on R54:** no `est_basis` exists, `expanded_unit_set_digest` is not computed, and the invariant tuple omits the fields (1) and (4) read. |

### 21.3 Gap — the timing feedback loop is open (ID-5, ID-14, ID-22)

| | |
|---|---|
| **Label** | The closed-loop update path does not exist. A measured bucket cannot influence any future plan. |
| **Current source** | `.github/workflows/bucketed-reusable.yml:1087` uploads `testbucket-wall-*`; the record job at `:1144` downloads only `testbucket-events-*` and the shard plan. `cmd/testbucket/main.go` has no `--wall-observations` flag; `internal/core/ingest.go` folds only `RunSummary.PackageSeconds`. There is no ring, no fit, and no consumer of a wall observation. |
| **Target** | **ID-5**: contract §15.1a — **ID-14**: contract §14.1, contract §14.2, contract §15.1b, contract §19.9c, contract §19.9c-1, contract §21, contract §6.8, contract §6.5 — **ID-22**: contract §15.1, contract §15.1c, contract §15.1d, contract §15.2, contract §6.8 |
| **How the five hops are wired** — derivation, not a target | |
| **Acceptance tests** | `TestWallHistoryFreezesPlanTimeFeatures`, `TestCampaignRowsNeverRefitTheModel`, `TestRecordInputReachesIngestFlags` and `TestWallStoreSchemaStateMatrixAndMigration` cover the ring, the campaign predicate, the record wiring and the store states. `TestWallObservationRoundTripClosesRecordLoop` (test 58) — the **23 → 24 → 25 boundary**, compared byte-for-byte against the in-tree oracle **`testdata/walltime/test58-feedback-oracle.json`** (R9-D1). At 23 rows: **rank 3**, `insufficient`, hard error, no coefficients. At 24: rank becomes 4, exactly one appended row with plan-time regressors frozen, status `ok`, and the **first literal model** equal to the fixture's `fit_at_24` = `[5e9, 1.0, 8e9, 3e9]` — a **cross-basis** transition, not an update. At 25: the model contract §6.4b refits equals `fit_at_25` = `[5e9, 37/53, 680/53 e9, 399/53 e9]`, three of four coefficients change, and the **actual bounded planner** moves from `[[w2],[w1],[s1,s2]]` to `[[s1],[s2],[w1,w2]]` — a composition change, not an index permutation. An empty store yields `insufficient`, never a refit. **Fails on R54:** hops 2–5 do not exist, so the observation is written and never read. Registry union adds `TestWallHistoryFreezesPlanTimeFeatures`. |

### 21.4 Gap — consumer adoption (ID-8, ID-19, ID-21)

| | |
|---|---|
| **Label** | Neither exact consumer runs this line. Mandel `d9ae1d43` pins testbucket **v0.2.2**; baml-rest `ff3012b1` pins action commit **`551d49ce`** and requests release tag **`v0.1.1`**. Both are pre-wall-time releases. Any statement that the product is exercised by a real consumer is **unproven** until the campaign has run. |
| **Current source** | No consumer snapshot exists in-tree; no manifest field distinguishes orchestration identity from workload identity; no test runs the production collision algorithm over the pinned path universe; the reusable workflow cannot run Mandel (`npm ci` against a pnpm project). |
| **Target** | **ID-8**: contract §20.6, contract §20.6a — **ID-21**: contract §19.3, contract §20.6b |
| **Out of this delta, deliberately** | The external orchestrator **existing** and having **completed a run** is **AG-2/AG-4 evidence**, not a source delta: no unit test can assert that a CI run happened. §14.6b owns it. This row is source-only — identities, fixture bytes, and checkout recomputation (R10-D8). |
| **Acceptance tests** | `TestConsumerAdoptionIdentitiesAreSeparateAndRecorded` — asserts (1) the snapshots exist and every recorded SHA-256 matches the snapshot bytes; (2) the snapshots still contain the constants this scope depends on (K=8, count=1, the exclusion prefix, the façade argv, the three project names, `-race -count=100`, the `551d49ce` and `v0.1.1` pins); (3) the campaign manifest schema admits only all five identity fields non-empty and rejects a manifest where `workload_commit` is absent or differs across arms; (4) **no VCS commit, push, tag, or tracked-source mutation targets the workload repository** — the orchestrator legitimately checks out, installs into, and runs tests in that tree — the orchestrator legitimately checks out, installs into, and runs tests inside that working tree, so "no code path writes to the workload repository" was overbroad and is replaced by the rule actually intended (D-12);  TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm (test 64) binds the collision property. **No acceptance test here asserts that an external run completed** — that is AG-2/AG-4. **Fails on R54:** no snapshots, no separate identity fields, and no production-algorithm regression. **Orchestrator existence and a completed run are deliberately not listed**: they are AG-2/AG-4 evidence and no unit test can assert them (R11-D10). Registry union adds `TestPinnedConsumerCheckoutMatchesManifest`, `TestPinnedConsumerStoredFixtureIntegrity`, `TestWorkloadBindingRecomputesFromPinnedCheckout`. |

---

## 22. Review criterion for the next adversarial visit

**Owner resolution contract §0.6.** A fresh reviewer must distinguish **current-source gaps** from
**specification defects**.

Grounds for rejecting this specification are exactly these:

- **missing** — a behaviour the contract states has no target and no acceptance test;
- **contradictory** — two requirements cannot both hold;
- **internally unsafe** — a stated rule would produce a wrong or unverifiable result if followed;
- **physically infeasible** — the target cannot be built or measured as described.

Do **not** reject it because R54's source does not yet implement it. The §21.0 registry declares
every known implementation gap explicitly — no count is quoted in prose (S-10) — and each row states
what the source does today, states the target, and names an acceptance test that fails on
current source and pass after implementation. A finding that restates one of those gaps is a
confirmation of §21, not a defect of this scope.

The standing items have changed status. The specification **is now in-tree** at `docs/walltime/`
and the consumer fixture **is now in-tree** at `testdata/consumers/` — both were the adjudicator's
primary blocker and both are closed in actual files. What remains open is the campaign itself
(§13.3, §14.6b): it has not run, and until gates AG-1…AG-4 hold the product is described as
**unadopted**. That is closed by a run, not by another revision of this document.

---

## 23. Adjudicator findings → the file that closes each

`scope-adjudication.md` returned `NEEDS_SCOPE_REVISION`, primarily because the governing artifacts
could not be read by a `jj`-only reviewer at all: they were not in the working copy. That single
condition produced most of the other findings, and it is fixed — every artifact below is now
tracked.

| # | Finding | Closed by, in actual files |
|---|---|---|
| — | Governing artifacts absent | `docs/walltime/{acceptance-contract,scope,assumption-ledger,salvage-map,source-to-claim-map}.md` + `component-map.json` |
| ADJ-T1 | Pinned consumer fixture as an acceptance input | `testdata/consumers/` with real bytes and digests; `SOURCE.md`; §14.6a; `TestPinnedConsumerCheckoutMatchesManifest` (test 39) |
| ADJ-C1 | Collision behaviour bound to the claimed workload | `SOURCE.md`'s production relation — **42 selected↔selected pairs, 0 cross-boundary** — over the full 1,512-path universe; §14.6a; `TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm` (test 64) |
| ADJ-P1 | Plan admission observable, not CLI constants | §13.3a `profile` block, admission checks **AD-1…AD-10**, contract §17.17, contract §17.20, `TestScoredPlanAdmission` (test 31) |
| ADJ-G1 | Go consumer safety contract + repin test | §14.3 surface table and `TestGoConsumerSurfaceUnchangedAcrossRepin`; RM-1…RM-4, RM-11; `TestReporterBasisPreservesLegacyProjectionAndDeclaresBasis` |
| ADJ-N1 | Bind features, cutoff, and which route touches topology | contract §6.4a (EWMA planner may; palloc scorer may not) and contract §6.4b (allowed features, authenticated cutoff, hold-out verification 1–4) |
| ADJ-M1 | Adoption gate before the workload claim | §14.6b gates AG-1…AG-4; the product is described as **unadopted** until all four hold |
| ADJ-D1 | Denominator contradiction | contract §10.4 freezes **80 total = 40 per arm** and 8 complete buckets 0–7; the contradicting `gates.go:568-569` comment is in §9.4, pinned by `TestCampaignDenominatorWording` (test 20) |
| ADJ-D2 | Predeclared B/C sequence verified against execution | contract §0.2 sequence, §10 authenticated-start ordering check, §13.5, contract §17.18, `TestPairsRunSequentiallyByAuthenticatedCompletion` (test 30) |
| ADJ-D3 | Relative-error gate alongside absolute | contract §10.4 — the absolute and relative calibration bounds are **conjunctive**, and every forecast, tail, cost, and population threshold is frozen in that one table. R13-A: the numeric values are stated there and are deliberately **not** restated here |
| ADJ-R1 | Source-to-claim map; false installer-only premise | `source-to-claim-map.md` — verified arithmetic for all three revision pairs, surface map, regression matrix RM-1…RM-12 |
| ADJ-Q1 | Component salvage map for the six named files | `salvage-map.md` — `exec/action/record` kept and simplified with reasons; `stage/campaign/ablation` removed, with the `stage.go` extraction ordering called out |
| ADJ-Q2 | Threat model recorded in the ledger | `assumption-ledger.md` A-39 — the four named exclusions entered as an owner decision |

**On R1 specifically.** The adjudicator's 17-file/+614/−25 figure is real but belongs to
`730cd78c → 8d27e9bc`, a two-commit span, not the `9537850e → 8d27e9bc` pair it names — that pair
is 3 files/+328/−5, confirmed here with `jj diff`. Both critiques still land: the candidate
delivered to a campaign is the **whole 184-file branch delta from v0.2.2**, and treating the rest
of the branch as invisible was the error. `source-to-claim-map.md` §1 shows all three pairs so the
mismatch cannot recur.
