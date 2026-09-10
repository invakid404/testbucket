package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// runtimeProfileInputs is what the planner knows about the environment it is
// planning in, before it observes the rest.
type runtimeProfileInputs struct {
	runnerKind    string
	root          string
	workingDir    string
	vitestCommand string
	cacheDecl     *walltime.CacheDeclaration
}

// buildRuntimeProfile OBSERVES §15.3a's runtime leaves rather than accepting
// them.
//
// The point of the declared/executed split is that the two can be compared: a
// declaration a caller typed and an execution nobody looked at would agree by
// construction and prove nothing. So each leaf here is read from the toolchain
// this process can actually reach, and a leaf that cannot be observed is left
// EMPTY rather than filled with a plausible value — an absent fact is
// reportable, an invented one is not.
func buildRuntimeProfile(in runtimeProfileInputs) walltime.RuntimeProfile {
	p := walltime.RuntimeProfile{
		TestbucketSHA256: selfBinaryDigest(),
		FacadeCommand:    strings.TrimSpace(in.vitestCommand),
		LockSHA256:       lockDigest(in.workingDir),
	}
	if in.cacheDecl != nil {
		p.DependencyCacheMode = in.cacheDecl.DependencyCacheMode
	}
	// The Node toolchain leaves only mean something for the Vitest adapter;
	// the Go lane is deliberately unwrapped and has none.
	if in.runnerKind == "vitest" {
		p.NodeVersion = toolVersion(in.workingDir, "node", "--version")
		p.PnpmVersion = toolVersion(in.workingDir, "pnpm", "--version")
		p.VitestVersion = vitestVersion(in.workingDir, in.vitestCommand)
	}
	return p
}

// runtimeProfileMap renders the profile as the registered object the plan
// document carries, through the profile's own ordered JSON so the map and the
// digest cannot describe different values.
func runtimeProfileMap(p walltime.RuntimeProfile) map[string]string {
	var out map[string]string
	if err := json.Unmarshal(p.OrderedJSON(), &out); err != nil {
		return nil
	}
	return out
}

// toolVersion asks one tool for its version, briefly. A tool that is absent,
// slow or unhappy yields no leaf rather than failing the plan: the profile
// records what was observable, and QC17 is what decides whether that is
// enough for the run in question.
func toolVersion(dir, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
}

// vitestVersion asks the configured façade for its version, so the leaf is the
// version that will actually run rather than whatever is on PATH.
func vitestVersion(dir, command string) string {
	argv := strings.Fields(command)
	if len(argv) == 0 {
		argv = []string{"npx", "vitest"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], append(append([]string(nil), argv[1:]...), "--version")...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	// `vitest --version` prints e.g. "vitest/4.1.11 darwin-arm64 node-v26.8.1".
	fields := strings.Fields(strings.TrimSpace(string(out)))
	for _, f := range fields {
		if v, ok := strings.CutPrefix(f, "vitest/"); ok {
			return v
		}
	}
	if len(fields) > 0 {
		return strings.TrimPrefix(fields[len(fields)-1], "v")
	}
	return ""
}

// selfBinaryDigest is the SHA-256 of the binary that is planning. It is the one
// runtime leaf this process can always answer for: whatever else is
// unobservable, the code deciding the plan is on disk in front of it.
func selfBinaryDigest() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(self)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// lockDigest hashes the dependency lock governing the working directory. A
// changed lock is a changed workload, which §15.3 makes a different history.
func lockDigest(dir string) string {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	for _, name := range []string{"pnpm-lock.yaml", "package-lock.json", "yarn.lock"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(b)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	return ""
}

// storeDigest hashes the store the plan was built from, so an observation can
// say which store state produced it.
func storeDigest(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// expandedUnitSetDigest is the identity of the unit TOPOLOGY the plan was built
// over. It is store-derived in both bases, which is what makes it
// byte-identical across a B/C pair: only the partition differs.
func expandedUnitSetDigest(live []runner.LivePackage) string {
	ids := make([]string, 0, len(live))
	for _, p := range live {
		if !p.HasTests {
			continue
		}
		ids = append(ids, p.ID)
	}
	sort.Strings(ids)
	b, err := json.Marshal(ids)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// bucketIndices is the profile's declared bucket set, 0..K-1.
func bucketIndices(k int) []int {
	out := make([]int, 0, k)
	for i := 0; i < k; i++ {
		out = append(out, i)
	}
	return out
}

// atomicWriteJSON writes a document so a reader never sees a partial one: a
// proposal that was interrupted halfway is not a proposal.
func atomicWriteJSON(path string, doc any) error {
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}

// runCalibration is contract §17.3a's proposer mode.
//
// It answers a different question from planning: not "how do I split this
// work" but "is there a layout whose design matrix reaches rank 4, so a fit
// can identify all four parameters at all". A run that cannot reach rank is
// not a failed plan — it is a universe that cannot calibrate, and the outcome
// says which of the two it is.
//
// It emits EVIDENCE and no matrix. The two are mutually exclusive on purpose:
// a caller that asked for a proposal must not receive something it could fan
// out over.
func runCalibration(rnr runner.Runner, opt core.PlanOptions, out string, maxPlans int) error {
	units, err := calibrationUniverse(rnr, opt)
	if err != nil {
		return err
	}
	ev, err := walltime.CalibrateProposer(units, opt.K, maxPlans, kkPacker)
	if err != nil {
		return err
	}
	if err := atomicWriteJSON(out, ev); err != nil {
		return fmt.Errorf("write calibration evidence: %w", err)
	}
	fmt.Fprintf(os.Stderr, "testbucket plan --calibrate: %s after %d of %d layout(s); evidence written to %s\n",
		ev.Outcome, ev.LayoutsTried, ev.LayoutBudget, out)
	if ev.Outcome != walltime.CalibrationSufficient {
		// A proposal that did not find a calibrating layout is a real answer,
		// and it is not success: a caller that fanned out anyway would be
		// measuring a population no fit can use.
		return fmt.Errorf("calibration %s: %s", ev.Outcome, ev.Reason)
	}
	return nil
}

// calibrationUniverse is the candidate unit universe the proposer searches
// over: the live targets with their stored weights, in the exact
// integer-nanosecond domain the design matrix is built in.
//
// It reuses the planner's own expansion rather than re-deriving one, so the
// units a proposal is made of are the units a plan would actually schedule.
func calibrationUniverse(rnr runner.Runner, opt core.PlanOptions) ([]walltime.CalibrationUnit, error) {
	units, err := core.ExpandUnitsFor(context.Background(), rnr, nil, opt)
	if err != nil {
		return nil, err
	}
	out := make([]walltime.CalibrationUnit, 0, len(units))
	for _, u := range units {
		ns, err := core.ReporterNs(u)
		if err != nil {
			return nil, fmt.Errorf("unit %s: %w", u.ID, err)
		}
		out = append(out, walltime.CalibrationUnit{
			ID:      u.ID,
			BaseNs:  ns,
			IsSlice: u.Kind == runner.KindRunSlice || u.Kind == runner.KindCountShard,
		})
	}
	return out, nil
}

// kkPacker is the injected packer §6.5 fixes: the SAME deterministic
// Karmarkar–Karp the reporter basis uses, not a second implementation that
// would drift from it.
func kkPacker(units []walltime.CalibrationUnit, slots int) [][]walltime.CalibrationUnit {
	items := make([]core.Item, 0, len(units))
	byID := make(map[string]walltime.CalibrationUnit, len(units))
	for _, u := range units {
		items = append(items, core.Item{ID: u.ID, Weight: float64(u.BaseNs)})
		byID[u.ID] = u
	}
	groups := core.KarmarkarKarp(items, slots)
	out := make([][]walltime.CalibrationUnit, len(groups))
	for i, g := range groups {
		for _, it := range g {
			out[i] = append(out[i], byID[it.ID])
		}
	}
	return out
}

// atomicWriteJSONCompact writes a document WITHOUT re-indenting it, for
// documents that carry canonical bytes another party will compare exactly.
// Pretty-printing rewrites embedded raw JSON, which silently breaks any
// byte-for-byte comparison downstream.
func atomicWriteJSONCompact(path string, doc any) error {
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}
