package walltime

import (
	"fmt"
	"sort"
)

// Aeta is the user-facing action forecast, and it is deliberately NOT an
// allocation input: it is assembled from component forecasts after allocation
// is frozen, so a forecast can never become the reason a unit landed in a
// bucket.
//
// The two-stage freeze is the whole design. Stage 1 fixes the exhaustive
// component TEMPLATE — every physical phase class, its formula, its permitted
// inputs, its bound. Stage 2 may only instantiate that template for a bucket.
// A component that appears for the first time after a result is an
// ETA-completeness failure even if its number is right.
const (
	RegistryKind = "tb.walltime.aeta-registry/v1"
	AetaKind     = "tb.walltime.aeta/v1"
)

// ComponentClass is how a material physical component is forecast.
type ComponentClass string

const (
	// ClassPalloc is a test-dependent aggregate of pre-KK Palloc terms.
	ClassPalloc ComponentClass = "palloc"
	// ClassActionOnly is a predeclared action-only forecast. It cannot alter
	// partitioning: it is a cost of running the action, not a property of the
	// tests.
	ClassActionOnly ComponentClass = "action_only"
	// ClassResidual is an explicit predeclared bounded allowance. It is not a
	// catch-all: each residual component is capped, and so is their total.
	ClassResidual ComponentClass = "residual"
)

// Formula identities. A component's forecast comes from one of these and from
// its declared inputs — never from a measurement taken during the run.
const (
	FormulaConstant      = "constant"
	FormulaPallocSum     = "palloc_sum"
	FormulaPerInvocation = "per_invocation_constant"
)

// Component is one entry of the frozen registry template.
type Component struct {
	ID     string         `json:"id"`
	Parent string         `json:"parent"`
	Owner  string         `json:"owner"`
	Class  ComponentClass `json:"class"`
	// Included is false for a phase that is deliberately excluded from A (a
	// diagnostic). Excluding is a bound decision, not an omission.
	Included bool     `json:"included"`
	Formula  string   `json:"formula"`
	Inputs   []string `json:"permitted_inputs"`
	// PointNs is the constant term; PerUnitNs multiplies the invocation count.
	PointNs   int64 `json:"point_ns,omitempty"`
	PerUnitNs int64 `json:"per_unit_ns,omitempty"`
	// IntervalNs and IntervalFraction compose the component's interval. The
	// wider of the two applies, so a small component keeps an absolute floor
	// and a large one scales.
	IntervalNs       int64   `json:"interval_ns,omitempty"`
	IntervalFraction float64 `json:"interval_fraction,omitempty"`
	// BoundNs is the component's OWN upper limit, and every physical
	// component declares one.
	//
	// It used to be described and enforced as a residual-only cap: the
	// registry required it for ClassResidual and accepted zero everywhere
	// else, and the completeness check read a zero as "no limit to enforce".
	// So an action-only or Palloc phase could be mapped, admissible and
	// entirely unbounded — the contract's component-local limit simply absent
	// for most of the taxonomy.
	//
	// Aggregate calibration cannot stand in for it. One component's overrun
	// hides inside another's underrun, which is exactly what a per-component
	// bound exists to catch. A residual component is bounded MORE tightly
	// still (see ResidualComponentLimit); this is the floor everything else
	// shares.
	BoundNs int64 `json:"bound_ns"`
}

// AetaRegistry is the Stage-1 frozen template.
type AetaRegistry struct {
	Kind       string      `json:"kind"`
	Version    string      `json:"version"`
	Components []Component `json:"components"`
	Signature  *Signature  `json:"signature,omitempty"`
}

// DigestOf is the registry's canonical identity.
func (r AetaRegistry) DigestOf() (Digest, error) {
	c := r
	c.Signature = nil
	return DigestJSON(c)
}

// Component looks one up by id.
func (r AetaRegistry) Component(id string) (Component, bool) {
	for _, c := range r.Components {
		if c.ID == id {
			return c, true
		}
	}
	return Component{}, false
}

// Validate refuses a template that could not be instantiated deterministically
// or that leaves a residual unbounded.
func (r AetaRegistry) Validate() error {
	if r.Kind != RegistryKind {
		return fmt.Errorf("aeta registry kind %q, want %q", r.Kind, RegistryKind)
	}
	if len(r.Components) == 0 {
		return fmt.Errorf("aeta registry is empty")
	}
	seen := map[string]bool{}
	for _, c := range r.Components {
		if seen[c.ID] {
			return fmt.Errorf("aeta registry defines %q twice", c.ID)
		}
		seen[c.ID] = true
		switch c.Class {
		case ClassPalloc, ClassActionOnly, ClassResidual:
		default:
			return fmt.Errorf("component %q has unknown class %q", c.ID, c.Class)
		}
		switch c.Formula {
		case FormulaConstant, FormulaPallocSum, FormulaPerInvocation:
		default:
			return fmt.Errorf("component %q has unknown formula %q", c.ID, c.Formula)
		}
		// EVERY PHYSICAL COMPONENT DECLARES A BOUND. Zero is not "unlimited";
		// it is a component the registry cannot enforce anything about, and
		// the contract asks the registry to map every physical phase class to
		// a bound.
		if c.BoundNs <= 0 {
			return fmt.Errorf("component %q declares no bound; every physical component carries its own limit, and a missing one is not an unlimited one", c.ID)
		}
		// A residual is bounded more tightly than the rest.
		if c.Class == ClassResidual && c.BoundNs > ResidualComponentLimit {
			return fmt.Errorf("residual component %q must declare a bound in (0, %s]", c.ID, dur(ResidualComponentLimit))
		}
		if c.Class == ClassPalloc && c.Formula != FormulaPallocSum {
			return fmt.Errorf("component %q is test-dependent but does not aggregate Palloc", c.ID)
		}
		if c.Class == ClassActionOnly && c.Formula == FormulaPallocSum {
			return fmt.Errorf("component %q is action-only but is formulated from Palloc", c.ID)
		}
	}
	return nil
}

// AetaInputs is everything Stage 2 may use to instantiate. There is
// deliberately no field for a measurement, a host fact, a cache state or a
// candidate result: instantiation is arithmetic on frozen inputs.
type AetaInputs struct {
	BucketID string `json:"bucket_id"`
	// BucketIndex is the same bucket by its position in the plan. Both are
	// carried because the measured row names the bucket by NAME and the
	// Stage-2 receipt binds derived documents by INDEX, and a forecast that
	// answered to only one of them could be checked against only one of them.
	BucketIndex int `json:"bucket"`
	// PallocSeconds is the bucket's frozen pre-KK Palloc total.
	PallocSeconds float64 `json:"palloc_seconds"`
	// Invocations is the rendered invocation count from the verified Stage-2
	// membership.
	Invocations int    `json:"invocations"`
	Stage2      Digest `json:"stage2_digest"`
}

// InstantiatedComponent is one component's forecast for one bucket.
type InstantiatedComponent struct {
	ID      string         `json:"id"`
	Class   ComponentClass `json:"class"`
	PointNs int64          `json:"point_ns"`
	LowerNs int64          `json:"lower_ns"`
	UpperNs int64          `json:"upper_ns"`
}

// AetaInstance is the pre-action forecast for one bucket: a point and a FINITE
// interval.
//
// It carries the INPUTS it was instantiated from, so a verifier can re-run the
// frozen formulas and compare rather than believe the point it was handed. A
// forecast nobody can recompute is an allowance, and the contract is explicit
// that a post-action adjustment is an ETA-completeness failure even when the
// trace was exact.
type AetaInstance struct {
	Kind           string                  `json:"kind"`
	RegistryDigest Digest                  `json:"registry_digest"`
	Stage2         Digest                  `json:"stage2_digest"`
	BucketID       string                  `json:"bucket_id"`
	Inputs         AetaInputs              `json:"inputs"`
	Components     []InstantiatedComponent `json:"components"`
	PointNs        int64                   `json:"point_ns"`
	LowerNs        int64                   `json:"lower_ns"`
	UpperNs        int64                   `json:"upper_ns"`
}

// Recompute re-instantiates this forecast from the frozen registry and the
// inputs it names, and reports every disagreement. Component by component: a
// total that happens to match while a component was edited is still a
// different forecast.
func (a AetaInstance) Recompute(r AetaRegistry) []string {
	var problems []string
	if a.Kind != AetaKind {
		problems = append(problems, fmt.Sprintf("kind is %q, want %q", a.Kind, AetaKind))
	}
	d, err := r.DigestOf()
	if err != nil {
		return append(problems, "the registry cannot be digested: "+err.Error())
	}
	if a.RegistryDigest != d {
		return append(problems, fmt.Sprintf("it names registry %s but the supplied registry digests to %s", a.RegistryDigest, d))
	}
	if a.Inputs.Stage2 != a.Stage2 {
		problems = append(problems, fmt.Sprintf("its inputs name Stage-2 %s but the instance names %s", a.Inputs.Stage2, a.Stage2))
	}
	want, err := r.Instantiate(a.Inputs)
	if err != nil {
		return append(problems, "re-instantiating from the frozen template failed: "+err.Error())
	}
	if want.PointNs != a.PointNs || want.LowerNs != a.LowerNs || want.UpperNs != a.UpperNs {
		problems = append(problems, fmt.Sprintf("the frozen template gives [%s, %s, %s] but the instance claims [%s, %s, %s]",
			dur(want.LowerNs), dur(want.PointNs), dur(want.UpperNs),
			dur(a.LowerNs), dur(a.PointNs), dur(a.UpperNs)))
	}
	if len(want.Components) != len(a.Components) {
		problems = append(problems, fmt.Sprintf("the frozen template instantiates %d components but the instance carries %d", len(want.Components), len(a.Components)))
		return problems
	}
	for i, c := range want.Components {
		got := a.Components[i]
		if got.ID != c.ID || got.Class != c.Class || got.PointNs != c.PointNs || got.LowerNs != c.LowerNs || got.UpperNs != c.UpperNs {
			problems = append(problems, fmt.Sprintf("component %q was instantiated as %s but the frozen formula gives %s",
				got.ID, dur(got.PointNs), dur(c.PointNs)))
		}
	}
	return problems
}

// Instantiate applies the frozen formulas to verified inputs. It cannot add a
// component, edit a formula, widen a bound, or re-plan; those are the four
// ways a forecast turns into an excuse after the fact.
func (r AetaRegistry) Instantiate(in AetaInputs) (*AetaInstance, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if in.Stage2 == "" {
		return nil, fmt.Errorf("aeta: instantiation requires the verified Stage-2 receipt digest")
	}
	d, err := r.DigestOf()
	if err != nil {
		return nil, err
	}
	out := &AetaInstance{Kind: AetaKind, RegistryDigest: d, Stage2: in.Stage2, BucketID: in.BucketID, Inputs: in}
	var residualTotal int64
	comps := append([]Component(nil), r.Components...)
	sort.Slice(comps, func(i, j int) bool { return comps[i].ID < comps[j].ID })
	for _, c := range comps {
		if !c.Included {
			continue
		}
		var point int64
		switch c.Formula {
		case FormulaConstant:
			point = c.PointNs
		case FormulaPerInvocation:
			point = c.PerUnitNs * int64(in.Invocations)
		case FormulaPallocSum:
			point = int64(in.PallocSeconds * float64(second))
		}
		half := c.IntervalNs
		if f := int64(c.IntervalFraction * float64(point)); f > half {
			half = f
		}
		ic := InstantiatedComponent{ID: c.ID, Class: c.Class, PointNs: point, LowerNs: point - half, UpperNs: point + half}
		if ic.LowerNs < 0 {
			ic.LowerNs = 0
		}
		if c.Class == ClassResidual {
			if point > c.BoundNs {
				return nil, fmt.Errorf("residual component %q instantiates at %s, above its %s bound", c.ID, dur(point), dur(c.BoundNs))
			}
			residualTotal += point
		}
		out.Components = append(out.Components, ic)
		out.PointNs += ic.PointNs
		out.LowerNs += ic.LowerNs
		out.UpperNs += ic.UpperNs
	}
	if residualTotal > ResidualTotalLimit {
		return nil, fmt.Errorf("aeta: residual allowance totals %s, above the %s limit", dur(residualTotal), dur(ResidualTotalLimit))
	}
	if frac := int64(ResidualFraction * float64(out.PointNs)); residualTotal > frac {
		return nil, fmt.Errorf("aeta: residual allowance %s exceeds %.1f%% of the %s forecast",
			dur(residualTotal), ResidualFraction*100, dur(out.PointNs))
	}
	if out.UpperNs <= out.LowerNs && out.PointNs > 0 {
		return nil, fmt.Errorf("aeta: the composed interval is degenerate")
	}
	return out, nil
}

// Sample renders this forecast for the calibration gate.
func (a AetaInstance) Sample(observedNs int64) AetaSample {
	return AetaSample{BucketID: a.BucketID, PointNs: a.PointNs, LowerNs: a.LowerNs, UpperNs: a.UpperNs, ObservedNs: observedNs}
}

// CheckCompleteness proves every OBSERVED material physical phase was
// forecast or explicitly bounded before the action ran. This is the check the
// contract calls ETA completeness, and it is where "we measured it precisely"
// stops being an answer: a phase nobody predicted is a phase nobody
// understood, however well it was timed.
func (r AetaRegistry) CheckCompleteness(phases []Phase, aeta *AetaInstance) []Finding {
	var out []Finding
	var residual int64
	for _, p := range phases {
		c, ok := r.Component(p.ComponentID)
		if !ok {
			// AN UNMAPPED INTERVAL IS NOT RESIDUAL TIME, at any duration.
			//
			// The frozen contract is explicit that U is "a named bounded
			// residual, never a catch-all", and that an unmapped interval
			// fails A eligibility. What used to happen instead was a
			// materiality test: a phase carrying no registered component was
			// refused only if it alone exceeded 500 ms, and anything smaller
			// was quietly added to the residual accumulator and passed if the
			// aggregate stayed inside its caps.
			//
			// Those caps bound MAGNITUDE. They do not restore what is missing,
			// which is the pre-action provenance: an immutable id, a parent, an
			// owner, a formula, permitted inputs and a bound, all frozen in
			// Stage 1 before the action ran. A row could be declared
			// Aeta-complete while observed physical work had none of it, so the
			// registry stopped being exhaustive and became a list of the parts
			// somebody happened to name.
			out = append(out, Finding{
				Code: "WT-017", Severity: SeverityIneligible,
				Detail: fmt.Sprintf("phase %s (%s) is not in the frozen component registry; an unmapped interval is not residual time, however short",
					p.Name(), dur(p.Duration())),
			})
			continue
		}
		// RESIDUAL IS A CLASS SOMETHING WAS REGISTERED AS, not a place to put
		// what did not fit.
		if c.Class == ClassResidual {
			residual += p.Duration()
		}
		// EVERY COMPONENT'S OWN BOUND, not only a residual one — and a missing
		// bound is a refusal here too, not a skipped check.
		//
		// Registry validation now requires a positive bound, so a zero can
		// only arrive through a registry that was never validated. Reading it
		// as "nothing to enforce" is how the check was bypassed in the first
		// place, so completeness refuses it defensively rather than trusting
		// that someone upstream looked.
		if c.BoundNs <= 0 {
			out = append(out, Finding{
				Code: "WT-017", Severity: SeverityIneligible,
				Detail: fmt.Sprintf("phase %s is mapped to component %q, which declares no bound; an unbounded component cannot be checked against anything",
					p.Name(), c.ID),
			})
			continue
		}
		if p.Duration() > c.BoundNs {
			out = append(out, Finding{
				Code: "WT-017", Severity: SeverityIneligible,
				Detail: fmt.Sprintf("phase %s observed at %s, above its %s bound", p.Name(), dur(p.Duration()), dur(c.BoundNs)),
			})
		}
	}
	if residual > ResidualTotalLimit {
		out = append(out, Finding{
			Code: "WT-017", Severity: SeverityIneligible,
			Detail: fmt.Sprintf("unnamed and residual time totals %s, above the %s aggregate limit", dur(residual), dur(ResidualTotalLimit)),
		})
	}
	if aeta != nil {
		if frac := int64(ResidualFraction * float64(aeta.PointNs)); residual > frac {
			out = append(out, Finding{
				Code: "WT-017", Severity: SeverityIneligible,
				Detail: fmt.Sprintf("unnamed and residual time %s exceeds %.1f%% of the %s pre-action forecast",
					dur(residual), ResidualFraction*100, dur(aeta.PointNs)),
			})
		}
	}
	return out
}
