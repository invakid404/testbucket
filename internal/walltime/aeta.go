package walltime

// THE AETA REGISTRY PROOF LIVED HERE, and this file is what is left of it.
//
// It published `tb.walltime.aeta-registry/v1` and exported ComponentClass,
// Component, AetaInputs, InstantiatedComponent and AetaInstance — a sealed
// component template, frozen by a Stage-1 receipt and instantiated per bucket
// against a Stage-2 one, so a pre-action forecast could be re-derived from
// documents nobody could have written afterwards. The two schemas carried
// `registry_digest` and `stage2_digest`, and its opening line said a two-stage
// freeze was "the whole design".
//
// The component map eliminates the Aeta registry proof with the rest of the
// authority model. Nothing outside this file ever referenced any of it, so it
// was dead and exported at once: an internal caller could still construct a
// registry-shaped document, and a reader of the package could still conclude
// that a two-stage freeze was how forecasts are bound.
//
// A_eta itself is retained and is not this. It is contract §0.9's four-term
// objective over the ORDINARY history the §15.1a ring keeps — core.AEtaNs
// evaluates it, core.AllocateWall optimizes it, and AetaSample in gates.go
// pairs one bucket's forecast with what it observed. None of that needs a
// registry, a seal, or a receipt to cite.
//
// The file stays because the component map lists it SIMPLIFY, and a SIMPLIFY
// path is reduced rather than deleted. There is nothing left to reduce.
