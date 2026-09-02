package walltime

import (
	"fmt"
	"sort"
)

// AblationDerived is the plan's own derived projections for one ablation: the
// documents whose digests the Stage-2 receipt binds.
//
// The gate used to hold only those digest STRINGS. Four distinct opaque hashes
// prove inequality and nothing else — four arbitrary tuples standing for one
// generic topology satisfied the whole prerequisite — so the documents
// themselves have to be present, rederived against the receipt that bound
// them, and read.
type AblationDerived struct {
	// Atoms maps each suffix-collision atom key to the package ids it holds.
	// A collision atom is an entry with more than one member.
	Atoms map[string][]string `json:"atoms"`
	// Topology maps each bucket to the unit kinds it scheduled.
	Topology map[string][]string `json:"topology"`
	// Membership maps each rendered invocation to the units it covers. A legal
	// non-atom slice is an invocation covering a proper subset of a file's
	// units without splitting an atom.
	Membership map[string][]string `json:"membership"`
}

// Digests recomputes the three projection digests exactly as the planner does,
// so they can be compared with the receipt that claims them.
func (d AblationDerived) Digests() (atoms, topology, membership Digest, err error) {
	if atoms, err = DigestJSON(d.Atoms); err != nil {
		return
	}
	if topology, err = DigestJSON(d.Topology); err != nil {
		return
	}
	membership, err = DigestJSON(d.Membership)
	return
}

// realizes reports whether these projections exhibit the shape a stratum
// names, and says what is missing when they do not.
//
// Each stratum is a DIFFERENT EXPERIMENT, and the difference is visible in the
// documents: a collision-atom run has an atom holding more than one package; a
// slice run has an invocation covering part of a file's units; a multi-file
// run schedules whole files across more than one of them; a sequential run
// renders more than one invocation. Comparing hashes could never establish any
// of that.
func (d AblationDerived) realizes(stratum string) string {
	switch stratum {
	case StratumCollisionAtom:
		for key, members := range d.Atoms {
			if len(members) > 1 {
				return ""
			}
			_ = key
		}
		return fmt.Sprintf("no atom holds more than one package (%d atom(s) derived), so no suffix collision was exercised", len(d.Atoms))

	case StratumLegalNonAtomSlice:
		// A slice is an invocation covering a strict subset of the units some
		// other invocation of the same bucket also draws from: at least two
		// invocations whose unit sets differ in size and overlap.
		for a, unitsA := range d.Membership {
			for b, unitsB := range d.Membership {
				if a == b {
					continue
				}
				if len(unitsA) > 0 && len(unitsA) < len(unitsB) && overlaps(unitsA, unitsB) {
					return ""
				}
			}
		}
		return "no invocation covers a strict, overlapping subset of another's units, so no legal non-atom slice was exercised"

	case StratumWholeFileMultiFile:
		files := map[string]bool{}
		for _, kinds := range d.Topology {
			for _, kind := range kinds {
				files[kind] = true
			}
		}
		if total := totalUnits(d.Membership); total < 2 {
			return fmt.Sprintf("the plan covers %d unit(s), so no multi-file whole-file topology was exercised", total)
		}
		return ""

	case StratumSequentialInvocs:
		if len(d.Membership) < 2 {
			return fmt.Sprintf("the plan renders %d invocation(s), so no sequence of them was exercised", len(d.Membership))
		}
		return ""
	}
	return "names a stratum that is not one of the four the contract fixes"
}

func overlaps(a, b []string) bool {
	seen := map[string]bool{}
	for _, v := range b {
		seen[v] = true
	}
	for _, v := range a {
		if seen[v] {
			return true
		}
	}
	return false
}

func totalUnits(membership map[string][]string) int {
	seen := map[string]bool{}
	for _, units := range membership {
		for _, u := range units {
			seen[u] = true
		}
	}
	out := make([]string, 0, len(seen))
	for u := range seen {
		out = append(out, u)
	}
	sort.Strings(out)
	return len(out)
}
