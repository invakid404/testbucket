package walltime

import ()

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

// Sample renders this forecast for the calibration gate.
func (a AetaInstance) Sample(observedNs int64) AetaSample {
	return AetaSample{BucketID: a.BucketID, PointNs: a.PointNs, LowerNs: a.LowerNs, UpperNs: a.UpperNs, ObservedNs: observedNs}
}
