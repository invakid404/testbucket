package vitestrunner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
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
}

// Render turns one planned bucket into the concrete `vitest run` command(s). It
// mirrors the Go adapter's shape: whole-file units merge into one invocation
// (Vitest runs several files in one call), while a name slice is its own call
// (its -t filter applies to the whole invocation). Files run SERIALLY
// (--no-file-parallelism) so a bucket's wall time is the sum of its file weights
// — the cost model the balancer partitions, exactly as the Go adapter's -p=1.
func (r *Runner) Render(b runner.Bucket) runner.Rendered {
	return renderBucket(b, r.render)
}

func renderBucket(b runner.Bucket, cfg renderConfig) runner.Rendered {
	// Vitest is a Node runner: every bucket needs Node set up.
	out := runner.Rendered{NeedsNode: true}

	var wholeFiles []string
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
		for _, p := range u.Packages {
			wholeFiles = append(wholeFiles, p.ID)
		}
	}

	var invs []runner.Invocation
	if len(wholeFiles) > 0 {
		sort.Strings(wholeFiles)
		wholeFiles = dedupe(wholeFiles)
		invs = append(invs, vitestInvocation(cfg, wholeFiles, nil))
	}
	sort.Slice(slices, func(i, j int) bool { return slices[i].id < slices[j].id })
	for _, s := range slices {
		invs = append(invs, vitestInvocation(cfg, []string{s.file}, s.names))
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
	args = append(args, "run", "--no-file-parallelism")
	if len(names) > 0 {
		sorted := append([]string(nil), names...)
		sort.Strings(sorted)
		// Anchored alternation, like the Go adapter's -run: without ^...$ a name
		// "adds" would also match "adds two" and run it in two slices.
		args = append(args, "-t", fmt.Sprintf("^(%s)$", strings.Join(sorted, "|")))
	}
	files = append([]string(nil), files...)
	sort.Strings(files)
	args = append(args, files...)
	return runner.Invocation{Dir: cfg.rootRel, Args: args, Desc: strings.Join(files, " ")}
}

func shellLine(inv runner.Invocation, cfg renderConfig, bucket, seq int) string {
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
