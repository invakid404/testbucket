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
	fd := 0
	fmt.Sscanf(flags["--key-fd"], "%d", &fd)
	key, err := ReadKeyFD(fd)
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
		Parent: &ident, JoinParent: true, Timeout: 30 * time.Second,
	}); err != nil {
		t.Fatalf("Exec(script): %v", err)
	}
	// The handoff a script leaves for its invocation wrappers must not outlive
	// it: nesting under a containment that no longer exists would attribute an
	// invocation to a lifecycle that had already closed.
	if _, ok, _ := ScriptContainment(dir); ok {
		t.Errorf("the script containment handoff outlived the script")
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

// TestScriptPublishesItsContainmentWhileItRuns: an invocation wrapper is a
// separate process and has to find the containment it must nest inside.
// Nesting is not decoration — an invocation containment created BESIDE the
// script's would take the invocation's processes out of the lifecycle the
// script's trace claims to bracket.
//
// It is published ONLY when it can be authenticated. A script wrapper that
// holds the run key writes a signed handoff; one that does not writes nothing,
// because a file in a directory the measured script can write, with no
// signature on it, is not a handoff — it is whatever the workload last put
// there. In that configuration the invocation wrapper asks the kernel which
// cgroup it is already in instead, which is a fact the workload cannot forge.
func TestScriptPublishesItsContainmentWhileItRuns(t *testing.T) {
	probeScript := func(dir string) []string {
		return []string{"sh", "-c", "cat " + filepath.Join(dir, "script-containment.json") +
			" > " + filepath.Join(dir, "seen.json") + " 2>/dev/null; true"}
	}

	// WITH a run key: signed, and it names the containment the script recorded.
	dir := t.TempDir()
	t.Setenv(RunKeyEnv, EncodeKey(mustSigningKey()))
	if _, err := Exec(ExecOptions{
		Level: LevelScript, Dir: dir, Cwd: dir, Timeout: 30 * time.Second,
		Argv: probeScript(dir),
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "seen.json"))
	if err != nil {
		t.Fatalf("the script could not read its own containment handoff: %v", err)
	}
	var handoff ScriptHandoff
	if err := json.Unmarshal(b, &handoff); err != nil {
		t.Fatalf("the handoff is not a script handoff document: %v", err)
	}
	if handoff.Signature == nil {
		t.Error("the handoff is unsigned; a measured script sharing this directory could have written it")
	}
	recs, err := ReadRecords(filepath.Join(dir, streamName(ProducerPhysical, LevelScript, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if seen := handoff.Containment; !seen.Same(recs[0].Containment) {
		t.Errorf("the handoff names %s/%s but the script recorded %s/%s",
			seen.ID, seen.Inode, recs[0].Containment.ID, recs[0].Containment.Inode)
	}

	// WITHOUT one — the production shape, since the run key is scoped to the
	// envelope steps — nothing is published at all.
	bare := t.TempDir()
	t.Setenv(RunKeyEnv, "")
	if _, err := Exec(ExecOptions{
		Level: LevelScript, Dir: bare, Cwd: bare, Timeout: 30 * time.Second,
		Argv: probeScript(bare),
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if _, err := os.Stat(scriptHandoffPath(bare)); !os.IsNotExist(err) {
		t.Errorf("an unauthenticatable handoff was published anyway (stat err=%v); the measured work could rewrite it and choose its own enclosing containment", err)
	}
}

// TestEnvelopeCoversEveryWrapperOwnedOperation is the endpoint-coverage test.
//
// The contract puts A from the FIRST action-owned operation through the FINAL
// epilogue, and the tempting shortcut is to open the envelope after the
// wrapper's own setup and close it before its own cleanup — which reports an
// action shorter than the one that ran. Timings cannot catch that: the setup
// costs tens of microseconds. The probes below inspect the observable state at
// the instant of each reading instead, so a reading that moves after the setup
// finds a directory that already exists and fails here.
func TestEnvelopeCoversEveryWrapperOwnedOperation(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "records")

	var startState, endState error
	atStartReading = func(d string) {
		if _, err := os.Stat(d); err == nil {
			startState = fmt.Errorf("the records directory already existed when the envelope opened: creating it is action-owned work and belongs INSIDE A")
			return
		}
		if entries, err := os.ReadDir(parent); err == nil && len(entries) != 0 {
			startState = fmt.Errorf("the wrapper had already written %d file(s) when the envelope opened", len(entries))
		}
	}
	atEndReading = func(d string) {
		if _, err := os.Stat(filepath.Join(d, actionStateFile)); err == nil {
			endState = fmt.Errorf("the action-state handoff still existed when the envelope closed: removing it is epilogue work and belongs INSIDE A")
		}
	}
	t.Cleanup(func() { atStartReading, atEndReading = nil, nil })

	run := RunIdentity{BucketID: "b1", Stage2: "sha256:test"}
	if _, err := BeginAction(dir, run, 30*time.Second); err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	if startState != nil {
		t.Errorf("BeginAction: %v", startState)
	}
	if _, err := EndAction(dir, TerminalPassed, ""); err != nil {
		t.Fatalf("EndAction: %v", err)
	}
	if endState != nil {
		t.Errorf("EndAction: %v", endState)
	}

	// The same rule for a command envelope: the reading precedes the signing
	// key, the writer and the stream file.
	execDir := filepath.Join(parent, "exec-records")
	startState = nil
	atStartReading = func(d string) {
		if _, err := os.Stat(filepath.Join(d, streamName(ProducerPhysical, LevelInvocation, 0))); err == nil {
			startState = fmt.Errorf("the physical stream already existed when the envelope opened")
		}
	}
	if _, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: execDir, Cwd: parent, Timeout: 30 * time.Second,
		Argv: []string{"sh", "-c", "true"},
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if startState != nil {
		t.Errorf("Exec: %v", startState)
	}
}

// TestEnvelopeBracketsTheWholeCall is the coarse companion: whatever else
// happens, A must lie inside the wall-clock window of the call that produced
// it. It is a sanity bound, not the coverage proof above.
func TestEnvelopeBracketsTheWholeCall(t *testing.T) {
	dir := t.TempDir()
	clock := NewSystemClock()
	before := clock.Now()
	if _, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: dir, Cwd: dir, Timeout: 30 * time.Second,
		Argv: []string{"sh", "-c", "true"},
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	after := clock.Now()
	recs, err := ReadRecords(filepath.Join(dir, streamName(ProducerPhysical, LevelInvocation, 0)))
	if err != nil {
		t.Fatal(err)
	}
	var start, end Nanos
	for _, r := range recs {
		switch r.Boundary {
		case "start":
			start = r.Instant.Mono
		case "end":
			end = r.Instant.Mono
		}
	}
	if start < before.Mono || end > after.Mono {
		t.Errorf("the envelope [%d,%d] is not inside the call [%d,%d]", start, end, before.Mono, after.Mono)
	}
}

// TestRunInActionJoinsBeforeItSpawns is the property the composite action's
// bucket step depends on.
//
// A process can move ITSELF into a containment; it cannot move a sibling that
// is already running. So everything that must be inside the action containment
// has to be a descendant of a process that joined first — which is why the
// bucket step's preparation runs inside `wall run` rather than beside it. If
// the join moved after the spawn, the child would inherit the wrong cgroup and
// the peer and the trace would never see that work.
func TestRunInActionJoinsBeforeItSpawns(t *testing.T) {
	dir := t.TempDir()
	run := RunIdentity{BucketID: "b1", Stage2: "sha256:test"}
	if _, err := BeginAction(dir, run, 30*time.Second); err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	t.Cleanup(func() {
		atContainmentJoin = nil
		_, _ = EndAction(dir, TerminalPassed, "")
	})

	joined := false
	marker := filepath.Join(dir, "child-ran")
	atContainmentJoin = func(string) {
		joined = true
		// The child has not run yet: the join must come first.
		if _, err := os.Stat(marker); err == nil {
			t.Errorf("the child had already run when this process joined the containment")
		}
	}

	code, err := RunInAction(dir, []string{"sh", "-c", "touch " + marker}, dir, nil, nil)
	if err != nil {
		t.Fatalf("RunInAction: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !joined {
		t.Errorf("RunInAction never joined the action containment")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the admitted command did not run: %v", err)
	}

	// And it propagates a failure: the bucket step takes its status from here,
	// so a swallowed non-zero would make a red bucket look green. The probe is
	// cleared first — the marker from the run above exists now, and the
	// ordering it checks has already been established.
	atContainmentJoin = nil
	code, err = RunInAction(dir, []string{"sh", "-c", "exit 7"}, dir, nil, nil)
	if err != nil {
		t.Fatalf("RunInAction: %v", err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

// TestBootstrapFailureIsRetained is the retention rule for setup that never
// got off the ground.
//
// A bootstrap that dies without a record is indistinguishable from an action
// that never started — and "never started" is exactly what a campaign would
// have to assume about a row that simply is not there. The contract says a
// failed setup stays in the ledger with its reason, so it does.
func TestBootstrapFailureIsRetained(t *testing.T) {
	dir := t.TempDir()
	run := RunIdentity{BucketID: "b1", Stage2: "sha256:test"}

	// Make observer startup fail: the launcher is the seam production uses to
	// spawn them, so this is the real failure path, not a simulated one.
	original := ObserverLauncher
	ObserverLauncher = func([]string) (*exec.Cmd, error) {
		return nil, fmt.Errorf("no observer binary on this host")
	}
	t.Cleanup(func() { ObserverLauncher = original })

	if _, err := BeginAction(dir, run, 10*time.Second); err == nil {
		t.Fatalf("BeginAction succeeded with no observer")
	}

	recs, err := ReadRecords(filepath.Join(dir, streamName(ProducerPhysical, LevelAction, 0)))
	if err != nil {
		t.Fatalf("the failed bootstrap left no records at all: %v", err)
	}
	var start, terminal *Record
	for i := range recs {
		switch recs[i].Kind {
		case "boundary":
			if recs[i].Boundary == "start" {
				start = &recs[i]
			}
		case "terminal":
			terminal = &recs[i]
		}
	}
	if start == nil {
		t.Errorf("the envelope's opening reading was not retained")
	}
	if terminal == nil {
		t.Fatalf("the bootstrap failure was not retained as a terminal record: %+v", recs)
	}
	if terminal.Terminal != TerminalWrapperError {
		t.Errorf("terminal state = %q, want %q", terminal.Terminal, TerminalWrapperError)
	}
	if !strings.Contains(terminal.Reason, "no observer binary") {
		t.Errorf("the terminal record does not say what failed: %q", terminal.Reason)
	}

	// And the verifier refuses to score it while retaining it.
	v, err := VerifyDir(VerifyOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if v.Eligible {
		t.Errorf("a failed bootstrap was scorable")
	}
}

// AT_start is read before the records directory, the signing key and the
// writer exist. Everything after that reading is action time, so a failure in
// that window must still leave a terminal record: an action that died during
// bootstrap and an action that never started are different facts, and a
// campaign retains the first.
//
// The window has no writer yet, so the retention path mints its own. That is
// the only thing that can be proved here — a real failure of the key or the
// stream cannot be provoked on a working filesystem — so the failure is
// injected at exactly the point the ledger is still empty.
func TestAPreWriterBootstrapFailureIsRetained(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "records")
	injected := fmt.Errorf("the runner's disk filled between the reading and the writer")

	original := atRecordsDir
	atRecordsDir = func(string) error { return injected }
	t.Cleanup(func() { atRecordsDir = original })

	run := RunIdentity{CampaignID: "ewj2", RunID: "r1", BucketID: "bucket-1", Stage2: "sha256:test"}
	if _, err := BeginAction(dir, run, time.Minute); err == nil {
		t.Fatalf("an injected bootstrap failure was not reported")
	} else if !strings.Contains(err.Error(), injected.Error()) {
		t.Errorf("error %q does not carry the cause", err)
	}

	recs, err := ReadDir(dir)
	if err != nil {
		t.Fatalf("the failure left no readable ledger: %v", err)
	}
	if len(recs) == 0 {
		t.Fatalf("a bootstrap failure after AT_start left nothing in the ledger, so it cannot be told from an action that never started")
	}
	var terminal *Record
	for i, r := range recs {
		if r.Kind == "terminal" {
			terminal = &recs[i]
		}
	}
	if terminal == nil {
		t.Fatalf("the ledger holds %d record(s) but none is terminal", len(recs))
	}
	if terminal.Terminal != TerminalWrapperError {
		t.Errorf("the retained record is %q, want %q", terminal.Terminal, TerminalWrapperError)
	}
	if !strings.Contains(terminal.Reason, injected.Error()) {
		t.Errorf("the retained reason %q does not say what failed", terminal.Reason)
	}
	if terminal.Run.RunID != run.RunID || terminal.Run.BucketID != run.BucketID {
		t.Errorf("the retained record names run %+v, not the one that failed", terminal.Run)
	}
	// And it must be readable as an INCOMPLETE measurement rather than as a
	// well-formed one: a retained failure is evidence, not a passing row.
	v, err := VerifyDir(VerifyOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if v.Eligible {
		t.Errorf("a bootstrap failure was scored")
	}
}

// The run key is a STEP capability, and the action-level observers are
// deliberately detached so they outlive the step that started them and span
// the measured work. An inherited key would therefore sit in a live process
// for exactly the window a hostile same-uid workload is running in, readable
// through /proc/<pid>/environ — and recovering it buys the ability to sign
// replacement rosters and closing seals, which is a forged attestation that
// verifies rather than an ineligible row.
//
// The YAML step-binding test cannot see this: it scans the action's own step
// bodies and knows nothing about what a child inherits. This checks the
// environment the observer is actually launched with.
func TestObserversNeverInheritAWallTimeSecret(t *testing.T) {
	// Every secret is set, so the test fails if a new one is added to the
	// scrub list without being covered here — and fails if any is missed.
	//
	// The run key carries a REAL encoded key rather than a sentinel string,
	// because the script wrapper now signs its containment handoff with it and
	// an undecodable key is a loud failure by design. What is under test here
	// is that the value does not reach the observer, and a well-formed key
	// proves that exactly as well as a malformed one.
	for _, name := range WallTimeSecretEnv {
		t.Setenv(name, "secret-value-for-"+name)
	}
	t.Setenv(RunKeyEnv, EncodeKey(mustSigningKey()))
	// An ambient variable the observer legitimately needs, to prove the scrub
	// is a denylist and not a wholesale drop.
	t.Setenv("TB_WALLTIME_TEST_AMBIENT", "must-survive")

	original := ObserverLauncher
	var launched []*exec.Cmd
	ObserverLauncher = func(args []string) (*exec.Cmd, error) {
		cmd, err := original(args)
		if err != nil {
			return nil, err
		}
		launched = append(launched, cmd)
		return cmd, nil
	}
	t.Cleanup(func() { ObserverLauncher = original })

	// A SHORT records directory. The script level now opens a unix socket in
	// it for the invocation controller, and a socket path is bounded by the
	// kernel at ~104 bytes; the default per-test temporary directory is longer
	// than that on this platform, which would fail the envelope for a reason
	// this test is not about.
	dir := shortTempDir(t)
	run := RunIdentity{CampaignID: "ewj2", RunID: "r1", BucketID: "bucket-1", Stage2: "sha256:test"}
	// Both levels: the action level detaches, the script level does not, and
	// neither may carry a secret.
	if _, err := Exec(ExecOptions{
		Level: LevelScript, Dir: dir, Run: run, Timeout: time.Minute,
		Argv: []string{"true"},
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if len(launched) == 0 {
		t.Fatalf("no observer was launched, so this test proved nothing")
	}

	for _, cmd := range launched {
		if cmd.Env == nil {
			t.Errorf("an observer was launched with a nil Env, which inherits this process's whole environment including every secret")
			continue
		}
		got := map[string]string{}
		for _, kv := range cmd.Env {
			k, v, _ := strings.Cut(kv, "=")
			got[k] = v
		}
		for _, name := range WallTimeSecretEnv {
			if v, present := got[name]; present {
				t.Errorf("an observer inherits %s=%q; a detached observer holding it exposes the key for the whole measured window", name, v)
			}
		}
		if got["TB_WALLTIME_TEST_AMBIENT"] != "must-survive" {
			t.Errorf("the scrub dropped an ambient variable the observer needs; it is a denylist, not an allowlist")
		}
	}
}

// TestDetachedActionObserversAreScrubbed drives the OTHER launch path.
//
// The test above runs `Exec`, whose script and invocation observers are waited
// children. `BeginAction` launches the ACTION peer and trace, and those are
// detached so they can span the begin and end steps — which is precisely why a
// capability leaked to them is the worst case: it lives for the whole measured
// window rather than for one invocation. That path had no direct coverage, and
// the builder-key leak was reported against exactly these two processes.
func TestDetachedActionObserversAreScrubbed(t *testing.T) {
	for _, name := range WallTimeSecretEnv {
		t.Setenv(name, "detached-secret-for-"+name)
	}
	// BeginAction parses the run key while launching, so it needs a real one.
	t.Setenv(RunKeyEnv, EncodeKey(mustSigningKey()))
	t.Setenv("TB_WALL_CGROUP_ROOT", "/required/nonsecret/cgroup-root")
	t.Setenv("TB_WALLTIME_DETACHED_AMBIENT", "must-survive")

	original := ObserverLauncher
	var launched []*exec.Cmd
	ObserverLauncher = func(args []string) (*exec.Cmd, error) {
		cmd, err := original(args)
		if err == nil {
			launched = append(launched, cmd)
		}
		return cmd, err
	}
	t.Cleanup(func() { ObserverLauncher = original })

	dir := t.TempDir()
	run := RunIdentity{CampaignID: "ewj2", RunID: "detached", BucketID: "bucket-0", Stage2: "sha256:stage2"}
	if _, err := BeginAction(dir, run, 20*time.Second); err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	if len(launched) != 2 {
		t.Fatalf("BeginAction launched %d observer(s), want the action peer and trace", len(launched))
	}
	for i, cmd := range launched {
		// Detached, which is the property that makes the leak durable.
		if cmd.SysProcAttr == nil {
			t.Errorf("action observer %d is not on the detached production path", i)
		}
		if cmd.Env == nil {
			t.Errorf("action observer %d has a nil Env and would inherit every capability", i)
			continue
		}
		got := map[string]string{}
		for _, kv := range cmd.Env {
			k, v, _ := strings.Cut(kv, "=")
			got[k] = v
		}
		for _, name := range WallTimeSecretEnv {
			if v, present := got[name]; present {
				t.Errorf("action observer %d inherits %s=%q for the whole measured window", i, name, v)
			}
		}
		if got["TB_WALL_CGROUP_ROOT"] != "/required/nonsecret/cgroup-root" {
			t.Errorf("action observer %d lost the cgroup root it needs", i)
		}
		if got["TB_WALLTIME_DETACHED_AMBIENT"] != "must-survive" {
			t.Errorf("action observer %d lost an ambient nonsecret value; the scrub is a denylist", i)
		}
	}
	if _, err := EndAction(dir, TerminalPassed, ""); err != nil {
		t.Fatalf("EndAction: %v", err)
	}
}

// scrubSecrets is the mechanism itself, checked directly: nil means inherit,
// which is the default that caused the defect.
func TestScrubSecretsResolvesNilAndRemovesEverySecret(t *testing.T) {
	for _, name := range WallTimeSecretEnv {
		t.Setenv(name, "leaked")
	}
	t.Setenv("TB_WALLTIME_TEST_KEEP", "kept")

	for _, tc := range []struct {
		name string
		in   []string
	}{
		{name: "nil inherits, and must still be scrubbed", in: nil},
		{name: "an explicit environment is scrubbed in place", in: append(os.Environ(), "TB_EXTRA=1")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := scrubSecrets(tc.in)
			if out == nil {
				t.Fatalf("scrubSecrets returned nil, which the child would resolve to the full environment")
			}
			seen := map[string]bool{}
			for _, kv := range out {
				k, _, _ := strings.Cut(kv, "=")
				seen[k] = true
			}
			for _, name := range WallTimeSecretEnv {
				if seen[name] {
					t.Errorf("%s survived the scrub", name)
				}
			}
			if !seen["TB_WALLTIME_TEST_KEEP"] {
				t.Errorf("a non-secret variable did not survive the scrub")
			}
		})
	}
}
