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

The matrix and shard-plan JSON are **additive**: every field a consumer reads
today (`bucket`, `name`, numeric one-decimal `est_seconds`, `needs_node`,
`script`, `units`, `invocations`) keeps its meaning. Each `invocation` gained
`units`, `selector` and `atoms`, which is worth knowing if you validate the
plan artifact against a strict schema.

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
- Cancellation is **bounded and reaped**. A signal or the deadline sends
  SIGTERM to the whole containment, a 30s grace follows, and anything still
  alive is killed and reaped within 10s. A root that ignores TERM no longer
  hangs the job, and a descendant that outlived its root is killed rather than
  merely labelled — the escape stays terminal either way.
- Every record repeats the **full delivery identity**, and the verifier
  compares all of it across every record, the signer roster and the closing
  seal. A stream that is intact, signed and sealed but names another run,
  bucket, attempt, job or plan is two measurements in one directory, not one.
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
- Authority approval needs a **predeclared** key AND the **exact protected
  environment**. Verifying a signature against whatever signed the document
  accepts any self-generated key, so a run with no predeclared authority is
  reported ineligible rather than trusted — and a key can sign under any label,
  so `wall replay --stage1` also requires `--authority` and the eligible guard
  requires exactly `ewj2-campaign`. Checking the key alone would accept a
  correctly keyed manifest approved by some other environment, and this is the
  gate that runs before the measured action rather than after it.
- Scoring needs an **invocation manifest** and a **step attempt**. The first
  says what the authorised plan rendered, so the measured argv, selector, unit
  membership and atom closure are compared to it rather than merely recorded —
  two legal name slices of one file share a description and differ only there.
  The second is A_GH: GitHub reports whole seconds, so it is never a gate, but
  it says which step a ledger measured and accounts for the wrapper install
  that necessarily precedes AT_start.
- Scoring needs an **independent replay**. The Stage-2 receipt is the planner's
  own account of what it produced, and checking it against itself proves
  nothing, so `wall verify --replay` requires a signed attestation from a
  separate party that re-derived the same plan from the same frozen bytes
  (`wall replay --attest`). That attestation is retained and signed under the
  REPLAYING PARTY's identity, not the campaign authority's: naming the
  authority there would erase the very distinction the attestation exists to
  establish.
- `wall campaign --index` assembles its population from **verifier verdicts**,
  not from durations in a file: every row must be an `eligible: true` verdict
  that names the same campaign, run and Stage-1 manifest, and each pair's two
  signed manifests are compared field by field and may differ only in the
  enumerated candidate tuple. `--in` still runs the arithmetic on a
  hand-written file and always exits non-zero, because a number in a JSON file
  is not an observation.
- A campaign authorises the **delivery it was produced for**, by identity and
  by bytes. `wall release-manifest` derives the publish set ONCE from
  goreleaser's own artifact manifest — every asset a release uploads, hashed,
  plus the digest of every file inside each archive — and both the gate and the
  publisher read that one document. `wall campaign --release-sha …
  --release-manifest …` re-verifies it against the files on disk and requires
  every pair's candidate arm to have reviewed, been released from, and
  delivered a binary this release actually publishes: an asset itself, or a
  file inside one. A raw build intermediate that nothing uploads does not
  count. With no publish set supplied the gate does not pass: historical
  evidence stays auditable, and authorises nothing else.
- Scoring needs the **sealed training set**, not a digest of it. `wall verify
  --training-set` revalidates it under the training authority the Stage-1
  manifest predeclares and REFITS the scorer from it. A model that cites this
  evidence and a model built from it are otherwise indistinguishable, because
  the receipt-set digest is a string the model states about itself.
- The delivered binary needs a **signed build attestation**, not a sentence.
  Stage 1 verifies its subject digest against the binary it delivers, its
  source commit against the reviewed tip, its signature against a predeclared
  builder key, its signer against the builder it names, and its retained result
  against the only value that admits a delivery. The GitHub run and attempt are
  required, because a result bound to nothing is not provenance. `wall attest`
  produces one, and the release workflow attests every published asset.
- Every signature covers the **authority label recorded beside it**. The label
  used to sit outside the signed bytes, so a valid approval from an
  unprotected context could be relabelled as the protected campaign authority
  after the fact and every later comparison read a field its own signature did
  not cover.
- Signed documents are decoded **strictly**: unknown fields and trailing
  content are refused. Both are the same hazard — anything the decoder drops is
  outside the canonical digest, outside the signature, and invisible to every
  check downstream. For a training label, whose receipt hash addresses exact
  bytes, an appended second JSON value changes the hash the sealed set admits
  while the inner signature still covers only the first.
- Three roles, three key sets, none of them caller-supplied. The campaign
  authority approves Stage-1 inputs; the **verdict signers** Stage 1 declares
  sign verifier verdicts; the run signers it declares sign the roster and seal.
  A caller can no longer enlarge the signer set, and a verdict key cannot
  approve the inputs it judges.
- Every private signing capability is **scrubbed from observers**, and a test
  scans the whole tree for `TB_WALL_*KEY` rather than reading the denylist, so
  a capability introduced elsewhere and forgotten by the list is caught at the
  commit that introduces it.
- The **pre-flight** compares the Stage-1, Stage-2, registry and verifier
  identities the action will stamp on every record with the ones it derives
  itself, and fails the bucket before `wall begin`. All four are required, and
  the eligible guard refuses a scored request that omits any of them: an
  identity nobody supplied is not an identity that agrees. Refusing after the tests
  have run can invalidate a row; it cannot un-measure it.
- The publisher **re-resolves the release tag** immediately before uploading
  and refuses unless it still points at the campaign-gated commit. A tag is
  mutable, and GitHub ignores `--target` for a tag that already exists.
- Every training label carries the **exact bytes** of its physical-V receipt,
  its selected work and its topology validation — not three digests. The
  verifier hashes them, checks each against the reference that names it,
  verifies their signatures against the evidence authority the sealed set
  predeclares, and requires the receipt itself to be a passed, invocation-level,
  physical, containment-delimited observation of that unit at that duration.
  Refitting proves the coefficients follow the rows; only the evidence proves
  the rows are observations. Exclusions are matched against every identity a
  label carries — campaign, candidate, run and holdout, all in the signed
  receipt — and evidence decoding refuses unknown fields, so an identity the
  schema does not model cannot slip past the checks that read it.
- Every frozen listing binds the **closure of its own argv**: the exact
  command, cwd, planning-relevant environment, resolved executable path and a
  complete tool/version/integrity closure. An unresolved or empty identity is a
  bound fact about a failure, and `wall bundle` refuses to freeze one rather
  than planning on it. A launcher binds what it LAUNCHES: `pnpm exec tsx …`
  resolves the package-selected `tsx` shim from the project's own
  `node_modules/.bin`, because that is the program the façade actually runs. The source profile carries the exact façade, config and
  lockfile bytes, and its package closure is re-derived from the lock instead
  of read back from the receipt — the WHOLE closure, not the Vitest family
  within it, because a substituted transitive dependency changes what ran and a
  receipt that may omit it cannot tell the two trees apart. The closure is a
  multiset of resolved NODES keyed by the lock's own identity — both pnpm
  sections, so every peer context in `snapshots:` is a node of its own — and a
  version comes from the entry's own field rather than from its key. A node the
  lock does not pin is refused unless the receipt declares that exception and
  names the tarball. The parser identity in the receipt must be the identity of
  the parser that actually ran. The real adapter fixture is pinned to that same frozen
  version, and a test reads the committed manifest and lockfile so the pin
  cannot drift away from it.

### A reproducible plan

```sh
# Freeze every planning input — the canonical instant, the raw discovery and
# runnable bytes (and the names they parse to), the store bytes, the
# acquisition closure. This is the ONLY live read in the whole path.
testbucket wall bundle --out bundle.json --root . --k 8 --wall-dir /tmp/tb-wall

# Authorise it. Every identity the contract needs bound before either arm
# plans is required here, not discovered at verification time. The signing key
# comes from TB_WALL_AUTHORITY_KEY, never a flag.
testbucket wall stage1 --bundle bundle.json --out stage1.json --role candidate \
  --action-commit "$SHA" --review-tip "$SHA" --release-sha "$SHA" \
  --binary ./testbucket --build-attestation "$ATTESTATION" \
  --source-profile profile.json --store-receipt store-receipt.json \
  --scorer scorer.json --training-set training-set.json \
  --training-authority-key "$TRAINING_KEY" --registry registry.json \
  --runner-image "ubuntu-24.04@sha256:…" --consumer-repository owner/repo \
  --consumer-commit "$CONSUMER_SHA" --caller-workflow-sha "$WORKFLOW_SHA" \
  --downstream-ref "$REF"

# Plan from the bundle and nothing else: no clock, no discovery, no listing.
testbucket plan --wall-bundle bundle.json --wall-stage1 stage1.json \
  --wall-stage2 stage2.json --shard-plan plan.json --json > matrix.json

# Replay it independently and ATTEST the result. `wall verify` requires this:
# the receipt above is the planner's own account of its own output.
testbucket wall replay --bundle bundle.json --stage2 stage2.json \
  --stage1 stage1.json --authority-key "$KEY" --authority ewj2-campaign \
  --attest replay.json --verifier-id independent-verifier
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

Each bucket's records are uploaded as their own artifact, because the records
are the evidence: a row whose records went away with the runner cannot be
re-verified by anyone.

The reusable workflow references its own composite actions with GitHub's `$/`
self-repository syntax, so they resolve to **this** repository at the ref you
called — not to your checkout. Plain `./` would resolve against the workspace,
which for a called reusable workflow is *yours*. That needs a GitHub-hosted
runner or a self-hosted runner ≥ 2.336.0.

For a **scored** arm, add the campaign identities and hand the workflow the
frozen documents — the signed manifest, the one authorised Stage-2 receipt, the
independent replay attestation, the registry, and each bucket's forecast and
projection — as an artifact:

```yaml
with:
  runner: vitest
  wall-time-dir: /tmp/testbucket-wall
  cgroup-root: /sys/fs/cgroup/testbucket
  verify-require: eligible          # refuses an unmeasured or mutably-delivered row
  testbucket-version: v0.3.0        # an EXACT published tag; never an alias
  campaign-id: ewj2
  stage1-digest: sha256:…
  stage2-digest: sha256:…
  registry-digest: sha256:…
  authority: ewj2-campaign
  authority-key: <hex public key>
  frozen-documents-artifact: testbucket-frozen-candidate
```

Without that artifact, `verify-require: eligible` fails closed — the verifier
is being asked to prove something it has not been given, and saying so is the
right answer. So does an `eligible` request with no `wall-time-dir`: an
eligible row is a *measured* row, and the workflow refuses that contradiction
before any bucket runs rather than finishing green having proven nothing.

A_GH is not in that artifact and cannot be: `step-attempt.json` describes the
bucket step, which has not finished when the artifact is built. The workflow
reads it back from the Actions API after the run and hands it to the verifier —
so a scored caller must grant `actions: read` alongside `contents: read`:

```yaml
permissions:
  contents: read
  actions: read      # only needed for a scored arm, to collect A_GH
```

The workflow cannot request that for you: a called reusable workflow may only
narrow its caller's permissions, so declaring it here would break every caller
that grants less. Without it the collector says so and exits 0, and the
verifier reports the row ineligible.

**On `version`:** every action defaults to the moving `v0` alias, which
resolves to the highest published 0.x release — this project is deliberately
pre-1.0. A scored arm *must* pin an exact `vX.Y.Z`: an alias is descriptive
metadata rather than a delivery identity, and the installer downloads and
checksum-verifies a release asset, so a commit SHA — which has no asset — is
refused rather than advertised as deliverable.

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
