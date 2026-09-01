package walltime

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// The two-stage delivery protocol. Stage 1 binds INPUTS before anything plans;
// Stage 2 records what the single authorised planner DERIVED from them. The
// split exists because a receipt that binds outputs and inputs together can
// always be read as authorising whatever it happened to consume.
const (
	Stage1Kind = "tb.walltime.stage1/v1"
	Stage2Kind = "tb.walltime.stage2/v1"
	// BundleKind is the versioned planning-input bundle inside Stage 1.
	BundleKind = "tb.walltime.planning-input-bundle/v1"
)

// AlgorithmIdentity names a versioned algorithm and the implementation that
// ran it. A digest whose algorithm identity is unknown to the verifier is not
// a digest it may compare.
type AlgorithmIdentity struct {
	Name           string `json:"name"`
	Canonicalizer  string `json:"canonicalizer"`
	Implementation string `json:"implementation"`
}

// RawSnapshot is one byte-exact frozen input. Bytes are carried INLINE (base64
// through encoding/json) rather than by path: a path is a promise about a file
// that may since have changed, and the whole point of the bundle is that it
// cannot.
type RawSnapshot struct {
	Name string `json:"name"`
	// Argv, Cwd and Env record how the snapshot was acquired, so an
	// independent verifier can say what it would have taken to reproduce it.
	Argv []string          `json:"argv,omitempty"`
	Cwd  string            `json:"cwd,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
	// Empty is explicit: "this input is absent" is a bound fact, not a
	// missing field that a later reader may fill in.
	Empty  bool   `json:"empty"`
	Bytes  []byte `json:"bytes"`
	Digest Digest `json:"digest"`
}

// NewRawSnapshot freezes bytes with their acquisition context.
func NewRawSnapshot(name string, argv []string, cwd string, b []byte) RawSnapshot {
	return RawSnapshot{Name: name, Argv: argv, Cwd: cwd, Empty: len(b) == 0, Bytes: b, Digest: DigestBytes(b)}
}

// RunnableSnapshot is one target's frozen runnable listing, INCLUDING the raw
// order. Order is part of the input because a planner that slices by name can
// produce a different plan from the same names in a different sequence.
type RunnableSnapshot struct {
	TargetID string `json:"target_id"`
	// Argv, Cwd and Env are this listing's own acquisition closure. A listing
	// taken from a different directory or under a different environment is a
	// different observation, and the bundle's promise is that a replay could
	// say what taking it again would require.
	Argv   []string          `json:"argv"`
	Cwd    string            `json:"cwd"`
	Env    map[string]string `json:"env"`
	Names  []string          `json:"names"`
	Empty  bool              `json:"empty"`
	Bytes  []byte            `json:"bytes,omitempty"`
	Digest Digest            `json:"digest"`
}

// ClockPolicy freezes the canonical planning instant. Ambient time.Now() is
// prohibited: store staleness, cold-start reasons and the recorded age all
// depend on it, so an unbound clock can change a plan without changing an
// input.
type ClockPolicy struct {
	Policy           string   `json:"policy"`
	Instant          string   `json:"instant"`
	Precision        string   `json:"precision"`
	TimeZone         string   `json:"time_zone"`
	PermittedSources []string `json:"permitted_sources"`
	StaleThreshold   string   `json:"stale_threshold"`
}

// Instant parses the frozen canonical planning instant.
func (c ClockPolicy) Time() (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, c.Instant)
	if err != nil {
		return time.Time{}, fmt.Errorf("planning instant %q: %w", c.Instant, err)
	}
	return t, nil
}

// ParserIdentity binds one parser or policy by name, version and bytes.
type ParserIdentity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  Digest `json:"digest"`
}

// PlanningInputBundle is every input the one authorised plan execution may
// read. Anything not in here is an UNBOUND input and is prohibited.
type PlanningInputBundle struct {
	Kind  string      `json:"kind"`
	Clock ClockPolicy `json:"clock"`
	// Discovery and Runnables are the raw listings, byte for byte.
	Discovery []RawSnapshot      `json:"discovery"`
	Runnables []RunnableSnapshot `json:"runnables"`
	// Store is the exact admitted timing store, bytes and all.
	Store RawSnapshot `json:"store"`
	// StoreAbsent is the cold start as a BOUND FACT rather than a sentence in
	// AbsentInputs. Validation requires it to agree with the bytes, so a
	// bundle cannot declare a cold start and then plan warm.
	StoreAbsent bool `json:"store_absent"`
	// Source identifies the tree the inputs were taken from.
	Source struct {
		Repository string `json:"repository"`
		Commit     string `json:"commit"`
		Tree       string `json:"tree"`
	} `json:"source"`
	// Acquisition is the closure that produced the snapshots.
	Acquisition struct {
		Argv        []string          `json:"argv"`
		Cwd         string            `json:"cwd"`
		Env         map[string]string `json:"env"`
		Executables map[string]string `json:"executables"`
		Tools       map[string]string `json:"tools"`
	} `json:"acquisition"`
	Parsers    []ParserIdentity `json:"parsers"`
	Algorithms struct {
		FullPlan     AlgorithmIdentity `json:"full_plan"`
		SemanticPlan AlgorithmIdentity `json:"semantic_plan"`
	} `json:"algorithms"`
	// Selection is the neutral plan configuration: K, the sweep count, the
	// comparability token, the renderer identity.
	Selection struct {
		K        int    `json:"k"`
		Count    int    `json:"count"`
		Token    string `json:"token"`
		Runner   string `json:"runner"`
		Renderer string `json:"renderer"`
		TieBreak string `json:"tie_break"`
	} `json:"selection"`
	// Render is the render configuration the generated script bytes depend on.
	// It belongs in the bundle rather than in a replay flag: a replay that had
	// to be TOLD how to render could produce a different script from the same
	// bound inputs, which is exactly the unbound input the bundle exists to
	// close.
	Render struct {
		EventsDir       string `json:"events_dir"`
		FileParallelism int    `json:"file_parallelism"`
		WallDir         string `json:"wall_dir"`
	} `json:"render"`
	// AbsentInputs names every input that legitimately does not exist, so
	// "not present" is a bound claim rather than an omission.
	AbsentInputs []string `json:"absent_inputs"`
}

// Digest is the bundle's canonical identity.
func (b PlanningInputBundle) DigestOf() (Digest, error) { return DigestJSON(b) }

// Validate refuses a bundle that leaves an input unbound. It is deliberately
// strict about emptiness: an empty discovery snapshot is admissible only when
// it says so.
func (b PlanningInputBundle) Validate() error {
	if b.Kind != BundleKind {
		return fmt.Errorf("planning-input bundle kind %q, want %q", b.Kind, BundleKind)
	}
	if _, err := b.Clock.Time(); err != nil {
		return err
	}
	if b.Clock.Policy == "" || b.Clock.Precision == "" || b.Clock.TimeZone == "" {
		return fmt.Errorf("planning-input bundle: the clock policy is incomplete")
	}
	if len(b.Clock.PermittedSources) == 0 {
		return fmt.Errorf("planning-input bundle: the clock policy names no permitted source, so any clock would be admissible")
	}
	stale, err := time.ParseDuration(b.Clock.StaleThreshold)
	if err != nil {
		return fmt.Errorf("planning-input bundle: stale threshold %q: %w", b.Clock.StaleThreshold, err)
	}
	if stale <= 0 {
		return fmt.Errorf("planning-input bundle: the stale threshold is %s, so no store could ever be stale", b.Clock.StaleThreshold)
	}
	if len(b.Discovery) == 0 {
		return fmt.Errorf("planning-input bundle: no discovery snapshot (an absent one must be recorded explicitly)")
	}
	for _, s := range b.Discovery {
		if s.Digest != DigestBytes(s.Bytes) {
			return fmt.Errorf("planning-input bundle: discovery snapshot %q does not match its digest", s.Name)
		}
		if s.Empty != (len(s.Bytes) == 0) {
			return fmt.Errorf("planning-input bundle: discovery snapshot %q disagrees with its own empty flag", s.Name)
		}
		// Each snapshot carries its OWN acquisition closure, not just the
		// bundle's: two listings taken from different directories are
		// different observations.
		if s.Name == "" || len(s.Argv) == 0 || s.Cwd == "" {
			return fmt.Errorf("planning-input bundle: discovery snapshot %q does not record how it was acquired (name, argv, cwd)", s.Name)
		}
	}
	for _, s := range b.Runnables {
		if s.Digest != DigestBytes(s.Bytes) {
			return fmt.Errorf("planning-input bundle: runnable snapshot %q does not match its digest", s.TargetID)
		}
		if s.Empty != (len(s.Bytes) == 0) {
			return fmt.Errorf("planning-input bundle: runnable snapshot %q disagrees with its own empty flag", s.TargetID)
		}
		// Names are frozen alongside the bytes so the runtime feature vector
		// reads the evidence rather than re-deriving it. A listing with bytes
		// and no names would present a satisfied schema while reporting a
		// count of zero, which is worse than a missing feature.
		if len(s.Bytes) > 0 && len(s.Names) == 0 {
			return fmt.Errorf("planning-input bundle: runnable snapshot %q carries bytes but no parsed names", s.TargetID)
		}
		if len(s.Argv) == 0 || s.Cwd == "" || s.Env == nil {
			return fmt.Errorf("planning-input bundle: runnable snapshot %q does not record how it was acquired (argv, cwd, environment)", s.TargetID)
		}
	}
	if b.Store.Digest != DigestBytes(b.Store.Bytes) {
		return fmt.Errorf("planning-input bundle: the store snapshot does not match its digest")
	}
	// Store presence is STRUCTURAL, and absence and emptiness must agree with
	// the bytes.
	//
	// Absence used to be a sentence in AbsentInputs while the snapshot carried
	// whatever bytes it was given, so a bundle could declare a cold start and
	// hand the planner a warm store — validating cleanly and planning from
	// weights it had just said did not exist. Whether that is scored as a cold
	// row or a warm one then depends on which half of the bundle a reader
	// believes.
	//
	// A present but zero-byte store is bound as ABSENT: it carries no weights,
	// nothing in the evidence distinguishes it from a missing file, and both
	// are cold. An intentionally empty store is a store with bytes that parse
	// to no units, which is a different and explicitly representable thing.
	if b.StoreAbsent != (len(b.Store.Bytes) == 0) {
		if b.StoreAbsent {
			return fmt.Errorf("planning-input bundle: it declares the store absent but froze %d byte(s) of store; a cold start that plans from weights is not a cold start", len(b.Store.Bytes))
		}
		return fmt.Errorf("planning-input bundle: it declares a present store but froze no bytes; a store with nothing in it is a cold start and must be bound as one")
	}
	if b.Store.Empty != (len(b.Store.Bytes) == 0) {
		return fmt.Errorf("planning-input bundle: the store snapshot disagrees with its own empty flag")
	}
	if b.Algorithms.FullPlan.Name != FullPlanDigestAlgorithm || b.Algorithms.SemanticPlan.Name != SemanticPlanDigestAlgorithm {
		return fmt.Errorf("planning-input bundle: unknown plan-digest algorithm identities")
	}
	// A digest algorithm is a name AND an implementation. Two implementations
	// of one named algorithm can disagree, so binding the name alone leaves
	// the thing that actually computed the digest unbound.
	for label, a := range map[string]AlgorithmIdentity{
		"full-plan": b.Algorithms.FullPlan, "semantic-plan": b.Algorithms.SemanticPlan,
	} {
		if a.Canonicalizer == "" || a.Implementation == "" {
			return fmt.Errorf("planning-input bundle: the %s digest algorithm binds no canonicaliser or implementation identity", label)
		}
	}
	if len(b.Parsers) == 0 {
		return fmt.Errorf("planning-input bundle: no parser or policy identity is bound")
	}
	for _, p := range b.Parsers {
		if p.Name == "" || p.Version == "" || p.Digest == "" {
			return fmt.Errorf("planning-input bundle: parser %q has no version or digest", p.Name)
		}
	}
	// The source and acquisition closure: what tree the inputs came from, and
	// what it would take to reproduce taking them.
	if err := requireSet(map[string]string{
		"the source repository": b.Source.Repository,
		"the source commit":     b.Source.Commit,
		"the source tree":       b.Source.Tree,
		"the acquisition cwd":   b.Acquisition.Cwd,
	}); err != nil {
		return fmt.Errorf("planning-input bundle %w", err)
	}
	if err := requireFullSHA("the source commit", b.Source.Commit); err != nil {
		return fmt.Errorf("planning-input bundle: %w", err)
	}
	if len(b.Acquisition.Argv) == 0 {
		return fmt.Errorf("planning-input bundle: the acquisition argv is empty")
	}
	if len(b.Acquisition.Executables) == 0 {
		return fmt.Errorf("planning-input bundle: no resolved executable path is bound, so \"npx\" could name two different binaries on two runners")
	}
	if len(b.Acquisition.Tools) == 0 {
		return fmt.Errorf("planning-input bundle: no tool version is bound, so the same executable path could be two different toolchains")
	}
	if b.Acquisition.Env == nil {
		return fmt.Errorf("planning-input bundle: the planning-relevant environment is unbound (an empty map is a bound fact; a missing one is not)")
	}
	// The selection closure the plan is a function of.
	if b.Selection.K < 1 {
		return fmt.Errorf("planning-input bundle: K is %d", b.Selection.K)
	}
	if b.Selection.Count < 1 {
		return fmt.Errorf("planning-input bundle: the sweep count is %d", b.Selection.Count)
	}
	if err := requireSet(map[string]string{
		"the comparability token": b.Selection.Token,
		"the runner":              b.Selection.Runner,
		"the renderer identity":   b.Selection.Renderer,
		"the tie-break order":     b.Selection.TieBreak,
	}); err != nil {
		return fmt.Errorf("planning-input bundle %w", err)
	}
	return nil
}

// Signature is a detached authority signature over a canonical digest.
type Signature struct {
	// Authority is the protected environment that approved the document.
	Authority string `json:"authority"`
	KeyID     string `json:"key_id"`
	Digest    Digest `json:"digest"`
	Value     string `json:"value"`
}

// Stage1Manifest binds every INPUT before either role plans. It contains no
// plan, atom, topology, invocation, script or matrix digest: those do not
// exist yet, and a manifest that claimed them would be authorising an output
// nobody had derived.
type Stage1Manifest struct {
	Kind string `json:"kind"`
	Role string `json:"role"`
	// Actions maps each action directory (plan, run-bucket, record) to its
	// reviewed commit and content digest. A tag is metadata; this is identity.
	Actions map[string]ActionIdentity `json:"actions"`
	Source  struct {
		ReviewTip        string `json:"review_tip"`
		BinaryDigest     Digest `json:"binary_digest"`
		BuildAttestation string `json:"build_attestation"`
		ReleaseRefSHA    string `json:"release_ref_sha,omitempty"`
	} `json:"source"`
	Consumer struct {
		Repository    string `json:"repository"`
		Commit        string `json:"commit"`
		WorkflowSHA   string `json:"workflow_sha"`
		DownstreamRef string `json:"downstream_ref"`
		RunnerImage   string `json:"runner_image"`
		Facade        Digest `json:"facade_digest"`
		Config        Digest `json:"config_digest"`
		Lockfile      Digest `json:"lockfile_digest"`
	} `json:"consumer"`
	// SourceProfile proves the exact Vitest closure the lifecycle inventory
	// was written against.
	SourceProfile SourceProfileReceipt `json:"source_profile"`
	// Store is the admitted timing store's provenance: its exact bytes, its
	// migration epoch, how the copy was obtained, and the classification of
	// every row.
	Store StoreReceipt `json:"store_receipt"`
	// TrainingLineage names the sealed offline training receipt set and the
	// frozen scorer built from it. Runtime never reads a label; this is where
	// the labels are allowed to have existed.
	TrainingLineage TrainingLineageID `json:"training_lineage"`
	// Instrumentation binds the schema and binary identity of every producer
	// and of the verifier itself.
	Instrumentation InstrumentationIdentity `json:"instrumentation"`
	// AllowedDifferences enumerates what may differ between the two arms of a
	// pair. Anything else differing fails the pair.
	AllowedDifferences []string `json:"allowed_differences"`
	// Schedule is the authority-frozen campaign identity and pair order. The
	// contract requires Stage 1 to bind campaign/pair order before planning
	// and role assignment, and to freeze that order before the first candidate
	// run: without it, which five pairs count, which arm is baseline, and the
	// sequence they are attempted in are all decided after this document is
	// signed.
	Schedule CampaignSchedule `json:"campaign_schedule"`
	// Registry is the frozen Aeta component-registry template digest.
	Registry Digest `json:"component_registry_digest"`
	// Bundle is the planning-input bundle this manifest authorises.
	Bundle    PlanningInputBundle `json:"bundle"`
	Signature *Signature          `json:"signature,omitempty"`
}

// ActionIdentity is one action directory's reviewed identity.
type ActionIdentity struct {
	Commit string `json:"commit"`
	// ContentDigest is the sorted (relative path, mode, sha256) listing of the
	// directory. Symlinks are rejected when it is computed.
	ContentDigest Digest `json:"content_digest"`
}

// SourceProfileReceipt binds the resolved package closure the Vitest 4.1.10
// lifecycle claims depend on. A different version does not inherit them: it
// starts a new source-inventory epoch.
type SourceProfileReceipt struct {
	Repository  string            `json:"repository"`
	Commit      string            `json:"commit"`
	Facade      Digest            `json:"facade_digest"`
	Config      Digest            `json:"config_digest"`
	Lockfile    Digest            `json:"lockfile_digest"`
	ParserID    ParserIdentity    `json:"lock_parser"`
	Packages    map[string]string `json:"packages"`
	Integrities map[string]string `json:"integrities"`
}

// RequiredVitest is the version the exact lifecycle inventory was written
// against.
const RequiredVitest = "4.1.10"

// Validate proves the closure contains vitest and every @vitest/* package at
// the recorded version. A missing @vitest/runner is the interesting case: the
// façade loads it, so a closure without it has not proven what actually ran.
func (r SourceProfileReceipt) Validate() error {
	if err := requireSet(map[string]string{
		"the repository":      r.Repository,
		"the commit":          r.Commit,
		"the façade digest":   string(r.Facade),
		"the config digest":   string(r.Config),
		"the lockfile digest": string(r.Lockfile),
		"the lock parser":     r.ParserID.Name,
	}); err != nil {
		return fmt.Errorf("source profile %w", err)
	}
	if err := requireFullSHA("the source-profile commit", r.Commit); err != nil {
		return fmt.Errorf("source profile: %w", err)
	}
	if r.ParserID.Digest == "" || r.ParserID.Version == "" {
		return fmt.Errorf("source profile: the lock parser has no version or digest")
	}
	if len(r.Packages) == 0 {
		return fmt.Errorf("source profile: the resolved package closure is empty")
	}
	names := make([]string, 0, len(r.Packages))
	for n := range r.Packages {
		names = append(names, n)
	}
	sort.Strings(names)
	sawRunner := false
	for _, n := range names {
		if n != "vitest" && !hasPrefix(n, "@vitest/") {
			continue
		}
		if r.Packages[n] != RequiredVitest {
			return fmt.Errorf("source profile: %s is %s, not %s; this starts a new source-inventory epoch", n, r.Packages[n], RequiredVitest)
		}
		if r.Integrities[n] == "" {
			return fmt.Errorf("source profile: %s has no recorded lock integrity", n)
		}
		if n == "@vitest/runner" {
			sawRunner = true
		}
	}
	if r.Packages["vitest"] == "" {
		return fmt.Errorf("source profile: the closure does not contain vitest")
	}
	if !sawRunner {
		return fmt.Errorf("source profile: the closure does not contain @vitest/runner, which the façade loads")
	}
	return nil
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

// parsedStoreIdentity reads the schema and comparability token out of the
// exact frozen store bytes, so a receipt is checked against what the store
// SAYS rather than against what the receipt claims about it.
//
// Absent bytes are a cold start: schema 0 and an empty token, which a receipt
// must then also declare.
func parsedStoreIdentity(b []byte) (int, string) {
	d, err := DeriveStoreFacts(b)
	if err != nil {
		return 0, ""
	}
	return d.Schema, d.Token
}

// StoreFacts is what the ADMITTED STORE BYTES themselves say. Every field here
// is derived by parsing, never read from a receipt: the point of a receipt is
// to be checked, and a receipt that is only checked against its own claims is
// a signed assertion.
type StoreFacts struct {
	Schema int
	Token  string
	Rows   int
	// Coverage is the live target set the store recorded, sorted.
	Coverage []string
	// Measured counts rows carrying at least one ingest sample; Unmeasured
	// counts rows that exist with none. A row with samples but zero weight is
	// counted in Measured and is NOT a gap — that distinction is the whole
	// reason observed_zero is a class of its own.
	Measured   int
	Unmeasured int
	// Zero counts measured rows whose weight is zero. The bytes cannot tell a
	// target that ran in no time from one with no tests, so the receipt is
	// checked against the SUM of those two classes rather than against a split
	// the store does not record.
	Zero int
}

// DeriveStoreFacts parses the frozen store bytes and reports what they say.
func DeriveStoreFacts(b []byte) (StoreFacts, error) {
	var f StoreFacts
	if len(b) == 0 {
		return f, fmt.Errorf("the frozen store bytes are empty")
	}
	var probe struct {
		Schema   int      `json:"schema"`
		Flags    string   `json:"flags"`
		Coverage []string `json:"coverage"`
		Units    map[string]struct {
			Seconds float64 `json:"seconds"`
			Samples int     `json:"samples"`
		} `json:"units"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return f, fmt.Errorf("parse the frozen store bytes: %w", err)
	}
	f.Schema, f.Token, f.Rows = probe.Schema, probe.Flags, len(probe.Units)
	f.Coverage = append([]string(nil), probe.Coverage...)
	sort.Strings(f.Coverage)
	for _, u := range probe.Units {
		if u.Samples > 0 {
			f.Measured++
			if u.Seconds == 0 {
				f.Zero++
			}
			continue
		}
		f.Unmeasured++
	}
	return f, nil
}

// fullSHALen is the length of a full Git commit SHA. The contract is explicit
// that a full commit SHA is the immutable release reference; an abbreviation
// is a prefix that another object can grow into, and a tag is metadata.
const fullSHALen = 40

// requireFullSHA rejects anything that is not a full 40-hex-character commit
// identity.
func requireFullSHA(field, v string) error {
	if len(v) != fullSHALen {
		return fmt.Errorf("%s must be a full %d-character commit SHA, got %q", field, fullSHALen, v)
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return fmt.Errorf("%s must be lowercase hexadecimal, got %q", field, v)
		}
	}
	return nil
}

// requireSet refuses an empty identity. It exists so the manifest's required
// fields are enumerated in one readable list rather than as a wall of ifs.
func requireSet(fields map[string]string) error {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	var missing []string
	for _, name := range names {
		if strings.TrimSpace(fields[name]) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("does not bind %s", strings.Join(missing, ", "))
	}
	return nil
}

// isImmutableImage rejects a runner-image alias. `ubuntu-latest` names
// whatever GitHub rebuilt this morning; two arms of a pair resolved through it
// are not proven to have run on the same image, which is the entire point of
// binding it.
func isImmutableImage(v string) bool {
	return strings.Contains(v, "@sha256:") || strings.Contains(v, "@sha512:")
}

// StoreReceipt is the admitted timing store's provenance.
//
// It exists because the store FILE cannot carry these facts without a schema
// change, and a schema change is a new epoch that would strand every existing
// store. The receipt records them beside the store instead: the exact bytes,
// the schema and migration identity, the token the weights were measured
// under, and the classification of every row — including `observed_zero`,
// which the contract insists stays distinct from missing, failed and
// malformed. A zero that cannot be told from a gap is a gap.
type StoreReceipt struct {
	// Digest is the exact admitted bytes.
	Digest Digest `json:"digest"`
	Schema int    `json:"schema"`
	// MigrationID names the migration epoch the store belongs to. A migration
	// starts a new epoch and the old store is retained unchanged for audit, so
	// a store with no migration identity cannot say which epoch it is in.
	MigrationID string `json:"migration_id"`
	Token       string `json:"token"`
	// CacheKey and RestoreMethod are how the copy was obtained. A restore-key
	// FALLBACK is never warm evidence, so the method is recorded rather than
	// assumed.
	CacheKey      string `json:"cache_key"`
	RestoreMethod string `json:"restore_method"`
	// StaleAt is the instant beyond which this store is not warm evidence.
	StaleAt string `json:"stale_at"`
	// Classifications counts the mutually exclusive row states. They must sum
	// to Rows.
	Classifications map[string]int `json:"classifications"`
	Rows            int            `json:"rows"`
	// Coverage is the live target set the store was recorded against.
	Coverage []string `json:"coverage"`
}

// Row classification states. They are mutually exclusive, and `observed_zero`
// is deliberately its own state: a measured zero is a measurement, and folding
// it into "missing" would let a real observation disappear into a gap.
const (
	RowObservedPositive = "observed_positive"
	RowObservedZero     = "observed_zero"
	RowNoTests          = "no_tests"
	RowMissing          = "missing"
	RowFailed           = "failed"
	RowCancelled        = "cancelled"
	RowMalformed        = "malformed"
	RowExcluded         = "excluded"
)

// StoreRowStates is every admissible classification, in report order.
var StoreRowStates = []string{
	RowObservedPositive, RowObservedZero, RowNoTests, RowMissing,
	RowFailed, RowCancelled, RowMalformed, RowExcluded,
}

// Validate refuses a receipt that cannot support a warm claim.
//
// bytes are the EXACT admitted store bytes the bundle froze, parsedSchema and
// parsedToken what those bytes actually say, and now the canonical planning
// instant. All four are required: a receipt that merely cites a digest can
// still claim the wrong schema, the wrong comparability token, or a staleness
// nobody checked, and every one of those is a different store than the one the
// weights came from.
func (r StoreReceipt) Validate(bytes []byte, parsedSchema int, parsedToken string, now time.Time) error {
	if got := DigestBytes(bytes); got != r.Digest {
		return fmt.Errorf("store receipt: it describes %s but the bundle froze %s", r.Digest, got)
	}
	if r.Schema != parsedSchema {
		return fmt.Errorf("store receipt: it claims schema %d but the frozen bytes are schema %d", r.Schema, parsedSchema)
	}
	// An empty token in the bytes is a cold store; the receipt must say the
	// same thing rather than assert a comparability the bytes do not carry.
	if r.Token != parsedToken {
		return fmt.Errorf("store receipt: it claims token %q but the frozen bytes were measured under %q", r.Token, parsedToken)
	}
	if err := r.checkAgainstBytes(bytes); err != nil {
		return err
	}
	return r.validateFields(now)
}

// checkAgainstBytes DERIVES the row count, the coverage set and the row
// classification from the admitted store and refuses a receipt that disagrees.
//
// Before this, those three were signed assertions: a receipt could claim any
// coverage and any classification over bytes it merely cited by digest, and
// the only thing checked was that its own numbers added up. A receipt that is
// checked against its own arithmetic is not evidence about the store.
func (r StoreReceipt) checkAgainstBytes(b []byte) error {
	f, err := DeriveStoreFacts(b)
	if err != nil {
		return fmt.Errorf("store receipt: %w", err)
	}
	// The RESIDENT classes are the ones the store can speak to. The rest —
	// failed, cancelled, malformed, excluded — describe rows the ingest
	// declined to admit, which are by definition not in the store, so the
	// bytes constrain the resident total rather than Rows itself.
	resident := r.Classifications[RowObservedPositive] + r.Classifications[RowObservedZero] +
		r.Classifications[RowNoTests] + r.Classifications[RowMissing]
	if resident != f.Rows {
		return fmt.Errorf("store receipt: it classifies %d row(s) as present in the store but the frozen bytes hold %d", resident, f.Rows)
	}
	got := append([]string(nil), r.Coverage...)
	sort.Strings(got)
	if !equalStrings(got, f.Coverage) {
		return fmt.Errorf("store receipt: it claims coverage of %d target(s) but the frozen bytes recorded %d, and the two sets are not the same",
			len(got), len(f.Coverage))
	}
	// The bytes distinguish measured from unmeasured rows, and among measured
	// rows those with zero weight. They cannot tell a target that ran in no
	// time from one with no tests at all, so observed_zero and no_tests are
	// checked as one class rather than against a split the store never
	// recorded.
	if got, want := r.Classifications[RowMissing], f.Unmeasured; got != want {
		return fmt.Errorf("store receipt: it classifies %d row(s) as %s but %d row(s) in the frozen bytes carry no sample", got, RowMissing, want)
	}
	if got, want := r.Classifications[RowObservedZero]+r.Classifications[RowNoTests], f.Zero; got != want {
		return fmt.Errorf("store receipt: it classifies %d row(s) as %s or %s but %d measured row(s) in the frozen bytes weigh zero",
			got, RowObservedZero, RowNoTests, want)
	}
	if got, want := r.Classifications[RowObservedPositive], f.Measured-f.Zero; got != want {
		return fmt.Errorf("store receipt: it classifies %d row(s) as %s but %d measured row(s) in the frozen bytes carry a positive weight", got, RowObservedPositive, want)
	}
	return nil
}

// equalStrings compares two sorted lists.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// validateFields checks the receipt's internal consistency, independent of the
// bytes it describes.
func (r StoreReceipt) validateFields(now time.Time) error {
	if err := requireSet(map[string]string{
		"the store digest":        string(r.Digest),
		"the migration id":        r.MigrationID,
		"the comparability token": r.Token,
		"the restore method":      r.RestoreMethod,
		"the stale instant":       r.StaleAt,
	}); err != nil {
		return fmt.Errorf("store receipt %w", err)
	}
	// A restore-key fallback found SOME store, not THIS one. The contract is
	// explicit that it never proves a warm input.
	if r.RestoreMethod == "restore-key-fallback" {
		return fmt.Errorf("store receipt: the copy came from a restore-key fallback, which found some store rather than the admitted one")
	}
	staleAt, err := parseInstant(r.StaleAt)
	if err != nil {
		return fmt.Errorf("store receipt: %w", err)
	}
	// The staleness question is asked at the CANONICAL planning instant, not
	// at whatever time the check happens to run. A zero instant would silently
	// skip it, so it is refused.
	if now.IsZero() {
		return fmt.Errorf("store receipt: staleness must be judged against the canonical planning instant, and none was supplied")
	}
	if !now.Before(staleAt) {
		return fmt.Errorf("store receipt: the store is stale as of %s at the canonical instant %s, and a stale store is not warm evidence",
			r.StaleAt, now.UTC().Format(time.RFC3339))
	}
	total := 0
	for state, n := range r.Classifications {
		known := false
		for _, s := range StoreRowStates {
			if s == state {
				known = true
			}
		}
		if !known {
			return fmt.Errorf("store receipt: unknown row classification %q", state)
		}
		if n < 0 {
			return fmt.Errorf("store receipt: classification %q counts %d rows", state, n)
		}
		total += n
	}
	if total != r.Rows {
		return fmt.Errorf("store receipt: the classifications count %d rows but the store has %d", total, r.Rows)
	}
	return nil
}

// InstrumentationIdentity binds every producer and the verifier. A peer built
// from different bytes than these is not the peer the campaign authorised.
type InstrumentationIdentity struct {
	Schema             string   `json:"schema"`
	PhysicalBinary     Digest   `json:"physical_binary"`
	PeerBinary         Digest   `json:"peer_binary"`
	TraceBinary        Digest   `json:"trace_binary"`
	VerifierBinary     Digest   `json:"verifier_binary"`
	ContainmentPolicy  string   `json:"containment_policy"`
	ChildAdmission     string   `json:"child_admission_policy"`
	EndpointOrder      string   `json:"endpoint_order_policy"`
	CancellationPolicy string   `json:"cancellation_policy"`
	RawSourceTaxonomy  []string `json:"raw_source_taxonomy"`
	// Signers lists the PUBLIC halves of the run keys that may sign a
	// measurement's roster and closing seal. Per-producer record keys are
	// minted at run time and cannot be predeclared — they do not exist when
	// this manifest is signed — so what is bound here is the key that attests
	// to them, delivered only to the steps the measured script is not in.
	// Empty means nothing was declared, and a run whose signer set nobody
	// declared authenticates only itself.
	Signers []string `json:"signers"`
	// ReplaySigners are the keys allowed to sign an independent Stage-2 replay
	// attestation. They are kept separate from the authority key on purpose:
	// an attestation signed by the party that authorised the plan is the
	// planner re-checking its own work, which is what "independent" excludes.
	ReplaySigners []string `json:"replay_signers,omitempty"`
}

// policy is the part of the instrumentation identity that must be EQUAL
// between the two arms of a pair. The four binary digests are excluded: a
// candidate ships a different testbucket by definition, and its wrappers come
// with it. Everything describing HOW the measurement was taken stays.
func (i InstrumentationIdentity) policy() InstrumentationIdentity {
	i.PhysicalBinary, i.PeerBinary, i.TraceBinary, i.VerifierBinary = "", "", "", ""
	// Signer sets are per-arm delivery facts, not measurement policy: the two
	// arms of a pair run in different jobs and hold different run keys.
	i.Signers, i.ReplaySigners = nil, nil
	return i
}

// DigestOf is the manifest's canonical identity, taken over everything except
// the signature that covers it.
func (m Stage1Manifest) DigestOf() (Digest, error) {
	c := m
	c.Signature = nil
	return DigestJSON(c)
}

// Sign attaches the authority's detached signature.
func (m *Stage1Manifest) Sign(authority string, key ed25519.PrivateKey) error {
	d, err := m.DigestOf()
	if err != nil {
		return err
	}
	m.Signature = &Signature{
		Authority: authority,
		KeyID:     PublicKeyOf(key),
		Digest:    d,
		Value:     SignDigest(key, d),
	}
	return nil
}

// VerifySigned checks a detached signature over a document's own digest.
func VerifySigned(sig *Signature, digest Digest, allowedKeys []string) error {
	if sig == nil {
		return fmt.Errorf("document is unsigned")
	}
	if sig.Digest != digest {
		return fmt.Errorf("signature covers %s but the document digests to %s", sig.Digest, digest)
	}
	if len(allowedKeys) > 0 {
		ok := false
		for _, k := range allowedKeys {
			if k == sig.KeyID {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("signer %s is not an authorised authority key", sig.KeyID)
		}
	}
	pub, err := hex.DecodeString(sig.KeyID)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("malformed authority key id")
	}
	raw, err := base64.StdEncoding.DecodeString(sig.Value)
	if err != nil {
		return fmt.Errorf("malformed signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(digest), raw) {
		return fmt.Errorf("signature does not verify")
	}
	return nil
}

// Validate checks that the manifest binds EVERY identity the contract requires
// before either arm plans, and that it contains no derived output.
//
// It is deliberately exhaustive rather than representative. A manifest that
// binds most of the delivery identity proves most of the delivery, and "most"
// is indistinguishable from "none" when the question is whether two arms ran
// the same thing. Each field below is one the contract names.
func (m Stage1Manifest) Validate() error {
	if m.Kind != Stage1Kind {
		return fmt.Errorf("stage-1 manifest kind %q, want %q", m.Kind, Stage1Kind)
	}
	if m.Role != "baseline" && m.Role != "candidate" {
		return fmt.Errorf("stage-1 role %q must be baseline or candidate", m.Role)
	}

	// The three action directories, each by FULL commit SHA and content digest.
	for _, name := range []string{"plan", "run-bucket", "record"} {
		a, ok := m.Actions[name]
		if !ok || a.ContentDigest == "" {
			return fmt.Errorf("stage-1 manifest does not bind the %s action's commit and content digest", name)
		}
		if err := requireFullSHA("the "+name+" action commit", a.Commit); err != nil {
			return fmt.Errorf("stage-1 manifest: %w", err)
		}
	}

	// Delivery: reviewed tip, the release ref it must equal, the exact binary
	// and its build attestation. A local binary cannot deliver a scored row.
	if err := requireSet(map[string]string{
		"the reviewed tip":      m.Source.ReviewTip,
		"the binary digest":     string(m.Source.BinaryDigest),
		"the build attestation": m.Source.BuildAttestation,
		"the release ref SHA":   m.Source.ReleaseRefSHA,
	}); err != nil {
		return fmt.Errorf("stage-1 manifest %w", err)
	}
	if err := requireFullSHA("the reviewed tip", m.Source.ReviewTip); err != nil {
		return fmt.Errorf("stage-1 manifest: %w", err)
	}
	if err := requireFullSHA("the release ref SHA", m.Source.ReleaseRefSHA); err != nil {
		return fmt.Errorf("stage-1 manifest: %w", err)
	}
	if m.Source.ReleaseRefSHA != m.Source.ReviewTip {
		return fmt.Errorf("stage-1 manifest: the release ref resolves to %s but the reviewed tip is %s", m.Source.ReleaseRefSHA, m.Source.ReviewTip)
	}

	// Consumer closure: the repository, the commit, the caller workflow, the
	// downstream ref it resolves, and an IMMUTABLE runner image.
	if err := requireSet(map[string]string{
		"the consumer repository": m.Consumer.Repository,
		"the consumer commit":     m.Consumer.Commit,
		"the caller workflow SHA": m.Consumer.WorkflowSHA,
		"the downstream ref":      m.Consumer.DownstreamRef,
		"the runner image":        m.Consumer.RunnerImage,
		"the façade digest":       string(m.Consumer.Facade),
		"the config digest":       string(m.Consumer.Config),
		"the lockfile digest":     string(m.Consumer.Lockfile),
	}); err != nil {
		return fmt.Errorf("stage-1 manifest %w", err)
	}
	if err := requireFullSHA("the consumer commit", m.Consumer.Commit); err != nil {
		return fmt.Errorf("stage-1 manifest: %w", err)
	}
	if err := requireFullSHA("the caller workflow SHA", m.Consumer.WorkflowSHA); err != nil {
		return fmt.Errorf("stage-1 manifest: %w", err)
	}
	if !isImmutableImage(m.Consumer.RunnerImage) {
		return fmt.Errorf("stage-1 manifest: runner image %q is an alias, not an immutable identity; two arms resolved through an alias are not proven to have run on the same image", m.Consumer.RunnerImage)
	}

	if err := m.SourceProfile.Validate(); err != nil {
		return err
	}
	// The store the weights came from, bound to the EXACT bytes the bundle
	// froze and judged at the bundle's own canonical instant — not at signing
	// time, and not against fields that merely look filled in.
	instant, err := m.Bundle.Clock.Time()
	if err != nil {
		return fmt.Errorf("stage-1 manifest: %w", err)
	}
	schema, token := parsedStoreIdentity(m.Bundle.Store.Bytes)
	if err := m.Store.Validate(m.Bundle.Store.Bytes, schema, token, instant); err != nil {
		return err
	}
	if m.Store.Token != m.Bundle.Selection.Token {
		return fmt.Errorf("stage-1 manifest: the store was measured under token %q but this plan runs %q; weights are comparable only within one token",
			m.Store.Token, m.Bundle.Selection.Token)
	}

	// The source profile must describe the SOURCE THIS BUNDLE FROZE. A valid
	// receipt for some other tree proves that tree, not this one.
	if m.SourceProfile.Repository != m.Bundle.Source.Repository {
		return fmt.Errorf("stage-1 manifest: the source profile describes repository %q but the bundle froze %q",
			m.SourceProfile.Repository, m.Bundle.Source.Repository)
	}
	if m.SourceProfile.Commit != m.Bundle.Source.Commit {
		return fmt.Errorf("stage-1 manifest: the source profile describes commit %s but the bundle froze %s",
			m.SourceProfile.Commit, m.Bundle.Source.Commit)
	}
	// And the consumer identity must be the same façade, config and lockfile
	// the profile proved the Vitest closure for.
	for _, f := range []struct {
		name              string
		profile, consumer Digest
	}{
		{"façade", m.SourceProfile.Facade, m.Consumer.Facade},
		{"config", m.SourceProfile.Config, m.Consumer.Config},
		{"lockfile", m.SourceProfile.Lockfile, m.Consumer.Lockfile},
	} {
		if f.profile != f.consumer {
			return fmt.Errorf("stage-1 manifest: the source profile's %s digest is %s but the consumer identity binds %s",
				f.name, f.profile, f.consumer)
		}
	}
	// The delivered binary must be the one the producers actually run.
	if m.Source.BinaryDigest != m.Instrumentation.PhysicalBinary {
		return fmt.Errorf("stage-1 manifest: the delivered binary is %s but the physical wrapper is approved as %s",
			m.Source.BinaryDigest, m.Instrumentation.PhysicalBinary)
	}

	// The sealed training lineage and the frozen component registry.
	if err := requireSet(map[string]string{
		"the training receipt-set digest": string(m.TrainingLineage.ReceiptSetDigest),
		"the training cutoff instant":     m.TrainingLineage.Cutoff,
		"the training epoch":              m.TrainingLineage.Epoch,
		"the frozen scorer digest":        string(m.TrainingLineage.ScorerDigest),
		"the component registry digest":   string(m.Registry),
	}); err != nil {
		return fmt.Errorf("stage-1 manifest %w", err)
	}
	if _, err := parseInstant(m.TrainingLineage.Cutoff); err != nil {
		return fmt.Errorf("stage-1 manifest: training %w", err)
	}

	// The instrumentation identities and the allowed-difference matrix.
	if m.Instrumentation.Schema != SchemaVersion {
		return fmt.Errorf("stage-1 manifest binds instrumentation schema %q, want %q", m.Instrumentation.Schema, SchemaVersion)
	}
	if err := requireSet(map[string]string{
		"the physical wrapper binary": string(m.Instrumentation.PhysicalBinary),
		"the containment peer binary": string(m.Instrumentation.PeerBinary),
		"the trace collector binary":  string(m.Instrumentation.TraceBinary),
		"the verifier binary":         string(m.Instrumentation.VerifierBinary),
		"the containment policy":      m.Instrumentation.ContainmentPolicy,
		"the child-admission policy":  m.Instrumentation.ChildAdmission,
		"the endpoint-order policy":   m.Instrumentation.EndpointOrder,
		"the cancellation policy":     m.Instrumentation.CancellationPolicy,
	}); err != nil {
		return fmt.Errorf("stage-1 manifest %w", err)
	}
	if len(m.Instrumentation.RawSourceTaxonomy) == 0 {
		return fmt.Errorf("stage-1 manifest does not bind the raw-source taxonomy")
	}
	if len(m.AllowedDifferences) == 0 {
		return fmt.Errorf("stage-1 manifest does not enumerate the allowed differences between the two arms of a pair")
	}

	return m.Bundle.Validate()
}

// InvariantTuple is what must be BYTE-IDENTICAL between the two arms of a
// pair. Everything a run could differ in, other than the enumerated candidate
// testbucket tuple, lives here: if two arms disagree on any of it, the pair
// compared two different experiments.
func (m Stage1Manifest) InvariantTuple() map[string]string {
	// The rule is stated as an exclusion, so it is implemented as one: digest
	// the WHOLE bundle and the WHOLE instrumentation identity, and list only
	// the fields the candidate tuple is allowed to move. Enumerating what must
	// match instead is how a field like file_parallelism or wall_dir ends up
	// uncompared because nobody remembered to add it.
	bundle := m.Bundle
	out := map[string]string{
		"consumer.repository":     m.Consumer.Repository,
		"consumer.commit":         m.Consumer.Commit,
		"consumer.workflow_sha":   m.Consumer.WorkflowSHA,
		"consumer.downstream_ref": m.Consumer.DownstreamRef,
		"consumer.runner_image":   m.Consumer.RunnerImage,
		"consumer.facade":         string(m.Consumer.Facade),
		"consumer.config":         string(m.Consumer.Config),
		"consumer.lockfile":       string(m.Consumer.Lockfile),
		"source_profile":          string(mustDigestOf(m.SourceProfile)),
		"training_lineage":        string(mustDigestOf(m.TrainingLineage)),
		"component_registry":      string(m.Registry),
		"store_receipt":           string(mustDigestOf(m.Store)),
		"allowed_differences":     string(mustDigestOf(m.AllowedDifferences)),
		// The frozen pair ORDER. Both arms of a pair must be authorised by the
		// same schedule; two manifests carrying different orders are two
		// campaigns sharing an id, and comparing their arms compares nothing.
		"campaign_schedule": string(mustDigestOf(m.Schedule)),
		// The entire planning-input bundle: discovery and runnable bytes, the
		// acquisition closure, every parser and policy, both algorithm
		// implementations, the absent-input claims, the selection AND the
		// render configuration. Two arms that planned from different inputs
		// did not run the same experiment.
		"planning_input_bundle": string(mustDigestOf(bundle)),
		// The instrumentation POLICY, not its binaries. The candidate arm is
		// allowed to ship a different testbucket — that is the enumerated
		// difference the whole campaign is testing — and its wrappers are
		// "directly necessary schema-versioned" parts of that tuple. What may
		// NOT differ is how the two arms were instrumented: the containment
		// primitive, the admission and endpoint rules, the cancellation
		// behaviour, the raw-source taxonomy and the schema.
		"instrumentation_policy": string(mustDigestOf(m.Instrumentation.policy())),
	}
	// Readable sub-keys for the fields a reader most often wants named in a
	// diff. They are redundant with the bundle digest above and that is the
	// point: the digest catches everything, these say what changed.
	for k, v := range map[string]string{
		"selection.k":             fmt.Sprint(bundle.Selection.K),
		"selection.count":         fmt.Sprint(bundle.Selection.Count),
		"selection.token":         bundle.Selection.Token,
		"selection.runner":        bundle.Selection.Runner,
		"selection.renderer":      bundle.Selection.Renderer,
		"selection.tie_break":     bundle.Selection.TieBreak,
		"render.events_dir":       bundle.Render.EventsDir,
		"render.file_parallelism": fmt.Sprint(bundle.Render.FileParallelism),
		"render.wall_dir":         bundle.Render.WallDir,
		"store":                   string(bundle.Store.Digest),
		"clock.stale_threshold":   bundle.Clock.StaleThreshold,
		"clock.policy":            bundle.Clock.Policy,
		"instrumentation.schema":  m.Instrumentation.Schema,
		"containment_policy":      m.Instrumentation.ContainmentPolicy,
		"child_admission_policy":  m.Instrumentation.ChildAdmission,
		"endpoint_order_policy":   m.Instrumentation.EndpointOrder,
		"cancellation_policy":     m.Instrumentation.CancellationPolicy,
		"raw_source_taxonomy":     strings.Join(m.Instrumentation.RawSourceTaxonomy, ","),
	} {
		out[k] = v
	}
	return out
}

// CompareArms reports every invariant the two arms of a pair disagree on. An
// empty result is the only admissible one: the ONLY permitted difference is
// the enumerated candidate testbucket source/action/binary tuple, which lives
// outside this map by construction.
func CompareArms(baseline, candidate Stage1Manifest) []string {
	b, c := baseline.InvariantTuple(), candidate.InvariantTuple()
	keys := make([]string, 0, len(b))
	for k := range b {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var diffs []string
	for _, k := range keys {
		if b[k] != c[k] {
			diffs = append(diffs, fmt.Sprintf("%s: baseline %q, candidate %q", k, b[k], c[k]))
		}
	}
	if baseline.Role != "baseline" || candidate.Role != "candidate" {
		diffs = append(diffs, fmt.Sprintf("roles are %q and %q, want baseline and candidate", baseline.Role, candidate.Role))
	}
	return diffs
}

// mustDigestOf digests a sub-document for the invariant comparison. A value
// that cannot be canonicalised compares unequal to everything, which fails the
// pair rather than silently matching.
func mustDigestOf(v any) Digest {
	d, err := DigestJSON(v)
	if err != nil {
		return Digest("uncanonicalisable:" + err.Error())
	}
	return d
}

// InputAccess is one field of the bundle the planner actually read. The
// receipt of what was consumed is what makes "derived from these inputs"
// checkable rather than asserted.
type InputAccess struct {
	Field  string `json:"field"`
	Digest Digest `json:"digest"`
}

// Stage2Receipt is what the single authorised plan execution DERIVED. Its
// parent digests point back at the inputs; it cannot authorise an input, a
// result, a store state or a topology.
type Stage2Receipt struct {
	Kind         string        `json:"kind"`
	Stage1Digest Digest        `json:"stage1_digest"`
	BundleDigest Digest        `json:"planning_input_bundle_digest"`
	InputAccess  []InputAccess `json:"input_access"`

	PlanDigest       Digest `json:"full_plan_document_digest"`
	SemanticDigest   Digest `json:"semantic_plan_projection_digest"`
	AtomDigest       Digest `json:"atom_digest"`
	TopologyDigest   Digest `json:"topology_digest"`
	MembershipDigest Digest `json:"rendered_membership_digest"`
	InvocationDigest Digest `json:"invocation_digest"`
	ScriptDigest     Digest `json:"generated_script_digest"`
	MatrixDigest     Digest `json:"matrix_digest"`

	// Sidecars binds every per-bucket document this plan derived, by name and
	// digest.
	//
	// The receipt already digests the plan, the membership and the
	// invocations in aggregate. What it could not say is which per-bucket
	// Pcheck, forecast and invocation manifest were the authorised output —
	// those were written beside the receipt carrying nothing but a Stage-2
	// string, which any substituted document can also carry. Naming them here
	// puts them inside the one document that is signed and independently
	// replayed.
	Sidecars map[string]Digest `json:"derived_document_digests,omitempty"`

	Algorithms struct {
		FullPlan     AlgorithmIdentity `json:"full_plan"`
		SemanticPlan AlgorithmIdentity `json:"semantic_plan"`
	} `json:"algorithms"`
	PlannerResult  string     `json:"planner_result"`
	RendererResult string     `json:"renderer_result"`
	Signature      *Signature `json:"signature,omitempty"`
}

// PlanDigestOf is the receipt's PLAN identity: everything it says about the
// plan, excluding both its signature and the binding over the documents that
// plan derived.
//
// The two identities exist because the reference is circular: the receipt
// binds each derived document by digest, and each derived document names the
// receipt. Something has to be named that does not include the binding, and
// the plan itself is the honest choice — a sidecar is derived FROM the plan,
// so the plan is what it should be citing. Records and attestations continue
// to name DigestOf, which does cover the binding.
func (r Stage2Receipt) PlanDigestOf() (Digest, error) {
	c := r
	c.Signature, c.Sidecars = nil, nil
	return DigestJSON(c)
}

// SidecarName is the stable key a derived per-bucket document is bound under.
// It is a function so the writer and the verifier cannot spell it differently.
func SidecarName(kind string, bucket int) string {
	return fmt.Sprintf("%s-%d", kind, bucket)
}

// Sidecar kinds, as they appear in a Stage-2 receipt's binding.
const (
	SidecarPcheck      = "pcheck"
	SidecarAeta        = "aeta"
	SidecarInvocations = "invocations"
)

// checkSidecar reports whether a derived document is the one Stage 2 bound.
//
// An unbound sidecar is refused rather than accepted-with-a-warning: the whole
// point of the two-stage freeze is that exactly one plan was authorised, and a
// per-bucket document nobody bound is a document anybody could have written.
func (r Stage2Receipt) checkSidecar(kind string, bucket int, doc any) error {
	name := SidecarName(kind, bucket)
	want, ok := r.Sidecars[name]
	if !ok {
		return fmt.Errorf("the Stage-2 receipt binds no %s document for bucket %d, so the supplied one is not provably the authorised plan's output", kind, bucket)
	}
	got, err := DigestJSON(doc)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("the Stage-2 receipt binds %s %s for bucket %d but the supplied document digests to %s", kind, want, bucket, got)
	}
	return nil
}

// DigestOf is the receipt's canonical identity.
func (r Stage2Receipt) DigestOf() (Digest, error) {
	c := r
	c.Signature = nil
	return DigestJSON(c)
}

// Validate refuses a receipt that does not carry both plan digests, both
// algorithm identities, and its parent input digests.
func (r Stage2Receipt) Validate() error {
	if r.Kind != Stage2Kind {
		return fmt.Errorf("stage-2 receipt kind %q, want %q", r.Kind, Stage2Kind)
	}
	if r.Stage1Digest == "" || r.BundleDigest == "" {
		return fmt.Errorf("stage-2 receipt does not name its parent Stage-1 manifest and input bundle")
	}
	if r.PlanDigest == "" || r.SemanticDigest == "" {
		return fmt.Errorf("stage-2 receipt must carry BOTH the full-document and semantic-projection digests")
	}
	if r.Algorithms.FullPlan.Name != FullPlanDigestAlgorithm || r.Algorithms.SemanticPlan.Name != SemanticPlanDigestAlgorithm {
		return fmt.Errorf("stage-2 receipt names unknown plan-digest algorithms")
	}
	for label, a := range map[string]AlgorithmIdentity{
		"full-plan": r.Algorithms.FullPlan, "semantic-plan": r.Algorithms.SemanticPlan,
	} {
		if a.Canonicalizer == "" || a.Implementation == "" {
			return fmt.Errorf("stage-2 receipt: the %s digest algorithm binds no canonicaliser or implementation identity", label)
		}
	}
	// Every derived identity, not a representative sample. A receipt missing
	// the atom digest cannot detect an atom split; missing the topology
	// digest, a re-shaped schedule; missing the matrix digest, a different
	// fan-out. Each of those is separately terminal in the contract.
	if err := requireSet(map[string]string{
		"the rendered script digest":     string(r.ScriptDigest),
		"the invocation digest":          string(r.InvocationDigest),
		"the rendered membership digest": string(r.MembershipDigest),
		"the atom digest":                string(r.AtomDigest),
		"the topology digest":            string(r.TopologyDigest),
		"the matrix digest":              string(r.MatrixDigest),
	}); err != nil {
		return fmt.Errorf("stage-2 receipt %w", err)
	}
	if len(r.InputAccess) == 0 {
		return fmt.Errorf("stage-2 receipt carries no input-access record")
	}
	return nil
}

// Matches compares a receipt against an independently recomputed one. Every
// digest must agree: a semantic match with a differing full-document digest is
// still a different plan than the one that was authorised.
func (r Stage2Receipt) Matches(other Stage2Receipt) error {
	pairs := []struct {
		name string
		a, b Digest
	}{
		{"full plan document", r.PlanDigest, other.PlanDigest},
		{"semantic plan projection", r.SemanticDigest, other.SemanticDigest},
		{"atoms", r.AtomDigest, other.AtomDigest},
		{"topology", r.TopologyDigest, other.TopologyDigest},
		{"rendered membership", r.MembershipDigest, other.MembershipDigest},
		{"invocations", r.InvocationDigest, other.InvocationDigest},
		{"generated script", r.ScriptDigest, other.ScriptDigest},
		{"matrix", r.MatrixDigest, other.MatrixDigest},
		{"stage-1 parent", r.Stage1Digest, other.Stage1Digest},
		{"input bundle", r.BundleDigest, other.BundleDigest},
	}
	for _, p := range pairs {
		if p.a != p.b {
			return fmt.Errorf("%s digest mismatch: %s vs %s", p.name, p.a, p.b)
		}
	}
	// The per-bucket bindings are compared too. Aggregate digests say the two
	// parties derived the same plan; only these say they derived the same
	// documents the buckets will be verified against.
	if len(r.Sidecars) != len(other.Sidecars) {
		return fmt.Errorf("derived-document binding mismatch: the receipt binds %d document(s), the replay derived %d", len(r.Sidecars), len(other.Sidecars))
	}
	for name, want := range r.Sidecars {
		got, ok := other.Sidecars[name]
		if !ok {
			return fmt.Errorf("the receipt binds derived document %q, which the replay did not derive", name)
		}
		if got != want {
			return fmt.Errorf("derived document %q: %s vs %s", name, want, got)
		}
	}
	return nil
}

// ReplayKind identifies an independent Stage-2 replay attestation.
const ReplayKind = "tb.walltime.stage2-replay/v1"

// ReplayAttestation is an INDEPENDENT verifier's statement that it re-derived
// the plan from the frozen bundle and got the same thing.
//
// It exists because a Stage-2 receipt is the planner's own account of what it
// produced. Comparing that account to itself proves nothing; the contract
// requires a separate party to rerun the frozen parsers and policies over the
// frozen bytes and reject a changed plan, atom, topology, membership,
// invocation, script or matrix BEFORE the action starts. This document is that
// rerun's result, and `wall verify` refuses to score a run without one.
type ReplayAttestation struct {
	Kind         string `json:"kind"`
	Stage1Digest Digest `json:"stage1_digest"`
	// Stage2Digest is the ISSUED receipt this replay was checked against.
	Stage2Digest Digest `json:"stage2_digest"`
	BundleDigest Digest `json:"planning_input_bundle_digest"`
	// Recomputed is what the replay independently derived. It is kept whole
	// rather than reduced to a boolean so a reader can see WHICH digest
	// disagreed, if one did.
	Recomputed Stage2Receipt `json:"recomputed"`
	// VerifierID and VerifierBinary identify who ran the replay and with what
	// bytes; Stage 1 binds the approved verifier binary.
	VerifierID     string     `json:"verifier_id"`
	VerifierBinary Digest     `json:"verifier_binary"`
	Signature      *Signature `json:"signature,omitempty"`
}

// DigestOf is the attestation's canonical identity.
func (a ReplayAttestation) DigestOf() (Digest, error) {
	c := a
	c.Signature = nil
	return DigestJSON(c)
}

// Verify checks the attestation against the issued receipt and the Stage-1
// identities, and reports everything that disagrees.
func (a ReplayAttestation) Verify(issued Stage2Receipt, issuedDigest, stage1 Digest, instr InstrumentationIdentity) []string {
	var problems []string
	if a.Kind != ReplayKind {
		problems = append(problems, fmt.Sprintf("kind is %q, want %q", a.Kind, ReplayKind))
	}
	if issuedDigest != "" && a.Stage2Digest != issuedDigest {
		problems = append(problems, fmt.Sprintf("it attests to Stage-2 receipt %s, not the supplied %s", a.Stage2Digest, issuedDigest))
	}
	if stage1 != "" && a.Stage1Digest != stage1 {
		problems = append(problems, fmt.Sprintf("it names Stage-1 %s, not the verified %s", a.Stage1Digest, stage1))
	}
	if err := a.Recomputed.Validate(); err != nil {
		problems = append(problems, "the recomputed receipt is not well formed: "+err.Error())
	}
	if err := issued.Matches(a.Recomputed); err != nil {
		problems = append(problems, "the independent replay derived a different plan: "+err.Error())
	}
	if instr.VerifierBinary != "" && a.VerifierBinary != instr.VerifierBinary {
		problems = append(problems, fmt.Sprintf("the replay ran verifier binary %s, not the %s Stage 1 approved", a.VerifierBinary, instr.VerifierBinary))
	}
	if strings.TrimSpace(a.VerifierID) == "" {
		problems = append(problems, "the replay names no verifier")
	}
	return problems
}

// WriteJSONFile writes a receipt, manifest or bundle, refusing to overwrite.
//
// Refusal is the point. These documents are identities: the bound planner runs
// exactly once, a manifest authorises one set of inputs, and a scorer is the
// scorer a campaign froze. A second write that silently replaced the first
// would be indistinguishable from the first, which is the one thing a receipt
// must never be.
func WriteJSONFile(path string, v any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("walltime: %s already exists and these documents are identities, not outputs; write the new one to a new path", path)
		}
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return f.Sync()
}

// ReadJSONFile loads a receipt, manifest or bundle.
func ReadJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
