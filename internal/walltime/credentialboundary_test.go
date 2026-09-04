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
	// The command is now built by the shared held-child launcher — the child
	// is this binary at a barrier, which execs the measured argv in place —
	// so the construction to order against is the launcher call, not a bare
	// exec.Command.
	spawn := strings.Index(body, "HeldChildLauncher(argv)")
	if drop < 0 {
		t.Fatal("runChild builds its command without the credential drop; the measured work runs as the wrapper")
	}
	if spawn < 0 || drop > spawn {
		t.Errorf("the drop at %d does not precede the command construction at %d", drop, spawn)
	}
	if strings.Contains(body, "opt.Argv[0], opt.Argv[1:]...") || strings.Contains(body, "HeldChildLauncher(opt.Argv)") {
		t.Error("runChild still constructs the measured command from the raw argv, bypassing the drop")
	}
}

// TestBothMeasuredLevelsDropToTheirOwnAccount is the F2 regression.
//
// Dropping only the invocation could not produce an eligible SCRIPT row. The
// script's measured child kept the wrapper's credential, and the wrapper's own
// verifier makes a measured process running as the credential that owns its
// containment unscorable — so the producer and the verifier were asking for
// opposite things at that level and no passing script row could exist.
//
// The resolution is a second account, not a relaxed rule. There are two
// measured parties: the harness-generated bucket script, which starts the
// nested wrappers and is therefore handed the delegated script subtree, and
// the test code, which is handed nothing. The ACTION level still does not
// drop, because it has no measured child at all — its containment is joined by
// the step processes themselves.
func TestBothMeasuredLevelsDropToTheirOwnAccount(t *testing.T) {
	t.Setenv(WorkloadUserEnv, "tb-workload")
	t.Setenv(ScriptUserEnv, "tb-script")
	argv := []string{"bash", "-c", "generated bucket body"}
	script := workloadArgv(LevelScript, argv)
	if len(script) == len(argv) {
		t.Error("the bucket script was NOT dropped; it would run as the credential owning its own containment, which this wrapper's verifier refuses to score")
	}
	if len(script) > 3 && script[3] != "tb-script" {
		t.Errorf("the script dropped to %q, not the script account", script[3])
	}
	invocation := workloadArgv(LevelInvocation, argv)
	if len(invocation) == len(argv) {
		t.Error("the invocation child was NOT dropped; that is the one process tree running somebody else's code")
	}
	if len(invocation) > 3 && invocation[3] != "tb-workload" {
		t.Errorf("the invocation dropped to %q, not the workload account", invocation[3])
	}
	// TWO ACCOUNTS, NOT ONE. A single account for both would give the test
	// code the script's delegated subtree back.
	if len(script) > 3 && len(invocation) > 3 && script[3] == invocation[3] {
		t.Error("the script and the invocation dropped to the same account; the delegated subtree the script needs is exactly what the test code must not have")
	}
	if got := workloadArgv(LevelAction, argv); len(got) != len(argv) {
		t.Errorf("the action level dropped: %v; it wraps a sequence of job steps and has no measured child to drop", got)
	}
	// And an undeclared account still runs, unwrapped and ineligible, rather
	// than failing closed on a host that has not been set up.
	t.Setenv(ScriptUserEnv, "")
	if got := workloadArgv(LevelScript, argv); len(got) != len(argv) {
		t.Errorf("an undeclared script account still wrapped the command: %v", got)
	}
}

// TestTheScriptDoesItsOwnNestingThroughTheController is the other half of F2.
//
// The subtree used to be DELEGATED to the measured script so it could create
// the invocation containments itself. cgroup-v2 requires write access to the
// common ancestor's `cgroup.procs` to place a process into a sub-cgroup, so a
// script able to do that is a script able to write the migration control its
// own envelope rests on — and the verifier correctly made every
// complete-script row ineligible for exactly that reason.
//
// The nesting is done FOR it instead, by the wrapper that already holds the
// credential, over a channel the script can only ask on.
func TestTheScriptDoesItsOwnNestingThroughTheController(t *testing.T) {
	// The delegation is gone from the tree, not merely unused.
	for _, file := range []string{"contain_linux.go", "contain_other.go", "exec.go"} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, gone := range []string{"func delegateSubtree(", "func delegateScriptSubtree(", "delegateScriptSubtree(cont)"} {
			if strings.Contains(string(b), gone) {
				t.Errorf("%s still carries %s; delegating the script's own containment to the measured script is the arrangement the membership rule exists to refuse", file, gone)
			}
		}
	}
	body := productionFunc(t, "exec.go", "func Exec(")
	start := strings.Index(body, "StartInvocationController(opt, cont.Identity())")
	child := strings.Index(body, "runChild(")
	if start < 0 {
		t.Fatal("Exec starts no invocation controller, so a dropped script cannot get an invocation measured at all")
	}
	if child >= 0 && start > child {
		t.Errorf("the controller opens at %d, after the script starts at %d", start, child)
	}
	if !strings.Contains(body, "if opt.Level == LevelScript {") {
		t.Error("the controller is not scoped to the script level")
	}
	// And it closes inside the envelope, with the handoff.
	if !strings.Contains(body, "controller.Close()") {
		t.Error("the controller outlives the envelope it serves")
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
	// The verifier reruns the rule over the RETAINED workload credential, and
	// at the invocation level over the measured process's own vector as well —
	// the level where the measured process is the workload.
	subject := productionFunc(t, "proctree.go", "func membershipSubject(")
	if !strings.Contains(subject, "append(append([]int{}, p.Groups...), p.GID)") {
		t.Error("the verifier does not rederive over the process's own group vector")
	}
	if !strings.Contains(subject, "level == LevelInvocation") {
		t.Error("the subject of the rederivation is not level-aware")
	}
	if !strings.Contains(subject, "c.WorkloadUID > 0") {
		t.Error("the verifier does not use the retained workload credential, so the action level has no workload to rerun the rule about")
	}
}

// TestTheRetainedWorkloadCredentialIsResolvedNotDeclared: the producer used to
// answer the membership question by reading /etc/passwd and /etc/group at
// decision time and writing down its conclusion. Nobody could rerun that — the
// accounts database is not part of the evidence and the cgroup is gone — so
// the facts are resolved once and retained on the identity the records carry.
func TestTheRetainedWorkloadCredentialIsResolvedNotDeclared(t *testing.T) {
	retain := productionFunc(t, "contain_linux.go", "func retainMembershipFactsFor(")
	for _, want := range []string{
		"resolveWorkloadCredential(account)",
		"ident.WorkloadUID = w.UID",
		"ident.WorkloadGIDs = append",
	} {
		if !strings.Contains(retain, want) {
			t.Errorf("the containment identity does not retain %q, so the rule cannot be rerun from the record", want)
		}
	}
	// And the decision function takes those facts as an ARGUMENT rather than
	// reading the accounts files itself, which is what makes the producer's
	// answer and the verifier's rerun the same computation.
	decide := productionFunc(t, "membership_linux.go", "func membershipControl(")
	if strings.Contains(decide, "/etc/group") || strings.Contains(decide, "/etc/passwd") {
		t.Error("membershipControl still reads the accounts database at decision time")
	}
	if !strings.Contains(decide, "w WorkloadCredential") {
		t.Error("membershipControl does not take the resolved workload credential as retained facts")
	}
	// AND THE ACCOUNT IS THE ONE THAT LEVEL MEASURES. A script containment was
	// labelled using the INVOCATION account's groups — an answer about a
	// process that was never in it, given immediately after delegating that
	// containment to the script account's group.
	level := productionFunc(t, "contain.go", "func measuredAccountFor(")
	if !strings.Contains(level, "level == LevelScript") || !strings.Contains(level, "ScriptUserEnv") {
		t.Error("the retained membership account is not the account the level measures")
	}
}

// TestTheScriptLevelIsJudgedOnTheScriptsOwnCredential is the F4 regression.
//
// The script cgroup is deliberately made group-writable by the script
// account's group — that delegation is what lets the nested wrappers create
// the invocation containments at all — and the membership rule was then rerun
// against the INVOCATION account's groups, which is a different party that was
// never in it. A containment the measured script can write was labelled
// supervisor-owned using an unrelated account's credential.
func TestTheScriptLevelIsJudgedOnTheScriptsOwnCredential(t *testing.T) {
	base := ContainmentIdentity{
		Primitive: PrimitiveCgroup2, ID: "/sys/fs/cgroup/testbucket/action/script",
		Inode: "42", BootID: "boot", RootPID: 100, RootStart: "1000",
		OwnerUID: 1000, OwnerGID: 2001, Mode: 0o770,
		WorkloadUID: 2002, WorkloadGIDs: []int{2002},
		MembershipControl: MembershipSupervisorOwned,
	}
	// The measured script is in the group that may write this containment.
	script := ProcIdentity{UID: 2001, GID: 2001, Groups: []int{2001}}
	v := &Verdict{}
	rederiveMembership(v, "script[0]", Envelope{Level: LevelScript, Containment: base},
		Record{Level: LevelScript, Containment: base, Proc: script})
	if len(findingsMentioning(v, "WT-033", "could write this containment")) == 0 {
		t.Errorf("a script containment its own measured process can write was accepted: %+v", v.Findings)
	}

	// A script that is NOT in the owning group is still scorable, so the rule
	// was widened to the right party rather than made unsatisfiable.
	clean := base
	clean.OwnerGID = 3003
	outsider := ProcIdentity{UID: 2001, GID: 2001, Groups: []int{2001}}
	ok := &Verdict{}
	rederiveMembership(ok, "script[0]", Envelope{Level: LevelScript, Containment: clean},
		Record{Level: LevelScript, Containment: clean, Proc: outsider})
	if len(ok.Findings) != 0 {
		t.Errorf("a script containment the measured script cannot write was refused: %+v", ok.Findings)
	}

	// And the subject names both parties, so a finding says whose credential
	// reached the file.
	_, _, described := membershipSubject(base, script, LevelScript)
	for _, want := range []string{"retained workload credential", "measured process's own credential"} {
		if !strings.Contains(described, want) {
			t.Errorf("the rederivation subject %q does not name the %s", described, want)
		}
	}
}

// TestThePublicInputsDoNotContradictTheMembershipBoundary is the F2/F3
// regression.
//
// The public action inputs are a consumer contract: a caller provisions
// accounts and permissions from what they say, and nothing else. Two of them
// described a system this wrapper deliberately does not build.
//
// `script-user` said the script containment is delegated to the measured
// account's group "so the script can arrange containments beneath it" — while
// the same input, four lines earlier, correctly said the containment stays
// wrapper-owned. Delegating it is the one arrangement the membership rule
// exists to refuse: it hands the measured process write access to the
// `cgroup.procs` deciding what its own envelope contains, which is exactly
// what made every complete-script row ineligible before the controller
// existed. A caller who provisioned from the wrong half would have built that
// hole on purpose, and the runtime being safer would not have told them.
//
// `workload-uid` said the wrapper's uid "always counts as the workload's,
// because nothing here changes credentials" — true before `workload-user`
// existed, false now that the wrapper drops to a resolved account. A reviewer
// reading it would conclude a two-account deployment cannot establish the
// boundary, which is the opposite of what `membershipModelFor` decides.
//
// The descriptions are checked, not just the code, because the code was right
// in both cases and the contract was still wrong.
func TestThePublicInputsDoNotContradictTheMembershipBoundary(t *testing.T) {
	// YAML folds these descriptions across lines; collapse to one space so a
	// phrase is matched however it happens to be wrapped.
	fold := func(t *testing.T, path string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(strings.Fields(string(b)), " ")
	}

	action := fold(t, filepath.Join("..", "..", ".github", "actions", "run-bucket", "action.yml"))
	reusable := fold(t, filepath.Join("..", "..", ".github", "workflows", "bucketed-reusable.yml"))

	for _, tc := range []struct {
		name    string
		doc     string
		claim   string
		why     string
		require string
	}{
		{
			name:    "run-bucket does not promise the script containment is delegated",
			doc:     action,
			claim:   "The script containment is delegated to this account's group",
			why:     "a caller provisioning from this would give the measured script write access to its own cgroup.procs, which is the arrangement the membership rule refuses",
			require: "The script containment is NOT delegated",
		},
		{
			name:    "run-bucket does not promise a declaration is powerless because credentials never change",
			doc:     action,
			claim:   "because nothing here changes credentials between the wrapper and the measured child",
			why:     "workload-user resolves an account and drops to it, so credentials do change and the wrapper's uid stops counting as the workload's",
			require: "What does move the boundary is workload-user",
		},
		{
			name:    "the reusable workflow does not promise the wrapper uid always counts",
			doc:     reusable,
			claim:   "is not trusted to: the wrapper's own uid always counts as the workload's",
			why:     "a resolved workload account replaces the wrapper's uid, which is the only way a scored arm is reachable",
			require: "only when such an account is actually resolved and dropped to does the wrapper's uid stop counting",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.doc, tc.claim) {
				t.Errorf("the public contract still says %q; %s", tc.claim, tc.why)
			}
			if !strings.Contains(tc.doc, tc.require) {
				t.Errorf("the public contract no longer states %q, so the corrected model is not documented anywhere a caller reads", tc.require)
			}
		})
	}

	// AND THE SAFE MODEL IS STATED POSITIVELY, not merely left unsaid.
	for _, must := range []string{
		"the controller channel",
		"SO_PEERCRED",
	} {
		if !strings.Contains(action, must) {
			t.Errorf("run-bucket's script-user no longer explains %q, so how the nesting actually happens is undocumented", must)
		}
	}
}
