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
	// StorePath names the timing store, and StoreBytes are its EXACT bytes as
	// the caller read them. The bytes are passed in rather than re-read here:
	// the caller has already used them to decide which targets need a runnable
	// listing, and a second read could freeze a different store than the one
	// that decision was made from.
	StorePath string
	// StoreBytes is nil when the store is absent, which is a bound cold start.
	StoreBytes []byte
	// StoreAbsent distinguishes "no store" from "an empty store": both are
	// legitimate, and only one of them is a cold start.
	StoreAbsent bool
	// Discovery is the raw discovery output and the argv that produced it.
	DiscoveryArgv []string
	Discovery     []byte
	// Runnables maps a file id to its raw listing bytes, for name-sliced
	// targets. An empty map is recorded as an explicit absent input.
	Runnables map[string][]byte
	// RunnableArgv is the EXACT command that produced each of those listings.
	// It is supplied by the caller that ran them rather than reconstructed
	// here: this package cannot know the configured Vitest command, and a
	// provenance record naming a command nobody executed would send a replay
	// after the wrong bytes with nothing to explain the difference.
	RunnableArgv map[string][]string
	// Env is the planning-relevant environment. It is recorded so an
	// independent verifier can say what it would take to reproduce the
	// acquisition.
	Env map[string]string
	// BundleArgv is the ACTUAL command that produced this bundle — the real
	// `wall bundle` invocation, with its real flags, as it was run.
	//
	// It is supplied by the caller that ran it rather than reconstructed here.
	// The reconstruction used to be `{"testbucket","wall","bundle"}` with the
	// separate Vitest DISCOVERY argv concatenated onto it, which is neither
	// command: it omits every flag `wall bundle` was actually given and
	// presents the discovery program's arguments as stray arguments to a
	// different program. Running it would do something nobody did. A signed
	// provenance record naming a command that was never run is worse than
	// none, because it reads as evidence.
	BundleArgv []string
	// Resolve returns the resolved-executable and tool closure for ONE exact
	// argv, and is called once per snapshot with the argv that snapshot was
	// actually taken by.
	//
	// It is a function rather than two maps because a single closure supplied
	// for the whole bundle describes one command, and the bundle can hold
	// several: an overridden discovery command used to be frozen beside the
	// resolution of the ordinary Vitest command — a provenance record naming a
	// binary that had not run. Making the closure a function OF the argv makes
	// that mismatch unrepresentable rather than merely discouraged.
	//
	// It must fail rather than return a placeholder: a closure that could not
	// be resolved is an unbound input, and planning on one is exactly what the
	// bundle exists to prevent.
	Resolve func(argv []string) (map[string]string, map[string]walltime.ToolIdentity, error)
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

	var err error
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
	if opt.Resolve == nil {
		return nil, fmt.Errorf("planbind: no executable/tool resolver was supplied; a snapshot whose resolved closure is invented cannot be replayed")
	}
	discovery := walltime.NewRawSnapshot("vitest-discovery", opt.DiscoveryArgv, opt.Root, opt.Discovery)
	discovery.Env = copyMap(opt.Env)
	// The closure for the argv that ACTUALLY took this listing.
	if discovery.Executables, discovery.Tools, err = resolveFor(opt, opt.DiscoveryArgv, "the discovery snapshot"); err != nil {
		return nil, err
	}
	b.Discovery = []walltime.RawSnapshot{discovery}
	for _, id := range sortedKeys(opt.Runnables) {
		raw := opt.Runnables[id]
		// The names are parsed HERE, through the bound parser, and frozen
		// alongside the bytes. Freezing only the bytes would leave the runtime
		// feature vector to re-derive them, and the one thing worse than a
		// missing feature is a feature that reports zero while the evidence in
		// the same bundle says otherwise.
		names, err := vitestrunner.ParseRunnableNames(opt.Root, id, raw)
		if err != nil {
			return nil, fmt.Errorf("planbind: freeze the runnable listing for %s: %w", id, err)
		}
		if len(raw) > 0 && len(names) == 0 {
			return nil, fmt.Errorf("planbind: the runnable listing for %s parsed to no names; a target flagged for slicing with no runnable universe cannot be sliced", id)
		}
		argv := opt.RunnableArgv[id]
		if len(argv) == 0 {
			return nil, fmt.Errorf("planbind: the runnable listing for %s records no acquisition argv; a frozen input whose provenance is invented cannot be replayed", id)
		}
		execs, tools, err := resolveFor(opt, argv, "the runnable listing for "+id)
		if err != nil {
			return nil, err
		}
		b.Runnables = append(b.Runnables, walltime.RunnableSnapshot{
			TargetID:    id,
			Argv:        append([]string(nil), argv...),
			Cwd:         opt.Root,
			Env:         copyMap(opt.Env),
			Executables: execs,
			Tools:       tools,
			Names:       names,
			Empty:       len(raw) == 0,
			Bytes:       raw,
			Digest:      walltime.DigestBytes(raw),
		})
	}
	if len(b.Runnables) == 0 {
		b.AbsentInputs = append(b.AbsentInputs, "runnable_listings: no name-sliced target in this plan")
	}

	// A cold start is normal and must be BOUND as a cold start, so a replay
	// cold-starts too instead of finding a store that appeared later. The
	// bound fact is StoreAbsent; the AbsentInputs line is the human-readable
	// half of the same statement, and validation requires both to agree with
	// the bytes.
	//
	// Absence is taken from the BYTES, not from the caller's flag. A caller
	// that passed StoreAbsent=true alongside a warm store used to produce a
	// bundle that validated and then planned from the weights it had just
	// declared missing; deriving it here means the two can no longer disagree.
	b.StoreAbsent = len(opt.StoreBytes) == 0
	if b.StoreAbsent {
		b.AbsentInputs = append(b.AbsentInputs, "store: cold start, no store at "+opt.StorePath)
	}
	if opt.StoreAbsent && !b.StoreAbsent {
		return nil, fmt.Errorf("planbind: the caller declared the store at %s absent but supplied %d byte(s) of it; a cold start that plans from weights is not a cold start", opt.StorePath, len(opt.StoreBytes))
	}
	b.Store = walltime.NewRawSnapshot(opt.StorePath, nil, opt.Root, opt.StoreBytes)

	b.Source.Repository, b.Source.Commit, b.Source.Tree = opt.Repository, opt.Commit, opt.Tree
	if len(opt.BundleArgv) == 0 {
		return nil, fmt.Errorf("planbind: no bundle argv was supplied; the acquisition closure would name a command nobody ran")
	}
	b.Acquisition.Argv = append([]string(nil), opt.BundleArgv...)
	b.Acquisition.Cwd = opt.Root
	b.Acquisition.Env = copyMap(opt.Env)
	if b.Acquisition.Executables, b.Acquisition.Tools, err = resolveFor(opt, b.Acquisition.Argv, "the bundle acquisition closure"); err != nil {
		return nil, err
	}
	// The identities of the implementations that will RUN, not labels for
	// them. Every stage the contract names is bound, and each digest is the
	// binary this build is — so a reader who can reproduce the binary can
	// reproduce every identity here.
	b.Parsers = walltime.ImplementedParserIdentities()
	b.Algorithms.FullPlan = walltime.ImplementedFullPlanAlgorithm()
	b.Algorithms.SemanticPlan = walltime.ImplementedSemanticPlanAlgorithm()
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
	// Derived is the atom/topology/membership projections whose digests the
	// receipt binds — the document the campaign's ablation gate reads.
	Derived walltime.AblationDerived
}

// PlanOptions configures the replay.
type PlanOptions struct {
	// PlannerClaim is the one-shot execution claim this derivation is being
	// performed under.
	//
	// It is an INPUT rather than something stamped on afterwards: the receipt
	// is validated as it is built, and a receipt whose claim arrives later is
	// a receipt that was complete without one. The caller takes the claim
	// before calling here — that is the whole point of a pre-plan claim — so
	// passing it in costs nothing and makes the derivation and its claim one
	// object.
	PlannerClaim *walltime.PlannerClaimReceipt
	// Stage1Approval is the authority approval the planner SAW, for the same
	// reason the claim is an input: the receipt is validated as it is built,
	// and an approval stamped on afterwards is an approval the validated
	// document did not have.
	Stage1Approval walltime.Stage1Approval
	Bundle         *walltime.PlanningInputBundle
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
	// A STALE store is not warm evidence. The everyday planner warns and
	// carries on — that is v0.2.2 behaviour and consumers depend on it — but
	// the frozen path is where a warm claim is made, so here it is refused.
	// The alternative is a scored row whose weights came from a store the
	// contract says cannot support one.
	if st != nil && st.UpdatedAt != "" {
		recorded, perr := time.Parse(time.RFC3339, st.UpdatedAt)
		if perr == nil && instant.Sub(recorded) > stale {
			return nil, fmt.Errorf(
				"planbind: the frozen store was recorded at %s, %s before the canonical planning instant and beyond the %s stale policy; a stale store is not warm evidence",
				st.UpdatedAt, instant.Sub(recorded).Round(time.Hour), stale)
		}
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

	receipt, err := deriveReceipt(opt.Stage1, opt.PlannerClaim, opt.Stage1Approval, b, doc, live)
	if err != nil {
		return nil, err
	}
	if opt.Scorer != nil {
		receipt.PlannerResult += fmt.Sprintf(" allocation=palloc scorer=%s", opt.Scorer.ID)
	}
	return &Result{Doc: doc, Receipt: *receipt, Bundle: b, Allocator: allocator,
		Derived: DerivedProjections(doc, live)}, nil
}

// deriveReceipt computes every digest the Stage-2 receipt carries, plus the
// input-access record of what the planner actually consumed.
func deriveReceipt(stage1 walltime.Digest, claim *walltime.PlannerClaimReceipt, approval walltime.Stage1Approval, b *walltime.PlanningInputBundle, doc *core.PlanDocument, live []runner.LivePackage) (*walltime.Stage2Receipt, error) {
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
	derived := DerivedProjections(doc, live)
	atoms, err := walltime.DigestJSON(derived.Atoms)
	if err != nil {
		return nil, err
	}
	topology, err := walltime.DigestJSON(derived.Topology)
	if err != nil {
		return nil, err
	}
	membership, err := walltime.DigestJSON(derived.Membership)
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
		PlannerClaim:     claim,
		Stage1Approval:   approval,
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
	// The identities of the implementations that JUST RAN, taken from this
	// build rather than copied out of the bundle. Echoing the bundle made the
	// receipt repeat a claim; the bundle is now checked against these same
	// values before the planner executes, so recording them directly is what
	// makes Stage 2 a statement about what happened.
	r.Algorithms.FullPlan = walltime.ImplementedFullPlanAlgorithm()
	r.Algorithms.SemanticPlan = walltime.ImplementedSemanticPlanAlgorithm()
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

// DerivedProjections is the plan's own atom, topology and membership
// projections, as ONE document.
//
// The campaign gate reads exactly these three maps and asks what the schedule
// realized; the Stage-2 receipt binds their digests. They are built here, by
// the same call that computes those digests, so the document a run publishes
// and the digests its receipt carries cannot be derived from different plans.
// Nothing in production produced this document before: the gate could only
// ever be handed one somebody wrote by hand.
func DerivedProjections(doc *core.PlanDocument, live []runner.LivePackage) walltime.AblationDerived {
	return walltime.AblationDerived{
		Atoms:      atomProjection(live),
		Topology:   topologyProjection(doc),
		Membership: membershipProjection(doc),
	}
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
// which bucket, in order — AND WHICH FILES each of those units covers.
//
// The kind alone could not establish the topology the contract's strata are
// about. "Two whole-file units" says nothing about how many files were
// scheduled, so a plan running one file twice and a plan running two files
// projected identically, and a multi-file condition could not be proved from
// the document that is supposed to prove it. The files a unit covers are the
// unit's own Packages, spelled out rather than parsed back out of an id whose
// slice form embeds arbitrary runnable names.
func topologyProjection(doc *core.PlanDocument) map[string][]string {
	out := map[string][]string{}
	for _, b := range doc.Buckets {
		key := fmt.Sprintf("bucket-%d", b.Index)
		for _, u := range b.Units {
			files := append([]string(nil), u.Packages...)
			sort.Strings(files)
			out[key] = append(out[key], walltime.TopologyEntry(string(u.Kind), files))
		}
	}
	return out
}

// membershipProjection is which UNITS each rendered invocation covers — the
// immutable membership Pcheck projects over.
//
// It reads Invocation.Units, not the description. Two legal name slices of one
// file have the same description and different units, so a membership digest
// taken over descriptions cannot tell them apart — which is exactly the atom
// and slice identity the contract makes terminal.
func membershipProjection(doc *core.PlanDocument) map[string][]string {
	out := map[string][]string{}
	for _, b := range doc.Buckets {
		for i, inv := range b.Invocations {
			key := fmt.Sprintf("bucket-%d/inv-%d", b.Index, i)
			out[key] = append([]string(nil), inv.Units...)
			sort.Strings(out[key])
		}
	}
	return out
}

// InvocationManifestFor is the per-bucket document the verifier compares each
// physical invocation record against.
//
// Without it the wrapper's Spec is an assertion travelling beside the plan
// rather than a claim checked against it: the verifier could confirm that a
// record names SOME argv and selector, but not that they are the ones the
// authorised plan rendered.
func InvocationManifestFor(doc *core.PlanDocument, bucket int, stage2 walltime.Digest) (*walltime.InvocationManifest, error) {
	for _, b := range doc.Buckets {
		if b.Index != bucket {
			continue
		}
		m := &walltime.InvocationManifest{
			Kind: walltime.InvocationManifestKind, Stage2: stage2,
			BucketIndex: b.Index, BucketName: b.Name,
		}
		for i, inv := range b.Invocations {
			units := append([]string(nil), inv.Units...)
			sort.Strings(units)
			atoms := append([]string(nil), inv.Atoms...)
			sort.Strings(atoms)
			m.Invocations = append(m.Invocations, walltime.InvocationIdentity{
				Seq:            i,
				ArgvDigest:     walltime.DigestJSONOrEmpty(inv.Args),
				Cwd:            inv.Dir,
				SelectorDigest: walltime.DigestJSONOrEmpty(inv.Selector),
				UnitDigest:     walltime.DigestJSONOrEmpty(units),
				AtomDigest:     walltime.DigestJSONOrEmpty(atoms),
				Units:          units,
				Atoms:          atoms,
			})
		}
		return m, nil
	}
	return nil, fmt.Errorf("planbind: the plan has no bucket %d", bucket)
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

// resolveFor asks the caller's resolver for one argv's closure and refuses
// anything short of a resolved answer. `unresolved` is retained by the
// resolver as a bound fact about a failure; it is not a value a plan may be
// derived from, so it is rejected HERE rather than at verification time, where
// the bundle has already been signed.
func resolveFor(opt AcquireOptions, argv []string, what string) (map[string]string, map[string]walltime.ToolIdentity, error) {
	if len(argv) == 0 {
		return nil, nil, fmt.Errorf("planbind: %s has no argv to resolve", what)
	}
	execs, tools, err := opt.Resolve(append([]string(nil), argv...))
	if err != nil {
		return nil, nil, fmt.Errorf("planbind: resolve %s: %w", what, err)
	}
	if err := walltime.ValidateAcquisitionClosure(what, argv, opt.Root, copyMap(opt.Env), execs, tools); err != nil {
		return nil, nil, fmt.Errorf("planbind: %w", err)
	}
	return execs, tools, nil
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

// The canonical runnable parser is registered here, at the one place that
// links both the parser and the bundle schema.
//
// Bundle validation re-derives every snapshot's names from its own frozen
// bytes and requires exact equality, which it cannot do by importing the
// parser directly: the parser lives in the runner package and that package
// imports the schema. Registering it from this package's init means every
// production path — the planner, the verifier, replay — has it, because they
// all go through here.
func init() {
	walltime.RegisterRunnableNameParser(vitestrunner.ParseRunnableNames)
}
