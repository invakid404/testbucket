package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/walltime"
)

// verifierKeyEnv is where the verifier's own signing key is read from, for
// the same reason the authority key is: a key on a command line is a key in
// the process table.
const verifierKeyEnv = "TB_WALL_VERIFIER_KEY"

// replayKeyEnv is the INDEPENDENT replay party's own signing key. It is a
// separate variable from the authority key because the whole value of a replay
// is that a different party produced it; sharing one key would make the
// distinction editorial.
const replayKeyEnv = "TB_WALL_REPLAY_KEY"

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
  testbucket wall stage1  [flags]  assemble and sign the Stage-1 input manifest
                                   that authorises a bundle (the signing key
                                   comes from TB_WALL_AUTHORITY_KEY, never a
                                   flag)
  testbucket wall digest  [flags]  print the canonical digest of a manifest,
                                   receipt, bundle, registry or scorer — the
                                   identity every record has to bind to
  testbucket wall train   [flags]  fit the frozen scorer from a sealed training
                                   receipt set of historical wrapper-qualified
                                   physical V labels
  testbucket wall campaign [flags] apply the frozen five-pair decision rule to a
                                   campaign of AUTHENTICATED rows: each arm's
                                   signed Stage-1 manifest and one eligible
                                   verifier verdict per bucket

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
	case "stage1":
		return runWallStage1(args[1:])
	case "digest":
		return runWallDigest(args[1:])
	case "train":
		return runWallTrain(args[1:])
	case "campaign":
		return runWallCampaign(args[1:])
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
	joinAction := fs.Bool("join-action", true, "for --level script, join the enclosing action containment recorded by `wall begin`. An invocation wrapper is already inside the script containment by inheritance, so it never joins — joining would move it out")
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
	// The enclosing containment differs by level, and so does whether this
	// process joins it. A script wrapper is started fresh by an Actions step
	// and joins the ACTION containment; an invocation wrapper is already
	// inside the SCRIPT containment by inheritance and only needs to nest its
	// own containment under it.
	if st, err := walltime.LoadActionState(*dir); err == nil {
		ident := st.Containment
		opt.Parent = &ident
		opt.JoinParent = *joinAction
		if opt.Run.CampaignID == "" && opt.Run.Stage2 == "" {
			opt.Run = st.Run
		}
	}
	if opt.Level == walltime.LevelInvocation {
		opt.JoinParent = false
		if ident, ok := walltime.ScriptContainment(*dir); ok {
			opt.Parent = ident
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
	// The key arrives on an inherited descriptor, never in argv: argv is
	// readable by every process on the machine, and a signing key visible in
	// the process table is not a signing key.
	keyFD := fs.Int("key-fd", 0, "descriptor to read this observer's own signing key from")
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
	if *keyFD <= 0 {
		return fmt.Errorf("--key-fd is required: an observer is handed its key on an inherited descriptor")
	}
	priv, err := walltime.ReadKeyFD(*keyFD)
	if err != nil {
		return err
	}
	return walltime.RunObserver(walltime.ObserverConfig{
		Producer: walltime.Producer(*producer), Level: walltime.Level(*level), Seq: *seq,
		Dir: *dir, ControlBase: *control, Containment: ident, Run: run, Key: priv,
		Timeout: *timeout,
	})
}

// runWallTrain is the OFFLINE surface: the one place a historical V label is
// allowed to exist. It refuses an unvalidated receipt set, and an empty set is
// the expected answer today — no wrapper-qualified historical label exists
// yet, so no scorer can honestly be trained, and inventing one from reporter
// data is the leak the two surfaces exist to prevent.
// runWallDigest prints a document's canonical identity. The wrapper's records
// have to name the Stage-1 and Stage-2 digests, and a workflow that had to
// recompute them by hand would eventually compute them differently.
func runWallDigest(args []string) error {
	fs := flag.NewFlagSet("wall digest", flag.ExitOnError)
	file := fs.String("file", "", "the document to digest (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("--file is required")
	}
	var probe struct {
		Kind string `json:"kind"`
	}
	if err := walltime.ReadJSONFile(*file, &probe); err != nil {
		return err
	}
	var (
		d   walltime.Digest
		err error
	)
	switch probe.Kind {
	case walltime.Stage1Kind:
		var v walltime.Stage1Manifest
		if err = walltime.ReadJSONFile(*file, &v); err == nil {
			d, err = v.DigestOf()
		}
	case walltime.Stage2Kind:
		var v walltime.Stage2Receipt
		if err = walltime.ReadJSONFile(*file, &v); err == nil {
			d, err = v.DigestOf()
		}
	case walltime.BundleKind:
		var v walltime.PlanningInputBundle
		if err = walltime.ReadJSONFile(*file, &v); err == nil {
			d, err = v.DigestOf()
		}
	case walltime.RegistryKind:
		var v walltime.AetaRegistry
		if err = walltime.ReadJSONFile(*file, &v); err == nil {
			d, err = v.DigestOf()
		}
	case walltime.ScorerKind:
		var v walltime.Scorer
		if err = walltime.ReadJSONFile(*file, &v); err == nil {
			d, err = v.DigestOf()
		}
	case walltime.TrainingSetKind:
		var v walltime.TrainingReceiptSet
		if err = walltime.ReadJSONFile(*file, &v); err == nil {
			d, err = v.DigestOf()
		}
	case walltime.ScheduleKind:
		// A schedule digests to its ORDER, not to the whole document: the
		// order is what a campaign index cites, and it is what a reordering
		// changes. The schedule is validated first, because a digest of an
		// unusable order is a number nobody can act on.
		var v walltime.CampaignSchedule
		if err = walltime.ReadJSONFile(*file, &v); err == nil {
			if err = v.Validate(); err == nil {
				d, err = v.OrderDigest()
			}
		}
	default:
		return fmt.Errorf("%s has kind %q, which is not a document this verifier digests", *file, probe.Kind)
	}
	if err != nil {
		return err
	}
	fmt.Println(d)
	return nil
}

func runWallTrain(args []string) error {
	fs := flag.NewFlagSet("wall train", flag.ExitOnError)
	labels := fs.String("labels", "", "sealed training receipt set (required)")
	id := fs.String("id", "", "identity to give the frozen scorer (required)")
	out := fs.String("out", "", "write the frozen scorer here (required)")
	sealKeys := fs.String("training-authority-key", "", "comma-separated PREDECLARED public keys allowed to seal a training receipt set (required): a lineage nobody can attribute is the claim that somebody ran the right procedure")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *labels == "" || *id == "" || *out == "" {
		return fmt.Errorf("--labels, --id and --out are all required")
	}
	if strings.TrimSpace(*sealKeys) == "" {
		return fmt.Errorf("--training-authority-key is required: without a predeclared sealing key the receipt set's own signature would authenticate it, and a self-sealed lineage is not a sealed offline surface")
	}
	var set walltime.TrainingReceiptSet
	if err := walltime.ReadJSONFile(*labels, &set); err != nil {
		return err
	}
	// The ridge lambda comes from the SEALED SET, not from a flag here: it
	// decides the coefficients, and a verifier handed everything except the
	// lambda could not refit the scorer to check it.
	scorer, err := walltime.TrainScorer(set, *id, splitList(*sealKeys))
	if err != nil {
		return err
	}
	if err := walltime.WriteJSONFile(*out, scorer); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: fitted %s from %d sealed label(s)\n  scorer digest: %s\n  receipt set:   %s\n",
		scorer.ID, len(set.Labels), scorer.Lineage.ScorerDigest, scorer.Lineage.ReceiptSetDigest)
	return nil
}

// runWallCampaign applies the frozen decision rule. It is deliberately a
// separate command from `verify`: a per-run verdict says whether one row
// qualifies, and this says whether five pairs of them decide anything.
func runWallCampaign(args []string) error {
	fs := flag.NewFlagSet("wall campaign", flag.ExitOnError)
	index := fs.String("index", "", "campaign index naming each arm's Stage-1 manifest and its per-bucket verifier verdicts. This is the only input that can produce a campaign result")
	in := fs.String("in", "", "CALCULATOR ONLY: a JSON array of baseline/candidate pairs with durations already filled in. It exercises the arithmetic and can never pass, because nothing about those numbers is authenticated")
	asJSON := fs.Bool("json", false, "write the gate results as JSON")
	authority := fs.String("authority", "", "protected environment each arm's manifest must name. A key says WHO signed; this says WHICH environment approved")
	releaseSHA := fs.String("release-sha", "", "the full 40-hex commit the release ref resolves to. A campaign is evidence for the delivery it was produced for, so the release-binding gate does not pass without it — and every arm's reviewed tip and release ref must be this commit")
	var releaseArtifacts stringList
	fs.Var(&releaseArtifacts, "release-artifact", "path to one asset about to be published; repeatable, and its digest is computed HERE from the bytes on disk. A release publishes several files, and the campaign's delivered binary must be one of them. There is deliberately no flag that accepts a digest as a string: a gate that reads a declaration compares its manifests to a claim about the delivery rather than to the delivery")
	var authorityKeys stringList
	fs.Var(&authorityKeys, "authority-key", "a PREDECLARED authority public key (hex); repeatable and required with --index")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*index == "") == (*in == "") {
		return fmt.Errorf("pass exactly one of --index (a campaign) or --in (the calculator)")
	}
	release := walltime.CampaignRelease{SHA: strings.TrimSpace(*releaseSHA)}
	for _, path := range releaseArtifacts {
		d, err := walltime.FileDigest(path)
		if err != nil {
			return fmt.Errorf("digest the published artifact %s: %w", path, err)
		}
		release.Artifacts = append(release.Artifacts, walltime.ReleaseArtifact{
			Name: filepath.Base(path), Digest: d,
		})
	}
	var gates []walltime.GateResult
	calculatorOnly := false
	if *index != "" {
		var idx walltime.CampaignIndex
		if err := walltime.ReadJSONFile(*index, &idx); err != nil {
			return err
		}
		gates, _ = walltime.EvaluateCampaignIndex(idx, walltime.FileCampaignLoader{}, authorityKeys, *authority, release)
	} else {
		// The calculator path. It prints the arithmetic and ALWAYS fails: a
		// number in a JSON file is not an observation, and the one thing this
		// command must never do is let a hand-written file look like a result.
		calculatorOnly = true
		var pairs []walltime.CampaignPair
		if err := walltime.ReadJSONFile(*in, &pairs); err != nil {
			return err
		}
		gates = walltime.EvaluateCampaign(pairs)
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(gates); err != nil {
			return err
		}
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "gate\trequired\tobserved\tn\tresult\n")
		for _, g := range gates {
			result := "FAIL"
			if g.Pass {
				result = "pass"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", g.Name, g.Required, g.Observed, g.Population, result)
			if g.Detail != "" {
				fmt.Fprintf(w, "\t\t%s\n", g.Detail)
			}
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	for _, g := range gates {
		if !g.Pass {
			return fmt.Errorf("wall campaign: the decision rule is not satisfied (%s)", g.Name)
		}
	}
	if calculatorOnly {
		return fmt.Errorf("wall campaign: --in is the calculator; every gate above is arithmetic over unauthenticated numbers. " +
			"Pass --index with per-bucket verifier verdicts and each arm's signed Stage-1 manifest for a campaign result")
	}
	return nil
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
	stepAttempt := fs.String("step-attempt", "", "GitHub step-attempt diagnostic (A_GH). Non-gating — GitHub reports seconds — but required for identity sanity and to account for the wrapper install that necessarily precedes AT_start")
	invocations := fs.String("invocations", "", "this bucket's invocation manifest: what the authorised plan rendered. Without it the measured argv, selector, unit membership and atom closure are not checked against the plan")
	replay := fs.String("replay", "", "independent Stage-2 replay attestation (`wall replay --attest`). Required to score: comparing the planner's account of its own output to itself proves nothing")
	require := fs.String("require", "complete", "verdict this command exits non-zero below: complete (well-formed records) or eligible (scorable under every frozen ROW gate; the campaign-scope gates are decided by `wall campaign`)")
	authority := fs.String("authority", "", "the protected environment the Stage-1 manifest must name")
	scorer := fs.String("scorer", "", "the frozen scorer the Pcheck projection claims. Without it the projection is only checked against its own arithmetic, which a substituted allocation map satisfies")
	trainingSet := fs.String("training-set", "", "the EXACT sealed training receipt set the scorer was fitted from. The verifier revalidates it under the training authority Stage 1 declared and REFITS the scorer: without it the model's lineage is a digest the model states about itself, and the row is ineligible")
	shardPlan := fs.String("shard-plan", "", "the authorised plan artifact, for the exact-run coverage audit")
	eventsDir := fs.String("events", "", "this bucket's runner events directory, for the exact-run coverage audit. Without --events and --shard-plan nothing checks that the measured script ran the work the plan gave it")
	runnerKind := fs.String("runner", "go", "which adapter's event parser reads --events: go or vitest")
	var authorityKeys stringList
	fs.Var(&authorityKeys, "authority-key", "a PREDECLARED authority public key (hex); repeatable. Without one the verifier will not treat any signature as authority approval, because a self-generated key would otherwise pass")
	var recordSigners stringList
	fs.Var(&recordSigners, "record-signer", "a PREDECLARED run-key PUBLIC key (hex) allowed to sign this measurement's signer roster and closing seal; repeatable. The Stage-1 manifest normally declares these and is the authoritative source; this lets a caller state them independently, and the two sets are unioned")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("--dir is required")
	}
	v, err := walltime.VerifyDir(walltime.VerifyOptions{
		Dir: *dir, Stage1Path: *stage1, Stage2Path: *stage2,
		AetaPath: *aeta, PcheckPath: *pcheck, RegistryPath: *registry, ScorerPath: *scorer,
		TrainingSetPath: *trainingSet,
		ReplayPath:      *replay, InvocationsPath: *invocations, StepAttemptPath: *stepAttempt,
		Audit:         coverageAudit(*shardPlan, *eventsDir, *runnerKind),
		AuthorityKeys: authorityKeys, Authority: *authority, SignerKeys: recordSigners,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		// A campaign counts a row only if its verdict is attributable, so the
		// machine-readable form is signed by the verifier. Without a key the
		// verdict is still emitted — and the campaign will refuse it, loudly,
		// which is better than a silently uncountable row.
		if key := strings.TrimSpace(os.Getenv(verifierKeyEnv)); key != "" {
			priv, err := walltime.DecodeKey(key)
			if err != nil {
				return fmt.Errorf("%s: %w", verifierKeyEnv, err)
			}
			if err := v.Sign(*authority, priv); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(os.Stderr,
				"testbucket wall: %s is unset, so this verdict is UNSIGNED and no campaign will count it\n", verifierKeyEnv)
		}
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
