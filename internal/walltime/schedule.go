package walltime

import (
	"fmt"
	"sort"
	"strings"
)

// ScheduleKind is the frozen campaign schedule's schema identity.
const ScheduleKind = "tb.walltime.schedule/v1"

// ScheduledPair is one predeclared B/C pair, in the order the authority froze.
type ScheduledPair struct {
	// Index is the pair's position in the campaign, from zero.
	Index int `json:"index"`
	// BaselineRun and CandidateRun are the run identities this pair's two arms
	// must carry. Naming them is what makes ORDER binding: a schedule that
	// froze only the count would be satisfied by any five pairs, including
	// five chosen after the fact from a larger set of attempts.
	BaselineRun  string `json:"baseline_run"`
	CandidateRun string `json:"candidate_run"`
	// Date is the UTC calendar date (YYYY-MM-DD) the pair is scheduled for.
	// The contract requires at least three distinct dates, and choosing them
	// afterwards from whatever ran is not a randomised schedule.
	Date string `json:"date"`
}

// CampaignSchedule is the authority-frozen campaign identity and pair order.
//
// The contract requires the Stage-1 artifact to bind "campaign/pair order"
// before planning and role assignment, and to freeze the pair order before the
// first candidate run begins. Without it, pair membership, arm roles and the
// order runs are attempted in are all decided outside the immutable authority
// artifact — which leaves the selection control that the randomisation is
// there to provide entirely unenforced. Five pairs picked from ten attempts
// after the fact satisfy every other gate in this package.
type CampaignSchedule struct {
	Kind       string `json:"kind"`
	CampaignID string `json:"campaign_id"`
	// Seed is the randomisation seed the order was drawn with. It is recorded
	// so the draw is reproducible by a third party rather than asserted.
	Seed  int64           `json:"seed"`
	Pairs []ScheduledPair `json:"pairs"`
}

// OrderDigest is the canonical identity of the ORDER alone.
//
// It is separate from the schedule's own digest so a campaign index can cite
// the order it is executing without having to carry the whole schedule, and so
// that a reordering is a different digest even when every pair is the same.
func (s CampaignSchedule) OrderDigest() (Digest, error) {
	type entry struct {
		Index        int    `json:"index"`
		BaselineRun  string `json:"baseline_run"`
		CandidateRun string `json:"candidate_run"`
		Date         string `json:"date"`
	}
	out := make([]entry, 0, len(s.Pairs))
	for _, p := range s.Pairs {
		out = append(out, entry{p.Index, p.BaselineRun, p.CandidateRun, p.Date})
	}
	return DigestJSON(struct {
		CampaignID string  `json:"campaign_id"`
		Seed       int64   `json:"seed"`
		Pairs      []entry `json:"pairs"`
	}{s.CampaignID, s.Seed, out})
}

// Validate refuses a schedule that cannot bind an order.
func (s CampaignSchedule) Validate() error {
	if s.Kind != ScheduleKind {
		return fmt.Errorf("campaign schedule kind %q, want %q", s.Kind, ScheduleKind)
	}
	if strings.TrimSpace(s.CampaignID) == "" {
		return fmt.Errorf("campaign schedule names no campaign")
	}
	if len(s.Pairs) != CampaignPairs {
		return fmt.Errorf("campaign schedule holds %d pair(s), and the frozen campaign is %d", len(s.Pairs), CampaignPairs)
	}
	dates := map[string]bool{}
	runs := map[string]string{}
	for i, p := range s.Pairs {
		if p.Index != i {
			return fmt.Errorf("campaign schedule pair %d is indexed %d; the order is the artifact, so it is stored in it", i, p.Index)
		}
		if err := requireSet(map[string]string{
			fmt.Sprintf("pair %d's baseline run", i):  p.BaselineRun,
			fmt.Sprintf("pair %d's candidate run", i): p.CandidateRun,
			fmt.Sprintf("pair %d's UTC date", i):      p.Date,
		}); err != nil {
			return fmt.Errorf("campaign schedule %w", err)
		}
		// One run cannot be two arms, and no run may appear twice: reusing a
		// run across pairs would make the pairs dependent samples, which the
		// contract's five independent max-per-run observations exclude.
		for role, id := range map[string]string{"baseline": p.BaselineRun, "candidate": p.CandidateRun} {
			if prev, seen := runs[id]; seen {
				return fmt.Errorf("campaign schedule uses run %q as pair %d's %s and already at %s; pairs would not be independent", id, i, role, prev)
			}
			runs[id] = fmt.Sprintf("pair %d", i)
		}
		dates[p.Date] = true
	}
	if len(dates) < CampaignDates {
		return fmt.Errorf("campaign schedule spans %d UTC date(s), and the frozen campaign needs at least %d", len(dates), CampaignDates)
	}
	return nil
}

// bindOrder checks that a campaign index executes exactly the frozen schedule,
// in the frozen order.
//
// Order is compared positionally, not as a set. A campaign that ran the same
// five pairs in a different sequence ran a different experiment: the whole
// point of freezing a randomised order before the first candidate run is that
// the sequence cannot be chosen once the results are visible.
func bindOrder(index CampaignIndex, schedule CampaignSchedule) []string {
	var problems []string
	if schedule.CampaignID != index.CampaignID {
		problems = append(problems, fmt.Sprintf(
			"the frozen schedule is for campaign %q but this index is for %q", schedule.CampaignID, index.CampaignID))
	}
	if d, err := schedule.OrderDigest(); err != nil {
		problems = append(problems, fmt.Sprintf("campaign schedule: %v", err))
	} else if index.OrderDigest == "" {
		problems = append(problems, "the campaign index names no frozen pair order, so nothing says these five pairs are the five that were predeclared")
	} else if index.OrderDigest != d {
		problems = append(problems, fmt.Sprintf(
			"the campaign index executes order %s but the authority froze %s", index.OrderDigest, d))
	}
	if len(index.Pairs) != len(schedule.Pairs) {
		problems = append(problems, fmt.Sprintf(
			"the campaign index holds %d pair(s) and the frozen schedule %d", len(index.Pairs), len(schedule.Pairs)))
		return problems
	}
	for i, ref := range index.Pairs {
		want := schedule.Pairs[i]
		if ref.Baseline.RunID != want.BaselineRun {
			problems = append(problems, fmt.Sprintf(
				"pair %d's baseline is run %q but the frozen order predeclared %q", i, ref.Baseline.RunID, want.BaselineRun))
		}
		if ref.Candidate.RunID != want.CandidateRun {
			problems = append(problems, fmt.Sprintf(
				"pair %d's candidate is run %q but the frozen order predeclared %q", i, ref.Candidate.RunID, want.CandidateRun))
		}
		for role, at := range map[string]string{
			"baseline":  ref.Baseline.StartedAt,
			"candidate": ref.Candidate.StartedAt,
		} {
			if at == "" {
				continue
			}
			if date := utcDate(at); date != "" && date != want.Date {
				problems = append(problems, fmt.Sprintf(
					"pair %d's %s ran on %s but the frozen order scheduled %s", i, role, date, want.Date))
			}
		}
	}
	return problems
}

// utcDate is the calendar date part of an RFC3339 instant, or "" if it cannot
// be read. An unreadable instant is reported by the population gate, not here.
func utcDate(instant string) string {
	t, err := parseInstant(instant)
	if err != nil {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

// SortedDates is the schedule's distinct UTC dates, for reporting.
func (s CampaignSchedule) SortedDates() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range s.Pairs {
		if !seen[p.Date] {
			seen[p.Date] = true
			out = append(out, p.Date)
		}
	}
	sort.Strings(out)
	return out
}
