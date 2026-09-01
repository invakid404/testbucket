package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/invakid404/testbucket/internal/walltime"
)

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
  testbucket wall verify  [flags]  verify a records directory and report
                                   eligibility, reconciliation and every gate
  testbucket wall bundle  [flags]  freeze a planning-input bundle: the canonical
                                   instant, the raw discovery and runnable bytes,
                                   the store bytes, and the acquisition closure
  testbucket wall replay  [flags]  independently replay a bundle through the
                                   planner and refuse to agree unless every
                                   digest matches the issued Stage-2 receipt

Every endpoint is a fresh CLOCK_MONOTONIC read taken by the producer that
records it. A host with no delegated cgroup-v2 subtree (TB_WALL_CGROUP_ROOT)
still records everything, and ` + "`wall verify`" + ` reports the run INELIGIBLE rather
than scoring a lifecycle it cannot prove.
`

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
	case "observe":
		return runWallObserve(args[1:])
	case "verify":
		return runWallVerify(args[1:])
	case "bundle":
		return runWallBundle(args[1:])
	case "replay":
		return runWallReplay(args[1:])
	case "-h", "--help", "help":
		fmt.Fprint(os.Stderr, wallUsage)
		return nil
	default:
		return fmt.Errorf("unknown `wall` subcommand %q\n\n%s", args[0], wallUsage)
	}
}

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
	joinAction := fs.Bool("join-action", true, "join the enclosing action containment recorded by `wall begin`, when present")
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
	if *joinAction {
		if st, err := walltime.LoadActionState(*dir); err == nil {
			ident := st.Containment
			opt.Parent = &ident
			if opt.Run.CampaignID == "" {
				opt.Run = st.Run
			}
		}
	}
	code, err := walltime.Exec(opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testbucket wall: %v\n", err)
	}
	// The measured command's status is the status of this process: a wrapper
	// that swallowed a failing bucket would make a red run look green.
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

func runWallRun(args []string) error {
	fs := flag.NewFlagSet("wall run", flag.ExitOnError)
	dir := fs.String("dir", "", "records directory (required)")
	cwd := fs.String("cwd", "", "working directory for the command")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("--dir is required")
	}
	if len(fs.Args()) == 0 {
		return fmt.Errorf("no command: pass it after --")
	}
	code, err := walltime.RunInAction(*dir, fs.Args(), *cwd, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

func runWallObserve(args []string) error {
	fs := flag.NewFlagSet("wall observe", flag.ExitOnError)
	producer := fs.String("producer", "", "containment_peer or trace_collector")
	level := fs.String("level", "", "action, script or invocation")
	seq := fs.Int("seq", 0, "invocation ordinal")
	dir := fs.String("dir", "", "records directory")
	control := fs.String("control", "", "control file base path")
	containment := fs.String("containment", "", "containment identity as JSON")
	runJSON := fs.String("run", "", "run identity as JSON")
	key := fs.String("key", "", "this observer's own signing key")
	timeout := fs.Duration("timeout", walltime.DefaultTimeout, "bound on the wait for verified-empty containment")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var ident walltime.ContainmentIdentity
	if err := json.Unmarshal([]byte(*containment), &ident); err != nil {
		return fmt.Errorf("containment identity: %w", err)
	}
	var run walltime.RunIdentity
	if strings.TrimSpace(*runJSON) != "" {
		if err := json.Unmarshal([]byte(*runJSON), &run); err != nil {
			return fmt.Errorf("run identity: %w", err)
		}
	}
	priv, err := walltime.DecodeKey(*key)
	if err != nil {
		return err
	}
	return walltime.RunObserver(walltime.ObserverConfig{
		Producer: walltime.Producer(*producer), Level: walltime.Level(*level), Seq: *seq,
		Dir: *dir, ControlBase: *control, Containment: ident, Run: run, Key: priv,
		Timeout: *timeout,
	})
}

func runWallVerify(args []string) error {
	fs := flag.NewFlagSet("wall verify", flag.ExitOnError)
	dir := fs.String("dir", "", "records directory (required)")
	asJSON := fs.Bool("json", false, "write the verdict as JSON instead of a report")
	stage1 := fs.String("stage1", "", "Stage-1 input manifest to verify the records against")
	stage2 := fs.String("stage2", "", "Stage-2 derived-plan receipt to verify the records against")
	aeta := fs.String("aeta", "", "instantiated pre-action Aeta document for the ETA-completeness gate")
	pcheck := fs.String("pcheck", "", "post-render Pcheck projection for the predictor gate")
	registry := fs.String("registry", "", "frozen Aeta component-registry template; without it ETA completeness cannot be proven")
	require := fs.String("require", "complete", "verdict this command exits non-zero below: complete (well-formed records) or eligible (scorable under every frozen gate)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("--dir is required")
	}
	v, err := walltime.VerifyDir(walltime.VerifyOptions{
		Dir: *dir, Stage1Path: *stage1, Stage2Path: *stage2,
		AetaPath: *aeta, PcheckPath: *pcheck, RegistryPath: *registry,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			return err
		}
	} else if err := v.Write(os.Stdout); err != nil {
		return err
	}
	// Fail closed on the level the caller asked for. A campaign row demands
	// --require=eligible; a developer run asks only that the records it just
	// wrote are well-formed. Neither level can be satisfied by absent evidence.
	switch *require {
	case "complete":
		if !v.Complete {
			return fmt.Errorf("wall verify: the records are not a complete measurement (%d finding(s))", len(v.Findings))
		}
	case "eligible":
		if !v.Eligible {
			return fmt.Errorf("wall verify: the run is INELIGIBLE and contributes 0 scored rows (%d finding(s))", len(v.Findings))
		}
	default:
		return fmt.Errorf("--require must be complete or eligible, got %q", *require)
	}
	return nil
}
