# Pinned consumer fixture — provenance and acceptance input

Captured 2026-09-07 UTC with `jj` only, read-only, from clean working copies of the exact
consumer revisions. This directory is an **acceptance input** for the wall-time scope
(`docs/walltime/`), closing adjudicator findings T1, C1, and the fixture half of M1.

**This file is EVIDENCE, not an authority (R16-F1).** `acceptance-contract.md` §0.10 gives it the
`EVIDENCE` role: it records what was captured, from where, and with what digests, and every rule
about how those bytes are used — the collision property, the atom rule, the acceptance tests that
read them — is stated in the contract. Where this record and the contract differ, the contract
governs.

**The fixture contract (F8/S6) — direct entrypoint evidence, not an execution closure.**

An earlier version of this file called the snapshot a "full-file execution closure". That was
**false**: `scripts/run-unit-tests.ts` imports `offline_replset.ts` and `temporary_paths.ts` and
executes `replset_preflight.ts`; `.github/actions/setup-case-test-mongo/action.yml` executes
`replset_preflight.ts` and `prepare-case-mongod.ts`. None of those four files is here, so the
transitive preflight/setup graph is **not** made reviewable by this fixture and is no longer claimed
to be.

What this fixture is: the **direct entrypoints** the scope's constants are read from. What supplies
the complete closure for campaign purposes is the **workload commit identity** — §19.2a
recomputes every workload value from a checkout at `workload_commit`.

Two file classes are stored here. What each digest proves is a
fact about the capture; how the bytes are used is contract §20.6a's:

| Class | Stored here | What the digest proves |
|---|---|---|
| **Direct entrypoints, stored complete** — `package.json`, `vitest.config.ts`, `scripts/tb-vitest.ts`, `scripts/run-unit-tests.ts`, `scripts/cli-main-module.ts`, `setup-case-test-mongo/action.yml`, both workflows | complete bytes | the digest is over the **whole file**; a validator compares full checkout bytes to it |
| **Digest-only** — `pnpm-lock.yaml` (1.28 MB) | an **excerpt**, plus the full-file digest below | the *excerpt* proves only the constants quoted from it; the **full-file digest** is what execution is compared against |

No file here carries an "excerpt matches the full file" requirement — that is impossible and is not
claimed. Excerpts are read for constants; full-file digests, all derived from the pinned commit, are
what a checkout is compared against.

**What this is:** snapshots of external source at named commits, with per-file digests, so the
consumer claims in `acceptance-contract.md` §20 are checkable from this tree without a network.
**What this is not:** proof of the consumers' live state. The campaign manifest records the
consumer commit actually used, and `acceptance-contract.md` §19.2a re-derives every workload value
from a checkout at that commit rather than trusting these bytes.

## Pinned revisions

| Consumer | Repository | Commit | testbucket pin |
|---|---|---|---|
| Mandel (Vitest workload) | `mandel-ai/mandel` | `d9ae1d433bb45012c04d567879b66fc4bf6112c6` | actions + binary `v0.2.2` (= `693a19981fb6e0061d3fab62e59d75dc1c01ff3f`) |
| baml-rest (Go consumer) | `invakid404/baml-rest` | `ff3012b160aca29a920516ab7e03c772e948648a` | action source commit `551d49cea6a74a99706912fe18504f318bdba1cc`; installer requests release tag `v0.1.1` |

Neither revision runs the wall-time line. Both are **pre-wall-time** pins; see `scope.md` §21.4 (consumer adoption).

## File digests (SHA-256)

**Direct entrypoints — stored complete, digest over the whole file.** These are the files this
scope reads constants from. They are **not** a transitive closure.

| Path | Full-file SHA-256 |
|---|---|
| `mandel/package.json` | `b832872ff3775378d07fde267426cf1bdf2f989656f2a2c161824316ab4e20a9` |
| `mandel/vitest.config.ts` | `90266bab9baafc40017fa3b7ee8defe63f5414d56ca86f27b4606ba8b504c563` |
| `mandel/scripts/tb-vitest.ts` | `74ba69cbda080f91dfa7fff9336a4c79256ebe50655dd1b71a86b164bd863134` |
| `mandel/scripts/run-unit-tests.ts` | `ac91910d570ddb8e4aa1ce603c49998653f4b0532f7271ff48d8ed471816e587` |
| `mandel/scripts/cli-main-module.ts` | `627fca02f6ca488678601984c2d09559d8a3efffa996665fb8fd35b13385dde1` |
| `mandel/.github/actions/setup-case-test-mongo/action.yml` | `57f86ee0d2fe140a35819e6b8ce5d772761729088979d4e9663ae7f144373b8e` |
| `mandel/.github/workflows/unit-tests-bucketed.yaml` | `639d525c8227fe0ce99160333fc8aaf5fc9b4c4a3f08cc90a15c244db08db05b` |
| `baml-rest/.github/workflows/unit-tests-bucketed.yml` | `c9f930125a243b2ec26563ed5d0b7f66ae3f820863e95760f2665104155d50ef` |

`run-unit-tests.ts` and `cli-main-module.ts` are the façade's **direct** imports — `tb-vitest.ts`
imports `prepareOfflineUnitEnvV1`, `runOfflineUnitChildV1`, `unitTestLauncherDefaultsV1`, and
`PreflightFailedError` from the first and `isCliMainModuleV1` from the second, so a change in either
changes what the measured invocation actually does. `setup-case-test-mongo/action.yml` is the
provisioning composite the orchestrator reuses from the checkout (`acceptance-contract.md` §19.3).

**Digest-only — full file not vendored.**

| Path | Full-file SHA-256 at `d9ae1d43` | Stored here |
|---|---|---|
| `mandel/pnpm-lock.yaml` | `c9381f491ac8a5a3954b70f761834c7a69dbef890bde673d269a732952be611c` | `mandel/pnpm-lock.excerpt.yaml`, proving **only** the Vitest-family versions quoted in it |

**Every stored input carries a full digest (D-10).** An earlier revision abbreviated the excerpt
hash to `149a0e33…` and gave `tracked-test-paths.txt` no hash at all, while the wall-time scope of
that revision claimed the fixture had per-file digests. Both are recorded in full:

| Stored input | Full-file SHA-256 |
|---|---|
| `mandel/pnpm-lock.excerpt.yaml` | `149a0e3338a3c225c23f6eea38e7eecc872727348294e2533c3aebae713229ea` |
| `mandel/tracked-test-paths.txt` | `1119544350a94c56fd802c834c4dee6ff711c98c622d78288e55a202c76c85e6` |

`mandel/tracked-test-paths.txt` is the complete sorted list of tracked `*.test.ts` paths at
`d9ae1d43`, obtained with `jj file list -r d9ae1d43…`. It is the membership input below **and** the
input to the production collision regression.

## Two validations, deliberately separate (D-10)

Earlier acceptance language mixed two operations with different evidence and different failure
meanings. They are split:

| | **Offline fixture integrity** | **Pre-campaign checkout validation** |
|---|---|---|
| Runs | anywhere, no network | before `B_1`, against a checkout at `workload_commit` |
| Inputs | only the bytes stored in this directory | the pinned revision's real tree |
| Checks | every stored file's full digest matches the table above; membership counts and the production collision relation are re-derived **from `tracked-test-paths.txt`** | the **full** `pnpm-lock.yaml`, `package.json`, `vitest.config.ts`, façade, tree digest, façade argv, exclusion prefixes, and discovered path set match the values recomputed from that checkout (§19.2a) |
| Test | `TestPinnedConsumerStoredFixtureIntegrity`, `TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm` | `TestPinnedConsumerCheckoutMatchesManifest`, the campaign validator, `TestWorkloadBindingRecomputesFromPinnedCheckout` |
| Cannot check | the full `pnpm-lock.yaml`, which is not vendored — its recorded digest is a **checkout assertion**, not an offline result | nothing about the stored excerpt, which it does not read |

No offline assertion claims an excerpt equals a full file, and no checkout assertion is presented as
verifiable from stored bytes.

## Frozen profile constants (read from the fixture, not asserted)

From `mandel/.github/workflows/unit-tests-bucketed.yaml`:

```
TESTBUCKET_VERSION            = v0.2.2
TESTBUCKET_K                  = 8
TESTBUCKET_COUNT              = 1
TESTBUCKET_VITEST_COMMAND     = pnpm exec tsx scripts/tb-vitest.ts
TB_DISCOVERY_EXCLUDE_PREFIXES = shared/f/lib/cases/
MONGOMS_RUNTIME_DOWNLOAD      = false
```

From `mandel/vitest.config.ts`, three projects:

| Project | Include | In bucket universe? |
|---|---|---|
| `unit` | `**/*.test.ts` minus `**/integration-tests/**`, `packages/region-router/**`, and `shared/f/lib/cases/**/*.replset.test.ts` | yes |
| `case-replset` | `shared/f/lib/cases/**/*.replset.test.ts` | no — excluded prefix, misc lane |
| `harness-unit` | `integration-tests/lib/purchase-order-recon-extraction/__tests__/**/*.test.ts` | **yes** — pure, no infrastructure |

## Selected / excluded membership at `d9ae1d43`

Derived from `mandel/tracked-test-paths.txt` by applying the project rules above and the
`TB_DISCOVERY_EXCLUDE_PREFIXES` exclusion:

| Set | Count |
|---|---:|
| tracked `*.test.ts` | 1512 |
| excluded — `shared/f/lib/cases/**` (misc lane) | 48 |
| excluded — `integration-tests/**` outside `harness-unit` | 67 |
| excluded — `packages/region-router/**` | 1 |
| **bucket universe** | **1396** |
| of which `harness-unit` (under `integration-tests/`, legitimately selected) | 8 |

The 8 `harness-unit` files are why a path predicate of "nothing under `integration-tests/`" is
**wrong**; the predicate is project-based. See `acceptance-contract.md` §20.4.

## Collision regression fixture — two relations, and which one the regression binds

There are **two** collision relations here and they are not interchangeable. Earlier revisions of
this file presented the narrower one as if it were the safety property; it is not.

| # | Relation | Rule | selected↔selected pairs | selected → excluded-Case pairs |
|---|---|---|---:|---:|
| R1 | **root containment** — illustrative | Vitest matches a positional filter with `testFile.includes(filter)` against the **root-relative** path | **2** | **0** |
| R2 | **production** — the relation the contract binds (`acceptance-contract.md` §20.6a) | `internal/runner/vitestrunner/collide.go` `assignFilterAtoms`: containment checked in **both directions**, at the workspace root **and at every shared possible project-root suffix**, with conservative Unicode folding and union-find transitive closure over collisions and existing atoms | **42** | **0** |

Non-ASCII paths in the 1,512-path set: **0**, so conservative locale folding cannot hide a
cross-boundary pair at this revision.

**R1's two pairs are examples, not the property.** They are:

```
lib/attribution/resolveSupplierUserAttribution.test.ts
    ⊂ shared/f/lib/attribution/resolveSupplierUserAttribution.test.ts
lib/keto/organizations.test.ts
    ⊂ shared/f/lib/keto/organizations.test.ts
```

These are the keto/attribution pairs the v0.2.2 atom rule exists to co-schedule; a filter naming
either short path also selects its `shared/f/` twin, so both members land in one invocation under the co-scheduling rule acceptance-contract.md 20.6a states.

**The product safety claim depends on R2**, the broader shared-project-root suffix rule that
`assignFilterAtoms` actually computes. A regression that asserts only R1's two pairs would pass
while a break in project-root suffix enumeration invalidated the workload-specific claim. The
number that matters for safety is the same under both relations — **0 cross-boundary collisions**,
so no excluded Case file can be pulled into a bucket at this revision — but the number that would
*detect* a regression differs, and only R2 has it.

**Regression, over the full pinned universe.**
`TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm` (`acceptance-contract.md` §20.6a, test 64) feeds **all 1,512
tracked `*.test.ts` paths** from `mandel/tracked-test-paths.txt` — the 1,396 selected bucket paths,
the 48 excluded Case paths, and the 68 otherwise-excluded paths — through the **production
`assignFilterAtoms` implementation**, and asserts:

| # | Assertion |
|---|---|
| 1 | exactly **42** selected↔selected collision pairs under R2 |
| 2 | exactly **0** selected-filter → excluded-Case collision pairs under R2 |
| 3 | **transitive closure**: every R2 pair has both members in one union-find atom, and no atom contains a member with no collision path to the rest |
| 4 | **no atom is split** across an invocation or a bucket boundary in a K=8 plan over the selected set |
| 5 | every selected path renders into the argv as a `./x` path token |

These values pass at this snapshot. The regression is what was missing, not the algorithm.


## Acceptance tests

**Three symbols, because these are three jobs (R8-D8, R10-D9).** A single symbol was assigned both the offline
and the checkout job, which are deliberately disjoint — one reads only the bytes stored here, the
other reads a checkout and the non-vendored full `pnpm-lock.yaml`. They are split:

**`TestPinnedConsumerStoredFixtureIntegrity`** — **offline, no network, no checkout.** Digests every
**stored** input in this directory against the tables above (the eight complete entrypoints, the
lock **excerpt**, and `tracked-test-paths.txt`), and re-derives the six membership counts from
`tracked-test-paths.txt`. It makes **no claim about files it does not contain** —
`offline_replset.ts`, `temporary_paths.ts`, `replset_preflight.ts`, and `prepare-case-mongod.ts` are
outside it — and **never** reads the full `pnpm-lock.yaml`, whose recorded digest is a checkout
assertion.

**`TestPinnedConsumerCheckoutMatchesManifest`** — **checkout-bound, pre-campaign.** Against a
checkout at `d9ae1d43`, compares the full `package.json`, the **full `pnpm-lock.yaml`**,
`scripts/run-unit-tests.ts`, `scripts/cli-main-module.ts`,
`.github/actions/setup-case-test-mongo/action.yml`, `vitest.config.ts`, `scripts/tb-vitest.ts`, and
both workflows against the full-file digests recorded here. It is the fixture half of the contract §19.2a
recomputation and runs where a checkout exists.

How the two classes are compared is contract §20.6a's. This record notes only the fact that follows
from the capture: no assertion here claims an excerpt equals a full file.

**`TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm`** binds the collision property to the
production algorithm over the full 1,512-path universe, per the five assertions in the collision
section above. The three tests have disjoint jobs: **stored-byte integrity and membership counts** offline,
**checkout equality** against the pinned revision, and **production collision behaviour** over the
full path universe. None carries another's claim.
