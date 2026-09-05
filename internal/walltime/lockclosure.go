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
	// LockParserVersion is the version of THIS implementation. A receipt that
	// names a different one is naming a parser that did not run.
	LockParserVersion = "tb.lockclosure/v2"
)

// LockParserIdentity is the identity of the implementation that actually
// re-derives a closure for this parser name.
//
// A receipt used to carry a parser name, version and digest of which only the
// NAME was ever consulted: production dispatched on it and ran its own code,
// so the receipt could claim any version and any bytes and nothing compared
// them with the parser that ran. The digest here is the SHA-256 of the running
// binary — the one identity available at run time that actually contains this
// parser — which is the same identity Stage 1 binds for the verifier.
func LockParserIdentity(name string) (ParserIdentity, error) {
	switch name {
	case LockParserNPM, LockParserPNPM:
		return ParserIdentity{Name: name, Version: LockParserVersion, Digest: SelfDigest()}, nil
	default:
		return ParserIdentity{}, fmt.Errorf("lock parser %q is not one this verifier can re-derive a closure with (%s, %s)", name, LockParserNPM, LockParserPNPM)
	}
}

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
	// Name and Version are the package this node resolves. Version comes from
	// the entry's own `version:` field when it has one: a URL resolution keys
	// the node by its tarball, and reading the version out of that key reports
	// a URL where a version belongs.
	Name    string
	Version string
	// Integrity is the lock's recorded integrity, EMPTY when the resolution
	// carries none. Empty is not "fine": a node with no integrity is not
	// pinned, and the source profile refuses it unless the receipt declares
	// that exception explicitly. Tarball is retained so such a declaration has
	// something to name.
	Integrity string
	Tarball   string
	// PeerContext is the parenthesised peer resolution a pnpm snapshot node
	// carries, empty for a package-metadata node. Two peer contexts of one
	// package@version are two nodes in the graph the façade loads.
	PeerContext string
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

var (
	pnpmIntegrity = regexp.MustCompile(`integrity:\s*([^,}\s]+)`)
	pnpmTarball   = regexp.MustCompile(`tarball:\s*([^,}\s]+)`)
	pnpmVersion   = regexp.MustCompile(`^\s*version:\s*(.+?)\s*$`)
)

// derivePNPMLock reads BOTH sections of a pnpm-lock.yaml v9.
//
// `packages:` holds the resolution metadata — version, integrity, tarball —
// keyed by `name@version`. `snapshots:` holds the actual dependency graph,
// keyed by `name@version` plus the parenthesised PEER CONTEXT it was resolved
// under. The two are one closure: the metadata says what a package is, the
// snapshot says which of possibly several peer resolutions the façade loads.
//
// Reading only `packages:` looked complete and was not. The frozen Mandel lock
// has 2810 package entries and 1845 snapshots, 474 of them peer-qualified, and
// seven packages appear under more than one peer context — including
// `vitest@4.1.10` and `@vitest/mocker@4.1.10`. A closure that stopped at the
// first section silently dropped every one of those distinct nodes while
// reporting a node count that matched the section it had read.
//
// It is a deliberately narrow reader rather than a general YAML parser: it
// accepts exactly the shape pnpm writes and refuses anything else, because a
// tolerant parser that guessed at an unfamiliar line would be re-deriving a
// closure from a document it had not actually understood.
func derivePNPMLock(lock []byte) (map[string]LockedPackage, error) {
	lines := strings.Split(strings.ReplaceAll(string(lock), "\r\n", "\n"), "\n")
	packages, err := pnpmSection(lines, "packages:")
	if err != nil {
		return nil, err
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("pnpm-lock.yaml resolves no packages; the `packages:` section is missing or empty")
	}
	out := map[string]LockedPackage{}
	for key, body := range packages {
		node, err := pnpmNode(key, body)
		if err != nil {
			return nil, err
		}
		out[key] = node
	}

	// Snapshots inherit their metadata from the package entry for the same
	// name@version. A snapshot with no such entry is a graph node whose
	// resolution nobody recorded, which is exactly the kind of gap this
	// closure exists to make visible.
	snapshots, err := pnpmSection(lines, "snapshots:")
	if err != nil {
		return nil, err
	}
	for key := range snapshots {
		base, peer := splitPnpmPeerContext(key)
		if peer == "" {
			if _, ok := out[key]; ok {
				// An unqualified snapshot names the same node as its package
				// entry; there is nothing further to represent.
				continue
			}
		}
		meta, ok := out[base]
		if !ok {
			return nil, fmt.Errorf("pnpm-lock.yaml snapshot %q has no `packages:` entry for %q, so its resolution metadata is unrecorded", key, base)
		}
		node := meta
		node.Key, node.PeerContext = key, peer
		out[key] = node
	}
	return out, nil
}

// pnpmSection returns the two-space-indented entries of one top-level section,
// each mapped to its indented body lines.
func pnpmSection(lines []string, header string) (map[string][]string, error) {
	out := map[string][]string{}
	inSection := false
	current := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			inSection = trimmed == header
			current = ""
			continue
		}
		if !inSection {
			continue
		}
		if indent == 2 && strings.HasSuffix(strings.TrimRight(line, " "), ":") {
			key := strings.Trim(strings.TrimSuffix(trimmed, ":"), `'"`)
			if key == "" {
				return nil, fmt.Errorf("pnpm-lock.yaml has an empty key in %s", header)
			}
			current = key
			if _, ok := out[key]; !ok {
				out[key] = nil
			}
			continue
		}
		if current != "" {
			out[current] = append(out[current], line)
		}
	}
	return out, nil
}

// pnpmNode reads one `packages:` entry into a resolved node.
func pnpmNode(key string, body []string) (LockedPackage, error) {
	base, peer := splitPnpmPeerContext(key)
	name, version := splitPnpmKey(base)
	if name == "" {
		return LockedPackage{}, fmt.Errorf("pnpm-lock.yaml package key %q is not name@version", key)
	}
	node := LockedPackage{Key: key, Name: name, Version: version, PeerContext: peer}
	for _, line := range body {
		if m := pnpmIntegrity.FindStringSubmatch(line); m != nil && node.Integrity == "" {
			node.Integrity = strings.Trim(m[1], `'"`)
		}
		if m := pnpmTarball.FindStringSubmatch(line); m != nil && node.Tarball == "" {
			node.Tarball = strings.Trim(m[1], `'"`)
		}
		// The entry's OWN version wins. A URL resolution keys the node by its
		// tarball — `xlsx@https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz`
		// in the frozen Mandel lock — and splitting that key at its last '@'
		// reports a URL fragment where a version belongs.
		if m := pnpmVersion.FindStringSubmatch(line); m != nil {
			node.Version = strings.Trim(m[1], `'"`)
		}
	}
	if node.Version == "" {
		return LockedPackage{}, fmt.Errorf("pnpm-lock.yaml package %q records no version", key)
	}
	return node, nil
}

// splitPnpmPeerContext separates `name@version` from its `(peer@x)(peer@y)`
// suffix. Nested parentheses are ordinary in pnpm peer contexts, so the split
// is at the first top-level '(' rather than by matching pairs.
func splitPnpmPeerContext(key string) (base, peer string) {
	if i := strings.Index(key, "("); i >= 0 {
		return key[:i], key[i:]
	}
	return key, ""
}

// splitPnpmKey reads the name and the KEY'S version out of a peer-stripped
// lock key. The split is at the LAST '@' so a scoped name keeps its leading
// one. The version it returns is provisional: an entry's own `version:` field
// overrides it, which is what makes a URL resolution report a version.
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
