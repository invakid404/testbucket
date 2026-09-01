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

// ProducerID names one producer's execution context: its role, its binary, and
// its process identity. Two producers that share it are the SAME execution
// context and therefore not independent observers, which the verifier checks.
func ProducerID(p Producer) string {
	pid := os.Getpid()
	return fmt.Sprintf("%s@%s#%d.%s", p, shortDigest(SelfDigest()), pid, processStartID(pid))
}

func shortDigest(d Digest) string {
	s := string(d)
	if len(s) > 14 {
		return s[7:19]
	}
	return s
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

// streamName is the per-producer, per-level stream file. One writer per file
// is load-bearing: the hash chain is per stream, and two processes appending
// to one file would interleave into a chain neither of them can close.
func streamName(p Producer, l Level, seq int) string {
	base := string(p) + "." + string(l)
	if l == LevelInvocation {
		base += fmt.Sprintf(".%03d", seq)
	}
	return base + ".jsonl"
}
