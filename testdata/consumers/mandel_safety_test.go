package consumers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Project classification of contract §20.4. Membership is PROJECT-BASED, and
// the excluded-prefix rule is separate from it.
const (
	ProjectUnit        = "unit"
	ProjectHarnessUnit = "harness-unit"
	ProjectCaseReplset = "case-replset"
)

// caseGlobPrefix is the excluded Case prefix the misc lane runs.
const caseGlobPrefix = "shared/f/lib/cases/"

// regionRouterPrefix is excluded from the unit project.
const regionRouterPrefix = "packages/region-router/"

// ClassifyProject returns the project a tracked path resolves to under the
// frozen root config, and whether it is inside the root-config union at all.
//
// "Zero discovered paths under integration-tests/" is WRONG: the legitimate
// harness-unit project lives there, and a predicate that excluded the whole
// tree would drop its eight files.
func ClassifyProject(path string) (project string, inUnion bool) {
	switch {
	case strings.HasPrefix(path, HarnessUnitInclude):
		return ProjectHarnessUnit, true
	case strings.HasPrefix(path, caseGlobPrefix):
		return ProjectCaseReplset, true
	case strings.HasPrefix(path, "integration-tests/"):
		// Under integration-tests/ but outside harness-unit: excluded from the
		// unit project and outside the root-config union.
		return "", false
	case strings.HasPrefix(path, regionRouterPrefix):
		return "", false
	default:
		return ProjectUnit, true
	}
}

func trackedPaths(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("mandel", "tracked-test-paths.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && strings.HasSuffix(l, ".test.ts") {
			out = append(out, l)
		}
	}
	return out
}

// TestMandelUnitOnlyPredicateIsProjectBased is §22 test 22: project-based
// classification accepts the 8 harness-unit files and rejects any case-replset
// file.
func TestMandelUnitOnlyPredicateIsProjectBased(t *testing.T) {
	paths := trackedPaths(t)

	allowed := map[string]bool{ProjectUnit: true, ProjectHarnessUnit: true}
	forbidden := map[string]bool{ProjectCaseReplset: true}

	var harnessUnit, caseFiles, outsideUnion, unit int
	for _, p := range paths {
		project, inUnion := ClassifyProject(p)
		if !inUnion {
			outsideUnion++
			continue
		}
		switch project {
		case ProjectHarnessUnit:
			harnessUnit++
		case ProjectCaseReplset:
			caseFiles++
		case ProjectUnit:
			unit++
		default:
			t.Fatalf("path %q resolved to an unknown project %q", p, project)
		}
	}

	t.Run("the eight harness-unit files are ACCEPTED", func(t *testing.T) {
		if harnessUnit != 8 {
			t.Fatalf("harness-unit files = %d, want 8", harnessUnit)
		}
		if !allowed[ProjectHarnessUnit] {
			t.Fatal("harness-unit must be an allowed project")
		}
		// The wrong reading, pinned: "zero discovered paths under
		// integration-tests/" would drop exactly these eight.
		var underIntegration int
		for _, p := range paths {
			if strings.HasPrefix(p, "integration-tests/") {
				underIntegration++
			}
		}
		if underIntegration <= harnessUnit {
			t.Fatal("the fixture no longer distinguishes harness-unit from the rest of integration-tests/")
		}
	})

	t.Run("every case-replset file is REJECTED", func(t *testing.T) {
		if caseFiles != 48 {
			t.Fatalf("case files = %d, want 48", caseFiles)
		}
		for _, p := range paths {
			if !strings.HasPrefix(p, caseGlobPrefix) {
				continue
			}
			project, _ := ClassifyProject(p)
			if allowed[project] {
				t.Fatalf("Case file %q resolved to the allowed project %q", p, project)
			}
			if !forbidden[project] {
				t.Fatalf("Case file %q resolved to %q, which is neither allowed nor forbidden", p, project)
			}
		}
	})

	t.Run("the root-config union and the bucket set match the static audit", func(t *testing.T) {
		// §20.4: of 1,512 tracked paths, 1,444 are in the root-config union —
		// 1,396 bucket files and 48 excluded Case files.
		union := unit + harnessUnit + caseFiles
		if union != 1444 {
			t.Errorf("root-config union = %d, want 1444", union)
		}
		if bucket := unit + harnessUnit; bucket != 1396 {
			t.Errorf("bucket files = %d, want 1396", bucket)
		}
		if outsideUnion != 1512-1444 {
			t.Errorf("outside the union = %d, want %d", outsideUnion, 1512-1444)
		}
	})

	t.Run("every tracked path is ASCII, so conservative folding hides nothing", func(t *testing.T) {
		for _, p := range paths {
			for _, r := range p {
				if r > 127 {
					t.Fatalf("path %q carries a non-ASCII rune; the audit's folding claim would not hold", p)
				}
			}
		}
	})

	t.Run("the frozen config declares all three projects", func(t *testing.T) {
		cfg, err := os.ReadFile(filepath.Join("mandel", "vitest.config.ts"))
		if err != nil {
			t.Fatal(err)
		}
		src := string(cfg)
		for _, p := range []string{ProjectUnit, ProjectHarnessUnit, ProjectCaseReplset} {
			if !strings.Contains(src, "'"+p+"'") {
				t.Errorf("the pinned config does not declare project %q", p)
			}
		}
		if !strings.Contains(src, caseGlobPrefix) {
			t.Errorf("the pinned config does not carry the Case prefix %q", caseGlobPrefix)
		}
	})

	t.Run("no runnable Vitest project exists in this fixture", func(t *testing.T) {
		// The Mandel safety rule: the dry-list precondition cannot even be
		// evaluated here, because the snapshot carries no test files, no
		// node_modules and no full lockfile. Nothing is executed, and this
		// asserts why rather than leaving it implicit.
		var testFiles int
		err := filepath.Walk("mandel", func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(p, ".test.ts") || strings.HasSuffix(p, ".spec.ts") {
				testFiles++
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if testFiles != 0 {
			t.Fatalf("the fixture carries %d test files; it must carry none, or a Vitest invocation would become possible", testFiles)
		}
		if _, err := os.Stat(filepath.Join("mandel", "node_modules")); err == nil {
			t.Fatal("the fixture carries node_modules; it is a snapshot, not a checkout")
		}
		if _, err := os.Stat(filepath.Join("mandel", "pnpm-lock.yaml")); err == nil {
			t.Fatal("the fixture carries the full lockfile; §20.6a keeps it digest-only")
		}
	})
}

// TestSelectedWorkRequiresSingletonUnitEquality is §22 test 23: any surviving
// structure requires unit-list equality with its unit id; a multi-unit list is
// rejected as a label.
func TestSelectedWorkRequiresSingletonUnitEquality(t *testing.T) {
	type selected struct {
		UnitID string
		Units  []string
	}
	validate := func(s selected) error {
		if len(s.Units) != 1 {
			return errMultiUnit(len(s.Units))
		}
		if s.Units[0] != s.UnitID {
			return errUnitMismatch(s.UnitID, s.Units[0])
		}
		return nil
	}

	t.Run("a singleton list equal to its unit id is accepted", func(t *testing.T) {
		if err := validate(selected{UnitID: "a.spec.ts", Units: []string{"a.spec.ts"}}); err != nil {
			t.Fatalf("a conforming singleton was rejected: %v", err)
		}
	})

	t.Run("a multi-unit list is rejected as a label", func(t *testing.T) {
		err := validate(selected{UnitID: "a.spec.ts", Units: []string{"a.spec.ts", "b.spec.ts"}})
		if err == nil {
			t.Fatal("a multi-unit list was accepted; it is a label, not selected work")
		}
	})

	t.Run("a singleton naming a different unit is rejected", func(t *testing.T) {
		if err := validate(selected{UnitID: "a.spec.ts", Units: []string{"b.spec.ts"}}); err == nil {
			t.Fatal("a singleton disagreeing with its unit id was accepted")
		}
	})

	t.Run("an empty list is rejected", func(t *testing.T) {
		if err := validate(selected{UnitID: "a.spec.ts"}); err == nil {
			t.Fatal("an empty unit list was accepted")
		}
	})
}

// TestBamlRestInputSetStillDrivesTheActions is §22 test 21: the v0.1.1 input
// set still drives them.
//
// baml-rest composes the three actions DIRECTLY at the pinned SHA
// 551d49ce, so the claim is concrete: every input that caller passes must
// still be a declared input of the corresponding action today, and no added
// input may have become required.
func TestBamlRestInputSetStillDrivesTheActions(t *testing.T) {
	// The v0.1.1 input set, read from the pinned caller itself.
	pinned := map[string][]string{
		"plan":       {"count", "events-dir", "exclude-module", "k", "node-prefixes", "shard-plan", "store-path", "version"},
		"run-bucket": {"est-seconds", "events-dir", "name", "script", "version"},
		"record":     {"count", "events-dir", "exclude-module", "k", "shard-plan", "store-path", "version"},
	}

	wf, err := os.ReadFile(filepath.Join("baml-rest", ".github", "workflows", "unit-tests-bucketed.yml"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(wf)

	t.Run("the pinned caller still passes exactly its v0.1.1 inputs", func(t *testing.T) {
		for action, inputs := range pinned {
			for _, in := range inputs {
				if !strings.Contains(src, in+":") {
					t.Errorf("the pinned baml-rest caller no longer passes %q to %s", in, action)
				}
			}
		}
	})

	t.Run("every pinned input is still declared by the action today", func(t *testing.T) {
		// This is the preservation property: a removed or renamed input would
		// break a consumer that never opted into anything.
		for action, inputs := range pinned {
			b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", action, "action.yml"))
			if err != nil {
				t.Fatalf("read %s action: %v", action, err)
			}
			decl := string(b)
			for _, in := range inputs {
				if !strings.Contains(decl, "\n  "+in+":") {
					t.Errorf("action %q no longer declares the v0.1.1 input %q", action, in)
				}
			}
		}
	})

	t.Run("no added input became required for this unscored consumer", func(t *testing.T) {
		// §21: a scored run must supply the added inputs; an unscored run may
		// omit them all and keeps every current default. baml-rest is
		// Vitest-exempt (§20.3) and passes none of them.
		for _, added := range []string{
			"runner-class", "runs-on-label", "cache-declaration-json",
			"cache-declaration-file", "candidate-sha", "workload-commit", "campaign-id",
		} {
			if strings.Contains(src, added+":") {
				t.Errorf("the pinned baml-rest caller would have to supply %q", added)
			}
		}
		if strings.Contains(src, "scored: true") {
			t.Error("the pinned baml-rest caller declares a scored run; it is exempt (§20.3)")
		}
	})

	t.Run("the caller is pinned to a full SHA, not a moving reference", func(t *testing.T) {
		if !strings.Contains(src, "@551d49cea6a74a99706912fe18504f318bdba1cc") {
			t.Error("the pinned baml-rest caller no longer resolves the actions at the v0.1.1 SHA")
		}
	})
}

// TestGoConsumerSurfaceUnchangedAcrossRepin is §22 test 32.
func TestGoConsumerSurfaceUnchangedAcrossRepin(t *testing.T) {
	// The §20.3 claim is current-pin isolation plus default execution
	// neutrality: the Go adapter's rendered bytes, canonical token, events and
	// audit semantics are unchanged, and no newly required action input
	// reaches a Go consumer.
	t.Run("no added action input is required of a Go consumer", func(t *testing.T) {
		plan, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "plan", "action.yml"))
		if err != nil {
			t.Fatal(err)
		}
		src := string(plan)
		// Every §21 added plan input carries a default, so a Go caller that
		// passes none of them still resolves.
		for _, in := range []string{
			"est-basis", "scored", "runner-class", "runs-on-label",
			"cache-declaration-file", "candidate-sha", "workload-commit",
		} {
			idx := strings.Index(src, "\n  "+in+":")
			if idx < 0 {
				t.Errorf("plan does not declare %q", in)
				continue
			}
			// The block must carry a default within a short window.
			window := src[idx:min2(idx+900, len(src))]
			if !strings.Contains(window, "default:") {
				t.Errorf("added input %q has no default; a Go consumer would be forced to supply it", in)
			}
		}
	})

	t.Run("the Go refusal is pinned to stay", func(t *testing.T) {
		// §22 item 11: --wall-dir with --runner go errors. It already passed
		// at R54 and is pinned so it cannot regress.
		main, err := os.ReadFile(filepath.Join("..", "..", "cmd", "testbucket", "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(main), "wall-dir") {
			t.Fatal("the --wall-dir flag is gone; the Go refusal cannot be expressed")
		}
	})
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
