package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTheAcquisitionTakesOneEnvironmentSnapshot is the F11 regression.
//
// The comment claimed one read of the environment; the code took two, and each
// called os.Environ() for itself — so anything that changed a variable between
// them made the acquisition resolve programs in one environment while
// reporting another. Go makes os.Setenv callable and concurrency-safe at any
// time, so "nothing changes it in practice" is not a property.
//
// It is checked through the behaviour rather than by counting os.Environ calls
// in the source: a second read added anywhere in the acquisition path fails
// this, whatever it is spelled.
func TestTheAcquisitionTakesOneEnvironmentSnapshot(t *testing.T) {
	const key = "TB_PLANNING_SNAPSHOT_PROBE"
	t.Setenv(key, "snapshot-value")
	resetPlanningSnapshot()
	t.Cleanup(resetPlanningSnapshot)

	if got := planningEnvValue(key); got != "snapshot-value" {
		t.Fatalf("the snapshot reports %q, want the value it was taken at", got)
	}
	// The environment moves AFTER the snapshot. Everything the acquisition
	// does from here has to keep describing the environment it started in.
	if err := os.Setenv(key, "moved-value"); err != nil {
		t.Fatal(err)
	}
	if got := planningEnvValue(key); got != "snapshot-value" {
		t.Errorf("the snapshot re-read the environment and now reports %q; two reads cannot describe one acquisition", got)
	}
}

// TestTheClosureIsResolvedInTheFrozenEnvironment is the F6 regression.
//
// The closure used to be resolved with exec.LookPath, which reads the process
// environment at the moment of the call. A PATH that moved after the snapshot
// therefore bound a program the plan was not derived under — the resolution
// has to happen in the frozen PATH, not the ambient one.
func TestTheClosureIsResolvedInTheFrozenEnvironment(t *testing.T) {
	frozen, ambient := t.TempDir(), t.TempDir()
	// Two different executables with the SAME name: whichever one resolves
	// names the environment the resolution actually used.
	for _, dir := range []string{frozen, ambient} {
		if err := os.WriteFile(filepath.Join(dir, "vitest"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("PATH", frozen)
	resetPlanningSnapshot()
	t.Cleanup(resetPlanningSnapshot)
	// The snapshot is memoized LAZILY, so it is taken at the first read rather
	// than when it is armed. Forcing it here is what puts this test at the
	// state production is in by the time it resolves anything: no production
	// code calls os.Setenv, so the process's start environment is what the
	// first read sees.
	if got := planningEnvValue("PATH"); got != frozen {
		t.Fatalf("the snapshot's PATH is %q, want %q", got, frozen)
	}

	// The ambient PATH now points somewhere else entirely.
	if err := os.Setenv("PATH", ambient); err != nil {
		t.Fatal(err)
	}

	got, err := resolveProgram("vitest")
	if err != nil {
		t.Fatalf("resolveProgram: %v", err)
	}
	if want := filepath.Join(frozen, "vitest"); got != want {
		t.Errorf("resolved %q, want %q: the closure followed the ambient PATH rather than the frozen one", got, want)
	}

	// And an unresolvable program is an ERROR. A plan may not be derived from
	// a program nobody can name: falling back to the bare name would record a
	// closure that resolves to different bytes on every host.
	if _, err := resolveProgram("tb-no-such-program"); err == nil {
		t.Error("an unresolved program was accepted into the closure")
	}
}

// TestThisExecutableIsNotResolvedThroughAnyPath: `testbucket` names THIS
// process — the program that took the snapshot — and asking any PATH for it
// would resolve whatever copy happens to be installed instead.
func TestThisExecutableIsNotResolvedThroughAnyPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	resetPlanningSnapshot()
	t.Cleanup(resetPlanningSnapshot)

	got, err := resolveProgram("testbucket")
	if err != nil {
		t.Fatalf("resolveProgram: %v", err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got != self {
		t.Errorf("resolved %q, want this process's own executable %q", got, self)
	}
}
