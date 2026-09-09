//go:build !unix

package walltime

import "os/exec"

// On a platform without process groups there is nothing to own and nothing to
// signal; the drain reports that it could not confirm an empty group (§3.3).

func ownProcessGroup(*exec.Cmd) {}

func childProcessGroup(*exec.Cmd) (int, error) { return 0, errNoChildProcess }
