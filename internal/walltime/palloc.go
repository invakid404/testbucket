package walltime

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Palloc has TWO sealed surfaces and they never touch.
//
// The OFFLINE TRAINING surface may read historical, wrapper-qualified physical
// V labels — that is where a learnable score comes from, and forbidding it
// outright would make one impossible. The RUNTIME ALLOCATION surface is
// exactly frozen_scorer(frozen_preplan_unit_feature_vector): no label, no
// outcome, no observed timing, not even the store's own EWMA weight, which is
// reporter-derived and therefore an outcome.
//
// Keeping both surfaces in one file is deliberate: the rule that separates
// them should be readable in one place, and the prohibited-provenance list
// below is the enforcement, not a comment.
const (
	TrainingSetKind = "tb.walltime.training-receipts/v1"
	ScorerKind      = "tb.walltime.scorer/v1"
)

// Provenance classes admissible as RUNTIME features. Each one is an immutable
// pre-plan fact bound by Stage 1.
const (
	ProvUnitIdentity      = "frozen_unit_identity"
	ProvDiscoverySnapshot = "frozen_discovery_snapshot"
	ProvRunnableSnapshot  = "frozen_runnable_snapshot"
	ProvPreplanAtom       = "frozen_preplan_atom_membership"
)

// prohibitedRuntimeProvenance is the outcome-derived world the runtime scorer
// may not see. The store's rolling EWMA is on this list on purpose: it is
// built from reporter timings, so using it as a Palloc feature would leak an
// outcome into allocation through the side door.
var prohibitedRuntimeProvenance = map[string]string{
	"reporter_timing":     "reporter data is an outcome",
	"store_ewma":          "the timing store is built from reporter outcomes",
	"observed_timing":     "an observed duration is an outcome",
	"candidate":           "candidate-derived input leaks the arm under test",
	"campaign":            "campaign-derived input leaks the experiment",
	"current_run":         "current-run input leaks this run's outcome",
	"trace":               "trace time is an outcome",
	"physical_envelope":   "A, AT, VB and V are outcomes",
	"host":                "host state is not an immutable pre-plan input",
	"cache":               "cache state is not an immutable pre-plan input",
	"setup":               "setup/action-intercept timing is an outcome",
	"process":             "process timing is an outcome",
	"result":              "a result is an outcome",
	"rendered_membership": "rendered membership is post-plan",
	"post_plan_topology":  "topology after planning is post-plan",
}

// Feature is one runtime input with its provenance attached. Provenance is a
// FIELD, not a convention: the verifier proves exclusion by reading it.
type Feature struct {
	Name       string  `json:"name"`
	Value      float64 `json:"value"`
	Provenance string  `json:"provenance"`
}

// FeatureVector is one candidate unit's immutable pre-plan features.
type FeatureVector struct {
	UnitID   string    `json:"unit_id"`
	Features []Feature `json:"features"`
	// Stage1 binds the frozen inputs these were derived from.
	Stage1 Digest `json:"stage1_digest"`
}

// Validate rejects any prohibited provenance, an unknown class, or a
// non-finite value.
func (v FeatureVector) Validate(schema []string) error {
	if len(v.Features) == 0 {
		return fmt.Errorf("feature vector for %q is empty", v.UnitID)
	}
	seen := map[string]bool{}
	for _, f := range v.Features {
		if why, bad := prohibitedRuntimeProvenance[f.Provenance]; bad {
			return fmt.Errorf("runtime feature %q has prohibited provenance %q: %s", f.Name, f.Provenance, why)
		}
		switch f.Provenance {
		case ProvUnitIdentity, ProvDiscoverySnapshot, ProvRunnableSnapshot, ProvPreplanAtom:
		default:
			return fmt.Errorf("runtime feature %q has unrecognised provenance %q", f.Name, f.Provenance)
		}
		if math.IsNaN(f.Value) || math.IsInf(f.Value, 0) {
			return fmt.Errorf("runtime feature %q is not finite", f.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("runtime feature %q appears twice", f.Name)
		}
		seen[f.Name] = true
	}
	for _, name := range schema {
		if !seen[name] {
			return fmt.Errorf("feature vector for %q is missing schema feature %q", v.UnitID, name)
		}
	}
	// A feature outside the schema is allowed through — it has already passed
	// the provenance check above and the scorer has no coefficient for it, so
	// it cannot influence a score. Refusing it would only force the builder
	// and every frozen scorer to be versioned in lockstep.
	return nil
}

// Value looks a feature up by name. It is exported because the value a
// feature carries is as much a part of the contract as its provenance: a test
// that only checks where a number came from cannot notice that it is wrong.
func (v FeatureVector) Value(name string) (float64, bool) { return v.value(name) }

// value looks a feature up by name.
func (v FeatureVector) value(name string) (float64, bool) {
	for _, f := range v.Features {
		if f.Name == name {
			return f.Value, true
		}
	}
	return 0, false
}

// LabelProvenance is the only admissible training provenance.
const LabelProvenance = "wrapper_qualified_physical_v"

// TrainingLineageID is what Stage 1 binds: the sealed receipt set and the
// frozen scorer built from it, by digest.
type TrainingLineageID struct {
	ReceiptSetDigest Digest `json:"receipt_set_digest"`
	Cutoff           string `json:"cutoff_instant"`
	Epoch            string `json:"epoch"`
	ScorerID         string `json:"scorer_id"`
	ScorerDigest     Digest `json:"scorer_digest"`
	Algorithm        string `json:"algorithm"`
	Configuration    string `json:"configuration"`
	Seed             int64  `json:"seed"`
	TieBreak         string `json:"tie_break"`
}

func setOf(v []string) map[string]bool {
	out := map[string]bool{}
	for _, s := range v {
		out[s] = true
	}
	return out
}

// PermanentExclusions are the anti-overfit examples the contract names. They
// are never training, threshold, baseline, candidate or holdout data.
var PermanentExclusions = []string{"PR#4000", "PR#3999", "PR#182", "PR#4017"}

// Scorer is the frozen runtime allocation surface. It is a plain linear model
// over the frozen feature schema: deterministic, inspectable, and incapable of
// reading anything it was not handed.
type Scorer struct {
	Kind          string             `json:"kind"`
	ID            string             `json:"id"`
	Version       string             `json:"version"`
	FeatureSchema []string           `json:"feature_schema"`
	Coefficients  map[string]float64 `json:"coefficients"`
	Intercept     float64            `json:"intercept"`
	// Floor keeps a score positive: a unit the model scores at or below zero
	// still has to be scheduled somewhere, and a zero-weight unit would make
	// the partition think it is free.
	Floor float64 `json:"floor_seconds"`
	// Lineage names the sealed training set this was built from.
	Lineage TrainingLineageID `json:"lineage"`
}

// DigestOf is the scorer's identity. The lineage's own ScorerDigest field is
// excluded before hashing for the obvious reason: a document cannot contain
// its own digest, and leaving it in would make the identity unreproducible.
func (s Scorer) DigestOf() (Digest, error) {
	c := s
	c.Lineage.ScorerDigest = ""
	return DigestJSON(c)
}

// Score is the whole runtime allocation surface:
// Palloc[u] = frozen_scorer(frozen_preplan_unit_feature_vector[u]).
// It reads nothing else, and it refuses a vector that is not exactly the
// frozen schema.
func (s Scorer) Score(v FeatureVector) (float64, error) {
	if err := v.Validate(s.FeatureSchema); err != nil {
		return 0, err
	}
	out := s.Intercept
	for _, name := range s.FeatureSchema {
		val, ok := v.value(name)
		if !ok {
			return 0, fmt.Errorf("feature %q is missing for unit %q", name, v.UnitID)
		}
		out += s.Coefficients[name] * val
	}
	if out < s.Floor {
		out = s.Floor
	}
	if math.IsNaN(out) || math.IsInf(out, 0) {
		return 0, fmt.Errorf("scorer produced a non-finite value for unit %q", v.UnitID)
	}
	return out, nil
}

// ScorerTieBreak is the frozen ordering the scorer is fitted and read under.
const ScorerTieBreak = "unit_id_ascending"

// solve does Gaussian elimination with partial pivoting. It is written out
// rather than pulled in so the module keeps its zero-dependency build.
func solve(a [][]float64, b []float64) ([]float64, error) {
	n := len(b)
	m := make([][]float64, n)
	for i := range a {
		m[i] = append(append([]float64(nil), a[i]...), b[i])
	}
	for col := 0; col < n; col++ {
		pivot := col
		for r := col + 1; r < n; r++ {
			if math.Abs(m[r][col]) > math.Abs(m[pivot][col]) {
				pivot = r
			}
		}
		if math.Abs(m[pivot][col]) < 1e-12 {
			return nil, fmt.Errorf("the design matrix is singular at column %d; add ridge regularisation or drop the collinear feature", col)
		}
		m[col], m[pivot] = m[pivot], m[col]
		for r := 0; r < n; r++ {
			if r == col {
				continue
			}
			f := m[r][col] / m[col][col]
			for c := col; c <= n; c++ {
				m[r][c] -= f * m[col][c]
			}
		}
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = m[i][n] / m[i][i]
	}
	return out, nil
}

// PcheckInvocation is one rendered invocation's audit projection.
type PcheckInvocation struct {
	Seq         int      `json:"invocation_seq"`
	BucketIndex int      `json:"bucket"`
	Units       []string `json:"units"`
	// PredictedNs is sum(Palloc[u]) over the verified immutable membership.
	PredictedNs int64 `json:"predicted_ns"`
	// PallocDigest covers the exact (unit, value) pairs this sum came from,
	// not merely the unit names. Digesting the names alone would let the same
	// membership carry any number at all, which is precisely the post-hoc
	// value the audit exists to exclude.
	PallocDigest Digest `json:"palloc_digest"`
}

// PcheckDocument is the post-render predictor-audit projection. It is emitted
// AFTER rendering and can change nothing: it cannot re-run the partition,
// alter topology, or become a scorer input.
//
// It carries the frozen Palloc values themselves so a verifier can RECOMPUTE
// every projection rather than believe the numbers it was handed, and it names
// the Stage-2 receipt, the rendered membership and the scorer it came from so
// the recomputation is anchored to one authorised plan.
type PcheckDocument struct {
	Kind   string `json:"kind"`
	Stage2 Digest `json:"stage2_digest"`
	// BucketIndex and BucketName say WHICH bucket this projection is for. Two
	// buckets of one plan share a Stage-2 digest, a membership digest and a
	// scorer, so without these a projection for bucket 3 substituted into
	// bucket 1's job recomputes perfectly and predicts the wrong work.
	BucketIndex int    `json:"bucket"`
	BucketName  string `json:"bucket_name"`
	// MembershipDigest must equal the Stage-2 receipt's rendered-membership
	// digest: a projection over some other membership is a projection of some
	// other plan.
	MembershipDigest Digest `json:"rendered_membership_digest"`
	ScorerID         string `json:"scorer_id"`
	// ScorerDigest is the frozen scorer these values came from, which Stage 1
	// binds through the training lineage.
	ScorerDigest Digest `json:"scorer_digest"`
	// Palloc is the frozen per-unit allocation score. PallocDigest covers it.
	Palloc       map[string]float64 `json:"palloc"`
	PallocDigest Digest             `json:"palloc_digest"`
	// Features are the RUNTIME feature vectors each Palloc value was scored
	// from. Without them the projection recomputes only against itself: an
	// internally consistent Palloc map the frozen scorer never produced passes
	// every arithmetic check, and the runtime surface is then a number
	// asserting its own provenance.
	Features    []FeatureVector    `json:"features"`
	Invocations []PcheckInvocation `json:"invocations"`
}

// PallocNs converts a frozen score in seconds to the integer nanoseconds the
// projection and the gates compare in. One conversion, one place: two callers
// rounding differently would make a recomputation disagree with the document
// for a reason that has nothing to do with the plan.
func PallocNs(seconds float64) int64 { return int64(seconds * float64(second)) }

// PallocSubset is the (unit, value) map for one invocation's membership. It is
// what each invocation's PallocDigest covers.
func PallocSubset(palloc map[string]float64, units []string) (map[string]float64, error) {
	out := make(map[string]float64, len(units))
	for _, u := range units {
		v, ok := palloc[u]
		if !ok {
			return nil, fmt.Errorf("unit %q has no frozen Palloc value", u)
		}
		out[u] = v
	}
	return out, nil
}

// Recompute re-derives every projection from the document's own frozen values
// and reports what disagrees. It is the verifier's half of the audit: a
// projection nobody can recompute is a number, not a prediction.
// RecomputeFrom re-derives every Palloc value by running the FROZEN SCORER
// over the document's own feature vectors, and reports what disagrees.
//
// Recompute checks the projection against itself, which catches an edited
// number but not a substituted model: a Palloc map no scorer ever produced is
// perfectly self-consistent. This is the other half — the runtime surface is
// frozen_scorer(frozen_preplan_unit_feature_vector), so verifying it means
// running exactly that.
func (d PcheckDocument) RecomputeFrom(scorer Scorer) []string {
	problems := d.Recompute()
	got, err := scorer.DigestOf()
	if err != nil {
		return append(problems, "the supplied scorer cannot be digested: "+err.Error())
	}
	if got != d.ScorerDigest {
		return append(problems, fmt.Sprintf("the projection claims scorer %s but the supplied frozen scorer is %s", d.ScorerDigest, got))
	}
	if len(d.Features) == 0 {
		return append(problems, "the projection carries no runtime feature vectors, so its values cannot be re-derived from the frozen scorer and are unattributable to it")
	}
	seen := map[string]bool{}
	for _, fv := range d.Features {
		if seen[fv.UnitID] {
			problems = append(problems, fmt.Sprintf("unit %q has two feature vectors, so which one produced its score is undecided", fv.UnitID))
			continue
		}
		seen[fv.UnitID] = true
		want, ok := d.Palloc[fv.UnitID]
		if !ok {
			problems = append(problems, fmt.Sprintf("unit %q has a feature vector but no frozen Palloc value", fv.UnitID))
			continue
		}
		score, err := scorer.Score(fv)
		if err != nil {
			problems = append(problems, fmt.Sprintf("unit %q: %v", fv.UnitID, err))
			continue
		}
		// Compared at the resolution the values are CARRIED at: a float
		// equality here would fail on a value that round-tripped through JSON,
		// which is a serialisation artefact rather than a different score.
		if PallocNs(score) != PallocNs(want) {
			problems = append(problems, fmt.Sprintf("unit %q is frozen at %v but the frozen scorer scores its feature vector at %v", fv.UnitID, want, score))
		}
	}
	for unit := range d.Palloc {
		if !seen[unit] {
			problems = append(problems, fmt.Sprintf("unit %q carries a frozen Palloc value with no feature vector, so nothing says the frozen scorer produced it", unit))
		}
	}
	return problems
}

func (d PcheckDocument) Recompute() []string {
	var problems []string
	if d.Kind != PcheckKind {
		problems = append(problems, fmt.Sprintf("kind is %q, want %q", d.Kind, PcheckKind))
	}
	if len(d.Palloc) == 0 {
		problems = append(problems, "the document carries no frozen Palloc values, so nothing can be recomputed")
		return problems
	}
	if got, err := DigestJSON(d.Palloc); err != nil {
		problems = append(problems, "the frozen Palloc map cannot be digested: "+err.Error())
	} else if got != d.PallocDigest {
		problems = append(problems, fmt.Sprintf("the frozen Palloc map digests to %s but the document claims %s", got, d.PallocDigest))
	}
	for _, inv := range d.Invocations {
		subset, err := PallocSubset(d.Palloc, inv.Units)
		if err != nil {
			problems = append(problems, fmt.Sprintf("invocation %d: %v", inv.Seq, err))
			continue
		}
		var sum float64
		for _, v := range subset {
			sum += v
		}
		if want := PallocNs(sum); want != inv.PredictedNs {
			problems = append(problems, fmt.Sprintf("invocation %d projects %d ns but its membership sums to %d ns", inv.Seq, inv.PredictedNs, want))
		}
		got, err := DigestJSON(subset)
		if err != nil {
			problems = append(problems, fmt.Sprintf("invocation %d: %v", inv.Seq, err))
			continue
		}
		if got != inv.PallocDigest {
			problems = append(problems, fmt.Sprintf("invocation %d's membership values digest to %s but it claims %s", inv.Seq, got, inv.PallocDigest))
		}
	}
	return problems
}

// PcheckKind is the audit projection's schema identity.
const PcheckKind = "tb.walltime.pcheck/v1"

// BuildPcheck projects frozen Palloc values onto the renderer's deterministic
// membership. It takes the values as data — it cannot call the scorer — so
// there is no path by which a projection could re-score a unit.
func BuildPcheck(stage2, membership Digest, scorer Scorer, palloc map[string]float64, features []FeatureVector, invocations []PcheckInvocation, bucketIndex int, bucketName string) (*PcheckDocument, error) {
	scorerDigest, err := scorer.DigestOf()
	if err != nil {
		return nil, err
	}
	pallocDigest, err := DigestJSON(palloc)
	if err != nil {
		return nil, err
	}
	doc := &PcheckDocument{
		Kind: PcheckKind, Stage2: stage2, MembershipDigest: membership,
		BucketIndex: bucketIndex, BucketName: bucketName,
		ScorerID: scorer.ID, ScorerDigest: scorerDigest,
		Palloc: palloc, PallocDigest: pallocDigest,
		Features: features,
	}
	for _, inv := range invocations {
		subset, err := PallocSubset(palloc, inv.Units)
		if err != nil {
			return nil, fmt.Errorf("pcheck: invocation %d: %w", inv.Seq, err)
		}
		var sum float64
		for _, v := range subset {
			sum += v
		}
		inv.PredictedNs = PallocNs(sum)
		d, err := DigestJSON(subset)
		if err != nil {
			return nil, err
		}
		inv.PallocDigest = d
		doc.Invocations = append(doc.Invocations, inv)
	}
	if problems := doc.RecomputeFrom(scorer); len(problems) > 0 {
		// A document this function just built must recompute — including
		// against the frozen scorer it names. If it does not, the bug is here
		// and shipping it would hide it in a later verdict.
		return nil, fmt.Errorf("pcheck: the projection does not recompute: %s", strings.Join(problems, "; "))
	}
	return doc, nil
}

// parseInstant accepts RFC3339 with an explicit zone and refuses anything
// else, so "2026-08-31" cannot silently mean midnight in an unstated zone.
func parseInstant(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("instant %q is not RFC3339 with an explicit zone", strings.TrimSpace(s))
	}
	return t, nil
}
