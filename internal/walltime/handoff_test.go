package walltime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTheScriptHandoffIsAuthenticated is the production half of F3.
//
// The script containment identity was written as bare JSON, mode 0644, into
// the measured records directory — a directory the measured script can write.
// The script could rewrite it to name any containment at all, and the
// invocation wrappers it starts would nest under whatever it said; a missing
// or malformed handoff came back as an ordinary "no handoff", which the CLI
// silently treated as "nest under the action". So the workload could choose
// its own enclosing containment and then be accounted under the one it chose.
//
// The handoff is now signed with the run key — bound to the envelope steps and
// never to the measured work — and a present-but-unattributable one is an
// error rather than an absence.
func TestTheScriptHandoffIsAuthenticated(t *testing.T) {
	key := mustSigningKey()
	t.Setenv(RunKeyEnv, EncodeKey(key))
	dir := t.TempDir()
	ident := ContainmentIdentity{
		Primitive: PrimitiveCgroup2, ID: "/sys/fs/cgroup/testbucket/tb-action-0/tb-script-0",
		Inode: "900001", BootID: "boot-1", RootPID: 4242, RootStart: "778899",
		MembershipControl: MembershipSupervisorOwned,
	}
	run := RunIdentity{CampaignID: "ewj2", RunID: "r1", BucketID: "b0"}
	if err := writeContainmentHandoff(dir, ident, run); err != nil {
		t.Fatalf("writeContainmentHandoff: %v", err)
	}

	// It is not world-readable: it is addressed to the wrapper processes this
	// step starts, not to everything that can reach the records directory.
	info, err := os.Stat(scriptHandoffPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the handoff is mode %o; a measured script sharing this directory can read and rewrite it", perm)
	}

	got, ok, err := ScriptContainment(dir)
	if err != nil || !ok {
		t.Fatalf("the genuine handoff was refused: ok=%v err=%v", ok, err)
	}
	if !got.Same(ident) {
		t.Errorf("the handoff round-tripped as %s, want %s", got.ID, ident.ID)
	}

	// THE DEFECT: a measured script rewriting the file to name a containment
	// of its choosing. The signature is over the identity, so the edit is
	// caught rather than obeyed.
	var h ScriptHandoff
	b, err := os.ReadFile(scriptHandoffPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatal(err)
	}
	h.Containment.ID = "/sys/fs/cgroup/testbucket/attacker/tb-script-0"
	edited, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptHandoffPath(dir), edited, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := ScriptContainment(dir); err == nil {
		t.Errorf("a rewritten handoff was accepted (ok=%v); the measured script chose its own enclosing containment", ok)
	}

	// An UNSIGNED handoff is refused for the same reason: the script can write
	// this file, so an unsigned one names whatever it chose.
	h.Signature = nil
	unsigned, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptHandoffPath(dir), unsigned, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ScriptContainment(dir); err == nil {
		t.Error("an unsigned handoff was accepted")
	}

	// And a handoff signed by SOME OTHER key is not this run's.
	other := mustSigningKey()
	h.Containment = ident
	d, err := h.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	h.Signature = &Signature{
		Authority: run.CampaignID, KeyID: PublicKeyOf(other), Digest: d,
		Value: SignApproval(run.CampaignID, other, d),
	}
	foreign, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptHandoffPath(dir), foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ScriptContainment(dir); err == nil {
		t.Error("a handoff signed by a key this run does not hold was accepted")
	}
}

// TestAnAbsentHandoffIsNotAnError keeps the distinction the fix rests on: an
// invocation started outside a measured script legitimately has no enclosing
// script containment, and that must stay different from a handoff that exists
// and cannot be trusted.
func TestAnAbsentHandoffIsNotAnError(t *testing.T) {
	t.Setenv(RunKeyEnv, EncodeKey(mustSigningKey()))
	dir := t.TempDir()
	got, ok, err := ScriptContainment(dir)
	if err != nil {
		t.Errorf("an absent handoff reported an error: %v", err)
	}
	if ok || got != nil {
		t.Errorf("an absent handoff returned a containment: %+v", got)
	}
	// Present but not JSON at all is an error, not an absence.
	if err := os.WriteFile(filepath.Join(dir, "script-containment.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ScriptContainment(dir); err == nil {
		t.Error("a corrupt handoff was reported as simply absent, which the CLI would silently treat as nest-under-the-action")
	}
}
