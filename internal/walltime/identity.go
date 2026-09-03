package walltime

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
)

// selfOnce caches the binary identity: the digest of the executable actually
// running. It is delivery-bound evidence — a peer built from different bytes
// than the one Stage 1 bound is not the peer the campaign authorised.
var selfOnce struct {
	sync.Once
	digest Digest
	path   string
}

// SelfDigest is the SHA-256 of the running executable, or the empty digest if
// it cannot be read. An unknown binary identity is not fatal here; it is a
// missing delivery fact the verifier refuses to score.
func SelfDigest() Digest {
	selfOnce.Do(func() {
		p, err := os.Executable()
		if err != nil {
			return
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return
		}
		selfOnce.path, selfOnce.digest = p, DigestBytes(b)
	})
	return selfOnce.digest
}

// ProducerID names one producer's execution context: its role and its process
// identity. Two producers that share it are the SAME execution context and
// therefore not independent observers, which the verifier checks.
//
// The binary identity is deliberately NOT encoded here. It lives in its own
// Record field as a full digest, because the verifier compares it for exact
// equality and a digest embedded in a display string invites a substring
// match — which a prefix collision satisfies.
func ProducerID(p Producer) string {
	pid := os.Getpid()
	return fmt.Sprintf("%s#%d.%s", p, pid, processStartID(pid))
}

// NewSigningKey mints a per-producer ed25519 key. Each producer signs with its
// own key, so a peer record and a trace record of the same transition carry
// different signers — the contract's "distinct record hashes/signers".
func NewSigningKey() (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("walltime: signing key: %w", err)
	}
	return priv, nil
}

// EncodeKey renders a private key for handing to a child observer process.
func EncodeKey(k ed25519.PrivateKey) string { return base64.StdEncoding.EncodeToString(k) }

// DecodeKey parses a key produced by EncodeKey.
func DecodeKey(s string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("walltime: decode key: %w", err)
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("walltime: key is %d bytes, want %d", len(b), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(b), nil
}

// SignDigest produces the detached signature value a Signature carries. It is
// one function so every document in this package is signed the same way and a
// second implementation cannot drift.
func SignDigest(k ed25519.PrivateKey, d Digest) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(k, []byte(d)))
}

// SignApproval signs a document digest TOGETHER WITH the authority label that
// will be recorded beside it.
//
// The label used to be outside everything signed: `DigestOf` excludes the
// whole Signature struct, and `SignDigest` covers the digest alone. A valid
// signature from an unprotected context could therefore be relabelled
// `ewj2-campaign` after the fact, and every later check that compared the
// protected authority name was reading a field its own signature did not
// cover. Signing over `authority NUL digest` binds the two: the same bytes
// under a different label do not verify.
func SignApproval(authority string, k ed25519.PrivateKey, d Digest) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(k, approvalMessage(authority, d)))
}

// approvalMessage is the exact byte string an approval signature covers. The
// NUL separator means an authority ending in the digest's first characters
// cannot be confused with a shorter authority and a longer digest.
func approvalMessage(authority string, d Digest) []byte {
	return []byte(authority + "\x00" + string(d))
}

// PublicKeyOf renders a signer id (the hex public key) from a private key.
func PublicKeyOf(k ed25519.PrivateKey) string {
	return hex.EncodeToString(k.Public().(ed25519.PublicKey))
}

// VerifySignature checks a record's detached signature against its hash.
func VerifySignature(r Record) error {
	if r.Signature == "" || r.SignerID == "" {
		return fmt.Errorf("record %d is unsigned", r.Seq)
	}
	pub, err := hex.DecodeString(r.SignerID)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("record %d has a malformed signer id", r.Seq)
	}
	sig, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil {
		return fmt.Errorf("record %d has a malformed signature", r.Seq)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(r.Hash), sig) {
		return fmt.Errorf("record %d signature does not verify", r.Seq)
	}
	return nil
}

// streamName is the per-producer, per-level, PER-SEQUENCE stream file. One
// writer per file is load-bearing: the hash chain is per stream, and two
// processes appending to one file would interleave into a chain neither of
// them can close.
//
// THE SEQUENCE IS ALWAYS IN THE NAME, at every level.
//
// It used to be added only for invocations, so the action envelope (sequence
// 0) and every action-owned child (sequence 1 and up) resolved to the same
// path. The writer resumes a file-wide chain while the verifier groups records
// by producer, level AND sequence — so the child's records began with the
// envelope's chain state and were then read as a stream of their own, which is
// a terminal WT-002 on every action that runs a setup or bucket command. That
// is every measured action.
//
// A rule with a level in it is a rule that gets this wrong again the next time
// a level acquires sequences. One logical identity, one file: the name carries
// everything the verifier groups by.
func streamName(p Producer, l Level, seq int) string {
	return fmt.Sprintf("%s.%s.%03d.jsonl", p, l, seq)
}
