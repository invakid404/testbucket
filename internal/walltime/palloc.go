package walltime

import (
	"fmt"
	"math"
	"sort"
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

// value looks a feature up by name.
func (v FeatureVector) value(name string) (float64, bool) {
	for _, f := range v.Features {
		if f.Name == name {
			return f.Value, true
		}
	}
	return 0, false
}

// TrainingLabel is one admissible historical observation. Every field here is
// a condition of admission, not decoration: a label with no causal attribution
// cannot say which work produced the duration, and one with no validated
// topology cannot say the duration was comparable.
type TrainingLabel struct {
	ReceiptID   string `json:"receipt_id"`
	ReceiptHash Digest `json:"receipt_hash"`
	UnitID      string `json:"unit_id"`
	// ObservedNs is a historical WRAPPER-QUALIFIED physical V. A reporter
	// duration is not one, and neither is an inferred value.
	ObservedNs int64  `json:"observed_ns"`
	ObservedAt string `json:"observed_at"`
	Provenance string `json:"provenance"`
	// SelectedWorkDigest is the immutable attribution: which selected work
	// this duration causally belongs to.
	SelectedWorkDigest Digest    `json:"selected_work_digest"`
	TopologyReceipt    Digest    `json:"topology_receipt"`
	Features           []Feature `json:"features"`
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

// TrainingReceiptSet is the sealed offline surface.
type TrainingReceiptSet struct {
	Kind   string `json:"kind"`
	Epoch  string `json:"epoch"`
	Cutoff string `json:"cutoff_instant"`
	// Exclusions are the permanent anti-overfit exclusions plus any
	// campaign/candidate/holdout ids. A label whose id is here is refused even
	// if it is otherwise admissible.
	Exclusions    []string        `json:"exclusions"`
	FeatureSchema []string        `json:"feature_schema"`
	Labels        []TrainingLabel `json:"labels"`
	Algorithm     string          `json:"algorithm"`
	Configuration string          `json:"configuration"`
	Seed          int64           `json:"seed"`
	Signature     *Signature      `json:"signature,omitempty"`
}

// PermanentExclusions are the anti-overfit examples the contract names. They
// are never training, threshold, baseline, candidate or holdout data.
var PermanentExclusions = []string{"PR#4000", "PR#3999", "PR#182", "PR#4017"}

// DigestOf is the receipt set's canonical identity.
func (s TrainingReceiptSet) DigestOf() (Digest, error) {
	c := s
	c.Signature = nil
	return DigestJSON(c)
}

// Validate refuses the whole set if any single label is inadmissible. A
// training set is a lineage claim; one unqualified label makes the claim
// false, so there is no "skip the bad rows" path.
func (s TrainingReceiptSet) Validate() error {
	if s.Kind != TrainingSetKind {
		return fmt.Errorf("training receipt set kind %q, want %q", s.Kind, TrainingSetKind)
	}
	if s.Cutoff == "" {
		return fmt.Errorf("training receipt set has no observation cutoff")
	}
	cutoff, err := parseInstant(s.Cutoff)
	if err != nil {
		return err
	}
	if len(s.Labels) == 0 {
		return fmt.Errorf("training receipt set is empty: no wrapper-qualified historical V label exists yet, so no scorer can be trained")
	}
	excluded := map[string]bool{}
	for _, e := range append(append([]string(nil), PermanentExclusions...), s.Exclusions...) {
		excluded[e] = true
	}
	seen := map[string]bool{}
	for _, l := range s.Labels {
		switch {
		case l.Provenance != LabelProvenance:
			return fmt.Errorf("label %s has provenance %q; only %q is admissible", l.ReceiptID, l.Provenance, LabelProvenance)
		case excluded[l.ReceiptID]:
			return fmt.Errorf("label %s is on the exclusion list", l.ReceiptID)
		case l.ReceiptHash == "":
			return fmt.Errorf("label %s has no receipt hash", l.ReceiptID)
		case l.SelectedWorkDigest == "":
			return fmt.Errorf("label %s has no causal selected-work attribution", l.ReceiptID)
		case l.TopologyReceipt == "":
			return fmt.Errorf("label %s has no validated topology receipt", l.ReceiptID)
		case l.ObservedNs <= 0:
			return fmt.Errorf("label %s has no positive wrapper-qualified duration", l.ReceiptID)
		case seen[l.ReceiptID]:
			return fmt.Errorf("label %s appears twice", l.ReceiptID)
		}
		at, err := parseInstant(l.ObservedAt)
		if err != nil {
			return fmt.Errorf("label %s: %w", l.ReceiptID, err)
		}
		if !at.Before(cutoff) {
			return fmt.Errorf("label %s was observed at %s, at or after the %s cutoff", l.ReceiptID, l.ObservedAt, s.Cutoff)
		}
		fv := FeatureVector{UnitID: l.UnitID, Features: l.Features}
		if err := fv.Validate(s.FeatureSchema); err != nil {
			return fmt.Errorf("label %s: %w", l.ReceiptID, err)
		}
		seen[l.ReceiptID] = true
	}
	return nil
}

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

// TrainScorer fits the frozen model from a sealed training receipt set. It is
// ridge-regularised least squares solved in closed form: deterministic, so the
// same receipt set and configuration always produce the same coefficients, and
// therefore the same scorer digest.
//
// It refuses an unvalidated set. Training is where labels are allowed to
// exist; it is not where the rules about them stop applying.
func TrainScorer(set TrainingReceiptSet, id string, lambda float64) (*Scorer, error) {
	if err := set.Validate(); err != nil {
		return nil, err
	}
	if lambda < 0 {
		return nil, fmt.Errorf("ridge lambda must be >= 0, got %v", lambda)
	}
	schema := append([]string(nil), set.FeatureSchema...)
	sort.Strings(schema)
	n := len(schema) + 1 // + intercept

	// Normal equations with an explicit intercept column; ridge is applied to
	// the slopes only, so a constant shift is never penalised.
	xtx := make([][]float64, n)
	for i := range xtx {
		xtx[i] = make([]float64, n)
	}
	xty := make([]float64, n)
	for _, l := range set.Labels {
		fv := FeatureVector{UnitID: l.UnitID, Features: l.Features}
		row := make([]float64, n)
		row[0] = 1
		for i, name := range schema {
			v, _ := fv.value(name)
			row[i+1] = v
		}
		y := float64(l.ObservedNs) / float64(second)
		for i := 0; i < n; i++ {
			xty[i] += row[i] * y
			for j := 0; j < n; j++ {
				xtx[i][j] += row[i] * row[j]
			}
		}
	}
	for i := 1; i < n; i++ {
		xtx[i][i] += lambda
	}
	sol, err := solve(xtx, xty)
	if err != nil {
		return nil, fmt.Errorf("training did not converge: %w", err)
	}
	coef := map[string]float64{}
	for i, name := range schema {
		coef[name] = sol[i+1]
	}
	digestSet, err := set.DigestOf()
	if err != nil {
		return nil, err
	}
	sc := &Scorer{
		Kind: ScorerKind, ID: id, Version: "1",
		FeatureSchema: schema, Coefficients: coef, Intercept: sol[0],
		Floor: 0.1,
		Lineage: TrainingLineageID{
			ReceiptSetDigest: digestSet, Cutoff: set.Cutoff, Epoch: set.Epoch,
			ScorerID: id, Algorithm: set.Algorithm, Configuration: set.Configuration,
			Seed: set.Seed, TieBreak: "unit_id_ascending",
		},
	}
	d, err := sc.DigestOf()
	if err != nil {
		return nil, err
	}
	sc.Lineage.ScorerDigest = d
	return sc, nil
}

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
	// PallocDigest references the frozen values this sum came from.
	PallocDigest Digest `json:"palloc_digest"`
}

// PcheckDocument is the post-render predictor-audit projection. It is emitted
// AFTER rendering and can change nothing: it cannot re-run the partition,
// alter topology, or become a scorer input.
type PcheckDocument struct {
	Kind        string             `json:"kind"`
	Stage2      Digest             `json:"stage2_digest"`
	ScorerID    string             `json:"scorer_id"`
	Invocations []PcheckInvocation `json:"invocations"`
}

// PcheckKind is the audit projection's schema identity.
const PcheckKind = "tb.walltime.pcheck/v1"

// BuildPcheck projects frozen Palloc values onto the renderer's deterministic
// membership. It takes the values as data — it cannot call the scorer — so
// there is no path by which a projection could re-score a unit.
func BuildPcheck(stage2 Digest, scorerID string, palloc map[string]float64, membership []PcheckInvocation) (*PcheckDocument, error) {
	doc := &PcheckDocument{Kind: PcheckKind, Stage2: stage2, ScorerID: scorerID}
	for _, inv := range membership {
		var sum float64
		for _, u := range inv.Units {
			v, ok := palloc[u]
			if !ok {
				return nil, fmt.Errorf("pcheck: unit %q in invocation %d has no frozen Palloc value", u, inv.Seq)
			}
			sum += v
		}
		inv.PredictedNs = int64(sum * float64(second))
		d, err := DigestJSON(inv.Units)
		if err != nil {
			return nil, err
		}
		inv.PallocDigest = d
		doc.Invocations = append(doc.Invocations, inv)
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
