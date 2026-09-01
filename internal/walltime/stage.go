package walltime

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
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
	TargetID string   `json:"target_id"`
	Argv     []string `json:"argv,omitempty"`
	Names    []string `json:"names"`
	Empty    bool     `json:"empty"`
	Bytes    []byte   `json:"bytes,omitempty"`
	Digest   Digest   `json:"digest"`
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
	}
	for _, s := range b.Runnables {
		if len(s.Bytes) > 0 && s.Digest != DigestBytes(s.Bytes) {
			return fmt.Errorf("planning-input bundle: runnable snapshot %q does not match its digest", s.TargetID)
		}
	}
	if b.Store.Digest != DigestBytes(b.Store.Bytes) {
		return fmt.Errorf("planning-input bundle: the store snapshot does not match its digest")
	}
	if b.Algorithms.FullPlan.Name != FullPlanDigestAlgorithm || b.Algorithms.SemanticPlan.Name != SemanticPlanDigestAlgorithm {
		return fmt.Errorf("planning-input bundle: unknown plan-digest algorithm identities")
	}
	if len(b.Parsers) == 0 {
		return fmt.Errorf("planning-input bundle: no parser or policy identity is bound")
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
	// Signers lists the public keys that may sign records. A record signed by
	// anything else is not evidence this campaign bound.
	Signers []string `json:"signers"`
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
		Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(d))),
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

// Validate checks the manifest's internal consistency, including that it
// contains no derived output.
func (m Stage1Manifest) Validate() error {
	if m.Kind != Stage1Kind {
		return fmt.Errorf("stage-1 manifest kind %q, want %q", m.Kind, Stage1Kind)
	}
	if m.Role != "baseline" && m.Role != "candidate" {
		return fmt.Errorf("stage-1 role %q must be baseline or candidate", m.Role)
	}
	for _, name := range []string{"plan", "run-bucket", "record"} {
		a, ok := m.Actions[name]
		if !ok || a.Commit == "" || a.ContentDigest == "" {
			return fmt.Errorf("stage-1 manifest does not bind the %s action's commit and content digest", name)
		}
	}
	if m.Source.ReviewTip == "" || m.Source.BinaryDigest == "" {
		return fmt.Errorf("stage-1 manifest does not bind the reviewed tip and binary digest")
	}
	if m.Source.ReleaseRefSHA != "" && m.Source.ReleaseRefSHA != m.Source.ReviewTip {
		return fmt.Errorf("stage-1 manifest: the release ref resolves to %s but the reviewed tip is %s", m.Source.ReleaseRefSHA, m.Source.ReviewTip)
	}
	if err := m.SourceProfile.Validate(); err != nil {
		return err
	}
	if m.Instrumentation.Schema != SchemaVersion {
		return fmt.Errorf("stage-1 manifest binds instrumentation schema %q, want %q", m.Instrumentation.Schema, SchemaVersion)
	}
	if err := m.Bundle.Validate(); err != nil {
		return err
	}
	return nil
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

	Algorithms struct {
		FullPlan     AlgorithmIdentity `json:"full_plan"`
		SemanticPlan AlgorithmIdentity `json:"semantic_plan"`
	} `json:"algorithms"`
	PlannerResult  string     `json:"planner_result"`
	RendererResult string     `json:"renderer_result"`
	Signature      *Signature `json:"signature,omitempty"`
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
	if r.ScriptDigest == "" || r.InvocationDigest == "" || r.MembershipDigest == "" {
		return fmt.Errorf("stage-2 receipt does not bind the rendered script, invocations and membership")
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
	return nil
}

// WriteJSONFile writes a receipt or manifest, refusing to overwrite. Refusal
// is the point: the planner may run EXACTLY ONCE, and a second run that
// silently replaced the first receipt would look identical to the first.
func WriteJSONFile(path string, v any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("walltime: %s already exists: the bound planner runs exactly once, and a replan is not admissible", path)
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
