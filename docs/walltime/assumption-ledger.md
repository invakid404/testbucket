# Assumption ledger — testbucket practical wall-time balancing (PWT-13)

Recorded: 2026-09-07 UTC · Revision 6, after the current `NEEDS_SCOPE_REVISION` from
`scope-adversary.md` on S-1…S-10 · Companion to `scope.md` and `acceptance-contract.md` · VCS: `jj`
only · All repository and consumer reads were read-only; no source, remote, or GitHub mutation was
performed.

**This ledger quotes no line count and no SHA-256 of any document in this set (S-10).** The rule
behind that, and the artifact inventory it applies to, are contract §0.10's;
`TestScopeArtifactConsistency` is the test that enforces it here.

**Status vocabulary — all six partitions, defined here.** Earlier revisions defined four while the
summary used six, so `OWNER-RESOLVED` and `HISTORICAL / SUPERSEDED` were asserted without a
definition. All six are defined below, and the summary's partitions are exactly these.

| Status | Meaning |
|---|---|
| `VERIFIED` | checked this round, with the command or path given |
| `INHERITED` | established by a named artifact, not re-checked; the re-check condition is stated |
| `UNVERIFIED` | asserted, not checked, with the consequence if wrong |
| `CORRECTED` | a prior entry this round found wrong, or a specification repair whose named acceptance test has not yet run (A-47's rule) |
| `OWNER-RESOLVED` | settled by an owner determination, not by review; a reviewer does not reopen it |
| `HISTORICAL / SUPERSEDED` | retained only as the record of a defect or a past verdict; **never current state**, and never cited as such |

**Promotion rule (adopted at A-47).** A repair entry is promoted from `CORRECTED` to `VERIFIED` only
when its named acceptance test passes against an implementation. Until then it states what was
changed, not that it works.

---

## A-01 — The frozen historical contract was read by digest, not by path · VERIFIED

Recovered from `validation-evidence-ba929e16-sol-r1.hFVb6j/acceptance-contract-numbered.txt` by
stripping the line-number prefix; hashes to
`2f23ef5cca314a805b5d49f674040445e8823f2f5cdf342591c2e00a2805c412`. Written only to this session's
scratchpad, never back to the canonical path. Historical design evidence, not the acceptance
criterion.

## A-02 — Baseline, target, and working-copy state · VERIFIED · UPDATED (S8)

`v0`, `v0.2.2`, and `master@origin` all resolve to `693a19981fb6e0061d3fab62e59d75dc1c01ff3f`. The
working copy's parent is exactly `8d27e9bccc537032615578748c80bed9300ec7bd`.

**Current `jj status` — enumerated from `jj`, never restated here (R10-D9).** Earlier revisions wrote a literal count that went stale the moment a path was added; the inventory is read from `jj status` / `jj file list -r @` by `TestScopeArtifactConsistency`. The groups are: Earlier revisions of this entry said the sole
working-copy change was `scope-adversary.md`; that is stale. The working copy now adds:

| Group | Paths |
|---|---:|
| `docs/walltime/` — contract, scope, ledger, salvage map, source-to-claim map, salvage audit, component map | 7 |
| `testdata/consumers/` — SOURCE.md; Mandel `vitest.config.ts`, `package.json`, `scripts/tb-vitest.ts`, `scripts/run-unit-tests.ts`, `scripts/cli-main-module.ts`, `setup-case-test-mongo/action.yml`, workflow, lock excerpt, tracked-path list; baml-rest workflow | 11 |
| `testdata/walltime/` — `test58-feedback-oracle.json`, the published feedback oracle (R11-D11) | 1 |
| review inputs — `scope-adversary.md`, `scope-adjudication.md` | 2 |
| **total** | *(derived from `jj`, not written here)* |

The two review inputs were authored by the reviewers; this round did not create, edit, or delete
either. No commit has been made — these are working-copy additions visible to `jj status` and
`jj file list -r @`.

## A-03 — R54 delta size, both ways · VERIFIED

From baseline: 184 paths (162 `A`, 22 `M`), +53,295 / −972, matching `component-map.json`. From
parent `9537850eed19`: 3 files, +328/−5 — the archive-enumeration fix under pipefail/SIGPIPE.
Distribution hardening with no measurement content (`scope.md` §12.6).

## A-04 — R54's test suite state · INHERITED from three sources, WITHDRAWN by a fourth · CORRECTED

PWT-4 recorded that "all three adversarial reports" report 1,419 tests in seven packages. That is
now wrong for the current report: the fourth review's Verification section states **"No
execution-test result is claimed here. This was a source, history, ledger, and exact-consumer audit
constrained to Jujutsu-addressed evidence."**

Standing evidence is therefore `salvage-audit.md` plus the second and third reports — three
independent records of 1,419/7 at this tree — and one review that deliberately declines to claim
it. Not re-run this round either; a read-only specification round adds no code and the tree is
unchanged (A-02).

**Consequence.** The suite's green state is not something a reviewer will keep re-establishing. It
is established by implementation: `go test -race -count=1 ./...` on Linux in the OrbStack VM
`tbtest`, not a container, **before** the first deletion in `scope.md` §12.3 and again after step 6
(`scope.md` test 25).

## A-05 — Exact Mandel: projects, façade routing, static audit · VERIFIED · CORRECTED twice

Clean at `d9ae1d433bb45012c04d567879b66fc4bf6112c6`. Three projects: `unit` (excluding
`**/integration-tests/**`, `packages/region-router/**`, and the case-replset glob), `case-replset`
(`shared/f/lib/cases/**/*.replset.test.ts`, serial, real replica set), `harness-unit`
(`integration-tests/lib/purchase-order-recon-extraction/__tests__/**/*.test.ts` — 8 files, verified
by listing). The façade routes `list --filesOnly --json` → `globTestSpecifications()` with no
test-code import then prefix-exclusion; `list <./file> --json` → forwarded verbatim; anything else
→ the offline-sealed `run` path.

Corrections: (1) against the 2026-09-06 ledger, exact Mandel source *is* available; (2) against
PWT-1, the unit-only predicate is project-based — "no path under `integration-tests/`" is
wrong because `harness-unit` lives there.

**Static audit, INHERITED** from the independent review and adopted in contract §20.4: of 1,512 tracked
`.test.ts` paths, 1,444 in the root-config union — 1,396 bucket files, 48 excluded Case files, 42
in-bucket collision pairs, **zero** planned-filter-to-excluded-Case collisions; all ASCII. Not
recomputed here; contract §20.4(d) makes the campaign re-derive it, and the contract's workload fallback re-derives it — contract §19.3 states that, and this row records it; the fallback needs
re-deriving it at any new workload commit.

## A-06 — baml-rest has no ordinary source path from this branch · VERIFIED · CORRECTED

Clean at `ff3012b160aca29a920516ab7e03c772e948648a`; sole unit gate at K=6, `count=100`, race,
serial.

**Correction.** PWT-3 through PWT-5 called this "an immutable v0.1.1 pin" and made it "the primary
guarantee". Verified this round, the two halves differ in strength:

| Half | Mechanism | Strength |
|---|---|---|
| action source | `uses: …@551d49cea6a74a99706912fe18504f318bdba1cc` | commit SHA — content-immutable |
| binary | `version: v0.1.1` → `TB_VERSION` → `install-testbucket.sh` | a release-**tag** request resolved at run time |

At `551d49ce` the `version` input is documented as "the moving v1 alias, an exact vX.Y.Z, or
`local`", so the binary half resolves a name, not a digest.

**Residual, stated rather than papered over:** a moved `v0.1.1` release tag would weaken the
isolation. That is a deliberate owner release action, visible in the release history, and outside
anything this work does — but it is not impossible, so the claim is "no ordinary source path", not
"immutable". Verified for the upgrade caveat: `--file-parallelism` appears
0 times in `cmd/testbucket/main.go` at `551d49ce` and 3 times at `693a1998`, so a future upgrade
crosses a surface absent at its pin and needs its own compatibility run.

## A-07 — Live GitHub repository state · INHERITED · not load-bearing

Zero environments, variables, secrets, self-hosted runners, rulesets; unprotected `master`. Not
re-verified. PWT-5 uses none of them.

## A-08 — Actions artifact and log retention · UNVERIFIED

Assumed: 90-day artifact retention permitted, raw job logs retrievable for ≥14 days. **If wrong:**
the window shortens and the operator downloads sooner. Explicitly not a permanence claim.

## A-09 — Same-run artifact download needs no `actions: read` · VERIFIED by existing behaviour

R54's `record` job declares only `contents: read` and downloads same-run artifacts; §6.1 adds one
more same-run download. **If wrong:** the job gains `actions: read`, a caller-visible permission
change and therefore a minor-release migration with a consumer note.

## A-10 — The Vitest renderer's invocation topology · VERIFIED

`renderBucket` (`render.go:49-114`) merges every whole-file unit into exactly one invocation and
gives each name slice its own, so `invocation_count = (1 if any whole-file unit else 0) +
slice_count`.

## A-11 — Matrix rounding and store schema · VERIFIED

`report.go:35` — `round1` is half-up for non-negative values. `store.go:20` — `storeSchema = 1`.
`ParseStore` returns an unknown schema as a *reason*, so cold-start-never-reinterpret already
exists.

## A-12 — The learning loop is genuinely open at R54 · VERIFIED

The record job downloads only events and the shard plan (`bucketed-reusable.yml:1144`); the wall
artifact is uploaded (`:1087`) and never downloaded; `--wall-dir` defaults empty (`main.go:342`);
the dogfood Vitest lane sets none (`bucketed.yml:86-99`).

## A-13 — The reusable workflow cannot run Mandel · VERIFIED

`npm ci` at `bucketed-reusable.yml:518,857,1142`; Mandel has no `package-lock.json`. Its
`actions/checkout` uses carry no `repository`/`ref`, which in a reusable workflow default to the
caller's repository.

## A-14 — `$/` from an external caller · UNVERIFIED · open task

Never exercised from another repository; needs runner agent ≥ 2.336.0. **Task** (`scope.md` §12.4): call
it at a full SHA from a scratch second repository and retain the raw job log. **If wrong:** only
the reusable workflow needs a different form; no consumer and no campaign path uses it.

## A-15 — The reporter EWMA is an adequate per-unit regressor · UNVERIFIED · the central bet · CORRECTED

Measurable before the campaign from `residual_mae_ns` / `residual_p90_ns`. contract §0.8's `degraded` status
makes a bad fit refuse to plan.

**Correction (current review).** Earlier revisions of this entry said that if the bet is wrong,
"either the fit degrades toward `scale ≈ 1` — still a better ETA — or the ceiling is exceeded, wall
basis refuses, and the product ships as a better estimate with no allocation change." **Neither
branch follows, and the promised better-ETA outcome is removed.**

- `scale ≈ 1` is not evidence of accuracy. In the repaired integer-nanosecond model (S-1) `scale` is
  dimensionless, so `scale ≈ 1` means only that the reporter sum and `A` happen to be numerically
  close on the fitted corpus. It says nothing about residual size, and a large `residual_mae_ns` can
  coexist with it.
- A refused wall model ships **no new estimate at all**. Under contract §0.8 outcome (c) explicit wall basis
  is a hard error with no matrix, and the default reporter basis emits exactly the v0.2.2
  reporter-work estimate. The fallback is the legacy estimate, not a better one.

**What is actually true if the bet is wrong:** the model is `degraded` or `insufficient`, wall basis
refuses, allocation is unchanged, and the product ships **no wall-time forecast**. That is a safe
outcome and a real cost, and it is stated as both. See [[a-20-scale-is-association-only]] in this
ledger's A-20, which carries the companion correction.

## A-16 — Per-unit physical attribution is deliberately not attempted · VERIFIED as a design choice

No code copies a multi-unit `V` onto one unit, divides it, or accepts a multi-unit selected-work
claim as a single-unit label. `scope.md` §7.6 costs the alternative at ~175 extra process startups per bucket.

## A-17 — Both campaign arms are the same bytes, asserted · VERIFIED as a design property

contract §19.2's canonical `BC-INV` list now includes the instrumentation binary,
`expanded_unit_set_digest`, and both identity pairs, asserted per pair; a violating pair is
unscored. **The entry count is stated only in contract §19.2's declaration** and is deliberately not
repeated here.

## A-18 — Owned-process-group completion and drain, not containment · VERIFIED as a stated limitation

`contain_pgroup.go` bounds and signals the owned tree; it cannot prove a `setsid`/double-fork
descendant did not escape. **Refined this round (adversary Q2):** the cgroup's *mechanical
descendant-drain* role is preserved by the process-group wait, reap, and drain of `scope.md` §4.3 — only
hostile containment as an eligibility gate is dropped. Contract §12 names the consumer measurement
that would restore it.

## A-19 — No security or permanence claim is made anywhere · VERIFIED by construction

Contract §9 and contract §18.3 enumerate the forbidden claims, now including "a seed inside a digest proves
a draw" and "the wall model selects unit topology". Threat model stated as non-hostile; re-entry is
by named measurement evidence (DEC-17).

## A-20 — Vitest 4.1.10's reporter interval semantics are not frozen · UNVERIFIED · CORRECTED

`vitestrunner/ingest.go:52-97` uses `(endTime − startTime)/1000`; whether that covers import,
transform, and setup is not established here.

**Correction (current review).** This entry previously said "**`scale` is that measurement**" — that
`scale` measures whether the reporter covers import, transform, and setup. It does not, and that
contradicted `acceptance-contract.md` §6.4, §0.9, and §6.8, which correctly describe
`scale` as a **predictive association coefficient with no causal or component-fraction meaning**.

`scale` is the fitted association between `reporter_sum_ns` and the observed `A` over the corpus. It
does not decompose `A` into reporter-covered and reporter-uncovered parts, does not identify what
fraction of the interval import or transform accounts for, and cannot distinguish two same-shaped
files with different import graphs. Import, transform, environment setup, and hooks occur **inside**
the measured target, which makes their **aggregate** effect observable; it does not make them
separately attributable.

**What remains unverified and how it is caught:** whether the reporter's shortfall is roughly
proportional to reporter weight. If it is not, the four-parameter model's residuals grow, the
`MODEL_MAE_CEILING` marks the model `degraded`, and wall basis refuses — the outcome A-15 now states
correctly, with no better-ETA branch.

## A-21 — The collision matcher's relationship to upstream Vitest · VERIFIED as a narrowed claim

Claim narrowed to **conservative over-grouping**; upstream byte-identity is not claimed and is not
needed for the containment property.

## A-22 — The specification is not jj-addressable · **SUPERSEDED by A-41** · historical

**Status: superseded. Do not cite this entry for current state.** It is retained only as the record
of a defect that stood across five adversarial rounds and one adjudication.

*Historical content.* Four consecutive reviewers reported that no tracked file contained the brief,
the contract, the scope, or a salvage map, because those documents lived only in `/tmp`. Each round
recorded the gap and deferred it as an owner action to land later.

*Why it is closed.* A-41 records the closure: the artifacts are in the working copy at
`docs/walltime/` and `testdata/consumers/`. The current-state summary at the end of this ledger
reflects A-41, not this entry, and no section of the scope or contract repeats the "not in-tree"
conclusion.

## A-23 — Consumer evidence was out-of-tree · **SUPERSEDED by A-41** · historical

**Status: superseded. Do not cite for current state.** Retained only as the record of the gap.

*Historical content.* A-05 and A-06 rested on working copies under `/tmp`, not addressable from the
testbucket repository, so the consumer claims could not be checked from the tree.

*Why it is closed.* A-41 records the closure: `testdata/consumers/` is in the working copy with
per-file digests, and F8/SR-8 narrowed its claim to **direct entrypoint evidence**. The
current-state summary reflects A-41, not this entry.

## A-24 — Five pairs is an engineering release gate · OWNER-RESOLVED · supersedes the prior framing

**Owner resolution (contract §0.3, §0.4) replaces the prior framing.** The campaign is an
**engineering release gate** over exactly the eighty rows measured — not a significance test, and
making no claim beyond the sample. PWT-3 through PWT-6 disclaimed significance while still arguing
in inferential terms: a null, a one-sided probability, a power limitation, a variance-triggered
escalation. All of it is removed. contract §19.6 now carries only the engineering motivation for 0.95, and
the pilot validates the harness and changes nothing, because thresholds are frozen before run 1 and
cannot be retuned from pilot or campaign outcomes.

## A-25 — Seven user-facing strings mislabel the product · VERIFIED · EXTENDED

Five carried from PWT-3 (`README.md:76-77`, `README.md:443-444`, `run-bucket/action.yml:19`,
`plan.go:400`, `plan.go:448`), plus two verified this round in A-34. All listed with repairs in
contract §16.4 and pinned by tests 13 and 20.

## A-26 — Work sits inside the same step at both ends of `A` · VERIFIED

`action.go:176-177` constructs `NewSystemClock()` before `clock.Now()`, and construction reads
`/proc/sys/kernel/random/boot_id` (`clock.go:70,109-112`) — so a file read, the binary's process
startup, and the step shell's dispatch precede `A_start`. `action.go:903-920` puts containment
destroy before the closing read (inside `A`) but the closing record write, `Writer.Close()`,
`sealDirectory`, process exit, and shell tail after it. Hence **instrumented run-bucket interval**.

## A-27 — Two things sit outside `V` and inside `VB` · VERIFIED · EXTENDED

`render.go:236-242` emits `printf '%s' <spec> > …/spec-<b>-<n>.json && testbucket wall exec …`, so
the spec write precedes the wrapper. **Added this round (adversary T2):** the `wall exec` process
startup, CLI dispatch, flag parsing, and clock-object construction also precede `Exec`'s first
reading. Hence `V` is the **Exec-envelope interval** — neither a pure Vitest timer nor a whole
wrapper-process lifetime. Both are accounted in `script_overhead_ns`.

## A-28 — Neither exact consumer uses the wall-time line · VERIFIED · the honest gap

Mandel pins v0.2.2, baml-rest pins v0.1.1. "The existing consumers are unchanged" passes; "the
product is exercised by an exact consumer" fails, and PWT-2 did not say so. Contract §10 refuses
the deployed-product claim until the campaign has run; baml-rest is exempt because the work is
Vitest-only.

## A-29 — Campaign dates are authenticated · VERIFIED as a requirement the contract states

R54's `CampaignSchedule.Validate` does not parse `YYYY-MM-DD` and `bindOrder` compares only against
optional unsigned index fields, so omitting them bypasses the check. contract §19.5 and contract §18.2 take every
date from the Actions API, and the contract voids the campaign on a non-matching scheduled date.

## A-30 — Order is fixed, precommitted, and counterbalanced · OWNER-RESOLVED

`schedule.go` includes `Seed` in the order digest but no code derives the order from it.

**Owner resolution (contract §0.2) closes it:** there is **no draw**. The order is a fixed
precommitted **counterbalanced** sequence — pairs 1, 3, 5 run `B→C`, pairs 2, 4 run `C→B` —
declared before run 1. Counterbalancing controls for ordering and drift within a pair without
randomizing. The seed/shuffle option PWT-5 and PWT-6 left open is removed from every document, and
test 18 forbids *randomized*, *seed*, *draw*, and *shuffle* in any campaign-order context.

## A-31 — The wall model does not choose the units · VERIFIED · NEW · a narrowed product claim

`internal/core/plan.go:179` calls `expandUnits(opt.Live, st, …)` **with the timing store**, and
`AllocationScore` is applied only at `:193-194`. `expandUnits` reads `st.Units[p.ID]` weights
(`internal/core/units.go:84-85`) to decide whale status, split policy, count-shard width, and which
runnable names share a slice.

**The three deciding fields, named this round:** `UnitStat.Seconds` against `total/K` decides whale
status; `UnitStat.SplitInto` via `clampShards` decides how many shards or slices
(`units.go:145,179`); and `UnitStat.Tests`, the per-runnable EWMA, decides which runnable names
share a slice (`sliceByName` at `units.go:211`, reading `row.Tests[n]` at `:222` and `:242`).

**Consequence.** `est_basis` selects the **partition weight**, not the work items. Unit topology is
reporter-derived and identical in both bases. PWT-3's contract §6.5 pseudocode packed "units" without saying
where they came from, which left the impression that wall basis governs more than it does.

**Second correction, this round.** PWT-4 stated the fact but framed it defensively — "historical
timing is a pre-treatment covariate, not leakage". The fourth review is right that this is still
the wrong shape: the contract must **affirmatively allow** pre-treatment reporter data rather than
imply a clean allocation surface. Contract §2 and §6.2 and `scope.md` §7.0 now say plainly that the
allocation surface is *not* outcome-free, that only current-run and post-assignment outcome is
forbidden, and that the phrasings "outcome-free allocation", "no outcome-derived influence", and
"mechanically pre-campaign" are false and appear nowhere (DEC-20, test 24).

**Repairs:** `scope.md` §7.0 states it; contract §2 and §6 state it; every observation carries it in
`limitations`; `expanded_unit_set_digest` joins the observation (§5), QC12, and the pair invariant
(contract §19.2) so a topology difference cannot hide inside the treatment; test 10 pins topology
invariance; DEC-18 records the narrowed claim; wall-derived topology is explicitly out of scope
(`scope.md` §7.6, §17).

**Accepted consequence.** The reachable improvement is bounded by the topology the reporter store
produces: a file whose reporter weight understates its wall cost badly enough that it should have
been split and was not cannot be fixed by re-weighting. That bound is stated rather than hidden.

## A-32 — Cutoff and exclusion domains are non-optional · VERIFIED as a requirement the contract states · NEW

In R54, label campaign/candidate/run/holdout identities are optional and rejected only when
explicitly named in exclusion lists, and the self-declared cutoff need not precede the
scheduled or authenticated campaign start. A pre-treatment guarantee that can be satisfied by
omitting a field is not a guarantee.

**Repair (§7.1, contract §17.15, DEC-19), as corrected by S-6 and D-13:** three conditions contract §6.4b states, each
failing the campaign when absent — `fitted_at` and the store's `updated_at` strictly precede the
first **authenticated** start; the fitting population contains **zero rows whose `run_id` is in the
campaign harness universe and zero rows inside the authenticated campaign window**; and every
exclusion domain is **named** in the manifest rather than merely absent from a list.
**Corrected:** this entry previously demanded "zero at the candidate SHA or orchestration commit".
That is wrong — comparability pins `testbucket_sha256`, so the warm corpus is *necessarily* produced
by the candidate binary, and excluding it would make warming unreachable. Exclusion is by campaign
run id and window; `candidate_sha` and `workload_commit` are recorded separately (contract §15.1a, contract §15.1b). `TestCampaignProvenanceAndCutoff` (`scope.md` test 15) covers all three.

## A-33 — Orchestration identity is separate from workload identity · VERIFIED as a requirement the contract states · NEW

The frozen profile pins Mandel at `d9ae1d43`, but `d9ae1d43`'s own workflow pins testbucket
v0.2.2. Any in-repository adoption creates a new Mandel commit and therefore a different workload
identity — so PWT-3's "add a new dispatch workflow in Mandel" quietly broke the freeze it depended
on.

**Repair (contract §19.3, DEC-6):** an **external digest-bound orchestrator** checks out Mandel at exactly
`d9ae1d43` into `./workload`, builds testbucket from the candidate SHA, reuses Mandel's own
`setup-case-test-mongo` composite from that checkout, and drives the composite actions with
`working-directory: workload`. **Mandel is not modified.** `orchestration_repo` /
`orchestration_commit` and `workload_repo` / `workload_commit` are separate non-optional fields
(contract §17.15), equal across arms (contract §19.2), asserted by test 16.

**What the analysis found the orchestrator needs, and where the rule now lives.** Read access to
the Mandel repository was the only access this design turned out to need — the
protected environments, signing keys and approval ceremonies of earlier drafts bought nothing the
claim needed. Contract §19.3 carries the requirement, and contract §19.2a carries the fallback for a
newer Mandel revision; this entry records only how the question was settled.

## A-34 — Two source comments assert things the code does not do · VERIFIED · NEW

- `internal/walltime/gates.go:568-569` — "exactly five pairs, ten eligible runs, **eighty complete
  action observations per arm**" — which would mean 160. The constant and every gate mean 80
  combined, 40 per arm.
- `internal/walltime/schedule.go:41` — "so the draw is **reproducible by a third party** rather
  than asserted" — but no code derives or verifies the listed order from the seed.

Both files are deleted by `scope.md` §12.3, so the repair is not an edit but a constraint on the replacement:
contract §16.4 lists them, tests 20 and 18 assert the corrected wording and behaviour. They are recorded
because a deleted file's wrong sentence tends to be re-typed into its successor, and because a
reviewer reading the tree today will find them and be misled.

## A-35 — The ledger is sufficient for evaluation, not for unit labels · VERIFIED as a scoped claim · NEW

The fourth review's new *Ledger sufficiency* section splits the question the previous rounds had
left joined, and both halves are adopted:

- **Sufficient at invocation and instrumented-action granularity.** Records digest-bind argv, cwd,
  selector, unit membership, atom membership, the process group **as the testbucket process
  observed it** (not a containment guarantee), terminal state, and both
  lifecycle boundaries — enough to recompute `V` and `A`, trace an observation back to its records,
  and reject incomplete or mismatched rows. This is the granularity every campaign gate uses.
- **Not sufficient at unit granularity.** One whole-file invocation may cover many planned units
  while its stream carries one aggregate `V`. No signature over a supplied claim creates the
  missing decomposition; the reviewer also notes `SelectedWorkDocument` may list multiple units
  while verification checks only non-emptiness plus a separate `UnitID` match, never singleton
  equality.

**Repair (§18.0, contract §6.3, DEC-21, test 23):** the product uses the ledger only for
invocation- and action-level evaluation, fits at bucket level, and takes the per-unit signal from
the reporter EWMA. Contract §6.3 holds any surviving selected-work structure to singleton equality with its
unit id. The single-unit-invocation alternative stays costed and deferred (`scope.md` §7.6).

## A-36 — Ordinary provenance is sufficient for this claim · VERIFIED as an affirmation · NEW

The same section states that for a non-hostile paired comparison, **exact content digests, terminal
coverage, and retained CI artifacts are sufficient provenance**, and that additional principals,
immutable storage, or more observers strengthen tamper checks without making an aggregate
observation more granular.

This is an independent affirmation of the evidence design in §18 and contract §9, and it
is recorded because it also closes a tempting wrong move: the unit-label gap of A-35 cannot be
fixed by adding signers or observers, so none is adopted as a remedy for it. Adopting any of them
remains a separate product decision tied to an explicit hostile-runner threat model (contract §12),
with the measurement-based re-entry rule of DEC-17.

## A-37 — Five owner resolutions are recorded and are not review findings · OWNER INPUT · NEW

`recovery_gate` supplied five determinations, recorded in force at `acceptance-contract.md` §0:

| # | Resolution | Where applied |
|---|---|---|
| 1 | Trusted/non-hostile CI; hostile-runner containment and multi-signer machinery stay out unless **a measured timing-bias result from an actual hostile environment** reopens them | contract §0.1, §12; `scope.md` §4.3, `scope.md` §12.3 |
| 2 | Fixed precommitted **counterbalanced** B/C order; randomization removed; sole intentional difference is planner mode, binary and action bytes identical | contract §0.2, §10; §19.5, DEC-16, test 18 |
| 3 | Campaign is an **engineering release gate**, not a significance test, no generalization claim | contract §0.3, §10; §19.6, DEC-15, test 26 |
| 4 | Thresholds frozen before run 1, never retuned from pilot or campaign outcomes; calibration gated absolutely **and** relatively; all exact values specified | contract §0.4, §10.4; §19.7, DEC-22, DEC-23, test 27 |
| 5 | Every R54 implementation gap labelled as a gap, each with an acceptance test that contract §0.5 requires to fail on current source; the set is enumerated by the `scope.md` §21.0 registry and **no count is quoted in prose** (S-10, D-13) | contract §0.5; `scope.md` §21, DEC-24, test 28 |

A sixth instruction governs the next review rather than the product: reject only for a missing,
contradictory, internally unsafe, or physically infeasible specification target or test — not
because R54 does not yet implement it (`scope.md` §22).

**Status.** These are owner decisions. Where they conflict with a position an earlier revision
reached through adversarial iteration — A-24's inferential framing and A-30's open seed option —
the owner resolution governs, and the earlier position is marked superseded above rather than
quietly rewritten.

## A-38 — Cross-arm equality cannot identify the intended workload · CORRECTED · NEW

The report gained a 35th finding (`M4`) mid-round, and it is correct. `RequireFrozenProfile` checks
only repository and commit **strings**; `SourceProfileReceipt` recomputes digests from whatever
bytes the receipt carries; `PlanningInputBundle` records whatever acquisition/discovery argv and
discovery bytes were supplied. Nothing compares them against the pinned revision's blobs. In the
reviewer's words: cross-arm equality can prove both arms used the same substituted partition, not
that it was the intended unit-only partition.

**My prior revisions had the same hole.** contract §19.2's invariant tuple compares workload fields *between
arms*, and contract §20.4's steps (a)–(d) check project membership, the excluded prefix, cross-arm digest
equality, and counts — every one of which a self-consistent substituted partition satisfies.

**Repair (contract §19.2a, contract §20.4(e), contract §17.16, DEC-25, test 29):** every workload-derived value is
**recomputed from the checkout at `workload_commit`** — façade, `vitest.config.ts`, lockfile,
`package.json`, a sorted tree digest over the tracked `*.test.ts` set plus those four files, the
façade argv and exclusion prefixes read from the pinned revision's own workflow, and the discovered
path set produced by running the pinned façade inside that checkout — and compared against the
manifest. Any mismatch voids the campaign under contract §19.2a. `TestWorkloadBindingRecomputesFromPinnedCheckout` (`scope.md` test 29) covers exactly the case the finding names: both
arms agreeing with each other and both disagreeing with the pinned revision.

This is where contract §19.3's external orchestrator pays off a second time: because it checks the workload
out at the full SHA instead of trusting supplied bytes, the recomputation has something real to
compare against.

## A-39 — Threat model: trusted / non-hostile CI · OWNER DECISION, entered here · NEW (Q2)

The adjudicator was right that this had only ever been *asserted in the adversary report*, never
entered as an owner-approved assumption in a ledger. It is entered here.

**The owner chooses a trusted / non-hostile CI threat model.** The runner, the test process, and
the artifact channel are trusted not to forge results.

**Explicitly out of scope**, each named because the adjudicator asked for them by name:

| Out of scope | Meaning | Consequence accepted |
|---|---|---|
| hostile tests / hostile runners | a test or runner deliberately manipulating its own measurement | a determined workload could bias its own `V`; the product would not detect it |
| detached-child security guarantees | proving a `setsid`/double-forked descendant did not escape | the promise is completion and **drain** of the owned process group, nothing stronger |
| artifact tampering | proving a stored observation or log was not altered after upload | provenance is a content digest computed at download time, not tamper-evidence |
| multi-principal provenance | independent signers, key rosters, countersignature, protected-environment authority | one producer; identity is the run/job/commit GitHub reports |

**Why this is defensible for this product.** Every consumer of this measurement is the repository's
own CI, run by the owner, on GitHub-hosted runners, to decide whether one bucket split is faster
than another. The failure being guarded against is a *wrong split*, not an *adversary*. Under that
model, content digests, terminal coverage, and retained CI artifacts are sufficient provenance
(A-36), and the removed machinery buys nothing the claim needs.

**Why a single measurable re-entry condition was chosen (owner §0.1).** Every rejected item was
rejected for the same reason — no measurement in hand justified its cost — so one condition covers
them all, and it has to be a measurement rather than an argument. Contract §12 states the condition
and what qualifies under it. What this entry adds is the reasoning: a hypothetical attacker or a
preference for more signatures is not evidence, and treating it as evidence is how the earlier
drafts acquired machinery nothing needed.

**Status:** owner decision, not a reviewer's assertion. It governs `salvage-map.md`'s removals and
contract §12, and it is the reason those removals are not gaps.

## A-40 — The R54 diff figure was quoted, not derived · CORRECTED · NEW (R1)

PWT-3 through PWT-7 recorded "R54's delta over its parent is 3 files, +328/−5" by **repeating the
adversary's number**. It happens to be right — `jj diff --from 9537850e --to 8d27e9bc` gives
3/328/5 — but it was not computed here, and quoting an unverified figure through five revisions is
the same failure mode as the claims those revisions corrected.

Derived this round, all three pairs:

| From | To | Files | + | − |
|---|---|---:|---:|---:|
| `693a1998` v0.2.2 | `8d27e9bc` R54 | 184 | 53,295 | 972 |
| `9537850e` parent | `8d27e9bc` R54 | 3 | 328 | 5 |
| `730cd78c` two back | `8d27e9bc` R54 | 17 | 614 | 25 |

The adjudicator's 17/614/25 is the **third** pair, not the second — it named `9537850e` but
measured `730cd78c`, a two-commit span. Its substantive point stands regardless: the candidate a
campaign delivers is the whole 184-file branch delta, so treating the rest of the branch as
invisible was wrong. `source-to-claim-map.md` records all three pairs and maps every surface.

## A-41 — The governing artifacts were not in the tree, and that was the real blocker · CORRECTED · NEW

Five adversarial rounds and one adjudication reported the same thing: no tracked file contains the
brief, the contract, the scope, or a salvage map. I recorded it each time (A-22) as an owner action
to land later and kept writing to `/tmp`. The adjudicator made the consequence concrete: it read
`jj file list -r @`, found none of the four governing artifacts, and declared the scope **not
admissible** — correctly, because a specification a reviewer cannot read cannot be judged.

**Closed this round.** `docs/walltime/` now holds the contract, scope, ledger, salvage map,
source-to-claim map, and component map; `testdata/consumers/` holds the pinned fixture. These are
working-copy files visible to `jj status` and `jj file list -r @`. Copies remain at
`/tmp/testbucket-practical-walltime/` because the directive names that path; the tree copies are
the ones a reviewer reads.

**Lesson recorded rather than repeated:** "declared, targeted, and tested here" is not closure when
the document making the declaration is invisible to the tool the reviewer uses.

## A-42 — A past re-review returned SCOPE_PASS · HISTORICAL · superseded as current status

**Status: historical. Do not cite this entry for current state.** It records the outcome of one
earlier adversarial visit, not the standing verdict. Two later visits — the SR-1…SR-10 set and the
current S-1…S-10 set — both returned `NEEDS_SCOPE_REVISION`, so a prior `SCOPE_PASS` is not evidence
about the artifacts as they now stand. The current status is [[a-48-s1-s10-tail-repair]].

*Historical content.* After the adjudicator findings were closed in actual files, the adversarial
stage was repeated. That report's overall decision was **`SCOPE_PASS`**: "no remaining specification
target that is missing, contradictory, internally unsafe, or physically impossible."

Sixteen findings pass. Two do not, and neither asks for a scope change:

- **L2 — `EXTERNAL_BLOCKED`.** No calibration/holdout corpus and no adopted campaign exist in
  `jj`-addressable evidence. This is the campaign; it is closed by running it (contract §20.6b AG-1…AG-4),
  not by another document.
- **Q2 — `PRODUCT_DECISION`.** The threat model. Decided by the owner and entered at A-39; the
  reviewer's own note agrees that no measurement in the reviewed evidence makes the simpler
  contract insufficient.

The reviewer's eight-item acceptance ledger is explicitly "implementation/evidence gates, not
requests to revise scope", and each maps to a specified, tested section (§20.1). Gates 1–7 are
implementation; gate 8 is the campaign.

**What this changes in the ledger's own terms:** the long-standing A-22 entry — that the
specification was not `jj`-addressable — is closed by A-41, and with it the structural reason five
consecutive reviews returned `NEEDS_SCOPE_REVISION` regardless of content.

## A-43 — Owner decisions PD-1, PD-2, PD-3 · OWNER DECISION · NEW

Three determinations, frozen and applied. They are owner decisions, not review findings, and a
reviewer does not reopen them.

| # | Decision | Applied |
|---|---|---|
| PD-1 | **Additive compatibility.** Every v0.2.2 legacy field name, value, ordering, and script byte preserved; `est_basis`, `A_eta` display metadata, and `expanded_unit_set_digest` are additive. A canonical v0.2.2-field projection is byte-identical for an unopted consumer. | contract §0.7; scope test 33; superseded rows marked in `salvage-audit.md` and `component-map.json` |
| PD-2 | **Cold start, four distinct cases.** Default/omitted and explicit-reporter both cold-plan; explicit `wall` with no usable model is a **hard error with no matrix**; scored admission is warm-only. | contract §0.8; test 34 |
| PD-3 | **Nonlinear partition objective.** Marginal bucket cost carries both `I(any whole-file) × whole_invocation_overhead` and `per_slice × slice_count`; the optimized objective equals displayed `A_eta` on whole-only, slice-only, and mixed topologies. | contract §0.9, contract §6.5; test 35 |

**PD-3 corrected a real error, not a wording choice.** The prior additive form treated the
whole-file invocation overhead as a constant every bucket pays, which is false for a slice-only
bucket. Because it was assumed constant it was dropped from the weight, so the optimized and
displayed numbers diverged exactly when topology was mixed. The worked K=2 counterexample in
contract §0.9 shows additive packing choosing a 250 s makespan where 200 s was available, while displaying
150. The objective is now one expression, evaluated once, used for both.

**PD-1 supersedes earlier salvage guidance.** `salvage-audit.md` and `component-map.json`
recommended exposing `wall_est_seconds` as a replacement for `est_seconds`. That is now marked
superseded in both files with an explicit status line; the replacement approach is not adopted.

## A-44 — Specification defects S4–S8 closed · CORRECTED · NEW

| # | Defect | Closure |
|---|---|---|
| S4 | no canonical profile / ring row; ledger claims exceeded the schema | one `profile` type (contract §13.0) copied verbatim into every observation (QC13); bounded ring row with plan-time freezing (contract §15.1a); contract §18.0 narrowed to a field-by-field table, and `cwd_digest` + `process_group_id` **added** so the claim matches what is stored rather than dropping it. `scope.md` tests 36–39 |
| S5 | B/C invariant restated with drifting counts | named canonical list `BC-INV`. **Corrected by D-13:** the normative definition is `scope.md` §2A's `memberships.bc_inv`; contract §19.2 carries a *generated reading table*, and no size is stated in either. Referenced by name everywhere else; mutation cases **generated** by walking the schema recursively over every scalar leaf; `plan_digest` and bucket composition classified as derived outputs, excluded by construction. §22 |
| S6 | fixture mixed excerpts and full-file claims | one stated two-class contract; superseded in visit 12 by F8, which renames it **direct entrypoint evidence** — the "execution closure" claim was false because the preflight/setup transitive graph is not in the fixture. §22 |
| S7 | "no retry, no replacement" collided with "re-drawn" | replaced by "no retry or replacement **after either arm starts a bucket script**"; the pre-treatment operation is **rescheduling the same precommitted pair**, never a re-draw. §22 |
| S8 | stale ledger status | A-02 updated to the **derived** added-path inventory; A-22 explicitly **superseded** by A-41 and marked historical; §19 and the current-state summary reconciled. §22. **R13-D6:** this row previously said "the actual 20 added paths" while `jj status` reported **21** — 7 `docs/walltime`, 11 `testdata/consumers`, 1 `testdata/walltime`, 2 reviewer inputs; the dropped group was `testdata/walltime/`. No count is written in this ledger or in `component-map.json`; both derive it from `jj` at read time |

**On S4 specifically.** The honest options were to narrow the claim or to store the fields. Both
`cwd_digest` and `process_group_id` are cheap and genuinely useful for tracing an observation back
to what ran, so they were added. What was *not* added is a broader "containment identity" claim:
the product owns a process group and says exactly that (`scope.md` §4.3).

## A-45 — Repair pass F1–F9 · CORRECTED · NEW

Nine specification defects, repaired as specification only; PD-1, PD-2, and PD-3 were not reopened.

| # | Defect | Repair |
|---|---|---|
| F1 | PD-3 split across two incompatible models | *(units later corrected by S-1: the column is `reporter_sum_ns` and `scale` is dimensionless — see [[a-48-s1-s10-tail-repair]])* one four-parameter model `[1, reporter_sum, I(any_whole_file), slice_count]` with `[fixed_ns, scale, whole_invocation_overhead_ns, per_slice_overhead_ns]`, used identically in fitting, refinement, display, and validation. `per_invocation_ns` and `invocation_count`-as-regressor removed from contract §6.8, contract §15.1, contract §15.3, test 7, DEC-3, and both machine-readable maps. §22 |
| F2 | row count treated as identifiability; `scale` given causal meaning | new contract §6.6: **superseded by F4/S-7** — rank sufficiency is now ONE criterion, `rank(X) == 4` for every wall plan, not a per-topology rule; the silent-zero rule for collinear columns is **removed**. Predictors, response, and diagnostic spans separated by name; `scale` described as a predictive coefficient, with the file-specific non-identifiability disclaimer kept. §22 |
| F3 | PD-2 had two escape hatches | the generic "no store forces reporter" row and `--allow-degraded-model` are **deleted**; contract §0.8 is the four cases. **Superseded by F3:** they are *ordered normative outcomes*, not an exhaustive mutually-exclusive table — (d) deliberately overlaps (a)/(b) and wins. §22 |
| F4 | residual full-matrix byte test contradicted PD-1 | `scope.md` §21.1 now says **canonical legacy projection byte-for-byte**, with additive fields asserted separately and script bytes compared separately. §22 |
| F5 | `BC-INV` canonical in one section only | **Superseded by D-13:** the normative definition is `scope.md` §2A `memberships.bc_inv`; contract §19.2 is a generated reading table and **no size is stated anywhere**. Cases are generated by recursing over scalar leaves, so the tuple entry expands automatically. §22 |
| F6 | wall-history key bound no execution profile | replaced by `comparability_key_digest` over **plan-job-sourced** leaves (the leaf list is normative in `scope.md` §2A `memberships.comparability_key`; **no count is quoted** — S-4 replaced `runner_name` with `runner_class`). **No image digest is needed** — under trusted CI `runner_image_label` is the observable identity, and no producer for a cryptographic digest exists. **S-4 added the producer it did lack:** a required `runs-on-label` action input. `lock_sha256` is inside the key; `K` is outside it on the PD-3 rationale. `TestWallComparabilityProfileResetsHistory` |
| F7 | infeasible descendant-reap promise | root wait/reap **only**, plus same-PGID signal with bounded escalation and empty-group drain. Every "reap descendants" claim removed; the setsid/double-fork and non-Linux limits stated. §22 |
| F8 | scored and attempted runs counted as one | two populations defined; date/window gates use **scored** starts; pre-start voids excluded from denominators but included in audit; post-start failures stay in ITT; every Actions-API run appears in the attempted population. §22 |
| F9 | inventory, schema, salvage, ledger disagreed | path count reconciled to **20** here, in A-02, and in `scope.md` §1; the "sole change" claim corrected; `SOURCE.md` located at `testdata/consumers/`; contract §20.6 lists the two façade helpers, setup composite, and tracked-path list; PD-3 supersession propagated to `salvage-audit.md` and `component-map.json`. §22 |

**F2 is the one worth flagging.** The prior rule dropped a collinear column, set it to zero, and
kept planning. For a corpus where every bucket has a whole-file unit, that turns "we cannot tell
fixed action cost from whole-file invocation cost" into a confident zero, and then uses it to score
exactly the slice-only bucket that depends on the distinction. Refusing is the only safe behaviour,
and PD-3's admission of slice-only and mixed topologies is what makes it material.

## A-46 — Repair pass F1–F9 (visit 12) · CORRECTED · NEW

Nine further specification defects, repaired as specification only. PD-1, PD-2, PD-3 and the
trusted-CI threat model were not reopened.

| # | Defect | Repair |
|---|---|---|
| F1 | `A_eta` had two incompatible regression targets | the sole response is the observation's **top-level `elapsed_ns` = `A`**. `invocations[].elapsed_ns` is `V[j]`; `script_ns` is `VB`; none is a response. `setup_ns`, `script_ns`, `wrapper_ns`, `script_overhead_ns`, `invocation_count`, `whole_file_count` moved to diagnostic/audit. The four PD-3 columns are exhaustive. §22 |
| F2 | causal `scale` language; unclassified exposed estimates | the "how much the reporter accounts for" phrasing is **deleted** from contract §0.9, leaving one statement: a **predictive association coefficient**, no causal fraction. contract §5.1 classifies every plan/matrix/summary/log duration under both bases, and states plainly that **no wall-basis per-unit estimate exists** — the `I()` indicator has no unique per-unit decomposition, so unit display stays reporter-labelled. §22 |
| F3 | PD-2 table neither exhaustive nor mutually exclusive | rewritten as **two ordered phases** — mode selection, then a higher-priority scored-admission veto — with four **normative outcomes**, (d) deliberately overlapping (a)/(b) and winning. Both words removed. §22 |
| F4 | topology-specific rank rules did not imply rank | replaced by **one executable criterion**: `rank([1, reporter_sum, I, slice_count]) == 4` for **every** wall plan, by rank-revealing QR with a fixed tolerance. Conservative on purpose; it also covers **every bucket shape the refinement evaluator may score**, which a seed-partition rule would not. §22 |
| F5 | observation/QC/ledger schemas were not one record | `est_basis` has one canonical home, **`profile.est_basis`**; QC13 is in no optional list; §5.1 defines `cwd_digest` (executed absolute cwd, hashed, plan-time vs execution-time recording, compared by QC7a) and `process_group_id` (well-formed PGID **as the testbucket process observed it**). "Containment identity" is gone from contract §18.0 and A-35 |
| F6 | BC-INV canonical only in one place | every numeric restatement removed; the count is **derived from the list**, never quoted. Cases are generated by recursing over scalar leaves, and the test asserts the set is non-empty and covers every leaf. `runner_image_label` settled here and in F7 |
| F7 | execution profile named an unavailable identity | `runner_image_digest` **has no producer** — fleet attestation was removed by settled decision. Replaced by `runner_image_label` across all documents, with the limitation stated honestly: a mutable label is a **proxy for environment change**, not image identity. `lock_sha256` moved **into** the key with its warming cost accepted. §22 |
| F8 | fixture was not the closure it claimed | renamed **direct entrypoint evidence**. The old claim was false: `run-unit-tests.ts` imports `offline_replset.ts`/`temporary_paths.ts` and executes `replset_preflight.ts`; the setup composite executes `replset_preflight.ts`/`prepare-case-mongod.ts` — none in the fixture. Test renamed `TestPinnedConsumerEntrypointDigestsMatch` (itself later **split** into `TestPinnedConsumerStoredFixtureIntegrity` and `TestPinnedConsumerCheckoutMatchesManifest` — R8-D8/R11-D11), validating only files present; the **workload commit identity** supplies the real closure (contract §19.2a) |
| F9 | artifacts failed their own consistency test | header and §20 now cite the **current** report; `scope.md` §21.0's ID rows are ordered — **the count claim itself is withdrawn by S-10/D-13; counts are derived from the registry, never written**; §21 duplication repaired (21.1/21.2 appeared four times); `component-map.json` renames `working_copy_clean` → `r54_working_copy_clean` with `historical_r54_state`; PD-3 supersession extended to dependency-order entries |

**F9 found a real corruption, not just stale prose.** `scope.md` §21.1 and `scope.md` §21.2 were duplicated four times and
`scope.md` §21.0 sat after them — an artifact of an earlier in-place edit. The section was rebuilt: register
first, then 21.1–21.3 once each, ID rows in order. That is exactly the class of defect
`TestScopeArtifactConsistency` exists to catch.

## A-47 — Canonical schema table and SR-1…SR-10 repair · CORRECTED · claim narrowed

**Correction (current review).** This entry declared the SR-1…SR-10 repairs **verified**. The
current review shows several of them were not satisfied by the artifacts as written: the canonical
table did not generate the schemas it claimed (S-3), the comparability key still carried a plan-job
runner instance name (S-4), the ring's recency and exclusion identities were undefined (S-6), and
the register the SR-8 row called fixed was internally inconsistent (S-10). The repairs below are
therefore recorded as **attempted and partly superseded**, not verified; each SR row's current
status is the one in `scope.md` §20.1, and the repairs that actually close those defects are in
[[a-48-s1-s10-tail-repair]].

**Rule adopted from this (S-10).** A ledger entry records a repair as `VERIFIED` only where its
named acceptance test passes. Until the implementation exists, repair entries are `CORRECTED` —
specification-level statements of what was changed — and the artifacts' own consistency test is the
thing that promotes them.

*Content of the SR pass, retained.*

The structural change is `scope.md` **§2A**: one machine-readable table assigning **every** field
exactly one role — `REGRESSOR`, `RESPONSE`, `ADMISSION`, `DIAGNOSTIC`, `COMPARABILITY_KEY`,
`OUTPUT_METADATA`, `BC_INV`. Seven derived artifacts now read from it instead of restating field
lists: the model equation, the history ring row, the plan/observation schema, the BC-INV set, the
comparability key, the mutation tests, and `TestScopeArtifactConsistency`. Restating a field list
was the mechanism behind most of the drift these visits kept finding.

| # | Defect | Repair |
|---|---|---|
| SR-1 | Stage 2 could not reach the K=2 witness | The arithmetic was checked and the reviewer is right: from the mixed seed at 250, moving a whole unit gives 100/300 and moving a slice gives 150/350 — **every single move raises the maximum**. A deterministic whole↔slice **pair swap** (neighborhood 2b) is now part of the algorithm of contract §6.5, entered when single moves stall; it reaches 200. Determinism is contract §6.5's, and this entry records only that the reviewer's arithmetic was checked and found right |
| SR-2 | history row omitted fields the algorithm consumes | `head_sha`, `run_attempt`, `bucket_index` added; `plan_digest` is the single canonical plan identity (`plan_id` removed everywhere); sorting and exclusion operate on the **decoded stored row**, so a re-fit on an archived corpus reaches the original verdict |
| SR-3 | profile key not constructible at plan time | every leaf sourced from the **plan job**; the eight matrix runners do not exist then and are not inputs. A plan/test label divergence is a **reported misconfiguration**, not silent tolerance |
| SR-4 | rank admission had no fixed tolerance | frozen: `tol = sigma_max(X) · 4 · 2.220446049250313e-16`, pivot rejected on strict `<` |
| SR-5 | estimate schema contradicted the model | `est_basis` has **two fields with two roles** — `profile.est_basis` (ADMISSION, source of truth) and top-level `est_basis` (OUTPUT_METADATA, additive display). Contract §5's "additive per-unit score" is replaced: `A_eta` is the bucket-level nonlinear objective; Stage-1 seeds are a linearized search device, never an estimate |
| SR-6 | diagnostics mislabelled as model inputs | §7.1 rewritten as a role taxonomy. `whole_file_count` is **DIAGNOSTIC**; column 3 is the indicator `I_any_whole_file = (whole_file_count > 0)` — computed differently at fit time and plan time |
| SR-7 | BC-INV test hard-coded a count | cases generated from `scope.md` §2A `role: BC_INV` rows, recursive over `tuple_leaves`; no literal count in any document or in the test source |
| SR-8 | artifacts failed their own consistency test | all eight items fixed literally, including a genuine defect this exposed: **A-24 and A-30 sat in both VERIFIED and OWNER-RESOLVED**, and A-22/A-23 in more than one set. The summary is now six **disjoint, exhaustive** partitions over A-01…A-47 |
| SR-9 | action clock described as one-process | `V[j]` is one process lifetime; **`A` spans two CLI invocations sharing a boot identity**, with `wall end` verifying the fingerprint against the persisted start. A boot mismatch is a hard error, and endpoints are never caller-supplied |
| SR-10 | no specified route to a warm profile | contract §17.3 gives a bounded pre-campaign calibration protocol: W-1…W-4, requiring at least one `I_any_whole_file = 0` bucket. The campaign cannot bootstrap itself because its rows are excluded from training, so the route is stated rather than assumed |

**Machine-readable supersession.** Superseded recommendations in `salvage-audit.md` and
`component-map.json` now carry a `# STATUS: SUPERSEDED BY <decision>` marker that
`TestScopeArtifactConsistency` recognises and excludes from normative scanning — replacing prose
that a scanner could not distinguish from live guidance.

## A-48 — Specification tail repair S-1…S-10 · CORRECTED · NEW

The current `scope-adversary.md` returns `NEEDS_SCOPE_REVISION` on ten specification defects. All
ten are repaired as specification in this round; none reopens PD-1, PD-2, PD-3, the trusted-CI
threat model, or the five owner resolutions, and no previously passed boundary is weakened.

| # | Defect | Repair, in actual files | Acceptance test | Later correction (R8) |
|---|---|---|---|---|
| S-1 | seconds predictor fitted against a nanosecond response; the planner combined the fitted scale as dimensionless, so a literal implementation is wrong by 1e9 | `acceptance-contract.md` §0.9 fixes one unit system — `reporter_sum_ns` in integer nanoseconds, `scale` dimensionless, every coefficient and the response in nanoseconds — and divides the **complete** `A_eta_ns` by 1e9 once, at display. Contract §0.9, §5, §6.4, §6.5, §6.8, and §15.1a, the worked K=2 example, and `component-map.json` all carry the same choice; `a_eta_ns` is serialized beside `est_seconds` | `TestWallModelIsIntegerNanosecondsEndToEnd` | — |
| S-2 | contract §0.4 said coefficients are fitted from the campaign C rows, contradicting the cutoff, the frozen digest, and hold-out validation; and "none derived at run time" rejected its own relative gates | contract §0.4 rewritten: coefficients fitted **only** from pre-campaign rows excluded by contract §15.1b; C rows compute residuals, MAE, worst error, and `mean(A)` and never refit. Formula and percentage precommitted, numerical bound evaluated from observed C rows — stated as distinct from retuning in contract §0.4, §10.4 and §19.7 | `TestCampaignRowsNeverRefitTheModel`; `TestThresholdsAreFrozenBeforeRunOne` | — |
| S-3 | the 85-row table claimed to govern artifact membership and generate schemas, and did neither | `scope.md` §2A rebuilt as three dimensions — **wire paths** (artifact, type, cardinality, covering every serialized path including the thirteen the observation example carried with no row), **roles** (one primary role each), **memberships** (by wire-path reference). Aliases deleted, `profile.store_sha256` distinguished, the loose top-level `runner_token` removed from §5, generation narrowed to four lists each byte-compared to its owning section | `TestFieldRegistryCoversEverySerializedPath` | — |
| S-4 | the key carried a plan-job runner **instance** name and a label with no producer | §15.3: `runner_class` — an explicit stable operator-chosen string, a non-optional `plan` action input — replaces `runner_name`; `runs-on-label` becomes a non-optional input to `plan` **and** `run-bucket`, echoed as `observed_runs_on_label` and compared by QC12; the instance name survives as diagnostic `actual_runner_name`; W-3 has three real runs reproduce one key | `TestExecutionProfileIsPlanTimeAvailableAndLabelBound`; `TestWallComparabilityProfileResetsHistory` | — |
| S-5 | timing-store bytes pinned, runtime cache state unbound; the pinned workflow restores its PNPM store with a prefix fallback | new `scope.md` §8.4 defines a scored `cache_state` block (mode, primary key, matched key, disposition, transform-cache mode, executed Mongo binary digest), forbids prefix-fallback matches (QC14), makes the **four** cross-arm-invariant leaves `BC-INV` members compared **index-wise** per pair (contract §17.19) — `matched_key` and `disposition` are per-row by design, since eight bucket jobs restore independently, and **R8-D4 corrected the earlier "all six" wording and the blanket `matched_key != primary_key` rejection that made a legal `exact-key` miss unrepresentable**, and names page-cache warmth as a limitation. Contract §0.2 and §10 restate the B/C claim as *sole intentional and configured difference* | `TestScoredCacheModeEqualityOrPairInvalid` | — |
| S-6 | "most recent W" was defined by a lexical solver sort; one `head_sha` carried three identities | §15.1a splits `head_sha` / `candidate_sha` / `workload_commit` and adds authenticated `run_started_at` plus per-store `ingest_seq`; new contract §15.1b defines the recency key and eviction at `W` **before** the summation sort of `acceptance-contract.md` §6.8, and explains why a blanket candidate-SHA exclusion would make warming unreachable. Campaign exclusion itself is not this row's repair: it is the `campaign_id` stamping of `acceptance-contract.md` §6.4b and §15.1b, which materializes `trainable: false` at append | `TestWallHistoryRecencyEvictionAndThreeIdentities`; QC15 | **R8-D3 then added `ring.repository` and `ring.job_id`**, without which the row-intrinsic key was not reconstructible from a stored row, and roled `ingest_seq` `DIAGNOSTIC` to match the prose. |
| S-7 | four heuristic layouts could not prove `INFEASIBLE`; `N` undefined outside 4; the round-trip test omitted the corpus its own gates read | §17.3a renames the bounded outcome `NOT_FOUND_WITHIN_BUDGET`, reserves `STRUCTURALLY_INFEASIBLE` for a real proof, and makes the boundary exact: the layout function **`L` is PARTIAL** — for some `(U, K, i)` no layout exists — while the **PROPOSER is TOTAL**, returning exactly one of the three outcomes for every legal `N ≥ 1` and recording `generator_exhausted` when `L` is undefined. **R13-D3:** this row previously said contract §17.3a "defines a **total** layout generator", a strictly stronger and different claim that contract §17.3a itself contradicts and that A-54's D4 row had already declared repaired everywhere; contract §17.3b separates the W-1…W-4 execution protocol; test 58 is seeded with 23 rows over ≥3 runs plus a named boundary fixture before the 24th | `TestCalibrationModeTerminatesSufficientOrNotFoundWithinBudget`; `TestWallObservationRoundTripClosesRecordLoop` | **R8-D1 then found the slot rule ignored residual units and the published fixture ranks were 2 and 3, not 3 and 4**; the rule reserves a residual slot and the boundary is republished at `N = 3` / `N = 4` with exactly-verified ranks. |
| S-8 | the collision claim rested on two root-containment examples while safety depends on the production suffix rule | §20.6a and `testdata/consumers/SOURCE.md` separate the illustrative relation (2 pairs) from the production relation (42 pairs) and have the regression run production `assignFilterAtoms` over all 1,512 pinned paths, asserting 42 pairs, 0 Case crossings, transitive closure, no split atom, `./x` tokens | `TestPinnedConsumerSuffixAtomsMatchProductionAlgorithm` | — |
| S-9 | pilot said "8 runs" and claimed to prove 80-row bookkeeping; order was start-only | §19.6 corrects the pilot to 1 pair × 2 arms × 8 buckets = 16 rows and restricts it to plumbing; contract §19.5 and contract §0.2/§10 define launch vs sequential order and require authenticated `completed_at(first) ≤ started_at(second)`; contract §19.6 scopes attribution to the inputs actually held equal | `TestPairsRunSequentiallyByAuthenticatedCompletion`; `TestCampaignDenominatorWording` | — |
| S-10 | the delta register said three and thirteen while carrying four gap sections and fourteen out-of-order IDs, six of them mapped to unrelated tests; mangled test references; stale self-referential counts | `scope.md` §21.0 is **one ordered machine-readable registry** generating the prose table, the counts, and the gap sections; `scope.md` §21.1–`scope.md` §21.4 appear once each in order; the six wrong mappings are corrected; every `the "…" test` fragment is restored to a real symbol across `scope.md` and this ledger; and **no document in this set quotes its own or another's line count or SHA-256** | `TestScopeArtifactConsistency`; `TestImplementationGapTestsFailAgainstR54` | **R8-D8 withdrew the generation claim for the prose table and gap bodies** — no renderer exists, so those are hand-authored projections whose ID set, order and test symbols the consistency test compares. |

**Two entries in this ledger were themselves defective and are corrected**, not by this table but in
place: [[a-15-reporter-ewma-adequacy]]'s promised "better ETA" branch is removed because a refused
wall model leaves the legacy reporter estimate, and A-20's "`scale` **is** that measurement" is
replaced by the association-only statement the contract already carried. A-42's `SCOPE_PASS` is
moved to historical status, and A-47's repair claims are narrowed from verified to attempted.

### A-48.1 — Second verification pass: executability gaps the first pass left open

The S-1…S-10 repairs were re-audited against each finding's literal closure text rather than against
the summary of them. Six defects were found and closed. Five are the same class: **a rule was stated
normatively but the artifact that would let a component obey it did not exist**, which makes the
repair unexecutable even though the prose is correct.

| # | Gap the first pass left | Closure |
|---|---|---|
| 1 | **Ring recency was keyed on an instant the record job cannot obtain.** contract §15.1b keyed recency on the *authenticated* `run_started_at`, but contract §18.2 states that authenticated Actions API instants are retrieved by the **operator, outside the workflow**, precisely so no scored workflow needs `actions: read`. Ingest runs *inside* the record job, so the rule was not implementable by the component that applies it. | contract §15.1a/contract §15.1b split the two: **`observed_start_realtime`** (the observation's own `realtime_start` — self-reported, run-intrinsic, needs no credential) is the recency stamp, and the tie key is the **row-intrinsic** `(repository, run_id, run_attempt, job_id, bucket_index, plan_digest)`, with **`ring.repository`** and **`ring.job_id`** added by R8-D3 so the key is reconstructible from the stored row; **`run_started_at`** stays the authenticated instant, **optional in-CI**, operator-annotated, and is what every campaign date, cutoff, and window gate uses. **Exclusion by `run_id` and by the authenticated window are BOTH operator-side, after dispatch; neither runs in CI (R13-D1).** Both belong to campaign validation there. The single in-CI selection predicate is `trainable == true`, materialized at append by contract §15.1b's class-A domains. The clock-skew limitation is stated, and every comparison is made total by the row-intrinsic tie key — **not** by `ingest_seq`, which no comparison reads. **This closure text replaces, rather than follows, the earlier wording** that said "campaign exclusion by `run_id` is applied in CI" and credited `ingest_seq` with totality: both were live contradictions of the singular mechanism, and the withdrawal note two paragraphs below did not erase them from this row. |
| 2 | **S-4's "pass the label explicitly" had no input to pass it through.** §15 and `component-map.json` listed no `runner-class` or `runs-on-label` on `plan`, and no `runs-on-label` on `run-bucket`. | §15 gains a table of added inputs with the reason each exists; `component-map.json` `action_interfaces` gains them; AD-8 and contract §17.20 make them fail-closed for a scored plan. |
| 3 | **S-5's cache mode was defined but the orchestrator still said "provision the way Mandel does."** That is the exact instruction the finding calls unfrozen — the pinned workflow's own PNPM restore carries a `restore-keys:` prefix fallback. | contract §19.3 step 3 is rewritten to declare the mode, forbid a `restore-keys:` list under `exact-key`, and point the transform cache at a fresh per-run directory. **R13-D2 replaces the rest of this closure text**, which still said "the six leaves" and named a **`cache-state-file`** input that no longer exists: the block is **seven** leaves — four plan-time declaration leaves carried in the **`cache-declaration-file`** (AD-9) plus three per-job outcome leaves — and the declaration crosses the job boundary as bytes plus a verified digest, not as a pathname. Transport, restore producers, the declaration/outcome record split, and the executed-binary binding are normative in `acceptance-contract.md` §10.5. |
| 4 | **QC15 named three identities the runner cannot know.** `${{ github.sha }}` supplies only the orchestration head; nothing on the runner knows the testbucket build commit or the consumer checkout. | `run-bucket` gains **`candidate-sha`** and **`workload-commit`** inputs, which contract §21 makes non-optional for a scored run (AD-10). |
| 5 | **`BC-INV` carried a duplicated identity.** contract §19.2's reading table listed both `testbucket_commit_sha` (row 5) and `candidate_sha` (row 29) — the same fact twice — and `testbucket_commit_sha` was in the table but not in the normative `scope.md` §2A registry. | The reading table is now **generated from `memberships.bc_inv`**, so it cannot diverge; `testbucket_commit_sha` is removed everywhere, `candidate_sha` being the registry's name for that identity. Verified equal in both directions. |
| 6 | **Gap counts survived in four places.** DEC-24, §19, §22, and contract §11 still said "three R54 implementation gaps" or listed a stale artifact set, which S-10 forbids. | All four now derive from the `scope.md` §21.0 registry or from `jj` enumeration; contract §11's artifact list is completed with `salvage-map.md` and `source-to-claim-map.md`. |

**The in-CI leakage rule, stated once and only once (R12-D1).** The **sole** in-CI mechanism is
`campaign_id → trainable:false`: `run-bucket` stamps the frozen config's opaque `campaign_id` onto
every pilot and campaign observation; `ingest` **appends every qualifying row** and sets
`trainable:false` on any row whose `campaign_id` matches the supplied config; a row whose
`campaign_id` is **missing or mismatched under a supplied config is refused** (fail-closed); a row
from a run with no config at all is `trainable:true`. The fitter reads **only** `trainable:true`.

`run_id` and the authenticated window are **operator-side / manual-invalidation checks only** — the
campaign validator applies them after dispatch, because those inputs do not exist when the config is
frozen. Earlier wording in this entry made `run_id` an in-CI predicate, spoke of rejecting campaign
rows **before append**, and credited `ingest_seq` with breaking recency ties. All three are
**withdrawn**: nothing is refused before append except a missing/mismatched ID, no ambient-time
window bans a refit, and `ingest_seq` is a diagnostic that no comparison reads.

### A-48.2 — Third verification pass: the documents' own ordering and record-keeping rules

The batch was audited a third time, against the classes the first two passes did not sweep:
numeric consistency of the worked examples, ordering of every numbered list, and completeness of the
documents' own record-keeping. Three defects were found, all of the class S-10 names — an artifact
failing a rule it states about itself.

| # | Defect | Closure |
|---|---|---|
| 1 | **`QC7a` was listed between `QC11` and `QC12`.** `TestScopeArtifactConsistency` is specified to fail on a number that appears out of order, and the qualification-check list failed that rule. | `QC7a` moved to directly after `QC7`, where its number says it belongs. Order is now QC1…QC7, QC7a, QC8…QC15. |
| 2 | **§18 recorded no decision from this batch.** The ten normative choices S-1…S-10 make — integer nanoseconds, pre-campaign-only fitting, the field registry, `runner_class`, `cache_state`, the three identities and the recency stamp, `NOT_FOUND_WITHIN_BUDGET`, the production atom regression, sequential order, the delta registry — were stated in their own sections but absent from the document's decision record, which claimed to hold "decisions taken in this scope". | **DEC-26…DEC-35** added, each naming the alternative it rejects, with a §19 pointer so the record is discoverable. DEC entries are contiguous 1…35. |
| 3 | **The ledger defined four status values while its summary used six.** `OWNER-RESOLVED` and `HISTORICAL / SUPERSEDED` were asserted as partitions without ever being defined, in a document whose summary claims the six sets are disjoint and exhaustive. | The vocabulary is now a six-row table covering exactly the partitions used, verified equal in both directions, plus the explicit promotion rule adopted at A-47. |

**Also verified, and correct as written:** the contract §0.9 worked K=2 counterexample was recomputed
literally under the new integer-nanosecond objective — mixed seed `250e9`, both single-unit moves
raising the maximum (`100e9/300e9` and `150e9/350e9`), the whole↔slice swap reaching `200e9/200e9`.
Every number contract §0.9, contract §6.5, and tests 42 and 51 assert is arithmetically right. Contract §7's items are
contiguous 1…17; §16's tests are contiguous 1…67; §10's fail-closed rows are contiguous 10.1…10.21;
admission rules are contiguous AD-1…AD-10; the ledger partition is exhaustive over A-01…A-48 with
no entry in two sets.

### A-48.3 — Fourth verification pass: cross-document schema and reference checks

Audited on the class the first three passes did not sweep: **agreement of the same fact stated in
more than one document**. One defect was found.

| # | Defect | Closure |
|---|---|---|
| 1 | **The two gate tables diverged.** §19.7 said it "mirrors [contract §10's] campaign-facing rows and must not diverge", and `TestThresholdsAreFrozenBeforeRunOne` asserts exactly that — but the tail repair had added three gates to contract §19.7 (**sequential order** S-9, **cache state** S-5, **model freeze** S-2) without carrying them into the contract, so the two authoritative tables disagreed on what the frozen gate set is. Separately, the contract's `integrity` row said "every **row** passes QC1–QC15", which contract §19.4a makes unsatisfiable: failed and malformed observations are deliberately retained as diagnostics and by construction do not pass QC. | The three gates are carried into contract §10; `integrity` is corrected to "every **scored** row … every harness run … appears in the **attempted** population with a disposition", matching contract §19.4a's two-population rule. At the time, both tables were reconciled to the same gate set in the same order. **Superseded by R13-A:** the duplicate table in §19.7 is **removed**, because two synchronized copies plus a test to keep them synchronized is a weaker arrangement than one copy. Contract §10 and §10.4 are now the only statement of the gate set and its numeric values, and test 27 asserts that no other document states a gate label, threshold, or comparison operator. |

**A second-order defect this exposed, and closed.** "Must not diverge" was never defined, so the
assertion was unfalsifiable. It was first closed by specifying the four dimensions test 27 compared.
**Superseded by R13-A:** the comparison itself is gone, because two synchronized copies plus a test
to keep them synchronized is strictly weaker than one copy. §19.7's duplicate table is
removed; contract §10 and §10.4 are the only statement of the gate set and its values; and test 27
now fails on a gate label, threshold, or comparison operator appearing in **any** other document.

**Verified consistent, not changed:** the pinned membership counts agree across all three places
that state them — §20.6a, `testdata/consumers/SOURCE.md`, and this ledger's A-05 — at
1,512 tracked / 1,444 in the root-config union / 1,396 bucket / 48 excluded Case / 67 other
integration / 1 region-router / 8 harness-unit, with 42 production collision pairs and 0
cross-boundary. Every test named in this ledger's A-48 S-row table is defined in contract §22. **Corrected by D-13:** the further claim
that every one also appears as a `scope.md` §21.0 registry acceptance value was **false** —
`TestCampaignRowsNeverRefitTheModel` and `TestThresholdsAreFrozenBeforeRunOne` are S-row tests that
govern campaign *policy* rather than an R54 implementation delta, so they correctly have no registry
row. Test 28 enumerates the **delta** tests from the registry; the S-row set is a superset of them.

### A-48.4 — Fifth verification pass: boundary preservation, verified mechanically

The governing instruction on this edge is "repair every exact adversarial finding **without
reopening settled product decisions**", and every prior pass asserted that boundaries were preserved
without checking it. This pass checked it, on the two classes that would show a regression.

| Sweep | Method | Result |
|---|---|---|
| **Settled `PRODUCT_DECISION` rejections re-introduced?** — protected environments, cgroup eligibility, multi-signer/countersignature/key rosters, object lock, fleet attestation, triple observers, replay attestation, subreaper, immutable storage, distinct-UID execution | every occurrence in the sections this batch **added or rewrote** (`scope.md` §2A, contract §15.1b, contract §10.5.0, contract §17.3, contract §19.3's orchestrator, §15, §21, contract §10's gates) was located and read in context | **0 re-introduced.** Five occurrences exist and all are explicit negations — "requires no cgroups, protected environments, or immutable storage" (contract §10.5.0), "not a protected environment, not a signing key" (contract §19.3), and ID-12's status line describing what R54 wrongly promised |
| **Forbidden claims asserted?** — the thirteen prohibitions of contract §9 and §18.3: immutability, permanence, tamper-evidence, external attestation, signature-proves-environment, process-group-proves-containment, upstream byte-identity, `A` as complete action or job time, randomized order, significance/power, outcome-free allocation, snapshots proving live state, unit-level training labels | each prohibition converted to the pattern it would match **if violated**, run across all four prose artifacts, and every hit classified by whether it falls inside a region that *enumerates* the prohibition | **0 violations.** Every hit is either inside contract §9 / contract §18.3 / `scope.md` §7.0 / §20 where the phrase is being forbidden, quoted as a rejected alternative (DEC-20's second column), or negated across a line break ("rather than claim an outcome-free allocation surface") |

Two repairs were specifically checked for the claim they could most easily have overstated, and
neither does: **S-5** binds `cache_state` and stops there — it says "the sole intentional and
configured difference", never that the two arms are physically identical, and names page-cache
warmth as unbound; **S-6** calls `observed_start_realtime` **self-reported** everywhere and never
authenticated, reserving that word for the operator-retrieved Actions API instants.

**No defect was found by this pass, and nothing was changed as a result of it.** That is the
reportable outcome; the entry exists because "boundaries preserved" had been an assertion for four
passes and is now evidence.

## A-49 — Fresh adversarial review R7: D-1…D-13 closed · CORRECTED · NEW

A genuinely fresh review (`/tmp/testbucket-practical-walltime/scope-tail-adversary-r7.md`) of the
**repaired** package returned `NEEDS_SCOPE_REVISION` on thirteen findings. Its independent
reproductions were re-verified here and every one held **at that revision**. **R13-A: the registry
cardinalities it published are HISTORICAL and are no longer reproduced in this ledger** — they
described the R7-era registry, not the current one, and a ledger that quotes a registry projection
is a second authority. What survives from that reproduction is the part that is not a registry
count: the integer objective and K=2 witness, rank behaviour, the full 42/0 production collision relation, the
pilot/campaign arithmetic, and both stored-fixture digests.

**Most of these defects were in the S-1…S-10 repairs, not in what preceded them.** Five are
outright logic errors that would have made an acceptance test unsatisfiable by any fixture:

| # | The error, stated plainly |
|---|---|
| D-5 | an `exact-key` cache **miss** was unrepresentable — `matched_key` had to be simultaneously empty and equal to a non-empty primary key |
| D-6 | ring retention depended on **arrival order**: 241 rows sharing a timestamp evict differently forward vs reversed, so the shuffle-invariance test could never pass |
| D-7 | `L(5)` re-isolated the unit `L(3)` had already isolated, so `L(5) = L(4)` and test 59's boundary test had no satisfying fixture |
| D-8 | the 23→24 test compared the 24th row's model against a coefficient and a wall plan that **do not exist at 23** |
| D-9 | Stage-2 accepted only strict makespan decreases while promising the lexicographically smallest vector — unreachable unless already there |

The remaining eight were overclaims (D-1 units and seconds, D-2 the registry's totality, D-12 the
delta registry's generation), missing data paths (D-3 leakage, D-4 scored/profile plumbing), or
stale companions (D-10, D-11, D-13). All thirteen are closed; `scope.md` §20.0b maps each to its
normative repair and acceptance test.

**What this says about the prior five passes.** Passes 2–5 swept executability, intra-document
ordering, cross-document agreement, and boundary preservation — and found 5, 3, 1, 0. None of them
swept **whether a stated rule is satisfiable by any input at all**, which is where every one of
D-5…D-9 lived. Self-audit converged on the classes it was looking for and was blind to the one it
was not. That is the argument for an independent adversary rather than more self-review, and it is
why pass 5's "the defect classes are exhausted" was true only of the classes I had chosen.

**Status is `CORRECTED`, not `VERIFIED`** — the A-47 promotion rule applies unchanged.

## A-50 — Fresh adversarial review R8: D1…D8 closed · CORRECTED · NEW

A second independent review of the **R7-repaired** package returned `NEEDS_SCOPE_REVISION` on eight
findings. Its reproductions were re-verified here and held **at that revision**: the 42/0 collision
relation, the pilot/campaign arithmetic, and all ten fixture digests. **R13-A: the registry
cardinalities it published are HISTORICAL and are not reproduced here** — they described the R11-era
registry (and its fourteen-leaf comparability key, which R12-D3 then corrected), so restating them
would preserve a stale count in a document that is not the registry.

**Five findings were rules or oracles that no input could satisfy**, four of them introduced by the
R7 repairs:

| # | The error | Closure |
|---|---|---|
| R8-D1 | the published calibration fixture had **exact ranks 2 and 3, not 3 and 4** — verified by rational elimination — and its `L(5)` bucket list did not follow the generator, which at `K = 4` leaves no slot for the residual whole units | the slot rule now reserves a residual slot; the boundary is republished at **`N = 3` not-found / `N = 4` sufficient** over a universe whose `L(1)…L(4)` bucket lists the generator actually produces and whose ranks are exactly 3, 3, 3, 4; `STRUCTURALLY_INFEASIBLE` names **column 4** for an all-whole universe, not column 3 |
| R8-D2 | `ingest` was told to reject rows whose `run_id` was "in the config's harness-run universe" — but the config is frozen **before dispatch** and those IDs are allocated after; the pilot was simultaneously rejected-before-append and retained-as-diagnostic; and the fit freeze depended on ambient `now` | the campaign identity travels **with the observation**: the config declares one opaque `campaign_id`, `run-bucket` stamps it, and ingest appends every row as a diagnostic while marking matching rows **`trainable: false`**. That is durable, needs no future run IDs, and lets "retained" and "never trains" both be true |
| R8-D3 | the row-intrinsic recency key read `repository` and `job_id`, **neither of which the 19-path ring schema serialized** | `ring.repository` and `ring.job_id` added; `ingest_seq` roled `DIAGNOSTIC` to match the prose that says it is read by nothing; test 52 round-trips every field the key reads |
| R8-D4 | test 62 rejected **every** `matched_key != primary_key`, which includes the explicitly legal `exact-key` **miss**; and "all six leaves"/"any cache field" equality contradicted the four-leaf gate table | four invariant leaves everywhere, compared index-wise; `matched_key`/`disposition` per-row by design; test 62 asserts the miss as a **positive** fixture |
| R8-D7 | Stage-2's bounded local descent was described as a **global** lexicographic minimum | the guarantee is narrowed to what the algorithm delivers — the deterministic output of the bounded schedule — **not** a fixed point and not convergence (corrected by R10-D2) — and global minimality is withdrawn |

The other three were structural overclaims: **R8-D5** (the registry collapsed two versioned campaign
artifacts into one and used three cardinality values it never declared), **R8-D6** (the reusable
workflow offered `scored` while withholding three inputs AD-9/AD-10 require), and **R8-D8** (the
delta registry's generation claim, ID-4's `AD-1…AD-9` target, ID-21 conflating a source delta with
external evidence, one test symbol holding both the offline and checkout contracts, and test 58's
"published" oracle that appeared nowhere — now published in full).

**A process failure worth recording.** While repairing R8-D1 I used an unbounded `rindex` span and
**deleted §11 through §16 of `scope.md`** — Evidence, Keep/Simplify, Campaign, Preservation gates,
Interfaces, and the whole Test plan. It was caught by a section count, recovered from the `jj`
operation log (`jj --at-op … file show`), and every subsequent edit used exact-string replacement
with an assertion, or a line-indexed splice with a span guard. The lesson is narrow and practical:
**never compute an edit span with `rindex` across a document**, and always assert a maximum span
length. `jj`'s automatic working-copy snapshots are what made the loss recoverable.

**Status is `CORRECTED`, not `VERIFIED`** — the A-47 promotion rule is unchanged.

## A-51 — Fresh adversarial review R9: D1…D10 closed, with executable checkers · CORRECTED · NEW

A third independent review returned `NEEDS_SCOPE_REVISION` on ten findings. Its central one is the
sharpest defect this package has had:

**R9-D1 — the published 23→24→25 oracle was arithmetically false.** Exact rational least squares
over the corpus I published solves to `[10, 1, 10, 5]` at 24 rows and `[18, 1, 2, 1]` at 25, so
`per_slice_overhead_ns` moves **down**, not up, and the two named partitions do not cross. I
reproduced both solves before repairing: R9 is exactly right. The oracle was prose asserting an
arithmetic result nobody had computed.

**The repair is a file, not a sentence.** `testdata/walltime/test58-feedback-oracle.json` now carries
the corpus, both exact rational fits, both integer-nanosecond coefficient sets, the shared Stage-1
seed, both final bucket vectors, per-bucket `A_eta_ns`, both makespans, and a cross-evaluation. Every
number was produced by an executable reference calculation — exact rank, exact least squares, and the
**real bounded Stage-1/Stage-2 planner** of contract §6.5 — and a checker recomputes all of it from the fixture
on every run. The four properties the owner asked for are each mechanically verified:

| Asked for | Verified |
|---|---|
| rank/admission boundary at the intended row | 23 rows have **rank 3** (every row satisfies `I + slice_count == 1`, so column 1 = column 3 + column 4) and fail both gates; row 24 is a **mixed** bucket that leaves the span, giving **rank 4** |
| coefficients with documented signs and units | `fit_at_24` = `[5, 1, 8, 3]` exactly — recovering the ground-truth model — all four non-negative, three int64 ns plus a dimensionless `scale` |
| next observation causes the claimed partition transition | row 25 refits to `[5, 37/53, 680/53, 399/53]`; the **actual planner** moves from `[[w2],[w1],[s1,s2]]` to `[[s1],[s2],[w1,w2]]` (corrected from an LPT-derived vector by R10-D1), differing as a **multiset**, and the row-25 model strictly prefers its own plan (`24.81e9` vs `27.04e9` ns) |
| the test compares artifacts that exist | it compares the fixture's bytes |

**R9-D2 was the other substantive one.** The `campaign_id` → `trainable` path existed only in
contract §19.9c while every other surface still named `excluded_run_ids`, and — worse — the rule was
**fail-open**: a row was marked non-trainable only when its id *matched*, so a miswired arm with a
wrong or absent id stayed trainable. It is now fail-closed (mismatch or absence under a supplied
config is a **rejection**), propagated to §7.1, contract §15.1b, contract §6.4b and the component map, with
run-id/window predicates reclassified as **operator-side cross-checks** that cannot be the in-CI
mechanism because those IDs do not exist when the config is frozen.

D3 (owner closure — the pair skeleton is `owner: both`, opaque containers marked, `roled_anyway`
regenerated, observation list 53→54), D4 (four-leaf cache rule propagated to seven surfaces), D5
(interface targets reconciled, `setup_command` producer fixed), D6 (calibration ranks recomputed),
D7 (fixed-point wording), D8 (ID targets), D9 and D10 (companions) are likewise closed.

**The method changed, at the owner's direction.** `SCOPE_READY` is no longer a manual assertion: a
read-only checker suite at `/tmp/testbucket-practical-walltime/checkers/` recomputes the fixture,
the registry's ownership closure, cross-document equality, the cache rule, calibration ranks, the
interface union, and the delta registry, and **exits non-zero on any violation**. It reproduced all
of R9's findings from the pre-repair bytes and reports **zero** after.

**Status is `CORRECTED`, not `VERIFIED`** — no product acceptance test has run.

## A-52 — Fresh adversarial review R10: D1…D9 and seven owner closure items · CORRECTED · NEW

R10's central finding is the most instructive one this package has produced.

**R10-D1 — the "executable reference calculation" executed the wrong algorithm.** The algorithm of
contract §6.5 seeds with the existing `karmarkarKarp` of `internal/core/partition.go`. My checker defined `kk_seed` as
"deterministic LPT" — greedy heaviest-item-to-lightest-bucket — and labelled it a stand-in. LPT and
KK are not aliases: for the row-24 model KK gives the seed `[[w1,w2],[s1],[s2]]` and LPT gives
`[[s1],[s2],[w1,w2]]`, and the published row-24 plan was consequently wrong. The previous turn's
report said the numbers came from "the real bounded planner". They came from a substitute.

**What was rebuilt.** The reference calculator now implements the generalized largest-differencing
KK exactly as `partition.go` does — per-item tuples, `twoLargest` by `(diff, tag)`, heaviest-to-
lightest pairing, normalisation, and the final true-load-descending ordering with the `￿` empty-part
sentinel — and it reproduces R10's independently computed seeds and plans exactly:

| | Stage-1 KK seed | after Stage 2 | costs (ns) |
|---|---|---|---|
| row-24 model | `[[w1,w2],[s1],[s2]]` | `[[w2],[w1],[s1,s2]]` | `18e9, 18e9, 21e9` |
| row-25 model | `[[s1],[s2],[w1,w2]]` | unchanged | `16_018_867_925, 16_018_867_925, 24_811_320_754` |

The seed is **model-specific** — a slice unit's seed weight carries `per_slice_overhead_ns` — which
is why publishing one shared seed was wrong. The composition transition survives, and the checker
now also asserts that **KK and LPT differ** on the published universe, so an LPT substitution can no
longer pass unnoticed.

**The other eight, and the seven owner closure items:** every fixed-point and convergence promise is
deleted in favour of "deterministic output after a bounded schedule", with the Stage-2 restart policy
made singular and explicit (item 1, D2); `campaign_id → trainable` is propagated into the §7.1 role
table, contract §6.8, contract §15.1b and test 61's six-case state machine, and the four-leaf cache rule into test 62's
positive fixtures (item 2, D3/D4); the false general rank sufficient-condition is withdrawn — R10's
counterexample `[1,1,0,0],[1,2,0,1],[1,2,1,0],[1,4,1,2]` has rank 3, which I reproduced — deficient
classes now say `≤ 3`, stale test-59 language is gone, and isolation exhaustion is defined for
`i − 3 > |W|` (item 3, D6); interfaces carry one disposition per input with derived cardinalities
(item 4, D5); ID-21 is source-only with the orchestrator run left to AG-2/AG-4, and the R54 overlay
is now **per-row isolated with symbol attribution** so a red result names why *that* delta is missing
(item 5, D8); and the companion sweep covers A-02, A-48.1, A-50, A-51, salvage-audit's live-model
block, component-map dispositions, and SOURCE.md's "two symbols" (item 6, D9).

**Item 7 — the checker fails on the prior state.** Demonstrated, not asserted: the suite was run
against the pre-repair bytes recovered from the `jj` operation log and **failed**; it passes only
after the repairs. The transcript is in the repair report.

**The lesson, stated plainly.** Making verification executable is necessary but not sufficient — the
executable has to be faithful to the normative spec. A checker that runs the wrong algorithm produces
confident, verified, wrong answers, which is worse than prose because it carries an evidence claim.
The check that catches this class is the one now in the suite: **assert the reference implementation
disagrees with the plausible substitute.**

**Status is `CORRECTED`, not `VERIFIED`.**

## A-53 — Fresh adversarial review R11: D1…D11 closed · CORRECTED · NEW

Eleven findings, closed with per-finding executable checks. Three were substantive design defects
rather than stale prose:

**R11-D3 — the cache-state file could not exist at the boundary it was passed to.** The orchestrator
was told to write all six `cache_state` leaves into one file handed to both `plan` and `run-bucket`,
so `plan` could validate it before matrix emission. Three of the six — matched key, disposition, and
the executed Mongo digest — are **outcomes of each bucket job's own restore and setup**, produced on
a different runner after the matrix exists. No filesystem path in the plan job can carry them.
New contract §10.5 splits **declaration** (four leaves, knowable before dispatch, immutable across the eight
jobs, validated by AD-9) from **outcome** (three leaves, filled by `run-bucket`), renames the input
`cache-declaration-file`, and adds a two-bucket fixture where bucket 0 hits and bucket 1 misses under
one declaration — both valid, pair still scored, plan never told either outcome.

**R11-D4 — cache inputs changed the measured lifecycle but were absent from history comparability.**
contract §10.5.0 says dependency/transform/Mongo state changes the measured preflight and import work, yet none
of it was in the 14-leaf key that selects the ring and model. Pairwise equality protects B against C
*inside* a pair; it does nothing to stop pre-campaign fitting from mixing `disabled` and `exact-key`
runs under one model. A 15th leaf, **`cache_declaration_digest`** over the declaration leaves,
now resets history exactly as `lock_sha256` does — while the two per-job outcomes stay out of the
key, because they vary legitimately between buckets of one run.

**R11-D6 — the numeric contract was not deterministic enough for its byte-identity claims.** Full
column rank gives a unique mathematical optimum, not identical floating-point bytes. The solver is
now pinned to **Lawson–Hanson active-set NNLS** with named column-selection, tie, inner-solve and
stopping rules; residuals and the `degraded` decision are computed from the **rounded integer**
coefficients, i.e. from the model that will actually plan; QR pivoting/tie behaviour and the
`deficient_columns` mapping are fixed; `sigma_max`/`tolerance`/`min_pivot`/`scale` serialize as
shortest round-tripping decimals; and `model_parameters_digest` has a canonical four-key JSON form
with `scale` **quoted** so no float formatter can vary the bytes. Test 54's four matrices are
published and their ranks verified here by exact rational elimination.

The remaining eight were singularity and inventory defects: one in-CI leakage mechanism (D1), one
cache scoring rule (D2), the reversed calibration acceptance paragraph plus `L` restated as a
**partial** function under a **total proposer** with the one-slice `K=3` case resolved (D5), §7.1
demoted to an explicitly **non-exhaustive synopsis** with the five `roled_anyway` paths and the
misleading YAML comments removed (D7), one authoritative duration inventory with `ImbalancePct`
removed as a percentage and a single `wall_est_seconds` presence rule (D8), the boundary's
descendant-drain language (D9), the prose-generation claim, corrected row-24 vector and a
**mechanically attributable** overlay via per-row `overlay_files` and `required_symbols` with
transitive dependency closure (D10), and the inventories, split fixture symbols and PWT-13 identity
(D11).

**Verification.** All six mandated rechecks pass, including the full 1,512-path collision
reproduction — **42 selected↔selected edges, 0 Case crossings, 24 atoms over 61 paths** — whose edge
**set is identical to R11's published list**. The zero-match checklist reports 15/15 ABSENT and the
semantic audit is clean.

**One method note.** Reproducing the collision relation meant reading
`internal/runner/vitestrunner/collide.go` and transcribing `collidesUnderSomeProjectRoot` exactly; my
first attempt used a plausible "any shared suffix" predicate and produced **586** edges instead of
42. That is the R10-D1 lesson recurring: when a check has to reproduce product behaviour, transcribe
the product's algorithm — never a paraphrase of it.

**Status is `CORRECTED`, not `VERIFIED`.**

## A-54 — Fresh adversarial review R12: D1…D8 closed · CORRECTED · NEW · **final repair visit**

Eight findings, all of them **propagation and determinism gaps rather than new design errors** — the
principal normative sections were right and the companions or interfaces had not caught up.

| # | What was wrong | Closure |
|---|---|---|
| D1 | the singular `campaign_id → trainable:false` rule was correct in contract §19.9c but two companions still carried run-id-in-CI, reject-before-append, and `ingest_seq` tie language | A-48.1 and §20's D-3 row rewritten to the append-and-retain rule, with `run_id`/window explicitly **operator-side / manual-invalidation only** and the fail-closed missing/mismatched cases preserved |
| D2 | contract §10.5's declaration/outcome split never reached the command interfaces, workflow, QC14, or the component map | the obsolete `cache-state-file` name was renamed to **`cache-declaration-file`** everywhere (19 sites); QC14 now checks both digests **and asserts `mongo_binary_sha256 == expected_mongo_binary_sha256`**; the block is **seven** leaves (4 declaration + 3 outcome) in scope, contract, and component map |
| D3 | the component map's comparability projection had drifted from the normative membership | the projection is taken **from** `memberships.comparability_key`, in that order, and now includes `cache_declaration_digest`. **R13-A: the leaf counts this row used to quote are removed** — a leaf count is a projection of the field registry, and a ledger that writes one is a second authority |
| D4 | eight live sites still called `L` or "the generator" total | every one now says **`L` is partial; the proposer is total** |
| D5 | `cache_state.expected_mongo_binary_sha256` was a non-exempt scalar with no primary role | added to the `cache_state` ADMISSION projection, so **every** non-exempt scalar carries exactly one role and none is missing. **R13-D7/R13-A:** the literal coverage ratio this row used to quote is removed — role coverage is a **derived** projection of the field registry, and test 63 compares computed sets, never a written number. `cache_state.mongo_binary_path` joined the same projection under R13-D2 |
| D6 | `wall_est_seconds` had a broad emission rule in contract §16.3, `scope.md` §2A over-generalised the division claim, and salvage-audit's live equation dropped `round_half_up` | contract §16.3 defers to contract §5.1's single predicate; `scope.md` §2A qualifies the claim to **wall-derived** displays and exempts the legacy reporter EWMA surfaces; the equation carries `round_half_up` everywhere |
| D7 | `residual_mae_ns` and `residual_p90_ns` could not be serialized deterministically, and `sigma_max` had no pinned method | MAE is `round_half_up` of the exact rational mean; p90 is **nearest-rank at `ceil(0.90n)`, no interpolation**; `sigma_max` is the largest singular value from a **Golub–Reinsch SVD** with named stopping criteria. The row-25 witness is published and verified: `Σ\|r\| = 38 641 509 430`, **MAE 1 545 660 377**, **p90 1 811 320 754** |
| D8 | component-map revision, count, and a stale symbol | `revision: PWT-13`; hand-written counts removed; the obsolete single fixture symbol replaced by the split pair |

**Verification.** Zero-match checklist **11/11 ABSENT**; checker suite **0 violations**; the complete
suite **9/9 PASS**, covering role coverage, the comparability-key projection, the cache block,
leakage singularity, `L` partial, the KK planner reproducing both published plans, and the residual
witness. **R13-A:** the registry cardinalities this paragraph used to quote — the role-coverage
ratio, the key-leaf count, the cache-leaf count — are **derived** from the field registry by the
checkers and are no longer written here; a ledger that restates them is a second authority, and the
first thing a second authority does is drift.

**This is the final repair visit; the repair→adversary edge is exhausted at 12/12.** The package is
sealed for a **fresh external adversary/adjudicator continuation**, with hashes recorded in the
handoff section of `/tmp/testbucket-practical-walltime/scope-tail-repair.md`. Nothing here claims
approval: the terminal state is a specification whose internal checks pass, not an accepted product.

**Status is `CORRECTED`, not `VERIFIED`** — no product acceptance test exists to run.

**The pattern across five self-audits and one independent review.**

| Pass | What it found | Underlying error |
|---|---|---|
| 1 | the ten findings, repaired | the original defects |
| 2 | five rules stated **without the input, field, or producer** that lets a component obey them | *a rule with no executable producer* — S-4's defect, one level up |
| 3 | three cases of **a document failing a rule it states about itself** | *claimed consistency never mechanically checked* |
| 4 | one **cross-document** divergence, plus an undefined "must not diverge" | *a consistency rule stated between two documents but never given a comparison* |
| 5 | **nothing** — boundary preservation verified clean on both sweeps | — the classes *I had chosen* were exhausted |
| R7 | **13 defects**, five of them unsatisfiable-as-written | *a rule no input can satisfy* — the class self-audit never swept |
| R8 | **8 defects**, five unsatisfiable, four of them introduced by the R7 repairs | *repairs that create new impossibilities* — verified arithmetic, not review, is what caught the false ranks |
| R9 | **10 defects**, headed by an oracle whose arithmetic had never been computed | *prose asserting a numerical result* — the fix is an executable reference calculation and a checker suite, not a better sentence |
| R10 | **9 defects**, headed by a reference calculator that used **LPT where the scope specifies Karmarkar–Karp** | *an executable check that executes the wrong algorithm* — a checker is only as good as its fidelity to the normative spec |

All three later patterns are the same underlying error as the original S-10: **the consistency the
documents claim was never mechanically checked**, so it drifted. That is precisely why
`TestScopeArtifactConsistency` parses the registries and enumerates the inventory rather than
reading a number, and why it **fails** on the snapshot each of these passes replaced.

**The pattern from pass two is worth keeping too.** Five of its six were the same failure mode:
stating a requirement and stopping, without adding the input, field, or producer that lets the
component subject to the rule actually satisfy it. It is the same defect S-4 identified in the original documents — "no
executable producer" — reappearing one level up, in the repairs themselves. The check that catches
it is: *for each new rule, name the component that has to obey it and confirm every value it needs is
reachable from where it runs.*

**Status is `CORRECTED`, not `VERIFIED`.** No acceptance test has been run against an
implementation; these are specification repairs with named executable tests. The rule adopted in
A-47 applies: a repair entry is promoted to `VERIFIED` only when its named test passes.

---

## Summary

| Status | Entries — **each entry appears in exactly one partition** |
|---|---|
| VERIFIED | A-01, A-02, A-03, A-05, A-06, A-09, A-10, A-11, A-12, A-13, A-16, A-17, A-18, A-19, A-21, A-25, A-26, A-27, A-28, A-29, A-31, A-32, A-33, A-34, A-35, A-36, A-40, A-41 |
| INHERITED | A-04, A-07 |
| UNVERIFIED | A-08, A-14, A-15, A-20 |
| OWNER-RESOLVED | A-24, A-30, A-37, A-39, A-43 |
| HISTORICAL / SUPERSEDED | A-22, A-23, A-42 — never current state; A-22/A-23 are closed by the artifacts-in-tree entry, A-42 by the two later `NEEDS_SCOPE_REVISION` verdicts |
| CORRECTED | A-38, A-44, A-45, A-46, A-47, A-48, A-49, A-50, A-51, A-52, A-53, A-54 |

**Partition rule.** These six sets are **disjoint and exhaustive** over A-01…A-54. An entry's
partition is its *current* status; the corrections it went through are narrated inside the entry,
not by listing it twice. Earlier revisions had A-24 and A-30 in both VERIFIED and OWNER-RESOLVED and
A-22/A-23 in more than one set; this revision additionally moves **A-42 out of VERIFIED** — a past
`SCOPE_PASS` is not current status once two later visits returned `NEEDS_SCOPE_REVISION` — and moves
**A-47 from VERIFIED to CORRECTED**, because the current review found several of the repairs it
called verified were not satisfied by the artifacts as written. `TestScopeArtifactConsistency`
rejects any entry appearing in two partitions and any `VERIFIED` repair entry whose named test does
not pass.

**Corrections narrated in place, not double-listed:** A-05 (twice — Mandel availability, then the
project-based predicate); A-06 ("immutable pin" overstated the binary half); A-26/A-27 (boundary
wording); A-31 and A-33 (allocation did not govern work items; a Mandel-side workflow was not a safe
harness); A-04 (a reviewer withdrew its test claim); A-40 (the R54 delta figure was quoted, not
derived); A-41 (the artifacts were not in the tree).

The four unverified entries, in order of consequence:

1. **A-15 / A-20** — one question with one answer, `scale`. Measured before the campaign; fails
   closed if bad.
2. **A-14** — an unexercised delivery form that gates neither the product nor the campaign.
3. **A-08** — a retention window that shortens a schedule and changes nothing else.

**Current state.** The specification and the consumer fixture **are in the tree** (A-41, A-02);
A-22, A-23, and A-42 are historical and none is current status.

Two things remain open, and only one of them is a document:

1. **A-48 is `CORRECTED`, not `VERIFIED`.** The S-1…S-10 repairs are specification-level. Each names
   an executable acceptance test; none of those tests has been run against an implementation,
   because the implementation does not exist. The artifacts' own consistency test
   (`TestScopeArtifactConsistency`) **fails** on the snapshot this revision replaced and pass
   only after these repairs — that is what promotes A-44…A-48 from `CORRECTED` to `VERIFIED`.
2. **A-28** — no consumer has adopted the wall path, so the workload claim is unproved until the
   adoption gate AG-1…AG-4 and the campaign complete. That is closed by a run, not by a document.

The earlier summary said "only external adoption remains". That was wrong while the internal
contradictions of S-1…S-10 stood, and this ledger no longer says it: internal repair (1) and
external evidence (2) are listed separately.
