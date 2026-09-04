package main

import "github.com/invakid404/testbucket/internal/walltime"

// runWallHold is the INTERNAL barrier an action-owned child waits at while the
// wrapper retains its containment proof. The wrapper starts it by
// re-executing this binary; it is not a command anyone runs by hand.
func runWallHold(args []string) error { return walltime.HoldActionChild(args) }
