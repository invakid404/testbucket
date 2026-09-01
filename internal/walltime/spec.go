package walltime

import (
	"encoding/json"
	"fmt"
	"os"
)

// InvocationSpec is the plan-bound description of one rendered invocation, and
// it is how the generated script tells the wrapper what to run.
//
// It is a FILE, not a command line, on purpose. The argv is serialised, so no
// shell gets to re-split it: a spec file name containing a space, a quote or a
// leading dash cannot turn into different work between the plan that was
// digested and the process that ran. The wrapper execs Argv directly and
// records its digest, so "this V measured that invocation" is checkable rather
// than assumed.
type InvocationSpec struct {
	Seq        int               `json:"seq"`
	Argv       []string          `json:"argv"`
	Cwd        string            `json:"cwd"`
	Selector   []string          `json:"selector,omitempty"`
	Desc       string            `json:"desc,omitempty"`
	UnitDigest Digest            `json:"unit_digest,omitempty"`
	AtomDigest Digest            `json:"atom_digest,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}

// MarshalSpec renders a spec for embedding in a generated script. It is
// compact and deterministic so the script bytes — and therefore the plan
// digest — are a function of the plan alone.
func MarshalSpec(s InvocationSpec) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("walltime: marshal invocation spec: %w", err)
	}
	return string(b), nil
}

// InvocationManifestKind identifies the per-bucket document that says what
// each rendered invocation was PLANNED to be.
const InvocationManifestKind = "tb.walltime.invocations/v1"

// InvocationIdentity is one rendered invocation as the authorised plan
// rendered it.
type InvocationIdentity struct {
	Seq            int      `json:"seq"`
	ArgvDigest     Digest   `json:"argv_digest"`
	Cwd            string   `json:"cwd"`
	SelectorDigest Digest   `json:"selector_digest"`
	UnitDigest     Digest   `json:"unit_digest"`
	AtomDigest     Digest   `json:"atom_digest"`
	Units          []string `json:"units"`
	Atoms          []string `json:"atoms,omitempty"`
}

// InvocationManifest is what the verifier compares each measured invocation
// record against.
//
// Without it a wrapper's Spec travels BESIDE the plan rather than being
// checked against it: a verifier could confirm that a record names some argv
// and some selector, but not that they are the ones the authorised plan
// rendered — and for two legal name slices of one file, "some selector" is
// exactly the difference that matters.
type InvocationManifest struct {
	Kind        string               `json:"kind"`
	Stage2      Digest               `json:"stage2_digest"`
	BucketIndex int                  `json:"bucket"`
	BucketName  string               `json:"bucket_name"`
	Invocations []InvocationIdentity `json:"invocations"`
}

// Find returns the planned identity of one invocation.
func (m InvocationManifest) Find(seq int) (InvocationIdentity, bool) {
	for _, inv := range m.Invocations {
		if inv.Seq == seq {
			return inv, true
		}
	}
	return InvocationIdentity{}, false
}

// Compare checks a measured spec against the planned identity and reports
// every disagreement.
func (i InvocationIdentity) Compare(spec SpecIdentity) []string {
	var problems []string
	if spec.ArgvDigest != i.ArgvDigest {
		problems = append(problems, fmt.Sprintf("argv digest %s, planned %s", spec.ArgvDigest, i.ArgvDigest))
	}
	if spec.Cwd != i.Cwd {
		problems = append(problems, fmt.Sprintf("cwd %q, planned %q", spec.Cwd, i.Cwd))
	}
	if spec.SelectorDigest != i.SelectorDigest {
		problems = append(problems, fmt.Sprintf("selector digest %s, planned %s", spec.SelectorDigest, i.SelectorDigest))
	}
	if spec.UnitDigest != i.UnitDigest {
		problems = append(problems, fmt.Sprintf("unit membership digest %s, planned %s", spec.UnitDigest, i.UnitDigest))
	}
	if spec.AtomDigest != i.AtomDigest {
		problems = append(problems, fmt.Sprintf("atom digest %s, planned %s", spec.AtomDigest, i.AtomDigest))
	}
	return problems
}

// LoadInvocationSpec reads a spec written by the generated script.
func LoadInvocationSpec(path string) (*InvocationSpec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("walltime: read invocation spec: %w", err)
	}
	var s InvocationSpec
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("walltime: invocation spec %s: %w", path, err)
	}
	if len(s.Argv) == 0 {
		return nil, fmt.Errorf("walltime: invocation spec %s has no argv", path)
	}
	return &s, nil
}
