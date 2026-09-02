package walltime

import (
	"strings"
	"testing"
)

// TestTheTopologyGrammarIsInjective is the F7 regression.
//
// The file list was joined with commas and split on commas, and a comma is a
// legal character in a path. `suite/a,b.spec.ts` was written as ONE file and
// read back as TWO, so a single-file schedule decoded as a multi-file topology
// and realized a stratum it had never run. A delimiter drawn from the domain
// it delimits cannot be injective over that domain.
func TestTheTopologyGrammarIsInjective(t *testing.T) {
	for _, files := range [][]string{
		{"suite/a,b.spec.ts"},
		{"suite/a.spec.ts", "suite/b.spec.ts"},
		{`suite/quote".spec.ts`, "suite/back\\slash.spec.ts"},
		{"suite/new\nline.spec.ts"},
		{"suite/colon:name.spec.ts"},
		{`suite/[bracket].spec.ts`},
	} {
		entry := TopologyEntry("package", files)
		got, ok := topologyUnitOf(entry)
		if !ok {
			t.Errorf("%q does not decode", entry)
			continue
		}
		if got.kind != "package" {
			t.Errorf("%q decodes kind %q", entry, got.kind)
		}
		if len(got.files) != len(files) {
			t.Errorf("%q decodes %d file(s) %v, not the %d encoded %v", entry, len(got.files), got.files, len(files), files)
			continue
		}
		for i := range files {
			if got.files[i] != files[i] {
				t.Errorf("%q decodes file %d as %q, not %q", entry, i, got.files[i], files[i])
			}
		}
	}

	// AND THE PREDICATE IS TRUTHFUL OVER THAT DOMAIN. One comma-bearing file
	// is one file, however it is spelled.
	one := AblationDerived{Topology: map[string][]string{
		"bucket-0": {TopologyEntry("package", []string{"suite/a,b.spec.ts"})},
	}}
	if problem := one.realizes(StratumWholeFileMultiFile); !strings.Contains(problem, "1 distinct file") {
		t.Errorf("a single comma-bearing filename realized the multi-file stratum: %q", problem)
	}
	two := AblationDerived{Topology: map[string][]string{
		"bucket-0": {
			TopologyEntry("package", []string{"suite/a,b.spec.ts"}),
			TopologyEntry("package", []string{"suite/c,d.spec.ts"}),
		},
	}}
	if problem := two.realizes(StratumWholeFileMultiFile); problem != "" {
		t.Errorf("two comma-bearing filenames did not realize the multi-file stratum: %q", problem)
	}

	// A malformed entry is refused rather than guessed at.
	for _, bad := range []string{"package", "package:", ":[]", `package:not-json`, `package:[]`, `package:[""]`} {
		if _, ok := topologyUnitOf(bad); ok {
			t.Errorf("%q decoded as a topology entry", bad)
		}
	}
}
