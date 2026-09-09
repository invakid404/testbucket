package walltime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestLedgerClaimsMatchObservationSchema is §22 test 38 (S4): every claim in
// §18.0's table names a field the §13 schema RETAINS, and no claim exceeds it.
//
// The claim set is parsed from the contract, so a claim added to §18.0 without
// a retained field fails rather than passing on prose.
func TestLedgerClaimsMatchObservationSchema(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "walltime", "acceptance-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, "### 18.0 ")
	if start < 0 {
		t.Fatal("contract §18.0 not found")
	}
	end := strings.Index(src[start:], "### 18.1 ")
	if end < 0 {
		t.Fatal("contract §18.1 not found; cannot bound §18.0")
	}
	seg := src[start : start+end]

	// The retained-field column, as backticked names.
	rows := regexp.MustCompile(`(?m)^\| ([^|]+) \| ([^|]+) \| ([^|]+) \|$`).FindAllStringSubmatch(seg, -1)
	var claims []struct{ claimed, fields, checked string }
	for _, r := range rows {
		c := strings.TrimSpace(r[1])
		if c == "Claimed" || strings.HasPrefix(c, "---") {
			continue
		}
		claims = append(claims, struct{ claimed, fields, checked string }{
			c, strings.TrimSpace(r[2]), strings.TrimSpace(r[3])})
	}
	if len(claims) == 0 {
		t.Fatal("parsed no ledger claims from §18.0")
	}

	// The fields the §13 schema actually serializes, taken from the type.
	_, obs := qcFixture(t)
	wire, err := CanonicalJSON(obs)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(wire)
	invWire, err := CanonicalJSON(obs.Invocations[0])
	if err != nil {
		t.Fatal(err)
	}
	invSerialized := string(invWire)

	fieldRe := regexp.MustCompile("`([A-Za-z_.\\[\\]]+)`")

	t.Run("every claimed field is retained by the schema", func(t *testing.T) {
		for _, c := range claims {
			for _, m := range fieldRe.FindAllStringSubmatch(c.fields, -1) {
				name := m[1]
				// Normalise the contract's `invocations[].x` and `.x` forms.
				leaf := name
				leaf = strings.TrimPrefix(leaf, "invocations[]")
				leaf = strings.TrimPrefix(leaf, ".")
				if strings.HasSuffix(leaf, "_*") {
					leaf = strings.TrimSuffix(leaf, "_*")
				}
				if leaf == "" {
					continue
				}
				inTop := strings.Contains(serialized, `"`+leaf+`"`)
				inInv := strings.Contains(invSerialized, `"`+leaf+`"`)
				// boot_id_* expands to two fields.
				if leaf == "boot_id" {
					inTop = strings.Contains(serialized, `"boot_id_start"`) &&
						strings.Contains(serialized, `"boot_id_end"`)
				}
				if !inTop && !inInv {
					t.Errorf("§18.0 claims %q via field %q, which the §13 schema does not retain",
						c.claimed, name)
				}
			}
		}
	})

	t.Run("every claim names the check that verifies it", func(t *testing.T) {
		for _, c := range claims {
			if !regexp.MustCompile(`QC\d+[ab]?`).MatchString(c.checked) {
				t.Errorf("§18.0 claim %q names no QC check (got %q)", c.claimed, c.checked)
			}
		}
	})

	t.Run("no claim exceeds the schema", func(t *testing.T) {
		// The two claims §18.0 explicitly narrows: the process group is
		// claimed only AS THE TESTBUCKET PROCESS OBSERVED IT, and containment
		// identity as a broader notion is not claimed at all.
		if !strings.Contains(seg, "not a containment guarantee") {
			t.Error("§18.0 no longer scopes the process-group claim")
		}
		// The disclaimer wraps across a line in the contract, so the match is
		// on its load-bearing phrase rather than a fixed span.
		flat := strings.Join(strings.Fields(seg), " ")
		if !strings.Contains(flat, "as a broader notion is **not** claimed") {
			t.Error("§18.0 no longer disclaims broader containment identity")
		}
		// And the unit-granularity limit is stated, since a whole-file
		// invocation covers many units under one aggregate V.
		if !strings.Contains(seg, "Not sufficient, at unit granularity") {
			t.Error("§18.0 no longer states the unit-granularity limit")
		}
	})

	t.Run("the shipped limitations do not exceed §18.0 either", func(t *testing.T) {
		joined := strings.Join(CanonicalLimitations(), " ")
		for _, overclaim := range []string{"containment guarantee", "per-unit label", "unit-level training"} {
			if strings.Contains(strings.ToLower(joined), overclaim) {
				t.Errorf("a shipped limitation claims %q, which §18.0 excludes", overclaim)
			}
		}
	})
}

// TestMandelWarmupPlanProducesQualifyingFullRankCorpus is §22 test 57
// (SR-10/S-7): W-1…W-4 of §17.3b.
//
// The proposal ALONE never satisfies the gate, and the test asserts that
// directly: a corpus of one calibration document and zero ring rows is
// insufficient. A topology proposal proves rank REACHABILITY; it cannot
// manufacture executions.
func TestMandelWarmupPlanProducesQualifyingFullRankCorpus(t *testing.T) {
	t.Run("a SUFFICIENT proposal plus zero ring rows is still insufficient", func(t *testing.T) {
		ev, err := CalibrateProposer(calibrationUniverse(), 4, 4, corePacker)
		if err != nil {
			t.Fatal(err)
		}
		if ev.Outcome != CalibrationSufficient {
			t.Fatalf("the proposal must be sufficient here, got %q", ev.Outcome)
		}
		// Zero ring rows: the gate reads the REAL ring, not the proposal.
		res, err := FitModel(nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != FitInsufficient {
			t.Fatalf("an empty ring gave status %q; a proposal cannot manufacture executions", res.Status)
		}
	})

	t.Run("all four warm-up conditions are required", func(t *testing.T) {
		// >= 24 accepted rows, >= 3 distinct (run_id, run_attempt), rank 4,
		// and at least one i_any_whole_file = 0 row.
		full := warmCorpus(t, 24, 3, true)
		res, err := FitModel(full)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != FitOK {
			t.Fatalf("a conforming warm corpus gave %q (%s)", res.Status, res.Subtype)
		}

		// Each condition removed in turn must fail.
		for name, rows := range map[string][]FitRow{
			"below 24 rows":             warmCorpus(t, MinRows-1, 3, true),
			"fewer than 3 runs":         warmCorpus(t, 24, 1, true),
			"no i_any_whole_file=0 row": warmCorpus(t, 24, 3, false),
		} {
			res, err := FitModel(rows)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if res.Status == FitOK {
				t.Errorf("%s was admitted; all four conditions are required", name)
			}
		}
	})

	t.Run("W-3: three distinct runs must reproduce one comparability key", func(t *testing.T) {
		// The proposal proves nothing about this; only real runs can.
		base := planComparability()
		key, err := ComparabilityKeyDigest(base)
		if err != nil {
			t.Fatal(err)
		}
		for _, instance := range []string{"GitHub Actions 2", "GitHub Actions 7", "GitHub Actions 41"} {
			// The runner INSTANCE differs across the three runs; the key must
			// not, which is the stability the old runner_name leaf destroyed.
			k, err := ComparabilityKeyDigest(base)
			if err != nil {
				t.Fatal(err)
			}
			if k != key {
				t.Fatalf("instance %q produced a different comparability key", instance)
			}
		}
	})

	t.Run("only then is explicit wall planning admitted", func(t *testing.T) {
		full := warmCorpus(t, 24, 3, true)
		res, err := FitModel(full)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != FitOK {
			t.Fatalf("status = %q, want ok before wall basis is admitted", res.Status)
		}
		if !res.Rank.Admitted || res.Rank.Rank != DesignColumns {
			t.Fatalf("rank = %d admitted = %v, want %d and true", res.Rank.Rank, res.Rank.Admitted, DesignColumns)
		}
	})
}

// warmCorpus builds a corpus with the requested row count, run count, and
// whether it contains an i_any_whole_file = 0 row.
func warmCorpus(t *testing.T, rows, runs int, withSliceOnly bool) []FitRow {
	t.Helper()
	shapes := []struct {
		reporter int64
		i, slice int
		a        int64
	}{
		{10e9, 1, 0, 23e9},
		{20e9, 1, 0, 33e9},
		{30e9, 1, 2, 49e9},
		{10e9, 1, 1, 42e9},
	}
	if withSliceOnly {
		shapes = append(shapes, struct {
			reporter int64
			i, slice int
			a        int64
		}{15e9, 0, 1, 23e9})
	}
	var out []FitRow
	for i := 0; i < rows; i++ {
		sh := shapes[i%len(shapes)]
		out = append(out, FitRow{
			ReporterSumNs: sh.reporter, IAnyWholeFile: sh.i, SliceCount: sh.slice,
			ElapsedNs: sh.a, HeadSHA: "h",
			RunID: "r" + string(rune('1'+i%runs)), RunAttempt: "1", BucketIndex: i,
		})
	}
	return out
}
