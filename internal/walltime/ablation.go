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
	seen := map[string]bool{}
	for i, a := range index.Ablations {
		where := fmt.Sprintf("ablation %d (%s)", i, a.Stratum)
		if !validStratum(a.Stratum) {
			problems = append(problems, fmt.Sprintf(
				"%s names a stratum that is not one of the four the contract fixes: %s", where, strings.Join(AblationStrata, ", ")))
		} else {
			counts[a.Stratum]++
		}
		// One verdict may not stand in for two ablations. Twelve references to
		// three measurements is three measurements.
		if strings.TrimSpace(a.VerdictPath) == "" {
			problems = append(problems, where+" names no verdict, so there is no measured row behind it")
			continue
		}
		if seen[a.VerdictPath] {
			problems = append(problems, fmt.Sprintf("%s reuses verdict %s, which another ablation already counted", where, a.VerdictPath))
			continue
		}
		seen[a.VerdictPath] = true
		problems = append(problems, checkAblationRow(where, a, loader, authorityKeys, authority, campaignStart)...)
	}

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
func checkAblationRow(where string, a CampaignAblationRef, loader CampaignLoader, authorityKeys []string, authority string, campaignStart time.Time) []string {
	var problems []string
	if strings.TrimSpace(a.Stage1Path) == "" {
		return append(problems, where+" names no Stage-1 manifest, so nothing authorised the inputs it measured")
	}
	m, err := loader.Manifest(a.Stage1Path)
	if err != nil {
		return append(problems, fmt.Sprintf("%s manifest: %v", where, err))
	}
	if err := m.RequireApproval(authorityKeys, authority); err != nil {
		return append(problems, fmt.Sprintf("%s manifest authority: %v", where, err))
	}
	v, err := loader.Verdict(a.VerdictPath)
	if err != nil {
		return append(problems, fmt.Sprintf("%s verdict: %v", where, err))
	}
	if !v.Eligible {
		problems = append(problems, where+" is not an eligible measured row; an ablation that could not be scored did not qualify the envelope, the peer or the trace")
	}
	if v.Signature == nil {
		problems = append(problems, where+" carries an unsigned verdict, so nobody is attributable for it")
	} else if strings.TrimSpace(v.Run.VerifierID) == "" {
		problems = append(problems, where+" names no delivery verifier identity")
	} else if v.Signature.Authority != v.Run.VerifierID {
		problems = append(problems, fmt.Sprintf(
			"%s was signed under authority %q but its body names delivery verifier %q", where, v.Signature.Authority, v.Run.VerifierID))
	}
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
