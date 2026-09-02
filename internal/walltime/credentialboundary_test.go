package walltime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheMeasuredCommandRunsAsTheWorkloadAccount is the F1/F4 repair, and it
// is the whole boundary.
//
// The wrapper must be able to create containments, freeze them, admit into
// them and destroy them, so it must be able to write the delegated subtree.
// The measured work must NOT, because on cgroup-v2 `cgroup.procs` is the
// process-migration control and a workload that can write it can move itself
// between containments and rewrite the membership history the envelope
// records. Both cannot be one credential.
//
// The previous attempt put the capability behind a socket and then handed the
// workload the credential to open it. This drops the measured command instead
// and leaves the wrapper where it is.
func TestTheMeasuredCommandRunsAsTheWorkloadAccount(t *testing.T) {
	argv := []string{"npx", "vitest", "run", "a.spec.ts"}

	t.Setenv(WorkloadUserEnv, "")
	if got := workloadArgv(LevelInvocation, argv); len(got) != len(argv) || got[0] != "npx" {
		t.Errorf("with no workload account declared the command was rewritten to %v; the unscored path must be unchanged", got)
	}

	t.Setenv(WorkloadUserEnv, "tb-workload")
	// ABOVE the invocation level the wrapper chain keeps its credential: the
	// script body writes evidence into the wrapper-owned records directory and
	// starts nested wrappers that must create and admit containments, and a
	// drop there would either hand the workload those capabilities or fail.
	for _, level := range []Level{LevelAction, LevelScript} {
		if got := workloadArgv(level, argv); len(got) != len(argv) {
			t.Errorf("the %s measured command was dropped to the workload account (%v); the nested wrappers below it could then neither create nor admit a containment", level, got)
		}
	}
	got := workloadArgv(LevelInvocation, argv)
	want := []string{"sudo", "-n", "-u", "tb-workload", "--", "npx", "vitest", "run", "a.spec.ts"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the measured command runs as %v, want %v", got, want)
	}
	// `-n` and never a prompt: a measurement that stops to ask for a password
	// is a measurement that hangs.
	if got[1] != "-n" {
		t.Errorf("the drop may prompt for a password: %v", got)
	}
	// `--` terminates the option list, so a test file named like a flag is an
	// argument rather than an option to sudo.
	if got[4] != "--" {
		t.Errorf("the argv is not terminated before the measured command: %v", got)
	}
}

// TestTheObservedCredentialSeparationIsRequired: the boundary is checked from
// what the kernel reported, not from what the caller declared.
//
// A declared workload account could otherwise mint a scored row on a runner
// where nothing dropped — which is exactly the defect the previous round's
// membership model had. The containment's owner comes off the filesystem and
// the measured process's uid comes out of /proc, and they must differ.
func TestTheObservedCredentialSeparationIsRequired(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit mutation
		want string
	}{
		{"the measured process ran as the containment's owner", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPhysical && strings.HasPrefix(b, "process_tree") {
				r.Proc.UID = r.Containment.OwnerUID
			}
		}, "which is the credential owning this containment"},

		{"the record does not say what it ran as", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPhysical && strings.HasPrefix(b, "process_tree") {
				r.Proc.UID = -1
			}
		}, "does not state the credential"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := verifySynth(t, tc.edit, nil)
			if len(findingsMentioning(v, "WT-033", tc.want)) == 0 {
				t.Errorf("no WT-033 finding mentions %q: %+v", tc.want, v.Findings)
			}
			if v.Eligible {
				t.Errorf("a run where %s scored", tc.name)
			}
		})
	}
}

// TestATStartIsTheFirstActionOwnedOperation is the F4 repair.
//
// A privileged setup step was added to this action ahead of `wall begin`: user
// and group creation, a recursive chown, cgroup creation and a long-lived root
// child. All of it is action-owned work performed before AT_start, and the
// step that follows claimed to be the first action-owned operation. The two
// claims cannot both be true, so the setup belongs to the CALLER's job, where
// the cgroup root already came from.
func TestATStartIsTheFirstActionOwnedOperation(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "actions", "run-bucket", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	yml := string(b)
	// WHAT THE ACTION EXECUTES, not what it documents. The input description
	// shows a caller the setup to perform in its own job, and that text is the
	// remedy rather than the defect — so the assertion reads the `run:` bodies.
	for _, forbidden := range []string{"useradd", "groupadd", "gpasswd", "wall supervise", "chown -R"} {
		for _, line := range strings.Split(yml, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "sudo useradd") && strings.Contains(yml, "Establish it in a PRIOR step") {
				continue
			}
			if !strings.Contains(line, forbidden) {
				continue
			}
			// Description blocks are indented under `description: >-`; a run
			// step's commands sit under `run: |`. Anything inside the eight
			// leading spaces of a run body is executed.
			if strings.HasPrefix(line, "        ") && !strings.HasPrefix(line, "          ") {
				t.Errorf("the action EXECUTES privileged setup (%q) before AT_start; that is action-owned work outside the interval that claims to contain it: %s", forbidden, trimmed)
			}
		}
	}
	// And nothing measured or privileged may precede the envelope: only the
	// eligible guard and the wrapper install, both preconditions.
	before := strings.Index(yml, "- name: Open the wall-time action envelope")
	if before < 0 {
		t.Fatal("the action has no envelope step")
	}
	for _, name := range []string{"Refuse an unmeasurable scored row", "Install testbucket"} {
		at := strings.Index(yml, "- name: "+name)
		if at < 0 || at > before {
			t.Errorf("step %q does not precede the envelope", name)
		}
	}
}

// TestEachActionRecordNamesTheWrapperThatTookIt is the F3 repair.
//
// The closing step used to copy the OPENING step's identity into its own
// records, so those records claimed a process that had already exited as the
// thing they had just observed. `wall begin` returns after writing the
// handoff; the setup, bucket and closing steps are sibling step processes that
// each join the same containment. No process spans an action, so no record may
// say one did — the containment spans it, and each record names the wrapper
// that actually took its reading.
func TestEachActionRecordNamesTheWrapperThatTookIt(t *testing.T) {
	// The opening identity is still retained as PROVENANCE — who opened the
	// envelope is a fact worth keeping — but it is not copied into a later
	// step's observations.
	st := ActionState{Root: ProcIdentity{PID: 4242, StartID: "778899", UID: 1000}}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var got ActionState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Root.PID != 4242 || got.Root.StartID != "778899" {
		t.Errorf("the opening identity did not survive the handoff: %+v", got.Root)
	}
	source, err := os.ReadFile("action.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		`retainActionProcessTreeFor(w, st.Run, clock, cont, "observed", st.Root)`,
		`retainActionProcessTreeFor(w, st.Run, clock, cont, "end", st.Root)`,
	} {
		if strings.Contains(string(source), forbidden) {
			t.Errorf("the closing step copies the opening step's identity into its own record: %s", forbidden)
		}
	}
}

// TestTheActionLevelIsNotJudgedAsAMeasuredChild is the other half of F3.
//
// A script or invocation envelope wraps a command, so its measured process
// must be neither the containment's own root nor running as the credential
// that owns it. The action envelope wraps a job step sequence: `wall begin`
// creates the containment and joins it, which made the producer emit exactly
// what the verifier called terminal. The production action path could never
// verify, and the candidate's own tests demanded both halves of the
// contradiction.
func TestTheActionLevelIsNotJudgedAsAMeasuredChild(t *testing.T) {
	self := ProcIdentity{PID: 4242, PGID: 4242, SessionID: 4242, StartID: "778899", ParentPID: 7, UID: 1000}
	e := Envelope{Level: LevelAction, Containment: ContainmentIdentity{
		Primitive: PrimitiveCgroup2, ID: "/sys/fs/cgroup/tb/action", Inode: "42", BootID: "boot",
		// The creating wrapper IS the containment root and owns it — the exact
		// shape BeginAction produces.
		RootPID: self.PID, RootStart: self.StartID, OwnerUID: self.UID,
		MembershipControl: MembershipSupervisorOwned,
	}}
	v := &Verdict{}
	checkProcessTree(v, "action[0]", e, Record{
		Kind: "process_tree", Boundary: "start", Producer: ProducerPhysical,
		Source: SourceProcessLifecycle, Level: LevelAction,
		Containment: e.Containment, Proc: self,
	})
	for _, f := range v.Findings {
		if f.Severity == SeverityTerminal {
			t.Errorf("the action wrapper observing its own containment was made terminal: %s", f.Detail)
		}
	}
	// The same record at INVOCATION level is still refused, so the rule was
	// scoped rather than removed.
	inv := e
	inv.Level = LevelInvocation
	iv := &Verdict{}
	checkProcessTree(iv, "invocation[0]", inv, Record{
		Kind: "process_tree", Boundary: "start", Producer: ProducerPhysical,
		Source: SourceProcessLifecycle, Level: LevelInvocation,
		Containment: inv.Containment, Proc: self,
	})
	if len(findingsMentioning(iv, "WT-033", "containment's own root process")) == 0 {
		t.Errorf("a measured invocation child identical to its containment root was accepted: %+v", iv.Findings)
	}
}

// TestTheProductionLaunchPathAppliesTheCredentialDrop is the F1 regression.
//
// The helper existed and NOTHING CALLED IT: `runChild` built its command
// directly, so the measured work ran under the wrapper's identity and could
// reach every wrapper-writable cgroup control, handoff file and same-uid
// process resource. The advertised boundary did not exist in production, and
// a test that called the helper directly proved only the helper.
//
// This reads the production launch site, because "the helper is correct" and
// "the production path uses it" are different claims and only the second one
// is a boundary.
func TestTheProductionLaunchPathAppliesTheCredentialDrop(t *testing.T) {
	body := productionFunc(t, "exec.go", "func runChild(")
	drop := strings.Index(body, "workloadArgv(opt.Level, opt.Argv)")
	spawn := strings.Index(body, "exec.Command(")
	if drop < 0 {
		t.Fatal("runChild builds its command without the credential drop; the measured work runs as the wrapper")
	}
	if spawn < 0 || drop > spawn {
		t.Errorf("the drop at %d does not precede the command construction at %d", drop, spawn)
	}
	if strings.Contains(body, "exec.Command(opt.Argv[0], opt.Argv[1:]...)") {
		t.Error("runChild still constructs the measured command from the raw argv, bypassing the drop")
	}
}

// TestTheNestedScoredPathStaysRunnable is the F2 regression.
//
// Dropping at every level is not a stricter version of dropping at one: the
// script body writes evidence into the wrapper-owned records directory and
// starts nested wrappers that create, admit into, freeze and destroy
// containments in the delegated subtree. A workload account able to do that
// has the boundary back; one unable to do it cannot run the nested topology at
// all. The composition has to be level-aware, and this pins which level.
func TestTheNestedScoredPathStaysRunnable(t *testing.T) {
	body := productionFunc(t, "exec.go", "func workloadArgv(")
	if !strings.Contains(body, "if level != LevelInvocation") {
		t.Error("the drop is not level-aware; at script level it would hand the workload the evidence directory and the nested containment operations, or break them")
	}
	t.Setenv(WorkloadUserEnv, "tb-workload")
	argv := []string{"bash", "-c", "generated bucket body"}
	if got := workloadArgv(LevelScript, argv); len(got) != len(argv) {
		t.Errorf("the script body was dropped to the workload account: %v", got)
	}
	if got := workloadArgv(LevelInvocation, argv); len(got) == len(argv) {
		t.Error("the invocation child was NOT dropped; that is the one process tree running somebody else's code")
	}
}

// TestTheMembershipDecisionIsRederivedFromRetainedFacts is the F4 regression.
//
// The producer wrote `MembershipControl` as a string and the verifier believed
// it — a non-reproducible summary of the one property eligibility turns on,
// about a cgroup that no longer exists by the time anyone reads the records.
// The inputs are retained now and the rule runs again, over the measured
// process's OWN group vector as the kernel reported it rather than over what
// /etc/group happened to say on the producer's host.
func TestTheMembershipDecisionIsRederivedFromRetainedFacts(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit mutation
		want string
	}{
		// A producer claiming supervisor-owned over facts that do not support
		// it: the containment is group-writable and the measured process is in
		// that group.
		{"a summary its own facts contradict", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPhysical && strings.HasPrefix(b, "process_tree") {
				r.Proc.Groups = append(r.Proc.Groups, r.Containment.OwnerGID)
			}
		}, "rerunning the rule over its retained owner"},

		// A world-writable containment: nothing excludes anyone.
		{"a world-writable containment", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPhysical && strings.HasPrefix(b, "process_tree") {
				r.Containment.Mode = 0o777
			}
		}, "could write this containment's cgroup.procs"},

		// The measured process owning the containment outright.
		{"the measured process owns it", func(_ Level, _ int, p Producer, b string, r *Record) {
			if p == ProducerPhysical && strings.HasPrefix(b, "process_tree") {
				r.Containment.OwnerUID = r.Proc.UID
			}
		}, "could write this containment's cgroup.procs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := verifySynth(t, tc.edit, nil)
			if len(findingsMentioning(v, "WT-033", tc.want)) == 0 {
				t.Errorf("no WT-033 finding mentions %q: %+v", tc.want, v.Findings)
			}
			if v.Eligible {
				t.Errorf("a run with %s scored", tc.name)
			}
		})
	}
}

// TestTheGroupVectorComesFromTheProcessNotEtcGroup: account resolution may go
// through NSS, LDAP or SSSD, so parsing /etc/group establishes what those
// files say and not what the process received.
func TestTheGroupVectorComesFromTheProcessNotEtcGroup(t *testing.T) {
	body := productionFunc(t, "idsampler.go", "func (s *identitySampler) sample()")
	if !strings.Contains(body, "processGroupsOf(s.pid)") {
		t.Error("the sampler does not read the process's own group vector")
	}
	run := productionFunc(t, "exec.go", "func runChild(")
	if !strings.Contains(run, "processGroupsOf(cmd.Process.Pid)") {
		t.Error("the measured child's group vector is not read from the process")
	}
	rederive := productionFunc(t, "proctree.go", "func checkProcessTree(")
	if !strings.Contains(rederive, "WorkloadGIDs: append(append([]int{}, p.Groups...), p.GID)") {
		t.Error("the verifier does not rederive over the process's own group vector")
	}
}
