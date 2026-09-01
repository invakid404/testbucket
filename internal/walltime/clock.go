package walltime

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Clock identities. The verifier scores a record only when its clock identity
// is ClockMonotonic: everything else is a diagnostic reading that cannot be
// compared across the three producers (physical wrapper, containment peer,
// trace collector), because they are separate processes.
const (
	// ClockMonotonic is a raw clock_gettime(CLOCK_MONOTONIC) read. Its epoch is
	// the boot, so two processes on one host read the same timeline.
	ClockMonotonic = "CLOCK_MONOTONIC"
	// ClockRealtimeUnscored is the fallback on a platform with no raw
	// CLOCK_MONOTONIC access: the host's realtime clock. It shares an epoch
	// across processes, so ordering and partitioning can still be CHECKED in
	// development — but it is not monotonic (an NTP step moves it), so a
	// record carrying it is never SCORED. A ruler that can be edited mid-run
	// is not a measurement.
	ClockRealtimeUnscored = "HOST_REALTIME_UNSCORED"
	// ClockProcessRelative is the last-resort fallback: a duration this
	// process can trust and nothing else can compare.
	ClockProcessRelative = "GO_MONOTONIC_PROCESS_RELATIVE"
)

// Instant is one fresh clock reading. Every endpoint in this package is an
// Instant taken by its own producer: a peer never copies the physical
// wrapper's, and the trace never copies the peer's.
type Instant struct {
	// ClockID names the clock the reading came from.
	ClockID string `json:"clock_id"`
	// Mono is the monotonic reading in nanoseconds. It is a Nanos, which
	// serialises as a STRING: a nanosecond count outgrows the exact float64
	// range that canonical JSON allows, and a digest must not depend on which
	// side of 2^53 a reading landed.
	Mono Nanos `json:"mono_ns"`
	// Realtime brackets the monotonic read for the API identity check. It is a
	// string, not a number, because a nanosecond epoch count is larger than an
	// exact float64 and would make the canonical digest depend on rounding.
	Realtime string `json:"realtime"`
	// BootID identifies the monotonic epoch. Two readings with different boot
	// identities are on different timelines and are never compared.
	BootID string `json:"boot_id"`
}

// Scorable reports whether this reading may delimit a scored interval.
func (i Instant) Scorable() bool { return i.ClockID == ClockMonotonic && i.BootID != "" }

// Clock reads fresh monotonic instants. It is an interface so that a test can
// replay a deterministic timeline; production always uses SystemClock.
type Clock interface {
	Now() Instant
}

// SystemClock is the process's real clock. Each Now is an independent syscall:
// no memoisation, no interpolation, no shared sample between two callers.
type SystemClock struct {
	// BootID is the monotonic epoch identity, resolved once at construction.
	BootID string
}

// NewSystemClock resolves the boot/host epoch identity and returns the clock.
// An unresolvable identity is not fatal here — it produces a clock whose
// readings are not scorable, and the verifier is what refuses them, so a
// diagnostic run still records everything it saw.
func NewSystemClock() SystemClock { return SystemClock{BootID: bootIdentity()} }

// Now takes one fresh reading, bracketed by realtime samples as the contract
// requires for the GitHub step identity check.
func (c SystemClock) Now() Instant {
	before := time.Now().UTC()
	ns, id := readMonotonic()
	mono := Nanos(ns)
	after := time.Now().UTC()
	return Instant{
		ClockID:  id,
		Mono:     mono,
		Realtime: before.Format(time.RFC3339Nano) + "/" + after.Format(time.RFC3339Nano),
		BootID:   c.BootID,
	}
}

// RealtimeBracket splits an Instant's bracketing realtime samples back out for
// the identity check against the GitHub step attempt.
func (i Instant) RealtimeBracket() (before, after time.Time, err error) {
	lo, hi, ok := strings.Cut(i.Realtime, "/")
	if !ok {
		return time.Time{}, time.Time{}, fmt.Errorf("realtime %q is not a before/after bracket", i.Realtime)
	}
	before, err = time.Parse(time.RFC3339Nano, lo)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("realtime bracket start %q: %w", lo, err)
	}
	after, err = time.Parse(time.RFC3339Nano, hi)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("realtime bracket end %q: %w", hi, err)
	}
	return before, after, nil
}

// bootIdentity names the monotonic epoch. On Linux it is the kernel's boot_id,
// which changes on every reboot and so cannot make two boots look like one
// timeline. Elsewhere it is whatever the platform can prove about its boot, or
// empty — and an empty identity is itself unscorable.
func bootIdentity() string {
	if b, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
		return strings.TrimSpace(string(b))
	}
	return platformBootIdentity()
}
