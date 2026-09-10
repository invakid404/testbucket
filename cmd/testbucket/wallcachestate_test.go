package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// actionStepScript extracts one composite step's shell body from an action.yml.
//
// The point of running the SHIPPED bytes is that a test over a Go function
// proves nothing about the YAML: the cache-state defect was a shell variable
// that no step ever assigned, which every Go-level test passed straight over.
func actionStepScript(t *testing.T, actionRel, stepName string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", actionRel))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, "- name: "+stepName) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s has no step named %q", actionRel, stepName)
	}
	runAt := -1
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "run: |" {
			runAt = i
			break
		}
		// A following step begins before this one's script: the step has none.
		if i > start && strings.Contains(lines[i], "- name: ") {
			break
		}
	}
	if runAt < 0 {
		t.Fatalf("step %q in %s has no `run: |` block", stepName, actionRel)
	}
	indent := len(lines[runAt]) - len(strings.TrimLeft(lines[runAt], " ")) + 2
	var body []string
	for i := runAt + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			body = append(body, "")
			continue
		}
		if len(l)-len(strings.TrimLeft(l, " ")) < indent {
			break
		}
		body = append(body, l[indent:])
	}
	return strings.Join(body, "\n")
}

// cacheStateCase is one producer state run end to end through the YAML.
type cacheStateCase struct {
	name       string
	mode       string
	primaryKey string
	producer   string
	hit        string
	matched    string
	// actionHit and actionMatched are producer A's own restore-step outputs,
	// which reach the step as different variables from the caller's inputs.
	actionHit     string
	actionMatched string
	disposition   string
	wantHit       *bool
}

// TestTheCacheStateStepCoversEveryProducerState runs the shipped step.
//
// `--cache-state "$TB_CACHE_STATE_FILE"` was guarded by a variable nothing in
// the repository ever assigned, and no step wrote a CacheState document at
// all, so the assembler serialized the all-zero Go value and QC14b refused
// every scored row. The producer now exists; this runs it for each legal
// producer state, through the action's own shell, and checks the tuple §10.5.1
// says each must produce.
func TestTheCacheStateStepCoversEveryProducerState(t *testing.T) {
	bin := planBinary(t)
	script := actionStepScript(t, filepath.Join(".github", "actions", "run-bucket", "action.yml"),
		"Materialize the cache state")

	yes, no := true, false
	cases := []cacheStateCase{
		{name: "producer none under a disabled cache", mode: "disabled", producer: "none",
			disposition: "disabled"},
		{name: "producer caller with an exact hit", mode: "exact-key", primaryKey: "deps-abc",
			producer: "caller", hit: "true", matched: "deps-abc",
			disposition: "exact-hit", wantHit: &yes},
		{name: "producer caller with a miss", mode: "exact-key", primaryKey: "deps-abc",
			producer: "caller", hit: "false", matched: "",
			disposition: "miss", wantHit: &no},
		// PRODUCER A reads the action's OWN restore step, so its result
		// arrives in different variables and its hit is NOT serialized onto
		// the row: §10.5.0 puts dependency_cache_hit there iff the producer is
		// caller, and QC14 re-derives that presence pattern.
		{name: "producer action with an exact hit", mode: "exact-key", primaryKey: "deps-abc",
			producer: "action", actionHit: "true", actionMatched: "deps-abc",
			matched: "deps-abc", disposition: "exact-hit"},
		{name: "producer action with a miss", mode: "exact-key", primaryKey: "deps-abc",
			producer: "action", actionHit: "false", actionMatched: "",
			disposition: "miss"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mongo := filepath.Join(dir, "mongod")
			if err := os.WriteFile(mongo, []byte("mongod-"+tc.name), 0o755); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte("mongod-" + tc.name))
			expected := hex.EncodeToString(sum[:])

			decl := filepath.Join(dir, "cache-declaration.json")
			writeFixture(t, decl, walltime.CacheDeclaration{
				DependencyCacheMode:       tc.mode,
				DependencyCachePrimaryKey: tc.primaryKey,
				TransformCacheMode:        "disabled",
				DependencyCacheProducer:   tc.producer,
				ExpectedMongoBinarySHA256: "sha256:" + expected,
			})

			githubEnv := filepath.Join(dir, "github-env")
			if err := os.WriteFile(githubEnv, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(),
				"PATH="+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"),
				"TB_DECL_FILE="+decl,
				"TB_PRODUCER="+tc.producer,
				"TB_MATCHED="+tc.matched,
				"TB_HIT="+tc.hit,
				"TB_ACTION_HIT="+tc.actionHit,
				"TB_ACTION_MATCHED="+tc.actionMatched,
				"MONGOMS_SYSTEM_BINARY="+mongo,
				"RUNNER_TEMP="+dir,
				"GITHUB_JOB=bucket-0",
				"GITHUB_ENV="+githubEnv,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("the step failed: %v\n%s", err, out)
			}

			// THE VARIABLE THE ASSEMBLE STEP READS must now be assigned, which
			// is the half that was missing entirely.
			env, err := os.ReadFile(githubEnv)
			if err != nil {
				t.Fatal(err)
			}
			prefix := "TB_CACHE_STATE_FILE="
			line := ""
			for _, l := range strings.Split(string(env), "\n") {
				if strings.HasPrefix(l, prefix) {
					line = strings.TrimPrefix(l, prefix)
				}
			}
			if line == "" {
				t.Fatalf("the step exported no TB_CACHE_STATE_FILE; the assemble step's guard stays false:\n%s", env)
			}

			var got walltime.CacheState
			sb, err := os.ReadFile(line)
			if err != nil {
				t.Fatalf("the exported path is not a readable document: %v", err)
			}
			if err := json.Unmarshal(sb, &got); err != nil {
				t.Fatalf("parse cache state: %v\n%s", err, sb)
			}
			if got.DependencyCacheMode != tc.mode || got.DependencyCacheProducer != tc.producer {
				t.Errorf("declaration leaves are (mode %q, producer %q), want (%q, %q)",
					got.DependencyCacheMode, got.DependencyCacheProducer, tc.mode, tc.producer)
			}
			if got.DependencyCachePrimaryKey != tc.primaryKey || got.DependencyCacheMatchedKey != tc.matched {
				t.Errorf("keys are (primary %q, matched %q), want (%q, %q)",
					got.DependencyCachePrimaryKey, got.DependencyCacheMatchedKey, tc.primaryKey, tc.matched)
			}
			if got.DependencyCacheDisposition != tc.disposition {
				t.Errorf("disposition is %q, want %q", got.DependencyCacheDisposition, tc.disposition)
			}
			switch {
			case tc.wantHit == nil && got.DependencyCacheHit != nil:
				t.Errorf("dependency_cache_hit is present under producer %q", tc.producer)
			case tc.wantHit != nil && got.DependencyCacheHit == nil:
				t.Error("dependency_cache_hit is absent under producer caller")
			case tc.wantHit != nil && *got.DependencyCacheHit != *tc.wantHit:
				t.Errorf("dependency_cache_hit is %v, want %v", *got.DependencyCacheHit, *tc.wantHit)
			}
			if got.MongoBinaryPath != mongo || got.MongoBinarySHA256 != expected {
				t.Errorf("the executed binary is (%q, %q), want (%q, %q)",
					got.MongoBinaryPath, got.MongoBinarySHA256, mongo, expected)
			}
			if !got.MongoBinaryVerifiedOnRunner {
				t.Error("mongo_binary_verified_on_runner is false for a binary that matches the declaration")
			}
			// The gate this document exists to pass.
			if err := walltime.QC14b(got); err != nil {
				t.Errorf("the materialized state does not pass QC14b: %v", err)
			}
		})
	}
}

// TestTheActionOwnedRestoreExists is §10.5.3's producer A.
//
// The action accepted `dependency-cache-producer: action` and then contained
// no restore step, so the mode validated and produced nothing: every scored
// row under it carried an all-zero cache state. §10.5.3 defines producer A as
// the restore running INSIDE run-bucket and the action reading its own step
// outputs, so both halves are checked here.
func TestTheActionOwnedRestoreExists(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "run-bucket", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yml := string(b)
	if !strings.Contains(yml, "uses: actions/cache/restore@") {
		t.Fatal("run-bucket contains no cache restore step, so producer action can never supply a result")
	}
	if !strings.Contains(yml, "steps.dep-cache.outputs.cache-hit") ||
		!strings.Contains(yml, "steps.dep-cache.outputs.cache-matched-key") {
		t.Error("the cache state does not read the action's OWN restore step outputs, which is what producer A means")
	}
	// §10.5.1 admits an exact primary-key match or nothing. A restore-keys
	// list produces the prefix-fallback tuple, which fails the row — so
	// offering it is a way to fail rows, not a convenience.
	restore := strings.Index(yml, "- name: Restore the action-owned dependency cache")
	if restore < 0 {
		t.Fatal("the action-owned restore step is not named as such")
	}
	next := strings.Index(yml[restore:], "\n    - name: ")
	if next < 0 {
		next = len(yml) - restore
	}
	if strings.Contains(yml[restore:restore+next], "restore-keys") {
		t.Error("the action-owned restore offers restore-keys; a prefix-fallback match fails the row under §10.5.1")
	}
	// It must run before the bucket script, and inside the measured envelope.
	script := strings.Index(yml, "- name: Run the bucket")
	if script >= 0 && restore > script {
		t.Error("the restore runs after the bucket script; a dependency cache restored afterwards restored nothing")
	}
	begin := strings.Index(yml, "- name: Open the wall-time action envelope")
	if begin >= 0 && restore < begin {
		t.Error("the action-owned restore runs before AT_start; action-owned work outside the envelope makes the reported action shorter than the one that ran")
	}
}

// TestTheProducerValidationAcceptsEveryDeclaredProducer is §10.5.3's closed
// set: exactly none, action and caller, and nothing else.
func TestTheProducerValidationAcceptsEveryDeclaredProducer(t *testing.T) {
	script := actionStepScript(t, filepath.Join(".github", "actions", "run-bucket", "action.yml"),
		"Validate the dependency-cache producer")
	dir := t.TempDir()
	write := func(mode, key, producer string) string {
		path := filepath.Join(dir, "decl-"+mode+"-"+producer+".json")
		writeFixture(t, path, walltime.CacheDeclaration{
			DependencyCacheMode:       mode,
			DependencyCachePrimaryKey: key,
			TransformCacheMode:        "disabled",
			DependencyCacheProducer:   producer,
			ExpectedMongoBinarySHA256: "sha256:" + strings.Repeat("a", 64),
		})
		return path
	}
	cases := []struct {
		name            string
		mode, key       string
		producer        string
		hit, matched    string
		wantAccepted    bool
		wantMessagePart string
	}{
		{name: "none under disabled", mode: "disabled", producer: "none", wantAccepted: true},
		{name: "action under exact-key", mode: "exact-key", key: "deps-abc", producer: "action", wantAccepted: true},
		{name: "caller under exact-key", mode: "exact-key", key: "deps-abc", producer: "caller",
			hit: "false", matched: "", wantAccepted: true},
		{name: "action with a companion input", mode: "exact-key", key: "deps-abc", producer: "action",
			hit: "true", matched: "deps-abc", wantMessagePart: "two producers is rejected"},
		{name: "an unknown producer", mode: "exact-key", key: "deps-abc", producer: "whatever",
			wantMessagePart: "it must be none, action or caller"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(),
				"TB_PRODUCER="+tc.producer, "TB_MATCHED="+tc.matched, "TB_HIT="+tc.hit,
				"TB_DECL_FILE="+write(tc.mode, tc.key, tc.producer))
			out, err := cmd.CombinedOutput()
			if tc.wantAccepted && err != nil {
				t.Fatalf("producer %q under %s was refused: %v\n%s", tc.producer, tc.mode, err, out)
			}
			if !tc.wantAccepted {
				if err == nil {
					t.Fatalf("producer state %q was accepted:\n%s", tc.name, out)
				}
				if !strings.Contains(string(out), tc.wantMessagePart) {
					t.Errorf("the refusal does not say %q:\n%s", tc.wantMessagePart, out)
				}
			}
		})
	}
}

// TestTheAssembleStepReceivesTheMaterializedCacheState closes the wire.
//
// The producer and the consumer are in different steps, so a document written
// under one name and read under another would leave the guard permanently
// false — which is exactly the shape of the original defect.
func TestTheAssembleStepReceivesTheMaterializedCacheState(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "run-bucket", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yml := string(b)
	produce := strings.Index(yml, `echo "TB_CACHE_STATE_FILE=$state" >> "$GITHUB_ENV"`)
	consume := strings.Index(yml, `--cache-state "$TB_CACHE_STATE_FILE"`)
	if produce < 0 {
		t.Fatal("no step exports TB_CACHE_STATE_FILE; the assemble step's guard can never be true")
	}
	if consume < 0 {
		t.Fatal("the assemble step no longer passes --cache-state")
	}
	if produce > consume {
		t.Error(fmt.Sprintf("TB_CACHE_STATE_FILE is exported after it is read (%d > %d)", produce, consume))
	}
}
