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

// TestTheProvisioningRecipeIsRunnable is the F4 regression.
//
// Two commands in the documented setup could never work, and both fail
// silently in ways that look like something else:
//
//   - `sudo install -d -m 2775 -g <group> <parent>` without `-o` leaves the
//     parent owned by ROOT. The wrapper is not in the script group, so mode
//     2775 gives it only the other-class bits, and `BeginAction`'s own
//     `MkdirAll` fails with permission denied before the explanatory refusal
//     can run.
//   - `usermod -aG` does not change a process that already exists.
//     Supplementary groups are fixed at process creation, so the running
//     GitHub runner and every step it starts never acquire the group; the
//     cgroup root stays unwritable however correct /etc/group looks.
func TestTheProvisioningRecipeIsRunnable(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "run-bucket", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yml := string(b)

	// The evidence parent is created WITH AN EXPLICIT OWNER.
	install := strings.Index(yml, "install -d -m 2775")
	if install < 0 {
		t.Fatal("the provisioning example no longer creates a setgid evidence parent")
	}
	line := yml[install:]
	if end := strings.IndexByte(line, '\n'); end > 0 {
		line = line[:end]
	}
	if !strings.Contains(line, "-o ") {
		t.Errorf("the evidence parent is created without an explicit owner, so it belongs to root and the wrapper cannot create the wall directory inside it: %s", strings.TrimSpace(line))
	}
	if !strings.Contains(line, "-g ") {
		t.Errorf("the evidence parent carries no group for the setgid inheritance to apply: %s", strings.TrimSpace(line))
	}

	// The cgroup root is OWNED by the wrapper rather than granted through a
	// group the live runner cannot join.
	for _, line := range strings.Split(yml, "\n") {
		code, _, _ := strings.Cut(line, "#")
		if strings.Contains(code, "usermod -aG") {
			t.Errorf("the provisioning still adds the live runner to a group, which does not affect the already-running process: %s", strings.TrimSpace(line))
		}
		if strings.Contains(code, "chown root:tb-wrapper") {
			t.Errorf("the cgroup root is given to a group the running wrapper does not hold: %s", strings.TrimSpace(line))
		}
	}
	// AND IT IS A DELEGATED SUBTREE, NOT A DIRECTORY BESIDE THE RUNNER'S OWN
	// CGROUP.
	//
	// Owning `/sys/fs/cgroup/testbucket` was the published recipe and could
	// never admit even the action root: cgroup-v2 authorises a migration at
	// the COMMON ANCESTOR of source and destination, which for that path is
	// the root cgroup. The root has to hang off the runner's own cgroup, and
	// that cgroup's membership file has to be given to the same credential.
	for _, want := range []string{
		`awk -F: '$1 == "0" { print $3 }' /proc/self/cgroup`,
		`sudo chown "$(id -un)" "/sys/fs/cgroup${own}" "/sys/fs/cgroup${own}/cgroup.procs"`,
		`"/sys/fs/cgroup${own}/testbucket"`,
	} {
		if !strings.Contains(yml, want) {
			t.Errorf("the provisioning does not delegate a usable subtree; it is missing %s", want)
		}
	}
	if strings.Contains(yml, `sudo chown "$(id -un)" /sys/fs/cgroup/testbucket`) {
		t.Error("the provisioning still gives the runner a cgroup root beside its own, whose common ancestor is the root cgroup — the first migration fails with permission denied")
	}
	// And the reason is written down where the next person will read it.
	for _, want := range []string{
		"does NOT change a process that is already running",
		"membership must be established BEFORE the",
		"without an explicit owner the parent belongs to",
	} {
		if !strings.Contains(yml, want) {
			t.Errorf("the provisioning does not explain %q", want)
		}
	}

	// The wrapper's own diagnostic names the same fix, including the -o.
	prep := productionFunc(t, "contain_linux.go", "func prepareEvidenceDir(")
	if !strings.Contains(prep, `-o \"$(id -un)\"`) {
		t.Error("the refusal recommends an install without an explicit owner, which cannot work")
	}
}
