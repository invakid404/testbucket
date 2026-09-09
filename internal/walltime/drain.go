package walltime

import (
	"fmt"
	"time"
)

// Contract §3.3 fixes what the wrapper does before the closing read, and it is
// exactly three things:
//
//  1. waits and reaps the ROOT CHILD it started — the only process it is the
//     parent of;
//  2. signals the process group by negative PGID, TERM first then KILL after a
//     bounded interval;
//  3. drains the group — probes for the PGID's continued existence and does
//     not take the closing reading while any member remains.
//
// Step 1 belongs to the exec path, which owns the child handle. Steps 2 and 3
// are here.
//
// It does NOT reap descendants. A normal parent can waitpid its own child; it
// cannot reap arbitrary grandchildren unless it becomes a child subreaper, and
// this product does not. Every claim of "reap descendants" is removed as
// infeasible as written — nothing in this file attempts one, and
// TestProcessGroupDrainsBeforeEnd asserts no such claim is made.

// DrainDefaults are the bounded escalation intervals. They are bounded so that
// a cooperative child that ignores TERM cannot hold the closing read open
// indefinitely.
const (
	DefaultTermGrace = 2 * time.Second
	DefaultKillGrace = 2 * time.Second
	drainPollEvery   = 5 * time.Millisecond
)

// DrainOutcome is what the drain could actually CONFIRM, which is not the same
// as what it attempted. §3.3 requires the wrapper to record that it could not
// confirm an empty group rather than implying it did, so `GroupEmpty` is only
// ever set from a successful probe.
type DrainOutcome struct {
	// Signalled reports that TERM was delivered to the group.
	Signalled bool
	// Escalated reports that the bounded TERM interval expired with members
	// still present, so KILL was delivered.
	Escalated bool
	// Probeable reports whether this platform can be asked whether the group
	// still has members. Where it cannot, group-level drain cannot be
	// guaranteed at all and GroupEmpty stays false.
	Probeable bool
	// GroupEmpty is true only when a probe CONFIRMED no member remains.
	GroupEmpty bool
	// Reaped reports that the root child was waited for and reaped — step 1 of
	// §3.3, which the drain sequences because it must land between the signal
	// and the probe.
	Reaped bool
	// RootReapErr records a non-nil reap result. A non-zero child exit is an
	// ordinary outcome the exec path reports separately, so it is recorded
	// here rather than failing the drain.
	RootReapErr string
	// Elapsed is how long the drain took, for the wrapper's own accounting.
	Elapsed time.Duration
}

// Limitation renders the stated limitation of §3.3 for the observation's
// `limitations` list when the drain could not confirm an empty group.
func (o DrainOutcome) Limitation() string {
	switch {
	case !o.Probeable:
		return "group drain could not be probed on this platform; an empty group was not confirmed"
	case !o.GroupEmpty:
		return "group drain did not confirm an empty group within the bounded escalation interval"
	default:
		return ""
	}
}

// DrainRequest carries the one owned process group and the root-child reap the
// wrapper must perform as part of the sequence.
type DrainRequest struct {
	PGID      int
	TermGrace time.Duration
	KillGrace time.Duration
	// ReapRoot waits for and reaps the ROOT CHILD the wrapper started. It is
	// supplied by the exec path, which owns the child handle.
	ReapRoot func() error
	// ImmediateKill skips the bounded TERM interval and escalates at once. It
	// is set for an ESCAPE — a descendant found alive after its root had
	// already been reaped. The interval a TERM grace exists to protect is a
	// child shutting itself down cooperatively, and the process this one
	// belonged to has already exited: there is nothing left to ask, and the
	// only remaining question is whether the wrapper leaves it running on the
	// runner for whatever comes next.
	ImmediateKill bool
}

// DrainGroup performs contract §3.3's three steps for one owned process group
// and returns only after the group is confirmed empty or the bounded
// escalation has been exhausted. The caller must not take the closing
// monotonic reading until this returns: that ordering is the measurable
// content of "does not take the closing reading while any member remains".
//
// The root reap runs CONCURRENTLY with the signal escalation, and that is
// forced rather than stylistic. The two steps are mutually dependent:
//
//   - a killed or exited root child becomes a ZOMBIE, a zombie is still a
//     member of the process group, and `kill(-pgid, 0)` keeps succeeding until
//     its parent reaps it — so probing before the reap can never see an empty
//     group;
//   - but a root child that IGNORES TERM does not exit until the escalation
//     reaches KILL, so reaping before escalating blocks for as long as the
//     child feels like running — which is the case the bounded escalation
//     exists for.
//
// Sequencing them in either order therefore fails to terminate. Running the
// reap concurrently and escalating on the bounded schedule is the only
// arrangement that satisfies both, and the group can go empty only once both
// have happened.
//
// A descendant that calls setsid or double-forks LEAVES the process group and
// is therefore neither signalled nor drained; the closing reading may be taken
// while it still runs. That is the stated limitation, not a defect, and it is
// reported through DrainOutcome rather than hidden.
func DrainGroup(req DrainRequest) (DrainOutcome, error) {
	var out DrainOutcome
	if req.PGID <= 1 {
		// Refuse to signal pgid 0 (the caller's own group), 1 (init) or a
		// negative value: a mistaken sign here would signal every process the
		// user owns.
		return out, fmt.Errorf("drain: refusing to signal process group %d", req.PGID)
	}
	start := monotonicNow()
	defer func() { out.Elapsed = monotonicSince(start) }()

	out.Probeable = groupProbeSupported()
	pgid := req.PGID

	// Step 1, started now and collected below.
	reaped := make(chan error, 1)
	if req.ReapRoot != nil {
		go func() { reaped <- req.ReapRoot() }()
	} else {
		close(reaped)
	}

	// Step 2: TERM by negative PGID.
	if !req.ImmediateKill {
		if err := signalGroup(pgid, sigTERM); err != nil && !isNoSuchProcess(err) {
			return out, fmt.Errorf("drain: TERM to group %d: %w", pgid, err)
		}
		out.Signalled = true
	}

	if !out.Probeable {
		// Nothing to poll: the platform cannot answer the question. §3.3 says
		// the wrapper records that it could not confirm, and no gate assumes
		// more.
		out.collectReap(reaped, req.TermGrace)
		return out, nil
	}

	// Step 3, first attempt: the group may go quiet under TERM alone, but only
	// once the root zombie has been reaped.
	if !req.ImmediateKill && out.drainWithin(pgid, reaped, req.TermGrace) {
		out.GroupEmpty = true
		return out, nil
	}

	// The bounded TERM interval expired with members still present.
	out.Escalated = true
	if err := signalGroup(pgid, sigKILL); err != nil && !isNoSuchProcess(err) {
		return out, fmt.Errorf("drain: KILL to group %d: %w", pgid, err)
	}
	out.GroupEmpty = out.drainWithin(pgid, reaped, req.KillGrace)
	return out, nil
}

// drainWithin polls for an empty group until the bound expires, collecting the
// root reap as soon as it completes. The group cannot read empty until the reap
// has happened, so both are awaited on the same clock.
func (o *DrainOutcome) drainWithin(pgid int, reaped chan error, bound time.Duration) bool {
	deadline := time.Now().Add(bound)
	for {
		select {
		case err, ok := <-reaped:
			if ok {
				o.Reaped = true
				if err != nil {
					// A non-zero child exit is an ordinary outcome the exec
					// path reports separately, not a drain failure.
					o.RootReapErr = err.Error()
				}
			}
			reaped = nil
		default:
		}
		if o.Reaped && !groupExists(pgid) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(drainPollEvery)
	}
}

// collectReap waits up to bound for the root reap, for the platform path where
// the group cannot be probed at all.
func (o *DrainOutcome) collectReap(reaped chan error, bound time.Duration) {
	select {
	case err, ok := <-reaped:
		if ok {
			o.Reaped = true
			if err != nil {
				o.RootReapErr = err.Error()
			}
		}
	case <-time.After(bound):
	}
}

// groupEmptyWithin polls for an empty process group until the bound expires.
//
// It is the ESCAPE PROBE, and it is only meaningful once the root child has
// been reaped: the root is a member of its own group and a reaped-but-unwaited
// root is a zombie that keeps the group alive, so before the reap this
// question cannot be asked. After it, what the probe answers is whether any
// DESCENDANT outlived the process it belonged to. The bound is what separates
// a member that is merely the tail of the root's own exit from one that has
// genuinely been left behind.
func groupEmptyWithin(pgid int, bound time.Duration) bool {
	deadline := time.Now().Add(bound)
	for {
		if !groupExists(pgid) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(drainPollEvery)
	}
}
