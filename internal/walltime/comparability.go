package walltime

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// comparabilityLeafOrder is the leaf sequence of contract §15.3's producer
// table, which the `memberships.comparability_key` projection of the field
// registry carries in the same order.
//
// As with the runtime profile, the digest is defined "in this order" rather
// than in RFC 8785's lexicographic order, so the key gets an ordered
// serializer. Sorting would put `arch` first and silently change every stored
// digest.
var comparabilityLeafOrder = [15]string{
	"runner_class",
	"runner_image_label",
	"os",
	"arch",
	"node_version",
	"pnpm_version",
	"vitest_version",
	"testbucket_sha256",
	"working_dir",
	"facade_command",
	"setup_command",
	"lock_sha256",
	"discovery_mode",
	"exclusions",
	"cache_declaration_digest",
}

// ComparabilityProfile is the execution profile of contract §15.3. EVERY leaf
// is sourced from the PLAN JOB: the key must be constructible before the matrix
// is emitted, so the eight matrix test-job runners — which do not exist at plan
// time — are never key inputs.
//
// Two leaves are the S-4 repairs. RunnerClass replaces the former `runner_name`
// because `${{ runner.name }}` is a per-instance name with no stability
// contract across runs, which made MIN_RUNS = 3 under one key unreachable; the
// instance name survives as the diagnostic `actual_runner_name` on the
// observation. RunnerImageLabel is a required caller-passed input because no
// context yields the caller's resolved `runs-on` label.
type ComparabilityProfile struct {
	RunnerClass      string   `json:"runner_class"`
	RunnerImageLabel string   `json:"runner_image_label"`
	OS               string   `json:"os"`
	Arch             string   `json:"arch"`
	NodeVersion      string   `json:"node_version"`
	PnpmVersion      string   `json:"pnpm_version"`
	VitestVersion    string   `json:"vitest_version"`
	TestbucketSHA256 string   `json:"testbucket_sha256"`
	WorkingDir       string   `json:"working_dir"`
	FacadeCommand    string   `json:"facade_command"`
	SetupCommand     string   `json:"setup_command"`
	LockSHA256       string   `json:"lock_sha256"`
	DiscoveryMode    string   `json:"discovery_mode"`
	Exclusions       []string `json:"exclusions"`
	// CacheDeclarationDigest binds the declaration leaves of §10.5.0 into the
	// key, so a change to any declared cache leaf resets wall history exactly
	// as a lock_sha256 change does. Pairwise equality protects B against C
	// inside one pair; it does nothing to stop PRE-CAMPAIGN fitting from mixing
	// `disabled` runs with `exact-key` runs under one model.
	//
	// The two per-job OUTCOMES — matched key and disposition — are deliberately
	// NOT here: they vary legitimately between bucket jobs of one run, so
	// keying on them would reset history on ordinary cache behaviour.
	CacheDeclarationDigest string `json:"cache_declaration_digest"`
}

// leaf returns the value of the 1-based leaf n of §15.3's table.
func (p ComparabilityProfile) leaf(n int) (name string, value any) {
	switch n {
	case 1:
		return comparabilityLeafOrder[0], p.RunnerClass
	case 2:
		return comparabilityLeafOrder[1], p.RunnerImageLabel
	case 3:
		return comparabilityLeafOrder[2], p.OS
	case 4:
		return comparabilityLeafOrder[3], p.Arch
	case 5:
		return comparabilityLeafOrder[4], p.NodeVersion
	case 6:
		return comparabilityLeafOrder[5], p.PnpmVersion
	case 7:
		return comparabilityLeafOrder[6], p.VitestVersion
	case 8:
		return comparabilityLeafOrder[7], p.TestbucketSHA256
	case 9:
		return comparabilityLeafOrder[8], p.WorkingDir
	case 10:
		return comparabilityLeafOrder[9], p.FacadeCommand
	case 11:
		return comparabilityLeafOrder[10], p.SetupCommand
	case 12:
		return comparabilityLeafOrder[11], p.LockSHA256
	case 13:
		return comparabilityLeafOrder[12], p.DiscoveryMode
	case 14:
		exc := p.Exclusions
		if exc == nil {
			exc = []string{}
		}
		return comparabilityLeafOrder[13], exc
	case 15:
		return comparabilityLeafOrder[14], p.CacheDeclarationDigest
	}
	panic(fmt.Sprintf("walltime: comparability profile has no leaf %d", n))
}

// OrderedJSON renders the fifteen leaves in §15.3's declared order.
func (p ComparabilityProfile) OrderedJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i := 1; i <= len(comparabilityLeafOrder); i++ {
		if i > 1 {
			b.WriteByte(',')
		}
		name, value := p.leaf(i)
		writeCanonicalString(&b, name)
		b.WriteByte(':')
		switch v := value.(type) {
		case string:
			writeCanonicalString(&b, v)
		case []string:
			b.WriteByte('[')
			for j, s := range v {
				if j > 0 {
					b.WriteByte(',')
				}
				writeCanonicalString(&b, s)
			}
			b.WriteByte(']')
		default:
			enc, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			b.Write(enc)
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// ComparabilityKeyDigest is SHA-256 over the canonical JSON of §15.3's fifteen
// leaves in that section's order. A change to any leaf clears
// `wall.observations`, sets the model status `insufficient`, and leaves the
// reporter EWMAs untouched — the two reset rules are independent.
func ComparabilityKeyDigest(p ComparabilityProfile) (Digest, error) {
	b, err := p.OrderedJSON()
	if err != nil {
		return "", err
	}
	return DigestBytes(b), nil
}

// QC12 compares the label the bucket actually received against the key's
// `runner_image_label`, and fails the row on divergence NAMING BOTH values.
//
// Plan job and test jobs are expected to share one runner label because they
// are configured in one workflow. A difference is a misconfiguration to be
// detected and reported, not silently tolerated.
//
// It also rejects a row whose file parallelism exceeds 1: such rows never
// train, so the check keeps them out of history rather than letting them
// contaminate it.
func QC12(keyLabel, observedLabel string, fileParallelism int) error {
	if observedLabel == "" {
		return fmt.Errorf("QC12: observation carries no observed_runs_on_label")
	}
	if keyLabel != observedLabel {
		return fmt.Errorf("QC12: runs-on label mismatch: plan key declared %q, bucket observed %q",
			keyLabel, observedLabel)
	}
	if fileParallelism > 1 {
		return fmt.Errorf("QC12: file_parallelism is %d; rows above 1 never train", fileParallelism)
	}
	return nil
}

// ComparabilityLeafNames returns the ordered leaf names, so a test can
// enumerate the membership rather than transcribing it.
func ComparabilityLeafNames() []string {
	return append([]string(nil), comparabilityLeafOrder[:]...)
}
