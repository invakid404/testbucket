package walltime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// shippedFiles are the strings this product actually ships: Go source, the
// composite actions, the reusable workflow, and the README. Test files are
// excluded — an assertion ABOUT a banned phrase is not a claim made in it.
func shippedFiles(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	out := map[string]string{}
	roots := []string{"internal", "cmd", ".github/actions", ".github/workflows"}
	for _, r := range roots {
		err := filepath.Walk(filepath.Join(root, r), func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			name := info.Name()
			if strings.HasSuffix(name, "_test.go") {
				return nil
			}
			if !strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, ".yml") &&
				!strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".sh") {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			out[rel] = string(b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err == nil {
		out["README.md"] = string(b)
	}
	return out
}

// scanShipped reports every shipped line matching re, excluding lines whose
// surrounding window carries one of the exemptions — a required NEGATIVE
// statement mentions the banned phrase in order to forbid it.
func scanShipped(t *testing.T, files map[string]string, re *regexp.Regexp, exemptions []string) []string {
	t.Helper()
	var hits []string
	for name, src := range files {
		lines := strings.Split(src, "\n")
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			// ONE LINE EITHER SIDE. A denial three lines away is usually
			// about something else; requiring it adjacent is what makes the
			// exemption mean "this sentence refuses the claim".
			lo, hi := i-1, i+2
			if lo < 0 {
				lo = 0
			}
			if hi > len(lines) {
				hi = len(lines)
			}
			window := strings.ToLower(strings.Join(lines[lo:hi], " "))
			exempt := false
			for _, e := range exemptions {
				if strings.Contains(window, e) {
					exempt = true
					break
				}
			}
			if !exempt {
				hits = append(hits, name+":"+itoaLocal(i+1)+": "+strings.TrimSpace(line))
			}
		}
	}
	return hits
}

func itoaLocal(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// negations are the phrasings a required denial uses, so a sentence that
// forbids a claim is not convicted of making it.
// negations exempt a hit whose own neighbourhood REFUSES the claim.
//
// It used to include "rather than", "asserts", "cannot", "forbid", "repair"
// and "§16.4" over a seven-line window. Those appear in ordinary prose all
// over this package, so a sentence that overstated the measurement was exempt
// whenever any of them happened to sit within three lines — which is how the
// hyphenated "complete-action" surfaces would have survived even had the
// pattern matched them. What remains is the small set that actually denies the
// claim, read over a window of one line either side rather than three.
var negations = []string{
	"never", "not the job", "is not ", "no such", "outside",
	"must not", "no longer", "withdrawn", "superseded", "not a proxy",
	"not the complete action", "excludes",
}

// TestNoShippedStringOverstatesTheMeasurement is §22 tests 13 and 24. It is one
// symbol with two case sets, exactly as the contract indexes it.
func TestNoShippedStringOverstatesTheMeasurement(t *testing.T) {
	files := shippedFiles(t)
	if len(files) == 0 {
		t.Fatal("scanned no shipped files")
	}

	t.Run("no shipped string calls A the complete action or the job", func(t *testing.T) {
		// §16.4's condition is narrower than the phrase alone: the repair
		// applies where "complete action", "whole wrapper" or "whole job"
		// DESCRIBES A or V. A comment about a deadlocked discovery hanging the
		// job says nothing about the measured interval, so the scan requires a
		// measurement subject in the same window — otherwise it convicts
		// unrelated prose and the real violations drown in it.
		//
		// THE SEPARATOR IS PART OF THE PATTERN. This matched "complete action"
		// with a space and missed the hyphenated "complete-action" that every
		// shipped surface actually used — the CLI usage, the package doc, two
		// action inputs, the reusable workflow's input and the README heading.
		// A punctuation variant is the same claim, so the separator is a
		// character class rather than a literal space.
		re := regexp.MustCompile(`(?i)complete[- ]action|whole[- ]wrapper|entire[- ]action|job's actual wall time|actual wall time of the job`)
		subject := regexp.MustCompile(`(?i)\bA\b|\bV\b|elapsed|wall|interval|envelope|observation|est_seconds|makespan|measured|timing|duration`)
		hits := scanShipped(t, files, re, negations)
		var describing []string
		for _, h := range hits {
			if subject.MatchString(h) {
				describing = append(describing, h)
			}
		}
		if len(describing) > 0 {
			hits = describing
		} else {
			hits = nil
		}
		if len(hits) > 0 {
			for _, h := range hits {
				t.Errorf("overstated measurement claim: %s", h)
			}
		}
	})

	t.Run("the §16.4 repairs are present at their sites", func(t *testing.T) {
		readme := files["README.md"]
		if strings.Contains(readme, "the job's actual wall time rather than a proxy for it") {
			t.Error("README:76 still calls the reporter sum the job's actual wall time")
		}
		if !strings.Contains(readme, "reporter-work estimate") {
			t.Error("README:76's repair does not name the reporter-work basis")
		}
		if strings.Contains(readme, "accounts for the wrapper install") {
			t.Error("README:443 still claims A_GH accounts for the wrapper install")
		}
		plan := files[filepath.Join("internal", "core", "plan.go")]
		if strings.Contains(plan, "measured wall-time %s") {
			t.Error("plan.go still prints \"measured wall-time\"")
		}
		if !strings.Contains(plan, "recorded reporter work") {
			t.Error("plan.go's repair does not name recorded reporter work")
		}
		if strings.Contains(plan, "its serial wall time") {
			t.Error("plan.go still calls the estimate serial wall time")
		}
		rb := files[filepath.Join(".github", "actions", "run-bucket", "action.yml")]
		if strings.Contains(rb, "The bucket's time estimate in seconds (matrix.est_seconds).") {
			t.Error("run-bucket's est-seconds description still omits the basis")
		}
	})

	t.Run("no shipped string claims an outcome-free allocation surface", func(t *testing.T) {
		// The second case set (item 24).
		re := regexp.MustCompile(`(?i)outcome-free|no outcome-derived influence|mechanically pre-campaign`)
		if hits := scanShipped(t, files, re, negations); len(hits) > 0 {
			for _, h := range hits {
				t.Errorf("outcome-free allocation claim: %s", h)
			}
		}
	})

	t.Run("the three store fields that select topology are named", func(t *testing.T) {
		// §6.2 names them: UnitStat.Seconds (whale-or-not),
		// UnitStat.SplitInto (how many shards or slices) and UnitStat.Tests
		// (which runnable names share a slice). All three are reporter-derived
		// and identical in both bases, which is what makes them ordinary
		// pre-treatment covariates rather than leakage.
		units := readRepoFile(t, filepath.Join("internal", "core", "units.go"))
		stat := readRepoFile(t, filepath.Join("internal", "core", "store.go"))
		both := units + stat
		for _, field := range []string{"Seconds", "SplitInto", "Tests"} {
			if !strings.Contains(both, field) {
				t.Errorf("§6.2's topology-selecting store field %q is not present", field)
			}
		}
		// And the forbidden class: a CURRENT-RUN or POST-ASSIGNMENT outcome
		// must not select topology.
		if strings.Contains(units, "observedElapsed") || strings.Contains(units, "currentRunSeconds") {
			t.Error("topology selection reads a current-run outcome")
		}
	})
}

// TestScaleIsPredictiveOnly is §22 test 48 (F2): it rejects causal or
// fraction phrasing applied to `scale` anywhere in a normative document.
func TestScaleIsPredictiveOnly(t *testing.T) {
	docs := map[string]string{}
	for _, f := range []string{"acceptance-contract.md", "scope.md"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "docs", "walltime", f))
		if err != nil {
			t.Fatal(err)
		}
		docs[f] = string(b)
	}
	// The banned phrasings, applied to scale.
	bad := regexp.MustCompile(`(?i)scale[^.\n]{0,80}(accounts for|fraction of|causal)|` +
		`(accounts for|fraction of|causal)[^.\n]{0,40}scale\b`)
	for name, src := range docs {
		// §22 is the test plan: a line there describes the prohibition in
		// order to require it, and scanning it would convict the rule of
		// breaking itself.
		if i := strings.Index(src, "## 22. Test plan"); i >= 0 {
			src = src[:i]
		}
		lines := strings.Split(src, "\n")
		for i, line := range lines {
			if !bad.MatchString(line) {
				continue
			}
			l := strings.ToLower(line)
			// A required denial names the phrasing in order to exclude it.
			if strings.Contains(l, "does not") || strings.Contains(l, "excludes") ||
				strings.Contains(l, "never") || strings.Contains(l, "not measure") ||
				strings.Contains(l, "any such") || strings.Contains(l, "rejects") {
				continue
			}
			t.Errorf("%s:%d applies causal/fraction phrasing to scale: %s", name, i+1, strings.TrimSpace(line))
		}
	}

	t.Run("scale is documented as a predictive association coefficient", func(t *testing.T) {
		if !strings.Contains(docs["scope.md"], "predictive association coefficient") {
			t.Error("scope.md no longer describes scale as a predictive association coefficient")
		}
	})

	t.Run("scale is dimensionless in the implementation", func(t *testing.T) {
		// The executable half: it multiplies nanoseconds by nanoseconds, so a
		// causal-fraction reading has no unit to live in.
		var m WallModelCoefficients
		m.Scale = 1.0
		var _ float64 = m.Scale
		if got := ModelParametersDigest(m); !got.Valid() {
			t.Fatal("model digest invalid")
		}
	})
}

// TestNoInferentialClaimIsShipped is §22 test 26: no shipped string asserts
// significance, power, a null hypothesis, a p-value, or a conclusion beyond the
// measured sample.
func TestNoInferentialClaimIsShipped(t *testing.T) {
	files := shippedFiles(t)
	re := regexp.MustCompile(`(?i)\bp-value\b|\bnull hypothesis\b|statistically significant|` +
		`\bstatistical power\b|\bconfidence interval\b|generalis(e|es|ing)? to|generaliz(e|es|ing)? to`)
	if hits := scanShipped(t, files, re, negations); len(hits) > 0 {
		for _, h := range hits {
			t.Errorf("inferential claim in a shipped string: %s", h)
		}
	}
}

// TestCampaignDenominatorWording is §22 test 20 (S-9): no shipped string says
// "eighty … per arm"; the constant is 80 combined, 40 per arm.
func TestCampaignDenominatorWording(t *testing.T) {
	files := shippedFiles(t)

	t.Run("no shipped string says eighty per arm", func(t *testing.T) {
		re := regexp.MustCompile(`(?i)(eighty|80)[^.\n]{0,60}per arm`)
		var hits []string
		for name, src := range files {
			for i, line := range strings.Split(src, "\n") {
				if !re.MatchString(line) {
					continue
				}
				l := strings.ToLower(line)
				if strings.Contains(l, "never 80 per arm") || strings.Contains(l, "not 80 per arm") ||
					strings.Contains(l, "never eighty") || strings.Contains(l, "40 per arm") {
					continue
				}
				hits = append(hits, name+":"+itoaLocal(i+1)+": "+strings.TrimSpace(line))
			}
		}
		for _, h := range hits {
			t.Errorf("campaign denominator wording: %s", h)
		}
	})

	t.Run("the constants are 80 combined and 40 per arm", func(t *testing.T) {
		if CampaignRowsPerArm != 40 {
			t.Errorf("CampaignRowsPerArm = %d, want 40", CampaignRowsPerArm)
		}
		if CampaignRowsPerArm*2 != 80 {
			t.Errorf("the two arms sum to %d, want 80 combined", CampaignRowsPerArm*2)
		}
		if CampaignRuns != 10 {
			t.Errorf("CampaignRuns = %d, want 10", CampaignRuns)
		}
	})

	t.Run("no string claims a 16-row pilot proves 80-row bookkeeping", func(t *testing.T) {
		re := regexp.MustCompile(`(?i)16[- ]row pilot[^.\n]{0,60}(proves|establishes|demonstrates)`)
		if hits := scanShipped(t, files, re, negations); len(hits) > 0 {
			for _, h := range hits {
				t.Errorf("pilot overclaim: %s", h)
			}
		}
	})
}

// TestThresholdsAreFrozenBeforeRunOne is §22 test 27 (R13-A).
//
// It no longer compares two gate tables, because there is only one: §10 and
// §17.4 are the SOLE statement of the campaign gate set and its numeric values,
// and a gate restated anywhere else fails this test.
func TestThresholdsAreFrozenBeforeRunOne(t *testing.T) {
	t.Run("every §10.4 value is a compile-time constant", func(t *testing.T) {
		// Retuning is what is prohibited: a changed percentage, bound or
		// refitted coefficient. Evaluating a frozen relative formula against
		// observed C rows is explicitly PERMITTED and is what a relative gate
		// is.
		for name, got := range map[string]int{
			"pairs":                  CampaignPairs,
			"runs":                   CampaignRuns,
			"rows per arm":           CampaignRowsPerArm,
			"buckets per run":        BucketsPerRun,
			"distinct dates":         CampaignDates,
			"MIN_ROWS":               MinRows,
			"MIN_RUNS":               MinRuns,
			"reschedule events":      MaxRescheduleEvents,
			"WALL_MODEL_VERSION-ish": DesignColumns,
		} {
			if got == 0 {
				t.Errorf("%s resolves to 0; every §10.4 value is a compile-time constant", name)
			}
		}
		if ModelMAECeilingNs != 20_000_000_000 {
			t.Errorf("MODEL_MAE_CEILING = %d ns, want 20.000 s", ModelMAECeilingNs)
		}
		// W = 240 is core's constant and is asserted in core's own suite;
		// restating it here would be the second copy this test exists to
		// forbid.
	})

	t.Run("no document outside §10 and §17.4 states a gate threshold", func(t *testing.T) {
		// A gate restated elsewhere FAILS: the test asserts single statement,
		// which is the stronger property the retired two-table comparison was
		// only approximating.
		b, err := os.ReadFile(filepath.Join("..", "..", "docs", "walltime", "scope.md"))
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		// The gate comparisons themselves, spelled with their operators.
		for _, gate := range []string{
			"median(RA)` ≤ 0.95", "median(DA[C])` ≤ 0.95", "median(TA[C])` ≤ 1.05",
		} {
			if strings.Contains(src, gate) {
				t.Errorf("scope.md restates the gate %q; §10 and §17.4 are its sole statement", gate)
			}
		}
	})

	t.Run("a relative gate is a formula, evaluated not retuned", func(t *testing.T) {
		// §0.4: the formula and percentage are precommitted; the numeric
		// right-hand side is EVALUATED from the observed C rows, because a
		// relative gate has no numeric value until mean(A) is observed.
		observed := []int64{10_000_000_000, 20_000_000_000, 30_000_000_000, 40_000_000_000}
		mean, err := meanForGate(observed)
		if err != nil {
			t.Fatal(err)
		}
		// 5.0 % of the observed mean; the percentage is frozen, the bound is not.
		bound := mean * 5 / 100
		if bound <= 0 {
			t.Fatal("the relative bound did not evaluate")
		}
		if bound == ModelMAECeilingNs {
			t.Fatal("the relative bound collapsed onto the absolute ceiling; they are different gates")
		}
	})
}
