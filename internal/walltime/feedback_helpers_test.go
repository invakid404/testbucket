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

// sha40 turns a short, readable fixture label into the 40 lowercase hex
// characters §13 declares for head_sha, candidate_sha and workload_commit.
//
// QC15 used to accept any three distinct non-empty strings, so fixtures named
// identities "h1" and "w1" and passed. Those are not commits; the fixtures keep
// their readable labels and this is what makes the bytes legal, so a test that
// passes is a test over a row a real run could produce.
func sha40(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])[:CommitSHALen]
}
