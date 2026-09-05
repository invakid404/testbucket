package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestThePublishedRecordSignerContractMatchesProduction is the F8 regression.
//
// verifySignerSet gives the authority-signed Stage-1 signer set precedence
// OUTRIGHT: when Stage 1 declares any signer, caller `record-signer` keys are
// ignored rather than added. The CLI flag help, the composite action input and
// the reusable workflow input all still told callers the two sets "are
// unioned". The implementation was the safe one, but the published contract
// was false in the direction that matters: a caller reading it would expect a
// key it supplied to be admitted, ship a run depending on that, and be refused
// by production.
//
// Every surface a caller can read this from is checked, because the defect was
// that the text was corrected in one place and left stale in three.
func TestThePublishedRecordSignerContractMatchesProduction(t *testing.T) {
	surfaces := []struct{ name, path, anchor string }{
		{"the CLI flag help", filepath.Join("wall.go"), "record-signer"},
		{"the verify-wall action input", filepath.Join("..", "..", ".github", "actions", "verify-wall", "action.yml"), "record-signer:"},
		{"the reusable workflow input", filepath.Join("..", "..", ".github", "workflows", "bucketed-reusable.yml"), "record-signer:"},
	}
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			b, err := os.ReadFile(s.path)
			if err != nil {
				t.Fatal(err)
			}
			body := string(b)
			at := strings.Index(body, s.anchor)
			if at < 0 {
				t.Fatalf("%s no longer documents %q", s.name, s.anchor)
			}
			// The description of this one input, not the whole file: another
			// input may legitimately union something.
			desc := body[at:]
			if end := strings.Index(desc[len(s.anchor):], "\n\n"); end >= 0 {
				desc = desc[:len(s.anchor)+end]
			}
			if i := strings.Index(desc, "\n  verifier-key"); i >= 0 {
				desc = desc[:i]
			}
			if strings.Contains(desc, "union") {
				t.Errorf("%s still tells callers the caller and Stage-1 signer sets are unioned; production ignores caller keys whenever Stage 1 declares any:\n%s", s.name, desc)
			}
			// Saying it is not a union is not enough — the caller has to be
			// told which side wins and when its own key is used at all.
			for _, must := range []string{"Stage 1", "declares"} {
				if !strings.Contains(desc, must) && !strings.Contains(desc, strings.ToLower(must)) {
					t.Errorf("%s does not say %q, so it never states when a caller key is used:\n%s", s.name, must, desc)
				}
			}
			if !strings.Contains(strings.ToUpper(desc), "IGNORED") && !strings.Contains(strings.ToUpper(desc), "ARE IGNORED") &&
				!strings.Contains(strings.ToUpper(desc), "WINS OUTRIGHT") {
				t.Errorf("%s does not tell a caller its key is refused when Stage 1 declares its own:\n%s", s.name, desc)
			}
		})
	}
}
