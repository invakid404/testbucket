//go:build !unix

package walltime

import (
	"errors"
	"time"
)

// On a platform with no process groups there is nothing to signal and nothing
// to probe, so the drain reports that it could not confirm an empty group
// rather than implying it did (§3.3).

type sigT int

const (
	sigTERM sigT = 15
	sigKILL sigT = 9
)

func groupProbeSupported() bool   { return false }
func groupExists(int) bool        { return false }
func signalGroup(int, sigT) error { return errors.New("process groups unsupported on this platform") }
func isNoSuchProcess(error) bool  { return true }

func monotonicNow() time.Time                  { return time.Now() }
func monotonicSince(t time.Time) time.Duration { return time.Since(t) }
