//go:build !linux

package walltime

import (
	"crypto/ed25519"
	"os"
)

// writeDelegateKey stores the delegate private key owner-readable. Off Linux
// there is no second account to share it with, and no scorable containment
// either.
func writeDelegateKey(path string, key ed25519.PrivateKey) error {
	return os.WriteFile(path, []byte(EncodeKey(key)), 0o600)
}
