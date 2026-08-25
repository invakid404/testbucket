package core

import (
	"fmt"
	"io"
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
)

// errWriter records the first write failure and swallows the rest, so a report
// built from dozens of Fprintf calls can be checked once at the end instead of
// threading an error through every line.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		// Report the length as written and return nil: the caller is
		// mid-report, the FIRST error is the one worth keeping, and surfacing
		// every subsequent failure would turn one broken pipe into a page of
		// identical errors. The caller returns e.err.
		//nolint:nilerr // deliberate: the stored first error is returned by the caller
		return len(p), nil
	}
	n, err := e.w.Write(p)
	if err != nil {
		e.err = err
	}
	return n, err
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

// shortenID trims the repo's import-path prefix for display only. The canonical
// IDs in the matrix and the store stay fully qualified.
func shortenID(id, prefix string) string {
	if prefix == "" {
		return id
	}
	return strings.ReplaceAll(id, prefix, "")
}

// displayID additionally collapses a run-slice's name alternation, which can
// run to hundreds of characters, down to its size. The full list stays in the
// matrix and the --shard-plan artifact.
func displayID(id, prefix string) string {
	short := shortenID(id, prefix)
	open := strings.IndexByte(short, '[')
	if open < 0 || !strings.HasSuffix(short, "]") {
		return short
	}
	n := strings.Count(short[open+1:len(short)-1], "|") + 1
	return fmt.Sprintf("%s[%d tests]", short[:open], n)
}

// CommonImportPrefix finds the longest shared import-path prefix ending at a
// path separator, used purely to keep the human tables readable.
func CommonImportPrefix(live []runner.LivePackage) string {
	prefix := ""
	for i, p := range live {
		if i == 0 {
			prefix = p.ID
			continue
		}
		prefix = sharedPrefix(prefix, p.ID)
	}
	if idx := strings.LastIndex(prefix, "/"); idx >= 0 {
		return prefix[:idx+1]
	}
	return ""
}

func sharedPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}

func truncList(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s, … (+%d more)", strings.Join(items[:limit], ", "), len(items)-limit)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
