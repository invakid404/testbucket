package walltime

import (
	"crypto/ed25519"
	"os"
	"strings"
)

// writeDelegateKey stores the delegate private key where the WRAPPER CHAIN can
// read it and the measured workload cannot.
//
// The nested invocation wrappers run as the script account — they are started
// by the measured script — so that account must be able to read the delegate.
// The workload account, which runs the test code, is in neither the owner nor
// the group, and `sudo` resets the environment of the measured child, so it
// reaches the delegate by neither route. Mode 0640 rather than 0644 is the
// whole point: a key readable by everything is not a capability.
func writeDelegateKey(path string, key ed25519.PrivateKey) error {
	if err := os.WriteFile(path, []byte(EncodeKey(key)), 0o600); err != nil {
		return err
	}
	user := strings.TrimSpace(os.Getenv(ScriptUserEnv))
	if user == "" {
		return nil
	}
	w := resolveWorkloadCredential(user)
	if len(w.GIDs) == 0 {
		return nil
	}
	if err := os.Chown(path, -1, w.GIDs[0]); err != nil {
		return err
	}
	return os.Chmod(path, 0o640)
}
