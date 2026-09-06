# Source-to-claim map (adjudicator R1)

Read-only, `jj` only, 2026-09-07 UTC. This closes R1: it replaces the "installer-only R54"
premise with verified diff arithmetic and maps each changed surface to its role in the claim.

## 1. The diff arithmetic, verified three ways

The adjudicator reported "17 files (614 additions, 25 deletions)" for
`9537850eed193b9dfe29dce272d43b87d1f4bbf9` → `8d27e9bccc537032615578748c80bed9300ec7bd`. That
number is real but belongs to a **different revision pair**. Measured here:

| From | To | Files | Insertions | Deletions |
|---|---|---:|---:|---:|
| `693a1998` (v0.2.2 baseline) | `8d27e9bc` (R54) | **184** | 53,295 | 972 |
| `9537850e` (R54's sole parent) | `8d27e9bc` (R54) | **3** | 328 | 5 |
| `730cd78c` (two commits back) | `8d27e9bc` (R54) | **17** | 614 | 25 |

`730cd78c → 8d27e9bc` reproduces 17/614/25 exactly, so that is the pair the adjudicator measured;
it spans two commits (`9537850e` and `8d27e9bc`), not the named predecessor pair. The
`9537850e → 8d27e9bc` figure of 3/328/5 is confirmed by `jj diff --summary` and `--stat`.

**Both critiques land anyway, and the scope now reflects them:**

- The adversary's framing — "R54 is an installer-only change" — is wrong *as a description of the
  candidate*. The candidate delivered to a campaign is the **whole 184-file branch delta from
  v0.2.2**, not its last commit. Treating unrelated R54 surfaces as invisible was the error.
- The prior scope carried the 3-file figure without deriving it. It is correct for the pair named,
  but it was quoted, not computed. It is computed here.

## 2. Surface map over the 184-file candidate delta

Every changed path is one of three roles. Counts come from `component-map.json`, whose inventory
was checked against `jj diff --summary` (184 paths, each exactly once).

| Role | Meaning | Count |
|---|---|---:|
| **Measured product** | Directly produces, consumes, or bounds the measured quantity, the allocation, or the estimate | 22 KEEP + the practical kernel of 49 SIMPLIFY |
| **Compatibility-sensitive** | Does not measure anything, but a change here is visible to a consumer's matrix, script bytes, store, or audit | subset of KEEP/SIMPLIFY, listed below |
| **Excluded** | Research protocol or distribution machinery; not part of this claim | 112 REMOVE + 1 UNCERTAIN |

### 2.1 Measured product

| Surface | Role in the claim |
|---|---|
| `internal/walltime/{clock,clock_linux,clock_fallback,clock_other,nanos}.go` | the monotonic reading itself; the boundary of §4 rests on these |
| `internal/walltime/{exec,action}.go` | the `Exec` envelope and the instrumented run-bucket interval — the two observables |
| `internal/walltime/{contain,contain_pgroup,contain_other,spawn_other}.go` | owned-process-group **root wait and reap**, plus same-PGID signal and **group drain where probeable**, inside the envelope — descendants that leave the group are never reaped or drained (R11-D9) |
| `internal/walltime/{record,verify}.go` | the observation document and its qualification checks |
| `internal/walltime/{palloc,aeta,gates}.go` | scoring, forecast, and the campaign arithmetic |
| `internal/core/plan.go` | `AllocationScore` seam, matrix schema, and the displayed estimate contract §5.1 defines |
| `internal/core/store.go` | reporter EWMA weights — the model's regressor and the topology input |
| `internal/planbind/*` | the allocation bridge between the model and the planner |
| `internal/runner/vitestrunner/{render,exec}.go` | argv, invocation grouping, the wall hook |
| `.github/actions/run-bucket/action.yml` | the step order that defines where the interval opens and closes |

### 2.2 Compatibility-sensitive

| Surface | Why a change here is visible to a consumer |
|---|---|
| `internal/core/plan.go` (matrix serialization) | the matrix fields contract §16.3 governs; read by both consumers' workflows |
| `internal/core/store.go` (schema, token) | a schema or token change cold-starts a consumer's store |
| `internal/core/audit*.go`, `gate.go` | the never-drop-a-test coverage guarantee |
| `internal/runner/types.go` | `Invocation` shape shared by both adapters |
| `internal/runner/gorunner/render.go` | metadata added; **default rendered Go bytes unchanged**, per contract §8 |
| `internal/runner/vitestrunner/{collide,discover,grammar,names}.go` | exact-path atoms and discovery — the Mandel safety property |
| `.github/actions/{plan,record}/action.yml`, `.github/workflows/bucketed-reusable.yml` | consumer-facing inputs; both consumers compose the actions directly |

### 2.3 Excluded

Candidate delivery and release proof; protected authority, Stage 1/2, replay; multi-signer,
delegation, rosters; triple observers and raw evidence; cgroup delegation and credentials;
campaign/ablation/schedule/label-evidence machinery; study fixtures; and their 59 tests. Component
justification is in `salvage-map.md`; exact paths in `component-map.json`.

The three files of `9537850e → 8d27e9bc` (`install-testbucket.sh`,
`archiveenumeration_test.go`, `candidateinstall_test.go`) are **excluded** from the measured
product and ship as their own change (`scope.md` §12.6). That remains true; what was wrong was implying
the rest of the branch did not exist.

## 3. Focused regression matrix

Scoped to the surfaces above, not to the whole tree. Contract §22 requires each row before validation.

| # | Surface at risk | Regression | Fails today if the surface breaks |
|---|---|---|---|
| RM-1 | matrix serialization | the matrix shape contract §16.3 preserves, with the basis field present | yes |
| RM-2 | Go rendered bytes | Go script byte-identical; no `testbucket wall` / `spec-` / `--level invocation` | yes |
| RM-3 | Go token and sweep | `-race -count=100`, `-p=1`, `-timeout 20m`, count shards sum to the sweep | yes |
| RM-4 | Go events and audit | `go test -json` parse and `AuditCoverage` unchanged | yes |
| RM-5 | exact-path atoms | `./x` path tokens; atoms never split; and the **full production relation over the pinned 1,512-path universe** — 42 selected↔selected pairs, 0 selected→excluded-Case pairs, transitive atom closure, no atom split across an invocation or bucket boundary. The 2 keto/attribution pairs are the illustrative root-containment subset, **not** the protected property (D-10, contract §20.6a, test 64) | yes |
| RM-6 | cross-boundary safety | 0 bucket-filter → excluded-Case collisions at `d9ae1d43`, under the **production** shared-project-root suffix relation, not only root containment | yes |
| RM-7 | coverage gate | plan refuses a matrix that drops or duplicates a live target | yes |
| RM-8 | store schema | 1→2 migration preserves reporter rows; unknown schema cold-starts loudly | yes |
| RM-9 | envelope boundary | `A ≥ setup + script`, `script ≥ Σ V`, no negative interval | yes |
| RM-10 | invocation grouping | whole-file units → one invocation; each slice its own | yes |
| RM-11 | Go refusal | `--wall-dir` with `--runner go` errors | no — already passes; pinned to stay |
| RM-12 | action step order | envelope-open is the action's first step; no acquisition inside it | yes |

RM-1 through RM-10 and RM-12 are the focused matrix the adjudicator asked for: each is tied to a
surface the 184-file delta actually touches, and none is a whole-tree sweep.

## 5. Which document holds what — the reviewed map (owner F3)

`acceptance-contract.md` §0.10 withdrew the machine-readable authority grammar. This table is what
replaces it: short, read by a person, and revised when a document's job changes.

| Document | What it is | What it may contain | Where its claims are owned |
|---|---|---|---|
| `acceptance-contract.md` | the **one normative root** | every measurable requirement | itself |
| `scope.md` §2A `field-registry v2` | machine registry | wire paths, roles, memberships | itself, within that domain |
| `scope.md` §21.0 `implementation-delta-registry v1` | machine registry | the implementation deltas against R54 | itself, within that domain |
| `scope.md`, rest | derivation | rationale, worked examples, redirects to the contract | contract §§1–22 |
| `component-map.json` | derivation | inventory of R54 surfaces and their disposition, pointers | contract §21 for interfaces, §10.5 for cache, §0.9 for units |
| `salvage-map.md` | derivation | which R54 file serves which claim | contract §20 |
| `source-to-claim-map.md` (this file) | derivation | diff arithmetic, the regression matrix, this map | contract §20, §22 |
| `assumption-ledger.md` | history | superseded assumptions, labelled | nothing live |
| `salvage-audit.md` | history | the superseded pre-PD-3 audit, labelled | nothing live |
| `testdata/consumers/SOURCE.md` | evidence | vendored consumer bytes and their digests | contract §20.4–§20.6 |
| `testdata/walltime/test58-feedback-oracle.json` | evidence | the planner replay oracle's recorded values | contract §6.5 |

**How to use it.** If a companion sentence looks like a rule, find its subject in the last column and
read the contract section named there; that section is the statement. If the two disagree, the
contract wins and the companion is wrong. There is no third possibility, and no checker is needed to
say so.

**What is deliberately not here.** No closed artifact inventory, no declared modal vocabulary, no
owned-identifier index, no parsed key classification for the JSON, and no release gate over any of
them. Contract §0.10 records why the owner withdrew that machinery.

