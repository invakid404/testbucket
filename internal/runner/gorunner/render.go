package gorunner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
)

// renderConfig is the Go adapter's own render configuration — the settings that
// are uniform across a plan. It lives here, not on the neutral seam: the core
// hands Render a bucket and nothing else. The per-invocation sweep count comes
// from the units themselves (Unit.Count).
type renderConfig struct {
	race         bool
	count        int
	timeout      string
	eventsDir    string
	nodePrefixes []string
	// fileParallelism is the -p value every emitted invocation carries (#22).
	// 0 falls back to serialPackages.
	fileParallelism int
}

// serialPackages is the DEFAULT -p value every emitted invocation carries.
//
// It is 1 because the balancer's objective must be the job's wall time, and the
// weights it partitions are SUMMED package elapsed times. `go test` runs package
// test binaries in parallel by default, so a coalesced invocation would finish
// in something closer to the bucket's critical package than its sum — the
// planner would be optimising a cost function the runner does not have.
// Serialising the packages makes the measured sum the thing that actually
// happens, and it makes the timings ingest records contention-free and
// therefore comparable across runs.
//
// This is NOT `-parallel`: subtests inside one package still run in parallel,
// and that concurrency is already inside the package's measured elapsed time.
// Only cross-package concurrency is given up, and it is bought back — with far
// better balance — by the K buckets themselves.
//
// The #22 fileParallelism knob can raise -p to use more of a bucket's cores, at
// the cost of that sum-of-weights guarantee: a bucket then finishes nearer its
// heaviest package than its sum, so its plan estimate over-reads and — because
// the packages now contend — the timings a `record` job ingests under it are no
// longer contention-free. Default stays 1.
const serialPackages = 1

// packageParallelism is the effective -p for this render config.
func (c renderConfig) packageParallelism() int {
	if c.fileParallelism > 1 {
		return c.fileParallelism
	}
	return serialPackages
}

// renderBucket turns a bucket's units into the concrete invocations the CI job
// will run, merging everything that legitimately shares one `go test` call and
// keeping apart everything that cannot.
func renderBucket(b runner.Bucket, cfg renderConfig) runner.Rendered {
	var out runner.Rendered
	type group struct {
		key   string
		mode  string
		dir   string
		count int
		run   []string
		paths []string
		ids   []string
	}
	var order []string
	groups := map[string]*group{}

	for _, u := range b.Units {
		if needsNode(u, cfg.nodePrefixes) {
			out.NeedsNode = true
		}

		// Workspace-mode packages resolve by import path from the repo root, so
		// they merge across module lines — the soft boundary. GOWORK=off modules
		// must run from their own directory with their own build list, so they
		// never merge with anything else.
		dir := "."
		if u.Mode == runner.ModeOff {
			dir = u.Module
		}
		key := fmt.Sprintf("plain|%s|%s|%d", u.Mode, dir, u.Count)
		switch u.Kind {
		case runner.KindCountShard, runner.KindRunSlice:
			// Each carries its own -count / -run, so it is its own call.
			key = "solo|" + u.ID
		}
		g := groups[key]
		if g == nil {
			g = &group{key: key, mode: u.Mode, dir: dir, count: u.Count, run: u.Run}
			groups[key] = g
			order = append(order, key)
		}
		g.ids = append(g.ids, u.ID)
		for _, p := range u.Packages {
			if u.Mode == runner.ModeOff {
				g.paths = append(g.paths, pattern(p))
				continue
			}
			g.paths = append(g.paths, p.ID)
		}
	}

	sort.Strings(order)
	var lines []string
	for _, key := range order {
		g := groups[key]
		sort.Strings(g.paths)
		inv := runner.Invocation{Dir: g.dir, Args: goTestArgs(cfg, g.count, g.run, g.paths)}
		if g.mode == runner.ModeOff {
			inv.Env = map[string]string{"GOWORK": "off"}
		}
		sort.Strings(g.ids)
		inv.Desc = strings.Join(g.ids, " ")
		inv.Units = append([]string(nil), g.ids...)
		// The Go adapter records the same selection identity, so the neutral
		// seam means one thing for both adapters. It is data only: the Go
		// renderer's emitted bytes are unchanged.
		inv.Selector = append(append([]string(nil), g.run...), g.paths...)
		out.Invocations = append(out.Invocations, inv)
		lines = append(lines, shellLine(inv, cfg, b.Index, len(lines)))
	}
	out.Script = strings.Join(append([]string{"set -euo pipefail"}, lines...), "\n")
	return out
}

func goTestArgs(cfg renderConfig, count int, run []string, paths []string) []string {
	args := []string{"go", "test"}
	if cfg.race {
		args = append(args, "-race")
	}
	args = append(args, fmt.Sprintf("-p=%d", cfg.packageParallelism()))
	args = append(args, fmt.Sprintf("-count=%d", count))
	if cfg.timeout != "" {
		args = append(args, "-timeout", cfg.timeout)
	}
	if len(run) > 0 {
		// Anchored alternation: without ^...$ a slice named TestFoo would also
		// pull in TestFooBar and run it twice across two slices.
		args = append(args, "-run", fmt.Sprintf("^(%s)$", strings.Join(run, "|")))
	}
	if cfg.eventsDir != "" {
		args = append(args, "-json")
	}
	return append(args, paths...)
}

func shellLine(inv runner.Invocation, cfg renderConfig, bucket, seq int) string {
	var sb strings.Builder
	sb.WriteString("( cd ")
	sb.WriteString(shellQuote(inv.Dir))
	sb.WriteString(" && ")
	// The resolution-mode envelope is a command PREFIX, not a standalone
	// assignment: `GOWORK=off && go test` would set nothing for go test.
	for _, k := range runner.SortedKeys(inv.Env) {
		fmt.Fprintf(&sb, "%s=%s ", k, shellQuote(inv.Env[k]))
	}
	for i, a := range inv.Args {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(shellQuote(a))
	}
	if cfg.eventsDir != "" {
		fmt.Fprintf(&sb, " | tee -a %s", shellQuote(fmt.Sprintf("%s/bucket-%d-%02d.ndjson", strings.TrimSuffix(cfg.eventsDir, "/"), bucket, seq)))
	}
	sb.WriteString(" )")
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

func needsNode(u runner.Unit, prefixes []string) bool {
	for _, p := range u.Packages {
		for _, pre := range prefixes {
			pre = strings.TrimSpace(pre)
			if pre == "" {
				continue
			}
			if p.Dir == strings.TrimSuffix(pre, "/") || strings.HasPrefix(p.Dir, strings.TrimSuffix(pre, "/")+"/") {
				return true
			}
		}
	}
	return false
}
