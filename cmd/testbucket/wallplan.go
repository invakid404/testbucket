package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/planbind"
	"github.com/invakid404/testbucket/internal/runner/vitestrunner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// runWallBundle freezes a planning-input bundle: the canonical instant, the
// raw discovery bytes, the raw runnable listings of every name-sliced target,
// the store bytes, and the acquisition closure.
//
// This is the ONLY command in the wall-time path that reads the live world.
// Everything downstream — planning, rendering, the matrix, the script — is a
// function of what this wrote.
func runWallBundle(args []string) error {
	defDiscoveryTimeout, err := discoveryTimeoutDefault()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("wall bundle", flag.ExitOnError)
	out := fs.String("out", "", "write the planning-input bundle here (required)")
	root := fs.String("root", ".", "vitest project directory")
	store := fs.String("store", "test-timings.json", "timing store to freeze; a missing store is bound as a cold start")
	k := fs.Int("k", 6, "number of buckets")
	instant := fs.String("instant", "", "canonical planning instant (RFC3339); empty uses the current time ONCE, here, and freezes it")
	staleAfter := fs.Duration("stale-after", 14*24*time.Hour, "frozen staleness policy")
	vitestCommand := fs.String("vitest-command", "", "bare-vitest invocation; empty means \"npx vitest\"")
	vitestDiscovery := fs.String("vitest-discovery", "glob", "vitest discovery mode: glob or list")
	vitestDiscoveryCommand := fs.String("vitest-discovery-command", "", "override discovery with a command run VERBATIM")
	discoveryTimeout := fs.Duration("discovery-timeout", defDiscoveryTimeout, "fail-fast deadline for discovery")
	eventsDir := fs.String("events-dir", "", "events directory the rendered script tees into")
	fileParallelism := fs.Int("file-parallelism", 1, "intra-bucket file concurrency")
	wallDir := fs.String("wall-dir", "", "records directory; when set, every rendered invocation runs under `testbucket wall exec`")
	repository := fs.String("repository", "", "source repository identity")
	commit := fs.String("commit", "", "source commit")
	tree := fs.String("tree", "", "source tree digest")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("--out is required")
	}

	// The canonical instant is read at most once, here, and then frozen. Every
	// later step reads it from the bundle.
	now := time.Now().UTC()
	if strings.TrimSpace(*instant) != "" {
		now, err = time.Parse(time.RFC3339Nano, *instant)
		if err != nil {
			return fmt.Errorf("--instant %q must be RFC3339: %w", *instant, err)
		}
	}

	rnr, err := vitestrunner.New(vitestrunner.Options{
		Root:             *root,
		Command:          splitCommand(*vitestCommand),
		DiscoveryMode:    *vitestDiscovery,
		DiscoveryCommand: splitCommand(*vitestDiscoveryCommand),
		DiscoveryTimeout: *discoveryTimeout,
		// THE ENVIRONMENT IS FIXED BEFORE ANYTHING RUNS, and it is the same
		// one the bundle retains. Collecting it afterwards recorded what the
		// process happened to hold at the end of the acquisition, beside
		// subprocesses that had inherited whatever was ambient at the start.
		Env: planningEnvArgs(),
	})
	if err != nil {
		return err
	}
	ctx := context.Background()

	// ONE live discovery. Everything downstream — the target set, the project
	// scoping for name listings, the bundle itself — is derived from these
	// exact bytes. Calling Discover again would take a second observation that
	// could disagree with the one being frozen, which is precisely the unbound
	// input the bundle exists to close.
	discovery, err := rnr.CaptureDiscovery(ctx)
	if err != nil {
		return err
	}
	frozen, err := vitestrunner.New(vitestrunner.Options{
		Root:   *root,
		Frozen: &vitestrunner.FrozenInputs{Discovery: discovery},
	})
	if err != nil {
		return err
	}
	live, err := frozen.Discover(ctx)
	if err != nil {
		return fmt.Errorf("parse the captured discovery snapshot: %w", err)
	}

	// ONE read of the store, too. The same bytes decide which targets need a
	// runnable listing AND are frozen into the bundle: reading it twice would
	// let a store that changed between the two reads make the capture decision
	// and the frozen evidence disagree.
	storeBytes, err := os.ReadFile(*store)
	storeAbsent := false
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read store %s: %w", *store, err)
		}
		storeBytes, storeAbsent = nil, true
	}

	// Capture a runnable listing for exactly the targets the store has flagged
	// for name slicing. Listing every file would import the whole project —
	// the cost `vitest list --filesOnly` discovery exists to avoid — and
	// listing none would leave the slice's names unbound.
	runnables := map[string][]byte{}
	st, _, err := core.ParseStore(storeBytes, *store)
	if err != nil {
		return err
	}
	runnableArgv := map[string][]string{}
	if st != nil {
		var sliced []string
		for _, p := range live {
			row := st.Units[p.ID]
			if row != nil && row.Split == "run" && row.SplitInto >= 2 {
				sliced = append(sliced, p.ID)
			}
		}
		sort.Strings(sliced)
		for _, id := range sliced {
			raw, argv, err := rnr.CaptureRunnables(ctx, id, discovery)
			if err != nil {
				return fmt.Errorf("capture runnables for %s: %w", id, err)
			}
			// The argv that ACTUALLY ran, not a reconstruction of it. A
			// provenance record naming a command nobody executed is worse than
			// none: a replay would reproduce it, get different bytes, and have
			// nothing to explain the difference.
			runnables[id], runnableArgv[id] = raw, argv
		}
	}

	// THE ROOT EVERY SUBPROCESS ACTUALLY RAN FROM, canonicalised by the runner
	// itself. `--root .` defaults the caller's spelling into the bundle, and
	// "." names a different directory from every other working directory in
	// the world — so a replay could not know where discovery had run.
	acquiredRoot := rnr.Root()
	// THE DISCOVERY INVOCATION THAT RAN, taken from the operation that issued
	// it rather than rebuilt here from the same flags. The two agreed, and
	// were not one observed value.
	observedDiscoveryArgv := discoveryArgv(*vitestCommand, *vitestDiscovery, *vitestDiscoveryCommand)
	// THE EXECUTABLES THAT ACTUALLY RAN, keyed by the name that named them.
	observedPaths := map[string]string{}
	if seen := rnr.Discovered(); seen != nil {
		observedDiscoveryArgv = seen.Argv
		acquiredRoot = seen.Cwd
		if len(seen.Argv) > 0 && seen.Path != "" {
			observedPaths[seen.Argv[0]] = seen.Path
		}
	}
	for _, p := range rnr.RunnablePaths() {
		observedPaths[p.Name] = p.Path
	}
	bundle, err := planbind.Acquire(planbind.AcquireOptions{
		Root: acquiredRoot, Runner: "vitest", Instant: now, StaleAfter: *staleAfter,
		K: *k, Count: 1, Token: rnr.CanonicalToken(),
		StorePath: *store, StoreBytes: storeBytes, StoreAbsent: storeAbsent,
		DiscoveryArgv: observedDiscoveryArgv,
		Discovery:     discovery, Runnables: runnables, RunnableArgv: runnableArgv,
		// The REAL invocation, as it was run. os.Args[0] is the program this
		// process actually is, so the closure resolves the binary that took
		// the snapshots rather than whatever `testbucket` resolves to on the
		// next machine.
		Env: planningEnv(), Resolve: closureResolver(acquiredRoot, observedPaths),
		BundleArgv: append([]string(nil), os.Args...),
		Repository: *repository, Commit: *commit, Tree: *tree,
		EventsDir: *eventsDir, FileParallelism: *fileParallelism, WallDir: *wallDir,
	})
	if err != nil {
		return err
	}
	if err := walltime.WriteJSONFile(*out, bundle); err != nil {
		return err
	}
	d, err := bundle.DigestOf()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: froze %d discovery byte(s) and %d runnable listing(s) at %s\nbundle digest: %s\n",
		len(discovery), len(runnables), bundle.Clock.Instant, d)
	fmt.Println(d)
	return nil
}

// discoveryArgv records how discovery was invoked, for the acquisition
// closure. It mirrors the adapter's own selection rather than guessing.
func discoveryArgv(command, mode, override string) []string {
	if strings.TrimSpace(override) != "" {
		return splitCommand(override)
	}
	base := splitCommand(command)
	if len(base) == 0 {
		base = []string{"npx", "vitest"}
	}
	if mode == "list" {
		return append(base, "list", "--json")
	}
	return append(base, "list", "--filesOnly", "--json")
}

// planningEnv is the environment the acquisition subprocesses ran with,
// RETAINED EXACTLY so the run can be reproduced rather than described.
//
// It was an allow-list of exact values plus, for everything else inherited, a
// digest of the value. The set was complete and the CONTENT was not: a digest
// says that a variable had some value, and nobody can rerun `vitest list` from
// a hash. The subprocesses inherited the whole ambient environment through a
// nil Cmd.Env, so the plan was derived under an environment that could not be
// reconstructed from the bundle that is supposed to bind its inputs.
//
// Now the map IS the environment: planningEnvironment builds both this record
// and the exact KEY=VALUE set the subprocesses are given, from one read, so
// the retained environment and the executed environment cannot differ.
//
// The wall-time secret and capability keys are the ONE exception. They are
// withheld from the subprocess as well as from the record — the same six keys
// the observer launcher scrubs — so nothing is claimed to have run with a
// value that was not there. Their names and value digests are retained, which
// makes their presence and any change to them visible without writing a
// signing key into a bundle meant to be published.
func planningEnv() map[string]string {
	record, _ := planningEnvironment()
	return record
}

// planningEnvArgs is the exact environment handed to every acquisition
// subprocess, as KEY=VALUE.
func planningEnvArgs() []string {
	_, env := planningEnvironment()
	return env
}

// planningSnapshot is THE ONE READ of the process environment.
//
// The comment used to claim a single snapshot while the code took two: the
// runner was constructed from planningEnvArgs() and the bundle recorded
// planningEnv() afterwards, and each called os.Environ() for itself. Anything
// that changed a variable between those two calls — and Go makes that possible
// and concurrency-safe — produced a bundle describing an environment the
// subprocesses had not run under, which is exactly the equivalence the bundle
// exists to establish.
//
// One memoized read makes the two call sites return the same environment by
// construction rather than by ordering. It is memoized rather than threaded
// through because both helpers are called from different layers of the command
// and the property must hold however they are ordered.
var planningSnapshot = sync.OnceValue(os.Environ)

// resetPlanningSnapshot re-reads the environment. Only tests use it: a test
// that sets a variable and then asks what the acquisition would record needs
// the snapshot taken after its own setup, and nothing in production may
// re-read it.
func resetPlanningSnapshot() { planningSnapshot = sync.OnceValue(os.Environ) }

func planningEnvironment() (map[string]string, []string) {
	withheld := map[string]bool{}
	for _, k := range walltime.WallTimeSecretEnv {
		withheld[k] = true
	}
	record := map[string]string{}
	var env []string
	for _, kv := range planningSnapshot() {
		k, v, _ := strings.Cut(kv, "=")
		if withheld[k] {
			record["digest:"+k] = string(walltime.DigestBytes([]byte(v)))
			continue
		}
		record[k] = v
		env = append(env, kv)
	}
	sort.Strings(env)
	return record, env
}

// closureResolver resolves the executable and tool closure FOR ONE ARGV.
//
// The closure is a function of the argv the snapshot was actually taken by,
// not of the configured Vitest command. Those are the same thing only until
// somebody passes --vitest-discovery-command, and the old resolver kept
// answering about the ordinary command: the bundle then carried a resolution
// of a program that had not run, which a replay would follow to the wrong
// binary with nothing to explain the difference.
//
// It FAILS rather than recording `unresolved`. A resolution nobody could make
// is an unbound input, and `wall bundle` is the one place that can still
// refuse to freeze one; a bundle that carried it would be signed by Stage 1
// before anybody noticed.
func closureResolver(root string, observed map[string]string) func([]string) (map[string]string, map[string]walltime.ToolIdentity, error) {
	return func(argv []string) (map[string]string, map[string]walltime.ToolIdentity, error) {
		if len(argv) == 0 {
			return nil, nil, fmt.Errorf("no argv to resolve")
		}
		head := argv[0]
		// THE PATH THAT ACTUALLY RAN, when the operation observed one.
		//
		// Resolving the head again here answers "what would this name resolve
		// to now", which is a different question from "what executed", and on
		// a changed PATH it is a different answer. The acquisition records the
		// resolved executable `exec.Command` used; that is the binary the
		// snapshot came from, so it is the binary the closure binds.
		path, ok := observed[head]
		if !ok {
			var err error
			if path, err = resolveProgram(head); err != nil {
				return nil, nil, err
			}
		}
		execs := map[string]string{head: path}
		// The DELEGATED program, when the head is only a launcher.
		//
		// The frozen Mandel façade is `pnpm exec tsx scripts/tb-vitest.ts`.
		// Resolving argv[0] alone bound pnpm and called the closure complete,
		// while the program that actually launches the TypeScript façade —
		// the package-selected `tsx` shim — was never resolved, hashed or
		// named. "The exact resolved executable path" has to mean the
		// executable that runs, not the first word of the command line.
		delegated, err := delegatedProgram(root, argv)
		if err != nil {
			return nil, nil, err
		}
		if delegated.name != "" {
			execs[delegated.name] = delegated.path
		}
		tools := map[string]walltime.ToolIdentity{}
		// The head and any delegated program are what ran; node and npm are
		// the toolchain they ran on. All are bound, deduplicated by name,
		// because the same resolved path under two toolchains is two different
		// observations.
		for _, name := range dedupe([]string{head, delegated.name, "node", "npm"}) {
			t, err := resolveToolAt(root, name, execs[name])
			if err != nil {
				return nil, nil, err
			}
			tools[name] = t
		}
		return execs, tools, nil
	}
}

// packageRunners are the launchers whose next non-flag argument is a PACKAGE
// EXECUTABLE they select and execute, rather than a file they read.
var packageRunners = map[string]map[string]bool{
	"pnpm": {"exec": true, "dlx": true},
	"npm":  {"exec": true},
	"yarn": {"exec": true, "dlx": true},
	"bun":  {"x": true},
	"npx":  {},
}

type delegated struct{ name, path string }

// delegatedProgram resolves the executable a launcher selects.
//
// It looks where the package manager looks — the project's `node_modules/.bin`
// under the acquisition cwd, then PATH — because that is what decides which
// bytes run. A launcher whose delegated program cannot be resolved is an
// unbound input and fails closed, exactly as an unresolvable head does: the
// façade would run something, and the bundle could not say what.
func delegatedProgram(root string, argv []string) (delegated, error) {
	if len(argv) < 2 {
		return delegated{}, nil
	}
	sub, ok := packageRunners[filepath.Base(argv[0])]
	if !ok {
		return delegated{}, nil
	}
	rest := argv[1:]
	// `npx <prog>` delegates directly; `pnpm exec <prog>` needs its
	// subcommand first.
	if len(sub) > 0 {
		if !sub[rest[0]] {
			return delegated{}, nil
		}
		rest = rest[1:]
	}
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return delegated{}, nil
	}
	name := rest[0]
	if p := filepath.Join(root, "node_modules", ".bin", name); fileExists(p) {
		// ABSOLUTE. With a relative root this returned `node_modules/.bin/...`,
		// which names a different program in every working directory — an
		// executable identity that is not an identity.
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		return delegated{name: name, path: p}, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return delegated{}, fmt.Errorf("resolve the %s-selected executable %q under %s: %w; the program that actually launches the façade may not be left unbound",
			argv[0], name, root, err)
	}
	return delegated{name: name, path: p}, nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// resolveProgram finds the exact file a program name runs. `testbucket` names
// THIS process: it is the program that took the snapshot, and asking PATH for
// it would resolve whatever copy happens to be installed instead.
func resolveProgram(name string) (string, error) {
	if name == "testbucket" {
		self, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve this executable: %w", err)
		}
		return self, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w; a plan may not be derived from an unresolved program", name, err)
	}
	return p, nil
}

// resolveTool records what a tool IS: its resolved path, the version it
// reports, and the SHA-256 of the exact bytes that reported it. A version
// alone is a program's own account of itself, and two builds can tell the
// same story.
func resolveTool(root, name string) (walltime.ToolIdentity, error) {
	return resolveToolAt(root, name, "")
}

// resolveToolAt is resolveTool for a program whose path is already known —
// a package-selected shim is not on PATH, and asking PATH for it would either
// fail or resolve a different installation.
func resolveToolAt(root, name, known string) (walltime.ToolIdentity, error) {
	path := known
	if path == "" {
		var err error
		path, err = resolveProgram(name)
		if err != nil {
			return walltime.ToolIdentity{}, err
		}
	}
	integrity, err := walltime.FileDigest(path)
	if err != nil {
		return walltime.ToolIdentity{}, fmt.Errorf("digest %s at %s: %w", name, path, err)
	}
	version, err := toolVersion(root, name, path)
	if err != nil {
		return walltime.ToolIdentity{}, err
	}
	return walltime.ToolIdentity{Version: version, Path: path, Integrity: integrity}, nil
}

// toolVersion asks a tool what it is. This binary answers for itself rather
// than re-executing: `wall bundle` is already running it, and a subprocess
// would be a second, unbound observation of the same program.
func toolVersion(root, name, path string) (string, error) {
	if name == "testbucket" {
		return version, nil
	}
	cmd := exec.Command(path, "--version")
	cmd.Dir = root
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ask %s at %s for its version: %w", name, path, err)
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("%s at %s reported an empty version", name, path)
	}
	return v, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// runWallReplay is the independent verifier's half of Stage 2: it replays the
// frozen bundle through the real planner and refuses to agree unless every
// digest matches the receipt that was issued.
func runWallReplay(args []string) error {
	fs := flag.NewFlagSet("wall replay", flag.ExitOnError)
	bundlePath := fs.String("bundle", "", "planning-input bundle to replay (required)")
	receiptPath := fs.String("stage2", "", "Stage-2 derived-plan receipt to check against (required)")
	stage1Path := fs.String("stage1", "", "Stage-1 manifest whose digest the receipt must name")
	var authorityKeys stringList
	fs.Var(&authorityKeys, "authority-key", "a PREDECLARED authority public key (hex) the Stage-1 signature must come from; repeatable and required with --stage1")
	authority := fs.String("authority", "", "the EXACT protected environment that must have approved Stage 1, e.g. "+walltime.CampaignAuthority+". Required with --stage1: a key check alone accepts a correctly keyed manifest approved under some other label, and this is the pre-action gate — a refusal afterwards cannot un-run the measured work")
	scorerPath := fs.String("scorer", "", "the frozen scorer the plan allocated with; its digest must match the training lineage Stage 1 bound")
	shardPlan := fs.String("shard-plan", "", "also write the replayed plan here")
	registryPath := fs.String("registry", "", "frozen Aeta component-registry template. Required when the issued receipt binds per-bucket documents: an independent replay that skipped them would leave exactly those documents unre-derived")
	attest := fs.String("attest", "", "write a SIGNED replay attestation here (signing key from TB_WALL_REPLAY_KEY). `wall verify` requires one, signed by a key Stage 1 declared as a replay signer and distinct from the authority key: comparing the planner's account of its own output to itself proves nothing, and neither does having the issuer of the plan re-check it")
	verifierID := fs.String("verifier-id", "", "identity of the party running this replay (required with --attest)")
	// The four identities the ACTION will stamp onto every measured record.
	// Replaying the documents proves the plan; it says nothing about the
	// separate strings a caller passes to the wrapper, and a mismatch there
	// used to be discovered only by verification AFTER the bucket had run.
	expectStage1 := fs.String("expect-stage1", "", "the Stage-1 digest the measured records will carry. Compared with this replay's own derivation and refused before anything is measured: verification after the fact can refuse the row, it cannot un-run it")
	expectStage2 := fs.String("expect-stage2", "", "the Stage-2 digest the measured records will carry")
	expectRegistry := fs.String("expect-registry", "", "the Aeta component-registry digest the measured records will carry")
	expectVerifier := fs.String("expect-verifier-id", "", "the verifier identity the measured records will carry; it must be non-empty for a scored row, because a record naming no verifier is attributable to none")
	expectAttestation := fs.String("expect-attestation", "", "the SIGNED replay attestation the measured records will be verified against. Its signature is authenticated against the replay signers Stage 1 declares and its verifier identity is compared with --expect-verifier-id, BEFORE anything is measured: on its own --expect-verifier-id only proves a caller supplied SOME identity, so a different nonblank one opened the envelope, ran the tests, and was refused only by verification afterwards")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *bundlePath == "" || *receiptPath == "" {
		return fmt.Errorf("--bundle and --stage2 are both required")
	}

	var bundle walltime.PlanningInputBundle
	if err := walltime.ReadJSONFile(*bundlePath, &bundle); err != nil {
		return err
	}
	var issued walltime.Stage2Receipt
	if err := walltime.ReadJSONFile(*receiptPath, &issued); err != nil {
		return err
	}
	if err := issued.Validate(); err != nil {
		return err
	}

	stage1 := issued.Stage1Digest
	var lineage walltime.TrainingLineageID
	// The keys Stage 1 declares for an INDEPENDENT replay, and the rest of the
	// instrumentation identity it approved. They are what makes the
	// attestation compared below evidence rather than a file, so they are
	// taken from the authority-signed manifest this replay just checked, never
	// from the attestation itself.
	var replaySigners []string
	var instrumentation walltime.InstrumentationIdentity
	// The approval this replay independently observed on the Stage-1 manifest.
	// An empty one means no manifest was supplied, and Matches then reports
	// the receipt's claim as unre-derived rather than agreeing with it.
	var replayApproval walltime.Stage1Approval
	if *stage1Path != "" {
		var m walltime.Stage1Manifest
		if err := walltime.ReadJSONFile(*stage1Path, &m); err != nil {
			return err
		}
		if err := m.Validate(); err != nil {
			return err
		}
		d, err := m.DigestOf()
		if err != nil {
			return err
		}
		if d != issued.Stage1Digest {
			return fmt.Errorf("the receipt names Stage-1 %s but the supplied manifest digests to %s", issued.Stage1Digest, d)
		}
		// The same rule the verifier applies: a signature checked against
		// whatever signed it is not an authority check. A replay that vouched
		// for a manifest signed by an undeclared key would launder it.
		if len(authorityKeys) == 0 {
			return fmt.Errorf("--stage1 needs at least one --authority-key: verifying a signature against whatever signed the document accepts any self-generated key")
		}
		if strings.TrimSpace(*authority) == "" {
			return fmt.Errorf("--stage1 needs --authority: the contract puts the PROTECTED environment's approval before the plan, and a key check alone accepts a correctly keyed manifest approved under some other label")
		}
		// The EXACT label, not merely a signature that is self-consistent with
		// whatever label it carries. A key can sign under any name; which
		// protected environment approved is the question the contract asks
		// before AT_start.
		if err := m.RequireApproval(authorityKeys, *authority); err != nil {
			return err
		}
		stage1 = d
		lineage = m.TrainingLineage
		replaySigners = m.Instrumentation.ReplaySigners
		instrumentation = m.Instrumentation
		if replayApproval, err = walltime.ApprovalOf(m); err != nil {
			return err
		}
	}

	// The scorer is a delivery-bound identity: Stage 1 names its digest, and a
	// replay that used different coefficients would produce a different plan
	// for a reason nobody authorised. Supplying the wrong bytes is caught here
	// rather than showing up as an unexplained digest mismatch below.
	var scorer *walltime.Scorer
	if *scorerPath != "" {
		var sc walltime.Scorer
		if err := walltime.ReadJSONFile(*scorerPath, &sc); err != nil {
			return err
		}
		d, err := sc.DigestOf()
		if err != nil {
			return err
		}
		if lineage.ScorerDigest != "" && lineage.ScorerDigest != d {
			return fmt.Errorf("the supplied scorer digests to %s but Stage 1 binds %s", d, lineage.ScorerDigest)
		}
		scorer = &sc
	} else if lineage.ScorerDigest != "" {
		return fmt.Errorf("Stage 1 binds scorer %s but none was supplied; pass --scorer so the replay allocates the way the plan did", lineage.ScorerDigest)
	}

	res, err := planbind.Plan(context.Background(), planbind.PlanOptions{Bundle: &bundle, Stage1: stage1, Scorer: scorer})
	if err != nil {
		return err
	}
	// The replay re-derives the approval too: it is a field of the receipt it
	// is checking, so leaving it out would let a receipt claim an approval no
	// independent party ever saw.
	res.Receipt.Stage1Approval = replayApproval
	// The replay re-derives the PER-BUCKET documents too. Comparing only the
	// aggregate digests would leave the Pcheck projections, forecasts and
	// invocation manifests — the documents the buckets actually run against —
	// re-derived by nobody.
	if err := deriveDocuments(res, *registryPath, ""); err != nil {
		return err
	}
	// The registry's own identity, derived here rather than taken from the
	// receipt, so the value compared below is one this replay computed.
	var registryDigest walltime.Digest
	if *registryPath != "" {
		var reg walltime.AetaRegistry
		if err := walltime.ReadJSONFile(*registryPath, &reg); err != nil {
			return err
		}
		if registryDigest, err = reg.DigestOf(); err != nil {
			return err
		}
	}
	if err := issued.Matches(res.Receipt); err != nil {
		return fmt.Errorf("the replayed plan does not match the issued receipt: %w", err)
	}
	// The identities the ACTION will stamp on every measured record, compared
	// with the ones this replay just derived — BEFORE anything is measured.
	// Run whenever ANY of the four was supplied: binding some of the record
	// identities and not the rest is the same hole in a smaller shape. The
	// eligible guard makes a scored request supply all four.
	if *expectStage1 != "" || *expectStage2 != "" || *expectRegistry != "" || *expectVerifier != "" {
		if err := checkRecordIdentities(issued, stage1, registryDigest, *expectStage1, *expectStage2, *expectRegistry, *expectVerifier); err != nil {
			return err
		}
		// And the verifier identity is bound to a SIGNED replay OF THIS PLAN,
		// not merely to one that is signed and non-empty.
		stage2Digest, err := issued.DigestOf()
		if err != nil {
			return err
		}
		if err := checkAttestedVerifier(*expectAttestation, preflightPlan{
			issued: issued, stage2: stage2Digest, stage1: stage1, instrumentation: instrumentation,
		}, *expectVerifier, replaySigners, authorityKeys); err != nil {
			return err
		}
	}
	if *attest != "" {
		if err := writeReplayAttestation(*attest, *verifierID, issued, bundle, res.Receipt); err != nil {
			return err
		}
	}
	if *shardPlan != "" {
		if err := writeJSONFile(*shardPlan, res.Doc); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: replay reproduced the plan exactly\n  full document: %s\n  semantic:      %s\n",
		res.Receipt.PlanDigest, res.Receipt.SemanticDigest)
	return nil
}

// checkRecordIdentities compares the four identities a caller will stamp onto
// every measured record with the ones this replay independently derived.
//
// The pre-flight replayed the frozen documents and stopped there. But
// `run-bucket` takes those four values as SEPARATE caller strings and writes
// them onto every record, so an empty or mismatched one passed the pre-flight,
// opened the action envelope, ran the tests, and was refused only afterwards by
// verification. Refusal after the work has run is not the contract's
// before-AT_start equivalence — it can invalidate the row, it cannot un-measure
// it, and the runner has already spent the time.
//
// An expectation nobody supplied is not checked: this command is also used
// outside the action, where those strings do not exist. The workflow supplies
// all four, and the eligible guard refuses a scored request that omits them.
func checkRecordIdentities(issued walltime.Stage2Receipt, stage1, registry walltime.Digest, wantStage1, wantStage2, wantRegistry, wantVerifier string) error {
	stage2, err := issued.DigestOf()
	if err != nil {
		return err
	}
	var problems []string
	for _, c := range []struct {
		what, supplied string
		derived        walltime.Digest
	}{
		{"--expect-stage1", wantStage1, stage1},
		{"--expect-stage2", wantStage2, stage2},
		{"--expect-registry", wantRegistry, registry},
	} {
		// MISSING IS A REFUSAL. Skipping an empty expectation made all four
		// identities optional: a scored request could omit every one, pass
		// pre-flight, open the envelope and run the tests, and be refused only
		// by verification afterwards. An identity nobody supplied is not an
		// identity that agrees.
		if strings.TrimSpace(c.supplied) == "" {
			problems = append(problems, fmt.Sprintf("%s was not supplied; the records will carry an identity this pre-flight never compared", c.what))
			continue
		}
		if c.derived == "" {
			problems = append(problems, fmt.Sprintf("%s was supplied as %s, but this replay derived no such identity to compare it with", c.what, c.supplied))
			continue
		}
		if walltime.Digest(strings.TrimSpace(c.supplied)) != c.derived {
			problems = append(problems, fmt.Sprintf("the records will be stamped %s=%s, but the frozen documents derive %s", c.what, c.supplied, c.derived))
		}
	}
	// The verifier identity has no document to derive it from; what a
	// pre-flight can prove about it is that it exists. A record naming no
	// verifier is attributable to none, and that is decidable here.
	if strings.TrimSpace(wantVerifier) == "" {
		problems = append(problems, "--expect-verifier-id is blank or absent; a record that names no verifier identity is attributable to nobody")
	}
	if len(problems) > 0 {
		return fmt.Errorf("the identities the measured records will carry do not match the frozen plan, and no measured work may start:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// checkAttestedVerifier binds the verifier identity the measured records will
// carry to the SIGNED replay attestation the row will later be verified
// against — before AT_start.
//
// --expect-verifier-id is a caller string like the other three, but unlike them
// it has no document to be derived from, so the pre-flight could only say it
// was non-blank. That is not the contract's resolved-value equivalence: a
// caller supplying a different, perfectly non-blank identity passed the
// pre-flight, opened the action envelope and ran the tests, and the genuine
// signed replay disagreed with it only at verification. A refusal afterwards
// can invalidate the row; it cannot un-measure it.
//
// The attestation is AUTHENTICATED here under exactly the rule the verifier
// applies — signed by a replay signer the authority-signed Stage-1 manifest
// declares, distinct from the authority key, under the identity it names.
// Comparing against an unauthenticated file would prove nothing: the caller
// could write one that agrees with whatever it passed.
// preflightPlan is the plan the pre-flight is running for, carried as one
// value so the replay comparator cannot be handed a subset of it.
//
// Every field is one this command DERIVED or VERIFIED for itself: the issued
// receipt it read, the digest it computed from that receipt, the Stage-1
// digest it checked the authority signature over, and the instrumentation
// identity that authority-signed manifest declares. None of them comes from
// the attestation being checked, which would be the document vouching for
// itself.
type preflightPlan struct {
	issued          walltime.Stage2Receipt
	stage2          walltime.Digest
	stage1          walltime.Digest
	instrumentation walltime.InstrumentationIdentity
}

func checkAttestedVerifier(path string, plan preflightPlan, wantVerifier string, replaySigners, authorityKeys []string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("--expect-attestation was not supplied, so --expect-verifier-id is checked only for being non-blank; any other identity a caller resolves would open the envelope and be refused only after the bucket had run")
	}
	var a walltime.ReplayAttestation
	if err := walltime.ReadJSONFile(path, &a); err != nil {
		return fmt.Errorf("replay attestation: %w", err)
	}
	if len(replaySigners) == 0 {
		return fmt.Errorf("no Stage-1 replay signer is available to this pre-flight, so the attestation it is about to compare against could have been signed by anyone, including the party that issued the plan; pass --stage1 with a manifest that declares one")
	}
	// The same independence rule the verifier enforces: an attestation the
	// plan's own authority could have signed is the issuer re-checking itself.
	for _, rk := range replaySigners {
		for _, ak := range authorityKeys {
			if rk == ak {
				return fmt.Errorf("replay signer %s is also a predeclared authority key; a replay by the issuer of the plan is not an independent re-derivation", rk)
			}
		}
	}
	if a.Signature == nil {
		return fmt.Errorf("the replay attestation is unsigned, so the verifier identity it names is an assertion by whoever wrote the file")
	}
	d, err := a.DigestOf()
	if err != nil {
		return fmt.Errorf("replay attestation: %w", err)
	}
	if err := walltime.VerifySigned(a.Signature, d, replaySigners); err != nil {
		return fmt.Errorf("replay attestation signature: %w (only a replay signer Stage 1 declares may attest an independent re-derivation)", err)
	}
	if strings.TrimSpace(a.VerifierID) == "" {
		return fmt.Errorf("the replay attestation names no verifier identity, so there is nothing for the records' identity to be equivalent to")
	}
	// Signed UNDER the identity it names. A signature made under some other
	// label would let a declared replay key attest on behalf of an identity
	// that never ran anything.
	if a.Signature.Authority != a.VerifierID {
		return fmt.Errorf("the replay attestation names verifier %q but was signed under authority %q; a valid key signing under another party's identity is not that party's attestation", a.VerifierID, a.Signature.Authority)
	}
	if strings.TrimSpace(wantVerifier) != a.VerifierID {
		return fmt.Errorf("the measured records will be stamped --expect-verifier-id=%q, but the signed replay attestation this row is verified against was made by %q; the two must be the same resolved value BEFORE any measured work starts", strings.TrimSpace(wantVerifier), a.VerifierID)
	}

	// AND IT MUST BE A REPLAY OF THIS PLAN.
	//
	// Everything above authenticates the document and its signer. None of it
	// asks which plan the document is about, so an attestation from an
	// entirely different plan — correctly signed, by a declared replay signer,
	// under the expected verifier identity — opened the gate. `wall verify`
	// rejected it afterwards, which is the wrong side of AT_start: a refusal
	// after the fact can invalidate the row, it cannot un-measure it.
	//
	// This is the SAME check the post-action verifier runs, run here, so the
	// two boundaries cannot disagree about what counts as an attested plan.
	if problems := a.Verify(plan.issued, plan.stage2, plan.stage1, plan.instrumentation, strings.TrimSpace(wantVerifier)); len(problems) > 0 {
		return fmt.Errorf("the signed replay attestation does not attest the plan being pre-flighted, and no measured work may start:\n  %s",
			strings.Join(problems, "\n  "))
	}
	return nil
}

// writeReplayAttestation records that THIS party independently re-derived the
// plan and got the issued receipt.
//
// It is signed, because an attestation nobody can attribute is an assertion.
// The signing key is the authority's, read from the environment for the same
// reason `wall stage1` reads it there: a key on a command line is a key in the
// process table.
func writeReplayAttestation(path, verifierID string, issued walltime.Stage2Receipt, bundle walltime.PlanningInputBundle, recomputed walltime.Stage2Receipt) error {
	if strings.TrimSpace(verifierID) == "" {
		return fmt.Errorf("--attest needs --verifier-id: an attestation nobody can attribute is an assertion")
	}
	// A key of the replay party's OWN, not the campaign authority's. Signing
	// an "independent" re-derivation with the key that authorised the plan
	// would make independence a label on a document rather than a property of
	// who produced it, and the verifier now refuses that pairing outright.
	key, err := walltime.DecodeKey(strings.TrimSpace(os.Getenv(replayKeyEnv)))
	if err != nil {
		return fmt.Errorf("%s: %w (an independent attestation must be signed by the replaying party, not by the plan's authority)", replayKeyEnv, err)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	selfDigest, err := walltime.FileDigest(self)
	if err != nil {
		return err
	}
	issuedDigest, err := issued.DigestOf()
	if err != nil {
		return err
	}
	bundleDigest, err := bundle.DigestOf()
	if err != nil {
		return err
	}
	a := walltime.ReplayAttestation{
		Kind:           walltime.ReplayKind,
		Stage1Digest:   issued.Stage1Digest,
		Stage2Digest:   issuedDigest,
		BundleDigest:   bundleDigest,
		Recomputed:     recomputed,
		VerifierID:     verifierID,
		VerifierBinary: selfDigest,
	}
	d, err := a.DigestOf()
	if err != nil {
		return err
	}
	// The retained authority and the identity the signature COVERS are the
	// same string, and that string is the replaying party.
	//
	// This used to retain `ewj2-campaign` while signing over the verifier id,
	// so the production writer emitted an artifact the production verifier
	// rejected: signatures cover `authority NUL digest`, and the two halves
	// disagreed. Naming the campaign authority here would have made them
	// agree and erased the distinction the whole attestation exists for —
	// the replay is independent precisely because it is NOT the party that
	// authorised the plan.
	a.Signature = &walltime.Signature{
		Authority: verifierID, KeyID: walltime.PublicKeyOf(key), Digest: d,
		Value: walltime.SignApproval(verifierID, key, d),
	}
	if err := walltime.WriteJSONFile(path, a); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: attested the replay as %s\n  attestation digest: %s\n", verifierID, d)
	return nil
}

// planFromBundle is `plan`'s frozen path: instead of discovering and reading
// the clock, it replays a bundle and writes the Stage-2 receipt.
//
// The receipt is written with O_EXCL. That is the exactly-once rule made
// mechanical: the bound planner runs once, and a second run that quietly
// replaced the first receipt would be indistinguishable from the first.
// frozenPlanOptions is what the frozen `plan` path needs beyond the bundle.
type frozenPlanOptions struct {
	bundlePath string
	stage1Path string
	stage2Path string
	shardPlan  string
	asJSON     bool
	// scorerPath, when set, makes the frozen pre-plan score the ALLOCATION
	// input. Without it the partition uses the store's measured weights, which
	// is a perfectly good split and is not campaign eligible.
	scorerPath string
	// registryPath is the frozen Aeta component template; outDir is where the
	// per-bucket derived documents (Palloc, Pcheck, Aeta) are written.
	registryPath string
	outDir       string
	// authorityKeys are the PREDECLARED public keys allowed to approve the
	// Stage-1 inputs, and authority the protected environment they must name.
	// Both are required: the frozen path plans only from authorised inputs.
	authorityKeys []string
	authority     string
}

func planFromBundle(o frozenPlanOptions) error {
	bundlePath, stage1Path, stage2Path, shardPlan, asJSON := o.bundlePath, o.stage1Path, o.stage2Path, o.shardPlan, o.asJSON

	// AUTHORISATION BEFORE PLANNING — and before reading anything. The
	// contract puts an owner-authority signature on the inputs before the plan
	// exists, and the planner is where that has to be enforced: a post-run
	// verifier can refuse the row, but it cannot un-run an action or restore
	// an approval that never happened.
	//
	// All three are required on the frozen path. A frozen plan with no Stage-1
	// is an unauthorised plan wearing the frozen path's determinism; a
	// signature checked against whatever signed it is not an authority check;
	// and a key signs under any label it is given, so without the expected
	// label a manifest approved by some other protected environment drives the
	// frozen planner.
	if stage1Path == "" {
		return fmt.Errorf("--wall-stage1 is required: planning from a frozen bundle with no Stage-1 manifest is planning from inputs nobody authorised")
	}
	if len(o.authorityKeys) == 0 {
		return fmt.Errorf("--wall-authority-key is required with --wall-stage1: verifying a signature against whatever signed the document accepts any self-generated key")
	}
	if strings.TrimSpace(o.authority) == "" {
		return fmt.Errorf("--wall-authority is required with --wall-stage1: the contract puts the PROTECTED environment's approval before either role plans, and a key can sign under any label — so checking the key alone lets a manifest approved by some other environment drive the frozen planner")
	}

	var bundle walltime.PlanningInputBundle
	if err := walltime.ReadJSONFile(bundlePath, &bundle); err != nil {
		return err
	}
	var m walltime.Stage1Manifest
	if err := walltime.ReadJSONFile(stage1Path, &m); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if err := m.RequireApproval(o.authorityKeys, o.authority); err != nil {
		return err
	}
	approval, err := walltime.ApprovalOf(m)
	if err != nil {
		return err
	}
	stage1, err := m.DigestOf()
	if err != nil {
		return err
	}
	if bd, err := bundle.DigestOf(); err == nil && m.Bundle.Kind != "" {
		mbd, err := m.Bundle.DigestOf()
		if err != nil {
			return err
		}
		if mbd != bd {
			return fmt.Errorf("the Stage-1 manifest authorises input bundle %s, not the supplied %s", mbd, bd)
		}
	}

	var scorer *walltime.Scorer
	if o.scorerPath != "" {
		var sc walltime.Scorer
		if err := walltime.ReadJSONFile(o.scorerPath, &sc); err != nil {
			return err
		}
		if sc.Kind != walltime.ScorerKind {
			return fmt.Errorf("%s is not a frozen scorer (kind %q)", o.scorerPath, sc.Kind)
		}
		if sc.Lineage.ReceiptSetDigest == "" {
			return fmt.Errorf("%s names no sealed training receipt set; a scorer with no lineage cannot allocate", o.scorerPath)
		}
		scorer = &sc
	}

	res, err := planbind.Plan(context.Background(), planbind.PlanOptions{Bundle: &bundle, Stage1: stage1, Scorer: scorer})
	if err != nil {
		return err
	}
	// The approval as the PLANNER saw it. Stage-1's digest excludes the
	// detached signature, so a manifest signed after this point carries the
	// same digest — this is the field that says the approval came first.
	res.Receipt.Stage1Approval = approval
	// The derived documents are written BEFORE the receipt, because the
	// receipt binds them: a per-bucket projection or forecast that the one
	// authorised plan does not name is a document anybody could have written,
	// and the verifier now refuses it.
	if o.outDir != "" {
		if err := writeDerivedDocuments(o, res); err != nil {
			return err
		}
	}
	if shardPlan != "" {
		if err := writeJSONFile(shardPlan, res.Doc); err != nil {
			return err
		}
	}
	if stage2Path != "" {
		if err := os.MkdirAll(filepath.Dir(stage2Path), 0o755); err != nil {
			return err
		}
		if err := walltime.WriteJSONFile(stage2Path, res.Receipt); err != nil {
			return err
		}
	}

	summaryOut := os.Stdout
	if asJSON {
		summaryOut = os.Stderr
	}
	if err := res.Doc.WriteSummary(summaryOut, ""); err != nil {
		return fmt.Errorf("write plan summary: %w", err)
	}
	if asJSON {
		matrix, err := res.Doc.MatrixJSON()
		if err != nil {
			return err
		}
		if _, err := fmt.Println(string(matrix)); err != nil {
			return fmt.Errorf("write matrix: %w", err)
		}
	}
	fmt.Fprintf(os.Stderr, "testbucket plan: derived from frozen inputs\n  full document: %s\n  semantic:      %s\n",
		res.Receipt.PlanDigest, res.Receipt.SemanticDigest)
	return nil
}

// writeDerivedDocuments emits the per-bucket Palloc projection and pre-action
// forecast. Both are Stage-2 instantiations: they happen after the one
// authorised plan and before any bucket action starts, and neither can change
// the plan they describe.
func writeDerivedDocuments(o frozenPlanOptions, res *planbind.Result) error {
	return deriveDocuments(res, o.registryPath, o.outDir)
}

// deriveDocuments derives every per-bucket document this plan implies, binds
// each into the Stage-2 receipt by digest, and — when outDir is set — writes
// them.
//
// The binding is the point. A Pcheck projection, a forecast and an invocation
// manifest used to be written beside the receipt carrying nothing but a
// Stage-2 string, which any substituted document can also carry; naming them
// in the receipt puts them inside the one document that is signed and
// independently replayed. outDir is optional so the REPLAY can derive the same
// bindings and compare them without writing a second copy of the plan's
// output.
func deriveDocuments(res *planbind.Result, registryPath, outDir string) error {
	if outDir != "" {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return err
		}
	}
	// The PLAN identity, not the full receipt digest: the receipt binds these
	// documents and these documents name the receipt, so one side of that
	// circle has to cite an identity that excludes the binding. A sidecar is
	// derived from the plan, so the plan is what it cites.
	stage2, err := res.Receipt.PlanDigestOf()
	if err != nil {
		return err
	}
	res.Receipt.Sidecars = map[string]walltime.Digest{}
	emit := func(kind string, bucket int, doc any) error {
		d, err := walltime.DigestJSON(doc)
		if err != nil {
			return err
		}
		res.Receipt.Sidecars[walltime.SidecarName(kind, bucket)] = d
		if outDir == "" {
			return nil
		}
		return walltime.WriteJSONFile(filepath.Join(outDir, fmt.Sprintf("%s-%d.json", kind, bucket)), doc)
	}

	// THE ABLATION'S DERIVED PLAN, WRITTEN BY THE PLANNER THAT DERIVED IT.
	//
	// The campaign's ablation gate loads this document, rederives its three
	// digests against the Stage-2 receipt and then READS it to decide whether
	// the stratum a row is authorised into is the topology the row actually
	// ran. Nothing in production wrote it: the gate could only ever be handed
	// a document somebody composed by hand, and a hand-composed document is
	// the thing the gate exists to refuse.
	if outDir != "" {
		if err := walltime.WriteJSONFile(filepath.Join(outDir, "derived.json"), res.Derived); err != nil {
			return err
		}
	}
	if res.Allocator != nil && outDir != "" {
		if err := walltime.WriteJSONFile(filepath.Join(outDir, "palloc.json"), res.Allocator.Values()); err != nil {
			return err
		}
	}
	var registry *walltime.AetaRegistry
	if registryPath != "" {
		var r walltime.AetaRegistry
		if err := walltime.ReadJSONFile(registryPath, &r); err != nil {
			return err
		}
		if err := r.Validate(); err != nil {
			return err
		}
		registry = &r
	}
	for _, b := range res.Doc.Buckets {
		if res.Allocator != nil {
			pcheck, err := planbind.PcheckFor(res.Doc, b.Index, stage2, res.Receipt.MembershipDigest, res.Allocator)
			if err != nil {
				return err
			}
			if err := emit(walltime.SidecarPcheck, b.Index, pcheck); err != nil {
				return err
			}
		}
		// What the plan rendered for this bucket, so the verifier can compare
		// each measured invocation to it rather than take the wrapper's word.
		manifest, err := planbind.InvocationManifestFor(res.Doc, b.Index, stage2)
		if err != nil {
			return err
		}
		if err := emit(walltime.SidecarInvocations, b.Index, manifest); err != nil {
			return err
		}
		if registry == nil {
			continue
		}
		palloc := 0.0
		if res.Allocator != nil {
			if palloc, err = planbind.PallocTotal(res.Doc, b.Index, res.Allocator); err != nil {
				return err
			}
		}
		aeta, err := registry.Instantiate(walltime.AetaInputs{
			BucketID: b.Name, BucketIndex: b.Index, PallocSeconds: palloc,
			Invocations: len(b.Invocations), Stage2: stage2,
		})
		if err != nil {
			return fmt.Errorf("instantiate Aeta for bucket %d: %w", b.Index, err)
		}
		if err := emit(walltime.SidecarAeta, b.Index, aeta); err != nil {
			return err
		}
	}
	return nil
}
