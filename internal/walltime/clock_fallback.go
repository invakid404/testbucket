package walltime

import "time"

// procEpoch anchors the process-relative fallback clock. Go's time.Since reads
// the runtime's monotonic clock, so the DURATION between two fallback readings
// in one process is sound; only the epoch is private, which is why a record
// carrying it is never scored.
var procEpoch = time.Now()

func processRelativeNanos() int64 { return int64(time.Since(procEpoch)) }
