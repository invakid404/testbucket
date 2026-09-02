package walltime

import "testing"

// TestTheContainmentHierarchyIsProved is the F3 regression.
//
// Every other containment check asks about ONE envelope: that its physical,
// peer and trace ledgers name the same containment, that the primitive is
// scorable, that the root process agrees. None asked how the envelopes relate
// to each other — and nesting is the whole point of the design, because a
// descendant that cannot escape upward is what makes an invocation's time
// part of its script's and a script's part of the action's.
//
// So an invocation containment somewhere else entirely — a sibling subtree,
// another bucket's tree, a path the workload chose — carried valid signatures,
// consistent timestamps and matching peer/trace agreement, and scored. The
// measured work was accounted to a lifecycle that never contained it.
func TestTheContainmentHierarchyIsProved(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit mutation
		want string
	}{
		// THE DEFECT.
		{"invocations in a foreign subtree", func(level Level, seq int, _ Producer, _ string, r *Record) {
			if level == LevelInvocation {
				r.Containment.ID = "/sys/fs/cgroup/testbucket/foreign/tb-invocation-000"
			}
		}, "is not inside its script containment"},

		{"a script outside the action", func(level Level, _ int, _ Producer, _ string, r *Record) {
			if level == LevelScript {
				r.Containment.ID = "/sys/fs/cgroup/testbucket/elsewhere/tb-script-0"
			}
		}, "is not inside its action containment"},

		// A sibling that merely SHARES A PREFIX is not inside: the check is a
		// path-component boundary, not a string prefix.
		{"an invocation beside its script rather than inside it", func(level Level, _ int, _ Producer, _ string, r *Record) {
			if level == LevelInvocation {
				r.Containment.ID = "/sys/fs/cgroup/testbucket/tb-action-0/tb-script-0-evil/tb-invocation-000"
			}
		}, "is not inside its script containment"},

		{"a level reusing its parent's containment", func(level Level, _ int, _ Producer, _ string, r *Record) {
			if level == LevelInvocation {
				r.Containment.ID = synthContainmentPath(LevelScript, 0)
			}
		}, "bounds no lifecycle of its own"},

		{"a nested path from another boot", func(level Level, _ int, _ Producer, _ string, r *Record) {
			if level == LevelInvocation {
				r.Containment.BootID = "a-different-boot"
			}
		}, "two boots are two machines"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := verifySynth(t, tc.edit, nil)
			if len(findingsMentioning(v, "WT-030", tc.want)) == 0 {
				t.Errorf("no WT-030 finding mentions %q; eligible=%v findings=%+v", tc.want, v.Eligible, v.Findings)
			}
			if v.Eligible {
				t.Errorf("a run with %s scored", tc.name)
			}
		})
	}
}

// TestTheNestedFixtureIsWhatProductionBuilds is the positive control: the
// fixture's paths are the ones newCgroupUnder produces, so the table above
// fails on its mutation rather than on the harness.
func TestTheNestedFixtureIsWhatProductionBuilds(t *testing.T) {
	v := verifySynth(t, nil, nil)
	for _, f := range v.Findings {
		if f.Code == "WT-030" {
			t.Errorf("an untouched run raised %s: %s", f.Code, f.Detail)
		}
	}
	action := synthContainmentPath(LevelAction, 0)
	script := synthContainmentPath(LevelScript, 0)
	inv := synthContainmentPath(LevelInvocation, 0)
	if !hasParent(script, action) || !hasParent(inv, script) {
		t.Errorf("the fixture is not nested: action=%s script=%s invocation=%s", action, script, inv)
	}
}

func hasParent(child, parent string) bool {
	return len(child) > len(parent)+1 && child[:len(parent)+1] == parent+"/"
}
