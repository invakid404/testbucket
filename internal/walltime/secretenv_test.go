package walltime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// wallPrivateKeyEnvPattern matches the naming convention every wall-time
// signing capability follows. It is named distinctly so an external contract
// control dropped into this package does not collide with it.
var wallPrivateKeyEnvPattern = regexp.MustCompile(`"(TB_WALL_[A-Z0-9_]*KEY)"`)

// TestEveryPrivateKeyEnvironmentVariableIsScrubbed scans the REPOSITORY, not
// the denylist.
//
// The observer scrub test beside this one enumerates WallTimeSecretEnv and
// checks each entry is removed. That proves the list is enforced; it cannot
// prove the list is complete, because it derives its expectation from the very
// thing under test. `TB_WALL_BUILDER_KEY` was introduced in cmd/testbucket,
// never added to the denylist, and leaked to every detached observer while
// that test went on passing.
//
// This reads the source instead: any `TB_WALL_*KEY` literal anywhere in the
// tree is a signing capability, and one the scrub list does not carry is a
// capability that outlives the step it was granted to.
//
// A CAPABILITY IS NOT ONLY A KEY. `TB_WALL_WORKLOAD_USER` names the account
// the measured work runs under, and an observer holding it can be asked to
// become the thing it observes — so the scrub list carries it too, and this
// test knows the difference between a capability and a configuration value
// rather than pattern-matching on the word KEY.
func TestEveryPrivateKeyEnvironmentVariableIsScrubbed(t *testing.T) {
	scrubbed := map[string]bool{}
	for _, k := range WallTimeSecretEnv {
		scrubbed[k] = true
	}
	found := map[string][]string{}
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".jj", ".git", "node_modules", "dist", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".yml", ".yaml", ".sh":
		default:
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range wallPrivateKeyEnvPattern.FindAllStringSubmatch(string(b), -1) {
			name := m[1]
			// TB_WALL_DIR and the cgroup root are operational values, not
			// capabilities; the pattern only matches names ending in KEY, and
			// a PUBLIC key is not a capability either.
			if strings.Contains(name, "PUBLIC") {
				continue
			}
			found[name] = append(found[name], path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("the scan found no TB_WALL_*KEY literal at all; it is not reading the tree")
	}
	for name, where := range found {
		if !scrubbed[name] {
			t.Errorf("%s is a signing capability (declared in %s) that WallTimeSecretEnv does not scrub; every observer would inherit it",
				name, strings.Join(dedupePaths(where), ", "))
		}
	}
	// And the denylist must not carry a name nothing declares: a stale entry
	// is a rule about a capability that no longer exists, which quietly
	// weakens the guarantee this test is meant to give.
	for _, name := range WallTimeSecretEnv {
		if nonKeyCapabilities[name] {
			continue
		}
		if _, ok := found[name]; !ok {
			t.Errorf("WallTimeSecretEnv scrubs %s, which nothing in the tree declares", name)
		}
	}
	// The non-key capabilities are named here so adding one is a deliberate
	// act, and so this test still refuses a stale KEY entry.
	for name := range nonKeyCapabilities {
		if !scrubbed[name] {
			t.Errorf("%s confers a capability and is not scrubbed", name)
		}
	}
}

// nonKeyCapabilities are the capability-conferring variables that are not
// signing keys. An account name is one: `sudo -u <it>` is the whole of
// becoming a measured party, and an observer must never be able to become the
// thing it observes.
var nonKeyCapabilities = map[string]bool{
	WorkloadUserEnv: true,
	// The SCRIPT account name is the same capability one level up: the script
	// level drops with `sudo -u $TB_WALL_SCRIPT_USER` exactly as the
	// invocation level drops with the workload name. It was introduced with
	// the second measured party and left out of the scrub, so every observer
	// inherited it.
	ScriptUserEnv: true,
}

func dedupePaths(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
