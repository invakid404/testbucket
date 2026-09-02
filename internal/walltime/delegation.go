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

// signerDelegateFile is where `wall begin` leaves the delegate private key for
// the wrapper chain, inside the evidence directory.
const signerDelegateFile = ".signer-delegate.key"

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
// this document with the run key naming it, and leaves the private half where
// the wrapper chain can read it and the measured workload cannot. Holding it
// confers strictly less than the run key:
//
//   - it is bound to ONE run, so it cannot vouch for another;
//   - it may authorize the SCRIPT and INVOCATION levels only, so it cannot
//     register an action-level signer — `wall begin` and `wall end` remain the
//     only parties that can;
//   - it cannot sign the roster or the closing seal, which are checked against
//     the predeclared run signers directly.
type SignerDelegation struct {
	Kind string `json:"kind"`
	// Run is the run this delegation is good for.
	Run RunIdentity `json:"run"`
	// PublicKey is the delegate that may authorize registrations.
	PublicKey string `json:"public_key"`
	// Levels are the levels it may authorize. The action level is never among
	// them.
	Levels []Level `json:"levels"`
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
	for _, l := range d.Levels {
		if l == LevelAction {
			return "", fmt.Errorf("the signer delegation claims the action level; the action signers are declared by the roster the run key signs, and a delegate that could register them would be the run key")
		}
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

// authorizes reports whether this delegation covers one entry's level.
func (d SignerDelegation) authorizes(level Level) bool {
	for _, l := range d.Levels {
		if l == level {
			return true
		}
	}
	return false
}

// OpenSignerDelegation mints the delegate, signs the delegation with the run
// key, and leaves both where they belong: the document in the evidence
// directory beside the roster, the private key in a file the wrapper chain can
// read and the measured workload cannot.
//
// Without a run key nothing is delegated. That is the developer run: no
// capability exists to delegate, the lower keys stay unauthorized, and the
// verifier reports the row ineligible rather than scoring evidence nobody
// vouched for.
func OpenSignerDelegation(dir string, run RunIdentity, runKey ed25519.PrivateKey) error {
	if runKey == nil {
		return nil
	}
	key, err := NewSigningKey()
	if err != nil {
		return err
	}
	d := SignerDelegation{
		Kind: SignerDelegationKind, Run: run, PublicKey: PublicKeyOf(key),
		// THE LOWER LEVELS ONLY.
		Levels: []Level{LevelScript, LevelInvocation},
		Binary: SelfDigest(),
	}
	if err := d.Sign(runKey); err != nil {
		return err
	}
	if err := WriteJSONFile(filepath.Join(dir, signerDelegationFile), d); err != nil {
		return err
	}
	return writeDelegateKey(filepath.Join(dir, signerDelegateFile), key)
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

// delegateKeyFromEnvOrDir is the delegate private key the wrapper chain
// authorizes registrations with.
//
// The environment is consulted first because that is how a nested wrapper
// inherits it without reading the evidence directory at all; the file is the
// fallback for a wrapper started by a script that did not carry it. Both are
// readable by the wrapper chain and by nothing the measured workload runs as:
// the variable is scrubbed from every observer, and `sudo` resets the
// environment of the measured child.
func delegateKeyFromEnvOrDir(dir string) (ed25519.PrivateKey, error) {
	if v := strings.TrimSpace(os.Getenv(SignerDelegateKeyEnv)); v != "" {
		return DecodeKey(v)
	}
	if dir == "" {
		return nil, nil
	}
	b, err := os.ReadFile(filepath.Join(dir, signerDelegateFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return DecodeKey(strings.TrimSpace(string(b)))
}
