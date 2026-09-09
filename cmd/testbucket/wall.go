package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/walltime"
)

// verifierKeyEnv is where the verifier's own signing key is read from, for
// the same reason the authority key is: a key on a command line is a key in
// the process table.
const verifierKeyEnv = walltime.VerifierKeyEnv

// replayKeyEnv is the INDEPENDENT replay party's own signing key. It is a
// separate variable from the authority key because the whole value of a replay
// is that a different party produced it; sharing one key would make the
// distinction editorial.
const replayKeyEnv = walltime.ReplayKeyEnv

const wallUsage = `testbucket wall — complete-action wall-time measurement

usage:
  testbucket wall begin   [flags]  open the physical action envelope (AT_start),
                                   its containment, and the independent CPA peer
                                   and VTA collector; leaves state for ` + "`wall end`" + `
  testbucket wall end     [flags]  close that envelope after verified-empty
                                   containment (AT_end)
  testbucket wall exec    [flags] -- cmd...
                                   run one command under a physical envelope
                                   (VB or V) with its own peer and collector
  testbucket wall run     [flags] -- cmd...
                                   run an action-owned command inside the action
                                   containment, with no envelope of its own (a
                                   per-bucket setup command)
  testbucket wall observe [flags]  INTERNAL: the peer/collector process itself
  testbucket wall hold    -- cmd...  INTERNAL: the action-owned child's barrier;
                                   it waits for the wrapper's containment proof
                                   and then execs the command in place
  testbucket wall verify  [flags]  verify a records directory and report
                                   eligibility, reconciliation and every gate
  testbucket wall bundle  [flags]  freeze a planning-input bundle: the canonical
                                   instant, the raw discovery and runnable bytes,
                                   the store bytes, and the acquisition closure
  testbucket wall replay  [flags]  independently replay a bundle through the
                                   planner and refuse to agree unless every
                                   digest matches the issued Stage-2 receipt
  testbucket wall stage1  [flags]  assemble and sign the Stage-1 input manifest
                                   that authorises a bundle (the signing key
                                   comes from TB_WALL_AUTHORITY_KEY, never a
                                   flag)
  testbucket wall stage1-binary [flags]  print the binary digest a signed
                                   Stage-1 manifest AUTHORISES, for the
                                   installer's mandatory candidate check
  testbucket wall check-delegation [flags]  verify that a cgroup-v2 subtree is
                                   really delegated — that this credential can
                                   create containments under it AND migrate
                                   into them
  testbucket wall digest  [flags]  print the canonical digest of a manifest,
                                   receipt, bundle, registry or scorer — the
                                   identity every record has to bind to
  testbucket wall train   [flags]  fit the frozen scorer from a sealed training
                                   receipt set of historical wrapper-qualified
                                   physical V labels
  testbucket wall verify-attestation [flags]  refuse an asset whose
                                    attestation does not authenticate against
                                    BOTH predeclared keys
  testbucket wall countersign [flags]  the VERIFIER'S independent signature
                                    over a builder attestation, after
                                    re-deriving the artifact's digest
  testbucket wall attest-runner [flags]  the FLEET'S signed statement that a
                                    host was booted from a named image, scoped
                                    to one run; a scored arm requires it
  testbucket wall attest   [flags]  produce the builder's SIGNED build
                                   attestation for one exact artifact: its
                                   subject digest, source, builder, issuer,
                                   verifier identity and retained result
  testbucket wall release-manifest [flags]
                                   derive the canonical publish set from
                                   goreleaser's own artifact manifest: every
                                   asset a release uploads, hashed, plus the
                                   digest of every file inside each archive
  testbucket wall campaign [flags] apply the frozen five-pair decision rule to a
                                   campaign of AUTHENTICATED rows: each arm's
                                   signed Stage-1 manifest and one eligible
                                   verifier verdict per bucket

Every endpoint is a fresh CLOCK_MONOTONIC read taken by the producer that
records it. A host with no delegated cgroup-v2 subtree (TB_WALL_CGROUP_ROOT)
still records everything, and ` + "`wall verify`" + ` reports the run INELIGIBLE rather
than scoring a lifecycle it cannot prove.
`

// runIdentityFlags collects the campaign/delivery keys every record carries.
// They are flags rather than environment sniffing so that what a record claims
// about its run is something the caller stated, not something the wrapper
// guessed from an ambient variable.
type runIdentityFlags struct {
	campaign, run, attempt, bucket string
	repository, workflowRun        string
	job, step, stepAttempt         string
	stage1, stage2                 string
	registry, verifier             string
}

func (f *runIdentityFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&f.campaign, "campaign-id", "", "campaign identity recorded on every record")
	fs.StringVar(&f.run, "run-id", "", "run identity")
	fs.StringVar(&f.attempt, "attempt-id", "", "attempt identity")
	fs.StringVar(&f.bucket, "bucket-id", "", "bucket identity")
	fs.StringVar(&f.repository, "repository", "", "GitHub repository")
	fs.StringVar(&f.workflowRun, "workflow-run", "", "GitHub workflow run id")
	fs.StringVar(&f.job, "job", "", "GitHub job id")
	fs.StringVar(&f.step, "step", "", "GitHub step id")
	fs.StringVar(&f.stepAttempt, "step-attempt", "", "GitHub step attempt id")
	fs.StringVar(&f.stage1, "stage1", "", "Stage-1 input manifest digest this run is bound to")
	fs.StringVar(&f.stage2, "stage2", "", "Stage-2 derived-plan receipt digest this run is bound to")
	fs.StringVar(&f.registry, "registry", "", "Aeta component-registry digest in force")
	fs.StringVar(&f.verifier, "verifier-id", "", "delivery-bound verifier identity")
}

func (f *runIdentityFlags) identity() walltime.RunIdentity {
	return walltime.RunIdentity{
		CampaignID: f.campaign, RunID: f.run, AttemptID: f.attempt, BucketID: f.bucket,
		Repository: f.repository, WorkflowRun: f.workflowRun, Job: f.job,
		Step: f.step, StepAttempt: f.stepAttempt,
		Stage1: walltime.Digest(f.stage1), Stage2: walltime.Digest(f.stage2),
		ComponentRegistry: walltime.Digest(f.registry), VerifierID: f.verifier,
		// THE EXECUTING HOST, OBSERVED HERE. These are read from the runner's
		// own environment on the machine that runs the row, not passed in by
		// the caller and not taken from any attestation: a fleet's signed
		// statement says what the fleet BOOTED, and which host executed this
		// matrix row is a different claim. The verifier compares the two, so
		// one statement naming a host can no longer be replayed across jobs
		// and buckets that never ran on it.
		RunnerName: strings.TrimSpace(os.Getenv("RUNNER_NAME")),
		RunnerOS:   strings.TrimSpace(os.Getenv("RUNNER_OS")),
		RunnerArch: strings.TrimSpace(os.Getenv("RUNNER_ARCH")),
	}
}

func runWallBegin(args []string) error {
	fs := flag.NewFlagSet("wall begin", flag.ExitOnError)
	dir := fs.String("dir", "", "records directory (required)")
	timeout := fs.Duration("timeout", walltime.DefaultTimeout, "bound on every wait; a lifecycle that cannot close becomes a terminal record")
	var ids runIdentityFlags
	ids.bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("--dir is required")
	}
	st, err := walltime.BeginAction(*dir, ids.identity(), *timeout)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: action envelope open (containment %s, %s)\n",
		st.Containment.ID, st.Containment.Primitive)
	// THE SIGNER DELEGATE: EXACTLY ONE LINE ON STDOUT.
	//
	// The script and invocation producers — and the action-owned children —
	// mint their signing keys during the measured step, where no run key
	// exists to authorize them. This is the capability that does, and it must
	// reach that step and nothing else. It is printed rather than written into
	// the evidence directory because the measured script can read that
	// directory and every observer is handed it as `--dir`: a capability left
	// where the reader is told to look is not isolated by scrubbing the
	// variable that names it.
	//
	// THE MASK DIRECTIVE GOES TO STDERR, and this is the whole of what stdout
	// carries. The caller captures stdout in a command substitution, so a
	// second line there is captured too: the runner never sees the workflow
	// command it was meant to process, and the captured value becomes
	// `::add-mask::<key>` followed by a bare line, which the environment-file
	// parser rejects and which no base64 decoder can read. The runner reads
	// workflow commands from the step's log, which stderr is part of, so the
	// mask still takes effect — and stdout stays exactly one decodable value.
	if st.SignerDelegate != "" {
		fmt.Fprintf(os.Stderr, "::add-mask::%s\n", st.SignerDelegate)
		fmt.Println(st.SignerDelegate)
	}
	return nil
}

func runWallEnd(args []string) error {
	fs := flag.NewFlagSet("wall end", flag.ExitOnError)
	dir := fs.String("dir", "", "records directory (required)")
	terminal := fs.String("terminal", "", "the action's own outcome: passed, failed, signalled, cancelled")
	reason := fs.String("reason", "", "why, for a non-passed outcome")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("--dir is required")
	}
	st, err := walltime.EndAction(*dir, *terminal, *reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: action envelope closed (containment %s)\n", st.Containment.ID)
	return nil
}

func runWallRun(args []string) error {
	fs := flag.NewFlagSet("wall run", flag.ExitOnError)
	dir := fs.String("dir", "", "records directory (required)")
	cwd := fs.String("cwd", "", "working directory for the command")
	// THE CAPABILITY BOUNDARY, DECLARED AT THE CALL SITE.
	//
	// Two very different commands run through `wall run`: the bucket command,
	// which is this tool's own wrapper starting the measured script and needs
	// the wall-time capabilities to do it, and the consumer-supplied setup
	// command, which is somebody else's code and needs none of them. The
	// default is the scrubbed environment, so a caller that does not think
	// about it does not hand a signing capability to code it did not write.
	wrapperChain := fs.Bool("wrapper-chain", false,
		"this child continues the wrapper chain and needs the wall-time capabilities (the bucket command). Without it the child runs with every wall-time secret and account selector removed, which is what a consumer-supplied setup command must get")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("--dir is required")
	}
	if len(fs.Args()) == 0 {
		return fmt.Errorf("no command: pass it after --")
	}
	code, err := walltime.RunInActionWith(walltime.RunInActionOptions{
		Dir: *dir, Argv: fs.Args(), Cwd: *cwd,
		Stdout: os.Stdout, Stderr: os.Stderr, WrapperChain: *wrapperChain,
	})
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

// builderKeyEnv is where the builder's signing key is read from — an
// environment variable rather than a flag, for the same reason the authority
// key is: a key on a command line is a key in the process table.
const builderKeyEnv = walltime.BuilderKeyEnv

// verdictSigningIdentity is the identity a machine-readable verdict must be
// signed under: the DELIVERY VERIFIER the measured records name.
//
// A signature covers `authority NUL digest`, and the retained authority is the
// party the signature was made under. LoadCampaign requires that party to be
// the delivery verifier the verdict's own body names, because a verdict signed
// under some other identity attributes a row to somebody who did not verify
// it.
//
// This used to be the --authority value, which in the scored workflow is the
// protected Stage-1 environment `ewj2-campaign`, while the body's verifier
// identity comes from the measured records and is `ewj2-verifier`. The
// producer therefore emitted, by construction, exactly the verdict the
// production campaign loader refuses — so no genuine population could ever
// have been assembled from real runs. --authority keeps its own job: saying
// which protected environment must have approved Stage 1.
func verdictSigningIdentity(v *walltime.Verdict) (string, error) {
	identity := strings.TrimSpace(v.Run.VerifierID)
	if identity == "" {
		return "", fmt.Errorf("these records name no delivery verifier identity, so there is nobody to sign this verdict as; signing it under any other name would attribute the row to a party that did not verify it")
	}
	return identity, nil
}

// coverageAudit builds the verifier's exact-run coverage check.
//
// It lives here rather than in internal/walltime because the audit belongs to
// the planner/adapter layer, and the measurement package deliberately imports
// neither — the code that measures must not be able to reach the code it
// measures. Returning nil when the inputs are absent is not a way to skip the
// check: the verifier turns a nil audit into a finding.
func coverageAudit(shardPlan, eventsDir, runnerKind string) walltime.AuditFunc {
	if shardPlan == "" || eventsDir == "" {
		return nil
	}
	return func(bucketID string) (*walltime.AuditEvidence, error) {
		// ONE read, and everything below derives from it.
		//
		// The digest, the bucket lookup and the expected coverage all describe
		// "the plan", and a path re-read three times is three plans. Taking
		// the digest from the authorised file and the expected coverage from a
		// narrowed one substituted in between produces an audit that reports
		// the Stage-2-matching digest over a population that was never
		// planned — which is the substitution the digest exists to catch,
		// wearing the digest as a disguise.
		doc, err := core.ParseShardPlan(shardPlan)
		if err != nil {
			return nil, err
		}
		planDigest, err := walltime.DigestJSON(doc)
		if err != nil {
			return nil, err
		}
		index, err := core.BucketIndexIn(doc, bucketID)
		if err != nil {
			return nil, err
		}
		planned, err := core.PlannedCoverageForBucket(doc, index)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(eventsDir)
		if err != nil {
			return nil, fmt.Errorf("read events %s: %w", eventsDir, err)
		}
		var readers []io.Reader
		var closers []io.Closer
		defer func() {
			for _, c := range closers {
				c.Close()
			}
		}()
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			f, err := os.Open(filepath.Join(eventsDir, e.Name()))
			if err != nil {
				return nil, fmt.Errorf("open events %s: %w", e.Name(), err)
			}
			closers = append(closers, f)
			readers = append(readers, f)
		}
		if len(readers) == 0 {
			// An empty events directory is the exact failure the audit exists
			// to catch — a bucket that produced nothing — so it is reported as
			// a coverage problem rather than as a missing input.
			return &walltime.AuditEvidence{Bucket: bucketID, PlanDigest: planDigest, Planned: planned.Units, Problems: []string{
				fmt.Sprintf("bucket %s produced no runner events at all, so none of its %d planned unit(s) can be shown to have run", bucketID, planned.Units),
			}}, nil
		}
		rnr, _, err := newRunner(runnerConfig{kind: runnerKind})
		if err != nil {
			return nil, err
		}
		sum, err := rnr.ParseTimings(readers...)
		if err != nil {
			return nil, err
		}
		var report strings.Builder
		ev := &walltime.AuditEvidence{
			Bucket: bucketID, PlanDigest: planDigest,
			Planned: planned.Units, Reported: len(sum.PackageRuns),
		}
		if err := core.AuditCoverage(&report, planned, sum); err != nil {
			ev.Problems = append(ev.Problems, err.Error())
		}
		ev.Report = report.String()
		return ev, nil
	}
}

// fullPlanDigest canonicalises a shard-plan artifact with the SAME algorithm
// the Stage-2 receipt's full-plan digest was taken with: parse the document and
// digest the parsed structure, not the file's bytes.
//
// Bytes would be the wrong thing to compare. The receipt digests the planner's
// in-memory document; the artifact is written indented, and a re-serialisation
// that differed only in whitespace would read as a substituted plan. Parsing
// and re-canonicalising compares the plan, which is what is being bound.
//
// The audit itself does NOT call this: it parses once and digests the document
// it actually used, so its digest and its expected coverage cannot describe two
// different files. This remains for callers that only need the digest.
func fullPlanDigest(path string) (walltime.Digest, error) {
	doc, err := core.ParseShardPlan(path)
	if err != nil {
		return "", err
	}
	return walltime.DigestJSON(doc)
}

// originSuffix names where evidence was said to come from, when the caller
// stated it. A digest mismatch is much easier to act on when the message says
// which artifact was supposed to have been downloaded.
func originSuffix(origin string) string {
	if o := strings.TrimSpace(origin); o != "" {
		return " (fetched from " + o + ")"
	}
	return ""
}

func runWallExec(args []string) error {
	fs := flag.NewFlagSet("wall exec", flag.ExitOnError)
	dir := fs.String("dir", "", "records directory (required)")
	level := fs.String("level", "invocation", "measurement level: script or invocation")
	seq := fs.Int("seq", 0, "stable ordinal of this invocation within its bucket script")
	cwd := fs.String("cwd", "", "working directory for the command")
	desc := fs.String("desc", "", "human description of what this invocation runs")
	spec := fs.String("spec", "", "read the invocation spec (argv, cwd, selector, digests) from this JSON file instead of flags")
	unit := fs.String("unit-digest", "", "digest of the planned unit this invocation renders")
	atom := fs.String("atom-digest", "", "digest of the atom membership this invocation covers")
	// --join-action is accepted and ignored: the flag is part of the rendered
	// script bytes a pinned consumer already emits, and removing it would break
	// a caller that passes it. There is no containment to join any more.
	_ = fs.Bool("join-action", true, "accepted for compatibility; the action containment it joined is removed")
	timeout := fs.Duration("timeout", walltime.DefaultTimeout, "bound on every wait")
	var selector stringList
	fs.Var(&selector, "selector", "a test-selection token this invocation applies; repeatable")
	var ids runIdentityFlags
	ids.bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("--dir is required")
	}

	opt := walltime.ExecOptions{
		Level: walltime.Level(*level), Seq: *seq, Dir: *dir, Run: ids.identity(),
		Argv: fs.Args(), Cwd: *cwd, Selector: selector, Desc: *desc,
		UnitDigest: walltime.Digest(*unit), AtomDigest: walltime.Digest(*atom),
		Timeout: *timeout,
	}
	if *spec != "" {
		loaded, err := walltime.LoadInvocationSpec(*spec)
		if err != nil {
			return err
		}
		// The spec file is the authority when given: the plan bound those exact
		// bytes, and re-deriving them from a command line is how a measured
		// invocation drifts from the planned one.
		opt.Argv, opt.Cwd, opt.Selector = loaded.Argv, loaded.Cwd, loaded.Selector
		opt.Desc, opt.Seq = loaded.Desc, loaded.Seq
		if loaded.UnitDigest != "" {
			opt.UnitDigest = loaded.UnitDigest
		}
		if loaded.AtomDigest != "" {
			opt.AtomDigest = loaded.AtomDigest
		}
	}
	if len(opt.Argv) == 0 {
		return fmt.Errorf("no command: pass it after -- or supply --spec")
	}
	// The action-state handoff still carries the run identity across the two
	// steps, which is the part of it that survives. The containment nesting
	// and the invocation controller that stood here are gone with the cgroup
	// admission model: Exec owns one process group and drains it, and there is
	// no enclosing containment for an invocation to nest under.
	if st, err := walltime.LoadActionState(*dir); err == nil {
		if opt.Run.CampaignID == "" && opt.Run.Stage2 == "" {
			opt.Run = st.Run
		}
	}

	code, err := walltime.Exec(opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testbucket wall: %v\n", err)
		if code == 0 {
			// A wrapper failure with a successful child is still a failure:
			// the measurement did not close, and reporting success would make
			// a missing row look like a recorded one.
			code = 1
		}
	}
	// The measured command's status is the status of this process: a wrapper
	// that swallowed a failing bucket would make a red run look green.
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

// runWall dispatches the wall subcommands that survive the removal plan.
//
// The removed verbs went with their machinery: observe, hold, verify, bundle,
// replay, stage1, stage1-binary, check-delegation, digest, train, campaign,
// release-manifest, verify-attestation and countersign all served the
// protected-authority, observer or campaign-proof models. What remains is the
// measurement lifecycle the rendered script actually calls.
func runWall(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, wallUsage)
		os.Exit(2)
	}
	switch args[0] {
	case "begin":
		return runWallBegin(args[1:])
	case "end":
		return runWallEnd(args[1:])
	case "exec":
		return runWallExec(args[1:])
	case "run":
		return runWallRun(args[1:])
	case "-h", "--help", "help":
		fmt.Fprint(os.Stderr, wallUsage)
		return nil
	default:
		return fmt.Errorf("unknown wall subcommand %q", args[0])
	}
}
