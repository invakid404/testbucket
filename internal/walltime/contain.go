package walltime

// THE CONTAINMENT ABSTRACTION LIVED HERE, and this file is what is left of it.
//
// It declared two primitives — PrimitiveCgroup2, a dedicated cgroup-v2 subtree
// that alone could delimit a SCORED lifecycle, and PrimitiveProcessGroup, the
// unscored fallback — plus the exported `Containment` interface (Identity,
// Admit, Procs, Signal, Destroy) and the `NewContainment` factory that chose
// between them.
//
// The component map classifies that evidence/control schema REMOVE and says
// the practical runner needs an internal process-group controller rather than
// an evidence schema. It has one: exec.go's runOwnedChild puts the child in
// its own process group at spawn and hands the group to DrainGroup, which
// performs §3.3's three steps — reap the root, signal the group by negative
// PGID with bounded escalation, drain it. No caller of NewContainment ever
// existed on that path.
//
// What the interface promised beyond that was the proof model: admission
// before the child can run, membership snapshots read from the kernel, whole-
// container signalling, verified emptiness, and destroy. Those answer "can the
// measured workload have moved itself out of what contains it", which §3's
// trusted-CI boundary does not ask.
//
// The file stays because the component map lists it SIMPLIFY, and a SIMPLIFY
// path is reduced rather than deleted. There is nothing left to reduce.
