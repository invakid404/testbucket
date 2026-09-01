package walltime

import (
	"fmt"
	"sort"
	"strings"
)

// CampaignIndexKind identifies the document that names a campaign's rows.
const CampaignIndexKind = "tb.walltime.campaign-index/v1"

// CampaignArm is one arm of one pair: which Stage-1 manifest authorised it and
// which verifier verdicts are its rows.
//
// It names FILES rather than carrying durations, because a campaign assembled
// from bare numbers is a spreadsheet. Every action time below is read out of a
// verdict that was verified, and every verdict is checked for eligibility
// before its number is used.
type CampaignArm struct {
	RunID     string `json:"run_id"`
	StartedAt string `json:"started_at"`
	Terminal  string `json:"terminal"`
	// Stage1Path is the manifest that authorised this arm. Both arms of a pair
	// are compared through it.
	Stage1Path string `json:"stage1"`
	// VerdictPaths is one verifier verdict per bucket.
	VerdictPaths []string `json:"verdicts"`
}

// CampaignPairRef is one randomized baseline/candidate pair.
type CampaignPairRef struct {
	Baseline  CampaignArm `json:"baseline"`
	Candidate CampaignArm `json:"candidate"`
}

// CampaignIndex is the campaign's population, by reference.
type CampaignIndex struct {
	Kind       string            `json:"kind"`
	CampaignID string            `json:"campaign_id"`
	Pairs      []CampaignPairRef `json:"pairs"`
}

// CampaignLoader reads the artifacts an index names. It is an interface so a
// test can supply documents without a filesystem; production uses
// FileCampaignLoader.
type CampaignLoader interface {
	Verdict(path string) (*Verdict, error)
	Manifest(path string) (*Stage1Manifest, error)
}

// FileCampaignLoader reads them from disk.
type FileCampaignLoader struct{}

// Verdict loads one verifier verdict.
func (FileCampaignLoader) Verdict(path string) (*Verdict, error) {
	var v Verdict
	if err := ReadJSONFile(path, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// Manifest loads one Stage-1 manifest.
func (FileCampaignLoader) Manifest(path string) (*Stage1Manifest, error) {
	var m Stage1Manifest
	if err := ReadJSONFile(path, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// LoadCampaign turns an index into an AUTHENTICATED population, or into the
// list of reasons it is not one.
//
// This is the difference between a calculator and a campaign verifier. Every
// action time comes from a verdict that says `eligible: true`; every verdict
// names the campaign, the run and the Stage-1 manifest it belongs to; every
// pair's two manifests are compared field by field and may differ only in the
// enumerated candidate tuple; and every manifest is signed by a predeclared
// authority. A number that survives all of that is evidence. A number in a
// JSON file is not.
func LoadCampaign(index CampaignIndex, loader CampaignLoader, authorityKeys []string) ([]CampaignPair, []string) {
	var problems []string
	if index.Kind != CampaignIndexKind {
		problems = append(problems, fmt.Sprintf("campaign index kind is %q, want %q", index.Kind, CampaignIndexKind))
	}
	if strings.TrimSpace(index.CampaignID) == "" {
		problems = append(problems, "the campaign index names no campaign")
	}
	if len(authorityKeys) == 0 {
		problems = append(problems, "no authority key was predeclared, so no arm's manifest can be treated as authorised")
	}

	var pairs []CampaignPair
	for i, ref := range index.Pairs {
		baseline, bm, bp := loadArm(index, ref.Baseline, "baseline", i, loader, authorityKeys)
		candidate, cm, cp := loadArm(index, ref.Candidate, "candidate", i, loader, authorityKeys)
		problems = append(problems, bp...)
		problems = append(problems, cp...)
		if bm != nil && cm != nil {
			for _, d := range CompareArms(*bm, *cm) {
				problems = append(problems, fmt.Sprintf("pair %d arms differ outside the allowed-difference matrix — %s", i, d))
			}
		}
		pairs = append(pairs, CampaignPair{Baseline: baseline, Candidate: candidate})
	}
	return pairs, problems
}

// loadArm authenticates one arm and returns its run, its manifest and every
// reason it is not admissible.
func loadArm(index CampaignIndex, arm CampaignArm, role string, pair int, loader CampaignLoader, authorityKeys []string) (CampaignRun, *Stage1Manifest, []string) {
	where := fmt.Sprintf("pair %d %s run %q", pair, role, arm.RunID)
	var problems []string
	run := CampaignRun{RunID: arm.RunID, StartedAt: arm.StartedAt, Terminal: arm.Terminal}

	var manifest *Stage1Manifest
	var manifestDigest Digest
	if strings.TrimSpace(arm.Stage1Path) == "" {
		problems = append(problems, where+" names no Stage-1 manifest")
	} else if m, err := loader.Manifest(arm.Stage1Path); err != nil {
		problems = append(problems, fmt.Sprintf("%s: %v", where, err))
	} else if err := m.Validate(); err != nil {
		problems = append(problems, fmt.Sprintf("%s: %v", where, err))
	} else if m.Role != role {
		problems = append(problems, fmt.Sprintf("%s is bound to a manifest whose role is %q", where, m.Role))
	} else {
		d, err := m.DigestOf()
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", where, err))
		} else if err := VerifySigned(m.Signature, d, authorityKeys); err != nil {
			problems = append(problems, fmt.Sprintf("%s manifest authority: %v", where, err))
		} else {
			manifest, manifestDigest = m, d
		}
	}

	for j, path := range arm.VerdictPaths {
		row := fmt.Sprintf("%s bucket %d", where, j)
		v, err := loader.Verdict(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", row, err))
			continue
		}
		switch {
		case !v.Complete:
			problems = append(problems, row+" is not a complete measurement")
			continue
		case !v.Eligible:
			// Intention-to-treat: the row is RETAINED in the population — it
			// is why the pair fails — but its duration is not scored.
			problems = append(problems, row+" is not eligible, so its duration is not a scored observation")
			continue
		case v.ActionNs <= 0:
			problems = append(problems, row+" has no positive complete action")
			continue
		}
		if v.Run.CampaignID != index.CampaignID {
			problems = append(problems, fmt.Sprintf("%s belongs to campaign %q, not %q", row, v.Run.CampaignID, index.CampaignID))
		}
		if arm.RunID != "" && v.Run.RunID != arm.RunID {
			problems = append(problems, fmt.Sprintf("%s was recorded under run %q, not %q", row, v.Run.RunID, arm.RunID))
		}
		if manifestDigest != "" && v.Run.Stage1 != manifestDigest {
			problems = append(problems, fmt.Sprintf("%s names Stage-1 %s, not this arm's %s", row, v.Run.Stage1, manifestDigest))
		}
		d, err := v.DigestOf()
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", row, err))
			continue
		}
		run.ActionNs = append(run.ActionNs, v.ActionNs)
		run.VerdictDigests = append(run.VerdictDigests, d)
	}
	return run, manifest, problems
}

// EvaluateCampaignIndex is the whole product decision: authenticate the
// population, then apply the frozen rule to it. A campaign that cannot be
// authenticated does not reach the arithmetic, because a ratio over
// unauthenticated rows answers a question nobody asked.
func EvaluateCampaignIndex(index CampaignIndex, loader CampaignLoader, authorityKeys []string) ([]GateResult, []string) {
	pairs, problems := LoadCampaign(index, loader, authorityKeys)
	sort.Strings(problems)
	if len(problems) > 0 {
		return []GateResult{{
			Name: "campaign:authenticated-population", Scope: ScopeCampaign,
			Required: "every row an eligible verifier verdict, every arm an authorised Stage-1 manifest, every pair equal outside the allowed-difference matrix",
			Observed: fmt.Sprintf("%d problem(s)", len(problems)),
			Detail:   strings.Join(problems, "; "),
		}}, problems
	}
	authenticated := GateResult{
		Name: "campaign:authenticated-population", Scope: ScopeCampaign,
		Required: "every row an eligible verifier verdict, every arm an authorised Stage-1 manifest, every pair equal outside the allowed-difference matrix",
		Observed: fmt.Sprintf("%d pair(s) authenticated", len(pairs)),
		Pass:     true, Population: len(pairs),
	}
	return append([]GateResult{authenticated}, EvaluateCampaign(pairs)...), nil
}
