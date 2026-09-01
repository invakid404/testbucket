package walltime

import (
	"fmt"
	"sort"
	"strings"
)

// PlanImplementationVersion is the version of the parser, policy, renderer and
// digest-algorithm implementations THIS BUILD contains.
const PlanImplementationVersion = "tb.planbind/v2"

// The complete inventory of parser and policy implementations the frozen
// contract requires a planning-input bundle to bind.
//
// Every stage the contract names is here, and it names them explicitly:
// discovery, runnable parsing, LOCK, stale policy, UNIT EXPANSION, suffix
// collision, COVERAGE, selection and rendering. The previous list omitted the
// lock parser, unit expansion and coverage outright, so a bundle could satisfy
// validation while binding nothing at all for three of the stages that decide
// the plan.
const (
	ParserDiscovery      = "vitest-discovery-parser"
	ParserRunnable       = "vitest-runnable-parser"
	ParserLock           = "lock-closure-parser"
	ParserStale          = "staleness-policy"
	ParserUnitExpansion  = "unit-expansion-policy"
	ParserSuffixAtomiser = "suffix-collision-atomiser"
	ParserCoverage       = "coverage-policy"
	ParserSelection      = "kk-partitioner"
	ParserRenderer       = "vitest-renderer"
	ParserStoreSchema    = "store-schema"
)

// RequiredParserIdentities is that inventory, in the order the contract lists
// the stages.
var RequiredParserIdentities = []string{
	ParserDiscovery, ParserRunnable, ParserLock, ParserStale,
	ParserUnitExpansion, ParserSuffixAtomiser, ParserCoverage,
	ParserSelection, ParserRenderer, ParserStoreSchema,
}

// ImplementedParserIdentities is what the implementations that WILL EXECUTE
// actually are.
//
// The digest is the SHA-256 of the running binary. That is the only
// implementation identity available at run time that genuinely covers the code
// about to run: the previous digests were SHA-256 of label strings like
// `"vitest-discovery-parser/testbucket/v0.3"`, which identify the NAME of a
// parser and say nothing whatever about its bytes. A caller could invent every
// one of them and the planner would execute its own implementations regardless,
// so the bundle bound a claim and the receipt echoed it.
//
// It is the same construction the lock-closure parser identity already uses,
// and the same identity Stage 1 binds for the verifier binary — so a reader who
// can reproduce the binary can reproduce every parser identity in the bundle.
func ImplementedParserIdentities() []ParserIdentity {
	self := SelfDigest()
	out := make([]ParserIdentity, 0, len(RequiredParserIdentities))
	for _, name := range RequiredParserIdentities {
		out = append(out, ParserIdentity{Name: name, Version: PlanImplementationVersion, Digest: self})
	}
	return out
}

// ImplementedFullPlanAlgorithm and ImplementedSemanticPlanAlgorithm are the
// digest algorithms this build implements. `Implementation` is the binary
// digest for the same reason: two implementations of one named algorithm can
// disagree, and a version string cannot tell them apart.
func ImplementedFullPlanAlgorithm() AlgorithmIdentity {
	return AlgorithmIdentity{Name: FullPlanDigestAlgorithm, Canonicalizer: CanonAlgorithm, Implementation: string(SelfDigest())}
}

func ImplementedSemanticPlanAlgorithm() AlgorithmIdentity {
	return AlgorithmIdentity{Name: SemanticPlanDigestAlgorithm, Canonicalizer: CanonAlgorithm, Implementation: string(SelfDigest())}
}

// CheckPlanImplementationIdentities compares what a bundle CLAIMS about the
// parsers, policies and digest algorithms with what this build will actually
// run.
//
// This is the check that turns the inventory from a description into an
// identity. Both the planner and the independent replay call it before they
// execute anything: a bundle whose claims do not match the implementations
// about to run is a bundle describing a different derivation, and running it
// anyway would produce a plan under identities nobody can check.
func CheckPlanImplementationIdentities(b PlanningInputBundle) error {
	claimed := map[string]ParserIdentity{}
	for _, p := range b.Parsers {
		if _, dup := claimed[p.Name]; dup {
			return fmt.Errorf("planning-input bundle: parser %q is bound twice", p.Name)
		}
		claimed[p.Name] = p
	}
	var problems []string
	for _, want := range ImplementedParserIdentities() {
		got, ok := claimed[want.Name]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s is not bound at all", want.Name))
			continue
		}
		if got != want {
			problems = append(problems, fmt.Sprintf("%s is bound as %s/%s but the implementation that will run is %s/%s",
				want.Name, got.Version, got.Digest, want.Version, want.Digest))
		}
		delete(claimed, want.Name)
	}
	// A bundle may not bind a parser this build does not implement: it would
	// be naming a stage nothing here executes, and nobody could tell whether
	// it ran.
	for _, name := range sortedParserNames(claimed) {
		problems = append(problems, fmt.Sprintf("%s is bound but this build implements no such parser or policy", name))
	}
	for _, a := range []struct {
		what      string
		got, want AlgorithmIdentity
	}{
		{"the full-plan digest algorithm", b.Algorithms.FullPlan, ImplementedFullPlanAlgorithm()},
		{"the semantic-plan digest algorithm", b.Algorithms.SemanticPlan, ImplementedSemanticPlanAlgorithm()},
	} {
		if a.got != a.want {
			problems = append(problems, fmt.Sprintf("%s is bound as %s/%s/%s but the implementation that will run is %s/%s/%s",
				a.what, a.got.Name, a.got.Canonicalizer, a.got.Implementation,
				a.want.Name, a.want.Canonicalizer, a.want.Implementation))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("planning-input bundle: the bound parser, policy and algorithm identities are not the implementations that would execute:\n  %s",
			strings.Join(problems, "\n  "))
	}
	return nil
}

func sortedParserNames(m map[string]ParserIdentity) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
