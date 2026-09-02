package walltime

import (
	"os"
	"strings"
	"testing"
)

// TestTheEvidenceDirectoryIsDelegatedAppendOnly is the F2 regression.
//
// The measured bucket script runs as its own account and must create files in
// the evidence directory: one invocation spec per call, and the nested ledgers
// the invocation wrappers open beside them. The directory was 0755 and owned
// by the wrapper, so it could create none of them — the two-account path
// stopped before it could produce a single row.
//
// General write access is not the answer either: the measured script would
// then be able to rewrite the evidence being attested. What it gets is CREATE
// and nothing more.
func TestTheEvidenceDirectoryIsDelegatedAppendOnly(t *testing.T) {
	// Without a script account there is nothing to separate, and the mode is
	// unchanged: the wrapper chain and the measured script are one credential.
	t.Setenv(ScriptUserEnv, "")
	if gid, mode := evidenceDirDelegation(); gid >= 0 || mode.Perm() != 0o755 {
		t.Errorf("an undeclared script account delegated gid=%d mode=%v", gid, mode)
	}

	// With one, the mode must grant the group create access, carry setgid so
	// new files inherit the group, and carry the STICKY bit so only an owner
	// may remove or rename what is already there.
	_, mode := evidenceDirDelegationFor([]int{4242})
	switch {
	case mode.Perm()&0o070 != 0o070:
		t.Errorf("mode %v does not let the script account create files", mode)
	case mode&os.ModeSetgid == 0:
		t.Errorf("mode %v is not setgid, so files the script creates would not carry the directory's group", mode)
	case mode&os.ModeSticky == 0:
		t.Errorf("mode %v is not sticky, so the measured script could delete or replace the wrapper's ledgers", mode)
	case mode.Perm()&0o007 != 0:
		t.Errorf("mode %v grants access beyond the delegated group", mode)
	}

	// AND THE LEDGERS THEMSELVES STAY WRAPPER-OWNED. The delegation is of the
	// directory, not of the evidence in it: a ledger the script could rewrite
	// is not evidence about the script.
	dir := t.TempDir()
	key := mustSigningKey()
	w, err := NewWriter(dir+"/physical_wrapper.action.jsonl", ProducerPhysical, ProducerID(ProducerPhysical), key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(Record{Kind: "boundary", Boundary: "start", Level: LevelAction}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir + "/physical_wrapper.action.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o022 != 0 {
		t.Errorf("a ledger is group- or world-writable (%v); the delegated account could rewrite the evidence being attested", info.Mode())
	}

	// And the production path applies it where the directory is created.
	body := productionFunc(t, "action.go", "func BeginAction(")
	mkdir := strings.Index(body, "os.MkdirAll(dir, 0o755)")
	chmod := strings.Index(body, "evidenceDirDelegation(); gid >= 0")
	writer := strings.Index(body, "NewWriter(filepath.Join(dir,")
	if mkdir < 0 || chmod < 0 || writer < 0 {
		t.Fatalf("the evidence-directory path is not recognizable: mkdir=%d chmod=%d writer=%d", mkdir, chmod, writer)
	}
	if !(mkdir < chmod && chmod < writer) {
		t.Errorf("the delegation at %d does not happen between the mkdir at %d and the first ledger at %d", chmod, mkdir, writer)
	}
	for _, want := range []string{"os.Chown(dir, -1, gid)", "os.Chmod(dir, mode)"} {
		if !strings.Contains(body, want) {
			t.Errorf("BeginAction does not apply %s, so the declared script account cannot create a single file here", want)
		}
	}
}
