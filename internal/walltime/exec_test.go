package walltime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The independent observers are separate PROCESSES by design, so the tests
// re-execute this test binary as the observer. TestMain dispatches on the
// environment variable the launcher sets; everything else about the observer's
// life is production code.
const observerEnv = "TB_WALLTIME_TEST_OBSERVER"

func TestMain(m *testing.M) {
	if os.Getenv(observerEnv) != "" {
		if err := observerFromArgs(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	ObserverLauncher = func(args []string) (*exec.Cmd, error) {
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		cmd := exec.Command(self, args...)
		cmd.Env = append(os.Environ(), observerEnv+"=1")
		return cmd, nil
	}
	os.Exit(m.Run())
}

// observerFromArgs parses the same flags the CLI passes and runs the real
// observer, so what the tests exercise is the shipped code path.
func observerFromArgs(args []string) error {
	flags := map[string]string{}
	for i := 0; i+1 < len(args); i += 2 {
		flags[args[i]] = args[i+1]
	}
	var ident ContainmentIdentity
	if err := json.Unmarshal([]byte(flags["--containment"]), &ident); err != nil {
		return err
	}
	var run RunIdentity
	if s := flags["--run"]; s != "" {
		if err := json.Unmarshal([]byte(s), &run); err != nil {
			return err
		}
	}
	key, err := DecodeKey(flags["--key"])
	if err != nil {
		return err
	}
	timeout, err := time.ParseDuration(flags["--timeout"])
	if err != nil {
		timeout = time.Minute
	}
	seq := 0
	fmt.Sscanf(flags["--seq"], "%d", &seq)
	return RunObserver(ObserverConfig{
		Producer: Producer(flags["--producer"]), Level: Level(flags["--level"]), Seq: seq,
		Dir: flags["--dir"], ControlBase: flags["--control"], Containment: ident,
		Run: run, Key: key, Timeout: timeout,
	})
}

// TestExecProducesThreeIndependentLedgers is the end-to-end shape of one
// measured invocation: three producers, three streams, one lifecycle.
func TestExecProducesThreeIndependentLedgers(t *testing.T) {
	dir := t.TempDir()
	code, err := Exec(ExecOptions{
		Level: LevelInvocation, Seq: 0, Dir: dir,
		Run:  RunIdentity{BucketID: "b1", Stage2: "sha256:test"},
		Argv: []string{"sh", "-c", "sleep 0.05"}, Cwd: dir,
		Selector: []string{"tests/fast.spec.ts"}, Desc: "tests/fast.spec.ts",
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	recs, err := ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	producers := map[Producer]int{}
	for _, r := range recs {
		if r.Kind == "boundary" {
			producers[r.Producer]++
		}
	}
	for _, p := range []Producer{ProducerPhysical, ProducerPeer, ProducerTrace} {
		if producers[p] != 2 {
			t.Errorf("%s wrote %d boundary records, want 2 (start and end)", p, producers[p])
		}
	}

	v, err := VerifyDir(VerifyOptions{Dir: dir})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	// Ordering, chaining, independence and the partition must all hold even on
	// a host that cannot score: those are properties of the records, not of
	// the containment primitive.
	for _, f := range v.Findings {
		if f.Severity == SeverityTerminal {
			t.Errorf("terminal finding on a well-formed run: %s %s", f.Code, f.Detail)
		}
	}
	if !v.Complete {
		t.Errorf("Complete = false, want true")
	}
	// And it must NOT be eligible: nothing here is bound to a Stage-1 manifest
	// or a Stage-2 receipt, and this host has no scored containment primitive.
	if v.Eligible {
		t.Errorf("Eligible = true for an unbound run on an unscored host; the verifier must fail closed")
	}
	if v.InvocationNs == nil || v.InvocationNs[0] < int64(40*time.Millisecond) {
		t.Errorf("invocation duration = %v, want at least the 50ms the child slept", v.InvocationNs)
	}
}

// TestExecPropagatesChildFailure proves a failing child stays a failing
// bucket: the wrapper must never make a red run look green.
func TestExecPropagatesChildFailure(t *testing.T) {
	dir := t.TempDir()
	code, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: dir, Argv: []string{"sh", "-c", "exit 3"}, Cwd: dir,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
	v, err := VerifyDir(VerifyOptions{Dir: dir})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	found := false
	for _, f := range v.Findings {
		if f.Code == "WT-014" {
			found = true
		}
	}
	if !found {
		t.Errorf("a failed child left no WT-014 finding; findings = %+v", v.Findings)
	}
	if v.Eligible {
		t.Errorf("a failed run must not be scorable")
	}
}

// TestPeerAndTraceAreIndependent is the check a cheap implementation fails.
func TestPeerAndTraceAreIndependent(t *testing.T) {
	dir := t.TempDir()
	if _, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: dir, Argv: []string{"sh", "-c", "true"}, Cwd: dir,
		Timeout: 30 * time.Second,
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	recs, err := ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var peer, trace []Record
	for _, r := range recs {
		if r.Kind != "boundary" {
			continue
		}
		switch r.Producer {
		case ProducerPeer:
			peer = append(peer, r)
		case ProducerTrace:
			trace = append(trace, r)
		}
	}
	if len(peer) != 2 || len(trace) != 2 {
		t.Fatalf("peer=%d trace=%d boundary records, want 2 each", len(peer), len(trace))
	}
	for i := range peer {
		switch {
		case peer[i].RawEventID == trace[i].RawEventID:
			t.Errorf("boundary %d: peer and trace share raw event id %q", i, peer[i].RawEventID)
		case peer[i].Instant.Mono == trace[i].Instant.Mono:
			t.Errorf("boundary %d: peer and trace report the same reading; an endpoint was copied", i)
		case peer[i].ProducerID == trace[i].ProducerID:
			t.Errorf("boundary %d: peer and trace share execution context %q", i, peer[i].ProducerID)
		case peer[i].SignerID == trace[i].SignerID:
			t.Errorf("boundary %d: peer and trace share a signer", i)
		}
	}
	// The contract's endpoint containment, on the records themselves.
	if !(peer[0].Instant.Mono <= trace[0].Instant.Mono && trace[1].Instant.Mono <= peer[1].Instant.Mono) {
		t.Errorf("peer does not bracket trace: peer=[%d,%d] trace=[%d,%d]",
			peer[0].Instant.Mono, peer[1].Instant.Mono, trace[0].Instant.Mono, trace[1].Instant.Mono)
	}
}

// TestActionEnvelopeSpansSteps covers the two-step action envelope and the
// nesting the verifier derives from it.
func TestActionEnvelopeSpansSteps(t *testing.T) {
	dir := t.TempDir()
	run := RunIdentity{BucketID: "b1", Stage2: "sha256:test"}
	if _, err := BeginAction(dir, run, 30*time.Second); err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	st, err := LoadActionState(dir)
	if err != nil {
		t.Fatalf("LoadActionState: %v", err)
	}
	ident := st.Containment
	if _, err := Exec(ExecOptions{
		Level: LevelScript, Dir: dir, Run: run, Argv: []string{"sh", "-c", "sleep 0.02"}, Cwd: dir,
		Parent: &ident, Timeout: 30 * time.Second,
	}); err != nil {
		t.Fatalf("Exec(script): %v", err)
	}
	if _, err := EndAction(dir, TerminalPassed, ""); err != nil {
		t.Fatalf("EndAction: %v", err)
	}

	v, err := VerifyDir(VerifyOptions{Dir: dir})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	for _, f := range v.Findings {
		if f.Severity == SeverityTerminal {
			t.Errorf("terminal finding: %s %s", f.Code, f.Detail)
		}
	}
	if v.ActionNs <= v.ScriptNs {
		t.Errorf("action %d ns must strictly contain script %d ns", v.ActionNs, v.ScriptNs)
	}
	// The partition must name the bootstrap phase the contract names, and the
	// phases must sum exactly to the envelope.
	var total int64
	sawBootstrap := false
	for _, p := range v.Phases {
		if p.Parent != "action" {
			continue
		}
		total += p.Duration()
		if p.ComponentID == "action_containment_bootstrap" {
			sawBootstrap = true
		}
	}
	if !sawBootstrap {
		t.Errorf("the action partition has no action_containment_bootstrap phase")
	}
	if total != v.ActionNs {
		t.Errorf("action phases total %d ns, envelope is %d ns", total, v.ActionNs)
	}
}

// TestRecordsAreTamperEvident proves the hash chain does the job it is there
// for: an edited duration cannot pass verification.
func TestRecordsAreTamperEvident(t *testing.T) {
	dir := t.TempDir()
	if _, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: dir, Argv: []string{"sh", "-c", "true"}, Cwd: dir,
		Timeout: 30 * time.Second,
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	path := filepath.Join(dir, streamName(ProducerPhysical, LevelInvocation, 0))
	recs, err := ReadRecords(path)
	if err != nil {
		t.Fatalf("ReadRecords: %v", err)
	}
	// Shave 10 seconds off the measured envelope, exactly as a tamperer would.
	for i := range recs {
		if recs[i].Boundary == "end" {
			recs[i].Instant.Mono -= Nanos(10 * time.Second)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	v, err := VerifyDir(VerifyOptions{Dir: dir})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	if v.Complete {
		t.Errorf("a rewritten record verified as complete")
	}
	found := false
	for _, f := range v.Findings {
		if f.Code == "WT-002" {
			found = true
		}
	}
	if !found {
		t.Errorf("no WT-002 chain finding for a rewritten record: %+v", v.Findings)
	}
}

// TestExecPassesTheChildOutputThrough is a regression test with a scar: a
// wrapper that swallows the test log is worse than no wrapper, and a nil
// *os.File stored in an io.Writer is a non-nil interface holding a nil
// pointer, so the obvious nil check does not catch it.
func TestExecPassesTheChildOutputThrough(t *testing.T) {
	dir := t.TempDir()
	out, err := os.CreateTemp(dir, "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if _, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: dir, Cwd: dir, Timeout: 30 * time.Second,
		Argv:   []string{"sh", "-c", "echo the-test-log; echo the-error-log >&2"},
		Stdout: out, Stderr: out,
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	b, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"the-test-log", "the-error-log"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("the child's output did not reach the caller: wanted %q in %q", want, b)
		}
	}
}
