package walltime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// EstBasis names the weight the partition was built from. Contract §5.1 permits
// exactly two values, and §13.0 makes `profile.est_basis` the source of truth —
// the top-level `est_basis` on the plan and matrix is additive output derived
// from it (SR-5).
type EstBasis string

const (
	BasisReporter EstBasis = "reporter"
	BasisWall     EstBasis = "wall"
)

// Valid reports whether b is one of the two basis values §5.1 permits. A third
// value is a fail-closed condition, never a default.
func (b EstBasis) Valid() bool { return b == BasisReporter || b == BasisWall }

// CanonicalProfile is the canonical `profile` type of contract §13.0: one type,
// three consumers. The plan emits it, every observation copies it VERBATIM, and
// the validator compares the two.
//
// Every field is frozen at plan time. `BucketIndices` is the complete index set
// and never a count.
type CanonicalProfile struct {
	Scored                bool     `json:"scored"`
	RunnerToken           string   `json:"runner_token"`
	K                     int      `json:"k"`
	Count                 int      `json:"count"`
	FileParallelism       int      `json:"file_parallelism"`
	BucketIndices         []int    `json:"bucket_indices"`
	EstBasis              EstBasis `json:"est_basis"`
	StoreSHA256           string   `json:"store_sha256"`
	ExpandedUnitSetDigest string   `json:"expanded_unit_set_digest"`
	// SameRepositoryWorkload declares that the workload this plan schedules is
	// the ORCHESTRATION CHECKOUT ITSELF — the project dogfooding its own test
	// suite — so §13's three provenance identities are genuinely one commit.
	//
	// It exists because QC15 rejects a row carrying one value in all three, and
	// that rejection is right for every other shape: S-6's defect was one
	// `head_sha` overloaded into three fields. A same-repository dogfood is not
	// that defect. Its orchestration head, the source the `local` build compiled,
	// and the workload checkout ARE the same commit, and there is no truthful
	// distinct value to put in the other two fields.
	//
	// IT IS A PLAN DECLARATION, not a row's claim about itself. §13.0 has every
	// observation copy this block VERBATIM and QC13 compares it byte for byte, so
	// a bucket runner cannot award itself the carve-out: the plan job declared it
	// before the matrix existed. A scored run may never declare it — see
	// AdmitScoredProfile — and an external-consumer run has no reason to, which
	// is what keeps the carve-out narrow.
	SameRepositoryWorkload bool `json:"same_repository_workload"`

	// scoredExplicit records that the wire carried `scored` as a JSON boolean —
	// not absent, not null. It is set by Parse and never serialized: it is a fact
	// about the BYTES, and a block constructed in Go always has the key because
	// `scored` carries no omitempty.
	//
	// ScoredIsExplicit is the only reader, and §13.0's QC15 carve-out is the only
	// rule that needs it.
	scoredExplicit bool
}

// ScoredIsExplicit reports whether the parsed block stated `scored` as a JSON
// boolean, rather than omitting it or setting it to null.
//
// It exists because the carve-out of §13.0 is for an EXPLICITLY unscored row.
// Absent and null both decode to Go's `false`, so "this run is not scored" and
// "this block does not say" were the same value — and the second is not a
// declaration. A block NewProfileBlock produced always satisfies this; one that
// does not came from somewhere else.
func (p CanonicalProfile) ScoredIsExplicit() bool { return p.scoredExplicit }

// runtimeProfileFieldOrder is the canonical order contract §15.3a's table
// gives, and the digest is defined over exactly this sequence.
//
// This is NOT RFC 8785 order. Canonical JSON sorts keys lexicographically,
// which would put `dependency_cache_mode` first; §15.3a fixes a different,
// semantic order and test 73 asserts that permuting the seven fields changes
// the digest. So the runtime profile gets its own ordered serializer rather
// than going through CanonicalJSON.
var runtimeProfileFieldOrder = [7]string{
	"node_version",
	"pnpm_version",
	"vitest_version",
	"testbucket_sha256",
	"facade_command",
	"lock_sha256",
	"dependency_cache_mode",
}

// RuntimeProfile is the seven-field object of contract §15.3a. The plan job
// serializes it as `runtime_profile_declared`; every bucket recomputes it from
// what it actually ran and serializes it as `runtime_profile`.
//
// Both the object and its digest travel: the digest alone could tell you a
// bucket drifted but never WHICH field, and QC17 is required to name the first
// differing constituent by field number.
type RuntimeProfile struct {
	NodeVersion         string `json:"node_version"`
	PnpmVersion         string `json:"pnpm_version"`
	VitestVersion       string `json:"vitest_version"`
	TestbucketSHA256    string `json:"testbucket_sha256"`
	FacadeCommand       string `json:"facade_command"`
	LockSHA256          string `json:"lock_sha256"`
	DependencyCacheMode string `json:"dependency_cache_mode"`
}

// field returns the constituent at 1-based position n of §15.3a's table, which
// is the numbering QC17 reports.
func (p RuntimeProfile) field(n int) (name, value string) {
	switch n {
	case 1:
		return runtimeProfileFieldOrder[0], p.NodeVersion
	case 2:
		return runtimeProfileFieldOrder[1], p.PnpmVersion
	case 3:
		return runtimeProfileFieldOrder[2], p.VitestVersion
	case 4:
		return runtimeProfileFieldOrder[3], p.TestbucketSHA256
	case 5:
		return runtimeProfileFieldOrder[4], p.FacadeCommand
	case 6:
		return runtimeProfileFieldOrder[5], p.LockSHA256
	case 7:
		return runtimeProfileFieldOrder[6], p.DependencyCacheMode
	}
	panic(fmt.Sprintf("walltime: runtime profile has no field %d", n))
}

// OrderedJSON renders the seven fields in §15.3a's declared order with no
// whitespace and strings as JSON strings. It is the input to the digest.
func (p RuntimeProfile) OrderedJSON() []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i := 1; i <= len(runtimeProfileFieldOrder); i++ {
		if i > 1 {
			b.WriteByte(',')
		}
		name, value := p.field(i)
		writeCanonicalString(&b, name)
		b.WriteByte(':')
		writeCanonicalString(&b, value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// RuntimeProfileDigest is SHA-256 over OrderedJSON. Contract §15.3a defines it
// over exactly the seven fields in exactly that order.
func RuntimeProfileDigest(p RuntimeProfile) Digest {
	return DigestBytes(p.OrderedJSON())
}

// QC17 compares an executed runtime profile against the plan's declared one,
// field by field in the declared order, and the digests as a whole.
//
// It names the FIRST differing constituent by field number: a digest
// comparison alone could never do that, which is why §15.3a serializes both
// objects rather than only their digests. A digest difference with no field
// difference — or the reverse — is itself a failure, because the digest must be
// the one §15.3a defines over that object.
//
// The outcome is a row failure and a POST-START one: the row is not appended
// and never enters history, and a scored pair is left retained, unscored and
// non-passing under §19.8 — never voided, never rescheduled.
func QC17(declared, executed RuntimeProfile, declaredDigest, executedDigest Digest) error {
	firstDiff := 0
	for i := 1; i <= len(runtimeProfileFieldOrder); i++ {
		dn, dv := declared.field(i)
		_, ev := executed.field(i)
		if dv != ev {
			firstDiff = i
			return fmt.Errorf("QC17: runtime profile field %d (%s) differs: plan declared %q, bucket executed %q",
				i, dn, dv, ev)
		}
	}

	wantDeclared := RuntimeProfileDigest(declared)
	wantExecuted := RuntimeProfileDigest(executed)
	if declaredDigest != wantDeclared {
		return fmt.Errorf("QC17: declared runtime_profile_declared_digest %s is not the digest of its own object (%s)",
			declaredDigest, wantDeclared)
	}
	if executedDigest != wantExecuted {
		return fmt.Errorf("QC17: observation runtime_profile_digest %s is not the digest of its own object (%s)",
			executedDigest, wantExecuted)
	}
	// Every field agreed, so the digests must too. A difference here means one
	// side computed the digest over something other than its object.
	if declaredDigest != executedDigest {
		return fmt.Errorf("QC17: runtime profile fields all agree but digests differ (%s vs %s)",
			declaredDigest, executedDigest)
	}
	_ = firstDiff
	return nil
}

// ProfileBlock carries the canonical profile as it was transported, so QC13 can
// compare bytes rather than meaning.
//
// QC13 must reject a REORDERED block (§22 test 36), and canonicalising both
// sides would normalise key order away — so the raw bytes are retained and
// compared directly. Parsing is a separate, lossy view.
type ProfileBlock struct {
	raw json.RawMessage
}

// NewProfileBlock serializes p into a transportable block. The bytes are
// canonical JSON, so a plan and a faithful copy agree byte-for-byte.
func NewProfileBlock(p CanonicalProfile) (ProfileBlock, error) {
	b, err := CanonicalJSON(p)
	if err != nil {
		return ProfileBlock{}, fmt.Errorf("serialize canonical profile: %w", err)
	}
	return ProfileBlock{raw: b}, nil
}

// Raw returns the transported bytes.
func (b ProfileBlock) Raw() json.RawMessage { return b.raw }

// Parse decodes the block. It rejects unknown keys: the block's membership is
// the field registry's `profile` projection, so a key outside it is drift and
// not something to tolerate.
//
// IT ALSO RECORDS WHETHER `scored` WAS AN EXPLICIT JSON BOOLEAN.
//
// Go decodes an absent key and a `null` value into the same `false` a present
// `false` produces, and §13.0's QC15 carve-out is available only to a row that is
// EXPLICITLY unscored. Without the distinction, a block that simply omits
// `scored` — or sets it to null — reads as "not scored" and collects an exception
// meant for a declared, deliberate shape. QC13's byte-identity cannot help: it
// makes the plan and the row agree, and two blocks can agree on a field neither
// of them contains.
func (b ProfileBlock) Parse() (CanonicalProfile, error) {
	var p CanonicalProfile
	dec := json.NewDecoder(bytes.NewReader(b.raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return CanonicalProfile{}, fmt.Errorf("parse canonical profile: %w", err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(b.raw, &probe); err != nil {
		return CanonicalProfile{}, fmt.Errorf("parse canonical profile presence: %w", err)
	}
	raw, ok := probe["scored"]
	p.scoredExplicit = ok && (string(bytes.TrimSpace(raw)) == "true" || string(bytes.TrimSpace(raw)) == "false")
	return p, nil
}

func (b ProfileBlock) MarshalJSON() ([]byte, error) {
	if len(b.raw) == 0 {
		return []byte("null"), nil
	}
	return b.raw, nil
}

func (b *ProfileBlock) UnmarshalJSON(data []byte) error {
	b.raw = append(json.RawMessage(nil), data...)
	return nil
}

// QC13 rejects an observation whose profile block differs by any byte from the
// block the plan gave it. It is mandatory for every wall-basis observation and
// every campaign row, and appears in no optional list: it is what makes
// §19.3a's plan-time admission checkable at run time rather than merely
// declared.
func QC13(planBlock, observationBlock ProfileBlock) error {
	if !bytes.Equal(planBlock.raw, observationBlock.raw) {
		return fmt.Errorf("QC13: observation profile block is not the plan's block verbatim:\n  plan: %s\n  obs:  %s",
			planBlock.raw, observationBlock.raw)
	}
	return nil
}

// AdmitScoredProfile is contract §19.3a's observable plan admission. A scored
// plan refuses to emit a matrix unless every one of these holds; §0.8's phase 2
// makes this a veto over whatever mode selection chose.
func AdmitScoredProfile(p CanonicalProfile, runnerClass, runsOnLabel, candidateSHA, workloadCommit string, cacheDeclared bool) error {
	if !p.Scored {
		return nil
	}
	if p.K != 8 {
		return fmt.Errorf("scored plan admission: k is %d, must be 8", p.K)
	}
	if p.Count != 1 {
		return fmt.Errorf("scored plan admission: count is %d, must be 1", p.Count)
	}
	if p.FileParallelism > 1 {
		return fmt.Errorf("scored plan admission: file_parallelism is %d, must not exceed 1", p.FileParallelism)
	}
	if p.RunnerToken != "vitest" {
		return fmt.Errorf("scored plan admission: runner_token is %q, must be vitest", p.RunnerToken)
	}
	// A SCORED ARM MEASURES A PINNED EXTERNAL WORKLOAD, so it can never be the
	// orchestration checkout and may not declare the same-repository carve-out.
	// §21 requires a scored arm to pin an exact `vX.Y.Z` binary, which already
	// makes `candidate_sha` a different commit from the orchestration head. The
	// refusal is here as well as in QC15 because a plan that cannot produce an
	// ingestible row should not emit a matrix at all.
	if p.SameRepositoryWorkload {
		return fmt.Errorf("scored plan admission: same_repository_workload is declared; " +
			"a scored arm measures a pinned external workload and its three provenance identities " +
			"are separately bound (§7.1 QC15)")
	}
	want := make([]int, 8)
	for i := range want {
		want[i] = i
	}
	got := append([]int(nil), p.BucketIndices...)
	sort.Ints(got)
	if len(got) != len(want) {
		return fmt.Errorf("scored plan admission: bucket set has %d entries, must be exactly [0..7]", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			return fmt.Errorf("scored plan admission: bucket set is %v, must be exactly [0..7]", p.BucketIndices)
		}
	}
	// AD-8: the comparability key's two action-supplied leaves.
	if runnerClass == "" {
		return fmt.Errorf("scored plan admission: runner-class is empty (AD-8)")
	}
	if runsOnLabel == "" {
		return fmt.Errorf("scored plan admission: runs-on-label is empty (AD-8)")
	}
	// AD-9: the cache state must be declared, not inferred.
	if !cacheDeclared {
		return fmt.Errorf("scored plan admission: cache_state is undeclared (AD-9)")
	}
	// AD-10: two of the three identities of §13; head_sha comes from the event.
	if candidateSHA == "" {
		return fmt.Errorf("scored plan admission: candidate-sha is empty (AD-10)")
	}
	if workloadCommit == "" {
		return fmt.Errorf("scored plan admission: workload-commit is empty (AD-10)")
	}
	return nil
}
