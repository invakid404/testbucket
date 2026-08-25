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
)

const usage = `testbucket — time-balanced unit-test bucketing

usage:
  testbucket plan   [flags]   compute K buckets and emit a GH-Actions matrix
  testbucket ingest [flags]   fold a run's timings back into the store
  testbucket whales [flags]   show the per-runnable distribution behind each
                              split decision, so the divisibility question can
                              be re-derived from any store
  testbucket audit  [flags]   check a finished run's captured events against
                              the plan it was fanned out from: every target
                              covered exactly as scheduled, shards and slices
                              accounted for

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
	count := fs.Int("count", 100, "-count for the flake sweep; count-shards divide it")
	timeout := fs.String("timeout", "20m", "-timeout passed to each go test invocation")
	live := fs.String("live", "", "read the live package set from this JSON file instead of running go list")
	nodePrefixes := fs.String("node-prefixes", "", "comma-separated package-dir prefixes whose buckets need Node set up (empty = none; a consumer opts in)")
	eventsDir := fs.String("events-dir", "", "if set, emitted invocations add -json and tee events into this directory")
	staleAfter := fs.Duration("stale-after", 14*24*time.Hour, "warn when the store was recorded longer ago than this (0 disables)")
	toolchainTimeout := fs.Duration("toolchain-timeout", 10*time.Minute, "deadline for each `go` subprocess (go work edit / go list / go test -list); 0 disables")
	var excludes stringList
	fs.Var(&excludes, "exclude-module", "module dir (glob) to leave out of the module set; repeatable, replaces the defaults")
	if err := fs.Parse(args); err != nil {
		return err
	}

	opt := core.PlanOptions{
		K:          *k,
		StorePath:  *store,
		Count:      *count,
		StaleAfter: *staleAfter,
		Now:        time.Now(),
	}
	// Validate before the expensive discovery: a bad --count should cost a line
	// of output, not a full `go list` sweep of every module.
	if err := opt.Validate(); err != nil {
		return err
	}

	ctx := context.Background()
	rnr, err := newGoRunner(gorunner.Options{
		ToolchainTimeout: *toolchainTimeout,
		Excludes:         excludes,
		Race:             *race,
		Count:            *count,
		Timeout:          *timeout,
		EventsDir:        *eventsDir,
		NodePrefixes:     splitPrefixes(*nodePrefixes),
	})
	if err != nil {
		return err
	}

	var livePkgs []runner.LivePackage
	if *live != "" {
		livePkgs, err = gorunner.LoadLivePackages(*live)
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
	count := fs.Int("count", 100, "the -count the measured run swept at (aggregate across shards)")
	whaleK := fs.Int("whale-k", 6, "flag a package as a split candidate once it exceeds total/K")
	whaleSeconds := fs.Float64("whale-seconds", 0, "absolute split threshold in seconds; overrides --whale-k")
	minShard := fs.Float64("min-shard-seconds", 30, "never slice a unit into pieces smaller than this; each slice costs a whole CI job's fixed overhead")
	live := fs.String("live", "", "read the live package set from this JSON file instead of running go list")
	noGoList := fs.Bool("no-golist", false, "skip go list; record coverage from the observed events only (no row pruning)")
	toolchainTimeout := fs.Duration("toolchain-timeout", 10*time.Minute, "deadline for each `go` subprocess; 0 disables")
	var in stringList
	fs.Var(&in, "in", "go test -json file to ingest, or - for stdin; repeatable (extra positional args also count)")
	var excludes stringList
	fs.Var(&excludes, "exclude-module", "module dir (glob) to leave out of the module set; repeatable, replaces the defaults")
	if err := fs.Parse(args); err != nil {
		return err
	}

	opt := core.IngestOptions{
		Alpha:           *alpha,
		Count:           *count,
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
	rnr, err := newGoRunner(gorunner.Options{
		ToolchainTimeout: *toolchainTimeout,
		Excludes:         excludes,
		Race:             *race,
		Count:            *count,
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
		livePkgs, err = gorunner.LoadLivePackages(*live)
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
	var in stringList
	fs.Var(&in, "in", "go test -json file to audit, or - for stdin; repeatable (extra positional args also count)")
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
	rnr, err := newGoRunner(gorunner.Options{})
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
