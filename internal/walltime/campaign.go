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
	Kind       string `json:"kind"`
	CampaignID string `json:"campaign_id"`
	// OrderDigest is the frozen pair order this campaign claims to be
	// executing. It is checked against the authority-signed schedule Stage 1
	// binds; without it the five pairs are whichever five the index happens to
	// list, which is a selection nobody predeclared.
	OrderDigest Digest            `json:"order_digest"`
	Pairs       []CampaignPairRef `json:"pairs"`
}

// CampaignRelease is the IMMUTABLE delivery a campaign is being asked to
// authorise: the exact commit the tag resolves to and the exact bytes of the
// binary that will be published from it.
//
// It is supplied by the party doing the release, never read out of the
// evidence. Without it a campaign proves only that some five pairs passed at
// some tip: `LoadCampaign` required each arm's ReviewTip to equal its own
// ReleaseRefSHA, which two arms of a historical campaign satisfy perfectly
// while describing a commit that has nothing to do with the tag being cut. A
// valid campaign committed at campaign/index.json could therefore authorise
// every later release.
//
// Historical evidence stays auditable — nothing here deletes or rewrites it —
// but it authorises exactly the delivery it was produced for.
type CampaignRelease struct {
	// SHA is the full 40-hex commit the release ref resolves to.
	SHA string
	// Manifest is THE canonical publish set, derived once and re-verified
	// against the files on disk before it is believed.
	//
	// It replaced a bare list of hashed paths because a list said nothing
	// about whether those paths were the ones being uploaded. The gate hashed
	// goreleaser's Binary, Archive and Checksum rows while the uploader
	// globbed archives and checksums, so the raw platform binaries were gated
	// and never published — and the campaign's delivered-binary match could be
	// satisfied by a file no consumer ever receives.
	Manifest *ReleaseManifest
}

// Bound reports whether an expected delivery was supplied at all.
func (r CampaignRelease) Bound() bool { return strings.TrimSpace(r.SHA) != "" }

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
func LoadCampaign(index CampaignIndex, loader CampaignLoader, authorityKeys []string, authority string) ([]CampaignPair, []string) {
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
	// The frozen ORDER, bound before anything else is believed. Every other
	// check in this file asks whether a row is genuine; this one asks whether
	// this is the experiment the authority predeclared, and five genuine pairs
	// chosen after the fact from ten attempts pass all the others.
	var schedule *CampaignSchedule
	for i, ref := range index.Pairs {
		baseline, bm, bp := loadArm(index, ref.Baseline, "baseline", i, loader, authorityKeys, authority)
		candidate, cm, cp := loadArm(index, ref.Candidate, "candidate", i, loader, authorityKeys, authority)
		problems = append(problems, bp...)
		problems = append(problems, cp...)
		if bm != nil && cm != nil {
			for _, d := range CompareArms(*bm, *cm) {
				problems = append(problems, fmt.Sprintf("pair %d arms differ outside the allowed-difference matrix — %s", i, d))
			}
		}
		pairs = append(pairs, CampaignPair{Baseline: baseline, Candidate: candidate})
		// The schedule comes from the arms' own Stage-1 manifests, which are
		// authority-signed and already verified above. Taking it from the
		// index instead would let the thing being checked supply its own rule.
		for _, m := range []*Stage1Manifest{bm, cm} {
			if m == nil {
				continue
			}
			if err := m.Schedule.Validate(); err != nil {
				problems = append(problems, fmt.Sprintf("pair %d: %v", i, err))
				continue
			}
			if schedule == nil {
				sc := m.Schedule
				schedule = &sc
				continue
			}
			// Every arm must be authorised by the SAME order. Two manifests
			// carrying different schedules is two campaigns wearing one id.
			a, errA := schedule.OrderDigest()
			b, errB := m.Schedule.OrderDigest()
			if errA == nil && errB == nil && a != b {
				problems = append(problems, fmt.Sprintf(
					"pair %d is authorised by a different frozen pair order (%s) than an earlier arm (%s)", i, b, a))
			}
		}
	}
	if schedule == nil {
		problems = append(problems, "no arm's Stage-1 manifest binds a frozen campaign schedule, so the pair order, the roles and the dates were all decided outside the authority artifact")
	} else {
		problems = append(problems, bindOrder(index, *schedule)...)
	}
	return pairs, problems
}

// loadArm authenticates one arm and returns its run, its manifest and every
// reason it is not admissible.
func loadArm(index CampaignIndex, arm CampaignArm, role string, pair int, loader CampaignLoader, authorityKeys []string, authority string) (CampaignRun, *Stage1Manifest, []string) {
	where := fmt.Sprintf("pair %d %s run %q", pair, role, arm.RunID)
	var problems []string
	// StartedAt and Terminal are deliberately NOT taken from the arm. The
	// campaign index is an unsigned file, and these two fields decide the
	// three-UTC-date rule, the fourteen-day window and intention-to-treat
	// retention — an index that claimed schedule-shaped dates and "passed"
	// over a genuine signed row set would satisfy all three with none of the
	// facts authenticated. They are derived from the verdicts below and the
	// index's own claims are checked against them.
	run := CampaignRun{RunID: arm.RunID}

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
		} else if authority != "" && m.Signature.Authority != authority {
			// A key is WHO signed; the authority is WHICH protected
			// environment approved. Checking only the key would accept a
			// manifest approved by a different environment that happens to
			// share a signer.
			problems = append(problems, fmt.Sprintf("%s manifest names authority %q, not the required %q", where, m.Signature.Authority, authority))
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
		// A verdict is a CLAIM. Before any field of it is believed it must be
		// attributable to the approved verifier and to the exact records it
		// describes — otherwise a hand-written JSON file satisfies the
		// campaign, which is precisely the hole this closes.
		if v.Signature == nil {
			problems = append(problems, row+" is unsigned, so it attributes its verdict to nobody")
			continue
		}
		vd, err := v.DigestOf()
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", row, err))
			continue
		}
		// VERDICT SIGNERS, not the campaign authority. The two used to share
		// one key set, so a verdict-signing key was also a Stage-1 approval
		// key: the party deciding whether a row is eligible could approve the
		// inputs it was judging. The manifest this arm was authorised by is
		// what declares who may sign its verdicts.
		var verdictKeys []string
		if manifest != nil {
			verdictKeys = manifest.VerdictSigners
		}
		if len(verdictKeys) == 0 {
			problems = append(problems, row+" has no predeclared verdict signer, so its verdict would be authenticated by the campaign authority's own key")
			continue
		}
		if err := VerifySigned(v.Signature, vd, verdictKeys); err != nil {
			problems = append(problems, fmt.Sprintf("%s verdict signature: %v", row, err))
			continue
		}
		if v.RecordsDigest == "" {
			problems = append(problems, row+" names no records digest, so it cannot be tied to the evidence it was derived from")
			continue
		}
		if manifest != nil && v.VerifierBinary != manifest.Instrumentation.VerifierBinary {
			problems = append(problems, fmt.Sprintf("%s was produced by verifier %s, not the %s Stage 1 approved",
				row, v.VerifierBinary, manifest.Instrumentation.VerifierBinary))
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
		run.ActionNs = append(run.ActionNs, v.ActionNs)
		run.VerdictDigests = append(run.VerdictDigests, vd)
		if v.AetaSample != nil {
			run.AetaSamples = append(run.AetaSamples, *v.AetaSample)
		} else {
			problems = append(problems, row+" retains no Aeta sample, so the population-wide forecast mean cannot be computed over it")
		}

		// PREDICTOR COVERAGE, checked per row against that row's own measured
		// invocation count. Appending whatever samples happen to be present
		// makes an omission indistinguishable from a row with no invocations,
		// and eighty such omissions used to reach a passing campaign with the
		// Pcheck-versus-observed-V gates never evaluated at all.
		measured := len(v.InvocationNs)
		run.Invocations = append(run.Invocations, measured)
		if len(v.PredictorSample) != measured {
			problems = append(problems, fmt.Sprintf(
				"%s measured %d invocation(s) but retains %d Pcheck/observed-V sample(s), so it is not evidence for the predictor gates",
				row, measured, len(v.PredictorSample)))
		}
		// The samples are re-keyed to this arm's bucket ORDINAL. A verdict
		// carries the bucket index its own plan gave it; the campaign counts
		// coverage per row, and two rows that shared an index would collide.
		for _, sample := range v.PredictorSample {
			sample.BucketIndex = j
			run.PredictorSamples = append(run.PredictorSamples, sample)
		}

		// RECONCILIATION, retained so the campaign can compute the frozen
		// aggregate. The verifier decides each row's own 50/100 ms gate; only
		// the campaign can decide it over all 80 action peers, all 80 script
		// peers and every actual invocation peer, and it can only do that from
		// the deltas.
		if len(v.Recon) == 0 {
			problems = append(problems, row+" retains no like-for-like reconciliation, so the campaign-wide peer/trace population cannot be computed over it")
		}
		// The row's own AUTHENTICATED start and outcome, carried by the signed
		// verdict and derived there from the action envelope's records. Every
		// bucket of one run is one workflow run, so they must agree; the
		// earliest start is the run's, and any disagreement about the outcome
		// is reported rather than resolved.
		if v.StartedAt == "" {
			problems = append(problems, row+" carries no authenticated start instant, so the campaign's date and window rules would rest on the unsigned index")
		} else if run.StartedAt == "" || v.StartedAt < run.StartedAt {
			run.StartedAt = v.StartedAt
		}
		if v.Terminal == "" {
			problems = append(problems, row+" carries no authenticated terminal state, so intention-to-treat retention would rest on the unsigned index")
		} else if run.Terminal == "" {
			run.Terminal = v.Terminal
		} else if run.Terminal != v.Terminal {
			problems = append(problems, fmt.Sprintf("%s reports terminal %q but an earlier bucket of the same run reports %q", row, v.Terminal, run.Terminal))
		}

		// COPIED, not aliased. A Reconciliation carries a slice, so appending
		// the struct would leave the campaign's aggregate sharing memory with
		// the signed verdict it came from — and anything that later touched
		// the aggregate would silently edit the document whose signature is
		// the only reason the numbers are believed.
		for _, r := range v.Recon {
			run.Recon = append(run.Recon, Reconciliation{
				Level: r.Level, Deltas: append([]int64(nil), r.Deltas...),
			})
		}
	}
	problems = append(problems, checkArmClaims(where, arm, run)...)
	return run, manifest, problems
}

// checkArmClaims compares what the campaign index SAYS about a run with what
// its signed verdicts SHOW.
//
// The index is convenient — it is how a harness names runs and orders pairs —
// but it is a plain file. Where it repeats a fact the evidence already
// carries, the two must agree, and the evidence decides. An index that could
// restate the date or the outcome would be able to move a run into the
// three-date window, or report a cancelled run as passed, over a row set that
// is genuinely signed throughout.
func checkArmClaims(where string, arm CampaignArm, run CampaignRun) []string {
	var problems []string
	if arm.StartedAt != "" && run.StartedAt != "" && utcDate(arm.StartedAt) != utcDate(run.StartedAt) {
		problems = append(problems, fmt.Sprintf(
			"%s: the campaign index says it started on %s but its signed records say %s",
			where, utcDate(arm.StartedAt), utcDate(run.StartedAt)))
	}
	if arm.Terminal != "" && run.Terminal != "" && arm.Terminal != run.Terminal {
		problems = append(problems, fmt.Sprintf(
			"%s: the campaign index says it %s but its signed records say %s",
			where, arm.Terminal, run.Terminal))
	}
	return problems
}

// EvaluateCampaignIndex is the whole product decision: authenticate the
// population, then apply the frozen rule to it. A campaign that cannot be
// authenticated does not reach the arithmetic, because a ratio over
// unauthenticated rows answers a question nobody asked.
func EvaluateCampaignIndex(index CampaignIndex, loader CampaignLoader, authorityKeys []string, authority string, release CampaignRelease) ([]GateResult, []string) {
	pairs, problems := LoadCampaign(index, loader, authorityKeys, authority)
	sort.Strings(problems)
	if len(problems) > 0 {
		// Authentication first, and alone. A campaign whose rows are not
		// authenticated reports exactly that: reporting further gates
		// alongside it — passing or failing — would invite reading one of them
		// as a result over a population that does not exist.
		return []GateResult{{
			Name: "campaign:authenticated-population", Scope: ScopeCampaign,
			Required: "every row an eligible verifier verdict, every arm an authorised Stage-1 manifest, every pair equal outside the allowed-difference matrix",
			Observed: fmt.Sprintf("%d problem(s)", len(problems)),
			Detail:   strings.Join(problems, "; "),
		}}, problems
	}
	binding := releaseBindingGate(index, loader, release)
	authenticated := GateResult{
		Name: "campaign:authenticated-population", Scope: ScopeCampaign,
		Required: "every row an eligible verifier verdict, every arm an authorised Stage-1 manifest, every pair equal outside the allowed-difference matrix",
		Observed: fmt.Sprintf("%d pair(s) authenticated", len(pairs)),
		Pass:     true, Population: len(pairs),
	}
	return append([]GateResult{authenticated, binding}, EvaluateCampaign(pairs)...), nil
}

// releaseBindingGate decides whether this campaign authorises THIS delivery.
//
// It fails closed in both directions. With no expected release identity
// supplied it does not pass: a campaign evaluated against no particular
// delivery cannot authorise one, and treating "nothing was asked" as "anything
// is allowed" is exactly how historical evidence came to authorise later tags.
// With one supplied, every candidate arm's reviewed tip and release ref must
// be that commit, and its delivered binary must be one of the artifacts whose
// actual bytes were hashed here.
//
// A digest supplied as a STRING is deliberately not accepted anywhere in this
// path. The release gate previously read a committed declaration and passed it
// through, so the evaluator compared its manifests to a claim about the
// delivery instead of to the delivery: a matching declaration could authorise
// whatever was built afterwards.
func releaseBindingGate(index CampaignIndex, loader CampaignLoader, release CampaignRelease) GateResult {
	g := GateResult{
		Name: "campaign:release-binding", Scope: ScopeCampaign,
		Required: "every pair's CANDIDATE arm reviewed, was released from, and delivered one of the exact artifacts about to be published, all hashed from their actual bytes",
	}
	if !release.Bound() {
		g.Observed = "no expected release identity was supplied"
		g.Detail = "a campaign is evidence for the delivery it was produced for; without the tagged SHA and the exact built artifact this campaign cannot authorise any release"
		return g
	}
	if err := requireFullSHA("the expected release SHA", release.SHA); err != nil {
		g.Observed = release.SHA
		g.Detail = err.Error()
		return g
	}
	if release.Manifest == nil || len(release.Manifest.Assets) == 0 {
		g.Observed = release.SHA
		g.Detail = "no publish set was supplied; a campaign that bound a binary nobody compared authorises an asset it never saw"
		return g
	}
	assets := release.Manifest.UploadNames()
	// The CANDIDATE arm is the delivery. A pair's baseline is the reference
	// testbucket it was measured against, and a different source tip and
	// binary there are the enumerated permitted difference the whole campaign
	// exists to test — so requiring the baseline to be the released commit
	// would refuse every genuine campaign.
	var problems []string
	var bound []string
	arms := 0
	for i, ref := range index.Pairs {
		if strings.TrimSpace(ref.Candidate.Stage1Path) == "" {
			continue
		}
		m, err := loader.Manifest(ref.Candidate.Stage1Path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("pair %d candidate: %v", i, err))
			continue
		}
		arms++
		where := fmt.Sprintf("pair %d candidate", i)
		if m.Source.ReleaseRefSHA != release.SHA {
			problems = append(problems, fmt.Sprintf("%s was authorised for release ref %s, not the %s being released", where, m.Source.ReleaseRefSHA, release.SHA))
		}
		if m.Source.ReviewTip != release.SHA {
			problems = append(problems, fmt.Sprintf("%s reviewed tip %s, not the %s being released", where, m.Source.ReviewTip, release.SHA))
		}
		// The campaign ran on ONE platform, so its delivered binary must be
		// reachable from something this release actually publishes: either an
		// asset itself, or a file inside one. A raw goreleaser intermediate
		// that nobody uploads does not count, which is precisely the hole this
		// closes.
		asset, member, ok := release.Manifest.Locate(m.Source.BinaryDigest)
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s delivered binary %s, which is not published by, or contained in, any of the %d asset(s) being released (%s)",
				where, m.Source.BinaryDigest, len(assets), strings.Join(assets, ", ")))
		case member != "":
			bound = append(bound, fmt.Sprintf("%s: %s is published inside %s as %s", where, m.Source.BinaryDigest, asset, member))
		default:
			bound = append(bound, fmt.Sprintf("%s: %s is published as %s", where, m.Source.BinaryDigest, asset))
		}
	}
	if arms == 0 {
		g.Observed = "no candidate arm named a Stage-1 manifest"
		g.Detail = "there is nothing in this campaign to bind to the release"
		return g
	}
	g.Population = arms
	if len(problems) > 0 {
		sort.Strings(problems)
		g.Observed = fmt.Sprintf("%d candidate arm(s) bound to another delivery", len(problems))
		g.Detail = strings.Join(problems, "; ")
		return g
	}
	sort.Strings(bound)
	g.Observed = fmt.Sprintf("all %d candidate arm(s) authorised %s", arms, release.SHA)
	g.Detail = fmt.Sprintf("%d published asset(s), verified against their actual bytes: %s. %s",
		len(assets), strings.Join(assets, ", "), strings.Join(bound, "; "))
	g.Pass = true
	return g
}
