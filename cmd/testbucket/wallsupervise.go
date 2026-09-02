package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/invakid404/testbucket/internal/walltime"
)

// runWallSupervise is the PRIVILEGED half of a scored measurement.
//
// It exists because of a fact about the runner, not a preference. The wrapper
// and the measured workload used to share one credential, so every capability
// the evidence rests on — creating a containment whose `cgroup.procs` the
// workload cannot write, and countersigning the producer keys that sign the
// records — was a capability the workload also held. No arrangement of files
// or environment variables closes that; the boundary has to be a different
// credential holding the capability, and this process is it.
//
// On a GitHub-hosted Linux runner it is started from a setup step under
// `sudo`, before the measured step, with the run key on a file descriptor and
// the measured workload's uid named. The measured step then runs as that
// distinct, non-sudo uid: it can ask this process for a containment or a
// producer registration, and it cannot perform either itself.
func runWallSupervise(args []string) error {
	fs := flag.NewFlagSet("wall supervise", flag.ExitOnError)
	socket := fs.String("socket", "", "unix socket to serve privileged requests on (required)")
	root := fs.String("cgroup-root", "", "the delegated cgroup-v2 subtree this supervisor owns (required)")
	workloadUID := fs.Int("workload-uid", 0, "the uid the MEASURED WORKLOAD runs as. It must differ from this process's own: a supervisor sharing the workload's credential enforces nothing")
	wrapperGID := fs.Int("wrapper-gid", 0, "group allowed to open the socket. The measured workload must not be in it")
	keyFD := fs.Int("key-fd", 3, "file descriptor carrying the run key. A key on a command line is a key in the process table")
	campaign := fs.String("campaign", "", "the campaign this supervisor serves")
	runID := fs.String("run", "", "the workflow run this supervisor serves")
	bucket := fs.String("bucket", "", "the bucket this supervisor serves")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*socket) == "" || strings.TrimSpace(*root) == "" {
		return fmt.Errorf("--socket and --cgroup-root are both required")
	}
	// ONE MEASUREMENT. A supervisor that served any run would let a second
	// measurement borrow the first one's authority, and the authority is the
	// whole point of the process.
	if strings.TrimSpace(*runID) == "" {
		return fmt.Errorf("--run is required: a supervisor serves exactly one measurement, so that no other can borrow its authority")
	}
	key, err := walltime.ReadKeyFD(*keyFD)
	if err != nil {
		return fmt.Errorf("read the run key from fd %d: %w", *keyFD, err)
	}
	fmt.Fprintf(os.Stderr,
		"testbucket wall: supervising run %s/%s as uid %d for workload uid %d on %s\n",
		*campaign, *runID, os.Getuid(), *workloadUID, *socket)
	return walltime.RunSupervisor(walltime.SupervisorOptions{
		Socket: *socket, Root: *root,
		Run:         walltime.RunIdentity{CampaignID: *campaign, RunID: *runID, BucketID: *bucket},
		RunKey:      key,
		WorkloadUID: *workloadUID,
		WrapperGID:  *wrapperGID,
	})
}
