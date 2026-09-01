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
