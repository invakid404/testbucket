package walltime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// VerifiedCampaignConfig is the frozen §19.9a document AFTER its bytes have
// been checked against the caller-supplied expected digest (§19.9c-1).
//
// The type exists so an unverified config cannot reach append by construction:
// ingest takes this, and the only way to obtain one is through
// VerifyCampaignConfig, which fails before `ingest` runs.
type VerifiedCampaignConfig struct {
	CampaignID string
	// WorkloadCommit and ExcludedOrchestrationCommits materialize the two
	// foreign-row predicates AT APPEND, so no fit at any later time needs a
	// second predicate (R13-D1).
	WorkloadCommit               string
	ExcludedOrchestrationCommits []string
	digest                       string
}

// Digest returns the verified digest of the config bytes.
func (c VerifiedCampaignConfig) Digest() string { return c.digest }

// campaignConfigDocument is the subset of §19.9a this hop reads.
type campaignConfigDocument struct {
	Schema   string `json:"schema"`
	Manifest struct {
		CampaignID                   string   `json:"campaign_id"`
		WorkloadCommit               string   `json:"workload_commit"`
		ExcludedOrchestrationCommits []string `json:"excluded_orchestration_commits"`
	} `json:"manifest"`
}

// VerifyCampaignConfig is §19.9c-1's verify step. It recomputes SHA-256 over
// the materialized bytes and refuses unless it equals the caller's expected
// digest — BEFORE `ingest` runs, so no row is appended, no selected identity or
// model byte changes, and the fitter is never called.
//
// The two perturbations §22 test 61 requires are independent: bytes changed
// with the expected digest held, and the expected digest changed with the bytes
// held, each fail here on their own.
func VerifyCampaignConfig(content []byte, expectedDigest string) (VerifiedCampaignConfig, error) {
	sum := sha256.Sum256(content)
	got := hex.EncodeToString(sum[:])
	if expectedDigest == "" {
		return VerifiedCampaignConfig{}, fmt.Errorf("campaign config: no expected digest supplied; §19.9c-1 verifies before ingest runs")
	}
	if got != expectedDigest {
		return VerifiedCampaignConfig{}, fmt.Errorf("campaign config: materialized bytes digest %s does not equal the expected %s; refused before ingest",
			got, expectedDigest)
	}
	var doc campaignConfigDocument
	if err := json.Unmarshal(content, &doc); err != nil {
		return VerifiedCampaignConfig{}, fmt.Errorf("campaign config: parse: %w", err)
	}
	if doc.Manifest.CampaignID == "" {
		return VerifiedCampaignConfig{}, fmt.Errorf("campaign config: manifest.campaign_id is empty")
	}
	return VerifiedCampaignConfig{
		CampaignID:                   doc.Manifest.CampaignID,
		WorkloadCommit:               doc.Manifest.WorkloadCommit,
		ExcludedOrchestrationCommits: doc.Manifest.ExcludedOrchestrationCommits,
		digest:                       got,
	}, nil
}

// IngestDecision is what the loop decided about one observation.
type IngestDecision struct {
	BucketName string
	Accepted   bool
	// Trainable is written ONCE AT APPEND from the row's own bytes plus the
	// supplied config, which is what lets the fitter select on one predicate.
	Trainable bool
	Reason    string
}

// IngestResult is the outcome of one `ingest --wall-observations` invocation.
type IngestResult struct {
	Decisions []IngestDecision
	// Appended counts rows that entered the ring.
	Appended int
	// SelectionChanged reports whether the append changed the SELECTED
	// trainable population — the one condition under which §14.2 permits a
	// refit.
	SelectionChanged bool
	// FitterCalls is instrumentation: §14.2 requires it to stay ZERO when the
	// selected population did not change.
	FitterCalls int
}

// ObservationSource is one observation file as read from the artifact
// directory, kept with its path so a rejection can name it.
type ObservationSource struct {
	Path string
	Obs  Observation
}

// ReadWallObservations reads every observation document in dir. It is the
// same-run artifact download half of §14.1: R54 uploaded a wall artifact that
// nothing downloaded, which is the open end of the loop ID-14 closes.
func ReadWallObservations(dir string) ([]ObservationSource, error) {
	// RECURSIVE, deliberately.
	//
	// This listed one level and skipped every directory. An artifact download
	// that lands each bucket's document under its own artifact-named
	// subdirectory then yielded NOTHING, and the caller reported "0 of 0
	// observations", exited 0 and saved the reporter update — learning
	// silently from no rows, which is indistinguishable from having no rows to
	// learn from.
	//
	// The download layout is fixed elsewhere, and this is fixed here as well
	// on purpose: a reader that only works for one layout makes the next
	// layout change a silent regression rather than a loud one.
	var out []ObservationSource
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(d.Name()) != ".json" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		var obs Observation
		if err := json.Unmarshal(b, &obs); err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
		out = append(out, ObservationSource{Path: p, Obs: obs})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read wall observations: %w", err)
	}
	// A deterministic order, so the accept/reject table is reproducible.
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// TrainableAtAppend decides §15.1b's single carrier of every in-CI exclusion.
//
// It is evaluated ONCE, at append, from the row's own bytes plus the supplied
// config. Every branch below therefore materializes its exclusion into the
// stored `trainable` value instead of leaving a predicate for the fitter to
// re-derive later.
func TrainableAtAppend(obs Observation, cfg *VerifiedCampaignConfig) (trainable bool, accept bool, reason string) {
	prof, err := obs.Profile.Parse()
	if err != nil {
		return false, false, "profile does not parse"
	}

	// AN UNSCORABLE CLOCK DOMAIN NEVER TRAINS.
	//
	// §15.1b makes `trainable` the single carrier of every in-CI exclusion,
	// written once at append from the row's own bytes — and the clock domain is
	// one of the row's own bytes. The non-Linux backend reads the host REALTIME
	// clock under an honest name, and nothing kept its measurements out of the
	// fit: assembly dropped clock_id, the durations advance, and the fixed boot
	// marker matches itself, so every ordering and boot check passed. An NTP step
	// moves that clock, which is the whole reason the backend calls itself
	// unscorable.
	//
	// The row is RETAINED as a diagnostic rather than refused: a developer run
	// that exercises the real endpoint ordering is worth recording, and refusing
	// it would delete the evidence instead of labelling it. A SCORED row on such a
	// clock is a different thing and QC8 refuses it outright.
	unscorableClock := obs.ClockID != ClockMonotonic

	if cfg == nil {
		// The ordinary CI path. §22 test 61: with NO config supplied, a
		// campaign_id present, or a scored profile, is a MISWIRING and the row
		// is refused BEFORE append — a scored run is required to carry a
		// campaign identity, so its absence cannot be treated as an ordinary
		// row.
		if obs.CampaignID != "" {
			return false, false, "campaign_id present with no verified config supplied: refused before append"
		}
		if prof.Scored {
			return false, false, "profile.scored is true with no verified config supplied: refused before append"
		}
		if unscorableClock {
			return false, true, fmt.Sprintf("clock domain %q may not delimit a scored interval: retained diagnostic-only, never trains", obs.ClockID)
		}
		return true, true, "ordinary unscored CI row"
	}

	// A config IS supplied.
	switch obs.CampaignID {
	case "":
		return false, false, "campaign_id missing under a supplied config: fail-closed"
	case cfg.CampaignID:
		// Matching: appended and RETAINED as a diagnostic, not rejected.
		return false, true, "campaign or pilot row: retained diagnostic-only, never trains"
	default:
		return false, false, fmt.Sprintf("campaign_id %q does not match the verified config: fail-closed", obs.CampaignID)
	}
}

// ForeignAtAppend applies §15.1b's two foreign-row predicates, also at append.
// A foreign workload or a foreign orchestration commit marks the row
// non-trainable; it is never a second fit-time predicate.
func ForeignAtAppend(obs Observation, cfg *VerifiedCampaignConfig) (foreign bool, reason string) {
	if cfg == nil {
		return false, ""
	}
	if cfg.WorkloadCommit != "" && obs.WorkloadCommit != cfg.WorkloadCommit {
		return true, "foreign workload: workload_commit differs from the manifest's"
	}
	for _, ex := range cfg.ExcludedOrchestrationCommits {
		if obs.HeadSHA == ex {
			return true, "foreign orchestration: head_sha is in excluded_orchestration_commits"
		}
	}
	return false, ""
}

// RingStore is the slice of the store the ingest loop touches. It is an
// interface so the loop can be driven with an instrumented selector and an
// instrumented fitter, which is how §22 test 61 asserts the fitter is not
// invoked at all.
type RingStore interface {
	// SelectedIdentities returns a stable identity per row of the SELECTED
	// trainable population, in selection order.
	SelectedIdentities() []string
	// Append adds a row with its append-time trainable marking.
	Append(obs Observation, trainable bool) error
}

// Fitter is the model fit, injected so its call count is observable.
type Fitter func() error

// IngestWallObservations is contract §14.2's CLI hop: read the observations,
// apply §7.1, append qualifying rows under §15.1b, and refit under §6.8 IF AND
// ONLY IF the append changed the selected trainable population.
//
// The conditional refit is the point. An append that does not change the
// selected population does not invoke the fitter AT ALL: a trainable:false
// diagnostic never enters the selected population and, because §15.1b bounds
// the two retention classes independently, never removes a row from it. The
// stored model record is then preserved BYTE FOR BYTE, `fitted_at` included —
// not recomputed to the same value, but not written.
//
// An earlier revision made the refit unconditional, so a valid post-freeze
// diagnostic append re-invoked the fitter over an unchanged population and
// could advance `fitted_at`, which model_parameters_digest does not cover,
// while §19.5 requires the model frozen before the first authenticated
// campaign start. Consumption, displacement and invocation are the three paths
// by which a stored row could reach a fit, and this closes the third.
func IngestWallObservations(sources []ObservationSource, store RingStore, cfg *VerifiedCampaignConfig, fit Fitter) (IngestResult, error) {
	before := append([]string(nil), store.SelectedIdentities()...)
	var res IngestResult

	// QC11 IS PRECLASSIFIED OVER THE WHOLE BATCH, before anything is appended.
	//
	// §7.1 permits exactly one observation per execution key and says a
	// duplicate rejects ALL rows for that key. Deciding it inside the loop
	// made it FIRST-WINS: the first arrival was appended and refitted, and only
	// the second was refused — so which of two contradictory measurements
	// trained the model was decided by directory order. A key that appears
	// more than once is unresolvable by construction, and every row carrying
	// it is refused.
	//
	// Only sources that pass validation are counted, so a malformed document
	// is reported as malformed rather than as one half of a collision on the
	// empty key.
	occurrences := map[[4]string]int{}
	for _, s := range sources {
		if s.Obs.Validate() != nil {
			continue
		}
		occurrences[ObservationKey(s.Obs)]++
	}

	for _, s := range sources {
		name := s.Obs.BucketName
		if name == "" {
			name = s.Path
		}

		if err := s.Obs.Validate(); err != nil {
			res.Decisions = append(res.Decisions, IngestDecision{
				BucketName: name, Accepted: false, Reason: err.Error(),
			})
			continue
		}

		if n := occurrences[ObservationKey(s.Obs)]; n > 1 {
			res.Decisions = append(res.Decisions, IngestDecision{
				BucketName: name, Accepted: false,
				Reason: fmt.Sprintf("QC11: execution key %v appears %d times in this batch; a duplicate rejects ALL rows for the key",
					ObservationKey(s.Obs), n),
			})
			continue
		}

		trainable, accept, reason := TrainableAtAppend(s.Obs, cfg)
		if !accept {
			res.Decisions = append(res.Decisions, IngestDecision{
				BucketName: name, Accepted: false, Reason: reason,
			})
			continue
		}
		// The foreign predicates are materialized here too, never at fit time.
		if foreign, why := ForeignAtAppend(s.Obs, cfg); foreign {
			trainable = false
			reason = why
		}

		if err := store.Append(s.Obs, trainable); err != nil {
			res.Decisions = append(res.Decisions, IngestDecision{
				BucketName: name, Accepted: false, Reason: err.Error(),
			})
			continue
		}
		res.Appended++
		res.Decisions = append(res.Decisions, IngestDecision{
			BucketName: name, Accepted: true, Trainable: trainable, Reason: reason,
		})
	}

	after := store.SelectedIdentities()
	res.SelectionChanged = !sameIdentities(before, after)
	if res.SelectionChanged && fit != nil {
		res.FitterCalls++
		if err := fit(); err != nil {
			return res, err
		}
	}
	return res, nil
}

func sameIdentities(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
