package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/invakid404/testbucket/internal/walltime"
)

// authorityKeyEnv is where the campaign authority's signing key is read from.
//
// It is an environment variable and not a flag on purpose: a key on a command
// line is a key in the process table, in the shell history, and in every `ps`
// a co-tenant runs.
const authorityKeyEnv = "TB_WALL_AUTHORITY_KEY"

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
	releaseSHA := fs.String("release-sha", "", "the SHA the release ref resolves to; it must equal the reviewed tip")
	binary := fs.String("binary", "", "path to the exact binary asset being delivered")
	attestation := fs.String("build-attestation", "", "build-attestation identity for that binary")
	sourceProfile := fs.String("source-profile", "", "source-profile receipt JSON (required): the resolved Vitest closure")
	scorerPath := fs.String("scorer", "", "frozen scorer whose sealed training lineage this manifest binds")
	registryPath := fs.String("registry", "", "frozen Aeta component-registry template")
	runnerImage := fs.String("runner-image", "", "immutable runner image identity (never an alias such as ubuntu-latest)")
	consumerRepo := fs.String("consumer-repository", "", "consumer repository")
	consumerCommit := fs.String("consumer-commit", "", "consumer commit")
	workflowSHA := fs.String("caller-workflow-sha", "", "caller workflow commit SHA")
	downstreamRef := fs.String("downstream-ref", "", "downstream ref the caller resolves")
	authority := fs.String("authority", "ewj2-campaign", "protected environment that authorises these inputs")
	var allowed stringList
	fs.Var(&allowed, "allow-difference", "an enumerated permitted difference between the two arms of a pair; repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for name, v := range map[string]string{
		"--bundle": *bundlePath, "--out": *out, "--role": *role,
		"--action-commit": *actionCommit, "--review-tip": *reviewTip,
		"--source-profile": *sourceProfile,
	} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}

	var bundle walltime.PlanningInputBundle
	if err := walltime.ReadJSONFile(*bundlePath, &bundle); err != nil {
		return err
	}
	var profile walltime.SourceProfileReceipt
	if err := walltime.ReadJSONFile(*sourceProfile, &profile); err != nil {
		return err
	}

	m := walltime.Stage1Manifest{
		Kind: walltime.Stage1Kind, Role: *role, Bundle: bundle,
		SourceProfile: profile, Actions: map[string]walltime.ActionIdentity{},
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
	m.Source.BuildAttestation = *attestation
	if *binary != "" {
		d, err := walltime.FileDigest(*binary)
		if err != nil {
			return fmt.Errorf("digest the binary asset: %w", err)
		}
		m.Source.BinaryDigest = d
	}
	m.Consumer.Repository = *consumerRepo
	m.Consumer.Commit = *consumerCommit
	m.Consumer.WorkflowSHA = *workflowSHA
	m.Consumer.DownstreamRef = *downstreamRef
	m.Consumer.RunnerImage = *runnerImage
	m.Consumer.Facade = profile.Facade
	m.Consumer.Config = profile.Config
	m.Consumer.Lockfile = profile.Lockfile

	if *scorerPath != "" {
		var sc walltime.Scorer
		if err := walltime.ReadJSONFile(*scorerPath, &sc); err != nil {
			return err
		}
		m.TrainingLineage = sc.Lineage
	}
	if *registryPath != "" {
		var reg walltime.AetaRegistry
		if err := walltime.ReadJSONFile(*registryPath, &reg); err != nil {
			return err
		}
		if err := reg.Validate(); err != nil {
			return err
		}
		d, err := reg.DigestOf()
		if err != nil {
			return err
		}
		m.Registry = d
	}

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
		CancellationPolicy: "signal the containment, wait and reap, retain the incomplete receipt",
		RawSourceTaxonomy: []string{
			walltime.SourceContainment, walltime.SourceProcessLifecycle,
			walltime.SourceReporter, walltime.SourceWrapper,
		},
	}
	m.AllowedDifferences = allowed
	if len(m.AllowedDifferences) == 0 {
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
