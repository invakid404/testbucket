package consumers

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/runner/gorunner"
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
//
// The contract's G1 table names seven rows of Go surface that must not change,
// and owes the check on EVERY repin. The check it describes renders the
// baml-rest-shaped bucket at the currently pinned revision and at the proposed
// one and compares the two.
//
// A two-binary comparison is not available here: the pinned revision is a
// published release, and fetching it is a live network call this suite must not
// make. What is available — and what the contract is actually protecting — is
// each row's OBSERVABLE value at this revision, pinned byte-for-byte. A repin
// that changes any of them fails here, at the commit that changes it, which is
// earlier than a comparison against a downloaded binary would fire.
//
// The earlier form of this test scanned the plan action for `default:` and
// `cmd/testbucket/main.go` for the string "wall-dir". It rendered nothing,
// compared no bytes, and could not have noticed a changed script, a changed
// token, a changed event parse, or a refusal that stopped refusing.
func TestGoConsumerSurfaceUnchangedAcrossRepin(t *testing.T) {
	cfg := pinnedBamlRestPlanConfig(t)

	// The Go adapter, configured exactly as the pinned caller configures it.
	r, err := gorunner.New(gorunner.Options{
		Race: true, Count: cfg.count, Timeout: "20m",
		NodePrefixes: cfg.nodePrefixes, EventsDir: "/tmp/testbucket-events",
	})
	if err != nil {
		t.Fatalf("gorunner.New: %v", err)
	}
	doc, err := core.BuildPlan(t.Context(), r, nil, "cold", core.PlanOptions{
		K: 2, Count: cfg.count, Live: bamlRestShapedLive(), Token: r.CanonicalToken(),
		Now: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(doc.Buckets) != 2 {
		t.Fatalf("the plan has %d bucket(s), want 2", len(doc.Buckets))
	}

	// --- G1 row: rendered script bytes for a given bucket ------------------
	t.Run("rendered script bytes", func(t *testing.T) {
		want := map[int]string{
			0: "set -euo pipefail\n" +
				"( cd . && go test -race -p=1 -count=100 -timeout 20m -json dynclient server | tee -a /tmp/testbucket-events/bucket-0-00.ndjson )",
			1: "set -euo pipefail\n" +
				"( cd adapters/openai && GOWORK=off go test -race -p=1 -count=100 -timeout 20m -json . | tee -a /tmp/testbucket-events/bucket-1-00.ndjson )\n" +
				"( cd . && go test -race -p=1 -count=100 -timeout 20m -json internal/wire | tee -a /tmp/testbucket-events/bucket-1-01.ndjson )",
		}
		for _, b := range doc.Buckets {
			if b.Script != want[b.Index] {
				t.Errorf("bucket %d script bytes changed:\n got: %q\nwant: %q", b.Index, b.Script, want[b.Index])
			}
		}
	})

	// --- G1 row: canonical token `-race -count=100` ------------------------
	t.Run("canonical token", func(t *testing.T) {
		// The token is the store's comparability key: a changed token
		// cold-starts every consumer's store, silently discarding every
		// measurement it had.
		if got, want := r.CanonicalToken(), "-race -count=100"; got != want {
			t.Errorf("canonical token = %q, want %q", got, want)
		}
		if doc.Flags != "-race -count=100" {
			t.Errorf("the plan records flags %q, want the canonical token", doc.Flags)
		}
	})

	// --- G1 row: -p=1, -timeout 20m, count shards summing to the sweep -----
	t.Run("invocation envelope", func(t *testing.T) {
		for _, b := range doc.Buckets {
			for i, inv := range b.Invocations {
				where := fmt.Sprintf("bucket %d inv %d", b.Index, i)
				if idx := indexOfArg(inv.Args, "-p=1"); idx < 0 {
					t.Errorf("%s does not render -p=1: %q", where, inv.Args)
				}
				if idx := indexOfArg(inv.Args, "-timeout"); idx < 0 || idx+1 >= len(inv.Args) || inv.Args[idx+1] != "20m" {
					t.Errorf("%s does not render -timeout 20m: %q", where, inv.Args)
				}
				if idx := indexOfArg(inv.Args, "-count=100"); idx < 0 {
					t.Errorf("%s does not render the full sweep -count=100: %q", where, inv.Args)
				}
			}
		}

		// COUNT SHARDS SUM TO THE SWEEP. A shard group whose counts do not add
		// up runs fewer iterations than the sweep asked for while every
		// invocation looks well formed, so the grammar gate is asked directly.
		live := map[string]runner.LivePackage{}
		for _, p := range bamlRestShapedLive() {
			live[p.ID] = p
		}
		shard := func(n, of, count int) runner.Unit {
			return runner.Unit{
				ID: fmt.Sprintf("server#%d/%d", n, of), Kind: runner.KindCountShard,
				Packages: []runner.LivePackage{live["server"]}, Module: ".", Mode: "work",
				Count: count, Shard: n, Shards: of,
			}
		}
		total := 0
		for n := 1; n <= 4; n++ {
			u := shard(n, 4, cfg.count/4)
			total += u.Count
			if defects := r.ValidateUnit(u, live, cfg.count); len(defects) > 0 {
				t.Errorf("a well-formed count-shard was refused: %v", defects)
			}
		}
		if total != cfg.count {
			t.Errorf("four shards of the %d sweep sum to %d", cfg.count, total)
		}
		// And a shard that runs nothing is refused rather than rendered.
		if defects := r.ValidateUnit(shard(1, 4, 0), live, cfg.count); len(defects) == 0 {
			t.Error("a count-shard running -count=0 was accepted; it executes nothing and still passes")
		}
	})

	// --- G1 row: `go test -json` event parsing and AuditCoverage -----------
	t.Run("event parsing and audit coverage", func(t *testing.T) {
		// The events every rendered invocation would tee, fed through the
		// production parser and the production audit.
		var readers []io.Reader
		for _, b := range doc.Buckets {
			for _, inv := range b.Invocations {
				readers = append(readers, strings.NewReader(goTestJSONFor(inv.Units)))
			}
		}
		sum, err := r.ParseTimings(readers...)
		if err != nil {
			t.Fatalf("ParseTimings: %v", err)
		}
		for _, p := range bamlRestShapedLive() {
			if secs, ok := sum.PackageSeconds[p.ID]; !ok {
				t.Errorf("the parsed summary has no entry for %s", p.ID)
			} else if secs <= 0 {
				t.Errorf("%s parsed to %v seconds", p.ID, secs)
			}
		}

		blob, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "shard-plan.json")
		if err := os.WriteFile(path, blob, 0o644); err != nil {
			t.Fatal(err)
		}
		planned, err := core.LoadPlannedCoverage(path)
		if err != nil {
			t.Fatalf("LoadPlannedCoverage: %v", err)
		}
		if err := core.AuditCoverage(io.Discard, planned, sum); err != nil {
			t.Errorf("the audit refuses a run that executed exactly its plan: %v", err)
		}

		// And it is a control, not a formality: a bucket that skipped a unit
		// fails the audit.
		short, err := r.ParseTimings(strings.NewReader(goTestJSONFor([]string{"server"})))
		if err != nil {
			t.Fatal(err)
		}
		if err := core.AuditCoverage(io.Discard, planned, short); err == nil {
			t.Error("the audit passed a run that executed one unit of the plan")
		}
	})

	// --- G1 row: needs_node derivation -------------------------------------
	t.Run("needs_node derivation", func(t *testing.T) {
		// The pinned caller opts in with `node-prefixes: adapters`, so a
		// bucket carrying an adapters/ target needs Node and one that does not
		// must not claim it — a bucket that wrongly claimed it would provision
		// a toolchain inside the measured interval.
		for _, b := range doc.Buckets {
			wantNode := false
			for _, u := range b.Units {
				for _, pkg := range u.Packages {
					if strings.HasPrefix(pkg, "adapters") {
						wantNode = true
					}
				}
			}
			if b.NeedsNode != wantNode {
				t.Errorf("bucket %d needs_node = %v, want %v (units %v)", b.Index, b.NeedsNode, wantNode, b.Units)
			}
		}
	})

	// --- G1 row: refusal of --wall-dir under --runner go -------------------
	t.Run("the Go --wall-dir refusal", func(t *testing.T) {
		// Behavioural, not a source scan: the binary is built and run, so a
		// refusal that stopped refusing fails here.
		bin := buildTestbucket(t)
		out, err := exec.Command(bin, "plan", "--runner", "go", "--wall-dir", t.TempDir()).CombinedOutput()
		if err == nil {
			t.Fatalf("`plan --runner go --wall-dir` succeeded; wall-time is Vitest-only:\n%s", out)
		}
		if !strings.Contains(string(out), "--wall-dir needs --runner vitest") {
			t.Errorf("the refusal does not name the reason:\n%s", out)
		}
		// And the same flag with the Vitest adapter is NOT refused for this
		// reason — otherwise the check above would pass against a binary that
		// refuses everything.
		out, err = exec.Command(bin, "plan", "--runner", "vitest", "--wall-dir", t.TempDir(),
			"--live", filepath.Join(t.TempDir(), "no-such-live.json")).CombinedOutput()
		if err != nil && strings.Contains(string(out), "--wall-dir needs --runner vitest") {
			t.Errorf("the Vitest adapter was refused the wall-time flag:\n%s", out)
		}
	})

	// --- G1 row: no wall-time surface in any Go script ---------------------
	t.Run("no wall-time surface reaches a Go script", func(t *testing.T) {
		for _, b := range doc.Buckets {
			for _, banned := range []string{"testbucket wall", "spec-", "--level invocation"} {
				if strings.Contains(b.Script, banned) {
					t.Errorf("bucket %d's Go script contains %q:\n%s", b.Index, banned, b.Script)
				}
			}
		}
	})

	// --- and no newly required action input reaches a Go consumer ----------
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
		// And the inputs the pinned caller actually passes are all still
		// declared: a repin that renamed one would break that consumer.
		for _, in := range cfg.passedInputs {
			if !strings.Contains(src, "\n  "+in+":") {
				t.Errorf("the pinned baml-rest caller passes %q, which the plan action no longer declares", in)
			}
		}
	})
}

// bamlRestPlanConfig is the pinned Go consumer's plan configuration, read from
// its own vendored workflow rather than transcribed here.
type bamlRestPlanConfig struct {
	count        int
	nodePrefixes []string
	passedInputs []string
}

// pinnedBamlRestPlanConfig reads the configuration out of the pinned caller.
// SOURCE.md pins that file's digest, so the values below are the ones the
// frozen consumer actually uses; a transcribed constant could agree with
// itself while the fixture said something else.
func pinnedBamlRestPlanConfig(t *testing.T) bamlRestPlanConfig {
	t.Helper()
	p := filepath.Join("baml-rest", ".github", "workflows", "unit-tests-bucketed.yml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read the pinned baml-rest caller: %v", err)
	}
	src := string(b)

	cfg := bamlRestPlanConfig{}
	// The `with:` block of the pinned plan-action step.
	idx := strings.Index(src, "/.github/actions/plan@")
	if idx < 0 {
		t.Fatal("the pinned caller no longer uses the plan action")
	}
	block := src[idx:min2(idx+700, len(src))]
	if !strings.Contains(block, "count: \"100\"") {
		t.Fatalf("the pinned caller's sweep is no longer 100:\n%s", block)
	}
	cfg.count = 100
	if !strings.Contains(block, "node-prefixes: adapters") {
		t.Fatalf("the pinned caller's node prefixes changed:\n%s", block)
	}
	cfg.nodePrefixes = []string{"adapters"}
	for _, in := range []string{"version", "k", "count", "store-path", "events-dir", "shard-plan", "node-prefixes", "exclude-module"} {
		if strings.Contains(block, "\n          "+in+":") {
			cfg.passedInputs = append(cfg.passedInputs, in)
		}
	}
	if len(cfg.passedInputs) != 8 {
		t.Errorf("read %d of the pinned caller's 8 plan inputs: %v", len(cfg.passedInputs), cfg.passedInputs)
	}
	return cfg
}

// bamlRestShapedLive is a live set with that repo's shape: a workspace module
// with several packages, plus one out-of-workspace adapter module that cannot
// be mixed into a shared build list and therefore rides as its own atom.
func bamlRestShapedLive() []runner.LivePackage {
	return []runner.LivePackage{
		{ID: "adapters/openai", Dir: "adapters/openai", Module: "adapters/openai", Mode: "off", Atom: "adapters/openai", HasTests: true},
		{ID: "internal/wire", Dir: "internal/wire", Module: ".", Mode: "work", HasTests: true},
		{ID: "dynclient", Dir: "dynclient", Module: ".", Mode: "work", HasTests: true},
		{ID: "server", Dir: "server", Module: ".", Mode: "work", HasTests: true},
	}
}

// goTestJSONFor synthesises the `go test -json` stream an invocation covering
// these units would produce: one passing test per package, exactly as the real
// toolchain reports it.
func goTestJSONFor(units []string) string {
	var b strings.Builder
	for _, u := range units {
		pkg := strings.TrimPrefix(u, "mod:")
		fmt.Fprintf(&b, `{"Action":"run","Package":%q,"Test":"TestOne"}`+"\n", pkg)
		fmt.Fprintf(&b, `{"Action":"pass","Package":%q,"Test":"TestOne","Elapsed":0.5}`+"\n", pkg)
		fmt.Fprintf(&b, `{"Action":"pass","Package":%q,"Elapsed":0.5}`+"\n", pkg)
	}
	return b.String()
}

// indexOfArg reports where an exact argument appears, or -1.
func indexOfArg(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

// buildTestbucket builds the CLI once per test so a refusal can be exercised
// as behaviour rather than asserted about source text.
func buildTestbucket(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "testbucket")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/invakid404/testbucket/cmd/testbucket")
	cmd.Dir = filepath.Join("..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
