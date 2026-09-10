package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheShippedActionCompositionAssemblesAndIngests runs the run-bucket
// action's ACTUAL nesting, for both of its branches.
//
// The composite action runs the consumer's setup command through `wall run`
// and then the generated bucket script through `wall run --wrapper-chain`.
// `RunInActionWith` never branched on WrapperChain, so BOTH calls emitted a
// setup lifecycle:
//
//   - with no consumer setup, the single `setup` interval CONTAINED the script
//     it went on to start, and assembly refused the observation on §3.1's own
//     floor — "A (1234756000) < setup_ns + script_ns (1858303000)";
//   - with consumer setup, two lifecycles landed in one stream and verification
//     exited 1 on WT-020.
//
// Neither branch of the shipped action could produce an observation, so
// production learnability was zero. The existing end-to-end test recorded one
// setup lifecycle and then started the script lifecycle DIRECTLY, which is not
// what the action does — which is why an ingest test could pass while nothing
// shipped could ingest.
func TestTheShippedActionCompositionAssemblesAndIngests(t *testing.T) {
	for _, tc := range []struct {
		name     string
		setup    bool
		wantSetu bool
	}{
		{name: "no consumer setup command", setup: false},
		{name: "with a consumer setup command", setup: true, wantSetu: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := planBinary(t)
			dir := t.TempDir()
			records := filepath.Join(dir, "records")
			obsDir := filepath.Join(dir, "obs")
			for _, d := range []string{records, obsDir} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			run := func(args ...string) {
				t.Helper()
				cmd := exec.Command(bin, args...)
				var stderr strings.Builder
				cmd.Stderr = &stderr
				if err := cmd.Run(); err != nil {
					t.Fatalf("%v: %v\n%s", args, err, stderr.String())
				}
			}

			run("wall", "begin", "--dir", records, "--bucket-id", "bucket-0")
			if tc.setup {
				// THE CONSUMER'S COMMAND. No --wrapper-chain: it is somebody
				// else's code and gets the scrubbed environment. This is the
				// one call that owns the `setup` level.
				run("wall", "run", "--dir", records, "--", "bash", "-eo", "pipefail", "-c", "true")
			}
			// THE WRAPPER CHAIN CONTINUING, exactly as the action spells it:
			// `wall run --wrapper-chain` starts a bash that runs the generated
			// script, which is itself measured at the script level and which
			// runs the rendered invocation.
			invocation := fmt.Sprintf("%s wall exec --dir %s --level invocation --bucket-id bucket-0 --cwd %s -- sh -c true",
				bin, records, dir)
			script := fmt.Sprintf("%s wall exec --dir %s --level script --bucket-id bucket-0 --cwd %s -- sh -c '%s'",
				bin, records, dir, invocation)
			run("wall", "run", "--dir", records, "--wrapper-chain", "--",
				"bash", "-euo", "pipefail", "-c", script)
			run("wall", "end", "--dir", records, "--terminal", "passed")

			plan, _ := writePlanDeclaring(t, dir, "bucket-0", 0, []string{"sh", "-c", "true"}, dir,
				observedProfileOf(t, bin))

			// The verifier the action itself runs must accept the records.
			verify := exec.Command(bin, "wall", "verify", "--dir", records, "--shard-plan", plan)
			var vErr strings.Builder
			verify.Stderr = &vErr
			verify.Stdout = &vErr
			if err := verify.Run(); err != nil {
				t.Fatalf("wall verify refused the shipped composition: %v\n%s", err, vErr.String())
			}

			obsFile := filepath.Join(obsDir, "bucket-0.json")
			assemble := exec.Command(bin, "wall", "assemble-observation",
				"--dir", records, "--shard-plan", plan, "--bucket-name", "bucket-0",
				"--out", obsFile, "--runs-on-label", "ubuntu-latest",
				"--repository", "owner/name", "--run-id", "run-9", "--attempt-id", "1",
				"--job", "job-1", "--head-sha", strings.Repeat("1", 40),
				"--candidate-sha", strings.Repeat("2", 40),
				"--workload-commit", strings.Repeat("3", 40))
			var aErr strings.Builder
			assemble.Stderr = &aErr
			if err := assemble.Run(); err != nil {
				t.Fatalf("assemble-observation refused the shipped composition: %v\n%s", err, aErr.String())
			}

			var obs struct {
				ElapsedNs string `json:"elapsed_ns"`
				SetupNs   string `json:"setup_ns"`
				ScriptNs  string `json:"script_ns"`
			}
			b, err := os.ReadFile(obsFile)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b, &obs); err != nil {
				t.Fatal(err)
			}
			// §3.1's floor, on the values that shipped.
			a, setup, scriptNs := asNs(t, obs.ElapsedNs), asNs(t, obs.SetupNs), asNs(t, obs.ScriptNs)
			if a < setup+scriptNs {
				t.Errorf("A (%d) < setup_ns (%d) + script_ns (%d); the wrapper chain is being charged twice",
					a, setup, scriptNs)
			}
			if tc.wantSetu && setup == 0 {
				t.Error("setup_ns is 0 although a consumer setup command ran")
			}
			if !tc.wantSetu && setup != 0 {
				t.Errorf("setup_ns is %d with no consumer setup command; the wrapper chain is not a level", setup)
			}
			if scriptNs == 0 {
				t.Error("script_ns is 0; the generated bucket script was not measured")
			}

			store := filepath.Join(dir, "store.json")
			duplicateFixtureStore(t, store)
			events := writeEvents(t, dir)
			out := ingestObservations(t, bin, store, obsDir, plan, events)
			if strings.Contains(out, "REJECT") {
				t.Fatalf("the shipped ingest REJECTED the shipped action's own observation:\n%s", out)
			}
			if rows := ringRows(t, store); len(rows) != 1 {
				t.Fatalf("the ring holds %d row(s); the shipped composition must be learnable:\n%s",
					len(rows), out)
			}
		})
	}
}

// asNs reads one of the observation's string-encoded nanosecond counts.
func asNs(t *testing.T, s string) int64 {
	t.Helper()
	if s == "" {
		return 0
	}
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		t.Fatalf("nanosecond value %q: %v", s, err)
	}
	return n
}
