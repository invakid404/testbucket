package walltime

import (
	"strings"
	"testing"
)

func planComparability() ComparabilityProfile {
	return ComparabilityProfile{
		RunnerClass:            "github-hosted-ubuntu-24.04-2core",
		RunnerImageLabel:       "ubuntu-24.04",
		OS:                     "Linux",
		Arch:                   "X64",
		NodeVersion:            "v22.11.0",
		PnpmVersion:            "9.12.3",
		VitestVersion:          "2.1.4",
		TestbucketSHA256:       "sha256:cc",
		WorkingDir:             ".",
		FacadeCommand:          "pnpm tb-vitest",
		SetupCommand:           "pnpm install --frozen-lockfile",
		LockSHA256:             "sha256:dd",
		DiscoveryMode:          "facade",
		Exclusions:             []string{"integration-tests/"},
		CacheDeclarationDigest: "sha256:ee",
	}
}

// TestExecutionProfileIsPlanTimeAvailableAndLabelBound is §22 test 53
// (SR-3/S-4) and the acceptance test the registry names for ID-11.
func TestExecutionProfileIsPlanTimeAvailableAndLabelBound(t *testing.T) {
	base := planComparability()
	baseKey, err := ComparabilityKeyDigest(base)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("every leaf is constructible before matrix emission", func(t *testing.T) {
		// The profile is built entirely from plan-job inputs and context. The
		// executable form of that claim: the key is computable from the struct
		// alone, with no bucket-side value supplied.
		if baseKey == "" || !baseKey.Valid() {
			t.Fatalf("key digest = %q, want a valid digest", baseKey)
		}
	})

	t.Run("the key is stable across runs whose actual_runner_name differs", func(t *testing.T) {
		// This is the stability the old `runner_name` leaf destroyed. Three
		// simulated runs on different hosted instances must produce one key.
		for _, instance := range []string{"GitHub Actions 2", "GitHub Actions 7", "GitHub Actions 41"} {
			// actual_runner_name is a DIAGNOSTIC on the observation and is not
			// a key input at all, so it cannot enter here by construction.
			obs := Observation{ActualRunnerName: instance}
			k, err := ComparabilityKeyDigest(base)
			if err != nil {
				t.Fatal(err)
			}
			if k != baseKey {
				t.Fatalf("instance %q changed the key", obs.ActualRunnerName)
			}
		}
		for _, name := range ComparabilityLeafNames() {
			if name == "actual_runner_name" {
				t.Fatal("actual_runner_name must not be a comparability leaf (S-4)")
			}
		}
	})

	t.Run("mutating each leaf changes the key", func(t *testing.T) {
		// Enumerated from the membership, not from a transcribed list.
		mutators := map[string]func(*ComparabilityProfile){
			"runner_class":             func(p *ComparabilityProfile) { p.RunnerClass = "other-class" },
			"runner_image_label":       func(p *ComparabilityProfile) { p.RunnerImageLabel = "ubuntu-22.04" },
			"os":                       func(p *ComparabilityProfile) { p.OS = "macOS" },
			"arch":                     func(p *ComparabilityProfile) { p.Arch = "ARM64" },
			"node_version":             func(p *ComparabilityProfile) { p.NodeVersion = "v20.0.0" },
			"pnpm_version":             func(p *ComparabilityProfile) { p.PnpmVersion = "8.0.0" },
			"vitest_version":           func(p *ComparabilityProfile) { p.VitestVersion = "1.0.0" },
			"testbucket_sha256":        func(p *ComparabilityProfile) { p.TestbucketSHA256 = "sha256:zz" },
			"working_dir":              func(p *ComparabilityProfile) { p.WorkingDir = "sub" },
			"facade_command":           func(p *ComparabilityProfile) { p.FacadeCommand = "pnpm other" },
			"setup_command":            func(p *ComparabilityProfile) { p.SetupCommand = "pnpm i" },
			"lock_sha256":              func(p *ComparabilityProfile) { p.LockSHA256 = "sha256:yy" },
			"discovery_mode":           func(p *ComparabilityProfile) { p.DiscoveryMode = "glob" },
			"exclusions":               func(p *ComparabilityProfile) { p.Exclusions = []string{"other/"} },
			"cache_declaration_digest": func(p *ComparabilityProfile) { p.CacheDeclarationDigest = "sha256:ff" },
		}
		for _, name := range ComparabilityLeafNames() {
			m, ok := mutators[name]
			if !ok {
				t.Fatalf("leaf %q has no mutation case; the membership grew without the test", name)
			}
			p := planComparability()
			m(&p)
			k, err := ComparabilityKeyDigest(p)
			if err != nil {
				t.Fatal(err)
			}
			if k == baseKey {
				t.Errorf("mutating leaf %q did not change the key", name)
			}
		}
	})

	t.Run("the digest is taken over the declared order, not sorted order", func(t *testing.T) {
		b, err := base.OrderedJSON()
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		// §15.3's order starts at runner_class; lexicographic order would
		// start at arch.
		if !strings.HasPrefix(s, `{"runner_class":`) {
			t.Fatalf("serialization does not start at the declared first leaf: %s", s[:60])
		}
		if strings.HasPrefix(s, `{"arch":`) {
			t.Fatal("the comparability digest is being taken over lexicographic order")
		}
		// And the leaves appear in exactly the declared sequence.
		at := -1
		for _, name := range ComparabilityLeafNames() {
			i := strings.Index(s, `"`+name+`":`)
			if i < 0 {
				t.Fatalf("leaf %q missing from the serialization", name)
			}
			if i < at {
				t.Fatalf("leaf %q appears out of declared order", name)
			}
			at = i
		}
	})

	t.Run("no cryptographic image digest is constructed or required", func(t *testing.T) {
		// §15.3: the label IS the identity this product binds; fleet
		// attestation was removed by a settled PRODUCT_DECISION and no
		// document, test or delta requires an image digest.
		for _, name := range ComparabilityLeafNames() {
			if strings.Contains(name, "image_digest") {
				t.Fatalf("leaf %q constructs an image digest, which §15.3 excludes", name)
			}
		}
	})

	t.Run("K and file_parallelism are outside the key", func(t *testing.T) {
		for _, name := range ComparabilityLeafNames() {
			if name == "k" || name == "file_parallelism" {
				t.Fatalf("%q must be outside the key: topology enters as frozen per-row columns", name)
			}
		}
	})

	t.Run("cache OUTCOME leaves are excluded from the key", func(t *testing.T) {
		// They vary legitimately between bucket jobs of one run, so keying on
		// them would reset history on ordinary cache behaviour (R11-D4).
		for _, name := range ComparabilityLeafNames() {
			if name == "dependency_cache_matched_key" || name == "dependency_cache_disposition" {
				t.Fatalf("outcome leaf %q must not be a key input", name)
			}
		}
	})

	t.Run("QC12 reports a plan/bucket label mismatch naming both values", func(t *testing.T) {
		err := QC12("ubuntu-24.04", "ubuntu-22.04", 1)
		if err == nil {
			t.Fatal("QC12 accepted a label mismatch")
		}
		if !strings.Contains(err.Error(), "ubuntu-24.04") || !strings.Contains(err.Error(), "ubuntu-22.04") {
			t.Fatalf("QC12 must name both labels, got %q", err.Error())
		}
		if err := QC12("ubuntu-24.04", "ubuntu-24.04", 1); err != nil {
			t.Fatalf("QC12 rejected a matching label: %v", err)
		}
		if err := QC12("ubuntu-24.04", "", 1); err == nil {
			t.Fatal("QC12 accepted an absent observed label")
		}
		if err := QC12("ubuntu-24.04", "ubuntu-24.04", 2); err == nil {
			t.Fatal("QC12 accepted file_parallelism > 1")
		}
	})

	t.Run("the key is byte-identical for identical inputs", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			k, err := ComparabilityKeyDigest(planComparability())
			if err != nil {
				t.Fatal(err)
			}
			if k != baseKey {
				t.Fatal("the key is not a pure function of its inputs")
			}
		}
	})
}
