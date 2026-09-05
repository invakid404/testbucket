package walltime

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestFabricatedTrainingReferencesAreRefused is the F2 regression, and it is
// the validator's own control written in-tree.
//
// Every one of these three digests is a syntactically valid SHA-256 that
// refers to nothing. The set is sealed by the predeclared training authority,
// so its signature is genuine — it signs the STRINGS. Before this the
// constructor accepted it, `TrainScorer` fitted a model from it, and
// `VerifyTrainingSurface` reported no problem at all: refit proves the
// coefficients follow the signed rows, never that the rows are wrapper-
// qualified, causal, topologically valid observations.
func TestFabricatedTrainingReferencesAreRefused(t *testing.T) {
	set := admissibleSet()
	for i := range set.Labels {
		set.Labels[i].ReceiptHash = DigestBytes([]byte("other-1"))
		set.Labels[i].SelectedWorkDigest = DigestBytes([]byte("other-2"))
		set.Labels[i].TopologyReceipt = DigestBytes([]byte("other-3"))
		set.Labels[i].Evidence = nil
	}
	if err := set.Seal("ewj2-training", trainingSealKey); err != nil {
		t.Fatal(err)
	}
	if err := set.Validate(sealKeys()); err == nil {
		t.Fatal("a set whose references point at nothing was admitted")
	}
	if _, err := TrainScorer(set, "fabricated-lineage", sealKeys()); err == nil {
		t.Fatal("a scorer was fitted from labels whose evidence does not exist")
	}
}

// TestASetWithNoEvidenceSignerIsRefused: evidence verified against whatever
// signed it is evidence anybody can mint, so the sealed set has to say which
// authority may vouch for the observations it is built from.
func TestASetWithNoEvidenceSignerIsRefused(t *testing.T) {
	set := admissibleSet()
	set.EvidenceSigners = nil
	if err := set.Seal("ewj2-training", trainingSealKey); err != nil {
		t.Fatal(err)
	}
	err := set.Validate(sealKeys())
	if err == nil {
		t.Fatal("a set that predeclares no evidence signer was admitted")
	}
	if !strings.Contains(err.Error(), "evidence signer") {
		t.Errorf("error %q does not name the missing evidence signer", err)
	}
}

// TestEveryClauseOfTheLabelEvidenceChain walks the chain one link at a time.
//
// Each case starts from a label the verifier ACCEPTS and breaks exactly one
// thing, so what it proves is that clause and not the general fact that
// something is wrong. The mutations are applied to the evidence BYTES and the
// references are re-pointed at them where the case is about content rather
// than about the reference, because a case that only corrupted a digest would
// test the hash comparison over and over.
func TestEveryClauseOfTheLabelEvidenceChain(t *testing.T) {
	// reseal rewrites one evidence document, re-signs it, and re-points the
	// label at the new bytes.
	resealReceipt := func(l *TrainingLabel, edit func(*PhysicalVReceipt)) {
		var r PhysicalVReceipt
		if err := json.Unmarshal(l.Evidence.ReceiptBytes, &r); err != nil {
			t.Fatal(err)
		}
		r.Signature = nil
		edit(&r)
		raw, digest := signEvidence(&r, func(d *PhysicalVReceipt, dg Digest) { d.Signature = sig(dg) })
		l.Evidence.ReceiptBytes, l.ReceiptHash = raw, digest
	}
	resealWork := func(l *TrainingLabel, edit func(*SelectedWorkDocument)) {
		var w SelectedWorkDocument
		if err := json.Unmarshal(l.Evidence.SelectedWorkBytes, &w); err != nil {
			t.Fatal(err)
		}
		w.Signature = nil
		edit(&w)
		raw, digest := signEvidence(&w, func(d *SelectedWorkDocument, dg Digest) { d.Signature = sig(dg) })
		l.Evidence.SelectedWorkBytes, l.SelectedWorkDigest = raw, digest
	}
	resealTopology := func(l *TrainingLabel, edit func(*TopologyValidationReceipt)) {
		var tp TopologyValidationReceipt
		if err := json.Unmarshal(l.Evidence.TopologyBytes, &tp); err != nil {
			t.Fatal(err)
		}
		tp.Signature = nil
		edit(&tp)
		raw, digest := signEvidence(&tp, func(d *TopologyValidationReceipt, dg Digest) { d.Signature = sig(dg) })
		l.Evidence.TopologyBytes, l.TopologyReceipt = raw, digest
	}

	cases := []struct {
		name string
		edit func(*TrainingLabel)
		want string
	}{
		{"no evidence at all", func(l *TrainingLabel) { l.Evidence = nil }, "carries no evidence"},
		{"an absent receipt", func(l *TrainingLabel) { l.Evidence.ReceiptBytes = nil }, "is absent"},
		{"a receipt hash that addresses other bytes", func(l *TrainingLabel) {
			l.ReceiptHash = DigestBytes([]byte("other-4"))
		}, "digests to"},
		{"an unsigned receipt", func(l *TrainingLabel) {
			var r PhysicalVReceipt
			if err := json.Unmarshal(l.Evidence.ReceiptBytes, &r); err != nil {
				t.Fatal(err)
			}
			r.Signature = nil
			raw, err := json.Marshal(&r)
			if err != nil {
				t.Fatal(err)
			}
			l.Evidence.ReceiptBytes, l.ReceiptHash = raw, DigestBytes(raw)
		}, "signature"},
		{"a receipt signed by an undeclared key", func(l *TrainingLabel) {
			var r PhysicalVReceipt
			if err := json.Unmarshal(l.Evidence.ReceiptBytes, &r); err != nil {
				t.Fatal(err)
			}
			r.Signature = nil
			d, err := r.DigestOf()
			if err != nil {
				t.Fatal(err)
			}
			other := mustSigningKey()
			r.Signature = &Signature{Authority: "ewj2-observation", KeyID: PublicKeyOf(other), Digest: d, Value: SignApproval("ewj2-observation", other, d)}
			raw, err := json.Marshal(&r)
			if err != nil {
				t.Fatal(err)
			}
			l.Evidence.ReceiptBytes, l.ReceiptHash = raw, DigestBytes(raw)
		}, "signature"},
		{"a peer record standing in for a physical V", func(l *TrainingLabel) {
			resealReceipt(l, func(r *PhysicalVReceipt) { r.Producer = ProducerPeer })
		}, "not the physical wrapper"},
		{"an action-level receipt", func(l *TrainingLabel) {
			resealReceipt(l, func(r *PhysicalVReceipt) { r.Level = LevelAction })
		}, "not an invocation"},
		{"a wrapper annotation as the delimiter", func(l *TrainingLabel) {
			resealReceipt(l, func(r *PhysicalVReceipt) { r.Source = SourceWrapper })
		}, "may delimit a lifecycle"},
		{"an unscorable containment", func(l *TrainingLabel) {
			resealReceipt(l, func(r *PhysicalVReceipt) { r.Containment.Primitive = PrimitiveProcessGroup })
		}, "no descendant escaped"},
		{"a failed observation", func(l *TrainingLabel) {
			resealReceipt(l, func(r *PhysicalVReceipt) { r.Terminal = TerminalFailed })
		}, "never trained on"},
		{"a receipt for another unit", func(l *TrainingLabel) {
			resealReceipt(l, func(r *PhysicalVReceipt) { r.UnitID = "somebody-else" })
		}, "measures unit"},
		{"a duration the receipt did not observe", func(l *TrainingLabel) {
			resealReceipt(l, func(r *PhysicalVReceipt) { r.DurationNs += int64(second) })
		}, "but its receipt observed"},
		{"an instant the receipt did not observe", func(l *TrainingLabel) {
			resealReceipt(l, func(r *PhysicalVReceipt) { r.ObservedAt = "2026-08-02T00:00:00Z" })
		}, "but its receipt observed"},
		{"an attribution the receipt does not make", func(l *TrainingLabel) {
			resealReceipt(l, func(r *PhysicalVReceipt) {
				r.SelectedWorkDigest = DigestBytes([]byte("other-5"))
			})
		}, "its own receipt attributes it to"},
		{"selected work for another unit", func(l *TrainingLabel) {
			resealWork(l, func(w *SelectedWorkDocument) { w.UnitID = "somebody-else" })
			resealReceipt(l, func(r *PhysicalVReceipt) { r.SelectedWorkDigest = l.SelectedWorkDigest })
		}, "selected-work document is for unit"},
		{"selected work that selects nothing", func(l *TrainingLabel) {
			resealWork(l, func(w *SelectedWorkDocument) { w.Units = nil })
			resealReceipt(l, func(r *PhysicalVReceipt) { r.SelectedWorkDigest = l.SelectedWorkDigest })
		}, "attributed to no work"},
		{"a topology that was never validated", func(l *TrainingLabel) {
			resealTopology(l, func(tp *TopologyValidationReceipt) { tp.Validated = false })
			resealReceipt(l, func(r *PhysicalVReceipt) { r.TopologyReceipt = l.TopologyReceipt })
		}, "does not record a validated topology"},
		{"a topology receipt naming no validator", func(l *TrainingLabel) {
			resealTopology(l, func(tp *TopologyValidationReceipt) { tp.Validator = "" })
			resealReceipt(l, func(r *PhysicalVReceipt) { r.TopologyReceipt = l.TopologyReceipt })
		}, "names no validator"},
		{"a topology receipt for other selected work", func(l *TrainingLabel) {
			resealTopology(l, func(tp *TopologyValidationReceipt) {
				tp.SelectedWorkDigest = DigestBytes([]byte("other-6"))
			})
			resealReceipt(l, func(r *PhysicalVReceipt) { r.TopologyReceipt = l.TopologyReceipt })
		}, "validates selected work"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := admissibleLabel("h1", 1, 1, 2)
			// The starting point must be accepted, or the case below proves
			// nothing about the clause it broke.
			if problems := l.VerifyEvidence(testEvidenceSigners()); len(problems) > 0 {
				t.Fatalf("the unedited label does not verify: %v", problems)
			}
			tc.edit(&l)
			problems := l.VerifyEvidence(testEvidenceSigners())
			if len(problems) == 0 {
				t.Fatalf("the evidence chain accepted %s", tc.name)
			}
			if !strings.Contains(strings.Join(problems, "; "), tc.want) {
				t.Errorf("no problem mentions %q:\n%s", tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}

// TestVerifyTrainingSurfaceLoadsTheEvidence: the independent verifier — not
// just the offline constructor — must refuse a lineage whose references cannot
// be produced. This is the path Stage 1 and `wall verify` actually call.
func TestVerifyTrainingSurfaceLoadsTheEvidence(t *testing.T) {
	good := sealedSet()
	scorer, err := TrainScorer(good, "test-scorer", sealKeys())
	if err != nil {
		t.Fatalf("the genuine set does not fit: %v", err)
	}
	if problems := VerifyTrainingSurface(good, scorer.Lineage, *scorer, sealKeys()); len(problems) > 0 {
		t.Fatalf("the genuine surface does not verify: %v", problems)
	}

	// Same signed set, same coefficients, evidence removed. The digests, the
	// seal and the refit are all still internally consistent.
	stripped := good
	stripped.Labels = append([]TrainingLabel(nil), good.Labels...)
	for i := range stripped.Labels {
		l := stripped.Labels[i]
		l.Evidence = nil
		stripped.Labels[i] = l
	}
	if err := stripped.Seal("ewj2-training", trainingSealKey); err != nil {
		t.Fatal(err)
	}
	problems := VerifyTrainingSurface(stripped, scorer.Lineage, *scorer, sealKeys())
	if len(problems) == 0 {
		t.Fatal("independent verification accepted a lineage whose evidence does not exist")
	}
	if !strings.Contains(strings.Join(problems, "; "), "carries no evidence") {
		t.Errorf("no problem names the missing evidence:\n%s", strings.Join(problems, "\n"))
	}
}
