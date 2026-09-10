package walltime

// THE PROCESS-GROUP CONTAINMENT WRAPPER LIVED HERE.
//
// `processGroup` implemented the removed Containment interface over a pgid:
// Identity returned a ContainmentIdentity, Admit recorded the child's group,
// Procs reported nothing because a process group cannot be enumerated
// portably, Signal delivered to the negative pgid, and Destroy was a no-op.
// It described itself as the UNSCORED fallback, which is a scoring distinction
// the practical contract does not draw.
//
// The behaviour that mattered is not gone, it is direct: exec.go's
// runOwnedChild starts the child in its own process group and calls
// DrainGroup, which reaps the root, signals the group by negative PGID with
// bounded escalation and drains it — §3.3's three steps, with no abstraction
// between the runner and the group it owns.
//
// The file stays because the component map lists it SIMPLIFY, and a SIMPLIFY
// path is reduced rather than deleted. There is nothing left to reduce.
