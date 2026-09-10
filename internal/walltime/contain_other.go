package walltime

// PROCESS-GROUP CONTAINMENT IS THE ONLY CONTAINMENT, on every platform.
//
// This file used to be `//go:build !linux`, holding the fallbacks a host with
// no delegated cgroup-v2 subtree used. Its Linux counterpart — newContainment,
// attachCgroup2, retainLevelMembershipFacts, processStartID — had already been
// deleted with the cgroup/credential proof machinery, so `GOOS=linux` could not
// build the package at all. The build tag is gone with the second
// implementation it was there to select.
func newContainment(name string, parent *ContainmentIdentity) (Containment, error) {
	return newProcessGroupContainment(name, "process-group containment")
}

// processStartID has no portable equivalent, and nothing reads it as an
// identity any more: PID reuse mattered to a verifier adjudicating a
// containment it could not see, and that verifier is gone.
func processStartID(int) string { return "" }

// prepareEvidenceDir has no second account to prepare for. The delegated
// evidence directory went with the credential separation.
func prepareEvidenceDir(string) error { return nil }
