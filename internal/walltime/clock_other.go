//go:build !linux

package walltime

import "time"

// readMonotonic on a platform without raw CLOCK_MONOTONIC access falls back to
// the host's realtime clock. That is a deliberate trade: realtime shares an
// epoch across processes, so a developer run still exercises the real endpoint
// ordering, partition and independence checks rather than only the parts that
// fit inside one process — while remaining unscorable, because an NTP step can
// move it and the verifier refuses any clock but CLOCK_MONOTONIC.
func readMonotonic() (int64, string) {
	return time.Now().UnixNano(), ClockRealtimeUnscored
}

// platformBootIdentity gives the fallback readings a shared, honestly named
// epoch identity. It is stable across the producers of one run, which is what
// the ordering check needs, and it says in its own name that it is not a boot.
func platformBootIdentity() string { return "unscored-host-realtime" }
