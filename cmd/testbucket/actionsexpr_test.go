package main

import (
	"fmt"
	"strings"
	"testing"
)

// A very small evaluator for the GitHub Actions expression subset the workflow
// uses: string literals, the `secrets` and `inputs` contexts (dotted and
// indexed), `==`/`!=`, and the value-returning `&&`/`||`.
//
// It exists so a workflow predicate can be asserted by MEANING rather than by
// matching its text. Two expressions that must agree — a guard and the value it
// guards — can be spelled differently and still be equivalent, or spelled
// almost identically and not be; only evaluating both against the same context
// settles it.
//
// Anything outside the subset is an error, not a guess. If a predicate is
// rewritten using a function this does not know, the test that depends on it
// fails loudly and asks to be updated, which is the correct outcome for a
// control whose whole value is that it is checked.

type ghVal struct {
	s      string
	b      bool
	isBool bool
}

func (v ghVal) truthy() bool {
	if v.isBool {
		return v.b
	}
	return v.s != ""
}

// text is the value as an interpolation would render it.
func (v ghVal) text() string {
	if v.isBool {
		if v.b {
			return "true"
		}
		return "false"
	}
	return v.s
}

// evalActionsExpression evaluates one `${{ … }}` body against the given
// contexts. A context key that is absent reads as the empty string, which is
// how an undeclared or unset secret behaves.
func evalActionsExpression(expr string, ctx map[string]map[string]string) (string, error) {
	toks, err := lexActionsExpression(expr)
	if err != nil {
		return "", err
	}
	p := &ghParser{toks: toks, ctx: ctx}
	v, err := p.parseOr()
	if err != nil {
		return "", err
	}
	if p.pos != len(p.toks) {
		return "", fmt.Errorf("trailing token %q", p.toks[p.pos])
	}
	return v.text(), nil
}

// A string literal token is stored with a leading quote; no identifier can
// start with one, so the two never collide.
func lexActionsExpression(src string) ([]string, error) {
	isIdent := func(c byte) bool {
		return c == '_' || c == '-' ||
			(c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z')
	}
	var out []string
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '\'':
			var sb strings.Builder
			j := i + 1
			for {
				if j >= len(src) {
					return nil, fmt.Errorf("unterminated string literal")
				}
				if src[j] == '\'' {
					// '' inside a literal is one quote.
					if j+1 < len(src) && src[j+1] == '\'' {
						sb.WriteByte('\'')
						j += 2
						continue
					}
					j++
					break
				}
				sb.WriteByte(src[j])
				j++
			}
			out = append(out, "'"+sb.String())
			i = j
		case strings.HasPrefix(src[i:], "&&"), strings.HasPrefix(src[i:], "||"),
			strings.HasPrefix(src[i:], "=="), strings.HasPrefix(src[i:], "!="):
			out = append(out, src[i:i+2])
			i += 2
		case c == '(' || c == ')' || c == '[' || c == ']' || c == '.':
			out = append(out, string(c))
			i++
		case isIdent(c):
			j := i
			for j < len(src) && isIdent(src[j]) {
				j++
			}
			out = append(out, src[i:j])
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q", string(c))
		}
	}
	return out, nil
}

type ghParser struct {
	toks []string
	pos  int
	ctx  map[string]map[string]string
}

func (p *ghParser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *ghParser) next() string {
	t := p.peek()
	p.pos++
	return t
}

// `||` yields the left operand when it is truthy and the right one otherwise —
// it returns a VALUE, not a boolean, which is what makes the workflow's
// `a != ” && a || b` idiom mean "a, else b".
func (p *ghParser) parseOr() (ghVal, error) {
	left, err := p.parseAnd()
	if err != nil {
		return ghVal{}, err
	}
	for p.peek() == "||" {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return ghVal{}, err
		}
		if !left.truthy() {
			left = right
		}
	}
	return left, nil
}

// `&&` is the mirror: the right operand when the left is truthy, else the left.
func (p *ghParser) parseAnd() (ghVal, error) {
	left, err := p.parseComparison()
	if err != nil {
		return ghVal{}, err
	}
	for p.peek() == "&&" {
		p.next()
		right, err := p.parseComparison()
		if err != nil {
			return ghVal{}, err
		}
		if left.truthy() {
			left = right
		}
	}
	return left, nil
}

func (p *ghParser) parseComparison() (ghVal, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return ghVal{}, err
	}
	op := p.peek()
	if op != "==" && op != "!=" {
		return left, nil
	}
	p.next()
	right, err := p.parsePrimary()
	if err != nil {
		return ghVal{}, err
	}
	eq := left.text() == right.text()
	return ghVal{b: eq == (op == "=="), isBool: true}, nil
}

func (p *ghParser) parsePrimary() (ghVal, error) {
	tok := p.next()
	switch {
	case tok == "":
		return ghVal{}, fmt.Errorf("unexpected end of expression")
	case tok == "(":
		v, err := p.parseOr()
		if err != nil {
			return ghVal{}, err
		}
		if got := p.next(); got != ")" {
			return ghVal{}, fmt.Errorf("expected ) and found %q", got)
		}
		return v, nil
	case strings.HasPrefix(tok, "'"):
		return ghVal{s: tok[1:]}, nil
	case tok == "true" || tok == "false":
		return ghVal{b: tok == "true", isBool: true}, nil
	}
	ctx, known := p.ctx[tok]
	if !known {
		return ghVal{}, fmt.Errorf("this evaluator knows only the contexts it was given, not %q", tok)
	}
	switch p.peek() {
	case ".":
		p.next()
		name := p.next()
		if name == "" {
			return ghVal{}, fmt.Errorf("%s. has no property name", tok)
		}
		return ghVal{s: ctx[name]}, nil
	case "[":
		p.next()
		key, err := p.parseOr()
		if err != nil {
			return ghVal{}, err
		}
		if got := p.next(); got != "]" {
			return ghVal{}, fmt.Errorf("expected ] and found %q", got)
		}
		return ghVal{s: ctx[key.text()]}, nil
	}
	return ghVal{}, fmt.Errorf("context %q used without a property", tok)
}

// The evaluator is itself load-bearing, so its semantics are pinned here rather
// than assumed. A `&&`/`||` that returned booleans instead of operands would
// make every expression under test look like it resolved to `true`.
func TestTheActionsExpressionEvaluatorMatchesGitHubSemantics(t *testing.T) {
	ctx := map[string]map[string]string{
		"secrets": {"declared": "value", "NAMED": "named-value", "empty": ""},
		"inputs":  {"selector": "NAMED", "blank": ""},
	}
	for _, tc := range []struct{ expr, want string }{
		{"'literal'", "literal"},
		{"secrets.declared", "value"},
		{"secrets.absent", ""},
		{"secrets[inputs.selector]", "named-value"},
		{"secrets[inputs.blank]", ""},
		{"inputs.selector != ''", "true"},
		{"inputs.blank != ''", "false"},
		{"inputs.blank == ''", "true"},
		// The value-returning forms the workflow is built out of.
		{"secrets.declared != '' && secrets.declared || 'fallback'", "value"},
		{"secrets.empty != '' && secrets.empty || 'fallback'", "fallback"},
		{"inputs.selector != '' && secrets[inputs.selector] || ''", "named-value"},
		{"inputs.blank != '' && secrets[inputs.selector] || ''", ""},
		// Precedence: != binds tighter than &&, which binds tighter than ||.
		{"secrets.empty != '' && 'yes' || 'no'", "no"},
		{"('' ) != '' && 'yes' || 'no'", "no"},
		{"('x') != '' && 'yes' || 'no'", "yes"},
	} {
		got, err := evalActionsExpression(tc.expr, ctx)
		if err != nil {
			t.Errorf("%s: %v", tc.expr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %q, want %q", tc.expr, got, tc.want)
		}
	}

	// Outside the subset is an error, never a guess.
	for _, expr := range []string{"format('{0}', 'a')", "github.ref", "secrets", "secrets.declared &&"} {
		if _, err := evalActionsExpression(expr, ctx); err == nil {
			t.Errorf("%s evaluated without error; an unsupported expression must fail loudly", expr)
		}
	}
}
