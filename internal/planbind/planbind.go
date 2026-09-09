// Package planbind is the bridge between the frozen two-stage delivery
// protocol (internal/walltime) and the planner (internal/core).
//
// It does two things and nothing else:
//
//   - ACQUIRE freezes a planning-input bundle: the canonical instant, the raw
//     discovery and runnable-listing bytes, the store bytes, the acquisition
//     closure, and the parser and algorithm identities. This is the only place
//     a live subprocess is allowed to be read.
//   - PLAN replays that bundle through the real planner and emits the Stage-2
//     derived-plan receipt: both plan digests plus the atom, topology,
//     membership, invocation, script and matrix digests.
//
// The separation is the whole design. After ACQUIRE, planning is a pure
// function of recorded bytes: no clock, no discovery, no listing, no
// environment lookup. Replaying the same bundle must therefore produce the
// same digests, and an independent verifier proves it by doing exactly that.
//
// It lives outside internal/walltime so that package stays free of any
// planner or adapter dependency, and outside internal/core so the neutral
// core/adapter seam is untouched.
package planbind

import (
	"fmt"
	"sort"
	"strings"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// ParserVersion identifies the parser and policy implementations this build
// binds into a bundle. It changes when their behaviour changes, because a
// bundle replayed by a different parser is not the same plan replayed.
const ParserVersion = "testbucket/v0.3"

// SemanticPlan is the projection the semantic digest is taken over: what
// actually runs, and nothing about how it was summarised. Two plans that
// differ only in a human counter share it; two plans that would run one
// different test never do.
type SemanticPlan struct {
	K       int              `json:"k"`
	Buckets []SemanticBucket `json:"buckets"`
}

// SemanticBucket is one lane's schedulable content.
type SemanticBucket struct {
	Index       int                 `json:"bucket"`
	NeedsNode   bool                `json:"needs_node"`
	Units       []SemanticUnit      `json:"units"`
	Invocations []runner.Invocation `json:"invocations"`
	Script      string              `json:"script"`
}

// SemanticUnit is one scheduled unit's identity, without its estimate: a
// changed weight is a different forecast, not different work.
type SemanticUnit struct {
	ID       string      `json:"id"`
	Kind     runner.Kind `json:"kind"`
	Packages []string    `json:"packages"`
	Run      []string    `json:"run,omitempty"`
}

// SemanticProjection extracts the semantic plan from a plan document.
func SemanticProjection(doc *core.PlanDocument) SemanticPlan {
	out := SemanticPlan{K: doc.K}
	for _, b := range doc.Buckets {
		sb := SemanticBucket{Index: b.Index, NeedsNode: b.NeedsNode, Invocations: b.Invocations, Script: b.Script}
		for _, u := range b.Units {
			sb.Units = append(sb.Units, SemanticUnit{ID: u.ID, Kind: u.Kind, Packages: u.Packages, Run: u.Run})
		}
		out.Buckets = append(out.Buckets, sb)
	}
	return out
}

// atomProjection is the suffix-collision atom closure: which targets must ride
// together. It is digested separately because an atom split is terminal, and a
// terminal condition deserves its own identity.
func atomProjection(live []runner.LivePackage) map[string][]string {
	out := map[string][]string{}
	for _, p := range live {
		key := p.AtomKey()
		if key == "" {
			continue
		}
		out[key] = append(out[key], p.ID)
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// membershipProjection is which UNITS each rendered invocation covers — the
// immutable membership Pcheck projects over.
//
// It reads Invocation.Units, not the description. Two legal name slices of one
// file have the same description and different units, so a membership digest
// taken over descriptions cannot tell them apart — which is exactly the atom
// and slice identity the contract makes terminal.
func membershipProjection(doc *core.PlanDocument) map[string][]string {
	out := map[string][]string{}
	for _, b := range doc.Buckets {
		for i, inv := range b.Invocations {
			key := fmt.Sprintf("bucket-%d/inv-%d", b.Index, i)
			out[key] = append([]string(nil), inv.Units...)
			sort.Strings(out[key])
		}
	}
	return out
}

// InvocationManifestFor is the per-bucket document the verifier compares each
// physical invocation record against.
//
// Without it the wrapper's Spec is an assertion travelling beside the plan
// rather than a claim checked against it: the verifier could confirm that a
// record names SOME argv and selector, but not that they are the ones the
// authorised plan rendered.
func InvocationManifestFor(doc *core.PlanDocument, bucket int, stage2 walltime.Digest) (*walltime.InvocationManifest, error) {
	for _, b := range doc.Buckets {
		if b.Index != bucket {
			continue
		}
		m := &walltime.InvocationManifest{
			Kind: walltime.InvocationManifestKind, Stage2: stage2,
			BucketIndex: b.Index, BucketName: b.Name,
		}
		for i, inv := range b.Invocations {
			units := append([]string(nil), inv.Units...)
			sort.Strings(units)
			atoms := append([]string(nil), inv.Atoms...)
			sort.Strings(atoms)
			m.Invocations = append(m.Invocations, walltime.InvocationIdentity{
				Seq:            i,
				ArgvDigest:     walltime.DigestJSONOrEmpty(inv.Args),
				Cwd:            inv.Dir,
				SelectorDigest: walltime.DigestJSONOrEmpty(inv.Selector),
				UnitDigest:     walltime.DigestJSONOrEmpty(units),
				AtomDigest:     walltime.DigestJSONOrEmpty(atoms),
				Units:          units,
				Atoms:          atoms,
			})
		}
		return m, nil
	}
	return nil, fmt.Errorf("planbind: the plan has no bucket %d", bucket)
}

func invocationProjection(doc *core.PlanDocument) [][]runner.Invocation {
	out := make([][]runner.Invocation, 0, len(doc.Buckets))
	for _, b := range doc.Buckets {
		out = append(out, b.Invocations)
	}
	return out
}

func scriptBytes(doc *core.PlanDocument) string {
	var sb strings.Builder
	for _, b := range doc.Buckets {
		fmt.Fprintf(&sb, "# bucket %d\n%s\n", b.Index, b.Script)
	}
	return sb.String()
}

func countInvocations(doc *core.PlanDocument) int {
	n := 0
	for _, b := range doc.Buckets {
		n += len(b.Invocations)
	}
	return n
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func copyMap(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
