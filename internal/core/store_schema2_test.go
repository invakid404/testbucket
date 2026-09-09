package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// matrixRow is one parsed row of contract §15.1c's status-indexed presence
// matrix.
type matrixRow struct {
	State     string
	Status    string
	Subtype   string
	Always    string
	FitGroup  string
	Observing string
}

// parsePresenceMatrix reads §15.1c's table out of the contract.
//
// The cases below are the ENUMERATION of that table rather than a transcription
// of it: §15.1c states that its rows are the exhaustive state set and that no
// count of states appears in any document, so a row added to the contract must
// add a case here without an edit.
func parsePresenceMatrix(t *testing.T) []matrixRow {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "walltime", "acceptance-contract.md"))
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "### 15.1c")
	if start < 0 {
		t.Fatal("contract §15.1c not found")
	}
	end := strings.Index(src[start:], "#### 15.1d")
	if end < 0 {
		t.Fatal("contract §15.1d not found; cannot bound the matrix")
	}
	seg := src[start : start+end]

	var rows []matrixRow
	for _, line := range strings.Split(seg, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || !strings.Contains(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 6 {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		// Skip the header and the separator.
		if strings.HasPrefix(cells[0], "State") || strings.HasPrefix(cells[0], "---") {
			continue
		}
		if cells[1] == "" {
			continue
		}
		rows = append(rows, matrixRow{
			State: cells[0], Status: cells[1], Subtype: cells[2],
			Always: cells[3], FitGroup: cells[4], Observing: cells[5],
		})
	}
	if len(rows) == 0 {
		t.Fatal("parsed no rows from §15.1c's matrix")
	}
	return rows
}

// cellValue strips the contract's markdown emphasis and backticks so a cell
// can be compared as a value.
func cellValue(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	// Cells carry a trailing gloss after an em dash in the State column only.
	if i := strings.Index(s, " — "); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// TestWallStoreSchemaStateMatrixAndMigration is §22 test 71a
// (R23-F1/R23-F2/R24-F2): the store projection gate. It deserializes and
// re-serializes a literal document for EVERY state §15.1c indexes, with the
// state set, the subtype vocabulary and the no-fit set read from the PARSED
// matrix rows and compared as sets. No case is selected by a phrase or a count.
func TestWallStoreSchemaStateMatrixAndMigration(t *testing.T) {
	rows := parsePresenceMatrix(t)

	// §15.1d fixes the current value at 1, and §15.1c's determinacy claim
	// needs the VALUE pinned rather than only its presence.
	if WallModelVersion != 1 {
		t.Fatalf("WALL_MODEL_VERSION = %d, §15.1d fixes the current value at 1", WallModelVersion)
	}

	t.Run("the parsed state set matches the implementation's vocabulary", func(t *testing.T) {
		parsed := map[string]bool{}
		for _, r := range rows {
			sub := cellValue(r.Subtype)
			if sub == "absent" {
				continue
			}
			parsed[sub] = true
		}
		impl := map[string]bool{}
		for _, s := range []WallFailureSubtype{
			SubtypeMigratedNoHistory, SubtypeRowsBelowMinimum, SubtypeRunsBelowMinimum,
			SubtypeRankInsufficient, SubtypeNNLSBudgetExhausted, SubtypeMAECeilingExceeded,
		} {
			impl[string(s)] = true
		}
		for s := range parsed {
			if !impl[s] {
				t.Errorf("contract subtype %q is not in the implementation's vocabulary", s)
			}
		}
		for s := range impl {
			if !parsed[s] {
				t.Errorf("implementation subtype %q appears in no §15.1c row", s)
			}
		}
	})

	t.Run("every indexed state round-trips with the right presence", func(t *testing.T) {
		for _, r := range rows {
			state := cellValue(r.State)
			t.Run(state, func(t *testing.T) {
				status := WallStatus(cellValue(r.Status))
				sub := cellValue(r.Subtype)
				fitAbsent := strings.Contains(strings.ToLower(r.FitGroup), "absent")

				w := newWall()
				w.Status = status
				if sub == "absent" {
					w.FailureSubtype = ""
				} else {
					w.FailureSubtype = WallFailureSubtype(sub)
				}
				if !fitAbsent {
					w.Fit = &WallFitGroup{
						FixedNs: 5_000_000_000, Scale: 1.0,
						WholeInvocationOverheadNs: 8_000_000_000,
						PerSliceOverheadNs:        3_000_000_000,
						FittedAt:                  "2026-09-01T00:00:00Z",
						RowsUsed:                  25, RunsUsed: 4,
						ResidualMAENs: 1_545_660_377, ResidualP90Ns: 1_811_320_754,
						RankSupport: []string{"c1", "c2", "c3", "c4"},
					}
				}
				if state == "migrated" {
					from := 1
					w.MigratedFrom = &from
				}

				if err := w.Validate(); err != nil {
					t.Fatalf("the matrix's own state failed validation: %v", err)
				}

				// Round-trip: deserialize and re-serialize a literal document.
				b, err := json.Marshal(w)
				if err != nil {
					t.Fatal(err)
				}
				var back WallObject
				if err := json.Unmarshal(b, &back); err != nil {
					t.Fatal(err)
				}
				if err := back.Validate(); err != nil {
					t.Fatalf("round-tripped state failed validation: %v", err)
				}

				// The three always-present leaves.
				if back.ModelVersion == nil || back.ComparabilityKeyDigest == "" || back.Observations == nil {
					t.Fatal("model_version, comparability_key_digest and observations are always present once wall exists")
				}
				// model_version is asserted by VALUE, not merely present.
				if *back.ModelVersion != WallModelVersion {
					t.Fatalf("model_version = %d, want WALL_MODEL_VERSION = %d", *back.ModelVersion, WallModelVersion)
				}

				// A coefficient in a no-fit row is a failure — instantiated
				// once per such row, from the parsed matrix.
				if fitAbsent {
					bad := *w
					bad.Fit = &WallFitGroup{FixedNs: 1}
					if err := bad.Validate(); err == nil {
						t.Fatal("a fit group in a no-fit state was accepted")
					}
				} else {
					bad := *w
					bad.Fit = nil
					if err := bad.Validate(); err == nil {
						t.Fatal("a fitted state with no fit group was accepted")
					}
				}
			})
		}
	})

	t.Run("failure_subtype is required for non-ok and absent for ok", func(t *testing.T) {
		w := newWall()
		w.Status = WallStatusInsufficient
		w.FailureSubtype = ""
		if err := w.Validate(); err == nil {
			t.Fatal("a non-ok status with no subtype was accepted")
		}
		w = newWall()
		w.Status = WallStatusOK
		w.FailureSubtype = SubtypeRowsBelowMinimum
		w.Fit = &WallFitGroup{}
		if err := w.Validate(); err == nil {
			t.Fatal("status ok carrying a subtype was accepted")
		}
	})

	t.Run("a subtype outside the spelled vocabulary fails", func(t *testing.T) {
		w := newWall()
		w.Status = WallStatusInsufficient
		w.FailureSubtype = WallFailureSubtype("something_else")
		if err := w.Validate(); err == nil {
			t.Fatal("an unspelled subtype was accepted")
		}
	})

	t.Run("precedence resolves simultaneous predicates deterministically", func(t *testing.T) {
		// §15.1c's fixed order: migrated, rows, runs, rank, nnls budget.
		cases := []struct {
			holding map[WallFailureSubtype]bool
			want    WallFailureSubtype
		}{
			{map[WallFailureSubtype]bool{SubtypeRowsBelowMinimum: true, SubtypeRunsBelowMinimum: true}, SubtypeRowsBelowMinimum},
			{map[WallFailureSubtype]bool{SubtypeRowsBelowMinimum: true, SubtypeRankInsufficient: true}, SubtypeRowsBelowMinimum},
			{map[WallFailureSubtype]bool{SubtypeRunsBelowMinimum: true, SubtypeRankInsufficient: true}, SubtypeRunsBelowMinimum},
			{map[WallFailureSubtype]bool{SubtypeRankInsufficient: true, SubtypeNNLSBudgetExhausted: true}, SubtypeRankInsufficient},
			{map[WallFailureSubtype]bool{SubtypeMigratedNoHistory: true, SubtypeRowsBelowMinimum: true}, SubtypeMigratedNoHistory},
		}
		for _, c := range cases {
			got, ok := WallStatusPrecedence(c.holding)
			if !ok || got != c.want {
				t.Errorf("precedence over %v = %q (%v), want %q", c.holding, got, ok, c.want)
			}
		}
		if _, ok := WallStatusPrecedence(map[WallFailureSubtype]bool{}); ok {
			t.Error("precedence reported a subtype with no predicate holding")
		}
	})

	t.Run("the 1 to 2 migration carries the always-present leaves and invents no history", func(t *testing.T) {
		const legacy = `{"schema":1,"flags":"vitest","units":{"a":{"seconds":3,"samples":2}},"coverage":["a"],"coverage_source":"live"}`
		st, reason, err := ParseStore([]byte(legacy), "s")
		if err != nil {
			t.Fatal(err)
		}
		if reason != "" {
			t.Fatalf("a schema-1 store must migrate, not cold-start; reason = %q", reason)
		}
		if !st.NeedsWallMigration() {
			t.Fatal("a schema-1 store must be marked for migration")
		}
		// Reporter state is carried verbatim.
		if st.Flags != "vitest" || len(st.Units) != 1 || len(st.Coverage) != 1 || st.CoverageSource != "live" {
			t.Fatalf("migration did not carry reporter state verbatim: %+v", st)
		}

		st.MigrateWall("sha256:key")
		w := st.Wall
		if w == nil {
			t.Fatal("migration produced no wall object")
		}
		if err := w.Validate(); err != nil {
			t.Fatalf("migrated wall failed validation: %v", err)
		}
		if w.Status != WallStatusInsufficient || w.FailureSubtype != SubtypeMigratedNoHistory {
			t.Fatalf("migrated wall status/subtype = %q/%q", w.Status, w.FailureSubtype)
		}
		if w.MigratedFrom == nil || *w.MigratedFrom != 1 {
			t.Fatal("migrated_from must be 1")
		}
		if *w.ModelVersion != WallModelVersion {
			t.Fatal("the migration must write model_version alongside the other always-present leaves")
		}
		if w.Observations == nil || len(w.Observations) != 0 {
			t.Fatal("observations must be present and EMPTY: no wall history is invented")
		}
		if w.Fit != nil {
			t.Fatal("a migrated store carries no fit group")
		}
	})

	t.Run("2 to 1 backward cold-starts loudly", func(t *testing.T) {
		_, reason, err := ParseStore([]byte(`{"schema":99}`), "s")
		if err != nil {
			t.Fatal(err)
		}
		if reason == "" {
			t.Fatal("an unknown schema must cold-start loudly with a reason")
		}
	})

	t.Run("an absent or unknown model_version fails closed", func(t *testing.T) {
		w := newWall()
		w.ModelVersion = nil
		if err := w.Validate(); err == nil {
			t.Fatal("an absent model_version was accepted")
		}
		unknown := WallModelVersion + 7
		w = newWall()
		w.ModelVersion = &unknown
		if err := w.Validate(); err == nil {
			t.Fatal("an unimplemented model_version was accepted")
		}
		// And a store carrying it is refused at parse rather than reinterpreted.
		body := `{"schema":2,"units":{},"wall":{"model_version":99,"comparability_key_digest":"sha256:k","status":"ok","observations":[]}}`
		_, reason, err := ParseStore([]byte(body), "s")
		if err != nil {
			t.Fatal(err)
		}
		if reason == "" {
			t.Fatal("a store with an unknown model_version must be refused with a named reason")
		}
	})

	t.Run("a different model_version together with a fit group fails", func(t *testing.T) {
		// §15.1d: coefficients written under one model_version are not valid
		// under another, and keeping the old fit group under a new version
		// number is the forbidden third option.
		other := WallModelVersion + 1
		w := newWall()
		w.ModelVersion = &other
		w.Status = WallStatusOK
		w.FailureSubtype = ""
		w.Fit = &WallFitGroup{FixedNs: 1, Scale: 1}
		if err := w.Validate(); err == nil {
			t.Fatal("a fit group under a different model_version was accepted")
		}
	})

	t.Run("the ring is bounded at W", func(t *testing.T) {
		w := newWall()
		w.Observations = make([]WallRingRow, W+1)
		if err := w.Validate(); err == nil {
			t.Fatalf("a ring above W = %d was accepted", W)
		}
	})

	t.Run("no document writes a count of states or no-fit rows", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join("..", "..", "docs", "walltime", "acceptance-contract.md"))
		if err != nil {
			t.Fatal(err)
		}
		seg := string(b)
		i := strings.Index(seg, "### 15.1c")
		j := strings.Index(seg[i:], "#### 15.1d")
		body := seg[i : i+j]
		for _, banned := range []string{"seven states", "two no-coefficient", "five insufficient", "six states"} {
			if strings.Contains(strings.ToLower(body), banned) {
				t.Errorf("§15.1c writes a literal count: %q", banned)
			}
		}
	})
}
