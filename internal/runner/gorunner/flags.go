package gorunner

import (
	"fmt"
	"strings"
)

// canonicalFlags renders the flag set the weights are comparable within.
// -timeout is deliberately excluded: it bounds a run, it does not change how
// much work the run does.
func canonicalFlags(race bool, count int) string {
	var sb strings.Builder
	if race {
		sb.WriteString("-race ")
	}
	fmt.Fprintf(&sb, "-count=%d", count)
	return sb.String()
}
