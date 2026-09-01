package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// TestTheProductionReplayAttestationVerifies is the F1 regression, and it is
// deliberately end-to-end: the real writer, then the real verifier.
//
// The writer retained `Authority: "ewj2-campaign"` while signing over the
// VERIFIER ID, and signatures cover `authority NUL digest` — so every
// attestation production emitted was rejected by production. Existing tests
// missed it because they assembled their replay signatures by hand with a
// consistent label, exercising the verifier against a document the writer
// never produced. Nothing short of calling both halves catches that.
func TestTheProductionReplayAttestationVerifies(t *testing.T) {
	key, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(replayKeyEnv, walltime.EncodeKey(key))

	const verifierID = "independent-verifier"
	path := filepath.Join(t.TempDir(), "replay.json")
	if err := writeReplayAttestation(path, verifierID,
		walltime.Stage2Receipt{}, walltime.PlanningInputBundle{}, walltime.Stage2Receipt{}); err != nil {
		t.Fatalf("the production writer failed: %v", err)
	}

	var got walltime.ReplayAttestation
	if err := walltime.ReadJSONFile(path, &got); err != nil {
		t.Fatalf("read what production wrote: %v", err)
	}
	digest, err := got.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	if err := walltime.VerifySigned(got.Signature, digest, []string{walltime.PublicKeyOf(key)}); err != nil {
		t.Fatalf("production replay attestation is not cryptographically self-consistent: %v", err)
	}

	// The retained label is the REPLAYING PARTY, not the campaign authority.
	// Making them equal would have made the halves agree and erased the
	// distinction the attestation exists for: the replay is independent
	// precisely because it is not the party that authorised the plan.
	if got.Signature.Authority != verifierID {
		t.Errorf("the attestation retains authority %q, want the replaying party %q", got.Signature.Authority, verifierID)
	}
	if got.Signature.Authority == walltime.CampaignAuthority {
		t.Error("the replay attestation is retained under the campaign authority, which is the party whose independence it is meant to establish")
	}
}

// TestTheReplayRequiresTheExactProtectedAuthority is the F1/F2 pairing at the
// CLI boundary: `wall replay --stage1` must refuse without an expected label.
func TestTheReplayRequiresTheExactProtectedAuthority(t *testing.T) {
	// A key signs under any label it likes; only comparing the label catches
	// a manifest approved by some other protected environment.
	key, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	m := walltime.Stage1Manifest{Kind: walltime.Stage1Kind}
	if err := m.Sign("some-other-environment", key); err != nil {
		t.Fatal(err)
	}
	keys := []string{walltime.PublicKeyOf(key)}

	// The key alone accepts it.
	d, err := m.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	if err := walltime.VerifySigned(m.Signature, d, keys); err != nil {
		t.Fatalf("a correctly keyed manifest failed its own key check: %v", err)
	}
	// The exact label does not.
	if err := m.RequireApproval(keys, walltime.CampaignAuthority); err == nil {
		t.Fatal("a correctly keyed manifest approved under another environment passed the exact-authority check")
	}
	// And the genuine label does.
	if err := m.RequireApproval(keys, "some-other-environment"); err != nil {
		t.Fatalf("the genuine approval was refused: %v", err)
	}
}

// TestThePreflightRequiresTheExactAuthorityBeforeAnythingRuns is the
// workflow-level half of F2.
//
// The Go check above cannot see a workflow that never passes the label. The
// `authority` input defaulted to empty, was not forwarded to the preflight
// action, and the eligible guard required only a non-empty KEY — so a
// correctly keyed manifest approved under any label passed the pre-action gate
// and the measured action started.
func TestThePreflightRequiresTheExactAuthorityBeforeAnythingRuns(t *testing.T) {
	steps := workflowJobSteps(t, "test")
	if len(steps) == 0 || steps[0].name != "Refuse an eligible request that cannot be pre-flighted" {
		t.Fatal("the eligible guard is not the first test-job step")
	}
	// The guard requires the exact protected environment, by name.
	if !strings.Contains(steps[0].body, walltime.CampaignAuthority) {
		t.Errorf("the eligible guard does not require the exact protected authority %q", walltime.CampaignAuthority)
	}
	if !strings.Contains(steps[0].body, "inputs.authority") {
		t.Error("the eligible guard does not read the resolved authority input")
	}
	// And the preflight is handed it, so the refusal happens there too.
	var preflight string
	for _, step := range steps {
		if step.name == "Pre-flight the frozen plan" {
			preflight = step.body
		}
	}
	if preflight == "" {
		t.Fatal("the test job has no preflight step")
	}
	if !strings.Contains(preflight, "authority: ${{ inputs.authority }}") {
		t.Error("the preflight action is never given the protected authority label")
	}
}
