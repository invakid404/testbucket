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
	r.Signature = &Signature{Authority: authority, KeyID: PublicKeyOf(key), Digest: d, Value: SignApproval(authority, key, d)}
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
	s.Signature = &Signature{Authority: authority, KeyID: PublicKeyOf(key), Digest: d, Value: SignApproval(authority, key, d)}
	return nil
}

// KeyLogEntry registers one signing key that was minted after the roster was
// sealed. Script- and invocation-level wrappers are started by the measured
// step, so their keys cannot be predeclared; the log is closed at AT_end and
// nothing may be added to it afterwards.
//
// Closing the set is NOT the same as authorising it. The log lives in the
// records directory, which the measured step can write, and an entry says who
// its author claims to be. So a hostile script running as the same user could
// mint a key for every lower-level physical, peer and trace producer, append
// each to this file, write ledgers signed by them, and be scored — the trusted
// closing step's seal fixes those bytes but says nothing about who produced
// them. Sealing attacker-writable bytes establishes finality, not provenance.
//
// Authorization is therefore a SIGNATURE by the run key, over the entry's
// whole claimed identity. The run key is delivered only to the envelope steps
// and never to the measured work, so an entry carrying a valid one was
// registered by a party that holds a capability the measured step does not.
// Binding producer, level, seq and binary into the signed payload is what
// stops an admitted key being replayed under a different role.
type KeyLogEntry struct {
	Producer  Producer `json:"producer"`
	Level     Level    `json:"level"`
	Seq       int      `json:"seq"`
	PublicKey string   `json:"public_key"`
	Binary    Digest   `json:"binary"`
	// Authorization is the run-key signature over this entry. An entry
	// without one is retained — the record it explains still exists — but it
	// cannot admit a signer, because nothing distinguishes it from an entry
	// the measured work wrote for itself.
	Authorization *Signature `json:"authorization,omitempty"`
}

// DigestOf is the canonical digest the authorization covers: the entry's whole
// claimed identity, minus the signature itself.
func (e KeyLogEntry) DigestOf() (Digest, error) {
	e.Authorization = nil
	return DigestJSON(e)
}

// Authorize countersigns one entry with the run key.
func (e *KeyLogEntry) Authorize(authority string, key ed25519.PrivateKey) error {
	d, err := e.DigestOf()
	if err != nil {
		return err
	}
	e.Authorization = &Signature{
		Authority: authority, KeyID: PublicKeyOf(key), Digest: d,
		Value: SignApproval(authority, key, d),
	}
	return nil
}

// RegisterKey appends one minted public key to the directory's key log,
// countersigned by the run key when this process holds one.
//
// In a deployment where the run key reaches only the envelope steps, nothing
// inside the measured window can produce an authorized entry — which is the
// point. The lower-level evidence is still written and still retained; what it
// cannot do is qualify a row. Making it qualify requires a producer path that
// holds a capability the measured work does not, and this is the check that
// says whether one existed.
func RegisterKey(dir string, e KeyLogEntry) error {
	runKey, err := RunKeyFromEnv()
	if err != nil {
		return fmt.Errorf("walltime: key log: %w", err)
	}
	return RegisterKeyWith(dir, e, runKey)
}

// RegisterKeyWith is RegisterKey with the authorizing key supplied directly,
// for a producer path that holds it without going through the environment.
func RegisterKeyWith(dir string, e KeyLogEntry, runKey ed25519.PrivateKey) error {
	if dir == "" {
		return nil
	}
	if runKey != nil {
		if err := e.Authorize(keyLogAuthority(e), runKey); err != nil {
			return fmt.Errorf("walltime: key log: %w", err)
		}
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
	// The AUTHORITY-SIGNED set wins outright. Caller-supplied keys used to be
	// unioned with it, and `record-signer` is a caller input all the way down
	// through the CLI, the composite action and the reusable workflow — so a
	// caller could authorise a roster and seal key the authority-signed
	// Stage-1 manifest never declared, while supplying the matching run-key
	// secret itself. That is the measured work choosing who attests it.
	//
	// Caller keys remain useful where Stage 1 declares nothing: an unbound
	// developer run has no authority to defer to, and its row is ineligible on
	// other grounds anyway.
	keys := declared
	if len(keys) == 0 {
		keys = opt.SignerKeys
	}
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
	// The FULL repeated identity, not three fields of it. A roster that agreed
	// about the campaign, the run and the bucket while naming another attempt,
	// job, step or plan would authorise the signer set of a different
	// measurement — and the roster is what decides which keys may sign this
	// one.
	if diff := runIdentityDiff(v.Run, roster.Run); diff != "" {
		v.add("WT-023", SeverityIneligible,
			fmt.Sprintf("the signer roster repeats a different delivery identity than the measured records (%s)", diff))
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
	// A LOGGED KEY IS ADMITTED ONLY IF THE RUN KEY AUTHORIZED IT.
	//
	// Every logged key used to be added unconditionally. The key log is
	// written during the measured step, so that let a hostile script running
	// as the same user mint a key for every lower-level physical, peer and
	// trace producer, register them all, write ledgers signed by them, and be
	// scored — the closing seal fixed those bytes without saying who produced
	// them.
	//
	// The countersignature is checked against the SAME predeclared run-signer
	// set the roster and the seal are checked against, so an entry is
	// admissible exactly when a party holding a capability the measured work
	// does not have vouched for it.
	// registered is every key the log names; allowed is the subset a run-key
	// authorization vouches for. The distinction matters for the SEVERITY: a
	// record signed by a key nobody registered at all is malformed evidence
	// and terminal, while one signed by a registered-but-unauthorized key is
	// well-formed evidence nobody with the right capability vouched for, which
	// is unscorable. Collapsing the two would report a developer run with no
	// run key as a broken record chain.
	registered := map[string]bool{}
	unauthorized := map[string]bool{}
	for _, e := range logged {
		registered[e.PublicKey] = true
		if err := checkKeyLogAuthorization(e, keys); err != nil {
			if !unauthorized[e.PublicKey] {
				unauthorized[e.PublicKey] = true
				v.add("WT-032", SeverityIneligible, fmt.Sprintf(
					"the key log registers %s/%s signer %s %v; the log is written during the measured step, so a self-registered lower-level key is the measured work choosing who attests it. Admitting one needs a producer path holding a capability the workload does not — a different workload uid, or a privileged supervisor — which this wrapper does not ship: see README, blockers to an eligible scored row",
					e.Producer, e.Level, e.PublicKey, err))
			}
			continue
		}
		allowed[e.PublicKey] = true
	}
	// Report each undeclared signer once: one substituted producer writes many
	// records, and twenty findings naming one key is noise, not detail.
	undeclared := map[string]bool{}
	for _, r := range recs {
		if r.SignerID != "" && !allowed[r.SignerID] && !registered[r.SignerID] && !undeclared[r.SignerID] {
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
	// The seal is what fixes the exact bytes of every stream at AT_end. A seal
	// for another run fixes another run's streams, and comparing it to these
	// would be checking the wrong document's promise.
	if diff := runIdentityDiff(v.Run, seal.Run); diff != "" {
		v.add("WT-023", SeverityIneligible,
			fmt.Sprintf("the closing seal repeats a different delivery identity than the measured records (%s)", diff))
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
// keyLogAuthority is the label a key-log authorization is retained under. It
// is the entry's own role rather than a campaign name, so a signature over one
// producer's registration cannot be lifted onto another's.
func keyLogAuthority(e KeyLogEntry) string {
	return fmt.Sprintf("keylog:%s:%s:%d", e.Producer, e.Level, e.Seq)
}

// checkKeyLogAuthorization decides whether one logged key may sign records.
func checkKeyLogAuthorization(e KeyLogEntry, keys []string) error {
	if e.Authorization == nil {
		return fmt.Errorf("with no run-key authorization")
	}
	if want := keyLogAuthority(e); e.Authorization.Authority != want {
		return fmt.Errorf("under authority %q, not the %q this entry claims", e.Authorization.Authority, want)
	}
	if len(keys) == 0 {
		return fmt.Errorf("with no predeclared run signer to check it against")
	}
	d, err := e.DigestOf()
	if err != nil {
		return fmt.Errorf("whose entry cannot be digested: %v", err)
	}
	if err := VerifySigned(e.Authorization, d, keys); err != nil {
		return fmt.Errorf("whose authorization does not verify: %v", err)
	}
	return nil
}

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
