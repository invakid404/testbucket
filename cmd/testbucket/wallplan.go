package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/planbind"
	"github.com/invakid404/testbucket/internal/runner/vitestrunner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// runWallBundle freezes a planning-input bundle: the canonical instant, the
// raw discovery bytes, the raw runnable listings of every name-sliced target,
// the store bytes, and the acquisition closure.
//
// This is the ONLY command in the wall-time path that reads the live world.
// Everything downstream — planning, rendering, the matrix, the script — is a
// function of what this wrote.
func runWallBundle(args []string) error {
	defDiscoveryTimeout, err := discoveryTimeoutDefault()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("wall bundle", flag.ExitOnError)
	out := fs.String("out", "", "write the planning-input bundle here (required)")
	root := fs.String("root", ".", "vitest project directory")
	store := fs.String("store", "test-timings.json", "timing store to freeze; a missing store is bound as a cold start")
	k := fs.Int("k", 6, "number of buckets")
	instant := fs.String("instant", "", "canonical planning instant (RFC3339); empty uses the current time ONCE, here, and freezes it")
	staleAfter := fs.Duration("stale-after", 14*24*time.Hour, "frozen staleness policy")
	vitestCommand := fs.String("vitest-command", "", "bare-vitest invocation; empty means \"npx vitest\"")
	vitestDiscovery := fs.String("vitest-discovery", "glob", "vitest discovery mode: glob or list")
	vitestDiscoveryCommand := fs.String("vitest-discovery-command", "", "override discovery with a command run VERBATIM")
	discoveryTimeout := fs.Duration("discovery-timeout", defDiscoveryTimeout, "fail-fast deadline for discovery")
	eventsDir := fs.String("events-dir", "", "events directory the rendered script tees into")
	fileParallelism := fs.Int("file-parallelism", 1, "intra-bucket file concurrency")
	wallDir := fs.String("wall-dir", "", "records directory; when set, every rendered invocation runs under `testbucket wall exec`")
	repository := fs.String("repository", "", "source repository identity")
	commit := fs.String("commit", "", "source commit")
	tree := fs.String("tree", "", "source tree digest")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("--out is required")
	}

	// The canonical instant is read at most once, here, and then frozen. Every
	// later step reads it from the bundle.
	now := time.Now().UTC()
	if strings.TrimSpace(*instant) != "" {
		now, err = time.Parse(time.RFC3339Nano, *instant)
		if err != nil {
			return fmt.Errorf("--instant %q must be RFC3339: %w", *instant, err)
		}
	}

	rnr, err := vitestrunner.New(vitestrunner.Options{
		Root:             *root,
		Command:          splitCommand(*vitestCommand),
		DiscoveryMode:    *vitestDiscovery,
		DiscoveryCommand: splitCommand(*vitestDiscoveryCommand),
		DiscoveryTimeout: *discoveryTimeout,
	})
	if err != nil {
		return err
	}
	ctx := context.Background()
	discovery, err := rnr.CaptureDiscovery(ctx)
	if err != nil {
		return err
	}
	live, err := rnr.Discover(ctx)
	if err != nil {
		return err
	}

	// Capture a runnable listing for exactly the targets the store has flagged
	// for name slicing. Listing every file would import the whole project —
	// the cost `vitest list --filesOnly` discovery exists to avoid — and
	// listing none would leave the slice's names unbound.
	runnables := map[string][]byte{}
	st, _, err := core.LoadStore(*store)
	if err != nil {
		return err
	}
	if st != nil {
		var sliced []string
		for _, p := range live {
			row := st.Units[p.ID]
			if row != nil && row.Split == "run" && row.SplitInto >= 2 {
				sliced = append(sliced, p.ID)
			}
		}
		sort.Strings(sliced)
		for _, id := range sliced {
			raw, err := rnr.CaptureRunnables(ctx, id)
			if err != nil {
				return fmt.Errorf("capture runnables for %s: %w", id, err)
			}
			runnables[id] = raw
		}
	}

	bundle, err := planbind.Acquire(planbind.AcquireOptions{
		Root: *root, Runner: "vitest", Instant: now, StaleAfter: *staleAfter,
		K: *k, Count: 1, Token: rnr.CanonicalToken(), StorePath: *store,
		DiscoveryArgv: discoveryArgv(*vitestCommand, *vitestDiscovery, *vitestDiscoveryCommand),
		Discovery:     discovery, Runnables: runnables,
		Env: planningEnv(), Executables: resolvedExecutables(*vitestCommand),
		Tools:      map[string]string{},
		Repository: *repository, Commit: *commit, Tree: *tree,
		EventsDir: *eventsDir, FileParallelism: *fileParallelism, WallDir: *wallDir,
	})
	if err != nil {
		return err
	}
	if err := walltime.WriteJSONFile(*out, bundle); err != nil {
		return err
	}
	d, err := bundle.DigestOf()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: froze %d discovery byte(s) and %d runnable listing(s) at %s\nbundle digest: %s\n",
		len(discovery), len(runnables), bundle.Clock.Instant, d)
	fmt.Println(d)
	return nil
}

// discoveryArgv records how discovery was invoked, for the acquisition
// closure. It mirrors the adapter's own selection rather than guessing.
func discoveryArgv(command, mode, override string) []string {
	if strings.TrimSpace(override) != "" {
		return splitCommand(override)
	}
	base := splitCommand(command)
	if len(base) == 0 {
		base = []string{"npx", "vitest"}
	}
	if mode == "list" {
		return append(base, "list", "--json")
	}
	return append(base, "list", "--filesOnly", "--json")
}

// planningEnv records the environment variables that can change a plan. It is
// an allow-list, not the whole environment: a bundle that carried every
// variable would carry secrets, and a bundle nobody can publish is a bundle
// nobody can verify.
func planningEnv() map[string]string {
	out := map[string]string{}
	for _, k := range []string{
		"TB_DISCOVERY_EXCLUDE_PREFIXES", "TB_DISCOVERY_TIMEOUT",
		"VITEST_MODE", "NODE_ENV", "CI",
	} {
		out[k] = os.Getenv(k)
	}
	return out
}

// resolvedExecutables records where the discovery program actually resolved
// to, so "npx" naming two different binaries on two runners is visible.
func resolvedExecutables(command string) map[string]string {
	out := map[string]string{}
	base := splitCommand(command)
	if len(base) == 0 {
		base = []string{"npx"}
	}
	if p, err := exec.LookPath(base[0]); err == nil {
		out[base[0]] = p
	} else {
		out[base[0]] = "unresolved"
	}
	return out
}

// runWallReplay is the independent verifier's half of Stage 2: it replays the
// frozen bundle through the real planner and refuses to agree unless every
// digest matches the receipt that was issued.
func runWallReplay(args []string) error {
	fs := flag.NewFlagSet("wall replay", flag.ExitOnError)
	bundlePath := fs.String("bundle", "", "planning-input bundle to replay (required)")
	receiptPath := fs.String("stage2", "", "Stage-2 derived-plan receipt to check against (required)")
	stage1Path := fs.String("stage1", "", "Stage-1 manifest whose digest the receipt must name")
	shardPlan := fs.String("shard-plan", "", "also write the replayed plan here")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *bundlePath == "" || *receiptPath == "" {
		return fmt.Errorf("--bundle and --stage2 are both required")
	}

	var bundle walltime.PlanningInputBundle
	if err := walltime.ReadJSONFile(*bundlePath, &bundle); err != nil {
		return err
	}
	var issued walltime.Stage2Receipt
	if err := walltime.ReadJSONFile(*receiptPath, &issued); err != nil {
		return err
	}
	if err := issued.Validate(); err != nil {
		return err
	}

	stage1 := issued.Stage1Digest
	if *stage1Path != "" {
		var m walltime.Stage1Manifest
		if err := walltime.ReadJSONFile(*stage1Path, &m); err != nil {
			return err
		}
		if err := m.Validate(); err != nil {
			return err
		}
		d, err := m.DigestOf()
		if err != nil {
			return err
		}
		if d != issued.Stage1Digest {
			return fmt.Errorf("the receipt names Stage-1 %s but the supplied manifest digests to %s", issued.Stage1Digest, d)
		}
		if err := walltime.VerifySigned(m.Signature, d, nil); err != nil {
			return fmt.Errorf("stage-1 authority signature: %w", err)
		}
		stage1 = d
	}

	res, err := planbind.Plan(context.Background(), planbind.PlanOptions{Bundle: &bundle, Stage1: stage1})
	if err != nil {
		return err
	}
	if err := issued.Matches(res.Receipt); err != nil {
		return fmt.Errorf("the replayed plan does not match the issued receipt: %w", err)
	}
	if *shardPlan != "" {
		if err := writeJSONFile(*shardPlan, res.Doc); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: replay reproduced the plan exactly\n  full document: %s\n  semantic:      %s\n",
		res.Receipt.PlanDigest, res.Receipt.SemanticDigest)
	return nil
}

// planFromBundle is `plan`'s frozen path: instead of discovering and reading
// the clock, it replays a bundle and writes the Stage-2 receipt.
//
// The receipt is written with O_EXCL. That is the exactly-once rule made
// mechanical: the bound planner runs once, and a second run that quietly
// replaced the first receipt would be indistinguishable from the first.
func planFromBundle(bundlePath, stage1Path, stage2Path, shardPlan string, asJSON bool) error {
	var bundle walltime.PlanningInputBundle
	if err := walltime.ReadJSONFile(bundlePath, &bundle); err != nil {
		return err
	}
	stage1 := walltime.Digest("")
	if stage1Path != "" {
		var m walltime.Stage1Manifest
		if err := walltime.ReadJSONFile(stage1Path, &m); err != nil {
			return err
		}
		if err := m.Validate(); err != nil {
			return err
		}
		d, err := m.DigestOf()
		if err != nil {
			return err
		}
		if bd, err := bundle.DigestOf(); err == nil && m.Bundle.Kind != "" {
			mbd, err := m.Bundle.DigestOf()
			if err != nil {
				return err
			}
			if mbd != bd {
				return fmt.Errorf("the Stage-1 manifest authorises input bundle %s, not the supplied %s", mbd, bd)
			}
		}
		stage1 = d
	}

	res, err := planbind.Plan(context.Background(), planbind.PlanOptions{Bundle: &bundle, Stage1: stage1})
	if err != nil {
		return err
	}
	if shardPlan != "" {
		if err := writeJSONFile(shardPlan, res.Doc); err != nil {
			return err
		}
	}
	if stage2Path != "" {
		if err := os.MkdirAll(filepath.Dir(stage2Path), 0o755); err != nil {
			return err
		}
		if err := walltime.WriteJSONFile(stage2Path, res.Receipt); err != nil {
			return err
		}
	}

	summaryOut := os.Stdout
	if asJSON {
		summaryOut = os.Stderr
	}
	if err := res.Doc.WriteSummary(summaryOut, ""); err != nil {
		return fmt.Errorf("write plan summary: %w", err)
	}
	if asJSON {
		matrix, err := res.Doc.MatrixJSON()
		if err != nil {
			return err
		}
		if _, err := fmt.Println(string(matrix)); err != nil {
			return fmt.Errorf("write matrix: %w", err)
		}
	}
	fmt.Fprintf(os.Stderr, "testbucket plan: derived from frozen inputs\n  full document: %s\n  semantic:      %s\n",
		res.Receipt.PlanDigest, res.Receipt.SemanticDigest)
	return nil
}
