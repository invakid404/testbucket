package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestEveryAdvisedWallSubcommandIsDispatched is R08's generalizable half.
//
// The verifier's own report told an operator that the campaign-scope gates
// "are decided by `wall campaign` over the full frozen population". There is
// no `campaign` case in the wall dispatcher and there has not been since the
// protected-authority model was removed, so following that advice produced
// `unknown wall subcommand "campaign"` — and the reader was left unable to
// tell whether the gates were decided somewhere else or not at all. Naming a
// command that does not exist is worse than naming none.
//
// IT SCANS STRING LITERALS, not comments. The defect is in what a user is
// TOLD: a comment may truthfully describe a subcommand that was removed (this
// package has several, deliberately), while a string literal reaches a
// terminal as advice. And it reads only the two forms that actually advise —
// `testbucket wall x` and a backticked `wall x` — because "the wall object"
// and "wall time" are prose about the subsystem, not instructions.
func TestEveryAdvisedWallSubcommandIsDispatched(t *testing.T) {
	dispatched := dispatchedWallSubcommands(t)
	if len(dispatched) < 5 {
		t.Fatalf("only %d wall subcommands were found in the dispatcher; this test is reading the wrong switch", len(dispatched))
	}

	advises := regexp.MustCompile("(?:testbucket wall|`wall) ([a-z][a-z-]+)")
	scanned := 0
	for _, root := range []string{".", filepath.Join("..", "..", "internal")} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, p, nil, 0)
			if perr != nil {
				t.Errorf("parse %s: %v", p, perr)
				return nil
			}
			scanned++
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, uerr := strconv.Unquote(lit.Value)
				if uerr != nil {
					text = lit.Value
				}
				for _, m := range advises.FindAllStringSubmatch(text, -1) {
					if !dispatched[m[1]] {
						t.Errorf("%s advises `wall %s`, which the dispatcher has no case for: a reader following it gets \"unknown wall subcommand\"",
							fset.Position(lit.Pos()), m[1])
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if scanned == 0 {
		t.Fatal("no source file was scanned; the paths this test reads have moved")
	}
}

// dispatchedWallSubcommands reads the cases of runWall's switch.
//
// It reads the SOURCE rather than calling runWall with every candidate,
// because the default case returns an error and the real cases do work: a
// probe would open records directories and parse flag sets to learn a fact the
// switch states outright.
func dispatchedWallSubcommands(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile("wall.go")
	if err != nil {
		t.Fatalf("read wall.go: %v", err)
	}
	src := string(b)
	at := strings.Index(src, "switch args[0] {")
	if at < 0 {
		t.Fatal("runWall no longer dispatches on a switch over args[0]")
	}
	body := src[at:]
	if end := strings.Index(body, "\n\t}\n"); end > 0 {
		body = body[:end]
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`case ([^\n:]+):`).FindAllStringSubmatch(body, -1) {
		for _, name := range strings.Split(m[1], ",") {
			if s, err := strconv.Unquote(strings.TrimSpace(name)); err == nil {
				out[s] = true
			}
		}
	}
	return out
}
