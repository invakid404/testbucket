package main

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// currentPlan is A REAL PLAN for the pre-flight to be run against: a bundle,
// an issued Stage-2 receipt derived from it, and the two digests this command
// would have computed for itself.
//
// The helper these tests used to share handed the writer three ZERO values, so
// every attestation it produced was about no plan at all. That is exactly why
// the coverage could not see F2: a comparator that never asks which plan a
// replay is for agrees with an attestation about nothing just as readily as
// with one about this plan.
func currentPlan(t *testing.T) (walltime.PlanningInputBundle, walltime.Stage2Receipt, walltime.Digest, walltime.Digest) {
	t.Helper()
	const frozenProfileCommit = "d9ae1d433bb45012c04d567879b66fc4bf6112c6"
	var bundle walltime.PlanningInputBundle
	bundle.Kind = walltime.BundleKind
	bundle.Source.Repository = "mandel-ai/mandel"
	bundle.Source.Commit = frozenProfileCommit
	bundle.Source.Tree = "sha256:dc9c5edb8b2d479e697b4b0b8ab874f32b325138598ce9e7b759eb8292110622"
	bundleDigest, err := bundle.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	d := func(c byte) walltime.Digest {
		return walltime.DigestBytes([]byte{c})
	}
	issued := walltime.Stage2Receipt{
		Kind: walltime.Stage2Kind, Stage1Digest: d('1'), BundleDigest: bundleDigest,
		InputAccess: []walltime.InputAccess{{Field: "bundle", Digest: bundleDigest}},
		PlanDigest:  d('5'), SemanticDigest: d('6'), AtomDigest: d('7'),
		TopologyDigest: d('8'), MembershipDigest: d('9'), InvocationDigest: d('a'),
		ScriptDigest: d('b'), MatrixDigest: d('c'),
		PlannerResult: "pass", RendererResult: "pass",
		// Every real derivation is performed under a one-shot claim naming its
		// own parents, so the fixture that stands in for one carries it.
		PlannerClaim: fixtureClaim(d('1'), bundleDigest),
	}
	issued.Algorithms.FullPlan = walltime.AlgorithmIdentity{
		Name: walltime.FullPlanDigestAlgorithm, Canonicalizer: "rfc8785/v1", Implementation: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}
	issued.Algorithms.SemanticPlan = walltime.AlgorithmIdentity{
		Name: walltime.SemanticPlanDigestAlgorithm, Canonicalizer: "rfc8785/v1", Implementation: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}
	if err := issued.Validate(); err != nil {
		t.Fatalf("the fixture plan is not a valid issued receipt: %v", err)
	}
	stage2, err := issued.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	return bundle, issued, issued.Stage1Digest, stage2
}

// planOf is the pre-flight's view of that plan.
func planOf(issued walltime.Stage2Receipt, stage1, stage2 walltime.Digest) preflightPlan {
	return preflightPlan{issued: issued, stage2: stage2, stage1: stage1}
}

// attestedBy writes a replay attestation the way production does, FOR THE PLAN
// it is handed, so what these tests compare against is a document the real
// writer emits about a real plan.
func attestedBy(t *testing.T, key ed25519.PrivateKey, verifierID string,
	bundle walltime.PlanningInputBundle, issued walltime.Stage2Receipt) string {
	t.Helper()
	t.Setenv(replayKeyEnv, walltime.EncodeKey(key))
	path := filepath.Join(t.TempDir(), "replay.json")
	// Recomputed is the receipt an honest independent replay would derive: the
	// same plan. Matches compares them field by field.
	if err := writeReplayAttestation(path, verifierID, issued, bundle, issued); err != nil {
		t.Fatalf("writeReplayAttestation: %v", err)
	}
	return path
}

// TestThePreflightBindsTheVerifierIdentityToTheSignedReplay is the F2
// regression.
//
// Three of the four record identities are digests the replay re-derives, so a
// wrong one is caught by arithmetic. The verifier identity has no document
// behind it, and the pre-flight could therefore only say it was non-blank —
// which is not the contract's resolved-value equivalence. An external control
// supplied a DIFFERENT, perfectly non-blank caller identity: it passed
// pre-flight, `run-bucket` was free to open the envelope and measure, and the
// genuine signed replay disagreed with it only at post-action verification. A
// refusal afterwards can invalidate the row; it cannot un-run it.
//
// So the signed attestation is authenticated here, under the same rule the
// verifier applies, and the identity it was made under must equal the one the
// records will carry.
func TestThePreflightBindsTheVerifierIdentityToTheSignedReplay(t *testing.T) {
	replayKey, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	authorityKey, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	const genuine = "ewj2-verifier"
	declared := []string{walltime.PublicKeyOf(replayKey)}
	authorityKeys := []string{walltime.PublicKeyOf(authorityKey)}
	bundle, issued, stage1, stage2 := currentPlan(t)
	plan := planOf(issued, stage1, stage2)
	good := attestedBy(t, replayKey, genuine, bundle, issued)

	// A replay of SOME OTHER PLAN, signed correctly by a declared replay
	// signer under exactly the expected verifier identity. Everything about
	// the document authenticates; it is simply not about this plan.
	foreign := attestedBy(t, replayKey, genuine, walltime.PlanningInputBundle{}, walltime.Stage2Receipt{})

	for _, tc := range []struct {
		name          string
		path          string
		want          string
		signers, auth []string
		wantErr       string
	}{
		{name: "the identity that actually attested the plan",
			path: good, want: genuine, signers: declared, auth: authorityKeys},

		// THE DEFECT. Non-blank, so the old pre-flight passed it.
		{name: "a different non-blank caller identity",
			path: good, want: "attacker-verifier", signers: declared, auth: authorityKeys,
			wantErr: "was made by"},

		{name: "no attestation to compare against at all",
			path: "", want: genuine, signers: declared, auth: authorityKeys,
			wantErr: "--expect-attestation was not supplied"},

		// An attestation nobody declared a signer for is a file, so agreeing
		// with it proves only that the caller wrote both sides.
		{name: "a Stage 1 that declares no replay signer",
			path: good, want: genuine, signers: nil, auth: authorityKeys,
			wantErr: "no Stage-1 replay signer"},
		{name: "an attestation signed by an undeclared key",
			path: good, want: genuine, signers: []string{walltime.PublicKeyOf(authorityKey)},
			auth:    []string{"0000000000000000000000000000000000000000000000000000000000000000"},
			wantErr: "replay attestation signature"},
		{name: "a replay signer that is also the plan's authority",
			path: good, want: genuine, signers: declared, auth: declared,
			wantErr: "not an independent re-derivation"},

		// THE DEFECT. Authenticated, independent, under the right identity —
		// and about a different plan entirely. It used to open the gate, and
		// only `wall verify` refused it, after the bucket had run.
		{name: "an authenticated replay of a different plan",
			path: foreign, want: genuine, signers: declared, auth: authorityKeys,
			wantErr: "does not attest the plan being pre-flighted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkAttestedVerifier(tc.path, plan, tc.want, tc.signers, tc.auth)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("the genuine attested identity was refused: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatal("an unauthenticated or disagreeing verifier identity was accepted before AT_start")
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Errorf("error %q does not name %q", err, tc.wantErr)
			}
		})
	}
}

// TestAnAttestationSignedUnderAnotherPartysIdentityIsRefused: signatures cover
// `authority NUL digest`, so a declared replay key could otherwise sign under
// some other party's verifier id and this comparison would agree with an
// attestation that party never made.
func TestAnAttestationSignedUnderAnotherPartysIdentityIsRefused(t *testing.T) {
	key, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	bundle, issued, stage1, stage2 := currentPlan(t)
	path := attestedBy(t, key, "ewj2-verifier", bundle, issued)
	var a walltime.ReplayAttestation
	if err := walltime.ReadJSONFile(path, &a); err != nil {
		t.Fatal(err)
	}
	// The body now names a different party; the signature is still valid, and
	// still made under the original identity.
	a.VerifierID = "some-other-verifier"
	moved := filepath.Join(t.TempDir(), "replay.json")
	if err := walltime.WriteJSONFile(moved, a); err != nil {
		t.Fatal(err)
	}
	err = checkAttestedVerifier(moved, planOf(issued, stage1, stage2), "some-other-verifier", []string{walltime.PublicKeyOf(key)}, nil)
	if err == nil {
		t.Fatal("an attestation signed under another party's identity was accepted")
	}
	if !strings.Contains(err.Error(), "signed under authority") && !strings.Contains(err.Error(), "signature") {
		t.Errorf("error %q does not say the signature and the named party disagree", err)
	}
}

// TestABlankAttestedIdentityIsNotAnEquivalence: an empty identity on either
// side is not two values that agree, it is two values nobody stated.
func TestABlankAttestedIdentityIsNotAnEquivalence(t *testing.T) {
	key, err := walltime.NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	bundle, issued, stage1, stage2 := currentPlan(t)
	path := attestedBy(t, key, "ewj2-verifier", bundle, issued)
	if err := checkAttestedVerifier(path, planOf(issued, stage1, stage2), "   ", []string{walltime.PublicKeyOf(key)}, nil); err == nil {
		t.Error("a blank record verifier identity was accepted against a signed attestation")
	}
}

// TestThePreflightActionAndWorkflowCarryTheSignedReplay: the CLI can only
// compare what it is handed. The composite action had no replay input at all,
// so in production nothing reached the comparison.
func TestThePreflightActionAndWorkflowCarryTheSignedReplay(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "preflight", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	action := string(b)
	if !strings.Contains(action, "replay-attestation:") {
		t.Error("the preflight action takes no replay attestation, so the verifier identity it checks is compared with nothing")
	}
	if !strings.Contains(action, "--expect-attestation") {
		t.Error("the preflight action never passes the attestation to `wall replay`")
	}

	var preflight string
	for _, step := range workflowJobSteps(t, "test") {
		if step.name == "Pre-flight the frozen plan" {
			preflight = step.body
		}
	}
	if preflight == "" {
		t.Fatal("the test job has no preflight step")
	}
	if !strings.Contains(preflight, "replay-attestation:") {
		t.Error("the workflow never hands the signed replay to the pre-flight, so the identity comparison cannot happen before AT_start")
	}
	if !strings.Contains(preflight, "replay.json") {
		t.Error("the preflight step does not name the frozen replay document")
	}
}

// TestAnEligibleRequestMustSupplyTheSignedReplay: the guard refuses a scored
// request that cannot reach the comparison, rather than letting it measure
// first and discover the gap at verification.
func TestAnEligibleRequestMustSupplyTheSignedReplay(t *testing.T) {
	var guard string
	for _, step := range workflowJobSteps(t, "test") {
		if strings.Contains(step.body, "verify-require: eligible") {
			guard = step.body
			break
		}
	}
	if guard == "" {
		t.Fatal("the test job has no eligible guard")
	}
	// The CLAUSE, not merely the name: the guard reads the input into an
	// environment variable either way, so looking for the input name alone
	// passes against a guard that never tests it.
	if !strings.Contains(guard, `[ -z "$TB_FROZEN_DOCS" ]`) || !strings.Contains(guard, "needs frozen-documents-artifact") {
		t.Error("an eligible request may omit the frozen documents, so it carries no signed replay: the pre-flight would check only that verifier-id is non-blank, and verification would then find no replay to score against")
	}
}

// TestTheReplayCommandRunsBothIdentityComparators: `checkRecordIdentities` is
// the digest half and `checkAttestedVerifier` is the signed-identity half, and
// the pre-flight is only closed if the command runs BOTH. Calling one of them
// is the defect in a smaller shape, so the pairing is asserted structurally
// rather than left to review — the same way this repository asserts the run
// key's step scope.
func TestTheReplayCommandRunsBothIdentityComparators(t *testing.T) {
	b, err := os.ReadFile("wallplan.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, "func runWallReplay(")
	if start < 0 {
		t.Fatal("wallplan.go no longer defines runWallReplay")
	}
	end := strings.Index(src[start+1:], "\nfunc ")
	if end < 0 {
		t.Fatal("could not delimit runWallReplay")
	}
	body := src[start : start+1+end]

	at := strings.Index(body, "checkRecordIdentities(")
	bt := strings.Index(body, "checkAttestedVerifier(")
	if at < 0 || bt < 0 {
		t.Fatalf("runWallReplay calls checkRecordIdentities=%v checkAttestedVerifier=%v; the pre-flight compares the record identities without binding the verifier to a signed replay",
			at >= 0, bt >= 0)
	}
	// Both inside the SAME guarded block, so an expectation that reaches one
	// comparator reaches the other.
	guard := strings.Index(body, `if *expectStage1 != ""`)
	if guard < 0 || guard > at || guard > bt {
		t.Error("the two comparators are not both inside the expectation guard, so a request can reach one and not the other")
	}
}
