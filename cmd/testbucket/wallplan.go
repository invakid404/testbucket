package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

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

// planningSnapshot is THE ONE READ of the process environment.
//
// The comment used to claim a single snapshot while the code took two: the
// runner was constructed from planningEnvArgs() and the bundle recorded
// planningEnv() afterwards, and each called os.Environ() for itself. Anything
// that changed a variable between those two calls — and Go makes that possible
// and concurrency-safe — produced a bundle describing an environment the
// subprocesses had not run under, which is exactly the equivalence the bundle
// exists to establish.
//
// One memoized read makes the two call sites return the same environment by
// construction rather than by ordering. It is memoized rather than threaded
// through because both helpers are called from different layers of the command
// and the property must hold however they are ordered.
var planningSnapshot = sync.OnceValue(os.Environ)

// resetPlanningSnapshot re-reads the environment. Only tests use it: a test
// that sets a variable and then asks what the acquisition would record needs
// the snapshot taken after its own setup, and nothing in production may
// re-read it.
func resetPlanningSnapshot() { planningSnapshot = sync.OnceValue(os.Environ) }

// packageRunners are the launchers whose next non-flag argument is a PACKAGE
// EXECUTABLE they select and execute, rather than a file they read.
var packageRunners = map[string]map[string]bool{
	"pnpm": {"exec": true, "dlx": true},
	"npm":  {"exec": true},
	"yarn": {"exec": true, "dlx": true},
	"bun":  {"x": true},
	"npx":  {},
}

type delegated struct{ name, path string }

// delegatedProgram resolves the executable a launcher selects.
//
// It looks where the package manager looks — the project's `node_modules/.bin`
// under the acquisition cwd, then PATH — because that is what decides which
// bytes run. A launcher whose delegated program cannot be resolved is an
// unbound input and fails closed, exactly as an unresolvable head does: the
// façade would run something, and the bundle could not say what.
func delegatedProgram(root string, argv []string) (delegated, error) {
	if len(argv) < 2 {
		return delegated{}, nil
	}
	sub, ok := packageRunners[filepath.Base(argv[0])]
	if !ok {
		return delegated{}, nil
	}
	rest := argv[1:]
	// `npx <prog>` delegates directly; `pnpm exec <prog>` needs its
	// subcommand first.
	if len(sub) > 0 {
		if !sub[rest[0]] {
			return delegated{}, nil
		}
		rest = rest[1:]
	}
	for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return delegated{}, nil
	}
	name := rest[0]
	if p := filepath.Join(root, "node_modules", ".bin", name); fileExists(p) {
		// ABSOLUTE. With a relative root this returned `node_modules/.bin/...`,
		// which names a different program in every working directory — an
		// executable identity that is not an identity.
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		return delegated{name: name, path: p}, nil
	}
	// THE FROZEN PATH, for the same reason the head is resolved in it: a
	// package-selected launcher found on a PATH the acquisition never had is
	// a program the plan was not derived under.
	p, err := lookPathIn(planningEnvValue("PATH"), name)
	if err != nil {
		return delegated{}, fmt.Errorf("resolve the %s-selected executable %q under %s: %w; the program that actually launches the façade may not be left unbound",
			argv[0], name, root, err)
	}
	return delegated{name: name, path: p}, nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// resolveProgram finds the exact file a program name runs, SEARCHING THE
// FROZEN PATH rather than the ambient one.
//
// `exec.LookPath` reads the process environment as it is at the moment of the
// call, so a `PATH` that moved after the acquisition resolved the closure to
// programs the plan was not derived under — the bundle then bound one
// executable while the snapshot it claims to describe had run another. The
// snapshot is the plan's environment by definition, so it is the environment
// the closure is resolved in.
//
// `testbucket` names THIS process: it is the program that took the snapshot,
// and asking any PATH for it would resolve whatever copy happens to be
// installed instead.
func resolveProgram(name string) (string, error) {
	if name == "testbucket" {
		self, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve this executable: %w", err)
		}
		return self, nil
	}
	p, err := lookPathIn(planningEnvValue("PATH"), name)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w; a plan may not be derived from an unresolved program", name, err)
	}
	return p, nil
}

// planningEnvValue reads one variable out of the frozen acquisition snapshot.
func planningEnvValue(name string) string {
	for _, kv := range planningSnapshot() {
		if k, v, _ := strings.Cut(kv, "="); k == name {
			return v
		}
	}
	return ""
}

// lookPathIn is exec.LookPath against a GIVEN search path.
//
// Go's LookPath has no such form: it reads os.Getenv("PATH") itself. This
// applies the same rules — an argument containing a separator is used as
// written, otherwise each entry is tried in order — against the PATH the
// acquisition actually ran with.
func lookPathIn(searchPath, name string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) {
		if err := executable(name); err != nil {
			return "", err
		}
		return filepath.Abs(name)
	}
	for _, dir := range filepath.SplitList(searchPath) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, name)
		if err := executable(candidate); err == nil {
			return filepath.Abs(candidate)
		}
	}
	return "", fmt.Errorf("%q not found in the frozen acquisition PATH", name)
}

// executable reports whether a path names a regular file this process may
// execute.
func executable(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.IsDir() || st.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// planFromBundle is `plan`'s frozen path: instead of discovering and reading
// the clock, it replays a bundle and writes the Stage-2 receipt.
//
// The receipt is written with O_EXCL. That is the exactly-once rule made
// mechanical: the bound planner runs once, and a second run that quietly
// replaced the first receipt would be indistinguishable from the first.
// frozenPlanOptions is what the frozen `plan` path needs beyond the bundle.
type frozenPlanOptions struct {
	// claimStore overrides where the one-shot planner claim is taken. It is a
	// STORE rather than an output directory: keying the claim to the place the
	// derivation writes meant a fresh working directory saw no claim, which is
	// exactly what a job rerun looks like. Anything set here is treated as a
	// store the deployment provides and every attempt of the job resolves.
	claimStore string
	// scored says this derivation is for an eligible/scored arm, where a
	// durable claim is mandatory rather than advisory.
	scored     bool
	bundlePath string
	stage1Path string
	stage2Path string
	shardPlan  string
	asJSON     bool
	// scorerPath, when set, makes the frozen pre-plan score the ALLOCATION
	// input. Without it the partition uses the store's measured weights, which
	// is a perfectly good split and is not campaign eligible.
	scorerPath string
	// registryPath is the frozen Aeta component template; outDir is where the
	// per-bucket derived documents (Palloc, Pcheck, Aeta) are written.
	registryPath string
	outDir       string
	// authorityKeys are the PREDECLARED public keys allowed to approve the
	// Stage-1 inputs, and authority the protected environment they must name.
	// Both are required: the frozen path plans only from authorised inputs.
	authorityKeys []string
	authority     string
}

// plannerClaimStoreEnv names the DURABLE claim store.
//
// It is an environment variable rather than only a flag because the store is a
// property of the deployment, not of one invocation: every attempt of a job
// must resolve the same store, or the claim proves nothing about the attempts
// it was supposed to exclude.
const plannerClaimStoreEnv = "TB_WALL_PLANNER_CLAIM_STORE"

// plannerClaimAttestationEnv carries the campaign authority's signature over
// the claim store's identity. Without it a store is not durable, whatever its
// pathname suggests.
const plannerClaimAttestationEnv = "TB_WALL_PLANNER_CLAIM_STORE_ATTESTATION"

// plannerClaimAuthorityKeysEnv carries the predeclared campaign-authority
// public keys the store attestation is verified against. A signature checked
// against whatever signed it is one anybody can mint.
const plannerClaimAuthorityKeysEnv = "TB_WALL_CAMPAIGN_AUTHORITY_KEYS"

// machineClaimStore is a stable location that does not move with the working
// directory. It is still one machine's disk, which is why holding a claim
// there is not durable across runners.
func machineClaimStore() (string, error) {
	base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve the machine planner claim store: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "testbucket", "planner-claims"), nil
}

// firstErr returns the first non-nil error, so a caller can fold several
// close-shaped failures into one without losing the first one that happened.
func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
