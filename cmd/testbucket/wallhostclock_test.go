package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// hostClockIsScorable reports whether THIS machine's clock backend can delimit a
// scored interval.
//
// §13.1 admits CLOCK_MONOTONIC only. On a platform without raw access the
// backend reads host realtime under an honest name, so a measurement made here is
// complete and INELIGIBLE — which is also why a plan-bound `wall verify` exits
// non-zero on it. A black-box test that runs the shipped binary cannot choose its
// clock, so it states the platform truth instead of asserting past it; before
// WT-027 existed these tests asserted eligibility on a clock the backend's own
// documentation calls unscorable.
func hostClockIsScorable(t *testing.T) bool {
	t.Helper()
	return walltime.NewSystemClock().Now().Scorable()
}

// TestPlanBoundVerifyFailsOnAnIdentityVerdict is F24's control.
//
// VerifyDir separates Complete from Eligible, and invocation-count, argv,
// membership and bucket-identity failures are SeverityIneligible. The CLI took
// its exit code from Complete alone, so a complete measurement that ran something
// other than its plan exited 0 — while the verdict printed above it said the
// identity did not match. The verify-wall action relies on that exit code, so a
// required step went green on a plan identity failure, which §17.10 forbids.
//
// The second half is the distinction the fix turns on: a consumer that supplies
// NO plan is not asserting an identity and must not be failed for the absence.
func TestPlanBoundVerifyFailsOnAnIdentityVerdict(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	records := filepath.Join(dir, "records")

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		var out strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
	}
	run("wall", "begin", "--dir", records, "--bucket-id", "bucket-0")
	inner := bin + " wall exec --dir " + records + " --level invocation" +
		" --bucket-id bucket-0 --cwd " + dir + " --selector ./f0.test.ts -- sh -c true"
	run("wall", "exec", "--dir", records, "--level", "script", "--bucket-id", "bucket-0",
		"--cwd", dir, "--", "sh", "-c", inner)
	run("wall", "end", "--dir", records, "--terminal", "passed")

	// A PLAN THAT RENDERS A DIFFERENT INVOCATION. The measurement is complete;
	// it simply did not run what this plan describes.
	plan, _ := writePlanFor(t, dir, "bucket-0", 0, []string{"run", "something-else.test.ts"}, dir)

	verify := exec.Command(bin, "wall", "verify", "--dir", records, "--shard-plan", plan)
	var out strings.Builder
	verify.Stdout, verify.Stderr = &out, &out
	if err := verify.Run(); err == nil {
		t.Fatalf("a plan-bound verification exited 0 over an identity mismatch:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "WT-021") && !strings.Contains(out.String(), "WT-025") {
		t.Errorf("the verdict names no identity finding; the exit code would be reporting something else:\n%s", out.String())
	}

	t.Run("no plan supplied is diagnostic, not a failure", func(t *testing.T) {
		// Complete records and no plan: the absence is a finding, the mode is
		// diagnostic, and a consumer that passed no plan artifact is not failed.
		cmd := exec.Command(bin, "wall", "verify", "--dir", records)
		var out strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("verification with no plan supplied failed: %v\n%s", err, out.String())
		}
	})
}
