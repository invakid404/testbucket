package walltime

// What is left of this file is the ACTION SPAN itself.
//
// Its other tests proved properties of the retained-evidence machinery — that
// the four step processes' readings were taken by different wrappers, that the
// closer stood outside the containment it drained, that each action-owned
// child record carried its own proof, and that two ledgers never shared one
// chain. That machinery is gone, and an assertion about it would now be an
// assertion about nothing. The span it all existed to establish is not gone,
// so it is checked here directly, on the records.

import (
	"testing"
	"time"
)

// TestTheActionEnvelopeSpansItsSteps is the COMPLETE SPAN property, asserted
// on the records themselves.
//
// `wall begin`, the measured script and `wall end` are three separate step
// processes, so no process spans an action — which is exactly why the span has
// to be checkable from the evidence rather than from a call stack. An action
// envelope that did not contain the script it bracketed would attribute the
// script's time to a lifecycle that was not open when it ran.
func TestTheActionEnvelopeSpansItsSteps(t *testing.T) {
	dir := t.TempDir()
	run := RunIdentity{BucketID: "b1", Stage2: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}
	if _, err := BeginAction(dir, run, 30*time.Second); err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	if _, err := Exec(ExecOptions{
		Level: LevelScript, Dir: dir, Run: run, Argv: []string{"sh", "-c", "sleep 0.02"}, Cwd: dir,
		Timeout: 30 * time.Second,
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

	recs, err := ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	bound := func(level Level, boundary string) (Nanos, bool) {
		for _, r := range recs {
			if r.Kind == "boundary" && r.Level == level && r.Boundary == boundary {
				return r.Instant.Mono, true
			}
		}
		return 0, false
	}
	actionStart, ok1 := bound(LevelAction, "start")
	actionEnd, ok2 := bound(LevelAction, "end")
	scriptStart, ok3 := bound(LevelScript, "start")
	scriptEnd, ok4 := bound(LevelScript, "end")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		t.Fatalf("missing boundary records: action=[%v,%v] script=[%v,%v]", ok1, ok2, ok3, ok4)
	}
	if !(actionStart <= scriptStart && scriptEnd <= actionEnd) {
		t.Errorf("the action envelope [%d,%d] does not span the script [%d,%d]",
			actionStart, actionEnd, scriptStart, scriptEnd)
	}
	if !(actionEnd-actionStart > scriptEnd-scriptStart) {
		t.Errorf("action %d ns must strictly contain script %d ns",
			actionEnd-actionStart, scriptEnd-scriptStart)
	}
}

// TestTheComposedActionDerivesEverySpanFromItsOwnBytes is the F5 regression.
//
// The pieces were each testable and each tested; what nothing checked was that
// a composed action PRODUCES the evidence its own contract is stated in. §3.1's
// floor is `A >= setup_ns + script_ns`, and `setup_ns` was not derivable from
// the bytes at all — `wall run` started the setup command and waited, with no
// clock read, no process identity and no bound. Meanwhile the closing record
// claimed in prose that the root had been reaped while carrying no `proc`
// object, so QC7a could not be satisfied by a genuinely emitted record.
//
// This composes begin → setup → script → invocation → end through the
// production entry points and then reads every span and identity back out of
// the produced records, which is the only way to see that.
func TestTheComposedActionDerivesEverySpanFromItsOwnBytes(t *testing.T) {
	dir := t.TempDir()
	run := RunIdentity{BucketID: "b1", Stage2: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}
	if _, err := BeginAction(dir, run, 30*time.Second); err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	if code, err := RunInAction(dir, []string{"sh", "-c", "sleep 0.02"}, dir, nil, nil); err != nil || code != 0 {
		t.Fatalf("RunInAction: code=%d err=%v", code, err)
	}
	if _, err := Exec(ExecOptions{
		Level: LevelScript, Dir: dir, Run: run, Cwd: dir, Timeout: 30 * time.Second,
		Argv: []string{"sh", "-c", "sleep 0.02"},
	}); err != nil {
		t.Fatalf("Exec(script): %v", err)
	}
	if _, err := Exec(ExecOptions{
		Level: LevelInvocation, Dir: dir, Run: run, Cwd: dir, Timeout: 30 * time.Second,
		Argv: []string{"sh", "-c", "true"},
	}); err != nil {
		t.Fatalf("Exec(invocation): %v", err)
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

	// Every span comes from the bytes, not from anything asserted beside them.
	if v.SetupNs <= 0 {
		t.Error("setup_ns is not derivable from the produced records; §3.1's floor has a term with no evidence")
	}
	if v.ScriptNs <= 0 {
		t.Errorf("script_ns = %d", v.ScriptNs)
	}
	if len(v.InvocationNs) != 1 || v.InvocationNs[0] <= 0 {
		t.Errorf("invocation spans = %v, want one positive span", v.InvocationNs)
	}
	if v.ActionNs <= 0 {
		t.Errorf("action_ns = %d", v.ActionNs)
	}
	// §3.1's floor, checked on the derived terms.
	if floor := v.SetupNs + v.ScriptNs; v.ActionNs < floor {
		t.Errorf("§3.1: A (%d) < setup_ns + script_ns (%d)", v.ActionNs, floor)
	}

	// And every closing record that claims a reaped root carries the identity
	// of the process it reaped.
	recs, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, r := range recs {
		if r.Kind != "boundary" || r.Boundary != "end" {
			continue
		}
		if r.Level != LevelScript && r.Level != LevelInvocation && r.Level != LevelSetup {
			continue
		}
		seen++
		if r.Proc.PID <= 0 {
			t.Errorf("%s closing record carries no process identity, while its note claims the root was reaped", r.Level)
		}
	}
	if seen < 3 {
		t.Errorf("saw %d measured closing records, want the setup, the script and the invocation", seen)
	}
}
