# testbucket

**Self-optimizing, time-balanced test bucketing.** `testbucket` splits a
repository's unit tests into **K time-balanced buckets** and keeps the split
honest as the tests change. It is the mechanism behind a bucketed unit-test
workflow: `plan` turns a rolling timing store plus the live test set into a
GitHub-Actions matrix, and `ingest` folds each run's timings back into the store
so the next split is better than the last.

It is **framework-agnostic at its core** and speaks to a concrete test runner
through a small **runner-adapter seam**. The Go runner (`go test`) is adapter #1
and Vitest is adapter #2; both implement the same interface without touching the
engine (`internal/core` is byte-for-byte unchanged between them). See
[Adding a framework adapter](#adding-a-framework-adapter).

This repository **self-hosts**: its own CI buckets its own test suite through
the tool (see [`.github/workflows/bucketed.yml`](.github/workflows/bucketed.yml)).

## Three load-bearing properties

- **Never drop a test.** `plan` enumerates the LIVE tree — not the store — and
  refuses to emit a matrix unless every live package, and every *runnable*
  (test, example, or fuzz target) of every name-sliced package, lands in exactly
  one bucket. A balanced-but-incomplete split is the one failure mode worse than
  an imbalanced one, because nothing goes red. The gate proves the *matrix* is
  complete; the `audit` oracle later proves the *run* was.
- **Cold start is normal, not an error.** The store is a rolling CI cache, not a
  committed file, so a miss is routine. Any unit without a recorded weight gets
  the mean weight and is scheduled immediately; its real weight lands on the next
  master record.
- **Staleness is never silent.** Every `plan` prints a loaded-vs-missing summary
  — how many units carry real timings, how much measured wall-time they account
  for, how far the store has drifted from the tree — so an expired or mis-keyed
  cache shows up as numbers in the job log instead of as a quietly worse split.

## CLI

```
testbucket plan   [flags]   compute K buckets and emit a GH-Actions matrix
testbucket ingest [flags]   fold a run's timings back into the store
testbucket whales [flags]   show the per-runnable distribution behind each split
testbucket audit  [flags]   check a finished run's events against its plan
testbucket wall   <sub>     complete-action wall-time measurement (opt-in)
```

A run of the loop, by hand:

```sh
# Plan K buckets, capturing per-bucket go test -json into an events dir.
testbucket plan --k 6 --store test-timings.json \
  --events-dir /tmp/ev --shard-plan plan.json --json > matrix.json

# ...run each bucket's `script` (the fromJSON matrix carries them)...

# Fold the measured timings back in; the next plan is better balanced.
testbucket ingest --store test-timings.json /tmp/ev/*.ndjson

# Prove the run executed exactly what the plan scheduled.
testbucket audit --shard-plan plan.json /tmp/ev/*.ndjson
```

`--k` is the **single knob**: adding a lane is bumping K and nothing else.

## How it works

**Karmarkar-Karp k-way partition.** The balancer is Karmarkar-Karp (largest
differencing), deterministic down to the tie-break, so the same store and the
same K always produce the same buckets. Its objective is the SUM of a bucket's
unit times, so by default every emitted invocation runs its units serially
(`-p=1` for Go, `--no-file-parallelism` for Vitest) — which is what makes that sum
the job's actual wall time rather than a proxy for it. (LPT is kept one function
away as the reference the KK partition is measured against in the tests.)

`--file-parallelism N` (N>1) opts out, rendering `-p=N` / `--maxWorkers=N` so a
bucket uses more of its cores. It is a deliberate trade: a parallel bucket
finishes nearer its heaviest unit than its sum, so the sum-of-weights estimate
over-reads and the timings a `record` job ingests under it contend and are less
comparable. Prefer bumping `k` first; reach for this only when lanes are scarcer
than cores.

**Rolling timing store.** A JSON store keyed by unit identity (the Go import
path) carries an EWMA-smoothed weight per unit. `ingest` folds each run in with
`new = alpha*measured + (1-alpha)*old`, so a single slow runner nudges the split
instead of rewriting it. The store records the flag set the weights were
measured under (e.g. `-race -count=100`) and cold-starts loudly when it changes
— the guard against the "renamed job → silently bad split" trap.

**Whale detection is data-driven.** A unit that alone exceeds `total/K` sets the
makespan, so it must be split before K can buy anything. `ingest` flags these
from the *measured* timings — never from a hardcoded list — and picks a split
mechanism by comparing the two on equal terms:

- **count-shard** (tier 2a): run the package whole but divide `-count` S ways
  (`K × -count=⌈base/S⌉`, coverage-equivalent in aggregate). Needs no per-test
  data, so it is the day-one harpoon for a package the store knows nothing about
  internally.
- **name-slice** (tier 2b): a `-run` subset of the package's runnables, packed
  by their recorded per-name weights. Needs per-test data; gives a genuinely
  finer cut. Its makespan can never fall below the single heaviest name, so it
  only wins when no name dominates.

A GOWORK=off / own-dir module packs as one whole-module atom; workspace-mode
packages mix freely across module lines for balance.

**The never-drop coverage gate + audit.** `plan` re-derives coverage from the
FINAL buckets (not the expander's bookkeeping) and fails closed on any gap: a
package in no bucket, a runnable in no slice, a missing count-shard, a shard
group that doesn't add back up to the requested `-count`, a unit whose rendered
command wouldn't run what it claims. `audit` then checks the *captured events*
against the plan — catching what the gate structurally cannot: a bucket whose
job produced no events, an artifact that failed to upload.

## Architecture: the runner-adapter seam

```
internal/core               the language-agnostic engine (imports only the seam + stdlib)
internal/runner             the Runner interface + the value types that cross the seam
internal/runner/gorunner    the Go adapter (adapter #1): go list / go test -json / go test
internal/runner/vitestrunner the Vitest adapter (adapter #2): vitest list --filesOnly (glob) / vitest run --reporter=json
cmd/testbucket              CLI wiring: parse flags, build the adapter, call the core
```

`internal/core` knows nothing about `go`, `go test`, `go list`, GOWORK, or any
toolchain (`go list -deps ./internal/core` is clean of all of it). Everything
framework-specific reaches it through the interface, so a second adapter reuses
the engine unchanged.

### The `Runner` interface

No method takes a framework-specific argument. An adapter's own run
configuration — its flags, timeout, sweep count, setup detection — lives inside
the adapter (supplied when it is constructed), never on the core or in this
interface.

```go
// internal/runner/runner.go
type Runner interface {
	// Discover enumerates the live test targets — for Go, `go list ./...` over
	// the module set. Honours the context.
	Discover(ctx context.Context) ([]LivePackage, error)

	// Runnables enumerates the selectable top-level runnable names inside one
	// target — for Go, `go test -list` — used to name-slice a whale.
	Runnables(ctx context.Context, p LivePackage) ([]string, error)

	// ParseTimings folds one or more of a run's timing streams — for Go,
	// `go test -json` — into a RunSummary the store ingests.
	ParseTimings(readers ...io.Reader) (*RunSummary, error)

	// Render turns one planned bucket into the concrete invocations and shell
	// script the CI job runs, using the adapter's own render config. Pure.
	Render(b Bucket) Rendered

	// ValidateUnit is the command-grammar half of the never-drop gate. baseCount
	// is the neutral sweep base the core is planning at, so the per-unit
	// "weakens the sweep" check agrees with the core's aggregate check.
	ValidateUnit(u Unit, live map[string]LivePackage, baseCount int) []string

	// CanonicalToken renders the opaque comparability token weights are measured
	// within (for Go, "-race -count=100"). The core treats it as a key and never
	// inspects it; the store cold-starts when it changes.
	CanonicalToken() string
}
```

The value types (`LivePackage`, `Unit`, `Bucket`, `Invocation`, `RunSummary`,
`Rendered`) also live in `internal/runner`. The core reads a target through
**three neutral fields only** — `ID` (identity / store key), `Atom` (the
co-scheduling group; empty means "mixes freely"), and `HasTests`. Everything
else on `LivePackage` (`Dir`, `Module`, `Mode`) is **backing data the owning
adapter populates** for its own renderer; the core never reads it. The Go
adapter fills those; a Vitest adapter leaves them zero and sets only `ID`,
`Atom`, `HasTests`.

### What is core vs adapter

| Concern | Lives in | Why |
|---|---|---|
| KK / LPT k-way partition | core | project-agnostic math |
| Rolling store: load / EWMA-merge / persist, cold-start mean | core | framework-neutral |
| Whale detection + count-shard/name-slice **modeling** | core | the split *policy* is cross-language |
| Never-drop coverage gate (structural) + `audit` | core | about the abstract unit model |
| Plan orchestration, the K knob, the summary | core | |
| Discovery (`go list`) | gorunner | toolchain |
| Timing ingest (`go test -json` → per-unit Elapsed) | gorunner | toolchain |
| Invocation **rendering** (`-run`/`-count`/count-shard → a `go test` command) | gorunner | toolchain |
| Command-grammar validation (the `-run`/`-count`/GOWORK checks) | gorunner | knows Go's command grammar |
| **Run configuration** (`-race`, `-timeout`, sweep count, node detection) | gorunner | framework-specific; supplied to the adapter, never to the core |
| Comparability token | gorunner | built from the adapter's own config |

## Adding a framework adapter

A new adapter is a sibling package under `internal/runner/` that implements the
six-method `Runner` interface against the same value types, holding its own run
config. Nothing in `internal/core` changes. To add, say, a Vitest adapter:

1. **Discover** — enumerate the test targets (Vitest spec files) as
   `[]runner.LivePackage`, setting only the neutral fields:

   ```go
   runner.LivePackage{ID: "web/login.spec.ts", Atom: "web", HasTests: true}
   ```

   `ID` is the identity your timings report; `Atom` is the co-scheduling group
   (empty = mixes freely; a Vitest project, say, to force a set together). Leave
   `Dir`/`Module`/`Mode` zero — those are the Go adapter's backing fields.
2. **Runnables** — for a target flagged `split=run`, return the selectable
   test-case names your runner's name filter can select. The never-drop gate
   checks the name slices against this set.
3. **ParseTimings** — parse your runner's machine-readable output (Vitest's JSON
   reporter) into a `runner.RunSummary`: per-target seconds and run counts,
   per-runnable (top-level) seconds, the failed/no-test sets. Weigh only
   top-level runnables — a parent's elapsed already includes its children.
4. **Render** — turn a `runner.Bucket` into the concrete commands using **your
   adapter's own config** (a Vitest `--testTimeout=10000`, say — a millisecond
   integer the core never sees, let alone parses as a Go duration), plus a
   `Script` and `NeedsNode`.
5. **ValidateUnit** — the command-grammar checks specific to your runner; the
   core hands you `baseCount` so a per-unit sweep check agrees with its
   aggregate one. Return one message per defect.
6. **CanonicalToken** — the opaque token your weights are comparable within,
   built from your own config.

Then wire it in a CLI (or reuse `cmd/testbucket`'s flow, selecting the adapter).
The engine — partition, store, whale policy, coverage gate, audit, K knob — is
inherited whole.

This is not aspirational: `internal/core/stub_adapter_test.go` is a minimal
Vitest-shaped adapter that plans a real matrix through `core.BuildPlan` **without
importing `gorunner`, without touching `internal/core`, and with a bare `10000`
timeout** — the compile-checked proof that the seam is genuinely
framework-agnostic.

The full worked example is **`internal/runner/vitestrunner`** (adapter #2). It
discovers by **glob** (`vitest list --filesOnly --json`), weighs files — and, for
whales, individual tests — from the Vitest JSON reporter, and renders `vitest run
<files>` for whole files plus `vitest run -t '<regex>' <file>` for each name slice
— bucketing at **file granularity**, then **name-slicing** a whale spec across
buckets by test name. Its end-to-end test discovers a real sample project
(`testdata/vitest-sample`), runs the tool's own emitted commands, ingests the
timings, and re-plans into a time-balanced split that slices the whale. It was
added with **zero changes to `internal/core`**.

Name-slicing turns on a subtlety of Vitest naming: a test's name has three
divergent forms — `vitest list`'s `" > "`-joined path, the JSON reporter's
space-joined `fullName`, and the string `-t` matches — so the adapter keys
everything on the first, renders a `-t` robust to the ambiguity between them (each
`" > "` becomes `(?: > | )`, each title regex-escaped), and refuses to slice a
file whose names collide under that projection. `ValidateUnit` still refuses a
count-shard: Vitest has no repeat sweep to divide.

The one place name-slicing needs per-test **names** — a whale's slice universe —
it gets them without reopening the collection deadlock glob discovery avoids: it
runs `vitest list --json --project <the whale's project>`, scoping the
(necessarily importing) call to the file's own project so a *sibling* project's
hang cannot reach it. The file→project map comes from the same deadlock-safe glob.
So the never-drop gate sees a whale's brand-new test live, even under the glob
default.

### Vitest discovery: glob, timeout, and the `--vitest-command` contract

**Glob discovery is the default and needs no collection.** `vitest list --json`
imports the whole module graph to enumerate per-test names; on a multi-project
config that collection can **deadlock**, hanging `plan` indefinitely. So discovery
(the file *list*) uses `vitest list --filesOnly --json` — the CLI surface of
Vitest's `globTestSpecifications()`, which resolves each project's include/exclude
by glob **without importing a line of test code**. It is immune to the collection
hang and returns in ~1 s on suites where `list` takes minutes.

- `--vitest-discovery glob` (default) | `list`. `list` is the importing
  full-collection *discovery* path and re-exposes the multi-project deadlock, so
  it is opt-in and rarely needed. Name-slicing's per-test **names** do NOT come
  from it — they come from a project-**scoped** `vitest list --json --project
  <name>` (see above), so slicing works under the glob default without importing
  a sibling project.
- `--discovery-timeout 180s` (env **`TB_DISCOVERY_TIMEOUT`**, a Go duration; `0`
  disables) bounds discovery specifically and **fails fast with a clear error**
  rather than hanging the job. It is separate from `--timeout` (the `go test`
  run budget) so a stalled discovery can't hide behind a generous run deadline.
  The subprocess is run in its own process group and killed as a group on timeout,
  so a deadlocked `node` worker tree cannot keep the deadline from firing. The same
  budget bounds the project-scoped slice-name listing.

**The `--vitest-command` contract.** testbucket treats `--vitest-command` as
*program + leading args* (whitespace-split) and **appends** the subcommand itself:

| path | testbucket runs |
|---|---|
| discovery (glob) | `<vitest-command> list --filesOnly --json` |
| discovery (list) | `<vitest-command> list --json` |
| slice-name listing | `<vitest-command> list --json [--project <name>]` |
| a run bucket | `<vitest-command> run [--no-file-parallelism \| --maxWorkers=N] [-t '<regex>'] <files> [--reporter=…]` |

So the command must behave like **bare `vitest`**: accept `list`, `run`,
positional files, `--filesOnly`, `--project`, `--no-file-parallelism`,
`--maxWorkers`, `-t`, `--reporter`, `--json`. A wrapper that hard-codes its own
subcommand (e.g. one that always runs `vitest run`) does **not** satisfy this —
testbucket would append a second `run` or `list`. For that case,
**`--vitest-discovery-command`** takes a command run **verbatim** (it owns its
subcommand and flags; testbucket appends nothing) and must print the same
`[{file}]` / `[{name,file}]` JSON on stdout — letting a run-wrapper be paired with
a separate discovery command without a second façade.

## Complete-action wall time

Everything above balances buckets by the timing store: a rolling EWMA of what
the *reporter* said each file took. That is a good split and a bad measurement.
It cannot tell you how long the **action** took — install, setup, script
preparation, every invocation and its whole process tree, the gaps between
them, the epilogue — and a number that leaves work out cannot be the thing you
optimise.

`testbucket wall` measures the complete action, and it is **opt-in**: without
`--wall-dir` / `wall-time-dir`, every rendered byte, every matrix field and
every action step is exactly what it was.

### What it records

Three producers, three ledgers, one lifecycle, at each of three levels:

| Level | Physical envelope | Containment peer | Independent trace |
| --- | --- | --- | --- |
| the whole action | `AT` | `CPA` | `VTA` |
| the generated bucket script | `VB` | `CPB` | `VTB` |
| each rendered invocation | `V` | `CPV` | `VT` |

The **physical wrapper** owns the complete envelope, including every cost
inside it: creating the containment, starting the two observers, waiting,
reaping, flushing. The **containment peer** and the **trace collector** are
separate processes with their own signing keys; each takes its own
`clock_gettime(CLOCK_MONOTONIC)` reads and its own raw `cgroup.events`
observations, and neither can see the other's records. The wrapper drives them
in an order that makes the endpoint containment hold by construction:

```text
AT_start <= CPA_start <= VTA_start <= VTA_end <= CPA_end <= AT_end
```

The reconciliation gate then compares **like for like** — a trace against its
own peer, both bracketing the same admission-to-verified-empty lifecycle —
never a trace against the longer physical envelope, which would fail a
correctly instrumented run for the crime of accounting for its own bootstrap.

### It fails closed

- No delegated cgroup-v2 subtree (`TB_WALL_CGROUP_ROOT`)? The run is recorded
  in full and reported **INELIGIBLE**. A process group cannot prove that no
  descendant escaped.
- Not Linux? Same answer, for the same reason, plus a clock that is not
  `CLOCK_MONOTONIC`.
- A missing endpoint is a **missing interval**, never a shorter one. A crash, a
  cancellation, an escaped descendant, or a root that exited with live children
  stays terminal and retained; it never becomes a duration.
- A rewritten record breaks a hash chain the verifier recomputes.
- `wall verify --require complete` asks whether the records are well formed;
  `--require eligible` asks whether this **row** may be scored, which
  additionally needs a real clock, a real containment, signatures, the frozen
  Stage-1 and Stage-2 documents re-derived rather than believed, and every
  row-scope gate inside its threshold. Absent evidence never passes either.
- Gates carry a **scope**. An individual error limit is decided by one row; a
  mean over eighty rows is not, and `wall verify` reports those as
  `campaign`-scope without ever passing them. `wall campaign` decides those,
  and it checks the population first: five pairs, ten runs, eighty action rows
  at eight buckets each, three UTC dates inside fourteen days, every run
  retained with its terminal state, and one verifier verdict named per row.
  Five pairs of half-sized runs is not most of a campaign — it is a different
  denominator, and it fails.
- Authority approval needs a **predeclared** key
  (`wall verify --authority-key`). Verifying a signature against whatever
  signed the document accepts any self-generated key, so a run with no
  predeclared authority is reported ineligible rather than trusted.

### A reproducible plan

```sh
# Freeze every planning input — the canonical instant, the raw discovery and
# runnable bytes, the store bytes, the acquisition closure. The ONLY live read.
testbucket wall bundle --out bundle.json --root . --k 8 --wall-dir /tmp/tb-wall

# Authorise it (the key comes from TB_WALL_AUTHORITY_KEY, never a flag).
testbucket wall stage1 --bundle bundle.json --out stage1.json --role candidate \
  --action-commit "$SHA" --review-tip "$SHA" --source-profile profile.json

# Plan from the bundle and nothing else: no clock, no discovery, no listing.
testbucket plan --wall-bundle bundle.json --wall-stage1 stage1.json \
  --wall-stage2 stage2.json --shard-plan plan.json --json > matrix.json

# Replay it independently: this refuses to agree unless EVERY digest matches.
testbucket wall replay --bundle bundle.json --stage2 stage2.json --stage1 stage1.json
```

Two plan digests are recorded because they answer different questions. Moving
the canonical instant three days changes the **full-document** digest (the
store is older and the summary says so) and leaves the **semantic projection**
identical; renaming one discovered file changes both.

The Stage-2 receipt is written with `O_EXCL`. That is the exactly-once rule
made mechanical: the bound planner runs once, and a second run that quietly
replaced the first receipt would be indistinguishable from the first.

### Allocation, forecast and audit are three different numbers

- **`Palloc`** is the allocation score: `frozen_scorer(frozen_preplan_features)`,
  and nothing else. It is fitted offline from a sealed set of historical,
  wrapper-qualified `V` receipts (`wall train`); at runtime it may read no
  label and no outcome — including the timing store's own EWMA weight, which is
  reporter-derived and would leak an outcome into allocation through the side
  door. Pass `--palloc-scorer` and KK packs by it.
- **`est_seconds`** is unchanged: the store's measured weights, one decimal,
  numeric. Consumers read it; the split no longer has to.
- **`Pcheck`** is the post-render audit projection of those same frozen values
  over the renderer's membership. It cannot re-plan.
- **`Aeta`** is the pre-action forecast: a Stage-1 component template
  instantiated per bucket before the action starts. A phase nobody predicted is
  an ETA-completeness failure even when the trace agreed with itself to the
  microsecond.

### In a workflow

```yaml
uses: invakid404/testbucket/.github/workflows/bucketed-reusable.yml@<sha>
with:
  runner: vitest
  wall-time-dir: /tmp/testbucket-wall
  cgroup-root: /sys/fs/cgroup/testbucket   # create it in a prior step
```

Wall-time measurement is Vitest-only today. `--wall-dir` with `--runner go` is
**refused**, not ignored: a flag that silently does nothing is how a consumer
ends up believing a campaign was instrumented when it was not.

## Development

```sh
gofmt -l .          # formatting
go vet ./...        # vet
go build ./...      # build
go test ./... -race # the full suite (~210 tests)
```

## License

[The Unlicense](LICENSE) — this is free and unencumbered software released into
the public domain.
