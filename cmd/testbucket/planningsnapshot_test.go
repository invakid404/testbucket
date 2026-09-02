package main

import (
	"os"
	"strings"
	"testing"
)

// TestTheAcquisitionAndTheRecordShareOneEnvironmentSnapshot is the F11
// regression.
//
// The comment claimed one read of the environment; the code took two. The
// runner was constructed from planningEnvArgs() and the bundle recorded
// planningEnv() afterwards, and each called os.Environ() for itself — so
// anything that changed a variable between them produced a bundle describing
// an environment the subprocesses had not run under, which is precisely the
// equivalence the bundle exists to establish.
func TestTheAcquisitionAndTheRecordShareOneEnvironmentSnapshot(t *testing.T) {
	const key = "TB_PLANNING_SNAPSHOT_PROBE"
	t.Setenv(key, "executed-value")
	resetPlanningSnapshot()
	t.Cleanup(resetPlanningSnapshot)

	executed := planningEnvArgs()
	// The environment moves between the two production calls. It can: Go
	// makes os.Setenv concurrency-safe and callable at any time.
	if err := os.Setenv(key, "retained-value"); err != nil {
		t.Fatal(err)
	}
	retained := planningEnv()

	var seen string
	for _, kv := range executed {
		if k, v, _ := strings.Cut(kv, "="); k == key {
			seen = v
		}
	}
	if seen != retained[key] {
		t.Errorf("the acquisition ran with %s=%q and the bundle retained %q; two snapshots cannot describe one acquisition",
			seen, key, retained[key])
	}
	if seen != "executed-value" {
		t.Errorf("the snapshot was taken after the environment moved: %q", seen)
	}

	// The reset is what makes a test able to set up its own environment; it
	// must not exist in the production path.
	b, err := os.ReadFile("wallplan.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	// THE ONLY READ IS THE MEMOIZED ONE. Any other os.Environ() in the
	// acquisition path is a second snapshot by definition.
	var reads int
	for _, line := range strings.Split(src, "\n") {
		code, _, _ := strings.Cut(line, "//")
		if strings.Contains(code, "os.Environ") {
			reads++
			if !strings.Contains(code, "sync.OnceValue(os.Environ)") {
				t.Errorf("the acquisition reads the environment outside the single snapshot: %s", strings.TrimSpace(line))
			}
		}
	}
	if reads != 2 {
		t.Errorf("wallplan.go has %d os.Environ reads; expected the snapshot and the test-only reset", reads)
	}
	if !strings.Contains(src, "for _, kv := range planningSnapshot()") {
		t.Error("planningEnvironment does not read the shared snapshot")
	}
	// The reset exists for tests, and production must never call it.
	before, _, _ := strings.Cut(src, "func resetPlanningSnapshot")
	if strings.Contains(before, "resetPlanningSnapshot()") {
		t.Error("production code re-reads the environment through resetPlanningSnapshot")
	}
}
