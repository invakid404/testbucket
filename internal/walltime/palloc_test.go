package walltime

import (
	"math"
	"strings"
	"testing"
)

// A label the training surface accepts: historical, wrapper-qualified, causally
// attributed, topology-validated, before the cutoff.
func admissibleLabel(id string, runnables, atomSize float64, seconds float64) TrainingLabel {
	return TrainingLabel{
		ReceiptID: id, ReceiptHash: Digest("sha256:" + id), UnitID: id,
		ObservedNs: int64(seconds * float64(second)), ObservedAt: "2026-08-01T00:00:00Z",
		Provenance:         LabelProvenance,
		SelectedWorkDigest: Digest("sha256:selected-" + id),
		TopologyReceipt:    Digest("sha256:topology-" + id),
		Features: []Feature{
			{Name: "runnable_count", Value: runnables, Provenance: ProvRunnableSnapshot},
			{Name: "atom_size", Value: atomSize, Provenance: ProvPreplanAtom},
		},
	}
}

func admissibleSet() TrainingReceiptSet {
	return TrainingReceiptSet{
		Kind: TrainingSetKind, Epoch: "vitest-4.1.10", Cutoff: "2026-08-30T00:00:00Z",
		FeatureSchema: []string{"atom_size", "runnable_count"},
		Algorithm:     "ridge-least-squares", Configuration: "lambda=0.01", Seed: 1,
		Labels: []TrainingLabel{
			admissibleLabel("a", 10, 1, 12),
			admissibleLabel("b", 20, 1, 22),
			admissibleLabel("c", 30, 2, 34),
			admissibleLabel("d", 40, 2, 44),
			admissibleLabel("e", 5, 1, 7),
		},
	}
}

// TestTrainingSurfaceAdmissionRules walks every way a label fails to qualify.
// One inadmissible label rejects the whole set: a training set is a lineage
// claim, and there is no "skip the bad rows" that leaves the claim true.
func TestTrainingSurfaceAdmissionRules(t *testing.T) {
	if err := admissibleSet().Validate(); err != nil {
		t.Fatalf("a fully qualified set was rejected: %v", err)
	}
	cases := []struct {
		name   string
		break_ func(*TrainingReceiptSet)
		want   string
	}{
		{"a reporter duration is not a wrapper-qualified label", func(s *TrainingReceiptSet) {
			s.Labels[0].Provenance = "reporter_timing"
		}, "provenance"},
		{"a label observed after the cutoff", func(s *TrainingReceiptSet) {
			s.Labels[1].ObservedAt = "2026-08-31T00:00:00Z"
		}, "cutoff"},
		{"a label with no causal attribution", func(s *TrainingReceiptSet) {
			s.Labels[2].SelectedWorkDigest = ""
		}, "causal"},
		{"a label with no validated topology", func(s *TrainingReceiptSet) {
			s.Labels[3].TopologyReceipt = ""
		}, "topology"},
		{"a permanently excluded anti-overfit example", func(s *TrainingReceiptSet) {
			s.Labels[0].ReceiptID = "PR#4000"
		}, "exclusion"},
		{"an empty set cannot train anything", func(s *TrainingReceiptSet) {
			s.Labels = nil
		}, "empty"},
		{"a feature outside the frozen schema", func(s *TrainingReceiptSet) {
			s.Labels[0].Features = append(s.Labels[0].Features,
				Feature{Name: "observed_seconds", Value: 1, Provenance: "observed_timing"})
		}, "prohibited provenance"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set := admissibleSet()
			tc.break_(&set)
			err := set.Validate()
			if err == nil {
				t.Fatalf("the set was accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestRuntimeSurfaceExcludesOutcomes is the leakage test. The store's own EWMA
// weight is the interesting case: it is the most useful number available at
// plan time and it is built from reporter outcomes, so it is exactly what a
// well-meaning implementation would reach for.
func TestRuntimeSurfaceExcludesOutcomes(t *testing.T) {
	scorer, err := TrainScorer(admissibleSet(), "test-scorer", 0.01)
	if err != nil {
		t.Fatalf("TrainScorer: %v", err)
	}
	good := FeatureVector{UnitID: "u", Features: []Feature{
		{Name: "runnable_count", Value: 15, Provenance: ProvRunnableSnapshot},
		{Name: "atom_size", Value: 1, Provenance: ProvPreplanAtom},
	}}
	if _, err := scorer.Score(good); err != nil {
		t.Fatalf("a pre-plan feature vector was refused: %v", err)
	}
	for _, prov := range []string{"store_ewma", "reporter_timing", "observed_timing", "rendered_membership", "post_plan_topology", "candidate"} {
		bad := FeatureVector{UnitID: "u", Features: []Feature{
			{Name: "runnable_count", Value: 15, Provenance: prov},
			{Name: "atom_size", Value: 1, Provenance: ProvPreplanAtom},
		}}
		if _, err := scorer.Score(bad); err == nil {
			t.Errorf("a runtime feature with provenance %q was accepted", prov)
		}
	}
	// A vector missing a schema feature is refused rather than defaulted: a
	// zero for an unknown input is a guess wearing a number's clothes.
	short := FeatureVector{UnitID: "u", Features: []Feature{
		{Name: "runnable_count", Value: 15, Provenance: ProvRunnableSnapshot},
	}}
	if _, err := scorer.Score(short); err == nil {
		t.Errorf("a vector missing a schema feature was scored")
	}
}

// TestScorerIsDeterministic: the same sealed receipt set must always produce
// the same coefficients and therefore the same scorer digest, or Stage 1
// cannot bind a scorer at all.
func TestScorerIsDeterministic(t *testing.T) {
	a, err := TrainScorer(admissibleSet(), "test-scorer", 0.01)
	if err != nil {
		t.Fatal(err)
	}
	b, err := TrainScorer(admissibleSet(), "test-scorer", 0.01)
	if err != nil {
		t.Fatal(err)
	}
	da, err := a.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	db, err := b.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Errorf("two fits of one receipt set produced different scorers: %s vs %s", da, db)
	}
	if a.Lineage.ScorerDigest != da {
		t.Errorf("the scorer's lineage does not name its own digest")
	}
	// The fit should actually track the labels: this training set is roughly
	// one second per runnable, so a 15-runnable unit should land near 17 s.
	v := FeatureVector{UnitID: "u", Features: []Feature{
		{Name: "runnable_count", Value: 15, Provenance: ProvRunnableSnapshot},
		{Name: "atom_size", Value: 1, Provenance: ProvPreplanAtom},
	}}
	got, err := a.Score(v)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-17) > 2 {
		t.Errorf("Palloc = %.2f s for 15 runnables, want roughly 17 s", got)
	}
}

// TestScorerFloorKeepsUnitsSchedulable: a unit the model scores at zero still
// has to be packed somewhere, and a zero-weight unit would make the partition
// think it is free.
func TestScorerFloorKeepsUnitsSchedulable(t *testing.T) {
	scorer, err := TrainScorer(admissibleSet(), "test-scorer", 0.01)
	if err != nil {
		t.Fatal(err)
	}
	v := FeatureVector{UnitID: "u", Features: []Feature{
		{Name: "runnable_count", Value: -1000, Provenance: ProvRunnableSnapshot},
		{Name: "atom_size", Value: 0, Provenance: ProvPreplanAtom},
	}}
	got, err := scorer.Score(v)
	if err != nil {
		t.Fatal(err)
	}
	if got < scorer.Floor {
		t.Errorf("Palloc = %v, below the %v floor", got, scorer.Floor)
	}
}

// TestPcheckProjectsFrozenValues: the audit projection sums frozen values over
// the renderer's membership. It cannot score, so a unit with no frozen value
// is an error rather than a silent zero.
func TestPcheckProjectsFrozenValues(t *testing.T) {
	scorer, err := TrainScorer(admissibleSet(), "test-scorer", 0.01)
	if err != nil {
		t.Fatal(err)
	}
	palloc := map[string]float64{"u1": 3, "u2": 4.5}
	doc, err := BuildPcheck("sha256:stage2", "sha256:membership", *scorer, palloc, []PcheckInvocation{
		{Seq: 0, BucketIndex: 1, Units: []string{"u1", "u2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Invocations[0].PredictedNs; got != int64(7.5*float64(second)) {
		t.Errorf("Pcheck = %d ns, want 7.5 s", got)
	}
	if _, err := BuildPcheck("sha256:stage2", "sha256:membership", *scorer, palloc, []PcheckInvocation{
		{Seq: 0, Units: []string{"u1", "unknown"}},
	}); err == nil {
		t.Errorf("a unit with no frozen Palloc value was projected as zero")
	}
}

// TestPcheckRecomputes is the audit half: the projection has to be checkable
// from the document's own frozen values, and an edited number must not survive
// that check. Digesting the unit NAMES alone — which is what the projection
// used to do — would let the same membership carry any prediction at all.
func TestPcheckRecomputes(t *testing.T) {
	scorer, err := TrainScorer(admissibleSet(), "test-scorer", 0.01)
	if err != nil {
		t.Fatal(err)
	}
	palloc := map[string]float64{"u1": 3, "u2": 4.5}
	doc, err := BuildPcheck("sha256:stage2", "sha256:membership", *scorer, palloc, []PcheckInvocation{
		{Seq: 0, BucketIndex: 1, Units: []string{"u1", "u2"}},
		{Seq: 1, BucketIndex: 1, Units: []string{"u1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if problems := doc.Recompute(); len(problems) != 0 {
		t.Fatalf("a freshly built projection did not recompute: %v", problems)
	}
	for _, tc := range []struct {
		name string
		edit func(*PcheckDocument)
		want string
	}{
		{"a prediction edited after the fact", func(d *PcheckDocument) {
			d.Invocations[0].PredictedNs = int64(2 * second)
		}, "sums to"},
		{"a frozen value edited after the fact", func(d *PcheckDocument) {
			d.Palloc["u1"] = 99
		}, "digests to"},
		{"membership widened after the fact", func(d *PcheckDocument) {
			d.Invocations[1].Units = []string{"u1", "u2"}
		}, "digest"},
		{"a unit dropped from the frozen values", func(d *PcheckDocument) {
			delete(d.Palloc, "u2")
		}, "no frozen Palloc value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edited, err := BuildPcheck("sha256:stage2", "sha256:membership", *scorer,
				map[string]float64{"u1": 3, "u2": 4.5}, []PcheckInvocation{
					{Seq: 0, BucketIndex: 1, Units: []string{"u1", "u2"}},
					{Seq: 1, BucketIndex: 1, Units: []string{"u1"}},
				})
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(edited)
			problems := edited.Recompute()
			if len(problems) == 0 {
				t.Fatalf("the edit survived recomputation")
			}
			if !strings.Contains(strings.Join(problems, "; "), tc.want) {
				t.Errorf("problems %v do not mention %q", problems, tc.want)
			}
		})
	}
}
