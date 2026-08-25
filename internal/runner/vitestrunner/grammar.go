package vitestrunner

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
)

// ValidateUnit is the command-grammar half of the never-drop gate for the Vitest
// runner: it checks that a final unit renders to a `vitest run` command that
// actually runs what the unit claims. Because this adapter buckets at file
// granularity, only whole-file and whole-project(atom) units are renderable;
// count-shard and run-slice units are refused as a backstop (the core will not
// produce them without a sweep count or per-test weights, which this adapter
// does not supply, but the gate must not trust that).
func (r *Runner) ValidateUnit(u runner.Unit, live map[string]runner.LivePackage, baseCount int) []string {
	var defects []string
	label := u.ID
	if strings.TrimSpace(label) == "" {
		label = "<unnamed " + string(u.Kind) + " unit>"
	}
	bad := func(format string, a ...any) {
		defects = append(defects, label+" "+fmt.Sprintf(format, a...))
	}

	switch u.Kind {
	case runner.KindPackage, runner.KindModuleAtom:
	case runner.KindCountShard:
		bad("is a count-shard; the Vitest adapter has no per-invocation sweep to divide, so it schedules at file granularity — refusing rather than emitting a command that reruns the file")
		return defects
	case runner.KindRunSlice:
		bad("is a run-slice; the Vitest adapter buckets whole files (name-slicing is a documented follow-up) — refusing rather than emitting a -t that would skip the rest of the file")
		return defects
	default:
		bad("has unknown kind %q", u.Kind)
		return defects
	}

	if len(u.Packages) == 0 {
		bad("is a %s covering no test file at all", u.Kind)
		return defects
	}
	if strings.TrimSpace(u.ID) == "" {
		bad("has no ID; the renderer keys invocations by unit ID")
	}

	for _, p := range u.Packages {
		lp, ok := live[p.ID]
		switch {
		case !ok:
			bad("names %s, which is not in the live test set", p.ID)
			continue
		case !lp.HasTests:
			bad("names %s, which has no tests", p.ID)
			continue
		case lp != p:
			bad("describes %s differently from the live tree (unit atom=%q, tree atom=%q)", p.ID, p.Atom, lp.Atom)
			continue
		}
	}

	// Sweep integrity. Vitest has no per-invocation repeat sweep in this adapter,
	// so the only renderable sweep is exactly one: Render emits a single
	// `vitest run`. A base above one — or a unit whose count is anything but one —
	// would have the gate certify iterations the command never runs, silently
	// dropping the extra ones. Refuse both, loudly, rather than plan at a sweep
	// the adapter cannot honour.
	if baseCount != 1 {
		bad("is planned at sweep base %d, but the Vitest adapter has no repeat sweep — plan at a base of 1", baseCount)
	}
	if u.Count != 1 {
		bad("runs a sweep of %d, but the Vitest adapter runs each file exactly once (no repeat sweep to divide or repeat)", u.Count)
	}

	// A -t filter or shard coordinates on a file-granularity unit would run a
	// subset while claiming the whole file.
	if len(u.Run) > 0 {
		bad("carries a name filter (%s) but the Vitest adapter runs whole files; it would skip the rest of the file", strings.Join(sortedCopy(u.Run), "|"))
	}
	if u.Shard != 0 || u.Shards != 0 {
		bad("is a %s but carries count-shard coordinates %d/%d", u.Kind, u.Shard, u.Shards)
	}
	if math.IsNaN(u.Seconds) || math.IsInf(u.Seconds, 0) || u.Seconds < 0 {
		bad("has weight %v; a unit's estimate must be finite and non-negative", u.Seconds)
	}
	return defects
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
