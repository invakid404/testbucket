package walltime

import (
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
	run := RunIdentity{CampaignID: "ewj2", RunID: "run-1", BucketID: "bucket-0", Stage2: "sha256:test"}
	keys := []string{PublicKeyOf(runKey)}

	dir := t.TempDir()
	if err := OpenSignerDelegation(dir, run, runKey); err != nil {
		t.Fatal(err)
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
	delegateKey, err := delegateKeyFromEnvOrDir(dir)
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
	t.Setenv(SignerDelegateKeyEnv, "")
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
		{"claiming the action level", func(d *SignerDelegation) { d.Levels = append(d.Levels, LevelAction) }},
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

	// THE DELEGATE IS NOT WORLD-READABLE. It is a capability: whoever holds it
	// can make a lower registration admissible.
	if _, err := delegateKeyFromEnvOrDir(t.TempDir()); err != nil {
		t.Errorf("a directory with no delegation should yield no delegate, not an error: %v", err)
	}
	writer, err := os.ReadFile("delegation_write_linux.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(writer), "0o600") || !strings.Contains(string(writer), "0o640") {
		t.Error("the delegate key is not restricted to the wrapper chain and the script account; a key readable by everything is not a capability")
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
