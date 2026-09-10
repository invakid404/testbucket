package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/invakid404/testbucket/internal/core"
	"github.com/invakid404/testbucket/internal/nsmath"
	"github.com/invakid404/testbucket/internal/walltime"
)

// runWallAssemble builds the canonical observation §13 defines, from the
// evidence the measured bucket actually produced.
//
// THIS IS THE MISSING HALF OF THE FEEDBACK LOOP. `ingest --wall-observations`
// reads observation documents, qualifies them and appends them to the ring —
// and nothing produced one. The test job uploaded raw record directories, the
// record job downloaded timing events and the shard plan, and no step turned
// the two into the document the loop consumes. A reader with nothing to read
// is not a loop.
//
// Every field is DERIVED from evidence rather than asserted: the intervals
// from the wall records, the profile copied verbatim from the plan, the
// invocation identities from the records compared against what the plan
// rendered. What a caller supplies is only what the runner alone knows — the
// run identity, the observed label, and the cache outcome its own restore step
// reported.
func runWallAssemble(args []string) error {
	fs := flag.NewFlagSet("wall assemble-observation", flag.ExitOnError)
	dir := fs.String("dir", "", "the records directory this bucket measured into (required)")
	shardPlan := fs.String("shard-plan", "", "the plan artifact this bucket was fanned out from (required): the profile block, the bucket's unit set and its rendered invocations are copied from it")
	bucketName := fs.String("bucket-name", "", "the bucket this observation is for (required)")
	out := fs.String("out", "", "write the observation document here (required)")
	runsOnLabel := fs.String("runs-on-label", "", "the runs-on label this bucket OBSERVED; QC12 compares it against the one the plan was dispatched under")
	actualRunner := fs.String("actual-runner-name", "", "the runner instance name, a diagnostic that is never part of the comparability key")
	cacheStateFile := fs.String("cache-state", "", "the cache outcome this bucket's own restore step reported, as JSON")
	cacheDeclFile := fs.String("cache-declaration-file", "", "the verified cache declaration, for its digest")
	headSHA := fs.String("head-sha", "", "the consumer commit this run measured; one of QC15's three provenance identities")
	candidateSHA := fs.String("candidate-sha", "", "the testbucket commit the measuring binary was built from (QC15)")
	workloadCommit := fs.String("workload-commit", "", "the workload checkout this run executed against (QC15)")
	var ids runIdentityFlags
	ids.bind(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, f := range []struct{ name, v string }{
		{"--dir", *dir}, {"--shard-plan", *shardPlan},
		{"--bucket-name", *bucketName}, {"--out", *out},
	} {
		if strings.TrimSpace(f.v) == "" {
			return fmt.Errorf("%s is required", f.name)
		}
	}

	// THE INTERVALS COME FROM THE RECORDS, through the same verifier the
	// wall-time action runs. A producer that reported its own spans could
	// report ones the readings do not support.
	v, err := walltime.VerifyDir(walltime.VerifyOptions{Dir: *dir})
	if err != nil {
		return err
	}
	if !v.Complete {
		return fmt.Errorf("the records in %s are not a complete measurement (%d finding(s)); "+
			"no observation is assembled from an incomplete envelope", *dir, len(v.Findings))
	}

	doc, err := core.ParseShardPlan(*shardPlan)
	if err != nil {
		return fmt.Errorf("--shard-plan: %w", err)
	}
	bucket, err := planBucketNamed(doc, *bucketName)
	if err != nil {
		return err
	}
	planDigest, err := walltime.DigestJSON(doc)
	if err != nil {
		return err
	}

	obs := walltime.Observation{
		Schema:                 walltime.ObservationSchema,
		ComparabilityKeyDigest: walltime.Digest(doc.ComparabilityKeyDigest),
		PlanDigest:             planDigest,
		BucketName:             *bucketName,
		BucketIndex:            bucket.Index,
		ObservedRunsOnLabel:    *runsOnLabel,
		ActualRunnerName:       *actualRunner,
		Limitations:            walltime.CanonicalLimitations(),
	}
	// The profile is copied VERBATIM: §13.0 makes the observation carry the
	// plan's block, and QC13 compares the two byte for byte. Re-deriving it
	// here would compare this command against itself.
	if len(doc.Profile) > 0 {
		if err := json.Unmarshal(doc.Profile, &obs.Profile); err != nil {
			return fmt.Errorf("plan profile: %w", err)
		}
	}
	obs.RuntimeProfile = runtimeProfileFromMap(doc.RuntimeProfileDeclared)
	obs.RuntimeProfileDigest = walltime.RuntimeProfileDigest(obs.RuntimeProfile)

	id := ids.identity()
	obs.Repository, obs.RunID, obs.RunAttempt = id.Repository, id.RunID, id.AttemptID
	obs.JobID = id.Job
	obs.CampaignID = id.CampaignID
	// The three provenance identities are SEPARATE and none is derivable from
	// another (QC15), so each is passed rather than inferred.
	obs.HeadSHA, obs.CandidateSHA, obs.WorkloadCommit = *headSHA, *candidateSHA, *workloadCommit

	// THE PAIR IS DERIVED, NEVER COPIED TWICE.
	//
	// §5.1 ties the two: est_seconds is exactly round1(a_eta_ns / 1e9). Taking
	// each from the plan independently lets them disagree, and an observation
	// whose displayed estimate is not the objective it reports is one the
	// schema refuses — correctly, because a reader comparing the two would be
	// comparing a number against something it was never derived from.
	if bucket.AEtaNs != nil {
		obs.AEtaNs = walltime.Nanos(*bucket.AEtaNs)
		obs.EstSeconds = nsmath.Round1Seconds(int64(obs.AEtaNs))
	} else {
		// A reporter-basis plan carries no objective, so the objective is the
		// estimate it does carry, converted the one checked way.
		ns, err := core.ReporterNsFromSeconds(bucket.Seconds)
		if err != nil {
			return fmt.Errorf("bucket %s estimate: %w", bucket.Name, err)
		}
		obs.AEtaNs = walltime.Nanos(ns)
		obs.EstSeconds = nsmath.Round1Seconds(ns)
	}
	for _, u := range bucket.Units {
		obs.UnitIDs = append(obs.UnitIDs, u.ID)
	}

	if err := fillIntervals(&obs, v, *dir); err != nil {
		return err
	}

	if strings.TrimSpace(*cacheStateFile) != "" {
		b, err := os.ReadFile(*cacheStateFile)
		if err != nil {
			return fmt.Errorf("--cache-state: %w", err)
		}
		if err := json.Unmarshal(b, &obs.CacheState); err != nil {
			return fmt.Errorf("--cache-state: %w", err)
		}
	}
	if strings.TrimSpace(*cacheDeclFile) != "" {
		b, err := os.ReadFile(*cacheDeclFile)
		if err != nil {
			return fmt.Errorf("--cache-declaration-file: %w", err)
		}
		var decl walltime.CacheDeclaration
		if err := json.Unmarshal(b, &decl); err != nil {
			return fmt.Errorf("--cache-declaration-file: %w", err)
		}
		obs.CacheDeclarationDigest = decl.Digest()
	}

	// Refuse to emit a document the loop would only reject. An observation
	// that cannot pass its own schema validation is not evidence of anything,
	// and writing it would move the failure to a job that cannot explain it.
	if err := obs.Validate(); err != nil {
		return fmt.Errorf("the assembled observation is not valid: %w", err)
	}
	if err := atomicWriteJSON(*out, obs); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testbucket wall: assembled the observation for %s into %s\n", *bucketName, *out)
	return nil
}

// planBucketNamed finds the bucket this observation is for.
func planBucketNamed(doc *core.PlanDocument, name string) (core.PlanBucket, error) {
	for _, b := range doc.Buckets {
		if b.Name == name {
			return b, nil
		}
	}
	return core.PlanBucket{}, fmt.Errorf("the plan has no bucket named %q", name)
}

// fillIntervals copies §3.1's spans out of the verified records and derives the
// two component spans in the checked domain.
func fillIntervals(obs *walltime.Observation, v *walltime.Verdict, dir string) error {
	obs.ElapsedNs = walltime.Nanos(v.ActionNs)
	obs.SetupNs = walltime.Nanos(v.SetupNs)
	obs.ScriptNs = walltime.Nanos(v.ScriptNs)
	obs.Terminal = v.Terminal
	if obs.Terminal == "" {
		obs.Terminal = "passed"
	}
	obs.RealtimeStart = v.StartedAt

	recs, err := walltime.ReadDir(dir)
	if err != nil {
		return err
	}
	var invNs []int64
	seq := 0
	for _, r := range recs {
		if r.Kind != "boundary" || r.Level != walltime.LevelInvocation {
			continue
		}
		if r.Boundary == "start" {
			continue
		}
		// One invocation per closing record, in the order the records carry.
		inv := walltime.Invocation{
			Seq:            seq,
			ProcessGroupID: fmt.Sprintf("%d", r.Proc.PGID),
			ExitCode:       r.Proc.ExitCode,
		}
		if r.Spec != nil {
			inv.ArgvDigest = r.Spec.ArgvDigest
			inv.CwdDigest = walltime.DigestJSONOrEmpty(r.Spec.Cwd)
		}
		obs.Invocations = append(obs.Invocations, inv)
		seq++
	}
	for i := range obs.Invocations {
		invNs = append(invNs, int64(obs.Invocations[i].ElapsedNs))
	}
	if len(v.InvocationNs) == len(obs.Invocations) {
		for i, ns := range v.InvocationNs {
			obs.Invocations[i].ElapsedNs = walltime.Nanos(ns)
		}
		invNs = v.InvocationNs
	}
	if obs.ProcessGroupID == "" && len(obs.Invocations) > 0 {
		obs.ProcessGroupID = obs.Invocations[0].ProcessGroupID
	}

	spans, err := walltime.ComputeComponentSpans(
		int64(obs.ElapsedNs), int64(obs.SetupNs), int64(obs.ScriptNs), invNs)
	if err != nil {
		return err
	}
	obs.ScriptOverheadNs = walltime.Nanos(spans.ScriptOverheadNs)
	obs.WrapperNs = walltime.Nanos(spans.WrapperNs)
	return nil
}
