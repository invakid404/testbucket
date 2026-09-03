package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

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
  testbucket wall verify-attestation [flags]  refuse an asset whose
                                    attestation does not authenticate against
                                    BOTH predeclared keys
  testbucket wall countersign [flags]  the VERIFIER'S independent signature
                                    over a builder attestation, after
                                    re-deriving the artifact's digest
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
	case "release-manifest":
		return runWallReleaseManifest(args[1:])
	case "verify-attestation":
		return runWallVerifyAttestation(args[1:])
	case "countersign":
		return runWallCountersign(args[1:])
	case "attest":
		return runWallAttest(args[1:])
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
		// THE CONTROLLER MEASURES IT, IF ONE IS SERVING.
		//
		// The measured script runs as its own account and holds none of the
		// capabilities an invocation envelope needs: it cannot create a
		// containment under the script's, admit a process into it, write a
		// ledger or register a signer. The script-level wrapper can, and stays
		// alive to do exactly that on request. So this process asks, and
		// relays the measured child's status; everything else happens under
		// the wrapper's credential on the other side of the socket.
		//
		// With no controller — a developer run with no second account — this
		// wrapper does the work itself, exactly as before.
		if *spec != "" {
			code, served, err := walltime.RequestInvocation(*dir, opt.Seq, *spec)
			if served {
				if err != nil {
					fmt.Fprintf(os.Stderr, "testbucket wall: %v\n", err)
					if code == 0 {
						code = 1
					}
				}
				if code != 0 {
					os.Exit(code)
				}
				return nil
			}
		}
		// FAIL CLOSED on a handoff that exists and cannot be authenticated.
		//
		// The fallback for "no handoff" is to nest under the action, which is
		// the right topology for an invocation started outside a measured
		// script. It must never become the silent outcome of a measured script
		// having rewritten or corrupted the file: that would let the workload
		// choose its own enclosing containment and then be accounted under the
		// one it chose.
		ident, ok, err := walltime.ScriptContainment(*dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "testbucket wall: %v\n", err)
			return err
		}
		if !ok {
			// THE KERNEL'S OWN ANSWER. An invocation wrapper is started by the
			// measured script and is therefore already inside the script's
			// containment; asking the kernel which cgroup this process is in
			// is a fact the measured work cannot forge, unlike the file it
			// used to be told through.
			ident, ok = walltime.SelfContainment()
		}
		if ok {
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

// builderKeyEnv is where the builder's signing key is read from — an
// environment variable rather than a flag, for the same reason the authority
// key is: a key on a command line is a key in the process table.
const builderKeyEnv = walltime.BuilderKeyEnv

// runWallAttest produces the builder's signed statement about one artifact.
//
// Stage 1 used to accept a caller-authored sentence here. This is the document
// that replaces it: it names the exact subject bytes, the source they were
// built from, the workload that built them, the identity vouching for that
// workload, and who checked it with what result — and it is signed, so a
// predeclared builder key is what makes the whole statement attributable.
func runWallAttest(args []string) error {
	fs := flag.NewFlagSet("wall attest", flag.ExitOnError)
	subject := fs.String("subject", "", "path to the exact artifact being attested (required); its digest is computed here")
	subjectName := fs.String("subject-name", "", "the name the artifact is delivered under; defaults to the file's base name")
	repository := fs.String("source-repository", "", "repository the artifact was built from (required)")
	commit := fs.String("source-commit", "", "the full 40-hex commit it was built from (required)")
	builderID := fs.String("builder-id", "", "the workload that produced it, e.g. the workflow ref (required)")
	issuer := fs.String("issuer", "", "the identity vouching for that workload, e.g. the OIDC issuer (required)")
	run := fs.String("build-run", "", "the run that produced it (required): the retained verification result is bound to it")
	attempt := fs.String("build-attempt", "", "that run's attempt (required)")
	out := fs.String("out", "", "write the signed attestation here (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	required := map[string]string{
		"--subject": *subject, "--source-repository": *repository, "--source-commit": *commit,
		"--builder-id": *builderID, "--issuer": *issuer, "--out": *out,
		// The run and attempt are what bind the retained verification result
		// to a run a reader can go and look at.
		"--build-run": *run, "--build-attempt": *attempt,
	}
	names := make([]string, 0, len(required))
	for name := range required {
		names = append(names, name)
	}
	sort.Strings(names)
	var missing []string
	for _, name := range names {
		if strings.TrimSpace(required[name]) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("these are required, and each is one of the identities the contract asks a build attestation to retain: %s", strings.Join(missing, ", "))
	}
	digest, err := walltime.FileDigest(*subject)
	if err != nil {
		return fmt.Errorf("digest the attested artifact: %w", err)
	}
	name := *subjectName
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(*subject)
	}
	// THE BUILDER STATES ONLY THE BUILDER'S HALF.
	//
	// This command used to write the verifier id, the verifier's binary
	// digest, the version, the instant of the verification and
	// `Result: verified` as well — every one of them chosen by the builder and
	// then signed with the builder's key. That document said a build had been
	// checked, and the only party in it was the party that made it. The
	// verifier fields are filled in by `wall countersign`, run by the verifier,
	// from the artifact the verifier obtained for itself.
	a := walltime.BuildAttestation{
		Kind: walltime.BuildAttestationKind, SubjectName: name, SubjectDigest: digest,
		SourceRepository: *repository, SourceCommit: *commit,
		BuilderID: *builderID, Issuer: *issuer, BuildRun: *run, BuildAttempt: *attempt,
	}
	key, err := walltime.DecodeKey(strings.TrimSpace(os.Getenv(builderKeyEnv)))
	if err != nil {
		return fmt.Errorf("%s: %w (only the builder may attest what it built)", builderKeyEnv, err)
	}
	if err := a.Sign(*builderID, key); err != nil {
		return err
	}
	if err := walltime.WriteJSONFile(*out, a); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: attested %s\n  subject: %s\n  source:  %s@%s\n  builder: %s (%s)\n  key:     %s\n  NOT YET VERIFIED: an independent verifier must countersign this before it can admit a delivery\n",
		name, digest, *repository, *commit, *builderID, *issuer, a.Signature.KeyID)
	fmt.Println(a.Signature.KeyID)
	return nil
}

// runWallCountersign is the VERIFIER'S half: obtain the artifact, re-derive
// its digest, and sign what was concluded under the verifier's own key.
//
// It exists because "verified" was a word the builder wrote. The builder
// cannot run this: the key it needs is a different secret, and the artifact it
// checks is the one this job downloaded rather than the one the build step
// still had on disk. What it produces is the second signature Verify requires,
// and the release cannot publish without it.
func runWallCountersign(args []string) error {
	fs := flag.NewFlagSet("wall countersign", flag.ExitOnError)
	in := fs.String("attestation", "", "the builder's signed attestation (required)")
	subject := fs.String("subject", "", "path to the artifact the VERIFIER obtained, whose digest is recomputed here (required)")
	verifierID := fs.String("verifier-id", "", "the identity doing the checking (required); it must not be the builder")
	at := fs.String("verified-at", "", "RFC3339 instant this check was made (required)")
	builderKey := fs.String("builder-key", "", "the builder's PUBLIC key, predeclared, so the builder half is authenticated before it is countersigned (required)")
	out := fs.String("out", "", "write the countersigned attestation here (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"--attestation": *in, "--subject": *subject, "--verifier-id": *verifierID,
		"--verified-at": *at, "--builder-key": *builderKey, "--out": *out,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	var a walltime.BuildAttestation
	if err := walltime.ReadJSONFile(*in, &a); err != nil {
		return err
	}
	// THE VERIFIER RE-DERIVES THE SUBJECT ITSELF. Countersigning a digest the
	// builder computed would add a signature and no verification.
	digest, err := walltime.FileDigest(*subject)
	if err != nil {
		return fmt.Errorf("digest the artifact this verifier obtained: %w", err)
	}
	if digest != a.SubjectDigest {
		return fmt.Errorf("REFUSED: the artifact this verifier obtained digests to %s, but the builder attested %s; these are different bytes and no signature can reconcile them", digest, a.SubjectDigest)
	}
	if strings.TrimSpace(*verifierID) == strings.TrimSpace(a.BuilderID) {
		return fmt.Errorf("REFUSED: the verifier identity %q is the builder's; a build that vouches for itself has been checked by nobody", *verifierID)
	}
	// The builder half is AUTHENTICATED before it is adopted. A
	// countersignature over an unauthenticated statement says the verifier
	// checked the bytes and believed whatever was written beside them.
	builderDigest, err := a.BuilderDigestOf()
	if err != nil {
		return err
	}
	if err := walltime.VerifySigned(a.Signature, builderDigest, []string{strings.TrimSpace(*builderKey)}); err != nil {
		return fmt.Errorf("the builder half does not authenticate against the predeclared builder key: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	selfDigest, err := walltime.FileDigest(self)
	if err != nil {
		return err
	}
	a.VerifierID, a.VerifierBinary, a.VerifierVersion = *verifierID, selfDigest, version
	a.VerifiedAt, a.Result = *at, walltime.AttestationVerified
	key, err := walltime.DecodeKey(strings.TrimSpace(os.Getenv(verifierKeyEnv)))
	if err != nil {
		return fmt.Errorf("%s: %w (the verifier signs under its own key, or it is not a second party)", verifierKeyEnv, err)
	}
	if walltime.PublicKeyOf(key) == strings.TrimSpace(*builderKey) {
		return fmt.Errorf("REFUSED: the verifier key is the builder key; two identities holding one key are one party")
	}
	if err := a.Countersign(*verifierID, key); err != nil {
		return err
	}
	// Checked before it is written, against both predeclared keys.
	if problems := a.Verify(digest, a.SourceCommit, []string{strings.TrimSpace(*builderKey), walltime.PublicKeyOf(key)}); len(problems) > 0 {
		return fmt.Errorf("the countersigned attestation does not verify:\n  %s", strings.Join(problems, "\n  "))
	}
	if err := walltime.WriteJSONFile(*out, a); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: countersigned %s\n  subject:  %s\n  builder:  %s\n  verifier: %s\n  key:      %s\n",
		a.SubjectName, digest, a.BuilderID, *verifierID, a.VerifierSignature.KeyID)
	fmt.Println(a.VerifierSignature.KeyID)
	return nil
}

// runWallVerifyAttestation is what makes an attestation a RELEASE INPUT.
//
// The attestations were produced and then consulted by nothing: no publish
// step read them, so a delivery could ship with an attestation that did not
// verify, or with none at all, and the files sat in a temp directory gating
// nothing on their own terms. This refuses the asset unless the document
// authenticates against both predeclared keys and describes the exact bytes
// about to be uploaded.
func runWallVerifyAttestation(args []string) error {
	fs := flag.NewFlagSet("wall verify-attestation", flag.ExitOnError)
	in := fs.String("attestation", "", "the countersigned attestation to check (required)")
	subject := fs.String("subject", "", "path to the artifact about to be published (required); its digest is recomputed here")
	commit := fs.String("source-commit", "", "the commit being released (required)")
	var keys stringList
	fs.Var(&keys, "key", "a PREDECLARED public key; repeat for the builder's and the verifier's (both required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*in) == "" || strings.TrimSpace(*subject) == "" || strings.TrimSpace(*commit) == "" {
		return fmt.Errorf("--attestation, --subject and --source-commit are required")
	}
	if len(keys) < 2 {
		return fmt.Errorf("two predeclared keys are required — the builder's and the independent verifier's; a delivery authenticated against one party is a delivery one party can mint")
	}
	var a walltime.BuildAttestation
	if err := walltime.ReadJSONFile(*in, &a); err != nil {
		return err
	}
	digest, err := walltime.FileDigest(*subject)
	if err != nil {
		return err
	}
	if problems := a.Verify(digest, *commit, keys); len(problems) > 0 {
		return fmt.Errorf("REFUSED: %s is not covered by an independently verified attestation:\n  %s",
			filepath.Base(*subject), strings.Join(problems, "\n  "))
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: %s is attested by %s and independently verified by %s\n",
		a.SubjectName, a.BuilderID, a.VerifierID)
	return nil
}

// runWallReleaseManifest derives the ONE publish set a release uses.
//
// It exists because the gate and the uploader used to choose their files
// independently — the gate from goreleaser's Binary/Archive/Checksum rows, the
// uploader from a `dist/*.tar.gz` glob — so the set that was checked and the
// set that was published were not the same set. The difference was the raw
// platform binaries: gated, never uploaded, and therefore able to satisfy the
// campaign's delivered-binary match with a file no consumer receives.
//
// Deriving it once and handing the same document to both closes that by
// construction rather than by convention.
func runWallReleaseManifest(args []string) error {
	fs := flag.NewFlagSet("wall release-manifest", flag.ExitOnError)
	dist := fs.String("dist", "dist", "goreleaser output directory holding artifacts.json")
	root := fs.String("root", ".", "directory the manifest's paths are relative to")
	out := fs.String("out", "", "write the canonical publish set here (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("--out is required")
	}
	artifacts, err := os.ReadFile(filepath.Join(*dist, "artifacts.json"))
	if err != nil {
		return fmt.Errorf("read goreleaser's artifact manifest: %w", err)
	}
	m, err := walltime.DeriveReleaseManifest(*root, artifacts)
	if err != nil {
		return err
	}
	if err := walltime.WriteJSONFile(*out, m); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: this release publishes %d asset(s)\n", len(m.Assets))
	for _, a := range m.Assets {
		fmt.Fprintf(os.Stderr, "  %s  %s\n", a.Digest, a.Name)
		for _, mem := range a.Contains {
			fmt.Fprintf(os.Stderr, "    contains %s  %s\n", mem.Digest, mem.Name)
		}
	}
	// The upload list, one name per line, so a publisher can read the set
	// rather than select it.
	for _, name := range m.UploadNames() {
		fmt.Println(name)
	}
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
	releaseManifest := fs.String("release-manifest", "", "the canonical publish set (`wall release-manifest`): every asset this release will upload, with its digest and, for archives, the digest of every file inside. It is RE-VERIFIED against the bytes on disk before it is believed, and the publisher uploads exactly the assets it names — so the set that is gated and the set that is published are the same set by construction")
	releaseDist := fs.String("release-dist", ".", "directory the release manifest's paths are relative to")
	var authorityKeys stringList
	fs.Var(&authorityKeys, "authority-key", "a PREDECLARED authority public key (hex); repeatable and required with --index")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*index == "") == (*in == "") {
		return fmt.Errorf("pass exactly one of --index (a campaign) or --in (the calculator)")
	}
	release := walltime.CampaignRelease{SHA: strings.TrimSpace(*releaseSHA)}
	if strings.TrimSpace(*releaseManifest) != "" {
		var m walltime.ReleaseManifest
		if err := walltime.ReadJSONFile(*releaseManifest, &m); err != nil {
			return err
		}
		// The manifest is a document, and a document can be edited. Checking
		// it against the files it describes is what stops a hand-written one
		// claiming the campaign's binary is inside an archive that does not
		// contain it.
		if problems := m.Verify(*releaseDist); len(problems) > 0 {
			return fmt.Errorf("the release manifest does not describe the files on disk:\n  %s", strings.Join(problems, "\n  "))
		}
		release.Manifest = &m
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
	fs.Var(&recordSigners, "record-signer", "a PREDECLARED run-key PUBLIC key (hex) allowed to sign this measurement's signer roster and closing seal; repeatable. The authority-signed Stage-1 manifest is the authoritative source and WINS OUTRIGHT: when it declares any signer, these are ignored entirely rather than added to it, because a caller who could add a key could authorise whoever attests its own measurement. They are used only when Stage 1 declares none")
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
			// THE DELIVERY VERIFIER SIGNS, not the Stage-1 authority.
			//
			// The identity is chosen by verdictSigningIdentity so the rule is
			// one function rather than one line inside a command, and can be
			// exercised directly against the campaign loader's requirement.
			//
			// A signature covers `authority NUL digest`, and the retained
			// authority is the party the signature is made under. The campaign
			// loader requires that party to be the delivery verifier the
			// verdict's own body names — because a verdict signed under some
			// other identity attributes a row to somebody who did not verify
			// it.
			//
			// This used to sign under --authority, which in the scored
			// workflow is the protected Stage-1 environment `ewj2-campaign`,
			// while the body's verifier identity comes from the measured
			// records and is `ewj2-verifier`. The producer therefore emitted,
			// by construction, exactly the verdict the production campaign
			// loader refuses: no genuine population could ever have been
			// assembled. --authority keeps its own job, which is saying which
			// protected environment must have approved Stage 1.
			identity, err := verdictSigningIdentity(v)
			if err != nil {
				return err
			}
			if err := v.Sign(identity, priv); err != nil {
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
