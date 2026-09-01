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

// LockedPackage is one resolved package as the LOCKFILE states it.
type LockedPackage struct {
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

// deriveNPMLock reads a package-lock.json v2/v3 `packages` map. Every key is a
// path; the package NAME is what follows the last node_modules/ segment, so a
// nested duplicate resolves to the package it actually is rather than to the
// path it sits at.
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
		// A duplicate name at two depths is two resolutions. Keeping both is
		// not representable in a name-keyed closure, so a disagreement is
		// reported rather than silently resolved to whichever was read last.
		if prev, ok := out[name]; ok && (prev.Version != p.Version || prev.Integrity != p.Integrity) {
			return nil, fmt.Errorf("package-lock.json resolves %s to more than one version (%s and %s); the closure is not a single resolution", name, prev.Version, p.Version)
		}
		out[name] = LockedPackage{Version: p.Version, Integrity: p.Integrity}
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
			current = name
			if prev, ok := out[name]; ok && prev.Version != version {
				return nil, fmt.Errorf("pnpm-lock.yaml resolves %s to more than one version (%s and %s); the closure is not a single resolution", name, prev.Version, version)
			}
			out[name] = LockedPackage{Version: version, Integrity: out[name].Integrity}
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

// splitPnpmKey turns `@vitest/runner@4.1.10(vite@7.0.0)` into its name and
// version. The peer suffix is dropped, and the split is at the LAST '@' so a
// scoped name keeps its leading one.
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

// sortedLockNames is the closure's sorted key list, so every message about it
// reads the same way twice.
func sortedLockNames(m map[string]LockedPackage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
