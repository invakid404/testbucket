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
	// BoundNs caps a residual component.
	BoundNs int64 `json:"bound_ns,omitempty"`
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
		if c.Class == ClassResidual {
			if c.BoundNs <= 0 || c.BoundNs > ResidualComponentLimit {
				return fmt.Errorf("residual component %q must declare a bound in (0, %s]", c.ID, dur(ResidualComponentLimit))
			}
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
type AetaInstance struct {
	Kind           string                  `json:"kind"`
	RegistryDigest Digest                  `json:"registry_digest"`
	Stage2         Digest                  `json:"stage2_digest"`
	BucketID       string                  `json:"bucket_id"`
	Components     []InstantiatedComponent `json:"components"`
	PointNs        int64                   `json:"point_ns"`
	LowerNs        int64                   `json:"lower_ns"`
	UpperNs        int64                   `json:"upper_ns"`
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
	out := &AetaInstance{Kind: AetaKind, RegistryDigest: d, Stage2: in.Stage2, BucketID: in.BucketID}
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
			if p.Duration() > MaterialThreshold {
				out = append(out, Finding{
					Code: "WT-017", Severity: SeverityIneligible,
					Detail: fmt.Sprintf("material phase %s (%s) is not in the frozen component registry", p.Name(), dur(p.Duration())),
				})
			} else {
				residual += p.Duration()
			}
			continue
		}
		if c.Class == ClassResidual {
			residual += p.Duration()
			if p.Duration() > c.BoundNs {
				out = append(out, Finding{
					Code: "WT-017", Severity: SeverityIneligible,
					Detail: fmt.Sprintf("residual phase %s observed at %s, above its %s bound", p.Name(), dur(p.Duration()), dur(c.BoundNs)),
				})
			}
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
