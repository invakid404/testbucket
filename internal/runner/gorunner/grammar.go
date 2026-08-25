package gorunner

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
)

// runNameForbidden lists the characters that must never appear in a name the
// renderer will splice into `-run '^(a|b|c)$'`.
//
// Go test names are identifiers, so none of these can occur legitimately —
// which is exactly why one appearing means the slice was built by something
// other than the resolver. A stray `|` adds an alternative, a `.` or `*` widens
// the match, and a `/` retargets the filter at a SUBTEST, quietly running one
// child instead of the whole top-level test.
const runNameForbidden = "|()[]{}.*+?^$\\/ \t\n\r\"'`"

// validateUnitGrammar checks every field of a final unit that the renderer can
// turn into emitted `go test` semantics, plus the fields the gate's own later
// checks depend on.
//
// The pairing with renderBucket/goTestArgs is deliberate: every field of Unit
// is classified as either command-changing (and validated here) or provably
// inert in the renderer. Without that, this gate keeps growing one "it never
// checked field X" hole at a time — the field is emitted, the gate ignores it,
// and the plan passes while running something other than what it claims.
//
// Defects are returned rather than thrown so one bad unit reports every problem
// it has, and so the caller can refuse to credit it.
func validateUnitGrammar(u runner.Unit, live map[string]runner.LivePackage, baseCount int) []string {
	var defects []string
	label := unitLabel(u)
	bad := func(format string, a ...any) {
		defects = append(defects, label+" "+fmt.Sprintf(format, a...))
	}

	// Kind first: the renderer switches on it to decide whether a unit gets a
	// solo invocation or is merged into a shared one, and it treats anything it
	// does not recognise as a mergeable whole-package unit. A zero-value kind
	// therefore does not fail loudly, it fails quietly.
	switch u.Kind {
	case runner.KindPackage, runner.KindModuleAtom, runner.KindCountShard, runner.KindRunSlice:
	default:
		bad("has unknown kind %q; the renderer merges anything it does not recognise into a shared whole-package invocation", u.Kind)
		// Every remaining rule is kind-specific, so continuing would only
		// produce noise on top of a unit that is already unschedulable.
		return defects
	}

	// Arity. A sub-package unit carries per-invocation arguments — one -run
	// regex, one divided -count — computed for exactly one package, and the
	// renderer applies them to every package in the unit. A passenger would run
	// under the FIRST package's regex; zero packages used to panic at the [0]
	// index where this gate promises an error.
	switch u.Kind {
	case runner.KindRunSlice, runner.KindCountShard:
		if len(u.Packages) != 1 {
			bad("is a %s over %d packages; a sub-package unit must cover exactly 1 (%s)",
				u.Kind, len(u.Packages), importPathsOf(u.Packages))
			return defects
		}
	default:
		if len(u.Packages) == 0 {
			bad("is a %s covering no package at all", u.Kind)
			return defects
		}
	}

	if strings.TrimSpace(u.ID) == "" {
		bad("has no ID; the renderer keys sub-package invocations by unit ID, so two unnamed units collapse into one command and one of them never runs")
	}

	// The resolution envelope. Mode picks the invocation's working directory and
	// whether GOWORK=off is exported; Module is that directory. A unit whose
	// envelope disagrees with its packages emits `cd <wrong module>` with
	// patterns relative to a different one, or resolves an out-of-workspace
	// package by import path from the repo root, where it does not exist.
	switch u.Mode {
	case runner.ModeWork, runner.ModeOff:
	default:
		bad("has unknown resolution mode %q; the renderer only knows %q and %q", u.Mode, runner.ModeWork, runner.ModeOff)
	}
	if strings.TrimSpace(u.Module) == "" {
		bad("has no module directory; a GOWORK=off invocation would cd to an empty path")
	}

	// Packages must be live, test-bearing, and described exactly as the tree
	// describes them. Comparing the whole LivePackage — not just the import
	// path — closes the sub-grammar the renderer reads out of it (Dir for
	// module-relative patterns and for the Node prefix match, Module and Mode
	// for the envelope) in one rule, including any field added to LivePackage
	// later.
	for _, p := range u.Packages {
		lp, ok := live[p.ID]
		switch {
		case !ok:
			bad("names %s, which is not in the live package set; the emitted pattern would not resolve to a package the tree has", p.ID)
			continue
		case !lp.HasTests:
			bad("names %s, which has no test files", p.ID)
			continue
		case lp != p:
			bad("describes %s differently from the live tree (unit has dir=%q module=%q mode=%q, tree has dir=%q module=%q mode=%q)",
				p.ID, p.Dir, p.Module, p.Mode, lp.Dir, lp.Module, lp.Mode)
			continue
		}
		if p.Mode != u.Mode {
			bad("runs in %q mode but %s resolves in %q", u.Mode, p.ID, p.Mode)
		}
		if p.Module != u.Module {
			bad("runs from module %q but %s lives in %q", u.Module, p.ID, p.Module)
		}
	}

	// -count. The renderer emits u.Count for EVERY kind, so a zero here is not
	// an inert field: `go test -count=0` runs nothing at all and reports
	// success.
	if u.Count < 1 {
		bad("runs -count=%d; go test -count=0 executes nothing and still passes", u.Count)
	} else if baseCount > 0 {
		switch u.Kind {
		case runner.KindPackage, runner.KindModuleAtom, runner.KindRunSlice:
			// These run their whole selection once per requested iteration; only
			// count-shards divide the sweep, and their aggregate is checked at
			// group level.
			if u.Count < baseCount {
				bad("runs -count=%d, weakening the requested -count=%d flake sweep", u.Count, baseCount)
			}
		}
	}

	// -run. This is the sharpest of the lot: goTestArgs emits a -run filter
	// whenever Run is non-empty, for ANY kind. A filter on a unit that is not a
	// name-slice therefore runs only the named runnables and silently skips
	// every other test, example and fuzz target in the package — while the unit
	// still looks like complete coverage of it.
	if len(u.Run) > 0 && u.Kind != runner.KindRunSlice {
		bad("is a %s carrying a -run filter (%s); the renderer emits -run for any kind, so this would execute only those names and silently skip the rest of the package",
			u.Kind, strings.Join(u.Run, "|"))
	}
	if u.Kind == runner.KindRunSlice && len(u.Run) == 0 {
		bad("is a run-slice with an empty -run set; the renderer would emit no -run at all and run the whole package, duplicating whatever the other slices cover")
	}
	for _, n := range u.Run {
		switch {
		case strings.TrimSpace(n) == "":
			bad("has an empty name in its -run set")
		case strings.ContainsAny(n, runNameForbidden):
			bad("has %q in its -run set; a Go test name cannot contain a regex metacharacter, so this would change what the alternation matches", n)
		}
	}

	// Shard coordinates. The renderer ignores these, but the gate's own
	// group-completeness check is derived from them, so an incoherent pair would
	// corrupt the evidence rather than the command.
	switch u.Kind {
	case runner.KindCountShard:
		if u.Shards < 2 {
			bad("declares %d count-shards; a split must have at least 2", u.Shards)
		} else if u.Shard < 1 || u.Shard > u.Shards {
			bad("has shard %d outside the 1..%d group it declares", u.Shard, u.Shards)
		}
	default:
		if u.Shard != 0 || u.Shards != 0 {
			bad("is a %s but carries count-shard coordinates %d/%d", u.Kind, u.Shard, u.Shards)
		}
	}

	// Weight. Not a command, but it is what the balancer partitioned and what
	// the plan advertises as the bucket's cost; a non-finite or negative value
	// makes every estimate downstream meaningless.
	if math.IsNaN(u.Seconds) || math.IsInf(u.Seconds, 0) || u.Seconds < 0 {
		bad("has weight %v; a unit's estimate must be a finite, non-negative number of seconds", u.Seconds)
	}

	return defects
}

// unitLabel names a unit in a defect message, including when it has no ID.
func unitLabel(u runner.Unit) string {
	if strings.TrimSpace(u.ID) != "" {
		return u.ID
	}
	kind := string(u.Kind)
	if kind == "" {
		kind = "kindless"
	}
	return "<unnamed " + kind + " unit>"
}

// importPathsOf renders a unit's packages for an error message.
func importPathsOf(pkgs []runner.LivePackage) string {
	if len(pkgs) == 0 {
		return "none"
	}
	out := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		out = append(out, p.ID)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
