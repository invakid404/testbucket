package walltime

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The four TOPOLOGY STRATA the contract fixes. Every ablation is qualified by
// exactly one of them, and three ablations are required in each.
//
// They are the same four the v0.2.2 audit must pass, and they are named here
// as constants rather than accepted as free text: a stratum a caller could
// spell however it liked would let twelve ablations of one shape satisfy a
// rule that exists to require four shapes.
const (
	StratumWholeFileMultiFile = "whole-file-multi-file"
	StratumCollisionAtom      = "collision-atom"
	StratumLegalNonAtomSlice  = "legal-non-atom-slice"
	StratumSequentialInvocs   = "multiple-sequential-invocations"
)

// AblationStrata is the fixed set, in the contract's order.
var AblationStrata = []string{
	StratumWholeFileMultiFile, StratumCollisionAtom,
	StratumLegalNonAtomSlice, StratumSequentialInvocs,
}

// AblationsPerStratum and RequiredAblations are the contract's counts: three
// in each of the four strata, twelve in total.
const (
	AblationsPerStratum = 3
	// RequiredAblations is three in each of the four fixed strata. A test
	// pins it against len(AblationStrata) so the two cannot drift.
	RequiredAblations = AblationsPerStratum * 4
)

// CampaignAblationRef is one controlled ablation that PRECEDES the campaign.
//
// It names files for the same reason an arm does: the durations and the
// eligibility are read out of a verifier verdict that was itself verified,
// never out of this index. What the index contributes is which stratum the
// ablation belongs to and which authorised manifest it ran under.
type CampaignAblationRef struct {
	// Stratum is one of AblationStrata.
	Stratum string `json:"stratum"`
	// RunID is the workflow run the ablation was measured in.
	RunID string `json:"run_id"`
	// Stage1Path is the authority-signed manifest that authorised it.
	Stage1Path string `json:"stage1"`
	// Stage2Path is the derived-plan receipt this ablation realized.
	//
	// The stratum in a signed Stage-1 manifest states an INTENT. It does not
	// show that the run produced the topology that intent names, and twelve
	// label-only copies of one baseline manifest satisfied the count exactly
	// as well as twelve experiments did. The Stage-2 receipt is the plan that
	// was actually derived; the verdict's own invocation and reconciliation
	// evidence is what it realized.
	Stage2Path string `json:"stage2"`
	// VerdictPath is the verifier verdict for the measured row.
	VerdictPath string `json:"verdict"`
}

// verifyAblations is the PRE-CAMPAIGN gate.
//
// The contract requires twelve controlled physical-envelope /
// containment-peer / trace-qualified ablations — three in each of the four
// topology strata — to precede the campaign, and says their outcome cannot
// tune the campaign's gates. Nothing represented them at all: the index
// carried a campaign id, an order digest and five pairs, so a release could be
// authorised by a population that skipped a mandatory experimental
// prerequisite entirely, and did so silently.
//
// This checks the three things that can be checked without letting the
// ablations' RESULTS influence anything: that they exist in the required
// number and shape, that each one is a genuine authenticated eligible measured
// row under the same authority, and that they came BEFORE the campaign. No
// duration, ratio or outcome from an ablation is read anywhere, which is what
// keeps "they ran" from becoming "they said what we wanted".
func verifyAblations(index CampaignIndex, loader CampaignLoader, authorityKeys []string, authority string) []string {
	var problems []string
	if len(index.Ablations) == 0 {
		return []string{fmt.Sprintf(
			"the campaign index records no pre-campaign ablations; the contract requires %d controlled physical-envelope/containment-peer/trace-qualified ablations (%d in each of the %d topology strata) to precede the campaign, and a release authorised without them skipped a mandatory prerequisite",
			RequiredAblations, AblationsPerStratum, len(AblationStrata))}
	}
	if len(index.Ablations) != RequiredAblations {
		problems = append(problems, fmt.Sprintf(
			"the campaign index records %d pre-campaign ablation(s), and the contract requires exactly %d",
			len(index.Ablations), RequiredAblations))
	}

	// The earliest pair start: every ablation must precede the campaign, and
	// "precede" is decided against the arms' own verified start times rather
	// than against anything the index claims.
	campaignStart := earliestPairStart(index, loader)

	counts := map[string]int{}
	seenPath := map[string]bool{}
	// Identity is the MEASUREMENT, not the pathname. Copying one genuinely
	// signed verdict under twelve names and spreading those names across the
	// four strata satisfied a path-only check exactly as well as twelve
	// measurements did — so one ablation counted as the whole prerequisite.
	// A measurement is identified by the records it was derived from, by the
	// verdict document itself, and by the run it was recorded under; a
	// collision in any of the three is the same measurement twice.
	seenRecords := map[Digest]string{}
	seenVerdict := map[Digest]string{}
	seenRun := map[string]string{}
	for i, a := range index.Ablations {
		where := fmt.Sprintf("ablation %d (%s)", i, a.Stratum)
		if !validStratum(a.Stratum) {
			problems = append(problems, fmt.Sprintf(
				"%s names a stratum that is not one of the four the contract fixes: %s", where, strings.Join(AblationStrata, ", ")))
		} else {
			counts[a.Stratum]++
		}
		if strings.TrimSpace(a.VerdictPath) == "" {
			problems = append(problems, where+" names no verdict, so there is no measured row behind it")
			continue
		}
		if seenPath[a.VerdictPath] {
			problems = append(problems, fmt.Sprintf("%s reuses verdict %s, which another ablation already counted", where, a.VerdictPath))
			continue
		}
		seenPath[a.VerdictPath] = true
		v, rowProblems := checkAblationRow(where, a, loader, authorityKeys, authority, campaignStart)
		problems = append(problems, rowProblems...)
		if v == nil {
			continue
		}
		for _, dup := range []struct {
			what  string
			key   string
			index map[string]string
		}{
			{"was recorded under run", v.Run.RunID, seenRun},
		} {
			if dup.key == "" {
				continue
			}
			if prev, ok := dup.index[dup.key]; ok {
				problems = append(problems, fmt.Sprintf(
					"%s %s %q, which %s already counted; twelve references to one measurement is one measurement",
					where, dup.what, dup.key, prev))
			} else {
				dup.index[dup.key] = where
			}
		}
		if prev, ok := seenRecords[v.RecordsDigest]; ok {
			problems = append(problems, fmt.Sprintf(
				"%s was derived from records %s, which %s already counted; twelve references to one measurement is one measurement",
				where, v.RecordsDigest, prev))
		} else {
			seenRecords[v.RecordsDigest] = where
		}
		if d, err := v.DigestOf(); err == nil {
			if prev, ok := seenVerdict[d]; ok {
				problems = append(problems, fmt.Sprintf(
					"%s is byte-identical to the verdict %s already counted", where, prev))
			} else {
				seenVerdict[d] = where
			}
		}
	}

	problems = append(problems, verifyStrataAreDistinct(index, loader)...)
	for _, stratum := range AblationStrata {
		if counts[stratum] != AblationsPerStratum {
			problems = append(problems, fmt.Sprintf(
				"topology stratum %q has %d admissible ablation(s), and the contract requires %d in each of the four",
				stratum, counts[stratum], AblationsPerStratum))
		}
	}
	sort.Strings(problems)
	return problems
}

func validStratum(s string) bool {
	for _, want := range AblationStrata {
		if s == want {
			return true
		}
	}
	return false
}

// checkAblationRow authenticates ONE ablation exactly as an arm is
// authenticated: an authority-signed manifest under the expected protected
// environment, and an eligible verdict signed by the delivery verifier it
// names.
func checkAblationRow(where string, a CampaignAblationRef, loader CampaignLoader, authorityKeys []string, authority string, campaignStart time.Time) (*Verdict, []string) {
	var problems []string
	if strings.TrimSpace(a.Stage1Path) == "" {
		return nil, append(problems, where+" names no Stage-1 manifest, so nothing authorised the inputs it measured")
	}
	m, err := loader.Manifest(a.Stage1Path)
	if err != nil {
		return nil, append(problems, fmt.Sprintf("%s manifest: %v", where, err))
	}
	// VALIDATED BEFORE IT IS BELIEVED, exactly as loadArm validates an arm's.
	//
	// This checked the authority signature and nothing else, so a manifest the
	// authority genuinely signed but which is not a well-formed Stage-1
	// document at all — the wrong kind, no reviewed actions, no source profile
	// — satisfied the mandatory prerequisite. A signature says who wrote a
	// document; it does not make the document mean anything.
	if err := m.Validate(); err != nil {
		return nil, append(problems, fmt.Sprintf("%s manifest: %v", where, err))
	}
	if err := m.RequireApproval(authorityKeys, authority); err != nil {
		return nil, append(problems, fmt.Sprintf("%s manifest authority: %v", where, err))
	}
	// THE STRATUM IS THE AUTHORITY'S, not the index's.
	//
	// CampaignAblationRef.Stratum lives in the unsigned campaign index, so
	// swapping two labels changed no manifest, verdict, records digest or
	// signature and still satisfied "three in each of the four strata". The
	// authority-signed manifest says which experiment this run was authorised
	// as, and the index may only agree with it.
	if strings.TrimSpace(m.AblationStratum) == "" {
		problems = append(problems, fmt.Sprintf(
			"%s is authorised by a Stage-1 manifest that declares no ablation stratum; the campaign index label is unsigned, so nothing else states which of the four topology strata this run belongs to", where))
	} else if m.AblationStratum != a.Stratum {
		problems = append(problems, fmt.Sprintf(
			"%s is indexed in stratum %q but its authority-signed manifest authorises stratum %q", where, a.Stratum, m.AblationStratum))
	}
	v, err := loader.Verdict(a.VerdictPath)
	if err != nil {
		return nil, append(problems, fmt.Sprintf("%s verdict: %v", where, err))
	}
	// THE SIGNATURE IS RECOMPUTED AND VERIFIED, exactly as an arm's is.
	//
	// This used to check that a Signature object existed and that its
	// authority string equalled the verdict's own declared verifier id — two
	// fields of the same unverified document agreeing with each other. It
	// never recomputed the digest, never called VerifySigned, and never used
	// the signers the manifest declares, so a verdict whose signed body had
	// been edited after signing satisfied the prerequisite while the signature
	// primitive itself rejected the document.
	if v.Signature == nil {
		return nil, append(problems, where+" carries an unsigned verdict, so nobody is attributable for it")
	}
	vd, err := v.DigestOf()
	if err != nil {
		return nil, append(problems, fmt.Sprintf("%s verdict: %v", where, err))
	}
	if len(m.VerdictSigners) == 0 {
		return nil, append(problems, where+" has no predeclared verdict signer, so its verdict would be authenticated by the campaign authority's own key")
	}
	if err := VerifySigned(v.Signature, vd, m.VerdictSigners); err != nil {
		return nil, append(problems, fmt.Sprintf("%s verdict signature: %v", where, err))
	}
	if strings.TrimSpace(v.Run.VerifierID) == "" {
		return nil, append(problems, where+" names no delivery verifier identity")
	}
	if v.Signature.Authority != v.Run.VerifierID {
		return nil, append(problems, fmt.Sprintf(
			"%s was signed under authority %q but its body names delivery verifier %q", where, v.Signature.Authority, v.Run.VerifierID))
	}
	// BOUND to this ablation and to the delivery it was measured under, so an
	// authenticated verdict for some other run cannot stand in for this one.
	if v.RecordsDigest == "" {
		problems = append(problems, where+" names no records digest, so it cannot be tied to the evidence it was derived from")
	}
	if md, err := m.DigestOf(); err == nil && v.Run.Stage1 != md {
		problems = append(problems, fmt.Sprintf("%s names Stage-1 %s, not the %s its own manifest digests to", where, v.Run.Stage1, md))
	}
	if v.VerifierBinary != m.Instrumentation.VerifierBinary {
		problems = append(problems, fmt.Sprintf("%s was produced by verifier %s, not the %s Stage 1 approved",
			where, v.VerifierBinary, m.Instrumentation.VerifierBinary))
	}
	if strings.TrimSpace(a.RunID) != "" && v.Run.RunID != a.RunID {
		problems = append(problems, fmt.Sprintf("%s is indexed under run %q but its verdict was recorded under %q", where, a.RunID, v.Run.RunID))
	}
	if !v.Complete {
		problems = append(problems, where+" is not a complete measurement")
	}
	if !v.Eligible {
		problems = append(problems, where+" is not an eligible measured row; an ablation that could not be scored did not qualify the envelope, the peer or the trace")
	}
	problems = append(problems, checkAblationTopology(where, a, m, v, loader)...)
	// BEFORE the campaign. An "ablation" run after the pairs it is supposed to
	// have informed is a post-hoc measurement wearing the name.
	if !campaignStart.IsZero() {
		at, err := time.Parse(time.RFC3339, v.StartedAt)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: its verdict records start time %q, which is not an RFC3339 instant, so it cannot be shown to precede the campaign", where, v.StartedAt))
		} else if !at.Before(campaignStart) {
			problems = append(problems, fmt.Sprintf(
				"%s started at %s, which is not before the campaign's first pair at %s; an ablation that follows the campaign cannot have preceded it",
				where, at.UTC().Format(time.RFC3339), campaignStart.UTC().Format(time.RFC3339)))
		}
	}
	return v, problems
}

// checkAblationTopology requires the ablation to have REALIZED the topology
// its stratum names, from the derived plan and the row's own evidence.
//
// A signed stratum in the Stage-1 manifest states an intent, and the gate
// believed it. Twelve rows built by copying one baseline manifest, changing
// only that label, re-signing and pairing a generic eligible verdict passed
// the whole prerequisite — no Stage-2 receipt, no invocation timings, no
// peer/trace reconciliation, nothing that showed twelve experiments had
// happened rather than one label written twelve times.
//
// What is required now is the evidence a real ablation necessarily produces:
// the derived plan it ran, bound to the same Stage-1 that authorised it; the
// invocation observations the row measured; and the peer/trace reconciliation
// that only exists when both observers bracketed the same lifecycle — which is
// what "physical-envelope/containment-peer/trace-qualified" means.
func checkAblationTopology(where string, a CampaignAblationRef, m *Stage1Manifest, v *Verdict, loader CampaignLoader) []string {
	var problems []string
	if strings.TrimSpace(a.Stage2Path) == "" {
		return append(problems, where+" names no Stage-2 receipt, so nothing states the plan it actually derived; a signed stratum label is an intent, not a realized topology")
	}
	receipt, err := loader.Stage2(a.Stage2Path)
	if err != nil {
		return append(problems, fmt.Sprintf("%s Stage-2: %v", where, err))
	}
	if err := receipt.Validate(); err != nil {
		return append(problems, fmt.Sprintf("%s Stage-2: %v", where, err))
	}
	stage2, err := receipt.DigestOf()
	if err != nil {
		return append(problems, fmt.Sprintf("%s Stage-2: %v", where, err))
	}
	// BOUND to the manifest that authorised it and to the row that ran it.
	if md, err := m.DigestOf(); err == nil && receipt.Stage1Digest != md {
		problems = append(problems, fmt.Sprintf(
			"%s ran a plan derived from Stage-1 %s, not the %s that authorised it", where, receipt.Stage1Digest, md))
	}
	if v.Run.Stage2 != stage2 {
		problems = append(problems, fmt.Sprintf(
			"%s measured Stage-2 %s but the receipt it names digests to %s", where, v.Run.Stage2, stage2))
	}
	// THE REALIZED OBSERVATION TOPOLOGY. A reconciliation entry exists only
	// where a peer and a trace both bracketed the same lifecycle, so its
	// absence means the row was not trace-qualified whatever its label says.
	if len(v.Recon) == 0 {
		problems = append(problems, where+" retains no peer/trace reconciliation, so nothing shows it was the physical-envelope/containment-peer/trace-qualified observation the contract requires")
	}
	if len(v.InvocationNs) == 0 {
		problems = append(problems, where+" retains no invocation observations, so its realized invocation topology is unstated")
	}
	// AND THE STRATUM'S OWN SHAPE. The four strata differ in what the run must
	// have executed, and the one that is decidable from retained evidence is
	// the sequential-invocation stratum: a single invocation cannot realize it.
	if a.Stratum == StratumSequentialInvocs && len(v.InvocationNs) < 2 {
		problems = append(problems, fmt.Sprintf(
			"%s is authorised into the %s stratum but measured %d invocation(s); that topology is not realized by one",
			where, StratumSequentialInvocs, len(v.InvocationNs)))
	}
	return problems
}

// verifyStrataAreDistinct requires the four strata to have realized DIFFERENT
// plans.
//
// Binding each ablation to a Stage-2 receipt proves it derived a plan; it does
// not prove the four strata are four experiments. Three strata handed one
// generic receipt shape — the same atom, membership, topology and invocation
// digests, differing only in the signed Stage-1 label above them — satisfied
// every per-row check while realizing one topology three times. Whole-file
// multi-file, collision-atom and legal-non-atom-slice runs derive materially
// different atom and membership documents; if two strata's plans agree on
// those digests, at most one of them is the experiment its label claims.
//
// The digests are compared rather than the documents re-derived because the
// documents are the planner's and the gate holds only their bound identities.
// Equality is decisive in the direction that matters: two identical plans
// cannot be two different topologies.
func verifyStrataAreDistinct(index CampaignIndex, loader CampaignLoader) []string {
	type shape struct {
		atom, membership, topology, invocation Digest
	}
	byStratum := map[string]shape{}
	first := map[string]string{}
	var problems []string
	for i, a := range index.Ablations {
		if strings.TrimSpace(a.Stage2Path) == "" {
			continue
		}
		receipt, err := loader.Stage2(a.Stage2Path)
		if err != nil {
			continue
		}
		got := shape{receipt.AtomDigest, receipt.MembershipDigest, receipt.TopologyDigest, receipt.InvocationDigest}
		where := fmt.Sprintf("ablation %d (%s)", i, a.Stratum)
		if prev, seen := byStratum[a.Stratum]; seen {
			// Within one stratum the three runs are repetitions of the same
			// experiment, so an identical plan is expected and says nothing.
			_ = prev
		} else {
			byStratum[a.Stratum] = got
			first[a.Stratum] = where
		}
		for stratum, other := range byStratum {
			if stratum == a.Stratum || other != got {
				continue
			}
			problems = append(problems, fmt.Sprintf(
				"%s realized the same plan as %s — identical atom, membership, topology and invocation digests — so the two rows are one experiment wearing two stratum labels",
				where, first[stratum]))
		}
	}
	sort.Strings(problems)
	return problems
}

// earliestPairStart is the campaign's own beginning, taken from the arms'
// verified verdicts rather than from the index's claims about them.
func earliestPairStart(index CampaignIndex, loader CampaignLoader) time.Time {
	var earliest time.Time
	for _, p := range index.Pairs {
		for _, arm := range []CampaignArm{p.Baseline, p.Candidate} {
			for _, path := range arm.VerdictPaths {
				v, err := loader.Verdict(path)
				if err != nil {
					continue
				}
				at, err := time.Parse(time.RFC3339, v.StartedAt)
				if err != nil {
					continue
				}
				if earliest.IsZero() || at.Before(earliest) {
					earliest = at
				}
			}
		}
	}
	return earliest
}
