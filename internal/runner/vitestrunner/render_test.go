package vitestrunner

import (
	"strings"
	"testing"
)

// TestReporterFlagsStayOutOfTheLegacyProjection is F16's control.
//
// The three reporter tokens were moved into Invocation.Args, which is a
// serialized plan field PD-1's legacy projection carries — so an UNOPTED reporter
// plan's `args` changed, and so did the script bytes: the shell line quotes each
// Args token whole, so an events path containing a space rendered as
// `'--outputFile.json=/tmp/a b/…'` where v0.2.2 emitted
// `--outputFile.json='/tmp/a b/…'`. Those are shell-equivalent and
// byte-different, and the frozen surface is the bytes.
func TestReporterFlagsStayOutOfTheLegacyProjection(t *testing.T) {
	const spacey = "/tmp/a b/events"

	t.Run("unopted args carry no reporter flag", func(t *testing.T) {
		cfg := renderConfig{rootRel: ".", eventsDir: spacey}
		inv := vitestInvocation(cfg, []string{"a.test.ts"}, nil, 0, 0)
		for _, a := range inv.Args {
			if strings.Contains(a, "--reporter") || strings.Contains(a, "--outputFile") {
				t.Fatalf("the unopted projection's args carry %q; PD-1 freezes this field", a)
			}
		}
	})

	t.Run("the unopted script quotes the path, not the whole token", func(t *testing.T) {
		cfg := renderConfig{rootRel: ".", eventsDir: spacey}
		inv := vitestInvocation(cfg, []string{"a.test.ts"}, nil, 0, 0)
		line := shellLine(inv, cfg, 0, 0)
		want := "--outputFile.json='" + spacey + "/bucket-0-00.json'"
		if !strings.Contains(line, want) {
			t.Fatalf("the rendered line does not carry %q:\n%s", want, line)
		}
		if strings.Contains(line, "'--outputFile.json=") {
			t.Fatalf("the whole token is quoted, which is byte-different from v0.2.2:\n%s", line)
		}
	})

	t.Run("the opted path carries the full argv, because the wrapper executes it", func(t *testing.T) {
		cfg := renderConfig{rootRel: ".", eventsDir: spacey, wallDir: "/tmp/records"}
		inv := vitestInvocation(cfg, []string{"a.test.ts"}, nil, 0, 0)
		var found bool
		for _, a := range inv.Args {
			if a == "--outputFile.json="+spacey+"/bucket-0-00.json" {
				found = true
			}
		}
		if !found {
			t.Fatalf("the opted argv does not carry the reporter output file; the wrapper digests Args and QC6 compares it: %v", inv.Args)
		}
	})
}
