package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
