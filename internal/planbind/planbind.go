// Package planbind is the bridge between the frozen two-stage delivery
// protocol (internal/walltime) and the planner (internal/core).
//
// It does two things and nothing else:
//
//   - ACQUIRE freezes a planning-input bundle: the canonical instant, the raw
//     discovery and runnable-listing bytes, the store bytes, the acquisition
//     closure, and the parser and algorithm identities. This is the only place
//     a live subprocess is allowed to be read.
//   - PLAN replays that bundle through the real planner and emits the Stage-2
//     derived-plan receipt: both plan digests plus the atom, topology,
//     membership, invocation, script and matrix digests.
//
// The separation is the whole design. After ACQUIRE, planning is a pure
// function of recorded bytes: no clock, no discovery, no listing, no
// environment lookup. Replaying the same bundle must therefore produce the
// same digests, and an independent verifier proves it by doing exactly that.
//
// It lives outside internal/walltime so that package stays free of any
// planner or adapter dependency, and outside internal/core so the neutral
// core/adapter seam is untouched.
package planbind

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/runner/vitestrunner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// ParserVersion identifies the parser and policy implementations this build
// binds into a bundle. It changes when their behaviour changes, because a
// bundle replayed by a different parser is not the same plan replayed.
const ParserVersion = "testbucket/v0.3"

// AcquireOptions is everything ACQUIRE needs to freeze a bundle. Every field
// is an input the planner would otherwise read from the live world.
type AcquireOptions struct {
	// Root is the Vitest project directory.
	Root string
	// Runner names the adapter. Only "vitest" is bindable today: the Go
	// adapter's discovery is `go list` over a module set, which needs its own
	// snapshot schema and its own epoch.
	Runner string
	// Instant is the canonical planning clock. It is REQUIRED: store staleness,
	// the cold-start reason and the recorded age all depend on it, so leaving
	// it to time.Now() is exactly the unbound input the bundle exists to close.
	Instant time.Time
	// StaleAfter is the frozen staleness policy.
	StaleAfter time.Duration
	K          int
	Count      int
	Token      string
	// StorePath is the timing store; its exact bytes are frozen into the
	// bundle. A missing store is an explicit absent input, not an error.
	StorePath string
	// Discovery is the raw discovery output and the argv that produced it.
	DiscoveryArgv []string
	Discovery     []byte
	// Runnables maps a file id to its raw listing bytes, for name-sliced
	// targets. An empty map is recorded as an explicit absent input.
	Runnables map[string][]byte
	// Env is the planning-relevant environment, Executables the resolved tool
	// paths, Tools their versions. They are recorded so an independent
	// verifier can say what it would take to reproduce the acquisition.
	Env         map[string]string
	Executables map[string]string
	Tools       map[string]string
	// Repository, Commit and Tree identify the source the inputs came from.
	Repository string
	Commit     string
	Tree       string
	// Renderer and TieBreak name the deterministic renderer and ordering.
	Renderer string
	TieBreak string
	// EventsDir, FileParallelism and WallDir are the render configuration. They
	// decide the generated script bytes, so they are bound inputs rather than
	// replay-time flags.
	EventsDir       string
	FileParallelism int
	WallDir         string
}

// Acquire freezes the bundle. It reads the store from disk — the last live
// read in the whole protocol — and records everything else the caller has
// already captured.
func Acquire(opt AcquireOptions) (*walltime.PlanningInputBundle, error) {
	if opt.Runner != "vitest" {
		return nil, fmt.Errorf("planbind: only the vitest adapter is bindable today (got %q); the Go adapter needs its own snapshot schema and epoch", opt.Runner)
	}
	if opt.Instant.IsZero() {
		return nil, fmt.Errorf("planbind: a canonical planning instant is required; an ambient clock is an unbound input")
	}
	if len(opt.Discovery) == 0 {
		return nil, fmt.Errorf("planbind: the discovery snapshot is empty; an absent discovery must be recorded as such, not left blank")
	}

	var b walltime.PlanningInputBundle
	b.Kind = walltime.BundleKind
	b.Clock = walltime.ClockPolicy{
		Policy:           "frozen_canonical_instant",
		Instant:          opt.Instant.UTC().Format(time.RFC3339Nano),
		Precision:        "1ns",
		TimeZone:         "UTC",
		PermittedSources: []string{"stage1_planning_input_bundle"},
		StaleThreshold:   opt.StaleAfter.String(),
	}
	b.Discovery = []walltime.RawSnapshot{
		walltime.NewRawSnapshot("vitest-discovery", opt.DiscoveryArgv, opt.Root, opt.Discovery),
	}
	for _, id := range sortedKeys(opt.Runnables) {
		raw := opt.Runnables[id]
		b.Runnables = append(b.Runnables, walltime.RunnableSnapshot{
			TargetID: id,
			Argv:     []string{"vitest", "list", id, "--json"},
			Empty:    len(raw) == 0,
			Bytes:    raw,
			Digest:   walltime.DigestBytes(raw),
		})
	}
	if len(b.Runnables) == 0 {
		b.AbsentInputs = append(b.AbsentInputs, "runnable_listings: no name-sliced target in this plan")
	}

	storeBytes, err := os.ReadFile(opt.StorePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("planbind: read store %s: %w", opt.StorePath, err)
		}
		// A cold start is normal and must be BOUND as a cold start, so a
		// replay cold-starts too instead of finding a store that appeared
		// later.
		storeBytes = nil
		b.AbsentInputs = append(b.AbsentInputs, "store: cold start, no store at "+opt.StorePath)
	}
	b.Store = walltime.NewRawSnapshot(opt.StorePath, nil, opt.Root, storeBytes)

	b.Source.Repository, b.Source.Commit, b.Source.Tree = opt.Repository, opt.Commit, opt.Tree
	b.Acquisition.Argv = append([]string{"testbucket", "wall", "bundle"}, opt.DiscoveryArgv...)
	b.Acquisition.Cwd = opt.Root
	b.Acquisition.Env = copyMap(opt.Env)
	b.Acquisition.Executables = copyMap(opt.Executables)
	b.Acquisition.Tools = copyMap(opt.Tools)
	b.Parsers = []walltime.ParserIdentity{
		{Name: "vitest-discovery-parser", Version: ParserVersion, Digest: walltime.DigestBytes([]byte("vitest-discovery-parser/" + ParserVersion))},
		{Name: "vitest-runnable-parser", Version: ParserVersion, Digest: walltime.DigestBytes([]byte("vitest-runnable-parser/" + ParserVersion))},
		{Name: "suffix-collision-atomiser", Version: ParserVersion, Digest: walltime.DigestBytes([]byte("suffix-collision-atomiser/" + ParserVersion))},
		{Name: "store-schema", Version: ParserVersion, Digest: walltime.DigestBytes([]byte("store-schema/" + ParserVersion))},
		{Name: "staleness-policy", Version: ParserVersion, Digest: walltime.DigestBytes([]byte("staleness-policy/" + ParserVersion))},
		{Name: "kk-partitioner", Version: ParserVersion, Digest: walltime.DigestBytes([]byte("kk-partitioner/" + ParserVersion))},
		{Name: "vitest-renderer", Version: ParserVersion, Digest: walltime.DigestBytes([]byte("vitest-renderer/" + ParserVersion))},
	}
	b.Algorithms.FullPlan = walltime.AlgorithmIdentity{
		Name: walltime.FullPlanDigestAlgorithm, Canonicalizer: walltime.CanonAlgorithm, Implementation: ParserVersion,
	}
	b.Algorithms.SemanticPlan = walltime.AlgorithmIdentity{
		Name: walltime.SemanticPlanDigestAlgorithm, Canonicalizer: walltime.CanonAlgorithm, Implementation: ParserVersion,
	}
	b.Selection.K = opt.K
	b.Selection.Count = opt.Count
	b.Selection.Token = opt.Token
	b.Selection.Runner = opt.Runner
	b.Selection.Renderer = firstNonEmpty(opt.Renderer, "vitest/"+ParserVersion)
	b.Selection.TieBreak = firstNonEmpty(opt.TieBreak, "unit_id_ascending")
	b.Render.EventsDir = opt.EventsDir
	b.Render.FileParallelism = opt.FileParallelism
	b.Render.WallDir = opt.WallDir

	if err := b.Validate(); err != nil {
		return nil, err
	}
	return &b, nil
}

// Result is one replay: the plan document and the receipt describing it.
type Result struct {
	Doc     *core.PlanDocument
	Receipt walltime.Stage2Receipt
	// Bundle is the bundle the plan was derived from, echoed so a caller does
	// not have to hold it alongside.
	Bundle *walltime.PlanningInputBundle
	// Allocator holds the frozen Palloc values the partition used, when one
	// was supplied. The audit projection is taken over exactly these numbers
	// rather than recomputed, so a scorer that moved afterwards cannot make a
	// prediction look better than it was.
	Allocator *Allocator
}

// PlanOptions configures the replay.
type PlanOptions struct {
	Bundle *walltime.PlanningInputBundle
	// Stage1 is the parent manifest digest the receipt records.
	Stage1 walltime.Digest
	// Scorer, when set, makes the frozen pre-plan score the ALLOCATION input:
	// KK packs by Palloc while every reported est_seconds keeps summing the
	// store's measured weights. Without it the partition uses the store weights
	// as it always has — which is a perfectly good split and is NOT campaign
	// eligible, because a reporter-derived weight is an outcome.
	Scorer *walltime.Scorer
}

// Plan replays the frozen bundle through the real planner and returns the
// derived plan with its Stage-2 receipt.
//
// Nothing here reads the clock, the filesystem or a subprocess: every input
// comes from the bundle. That is what makes the receipt reproducible, and what
// lets an independent verifier reject a plan that was not derived from the
// inputs it claims.
func Plan(ctx context.Context, opt PlanOptions) (*Result, error) {
	b := opt.Bundle
	if b == nil {
		return nil, fmt.Errorf("planbind: no planning-input bundle")
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	instant, err := b.Clock.Time()
	if err != nil {
		return nil, err
	}
	stale, err := time.ParseDuration(b.Clock.StaleThreshold)
	if err != nil {
		return nil, fmt.Errorf("planbind: stale threshold %q: %w", b.Clock.StaleThreshold, err)
	}

	frozen := &vitestrunner.FrozenInputs{
		Discovery: b.Discovery[0].Bytes,
		Runnables: map[string][]byte{},
	}
	for _, r := range b.Runnables {
		frozen.Runnables[r.TargetID] = r.Bytes
	}
	rnr, err := vitestrunner.New(vitestrunner.Options{
		Root:            b.Acquisition.Cwd,
		Frozen:          frozen,
		EventsDir:       b.Render.EventsDir,
		FileParallelism: b.Render.FileParallelism,
		WallDir:         b.Render.WallDir,
	})
	if err != nil {
		return nil, err
	}
	live, err := rnr.Discover(ctx)
	if err != nil {
		return nil, err
	}
	st, reason, err := core.ParseStore(b.Store.Bytes, b.Store.Name)
	if err != nil {
		return nil, err
	}

	planOpt := core.PlanOptions{
		K:          b.Selection.K,
		StorePath:  b.Store.Name,
		Count:      b.Selection.Count,
		StaleAfter: stale,
		Now:        instant,
		Live:       live,
		Token:      b.Selection.Token,
	}
	var allocator *Allocator
	if opt.Scorer != nil {
		allocator = NewAllocator(*opt.Scorer, NewFeatureBuilder(b, live, opt.Stage1))
		planOpt.AllocationScore = allocator.Score
	}

	doc, err := core.BuildPlan(ctx, rnr, st, reason, planOpt)
	if err != nil {
		return nil, err
	}

	receipt, err := deriveReceipt(opt.Stage1, b, doc, live)
	if err != nil {
		return nil, err
	}
	if opt.Scorer != nil {
		receipt.PlannerResult += fmt.Sprintf(" allocation=palloc scorer=%s", opt.Scorer.ID)
	}
	return &Result{Doc: doc, Receipt: *receipt, Bundle: b, Allocator: allocator}, nil
}

// deriveReceipt computes every digest the Stage-2 receipt carries, plus the
// input-access record of what the planner actually consumed.
func deriveReceipt(stage1 walltime.Digest, b *walltime.PlanningInputBundle, doc *core.PlanDocument, live []runner.LivePackage) (*walltime.Stage2Receipt, error) {
	bundleDigest, err := b.DigestOf()
	if err != nil {
		return nil, err
	}
	full, err := walltime.DigestJSON(doc)
	if err != nil {
		return nil, err
	}
	semantic, err := walltime.DigestJSON(SemanticProjection(doc))
	if err != nil {
		return nil, err
	}
	atoms, err := walltime.DigestJSON(atomProjection(live))
	if err != nil {
		return nil, err
	}
	topology, err := walltime.DigestJSON(topologyProjection(doc))
	if err != nil {
		return nil, err
	}
	membership, err := walltime.DigestJSON(membershipProjection(doc))
	if err != nil {
		return nil, err
	}
	invocations, err := walltime.DigestJSON(invocationProjection(doc))
	if err != nil {
		return nil, err
	}
	matrix, err := doc.MatrixJSON()
	if err != nil {
		return nil, err
	}

	r := &walltime.Stage2Receipt{
		Kind:             walltime.Stage2Kind,
		Stage1Digest:     stage1,
		BundleDigest:     bundleDigest,
		InputAccess:      inputAccess(b),
		PlanDigest:       full,
		SemanticDigest:   semantic,
		AtomDigest:       atoms,
		TopologyDigest:   topology,
		MembershipDigest: membership,
		InvocationDigest: invocations,
		ScriptDigest:     walltime.DigestBytes([]byte(scriptBytes(doc))),
		MatrixDigest:     walltime.DigestBytes(matrix),
		PlannerResult:    fmt.Sprintf("k=%d buckets=%d units=%d", doc.K, len(doc.Buckets), doc.Summary.ScheduledUnits),
		RendererResult:   fmt.Sprintf("invocations=%d", countInvocations(doc)),
	}
	r.Algorithms.FullPlan = b.Algorithms.FullPlan
	r.Algorithms.SemanticPlan = b.Algorithms.SemanticPlan
	return r, r.Validate()
}

// inputAccess records which bundle fields the planner consumed, by digest.
func inputAccess(b *walltime.PlanningInputBundle) []walltime.InputAccess {
	out := []walltime.InputAccess{
		{Field: "clock.instant", Digest: walltime.DigestBytes([]byte(b.Clock.Instant))},
		{Field: "store", Digest: b.Store.Digest},
	}
	for _, d := range b.Discovery {
		out = append(out, walltime.InputAccess{Field: "discovery/" + d.Name, Digest: d.Digest})
	}
	for _, r := range b.Runnables {
		out = append(out, walltime.InputAccess{Field: "runnables/" + r.TargetID, Digest: r.Digest})
	}
	for _, p := range b.Parsers {
		out = append(out, walltime.InputAccess{Field: "parser/" + p.Name, Digest: p.Digest})
	}
	return out
}

// SemanticPlan is the projection the semantic digest is taken over: what
// actually runs, and nothing about how it was summarised. Two plans that
// differ only in a human counter share it; two plans that would run one
// different test never do.
type SemanticPlan struct {
	K       int              `json:"k"`
	Buckets []SemanticBucket `json:"buckets"`
}

// SemanticBucket is one lane's schedulable content.
type SemanticBucket struct {
	Index       int                 `json:"bucket"`
	NeedsNode   bool                `json:"needs_node"`
	Units       []SemanticUnit      `json:"units"`
	Invocations []runner.Invocation `json:"invocations"`
	Script      string              `json:"script"`
}

// SemanticUnit is one scheduled unit's identity, without its estimate: a
// changed weight is a different forecast, not different work.
type SemanticUnit struct {
	ID       string      `json:"id"`
	Kind     runner.Kind `json:"kind"`
	Packages []string    `json:"packages"`
	Run      []string    `json:"run,omitempty"`
}

// SemanticProjection extracts the semantic plan from a plan document.
func SemanticProjection(doc *core.PlanDocument) SemanticPlan {
	out := SemanticPlan{K: doc.K}
	for _, b := range doc.Buckets {
		sb := SemanticBucket{Index: b.Index, NeedsNode: b.NeedsNode, Invocations: b.Invocations, Script: b.Script}
		for _, u := range b.Units {
			sb.Units = append(sb.Units, SemanticUnit{ID: u.ID, Kind: u.Kind, Packages: u.Packages, Run: u.Run})
		}
		out.Buckets = append(out.Buckets, sb)
	}
	return out
}

// atomProjection is the suffix-collision atom closure: which targets must ride
// together. It is digested separately because an atom split is terminal, and a
// terminal condition deserves its own identity.
func atomProjection(live []runner.LivePackage) map[string][]string {
	out := map[string][]string{}
	for _, p := range live {
		key := p.AtomKey()
		if key == "" {
			continue
		}
		out[key] = append(out[key], p.ID)
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// topologyProjection is the shape of the schedule: which unit kinds landed in
// which bucket, in order.
func topologyProjection(doc *core.PlanDocument) map[string][]string {
	out := map[string][]string{}
	for _, b := range doc.Buckets {
		key := fmt.Sprintf("bucket-%d", b.Index)
		for _, u := range b.Units {
			out[key] = append(out[key], string(u.Kind))
		}
	}
	return out
}

// membershipProjection is which targets each rendered invocation covers — the
// immutable membership Pcheck projects over.
func membershipProjection(doc *core.PlanDocument) map[string][]string {
	out := map[string][]string{}
	for _, b := range doc.Buckets {
		for i, inv := range b.Invocations {
			out[fmt.Sprintf("bucket-%d/inv-%d", b.Index, i)] = strings.Fields(inv.Desc)
		}
	}
	return out
}

func invocationProjection(doc *core.PlanDocument) [][]runner.Invocation {
	out := make([][]runner.Invocation, 0, len(doc.Buckets))
	for _, b := range doc.Buckets {
		out = append(out, b.Invocations)
	}
	return out
}

func scriptBytes(doc *core.PlanDocument) string {
	var sb strings.Builder
	for _, b := range doc.Buckets {
		fmt.Fprintf(&sb, "# bucket %d\n%s\n", b.Index, b.Script)
	}
	return sb.String()
}

func countInvocations(doc *core.PlanDocument) int {
	n := 0
	for _, b := range doc.Buckets {
		n += len(b.Invocations)
	}
	return n
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func copyMap(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
