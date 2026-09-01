package core

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/invakid404/testbucket/internal/runner"
)

// LoadPlannedCoverageForBucket is LoadPlannedCoverage restricted to ONE bucket.
//
// The whole-plan form answers "did the campaign run everything", which only the
// record job — on the default branch, after every bucket — can ask. A measured
// row has to answer a narrower question at the moment it is verified: did THIS
// bucket run exactly the targets, count-shards and name slices the authorised
// plan gave it. Without that, a row can be eligible having measured a script
// that quietly skipped half its work, because the wall-time ledger records how
// long something took and never what it was.
//
// It is a separate entry point rather than a parameter on the existing one so
// the audit consumers rely on stays byte-identical.
func LoadPlannedCoverageForBucket(path string, bucket int) (*PlannedCoverage, error) {
	doc, err := ParseShardPlan(path)
	if err != nil {
		return nil, err
	}
	return PlannedCoverageForBucket(doc, bucket)
}

// ParseShardPlan reads and parses a shard-plan artifact ONCE.
//
// The callers below take the parsed document rather than the path on purpose.
// A path is re-readable, and a control that reads it more than once can have
// each read see a different file: the digest could be taken from the
// authorised plan and the expected coverage from a narrowed one substituted in
// between, which is a complete-looking audit of an incomplete run.
func ParseShardPlan(path string) (*PlanDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read shard plan: %w", err)
	}
	var doc PlanDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse shard plan %s: %w", path, err)
	}
	return &doc, nil
}

// PlannedCoverageForBucket derives one bucket's expected coverage from an
// already-parsed plan.
func PlannedCoverageForBucket(doc *PlanDocument, bucket int) (*PlannedCoverage, error) {
	found := false
	out := &PlannedCoverage{Invocations: map[string]int{}, Runnables: map[string][]string{}}
	for _, b := range doc.Buckets {
		if b.Index != bucket {
			continue
		}
		found = true
		for _, u := range b.Units {
			out.Units++
			for _, p := range u.Packages {
				out.Invocations[p]++
			}
			if u.Kind == runner.KindRunSlice && len(u.Packages) == 1 {
				// The structural Run field first, for the same reason the
				// whole-plan form prefers it: a runnable name can contain the
				// '|' the id joins on, so parsing it back out of the id would
				// split one name into two.
				names := u.Run
				if len(names) == 0 {
					if open := strings.Index(u.ID, "["); open >= 0 && strings.HasSuffix(u.ID, "]") {
						names = strings.Split(u.ID[open+1:len(u.ID)-1], "|")
					}
				}
				out.Runnables[u.Packages[0]] = append(out.Runnables[u.Packages[0]], names...)
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("the shard plan has no bucket %d", bucket)
	}
	return out, nil
}

// BucketIndexOf resolves a bucket NAME to its index in the plan. A measured row
// carries the name the matrix gave it, and the plan is keyed by index; guessing
// the mapping by parsing the name would break the first time a caller renames a
// bucket.
func BucketIndexOf(path, name string) (int, error) {
	doc, err := ParseShardPlan(path)
	if err != nil {
		return 0, err
	}
	return BucketIndexIn(doc, name)
}

// BucketIndexIn resolves a bucket name against an already-parsed plan.
func BucketIndexIn(doc *PlanDocument, name string) (int, error) {
	for _, b := range doc.Buckets {
		if b.Name == name {
			return b.Index, nil
		}
	}
	return 0, fmt.Errorf("the shard plan has no bucket named %q", name)
}
