package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/invakid404/testbucket/internal/walltime"
)

// runWallCacheState materializes §10.5.0's cache state on the bucket runner.
//
// NOTHING PRODUCED THIS DOCUMENT BEFORE. The run-bucket action passed
// `--cache-state "$TB_CACHE_STATE_FILE"` guarded by a variable that was never
// assigned anywhere, so the assembler serialized the all-zero Go value and
// QC14b refused every scored row — the cache half of the contract had a
// complete declaration path, a complete verification path, and no producer.
//
// The two halves of §10.5.0's table are kept apart here exactly as the
// contract keeps them: the DECLARATION leaves are copied verbatim out of the
// file the runner already verified against the plan's published digest, and
// the OUTCOME leaves are derived from what the restore step actually reported
// plus the binary this runner actually has. Neither half is inferred from the
// other, and no leaf is defaulted — an outcome that cannot be derived fails
// the job here, before the observation is written.
func runWallCacheState(args []string) error {
	fs := flag.NewFlagSet("wall cache-state", flag.ExitOnError)
	declFile := fs.String("declaration", "", "the verified cache declaration this bucket ran under (required)")
	out := fs.String("out", "", "write the cache-state document here (required)")
	hit := fs.String("hit", "", "the restore step's result, exactly true or false. Present iff the producer is caller; empty is NOT false")
	matchedKey := fs.String("matched-key", "", "the restore step's matched key, possibly empty. Present iff the producer is caller")
	mongoBinary := fs.String("mongo-binary", "", "absolute path of the MongoDB binary this bucket executed (required): §10.5.6 records the digest of the file that ran, not of one resident elsewhere")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// WHICH FLAGS WERE PASSED, not which are non-empty. §10.5.3 makes absence
	// meaningful: an empty matched key WITH hit:false is a caller-owned miss,
	// and an empty matched key WITHOUT a hit is absent producer data. Reading
	// the empty string as "false" would collapse the two.
	passed := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { passed[f.Name] = true })

	for _, f := range []struct{ name, v string }{
		{"--declaration", *declFile}, {"--out", *out}, {"--mongo-binary", *mongoBinary},
	} {
		if strings.TrimSpace(f.v) == "" {
			return fmt.Errorf("%s is required", f.name)
		}
	}

	b, err := os.ReadFile(*declFile)
	if err != nil {
		return fmt.Errorf("--declaration: %w", err)
	}
	var decl walltime.CacheDeclaration
	if err := json.Unmarshal(b, &decl); err != nil {
		return fmt.Errorf("--declaration: %w", err)
	}

	var res walltime.ProducerResult
	if passed["hit"] {
		v, err := strconv.ParseBool(strings.TrimSpace(*hit))
		if err != nil {
			return fmt.Errorf("--hit %q: it is true or false and nothing else", *hit)
		}
		res.Hit = &v
	}
	if passed["matched-key"] {
		v := *matchedKey
		res.MatchedKey = &v
	}

	// §10.5.3's table, in the one implementation. A shell re-derivation here
	// would be a second table that could disagree with the one the checks read.
	matched, disposition, err := walltime.DeriveDisposition(decl, res)
	if err != nil {
		return err
	}

	if !filepath.IsAbs(*mongoBinary) {
		return fmt.Errorf("--mongo-binary %q is not absolute; §10.5.6 records the path a later reader can resolve on the runner that ran it", *mongoBinary)
	}
	bin, err := os.ReadFile(*mongoBinary)
	if err != nil {
		return fmt.Errorf("--mongo-binary: %w", err)
	}
	sum := sha256.Sum256(bin)
	executed := hex.EncodeToString(sum[:])
	expected := strings.TrimPrefix(decl.ExpectedMongoBinarySHA256, "sha256:")

	// §10.5.0: dependency_cache_hit is on the row IFF the producer is caller.
	// Producer A's restore result is consumed by the table above and is not
	// serialized: QC14 re-derives the presence pattern from the row, so a hit
	// recorded under `action` would make every such row illegal.
	var rowHit *bool
	if decl.DependencyCacheProducer == walltime.ProducerCaller {
		rowHit = res.Hit
	}

	state := walltime.CacheState{
		// Declaration leaves, verbatim.
		DependencyCacheMode:     decl.DependencyCacheMode,
		TransformCacheMode:      decl.TransformCacheMode,
		DependencyCacheProducer: decl.DependencyCacheProducer,
		// §10.5.1's tuple: the primary key is the declaration's under
		// exact-key and empty under disabled, which is what Validate has
		// already enforced.
		DependencyCachePrimaryKey: decl.DependencyCachePrimaryKey,
		DependencyCacheMatchedKey: matched,

		// Outcome leaves.
		DependencyCacheDisposition:  disposition,
		DependencyCacheHit:          rowHit,
		MongoBinarySHA256:           executed,
		MongoBinaryPath:             *mongoBinary,
		MongoBinaryVerifiedOnRunner: executed == expected,
		ExpectedMongoBinarySHA256:   expected,
	}

	// FAIL HERE, NOT IN THE RECORD JOB. QC14b is the gate this document exists
	// to pass, so running it on the runner that can still explain a failure is
	// strictly better than uploading a row whose only symptom is a rejection
	// in a job that never saw the binary.
	if err := walltime.QC14b(state); err != nil {
		return err
	}
	if err := atomicWriteJSONCompact(*out, state); err != nil {
		return fmt.Errorf("write cache state: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wall cache-state: mode %s, producer %s, disposition %s; written to %s\n",
		state.DependencyCacheMode, state.DependencyCacheProducer, state.DependencyCacheDisposition, *out)
	return nil
}
