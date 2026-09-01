//go:build !linux

package walltime

import (
	"fmt"
	"runtime"
)

// newContainment on a non-Linux host has no delegated cgroup-v2 subtree to
// use, so it returns the unscored process-group fallback. The contract already
// says as much: a different OS needs its own predeclared containment primitive
// before it can score, and until one exists the honest answer is INELIGIBLE.
func newContainment(name string) (Containment, error) {
	return newProcessGroupContainment(name, "no cgroup-v2 containment on "+runtime.GOOS)
}

// processStartID has no portable /proc equivalent here. An empty start
// identity makes the PID a reusable number rather than an identity, which the
// verifier already treats as unscorable.
func processStartID(int) string { return "" }

// attachCgroup2 cannot exist off Linux; saying so is better than pretending a
// directory is a containment.
func attachCgroup2(ident ContainmentIdentity) (Containment, error) {
	return nil, fmt.Errorf("walltime: cgroup-v2 containment is Linux-only (host is %s)", runtime.GOOS)
}
