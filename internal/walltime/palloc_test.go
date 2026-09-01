package walltime

import (
	"math"
	"sort"
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

// trainingSealKey is the fixture's predeclared training authority: the offline
// surface is sealed by a party, and a set nobody sealed is not admissible.
var trainingSealKey = mustSigningKey()

// sealedSet is admissibleSet, signed by that authority — the shape every
// caller of Validate/TrainScorer must now supply.
func sealedSet() TrainingReceiptSet {
	s := admissibleSet()
	if err := s.Seal("ewj2-training", trainingSealKey); err != nil {
		panic(err)
	}
	return s
}

// sealKeys is the predeclared set the fixtures validate against.
func sealKeys() []string { return []string{PublicKeyOf(trainingSealKey)} }

// scoredPalloc builds the feature vectors whose scores ARE the wanted values,
// so a Pcheck fixture cannot claim a number the frozen scorer never produced —
// which is the substitution the projection is now re-derived to catch.
func scoredPalloc(sc Scorer, want map[string]float64) (map[string]float64, []FeatureVector) {
	// Solve along the schema feature with the largest coefficient and zero the
	// rest: any vector that scores to the wanted value will do, and this one
	// exists for every scorer with a non-zero slope.
	free, coef := "", 0.0
	for _, name := range sc.FeatureSchema {
		if c := sc.Coefficients[name]; math.Abs(c) > math.Abs(coef) {
			free, coef = name, c
		}
	}
	if free == "" || coef == 0 {
		panic("scoredPalloc: the scorer has no non-zero coefficient to solve along")
	}
	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := map[string]float64{}
	var vectors []FeatureVector
	for _, id := range ids {
		fv := FeatureVector{UnitID: id}
		for _, name := range sc.FeatureSchema {
			value := 0.0
			if name == free {
				value = (want[id] - sc.Intercept) / coef
			}
			fv.Features = append(fv.Features, Feature{Name: name, Value: value, Provenance: ProvRunnableSnapshot})
		}
		v, err := sc.Score(fv)
		if err != nil {
			panic(err)
		}
		out[id], vectors = v, append(vectors, fv)
	}
	return out, vectors
}

func admissibleSet() TrainingReceiptSet {
	return TrainingReceiptSet{
		Kind: TrainingSetKind, Epoch: "vitest-4.1.10", Cutoff: "2026-08-30T00:00:00Z",
		FeatureSchema: []string{"atom_size", "runnable_count"},
		Algorithm:     "ridge-least-squares", Configuration: "lambda=0.01", Lambda: 0.01, Seed: 1,
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
	if err := sealedSet().Validate(sealKeys()); err != nil {
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
			// Sealed AFTER the mutation: the point of each case is that a
			// properly attributed set is still refused on its content, not
			// that an unsigned one is refused on its signature.
			if err := set.Seal("ewj2-training", trainingSealKey); err != nil {
				t.Fatal(err)
			}
			err := set.Validate(sealKeys())
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
	scorer, err := TrainScorer(sealedSet(), "test-scorer", sealKeys())
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
	a, err := TrainScorer(sealedSet(), "test-scorer", sealKeys())
	if err != nil {
		t.Fatal(err)
	}
	b, err := TrainScorer(sealedSet(), "test-scorer", sealKeys())
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
	scorer, err := TrainScorer(sealedSet(), "test-scorer", sealKeys())
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
	scorer, err := TrainScorer(sealedSet(), "test-scorer", sealKeys())
	if err != nil {
		t.Fatal(err)
	}
	palloc, features := scoredPalloc(*scorer, map[string]float64{"u1": 3, "u2": 4.5})
	doc, err := BuildPcheck("sha256:stage2", "sha256:membership", *scorer, palloc, features, []PcheckInvocation{
		{Seq: 0, BucketIndex: 1, Units: []string{"u1", "u2"}},
	}, 1, "bucket-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Invocations[0].PredictedNs; got != int64(7.5*float64(second)) {
		t.Errorf("Pcheck = %d ns, want 7.5 s", got)
	}
	if _, err := BuildPcheck("sha256:stage2", "sha256:membership", *scorer, palloc, features, []PcheckInvocation{
		{Seq: 0, Units: []string{"u1", "unknown"}},
	}, 1, "bucket-1"); err == nil {
		t.Errorf("a unit with no frozen Palloc value was projected as zero")
	}
}

// TestPcheckRecomputes is the audit half: the projection has to be checkable
// from the document's own frozen values, and an edited number must not survive
// that check. Digesting the unit NAMES alone — which is what the projection
// used to do — would let the same membership carry any prediction at all.
func TestPcheckRecomputes(t *testing.T) {
	scorer, err := TrainScorer(sealedSet(), "test-scorer", sealKeys())
	if err != nil {
		t.Fatal(err)
	}
	palloc, features := scoredPalloc(*scorer, map[string]float64{"u1": 3, "u2": 4.5})
	doc, err := BuildPcheck("sha256:stage2", "sha256:membership", *scorer, palloc, features, []PcheckInvocation{
		{Seq: 0, BucketIndex: 1, Units: []string{"u1", "u2"}},
		{Seq: 1, BucketIndex: 1, Units: []string{"u1"}},
	}, 1, "bucket-1")
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
			base, baseFeatures := scoredPalloc(*scorer, map[string]float64{"u1": 3, "u2": 4.5})
			edited, err := BuildPcheck("sha256:stage2", "sha256:membership", *scorer,
				base, baseFeatures, []PcheckInvocation{
					{Seq: 0, BucketIndex: 1, Units: []string{"u1", "u2"}},
					{Seq: 1, BucketIndex: 1, Units: []string{"u1"}},
				}, 1, "bucket-1")
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

// The offline surface is SEALED, which means a party sealed it. A receipt set
// carrying its own signature and nothing else proves only that whoever wrote
// the labels also signed them — and the lineage a scorer claims is then the
// claim that somebody, somewhere, followed the rules.
func TestTrainingSetMustBeAttributable(t *testing.T) {
	other, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		set  func() TrainingReceiptSet
		keys []string
		want string
	}{
		{
			name: "an unsigned set",
			set:  admissibleSet,
			keys: sealKeys(),
			want: "unsigned",
		},
		{
			name: "a set sealed by a key nobody declared",
			set: func() TrainingReceiptSet {
				s := admissibleSet()
				if err := s.Seal("ewj2-training", other); err != nil {
					t.Fatal(err)
				}
				return s
			},
			keys: sealKeys(),
			want: "signature",
		},
		{
			name: "a correctly sealed set with no predeclared authority",
			set:  sealedSet,
			keys: nil,
			want: "no training-authority key was predeclared",
		},
		{
			name: "labels edited after sealing",
			set: func() TrainingReceiptSet {
				s := sealedSet()
				s.Labels[0].ObservedNs *= 2
				return s
			},
			keys: sealKeys(),
			want: "signature",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := tc.set()
			if err := set.Validate(tc.keys); err == nil {
				t.Fatalf("an unattributable training set was accepted")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
			if _, err := TrainScorer(set, "test-scorer", tc.keys); err == nil {
				t.Errorf("a scorer was trained from an unattributable set")
			}
		})
	}
}

// The runtime surface is defined as frozen_scorer(frozen_preplan_feature_
// vector). Checking a projection's own arithmetic proves it adds up; it does
// not prove any model produced the numbers being added, because a substituted
// allocation map is perfectly self-consistent.
func TestPcheckIsReDerivedFromTheFrozenScorer(t *testing.T) {
	scorer, err := TrainScorer(sealedSet(), "test-scorer", sealKeys())
	if err != nil {
		t.Fatal(err)
	}
	palloc, features := scoredPalloc(*scorer, map[string]float64{"u1": 3, "u2": 4.5})
	// Copies, because BuildPcheck keeps what it is handed: an edit in one case
	// would otherwise be visible to the next.
	build := func() *PcheckDocument {
		p := map[string]float64{}
		for k, v := range palloc {
			p[k] = v
		}
		fs := make([]FeatureVector, 0, len(features))
		for _, fv := range features {
			c := fv
			c.Features = append([]Feature(nil), fv.Features...)
			fs = append(fs, c)
		}
		doc, err := BuildPcheck("sha256:stage2", "sha256:membership", *scorer, p, fs,
			[]PcheckInvocation{{Seq: 0, BucketIndex: 1, Units: []string{"u1", "u2"}}}, 1, "bucket-1")
		if err != nil {
			t.Fatal(err)
		}
		return doc
	}
	if problems := build().RecomputeFrom(*scorer); len(problems) != 0 {
		t.Fatalf("a freshly built projection did not re-derive: %v", problems)
	}

	for _, tc := range []struct {
		name string
		edit func(*PcheckDocument)
		want string
	}{
		{
			// The substitution the old check could not see: every number in
			// the document agrees with every other, and no scorer produced any
			// of them.
			name: "an internally consistent map the scorer never produced",
			edit: func(d *PcheckDocument) {
				d.Palloc["u1"] = 9
				d.PallocDigest = DigestJSONOrEmpty(d.Palloc)
				for i := range d.Invocations {
					subset, err := PallocSubset(d.Palloc, d.Invocations[i].Units)
					if err != nil {
						t.Fatal(err)
					}
					var sum float64
					for _, v := range subset {
						sum += v
					}
					d.Invocations[i].PredictedNs = PallocNs(sum)
					d.Invocations[i].PallocDigest = DigestJSONOrEmpty(subset)
				}
			},
			want: "the frozen scorer scores its feature vector at",
		},
		{
			name: "no feature vectors at all",
			edit: func(d *PcheckDocument) { d.Features = nil },
			want: "carries no runtime feature vectors",
		},
		{
			name: "a unit whose value has no vector behind it",
			edit: func(d *PcheckDocument) { d.Features = d.Features[:1] },
			want: "with no feature vector",
		},
		{
			name: "a feature vector edited to justify a different value",
			edit: func(d *PcheckDocument) { d.Features[0].Features[0].Value *= 3 },
			want: "the frozen scorer scores its feature vector at",
		},
		{
			name: "two vectors for one unit",
			edit: func(d *PcheckDocument) { d.Features = append(d.Features, d.Features[0]) },
			want: "has two feature vectors",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := build()
			tc.edit(doc)
			problems := doc.RecomputeFrom(*scorer)
			if len(problems) == 0 {
				t.Fatalf("the projection re-derived")
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Errorf("no problem mentions %q:\n%s", tc.want, strings.Join(problems, "\n"))
			}
		})
	}

	// A different model is a different projection, even when the numbers happen
	// to be self-consistent.
	otherSet := admissibleSet()
	otherSet.Lambda, otherSet.Configuration = 0.5, "lambda=0.5"
	if err := otherSet.Seal("ewj2-training", trainingSealKey); err != nil {
		t.Fatal(err)
	}
	other, err := TrainScorer(otherSet, "other-scorer", sealKeys())
	if err != nil {
		t.Fatal(err)
	}
	problems := build().RecomputeFrom(*other)
	if len(problems) == 0 {
		t.Errorf("a projection re-derived against a scorer that did not produce it")
	}
}
