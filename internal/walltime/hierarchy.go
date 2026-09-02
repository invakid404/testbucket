package walltime

import (
	"fmt"
	"strings"
)

// verifyContainmentHierarchy proves the levels were physically NESTED.
//
// Every other containment check in this verifier asks about one envelope: that
// its physical, peer and trace ledgers name the same containment, that the
// containment is a scorable primitive, that its root process identity agrees.
// None of them asks how the envelopes relate to each other, and the whole
// point of nesting is that a descendant cannot escape upward: production
// builds each level's containment as a directory INSIDE its parent's, so an
// invocation's processes are inside its script's containment and the script's
// are inside the action's.
//
// Without this, an invocation containment somewhere else entirely — a sibling
// subtree, another bucket's tree, an attacker-chosen path — carried valid
// signatures, consistent timestamps and matching peer/trace agreement, and
// scored. The measured work would have been accounted to a lifecycle that
// never contained it.
//
// The relation is proved from the containment PATH, which is what cgroup-v2
// nesting is: `/…/tb-action-0/tb-script-0/tb-invocation-000` is inside
// `/…/tb-action-0/tb-script-0`. A child must be a strict path descendant of
// its parent, and must not BE its parent — a level that reuses its parent's
// containment has no lifecycle of its own to bound.
func verifyContainmentHierarchy(v *Verdict, envs []Envelope) {
	action, script := scorableRoots(envs)
	for _, e := range envs {
		if e.Containment.Primitive != PrimitiveCgroup2 {
			continue
		}
		label := fmt.Sprintf("%s[%d]", e.Level, e.Seq)
		switch e.Level {
		case LevelScript:
			requireInside(v, label, "script", e.Containment, action, "action")
		case LevelInvocation:
			// An invocation nests under its SCRIPT when one was measured, and
			// under the action otherwise: an invocation wrapper started
			// outside a measured script still runs inside the action.
			parent, role := script, "script"
			if parent == nil {
				parent, role = action, "action"
			}
			requireInside(v, label, "invocation", e.Containment, parent, role)
		}
	}
}

// scorableRoots finds the action and script containments the other levels must
// nest under. A run with several script envelopes has no single enclosing
// script, so the invocation check falls back to the action rather than
// guessing which script an invocation belonged to.
func scorableRoots(envs []Envelope) (action, script *ContainmentIdentity) {
	scripts := 0
	for i := range envs {
		if envs[i].Containment.Primitive != PrimitiveCgroup2 {
			continue
		}
		switch envs[i].Level {
		case LevelAction:
			if action == nil {
				action = &envs[i].Containment
			}
		case LevelScript:
			scripts++
			script = &envs[i].Containment
		}
	}
	if scripts != 1 {
		script = nil
	}
	return action, script
}

func requireInside(v *Verdict, label, level string, child ContainmentIdentity, parent *ContainmentIdentity, parentLevel string) {
	if parent == nil {
		// Nothing to nest under. A measured script or invocation with no
		// enclosing action containment is not a hierarchy this verifier can
		// prove, and unprovable nesting is unscorable rather than assumed.
		v.add("WT-030", SeverityIneligible, fmt.Sprintf(
			"%s: no scorable %s containment was measured, so there is nothing proving this %s ran inside one",
			label, parentLevel, level))
		return
	}
	if child.ID == parent.ID {
		v.add("WT-030", SeverityIneligible, fmt.Sprintf(
			"%s: the %s containment IS its %s containment (%s); a level that reuses its parent's containment bounds no lifecycle of its own",
			label, level, parentLevel, child.ID))
		return
	}
	if !strings.HasPrefix(child.ID, strings.TrimSuffix(parent.ID, "/")+"/") {
		v.add("WT-030", SeverityIneligible, fmt.Sprintf(
			"%s: the %s containment %s is not inside its %s containment %s; a lifecycle measured outside the containment that is supposed to enclose it accounts for work that could have escaped upward",
			label, level, child.ID, parentLevel, parent.ID))
		return
	}
	// The same boot, and the same root process identity: a descendant path
	// under a containment created for a different process, or on another boot,
	// is a path that merely looks nested.
	if child.BootID != "" && parent.BootID != "" && child.BootID != parent.BootID {
		v.add("WT-030", SeverityIneligible, fmt.Sprintf(
			"%s: the %s containment carries boot %s while its %s containment carries %s; two boots are two machines' worth of paths",
			label, level, child.BootID, parentLevel, parent.BootID))
	}
}
