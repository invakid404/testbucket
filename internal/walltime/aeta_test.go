package walltime

import (
	"strings"
	"testing"
)

func minimalRegistry() AetaRegistry {
	return AetaRegistry{
		Kind: RegistryKind, Version: "1",
		Components: []Component{
			{ID: "action_containment_bootstrap", Parent: "action", Owner: "testbucket",
				Class: ClassActionOnly, Included: true, Formula: FormulaConstant,
				PointNs: 20 * millisecond, IntervalNs: 10 * millisecond},
			{ID: "bucket_script", Parent: "action", Owner: "testbucket",
				Class: ClassPalloc, Included: true, Formula: FormulaPallocSum,
				IntervalFraction: 0.10},
			{ID: "invocation_bootstrap", Parent: "invocation", Owner: "testbucket",
				Class: ClassActionOnly, Included: true, Formula: FormulaPerInvocation,
				PerUnitNs: 30 * millisecond, IntervalNs: 10 * millisecond},
			{ID: "unnamed_overhead", Parent: "action", Owner: "testbucket",
				Class: ClassResidual, Included: true, Formula: FormulaConstant,
				PointNs: 100 * millisecond, BoundNs: 200 * millisecond},
		},
	}
}

func TestRegistryInstantiation(t *testing.T) {
	reg := minimalRegistry()
	got, err := reg.Instantiate(AetaInputs{BucketID: "b1", PallocSeconds: 60, Invocations: 3, Stage2: "sha256:s2"})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	// 20 ms bootstrap + 60 s Palloc + 3 x 30 ms + 100 ms residual.
	want := 20*millisecond + 60*second + 3*30*millisecond + 100*millisecond
	if got.PointNs != want {
		t.Errorf("point = %d ns, want %d", got.PointNs, want)
	}
	if got.UpperNs <= got.LowerNs {
		t.Errorf("the composed interval is degenerate: [%d,%d]", got.LowerNs, got.UpperNs)
	}
	if got.Stage2 != "sha256:s2" {
		t.Errorf("the instance does not carry its Stage-2 parent")
	}
	// Instantiation without a verified Stage-2 receipt is refused: a forecast
	// for a plan nobody derived is not a forecast.
	if _, err := reg.Instantiate(AetaInputs{BucketID: "b1", PallocSeconds: 60, Invocations: 3}); err == nil {
		t.Errorf("instantiated without a Stage-2 receipt")
	}
}

func TestRegistryRefusesUnboundedResidual(t *testing.T) {
	cases := []struct {
		name string
		edit func(*AetaRegistry)
		want string
	}{
		{"a residual with no declared bound", func(r *AetaRegistry) {
			r.Components[3].BoundNs = 0
		}, "bound"},
		{"a residual bound above the frozen per-component limit", func(r *AetaRegistry) {
			r.Components[3].BoundNs = ResidualComponentLimit + 1
		}, "bound"},
		{"a test-dependent component that does not aggregate Palloc", func(r *AetaRegistry) {
			r.Components[1].Formula = FormulaConstant
		}, "aggregate Palloc"},
		{"an action-only component formulated from Palloc", func(r *AetaRegistry) {
			r.Components[0].Formula = FormulaPallocSum
		}, "action-only"},
		{"a duplicated component id", func(r *AetaRegistry) {
			r.Components = append(r.Components, r.Components[0])
		}, "twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := minimalRegistry()
			tc.edit(&reg)
			err := reg.Validate()
			if err == nil {
				t.Fatalf("the registry validated")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestAetaRecomputes is the ETA half of the same audit: the forecast has to be
// re-derivable from the frozen template and the inputs it names, component by
// component, so a point edited after the action cannot pass the calibration
// gate.
func TestAetaRecomputes(t *testing.T) {
	reg := minimalRegistry()
	in := AetaInputs{BucketID: "b1", PallocSeconds: 60, Invocations: 3, Stage2: "sha256:s2"}
	got, err := reg.Instantiate(in)
	if err != nil {
		t.Fatal(err)
	}
	if problems := got.Recompute(reg); len(problems) != 0 {
		t.Fatalf("a freshly instantiated forecast did not recompute: %v", problems)
	}
	for _, tc := range []struct {
		name string
		edit func(*AetaInstance)
		want string
	}{
		{"the point moved after the action", func(a *AetaInstance) { a.PointNs += 5 * second }, "the frozen template gives"},
		{"the interval widened after the action", func(a *AetaInstance) { a.UpperNs += 30 * second }, "the frozen template gives"},
		{"one component was edited", func(a *AetaInstance) {
			a.Components[0].PointNs += second
			a.PointNs += second
		}, "component"},
		{"the inputs were restated", func(a *AetaInstance) { a.Inputs.Invocations = 99 }, "the frozen template gives"},
		{"it names another plan", func(a *AetaInstance) { a.Stage2 = "sha256:other" }, "Stage-2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edited, err := reg.Instantiate(in)
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(edited)
			problems := edited.Recompute(reg)
			if len(problems) == 0 {
				t.Fatalf("the edit survived recomputation")
			}
			if !strings.Contains(strings.Join(problems, "; "), tc.want) {
				t.Errorf("problems %v do not mention %q", problems, tc.want)
			}
		})
	}

	// A forecast instantiated from a DIFFERENT registry cannot be checked
	// against this one, and saying so is the answer rather than comparing
	// numbers from two templates.
	other := minimalRegistry()
	other.Version = "2"
	if problems := got.Recompute(other); len(problems) == 0 {
		t.Errorf("a forecast from another registry recomputed against this one")
	}
}

func TestResidualAllowanceIsCapped(t *testing.T) {
	reg := minimalRegistry()
	// A residual that instantiates above its own bound is refused outright.
	reg.Components[3].PointNs = 300 * millisecond
	reg.Components[3].BoundNs = 200 * millisecond
	if _, err := reg.Instantiate(AetaInputs{BucketID: "b1", PallocSeconds: 60, Invocations: 1, Stage2: "sha256:s2"}); err == nil {
		t.Errorf("a residual above its bound was instantiated")
	}
	// And the aggregate is capped at 0.5% of the forecast, so a small action
	// cannot carry a large allowance.
	reg = minimalRegistry()
	reg.Components[3].PointNs = 400 * millisecond
	reg.Components[3].BoundNs = 500 * millisecond
	if _, err := reg.Instantiate(AetaInputs{BucketID: "b1", PallocSeconds: 1, Invocations: 1, Stage2: "sha256:s2"}); err == nil {
		t.Errorf("a 400 ms allowance on a ~1.5 s forecast was accepted")
	}
}

// TestCompletenessFindsUnforecastMaterialTime is the check that stops "we
// measured it precisely" from being an answer: a phase nobody predicted is a
// phase nobody understood.
func TestCompletenessFindsUnforecastMaterialTime(t *testing.T) {
	reg := minimalRegistry()
	aeta, err := reg.Instantiate(AetaInputs{BucketID: "b1", PallocSeconds: 60, Invocations: 1, Stage2: "sha256:s2"})
	if err != nil {
		t.Fatal(err)
	}
	phases := []Phase{
		{ComponentID: "action_containment_bootstrap", Parent: "action", StartNs: 0, EndNs: Nanos(20 * millisecond)},
		{ComponentID: "mystery_wait", Parent: "action", StartNs: Nanos(20 * millisecond), EndNs: Nanos(20*millisecond + 3*second)},
	}
	findings := reg.CheckCompleteness(phases, aeta)
	if len(findings) == 0 {
		t.Fatalf("a 3 s unforecast phase produced no finding")
	}
	if !strings.Contains(findings[0].Detail, "mystery_wait") {
		t.Errorf("the finding does not name the phase: %s", findings[0].Detail)
	}

	// AND A SHORT UNMAPPED PHASE IS STILL UNMAPPED.
	//
	// This used to assert the opposite: a 1 ms interval carrying no registered
	// component produced no finding, absorbed into the residual accumulator by
	// a materiality test. The frozen contract says an unmapped interval fails
	// A eligibility and that U is "a named bounded residual, never a
	// catch-all", so that expectation made the registry a list of the parts
	// somebody happened to name rather than an exhaustive taxonomy. The caps
	// bound magnitude; they cannot restore the id, parent, owner, formula,
	// permitted inputs and bound that Stage 1 was supposed to freeze before
	// the action ran.
	small := []Phase{
		{ComponentID: "action_containment_bootstrap", Parent: "action", StartNs: 0, EndNs: Nanos(20 * millisecond)},
		{ComponentID: "tiny_gap", Parent: "action", StartNs: Nanos(20 * millisecond), EndNs: Nanos(21 * millisecond)},
	}
	got := reg.CheckCompleteness(small, aeta)
	if len(got) == 0 {
		t.Fatal("a 1 ms interval absent from the frozen registry produced no finding; an unmapped interval is not residual time")
	}
	if !strings.Contains(got[0].Detail, "tiny_gap") || !strings.Contains(got[0].Detail, "not in the frozen component registry") {
		t.Errorf("the finding does not name the unmapped phase: %s", got[0].Detail)
	}

	// RESIDUAL TIME STILL WORKS — it is a class a component was REGISTERED as.
	// The rule refuses a catch-all, not the mechanism.
	named := []Phase{
		{ComponentID: "action_containment_bootstrap", Parent: "action", StartNs: 0, EndNs: Nanos(20 * millisecond)},
		{ComponentID: "unnamed_overhead", Parent: "action", StartNs: Nanos(20 * millisecond), EndNs: Nanos(21 * millisecond)},
	}
	if findings := reg.CheckCompleteness(named, aeta); len(findings) != 0 {
		t.Errorf("a registered residual component produced findings: %+v", findings)
	}

	// AND ITS OWN BOUND IS ENFORCED: unnamed_overhead is bounded at 200 ms.
	over := []Phase{
		{ComponentID: "unnamed_overhead", Parent: "action", StartNs: 0, EndNs: Nanos(300 * millisecond)},
	}
	overFindings := reg.CheckCompleteness(over, aeta)
	var bounded bool
	for _, f := range overFindings {
		if strings.Contains(f.Detail, "above its") {
			bounded = true
		}
	}
	if !bounded {
		t.Errorf("a residual component past its declared bound was accepted: %+v", overFindings)
	}
}
