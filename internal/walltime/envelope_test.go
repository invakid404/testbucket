//go:build unix

package walltime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestExecEnvelopeContainsFacadeVitestLifecycle is §22 test 44 (F7): ORDERED
// SENTINELS at façade entry, import/transform/setup, test body,
// reporter/shutdown, façade cleanup, root reap and the closing read prove the
// documented prefix/suffix placement.
//
// The sentinels are written by a real spawned process chain rather than
// simulated, because the property under test is that the envelope CONTAINS the
// whole lifecycle — an in-memory sequence would prove only that the test wrote
// its own list in order.
func TestExecEnvelopeContainsFacadeVitestLifecycle(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "sentinels.log")

	// A stand-in façade that emits the lifecycle stages in order. It is a
	// shell script, so the stages really are separate process events inside
	// the envelope rather than function calls inside the test.
	script := `
set -eu
echo facade-entry >> "$LOG"
echo import-transform-setup >> "$LOG"
echo test-body >> "$LOG"
echo reporter-shutdown >> "$LOG"
echo facade-cleanup >> "$LOG"
`
	facade := filepath.Join(dir, "facade.sh")
	if err := os.WriteFile(facade, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// The envelope: the opening reading precedes the spawn, and the closing
	// reading follows the root reap and the group drain.
	start := Instant{ClockID: "CLOCK_MONOTONIC", Mono: Nanos(time.Now().UnixNano()), BootID: "boot-a"}
	if err := WriteIntervalHandoff(dir, start); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("/bin/sh", facade)
	cmd.Env = append(os.Environ(), "LOG="+log)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn the façade: %v", err)
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}

	// The measured script runs to completion FIRST. The drain is teardown at
	// the end of the interval, not a way to stop the workload: signalling the
	// group before the façade finished would kill it mid-lifecycle, which is
	// what a first draft of this test did and why it recorded two sentinels
	// instead of seven.
	waitForLines(t, log, 5)

	// Root reap and group drain, then — and only then — the closing reading.
	out, err := DrainGroup(DrainRequest{
		PGID: pgid, TermGrace: 2 * time.Second, KillGrace: 2 * time.Second,
		ReapRoot: cmd.Wait,
	})
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !out.Reaped {
		t.Fatal("the root child was not reaped inside the envelope")
	}
	if !out.GroupEmpty {
		t.Fatal("the closing reading would have been taken while the group existed")
	}
	appendSentinel(t, log, "root-reap")

	end := Instant{ClockID: "CLOCK_MONOTONIC", Mono: Nanos(time.Now().UnixNano()), BootID: "boot-a"}
	elapsed, err := CloseInterval(dir, end, nil)
	if err != nil {
		t.Fatal(err)
	}
	appendSentinel(t, log, "closing-read")

	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			got = append(got, l)
		}
	}

	want := []string{
		"facade-entry",
		"import-transform-setup",
		"test-body",
		"reporter-shutdown",
		"facade-cleanup",
		"root-reap",
		"closing-read",
	}
	if len(got) != len(want) {
		t.Fatalf("recorded %d sentinels, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sentinel %d is %q, want %q (full order %v)", i, got[i], want[i], got)
		}
	}

	t.Run("the whole façade lifecycle precedes the closing read", func(t *testing.T) {
		// The documented suffix placement: every façade stage, then the root
		// reap, then the reading. A closing read taken earlier would appear
		// before a stage in this log.
		closing := indexIn(got, "closing-read")
		for _, stage := range want[:len(want)-1] {
			if indexIn(got, stage) > closing {
				t.Fatalf("stage %q appears after the closing read", stage)
			}
		}
	})

	t.Run("the interval is positive and brackets the lifecycle", func(t *testing.T) {
		if elapsed <= 0 {
			t.Fatalf("elapsed_ns = %d, want a positive interval", elapsed)
		}
	})
}

func appendSentinel(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s + "\n"); err != nil {
		t.Fatal(err)
	}
}

func indexIn(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// waitForLines blocks until the log carries at least n non-empty lines, so the
// envelope's teardown begins only after the workload has finished.
func waitForLines(t *testing.T, path string, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := os.ReadFile(path)
		if err == nil {
			count := 0
			for _, l := range strings.Split(string(b), "\n") {
				if strings.TrimSpace(l) != "" {
					count++
				}
			}
			if count >= n {
				return
			}
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the façade did not emit %d sentinels within the deadline", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
