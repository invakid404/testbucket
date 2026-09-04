package walltime

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
)

// A SIGNER IDENTITY IS EXACTLY 64 LOWER-CASE HEX CHARACTERS.
//
// `ParseSignerKey` decoded `strings.TrimSpace(id)`, and `hex.DecodeString`
// accepts upper case — so " KEY", "KEY\t\n" and the upper-case spelling of a
// key all parsed. Every caller inherited it: record verification, document
// verification, and the nested Stage-1 approval the Stage-2 receipt carries.
//
// Four spellings of one key are four different strings. They compare unequal
// to every predeclared key set, they digest differently, and a document binds
// the bytes it carries rather than what those bytes could be repaired into. A
// receipt could therefore name an approver that no membership test would ever
// match, and validation said it was well formed.
//
// The three levels are asserted separately because they are three different
// claims: that the parser refuses, that a signature check refuses, and that a
// receipt refuses — and only the last is what the campaign reads.
func TestASignerIdentityIsCanonicalAtEveryLevel(t *testing.T) {
	key := mustSigningKey()
	good := PublicKeyOf(key)
	if _, err := ParseSignerKey(good); err != nil {
		t.Fatalf("the canonical rendering of a real key was refused: %v", err)
	}

	spellings := map[string]string{
		"upper case":              strings.ToUpper(good),
		"leading space":           " " + good,
		"trailing space":          good + " ",
		"tab and newline padding": "\t" + good + "\n",
		"an embedded newline":     good[:32] + "\n" + good[32:],
		"too short":               good[:63],
		"too long":                good + "0",
	}

	for name, bad := range spellings {
		t.Run(name, func(t *testing.T) {
			// 1. THE PARSER.
			if _, err := ParseSignerKey(bad); err == nil {
				t.Errorf("ParseSignerKey accepted the non-canonical signer identity %q", bad)
			}

			// 2. A SIGNATURE CHECK. The signature is genuine and made with
			// the very key this string spells, so what is refused is the
			// SPELLING — which is the point: a canonicaliser that silently
			// rewrote it would accept a signer no key set can match.
			d := DigestBytes([]byte("a document"))
			sig := &Signature{
				Authority: CampaignAuthority, KeyID: bad, Digest: d,
				Value: SignApproval(CampaignAuthority, key, d),
			}
			if err := VerifySigned(sig, d, nil); err == nil {
				t.Errorf("VerifySigned accepted a signature whose signer id is %q", bad)
			}
			// And it is not rescued by naming the canonical key in the
			// allowed set: membership is by exact string, so the two do not
			// meet in the middle.
			if err := VerifySigned(sig, d, []string{good}); err == nil {
				t.Errorf("VerifySigned accepted signer id %q against the canonical allowed key", bad)
			}

			// A RECORD's own detached signature, the other signature path.
			rec := Record{Seq: 1, Hash: "sha256:whatever", SignerID: bad}
			rec.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(rec.Hash)))
			if err := VerifySignature(rec); err == nil {
				t.Errorf("VerifySignature accepted a record whose signer id is %q", bad)
			}

			// 3. THE STAGE-2 RECEIPT, which is what a campaign reads.
			r := claimed(matchableReceipt())
			r.Stage1Approval.KeyID = bad
			if err := r.Validate(); err == nil {
				t.Errorf("Stage2Receipt.Validate accepted the non-canonical Stage1Approval.KeyID %q", bad)
			}
		})
	}

	// AND THE CANONICAL SPELLING STILL WORKS AT ALL THREE, so the test cannot
	// be satisfied by a parser that refuses everything.
	t.Run("the canonical spelling is accepted", func(t *testing.T) {
		d := DigestBytes([]byte("a document"))
		sig := &Signature{
			Authority: CampaignAuthority, KeyID: good, Digest: d,
			Value: SignApproval(CampaignAuthority, key, d),
		}
		if err := VerifySigned(sig, d, []string{good}); err != nil {
			t.Errorf("a genuine signature by the predeclared key was refused: %v", err)
		}
		rec := Record{Seq: 1, Hash: "sha256:whatever", SignerID: good}
		rec.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(rec.Hash)))
		if err := VerifySignature(rec); err != nil {
			t.Errorf("a genuine record signature was refused: %v", err)
		}
		r := claimed(matchableReceipt())
		r.Stage1Approval.KeyID = good
		if err := r.Validate(); err != nil {
			t.Errorf("a receipt naming a canonical approver was refused: %v", err)
		}
	})

	// THE IDENTITY IS NOT REWRITTEN. `PublicKeyOf` is the one renderer, and
	// what it emits is what the parser accepts — no other spelling is turned
	// into it on the way in.
	if strings.ToLower(good) != good {
		t.Error("PublicKeyOf does not render a canonical lower-case identity")
	}
	if strings.TrimSpace(good) != good {
		t.Error("PublicKeyOf renders padding into the identity it produces")
	}
}
