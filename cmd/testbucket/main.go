// Command testbucket splits a repository's unit tests into K time-balanced
// buckets and keeps the split honest as the tests change.
//
// It is the mechanism behind a bucketed unit-test workflow: `plan` turns a
// rolling timing store plus the live target set into a GitHub-Actions matrix,
// and `ingest` folds each run's timings back into the store so the next split
// is better than the last.
//
//	testbucket plan   --k 6 --store test-timings.json --json
//	testbucket ingest --in events.ndjson --store test-timings.json
//
// The command is the CLI wiring only: it parses flags, constructs the Go runner
// adapter (internal/runner/gorunner), and hands both to the language-agnostic
// engine (internal/core). Three properties are load-bearing and all live in the
// core: NEVER DROP A TEST (the coverage gate refuses an incomplete matrix),
// COLD START IS NORMAL (a store miss is a mean-weight estimate, not an error),
// and STALENESS IS NEVER SILENT (every plan prints a loaded-vs-missing summary).
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/runner/gorunner"
	"github.com/invakid404/testbucket/internal/runner/vitestrunner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// Build metadata, injected at release time via -ldflags -X (goreleaser fills
// these from the tag). They default to "dev" for a `go build` / `go install`
// off a checkout, so `testbucket version` always answers something sensible.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

const usage = `testbucket — time-balanced unit-test bucketing

usage:
  testbucket plan    [flags]  compute K buckets and emit a GH-Actions matrix
  testbucket ingest  [flags]  fold a run's timings back into the store
  testbucket whales  [flags]  show the per-runnable distribution behind each
                              split decision, so the divisibility question can
                              be re-derived from any store
  testbucket audit   [flags]  check a finished run's captured events against
                              the plan it was fanned out from: every target
                              covered exactly as scheduled, shards and slices
                              accounted for
  testbucket wall    <sub>    complete-action wall-time measurement: open and
                              close the physical action envelope, run a command
                              under a physical envelope with its own containment
                              peer and independent trace, and verify a records
                              directory against every frozen gate
  testbucket render           replay a "go test -json" stream from stdin as the
                              plain log it would have printed; a pure filter that
                              never changes an exit status
  testbucket version          print the build version (a released binary reports
                              its tag; a checkout reports "dev")

Most subcommands take --runner go|vitest to pick the test-runner adapter.
run "testbucket <subcommand> -h" for the flags of each.
`

// printVersion writes the build metadata: the tag for a released binary, "dev"
// for a plain checkout. The commit and date lines are omitted when unset so a
// dev build stays terse.
func printVersion() {
	fmt.Printf("testbucket %s\n", version)
	if commit != "" {
		fmt.Printf("commit: %s\n", commit)
	}
	if date != "" {
		fmt.Printf("built:  %s\n", date)
	}
}

// stringList collects a repeatable flag.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// newGoRunner builds the Go adapter. All Go run configuration — flags, sweep
// count, timeout, event capture, node detection — is the adapter's, so it is
// passed here and never reaches core.PlanOptions.
func newGoRunner(opt gorunner.Options) (*gorunner.Runner, error) {
	opt.Dir = "."
	return gorunner.New(opt)
}

// runnerConfig is the union of run configuration the CLI collects from flags for
// whichever adapter --runner selects. Each adapter reads only the fields it
// understands: the Go adapter its -race/-count/-timeout envelope, the Vitest
// adapter its project root and invocation. This union lives in cmd/ — not core —
// which is the seam working as intended: core never sees a framework-typed field.
type runnerConfig struct {
	kind             string
	toolchainTimeout time.Duration
	excludes         []string
	// Go run envelope.
	race         bool
	count        int
	timeout      string
	nodePrefixes []string
	// Vitest run envelope.
	wallDir                string
	root                   string
	vitestCommand          string
	vitestDiscovery        string
	vitestDiscoveryCommand string
	discoveryTimeout       time.Duration
	// Shared by both adapters.
	eventsDir       string
	fileParallelism int
}

// liveLoader reads the live target set from a JSON file using the schema of the
// adapter --runner selected — the Go and Vitest loaders differ, so the
// dispatcher hands back the right one already bound to its project root.
type liveLoader func(path string) ([]runner.LivePackage, error)

// newRunner constructs the adapter --runner names and returns it behind the
// neutral seam interface, together with its live-set loader. Adding a third
// adapter is a new case here and nothing in core.
func newRunner(cfg runnerConfig) (runner.Runner, liveLoader, error) {
	switch cfg.kind {
	case "", "go":
		r, err := newGoRunner(gorunner.Options{
			ToolchainTimeout: cfg.toolchainTimeout,
			Excludes:         cfg.excludes,
			Race:             cfg.race,
			Count:            cfg.count,
			Timeout:          cfg.timeout,
			EventsDir:        cfg.eventsDir,
			NodePrefixes:     cfg.nodePrefixes,
			FileParallelism:  cfg.fileParallelism,
		})
		if err != nil {
			return nil, nil, err
		}
		return r, gorunner.LoadLivePackages, nil
	case "vitest":
		root := cfg.root
		if strings.TrimSpace(root) == "" {
			root = "."
		}
		// Discovery is bounded by --discovery-timeout (a discovery-specific
		// deadline), NOT --timeout: --timeout is the `go test -timeout` run budget
		// and reusing it for discovery is what let a deadlocked `vitest list` hang
		// for the whole job.
		r, err := vitestrunner.New(vitestrunner.Options{
			Root:             root,
			Command:          splitCommand(cfg.vitestCommand),
			DiscoveryMode:    cfg.vitestDiscovery,
			DiscoveryCommand: splitCommand(cfg.vitestDiscoveryCommand),
			DiscoveryTimeout: cfg.discoveryTimeout,
			EventsDir:        cfg.eventsDir,
			FileParallelism:  cfg.fileParallelism,
			WallDir:          cfg.wallDir,
		})
		if err != nil {
			return nil, nil, err
		}
		loader := func(path string) ([]runner.LivePackage, error) {
			return vitestrunner.LoadLivePackages(root, path)
		}
		return r, loader, nil
	default:
		return nil, nil, fmt.Errorf("unknown --runner %q (want \"go\" or \"vitest\")", cfg.kind)
	}
}

// splitCommand parses a --vitest-command string into argv on whitespace, empty
// for the empty string so the adapter applies its own default (["npx","vitest"]).
// A command that needs embedded spaces in a single argument is out of scope — a
// wrapper script on PATH covers that rare case.
func splitCommand(s string) []string {
	return strings.Fields(s)
}

// flagWasSet reports whether the named flag was given on the command line (as
// opposed to left at its default). flag.FlagSet.Visit walks only the flags that
// were actually set.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// resolveCount applies the adapter-aware --count default and refuses a Vitest
// sweep that is not 1. The Go adapter keeps its historical default of 100 (a
// flake sweep count-shards divide); the Vitest adapter runs each test exactly
// once, so 1 is its only representable sweep — an unset count defaults to it,
// and any other explicit value is rejected HERE, before discovery, ingest, or a
// store write. That rejection is load-bearing for ingest: the adapter's
// per-unit ValidateUnit runs during planning but not during ingest, so without
// it a Vitest file recorded at count=100 would persist an impossible
// split=count*N policy that then fails the next plan's coverage gate closed.
func resolveCount(runnerKind string, count int, explicit bool) (int, error) {
	if runnerKind == "vitest" {
		if !explicit {
			return 1, nil
		}
		if count != 1 {
			return 0, fmt.Errorf("--runner vitest requires --count 1 (Vitest runs each test once), got %d", count)
		}
		return 1, nil
	}
	return count, nil
}

// checkWallDirRunner refuses --wall-dir on the Go adapter rather than ignoring
// it. Wall-time measurement is Vitest-only today: the Go adapter's events,
// count shards and `-race -count=100` contract are deliberately untouched, and
// a flag that silently does nothing is how a consumer ends up believing a
// campaign was instrumented when it was not.
func checkWallDirRunner(runnerKind, wallDir string) error {
	if strings.TrimSpace(wallDir) == "" || runnerKind == "vitest" {
		return nil
	}
	return fmt.Errorf("--wall-dir needs --runner vitest: complete-action wall-time measurement is Vitest-only today, and the Go adapter is deliberately left unchanged")
}

// splitPrefixes turns a comma-separated flag into a prefix list, empty for the
// empty string (the default) rather than a single empty prefix.
func splitPrefixes(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// defaultDiscoveryTimeout is the fail-fast budget for test discovery, used when
// neither --discovery-timeout nor TB_DISCOVERY_TIMEOUT is given.
const defaultDiscoveryTimeout = 180 * time.Second

// discoveryTimeoutDefault resolves the --discovery-timeout flag default from the
// TB_DISCOVERY_TIMEOUT env var (a Go duration such as "180s" or "3m"), falling
// back to defaultDiscoveryTimeout. A malformed or negative env value is a loud
// error rather than a silently-ignored setting. An explicit --discovery-timeout
// on the command line still overrides whatever this returns.
func discoveryTimeoutDefault() (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv("TB_DISCOVERY_TIMEOUT"))
	if v == "" {
		return defaultDiscoveryTimeout, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("TB_DISCOVERY_TIMEOUT %q: %w", v, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("TB_DISCOVERY_TIMEOUT %q must be >= 0 (0 disables the deadline)", v)
	}
	return d, nil
}

func runIngest(args []string) error {
	defDiscoveryTimeout, err := discoveryTimeoutDefault()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	store := fs.String("store", "test-timings.json", "timing store path to create or update")
	alpha := fs.Float64("ewma", 0.5, "EWMA smoothing factor: new = alpha*measured + (1-alpha)*old")
	race := fs.Bool("race", true, "the measured run used -race")
	countFlag := fs.Int("count", 100, "the -count the measured run swept at (aggregate across shards; Go default 100; Vitest requires 1)")
	whaleK := fs.Int("whale-k", 6, "flag a package as a split candidate once it exceeds total/K")
	whaleSeconds := fs.Float64("whale-seconds", 0, "absolute split threshold in seconds; overrides --whale-k")
	minShard := fs.Float64("min-shard-seconds", 30, "never slice a unit into pieces smaller than this; each slice costs a whole CI job's fixed overhead")
	live := fs.String("live", "", "read the live package set from this JSON file instead of running go list")
	noGoList := fs.Bool("no-golist", false, "skip go list; record coverage from the observed events only (no row pruning)")
	toolchainTimeout := fs.Duration("toolchain-timeout", 10*time.Minute, "deadline for each `go` subprocess; 0 disables")
	runnerKind := fs.String("runner", "go", "test-runner adapter: go or vitest")
	comparabilityKey := fs.String("comparability-key", "", "the §15.3 comparability-key digest this run's measurements belong to. Needed whenever the restored store is schema 1 or carries no `wall` yet; when it is empty the key is read from --wall-shard-plan, which already carries it. A migration that cannot name a key produces a population nothing can compare against")
	root := fs.String("root", "", "vitest project directory (--runner vitest); empty means the working directory")
	vitestCommand := fs.String("vitest-command", "", "bare-vitest invocation (--runner vitest); empty means \"npx vitest\". testbucket appends the subcommand (discovery: \"list --filesOnly --json\"); see `plan -h`")
	vitestDiscovery := fs.String("vitest-discovery", "glob", "vitest discovery mode (--runner vitest): glob (`vitest list --filesOnly`, no import) or list (`vitest list --json`); see `plan -h`")
	vitestDiscoveryCommand := fs.String("vitest-discovery-command", "", "override discovery with a command run VERBATIM (--runner vitest); must print the discovery JSON to stdout. Empty = derive from --vitest-command + --vitest-discovery")
	discoveryTimeout := fs.Duration("discovery-timeout", defDiscoveryTimeout, "fail-fast deadline for vitest test discovery (--runner vitest); 0 disables. Default overridable via TB_DISCOVERY_TIMEOUT")
	var in stringList
	fs.Var(&in, "in", "go test -json file to ingest, or - for stdin; repeatable (extra positional args also count)")
	var excludes stringList
	fs.Var(&excludes, "exclude-module", "module dir (glob) to leave out of the module set; repeatable, replaces the defaults")

	// --- contract §14.2 added ingest inputs ----------------------------------
	wallObservations := fs.String("wall-observations", "", "directory of wall observations to ingest (§14.2): each document is parsed, the §7.1 qualification checks are applied, qualifying rows are appended to the store's bounded ring under §15.1b, and the model is refit under §6.8 IF AND ONLY IF the append changed the selected trainable population. R54 uploaded a wall artifact that nothing downloaded; this is the reader that closes the loop")
	wallRunsOnLabel := fs.String("runs-on-label", "", "the caller's resolved runs-on label the plan was dispatched under. QC12 compares it against the label each bucket OBSERVED, and no context inside a composite action yields it, so the record job passes the same value the plan job did")
	wallShardPlan := fs.String("wall-shard-plan", "", "the plan artifact the measured run was fanned out from. REQUIRED with --wall-observations: §7.1's checks compare each observation against the PLAN — QC6 against the rendered invocations, QC13 against the canonical profile, QC17 against the declared runtime profile — and a context derived from the observations themselves would compare a row against itself")
	campaignConfig := fs.String("campaign-config", "", "job-local path to the frozen campaign config (§19.9a), already materialized and verified against its expected digest by the record job (§19.9c-1). Supplying it makes every matching row `trainable: false` AT APPEND, so no fit at any later time can consume a campaign or pilot row")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// §19.9c-1: the config is verified BEFORE ingest touches the store, so an
	// unverified config can never reach append.
	var verifiedConfig *walltime.VerifiedCampaignConfig
	if *campaignConfig != "" {
		content, err := os.ReadFile(*campaignConfig)
		if err != nil {
			return fmt.Errorf("read campaign config: %w", err)
		}
		expected := strings.TrimSpace(os.Getenv("TB_CAMPAIGN_CONFIG_DIGEST_EXPECTED"))
		cfg, err := walltime.VerifyCampaignConfig(content, expected)
		if err != nil {
			return err
		}
		verifiedConfig = &cfg
	}

	// Resolve the adapter-aware sweep count (Go 100, Vitest 1) up front. For
	// Vitest this REJECTS any count but 1 before the store is read or written, so
	// an impossible count-shard split can never be persisted (the adapter's
	// per-unit validation runs only during planning, not ingest).
	count, err := resolveCount(*runnerKind, *countFlag, flagWasSet(fs, "count"))
	if err != nil {
		return err
	}

	opt := core.IngestOptions{
		Alpha:           *alpha,
		Count:           count,
		WhaleK:          *whaleK,
		WhaleSeconds:    *whaleSeconds,
		MinShardSeconds: *minShard,
		Now:             time.Now(),
	}
	// Validate before touching any input or the store, so a bad --ewma can never
	// leave a half-merged store behind.
	if err := opt.Validate(); err != nil {
		return err
	}

	inputs := append([]string(nil), in...)
	inputs = append(inputs, fs.Args()...)
	if len(inputs) == 0 {
		return fmt.Errorf("no input: pass --in <go-test-json> (or - for stdin)")
	}

	ctx := context.Background()
	rnr, loadLive, err := newRunner(runnerConfig{
		kind:                   *runnerKind,
		toolchainTimeout:       *toolchainTimeout,
		excludes:               excludes,
		race:                   *race,
		count:                  count,
		root:                   *root,
		vitestCommand:          *vitestCommand,
		vitestDiscovery:        *vitestDiscovery,
		vitestDiscoveryCommand: *vitestDiscoveryCommand,
		discoveryTimeout:       *discoveryTimeout,
	})
	if err != nil {
		return err
	}

	var readers []io.Reader
	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			c.Close()
		}
	}()
	for _, p := range inputs {
		if p == "-" {
			readers = append(readers, os.Stdin)
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			return fmt.Errorf("open events %s: %w", p, err)
		}
		closers = append(closers, f)
		readers = append(readers, f)
	}

	sum, err := rnr.ParseTimings(readers...)
	if err != nil {
		return err
	}

	var livePkgs []runner.LivePackage
	authoritative := false
	switch {
	case *live != "":
		livePkgs, err = loadLive(*live)
		if err != nil {
			return err
		}
		authoritative = true
	case !*noGoList:
		livePkgs, err = rnr.Discover(ctx)
		if err != nil {
			return err
		}
		authoritative = true
	}

	token := rnr.CanonicalToken()
	st, reason, err := core.LoadStore(*store)
	if err != nil {
		return err
	}
	if st == nil {
		st = core.NewStore(token)
		if reason != "" {
			fmt.Fprintf(os.Stderr, "testbucket ingest: starting a new store (%s)\n", reason)
		}
	}

	opt.Live = livePkgs
	opt.LiveAuthoritative = authoritative
	opt.Token = token

	rep, err := core.ApplyIngest(st, sum, opt)
	if err != nil {
		// Nothing has been written; the restored store is left as it was.
		return err
	}

	// §15.2's FORWARD MIGRATION, inside this transaction and before the save.
	//
	// It used to be reachable only by calling MigrateWall directly, which
	// nothing in production did, while Save stamped `schema: 2`
	// unconditionally. So an ordinary reporter ingest over an existing
	// schema-1 store rewrote it as schema 2 with no `wall` object and no
	// marker: a later reader saw a nominally current store carrying no
	// evidence of the migration that never happened.
	//
	// The key is required, not optional: `wall` records WHICH history its rows
	// belong to, and a migration that could not say would create a population
	// nothing can compare against.
	//
	// THE PLAN IS PARSED ONCE, HERE, and the same document serves both the key
	// and the qualification context below. Re-reading the artifact would let
	// the key a store migrates under come from a different file than the one
	// its rows are qualified against.
	var planDoc *core.PlanDocument
	if strings.TrimSpace(*wallShardPlan) != "" {
		planDoc, err = core.ParseShardPlan(*wallShardPlan)
		if err != nil {
			return fmt.Errorf("--wall-shard-plan: %w", err)
		}
	}
	// THE KEY TRAVELS WITH THE PLAN when it is not passed explicitly.
	//
	// The record action has no `comparability-key` input and the component map
	// does not give it one, so nothing passed the flag and every schema-1 store
	// failed the migration above — breaking the ordinary v0.2.2 upgrade even
	// with no wall observations in sight. The plan already rides into the record
	// job and already carries §15.3's key, so it is the transport.
	key := strings.TrimSpace(*comparabilityKey)
	if key == "" && planDoc != nil {
		key = strings.TrimSpace(planDoc.ComparabilityKeyDigest)
	}
	switch {
	case st.NeedsWallMigration():
		if key == "" {
			return fmt.Errorf("this store is schema 1 and ingest must migrate it (§15.2), " +
				"but no comparability key is available: pass --comparability-key, or " +
				"--wall-shard-plan so the key travels with the plan. `wall` records which " +
				"history its rows belong to, and a migration that cannot name one produces " +
				"a population nothing can compare against")
		}
		st.MigrateWall(key)
	case key != "":
		// COLD START AND KEY CHANGE, in the one call that handles both.
		//
		// A store from core.NewStore has Wall == nil and is saved as schema 2,
		// so NeedsWallMigration is false and the branch above never runs: every
		// fresh store stayed wall-less and refused --wall-observations forever.
		// ResetWallForKeyChange initialises the absent object and, per §15.3's
		// second reset rule, clears the history when the key has moved — which
		// is why it runs BEFORE any row can be appended below.
		if n := st.ResetWallForKeyChange(key); n > 0 {
			fmt.Fprintf(os.Stderr,
				"wall: the comparability key changed; %d row(s) discarded and the model reset (§15.3)\n", n)
		}
	}

	if *wallObservations != "" {
		sources, err := walltime.ReadWallObservations(*wallObservations)
		if err != nil {
			return err
		}
		// THE LOOP, not a report of it.
		//
		// This used to print the accept/reject table and stop: every piece —
		// the §7.1 qualification, the ring append, the conditional refit —
		// existed and was tested, and nothing in production called any of
		// them, so no measured bucket could change a later plan. The hop below
		// qualifies each row, appends the ones that pass, and refits IF AND
		// ONLY IF the append changed the selected trainable population.
		if planDoc == nil {
			return fmt.Errorf("--wall-observations needs --wall-shard-plan: §7.1 compares each " +
				"observation against the plan it was fanned out from, and a context built from the " +
				"observations themselves would compare each row against itself")
		}
		if st.Wall == nil {
			return fmt.Errorf("--wall-observations needs a schema-2 store carrying `wall`, and " +
				"neither --comparability-key nor the plan named a comparability key, so §15.2's " +
				"initialisation had nothing to record the history under")
		}
		planCtx, frozen, err := planContextOf(planDoc, *wallRunsOnLabel, st)
		if err != nil {
			return err
		}
		ring := &wallRingStore{st: st, plan: planCtx, ring: ringFactsOf(st, planCtx), frozen: frozen}
		res, err := walltime.IngestWallObservations(sources, ring, verifiedConfig, wallFitter(st))
		if err != nil {
			return err
		}
		for _, d := range res.Decisions {
			verdict := "REJECT"
			if d.Accepted {
				verdict = "accept"
			}
			fmt.Fprintf(os.Stderr, "%-8s %-12s trainable=%-5v %s\n",
				verdict, d.BucketName, d.Trainable, d.Reason)
		}
		fmt.Fprintf(os.Stderr,
			"wall: %d of %d observation(s) appended; selected population %s; fitter called %d time(s)\n",
			res.Appended, len(sources),
			map[bool]string{true: "CHANGED", false: "unchanged"}[res.SelectionChanged],
			res.FitterCalls)
	}

	if err := st.Save(*store); err != nil {
		return err
	}
	if err := rep.Write(os.Stderr, core.CommonImportPrefix(livePkgs)); err != nil {
		return fmt.Errorf("write ingest report: %w", err)
	}
	return nil
}

func runWhales(args []string) error {
	fs := flag.NewFlagSet("whales", flag.ExitOnError)
	store := fs.String("store", "test-timings.json", "timing store to analyse, or - for stdin")
	k := fs.Int("k", 6, "bucket count the split threshold (total/K) is derived from")
	top := fs.Int("top", 8, "how many runnables to list per package")
	all := fs.Bool("all", false, "report every package with per-test rows, not just the split candidates")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *k < 1 {
		return fmt.Errorf("--k must be >= 1, got %d", *k)
	}
	if *top < 0 {
		return fmt.Errorf("--top must be >= 0, got %d", *top)
	}

	st, reason, err := core.LoadStore(*store)
	if err != nil {
		return err
	}
	if st == nil {
		return fmt.Errorf("no usable store: %s", reason)
	}

	if err := core.WriteWhaleReport(os.Stdout, st, *k, *top, *all); err != nil {
		return fmt.Errorf("write whales report: %w", err)
	}
	return nil
}

func runAudit(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	planPath := fs.String("shard-plan", "", "the plan artifact the run was fanned out from (required)")
	runnerKind := fs.String("runner", "go", "test-runner adapter whose event schema to parse: go or vitest")
	var in stringList
	fs.Var(&in, "in", "captured events file to audit, or - for stdin; repeatable (extra positional args also count)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *planPath == "" {
		return fmt.Errorf("--shard-plan is required: the audit compares what ran against what was planned")
	}

	inputs := append([]string(nil), in...)
	inputs = append(inputs, fs.Args()...)
	if len(inputs) == 0 {
		return fmt.Errorf("no input: pass the captured event files")
	}

	planned, err := core.LoadPlannedCoverage(*planPath)
	if err != nil {
		return err
	}

	// The audit never runs the toolchain; the adapter is here only for its
	// event parser. A missing repo root is therefore fine.
	rnr, _, err := newRunner(runnerConfig{kind: *runnerKind})
	if err != nil {
		return err
	}

	var readers []io.Reader
	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			c.Close()
		}
	}()
	for _, p := range inputs {
		if p == "-" {
			readers = append(readers, os.Stdin)
			continue
		}
		f, ferr := os.Open(p)
		if ferr != nil {
			return fmt.Errorf("open events %s: %w", p, ferr)
		}
		closers = append(closers, f)
		readers = append(readers, f)
	}
	sum, err := rnr.ParseTimings(readers...)
	if err != nil {
		return err
	}

	return core.AuditCoverage(os.Stdout, planned, sum)
}

// runRender replays a `go test -json` stream from stdin as the plain log `go
// test` would have printed: exactly the `output` events, in order. It is the
// CI-side twin of scripts/testbucket-render.sh, without a jq dependency, so the
// run-bucket action can show a human-readable log while the very same stream is
// teed to the events directory for a later ingest.
//
// It is a PURE FILTER and never fails the run: a line that is not a JSON event
// is passed through verbatim (nothing is ever hidden), a read error is reported
// to stderr but returns success, and the exit status the caller acts on is the
// status of the command on the LEFT of the pipe (PIPESTATUS[0]) — a happy
// renderer must never make a red test run look green.
func runRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	sc := bufio.NewScanner(os.Stdin)
	// `go test -json` output lines can be large (a failing test dumps its whole
	// log in one Output event); give the scanner room so a long line is rendered
	// rather than truncated with bufio.ErrTooLong.
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	var ev struct {
		Action string `json:"Action"`
		Output string `json:"Output"`
	}
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		ev.Action, ev.Output = "", ""
		if err := json.Unmarshal(line, &ev); err != nil {
			// Not a testbucket/`go test` event line — surface it unchanged so a
			// stray log line is never swallowed. Degrading beats failing.
			out.Write(line)
			out.WriteByte('\n')
			continue
		}
		if ev.Action == "output" {
			out.WriteString(ev.Output)
		}
	}
	if err := sc.Err(); err != nil {
		// A read error on instrumentation must not be the reason a job goes red.
		fmt.Fprintf(os.Stderr, "testbucket render: %v\n", err)
	}
	return nil
}

// writeJSONFile writes a derived artifact WITHOUT truncating an existing one.
//
// It used to be os.Create, which truncates. On the frozen planner path that
// meant a second invocation could overwrite the shard plan before anything
// discovered it was not allowed to run at all — the derived outputs were
// replaced and only then was the exactly-once rule consulted. Refusing here
// keeps the outputs of the one authorised derivation intact even if a later
// guard is bypassed, and the planner claim now refuses the second invocation
// before any of this is reached.
func writeJSONFile(path string, v any) (err error) {
	f, cerr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if cerr != nil {
		if os.IsExist(cerr) {
			return fmt.Errorf("%s already exists; a derived artifact is the output of one authorised derivation and is not overwritten", path)
		}
		return fmt.Errorf("create %s: %w", path, cerr)
	}
	// The close error is the one that matters here: a buffered short write
	// surfaces only on close, and a silently truncated --shard-plan artifact is
	// a debugging aid that lies.
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, closeErr)
		}
	}()
	// WRITTEN COMPACT, and that is not a style choice.
	//
	// The plan carries §13.0's canonical profile as raw bytes, and QC13
	// compares the observation's copy against these BYTE FOR BYTE. An indenting
	// encoder re-indents embedded raw JSON, so the artifact this planner wrote
	// no longer held the canonical block at all: the shipped
	// planner -> assembler -> ingest path rejected its own valid observation
	// with "observation profile block is not the plan's block verbatim", and
	// the learning loop appended nothing. A canonical value only stays
	// canonical if nothing on its path reformats it.
	enc := json.NewEncoder(f)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// scoredPlanAdmission enforces contract §19.3a's AD-8…AD-10 at the component
// §7 rule 17 names: a scored plan refuses to emit a matrix unless every one of
// these is present. An unscored run may omit them all and keeps every current
// default.
func scoredPlanAdmission(runnerClass, runsOnLabel, candidateSHA, workloadCommit, cacheDeclarationFile string) error {
	for _, f := range []struct{ flag, value, rule string }{
		{"--runner-class", runnerClass, "AD-8"},
		{"--runs-on-label", runsOnLabel, "AD-8"},
		{"--cache-declaration-file", cacheDeclarationFile, "AD-9"},
		{"--candidate-sha", candidateSHA, "AD-10"},
		{"--workload-commit", workloadCommit, "AD-10"},
	} {
		if strings.TrimSpace(f.value) == "" {
			return fmt.Errorf("scored plan refused: %s is empty (%s, contract §19.3a); no matrix is emitted",
				f.flag, f.rule)
		}
	}
	return nil
}

func runPlan(args []string) error {
	defDiscoveryTimeout, err := discoveryTimeoutDefault()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	k := fs.Int("k", 6, "number of buckets (the single knob: adding a lane = bumping K)")
	store := fs.String("store", "test-timings.json", "timing store path, or - for stdin; a missing store is a cold start")
	asJSON := fs.Bool("json", false, "write the fromJSON matrix to stdout (summary then goes to stderr)")
	shardPlan := fs.String("shard-plan", "", "also write the full plan (buckets, invocations, summary) as JSON to this path")
	race := fs.Bool("race", true, "weights and invocations assume -race")
	countFlag := fs.Int("count", 100, "-count for the flake sweep; count-shards divide it (Go default 100; Vitest requires 1)")
	timeout := fs.String("timeout", "20m", "-timeout passed to each go test invocation")
	live := fs.String("live", "", "read the live package set from this JSON file instead of running go list")
	nodePrefixes := fs.String("node-prefixes", "", "comma-separated package-dir prefixes whose buckets need Node set up (empty = none; a consumer opts in)")
	eventsDir := fs.String("events-dir", "", "if set, emitted invocations add -json and tee events into this directory")
	fileParallelism := fs.Int("file-parallelism", 1, "intra-bucket file/package concurrency (#22): 1 keeps a bucket serial (the sum-of-weights model the balancer packs to); N>1 renders `-p=N` (Go) / `--maxWorkers=N` (Vitest), trading that estimate for more cores")
	staleAfter := fs.Duration("stale-after", 14*24*time.Hour, "warn when the store was recorded longer ago than this (0 disables)")
	toolchainTimeout := fs.Duration("toolchain-timeout", 10*time.Minute, "deadline for each `go` subprocess (go work edit / go list / go test -list); 0 disables")
	runnerKind := fs.String("runner", "go", "test-runner adapter: go or vitest")
	root := fs.String("root", "", "vitest project directory (--runner vitest); empty means the working directory")
	vitestCommand := fs.String("vitest-command", "", "bare-vitest invocation (--runner vitest); empty means \"npx vitest\". testbucket treats it as program + leading args (whitespace-split) and APPENDS the subcommand: discovery adds \"list --filesOnly --json\" (or \"list --json\" under --vitest-discovery=list); a run bucket adds \"run --no-file-parallelism <files>\". The command must therefore accept those like bare vitest")
	vitestDiscovery := fs.String("vitest-discovery", "glob", "vitest discovery mode (--runner vitest): glob (`vitest list --filesOnly` — resolves files by glob WITHOUT importing them, immune to the multi-project `vitest list` collection deadlock) or list (`vitest list --json` — imports the module graph; only its per-test names matter, which file-granularity bucketing does not use today)")
	vitestDiscoveryCommand := fs.String("vitest-discovery-command", "", "override discovery with a command run VERBATIM (--runner vitest): it OWNS its subcommand and flags (testbucket appends nothing) and must print the [{file}] / [{name,file}] JSON to stdout. Lets a run-wrapper that already owns `run` be paired with a separate discovery command. Empty = derive from --vitest-command + --vitest-discovery")
	discoveryTimeout := fs.Duration("discovery-timeout", defDiscoveryTimeout, "fail-fast deadline for vitest test discovery (--runner vitest); a stalled `vitest list` errors here instead of hanging the whole job. 0 disables. Default overridable via TB_DISCOVERY_TIMEOUT")
	wallDir := fs.String("wall-dir", "", "records directory for complete-action wall-time measurement (--runner vitest): every rendered invocation runs under `testbucket wall exec`, which gives it a physical envelope, a containment peer and an independent trace. Empty (the default) renders exactly the bytes v0.2.2 rendered")
	var excludes stringList
	fs.Var(&excludes, "exclude-module", "module dir (glob) to leave out of the module set; repeatable, replaces the defaults")

	// --- contract §21 added plan inputs --------------------------------------
	estBasis := fs.String("est-basis", string(core.BasisReporter), "which weight the partition is built from: reporter (default) or wall. Mode selection is contract §16.1; an explicit `wall` request with no fitted `ok` model is a hard error with no matrix (§0.8 outcome c), never a silent fallback")
	scored := fs.Bool("scored", false, "this plan belongs to a scored campaign run. Nothing else can derive it — basis does not imply it, since a scored B arm runs `reporter` — so it is explicit, and it gates §0.8's phase-2 veto plus AD-8…AD-10")
	runnerClass := fs.String("runner-class", "", "the stable execution-class leaf of the comparability key (§15.3). Required non-empty for a scored plan (AD-8); it replaces the former runner_name leaf, whose per-instance value had no cross-run stability contract")
	runsOnLabel := fs.String("runs-on-label", "", "the caller's resolved runs-on label, the producer for the runner_image_label key leaf. No context yields it inside a composite action, so the caller passes it; run-bucket echoes it and QC12 compares them (AD-8)")
	candidateSHA := fs.String("candidate-sha", "", "the testbucket commit the executing binary was built from, recorded as the observation's candidate_sha (AD-10, QC15)")
	workloadCommit := fs.String("workload-commit", "", "the consumer checkout this run executes against, recorded as the observation's workload_commit (AD-10, QC15)")
	cacheDeclarationFile := fs.String("cache-declaration-file", "", "job-local file holding the canonical cache declaration of §10.5.0, already verified against its expected digest by the plan job. plan validates only the declaration leaves and refuses before emitting a matrix; the outcome leaves do not exist yet (AD-9)")

	// --- contract §17.3a calibration ------------------------------------------
	calibrate := fs.Bool("calibrate", false, "propose the calibration plan set of §17.3a instead of emitting a matrix: the design rows a fit needs to reach rank, packed by the same deterministic KK the reporter basis uses")
	calibrationOut := fs.String("calibration-out", "", "write the proposed calibration plans here as JSON (required with --calibrate); the file is written atomically so a reader never sees a partial proposal")
	calibrationMaxPlans := fs.Int("calibration-max-plans", 8, "the bounded search budget of §17.3a: at most this many plans are proposed before the proposer returns NOT_FOUND_WITHIN_BUDGET rather than searching without end")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// §0.8's two ordered phases, and §19.3a's admission, run BEFORE any matrix
	// is emitted — §7 rule 17 names plan as the component that must refuse.
	basis := core.EstBasis(*estBasis)
	if !basis.Valid() {
		return fmt.Errorf("--est-basis %q: contract §5.1 permits exactly reporter or wall", *estBasis)
	}
	if *scored {
		if err := scoredPlanAdmission(*runnerClass, *runsOnLabel, *candidateSHA, *workloadCommit, *cacheDeclarationFile); err != nil {
			return err
		}
	}
	if *calibrate && strings.TrimSpace(*calibrationOut) == "" {
		return fmt.Errorf("--calibrate needs --calibration-out: a proposal nobody can read is not a proposal")
	}

	// THE CACHE DECLARATION IS VALIDATED, NOT MERELY NAMED.
	//
	// AD-9 requires a scored plan to carry one, and §10.5.0 fixes its leaves.
	// Reading it here means a declaration that is malformed, or that names a
	// mode and producer that cannot occur together, refuses BEFORE a matrix
	// exists rather than after every bucket has run under it.
	var cacheDecl *walltime.CacheDeclaration
	if strings.TrimSpace(*cacheDeclarationFile) != "" {
		raw, err := os.ReadFile(*cacheDeclarationFile)
		if err != nil {
			return fmt.Errorf("--cache-declaration-file: %w", err)
		}
		var d walltime.CacheDeclaration
		if err := json.Unmarshal(raw, &d); err != nil {
			return fmt.Errorf("--cache-declaration-file: %w", err)
		}
		if err := d.Validate(); err != nil {
			return fmt.Errorf("--cache-declaration-file: %w; no matrix is emitted", err)
		}
		cacheDecl = &d
	}

	// The frozen path takes over completely: a bundle carries K, the count, the
	// token, the clock and the render configuration, so honouring a flag here
	// as well would be a second, unbound source for the same input.
	// The frozen-bundle planning path is removed with the Stage-1/Stage-2
	// authority model: a bundle was authorised by a signed manifest and
	// derived through a one-shot planner claim, none of which exists now.

	// The effective sweep count is adapter-aware (Go 100, Vitest 1); resolve it
	// before validation, discovery or store access so a bad Vitest count fails
	// on a line of output rather than after a full discovery sweep.
	count, err := resolveCount(*runnerKind, *countFlag, flagWasSet(fs, "count"))
	if err != nil {
		return err
	}
	if *fileParallelism < 1 {
		return fmt.Errorf("--file-parallelism must be >= 1, got %d", *fileParallelism)
	}
	if err := checkWallDirRunner(*runnerKind, *wallDir); err != nil {
		return err
	}

	opt := core.PlanOptions{
		K:          *k,
		StorePath:  *store,
		Count:      count,
		StaleAfter: *staleAfter,
		Now:        time.Now(),
	}
	// Validate before the expensive discovery: a bad --count should cost a line
	// of output, not a full `go list` sweep of every module.
	if err := opt.Validate(); err != nil {
		return err
	}

	ctx := context.Background()
	rnr, loadLive, err := newRunner(runnerConfig{
		kind:                   *runnerKind,
		toolchainTimeout:       *toolchainTimeout,
		excludes:               excludes,
		race:                   *race,
		count:                  count,
		timeout:                *timeout,
		nodePrefixes:           splitPrefixes(*nodePrefixes),
		root:                   *root,
		vitestCommand:          *vitestCommand,
		vitestDiscovery:        *vitestDiscovery,
		vitestDiscoveryCommand: *vitestDiscoveryCommand,
		discoveryTimeout:       *discoveryTimeout,
		eventsDir:              *eventsDir,
		fileParallelism:        *fileParallelism,
		wallDir:                *wallDir,
	})
	if err != nil {
		return err
	}

	// THE STORE IS READ BEFORE DISCOVERY, because the basis decision depends on
	// it and an unusable one must refuse before an expensive `go list` or
	// `vitest list` sweep — not after.
	st, reason, err := core.LoadStore(*store)
	if err != nil {
		return err
	}

	// §0.8's two ordered phases, over the store that actually exists. An
	// explicit `--est-basis wall` with no fitted `ok` model is outcome (c): a
	// hard error with NO matrix, never a silent reporter fallback.
	status := core.WallStatusAbsent
	var fitted *core.WallModel
	if st != nil && st.Wall != nil {
		status = st.Wall.Status
		if f := st.Wall.Fit; f != nil {
			fitted = &core.WallModel{
				FixedNs:                   f.FixedNs,
				Scale:                     f.Scale,
				WholeInvocationOverheadNs: f.WholeInvocationOverheadNs,
				PerSliceOverheadNs:        f.PerSliceOverheadNs,
			}
		}
	}
	decision, err := core.SelectBasis(
		core.BasisRequest{Basis: basis, Explicit: flagWasSet(fs, "est-basis")},
		st != nil, status, *scored, *fileParallelism)
	if err != nil {
		return err
	}
	if decision.Basis == core.BasisWall && fitted == nil {
		return fmt.Errorf("%w: the store reports status ok but carries no fit",
			core.ErrWallModelUnusable)
	}
	if decision.ColdStart && decision.Reason != "" {
		fmt.Fprintf(os.Stderr, "testbucket plan: %s\n", decision.Reason)
	}

	var livePkgs []runner.LivePackage
	if *live != "" {
		livePkgs, err = loadLive(*live)
	} else {
		livePkgs, err = rnr.Discover(ctx)
	}
	if err != nil {
		return err
	}

	opt.Live = livePkgs
	opt.Token = rnr.CanonicalToken()
	opt.Basis = decision.Basis
	if decision.Basis == core.BasisWall {
		opt.WallModel = fitted
	}

	// THE RUNTIME PROFILE IS OBSERVED, not asserted: §15.3a's leaves come from
	// the toolchain this process can actually see, so a declaration and the
	// execution that follows it can be compared rather than assumed equal.
	rtProfile := buildRuntimeProfile(runtimeProfileInputs{
		runnerKind:    *runnerKind,
		root:          *root,
		workingDir:    *root,
		vitestCommand: *vitestCommand,
		cacheDecl:     cacheDecl,
	})
	opt.RuntimeProfileDeclared = runtimeProfileMap(rtProfile)
	opt.RuntimeProfileDeclaredDigest = string(walltime.RuntimeProfileDigest(rtProfile))

	// §15.3's comparability key: which wall history these measurements may
	// join. It is derived from the DECLARED leaves, never from a per-job
	// outcome, so ordinary cache behaviour cannot reset the population.
	keyProfile := walltime.ComparabilityProfile{
		RunnerClass:      *runnerClass,
		RunnerImageLabel: *runsOnLabel,
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		NodeVersion:      rtProfile.NodeVersion,
		PnpmVersion:      rtProfile.PnpmVersion,
		VitestVersion:    rtProfile.VitestVersion,
		TestbucketSHA256: rtProfile.TestbucketSHA256,
		WorkingDir:       *root,
		FacadeCommand:    rtProfile.FacadeCommand,
		SetupCommand:     "",
		LockSHA256:       rtProfile.LockSHA256,
		DiscoveryMode:    *vitestDiscovery,
		Exclusions:       []string(excludes),
	}
	if cacheDecl != nil {
		keyProfile.CacheDeclarationDigest = string(cacheDecl.Digest())
	}
	keyDigest, err := walltime.ComparabilityKeyDigest(keyProfile)
	if err != nil {
		return fmt.Errorf("comparability key: %w", err)
	}
	opt.ComparabilityKeyDigest = string(keyDigest)

	// §13.0's canonical profile: the shape this plan was built under, carried
	// verbatim into every observation so QC13 compares one value rather than
	// two derivations of it.
	expandedDigest := expandedUnitSetDigest(livePkgs)
	profile := walltime.CanonicalProfile{
		Scored:                *scored,
		RunnerToken:           rnr.CanonicalToken(),
		K:                     *k,
		Count:                 count,
		FileParallelism:       *fileParallelism,
		BucketIndices:         bucketIndices(*k),
		EstBasis:              walltime.EstBasis(decision.Basis),
		StoreSHA256:           storeDigest(*store),
		ExpandedUnitSetDigest: expandedDigest,
	}
	if *scored {
		if err := walltime.AdmitScoredProfile(profile, *runnerClass, *runsOnLabel,
			*candidateSHA, *workloadCommit, cacheDecl != nil); err != nil {
			return fmt.Errorf("%w; no matrix is emitted", err)
		}
	}
	// THE CANONICAL BYTES, not json.Marshal's. §13.0 has the observation copy
	// this block VERBATIM and QC13 compares it byte for byte, so the plan has
	// to carry the same serialization the observation will: an ordinary
	// marshal differs in key order and would fail the comparison it exists to
	// make meaningful.
	profileBlock, err := walltime.NewProfileBlock(profile)
	if err != nil {
		return fmt.Errorf("canonical profile: %w", err)
	}
	opt.Profile = profileBlock.Raw()
	opt.ExpandedUnitSetDigest = expandedDigest

	if *calibrate {
		return runCalibration(rnr, opt, *calibrationOut, *calibrationMaxPlans)
	}

	doc, err := core.BuildPlan(ctx, rnr, st, reason, opt)
	if err != nil {
		return err
	}

	if *shardPlan != "" {
		if err := writeJSONFile(*shardPlan, doc); err != nil {
			return err
		}
	}

	// The summary always reaches the job log; stdout stays machine-clean
	// whenever the caller is capturing the matrix.
	summaryOut := io.Writer(os.Stdout)
	if *asJSON {
		summaryOut = os.Stderr
	}
	if err := doc.WriteSummary(summaryOut, core.CommonImportPrefix(livePkgs)); err != nil {
		return fmt.Errorf("write plan summary: %w", err)
	}

	if *asJSON {
		matrix, err := doc.MatrixJSON()
		if err != nil {
			return err
		}
		// A short write here is the difference between a matrix and a truncated
		// one; `matrix=$(testbucket plan --json)` would happily consume the
		// fragment and fan out the wrong jobs.
		if _, err := fmt.Println(string(matrix)); err != nil {
			return fmt.Errorf("write matrix: %w", err)
		}
	}
	return nil
}

// main dispatches the subcommands that survive the removal plan. The wall
// verbs that went with the protected-authority, observer and campaign-proof
// models are no longer reachable; `wall` itself remains, because the rendered
// bucket script invokes `testbucket wall exec`.
func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "plan":
		err = runPlan(os.Args[2:])
	case "ingest":
		err = runIngest(os.Args[2:])
	case "whales":
		err = runWhales(os.Args[2:])
	case "audit":
		err = runAudit(os.Args[2:])
	case "render":
		err = runRender(os.Args[2:])
	case "wall":
		err = runWall(os.Args[2:])
	case "version", "--version", "-v":
		printVersion()
		return
	case "-h", "--help", "help":
		fmt.Fprint(os.Stderr, usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "testbucket: unknown subcommand %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "testbucket: %v\n", err)
		os.Exit(1)
	}
}
