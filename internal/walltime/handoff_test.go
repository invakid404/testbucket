package walltime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// TestTheProductionNoKeyPathPublishesNothing is the F3 regression.
//
// The writer signed only when a run key was present and the reader accepted
// both signed and unsigned documents when one was absent — and absent is the
// production configuration, because the run key is scoped to `wall begin` and
// `wall end` while the script wrapper that writes this file runs in the
// measured step. So the real path always emitted an unsigned document into a
// directory the measured script can write, and mode 0600 protects nothing from
// the uid that owns it.
//
// Nothing is published now when nothing can be signed, and an unsigned handoff
// is never accepted. The invocation wrapper asks the kernel where it already
// is instead, which is the answer the workload cannot forge.
func TestTheProductionNoKeyPathPublishesNothing(t *testing.T) {
	t.Setenv(RunKeyEnv, "")
	dir := t.TempDir()
	run := RunIdentity{CampaignID: "ewj2", RunID: "production-shape", BucketID: "0"}
	ident := ContainmentIdentity{
		Primitive: PrimitiveCgroup2, ID: "/sys/fs/cgroup/testbucket/tb-action-0/tb-script-0",
		Inode: "900001", BootID: "boot", RootPID: 4242, RootStart: "778899",
		MembershipControl: MembershipSupervisorOwned,
	}
	if err := writeContainmentHandoff(dir, ident, run); err != nil {
		t.Fatalf("writeContainmentHandoff: %v", err)
	}
	if _, err := os.Stat(scriptHandoffPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("the no-key path published a handoff it could not sign (stat err=%v)", err)
	}

	// And if one is planted there anyway — which is exactly what a measured
	// script would do — it is refused rather than obeyed.
	planted := ScriptHandoff{Kind: ScriptHandoffKind, Run: run, Containment: ident}
	planted.Containment.ID = "/sys/fs/cgroup/testbucket/attacker/tb-script-0"
	b, err := json.Marshal(planted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptHandoffPath(dir), b, 0o600); err != nil {
		t.Fatal(err)
	}
	got, present, err := ScriptContainment(dir)
	if err == nil {
		t.Fatalf("an unsigned handoff was accepted in the production no-key path: got=%+v present=%v", got, present)
	}
	if !strings.Contains(err.Error(), "unsigned") {
		t.Errorf("error %q does not say the handoff was unsigned", err)
	}

	// A SIGNED one is refused too when this process cannot check it: an
	// unverifiable handoff is not a handoff.
	signed := planted
	key := mustSigningKey()
	d, err := signed.DigestOf()
	if err != nil {
		t.Fatal(err)
	}
	signed.Signature = &Signature{
		Authority: run.CampaignID, KeyID: PublicKeyOf(key), Digest: d,
		Value: SignApproval(run.CampaignID, key, d),
	}
	sb, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptHandoffPath(dir), sb, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ScriptContainment(dir); err == nil {
		t.Error("a handoff this process holds no key to check was accepted")
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
