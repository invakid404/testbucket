package walltime

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SignerDelegationKind identifies the document that opens a signer delegation.
const SignerDelegationKind = "tb.walltime.signer-delegation/v1"

// SignerDelegation is the run key's statement that ONE OTHER KEY may authorize
// key-log registrations, for one run, at the lower levels only.
//
// The problem it solves is a real gap rather than a missing check. A key-log
// entry is admissible when a party holding a capability the measured work does
// not have vouches for it, and the only such capability was the RUN KEY —
// which is deliberately scoped to the `wall begin` and `wall end` steps, so
// that the measured step cannot forge the signer set. But the script and
// invocation producers mint their keys DURING the measured step, when no
// holder of the run key is left to vouch for anything. Every lower key was
// therefore registered unauthorized, and the verifier correctly reported every
// row ineligible for want of an authorization nobody could produce.
//
// A delegation is the missing party. `wall begin` mints a fresh key, signs
// this document with the run key naming it, and PRINTS the private half for
// the caller to place in the measured step's environment. Holding it confers
// strictly less than the run key:
//
//   - it is bound to ONE run, so it cannot vouch for another;
//   - each scope names a level AND the lowest sequence number it may
//     authorize, so the action envelope's own signers — declared by the roster
//     `wall begin` seals at sequence 0 — stay out of its reach while the
//     action-owned CHILDREN it must register, which start at 1, are inside it;
//   - it cannot sign the roster or the closing seal, which are checked against
//     the predeclared run signers directly.
//
// THE PRIVATE HALF IS NEVER WRITTEN TO DISK. It used to be left in the
// evidence directory, mode 0640 and grouped to the script account — readable
// by the measured bucket script, which is the party that must not choose its
// own attesters, and readable by every observer, which is handed that same
// directory as `--dir` and so is not isolated from it by an environment scrub.
// It travels in the environment instead, exactly as the run key does, where
// the observer scrub removes it and `sudo` strips it from the measured child.
type SignerDelegation struct {
	Kind string `json:"kind"`
	// Run is the run this delegation is good for.
	Run RunIdentity `json:"run"`
	// PublicKey is the delegate that may authorize registrations.
	PublicKey string `json:"public_key"`
	// Scopes are what it may authorize: a level, and the lowest sequence
	// number within it.
	//
	// The sequence bound is what lets the action level appear here at all. The
	// action ENVELOPE's physical, peer and trace signers are sequence 0 and
	// are declared by the roster the run key signs; the action-owned CHILDREN
	// are sequence 1 upwards and are registered from inside the measured step,
	// where no run key exists. A delegate scoped to `action` from 1 can
	// register those and cannot touch the envelope's own.
	Scopes []DelegationScope `json:"scopes"`
	// Binary is the build that opened the delegation.
	Binary    Digest     `json:"binary"`
	Signature *Signature `json:"signature,omitempty"`
}

// DigestOf is the delegation's canonical identity, excluding its signature.
func (d SignerDelegation) DigestOf() (Digest, error) {
	c := d
	c.Signature = nil
	return DigestJSON(c)
}

// signerDelegationAuthority names what the run key is vouching for, so the
// signature cannot be lifted onto another document.
func signerDelegationAuthority(d SignerDelegation) string {
	return "signer-delegation:" + d.Run.RunID + ":" + d.PublicKey
}

// Sign attaches the run key's signature.
func (d *SignerDelegation) Sign(runKey ed25519.PrivateKey) error {
	digest, err := d.DigestOf()
	if err != nil {
		return err
	}
	authority := signerDelegationAuthority(*d)
	d.Signature = &Signature{
		Authority: authority, KeyID: PublicKeyOf(runKey), Digest: digest,
		Value: SignApproval(authority, runKey, digest),
	}
	return nil
}

// Verify checks the delegation against the predeclared run signers and the run
// it claims. It returns the delegate public key it authorizes.
func (d SignerDelegation) Verify(run RunIdentity, keys []string) (string, error) {
	if d.Kind != SignerDelegationKind {
		return "", fmt.Errorf("signer delegation kind %q, want %q", d.Kind, SignerDelegationKind)
	}
	if strings.TrimSpace(d.PublicKey) == "" {
		return "", fmt.Errorf("the signer delegation names no delegate key")
	}
	if run.RunID != "" && d.Run.RunID != run.RunID {
		return "", fmt.Errorf("the signer delegation is for run %q, not %q", d.Run.RunID, run.RunID)
	}
	for _, sc := range d.Scopes {
		// THE ACTION ENVELOPE'S OWN SIGNERS ARE THE ROSTER'S.
		//
		// A delegate reaching sequence 0 at the action level could register a
		// second physical, peer or trace producer for the envelope itself,
		// which is what the roster exists to fix before the measured work
		// starts. Above 0 are the action-owned children, which are created
		// during that work and have no other authorizing party.
		if sc.Level == LevelAction && sc.MinSeq < 1 {
			return "", fmt.Errorf("the signer delegation claims the action level from sequence %d; the action envelope's own signers are declared by the roster the run key signs, so a delegate may only reach the action-owned children at sequence 1 and above", sc.MinSeq)
		}
		if strings.TrimSpace(string(sc.Level)) == "" {
			return "", fmt.Errorf("the signer delegation carries a scope with no level")
		}
	}
	if len(d.Scopes) == 0 {
		return "", fmt.Errorf("the signer delegation authorizes nothing")
	}
	if d.Signature == nil {
		return "", fmt.Errorf("the signer delegation is unsigned; an unauthorized delegate is the measured work choosing who attests it")
	}
	if want := signerDelegationAuthority(d); d.Signature.Authority != want {
		return "", fmt.Errorf("the signer delegation is signed under authority %q, not the %q it delegates", d.Signature.Authority, want)
	}
	if len(keys) == 0 {
		return "", fmt.Errorf("no predeclared run signer to check the delegation against")
	}
	digest, err := d.DigestOf()
	if err != nil {
		return "", err
	}
	if err := VerifySigned(d.Signature, digest, keys); err != nil {
		return "", fmt.Errorf("the signer delegation does not verify against a predeclared run signer: %w", err)
	}
	return d.PublicKey, nil
}

// DelegationScope is one level a delegate may authorize, from MinSeq upwards.
type DelegationScope struct {
	Level  Level `json:"level"`
	MinSeq int   `json:"min_seq"`
}

// authorizes reports whether this delegation covers one entry.
func (d SignerDelegation) authorizes(e KeyLogEntry) bool {
	for _, s := range d.Scopes {
		if s.Level == e.Level && e.Seq >= s.MinSeq {
			return true
		}
	}
	return false
}

// OpenSignerDelegation mints the delegate, signs the delegation with the run
// key, writes the public document beside the roster and RETURNS the private
// half for the caller to hand to the measured step.
//
// It returns "" when there is no run key: that is the developer run, where no
// capability exists to delegate, the lower keys stay unauthorized, and the
// verifier reports the row ineligible rather than scoring evidence nobody
// vouched for.
func OpenSignerDelegation(dir string, run RunIdentity, runKey ed25519.PrivateKey) (string, error) {
	if runKey == nil {
		return "", nil
	}
	key, err := NewSigningKey()
	if err != nil {
		return "", err
	}
	d := SignerDelegation{
		Kind: SignerDelegationKind, Run: run, PublicKey: PublicKeyOf(key),
		Scopes: []DelegationScope{
			{Level: LevelScript, MinSeq: 0},
			{Level: LevelInvocation, MinSeq: 0},
			// The action-owned CHILDREN, never the action envelope's own
			// signers at sequence 0.
			{Level: LevelAction, MinSeq: 1},
		},
		Binary: SelfDigest(),
	}
	if err := d.Sign(runKey); err != nil {
		return "", err
	}
	if err := WriteJSONFile(filepath.Join(dir, signerDelegationFile), d); err != nil {
		return "", err
	}
	return EncodeKey(key), nil
}

// signerDelegationFile is the delegation document's name in the evidence
// directory.
const signerDelegationFile = "signer-delegation.json"

// LoadSignerDelegation reads the delegation document, if one was opened.
func LoadSignerDelegation(dir string) (*SignerDelegation, error) {
	var d SignerDelegation
	if err := ReadJSONFile(filepath.Join(dir, signerDelegationFile), &d); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// delegateKeyFromEnv is the delegate private key the wrapper chain authorizes
// registrations with.
//
// The ENVIRONMENT is the only channel. It used to fall back to a file in the
// evidence directory, which put the capability where the measured script could
// read it — the script account was given group read deliberately — and where
// every observer could open it by path, since observers are handed that same
// directory as `--dir` and run under the wrapper's own credential. A capability
// in a directory the reader is told to walk is not isolated by scrubbing the
// variable that names it.
//
// In the environment it is covered by the scrub every observer already gets,
// and `sudo` resets the environment of the measured child, so neither party
// that must not hold it can.
func delegateKeyFromEnv() (ed25519.PrivateKey, error) {
	v := strings.TrimSpace(os.Getenv(SignerDelegateKeyEnv))
	if v == "" {
		return nil, nil
	}
	return DecodeKey(v)
}
