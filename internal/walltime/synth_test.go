package walltime

// The synthetic scorable record set lived here.
//
// It existed because a scorable run needed cgroup-v2 Linux, independent
// observer processes, a signed hash chain and a Stage-1/Stage-2 binding — none
// of which a developer host could produce, so the fixture had to be built by
// hand and kept in step with the verifier by hand. The practical verifier
// scores what `Exec` writes on any host, so the tests that needed a synthetic
// run now use a real one and the fixture has no remaining caller.
