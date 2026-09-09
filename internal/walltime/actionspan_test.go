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
