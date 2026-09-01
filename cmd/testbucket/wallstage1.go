package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/invakid404/testbucket/internal/walltime"
)

// authorityKeyEnv is where the campaign authority's signing key is read from.
//
// It is an environment variable and not a flag on purpose: a key on a command
// line is a key in the process table, in the shell history, and in every `ps`
// a co-tenant runs.
const authorityKeyEnv = walltime.AuthorityKeyEnv

// runWallStage1 assembles and signs the Stage-1 input manifest.
//
// It binds only facts that exist BEFORE anything plans: action commits and
// content digests, the reviewed tip, the binary, the consumer identities, the
// source-profile receipt, the training lineage, the instrumentation identity,
// and the planning-input bundle. There is deliberately no plan, atom,
// topology, invocation, script or matrix digest here — those do not exist yet,
// and a manifest that claimed them would be authorising an output nobody had
// derived.
func runWallStage1(args []string) error {
	fs := flag.NewFlagSet("wall stage1", flag.ExitOnError)
	bundlePath := fs.String("bundle", "", "planning-input bundle to authorise (required)")
	out := fs.String("out", "", "write the signed manifest here (required)")
	role := fs.String("role", "", "baseline or candidate (required)")
	actionsDir := fs.String("actions-dir", ".github/actions", "directory holding the plan, run-bucket and record action directories")
	actionCommit := fs.String("action-commit", "", "the full commit SHA the action directories were reviewed at (required)")
	reviewTip := fs.String("review-tip", "", "the reviewed testbucket source tip (required)")
	releaseSHA := fs.String("release-sha", "", "the SHA the release ref resolves to; it must equal the reviewed tip. A local binary cannot deliver a scored row")
	binary := fs.String("binary", "", "path to the exact binary asset being delivered")
	attestation := fs.String("build-attestation", "", "the builder's SIGNED build attestation for that binary (JSON, from `wall attest`). It is verified here against the delivered binary digest and the reviewed tip; a sentence about the build is not evidence about it")
	var builderKeys stringList
	fs.Var(&builderKeys, "builder-key", "a PREDECLARED public key (hex) allowed to sign the build attestation; repeatable and required. An attestation verified against whatever signed it is one anybody can mint")
	sourceProfile := fs.String("source-profile", "", "source-profile receipt JSON (required): the resolved Vitest closure")
	storeReceipt := fs.String("store-receipt", "", "store receipt JSON (required): the admitted store's exact bytes, migration epoch, restore method, stale instant, and the classification of every row — including observed_zero, which is not a gap")
	scorerPath := fs.String("scorer", "", "frozen scorer whose sealed training lineage this manifest binds")
	trainingSet := fs.String("training-set", "", "the EXACT signed training receipt set the scorer was fitted from (required). The manifest refits it and refuses a scorer whose coefficients did not come from it: a receipt-set digest nobody read is a citation, not a lineage")
	var trainingKeys stringList
	fs.Var(&trainingKeys, "training-authority-key", "a PREDECLARED public key (hex) allowed to seal the training receipt set; repeatable and required. It is separate from the campaign authority: the offline surface is sealed once, long before any campaign")
	registryPath := fs.String("registry", "", "frozen Aeta component-registry template")
	runnerImage := fs.String("runner-image", "", "immutable runner image identity, e.g. ubuntu-24.04@sha256:… — never an alias such as ubuntu-latest, which two arms could resolve differently")
	consumerRepo := fs.String("consumer-repository", "", "consumer repository")
	consumerCommit := fs.String("consumer-commit", "", "consumer commit")
	workflowSHA := fs.String("caller-workflow-sha", "", "caller workflow commit SHA")
	downstreamRef := fs.String("downstream-ref", "", "downstream ref the caller resolves")
	authority := fs.String("authority", walltime.CampaignAuthority, "protected environment that authorises these inputs")
	schedulePath := fs.String("campaign-schedule", "", "the authority-frozen campaign schedule JSON (required for a scored arm): the five predeclared pairs, which run is each arm, the randomisation seed and the UTC date each pair runs on. The contract freezes pair order before the first candidate run, and an order chosen afterwards from whatever ran is a selection nobody predeclared")
	signers := fs.String("record-signers", "", "comma-separated PUBLIC halves of the run keys allowed to sign a measurement's roster and closing seal (required for a scored row): the wrapper mints its per-producer keys at run time, so what a manifest can bind is the key that attests to them — and without it the records authenticate only themselves")
	replaySigners := fs.String("replay-signers", "", "comma-separated PUBLIC keys allowed to sign an independent Stage-2 replay attestation. They must not include the authority key: a replay signed by the party that authorised the plan is the planner checking its own work")
	var allowed stringList
	fs.Var(&allowed, "allow-difference", "an enumerated permitted difference between the two arms of a pair; repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Every one of these is an identity the contract requires bound BEFORE
	// either arm plans. Making them optional here would move the check to
	// `Validate`, where a campaign discovers at verification time that the
	// manifest it already signed cannot be used.
	required := map[string]string{
		"--bundle": *bundlePath, "--out": *out, "--role": *role,
		"--action-commit": *actionCommit, "--review-tip": *reviewTip,
		"--store-receipt": *storeReceipt,
		"--release-sha":   *releaseSHA, "--binary": *binary,
		"--build-attestation": *attestation, "--source-profile": *sourceProfile,
		"--scorer": *scorerPath, "--registry": *registryPath,
		"--training-set": *trainingSet,
		"--runner-image": *runnerImage, "--consumer-repository": *consumerRepo,
		"--consumer-commit": *consumerCommit, "--caller-workflow-sha": *workflowSHA,
		"--downstream-ref": *downstreamRef,
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
		return fmt.Errorf("these are required and each binds an identity the contract needs before either arm plans: %s",
			strings.Join(missing, ", "))
	}

	var bundle walltime.PlanningInputBundle
	if err := walltime.ReadJSONFile(*bundlePath, &bundle); err != nil {
		return err
	}
	var profile walltime.SourceProfileReceipt
	if err := walltime.ReadJSONFile(*sourceProfile, &profile); err != nil {
		return err
	}
	var store walltime.StoreReceipt
	if err := walltime.ReadJSONFile(*storeReceipt, &store); err != nil {
		return err
	}

	m := walltime.Stage1Manifest{
		Kind: walltime.Stage1Kind, Role: *role, Bundle: bundle,
		SourceProfile: profile, Store: store, Actions: map[string]walltime.ActionIdentity{},
	}
	// Each action directory is digested as the contract specifies — sorted
	// (relative path, mode, sha256), symlinks rejected — so "the action that
	// ran" and "the action that was reviewed" are the same claim.
	for _, name := range []string{"plan", "run-bucket", "record"} {
		d, err := walltime.DirectoryDigest(filepath.Join(*actionsDir, name))
		if err != nil {
			return fmt.Errorf("digest the %s action: %w", name, err)
		}
		m.Actions[name] = walltime.ActionIdentity{Commit: *actionCommit, ContentDigest: d}
	}
	m.Source.ReviewTip = *reviewTip
	m.Source.ReleaseRefSHA = *releaseSHA
	if len(builderKeys) == 0 {
		return fmt.Errorf("--builder-key is required: without a predeclared builder key the build attestation's own signature would authenticate it, and a build nobody can attribute is not an attested build")
	}
	m.BuilderKeys = builderKeys
	if err := walltime.ReadJSONFile(*attestation, &m.Source.BuildAttestation); err != nil {
		return fmt.Errorf("read the build attestation: %w", err)
	}
	binaryDigest, err := walltime.FileDigest(*binary)
	if err != nil {
		return fmt.Errorf("digest the binary asset: %w", err)
	}
	m.Source.BinaryDigest = binaryDigest
	m.Consumer.Repository = *consumerRepo
	m.Consumer.Commit = *consumerCommit
	m.Consumer.WorkflowSHA = *workflowSHA
	m.Consumer.DownstreamRef = *downstreamRef
	m.Consumer.RunnerImage = *runnerImage
	m.Consumer.Facade = profile.Facade
	m.Consumer.Config = profile.Config
	m.Consumer.Lockfile = profile.Lockfile

	if len(trainingKeys) == 0 {
		return fmt.Errorf("--training-authority-key is required: a training receipt set verified against whatever signed it accepts any self-generated key, and a lineage nobody can attribute is the claim that somebody, somewhere, ran the right procedure")
	}
	var sc walltime.Scorer
	if err := walltime.ReadJSONFile(*scorerPath, &sc); err != nil {
		return err
	}
	var set walltime.TrainingReceiptSet
	if err := walltime.ReadJSONFile(*trainingSet, &set); err != nil {
		return err
	}
	// The lineage is DERIVED from the sealed set, not copied from the scorer's
	// account of itself. Copying it is how a scorer nobody could attribute
	// obtained a signed Stage-1 binding: the manifest repeated whatever the
	// model claimed, and every later check compared that claim to itself.
	setDigest, err := set.DigestOf()
	if err != nil {
		return err
	}
	scorerDigest, err := sc.DigestOf()
	if err != nil {
		return err
	}
	m.TrainingAuthorityKeys = trainingKeys
	m.TrainingLineage = walltime.TrainingLineageID{
		ReceiptSetDigest: setDigest, Cutoff: set.Cutoff, Epoch: set.Epoch,
		ScorerID: sc.ID, ScorerDigest: scorerDigest,
		Algorithm: set.Algorithm, Configuration: set.Configuration,
		Seed: set.Seed, TieBreak: walltime.ScorerTieBreak,
	}
	// Refit HERE, before signing. A manifest that bound a scorer the sealed
	// set does not produce would be authorising a model built from evidence
	// nobody approved, and the campaign would discover it eighty rows later.
	if problems := walltime.VerifyTrainingSurface(set, m.TrainingLineage, sc, trainingKeys); len(problems) > 0 {
		return fmt.Errorf("the frozen scorer is not the one this sealed training set produces:\n  %s", strings.Join(problems, "\n  "))
	}
	var reg walltime.AetaRegistry
	if err := walltime.ReadJSONFile(*registryPath, &reg); err != nil {
		return err
	}
	if err := reg.Validate(); err != nil {
		return err
	}
	registryDigest, err := reg.DigestOf()
	if err != nil {
		return err
	}
	m.Registry = registryDigest

	self, err := os.Executable()
	if err != nil {
		return err
	}
	selfDigest, err := walltime.FileDigest(self)
	if err != nil {
		return err
	}
	// All three producers are this binary; binding them separately is what
	// lets a future split (a standalone collector, say) be a schema change
	// rather than a silent substitution.
	m.Instrumentation = walltime.InstrumentationIdentity{
		Schema:         walltime.SchemaVersion,
		PhysicalBinary: selfDigest, PeerBinary: selfDigest,
		TraceBinary: selfDigest, VerifierBinary: selfDigest,
		ContainmentPolicy:  "dedicated cgroup-v2 subtree per level; membership not modifiable by the workload",
		ChildAdmission:     "clone-into-cgroup before exec; no child starts before its peer's admission receipt",
		EndpointOrder:      "physical <= peer <= trace <= trace <= peer <= physical, on fresh non-copied reads",
		CancellationPolicy: walltime.CancellationPolicyID,
		RawSourceTaxonomy: []string{
			walltime.SourceContainment, walltime.SourceProcessLifecycle,
			walltime.SourceReporter, walltime.SourceWrapper,
		},
		Signers:       splitList(*signers),
		ReplaySigners: splitList(*replaySigners),
	}
	if *schedulePath != "" {
		var schedule walltime.CampaignSchedule
		if err := walltime.ReadJSONFile(*schedulePath, &schedule); err != nil {
			return err
		}
		// Validated HERE, before it is signed. A manifest that carried an
		// unvalidatable order would bind the campaign to a document the
		// verifier will refuse, which is a failure discovered eighty rows too
		// late.
		if err := schedule.Validate(); err != nil {
			return err
		}
		order, err := schedule.OrderDigest()
		if err != nil {
			return err
		}
		m.Schedule = schedule
		fmt.Fprintf(os.Stderr, "testbucket wall: frozen pair order %s over %v\n", order, schedule.SortedDates())
	}
	m.AllowedDifferences = allowed
	if len(allowed) == 0 {
		m.AllowedDifferences = []string{"the enumerated candidate testbucket source/action/binary tuple and its schema-versioned wrappers"}
	}

	if err := m.Validate(); err != nil {
		return err
	}

	key, err := walltime.DecodeKey(strings.TrimSpace(os.Getenv(authorityKeyEnv)))
	if err != nil {
		return fmt.Errorf("%s: %w (only the protected campaign authority may authorise Stage-1 inputs)", authorityKeyEnv, err)
	}
	if err := m.Sign(*authority, key); err != nil {
		return err
	}
	if err := walltime.WriteJSONFile(*out, m); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: signed the Stage-1 manifest as %s\n  manifest digest: %s\n  authority key:   %s\n",
		*role, m.Signature.Digest, m.Signature.KeyID)
	fmt.Println(m.Signature.Digest)
	return nil
}

// splitList parses a comma-separated flag into a trimmed, empty-free list.
func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
