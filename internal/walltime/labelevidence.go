package walltime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// The three documents a training label REFERS to, and which must exist.
//
// Before this file, a label carried three digests — a receipt hash, a
// selected-work digest and a topology receipt — and validation checked only
// that the strings were non-empty. Sealing the set signed those strings, which
// proves somebody wrote them and nothing else: an offline surface built from
// labels whose references point at nothing is indistinguishable from one built
// from real observations, and the campaign's whole allocation surface descends
// from it.
//
// The contract asks the verifier to prove each label's exact receipt hash,
// causal selected-work attribution and validated topology receipt. That is only
// possible if the bytes are reachable, so the label carries them: the set is
// sealed, so embedding keeps the evidence immutable and self-contained, the
// same reason the planning-input bundle carries its snapshots inline rather
// than by path.
const (
	PhysicalVReceiptKind = "tb.walltime.physical-v-receipt/v1"
	SelectedWorkKind     = "tb.walltime.selected-work/v1"
	TopologyReceiptKind  = "tb.walltime.topology-receipt/v1"
)

// PhysicalVReceipt is one historical wrapper-qualified physical V observation:
// the thing a training label claims its duration came from.
type PhysicalVReceipt struct {
	Kind      string `json:"kind"`
	ReceiptID string `json:"receipt_id"`
	UnitID    string `json:"unit_id"`
	// The EXCLUSION DOMAIN identities, in the signed receipt where they belong.
	//
	// The contract excludes candidate, campaign, current-run and holdout
	// evidence from the training surface. Those are properties of the
	// OBSERVATION, so a receipt that does not carry them cannot be excluded by
	// them: exclusions used to be compared against `receipt_id` alone, and a
	// label from an explicitly excluded campaign was admissible because
	// nothing in the schema could say which campaign it came from.
	//
	// They are part of the signed receipt bytes, so the observing authority
	// attests them and a later reader cannot add or drop one.
	CampaignID  string `json:"campaign_id,omitempty"`
	CandidateID string `json:"candidate_id,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	HoldoutID   string `json:"holdout_id,omitempty"`
	// Level, Producer and Source are the qualification. A reporter annotation
	// or a peer/trace record is not a physical V, and only an independently
	// observed OS class may delimit one.
	Level    Level    `json:"level"`
	Producer Producer `json:"producer"`
	Source   string   `json:"source"`
	// Containment must be a SCORED primitive: a lifecycle delimited by a
	// process group cannot prove no descendant escaped, so a duration measured
	// under one is not admissible training material either.
	Containment ContainmentIdentity `json:"containment"`
	Terminal    string              `json:"terminal"`
	ObservedAt  string              `json:"observed_at"`
	DurationNs  int64               `json:"duration_ns"`
	// SelectedWorkDigest and TopologyReceipt are stated BY THE RECEIPT. The
	// label repeats them, and the two must agree: a label that could name a
	// different attribution than its own receipt would be asserting the causal
	// chain rather than carrying it.
	SelectedWorkDigest Digest     `json:"selected_work_digest"`
	TopologyReceipt    Digest     `json:"topology_receipt"`
	Signature          *Signature `json:"signature,omitempty"`
}

// DigestOf is the receipt's canonical identity, excluding its own signature.
func (r PhysicalVReceipt) DigestOf() (Digest, error) {
	c := r
	c.Signature = nil
	return DigestJSON(c)
}

// SelectedWorkDocument is the immutable selected work a duration is causally
// attributed to.
type SelectedWorkDocument struct {
	Kind      string     `json:"kind"`
	UnitID    string     `json:"unit_id"`
	Units     []string   `json:"units"`
	Signature *Signature `json:"signature,omitempty"`
}

// DigestOf is the document's canonical identity, excluding its own signature.
func (d SelectedWorkDocument) DigestOf() (Digest, error) {
	c := d
	c.Signature = nil
	return DigestJSON(c)
}

// TopologyValidationReceipt is the proof that the selected work's topology was
// validated. An unvalidated topology makes the label inadmissible even when
// everything else about it is genuine.
type TopologyValidationReceipt struct {
	Kind               string     `json:"kind"`
	UnitID             string     `json:"unit_id"`
	SelectedWorkDigest Digest     `json:"selected_work_digest"`
	Validated          bool       `json:"validated"`
	Validator          string     `json:"validator"`
	Signature          *Signature `json:"signature,omitempty"`
}

// DigestOf is the receipt's canonical identity, excluding its own signature.
func (r TopologyValidationReceipt) DigestOf() (Digest, error) {
	c := r
	c.Signature = nil
	return DigestJSON(c)
}

// LabelEvidence is the exact bytes behind one label's three references.
//
// Bytes rather than parsed documents, because the label's digests address
// BYTES: re-serialising a parsed document could produce a different encoding
// and a different hash, and then "the reference matches" would be a statement
// about this package's JSON writer rather than about the evidence.
type LabelEvidence struct {
	ReceiptBytes      []byte `json:"receipt_bytes"`
	SelectedWorkBytes []byte `json:"selected_work_bytes"`
	TopologyBytes     []byte `json:"topology_bytes"`
}

// Verify proves that a label's three references point at real, admissible,
// mutually consistent evidence, and returns every problem rather than the
// first: a fabricated lineage usually fails in several places at once, and a
// reader repairing it should see all of them.
//
// signers are the PREDECLARED keys allowed to attest this evidence. An empty
// set is not "accept any key": evidence verified against whatever signed it is
// evidence anybody can mint.
func (l TrainingLabel) VerifyEvidence(signers []string) []string {
	var problems []string
	where := fmt.Sprintf("label %s", l.ReceiptID)
	if l.Evidence == nil {
		return []string{where + " carries no evidence: its receipt hash, selected-work digest and topology receipt are strings that refer to nothing, and sealing the set signs the strings rather than the observations"}
	}
	if len(signers) == 0 {
		return []string{where + ": no evidence signer was predeclared, so a receipt verified against whatever signed it would authenticate itself"}
	}

	// 1. The bytes ARE what the label's references address.
	receipt, p := decodeEvidence[PhysicalVReceipt](where, "receipt", l.Evidence.ReceiptBytes, l.ReceiptHash, PhysicalVReceiptKind, signers)
	problems = append(problems, p...)
	work, p := decodeEvidence[SelectedWorkDocument](where, "selected-work document", l.Evidence.SelectedWorkBytes, l.SelectedWorkDigest, SelectedWorkKind, signers)
	problems = append(problems, p...)
	topology, p := decodeEvidence[TopologyValidationReceipt](where, "topology receipt", l.Evidence.TopologyBytes, l.TopologyReceipt, TopologyReceiptKind, signers)
	problems = append(problems, p...)
	if receipt == nil || work == nil || topology == nil {
		return problems
	}

	// 2. The receipt is a WRAPPER-QUALIFIED PHYSICAL V, not something else
	//    wearing the label's provenance string.
	if receipt.Producer != ProducerPhysical {
		problems = append(problems, fmt.Sprintf("%s: its receipt was produced by %q, not the physical wrapper; a peer or trace record is not a physical V", where, receipt.Producer))
	}
	if receipt.Level != LevelInvocation {
		problems = append(problems, fmt.Sprintf("%s: its receipt measures level %q, not an invocation; V is the invocation envelope", where, receipt.Level))
	}
	if receipt.Source != SourceContainment && receipt.Source != SourceProcessLifecycle {
		problems = append(problems, fmt.Sprintf("%s: its receipt is delimited by %q; only an independently observed %s or %s record may delimit a lifecycle", where, receipt.Source, SourceContainment, SourceProcessLifecycle))
	}
	if !receipt.Containment.Scorable() {
		problems = append(problems, fmt.Sprintf("%s: its receipt was measured under containment %q, which cannot prove that no descendant escaped, so its duration is not admissible training material", where, receipt.Containment.Primitive))
	}
	if receipt.Terminal != TerminalPassed {
		problems = append(problems, fmt.Sprintf("%s: its receipt is terminal %q; a failed, cancelled or unclosed observation is retained, never trained on", where, receipt.Terminal))
	}

	// 3. The receipt describes THIS label. A genuine receipt for other work
	//    would otherwise qualify any duration somebody wrote beside it.
	if receipt.ReceiptID != l.ReceiptID {
		problems = append(problems, fmt.Sprintf("%s: its receipt identifies itself as %s", where, receipt.ReceiptID))
	}
	if receipt.UnitID != l.UnitID {
		problems = append(problems, fmt.Sprintf("%s: its receipt measures unit %s, not %s", where, receipt.UnitID, l.UnitID))
	}
	if receipt.DurationNs != l.ObservedNs {
		problems = append(problems, fmt.Sprintf("%s: it trains on %d ns but its receipt observed %d ns", where, l.ObservedNs, receipt.DurationNs))
	}
	if receipt.ObservedAt != l.ObservedAt {
		problems = append(problems, fmt.Sprintf("%s: it claims %s but its receipt observed %s", where, l.ObservedAt, receipt.ObservedAt))
	}

	// 4. The exclusion-domain identities the label repeats must be the ones
	//    its own signed receipt states. A label that could name a different
	//    campaign than its receipt would be excludable only on its own say-so.
	for _, f := range []struct {
		what        string
		label, from string
	}{
		{"campaign", l.CampaignID, receipt.CampaignID},
		{"candidate", l.CandidateID, receipt.CandidateID},
		{"run", l.RunID, receipt.RunID},
		{"holdout", l.HoldoutID, receipt.HoldoutID},
	} {
		if l.Evidence != nil && f.label != f.from {
			problems = append(problems, fmt.Sprintf("%s: it names %s %q, but its own receipt names %q", where, f.what, f.label, f.from))
		}
	}

	// 5. The causal chain is stated by the RECEIPT, not only by the label.
	if receipt.SelectedWorkDigest != l.SelectedWorkDigest {
		problems = append(problems, fmt.Sprintf("%s: it attributes the duration to selected work %s, but its own receipt attributes it to %s", where, l.SelectedWorkDigest, receipt.SelectedWorkDigest))
	}
	if receipt.TopologyReceipt != l.TopologyReceipt {
		problems = append(problems, fmt.Sprintf("%s: it names topology receipt %s, but its own receipt names %s", where, l.TopologyReceipt, receipt.TopologyReceipt))
	}

	// 6. The selected work is this unit's, and it is not empty: a duration
	//    attributed to no work is not causally attributed at all.
	if work.UnitID != l.UnitID {
		problems = append(problems, fmt.Sprintf("%s: its selected-work document is for unit %s", where, work.UnitID))
	}
	if len(work.Units) == 0 {
		problems = append(problems, fmt.Sprintf("%s: its selected-work document selects nothing, so the duration is attributed to no work", where))
	}

	// 7. The topology was VALIDATED, for this unit, over this selected work.
	if !topology.Validated {
		problems = append(problems, fmt.Sprintf("%s: its topology receipt does not record a validated topology", where))
	}
	if strings.TrimSpace(topology.Validator) == "" {
		problems = append(problems, fmt.Sprintf("%s: its topology receipt names no validator", where))
	}
	if topology.UnitID != l.UnitID {
		problems = append(problems, fmt.Sprintf("%s: its topology receipt is for unit %s", where, topology.UnitID))
	}
	if topology.SelectedWorkDigest != l.SelectedWorkDigest {
		problems = append(problems, fmt.Sprintf("%s: its topology receipt validates selected work %s, not the %s this label is attributed to", where, topology.SelectedWorkDigest, l.SelectedWorkDigest))
	}
	return problems
}

// decodeEvidence hashes the bytes, checks them against the reference the label
// carries, parses them, checks the document kind, and verifies the signature
// against the predeclared signer set.
func decodeEvidence[T any](where, what string, raw []byte, want Digest, kind string, signers []string) (*T, []string) {
	if len(raw) == 0 {
		return nil, []string{fmt.Sprintf("%s: its %s is absent, so %s refers to nothing", where, what, want)}
	}
	if got := DigestBytes(raw); got != want {
		return nil, []string{fmt.Sprintf("%s: its %s digests to %s, not the %s it names", where, what, got, want)}
	}
	var doc T
	// UNKNOWN FIELDS ARE REFUSED. Ordinary decoding discards them, and a
	// security-relevant field the schema does not model is one no check can
	// read: a receipt carrying an exclusion identity nobody decoded verified
	// its own signature perfectly while the value it named went unexamined.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, []string{fmt.Sprintf("%s: its %s does not parse: %v", where, what, err)}
	}
	k, sig, err := evidenceIdentity(&doc)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: its %s: %v", where, what, err)}
	}
	if k != kind {
		return nil, []string{fmt.Sprintf("%s: its %s has kind %q, want %q", where, what, k, kind)}
	}
	digest, err := evidenceDigest(&doc)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: its %s: %v", where, what, err)}
	}
	if err := VerifySigned(sig, digest, signers); err != nil {
		return nil, []string{fmt.Sprintf("%s: its %s signature: %v", where, what, err)}
	}
	return &doc, nil
}

// evidenceIdentity and evidenceDigest read the two fields every evidence
// document has. A type switch rather than an interface so the documents stay
// plain serialisable structs with no method set to satisfy.
func evidenceIdentity(doc any) (string, *Signature, error) {
	switch d := doc.(type) {
	case *PhysicalVReceipt:
		return d.Kind, d.Signature, nil
	case *SelectedWorkDocument:
		return d.Kind, d.Signature, nil
	case *TopologyValidationReceipt:
		return d.Kind, d.Signature, nil
	}
	return "", nil, fmt.Errorf("unknown evidence document type %T", doc)
}

func evidenceDigest(doc any) (Digest, error) {
	switch d := doc.(type) {
	case *PhysicalVReceipt:
		return d.DigestOf()
	case *SelectedWorkDocument:
		return d.DigestOf()
	case *TopologyValidationReceipt:
		return d.DigestOf()
	}
	return "", fmt.Errorf("unknown evidence document type %T", doc)
}
