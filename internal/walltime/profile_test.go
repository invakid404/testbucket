package walltime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func planProfile() CanonicalProfile {
	return CanonicalProfile{
		Scored:                true,
		RunnerToken:           "vitest",
		K:                     8,
		Count:                 1,
		FileParallelism:       1,
		BucketIndices:         []int{0, 1, 2, 3, 4, 5, 6, 7},
		EstBasis:              BasisWall,
		StoreSHA256:           "sha256:aa",
		ExpandedUnitSetDigest: "sha256:bb",
	}
}

// TestObservationCopiesCanonicalProfileVerbatim is §22 test 36 (S4): a
// mutated, reordered, or retyped `profile` is rejected by QC13.
func TestObservationCopiesCanonicalProfileVerbatim(t *testing.T) {
	plan, err := NewProfileBlock(planProfile())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a verbatim copy is accepted", func(t *testing.T) {
		var copied ProfileBlock
		if err := json.Unmarshal(plan.Raw(), &copied); err != nil {
			t.Fatal(err)
		}
		if err := QC13(plan, copied); err != nil {
			t.Fatalf("verbatim copy rejected: %v", err)
		}
	})

	t.Run("a mutated value is rejected", func(t *testing.T) {
		m := planProfile()
		m.K = 9
		mut, err := NewProfileBlock(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := QC13(plan, mut); err == nil {
			t.Fatal("QC13 accepted a mutated profile")
		}
	})

	t.Run("a reordered block is rejected", func(t *testing.T) {
		// Canonicalising both sides would normalise key order away, so QC13
		// compares bytes. This is the case that proves it does.
		var reordered ProfileBlock
		raw := string(plan.Raw())
		// Rebuild the same object with the two leading keys swapped.
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(plan.Raw(), &obj); err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		b.WriteByte('{')
		order := []string{"scored", "bucket_indices", "count", "est_basis", "expanded_unit_set_digest",
			"file_parallelism", "k", "runner_token", "store_sha256"}
		for i, k := range order {
			if i > 0 {
				b.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			b.Write(kb)
			b.WriteByte(':')
			b.Write(obj[k])
		}
		b.WriteByte('}')
		if b.String() == raw {
			t.Fatal("fixture failed to reorder the block")
		}
		if err := json.Unmarshal(b.Bytes(), &reordered); err != nil {
			t.Fatal(err)
		}
		if err := QC13(plan, reordered); err == nil {
			t.Fatal("QC13 accepted a reordered profile block")
		}
	})

	t.Run("a retyped field is rejected", func(t *testing.T) {
		retyped := strings.Replace(string(plan.Raw()), `"k":8`, `"k":"8"`, 1)
		if retyped == string(plan.Raw()) {
			t.Fatal("fixture failed to retype k")
		}
		var blk ProfileBlock
		if err := json.Unmarshal([]byte(retyped), &blk); err != nil {
			t.Fatal(err)
		}
		if err := QC13(plan, blk); err == nil {
			t.Fatal("QC13 accepted a retyped profile field")
		}
		// It must also fail to parse back into the canonical type.
		if _, err := blk.Parse(); err == nil {
			t.Fatal("a retyped k parsed as the canonical profile")
		}
	})

	t.Run("an added key is rejected", func(t *testing.T) {
		added := strings.Replace(string(plan.Raw()), "{", `{"extra":1,`, 1)
		var blk ProfileBlock
		if err := json.Unmarshal([]byte(added), &blk); err != nil {
			t.Fatal(err)
		}
		if err := QC13(plan, blk); err == nil {
			t.Fatal("QC13 accepted a profile with an added key")
		}
		if _, err := blk.Parse(); err == nil {
			t.Fatal("an out-of-registry key parsed as the canonical profile")
		}
	})
}

func planRuntimeProfile() RuntimeProfile {
	return RuntimeProfile{
		NodeVersion:         "v22.11.0",
		PnpmVersion:         "9.12.3",
		VitestVersion:       "2.1.4",
		TestbucketSHA256:    "sha256:cc",
		FacadeCommand:       "pnpm tb-vitest",
		LockSHA256:          "sha256:dd",
		DependencyCacheMode: "exact-key",
	}
}

// TestBucketRuntimeProfileMatchesPlan is §22 test 73 (owner F1): the executed
// runtime profile is bound to the planned one, against contract §15.3a.
func TestBucketRuntimeProfileMatchesPlan(t *testing.T) {
	declared := planRuntimeProfile()
	dd := RuntimeProfileDigest(declared)

	t.Run("the equal case passes and the digests agree", func(t *testing.T) {
		executed := planRuntimeProfile()
		ed := RuntimeProfileDigest(executed)
		if dd != ed {
			t.Fatalf("equal objects gave different digests: %s vs %s", dd, ed)
		}
		if err := QC17(declared, executed, dd, ed); err != nil {
			t.Fatalf("QC17 rejected the equal case: %v", err)
		}
	})

	t.Run("each of the seven constituents, mutated in the bucket only", func(t *testing.T) {
		mutators := []struct {
			field int
			name  string
			apply func(*RuntimeProfile)
		}{
			{1, "node_version", func(p *RuntimeProfile) { p.NodeVersion = "v20.0.0" }},
			{2, "pnpm_version", func(p *RuntimeProfile) { p.PnpmVersion = "8.0.0" }},
			{3, "vitest_version", func(p *RuntimeProfile) { p.VitestVersion = "1.0.0" }},
			{4, "testbucket_sha256", func(p *RuntimeProfile) { p.TestbucketSHA256 = "sha256:zz" }},
			{5, "facade_command", func(p *RuntimeProfile) { p.FacadeCommand = "pnpm other" }},
			{6, "lock_sha256", func(p *RuntimeProfile) { p.LockSHA256 = "sha256:yy" }},
			{7, "dependency_cache_mode", func(p *RuntimeProfile) { p.DependencyCacheMode = "disabled" }},
		}
		if len(mutators) != len(runtimeProfileFieldOrder) {
			t.Fatalf("the case set must cover every constituent: %d cases, %d fields",
				len(mutators), len(runtimeProfileFieldOrder))
		}
		for _, m := range mutators {
			t.Run(m.name, func(t *testing.T) {
				executed := planRuntimeProfile()
				m.apply(&executed)
				ed := RuntimeProfileDigest(executed)
				if ed == dd {
					t.Fatal("mutating a constituent did not change the digest")
				}
				err := QC17(declared, executed, dd, ed)
				if err == nil {
					t.Fatal("QC17 accepted a mutated constituent")
				}
				// The message names the FIRST differing constituent by field
				// number, so a drift is diagnosable without re-deriving the
				// digest.
				want := fmt.Sprintf("field %d", m.field)
				if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), m.name) {
					t.Fatalf("QC17 message %q does not name %s (%s)", err.Error(), want, m.name)
				}
			})
		}
	})

	t.Run("the first differing constituent is the one named", func(t *testing.T) {
		executed := planRuntimeProfile()
		executed.PnpmVersion = "8.0.0"   // field 2
		executed.LockSHA256 = "sha256:x" // field 6
		err := QC17(declared, executed, dd, RuntimeProfileDigest(executed))
		if err == nil {
			t.Fatal("QC17 accepted two mutated constituents")
		}
		if !strings.Contains(err.Error(), "field 2") {
			t.Fatalf("QC17 must name the FIRST differing constituent, got %q", err.Error())
		}
	})

	t.Run("a bucket that forwards the plan digest instead of computing its own fails", func(t *testing.T) {
		executed := planRuntimeProfile()
		executed.NodeVersion = "v20.0.0"
		// The bucket drifted but echoed the plan's declared digest.
		if err := QC17(declared, executed, dd, dd); err == nil {
			t.Fatal("QC17 accepted a forwarded digest over a drifted object")
		}
		// And with no field drift at all, a forwarded-but-wrong digest is still
		// a failure: the digest must be the one §15.3a defines over that object.
		equal := planRuntimeProfile()
		if err := QC17(declared, equal, dd, Digest("sha256:forged")); err == nil {
			t.Fatal("QC17 accepted a digest that is not the digest of its object")
		}
	})

	t.Run("field order is asserted, not assumed", func(t *testing.T) {
		p := planRuntimeProfile()
		canonical := p.OrderedJSON()

		// Permute two fields and rebuild the same key/value set in a different
		// order. §15.3a fixes the order, so the digest must change.
		permuted := []byte(`{"pnpm_version":"9.12.3","node_version":"v22.11.0","vitest_version":"2.1.4","testbucket_sha256":"sha256:cc","facade_command":"pnpm tb-vitest","lock_sha256":"sha256:dd","dependency_cache_mode":"exact-key"}`)
		if bytes.Equal(canonical, permuted) {
			t.Fatal("fixture failed to permute the field order")
		}
		if DigestBytes(canonical) == DigestBytes(permuted) {
			t.Fatal("permuting the seven fields did not change the digest")
		}

		// The declared order is §15.3a's table order, not lexicographic: a
		// sorted rendering must NOT be what the digest is taken over.
		sorted := []byte(`{"dependency_cache_mode":"exact-key","facade_command":"pnpm tb-vitest","lock_sha256":"sha256:dd","node_version":"v22.11.0","pnpm_version":"9.12.3","testbucket_sha256":"sha256:cc","vitest_version":"2.1.4"}`)
		if bytes.Equal(canonical, sorted) {
			t.Fatal("the runtime profile digest is being taken over lexicographic order, not §15.3a's declared order")
		}
	})

	t.Run("no signing, attestation or roster is used", func(t *testing.T) {
		// The check reads version strings and file digests on the executing
		// runner and nothing else (§3, §12).
		executed := planRuntimeProfile()
		if err := QC17(declared, executed, dd, RuntimeProfileDigest(executed)); err != nil {
			t.Fatal(err)
		}
		src := p73Source(t)
		for _, banned := range []string{"ed25519", "Signature", "roster", "attest"} {
			if strings.Contains(src, banned) {
				t.Errorf("the runtime profile path references %q; §15.3a requires none", banned)
			}
		}
	})
}

// TestScoredPlanAdmission is §22 test 31: contract §19.3a's observable plan
// admission — an offending scored plan refuses to emit a matrix.
func TestScoredPlanAdmission(t *testing.T) {
	ok := func() CanonicalProfile { return planProfile() }
	cases := []struct {
		name     string
		mutate   func(*CanonicalProfile)
		class    string
		label    string
		cand     string
		work     string
		declared bool
	}{
		{name: "k != 8", mutate: func(p *CanonicalProfile) { p.K = 9 }},
		{name: "count != 1", mutate: func(p *CanonicalProfile) { p.Count = 2 }},
		{name: "file_parallelism > 1", mutate: func(p *CanonicalProfile) { p.FileParallelism = 2 }},
		{name: "non-vitest runner", mutate: func(p *CanonicalProfile) { p.RunnerToken = "go" }},
		{name: "bucket set is not exactly [0..7]", mutate: func(p *CanonicalProfile) {
			p.BucketIndices = []int{0, 1, 2, 3, 4, 5, 6, 8}
		}},
		{name: "empty runner-class (AD-8)", class: "-"},
		{name: "empty runs-on-label (AD-8)", label: "-"},
		{name: "undeclared cache_state (AD-9)", declared: true},
		{name: "empty candidate-sha (AD-10)", cand: "-"},
		{name: "empty workload-commit (AD-10)", work: "-"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := ok()
			if c.mutate != nil {
				c.mutate(&p)
			}
			class, label, cand, work, declared := "ubuntu-latest-4core", "ubuntu-24.04", "a1", "b2", true
			if c.class == "-" {
				class = ""
			}
			if c.label == "-" {
				label = ""
			}
			if c.cand == "-" {
				cand = ""
			}
			if c.work == "-" {
				work = ""
			}
			if c.declared {
				declared = false
			}
			if err := AdmitScoredProfile(p, class, label, cand, work, declared); err == nil {
				t.Fatal("scored admission accepted an offending plan")
			}
		})
	}

	t.Run("a conforming scored plan is admitted", func(t *testing.T) {
		if err := AdmitScoredProfile(ok(), "ubuntu-latest-4core", "ubuntu-24.04", "a1", "b2", true); err != nil {
			t.Fatalf("scored admission rejected a conforming plan: %v", err)
		}
	})

	t.Run("an unscored plan is not subject to the veto", func(t *testing.T) {
		p := ok()
		p.Scored = false
		p.K = 3
		if err := AdmitScoredProfile(p, "", "", "", "", false); err != nil {
			t.Fatalf("unscored plan rejected by the scored veto: %v", err)
		}
	})
}
