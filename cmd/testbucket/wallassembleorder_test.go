package main

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/runner"
	"github.com/invakid404/testbucket/internal/walltime"
)

// writeEnvelope writes the action and script envelopes the assembler requires
// around the invocations. Their intervals bound the invocation sum, as §3.1's
// invariants demand.
func writeEnvelope(t *testing.T, dir string, endMono int64) {
	t.Helper()
	for _, lv := range []walltime.Level{walltime.LevelAction, walltime.LevelScript} {
		path := filepath.Join(dir, fmt.Sprintf("physical-%s-00.jsonl", lv))
		w, err := walltime.NewWriter(path, walltime.ProducerPhysical, "physical")
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range []struct {
			boundary string
			mono     int64
		}{{"start", 0}, {"end", endMono}} {
			r := walltime.Record{
				Kind: "boundary", Level: lv, Boundary: b.boundary,
				Source: walltime.SourceWrapper,
				Instant: walltime.Instant{
					ClockID: walltime.ClockMonotonic, Mono: walltime.Nanos(b.mono),
					Realtime: "2026-09-01T00:00:00Z/2026-09-01T00:00:00.001Z", BootID: "boot-a",
				},
				Proc: walltime.ProcIdentity{PGID: 4242, ExitCode: 0},
			}
			if b.boundary == "end" {
				r.Terminal = "passed"
			}
			if _, err := w.Append(r); err != nil {
				t.Fatal(err)
			}
		}
		w.Close()
	}
}

// writeInvocationStream writes one invocation's boundary pair THROUGH THE
// PRODUCTION WRITER, into the file name the wrapper uses — which pads the
// sequence to a minimum of two digits.
func writeInvocationStream(t *testing.T, dir string, seq int, startMono, endMono int64, argvDigest string) {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("physical-invocation-%02d.jsonl", seq))
	w, err := walltime.NewWriter(path, walltime.ProducerPhysical, "physical")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, r := range []walltime.Record{
		{
			Kind: "boundary", Level: walltime.LevelInvocation, Boundary: "start",
			Seqno: seq, Source: walltime.SourceWrapper,
			Instant: walltime.Instant{
				ClockID: walltime.ClockMonotonic, Mono: walltime.Nanos(startMono),
				Realtime: "2026-09-01T00:00:00Z/2026-09-01T00:00:00.001Z", BootID: "boot-a",
			},
		},
		{
			Kind: "boundary", Level: walltime.LevelInvocation, Boundary: "end",
			Seqno: seq, Source: walltime.SourceWrapper,
			Instant: walltime.Instant{
				ClockID: walltime.ClockMonotonic, Mono: walltime.Nanos(endMono),
				Realtime: "2026-09-01T00:00:01Z/2026-09-01T00:00:01.001Z", BootID: "boot-a",
			},
			Proc: walltime.ProcIdentity{PGID: 4242, ExitCode: 0},
			Spec: &walltime.SpecIdentity{ArgvDigest: walltime.Digest(argvDigest), Cwd: "/w"},
		},
	} {
		if _, err := w.Append(r); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAssemblyOrdersInvocationsNumerically is F22's control.
//
// Record streams are read in lexicographic FILENAME order and the wrapper pads
// the sequence to two digits, so `physical-invocation-100.jsonl` sorts between
// `-10` and `-11`. Assembly took each invocation's sequence from that order, so
// invocation 100's measured argv and duration were emitted as sequence 11 and
// attributed to plan invocation 11's membership — while nothing else in the
// format limits a bucket to 100 invocations.
//
// 101 invocations is the smallest population that reaches the collision.
func TestAssemblyOrdersInvocationsNumerically(t *testing.T) {
	const n = 101
	dir := t.TempDir()

	// A distinct argv digest and duration per invocation, so a misattribution is
	// visible rather than merely possible.
	for i := 0; i < n; i++ {
		start := int64(1_000_000_000 * (i + 1))
		writeInvocationStream(t, dir, i, start, start+int64(i+1), fmt.Sprintf("sha256:argv-%03d", i))
	}
	writeEnvelope(t, dir, int64(1_000_000_000*(n+10)))

	plan := &core.PlanBucket{}
	for i := 0; i < n; i++ {
		plan.Invocations = append(plan.Invocations, runner.Invocation{
			Units: []string{fmt.Sprintf("unit-%03d", i)},
		})
	}

	var obs walltime.Observation
	if err := fillIntervals(&obs, plan, dir); err != nil {
		t.Fatalf("fillIntervals: %v", err)
	}
	if len(obs.Invocations) != n {
		t.Fatalf("assembled %d invocations, want %d", len(obs.Invocations), n)
	}

	for i, inv := range obs.Invocations {
		if inv.Seq != i {
			t.Fatalf("invocation at index %d carries seq %d; QC6 compares membership and order against the plan", i, inv.Seq)
		}
		wantArgv := walltime.Digest(fmt.Sprintf("sha256:argv-%03d", i))
		if inv.ArgvDigest != wantArgv {
			t.Errorf("seq %d carries argv_digest %s, want %s — a measured invocation was attributed to another plan entry",
				i, inv.ArgvDigest, wantArgv)
		}
		if want := int64(i + 1); int64(inv.ElapsedNs) != want {
			t.Errorf("seq %d elapsed_ns is %d, want %d", i, inv.ElapsedNs, want)
		}
		if want := fmt.Sprintf("unit-%03d", i); len(inv.Units) != 1 || inv.Units[0] != want {
			t.Errorf("seq %d carries units %v, want [%s]", i, inv.Units, want)
		}
	}

	// The specific collision, named: physical invocation 100 must not be
	// sequence 11.
	if got := obs.Invocations[11].ArgvDigest; got != "sha256:argv-011" {
		t.Fatalf("sequence 11 carries %s; lexicographic stream order put invocation 100 there", got)
	}
	if got := obs.Invocations[100].ArgvDigest; got != "sha256:argv-100" {
		t.Fatalf("sequence 100 carries %s, want sha256:argv-100", got)
	}
}
