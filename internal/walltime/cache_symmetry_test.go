package walltime

import (
	"strings"
	"testing"
)

func ptrBool(v bool) *bool    { return &v }
func ptrStr(v string) *string { return &v }

func declExact() CacheDeclaration {
	return CacheDeclaration{
		DependencyCacheMode:       CacheModeExactKey,
		DependencyCachePrimaryKey: "pnpm-linux-abc",
		TransformCacheMode:        CacheModeDisabled,
		DependencyCacheProducer:   ProducerCaller,
		ExpectedMongoBinarySHA256: "sha256:mongo",
	}
}

func declDisabled() CacheDeclaration {
	return CacheDeclaration{
		DependencyCacheMode:       CacheModeDisabled,
		DependencyCachePrimaryKey: "",
		TransformCacheMode:        CacheModeDisabled,
		DependencyCacheProducer:   ProducerNone,
		ExpectedMongoBinarySHA256: "sha256:mongo",
	}
}

// stateExact builds a recorded cache_state for an exact-key bucket.
func stateExact(hit bool, matched string, disp string) CacheState {
	d := declExact()
	return CacheState{
		DependencyCacheMode:         d.DependencyCacheMode,
		DependencyCachePrimaryKey:   d.DependencyCachePrimaryKey,
		DependencyCacheMatchedKey:   matched,
		DependencyCacheDisposition:  disp,
		TransformCacheMode:          d.TransformCacheMode,
		DependencyCacheProducer:     d.DependencyCacheProducer,
		DependencyCacheHit:          ptrBool(hit),
		MongoBinarySHA256:           "sha256:mongo",
		MongoBinaryPath:             "/tmp/mongodb-binaries/mongod",
		MongoBinaryVerifiedOnRunner: true,
		ExpectedMongoBinarySHA256:   "sha256:mongo",
	}
}

func stateDisabled() CacheState {
	d := declDisabled()
	return CacheState{
		DependencyCacheMode:         d.DependencyCacheMode,
		DependencyCacheDisposition:  DispositionDisabled,
		TransformCacheMode:          d.TransformCacheMode,
		DependencyCacheProducer:     ProducerNone,
		MongoBinarySHA256:           "sha256:mongo",
		MongoBinaryPath:             "/tmp/mongodb-binaries/mongod",
		MongoBinaryVerifiedOnRunner: true,
		ExpectedMongoBinarySHA256:   "sha256:mongo",
	}
}

// TestScoredCacheModeEqualityOrPairInvalid is §22 test 62
// (S-5/R8-D4/R10-D4). POSITIVE FIXTURES FIRST, because earlier blanket
// predicates rejected behaviour the rule explicitly permits.
func TestScoredCacheModeEqualityOrPairInvalid(t *testing.T) {
	t.Run("exact-key hit is accepted", func(t *testing.T) {
		c := stateExact(true, "pnpm-linux-abc", DispositionExactHit)
		if err := QC14(c); err != nil {
			t.Fatalf("QC14 rejected a legal exact hit: %v", err)
		}
	})

	t.Run("exact-key MISS is accepted — a legal tuple, not a violation", func(t *testing.T) {
		c := stateExact(false, "", DispositionMiss)
		if err := QC14(c); err != nil {
			t.Fatalf("QC14 rejected a legal miss: %v", err)
		}
	})

	t.Run("disabled is accepted", func(t *testing.T) {
		if err := QC14(stateDisabled()); err != nil {
			t.Fatalf("QC14 rejected the disabled tuple: %v", err)
		}
	})

	t.Run("prefix fallback is rejected by QC14", func(t *testing.T) {
		c := stateExact(true, "pnpm-linux-", DispositionExactHit)
		if err := QC14(c); err == nil {
			t.Fatal("QC14 accepted a prefix-fallback match")
		}
	})

	t.Run("a non-disabled transform cache mode is rejected", func(t *testing.T) {
		c := stateExact(true, "pnpm-linux-abc", DispositionExactHit)
		c.TransformCacheMode = "enabled"
		if err := QC14(c); err == nil {
			t.Fatal("QC14 accepted a non-disabled transform cache mode")
		}
	})

	t.Run("a missing mongo_binary_sha256 is rejected", func(t *testing.T) {
		c := stateExact(true, "pnpm-linux-abc", DispositionExactHit)
		c.MongoBinarySHA256 = ""
		if err := QC14(c); err == nil {
			t.Fatal("QC14 accepted a row with no executed binary digest")
		}
	})

	t.Run("a pair differing at any index is retained, unscored and non-passing", func(t *testing.T) {
		armB := make([]CacheState, 8)
		armC := make([]CacheState, 8)
		for i := range armB {
			armB[i] = stateExact(true, "pnpm-linux-abc", DispositionExactHit)
			armC[i] = stateExact(true, "pnpm-linux-abc", DispositionExactHit)
		}
		// One index differs in disposition.
		armC[5] = stateExact(false, "", DispositionMiss)

		v, err := ScoredCacheSymmetry(armB, armC)
		if err != nil {
			t.Fatal(err)
		}
		if v.Scored {
			t.Fatal("a pair with an index-wise difference must NOT be scored; the superseded 'still scored' rule is withdrawn")
		}
		if v.Passing {
			t.Fatal("such a pair must be non-passing")
		}
		if !v.Retained {
			t.Fatal("both rows stay in the attempted population; the pair is never voided")
		}
		if v.FirstDifferingIndex != 5 {
			t.Fatalf("first differing index = %d, want 5", v.FirstDifferingIndex)
		}
		if !strings.Contains(v.Reason, "19.8") {
			t.Fatalf("the verdict must cite §19.8's post-start rule, got %q", v.Reason)
		}
	})
}

// TestScoredCacheIsSymmetric is §22 test 72 (owner F2) and the acceptance test
// the registry names for ID-24.
func TestScoredCacheIsSymmetric(t *testing.T) {
	eight := func(mk func() CacheState) []CacheState {
		out := make([]CacheState, 8)
		for i := range out {
			out[i] = mk()
		}
		return out
	}

	t.Run("mode (a) disabled scores", func(t *testing.T) {
		v, err := ScoredCacheSymmetry(eight(stateDisabled), eight(stateDisabled))
		if err != nil {
			t.Fatal(err)
		}
		if !v.Scored || !v.Passing {
			t.Fatalf("disabled/disabled must score: %+v", v)
		}
	})

	t.Run("mode (b) exact-key, index-wise equal, scores", func(t *testing.T) {
		mk := func() CacheState { return stateExact(true, "pnpm-linux-abc", DispositionExactHit) }
		v, err := ScoredCacheSymmetry(eight(mk), eight(mk))
		if err != nil {
			t.Fatal(err)
		}
		if !v.Scored || !v.Passing {
			t.Fatalf("index-wise equal exact-key must score: %+v", v)
		}
	})

	t.Run("mode (b) with one index differing in each leaf", func(t *testing.T) {
		base := func() CacheState { return stateExact(true, "pnpm-linux-abc", DispositionExactHit) }
		mutations := map[string]func(CacheState) CacheState{
			"hit": func(c CacheState) CacheState {
				c.DependencyCacheHit = ptrBool(false)
				c.DependencyCacheMatchedKey = ""
				c.DependencyCacheDisposition = DispositionMiss
				return c
			},
			"matched_key": func(c CacheState) CacheState {
				c.DependencyCacheMatchedKey = ""
				c.DependencyCacheDisposition = DispositionMiss
				c.DependencyCacheHit = ptrBool(false)
				return c
			},
			"disposition": func(c CacheState) CacheState {
				c.DependencyCacheDisposition = DispositionMiss
				c.DependencyCacheMatchedKey = ""
				c.DependencyCacheHit = ptrBool(false)
				return c
			},
		}
		for name, mut := range mutations {
			t.Run(name, func(t *testing.T) {
				armB, armC := eight(base), eight(base)
				armC[3] = mut(armC[3])
				v, err := ScoredCacheSymmetry(armB, armC)
				if err != nil {
					t.Fatal(err)
				}
				if v.Scored || v.Passing {
					t.Fatalf("a %s difference must leave the pair unscored and non-passing: %+v", name, v)
				}
				if !v.Retained {
					t.Fatal("the rows must be retained, never voided or rescheduled")
				}
			})
		}
	})

	t.Run("arms declaring different producer or mode fail §17.19 first", func(t *testing.T) {
		base := func() CacheState { return stateExact(true, "pnpm-linux-abc", DispositionExactHit) }
		armB, armC := eight(base), eight(base)
		armC[0].DependencyCacheProducer = ProducerAction
		armC[0].DependencyCacheHit = nil
		if _, err := ScoredCacheSymmetry(armB, armC); err == nil {
			t.Fatal("a differing producer must fail §17.19 before the index-wise rule is reached")
		} else if !strings.Contains(err.Error(), "17.19") {
			t.Fatalf("error must cite §17.19, got %q", err)
		}

		armB, armC = eight(base), eight(stateDisabled)
		if _, err := ScoredCacheSymmetry(armB, armC); err == nil {
			t.Fatal("a differing mode must fail §17.19")
		}
	})

	t.Run("QC14a binds the executed binary in both modes", func(t *testing.T) {
		for _, mk := range []func() CacheState{stateDisabled, func() CacheState { return stateExact(true, "pnpm-linux-abc", DispositionExactHit) }} {
			c := mk()
			if err := QC14b(c); err != nil {
				t.Fatalf("QC14b rejected a well-formed row: %v", err)
			}
			// A row that did not pass QC14a on its own runner is not ingestible.
			bad := mk()
			bad.MongoBinaryVerifiedOnRunner = false
			if err := QC14b(bad); err == nil {
				t.Fatal("QC14b accepted a row that did not pass QC14a on its runner")
			}
			// A decoy digest that does not equal the declared expectation fails.
			decoy := mk()
			decoy.MongoBinarySHA256 = "sha256:decoy"
			if err := QC14b(decoy); err == nil {
				t.Fatal("QC14b accepted an executed digest differing from the declaration")
			}
		}
	})

	t.Run("QC14b accepts a path that does not exist on the record runner", func(t *testing.T) {
		// The path names a file on ANOTHER runner. An implementation that
		// stats, opens or re-hashes it is wrong.
		c := stateDisabled()
		c.MongoBinaryPath = "/nonexistent/on/this/host/mongod"
		if err := QC14b(c); err != nil {
			t.Fatalf("QC14b must accept a foreign path: %v", err)
		}
	})

	t.Run("the withdrawn rule is a must-fail fixture", func(t *testing.T) {
		// An arrangement that RECORDS differing hit/matched-key/disposition
		// across the arms and scores anyway must fail: recording a difference
		// is not equalizing it.
		base := func() CacheState { return stateExact(true, "pnpm-linux-abc", DispositionExactHit) }
		armB, armC := eight(base), eight(base)
		armC[7] = stateExact(false, "", DispositionMiss)
		v, err := ScoredCacheSymmetry(armB, armC)
		if err != nil {
			t.Fatal(err)
		}
		if v.Scored {
			t.Fatal("the withdrawn 'still scored' behaviour is present")
		}
	})
}

// TestScoredCacheDeclarationTransportAndBinaryBinding is §22 test 70
// (R13-D2): the physical half of the cache-declaration transport.
func TestScoredCacheDeclarationTransportAndBinaryBinding(t *testing.T) {
	t.Run("the declaration digest is over the declaration leaves in table order", func(t *testing.T) {
		d := declExact()
		js := string(d.CanonicalJSON())
		at := -1
		for _, name := range DeclarationLeafNames() {
			i := strings.Index(js, `"`+name+`":`)
			if i < 0 {
				t.Fatalf("declaration leaf %q missing from the canonical bytes", name)
			}
			if i < at {
				t.Fatalf("declaration leaf %q is out of §10.5.0 order", name)
			}
			at = i
		}
		// No outcome leaf may appear in the declaration.
		for _, name := range OutcomeLeafNames() {
			if strings.Contains(js, `"`+name+`":`) {
				t.Fatalf("outcome leaf %q appears in the frozen declaration (§10.5.4)", name)
			}
		}
		if !d.Digest().Valid() {
			t.Fatal("declaration digest is not a valid digest")
		}
	})

	t.Run("a one-byte perturbation changes the digest", func(t *testing.T) {
		a := declExact()
		b := declExact()
		b.DependencyCachePrimaryKey += "x"
		if a.Digest() == b.Digest() {
			t.Fatal("a perturbed declaration produced the same digest")
		}
	})

	t.Run("the declaration/outcome partition is disjoint and total", func(t *testing.T) {
		// Derived from the two class lists, never from a written count —
		// §10.5.0 states that no document writes a leaf count.
		seen := map[string]int{}
		for _, n := range DeclarationLeafNames() {
			seen[n]++
		}
		for _, n := range OutcomeLeafNames() {
			seen[n]++
		}
		for n, c := range seen {
			if c != 1 {
				t.Errorf("leaf %q appears in %d classes; each leaf carries exactly one class", n, c)
			}
		}
	})

	t.Run("every legal mode has exactly one legal producer state", func(t *testing.T) {
		cases := []struct {
			name string
			d    CacheDeclaration
			ok   bool
		}{
			{"disabled + none", declDisabled(), true},
			{"exact-key + caller", declExact(), true},
			{"exact-key + action", func() CacheDeclaration { d := declExact(); d.DependencyCacheProducer = ProducerAction; return d }(), true},
			{"disabled + caller", func() CacheDeclaration { d := declDisabled(); d.DependencyCacheProducer = ProducerCaller; return d }(), false},
			{"disabled + action", func() CacheDeclaration { d := declDisabled(); d.DependencyCacheProducer = ProducerAction; return d }(), false},
			{"exact-key + none", func() CacheDeclaration { d := declExact(); d.DependencyCacheProducer = ProducerNone; return d }(), false},
		}
		for _, c := range cases {
			err := c.d.Validate()
			if c.ok && err != nil {
				t.Errorf("%s must be accepted: %v", c.name, err)
			}
			if !c.ok && err == nil {
				t.Errorf("%s must be rejected", c.name)
			}
		}
	})

	t.Run("a caller-owned miss and absent producer data are distinguishable", func(t *testing.T) {
		d := declExact()
		// matched-key "" WITH hit:false is a miss.
		mk, disp, err := DeriveDisposition(d, ProducerResult{Hit: ptrBool(false), MatchedKey: ptrStr("")})
		if err != nil {
			t.Fatalf("a caller-owned miss must be accepted: %v", err)
		}
		if mk != "" || disp != DispositionMiss {
			t.Fatalf("derived (%q, %q), want ('', miss)", mk, disp)
		}
		// matched-key "" with NO hit input at all is absent producer data.
		if _, _, err := DeriveDisposition(d, ProducerResult{MatchedKey: ptrStr("")}); err == nil {
			t.Fatal("absent producer data must be rejected; it was indistinguishable from a miss before this input existed")
		}
	})

	t.Run("supplying both producers fails, never silently resolved", func(t *testing.T) {
		d := declExact()
		d.DependencyCacheProducer = ProducerAction
		if _, _, err := DeriveDisposition(d, ProducerResult{Hit: ptrBool(true), MatchedKey: ptrStr("pnpm-linux-abc")}); err == nil {
			t.Fatal("producer action with companion inputs present must fail")
		}
	})

	t.Run("incoherent producer results fail the row", func(t *testing.T) {
		d := declExact()
		// hit true with an empty matched key.
		if _, _, err := DeriveDisposition(d, ProducerResult{Hit: ptrBool(true), MatchedKey: ptrStr("")}); err == nil {
			t.Fatal("hit true with an empty matched key must fail")
		}
		// hit false with a non-empty matched key.
		if _, _, err := DeriveDisposition(d, ProducerResult{Hit: ptrBool(false), MatchedKey: ptrStr("pnpm-linux-abc")}); err == nil {
			t.Fatal("hit false with a non-empty matched key must fail")
		}
	})

	t.Run("prefix fallback is rejected at derivation", func(t *testing.T) {
		d := declExact()
		if _, _, err := DeriveDisposition(d, ProducerResult{Hit: ptrBool(true), MatchedKey: ptrStr("pnpm-linux-")}); err == nil {
			t.Fatal("a prefix-fallback match must be rejected")
		}
	})

	t.Run("disabled mode with both companions absent is accepted", func(t *testing.T) {
		mk, disp, err := DeriveDisposition(declDisabled(), ProducerResult{})
		if err != nil {
			t.Fatalf("disabled/none with no companions must be accepted: %v", err)
		}
		if mk != "" || disp != DispositionDisabled {
			t.Fatalf("derived (%q, %q), want ('', disabled)", mk, disp)
		}
	})

	t.Run("the ring row carries no cache_state path", func(t *testing.T) {
		// §10.5.4: the outcome leaves live on the observation and nowhere
		// else. The ring holds the design columns, the response and the
		// identities a re-fit needs; the declaration already reaches it
		// through comparability_key_digest.
		src := ringRowSource(t)
		for _, name := range append(OutcomeLeafNames(), "cache_state") {
			if strings.Contains(src, `json:"`+name) {
				t.Errorf("the ring row registers cache path %q", name)
			}
		}
	})
}
