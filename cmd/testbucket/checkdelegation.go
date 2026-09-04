package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/invakid404/testbucket/internal/walltime"
)

// runWallCheckDelegation verifies, from the SETUP step, that the delegated
// cgroup-v2 subtree can actually contain a measured action.
//
// The published setup created `/sys/fs/cgroup/testbucket`, gave it to the
// runner user and stopped. Ownership of the destination is not what cgroup-v2
// migration needs: it needs the COMMON ANCESTOR of the mover's cgroup and the
// destination, and for a root beside the runner's own cgroup that ancestor is
// the root cgroup, owned by root. Every measured action therefore failed at
// its first migration with a bare permission denied, and the operator had no
// way to find that out except by running one.
//
// So the capability is checkable where it is established. This exits non-zero
// with the reason and the remedy, before any envelope opens.
func runWallCheckDelegation(args []string) error {
	fs := flag.NewFlagSet("wall check-delegation", flag.ExitOnError)
	root := fs.String("root", os.Getenv("TB_WALL_CGROUP_ROOT"), "the delegated cgroup-v2 subtree to check (defaults to TB_WALL_CGROUP_ROOT)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*root) == "" {
		return fmt.Errorf("wall check-delegation needs --root (or TB_WALL_CGROUP_ROOT): there is no delegated subtree to check, so no run can be scored on cgroup containment")
	}
	if err := walltime.CheckCgroupDelegation(*root); err != nil {
		return fmt.Errorf("the cgroup-v2 delegation is not usable: %w", err)
	}
	fmt.Fprintf(os.Stdout, "%s is a usable delegated cgroup-v2 subtree: this credential can create containments under it and migrate into them\n", *root)
	return nil
}
