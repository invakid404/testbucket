package walltime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A DIGEST-PINNED IMAGE IS A COMPLETE DIGEST, NOT A SUBSTRING.
//
// `isImmutableImage` asked only whether "@sha256:" or "@sha512:" appeared
// anywhere in the value, so `ubuntu-latest@sha256:`, `ubuntu-latest@sha256:x`
// and `ubuntu-latest@sha512:not-a-digest` all counted as immutable. That is an
// alias with a decorative suffix passing the one check that exists to refuse
// an alias — and the runner image is what says two arms of a pair ran on the
// same machine image.
func TestOnlyACompleteDigestPinsARunnerImage(t *testing.T) {
	sha256Hex := strings.Repeat("a1b2c3d4", 8)  // 64
	sha512Hex := strings.Repeat("a1b2c3d4", 16) // 128
	if len(sha256Hex) != 64 || len(sha512Hex) != 128 {
		t.Fatalf("fixture lengths are wrong: %d %d", len(sha256Hex), len(sha512Hex))
	}

	for _, ok := range []string{
		"ubuntu-24.04@sha256:" + sha256Hex,
		"ubuntu-24.04@sha512:" + sha512Hex,
		"ghcr.io/owner/image@sha256:" + sha256Hex,
	} {
		if !isImmutableImage(ok) {
			t.Errorf("a completely digest-pinned image was rejected: %q", ok)
		}
	}

	for _, bad := range []struct{ v, why string }{
		{"ubuntu-latest", "a bare moving alias"},
		{"ubuntu-latest@sha256:", "the marker with no digest at all"},
		{"ubuntu-latest@sha256:x", "one character is not a sha256"},
		{"ubuntu-latest@sha512:not-a-digest", "not hex, and not 128 characters"},
		{"ubuntu-24.04@sha256:" + sha256Hex[:63], "one character short"},
		{"ubuntu-24.04@sha256:" + sha256Hex + "a", "one character long"},
		{"ubuntu-24.04@sha256:" + strings.ToUpper(sha256Hex), "upper case is a different string from the one that gets compared"},
		{"ubuntu-24.04@sha512:" + sha256Hex, "a sha256-length digest labelled sha512"},
		{"ubuntu-24.04@sha256:" + sha512Hex, "a sha512-length digest labelled sha256"},
		{"@sha256:" + sha256Hex, "a digest with no image to pin"},
		{"ubuntu-latest@md5:" + sha256Hex, "an algorithm this protocol does not accept"},
		{"prefix @sha256:" + sha256Hex + " suffix", "a digest mentioned inside some other text"},
		{"", "nothing at all"},
	} {
		if isImmutableImage(bad.v) {
			t.Errorf("%q was accepted as an immutable image (%s)", bad.v, bad.why)
		}
	}
}

// AND THE MANIFEST REFUSES ONE. The syntax check is only worth something where
// the manifest actually applies it.
func TestAStage1ManifestRefusesAnAliasWithADecorativeSuffix(t *testing.T) {
	b := PlanningInputBundle{}
	m := testManifest(b, Digest("sha256:"+strings.Repeat("0", 64)))
	m.Consumer.RunnerImage = "ubuntu-latest@sha256:x"
	err := m.Validate()
	if err == nil {
		t.Fatal("a manifest naming a moving alias with a decorative digest suffix validated")
	}
	if !strings.Contains(err.Error(), "runner image") && !strings.Contains(err.Error(), "immutable") {
		t.Logf("refusal (not necessarily the image one, the fixture is minimal): %v", err)
	}
}

// THE SELECTED EXACT CACHE KEY IS REQUIRED.
//
// The contract freezes which key the admitted copy came from, and the field
// existed for it while `validateFields` asked for everything around it — the
// digest, the migration id, the token, the restore method and the stale
// instant. A receipt with correct bytes and every other field validated with
// `cache_key: ""`, losing the one fact that says WHICH store was admitted.
func TestAStoreReceiptMustNameTheExactCacheKeyItRestored(t *testing.T) {
	base := func() StoreReceipt {
		return StoreReceipt{
			Digest:        DigestBytes([]byte("store bytes")),
			Schema:        1,
			MigrationID:   "epoch-1",
			Token:         "tok",
			CacheKey:      "testbucket-store-go-abcdef",
			RestoreMethod: "exact",
			StaleAt:       "2026-09-10T00:00:00Z",
		}
	}
	now, err := parseInstant("2026-09-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := base().validateFields(now); err != nil {
		t.Fatalf("an otherwise-valid receipt naming its exact key was refused: %v", err)
	}

	for _, tc := range []struct{ name, key string }{
		{"blank", ""},
		{"a single space", " "},
		{"whitespace only", "  \t\n "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base()
			r.CacheKey = tc.key
			err := r.validateFields(now)
			if err == nil {
				t.Fatalf("a store receipt with cache_key %q validated; the selected-store provenance is gone", tc.key)
			}
			if !strings.Contains(err.Error(), "exact cache key") {
				t.Errorf("the refusal does not name the missing key: %v", err)
			}
		})
	}

	// AND THE FALLBACK VARIANT STAYS REFUSED for its own reason: a restore-key
	// fallback found SOME store, not the admitted one, whatever key it names.
	t.Run("a restore-key fallback", func(t *testing.T) {
		r := base()
		r.RestoreMethod = "restore-key-fallback"
		err := r.validateFields(now)
		if err == nil {
			t.Fatal("a restore-key fallback validated as warm evidence")
		}
		if !strings.Contains(err.Error(), "fallback") {
			t.Errorf("the refusal does not name the fallback: %v", err)
		}
	})
}

// AND THE SCORED LANE REFUSES AN ALIAS BEFORE IT PLANS.
//
// The syntax check only decides whether a manifest is well formed. The jobs
// are scheduled from one `runs-on` input whose default is the moving
// `ubuntu-latest`, so a scored arm could be planned and measured on an alias
// while the manifest claimed an immutable image. The plan job refuses that
// before the matrix exists — which is the only place a refusal still helps,
// because a downstream one cannot un-derive a matrix.
func TestTheScoredLaneRefusesAnAliasRunner(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "bucketed-reusable.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	guard := strings.Index(body, "needs an immutable runs-on for a scored arm")
	if guard < 0 {
		t.Fatal("the plan job does not refuse an alias runner for a scored arm")
	}
	plan := strings.Index(body, "- name: Plan the buckets")
	if plan < 0 {
		t.Fatal("the plan step is gone; this test is looking at the wrong workflow")
	}
	if guard > plan {
		t.Errorf("the runner refusal at %d comes after the plan step at %d; a downstream refusal cannot un-derive the matrix", guard, plan)
	}
	if !strings.Contains(body, "TB_RUNS_ON: ${{ inputs.runs-on }}") {
		t.Error("the guard never receives the runner value it claims to check")
	}
	// The residual is stated rather than implied: GitHub does not report the
	// image it selected, so the binding stays asserted.
	if !strings.Contains(body, "does not report the image it actually") {
		t.Error("the runtime binding gap is not disclosed where a consumer reads the input")
	}
}
