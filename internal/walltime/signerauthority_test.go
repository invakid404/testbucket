package walltime

import (
	"strings"
	"testing"
)

// TestTheMeasuredStepCannotSelfAuthorizeItsSigners is the F1 regression.
//
// keylog.jsonl lives in the records directory, which the measured step can
// write, and every logged key was added to the allowed signer set without
// anything authenticating the entry. So a hostile script running as the same
// user could mint a key for every lower-level physical, peer and trace
// producer, register them all, write ledgers signed by them, and be scored.
// The trusted closing step's seal fixed those bytes afterwards, which
// establishes finality and says nothing about who produced them.
func TestTheMeasuredStepCannotSelfAuthorizeItsSigners(t *testing.T) {
	v := verifySynth(t, nil, func(s *synthRun) { s.unauthorizedLowerKeys = true })
	found := findingsMentioning(v, "WT-032", "no run-key authorization")
	if len(found) == 0 {
		t.Fatalf("a run whose lower-level signers registered themselves raised no WT-032: %+v", v.Findings)
	}
	if v.Eligible {
		t.Error("a run whose lower-level evidence is signed by self-registered keys scored")
	}
	// The evidence is RETAINED, not treated as malformed: the records exist
	// and chain, they are simply attributable to nobody with the capability.
	if !v.Complete {
		t.Error("self-registered signers made the records incomplete; the chain is intact, only the authority is missing")
	}
	for _, f := range v.Findings {
		if f.Code == "WT-023" && strings.Contains(f.Detail, "neither the roster declared nor the key log registered") {
			t.Errorf("a registered-but-unauthorized signer was reported as undeclared: %s", f.Detail)
		}
	}
}

// TestAnAuthorizedKeyLogEntryIsBoundToItsWholeClaim: the countersignature
// covers producer, level, seq and binary, so an admitted key cannot be
// replayed under a different role than the one it was authorized for.
func TestAnAuthorizedKeyLogEntryIsBoundToItsWholeClaim(t *testing.T) {
	key := mustSigningKey()
	keys := []string{PublicKeyOf(key)}
	base := KeyLogEntry{
		Producer: ProducerPeer, Level: LevelInvocation, Seq: 3,
		PublicKey: "abc", Binary: "sha256:binary",
	}
	authorized := base
	if err := authorized.Authorize(keyLogAuthority(base), key); err != nil {
		t.Fatal(err)
	}
	if err := checkKeyLogAuthorization(authorized, keys); err != nil {
		t.Fatalf("a genuinely authorized entry was refused: %v", err)
	}
	for _, tc := range []struct {
		name string
		edit func(*KeyLogEntry)
		want string
	}{
		{"no authorization at all", func(e *KeyLogEntry) { e.Authorization = nil }, "no run-key authorization"},
		// Producer, level and seq are named in the retained authority label as
		// well as covered by the digest, so an edit is caught by the label
		// first. Either refusal is the same fact: this authorization was not
		// made for this claim.
		{"a different producer", func(e *KeyLogEntry) { e.Producer = ProducerTrace }, "not the"},
		{"a different level", func(e *KeyLogEntry) { e.Level = LevelScript }, "not the"},
		{"a different sequence", func(e *KeyLogEntry) { e.Seq = 4 }, "not the"},
		{"a different public key", func(e *KeyLogEntry) { e.PublicKey = "def" }, "does not verify"},
		{"a different binary", func(e *KeyLogEntry) { e.Binary = "sha256:other" }, "does not verify"},
		{"an authorization made for another role", func(e *KeyLogEntry) {
			other := *e
			other.Producer = ProducerTrace
			_ = e.Authorize(keyLogAuthority(other), key)
		}, "not the"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := authorized
			tc.edit(&e)
			err := checkKeyLogAuthorization(e, keys)
			if err == nil {
				t.Fatalf("an entry with %s was admitted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
	// And a key nobody predeclared cannot vouch for an entry.
	if err := checkKeyLogAuthorization(authorized, []string{PublicKeyOf(mustSigningKey())}); err == nil {
		t.Error("an entry authorized by an undeclared key was admitted")
	}
	if err := checkKeyLogAuthorization(authorized, nil); err == nil {
		t.Error("an entry was admitted with no predeclared run signer to check it against")
	}
}

// TestRegisterKeyAuthorizesWhenItHoldsTheCapability: the producer path that
// holds the run key produces admissible entries; one that does not produces
// retained-but-inadmissible ones. That asymmetry IS the control — it is what
// makes the check say whether a privileged producer path existed.
func TestRegisterKeyAuthorizesWhenItHoldsTheCapability(t *testing.T) {
	key := mustSigningKey()
	entry := KeyLogEntry{Producer: ProducerPeer, Level: LevelScript, Seq: 0, PublicKey: "abc", Binary: "sha256:b"}

	withKey := t.TempDir()
	t.Setenv(RunKeyEnv, EncodeKey(key))
	if err := RegisterKey(withKey, entry); err != nil {
		t.Fatal(err)
	}
	logged, _, err := ReadKeyLog(withKey)
	if err != nil || len(logged) != 1 {
		t.Fatalf("ReadKeyLog: %v (%d entries)", err, len(logged))
	}
	if err := checkKeyLogAuthorization(logged[0], []string{PublicKeyOf(key)}); err != nil {
		t.Errorf("a key registered by a holder of the run key is not admissible: %v", err)
	}

	without := t.TempDir()
	t.Setenv(RunKeyEnv, "")
	if err := RegisterKey(without, entry); err != nil {
		t.Fatal(err)
	}
	logged, _, err = ReadKeyLog(without)
	if err != nil || len(logged) != 1 {
		t.Fatalf("ReadKeyLog: %v (%d entries)", err, len(logged))
	}
	if logged[0].Authorization != nil {
		t.Error("a process with no run key produced an authorization anyway")
	}
	if err := checkKeyLogAuthorization(logged[0], []string{PublicKeyOf(key)}); err == nil {
		t.Error("a key registered with no run key was admissible")
	}
}
