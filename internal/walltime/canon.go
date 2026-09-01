package walltime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Digest algorithm identities. They are DATA, not documentation: a receipt
// records which algorithm produced its digest, and the verifier refuses a
// receipt whose algorithm identity it does not implement. Changing a rule below
// means minting a new identity here, never re-interpreting an old digest.
const (
	// CanonAlgorithm is the canonicalisation both plan digests are taken over:
	// RFC 8785 (JSON Canonicalization Scheme) plus SHA-256.
	CanonAlgorithm = "rfc8785+sha256/v1"
	// FullPlanDigestAlgorithm digests the WHOLE plan document, byte for byte
	// after canonicalisation — summary counters, notes and all.
	FullPlanDigestAlgorithm = "full-plan-document/v1"
	// SemanticPlanDigestAlgorithm digests only the projection that decides what
	// actually runs: bucket membership, atoms, invocation argv/cwd, script
	// bytes. Two plans that differ only in a human summary share it; two plans
	// that would run one different test never do.
	SemanticPlanDigestAlgorithm = "semantic-plan-projection/v1"
)

// Digest is a hash written as "sha256:<hex>" so a receipt field is
// self-describing rather than a bare hex blob of unknown provenance.
type Digest string

// DigestBytes hashes exact bytes. This is the identity of a file, a script, or
// a raw discovery snapshot: no canonicalisation, no normalisation, no trailing
// newline repair.
func DigestBytes(b []byte) Digest {
	sum := sha256.Sum256(b)
	return Digest("sha256:" + hex.EncodeToString(sum[:]))
}

// DigestJSON canonicalises v per RFC 8785 and hashes the result. It is the only
// way a struct becomes a digest in this package, so two processes that agree on
// the value agree on the digest regardless of field order or Go version.
func DigestJSON(v any) (Digest, error) {
	b, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	return DigestBytes(b), nil
}

// DigestJSONOrEmpty digests a value, or returns the empty digest when there is
// nothing to digest. It exists so a renderer can fill an identity field
// without deciding what an absent identity hashes to.
func DigestJSONOrEmpty(v any) Digest {
	if v == nil {
		return ""
	}
	if s, ok := v.([]string); ok && len(s) == 0 {
		return ""
	}
	d, err := DigestJSON(v)
	if err != nil {
		return ""
	}
	return d
}

// CanonicalJSON renders v as RFC 8785 canonical JSON: object members sorted by
// UTF-16 code unit, no insignificant whitespace, minimal string escapes, and
// ECMAScript number formatting.
//
// It goes through encoding/json first so that what gets canonicalised is
// exactly what the struct tags produce — a field that does not serialise is a
// field that is not in the digest, with no second definition of the mapping to
// drift out of sync.
func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("canonical json: marshal: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	// UseNumber keeps integer literals as their exact text so a large
	// nanosecond count is rejected below rather than silently rounded through
	// float64 into a different digest.
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, fmt.Errorf("canonical json: decode: %w", err)
	}
	var sb bytes.Buffer
	if err := writeCanonical(&sb, tree); err != nil {
		return nil, err
	}
	return sb.Bytes(), nil
}

func writeCanonical(w *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		w.WriteString("null")
	case bool:
		if t {
			w.WriteString("true")
		} else {
			w.WriteString("false")
		}
	case string:
		writeCanonicalString(w, t)
	case json.Number:
		s, err := canonicalNumber(t)
		if err != nil {
			return err
		}
		w.WriteString(s)
	case []any:
		w.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				w.WriteByte(',')
			}
			if err := writeCanonical(w, e); err != nil {
				return err
			}
		}
		w.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sortUTF16(keys)
		w.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				w.WriteByte(',')
			}
			writeCanonicalString(w, k)
			w.WriteByte(':')
			if err := writeCanonical(w, t[k]); err != nil {
				return err
			}
		}
		w.WriteByte('}')
	default:
		return fmt.Errorf("canonical json: unsupported value %T", v)
	}
	return nil
}

// canonicalNumber renders a JSON number the way ECMAScript's Number::toString
// does, which is what RFC 8785 requires.
//
// An integer too large to be an exact float64 is an ERROR, not a rounded
// number: the digest of a receipt must not depend on which side of 2^53 a
// nanosecond count landed on. Fields that legitimately exceed that range (a
// CLOCK_REALTIME sample, say) are carried as strings by every schema here.
func canonicalNumber(n json.Number) (string, error) {
	if i, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
		if i > 1<<53 || i < -(1<<53) {
			return "", fmt.Errorf("canonical json: integer %d exceeds exact float64 range; carry it as a string", i)
		}
		return strconv.FormatInt(i, 10), nil
	}
	f, err := n.Float64()
	if err != nil {
		return "", fmt.Errorf("canonical json: number %q: %w", n.String(), err)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("canonical json: %v is not a finite number", f)
	}
	return es6Number(f), nil
}

// es6Number formats a finite float64 as ECMAScript's Number::toString would:
// plain decimal when the magnitude sits in [1e-6, 1e21), exponential with an
// unpadded exponent otherwise.
func es6Number(f float64) string {
	if f == 0 {
		// ES prints both zeroes as "0"; JCS follows.
		return "0"
	}
	abs := math.Abs(f)
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	// Go writes "1e-07"; ECMAScript writes "1e-7".
	if i := strings.IndexByte(s, 'e'); i >= 0 {
		mant, exp := s[:i], s[i+1:]
		sign := ""
		if exp != "" && (exp[0] == '+' || exp[0] == '-') {
			sign, exp = string(exp[0]), exp[1:]
		}
		exp = strings.TrimLeft(exp, "0")
		if exp == "" {
			exp = "0"
		}
		s = mant + "e" + sign + exp
	}
	return s
}

// writeCanonicalString applies RFC 8785 string escaping: the two mandatory
// escapes, the five short control escapes, \u00xx for every other C0 control,
// and literal UTF-8 for everything else.
func writeCanonicalString(w *bytes.Buffer, s string) {
	w.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			w.WriteString(`\"`)
		case '\\':
			w.WriteString(`\\`)
		case '\b':
			w.WriteString(`\b`)
		case '\f':
			w.WriteString(`\f`)
		case '\n':
			w.WriteString(`\n`)
		case '\r':
			w.WriteString(`\r`)
		case '\t':
			w.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(w, `\u%04x`, r)
				continue
			}
			if r == utf8.RuneError {
				// encoding/json has already replaced invalid bytes; keep the
				// replacement character rather than inventing a byte escape.
				w.WriteRune(r)
				continue
			}
			w.WriteRune(r)
		}
	}
	w.WriteByte('"')
}

// sortUTF16 orders keys by UTF-16 code unit, which is what RFC 8785 specifies
// and what differs from Go's byte-wise sort for characters above the BMP.
func sortUTF16(keys []string) {
	sort.Slice(keys, func(i, j int) bool { return lessUTF16(keys[i], keys[j]) })
}

func lessUTF16(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}
