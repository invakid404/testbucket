package walltime

// THE PLATFORM CONTAINMENT FALLBACKS LIVED HERE.
//
// This file held newContainment, attachCgroup2, retainLevelMembershipFacts and
// the delegated evidence-directory helpers behind a `//go:build !linux` tag,
// selecting the unscored process-group fallback on any host with no delegated
// cgroup-v2 subtree. The cgroup implementation it was the alternative to was
// already gone; the abstraction it implemented is gone now too.
//
// One live helper remains, and it is not containment: processStartID is read by
// exec.go and action.go when they record a child's process identity, and by
// procsignal_linux.go when it verifies a pidfd still names the process it was
// opened for. It stays here rather than moving so the platform seam it belongs
// to keeps one home.

// processStartID has no portable equivalent. An empty start identity makes the
// PID a reusable number rather than an identity, which is a stated limitation
// of the record rather than a claim about it.
func processStartID(int) string { return "" }
