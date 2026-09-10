package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

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

	if err := fillIntervals(&obs, &bucket, *dir); err != nil {
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
	// WRITTEN COMPACT, and that is not a style choice.
	//
	// The observation carries the plan's canonical profile block VERBATIM, and
	// QC13 compares it byte for byte. `json.MarshalIndent` re-indents embedded
	// raw JSON, so a pretty-printed document arrived with a reformatted block
	// and was rejected by the one consumer it exists for. A canonical value
	// only stays canonical if nothing on its path reformats it.
	if err := atomicWriteJSONCompact(*out, obs); err != nil {
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

// fillIntervals derives every endpoint, identity and span from the RECORDS.
//
// It used to copy the three aggregate durations and leave the rest zero: the
// action and every invocation carried `started_mono_ns` and `ended_mono_ns` of
// 0, both boot identities empty, `realtime_end` empty, and the verifier's
// realtime BRACKET — two instants — in the single-instant `realtime_start`.
// The shipped ingest rejected every such document, so the assembler's whole
// output was unusable by the only consumer it has.
//
// The endpoints exist in the records; nothing needed inventing. Each boundary
// pair is read where it was written, which is also what makes the invocation
// endpoints distinct from the envelope's — QC7 rejects an invocation that
// reuses them, and zeroes made every one of them identical.
func fillIntervals(obs *walltime.Observation, plan *core.PlanBucket, dir string) error {
	recs, err := walltime.ReadDir(dir)
	if err != nil {
		return err
	}

	type pair struct{ start, end *walltime.Record }
	byLevel := map[walltime.Level]*pair{}
	var invPairs []*pair
	for i := range recs {
		r := &recs[i]
		if r.Kind != "boundary" {
			continue
		}
		if r.Level == walltime.LevelInvocation {
			if r.Boundary == "start" {
				invPairs = append(invPairs, &pair{start: r})
				continue
			}
			// Close the most recent open invocation: they nest inside the
			// script and are written in order.
			for j := len(invPairs) - 1; j >= 0; j-- {
				if invPairs[j].end == nil {
					invPairs[j].end = r
					break
				}
			}
			continue
		}
		p := byLevel[r.Level]
		if p == nil {
			p = &pair{}
			byLevel[r.Level] = p
		}
		if r.Boundary == "start" {
			p.start = r
		} else {
			p.end = r
		}
	}

	action := byLevel[walltime.LevelAction]
	if action == nil || action.start == nil || action.end == nil {
		return fmt.Errorf("the records carry no closed action envelope; there is no interval to report")
	}
	obs.StartedMonoNs = walltime.Nanos(action.start.Instant.Mono)
	obs.EndedMonoNs = walltime.Nanos(action.end.Instant.Mono)
	obs.ElapsedNs = obs.EndedMonoNs - obs.StartedMonoNs
	// ONE INSTANT EACH, taken from the record's own bracket.
	//
	// A record's `realtime` is a BEFORE/AFTER pair straddling the monotonic
	// read, written as "a/b". Copying that string whole gave the observation a
	// two-instant value in a single-instant field, and QC16 -- which parses it
	// as the recency key -- rejected every row. The conservative edge of each
	// bracket is the one that cannot overstate the interval: the LATER edge of
	// the opening bracket, and the EARLIER edge of the closing one.
	startBefore, startAfter, err := action.start.Instant.RealtimeBracket()
	if err != nil {
		return fmt.Errorf("action opening reading: %w", err)
	}
	endBefore, _, err := action.end.Instant.RealtimeBracket()
	if err != nil {
		return fmt.Errorf("action closing reading: %w", err)
	}
	_ = startBefore
	obs.RealtimeStart = startAfter.UTC().Format(time.RFC3339Nano)
	obs.RealtimeEnd = endBefore.UTC().Format(time.RFC3339Nano)
	obs.BootIDStart = action.start.Instant.BootID
	obs.BootIDEnd = action.end.Instant.BootID
	obs.Terminal = action.end.Terminal
	if obs.Terminal == "" {
		obs.Terminal = "passed"
	}
	obs.ExitCode = action.end.Proc.ExitCode

	if p := byLevel[walltime.LevelSetup]; p != nil && p.start != nil && p.end != nil {
		obs.SetupNs = walltime.Nanos(p.end.Instant.Mono - p.start.Instant.Mono)
	}
	if p := byLevel[walltime.LevelScript]; p != nil && p.start != nil && p.end != nil {
		obs.ScriptNs = walltime.Nanos(p.end.Instant.Mono - p.start.Instant.Mono)
		if obs.ProcessGroupID == "" && p.end.Proc.PGID != 0 {
			obs.ProcessGroupID = fmt.Sprintf("%d", p.end.Proc.PGID)
		}
	}

	var invNs []int64
	for seq, ip := range invPairs {
		if ip.start == nil || ip.end == nil {
			return fmt.Errorf("invocation %d has no closed start/end pair", seq)
		}
		inv := walltime.Invocation{
			Seq:            seq,
			StartedMonoNs:  walltime.Nanos(ip.start.Instant.Mono),
			EndedMonoNs:    walltime.Nanos(ip.end.Instant.Mono),
			ElapsedNs:      walltime.Nanos(ip.end.Instant.Mono - ip.start.Instant.Mono),
			ProcessGroupID: fmt.Sprintf("%d", ip.end.Proc.PGID),
			ExitCode:       ip.end.Proc.ExitCode,
		}
		if ip.end.Spec != nil {
			inv.ArgvDigest = ip.end.Spec.ArgvDigest
			inv.CwdDigest = walltime.DigestJSONOrEmpty(ip.end.Spec.Cwd)
		}
		// THE MEMBERSHIP COMES FROM THE PLAN. Units, selector and atoms are
		// what the plan RENDERED; the records carry digests of them, not the
		// lists, and an observation that omitted them left the audit with
		// nothing to attribute the interval to.
		if plan != nil && seq < len(plan.Invocations) {
			pi := plan.Invocations[seq]
			inv.Units = pi.Units
			inv.Selector = pi.Selector
			inv.Atoms = pi.Atoms
		}
		obs.Invocations = append(obs.Invocations, inv)
		invNs = append(invNs, int64(inv.ElapsedNs))
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
