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
			raw, err := rnr.CaptureRunnables(ctx, id, discovery)
			if err != nil {
				return fmt.Errorf("capture runnables for %s: %w", id, err)
			}
			runnables[id] = raw
		}
	}

	bundle, err := planbind.Acquire(planbind.AcquireOptions{
		Root: *root, Runner: "vitest", Instant: now, StaleAfter: *staleAfter,
		K: *k, Count: 1, Token: rnr.CanonicalToken(),
		StorePath: *store, StoreBytes: storeBytes, StoreAbsent: storeAbsent,
		DiscoveryArgv: discoveryArgv(*vitestCommand, *vitestDiscovery, *vitestDiscoveryCommand),
		Discovery:     discovery, Runnables: runnables,
		Env: planningEnv(), Executables: resolvedExecutables(*vitestCommand),
		Tools:      resolvedTools(*root),
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

// planningEnv records the environment variables that can change a plan. It is
// an allow-list, not the whole environment: a bundle that carried every
// variable would carry secrets, and a bundle nobody can publish is a bundle
// nobody can verify.
func planningEnv() map[string]string {
	out := map[string]string{}
	for _, k := range []string{
		"TB_DISCOVERY_EXCLUDE_PREFIXES", "TB_DISCOVERY_TIMEOUT",
		"VITEST_MODE", "NODE_ENV", "CI",
	} {
		out[k] = os.Getenv(k)
	}
	return out
}

// resolvedExecutables records where the discovery program actually resolved
// to, so "npx" naming two different binaries on two runners is visible.
func resolvedExecutables(command string) map[string]string {
	out := map[string]string{}
	base := splitCommand(command)
	if len(base) == 0 {
		base = []string{"npx"}
	}
	if p, err := exec.LookPath(base[0]); err == nil {
		out[base[0]] = p
	} else {
		out[base[0]] = "unresolved"
	}
	return out
}

// resolvedTools records the versions of the tools that produced the discovery
// snapshot. An executable PATH says which file ran; a version says what it
// was, and the same path on two runners can be two different toolchains.
//
// A tool that cannot be interrogated is recorded as unresolved rather than
// omitted: "we could not tell" is a bound fact, and a missing key is not.
func resolvedTools(root string) map[string]string {
	out := map[string]string{}
	for _, tool := range []struct {
		name string
		argv []string
	}{
		{"node", []string{"node", "--version"}},
		{"npm", []string{"npm", "--version"}},
	} {
		cmd := exec.Command(tool.argv[0], tool.argv[1:]...)
		cmd.Dir = root
		b, err := cmd.Output()
		if err != nil {
			out[tool.name] = "unresolved"
			continue
		}
		out[tool.name] = strings.TrimSpace(string(b))
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
	scorerPath := fs.String("scorer", "", "the frozen scorer the plan allocated with; its digest must match the training lineage Stage 1 bound")
	shardPlan := fs.String("shard-plan", "", "also write the replayed plan here")
	registryPath := fs.String("registry", "", "frozen Aeta component-registry template. Required when the issued receipt binds per-bucket documents: an independent replay that skipped them would leave exactly those documents unre-derived")
	attest := fs.String("attest", "", "write a SIGNED replay attestation here (signing key from TB_WALL_REPLAY_KEY). `wall verify` requires one, signed by a key Stage 1 declared as a replay signer and distinct from the authority key: comparing the planner's account of its own output to itself proves nothing, and neither does having the issuer of the plan re-check it")
	verifierID := fs.String("verifier-id", "", "identity of the party running this replay (required with --attest)")
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
		if err := walltime.VerifySigned(m.Signature, d, authorityKeys); err != nil {
			return fmt.Errorf("stage-1 authority signature: %w", err)
		}
		stage1 = d
		lineage = m.TrainingLineage
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
	if err := issued.Matches(res.Receipt); err != nil {
		return fmt.Errorf("the replayed plan does not match the issued receipt: %w", err)
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
	a.Signature = &walltime.Signature{
		Authority: "ewj2-campaign", KeyID: walltime.PublicKeyOf(key), Digest: d,
		Value: walltime.SignDigest(key, d),
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
	var bundle walltime.PlanningInputBundle
	if err := walltime.ReadJSONFile(bundlePath, &bundle); err != nil {
		return err
	}
	// AUTHORISATION BEFORE PLANNING. The contract puts an owner-authority
	// signature on the inputs before the plan exists, and the planner is where
	// that has to be enforced: a post-run verifier can refuse the row, but it
	// cannot un-run an action or restore an approval that never happened.
	//
	// Both are required on the frozen path. A frozen plan with no Stage-1 is
	// an unauthorised plan wearing the frozen path's determinism, and a
	// signature checked against whatever signed it is not an authority check.
	if stage1Path == "" {
		return fmt.Errorf("--wall-stage1 is required: planning from a frozen bundle with no Stage-1 manifest is planning from inputs nobody authorised")
	}
	if len(o.authorityKeys) == 0 {
		return fmt.Errorf("--wall-authority-key is required with --wall-stage1: verifying a signature against whatever signed the document accepts any self-generated key")
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
