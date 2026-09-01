package vitestrunner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// renderConfig is the Vitest adapter's own render configuration.
type renderConfig struct {
	// command is the Vitest invocation, e.g. ["npx", "vitest"].
	command []string
	// rootRel is the project directory the emitted script cds into, relative to
	// the CI checkout (the subprocess path is absolute; this is for the script).
	rootRel string
	// eventsDir, when set, makes each invocation write its JSON report there for
	// a later ingest.
	eventsDir string
	// fileParallelism is the intra-bucket file concurrency (#22). 1 (the default)
	// keeps Vitest serial (--no-file-parallelism), which is the cost model the
	// balancer packs to: a bucket's wall time is the SUM of its file weights.
	// A value >1 bounds Vitest's own file parallelism (--maxWorkers=N) instead,
	// trading that sum-of-weights guarantee — a bucket then finishes nearer its
	// heaviest file than its sum — for using more of a runner's cores.
	fileParallelism int
	// wallDir, when set, runs each invocation under `testbucket wall exec` so
	// it gets a physical envelope (V), a containment peer (CPV) and an
	// independent trace (VT). Empty — the default — renders exactly the bytes
	// v0.2.2 rendered: measurement is opt-in, and a consumer that does not ask
	// for it sees no change at all.
	wallDir string
}

// Render turns one planned bucket into the concrete `vitest run` command(s). It
// mirrors the Go adapter's shape: whole-file units merge into one invocation
// (Vitest runs several files in one call), while a name slice is its own call
// (its -t filter applies to the whole invocation). Files run SERIALLY by default
// (--no-file-parallelism) so a bucket's wall time is the sum of its file weights
// — the cost model the balancer partitions, exactly as the Go adapter's -p=1.
// The fileParallelism knob (#22) can bound intra-bucket concurrency instead; see
// renderConfig.fileParallelism for the makespan trade-off.
func (r *Runner) Render(b runner.Bucket) runner.Rendered {
	return renderBucket(b, r.render)
}

func renderBucket(b runner.Bucket, cfg renderConfig) runner.Rendered {
	// Vitest is a Node runner: every bucket needs Node set up.
	out := runner.Rendered{NeedsNode: true}

	var wholeFiles []string
	var wholeUnits []string
	type slice struct {
		id    string
		file  string
		names []string
	}
	var slices []slice

	for _, u := range b.Units {
		if u.Kind == runner.KindRunSlice && len(u.Packages) == 1 && len(u.Run) > 0 {
			slices = append(slices, slice{id: u.ID, file: u.Packages[0].ID, names: append([]string(nil), u.Run...)})
			continue
		}
		wholeUnits = append(wholeUnits, u.ID)
		for _, p := range u.Packages {
			wholeFiles = append(wholeFiles, p.ID)
		}
	}

	var invs []runner.Invocation
	if len(wholeFiles) > 0 {
		sort.Strings(wholeFiles)
		wholeFiles = dedupe(wholeFiles)
		sort.Strings(wholeUnits)
		inv := vitestInvocation(cfg, wholeFiles, nil)
		inv.Units = wholeUnits
		invs = append(invs, inv)
	}
	sort.Slice(slices, func(i, j int) bool { return slices[i].id < slices[j].id })
	for _, s := range slices {
		inv := vitestInvocation(cfg, []string{s.file}, s.names)
		inv.Units = []string{s.id}
		invs = append(invs, inv)
	}

	out.Invocations = invs
	var lines []string
	for i, inv := range invs {
		lines = append(lines, shellLine(inv, cfg, b.Index, i))
	}
	out.Script = strings.Join(append([]string{"set -euo pipefail"}, lines...), "\n")
	return out
}

// vitestInvocation builds one `vitest run` call. names, when non-empty, add an
// anchored -t name filter selecting exactly those tests.
func vitestInvocation(cfg renderConfig, files, names []string) runner.Invocation {
	args := append([]string(nil), cfg.command...)
	args = append(args, "run")
	// Intra-bucket file concurrency (#22). Default (1) stays serial so a bucket's
	// wall time is the sum of its file weights — what the balancer partitioned.
	if cfg.fileParallelism > 1 {
		args = append(args, fmt.Sprintf("--maxWorkers=%d", cfg.fileParallelism))
	} else {
		args = append(args, "--no-file-parallelism")
	}
	if len(names) > 0 {
		sorted := append([]string(nil), names...)
		sort.Strings(sorted)
		// A robust, anchored testNamePattern: -t matches the reporter's
		// space-joined full name, which the `" > "`-joined ids cannot be turned
		// into unambiguously, so runPattern matches every possible resolution and
		// never drops a test to a naming guess. See names.go.
		args = append(args, "-t", runPattern(sorted))
	}
	files = append([]string(nil), files...)
	sort.Strings(files)
	// Each file goes in as a PATH TOKEN (`./x`), never a bare id: Vitest/CAC reads
	// a positional beginning with '-' as an OPTION, so a root-level spec such as
	// `--odd.spec.ts` would fail the whole invocation with "Unknown option". This
	// is the same normalisation `Runnables` applies to its `vitest list` filter
	// (see filterPathArg), so plan-time listing and run-time execution now name a
	// file exactly one way.
	//
	// It deliberately does NOT claim to narrow WHICH files Vitest selects. A
	// positional is matched with `testFile.includes(filter)` against the
	// root-relative path, and Vitest normalises `./x` back to `x` before that
	// test, so a filter still selects every file whose path CONTAINS it. Making
	// the RUN exact is assignFilterAtoms' job on the discovery side: it
	// co-schedules the files a filter cannot tell apart into ONE invocation, so
	// an over-match can never cross an invocation boundary. See collide.go.
	for _, f := range files {
		args = append(args, filterPathArg(f))
	}
	// Desc keeps the canonical ids: it is the human/plan-facing identity, and it
	// must keep matching the store keys and the ids the reporter reports.
	return runner.Invocation{Dir: cfg.rootRel, Args: args, Desc: strings.Join(files, " ")}
}

func shellLine(inv runner.Invocation, cfg renderConfig, bucket, seq int) string {
	if cfg.wallDir != "" {
		return wallLine(inv, cfg, bucket, seq)
	}
	var sb strings.Builder
	sb.WriteString("( cd ")
	sb.WriteString(shellQuote(inv.Dir))
	sb.WriteString(" && ")
	for i, a := range inv.Args {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(shellQuote(a))
	}
	if cfg.eventsDir != "" {
		// A second reporter writes machine-readable JSON for `ingest` while the
		// default reporter keeps the console log human-readable; the JSON goes to
		// a per-bucket file the record job collects.
		fmt.Fprintf(&sb, " --reporter=default --reporter=json --outputFile.json=%s",
			shellQuote(fmt.Sprintf("%s/bucket-%d-%02d.json", strings.TrimSuffix(cfg.eventsDir, "/"), bucket, seq)))
	}
	sb.WriteString(" )")
	return sb.String()
}

// wallLine renders one invocation under the physical wrapper. The command is
// written out as a SPEC FILE and the wrapper is handed its path, so the argv
// the plan digested is the argv that executes: no shell re-splits it, and a
// file name with a space or a leading dash cannot become different work
// between planning and running.
//
// The spec is embedded in the script rather than written beside it because the
// script bytes are what the Stage-2 receipt digests. A spec that lived
// elsewhere could change without changing the plan.
func wallLine(inv runner.Invocation, cfg renderConfig, bucket, seq int) string {
	args := append([]string(nil), inv.Args...)
	if cfg.eventsDir != "" {
		args = append(args, "--reporter=default", "--reporter=json",
			fmt.Sprintf("--outputFile.json=%s/bucket-%d-%02d.json", strings.TrimSuffix(cfg.eventsDir, "/"), bucket, seq))
	}
	spec, err := walltime.MarshalSpec(walltime.InvocationSpec{
		Seq: seq, Argv: args, Cwd: inv.Dir, Selector: strings.Fields(inv.Desc), Desc: inv.Desc,
	})
	if err != nil {
		// MarshalSpec fails only on a value encoding/json cannot represent,
		// which an argv of strings cannot be; a panic here would be a bug in
		// this function, not a runtime condition.
		panic(err)
	}
	dir := strings.TrimSuffix(cfg.wallDir, "/")
	specPath := fmt.Sprintf("%s/spec-%d-%02d.json", dir, bucket, seq)
	var sb strings.Builder
	sb.WriteString("printf '%s' ")
	sb.WriteString(shellQuote(spec))
	sb.WriteString(" > ")
	sb.WriteString(shellQuote(specPath))
	sb.WriteString(" && testbucket wall exec --dir ")
	sb.WriteString(shellQuote(dir))
	sb.WriteString(" --level invocation --seq ")
	fmt.Fprintf(&sb, "%d", seq)
	sb.WriteString(" --spec ")
	sb.WriteString(shellQuote(specPath))
	return sb.String()
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r == '.' || r == '/' || r == '-' || r == '_' || r == '=' || r == ':' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func dedupe(sorted []string) []string {
	if len(sorted) == 0 {
		return nil
	}
	out := sorted[:1]
	for _, v := range sorted[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
