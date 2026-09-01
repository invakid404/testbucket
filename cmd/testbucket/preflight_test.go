package main

import (
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// TestThePreflightBindsTheIdentitiesStampedOnRecords is the F4 regression.
//
// The pre-flight replayed bundle.json, stage1.json, stage2.json, registry.json
// and the scorer, which proves the PLAN. But `run-bucket` takes the Stage-1,
// Stage-2, registry and verifier identities as four SEPARATE caller strings and
// writes them onto every measured record. An empty or mismatched one passed
// pre-flight, opened the action envelope, ran the tests, and was refused only
// by verification afterwards — which can invalidate the row but cannot
// un-measure it, and the runner has already spent the time.
func TestThePreflightBindsTheIdentitiesStampedOnRecords(t *testing.T) {
	steps := workflowJobSteps(t, "test")
	var preflight string
	for _, step := range steps {
		if step.name == "Pre-flight the frozen plan" {
			preflight = step.body
			break
		}
	}
	if preflight == "" {
		t.Fatal("the test job has no preflight step")
	}
	var missing []string
	for _, input := range []string{
		"inputs.stage1-digest", "inputs.stage2-digest",
		"inputs.registry-digest", "inputs.verifier-id",
	} {
		if !strings.Contains(preflight, input) {
			missing = append(missing, input)
		}
	}
	if len(missing) > 0 {
		t.Errorf("the preflight never consumes %v, even though run-bucket stamps those independent caller strings onto every record", missing)
	}
	// And it must reach the same step that runs the replay, so the comparison
	// happens before AT_start rather than in a later job.
	runBucket := -1
	preflightAt := -1
	for i, step := range steps {
		switch step.name {
		case "Pre-flight the frozen plan":
			preflightAt = i
		case "Run bucket ${{ matrix.bucket }}":
			runBucket = i
		}
	}
	if preflightAt < 0 || runBucket < 0 || preflightAt > runBucket {
		t.Errorf("the preflight does not precede the measured action (preflight=%d, run-bucket=%d)", preflightAt, runBucket)
	}
}

// TestTheReplayComparesTheExpectedRecordIdentities: the workflow can only pass
// the four values along; something has to actually compare them. This is the
// CLI half — `wall replay` refuses before it attests anything.
//
// Every one of the four is REQUIRED. An earlier version skipped a comparison
// whose expected value was empty, which made all four optional: a scored
// request could omit every one, pass pre-flight, open the envelope, run the
// tests, and be refused only by verification afterwards. An identity nobody
// supplied is not an identity that agrees.
func TestTheReplayComparesTheExpectedRecordIdentities(t *testing.T) {
	const derivedStage1 = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	const derivedRegistry = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	issued := walltime.Stage2Receipt{Kind: walltime.Stage2Kind}
	derivedStage2, err := issued.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                               string
		stage1, stage2, registry, verifier string
		wantErr                            string
	}{
		{"everything agrees", derivedStage1, string(derivedStage2), derivedRegistry, "ewj2-verifier", ""},
		{"nothing supplied at all", "", "", "", "", "--expect-stage1"},
		{"no Stage-2 identity", derivedStage1, "", derivedRegistry, "ewj2-verifier", "--expect-stage2"},
		{"no registry identity", derivedStage1, string(derivedStage2), "", "ewj2-verifier", "--expect-registry"},
		{"no verifier identity", derivedStage1, string(derivedStage2), derivedRegistry, "", "--expect-verifier-id"},
		{"a blank verifier identity", derivedStage1, string(derivedStage2), derivedRegistry, "   ", "--expect-verifier-id"},
		{"a Stage-1 identity the plan does not derive",
			"sha256:9999999999999999999999999999999999999999999999999999999999999999",
			string(derivedStage2), derivedRegistry, "ewj2-verifier", "--expect-stage1"},
		{"a registry identity the plan does not derive",
			derivedStage1, string(derivedStage2),
			"sha256:8888888888888888888888888888888888888888888888888888888888888888", "ewj2-verifier", "--expect-registry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkRecordIdentities(issued, derivedStage1, derivedRegistry,
				tc.stage1, tc.stage2, tc.registry, tc.verifier)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("agreeing identities were refused: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("a missing or mismatched identity was accepted")
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Errorf("error %q does not name %q", err, tc.wantErr)
			}
		})
	}
}
