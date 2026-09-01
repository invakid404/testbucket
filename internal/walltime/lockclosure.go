package walltime

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The lock parsers this verifier can independently re-derive a closure with.
// A receipt naming any other parser is refused rather than believed: the point
// of binding the lockfile bytes is that somebody other than the producer can
// read them, and a parser nobody here implements leaves the closure exactly as
// unverified as a bare digest did.
const (
	LockParserNPM  = "npm-lock"
	LockParserPNPM = "pnpm-lock"
)

// LockedPackage is one RESOLVED NODE as the LOCKFILE states it.
//
// The closure is keyed by Key — the lock's own identity for that node — and
// not by Name. A real lockfile resolves the same package at several versions:
// the frozen Mandel lock resolves `@ai-sdk/provider-utils` at both 2.2.8 and
// 3.0.30, and a name-keyed map cannot represent that at all. It could only
// report a conflict and refuse, which is what it did — the production parser
// could not read the very lockfile the frozen profile names.
//
// A resolution is therefore a multiset over names, keyed by the full lock
// identity: `name@version` for pnpm, plus any peer/snapshot suffix the lock
// itself carries, and the packages-map path for npm, where depth is what
// distinguishes two resolutions of one name.
type LockedPackage struct {
	// Key is the lock's own identity for this node, and the closure's key.
	Key string
	// Name and Version are parsed out of Key for the rules that are about a
	// package rather than about a node.
	Name      string
	Version   string
	Integrity string
}

// DeriveLockClosure re-derives the complete resolved package closure from the
// exact lockfile bytes.
//
// This is the independent half of the source profile. A receipt that carries a
// package map and a lockfile DIGEST proves only that somebody wrote both down;
// nothing checks that the map is what the lock says, that it is complete, or
// that the two entries it happens to list are the only @vitest/* packages in
// the tree. Deriving the closure here from the bound bytes makes the receipt
// checkable rather than asserted.
func DeriveLockClosure(parser string, lock []byte) (map[string]LockedPackage, error) {
	if len(lock) == 0 {
		return nil, fmt.Errorf("no lockfile bytes were bound, so the resolved closure cannot be re-derived")
	}
	switch parser {
	case LockParserNPM:
		return deriveNPMLock(lock)
	case LockParserPNPM:
		return derivePNPMLock(lock)
	default:
		return nil, fmt.Errorf("lock parser %q is not one this verifier can re-derive a closure with (%s, %s)", parser, LockParserNPM, LockParserPNPM)
	}
}

// deriveNPMLock reads a package-lock.json v2/v3 `packages` map. The map's own
// key — the install path — IS the node's identity: npm expresses two
// resolutions of one name as two paths at different depths, so keying by path
// represents the graph the lock actually describes. The NAME is what follows
// the last node_modules/ segment.
func deriveNPMLock(lock []byte) (map[string]LockedPackage, error) {
	var doc struct {
		LockfileVersion int `json:"lockfileVersion"`
		Packages        map[string]struct {
			Name      string `json:"name"`
			Version   string `json:"version"`
			Integrity string `json:"integrity"`
			Link      bool   `json:"link"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(lock, &doc); err != nil {
		return nil, fmt.Errorf("parse package-lock.json: %w", err)
	}
	if doc.LockfileVersion < 2 {
		return nil, fmt.Errorf("package-lock.json is lockfileVersion %d; only 2 and 3 carry the resolved closure this check re-derives", doc.LockfileVersion)
	}
	if len(doc.Packages) == 0 {
		return nil, fmt.Errorf("package-lock.json resolves no packages")
	}
	out := map[string]LockedPackage{}
	for path, p := range doc.Packages {
		if p.Link {
			continue
		}
		name := packageNameFromPath(path)
		if name == "" {
			name = p.Name
		}
		if name == "" || p.Version == "" {
			continue
		}
		// Keyed by the install PATH. A duplicate name at two depths is two
		// resolutions and both are kept: they are two nodes in the graph the
		// façade loads, and a closure that dropped one would not describe the
		// tree that ran.
		out[path] = LockedPackage{Key: path, Name: name, Version: p.Version, Integrity: p.Integrity}
	}
	return out, nil
}

func packageNameFromPath(path string) string {
	i := strings.LastIndex(path, "node_modules/")
	if i < 0 {
		return ""
	}
	return path[i+len("node_modules/"):]
}

var pnpmIntegrity = regexp.MustCompile(`integrity:\s*([^,}\s]+)`)

// derivePNPMLock reads the `packages:` section of a pnpm-lock.yaml v9. Keys
// there are `name@version` with an optional parenthesised peer suffix, and the
// integrity lives in the entry's `resolution:` mapping.
//
// It is a deliberately narrow reader rather than a general YAML parser: it
// accepts exactly the shape pnpm writes and refuses anything else, because a
// tolerant parser that guessed at an unfamiliar line would be re-deriving a
// closure from a document it had not actually understood.
func derivePNPMLock(lock []byte) (map[string]LockedPackage, error) {
	out := map[string]LockedPackage{}
	lines := strings.Split(strings.ReplaceAll(string(lock), "\r\n", "\n"), "\n")
	inPackages := false
	current := ""
	for _, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			inPackages = strings.TrimSpace(line) == "packages:"
			current = ""
			continue
		}
		if !inPackages {
			continue
		}
		if indent == 2 && strings.HasSuffix(strings.TrimRight(line, " "), ":") {
			key := strings.TrimSuffix(strings.TrimSpace(line), ":")
			key = strings.Trim(key, `'"`)
			name, version := splitPnpmKey(key)
			if name == "" || version == "" {
				return nil, fmt.Errorf("pnpm-lock.yaml package key %q is not name@version", key)
			}
			// Keyed by the lock's OWN key, peer/snapshot suffix and all. Two
			// versions of one name are two nodes, and one name under two peer
			// contexts is two nodes; both are what the lock resolved.
			if prev, ok := out[key]; ok && prev.Version != version {
				return nil, fmt.Errorf("pnpm-lock.yaml repeats key %q with two versions (%s and %s)", key, prev.Version, version)
			}
			current = key
			out[key] = LockedPackage{Key: key, Name: name, Version: version, Integrity: out[key].Integrity}
			continue
		}
		if current != "" && strings.Contains(line, "integrity:") {
			if m := pnpmIntegrity.FindStringSubmatch(line); m != nil {
				e := out[current]
				e.Integrity = strings.Trim(m[1], `'"`)
				out[current] = e
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("pnpm-lock.yaml resolves no packages; the `packages:` section is missing or empty")
	}
	return out, nil
}

// splitPnpmKey reads the name and version OUT of a lock key, leaving the key
// itself intact as the node's identity. A peer suffix is ignored for this
// purpose only — `@vitest/runner@4.1.10(vite@7.0.0)` is version 4.1.10 of
// @vitest/runner — and the split is at the LAST '@' so a scoped name keeps its
// leading one.
func splitPnpmKey(key string) (string, string) {
	if i := strings.Index(key, "("); i >= 0 {
		key = key[:i]
	}
	i := strings.LastIndex(key, "@")
	if i <= 0 {
		return "", ""
	}
	return key[:i], key[i+1:]
}

// IsVitestPackage reports whether a package name is part of the Vitest closure
// the frozen lifecycle inventory was written against.
func IsVitestPackage(name string) bool {
	return name == "vitest" || strings.HasPrefix(name, "@vitest/")
}

// sortedLockNames is the closure's sorted KEY list, so every message about it
// reads the same way twice.
func sortedLockNames(m map[string]LockedPackage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
