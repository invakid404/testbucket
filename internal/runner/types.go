// Package runner is the seam between testbucket's language-agnostic CORE
// (KK partition, the rolling timing store, whale detection, the never-drop
// coverage gate, plan + audit) and the per-framework ADAPTERS that know how
// to talk to a concrete test runner.
//
// It holds two things and nothing else:
//
//   - the Runner interface — the minimal set of operations the core needs a
//     framework adapter to provide (discovery, per-target runnable
//     enumeration, timing ingest, invocation rendering, command-grammar
//     validation, and an opaque comparability token). No method takes a
//     framework-specific argument; a runner's own run configuration (flags,
//     timeout, sweep count, node detection) lives entirely inside the adapter.
//   - the value types that cross that seam — LivePackage (one discovered
//     target), Unit (one schedulable invocation), Bucket, Invocation,
//     RunSummary (one run's timings), and the render-out struct.
//
// This package imports only the standard library. It carries NO toolchain
// knowledge: how a target is discovered, how a run's timings are parsed, and
// how a bucket becomes a concrete command are all the adapter's business, and
// the core speaks only in the neutral value types below. gorunner is adapter #1
// (Go's `go test`); a second adapter (e.g. Vitest) implements the same
// interface against the same types. See README.md, "Adding a framework
// adapter".
package runner

// LivePackage is one discovered test TARGET. It is the authority on what must
// run: the store only supplies weights.
//
// The core reads a target through THREE neutral fields only — Identity (the
// store key and timing identity), AtomKey (the co-scheduling group), and
// HasTests. Everything else is BACKING DATA the owning adapter populates for
// its own renderer and grammar check; the core never reads it. The Go adapter
// fills Dir/Module/Mode; a Vitest adapter would leave them zero and set only
// ID, Atom and HasTests.
type LivePackage struct {
	// ID is the neutral identity: the store key and the identity a run's
	// timings report. For the Go adapter it is the import path; for another
	// adapter, whatever its timings key on (a Vitest file, say).
	ID string `json:"id"`
	// Atom is the neutral co-scheduling key: a non-empty value means this
	// target must ride in one invocation with every other target sharing the
	// key (a whole Go module that cannot be mixed into a shared build list; a
	// Vitest project); an empty value means the target mixes freely for
	// balance. This is the one place the "co-scheduling boundary is a SOFT
	// factor" rule is expressed as data the core acts on without knowing what a
	// Go module is.
	Atom string `json:"atom,omitempty"`
	// HasTests is false for targets with no tests. They are excluded from
	// bucketing (running them is a no-op) and from the coverage gate, so a
	// target that GAINS a test is picked up by the next discovery and scheduled
	// immediately.
	HasTests bool `json:"has_tests"`

	// --- adapter render backing (Go adapter #1); other adapters leave zero ---

	// Dir is the target directory relative to the repo root.
	Dir string `json:"dir,omitempty"`
	// Module is the module directory the invocation is issued from.
	Module string `json:"module,omitempty"`
	// Mode is the Go resolution mode ("work" / "off").
	Mode string `json:"mode,omitempty"`
}

const (
	// ModeWork is a Go target that resolves under the active workspace.
	ModeWork = "work"
	// ModeOff is a Go target that resolves standalone (GOWORK=off) and so
	// cannot share an invocation with workspace-mode targets.
	ModeOff = "off"
)

// Identity is the neutral store key / timing identity of a target.
func (p LivePackage) Identity() string { return p.ID }

// AtomKey is the co-scheduling group of a target: a non-empty key means this
// target must ride in one invocation with every other target sharing the key;
// an empty key means the target mixes freely. The core groups by this without
// knowing what the key means to the adapter that set it.
func (p LivePackage) AtomKey() string { return p.Atom }

// Kind is the granularity tier a scheduled unit sits at.
type Kind string

const (
	// KindPackage is tier 1: one whole target, the default.
	KindPackage Kind = "package"
	// KindCountShard is tier 2a: the target run whole but with the flake sweep
	// count divided S ways. Needs no per-runnable data, which is what makes it
	// the day-one harpoon for a whale the store knows nothing about internally.
	KindCountShard Kind = "count-shard"
	// KindRunSlice is tier 2b: a name subset of the target. Needs per-runnable
	// weights, gives a genuinely finer cut than count-sharding.
	KindRunSlice Kind = "run-slice"
	// KindModuleAtom is a whole atom group packed as one unit because its
	// targets cannot be mixed into a shared invocation.
	KindModuleAtom Kind = "module-atom"
)

// Unit is one schedulable thing: exactly one runner invocation. The core builds
// it (weight assignment, whale expansion, KK name-packing); the adapter renders
// it into a concrete command and validates its grammar.
type Unit struct {
	ID       string        `json:"id"`
	Kind     Kind          `json:"kind"`
	Seconds  float64       `json:"seconds"`
	Estimate bool          `json:"estimated,omitempty"`
	Packages []LivePackage `json:"packages"`
	Module   string        `json:"module"`
	Mode     string        `json:"mode"`
	// Count is the neutral sweep repetitions for THIS invocation: the base
	// sweep for a whole/slice unit, or the divided sweep for a count-shard.
	Count  int      `json:"count"`
	Run    []string `json:"run,omitempty"`
	Shard  int      `json:"shard,omitempty"`
	Shards int      `json:"shards,omitempty"`
}

// Bucket is one lane of the generated matrix: the units KK assigned to it.
type Bucket struct {
	Index   int     `json:"bucket"`
	Units   []Unit  `json:"units"`
	Seconds float64 `json:"est_seconds"`
}

// Invocation is one concrete runner call a bucket must make.
type Invocation struct {
	// Dir is the directory to run from, relative to the repo root.
	Dir string `json:"dir"`
	// Env holds the resolution-mode envelope (e.g. GOWORK=off for modules that
	// are not workspace members).
	Env  map[string]string `json:"env,omitempty"`
	Args []string          `json:"args"`
	Desc string            `json:"desc"`
	// Units is the ids of the scheduled units this one call covers. The
	// adapter knows the grouping — which units merged into a shared command
	// and which had to be their own — and nothing downstream can re-derive it
	// reliably, since two name slices of one file are indistinguishable from
	// their descriptions alone. The audit and the predictor projection both
	// read it.
	Units []string `json:"units,omitempty"`
	// Selector is the test SELECTION this call applies: the path tokens it
	// passes and any name filter. It is recorded separately from Args because
	// the measured wrapper digests it as the invocation's selector identity,
	// and re-deriving a selection from a human description would lose exactly
	// the name filter that distinguishes two slices of one file.
	Selector []string `json:"selector,omitempty"`
	// Atoms is the co-scheduling keys of the targets this call covers, sorted.
	// An atom split is terminal, so the identity of what rode together is
	// bound rather than inferred.
	Atoms []string `json:"atoms,omitempty"`
}
