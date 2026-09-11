package walltime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// parseRegistryBlock extracts the fenced YAML block named by header from
// scope.md and returns it, so the tests read the MACHINE BLOCK rather than a
// transcription of it.
func parseRegistryBlock(t *testing.T, header string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "walltime", "scope.md"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, header)
	if i < 0 {
		t.Fatalf("registry block %q not found", header)
	}
	rest := src[i:]
	end := strings.Index(rest, "\n```")
	if end < 0 {
		t.Fatalf("registry block %q is unterminated", header)
	}
	return rest[:end]
}

type wirePath struct {
	Path        string
	Artifact    string
	Type        string
	Cardinality string
}

// parseWirePaths reads the `wire_paths:` entries. The block is a YAML flow
// mapping per line, so the fields are extracted by key rather than by position.
func parseWirePaths(t *testing.T, block string) []wirePath {
	t.Helper()
	seg := block
	if i := strings.Index(seg, "wire_paths:"); i >= 0 {
		seg = seg[i:]
	}
	if i := strings.Index(seg, "\nroles:"); i >= 0 {
		seg = seg[:i]
	}
	var out []wirePath
	entry := regexp.MustCompile(`path:\s*"([^"]+)"`)
	kv := func(line, key string) string {
		m := regexp.MustCompile(key + `:\s*([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(line)
		if len(m) == 2 {
			return m[1]
		}
		return ""
	}
	// Entries may wrap across lines; join a continuation into its opener.
	var joined []string
	for _, line := range strings.Split(seg, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- {") {
			joined = append(joined, trimmed)
			continue
		}
		if len(joined) > 0 && trimmed != "" && !strings.HasPrefix(trimmed, "#") &&
			!strings.HasSuffix(joined[len(joined)-1], "}") {
			joined[len(joined)-1] += " " + trimmed
		}
	}
	for _, line := range joined {
		m := entry.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, wirePath{
			Path: m[1], Artifact: kv(line, "artifact"),
			Type: kv(line, "type"), Cardinality: kv(line, "cardinality"),
		})
	}
	return out
}

// TestFieldRegistryCoversEverySerializedPath is §22 test 63 (S-3/D-2/R8-D5).
//
// The projections are the SIX the contract names by parsed registry key, owner
// section, direction and comparison. No other projection is compared, and no
// projection is compared against a written number — a count cannot choose an
// algorithm, which is why the contract names the projections instead.
func TestFieldRegistryCoversEverySerializedPath(t *testing.T) {
	block := parseRegistryBlock(t, "# field-registry v2")
	paths := parseWirePaths(t, block)
	if len(paths) == 0 {
		t.Fatal("parsed no wire paths from the registry")
	}

	byArtifact := map[string][]wirePath{}
	for _, p := range paths {
		byArtifact[p.Artifact] = append(byArtifact[p.Artifact], p)
	}

	t.Run("every entry carries artifact, type and cardinality", func(t *testing.T) {
		for _, p := range paths {
			if p.Artifact == "" || p.Type == "" || p.Cardinality == "" {
				t.Errorf("registry entry %q is missing artifact/type/cardinality: %+v", p.Path, p)
			}
		}
	})

	t.Run("the six wholly-new documents are all registered", func(t *testing.T) {
		// Projection 4: wire_paths grouped by artifact, for the six wholly-new
		// documents, compared SET-EQUAL against their owning sections.
		for _, a := range []string{"observation", "profile", "ring_row", "calibration_evidence", "manifest"} {
			if len(byArtifact[a]) == 0 {
				t.Errorf("no registry entry for the wholly-new artifact %q", a)
			}
		}
	})

	t.Run("projection 1: REGRESSOR roles are ordered by column", func(t *testing.T) {
		seg := block[strings.Index(block, "roles:"):]
		re := regexp.MustCompile(`field:\s*([a-z_]+),\s*role:\s*REGRESSOR,\s*column:\s*(\d)`)
		var cols []string
		for _, m := range re.FindAllStringSubmatch(seg, -1) {
			cols = append(cols, m[2]+":"+m[1])
		}
		if len(cols) != DesignColumns {
			t.Fatalf("registry declares %d REGRESSOR columns, want %d", len(cols), DesignColumns)
		}
		want := []string{"1:constant", "2:reporter_sum_ns", "3:i_any_whole_file", "4:slice_count"}
		for i := range want {
			if cols[i] != want[i] {
				t.Errorf("REGRESSOR column %d is %q, want %q — §0.9's design-column table is ORDER-equal",
					i+1, cols[i], want[i])
			}
		}
	})

	t.Run("projection 2: comparability_key is order-equal with §15.3", func(t *testing.T) {
		members := parseMembership(t, block, "comparability_key")
		impl := ComparabilityLeafNames()
		if len(members) != len(impl) {
			t.Fatalf("registry lists %d comparability leaves, the implementation has %d", len(members), len(impl))
		}
		for i := range impl {
			if members[i] != impl[i] {
				t.Errorf("comparability leaf %d is %q in the registry and %q in the implementation; the projection is ORDER-equal",
					i, members[i], impl[i])
			}
		}
	})

	t.Run("projection 3: bc_inv expands to scalar leaves and is compared whole", func(t *testing.T) {
		membership, tupleLeaves := bcInvMembershipFromRegistry(t)
		if len(membership) == 0 {
			t.Fatal("the bc_inv membership is empty")
		}
		expanded := ExpandMembership(membership, tupleLeaves)
		// Every container is expanded, so nothing is compared opaquely.
		for _, m := range expanded {
			if _, isContainer := tupleLeaves[m]; isContainer {
				t.Errorf("bc_inv member %q survived expansion as a container", m)
			}
		}
		// And the tuple is built over exactly that set, in both directions —
		// the constructor refuses an extra or a missing member.
		values := map[string]string{}
		for i, m := range expanded {
			values[m] = string(rune('a' + i%26))
		}
		if _, err := NewBCInvariantTuple(expanded, values); err != nil {
			t.Fatalf("the expanded membership does not build a tuple: %v", err)
		}
	})

	t.Run("clause 1: no canonical field appears in roles twice", func(t *testing.T) {
		seg := block[strings.Index(block, "roles:"):]
		re := regexp.MustCompile(`field:\s*([a-z_]+),\s*role:`)
		seen := map[string]int{}
		for _, m := range re.FindAllStringSubmatch(seg, -1) {
			seen[m[1]]++
		}
		for f, n := range seen {
			if n > 1 {
				t.Errorf("field %q claims %d roles; at most one is permitted", f, n)
			}
		}
	})

	t.Run("clause 4: containers are derived, never transcribed", func(t *testing.T) {
		// The container set is COMPUTED as every registered path with type
		// object or array_of_object. A document that transcribes the list or
		// its size fails — which is how the earlier nine-name prose came to
		// omit manifest.model_parameters.
		var containers []string
		for _, p := range paths {
			if p.Type == "object" || p.Type == "array_of_object" {
				containers = append(containers, p.Path)
			}
		}
		if len(containers) == 0 {
			t.Fatal("no containers derived from the registry")
		}
		sort.Strings(containers)
		if !contains2(containers, "manifest.model_parameters") {
			t.Error("manifest.model_parameters is not derived as a container; that omission is exactly what a transcribed list produced")
		}
		// Each container_roles entry must name a real container and a QC.
		croles := regexp.MustCompile(`\{path:\s*([a-z_]+),\s*role_field:\s*[a-z_]+,\s*checked_by:\s*(QC\d+[ab]?)\}`)
		seg := block[strings.Index(block, "container_roles:"):]
		for _, m := range croles.FindAllStringSubmatch(seg, -1) {
			if !contains2(containers, m[1]) && !containsSuffix(containers, "."+m[1]) {
				t.Errorf("container_roles names %q, which is not a derived container", m[1])
			}
			if m[2] == "" {
				t.Errorf("container_roles entry for %q names no checking QC", m[1])
			}
		}
	})

	t.Run("clause 5: no literal cardinality for any registry projection", func(t *testing.T) {
		// A count anywhere in this package fails; the projections are compared
		// as sets and orders.
		for _, doc := range []string{"scope.md", "acceptance-contract.md"} {
			b, err := os.ReadFile(filepath.Join("..", "..", "docs", "walltime", doc))
			if err != nil {
				t.Fatal(err)
			}
			src := string(b)
			for _, banned := range []string{
				"nine containers", "ten wire paths", "fifteen comparability",
				"seven leaves, four plus three",
			} {
				lower := strings.ToLower(src)
				idx := strings.Index(lower, banned)
				if idx < 0 {
					continue
				}
				// §10.5.0 quotes the superseded "seven leaves, four plus three"
				// in order to record that it drifted. A quoted description of a
				// retired count is not a count this package states, and
				// convicting it would forbid the document from explaining
				// itself.
				window := lower[maxInt(idx-300, 0):idx]
				if strings.Contains(window, "earlier revision") || strings.Contains(window, "superseded") ||
					strings.Contains(window, "described the block as") {
					continue
				}
				t.Errorf("%s writes a literal registry cardinality: %q", doc, banned)
			}
		}
	})

	t.Run("no alias_of key survives", func(t *testing.T) {
		if strings.Contains(block, "alias_of") {
			t.Error("an alias_of key survives in the registry")
		}
	})

	t.Run("the observation registry matches the implementation's serialized paths", func(t *testing.T) {
		// The forward direction for the observation document: every registered
		// observation path is really serialized.
		_, obs := qcFixture(t)
		obs.CampaignID = ""
		b, err := CanonicalJSON(obs)
		if err != nil {
			t.Fatal(err)
		}
		wire := string(b)
		for _, p := range byArtifact["observation"] {
			top := p.Path
			if i := strings.Index(top, "."); i >= 0 {
				top = top[:i]
			}
			if strings.Contains(top, "[") {
				continue
			}
			switch top {
			// Present only under conditions the fixture does not exercise.
			case "campaign_id", "a_eta_ns":
				continue
			}
			if !strings.Contains(wire, `"`+top+`"`) {
				t.Errorf("registered observation path %q is not serialized", p.Path)
			}
		}
	})
}

// parseMembership reads one memberships list from the registry block.
func parseMembership(t *testing.T, block, name string) []string {
	t.Helper()
	i := strings.Index(block, "\n  "+name+":")
	if i < 0 {
		t.Fatalf("membership %q not found", name)
	}
	seg := block[i+len(name)+4:]
	var out []string
	for _, line := range strings.Split(seg, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "- ") {
			if len(out) > 0 {
				break
			}
			continue
		}
		out = append(out, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
	}
	return out
}

func contains2(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func containsSuffix(list []string, suffix string) bool {
	for _, v := range list {
		if strings.HasSuffix(v, suffix) {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// TestCalibrationEvidenceMatchesItsRegistryEntries is the control §22 test 63's
// own clause needed and did not have.
//
// Test 63 checks that the calibration document has AT LEAST ONE registry entry.
// That is what let the producer and the registry diverge completely: the
// serialized type omitted seven §17.3a fields and emitted five nobody had
// registered, and both documents agreed with themselves. A bidirectional claim
// over a wholly-new document has to be compared in both directions or it is a
// claim about one of them.
//
// The comparison is SET-EQUAL over the serialized keys, which means a field
// added to the struct without a registry row fails here, and so does a row with
// no field behind it.
func TestCalibrationEvidenceMatchesItsRegistryEntries(t *testing.T) {
	block := parseRegistryBlock(t, "# field-registry v2")
	registered := map[string]bool{}
	for _, p := range parseWirePaths(t, block) {
		if p.Artifact != "calibration_evidence" {
			continue
		}
		registered[strings.TrimPrefix(p.Path, "calib.")] = true
	}
	if len(registered) == 0 {
		t.Fatal("the registry declares no calibration_evidence paths")
	}

	// EVERY field populated, because the comparison is over what the document
	// CAN serialize and most fields are omitempty. A half-filled value would
	// make an absent field look unregistered.
	ev := CalibrationEvidence{
		Schema:                 CalibrationEvidenceSchema,
		Outcome:                CalibrationSufficient,
		ComparabilityKeyDigest: "sha256:key",
		ProposedPlanDigests:    []Digest{"sha256:plan"},
		LayoutsTried:           1,
		LayoutBudget:           DefaultCalibrationMaxPlans,
		Rank:                   DesignColumns,
		SigmaMax:               "1",
		Tolerance:              "2",
		MinPivot:               "3",
		IndicatorValuesPresent: []int{0, 1},
		DistinctSliceCounts:    []int{0, 2},
		GeneratorExhausted:     true,
		DeficientColumns:       []int{4},
		ZeroColumn:             4,
		Reason:                 "because",
		Buckets:                [][]string{{"a"}},
		DesignRows:             [][]float64{{1, 2, 3, 4}},
		GeneratedAt:            "2026-09-11T00:00:00Z",
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}

	for k := range wire {
		if !registered[k] {
			t.Errorf("the calibration document serializes %q and the registry does not declare calib.%s", k, k)
		}
	}
	for k := range registered {
		if _, ok := wire[k]; !ok {
			t.Errorf("the registry declares calib.%s and the document cannot serialize it", k)
		}
	}

	// And the §17.3a identities specifically, because those are the ones a
	// later run reads: an outcome word with no population and no proposal is
	// not evidence.
	for _, need := range []string{
		"comparability_key_digest", "proposed_plan_digests", "sigma_max", "tolerance",
		"min_pivot", "indicator_values_present", "distinct_slice_counts", "generated_at",
	} {
		if _, ok := wire[need]; !ok {
			t.Errorf("§17.3a's evidence document requires %q and the type cannot emit it", need)
		}
	}
}
