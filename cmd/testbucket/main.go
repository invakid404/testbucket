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
	"strings"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/runner/gorunner"
	"github.com/invakid404/testbucket/internal/runner/vitestrunner"
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
  testbucket render           replay a "go test -json" stream from stdin as the
                              plain log it would have printed; a pure filter that
                              never changes an exit status
  testbucket version          print the build version (a released binary reports
                              its tag; a checkout reports "dev")

Most subcommands take --runner go|vitest to pick the test-runner adapter.
run "testbucket <subcommand> -h" for the flags of each.
`

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
	root          string
	vitestCommand string
	// Shared by both adapters.
	eventsDir string
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
		// The Go adapter splices --timeout verbatim into `go test -timeout`; the
		// Vitest adapter wants a real duration. Parse the same flag so one
		// --timeout serves both ("" leaves the Vitest deadline disabled).
		var timeout time.Duration
		if s := strings.TrimSpace(cfg.timeout); s != "" {
			d, err := time.ParseDuration(s)
			if err != nil {
				return nil, nil, fmt.Errorf("--timeout %q: %w", cfg.timeout, err)
			}
			timeout = d
		}
		r, err := vitestrunner.New(vitestrunner.Options{
			Root:      root,
			Command:   splitCommand(cfg.vitestCommand),
			Timeout:   timeout,
			EventsDir: cfg.eventsDir,
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

// splitPrefixes turns a comma-separated flag into a prefix list, empty for the
// empty string (the default) rather than a single empty prefix.
func splitPrefixes(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func runPlan(args []string) error {
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
	staleAfter := fs.Duration("stale-after", 14*24*time.Hour, "warn when the store was recorded longer ago than this (0 disables)")
	toolchainTimeout := fs.Duration("toolchain-timeout", 10*time.Minute, "deadline for each `go` subprocess (go work edit / go list / go test -list); 0 disables")
	runnerKind := fs.String("runner", "go", "test-runner adapter: go or vitest")
	root := fs.String("root", "", "vitest project directory (--runner vitest); empty means the working directory")
	vitestCommand := fs.String("vitest-command", "", "how to invoke vitest (--runner vitest); empty means \"npx vitest\"")
	var excludes stringList
	fs.Var(&excludes, "exclude-module", "module dir (glob) to leave out of the module set; repeatable, replaces the defaults")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// The effective sweep count is adapter-aware (Go 100, Vitest 1); resolve it
	// before validation, discovery or store access so a bad Vitest count fails
	// on a line of output rather than after a full discovery sweep.
	count, err := resolveCount(*runnerKind, *countFlag, flagWasSet(fs, "count"))
	if err != nil {
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
		kind:             *runnerKind,
		toolchainTimeout: *toolchainTimeout,
		excludes:         excludes,
		race:             *race,
		count:            count,
		timeout:          *timeout,
		nodePrefixes:     splitPrefixes(*nodePrefixes),
		root:             *root,
		vitestCommand:    *vitestCommand,
		eventsDir:        *eventsDir,
	})
	if err != nil {
		return err
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

	st, reason, err := core.LoadStore(*store)
	if err != nil {
		return err
	}

	opt.Live = livePkgs
	opt.Token = rnr.CanonicalToken()

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

func runIngest(args []string) error {
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
	root := fs.String("root", "", "vitest project directory (--runner vitest); empty means the working directory")
	vitestCommand := fs.String("vitest-command", "", "how to invoke vitest (--runner vitest); empty means \"npx vitest\"")
	var in stringList
	fs.Var(&in, "in", "go test -json file to ingest, or - for stdin; repeatable (extra positional args also count)")
	var excludes stringList
	fs.Var(&excludes, "exclude-module", "module dir (glob) to leave out of the module set; repeatable, replaces the defaults")
	if err := fs.Parse(args); err != nil {
		return err
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
		kind:             *runnerKind,
		toolchainTimeout: *toolchainTimeout,
		excludes:         excludes,
		race:             *race,
		count:            count,
		root:             *root,
		vitestCommand:    *vitestCommand,
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

func writeJSONFile(path string, v any) (err error) {
	f, cerr := os.Create(path)
	if cerr != nil {
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
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
