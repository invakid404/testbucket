package walltime

import (
	"crypto/ed25519"
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
	// The exclusion-domain identities, repeated from the signed receipt so the
	// sealed set carries them and validation can compare each against its own
	// exclusion list. They must equal the receipt's.
	CampaignID  string `json:"campaign_id,omitempty"`
	CandidateID string `json:"candidate_id,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	HoldoutID   string `json:"holdout_id,omitempty"`
	// Evidence is the EXACT BYTES the three references above address: the
	// physical-V receipt this duration came from, the selected work it is
	// attributed to, and the receipt that says that topology was validated.
	//
	// Without it the three digests are strings, and sealing the set signs the
	// strings rather than the observations — an offline surface built from
	// labels whose references point at nothing was indistinguishable from one
	// built from real measurements.
	Evidence *LabelEvidence `json:"evidence,omitempty"`
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
	// Exclusions is the UNIVERSAL exclusion list: a label is refused when this
	// names its receipt id OR any of its campaign, candidate, run or holdout
	// identities. It used to be compared against the receipt id alone, which
	// made the campaign/candidate/holdout half of its own documentation
	// unenforceable.
	Exclusions []string `json:"exclusions"`
	// The per-domain exclusion lists, for the common case where a whole
	// campaign, candidate, run or holdout is excluded and naming it in the
	// universal list would be ambiguous about what kind of thing it is.
	ExcludedCampaigns  []string `json:"excluded_campaigns,omitempty"`
	ExcludedCandidates []string `json:"excluded_candidates,omitempty"`
	ExcludedRuns       []string `json:"excluded_runs,omitempty"`
	ExcludedHoldouts   []string `json:"excluded_holdouts,omitempty"`

	FeatureSchema []string        `json:"feature_schema"`
	Labels        []TrainingLabel `json:"labels"`
	Algorithm     string          `json:"algorithm"`
	Configuration string          `json:"configuration"`
	// Lambda is the ridge regularisation the fit uses. It lives in the SEALED
	// SET rather than on the command line that fits it, because it decides the
	// coefficients: a verifier handed the set, the algorithm and the seed but
	// not the lambda could not recompute the scorer, and "recompute it and
	// compare" is the only check that distinguishes a model built from this
	// evidence from one that merely cites it.
	Lambda float64 `json:"ridge_lambda"`
	Seed   int64   `json:"seed"`
	// EvidenceSigners are the PREDECLARED keys allowed to attest a label's
	// physical-V receipt, selected-work document and topology receipt. They
	// are inside the sealed set, so the set's own signature covers WHICH
	// authority may vouch for the observations it is built from.
	EvidenceSigners []string   `json:"evidence_signers"`
	Signature       *Signature `json:"signature,omitempty"`
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

// DigestOf is the receipt set's canonical identity.
func (s TrainingReceiptSet) DigestOf() (Digest, error) {
	c := s
	c.Signature = nil
	return DigestJSON(c)
}

// Validate refuses the whole set if any single label is inadmissible. A
// training set is a lineage claim; one unqualified label makes the claim
// false, so there is no "skip the bad rows" path.
// Seal signs the receipt set as the training authority. It is the only way a
// set becomes admissible, and it is deliberately separate from the campaign
// authority: the offline surface is sealed once, long before any campaign.
func (s *TrainingReceiptSet) Seal(authority string, key ed25519.PrivateKey) error {
	d, err := s.DigestOf()
	if err != nil {
		return err
	}
	s.Signature = &Signature{Authority: authority, KeyID: PublicKeyOf(key), Digest: d, Value: SignApproval(authority, key, d)}
	return nil
}

// Validate takes the PREDECLARED keys allowed to seal a training set. An
// empty set is not "accept any key": a lineage claim nobody can attribute is
// the claim that somebody, somewhere, ran the right procedure — which is the
// thing a sealed offline surface exists to replace.
func (s TrainingReceiptSet) Validate(sealKeys []string) error {
	if s.Kind != TrainingSetKind {
		return fmt.Errorf("training receipt set kind %q, want %q", s.Kind, TrainingSetKind)
	}
	if s.Signature == nil {
		return fmt.Errorf("training receipt set is unsigned: an unattributable lineage cannot seal the offline surface")
	}
	if len(sealKeys) == 0 {
		return fmt.Errorf("no training-authority key was predeclared, so the set's own signature would authenticate it")
	}
	d, err := s.DigestOf()
	if err != nil {
		return err
	}
	if err := VerifySigned(s.Signature, d, sealKeys); err != nil {
		return fmt.Errorf("training receipt set signature: %w", err)
	}
	if s.Cutoff == "" {
		return fmt.Errorf("training receipt set has no observation cutoff")
	}
	// The frozen fit, in full. Every one of these decides the coefficients, so
	// a set that leaves one unbound cannot be recomputed — and an offline
	// surface nobody can recompute is a citation, not a lineage.
	if strings.TrimSpace(s.Epoch) == "" {
		return fmt.Errorf("training receipt set names no epoch")
	}
	if strings.TrimSpace(s.Algorithm) == "" || strings.TrimSpace(s.Configuration) == "" {
		return fmt.Errorf("training receipt set names no training algorithm or configuration")
	}
	if s.Lambda < 0 {
		return fmt.Errorf("training receipt set has ridge lambda %v; it must be >= 0", s.Lambda)
	}
	if len(s.FeatureSchema) == 0 {
		return fmt.Errorf("training receipt set freezes no feature schema")
	}
	cutoff, err2 := parseInstant(s.Cutoff)
	if err2 != nil {
		return err2
	}
	if len(s.Labels) == 0 {
		return fmt.Errorf("training receipt set is empty: no wrapper-qualified historical V label exists yet, so no scorer can be trained")
	}
	// The UNIVERSAL exclusion set, matched against every identity a label
	// carries — not against its receipt id alone.
	excluded := map[string]bool{}
	for _, e := range append(append([]string(nil), PermanentExclusions...), s.Exclusions...) {
		excluded[e] = true
	}
	byDomain := map[string]map[string]bool{
		"campaign":  setOf(s.ExcludedCampaigns),
		"candidate": setOf(s.ExcludedCandidates),
		"run":       setOf(s.ExcludedRuns),
		"holdout":   setOf(s.ExcludedHoldouts),
	}
	if len(s.EvidenceSigners) == 0 {
		return fmt.Errorf("training receipt set predeclares no evidence signer, so each label's receipt, selected-work and topology documents would authenticate themselves")
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
		// EVERY exclusion domain, each against the universal list and its own.
		// A whole excluded campaign used to be admissible because nothing
		// compared anything but the receipt id.
		for _, d := range []struct {
			what, id string
		}{
			{"campaign", l.CampaignID},
			{"candidate", l.CandidateID},
			{"run", l.RunID},
			{"holdout", l.HoldoutID},
		} {
			if d.id == "" {
				continue
			}
			if excluded[d.id] || byDomain[d.what][d.id] {
				return fmt.Errorf("label %s belongs to excluded %s %q; candidate, campaign, current-run and holdout evidence may never train the allocation surface", l.ReceiptID, d.what, d.id)
			}
		}
		fv := FeatureVector{UnitID: l.UnitID, Features: l.Features}
		if err := fv.Validate(s.FeatureSchema); err != nil {
			return fmt.Errorf("label %s: %w", l.ReceiptID, err)
		}
		// The references are LOADED and checked, not merely non-empty. A
		// training set is a lineage claim; a label whose receipt, attribution
		// or topology cannot be produced makes the claim unverifiable, and
		// there is no "skip the unverifiable rows" that leaves it true.
		if problems := l.VerifyEvidence(s.EvidenceSigners); len(problems) > 0 {
			return fmt.Errorf("training receipt set: %s", strings.Join(problems, "; "))
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
func TrainScorer(set TrainingReceiptSet, id string, sealKeys []string) (*Scorer, error) {
	if err := set.Validate(sealKeys); err != nil {
		return nil, err
	}
	lambda := set.Lambda
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
			Seed: set.Seed, TieBreak: ScorerTieBreak,
		},
	}
	d, err := sc.DigestOf()
	if err != nil {
		return nil, err
	}
	sc.Lineage.ScorerDigest = d
	return sc, nil
}

// ScorerTieBreak is the frozen ordering the scorer is fitted and read under.
const ScorerTieBreak = "unit_id_ascending"

// VerifyTrainingSurface is the INDEPENDENT check of the offline surface.
//
// Everything the offline constructor proves — the signature against a
// predeclared training authority, the cutoff, the exclusions, the causal
// selected-work attribution, the topology receipts, uniqueness, the feature
// schema — was proved at construction time and then thrown away. What reached
// Stage 1 and verification was a scorer carrying a receipt-set DIGEST, and the
// only thing checked about it was that the string was not empty. An
// independently written scorer could therefore claim any digest at all, get a
// signed Stage-1 binding, and allocate the whole campaign without anyone ever
// reading the set it named — which is the "signed Palloc training receipt set
// not independently verified" defect.
//
// This closes it by doing the construction again from the exact sealed bytes:
// validate the set under the predeclared authority, recompute its digest,
// refit the scorer deterministically, and require every lineage field and
// every coefficient to be the one that fit produces. A model that cites this
// evidence and a model built from it are then distinguishable.
//
// It returns every problem rather than the first: a lineage that disagrees in
// four places should say so once.
func VerifyTrainingSurface(set TrainingReceiptSet, lineage TrainingLineageID, sc Scorer, sealKeys []string) []string {
	var problems []string
	if err := set.Validate(sealKeys); err != nil {
		return []string{err.Error()}
	}
	digest, err := set.DigestOf()
	if err != nil {
		return []string{err.Error()}
	}
	if lineage.ReceiptSetDigest != digest {
		problems = append(problems, fmt.Sprintf(
			"Stage 1 binds training receipt set %s but the supplied sealed set digests to %s", lineage.ReceiptSetDigest, digest))
	}
	if sc.Lineage.ReceiptSetDigest != digest {
		problems = append(problems, fmt.Sprintf(
			"the frozen scorer claims training receipt set %s but the supplied sealed set digests to %s", sc.Lineage.ReceiptSetDigest, digest))
	}
	// Every lineage field is taken from the SET, never from the claim. A
	// scorer that named the right digest and the wrong cutoff would otherwise
	// be admitting labels the authority excluded.
	for _, f := range []struct {
		name        string
		claim, want string
	}{
		{"cutoff instant", lineage.Cutoff, set.Cutoff},
		{"epoch", lineage.Epoch, set.Epoch},
		{"algorithm", lineage.Algorithm, set.Algorithm},
		{"configuration", lineage.Configuration, set.Configuration},
		{"seed", fmt.Sprint(lineage.Seed), fmt.Sprint(set.Seed)},
		{"tie-break", lineage.TieBreak, ScorerTieBreak},
	} {
		if f.claim != f.want {
			problems = append(problems, fmt.Sprintf(
				"the bound training lineage names %s %q, but the sealed set fixes %q", f.name, f.claim, f.want))
		}
	}
	if strings.TrimSpace(lineage.ScorerID) == "" {
		problems = append(problems, "the bound training lineage names no scorer id, so the fit cannot be reproduced")
		return problems
	}
	// THE RECOMPUTATION. Same sealed labels, same frozen configuration, same
	// deterministic closed-form fit: the coefficients are a function of the
	// evidence, so a scorer that did not come from this set cannot survive it.
	refit, err := TrainScorer(set, lineage.ScorerID, sealKeys)
	if err != nil {
		return append(problems, "the sealed training set does not refit: "+err.Error())
	}
	refitDigest, err := refit.DigestOf()
	if err != nil {
		return append(problems, err.Error())
	}
	supplied, err := sc.DigestOf()
	if err != nil {
		return append(problems, err.Error())
	}
	if supplied != refitDigest {
		problems = append(problems, fmt.Sprintf(
			"the frozen scorer digests to %s, but refitting the sealed training set produces %s; its coefficients did not come from the set it names",
			supplied, refitDigest))
	}
	if lineage.ScorerDigest != refitDigest {
		problems = append(problems, fmt.Sprintf(
			"Stage 1 binds scorer %s, but refitting the sealed training set produces %s", lineage.ScorerDigest, refitDigest))
	}
	// Named comparisons in addition to the digest. The digest catches
	// everything; these say WHAT differs, which is what a reader needs.
	if !equalStrings(sc.FeatureSchema, refit.FeatureSchema) {
		problems = append(problems, fmt.Sprintf(
			"the frozen scorer reads feature schema %v; the sealed set fixes %v", sc.FeatureSchema, refit.FeatureSchema))
	}
	for _, name := range refit.FeatureSchema {
		if sc.Coefficients[name] != refit.Coefficients[name] {
			problems = append(problems, fmt.Sprintf(
				"the frozen scorer weights %q at %v; refitting the sealed set gives %v", name, sc.Coefficients[name], refit.Coefficients[name]))
		}
	}
	if sc.Intercept != refit.Intercept {
		problems = append(problems, fmt.Sprintf(
			"the frozen scorer's intercept is %v; refitting the sealed set gives %v", sc.Intercept, refit.Intercept))
	}
	if sc.Floor != refit.Floor {
		problems = append(problems, fmt.Sprintf(
			"the frozen scorer's floor is %v; the fit fixes %v", sc.Floor, refit.Floor))
	}
	return problems
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
