package walltime

import (
	"fmt"
	"sort"
	"strings"
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
	// Topology maps each bucket to the units it scheduled, each as
	// `kind:file[,file...]` — see TopologyEntry. The FILES are what make this
	// a topology rather than a list of tiers.
	Topology map[string][]string `json:"topology"`
	// Membership maps each rendered invocation to the units it covers. A legal
	// non-atom slice is an invocation covering a proper subset of a file's
	// units without splitting an atom.
	Membership map[string][]string `json:"membership"`
}

// TopologyEntry renders one scheduled unit's kind and the files it covers, and
// topologyUnit reads it back. One function writes the grammar and one reads
// it, so the planner's projection and the gate's predicate cannot disagree
// about what a topology entry says.
func TopologyEntry(kind string, files []string) string {
	return kind + ":" + strings.Join(files, ",")
}

type topologyUnit struct {
	kind  string
	files []string
}

func topologyUnitOf(entry string) (topologyUnit, bool) {
	kind, files, ok := strings.Cut(entry, ":")
	if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(files) == "" {
		return topologyUnit{}, false
	}
	return topologyUnit{kind: kind, files: strings.Split(files, ",")}, true
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
// Each stratum is a DIFFERENT EXPERIMENT, and the difference has to be visible
// in the documents themselves — not in a label, and not in a count that any
// plan satisfies. Every predicate here asks what the SCHEDULE did: which files
// rode together, which file was divided, how many invocations one bucket ran.
func (d AblationDerived) realizes(stratum string) string {
	switch stratum {
	case StratumCollisionAtom:
		// A collision atom in the inventory is a fact about the repository.
		// The stratum is about the SCHEDULE: the ablation must have packed
		// that atom's members into one unit, which is the whole reason a
		// collision atom cannot be split. Accepting any multi-member atom in
		// the inventory asked nothing of the plan at all.
		collisions := map[string][]string{}
		for key, members := range d.Atoms {
			if len(members) > 1 {
				collisions[key] = members
			}
		}
		if len(collisions) == 0 {
			return fmt.Sprintf("no atom holds more than one package (%d atom(s) derived), so no suffix collision was exercised", len(d.Atoms))
		}
		for key, members := range collisions {
			for _, u := range d.units() {
				if covers(u.files, members) {
					return ""
				}
			}
			_ = key
		}
		return fmt.Sprintf("%d collision atom(s) exist in the inventory, but no scheduled unit covers all the members of any of them, so the collision this stratum names was never actually scheduled together", len(collisions))

	case StratumLegalNonAtomSlice:
		// A slice divides ONE file into name subsets, so the real invocations
		// that result cover DISJOINT units — the previous predicate looked for
		// overlapping strict subsets, which a rendered slice schedule never
		// produces and only a fabricated document could.
		sliced := map[string][]string{}
		for _, units := range d.Membership {
			for _, u := range units {
				if base, ok := sliceBase(u); ok {
					sliced[base] = appendUnique(sliced[base], u)
				}
			}
		}
		if len(sliced) == 0 {
			return "no invocation covers a name-slice unit, so no slice of a file was exercised"
		}
		for base, units := range sliced {
			if len(units) < 2 {
				continue
			}
			// SPLITTING AN ATOM IS TERMINAL, so a legal slice is a slice of a
			// file that rides alone: a file sharing an atom with another
			// cannot be divided without separating members that must stay
			// together.
			if members, atom := d.atomOf(base); atom != "" && len(members) > 1 {
				continue
			}
			// And the slices must be SEPARATE invocations: two name subsets
			// gathered into one call is the whole file under another name.
			for _, covered := range d.Membership {
				if n := countIn(covered, units); n > 0 && n < len(units) {
					return ""
				}
			}
		}
		return "no file was divided into name slices across separate invocations without splitting an atom, so no legal non-atom slice was exercised"

	case StratumWholeFileMultiFile:
		// WHOLE files, and MORE THAN ONE OF THEM. The predicate built a file
		// set it never used and then accepted two units from a single file.
		files := map[string]bool{}
		var unreadable []string
		for _, u := range d.units() {
			if u.kind != "" && !wholeFileKind(u.kind) {
				continue
			}
			for _, f := range u.files {
				files[f] = true
			}
		}
		for _, entries := range d.Topology {
			for _, e := range entries {
				if _, ok := topologyUnitOf(e); !ok {
					unreadable = append(unreadable, e)
				}
			}
		}
		if len(unreadable) > 0 {
			sort.Strings(unreadable)
			return fmt.Sprintf("topology entries %v state no file identity, so which files were scheduled whole cannot be read from the plan at all", unreadable)
		}
		if len(files) < 2 {
			return fmt.Sprintf("the plan schedules whole units covering %d distinct file(s), so no multi-file whole-file topology was exercised", len(files))
		}
		return ""

	case StratumSequentialInvocs:
		// SEQUENTIAL means one bucket ran several invocations one after the
		// other. Counting invocations across the whole plan counted buckets
		// that run in parallel on different runners.
		perBucket := map[string]int{}
		for key := range d.Membership {
			bucket, _, _ := strings.Cut(key, "/")
			perBucket[bucket]++
		}
		for _, n := range perBucket {
			if n >= 2 {
				return ""
			}
		}
		return fmt.Sprintf("no bucket renders more than one invocation (%d invocation(s) across %d bucket(s)), so no sequence of them was exercised", len(d.Membership), len(perBucket))
	}
	return "names a stratum that is not one of the four the contract fixes"
}

// units is every scheduled unit the topology projection names, read through
// the one grammar that writes it.
func (d AblationDerived) units() []topologyUnit {
	var out []topologyUnit
	keys := make([]string, 0, len(d.Topology))
	for k := range d.Topology {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, e := range d.Topology[k] {
			if u, ok := topologyUnitOf(e); ok {
				out = append(out, u)
			}
		}
	}
	return out
}

// atomOf is the atom a file belongs to, and its members.
func (d AblationDerived) atomOf(file string) ([]string, string) {
	keys := make([]string, 0, len(d.Atoms))
	for k := range d.Atoms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, m := range d.Atoms[k] {
			if m == file {
				return d.Atoms[k], k
			}
		}
	}
	return nil, ""
}

// wholeFileKind reports whether a unit kind runs its files ENTIRE. A name
// slice runs part of one file and a count shard runs it with a divided sweep;
// neither is the whole-file topology this stratum is about.
func wholeFileKind(kind string) bool {
	return kind == "package" || kind == "module-atom"
}

// sliceBase is the file a name-slice unit id divides, from the `pkg[a|b]`
// grammar core renders. A runnable name may itself contain the separator, so
// only the FIRST bracket is structural.
func sliceBase(unit string) (string, bool) {
	i := strings.IndexByte(unit, '[')
	if i <= 0 || !strings.HasSuffix(unit, "]") {
		return "", false
	}
	return unit[:i], true
}

func covers(have, want []string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return len(want) > 0
}

func countIn(have, want []string) int {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	n := 0
	for _, w := range want {
		if set[w] {
			n++
		}
	}
	return n
}

func appendUnique(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}

// RealizesForTest exposes the stratum predicate so a producer-side test can
// check that what the planner derives realizes what the gate reads. It is the
// same function the gate calls; a second implementation for tests would be a
// second answer.
func RealizesForTest(d AblationDerived, stratum string) string { return d.realizes(stratum) }
