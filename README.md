# testbucket

**Self-optimizing, time-balanced test bucketing.** `testbucket` splits a
repository's unit tests into **K time-balanced buckets** and keeps the split
honest as the tests change. It is the mechanism behind a bucketed unit-test
workflow: `plan` turns a rolling timing store plus the live test set into a
GitHub-Actions matrix, and `ingest` folds each run's timings back into the store
so the next split is better than the last.

It is **framework-agnostic at its core** and speaks to a concrete test runner
through a small **runner-adapter seam**. The Go runner (`go test`) is adapter #1
and ships here; a second adapter (e.g. Vitest) implements the same interface
without touching the engine. See [Adding a framework adapter](#adding-a-framework-adapter).

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
unit times, so every emitted invocation runs its packages serially (`-p=1`) —
which is what makes that sum the job's actual wall time rather than a proxy for
it. (LPT is kept one function away as the reference the KK partition is measured
against in the tests.)

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
internal/core            the language-agnostic engine (imports only the seam + stdlib)
internal/runner          the Runner interface + the value types that cross the seam
internal/runner/gorunner the Go adapter (adapter #1): go list / go test -json / go test
cmd/testbucket           CLI wiring: parse flags, build the adapter, call the core
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

## Development

```sh
gofmt -l .          # formatting
go vet ./...        # vet
go build ./...      # build
go test ./... -race # the full suite (~112 tests)
```

## License

[The Unlicense](LICENSE) — this is free and unencumbered software released into
the public domain.
