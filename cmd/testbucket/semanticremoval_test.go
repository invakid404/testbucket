package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/planbind"
)

// TestNoProhibitedProofSymbolSurvives is the SEMANTIC half of the governed
// boundary.
//
// Path accounting — all 49 SIMPLIFY paths present, all 108 REMOVE paths absent
// — passed while the proof protocols were still compiled, exported and, in
// several cases, executing inside those retained paths. Presence is not
// reduction, so this reads the source for the identifiers the component map
// classifies REMOVE rather than the file list.
//
// It scans declarations, not mentions: a comment recording that something was
// removed must not make this fail, or the removal could never be explained.
func TestNoProhibitedProofSymbolSurvives(t *testing.T) {
	prohibited := []struct{ decl, why string }{
		{"type FrozenInputs struct", "Stage-frozen Vitest replay inputs"},
		{"type Scorer struct", "the sealed static-feature scorer"},
		{"type TrainingLineageID struct", "the sealed training receipt lineage"},
		{"type PcheckDocument struct", "the sealed allocation re-derivation"},
		{"func BuildPcheck(", "the sealed allocation re-derivation"},
		{"type Reconciliation struct", "the peer/trace reconciliation schema"},
		{"type CampaignRun struct", "the retired campaign-gate schema"},
		{"type CampaignPair struct", "the retired campaign-gate schema"},
		{"func EvaluateCampaign(", "the retired campaign decision rule"},
		{"type Role string", "the three-ledger role model"},
		{"type Allocator struct", "the frozen-scorer allocation adapter"},
		// The Aeta registry proof. These were dead AND exported: nothing
		// referenced them, so an internal caller could still construct a
		// registry-shaped document from a package that had "removed" it.
		{"type ComponentClass ", "the Aeta registry's component classes"},
		{"type Component struct", "the Aeta registry's component template"},
		{"type AetaInputs struct", "the Aeta registry's frozen inputs"},
		{"type InstantiatedComponent struct", "the per-bucket Aeta instantiation"},
		{"type AetaInstance struct", "the per-bucket Aeta instance"},
		// The frozen planner's option struct and its claim-store channels,
		// which outlived the planner they configured.
		{"type frozenPlanOptions struct", "the frozen planner's options"},
		{"func machineClaimStore(", "the one-shot planner claim store"},
		// The containment evidence/control schema. The practical runner owns a
		// process group directly; this interface promised admission,
		// membership snapshots, whole-container signalling, verified emptiness
		// and destroy, and had no caller on the shipped path.
		{"type Containment interface", "the containment evidence/control schema"},
		{"func NewContainment(", "the containment factory"},
		{"type ContainmentIdentity struct", "the containment identity schema"},
		{"type processGroup struct", "the containment wrapper over a pgid"},
		{"func newProcessGroupContainment(", "the unscored containment fallback"},
		{"func awaitChild(", "the disconnected cancellation policy"},
		{"func membershipSnapshot(", "the cgroup-membership snapshot"},
		{"func reapExitedChild(", "the observer reaper"},
		{"func rememberObserver(", "the observer process-handle registry"},
		{"func observerCloseBy(", "the observer closing budget"},
		{"func scriptHandoffPath(", "the script-containment handoff"},
		{"func containmentSysProc(", "the containment spawn attributes"},
		{"func joinContainment(", "the containment join"},
	}
	// Dead identifiers that are not declarations of their own: constants,
	// package-level variables and struct FIELDS. A field is the shape this
	// control has missed twice now — first the Stage-2 slot, then the
	// always-absent containment pointer — so they are listed by name.
	//
	// Each is matched as a line that BEGINS with the identifier after one tab,
	// which is a const-block entry, a var-block entry or a struct field, and
	// never prose: a comment line begins with `//`. The tombstones that record
	// each removal name these identifiers, and must stay legal — a removal
	// nobody can explain in the source is one the next reader undoes.
	prohibitedMembers := []struct{ name, why string }{
		{"ObserverCloseGrace", "the observer closing grace"},
		{"CancellationPolicyID", "the frozen cancellation-policy string"},
		{"observerHandles", "the observer process-handle registry"},
		{"observerCloseGrace", "the observer closing grace"},
		{"PrimitiveCgroup2", "the cgroup-v2 containment primitive"},
		{"PrimitiveProcessGroup", "the unscored containment primitive"},
		{"ScriptHandoffKind", "the script-containment handoff document"},
		{"Parent *ContainmentIdentity", "ExecOptions.Parent"},
		{"JoinParent bool", "ExecOptions.JoinParent"},
		{"Containment *ContainmentIdentity", "Record.Containment"},
		{"Containment ContainmentIdentity", "Record.Containment"},
	}
	// PROHIBITED TEXT, not prohibited tags.
	//
	// This list checked for `json:"stage2_digest,omitempty"` exactly, and the
	// three surviving Stage-2 fields were spelled `json:"stage2_digest"` —
	// so the control passed over the field it existed to catch, and the
	// practical path went on emitting a live empty proof slot. A serialized
	// name is prohibited however its options are spelled, so the tag name is
	// matched without them.
	prohibitedTags := []struct{ name, why string }{
		{"containment", "the containment identity slot"},
		{"stage2_digest", "the Stage-2 binding"},
		{"stage1_digest", "the Stage-1 binding"},
		{"registry_digest", "the Aeta component registry"},
		{"verifier_id", "the delivery-bound verifier identity"},
		{"component_registry_digest", "the Aeta component registry"},
		{"producer_binary", "the producer-binary digest"},
		{"prev_hash", "the record hash chain"},
		{"hash", "the record hash chain"},
		{"signature", "record signatures"},
		{"signer_id", "the signer identity"},
		{"peer_control", "the observer handshake"},
		{"trace_control", "the observer handshake"},
	}
	prohibitedEnv := []struct{ name, why string }{
		{"TB_WALL_PLANNER_CLAIM_STORE", "the one-shot planner claim store"},
		{"TB_WALL_CAMPAIGN_AUTHORITY_KEYS", "the predeclared campaign authority keys"},
		{"TB_CANDIDATE_BINARY_DIGEST", "the attested candidate delivery"},
	}
	prohibitedFields := []struct{ field, why string }{
		{"`json:\"prev_hash\"`", "the record hash chain"},
		{"`json:\"hash\"`", "the record hash chain"},
		{"`json:\"signature,omitempty\"`", "record signatures"},
		{"`json:\"signer_id,omitempty\"`", "the signer identity"},
		{"`json:\"peer_control\"`", "the observer handshake"},
		{"`json:\"trace_control\"`", "the observer handshake"},
		{"`json:\"stage1_digest,omitempty\"`", "the Stage-1 binding"},
		{"`json:\"stage2_digest,omitempty\"`", "the Stage-2 binding"},
		{"`json:\"verifier_id,omitempty\"`", "the delivery-bound verifier identity"},
		{"`json:\"component_registry_digest,omitempty\"`", "the Aeta component registry"},
		{"`json:\"producer_binary,omitempty\"`", "the producer-binary digest"},
	}

	var scanned int
	err := filepath.Walk(filepath.Join("..", ".."), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() {
				switch info.Name() {
				case ".jj", ".git", "node_modules", "testdata", "docs":
					return filepath.SkipDir
				}
			}
			return err
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		src := string(b)
		for _, p := range prohibited {
			if strings.Contains(src, "\n"+p.decl) {
				t.Errorf("%s declares %q — %s is REMOVE-classified", path, p.decl, p.why)
			}
		}
		for _, f := range prohibitedFields {
			if strings.Contains(src, f.field) {
				t.Errorf("%s serializes %s — %s is REMOVE-classified", path, f.field, f.why)
			}
		}
		for _, tag := range prohibitedTags {
			// Any option spelling: `json:"x"`, `json:"x,omitempty"`,
			// `json:"x,string"`. Only the struct-tag form is matched, so a
			// comment naming the removed field stays legal — a removal has to
			// be explainable in the source.
			for _, form := range []string{
				`json:"` + tag.name + `"`,
				`json:"` + tag.name + `,`,
			} {
				if strings.Contains(src, form) {
					t.Errorf("%s serializes %s — %s is REMOVE-classified", path, form, tag.why)
				}
			}
		}
		for _, m := range prohibitedMembers {
			if strings.Contains(src, "\n\t"+m.name) {
				t.Errorf("%s declares %s — %s is REMOVE-classified", path, m.name, m.why)
			}
		}
		for _, e := range prohibitedEnv {
			if strings.Contains(src, `"`+e.name+`"`) {
				t.Errorf("%s declares the %s environment channel — %s is REMOVE-classified", path, e.name, e.why)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned == 0 {
		t.Fatal("scanned no Go source; this test is reading the wrong tree")
	}
}

// TestTheLifecycleEmitsNoEmptyProofFields runs the SHIPPED binary and reads
// what it actually writes.
//
// The removal was reported complete while `wall begin` printed
// "action envelope open (containment , )", action-state.json carried
// `"containment":{"primitive":"","id":""}`, `"peer_control":""`,
// `"trace_control":""` and `"root":{}`, and the first physical record carried
// `"prev_hash":""` and `"hash":""`. Empty fields are how a removed model keeps
// teaching itself: a reader sees a slot where a fact belongs and concludes the
// fact was unavailable, not that the concept is gone.
func TestTheLifecycleEmitsNoEmptyProofFields(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	records := filepath.Join(dir, "records")
	if err := os.MkdirAll(records, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		var out strings.Builder
		cmd.Stderr = &out
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
		return out.String()
	}

	begin := run("wall", "begin", "--dir", records, "--bucket-id", "bucket-0",
		"--run-id", "run-1", "--attempt-id", "1")
	end := run("wall", "end", "--dir", records, "--terminal", "passed")

	for name, text := range map[string]string{"wall begin": begin, "wall end": end} {
		if strings.Contains(text, "containment") {
			t.Errorf("%s still reports a containment concept: %q", name, strings.TrimSpace(text))
		}
	}
	// The banners must still identify the envelope; silence is not the fix.
	if !strings.Contains(begin, "bucket-0") || !strings.Contains(end, "bucket-0") {
		t.Errorf("the lifecycle banners no longer name the bucket:\nbegin: %s\nend:   %s", begin, end)
	}

	// The JSONL the run produced.
	entries, err := os.ReadDir(records)
	if err != nil {
		t.Fatal(err)
	}
	prohibited := []string{
		"containment", "peer_control", "trace_control", "root",
		"prev_hash", "hash", "signer_id", "signature",
		"stage1_digest", "stage2_digest", "verifier_id",
		"component_registry_digest", "producer_binary", "role",
	}
	var checked int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(records, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(line), &doc); err != nil {
				t.Fatalf("%s is not JSON: %v", e.Name(), err)
			}
			checked++
			for _, key := range prohibited {
				if _, present := doc[key]; present {
					t.Errorf("%s emits %q: %s", e.Name(), key, line)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("the lifecycle wrote no documents to inspect")
	}
	// action-state.json is removed by `wall end`, so it is read while open.
	t.Run("the action-state handoff carries no proof fields", func(t *testing.T) {
		dir := t.TempDir()
		recs := filepath.Join(dir, "records")
		if err := os.MkdirAll(recs, 0o755); err != nil {
			t.Fatal(err)
		}
		run("wall", "begin", "--dir", recs, "--bucket-id", "bucket-0", "--run-id", "run-1")
		b, err := os.ReadFile(filepath.Join(recs, "action-state.json"))
		if err != nil {
			t.Fatalf("no action-state handoff: %v", err)
		}
		var st map[string]any
		if err := json.Unmarshal(b, &st); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"containment", "peer_control", "trace_control", "root", "peer_pid", "trace_pid"} {
			if _, present := st[key]; present {
				t.Errorf("action-state.json emits %q: %s", key, b)
			}
		}
		if _, ok := st["run"]; !ok {
			t.Error("action-state.json no longer carries the run identity it exists to hand off")
		}
	})
}

// TestTheInvocationManifestEmitsNoProofSlot is the output half of the same
// boundary, taken over the document the previous control never looked at.
//
// InvocationManifest.Stage2 was serialized as `json:"stage2_digest"` with no
// `omitempty`, and the live caller passed "" because the receipt it named was
// already gone — so every manifest the practical path rendered carried
// `"stage2_digest":""`. The lifecycle control inspected records and action
// state; the manifest is a third document, and nothing was reading it.
func TestTheInvocationManifestEmitsNoProofSlot(t *testing.T) {
	bin := planBinary(t)
	dir := t.TempDir()
	records := filepath.Join(dir, "records")
	if err := os.MkdirAll(records, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		var out strings.Builder
		cmd.Stderr, cmd.Stdout = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
	}
	// A complete lifecycle with one rendered invocation, exactly as the action
	// nests it.
	run("wall", "begin", "--dir", records, "--bucket-id", "bucket-0", "--run-id", "run-1")
	inner := bin + " wall exec --dir " + records + " --level invocation" +
		" --bucket-id bucket-0 --cwd " + dir + " -- sh -c true"
	run("wall", "run", "--dir", records, "--wrapper-chain", "--", "bash", "-euo", "pipefail", "-c",
		bin+" wall exec --dir "+records+" --level script --bucket-id bucket-0 --cwd "+dir+" -- sh -c '"+inner+"'")
	run("wall", "end", "--dir", records, "--terminal", "passed")

	plan, _ := writePlanDeclaring(t, dir, "bucket-0", 0, []string{"sh", "-c", "true"}, dir,
		observedProfileOf(t, bin))

	// `wall verify` renders the manifest from the plan and reports it. The
	// JSON form is what a consumer reads.
	cmd := exec.Command(bin, "wall", "verify", "--dir", records, "--shard-plan", plan, "--json")
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("wall verify: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "stage2_digest") {
		t.Errorf("the verifier's own output carries a stage2_digest slot:\n%s", out.String())
	}

	// And the manifest the builder produces, serialized directly: the verifier
	// may not print every field it holds.
	doc, err := core.ParseShardPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	m, err := planbind.InvocationManifestFor(doc, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"stage2_digest", "stage1_digest", "registry_digest"} {
		if _, present := raw[key]; present {
			t.Errorf("the invocation manifest emits %q: %s", key, b)
		}
	}
	if len(m.Invocations) != 1 {
		t.Errorf("the manifest renders %d invocations, want the one the plan declares", len(m.Invocations))
	}
}

// TestTheShippedHelpDescribesThePracticalLifecycle reads what a user is told.
//
// The no-argument help said `wall` runs a command "under a physical envelope
// with its own containment peer and independent trace" and verifies records
// "against every frozen gate". None of those exist. Documentation that
// describes a removed design is the removal's last hiding place: nothing
// compiles it, so nothing catches it.
func TestTheShippedHelpDescribesThePracticalLifecycle(t *testing.T) {
	bin := planBinary(t)
	prohibited := []string{
		"physical envelope", "containment peer", "independent trace",
		"frozen gate", "stage-1", "stage-2", "signer delegate",
	}
	for _, argv := range [][]string{{}, {"wall"}, {"--help"}} {
		name := "no arguments"
		if len(argv) > 0 {
			name = strings.Join(argv, " ")
		}
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(bin, argv...)
			var out strings.Builder
			cmd.Stdout, cmd.Stderr = &out, &out
			// These exit 2 by design; the TEXT is the subject.
			_ = cmd.Run()
			text := strings.ToLower(out.String())
			if strings.TrimSpace(text) == "" {
				t.Fatal("printed no help at all")
			}
			for _, p := range prohibited {
				if strings.Contains(text, p) {
					t.Errorf("the shipped help describes %q, which does not exist:\n%s", p, out.String())
				}
			}
		})
	}
	// And it still says what the command DOES; silence is not the fix.
	cmd := exec.Command(bin, "wall")
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	_ = cmd.Run()
	for _, want := range []string{"begin", "end", "exec", "verify", "assemble-observation"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the wall usage no longer names %q", want)
		}
	}
}
