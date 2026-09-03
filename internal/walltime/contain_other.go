//go:build !linux

package walltime

import "os"

import (
	"fmt"
	"runtime"
)

// newContainment on a non-Linux host has no delegated cgroup-v2 subtree to
// use, so it returns the unscored process-group fallback. The contract already
// says as much: a different OS needs its own predeclared containment primitive
// before it can score, and until one exists the honest answer is INELIGIBLE.
func newContainment(name string, parent *ContainmentIdentity) (Containment, error) {
	return newProcessGroupContainment(name, "no cgroup-v2 containment on "+runtime.GOOS)
}

// processStartID has no portable /proc equivalent here. An empty start
// identity makes the PID a reusable number rather than an identity, which the
// verifier already treats as unscorable.
func processStartID(int) string { return "" }

// startIDsAvailable says whether THIS PLATFORM can identify a process at all.
//
// It is the difference between a handle that LOST its identity and a host
// where no identity was ever obtainable, and the two call for opposite
// answers. Where identities exist, a handle without one is holding a bare
// number it cannot justify acting on, and the wrapper refuses to act. Where
// none exists anywhere, refusing would disable the observation entirely on
// every host of this kind rather than making anything safer, so existence
// remains the strongest available claim — which is exactly why a containment
// here is reported unscorable.
const startIDsAvailable = false

// attachCgroup2 cannot exist off Linux; saying so is better than pretending a
// directory is a containment.
func attachCgroup2(ident ContainmentIdentity) (Containment, error) {
	return nil, fmt.Errorf("walltime: cgroup-v2 containment is Linux-only (host is %s)", runtime.GOOS)
}

// retainLevelMembershipFacts has no cgroup facts to re-read off Linux.
func retainLevelMembershipFacts(Containment, Level) {}

// evidenceDirDelegation has no second account to resolve off Linux; the shared
// decision lives in contain.go so it is exercised on every host.
func evidenceDirDelegation() (int, os.FileMode) { return evidenceDirDelegationFor(nil) }

// prepareEvidenceDir has no second account to prepare for off Linux.
func prepareEvidenceDir(string) error { return nil }
