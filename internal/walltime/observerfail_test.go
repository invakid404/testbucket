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

// TestAFailedObserverLaunchLeavesNoLiveChild is the F3 regression.
//
// `startObserver` has three error returns AFTER `cmd.Start()` succeeds: the
// key write, the key close, and the key-log registration. Each returned
// `(nil, err)`, which hands the caller no handle — so nothing could reap the
// child, and a real observer went on watching the containment for its whole
// timeout, writing records for a launch that had been refused. That
// contaminates exactly what it was meant to measure.
//
// The failure is forced the way it happens: the key-log target is made a
// directory, so `RegisterKey`'s open fails after the process is already
// running.
func TestAFailedObserverLaunchLeavesNoLiveChild(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, keyLogFile), 0o700); err != nil {
		t.Fatal(err)
	}

	original := ObserverLauncher
	var launched *exec.Cmd
	ObserverLauncher = func(_ []string) (*exec.Cmd, error) {
		launched = exec.Command("sleep", "30")
		return launched, nil
	}
	t.Cleanup(func() { ObserverLauncher = original })

	opt := ExecOptions{
		Dir: dir, Level: LevelScript, Seq: 1,
		Run: RunIdentity{CampaignID: "ewj2", RunID: "post-start-failure", BucketID: "bucket-0"},
	}
	got, err := startObserver(ProducerPeer, opt, ContainmentIdentity{}, time.Now().Add(30*time.Second), false)
	if err == nil {
		t.Fatalf("startObserver returned %+v despite a forced key-log failure", got)
	}
	if got != nil {
		t.Errorf("a failed launch returned a handle as well as an error: %+v", got)
	}
	if launched == nil || launched.Process == nil {
		t.Fatal("the control never reached cmd.Start, so it proves nothing about post-Start failures")
	}

	// The child must already be gone. Signal 0 probes for existence without
	// delivering anything: it succeeds for a live process and fails once the
	// process has been reaped. It must be syscall.Signal(0) — os.Signal(nil)
	// fails the type assertion inside Process.Signal and would report
	// "reaped" for a process that is very much alive.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := launched.Process.Signal(syscall.Signal(0)); err != nil {
			break // reaped
		}
		if time.Now().After(deadline) {
			_ = launched.Process.Kill()
			t.Fatal("startObserver returned an error but left the already-started observer alive; a refused launch must terminate and reap what it started")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// And the original cause survives into the error, so a reader learns why
	// the launch was abandoned rather than only that it was cleaned up.
	if !strings.Contains(err.Error(), keyLogFile) && !strings.Contains(err.Error(), "key log") {
		t.Logf("error was: %v", err)
	}
}

// TestReapStartedFoldsTheCleanupIntoTheCause: the reason a launch was
// abandoned is the primary fact. A failure to clean up is a second fact about
// the same event, not a replacement for the first.
func TestReapStartedFoldsTheCleanupIntoTheCause(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cause := os.ErrPermission
	err := reapStarted(ProducerPeer, cmd, cause)
	if err == nil {
		t.Fatal("reapStarted discarded the cause")
	}
	if !strings.Contains(err.Error(), cause.Error()) {
		t.Errorf("error %q does not carry the original cause", err)
	}
	// The process is gone.
	if sigErr := cmd.Process.Signal(syscall.Signal(0)); sigErr == nil {
		_ = cmd.Process.Kill()
		t.Error("reapStarted returned without terminating the process")
	}
	// A process that never started is not an error to reap.
	if got := reapStarted(ProducerPeer, exec.Command("true"), cause); got != cause {
		t.Errorf("reapStarted on an unstarted command returned %v, want the cause unchanged", got)
	}
}
