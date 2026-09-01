package walltime

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Document kinds for the two run-key attestations that bracket a measurement.
const (
	RosterKind = "tb.walltime.roster/v1"
	SealKind   = "tb.walltime.seal/v1"
)

// keyLogFile is the append-only register of every public key that signed
// anything in this directory, and rosterFile/sealFile are the two attestations
// that bracket it.
const (
	keyLogFile = "keylog.jsonl"
	rosterFile = "roster.json"
	sealFile   = "seal.json"
)

// RunKeyEnv is where the run key is read from.
//
// It is an environment variable, and specifically a STEP-scoped one: under
// measurement `wall begin` and `wall end` are their own Actions steps and the
// measured bucket script is a different step, so a value bound to those two
// steps is not visible to the work being measured. That boundary is the whole
// mechanism — the roster and the seal are trustworthy exactly to the extent
// that the measured script cannot produce them.
const RunKeyEnv = "TB_WALL_RUN_KEY"

// RosterEntry is one producer's declared signing identity.
type RosterEntry struct {
	Producer  Producer `json:"producer"`
	Level     Level    `json:"level"`
	Seq       int      `json:"seq"`
	PublicKey string   `json:"public_key"`
	Binary    Digest   `json:"binary"`
}

// Roster is the predeclared signer set for one measurement.
//
// Before this existed the verifier checked a record's signature against the
// public key carried IN that record, which proves only that whoever wrote the
// record signed it. A roster is written by `wall begin` — a step the measured
// script is not in — and signed with the run key, so the set of keys that may
// sign action-level evidence is fixed before any measured work runs.
type Roster struct {
	Kind      string        `json:"kind"`
	Run       RunIdentity   `json:"run"`
	Entries   []RosterEntry `json:"entries"`
	Signature *Signature    `json:"signature,omitempty"`
}

// DigestOf is the roster's canonical identity, excluding its own signature.
func (r Roster) DigestOf() (Digest, error) {
	c := r
	c.Signature = nil
	return DigestJSON(c)
}

// Sign attaches the run key's detached signature.
func (r *Roster) Sign(authority string, key ed25519.PrivateKey) error {
	d, err := r.DigestOf()
	if err != nil {
		return err
	}
	r.Signature = &Signature{Authority: authority, KeyID: PublicKeyOf(key), Digest: d, Value: SignDigest(key, d)}
	return nil
}

// Allows reports whether a signer id is one this roster declared.
func (r Roster) Allows(signerID string) bool {
	for _, e := range r.Entries {
		if e.PublicKey == signerID {
			return true
		}
	}
	return false
}

// StreamDigest fixes one record stream's exact bytes.
type StreamDigest struct {
	Name   string `json:"name"`
	Digest Digest `json:"digest"`
	Bytes  int64  `json:"bytes"`
}

// Seal is the closing attestation: the exact bytes of every stream as of
// AT_end, plus the key log and the roster they belong to.
//
// A hash chain proves a stream was not edited in the middle. It does not stop
// a writer that owns the directory from rewriting the whole stream
// consistently. The seal closes that: it is produced by `wall end`, in a step
// the measured script is not in, and any rewrite after it is a digest
// mismatch.
type Seal struct {
	Kind         string         `json:"kind"`
	Run          RunIdentity    `json:"run"`
	RosterDigest Digest         `json:"roster_digest"`
	KeyLogDigest Digest         `json:"key_log_digest"`
	Streams      []StreamDigest `json:"streams"`
	Signature    *Signature     `json:"signature,omitempty"`
}

// DigestOf is the seal's canonical identity, excluding its own signature.
func (s Seal) DigestOf() (Digest, error) {
	c := s
	c.Signature = nil
	return DigestJSON(c)
}

// Sign attaches the run key's detached signature.
func (s *Seal) Sign(authority string, key ed25519.PrivateKey) error {
	d, err := s.DigestOf()
	if err != nil {
		return err
	}
	s.Signature = &Signature{Authority: authority, KeyID: PublicKeyOf(key), Digest: d, Value: SignDigest(key, d)}
	return nil
}

// KeyLogEntry registers one signing key that was minted after the roster was
// sealed. Script- and invocation-level wrappers are started by the measured
// step, so their keys cannot be predeclared; what CAN be fixed is that the set
// is closed at AT_end and nothing may be added to it afterwards.
type KeyLogEntry struct {
	Producer  Producer `json:"producer"`
	Level     Level    `json:"level"`
	Seq       int      `json:"seq"`
	PublicKey string   `json:"public_key"`
	Binary    Digest   `json:"binary"`
}

// RegisterKey appends one minted public key to the directory's key log.
func RegisterKey(dir string, e KeyLogEntry) error {
	if dir == "" {
		return nil
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, keyLogFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("walltime: key log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("walltime: key log: %w", err)
	}
	return f.Sync()
}

// ReadKeyLog returns every registered key and the digest of the exact bytes it
// was read from. A missing log is not an error: it is an empty set, which the
// verifier then reports against the records that needed it.
func ReadKeyLog(dir string) ([]KeyLogEntry, Digest, error) {
	b, err := os.ReadFile(filepath.Join(dir, keyLogFile))
	if os.IsNotExist(err) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("walltime: key log: %w", err)
	}
	var out []KeyLogEntry
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e KeyLogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, "", fmt.Errorf("walltime: key log: %w", err)
		}
		out = append(out, e)
	}
	return out, DigestBytes(b), nil
}

// SealStreams digests every record stream in the directory, in a stable order.
func SealStreams(dir string) ([]StreamDigest, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	out := make([]StreamDigest, 0, len(names))
	for _, p := range names {
		if filepath.Base(p) == keyLogFile {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("walltime: seal %s: %w", filepath.Base(p), err)
		}
		out = append(out, StreamDigest{Name: filepath.Base(p), Digest: DigestBytes(b), Bytes: int64(len(b))})
	}
	return out, nil
}

// WriteRoster seals the predeclared signer set. It is written O_EXCL: a second
// roster for one measurement is two different signer sets, and a verifier that
// accepted the later one would accept whichever was written last.
func WriteRoster(dir string, r Roster) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, rosterFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("walltime: roster: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// WriteSeal writes the closing attestation, also O_EXCL.
func WriteSeal(dir string, s Seal) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, sealFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("walltime: seal: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// ReadRoster and ReadSeal read the two attestations. A missing file is
// reported as such rather than as an empty document, because "there is no
// roster" and "the roster declares nobody" are different findings.
func ReadRoster(dir string) (*Roster, error) {
	var r Roster
	if err := ReadJSONFile(filepath.Join(dir, rosterFile), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func ReadSeal(dir string) (*Seal, error) {
	var s Seal
	if err := ReadJSONFile(filepath.Join(dir, sealFile), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// RunKeyFromEnv reads the run key, if one was delivered to this step.
func RunKeyFromEnv() (ed25519.PrivateKey, error) {
	v := strings.TrimSpace(os.Getenv(RunKeyEnv))
	if v == "" {
		return nil, nil
	}
	return DecodeKey(v)
}

// verifySignerSet is the check that makes wall-time evidence attributable.
//
// Verifying a record against the public key the record itself carries proves
// only that whoever wrote it signed it — a process that can write the records
// directory can mint keys, build a valid chain and sign every line. The
// signer set therefore has to be declared by something that process is not:
//
//   - the ROSTER is written by `wall begin`, before the measured script runs,
//     and signed with a run key delivered only to that step;
//   - the KEY LOG registers keys minted later, by wrappers the measured step
//     starts, which cannot be predeclared because they do not exist yet;
//   - the SEAL is written by `wall end`, in a step the measured script is
//     also not in, and fixes the exact bytes of every stream and of the key
//     log — so the set is closed at AT_end and nothing can be added, removed
//     or rewritten afterwards.
//
// The residual is stated plainly rather than papered over: a hostile measured
// script running as the same user CAN register a key and write records while
// the window is open. What it cannot do is produce a roster, produce a seal,
// or alter anything once the seal exists.
func verifySignerSet(v *Verdict, opt VerifyOptions, declared []string, recs []Record) {
	keys := append(append([]string(nil), opt.SignerKeys...), declared...)
	if len(keys) == 0 {
		v.add("WT-023", SeverityIneligible,
			"no run-key signer was predeclared to this verifier, so a roster and seal signed by any self-generated key would pass and the record chain is self-authenticated")
	}
	if opt.Dir == "" {
		return
	}
	roster, err := ReadRoster(opt.Dir)
	if err != nil {
		v.add("WT-023", SeverityIneligible,
			fmt.Sprintf("no signer roster: the records are authenticated only by the keys they carry, which is what a forger would also supply (%v)", err))
		return
	}
	if roster.Kind != RosterKind {
		v.add("WT-023", SeverityIneligible, fmt.Sprintf("signer roster kind %q, want %q", roster.Kind, RosterKind))
	}
	if roster.Run.CampaignID != v.Run.CampaignID || roster.Run.RunID != v.Run.RunID || roster.Run.BucketID != v.Run.BucketID {
		v.add("WT-023", SeverityIneligible,
			fmt.Sprintf("the signer roster is for run %s/%s/%s, not the measured %s/%s/%s",
				roster.Run.CampaignID, roster.Run.RunID, roster.Run.BucketID,
				v.Run.CampaignID, v.Run.RunID, v.Run.BucketID))
	}
	rosterDigest := Digest("")
	if d, err := roster.DigestOf(); err == nil {
		rosterDigest = d
	}
	verifyRunKeySignature(v, "signer roster", roster.Signature, rosterDigest, keys)

	logged, keyLogDigest, err := ReadKeyLog(opt.Dir)
	if err != nil {
		v.add("WT-023", SeverityIneligible, fmt.Sprintf("key log: %v", err))
		return
	}
	allowed := map[string]bool{}
	for _, e := range roster.Entries {
		allowed[e.PublicKey] = true
	}
	for _, e := range logged {
		allowed[e.PublicKey] = true
	}
	// Report each undeclared signer once: one substituted producer writes many
	// records, and twenty findings naming one key is noise, not detail.
	undeclared := map[string]bool{}
	for _, r := range recs {
		if r.SignerID != "" && !allowed[r.SignerID] && !undeclared[r.SignerID] {
			undeclared[r.SignerID] = true
			v.add("WT-023", SeverityTerminal,
				fmt.Sprintf("a %s/%s record is signed by %s, which neither the roster declared nor the key log registered", r.Producer, r.Level, r.SignerID))
		}
	}

	seal, err := ReadSeal(opt.Dir)
	if err != nil {
		v.add("WT-023", SeverityIneligible,
			fmt.Sprintf("no closing seal: the streams are fixed only by their own hash chains, which a writer that owns the directory can rebuild (%v)", err))
		return
	}
	if seal.Kind != SealKind {
		v.add("WT-023", SeverityIneligible, fmt.Sprintf("closing seal kind %q, want %q", seal.Kind, SealKind))
	}
	sealDigest := Digest("")
	if d, err := seal.DigestOf(); err == nil {
		sealDigest = d
	}
	verifyRunKeySignature(v, "closing seal", seal.Signature, sealDigest, keys)
	if rosterDigest != "" && seal.RosterDigest != rosterDigest {
		v.add("WT-023", SeverityTerminal,
			fmt.Sprintf("the closing seal fixes roster %s but the roster on disk digests to %s", seal.RosterDigest, rosterDigest))
	}
	if seal.KeyLogDigest != keyLogDigest {
		v.add("WT-023", SeverityTerminal,
			fmt.Sprintf("the closing seal fixes key log %s but the key log on disk digests to %s; a signer was added or removed after the measurement closed", seal.KeyLogDigest, keyLogDigest))
	}
	onDisk, err := SealStreams(opt.Dir)
	if err != nil {
		v.add("WT-023", SeverityIneligible, fmt.Sprintf("closing seal: %v", err))
		return
	}
	sealed := map[string]Digest{}
	for _, sd := range seal.Streams {
		sealed[sd.Name] = sd.Digest
	}
	for _, sd := range onDisk {
		want, ok := sealed[sd.Name]
		if !ok {
			v.add("WT-023", SeverityTerminal,
				fmt.Sprintf("stream %s exists but the closing seal does not cover it; it was written after the measurement closed", sd.Name))
			continue
		}
		if want != sd.Digest {
			v.add("WT-023", SeverityTerminal,
				fmt.Sprintf("stream %s was sealed as %s but now digests to %s; it was rewritten after the measurement closed", sd.Name, want, sd.Digest))
		}
		delete(sealed, sd.Name)
	}
	for name := range sealed {
		v.add("WT-023", SeverityTerminal,
			fmt.Sprintf("the closing seal covers stream %s, which is no longer present; evidence was removed after the measurement closed", name))
	}
}

// verifyRunKeySignature checks one run-key attestation against the predeclared
// signer set. An unsigned attestation and one signed by an undeclared key are
// different findings because they call for different fixes.
func verifyRunKeySignature(v *Verdict, what string, sig *Signature, digest Digest, keys []string) {
	if sig == nil {
		v.add("WT-023", SeverityIneligible,
			fmt.Sprintf("the %s is unsigned; without a run key it proves only that something wrote the file", what))
		return
	}
	if len(keys) == 0 {
		return // already reported: nothing was predeclared to check it against
	}
	if digest == "" {
		v.add("WT-023", SeverityIneligible, fmt.Sprintf("the %s cannot be digested, so its signature covers nothing checkable", what))
		return
	}
	if err := VerifySigned(sig, digest, keys); err != nil {
		v.add("WT-023", SeverityTerminal, fmt.Sprintf("the %s signature: %v", what, err))
	}
}
