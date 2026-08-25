package gorunner

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/invakid404/testbucket/internal/runner"
)

func TestPackagePatternIsRelativeToItsModule(t *testing.T) {
	lp := func(dir, module, mode string) runner.LivePackage {
		return runner.LivePackage{ID: "example.test/repo/" + dir, Dir: dir, Module: module, Mode: mode, HasTests: true}
	}
	cases := []struct {
		pkg  runner.LivePackage
		want string
	}{
		{lp("ext/common/codegen", "ext/common", runner.ModeOff), "./codegen"},
		{lp("ext/common", "ext/common", runner.ModeOff), "."},
		{lp("internal/engine", ".", runner.ModeWork), "./internal/engine"},
		{lp("netpkg/streamer", "netpkg", runner.ModeWork), "./streamer"},
	}
	for _, tc := range cases {
		if got := pattern(tc.pkg); got != tc.want {
			t.Errorf("%s pattern = %q, want %q", tc.pkg.ID, got, tc.want)
		}
	}
}

func TestIsRunnableMatchesGoTestRunSemantics(t *testing.T) {
	// The universe filter must mirror `go test -run`, which selects tests,
	// examples and fuzz targets — and NOT benchmarks (-bench does those). A
	// Benchmark name inside a slice's alternation would cover nothing while
	// claiming a weight the slicer balances around.
	cases := []struct {
		name string
		want bool
	}{
		{"TestRetry", true},
		{"ExampleClient", true},
		{"ExampleClient_stream", true},
		{"FuzzDecode", true},
		{"BenchmarkEncode", false},
		{"helper", false},
	}
	for _, tc := range cases {
		if got := isRunnable(tc.name); got != tc.want {
			t.Errorf("isRunnable(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTimeoutValidation(t *testing.T) {
	// The go-test -timeout is the adapter's render config, so the adapter
	// validates it at construction: it is spliced verbatim into every emitted
	// invocation, and an unparsable value would fail every bucket of the matrix
	// at once, far from the typo. (This check used to live in the core; it moved
	// here when run configuration moved behind the seam.)
	bad := []string{"20 minutes", "20", "-5m"}
	for _, tv := range bad {
		if err := validateTestTimeout(tv); err == nil {
			t.Errorf("--timeout %q was accepted", tv)
		} else if !strings.Contains(err.Error(), "--timeout") {
			t.Errorf("error for %q does not name the flag: %v", tv, err)
		}
		if _, err := New(Options{Timeout: tv}); err == nil {
			t.Errorf("New accepted --timeout %q", tv)
		}
	}
	for _, tv := range []string{"20m", "1h30m", "90s", ""} {
		if err := validateTestTimeout(tv); err != nil {
			t.Errorf("valid --timeout %q was rejected: %v", tv, err)
		}
	}
}

func TestShellQuoting(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "''"},
		{".", "."},
		{"ext/common", "ext/common"},
		{"-count=100", "-count=100"},
		{"^(TestA|TestB)$", `'^(TestA|TestB)$'`},
		{"a b", "'a b'"},
		{"it's", `'it'\''s'`},
	}
	for _, tc := range cases {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestToolchainTimeoutBoundary(t *testing.T) {
	// Zero is the ONLY opt-out from the subprocess deadline. A negative duration
	// parses fine, and treating it as "disabled" would let a typo (-10m for 10m)
	// silently remove the hang protection — leaving a job to hang until the
	// workflow's own timeout kills it, which is exactly what the deadline was
	// added to prevent.
	cases := []struct {
		name        string
		timeout     time.Duration
		wantErr     bool
		wantDeadlin bool
	}{
		{name: "a negative duration is rejected", timeout: -time.Second, wantErr: true},
		{name: "a large negative duration is rejected", timeout: -10 * time.Minute, wantErr: true},
		{name: "the smallest negative value is rejected", timeout: -1, wantErr: true},
		{name: "zero disables the deadline", timeout: 0},
		{name: "one nanosecond still sets a deadline", timeout: 1, wantDeadlin: true},
		{name: "the default sets a deadline", timeout: 10 * time.Minute, wantDeadlin: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateToolchainTimeout(tc.timeout)
			if tc.wantErr {
				if err == nil {
					t.Fatal("the value was accepted")
				}
				if !strings.Contains(err.Error(), "--toolchain-timeout") {
					t.Errorf("error does not name the flag: %v", err)
				}
			} else if err != nil {
				t.Fatalf("a legal value was rejected: %v", err)
			}

			// newToolchain re-validates rather than trusting its caller, so an
			// unbounded runner cannot be built by reaching it down a path that
			// forgot to check.
			tcr, cerr := newToolchain(tc.timeout)
			if tc.wantErr {
				if cerr == nil {
					t.Fatal("newToolchain built a runner from a rejected value")
				}
				if tcr.timeout != 0 {
					t.Errorf("newToolchain returned a usable runner alongside its error: %+v", tcr)
				}
				return
			}
			if cerr != nil {
				t.Fatalf("newToolchain: %v", cerr)
			}
			ctx, cancel := tcr.context(context.Background())
			defer cancel()
			if _, ok := ctx.Deadline(); ok != tc.wantDeadlin {
				t.Errorf("context has deadline=%v, want %v", ok, tc.wantDeadlin)
			}
		})
	}
}

func TestEachSubprocessGetsItsOwnDeadline(t *testing.T) {
	// The deadline must be PER SUBPROCESS, not a budget for the whole discovery
	// pass. `plan` runs `go work edit`, one `go list` per module and one
	// `go test -list` per name-sliced package, sequentially — with one shared
	// context a slow-but-healthy `go list` could consume the budget and make a
	// later `go test -list` fail the instant it started.
	tc, err := newToolchain(time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	first, cancel1 := tc.context(context.Background())
	defer cancel1()
	d1, ok := first.Deadline()
	if !ok {
		t.Fatal("the first subprocess got no deadline")
	}

	// Any elapsed time at all must push the next subprocess's deadline out; a
	// shared context would hand back the same instant forever.
	time.Sleep(3 * time.Millisecond)

	second, cancel2 := tc.context(context.Background())
	defer cancel2()
	d2, ok := second.Deadline()
	if !ok {
		t.Fatal("the second subprocess got no deadline")
	}
	if !d2.After(d1) {
		t.Errorf("both subprocesses share one deadline (%v vs %v); the timeout is a budget for the whole sweep, not per command", d1, d2)
	}
	if remaining := time.Until(d2); remaining < 50*time.Second {
		t.Errorf("the second subprocess got only %v of its 1m deadline; earlier commands consumed it", remaining)
	}

	// Zero still means "no deadline", for both.
	off, err := newToolchain(0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		ctx, cancel := off.context(context.Background())
		if _, ok := ctx.Deadline(); ok {
			t.Errorf("call %d: a disabled timeout produced a deadline", i)
		}
		cancel()
	}
}

func TestContextCancellationIsHonored(t *testing.T) {
	// The interface advertises cancellation, so the adapter must actually thread
	// the caller's context into its subprocesses — a cancelled context stops the
	// sweep instead of running to the per-command deadline.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}
	tc, err := newToolchain(time.Minute) // a generous per-command deadline
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	_, runErr := tc.run(ctx, t.TempDir(), nil, "list", "./...")
	if runErr == nil {
		t.Fatal("a cancelled context did not stop the subprocess")
	}
	// It must NOT be misreported as a per-command timeout: the deadline was a
	// minute away, the caller cancelled.
	if strings.Contains(runErr.Error(), "timed out") {
		t.Errorf("cancellation was reported as a deadline hit: %v", runErr)
	}
}

func TestToolchainReportsADeadlineHitAsSuch(t *testing.T) {
	// A timeout must not read as a broken repository: the message names the flag
	// so the reader knows which knob to turn.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}
	tc, err := newToolchain(time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	_, runErr := tc.run(context.Background(), t.TempDir(), nil, "work", "edit", "-json")
	if runErr == nil {
		t.Skip("the subprocess beat a 1ns deadline; nothing to assert")
	}
	if !strings.Contains(runErr.Error(), "timed out") || !strings.Contains(runErr.Error(), "--toolchain-timeout") {
		t.Errorf("a deadline hit does not name itself or the flag: %v", runErr)
	}
}
