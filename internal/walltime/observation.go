package walltime

import (
	"encoding/json"
	"fmt"
)

// ObservationSchema is the schema identifier of contract §13.
const ObservationSchema = "testbucket.wall-observation/v1"

// Invocation is one row of the observation's `invocations` list: contract §13
// gives it one entry per invocation the bucket ran.
type Invocation struct {
	Seq            int      `json:"seq"`
	Units          []string `json:"units"`
	ArgvDigest     Digest   `json:"argv_digest"`
	CwdDigest      Digest   `json:"cwd_digest"`
	Selector       []string `json:"selector"`
	Atoms          []string `json:"atoms"`
	ProcessGroupID string   `json:"process_group_id"`
	StartedMonoNs  Nanos    `json:"started_mono_ns"`
	EndedMonoNs    Nanos    `json:"ended_mono_ns"`
	ElapsedNs      Nanos    `json:"elapsed_ns"`
	ExitCode       int      `json:"exit_code"`
}

// CacheState is the scored cache-state block of contract §10.5.0, checked by
// QC14a on the bucket runner and QC14b at ingest.
//
// `DependencyCacheHit` is a pointer because §10.5 makes its PRESENCE the
// witness: it is present iff the producer is `caller`. An absent hit and a
// `false` hit are different facts — before this field existed, a caller-owned
// miss and absent producer data were indistinguishable, which is the finding
// R14-F2 records — so it must never be defaulted.
type CacheState struct {
	DependencyCacheMode         string `json:"dependency_cache_mode"`
	DependencyCachePrimaryKey   string `json:"dependency_cache_primary_key"`
	DependencyCacheMatchedKey   string `json:"dependency_cache_matched_key"`
	DependencyCacheDisposition  string `json:"dependency_cache_disposition"`
	TransformCacheMode          string `json:"transform_cache_mode"`
	DependencyCacheProducer     string `json:"dependency_cache_producer"`
	DependencyCacheHit          *bool  `json:"dependency_cache_hit,omitempty"`
	MongoBinarySHA256           string `json:"mongo_binary_sha256"`
	MongoBinaryPath             string `json:"mongo_binary_path"`
	MongoBinaryVerifiedOnRunner bool   `json:"mongo_binary_verified_on_runner"`
	ExpectedMongoBinarySHA256   string `json:"expected_mongo_binary_sha256"`
}

// Observation is the one document per bucket of contract §13, written
// atomically and uploaded under `if: always()`.
//
// The three identities are separate fields and none substitutes for another
// (S-6): HeadSHA is the orchestration head commit as GitHub authenticated it,
// CandidateSHA is the testbucket commit the executing binary was built from,
// and WorkloadCommit is the consumer checkout the bucket ran against.
type Observation struct {
	Schema                 string       `json:"schema"`
	ComparabilityKeyDigest Digest       `json:"comparability_key_digest"`
	Repository             string       `json:"repository"`
	HeadSHA                string       `json:"head_sha"`
	CandidateSHA           string       `json:"candidate_sha"`
	WorkloadCommit         string       `json:"workload_commit"`
	RunID                  string       `json:"run_id"`
	RunAttempt             string       `json:"run_attempt"`
	JobID                  string       `json:"job_id"`
	BucketIndex            int          `json:"bucket_index"`
	BucketName             string       `json:"bucket_name"`
	PlanDigest             Digest       `json:"plan_digest"`
	Profile                ProfileBlock `json:"profile"`

	// EstSeconds is the display rounding of AEtaNs and is never a model input
	// (S-1). AEtaNs is the integer-nanosecond prediction the plan made for this
	// bucket, echoed so calibration residuals are computed in the response's
	// own units.
	//
	// A_eta IS THE WALL OBJECTIVE, so it exists only where the plan optimized
	// one. §5.1 gives it to a wall-basis plan; a reporter-basis plan carries no
	// objective at all. It was unconditional here and unconditionally
	// validated, so the assembler manufactured one out of the reporter estimate
	// to satisfy the schema — a reporter observation shipped
	// `"a_eta_ns": "120000000000"` describing an objective nothing had computed.
	// The pointer is what makes absence expressible.
	EstSeconds float64 `json:"est_seconds"`
	AEtaNs     *Nanos  `json:"a_eta_ns,omitempty"`

	ProcessGroupID string `json:"process_group_id"`

	// Diagnostics (S-4). The runner instance name is deliberately not a
	// comparability leaf; the observed label is compared against the key's
	// runner_image_label at admission by QC12.
	ActualRunnerName    string `json:"actual_runner_name"`
	ObservedRunsOnLabel string `json:"observed_runs_on_label"`

	UnitIDs     []string     `json:"unit_ids"`
	Invocations []Invocation `json:"invocations"`

	StartedMonoNs Nanos `json:"started_mono_ns"`
	EndedMonoNs   Nanos `json:"ended_mono_ns"`
	ElapsedNs     Nanos `json:"elapsed_ns"`

	SetupNs          Nanos `json:"setup_ns"`
	ScriptNs         Nanos `json:"script_ns"`
	ScriptOverheadNs Nanos `json:"script_overhead_ns"`
	WrapperNs        Nanos `json:"wrapper_ns"`

	BootIDStart string `json:"boot_id_start"`
	BootIDEnd   string `json:"boot_id_end"`

	// Diagnostics; never a duration and never a campaign date, cutoff or window
	// source — those are always the authenticated Actions API instants of
	// §18.2. RealtimeStart has exactly one further use: it is copied into the
	// ring as observed_start_realtime, the recency stamp of §15.1b.
	RealtimeStart string `json:"realtime_start"`
	RealtimeEnd   string `json:"realtime_end"`

	// CampaignID is present and non-empty iff profile.scored is true. An
	// ordinary unscored observation OMITS the field entirely; it is never
	// serialized empty (R10-D3).
	CampaignID string `json:"campaign_id,omitempty"`

	CacheState             CacheState     `json:"cache_state"`
	CacheDeclarationDigest Digest         `json:"cache_declaration_digest"`
	RuntimeProfile         RuntimeProfile `json:"runtime_profile"`
	RuntimeProfileDigest   Digest         `json:"runtime_profile_digest"`

	Terminal      string `json:"terminal"`
	ExitCode      int    `json:"exit_code"`
	FailureReason string `json:"failure_reason"`

	// Limitations is required and non-empty, and names the Exec-envelope,
	// topology, runner-label and cache-state limits (§22 test 2).
	Limitations []string `json:"limitations"`
}

// NanosPtr is the optional-objective constructor. a_eta_ns exists only where a
// plan optimized an objective, so producers hold a pointer and this is how they
// fill it without taking the address of a temporary at every call site.
func NanosPtr(n int64) *Nanos {
	v := Nanos(n)
	return &v
}

// CanonicalLimitations is the limitation set contract §13 requires every
// observation to carry. It is required and non-empty; §22 test 2 asserts it
// names the Exec-envelope, topology, runner-label and cache-state limits.
func CanonicalLimitations() []string {
	return []string{
		"V is the Exec-envelope interval: it contains wrapper and containment overhead, and excludes wall-exec CLI startup and the spec-file write",
		"invocation time includes the consumer's whole spawned command chain, not Vitest alone",
		"root child waited and reaped; process group signalled and drained where the platform permits probing. A setsid/double-forked descendant leaves the group and is neither signalled nor drained; descendants are never reaped.",
		"this is the instrumented run-bucket interval, not the complete action and not job wall time",
		"est_basis selects the partition weight only; unit topology is store-derived in both bases",
		"runner_image_label is a mutable name, not an image identity; actual_runner_name is a diagnostic and is not part of the comparability key",
		"cache_state records the declared and matched cache disposition; page-cache and filesystem warmth are not bound and remain a paired-run limitation",
		"runtime_profile records the versions and digests this bucket actually executed; QC17 compares it field-by-field against the plan document's declared object",
	}
}

// Validate enforces the structural rules §13 states about the document itself,
// separately from the QC checks ingest applies. It is fail-closed: no field is
// defaulted and no absence is tolerated.
func (o *Observation) Validate() error {
	if o.Schema != ObservationSchema {
		return fmt.Errorf("observation schema is %q, want %q", o.Schema, ObservationSchema)
	}
	if len(o.Limitations) == 0 {
		return fmt.Errorf("observation limitations is required and non-empty")
	}
	prof, err := o.Profile.Parse()
	if err != nil {
		return err
	}
	if !prof.EstBasis.Valid() {
		return fmt.Errorf("profile.est_basis is %q, must be reporter or wall", prof.EstBasis)
	}
	// §13: campaign_id is present and non-empty iff scored; an unscored
	// observation omits it entirely and never serializes it empty.
	if prof.Scored && o.CampaignID == "" {
		return fmt.Errorf("scored observation must carry a non-empty campaign_id")
	}
	if !prof.Scored && o.CampaignID != "" {
		return fmt.Errorf("unscored observation must omit campaign_id, got %q", o.CampaignID)
	}
	// §5.1: est_seconds is the display rounding of a_eta_ns — WHERE THERE IS
	// ONE. Under the wall basis the pair must agree exactly; under the reporter
	// basis there is no objective, a_eta_ns is absent, and est_seconds is the
	// reporter estimate the plan displayed. Requiring the pair unconditionally
	// is what forced a reporter observation to invent an objective.
	switch {
	case o.AEtaNs != nil && prof.EstBasis != BasisWall:
		// ABSENT, not merely consistent. This accepted an a_eta_ns under the
		// reporter basis whenever it agreed with est_seconds — and the
		// assembler derives one from the other, so the agreement was free.
		// §5.1 gives the objective to a wall-basis plan and to no other: a
		// reporter row reporting one describes an optimization that did not
		// happen.
		return fmt.Errorf("a %s-basis observation carries a_eta_ns %d; the objective exists only under the wall basis",
			prof.EstBasis, int64(*o.AEtaNs))
	case o.AEtaNs != nil:
		if want := Round1Seconds(int64(*o.AEtaNs)); o.EstSeconds != want {
			return fmt.Errorf("est_seconds is %v, must be round1(a_eta_ns/1e9) = %v", o.EstSeconds, want)
		}
	case prof.EstBasis == BasisWall:
		return fmt.Errorf("a wall-basis observation carries no a_eta_ns; the objective it was planned against is what §5.1 displays")
	}
	if got := RuntimeProfileDigest(o.RuntimeProfile); got != o.RuntimeProfileDigest {
		return fmt.Errorf("runtime_profile_digest is %s, must be the digest of its own object (%s)",
			o.RuntimeProfileDigest, got)
	}
	// §13.0: there is no loose top-level runner_token — the profile block is
	// the only representation of the profile fields.
	if err := o.rejectLooseProfileKeys(); err != nil {
		return err
	}
	return nil
}

// rejectLooseProfileKeys enforces §13.0's "no loose top-level runner_token":
// the profile block is the sole representation, so a top-level duplicate is
// drift with two sources of truth.
func (o *Observation) rejectLooseProfileKeys() error {
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return err
	}
	for _, k := range []string{"runner_token", "scored", "k", "count", "file_parallelism",
		"bucket_indices", "store_sha256", "expanded_unit_set_digest", "est_basis"} {
		if _, ok := top[k]; ok {
			return fmt.Errorf("observation carries loose top-level %q; the profile block is the only representation (§13.0)", k)
		}
	}
	return nil
}
