package planbind

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/walltime"
)

// discoveryJSON is what `vitest list --filesOnly --json` prints: the raw bytes
// a bundle freezes.
const discoveryJSON = `[{"file":"tests/alpha.spec.ts"},{"file":"tests/beta.spec.ts"},{"file":"tests/gamma.spec.ts"}]`

// storeJSON is a warm store: three measured targets, one of them flagged for
// name slicing so the runnable-listing input actually matters.
const storeJSON = `{
  "schema": 1,
  "flags": "vitest",
  "updated_at": "2026-08-30T00:00:00Z",
  "units": {
    "tests/alpha.spec.ts": {"seconds": 90, "samples": 4, "split": "run", "split_into": 2,
      "tests": {"alpha one": 60, "alpha two": 30}},
    "tests/beta.spec.ts": {"seconds": 40, "samples": 4},
    "tests/gamma.spec.ts": {"seconds": 20, "samples": 4}
  }
}`

// testCommit is a FULL commit SHA: the bundle refuses an abbreviation, because
// a prefix is something another object can grow into.
const testCommit = "d9ae1d433bb45012c04d567879b66fc4bf6112c6"

// runnableJSON is what `vitest list <file> --json` prints for the sliced file.
const runnableJSON = `[{"name":"alpha one","file":"tests/alpha.spec.ts"},{"name":"alpha two","file":"tests/alpha.spec.ts"}]`

func acquire(t *testing.T, root string, mutate func(*AcquireOptions)) *walltime.PlanningInputBundle {
	t.Helper()
	b, err := Acquire(baseAcquire(t, root, mutate))
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return b
}

// baseAcquire is the fixture's acquisition options, so a test can vary one
// input and call Acquire itself when it expects a refusal.
func baseAcquire(t *testing.T, root string, mutate func(*AcquireOptions)) AcquireOptions {
	t.Helper()
	storePath := filepath.Join(root, "test-timings.json")
	if err := os.WriteFile(storePath, []byte(storeJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := AcquireOptions{
		StoreBytes: []byte(storeJSON),
		Root:       root, Runner: "vitest",
		Instant:       time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
		StaleAfter:    14 * 24 * time.Hour,
		K:             2,
		Count:         1,
		Token:         "vitest",
		StorePath:     storePath,
		DiscoveryArgv: []string{"npx", "vitest", "list", "--filesOnly", "--json"},
		Discovery:     []byte(discoveryJSON),
		Runnables:     map[string][]byte{"tests/alpha.spec.ts": []byte(runnableJSON)},
		Env:           map[string]string{"TB_DISCOVERY_EXCLUDE_PREFIXES": ""},
		Executables:   map[string]string{"npx": "/usr/local/bin/npx"},
		Tools:         map[string]string{"node": "24.19.0"},
		Repository:    "example/mandel", Commit: testCommit, Tree: "sha256:tree",
	}
	if mutate != nil {
		mutate(&opt)
	}
	return opt
}

func plan(t *testing.T, b *walltime.PlanningInputBundle) *Result {
	t.Helper()
	res, err := Plan(context.Background(), PlanOptions{Bundle: b, Stage1: "sha256:stage1"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	return res
}

// TestReplayIsByteIdentical is the replay evidence: the same frozen bundle
// planned twice produces the same plan, digest for digest. Without this, a
// Stage-2 receipt is a claim nobody can check.
func TestReplayIsByteIdentical(t *testing.T) {
	root := t.TempDir()
	b := acquire(t, root, nil)
	first := plan(t, b)
	second := plan(t, b)
	if err := first.Receipt.Matches(second.Receipt); err != nil {
		t.Fatalf("replaying one bundle produced a different plan: %v", err)
	}
	// The receipt must actually carry both digests and the derived identities;
	// a receipt that validates but is empty proves nothing.
	if first.Receipt.PlanDigest == first.Receipt.SemanticDigest {
		t.Errorf("the full-document and semantic digests are the same value")
	}
	for name, d := range map[string]walltime.Digest{
		"atom": first.Receipt.AtomDigest, "topology": first.Receipt.TopologyDigest,
		"membership": first.Receipt.MembershipDigest, "invocation": first.Receipt.InvocationDigest,
		"script": first.Receipt.ScriptDigest, "matrix": first.Receipt.MatrixDigest,
	} {
		if d == "" {
			t.Errorf("the receipt has no %s digest", name)
		}
	}
	// The sliced target's names came from the frozen listing, so the plan must
	// contain a name slice rather than the whole file.
	if !strings.Contains(scriptBytes(first.Doc), "alpha one") {
		t.Errorf("the frozen runnable listing did not reach the rendered slice:\n%s", scriptBytes(first.Doc))
	}
}

// TestChangedInputChangesTheDigest: one byte of discovery is a different plan,
// and the receipt says so.
func TestChangedInputChangesTheDigest(t *testing.T) {
	root := t.TempDir()
	base := plan(t, acquire(t, root, nil))
	// The same root, so the only difference between the two runs is the
	// discovery bytes themselves.
	changed := plan(t, acquire(t, root, func(o *AcquireOptions) {
		o.Discovery = []byte(strings.Replace(discoveryJSON, "gamma", "delta", 1))
	}))
	if err := base.Receipt.Matches(changed.Receipt); err == nil {
		t.Fatalf("renaming a discovered file did not change any digest")
	}
	if base.Receipt.SemanticDigest == changed.Receipt.SemanticDigest {
		t.Errorf("the semantic projection did not notice a different file set")
	}
}

// TestTheTwoDigestsAnswerDifferentQuestions is why both exist. Moving the
// canonical instant changes the plan DOCUMENT — the store is older, so the
// summary says so — while changing nothing about what will run.
func TestTheTwoDigestsAnswerDifferentQuestions(t *testing.T) {
	root := t.TempDir()
	early := plan(t, acquire(t, root, nil))
	late := plan(t, acquire(t, root, func(o *AcquireOptions) {
		o.Instant = o.Instant.Add(72 * time.Hour)
	}))
	if early.Receipt.PlanDigest == late.Receipt.PlanDigest {
		t.Errorf("a three-day-older store did not change the full plan document")
	}
	if early.Receipt.SemanticDigest != late.Receipt.SemanticDigest {
		t.Errorf("the clock changed WHAT RUNS, which it must not:\n%s\n%s",
			early.Receipt.SemanticDigest, late.Receipt.SemanticDigest)
	}
}

// TestAmbientInputsAreRefused: the bundle is the only admissible source, so a
// missing frozen listing is an error rather than a live `vitest list`.
func TestAmbientInputsAreRefused(t *testing.T) {
	root := t.TempDir()
	b := acquire(t, root, func(o *AcquireOptions) { o.Runnables = nil })
	_, err := Plan(context.Background(), PlanOptions{Bundle: b, Stage1: "sha256:stage1"})
	if err == nil {
		t.Fatalf("planning succeeded with no frozen listing for a name-sliced target")
	}
	if !strings.Contains(err.Error(), "unbound input") {
		t.Errorf("error %q does not name the unbound input", err)
	}

	// And an ambient clock is refused at acquisition, before anything is
	// frozen at all.
	if _, err := Acquire(AcquireOptions{Root: root, Runner: "vitest", Discovery: []byte(discoveryJSON)}); err == nil {
		t.Errorf("a bundle was acquired with no canonical planning instant")
	}
}

// TestBundleTamperingIsCaught: a snapshot whose bytes no longer match its
// digest cannot be planned from.
func TestBundleTamperingIsCaught(t *testing.T) {
	root := t.TempDir()
	b := acquire(t, root, nil)
	b.Discovery[0].Bytes = []byte(`[{"file":"tests/injected.spec.ts"}]`)
	if err := b.Validate(); err == nil {
		t.Fatalf("a rewritten discovery snapshot validated")
	}
	if _, err := Plan(context.Background(), PlanOptions{Bundle: b, Stage1: "sha256:stage1"}); err == nil {
		t.Fatalf("a rewritten discovery snapshot was planned from")
	}
}

// TestColdStartIsBoundAsAColdStart: an absent store is recorded explicitly, so
// a replay cold-starts too instead of finding a store that appeared later.
func TestColdStartIsBoundAsAColdStart(t *testing.T) {
	root := t.TempDir()
	b, err := Acquire(AcquireOptions{
		Root: root, Runner: "vitest", Instant: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
		StaleAfter: 14 * 24 * time.Hour, K: 2, Count: 1, Token: "vitest",
		StorePath: filepath.Join(root, "absent.json"), StoreAbsent: true,
		Discovery:     []byte(discoveryJSON),
		DiscoveryArgv: []string{"npx", "vitest", "list", "--filesOnly", "--json"},
		Executables:   map[string]string{"npx": "/usr/local/bin/npx"},
		Tools:         map[string]string{"node": "24.19.0"},
		Env:           map[string]string{},
		Repository:    "example/mandel", Commit: testCommit, Tree: "sha256:tree",
	})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	found := false
	for _, a := range b.AbsentInputs {
		if strings.HasPrefix(a, "store:") {
			found = true
		}
	}
	if !found {
		t.Errorf("a missing store was not recorded as an absent input: %v", b.AbsentInputs)
	}
	res := plan(t, b)
	if !res.Doc.Summary.ColdStart {
		t.Errorf("the replayed plan did not cold-start")
	}
}

// TestWallRenderedScriptCarriesItsSpecs: with a records directory, each
// invocation is written as a serialised spec and run through the wrapper, and
// the spec is IN the script so the plan digest covers it.
func TestWallRenderedScriptCarriesItsSpecs(t *testing.T) {
	root := t.TempDir()
	b := acquire(t, root, func(o *AcquireOptions) { o.WallDir = "/tmp/wall" })
	res, err := Plan(context.Background(), PlanOptions{Bundle: b, Stage1: "sha256:stage1"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	script := scriptBytes(res.Doc)
	for _, want := range []string{"testbucket wall exec", "--level invocation", `"argv":`, "/tmp/wall/spec-"} {
		if !strings.Contains(script, want) {
			t.Errorf("the rendered script does not contain %q:\n%s", want, script)
		}
	}
	// The same plan WITHOUT a records directory must render the v0.2.2 bytes,
	// so measurement stays strictly opt-in.
	plainRes := plan(t, acquire(t, root, nil))
	plain := scriptBytes(plainRes.Doc)
	if strings.Contains(plain, "testbucket wall") {
		t.Errorf("the default render mentions the wrapper:\n%s", plain)
	}
	if res.Receipt.ScriptDigest == plainRes.Receipt.ScriptDigest {
		t.Errorf("wrapping every invocation did not change the script digest")
	}
}

// TestFrozenPlanRefusesAStaleStore is the warm-evidence rule at the only place
// a warm claim is made.
//
// The everyday planner warns about a stale store and carries on — that is
// v0.2.2 behaviour and consumers depend on it. The frozen path is different:
// it is where a scored row's weights come from, and the contract says a stale
// store is not warm evidence, so planning from one is refused rather than
// annotated.
func TestFrozenPlanRefusesAStaleStore(t *testing.T) {
	root := t.TempDir()
	b := acquire(t, root, func(o *AcquireOptions) {
		// The fixture store was recorded on 2026-08-30; move the canonical
		// instant well past the frozen 14-day policy.
		o.Instant = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	})
	_, err := Plan(context.Background(), PlanOptions{Bundle: b, Stage1: "sha256:stage1"})
	if err == nil {
		t.Fatalf("the frozen path planned from a stale store")
	}
	if !strings.Contains(err.Error(), "not warm evidence") {
		t.Errorf("error %q does not say why a stale store is inadmissible", err)
	}

	// Inside the policy it plans normally: the rule is about staleness, not
	// about age.
	fresh := acquire(t, root, func(o *AcquireOptions) {
		o.Instant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	})
	if _, err := Plan(context.Background(), PlanOptions{Bundle: fresh, Stage1: "sha256:stage1"}); err != nil {
		t.Errorf("a store inside the stale policy was refused: %v", err)
	}
}

// The shard-plan artifact the workflow uploads, and the coverage audit later
// reads, must be exactly the document the Stage-2 receipt's full-plan digest
// was taken over.
//
// The verifier now refuses an audit whose plan does not match that digest. If
// the planner wrote an artifact that canonicalised to anything else, the
// binding would fire on every honest run — so the two are asserted to be the
// same document at the point they are produced, not assumed to be.
func TestTheShardPlanArtifactIsWhatTheReceiptBinds(t *testing.T) {
	root := t.TempDir()
	res := plan(t, acquire(t, root, nil))
	// What `plan --shard-plan` writes is res.Doc; what the receipt froze is
	// PlanDigest. A reader auditing the run has only these two.
	artifact, err := walltime.DigestJSON(res.Doc)
	if err != nil {
		t.Fatal(err)
	}
	if artifact != res.Receipt.PlanDigest {
		t.Fatalf("the artifact digests to %s but the receipt binds %s", artifact, res.Receipt.PlanDigest)
	}

	// And a document that differs at all is a different digest: the binding is
	// only a control if narrowing the plan cannot preserve it.
	narrowed := *res.Doc
	narrowed.Buckets = append([]core.PlanBucket(nil), res.Doc.Buckets...)
	if len(narrowed.Buckets) == 0 || len(narrowed.Buckets[0].Units) == 0 {
		t.Fatalf("the fixture plan has nothing to narrow")
	}
	narrowed.Buckets[0].Units = narrowed.Buckets[0].Units[:len(narrowed.Buckets[0].Units)-1]
	got, err := walltime.DigestJSON(&narrowed)
	if err != nil {
		t.Fatal(err)
	}
	if got == res.Receipt.PlanDigest {
		t.Errorf("a plan with a unit removed kept the authorised full-plan digest")
	}
}
