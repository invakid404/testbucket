package walltime

import (
	"bytes"
	"fmt"
	"path/filepath"
)

// Cache modes, dispositions and producers of contract §10.5.0. Each is a
// closed set: any other value fails, and no value is ever defaulted.
const (
	CacheModeDisabled = "disabled"
	CacheModeExactKey = "exact-key"

	DispositionDisabled = "disabled"
	DispositionExactHit = "exact-hit"
	DispositionMiss     = "miss"

	ProducerNone   = "none"
	ProducerAction = "action"
	ProducerCaller = "caller"
)

// declarationLeafOrder is the declaration class of §10.5.0's table, in the
// order that table lists them. §10.5.2 step 1 fixes both the order and the
// encoding, and the digest is taken over exactly this sequence.
var declarationLeafOrder = [5]string{
	"dependency_cache_mode",
	"dependency_cache_primary_key",
	"transform_cache_mode",
	"dependency_cache_producer",
	"expected_mongo_binary_sha256",
}

// outcomeLeafNames is the outcome class of §10.5.0's table. Outcome leaves live
// on the observation and NOWHERE ELSE — in particular the ring row registers no
// cache_state path at all, because the ring holds the design columns, the
// response and the identities a re-fit needs, and no fit reads a cache outcome.
var outcomeLeafNames = [6]string{
	"dependency_cache_hit",
	"dependency_cache_matched_key",
	"dependency_cache_disposition",
	"mongo_binary_sha256",
	"mongo_binary_path",
	"mongo_binary_verified_on_runner",
}

// DeclarationLeafNames and OutcomeLeafNames expose the two classes so a test
// can DERIVE the partition rather than transcribe it. §10.5.0 states that no
// document writes a leaf count, so nothing here does either.
func DeclarationLeafNames() []string { return append([]string(nil), declarationLeafOrder[:]...) }
func OutcomeLeafNames() []string     { return append([]string(nil), outcomeLeafNames[:]...) }

// CacheDeclaration is the frozen declaration half of §10.5.0: the leaves that
// are knowable before dispatch, identical across every bucket job of a run, and
// the only ones that reach `cache_declaration_digest` and therefore the
// comparability key.
type CacheDeclaration struct {
	DependencyCacheMode       string `json:"dependency_cache_mode"`
	DependencyCachePrimaryKey string `json:"dependency_cache_primary_key"`
	TransformCacheMode        string `json:"transform_cache_mode"`
	// DependencyCacheProducer is a DECLARATION, not an outcome. The caller
	// chooses it before dispatch exactly as it chooses runs-on-label; filing it
	// with the outcomes let two arms of one pair run under different producers
	// and still pass every invariant (owner F2, R14-F2).
	DependencyCacheProducer   string `json:"dependency_cache_producer"`
	ExpectedMongoBinarySHA256 string `json:"expected_mongo_binary_sha256"`
}

func (d CacheDeclaration) leaf(n int) (string, string) {
	switch n {
	case 1:
		return declarationLeafOrder[0], d.DependencyCacheMode
	case 2:
		return declarationLeafOrder[1], d.DependencyCachePrimaryKey
	case 3:
		return declarationLeafOrder[2], d.TransformCacheMode
	case 4:
		return declarationLeafOrder[3], d.DependencyCacheProducer
	case 5:
		return declarationLeafOrder[4], d.ExpectedMongoBinarySHA256
	}
	panic(fmt.Sprintf("walltime: cache declaration has no leaf %d", n))
}

// CanonicalJSON renders the declaration as §10.5.2 step 1 requires: the
// declaration leaves in §10.5.0's order, canonical JSON, no whitespace.
func (d CacheDeclaration) CanonicalJSON() []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i := 1; i <= len(declarationLeafOrder); i++ {
		if i > 1 {
			b.WriteByte(',')
		}
		name, value := d.leaf(i)
		writeCanonicalString(&b, name)
		b.WriteByte(':')
		writeCanonicalString(&b, value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// Digest is `cache_declaration_digest`, comparability-key leaf 15. The key
// leaf, the workflow output and the per-job verification are ONE value.
func (d CacheDeclaration) Digest() Digest { return DigestBytes(d.CanonicalJSON()) }

// Validate enforces §10.5.0's legal values and §10.5.3's mode/producer pairing.
// Every legal mode has exactly one legal producer state.
func (d CacheDeclaration) Validate() error {
	switch d.DependencyCacheMode {
	case CacheModeDisabled:
		if d.DependencyCachePrimaryKey != "" {
			return fmt.Errorf("cache declaration: primary key must be empty iff mode is disabled")
		}
		// `none` is required when, and only when, the mode is disabled.
		if d.DependencyCacheProducer != ProducerNone {
			return fmt.Errorf("cache declaration: mode disabled requires producer %q, got %q", ProducerNone, d.DependencyCacheProducer)
		}
	case CacheModeExactKey:
		if d.DependencyCachePrimaryKey == "" {
			return fmt.Errorf("cache declaration: primary key must be non-empty under exact-key")
		}
		if d.DependencyCacheProducer != ProducerAction && d.DependencyCacheProducer != ProducerCaller {
			return fmt.Errorf("cache declaration: mode exact-key requires producer action or caller, got %q", d.DependencyCacheProducer)
		}
	default:
		return fmt.Errorf("cache declaration: dependency_cache_mode %q is not disabled or exact-key", d.DependencyCacheMode)
	}
	// Vitest's transform cache directory is fresh per run, so `disabled` is
	// the only legal value and there is no other.
	if d.TransformCacheMode != CacheModeDisabled {
		return fmt.Errorf("cache declaration: transform_cache_mode is %q; disabled is the only legal value", d.TransformCacheMode)
	}
	if d.ExpectedMongoBinarySHA256 == "" {
		return fmt.Errorf("cache declaration: expected_mongo_binary_sha256 is absent")
	}
	return nil
}

// ProducerResult is what a restore step reported. Hit is a pointer because
// §10.5.3 makes a tri-state illegal while ABSENCE is meaningful: an empty
// matched key WITH hit:false is a caller-owned miss, and an empty matched key
// WITHOUT a hit input is absent producer data. Before this input existed the
// two were indistinguishable, which is the R14-F2 finding.
type ProducerResult struct {
	Hit        *bool
	MatchedKey *string
}

// DeriveDisposition is §10.5.3's table, and no other rule. It fails the job
// BEFORE the bucket script starts on every offending combination: the outcomes
// are never inferred from the filesystem, never defaulted to `miss`, and never
// omitted.
func DeriveDisposition(d CacheDeclaration, r ProducerResult) (matchedKey, disposition string, err error) {
	if err := d.Validate(); err != nil {
		return "", "", err
	}
	companionsPresent := r.Hit != nil || r.MatchedKey != nil

	switch d.DependencyCacheProducer {
	case ProducerNone:
		// No restore attempted; both companion inputs must be absent.
		if companionsPresent {
			return "", "", fmt.Errorf("cache: producer none with a companion input present; a producer under a disabled cache is rejected, never silently preferred")
		}
		return "", DispositionDisabled, nil

	case ProducerAction:
		// The action-owned restore reports its result through the same
		// companion fields as ProducerCaller. The absence of both is the
		// only rejected case: it means the restore step ran but its outputs
		// were never wired to this call, which fails closed.
		if r.Hit == nil || r.MatchedKey == nil {
			return "", "", fmt.Errorf("cache: producer action requires both dependency-cache-hit and dependency-cache-matched-key from the action's own restore step")
		}
		hit, mk := *r.Hit, *r.MatchedKey
		switch {
		case hit && mk == d.DependencyCachePrimaryKey:
			return mk, DispositionExactHit, nil
		case !hit && mk == "":
			return "", DispositionMiss, nil
		case hit && mk != "" && mk != d.DependencyCachePrimaryKey:
			return "", "", fmt.Errorf("cache: producer action prefix fallback rejected — matched key %q differs from primary key %q", mk, d.DependencyCachePrimaryKey)
		case hit && mk == "":
			return "", "", fmt.Errorf("cache: producer action incoherent result — hit true with an empty matched key")
		default: // !hit && mk != ""
			return "", "", fmt.Errorf("cache: producer action incoherent result — hit false with a non-empty matched key %q", mk)
		}

	case ProducerCaller:
		if r.Hit == nil || r.MatchedKey == nil {
			return "", "", fmt.Errorf("cache: producer caller requires both dependency-cache-hit and dependency-cache-matched-key")
		}
		hit, mk := *r.Hit, *r.MatchedKey
		switch {
		case hit && mk == d.DependencyCachePrimaryKey:
			return mk, DispositionExactHit, nil
		case !hit && mk == "":
			// A legal tuple, not a violation.
			return "", DispositionMiss, nil
		case hit && mk != "" && mk != d.DependencyCachePrimaryKey:
			return "", "", fmt.Errorf("cache: prefix fallback rejected — matched key %q differs from primary key %q", mk, d.DependencyCachePrimaryKey)
		case hit && mk == "":
			return "", "", fmt.Errorf("cache: incoherent producer result — hit true with an empty matched key")
		default: // !hit && mk != ""
			return "", "", fmt.Errorf("cache: incoherent producer result — hit false with a non-empty matched key %q", mk)
		}
	}
	return "", "", fmt.Errorf("cache: dependency_cache_producer %q is not none, action or caller", d.DependencyCacheProducer)
}

// QC14 checks the recorded tuple against §10.5.1's exhaustive table. Every
// other combination fails, including the prefix-fallback tuple a
// `restore-keys:` list produces.
func QC14(c CacheState) error {
	switch {
	case c.DependencyCacheMode == CacheModeExactKey &&
		c.DependencyCachePrimaryKey != "" &&
		c.DependencyCacheMatchedKey == c.DependencyCachePrimaryKey &&
		c.DependencyCacheDisposition == DispositionExactHit:
	case c.DependencyCacheMode == CacheModeExactKey &&
		c.DependencyCachePrimaryKey != "" &&
		c.DependencyCacheMatchedKey == "" &&
		c.DependencyCacheDisposition == DispositionMiss:
	case c.DependencyCacheMode == CacheModeDisabled &&
		c.DependencyCachePrimaryKey == "" &&
		c.DependencyCacheMatchedKey == "" &&
		c.DependencyCacheDisposition == DispositionDisabled:
	default:
		return fmt.Errorf("QC14: cache tuple (mode %q, primary %q, matched %q, disposition %q) is not one of §10.5.1's three legal tuples",
			c.DependencyCacheMode, c.DependencyCachePrimaryKey, c.DependencyCacheMatchedKey, c.DependencyCacheDisposition)
	}
	if c.TransformCacheMode != CacheModeDisabled {
		return fmt.Errorf("QC14: transform_cache_mode is %q; disabled is the only legal value", c.TransformCacheMode)
	}
	if c.MongoBinarySHA256 == "" {
		return fmt.Errorf("QC14: mongo_binary_sha256 is absent")
	}
	// §10.5.0: the hit is present iff the producer is caller.
	if c.DependencyCacheProducer == ProducerCaller && c.DependencyCacheHit == nil {
		return fmt.Errorf("QC14: producer caller but dependency_cache_hit is absent from the row")
	}
	if c.DependencyCacheProducer != ProducerCaller && c.DependencyCacheHit != nil {
		return fmt.Errorf("QC14: producer %q but dependency_cache_hit is present on the row", c.DependencyCacheProducer)
	}
	return nil
}

// validSHA256 reports whether s is a well-formed SHA-256 hex digest (exactly
// 64 lower-case hex characters). The contract requires the shortest round-trip
// representation, so upper-case hex and leading zeros are not an equivalence.
func validSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// QC14b is the record job's half: it runs over the UPLOADED ROW ONLY.
//
// It accepts a well-formed row whose `mongo_binary_path` does not exist on the
// record runner, because that path names a file on ANOTHER runner. An
// implementation that stats, opens or re-hashes it is wrong: the executed-binary
// check is QC14a's, on the runner that executed it.
func QC14b(c CacheState) error {
	if err := QC14(c); err != nil {
		return err
	}
	// §10.5.6: the executed-binary SHA-256 must be a well-formed lower-case hex
	// digest. An arbitrary equal string is not a digest — the grammar prevents
	// a future reader from misinterpreting the value.
	if !validSHA256(c.MongoBinarySHA256) {
		return fmt.Errorf("QC14b: mongo_binary_sha256 %q is not a well-formed 64-character lower-case hex SHA-256", c.MongoBinarySHA256)
	}
	// §10.5.6: the binary path must be present and absolute so a downstream
	// reader can distinguish "no path recorded" from "root-relative path". A
	// path that exists on another runner but is well-formed can still be
	// validated structurally.
	if c.MongoBinaryPath == "" || !filepath.IsAbs(c.MongoBinaryPath) {
		return fmt.Errorf("QC14b: mongo_binary_path %q is absent or not an absolute path", c.MongoBinaryPath)
	}
	if !c.MongoBinaryVerifiedOnRunner {
		return fmt.Errorf("QC14b: mongo_binary_verified_on_runner is false; a row that did not pass QC14a on its own runner is not ingestible")
	}
	if c.MongoBinarySHA256 != c.ExpectedMongoBinarySHA256 {
		return fmt.Errorf("QC14b: executed binary digest %q does not equal the declared expected_mongo_binary_sha256 %q",
			c.MongoBinarySHA256, c.ExpectedMongoBinarySHA256)
	}
	return nil
}

// DeclarationOf reads the declaration half back out of a serialized row.
//
// §10.5.0 splits the block into a frozen DECLARATION and per-job OUTCOMES, and
// the declaration leaves are carried on the row beside the outcomes. Extracting
// them is what makes QC14b requirement 3 decidable from the row: the five leaves
// re-digest to the value the row claims, or they are not the declaration it says
// it ran under.
func DeclarationOf(c CacheState) CacheDeclaration {
	return CacheDeclaration{
		DependencyCacheMode:       c.DependencyCacheMode,
		DependencyCachePrimaryKey: c.DependencyCachePrimaryKey,
		TransformCacheMode:        c.TransformCacheMode,
		DependencyCacheProducer:   c.DependencyCacheProducer,
		ExpectedMongoBinarySHA256: c.ExpectedMongoBinarySHA256,
	}
}

// QC14bAgainstPlan is §10.5.6 requirement 3, which had no implementation.
//
// QC14b checked the row's internal coherence and its executed-versus-expected
// binary digest. It could not check the requirement that matters for routing:
// that the declaration leaves are byte-identical to the digest-verified
// declaration the PLAN validated. Nothing carried that declaration into
// admission, so an internally coherent row describing a DIFFERENT exact key or
// producer was admitted while echoing the expected profile and key digests —
// which is a misrouted measurement joining a history it did not belong to.
//
// Both halves are decidable from bytes this job holds: the row's own leaves must
// re-digest to the digest the row claims, and that digest must be the plan's. No
// access to the matrix runner's filesystem is needed or taken.
func QC14bAgainstPlan(c CacheState, rowDeclDigest, planDeclDigest Digest) error {
	if rowDeclDigest == "" {
		return fmt.Errorf("QC14b: the row carries no cache_declaration_digest, so its declaration cannot be bound to the one the plan validated")
	}
	if got := DeclarationOf(c).Digest(); got != rowDeclDigest {
		return fmt.Errorf("QC14b: the row's declaration leaves digest to %s and it claims cache_declaration_digest %s; the leaves are not the declaration the row names",
			got, rowDeclDigest)
	}
	if planDeclDigest == "" {
		return fmt.Errorf("QC14b: the plan context carries no cache_declaration_digest, so §10.5.6 requirement 3 cannot be checked; an unverifiable declaration is not a verified one")
	}
	if rowDeclDigest != planDeclDigest {
		return fmt.Errorf("QC14b: the row ran under cache declaration %s and the plan validated %s; a row whose declaration is not the plan's belongs to another run",
			rowDeclDigest, planDeclDigest)
	}
	return nil
}

// ScoredCacheSymmetry is contract §19.2b (ID-24): a scored pair is
// cache-symmetric or cache-disabled, and there are only those two modes.
//
// Mode (a) disabled: both mode leaves are `disabled`, the producer is `none` in
// both, primary and matched keys are empty, and the hit is absent.
//
// Mode (b) exact-key: the same primary key and producer, and for EVERY declared
// cache index the two arms agree on hit, matched key and disposition.
//
// An index-wise difference is a POST-START outcome: the pair is retained,
// unscored and non-passing under §19.8, both rows stay in the attempted
// population, and it is never voided, never rescheduled and never averaged
// away. Recording a difference is not equalizing it — the withdrawn rule that
// scored such a pair anyway is a must-fail case.
type SymmetryVerdict struct {
	Scored  bool
	Passing bool
	// Retained is always true for a post-start outcome: the rows stay in the
	// attempted population.
	Retained bool
	Reason   string
	// FirstDifferingIndex is -1 when the arms agree at every index.
	FirstDifferingIndex int
}

func ScoredCacheSymmetry(armB, armC []CacheState) (SymmetryVerdict, error) {
	if len(armB) != len(armC) {
		return SymmetryVerdict{}, fmt.Errorf("cache symmetry: arms have %d and %d bucket rows; a pair compares index by index", len(armB), len(armC))
	}
	if len(armB) == 0 {
		return SymmetryVerdict{}, fmt.Errorf("cache symmetry: empty arms")
	}

	// §17.19 first: the whole-run cross-arm invariants must agree before the
	// index-wise rule is reached at all.
	for i := range armB {
		if armB[i].DependencyCacheMode != armC[i].DependencyCacheMode {
			return SymmetryVerdict{}, fmt.Errorf("§17.19: arms declare different dependency_cache_mode (%q vs %q)",
				armB[i].DependencyCacheMode, armC[i].DependencyCacheMode)
		}
		if armB[i].DependencyCacheProducer != armC[i].DependencyCacheProducer {
			return SymmetryVerdict{}, fmt.Errorf("§17.19: arms declare different dependency_cache_producer (%q vs %q)",
				armB[i].DependencyCacheProducer, armC[i].DependencyCacheProducer)
		}
		if armB[i].DependencyCachePrimaryKey != armC[i].DependencyCachePrimaryKey {
			return SymmetryVerdict{}, fmt.Errorf("§17.19: arms declare different dependency_cache_primary_key")
		}
	}

	mode := armB[0].DependencyCacheMode
	for _, arm := range [][]CacheState{armB, armC} {
		for i, c := range arm {
			if err := QC14(c); err != nil {
				return SymmetryVerdict{}, fmt.Errorf("bucket %d: %w", i, err)
			}
			if c.DependencyCacheMode != mode {
				return SymmetryVerdict{}, fmt.Errorf("cache symmetry: bucket %d declares mode %q against the pair's %q", i, c.DependencyCacheMode, mode)
			}
		}
	}

	switch mode {
	case CacheModeDisabled:
		// Mode (a): the shape is fixed and there is nothing to compare
		// index-wise, because no restore happened in either arm.
		for _, arm := range [][]CacheState{armB, armC} {
			for i, c := range arm {
				if c.DependencyCacheProducer != ProducerNone ||
					c.DependencyCachePrimaryKey != "" ||
					c.DependencyCacheMatchedKey != "" ||
					c.DependencyCacheHit != nil ||
					c.DependencyCacheDisposition != DispositionDisabled {
					return SymmetryVerdict{}, fmt.Errorf("cache symmetry: bucket %d does not take the prescribed disabled shape", i)
				}
			}
		}
		return SymmetryVerdict{Scored: true, Passing: true, Retained: true, FirstDifferingIndex: -1}, nil

	case CacheModeExactKey:
		// Mode (b): index-wise equality of the three outcome leaves.
		for i := range armB {
			b, c := armB[i], armC[i]
			differs := b.DependencyCacheMatchedKey != c.DependencyCacheMatchedKey ||
				b.DependencyCacheDisposition != c.DependencyCacheDisposition ||
				!boolPtrEqual(b.DependencyCacheHit, c.DependencyCacheHit)
			if differs {
				return SymmetryVerdict{
					Scored:              false,
					Passing:             false,
					Retained:            true,
					Reason:              fmt.Sprintf("arms differ at bucket index %d in hit, matched key or disposition; retained, unscored and non-passing under §19.8", i),
					FirstDifferingIndex: i,
				}, nil
			}
		}
		return SymmetryVerdict{Scored: true, Passing: true, Retained: true, FirstDifferingIndex: -1}, nil
	}
	return SymmetryVerdict{}, fmt.Errorf("cache symmetry: unreachable mode %q", mode)
}

func boolPtrEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
