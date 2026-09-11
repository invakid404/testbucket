package walltime

import (
	"fmt"
	"sort"
)

// The four representations of contract §21, each defined once.
//
// An earlier revision called these "ordered per-action partitions" and required
// them equal in both directions — a claim none of them can satisfy, because
// they do not share a domain: `plan` materializes two caller inputs into one
// action input, `run-bucket` receives three inputs the caller never supplies,
// and the scored-required projection deliberately omits a defaulted and two
// conditional names. Equality between unlike projections is not a stronger
// check; it is an untrue one. Only relations that exist are asserted here.

// WorkflowInputUnion is `U`: the reusable workflow's caller-input union, in the
// order §21 lists it. This list IS the order.
func WorkflowInputUnion() []string {
	return []string{
		"est-basis",
		"scored",
		"same-repository-workload",
		"runner-class",
		"runs-on-label",
		"cache-declaration-json",
		"cache-declaration-digest-expected",
		"dependency-cache-producer",
		"dependency-cache-matched-key",
		"dependency-cache-hit",
		"candidate-sha",
		"workload-commit",
		"campaign-id",
		"wall-observations-dir",
		"campaign-config-json",
		"campaign-config-digest-expected",
	}
}

// Job names for the route relation.
const (
	JobPlan   = "plan job"
	JobBucket = "bucket job"
	JobRecord = "record job"
)

// WorkflowRoute is `W`: which jobs each caller input reaches.
//
// It is a ROUTE, not a partition. `runs-on-label`, `candidate-sha` and
// `workload-commit` each reach more than one job, so the sets are NOT disjoint
// and no partition arithmetic applies. What is required is that every W[job] is
// an ordered subsequence of U and every member of U reaches at least one job.
func WorkflowRoute() map[string][]string {
	return map[string][]string{
		JobPlan: {
			"est-basis", "scored", "same-repository-workload",
			"runner-class", "runs-on-label",
			"cache-declaration-json", "cache-declaration-digest-expected",
			"candidate-sha", "workload-commit",
		},
		JobBucket: {
			"runs-on-label", "dependency-cache-producer",
			"dependency-cache-matched-key", "dependency-cache-hit",
			"candidate-sha", "workload-commit", "campaign-id",
		},
		JobRecord: {
			"wall-observations-dir", "campaign-config-json", "campaign-config-digest-expected",
		},
	}
}

// Action names.
const (
	ActionPlan      = "plan"
	ActionRunBucket = "run-bucket"
	ActionRecord    = "record"
)

// AddedActionInputs is `A`: the added action inputs, ordered canonically by
// §21's action table. The order is DECLARED, not inferred.
func AddedActionInputs() map[string][]string {
	return map[string][]string{
		ActionPlan: {
			"est-basis", "runner-class", "runs-on-label", "scored",
			"same-repository-workload",
			"cache-declaration-file", "candidate-sha", "workload-commit",
		},
		ActionRunBucket: {
			"runs-on-label", "cache-declaration-file", "cache-declaration-digest",
			"dependency-cache-producer", "dependency-cache-matched-key",
			"dependency-cache-hit", "campaign-id", "candidate-sha",
			"workload-commit", "shard-plan",
		},
		ActionRecord: {
			"wall-observations-dir", "campaign-config-json", "campaign-config-digest-expected",
		},
	}
}

// scoredExclusions are the two classes §21 excludes from `S`, by name: the
// DEFAULTED input and the PRODUCER-CONDITIONAL pair. S is never compared as A.
var scoredExclusions = map[string]string{
	"est-basis": "defaulted",
	// DEFAULTED for a sharper reason than a default: §19.3a AD-11 requires
	// `same_repository_workload` FALSE for a scored plan, so a scored caller does
	// not supply it and may not. An input a scored run is forbidden to set is not
	// an input it must pass, and leaving it out of this class would make S demand
	// exactly that (R31-F14).
	"same-repository-workload":     "defaulted",
	"dependency-cache-matched-key": "producer-conditional",
	"dependency-cache-hit":         "producer-conditional",
}

// ScoredRequiredInputs is `S`: which of A[action] a scored caller must supply.
// It inherits A's order and is computed from the exclusion classes rather than
// written out, so a name added to A lands in S automatically.
func ScoredRequiredInputs() map[string][]string {
	out := map[string][]string{}
	for action, inputs := range AddedActionInputs() {
		for _, in := range inputs {
			if _, excluded := scoredExclusions[in]; excluded {
				continue
			}
			out[action] = append(out[action], in)
		}
	}
	return out
}

// PlanJobOutputs is the exhaustive plan-job output set of §21.
func PlanJobOutputs() []string {
	return []string{"matrix", "cache-declaration-json", "cache-declaration-digest"}
}

// TransportTo is `T(W, plan outputs) → A`, which fixes MEMBERSHIP only. It
// cannot fix order as well, because the inputs a job receives from the plan
// job's outputs have no position in U at all.
func TransportTo(action string) ([]string, error) {
	route := WorkflowRoute()
	switch action {
	case ActionPlan:
		// §10.5.2 steps 0b–0c: the two caller inputs collapse into one action
		// input, because the plan job materializes the content and verifies it
		// against the expected digest job-locally.
		var out []string
		for _, in := range route[JobPlan] {
			switch in {
			case "cache-declaration-json":
				out = append(out, "cache-declaration-file")
			case "cache-declaration-digest-expected":
				// consumed by the verify step; contributes no action input
			default:
				out = append(out, in)
			}
		}
		return out, nil
	case ActionRunBucket:
		// §10.5.2 step 3: matrix jobs consume the plan job's published bytes
		// and digest, never a caller pathname. shard-plan is likewise produced
		// by plan.
		out := append([]string(nil), route[JobBucket]...)
		out = append(out, "cache-declaration-file", "cache-declaration-digest", "shard-plan")
		return out, nil
	case ActionRecord:
		return append([]string(nil), route[JobRecord]...), nil
	}
	return nil, fmt.Errorf("no transport defined for action %q", action)
}

// IsOrderedSubsequence reports whether sub appears in seq in order, which is
// the relation §21 requires between each W[job] and U. A set comparison would
// admit an order-only permutation, which §21 explicitly fails.
func IsOrderedSubsequence(sub, seq []string) bool {
	i := 0
	for _, s := range seq {
		if i < len(sub) && sub[i] == s {
			i++
		}
	}
	return i == len(sub)
}

// SameSet reports set equality, for the relations §21 defines as membership.
func SameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
