package walltime

import (
	"os"
	"path/filepath"
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
	prepare := strings.Index(body, "prepareEvidenceDir(dir)")
	writer := strings.Index(body, "NewWriter(filepath.Join(dir,")
	if mkdir < 0 || prepare < 0 || writer < 0 {
		t.Fatalf("the evidence-directory path is not recognizable: mkdir=%d prepare=%d writer=%d", mkdir, prepare, writer)
	}
	if !(mkdir < prepare && prepare < writer) {
		t.Errorf("the preparation at %d does not happen between the mkdir at %d and the first ledger at %d", prepare, mkdir, writer)
	}
	// AND THE WRAPPER DOES NOT CHOWN. On Linux an unprivileged owner may set a
	// file's group only to a group it is itself in, and the wrapper is
	// deliberately not in the measured script's — so the chown this used to
	// perform could only ever fail, before a scorable envelope could open. The
	// group comes from a setgid parent the caller prepares; the wrapper sets
	// the mode, which an owner may always do, and CHECKS the result.
	// The CODE, not the comment explaining why it is gone.
	for _, line := range strings.Split(body, "\n") {
		code, _, _ := strings.Cut(line, "//")
		if strings.Contains(code, "os.Chown(dir") {
			t.Errorf("BeginAction still tries to change the evidence directory's group, which the wrapper credential cannot do: %s", strings.TrimSpace(line))
		}
	}
	prep := productionFunc(t, "contain_linux.go", "func prepareEvidenceDir(")
	if strings.Contains(prep, "os.Chown(") {
		t.Error("prepareEvidenceDir still tries to change the group")
	}
	for _, want := range []string{"os.Chmod(dir, mode)", "int(sys.Gid) != gid", "setgid"} {
		if !strings.Contains(prep, want) {
			t.Errorf("prepareEvidenceDir does not %q; an unverified delegation fails later, where it cannot be explained", want)
		}
	}
}

// TestTheKeyLogIsWrittenOnlyByTheWrapper is the F3 regression.
//
// The shared key log is created append/create mode 0644 by the wrapper before
// the bucket script starts. The nested invocation wrappers used to be launched
// by the measured script and to run as the SEPARATE script credential, which
// cannot write an existing 0644 file owned by somebody else however the
// directory is delegated — so the authorization path that had just been built
// could not produce its own ledger.
//
// The controller dissolves it: the nested wrapper asks and the wrapper
// registers. What has to hold is that the asking side never writes.
func TestTheKeyLogIsWrittenOnlyByTheWrapper(t *testing.T) {
	// The ledger stays owner-writable and owner-owned; nothing widens it,
	// because widening it would let the measured party choose its attesters.
	dir := t.TempDir()
	if err := RegisterKeyWith(dir, KeyLogEntry{
		Producer: ProducerPhysical, Level: LevelInvocation, Seq: 0, PublicKey: "pk",
	}, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, keyLogFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o022 != 0 {
		t.Errorf("the key log is group- or world-writable (%v); the measured party could then register its own signers", info.Mode())
	}

	// AND THE ASKING SIDE NEVER REGISTERS. The invocation client returns the
	// controller's answer before any of `walltime.Exec` runs, so it opens no
	// ledger, registers no key and creates no containment.
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "testbucket", "wall.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	ask := strings.Index(src, "walltime.RequestInvocation(*dir, opt.Seq, *spec)")
	measure := strings.Index(src, "code, err := walltime.Exec(opt)")
	if ask < 0 || measure < 0 {
		t.Fatalf("the invocation path is not recognizable: ask=%d measure=%d", ask, measure)
	}
	if ask > measure {
		t.Errorf("the client measures at %d before asking at %d, so it would write the ledgers itself", measure, ask)
	}
}
