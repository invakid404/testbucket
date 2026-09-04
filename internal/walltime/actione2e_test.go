package walltime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTheActionLifecycleProducesCompleteRecords is the F3 end-to-end
// regression, and it is the test whose absence let F3 ship.
//
// The unit tests around this all built their streams by hand, so none of them
// went through the production `streamName`/`openActionChild` route — and that
// route gave the action envelope (sequence 0) and every action-owned child
// (sequence 1 and up) THE SAME FILE. The writer resumes a file-wide chain
// while the verifier groups records by producer, level and sequence, so the
// child's records began with the envelope's chain state and were then read as
// a stream of their own: a terminal WT-002 on every action that runs a setup
// or bucket command, which is every measured action.
//
// This drives the real lifecycle — begin, an action-owned child, end — and
// then verifies the directory the way `wall verify --require complete` does.
func TestTheActionLifecycleProducesCompleteRecords(t *testing.T) {
	dir := shortTempDir(t)
	run := RunIdentity{
		CampaignID: "ewj2", RunID: "e2e-1", AttemptID: "1", BucketID: "bucket-0",
		Repository: "invakid404/testbucket", WorkflowRun: "100", Job: "test",
		Step: "run-bucket", StepAttempt: "1",
		Stage1: "sha256:ef24c98b6f6843d9d586189733598c533de9fa109464aa1d7045c667a4621b0f", Stage2: "sha256:5e585fd3fab5cb85a941179b4df835cef988f0281af9f47878024f539c302df5",
		ComponentRegistry: "sha256:872491a30d60d598962de6e7b834ab76b2aa65fbab102c6ebaaae6acdc238822", VerifierID: "ewj2-verifier",
	}
	runKey := mustSigningKey()
	t.Setenv(RunKeyEnv, EncodeKey(runKey))

	st, err := BeginAction(dir, run, time.Minute)
	if err != nil {
		t.Fatalf("BeginAction: %v", err)
	}
	// The measured step holds the delegate and not the run key — the
	// production shape — so the action-owned child's signer is registered
	// through the delegation rather than by a party that cannot be there.
	t.Setenv(RunKeyEnv, "")
	t.Setenv(SignerDelegateKeyEnv, st.SignerDelegate)

	for _, argv := range [][]string{{"true"}, {"true"}} {
		if code, err := RunInAction(dir, argv, "", nil, nil); err != nil || code != 0 {
			t.Fatalf("RunInAction%v: code=%d err=%v", argv, code, err)
		}
	}

	t.Setenv(RunKeyEnv, EncodeKey(runKey))
	if _, err := EndAction(dir, TerminalPassed, ""); err != nil {
		t.Fatalf("EndAction: %v", err)
	}

	// EVERY LOGICAL IDENTITY HAS ITS OWN FILE. Two action-owned children plus
	// the envelope is three physical wrapper streams, not one.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	physical := map[string]bool{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), string(ProducerPhysical)+"."+string(LevelAction)) {
			physical[e.Name()] = true
		}
	}
	if len(physical) != 3 {
		t.Errorf("the action envelope and its two children occupy %d physical stream(s) %v; one file per logical identity is what keeps their chains apart",
			len(physical), physical)
	}

	// AND THE RECORDS VERIFY. This is what `wall verify --require complete`
	// asks, and it is what a per-file chain split by sequence cannot satisfy.
	recs, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := &Verdict{}
	verifyChains(v, groupStreams(recs))
	for _, f := range v.Findings {
		if f.Code == "WT-002" {
			t.Errorf("the production lifecycle produced a broken chain: %s", f.Detail)
		}
	}
	verdict, err := VerifyDir(VerifyOptions{Dir: dir, SignerKeys: []string{PublicKeyOf(runKey)}})
	if err != nil {
		t.Fatalf("VerifyDir: %v", err)
	}
	for _, f := range verdict.Findings {
		if f.Code == "WT-002" || f.Code == "WT-020" {
			t.Errorf("wall verify reports %s/%s: %s", f.Code, f.Severity, f.Detail)
		}
	}
	if !verdict.Complete {
		var why []string
		for _, f := range verdict.Findings {
			if f.Severity == SeverityTerminal {
				why = append(why, f.Code+": "+f.Detail)
			}
		}
		t.Errorf("the records of a real begin/run/end lifecycle are not complete: %v", why)
	}
}

// TestEveryLogicalStreamIdentityHasItsOwnFile states the naming rule directly,
// because it is the one place a future level can acquire sequences and be
// forgotten.
func TestEveryLogicalStreamIdentityHasItsOwnFile(t *testing.T) {
	seen := map[string]string{}
	for _, p := range []Producer{ProducerPhysical, ProducerPeer, ProducerTrace} {
		for _, l := range []Level{LevelAction, LevelScript, LevelInvocation} {
			for _, seq := range []int{0, 1, 2, 42} {
				name := streamName(p, l, seq)
				id := string(p) + "/" + string(l) + "#" + itoa(seq)
				if other, clash := seen[name]; clash {
					t.Errorf("%s and %s share the ledger %q", other, id, name)
				}
				seen[name] = id
				if !strings.Contains(name, "."+pad3(seq)+".") {
					t.Errorf("%s does not carry its sequence: %q", id, name)
				}
			}
		}
	}
	// And the production child path takes a sequence the envelope never uses.
	body := productionFunc(t, "action.go", "func openActionChild(")
	if !strings.Contains(body, "streamName(ProducerPhysical, LevelAction, seq)") ||
		!strings.Contains(body, "actionChildSeq(dir) + 1") {
		t.Error("the action-child ledger does not take its own sequence-named file")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func pad3(n int) string {
	s := itoa(n)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

var _ = filepath.Join
