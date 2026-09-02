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
	if got := workloadArgv(argv); len(got) != len(argv) || got[0] != "npx" {
		t.Errorf("with no workload account declared the command was rewritten to %v; the unscored path must be unchanged", got)
	}

	t.Setenv(WorkloadUserEnv, "tb-workload")
	got := workloadArgv(argv)
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

// TestTheActionRootIsCarriedAcrossTheStepBoundary is part of F5.
//
// `wall begin` and `wall end` are different steps and therefore different
// processes. Reading the current pid in both made the closing records describe
// the closing wrapper, which the verifier then read as the measured root
// changing identity mid-envelope — a terminal transition the design itself
// produced.
func TestTheActionRootIsCarriedAcrossTheStepBoundary(t *testing.T) {
	st := ActionState{Root: ProcIdentity{PID: 4242, PGID: 4242, SessionID: 4242, StartID: "778899", ParentPID: 7, UID: 1000}}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var got ActionState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Root.PID != 4242 || got.Root.StartID != "778899" || got.Root.UID != 1000 {
		t.Errorf("the action root did not survive the handoff: %+v", got.Root)
	}
	body, err := os.ReadFile("action.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	for _, want := range []string{
		`retainActionProcessTreeFor(w, st.Run, clock, cont, "observed", st.Root)`,
		`retainActionProcessTreeFor(w, st.Run, clock, cont, "end", st.Root)`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("EndAction does not use the recorded action root: missing %s", want)
		}
	}
}
