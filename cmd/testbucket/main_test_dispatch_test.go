package main

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/invakid404/testbucket/internal/walltime"
)

// The independent observers are separate PROCESSES by design, and the command
// under test launches them by re-executing itself. A test binary is not the
// CLI, so it re-executes ITSELF and dispatches here — which is the same shape
// the walltime package's own TestMain uses, and it keeps the command's real
// launch path in the test rather than stubbing it away.
const cliObserverEnv = "TB_TESTBUCKET_CLI_TEST_OBSERVER"

func TestMain(m *testing.M) {
	if os.Getenv(cliObserverEnv) != "" {
		if err := runWallObserve(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	original := walltime.ObserverLauncher
	walltime.ObserverLauncher = func(args []string) (*exec.Cmd, error) {
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(self, args...)
		cmd.Env = append(os.Environ(), cliObserverEnv+"=1")
		return cmd, nil
	}
	code := m.Run()
	walltime.ObserverLauncher = original
	os.Exit(code)
}
