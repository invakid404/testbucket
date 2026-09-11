package core

import (
	"fmt"
	"reflect"
	"testing"
)

// ringRow builds a valid row from distinct-run identity, so a fixture can vary
// one axis at a time.
func ringRow(runID string, bucket int, stamp string) WallRingRow {
	return WallRingRow{
		Repository:             "owner/name",
		JobID:                  "job-" + runID,
		HeadSHA:                "head-" + runID,
		CandidateSHA:           "cand-1",
		WorkloadCommit:         "work-1",
		RunID:                  runID,
		RunAttempt:             "1",
		ObservedStartRealtime:  stamp,
		Trainable:              true,
		BucketIndex:            bucket,
		PlanDigest:             "sha256:plan-" + runID,
		StoreSHA256:            "sha256:store",
		ComparabilityKeyDigest: "sha256:key",
		ReporterSumNs:          10_000_000_000,
		IAnyWholeFile:          1,
		SliceCount:             0,
		ElapsedNs:              20_000_000_000,
		Terminal:               "passed",
	}
}

func newWall() *WallObject {
	v := WallModelVersion
	return &WallObject{
		ModelVersion:           &v,
		ComparabilityKeyDigest: "sha256:key",
		Status:                 WallStatusInsufficient,
		FailureSubtype:         SubtypeRowsBelowMinimum,
		Observations:           []WallRingRow{},
	}
}

func retainedIDs(w *WallObject) []string {
	out := make([]string, 0, len(w.Observations))
	for _, r := range w.Observations {
		out = append(out, r.RunID)
	}
	return out
}

// TestWallHistoryRecencyEvictionAndThreeIdentities is §22 test 52
// (SR-2/S-6/R8-D3) and the acceptance test the registry names for ID-17.
//
// Per §22's split, this is the RING / RECENCY half: it is driven over the
// decoded ring rows ALONE, with no manifest and no API access, and selects by
// `trainable` only. The campaign-validation half is operator-side and lives
// with the campaign validator.
func TestWallHistoryRecencyEvictionAndThreeIdentities(t *testing.T) {
	t.Run("the recency key is reconstructible from the decoded row alone", func(t *testing.T) {
		r := ringRow("run-1", 3, "2026-09-01T00:00:00Z")
		k := RingRecencyKeyOf(r)
		if k.ObservedStartRealtime != r.ObservedStartRealtime {
			t.Fatal("the recency stamp is not the row's own observed_start_realtime")
		}
		// Every component of intrinsic_id is a separately addressable field.
		want := [6]string{"owner/name", "run-1", "1", "job-run-1", "3", "sha256:plan-run-1"}
		if k.Intrinsic != want {
			t.Fatalf("intrinsic_id = %v, want %v", k.Intrinsic, want)
		}
		// No observation is present: the key came from the row.
	})

	t.Run("the three identities are separate and none is derivable", func(t *testing.T) {
		r := ringRow("run-1", 0, "2026-09-01T00:00:00Z")
		if r.HeadSHA == r.CandidateSHA || r.CandidateSHA == r.WorkloadCommit || r.HeadSHA == r.WorkloadCommit {
			t.Fatal("the fixture collapses two identities; they must be independently addressable")
		}
		// A row that collapses them is rejected by QC15, with the plan declaring
		// no same-repository workload.
		bad := r
		bad.CandidateSHA = ""
		if err := QC15(bad, false); err == nil {
			t.Fatal("QC15 accepted a row missing candidate_sha")
		}
		bad = r
		bad.WorkloadCommit = ""
		if err := QC15(bad, false); err == nil {
			t.Fatal("QC15 accepted a row missing workload_commit")
		}
		if err := QC15(r, false); err != nil {
			t.Fatalf("QC15 rejected a well-formed row: %v", err)
		}

		// THE PLAN-BOUND EXCEPTION, at the ring gate as at the qualifier. The
		// declaration is the plan's, so it arrives as an argument; a row cannot
		// grant it to itself here any more than it can there.
		collapsed := r
		collapsed.CandidateSHA, collapsed.WorkloadCommit = r.HeadSHA, r.HeadSHA
		if err := QC15(collapsed, false); err == nil {
			t.Fatal("QC15 accepted a collapsed row with no declaration")
		}
		if err := QC15(collapsed, true); err != nil {
			t.Fatalf("QC15 rejected a declared same-repository row, so the dogfood can never reach the ring: %v", err)
		}
		// The exception admits EQUALITY only, never an absent identity.
		missing := collapsed
		missing.CandidateSHA = ""
		if err := QC15(missing, true); err == nil {
			t.Fatal("the carve-out excused an absent candidate_sha")
		}
	})

	t.Run("241 equal-timestamp rows retain the same population from either order", func(t *testing.T) {
		// §15.1b's counterexample fixture: W+1 valid rows from DISTINCT runs
		// sharing one parseable observed_start_realtime. Under the superseded
		// (stamp, ingest_seq) key, forward ingest evicts the first and reverse
		// ingest evicts the last, so retention depended on arrival order.
		const stamp = "2026-09-01T00:00:00Z"
		rows := make([]WallRingRow, 0, W+1)
		for i := 0; i <= W; i++ {
			rows = append(rows, ringRow(fmt.Sprintf("run-%04d", i), 0, stamp))
		}

		forward := newWall()
		for i, r := range rows {
			r.IngestSeq = int64(i)
			forward.AppendRow(r)
		}
		reverse := newWall()
		for i := len(rows) - 1; i >= 0; i-- {
			r := rows[i]
			r.IngestSeq = int64(len(rows) - i)
			reverse.AppendRow(r)
		}

		if len(forward.Observations) != W || len(reverse.Observations) != W {
			t.Fatalf("ring sizes = %d and %d, want %d", len(forward.Observations), len(reverse.Observations), W)
		}
		fw, rv := retainedIDs(forward), retainedIDs(reverse)
		if !reflect.DeepEqual(fw, rv) {
			t.Fatal("the two ingest orders retained different populations; retention depends on arrival order")
		}
		// And the evicted row is the same one: the oldest under the key, which
		// for equal stamps is the smallest intrinsic_id.
		evicted := map[string]bool{}
		for _, r := range rows {
			evicted[r.RunID] = true
		}
		for _, id := range fw {
			delete(evicted, id)
		}
		if len(evicted) != 1 {
			t.Fatalf("expected exactly one evicted row, got %d", len(evicted))
		}
		if !evicted["run-0000"] {
			t.Fatalf("evicted %v, want the smallest intrinsic_id (run-0000)", evicted)
		}
	})

	t.Run("ingest_seq is read by nothing", func(t *testing.T) {
		// Same rows, wildly different append counters: identical retention.
		const stamp = "2026-09-01T00:00:00Z"
		build := func(seqOf func(i int) int64) []string {
			w := newWall()
			for i := 0; i <= W; i++ {
				r := ringRow(fmt.Sprintf("run-%04d", i), 0, stamp)
				r.IngestSeq = seqOf(i)
				w.AppendRow(r)
			}
			return retainedIDs(w)
		}
		a := build(func(i int) int64 { return int64(i) })
		b := build(func(i int) int64 { return int64(10_000 - i) })
		if !reflect.DeepEqual(a, b) {
			t.Fatal("retention changed with ingest_seq; it must be read by nothing")
		}
	})

	t.Run("the summation sort is applied only after selection", func(t *testing.T) {
		// §6.8's sort is a summation order, not a recency definition. Selection
		// happens first and the two are separately observable.
		w := newWall()
		for i := 0; i < 5; i++ {
			w.AppendRow(ringRow(fmt.Sprintf("run-%d", i), i, fmt.Sprintf("2026-09-0%dT00:00:00Z", i+1)))
		}
		sel := w.TrainableRows()
		sorted := SummationSort(sel)
		if len(sorted) != len(sel) {
			t.Fatal("the summation sort changed the population")
		}
		// Shuffling the selected rows yields identical sorted bytes.
		shuffled := append([]WallRingRow(nil), sel...)
		for i, j := 0, len(shuffled)-1; i < j; i, j = i+1, j-1 {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		}
		if !reflect.DeepEqual(SummationSort(shuffled), sorted) {
			t.Fatal("the summation sort is not order-invariant")
		}
	})

	t.Run("selection evaluates trainable and nothing else", func(t *testing.T) {
		w := newWall()
		// A row at the candidate SHA outside any campaign window is RETAINED
		// and trainable, so a warm corpus built by the candidate binary is
		// reachable.
		warm := ringRow("run-warm", 0, "2020-01-01T00:00:00Z")
		warm.Trainable = true
		w.AppendRow(warm)
		diag := ringRow("run-campaign", 0, "2026-09-01T00:00:00Z")
		diag.Trainable = false
		w.AppendRow(diag)

		got := w.TrainableRows()
		if len(got) != 1 || got[0].RunID != "run-warm" {
			t.Fatalf("trainable selection = %v, want only run-warm", retainedRunIDs(got))
		}
		if len(w.Observations) != 2 {
			t.Fatal("the non-trainable row must be retained, not dropped")
		}
	})

	t.Run("each trainable class is bounded independently at W", func(t *testing.T) {
		// §15.1a bounds each `trainable` class at W INDEPENDENTLY. The ring
		// used to apply one combined cap, which had two consequences the
		// contract does not permit: W trainable rows and a single diagnostic
		// row could not coexist, so the accepted total could never reach 2W;
		// and a diagnostic append pushed the population over the cap and
		// evicted from the other class, letting an untrainable row displace
		// the corpus a fit reads.
		w := newWall()
		for i := 0; i < W; i++ {
			w.AppendRow(ringRow(fmt.Sprintf("t-%04d", i), 0, "2026-09-01T00:00:00Z"))
		}
		before := trainableIDs(w)

		for i := 0; i < 50; i++ {
			r := ringRow(fmt.Sprintf("d-%04d", i), 0, "2026-09-02T00:00:00Z")
			r.Trainable = false
			w.AppendRow(r)
		}
		if after := trainableIDs(w); !reflect.DeepEqual(before, after) {
			t.Fatal("appending non-trainable rows evicted trainable rows; the corpus must not be displaceable")
		}
		if got, want := len(w.Observations), W+50; got != want {
			t.Fatalf("ring holds %d rows, want %d: the two classes are bounded separately", got, want)
		}

		// And the diagnostic class has its own W, reached without touching the
		// corpus.
		for i := 50; i < W+25; i++ {
			r := ringRow(fmt.Sprintf("d-%04d", i), 0, "2026-09-03T00:00:00Z")
			r.Trainable = false
			w.AppendRow(r)
		}
		if after := trainableIDs(w); !reflect.DeepEqual(before, after) {
			t.Fatal("overflowing the diagnostic class evicted trainable rows")
		}
		trainable, diagnostic := 0, 0
		for _, r := range w.Observations {
			if r.Trainable {
				trainable++
			} else {
				diagnostic++
			}
		}
		if trainable != W || diagnostic != W {
			t.Fatalf("classes hold %d trainable and %d diagnostic rows, want %d each", trainable, diagnostic, W)
		}
	})

	t.Run("QC16 makes the recency key a total order", func(t *testing.T) {
		rows := []WallRingRow{ringRow("run-1", 0, "2026-09-01T00:00:00Z")}
		dup := ringRow("run-1", 0, "2026-09-01T00:00:00Z")
		if err := QC16(rows, dup); err == nil {
			t.Fatal("QC16 accepted a duplicate intrinsic_id")
		}
		other := ringRow("run-1", 1, "2026-09-01T00:00:00Z")
		if err := QC16(rows, other); err != nil {
			t.Fatalf("QC16 rejected a distinct bucket_index: %v", err)
		}
	})
}

func retainedRunIDs(rows []WallRingRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.RunID)
	}
	return out
}

// trainableIDs is the fit population's run ids, which is the sequence a
// diagnostic append must never disturb.
func trainableIDs(w *WallObject) []string {
	out := make([]string, 0, len(w.Observations))
	for _, r := range w.Observations {
		if r.Trainable {
			out = append(out, r.RunID)
		}
	}
	return out
}

// TestRecencyOrdersInstantsNotText is R05's control.
//
// §15.1b orders the recency key's first element as an RFC 3339 UTC INSTANT, and
// `Less` compared the text. Assembly writes the stamp with time.RFC3339Nano,
// whose fraction is VARIABLE WIDTH — trailing zeros are dropped — so two stamps
// 10ms apart can sort in the opposite order, and two spellings of the same
// instant can sort as different ones. At W the ring evicts on this order, so the
// wrong measurement was discarded.
func TestRecencyOrdersInstantsNotText(t *testing.T) {
	key := func(stamp, job string) RingRecencyKey {
		return RingRecencyKey{
			ObservedStartRealtime: stamp,
			Intrinsic:             [6]string{"owner/name", "run-1", "1", job, "0", "sha256:plan"},
		}
	}

	t.Run("differing fractional precision orders by time", func(t *testing.T) {
		// THE REPORT'S OWN EXAMPLE. `.1` is 100ms and `.11` is 110ms, so the
		// first is older — but as text `.11Z` sorts before `.1Z`, because '1'
		// precedes 'Z'.
		older := key("2026-09-11T12:00:00.1Z", "job-a")
		newer := key("2026-09-11T12:00:00.11Z", "job-b")
		if !older.Less(newer) {
			t.Error("100ms did not sort before 110ms; the key is comparing text")
		}
		if newer.Less(older) {
			t.Error("110ms sorted before 100ms")
		}
	})

	t.Run("more precision on the older instant still orders by time", func(t *testing.T) {
		older := key("2026-09-11T12:00:00.000001Z", "job-a")
		newer := key("2026-09-11T12:00:00.1Z", "job-b")
		if !older.Less(newer) {
			t.Error("1µs did not sort before 100ms")
		}
	})

	t.Run("equivalent encodings are the same instant", func(t *testing.T) {
		// Same instant, three spellings. The order must then fall to the
		// intrinsic tuple, which is what makes it total.
		for _, pair := range [][2]string{
			{"2026-09-11T12:00:00Z", "2026-09-11T12:00:00.000Z"},
			{"2026-09-11T12:00:00.5Z", "2026-09-11T12:00:00.500000Z"},
			{"2026-09-11T12:00:00Z", "2026-09-11T14:00:00+02:00"},
		} {
			a, b := key(pair[0], "job-a"), key(pair[1], "job-b")
			if a.Less(b) == b.Less(a) {
				t.Errorf("%q and %q did not resolve to a strict order", pair[0], pair[1])
			}
			// job-a < job-b, so a is first whichever spelling it used.
			if !a.Less(b) {
				t.Errorf("%q vs %q: the tie did not fall to the intrinsic tuple", pair[0], pair[1])
			}
			same := key(pair[1], "job-a")
			if a.Less(same) || same.Less(a) {
				t.Errorf("%q and %q are the same instant and the same row, but compared unequal", pair[0], pair[1])
			}
		}
	})

	t.Run("whole-second stamps still order as before", func(t *testing.T) {
		older := key("2026-09-01T00:00:00Z", "job-a")
		newer := key("2026-09-02T00:00:00Z", "job-b")
		if !older.Less(newer) || newer.Less(older) {
			t.Error("whole-second ordering regressed")
		}
	})

	t.Run("an unparseable stamp sorts oldest and the order stays total", func(t *testing.T) {
		bad := key("not-an-instant", "job-a")
		good := key("2026-09-01T00:00:00Z", "job-b")
		if !bad.Less(good) {
			t.Error("an uninterpretable stamp was treated as the more recent row")
		}
		if good.Less(bad) == bad.Less(good) {
			t.Error("the comparison is not a strict order across the parse boundary")
		}
		other := key("also-not-an-instant", "job-b")
		if bad.Less(other) == other.Less(bad) {
			t.Error("two unparseable stamps did not resolve to a strict order")
		}
	})

	t.Run("eviction takes the oldest instant, not the smallest string", func(t *testing.T) {
		// The property the order exists for, at the boundary that motivated it.
		rows := []WallRingRow{
			{ObservedStartRealtime: "2026-09-11T12:00:00.9Z", JobID: "job-late"},
			{ObservedStartRealtime: "2026-09-11T12:00:00.11Z", JobID: "job-early"},
		}
		a := RingRecencyKeyOf(rows[0])
		b := RingRecencyKeyOf(rows[1])
		if !b.Less(a) {
			t.Error("110ms did not sort before 900ms; text order would have put .9 first")
		}
	})
}
