package walltime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRun() RunIdentity {
	return RunIdentity{CampaignID: "ewj2", RunID: "run-1", BucketID: "bucket-0"}
}

// TestTheSupervisorAuthorizesLowerSignersTheWorkloadCannot is the F1 repair.
//
// A lower-level producer key is admissible only if the run key countersigned
// it, and the run key must never reach the measured step — a wrapper the
// workload started is a wrapper the workload can inspect and invoke. The
// capability therefore lives in a process running under a credential the
// workload does not have, and the wrapper ASKS.
func TestTheSupervisorAuthorizesLowerSignersTheWorkloadCannot(t *testing.T) {
	key := mustSigningKey()
	p := NewSupervisorPolicy(testRun(), "/sys/fs/cgroup/testbucket", 4242)
	entry := KeyLogEntry{
		Producer: ProducerPeer, Level: LevelInvocation, Seq: 0,
		PublicKey: "peer-key", Binary: "sha256:binary",
	}
	authorized, err := p.Authorize(SupervisorRequest{
		Kind: SupervisorAuthorizeKey, Run: testRun(), Entry: &entry,
	}, key)
	if err != nil {
		t.Fatalf("the supervisor refused a genuine lower-level registration: %v", err)
	}
	// What it produces is admissible under exactly the rule the verifier
	// applies, checked against the predeclared run signer.
	if err := checkKeyLogAuthorization(*authorized, []string{PublicKeyOf(key)}); err != nil {
		t.Errorf("a supervisor-authorized entry is not admissible: %v", err)
	}
}

// TestTheSupervisorRefusesWhatTheWorkloadWouldAskFor: holding the capability is
// only a boundary if the answers are bounded. Each of these is a request a
// measured workload could make through a wrapper it started.
func TestTheSupervisorRefusesWhatTheWorkloadWouldAskFor(t *testing.T) {
	key := mustSigningKey()
	run := testRun()
	fresh := func() *SupervisorPolicy { return NewSupervisorPolicy(run, "/sys/fs/cgroup/testbucket", 4242) }
	entry := func() *KeyLogEntry {
		return &KeyLogEntry{Producer: ProducerPeer, Level: LevelScript, Seq: 0, PublicKey: "k", Binary: "sha256:b"}
	}

	t.Run("a second signer for a role that already has one", func(t *testing.T) {
		p := fresh()
		if _, err := p.Authorize(SupervisorRequest{Run: run, Entry: entry()}, key); err != nil {
			t.Fatal(err)
		}
		second := entry()
		second.PublicKey = "the-workloads-own-key"
		if _, err := p.Authorize(SupervisorRequest{Run: run, Entry: second}, key); err == nil {
			t.Error("a second producer claimed a role that already had an attributable holder")
		}
	})

	t.Run("an action-level signer, which the roster already closed", func(t *testing.T) {
		p := fresh()
		e := entry()
		e.Level = LevelAction
		if _, err := p.Authorize(SupervisorRequest{Run: run, Entry: e}, key); err == nil {
			t.Error("a runtime request added a signer to the set the trusted opening step sealed")
		}
	})

	t.Run("a registration for another measurement", func(t *testing.T) {
		p := fresh()
		other := run
		other.RunID = "somebody-elses-run"
		if _, err := p.Authorize(SupervisorRequest{Run: other, Entry: entry()}, key); err == nil {
			t.Error("a second measurement borrowed this supervisor's authority")
		}
	})

	t.Run("a containment outside the tree it owns", func(t *testing.T) {
		p := fresh()
		if err := p.CheckCreate(SupervisorRequest{Run: run, Name: "tb-script", Parent: "/sys/fs/cgroup/elsewhere"}); err == nil {
			t.Error("a containment was nested under a parent this supervisor never created")
		}
		if err := p.CheckCreate(SupervisorRequest{Run: run, Name: "../escape"}); err == nil {
			t.Error("a containment name that is a path was accepted")
		}
	})

	t.Run("migrating a process already inside a containment", func(t *testing.T) {
		p := fresh()
		p.Note("/sys/fs/cgroup/testbucket/tb-a")
		p.Note("/sys/fs/cgroup/testbucket/tb-b")
		req := SupervisorRequest{Run: run, Containment: "/sys/fs/cgroup/testbucket/tb-b", PID: 99}
		if err := p.CheckAdmit(req, "/sys/fs/cgroup/testbucket/tb-a"); err == nil {
			t.Error("a process was moved between containments; that is the migration the boundary exists to prevent")
		}
		// A fresh process not yet in any of this run's containments is fine.
		if err := p.CheckAdmit(req, "/sys/fs/cgroup/other"); err != nil {
			t.Errorf("a fresh child could not be admitted: %v", err)
		}
	})
}

// TestASupervisorSharingTheWorkloadCredentialIsRefused: the boundary is the
// credential, not the process. A supervisor running as the uid the workload
// runs as enforces nothing, and starting one would only make the absence
// harder to see.
func TestASupervisorSharingTheWorkloadCredentialIsRefused(t *testing.T) {
	err := RunSupervisor(SupervisorOptions{
		Socket: filepath.Join(t.TempDir(), "s.sock"), Root: t.TempDir(),
		Run: testRun(), RunKey: mustSigningKey(), WorkloadUID: os.Getuid(),
	})
	if err == nil {
		t.Fatal("a supervisor sharing the workload's uid started anyway")
	}
	if !strings.Contains(err.Error(), "enforces nothing") {
		t.Errorf("error %q does not say why a shared credential is not a boundary", err)
	}
	// And one with no run key holds no authorizing capability at all.
	if err := RunSupervisor(SupervisorOptions{
		Socket: filepath.Join(t.TempDir(), "s.sock"), Root: t.TempDir(),
		Run: testRun(), WorkloadUID: os.Getuid() + 1,
	}); err == nil {
		t.Error("a supervisor with no run key started anyway")
	}
}

// TestTheSupervisorProtocolRoundTrips: the wire form is one request, one
// reply, one connection, so a partial exchange cannot be replayed as a
// complete one.
func TestTheSupervisorProtocolRoundTrips(t *testing.T) {
	req := SupervisorRequest{Kind: SupervisorAdmit, Run: testRun(), Containment: "/c", PID: 7}
	b, err := EncodeSupervisorRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeSupervisorRequest(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != req.Kind || got.PID != req.PID || got.Run.RunID != req.Run.RunID {
		t.Errorf("request round-tripped as %+v, want %+v", got, req)
	}
	refusal, err := EncodeSupervisorReply(SupervisorReply{Error: "no"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSupervisorReply(refusal); err == nil {
		t.Error("a refusal decoded as success")
	}
}

// TestTheProductionPathReachesTheSupervisor: the policy is only a boundary if
// production actually asks. Asserted structurally, because the wiring spans a
// composite action and three Go call sites and no single unit test spans it.
func TestTheProductionPathReachesTheSupervisor(t *testing.T) {
	for _, tc := range []struct{ file, want, why string }{
		{"contain.go", "supervisedContainment(name, parent, run)",
			"containment creation must ask the supervisor, or the wrapper creates one at its own credential"},
		{"roster.go", "supervisedRegisterKey(dir, e, run)",
			"key registration must ask the supervisor, or the measured step self-authorizes"},
		{"exec.go", "NewContainmentFor(containmentName(opt), opt.Parent, opt.Run)",
			"the script and invocation wrappers must use the supervised constructor"},
		{"action.go", "NewContainmentFor(containmentName(ExecOptions{Level: LevelAction, Run: run}), nil, run)",
			"the action envelope must use the supervised constructor"},
	} {
		b, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), tc.want) {
			t.Errorf("%s does not call %s: %s", tc.file, tc.want, tc.why)
		}
	}

	action, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "run-bucket", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yml := string(action)
	for _, want := range []string{
		"testbucket wall supervise",        // the privileged process is started
		"--workload-uid",                   // with the workload credential named
		"--key-fd 3",                       // and the key on a descriptor, not argv
		"useradd --system",                 // a distinct account exists
		"sudo -n -u \"$TB_WORKLOAD_USER\"", // and the measured work drops to it
		"TB_WALL_SUPERVISOR",               // the wrappers are told where to ask
		"sudo chown root:root",             // the delegated subtree is root's
	} {
		if !strings.Contains(yml, want) {
			t.Errorf("the composite action does not establish the boundary: missing %q", want)
		}
	}
	// The workload account must never be given sudo: a workload that could
	// sudo would simply take the capabilities back.
	if strings.Contains(yml, "usermod -aG sudo") || strings.Contains(yml, "gpasswd -a \"$TB_WORKLOAD_USER\" sudo") {
		t.Error("the workload account is granted sudo, which returns every capability the boundary removes")
	}
}
