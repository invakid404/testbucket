package walltime

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLowerProducerKeysHaveAPrivilegedAuthorizer is the F3 regression.
//
// The run key authorizes key-log registrations and is scoped to `wall begin`
// and `wall end`, so the measured step cannot forge the signer set. The script
// and invocation producers mint their keys DURING that step — so every one of
// them was registered unauthorized, and the verifier refused every row for
// want of an authorization no party present could produce. The delegation is
// that party: opened by `wall begin` with the run key, bound to one run, and
// limited to the levels the wrapper chain actually produces.
func TestLowerProducerKeysHaveAPrivilegedAuthorizer(t *testing.T) {
	runKey := mustSigningKey()
	run := RunIdentity{CampaignID: "ewj2", RunID: "run-1", BucketID: "bucket-0", Stage2: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}
	keys := []string{PublicKeyOf(runKey)}

	dir := t.TempDir()
	encoded, err := OpenSignerDelegation(dir, run, runKey)
	if err != nil {
		t.Fatal(err)
	}
	if encoded == "" {
		t.Fatal("no delegate was returned for the measured step to hold")
	}
	d, err := LoadSignerDelegation(dir)
	if err != nil || d == nil {
		t.Fatalf("no delegation was opened: %v", err)
	}
	delegate, err := d.Verify(run, keys)
	if err != nil {
		t.Fatalf("the delegation the run key signed does not verify: %v", err)
	}

	// A LOWER KEY THE DELEGATE VOUCHED FOR is admissible.
	lower := KeyLogEntry{Producer: ProducerPhysical, Level: LevelInvocation, Seq: 3, PublicKey: "pk-lower"}
	t.Setenv(SignerDelegateKeyEnv, encoded)
	delegateKey, err := delegateKeyFromEnv()
	if err != nil || delegateKey == nil {
		t.Fatalf("the wrapper chain cannot read the delegate: %v", err)
	}
	if err := lower.Authorize(keyLogAuthority(lower), delegateKey); err != nil {
		t.Fatal(err)
	}
	if err := checkKeyLogAuthorization(lower, keys, append(keys, delegate), d); err != nil {
		t.Errorf("a lower key the run key's delegate authorized was refused: %v", err)
	}

	// AND THE REGISTRATION PATH USES IT WITHOUT A RUN KEY, which is the whole
	// situation: the measured step has no run key, and the entry it writes
	// must still carry an authorization somebody with a capability made.
	t.Setenv(RunKeyEnv, "")
	t.Setenv(SignerDelegateKeyEnv, encoded)
	if err := RegisterLowerKey(dir, KeyLogEntry{
		Producer: ProducerPeer, Level: LevelScript, Seq: 0, PublicKey: "pk-peer",
	}, run); err != nil {
		t.Fatal(err)
	}
	logged, _, err := ReadKeyLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range logged {
		if e.PublicKey != "pk-peer" {
			continue
		}
		found = true
		if e.Authorization == nil {
			t.Fatal("a lower key registered from the measured step carries no authorization; the delegation was not used")
		}
		if err := checkKeyLogAuthorization(e, keys, append(keys, delegate), d); err != nil {
			t.Errorf("the registered lower key is not admissible: %v", err)
		}
	}
	if !found {
		t.Fatal("the lower key was not registered at all")
	}

	// THE SAME DELEGATE CANNOT REGISTER AN ACTION SIGNER. Those are declared
	// by the roster `wall begin` seals with the run key itself.
	action := KeyLogEntry{Producer: ProducerPhysical, Level: LevelAction, Seq: 0, PublicKey: "pk-action"}
	if err := action.Authorize(keyLogAuthority(action), delegateKey); err != nil {
		t.Fatal(err)
	}
	if err := checkKeyLogAuthorization(action, keys, append(keys, delegate), d); err == nil {
		t.Error("the delegate registered an action-level signer; a delegate that can do that is the run key")
	}
	if err := RegisterLowerKey(t.TempDir(), action, run); err == nil {
		t.Error("the lower-key registration path accepted an action-level entry")
	}

	// AND A DELEGATION NOBODY WITH THE RUN KEY SIGNED authorizes nothing.
	for _, tc := range []struct {
		name string
		edit func(*SignerDelegation)
	}{
		{"unsigned", func(d *SignerDelegation) { d.Signature = nil }},
		{"for another run", func(d *SignerDelegation) { d.Run.RunID = "somebody-elses-run" }},
		{"authorizing nothing", func(d *SignerDelegation) { d.Scopes = nil }},
		{"signed by a stranger", func(d *SignerDelegation) {
			if err := d.Sign(mustSigningKey()); err != nil {
				t.Fatal(err)
			}
		}},
		{"naming a different delegate than it signed", func(d *SignerDelegation) { d.PublicKey = "pk-substituted" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := *d
			tc.edit(&c)
			if _, err := c.Verify(run, keys); err == nil {
				t.Errorf("a delegation %s was accepted", tc.name)
			}
		})
	}

	// A PROPERLY SIGNED DELEGATION THAT CLAIMS THE ACTION ENVELOPE'S OWN
	// SIGNERS is refused on its SCOPE, not on its signature.
	//
	// It is built and signed here rather than edited after signing, because an
	// edited document is caught by the signature check and would prove only
	// that. Sequence 0 at the action level belongs to the roster: a delegate
	// that could register one could substitute the envelope's own producer.
	for _, bad := range []DelegationScope{{Level: LevelAction, MinSeq: 0}, {Level: "", MinSeq: 0}} {
		over := SignerDelegation{
			Kind: SignerDelegationKind, Run: run, PublicKey: PublicKeyOf(mustSigningKey()),
			Scopes: []DelegationScope{bad}, Binary: SelfDigest(),
		}
		if err := over.Sign(runKey); err != nil {
			t.Fatal(err)
		}
		if _, err := over.Verify(run, keys); err == nil {
			t.Errorf("a validly signed delegation scoped %+v was accepted", bad)
		}
	}

	// AN ABSENT DELEGATE IS ABSENT, not an error: that is the developer run
	// with no capability to delegate, which is reported ineligible rather than
	// scored.
	t.Setenv(SignerDelegateKeyEnv, "")
	if k, err := delegateKeyFromEnv(); err != nil || k != nil {
		t.Errorf("an unset delegate yielded key=%v err=%v", k != nil, err)
	}
	t.Setenv(SignerDelegateKeyEnv, encoded)
	// THE PRIVATE HALF IS NEVER ON DISK. It used to be written into the
	// evidence directory, group-readable by the measured script and openable
	// by every observer through the `--dir` it is handed.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(b), encoded) {
			t.Errorf("%s in the evidence directory contains the delegate private key", e.Name())
		}
	}
	if _, err := LoadSignerDelegation(filepath.Join(dir, "nope")); err != nil {
		t.Errorf("a missing delegation should read as absent: %v", err)
	}
}

// TestTheDelegationIsOpenedWhereTheRunKeyIs: the document is only worth
// anything if the step that holds the run key opens it, before the measured
// work starts.
func TestTheDelegationIsOpenedWhereTheRunKeyIs(t *testing.T) {
	body := productionFunc(t, "action.go", "func BeginAction(")
	open := strings.Index(body, "OpenSignerDelegation(dir, run, runKey)")
	roster := strings.Index(body, "WriteRoster(dir, roster)")
	if open < 0 {
		t.Fatal("BeginAction opens no signer delegation, so the lower producers have no authorizing party")
	}
	if roster < 0 || open < roster {
		t.Errorf("the delegation at %d is opened before the roster at %d is sealed", open, roster)
	}
	// And the measured step's registrations go through the path that uses it.
	exec := productionFunc(t, "exec.go", "func Exec(")
	observer := productionFunc(t, "exec.go", "func startObserver(")
	for what, body := range map[string]string{"the physical wrapper": exec, "the observers": observer} {
		if !strings.Contains(body, "RegisterLowerKey(opt.Dir") {
			t.Errorf("%s does not register through the delegated path", what)
		}
	}
}

// TestTheActionChildSignerIsAdmissible is the F5 regression.
//
// `openActionChild` mints an ACTION-level signer during the measured step,
// where no run key exists, so it authorized with the delegate — and the
// delegation covered only the script and invocation levels, so the candidate's
// own verifier rejected every one of them and every setup or run child made
// the row ineligible.
//
// The scope is bounded by SEQUENCE rather than widened by level: the action
// envelope's own physical, peer and trace signers are sequence 0 and stay with
// the roster the run key seals; the action-owned children start at 1.
func TestTheActionChildSignerIsAdmissible(t *testing.T) {
	runKey := mustSigningKey()
	run := RunIdentity{CampaignID: "ewj2", RunID: "action-child", BucketID: "bucket-0", Stage2: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}
	dir := t.TempDir()
	encoded, err := OpenSignerDelegation(dir, run, runKey)
	if err != nil {
		t.Fatal(err)
	}
	d, err := LoadSignerDelegation(dir)
	if err != nil || d == nil {
		t.Fatalf("no delegation: %v", err)
	}
	keys := []string{PublicKeyOf(runKey)}
	delegate, err := d.Verify(run, keys)
	if err != nil {
		t.Fatal(err)
	}
	authorizers := append(append([]string{}, keys...), delegate)

	t.Setenv(RunKeyEnv, "")
	t.Setenv(SignerDelegateKeyEnv, encoded)

	// THE ACTION-OWNED CHILD, at sequence 1 and up, is admissible.
	for _, seq := range []int{1, 2, 17} {
		entry := KeyLogEntry{
			Producer: ProducerPhysical, Level: LevelAction, Seq: seq,
			PublicKey: PublicKeyOf(mustSigningKey()), Binary: SelfDigest(),
		}
		child := t.TempDir()
		if err := RegisterKeyFor(child, entry, run); err != nil {
			t.Fatalf("seq %d: %v", seq, err)
		}
		logged, _, err := ReadKeyLog(child)
		if err != nil || len(logged) != 1 {
			t.Fatalf("seq %d: ReadKeyLog: %v entries=%d", seq, err, len(logged))
		}
		if err := checkKeyLogAuthorization(logged[0], keys, authorizers, d); err != nil {
			t.Errorf("an action-owned child signer at sequence %d was rejected: %v", seq, err)
		}
	}

	// THE ACTION ENVELOPE'S OWN SIGNERS ARE NOT. Sequence 0 belongs to the
	// roster `wall begin` seals with the run key; a delegate that could
	// register one could substitute the envelope's own producer.
	envelope := KeyLogEntry{
		Producer: ProducerPhysical, Level: LevelAction, Seq: 0,
		PublicKey: PublicKeyOf(mustSigningKey()), Binary: SelfDigest(),
	}
	if err := envelope.Authorize(keyLogAuthority(envelope), mustDecode(t, encoded)); err != nil {
		t.Fatal(err)
	}
	if err := checkKeyLogAuthorization(envelope, keys, authorizers, d); err == nil {
		t.Error("the delegate registered an action-ENVELOPE signer; the roster is what declares those")
	}
	// And the production child path takes a sequence above 0.
	body := productionFunc(t, "action.go", "func openActionChild(")
	if !strings.Contains(body, "actionChildSeq(dir) + 1") {
		t.Error("the action-child signer does not take a sequence the delegation can cover")
	}
}

func mustDecode(t *testing.T, encoded string) ed25519.PrivateKey {
	t.Helper()
	k, err := DecodeKey(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// TestADelegationIsBoundToTheWholeRunIdentity is the F5 regression.
//
// The delegation says it authorizes one run, and Verify compared RunID alone.
// Every bucket of a matrix shares one GitHub run id and every retry attempt
// keeps it — so a delegate minted for one bucket authorized the signers of
// every other bucket and of every later attempt, a replay across exactly the
// boundaries the identity exists to separate.
func TestADelegationIsBoundToTheWholeRunIdentity(t *testing.T) {
	runKey := mustSigningKey()
	want := RunIdentity{
		CampaignID: "ewj2", RunID: "shared-run", AttemptID: "1", BucketID: "bucket-0",
		Repository: "invakid404/testbucket", WorkflowRun: "100", Job: "test",
		Step: "run-bucket", StepAttempt: "1",
		Stage1: "sha256:e8bc163c82eee18733288c7d4ac636db3a6deb013ef2d37b68322be20edc45cc", Stage2: "sha256:ad328846aa18b32a335816374511cac1063c704b8c57999e51da9f908290a7a4",
		ComponentRegistry: "sha256:872491a30d60d598962de6e7b834ab76b2aa65fbab102c6ebaaae6acdc238822", VerifierID: "ewj2-verifier",
	}
	d := SignerDelegation{
		Kind: SignerDelegationKind, Run: want, PublicKey: PublicKeyOf(mustSigningKey()),
		Scopes: []DelegationScope{{Level: LevelInvocation, MinSeq: 0}}, Binary: SelfDigest(),
	}
	if err := d.Sign(runKey); err != nil {
		t.Fatal(err)
	}
	keys := []string{PublicKeyOf(runKey)}
	if _, err := d.Verify(want, keys); err != nil {
		t.Fatalf("the run it was minted for was refused: %v", err)
	}

	// EVERY FIELD IS A BOUNDARY. Each of these keeps the same RunID.
	for _, tc := range []struct {
		name string
		edit func(*RunIdentity)
	}{
		{"another bucket of the same matrix", func(r *RunIdentity) { r.BucketID = "bucket-7" }},
		{"a later attempt of the same run", func(r *RunIdentity) { r.AttemptID = "2" }},
		{"another campaign", func(r *RunIdentity) { r.CampaignID = "somebody-elses" }},
		{"another repository", func(r *RunIdentity) { r.Repository = "someone/else" }},
		{"another job", func(r *RunIdentity) { r.Job = "other-job" }},
		{"another step attempt", func(r *RunIdentity) { r.StepAttempt = "3" }},
		{"another Stage-1 manifest", func(r *RunIdentity) {
			r.Stage1 = "sha256:d9298a10d1b0735837dc4bd85dac641b0f3cef27a47e5d53a54f2f3f5b2fcffa"
		}},
		{"another Stage-2 receipt", func(r *RunIdentity) {
			r.Stage2 = "sha256:d9298a10d1b0735837dc4bd85dac641b0f3cef27a47e5d53a54f2f3f5b2fcffa"
		}},
		{"another component registry", func(r *RunIdentity) {
			r.ComponentRegistry = "sha256:d9298a10d1b0735837dc4bd85dac641b0f3cef27a47e5d53a54f2f3f5b2fcffa"
		}},
		{"another verifier", func(r *RunIdentity) { r.VerifierID = "other-verifier" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := want
			tc.edit(&other)
			if other.RunID != want.RunID {
				t.Fatal("this case must keep the RunID, or it proves nothing about the old comparison")
			}
			if _, err := d.Verify(other, keys); err == nil {
				t.Errorf("a delegation minted for %s was accepted for %s solely because the run id matched", want.BucketID, tc.name)
			}
		})
	}

	// A caller with no run loaded asks only for the document's own claims to
	// be validated, which is how the delegation is checked before a run
	// identity is known.
	if _, err := d.Verify(RunIdentity{}, keys); err != nil {
		t.Errorf("validating the document alone was refused: %v", err)
	}
}
