package walltime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// loadActionInterfaces reads component-map.json's action_interfaces added-input
// lists, so test 71b compares A against the recorded inventory rather than a
// transcription of it. The two entries use different key spellings, which the
// reader accommodates rather than assuming.
func loadActionInterfaces(t *testing.T) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "walltime", "component-map.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ActionInterfaces []struct {
			Action      string   `json:"action"`
			AddedInputs []string `json:"added_inputs"`
			AddInputs   []string `json:"add_inputs"`
		} `json:"action_interfaces"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, e := range doc.ActionInterfaces {
		switch {
		case len(e.AddedInputs) > 0:
			out[e.Action] = e.AddedInputs
		case len(e.AddInputs) > 0:
			out[e.Action] = e.AddInputs
		}
	}
	return out
}
