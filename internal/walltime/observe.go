package walltime

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Control phases. The wrapper drives its two observers through these, in this
// order, so the endpoint containment the verifier requires is established by
// CONSTRUCTION rather than hoped for:
//
//	physical start -> peer admit -> trace admit -> child -> child exit
//	              -> trace close -> peer close -> physical end
//
// Each transition is a file the wrapper creates and the observer polls for. A
// file is used rather than a pipe because the action-level observers outlive
// the process that started them: `wall begin` and `wall end` are two different
// GitHub Actions steps.
const (
	phaseAdmit = "admit"
	phaseClose = "close"
)

// pollInterval bounds how quickly an observer notices a phase change. The
// resulting handshake latency lands in the PHYSICAL prefix and suffix, where
// it is real work with a forecast, and it is symmetric across peer and trace,
// so it does not distort the like-for-like reconciliation.
const pollInterval = 500 * time.Microsecond

// control is the wrapper's side of the observer handshake.
type control struct{ base string }

func (c control) path(phase string) string { return c.base + "." + phase }

func (c control) signal(phase string) error {
	f, err := os.OpenFile(c.path(phase), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (c control) await(phase string, deadline time.Time) error {
	for {
		if _, err := os.Stat(c.path(phase)); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("walltime: timed out waiting for %s", c.path(phase))
		}
		time.Sleep(pollInterval)
	}
}

// ObserverConfig is everything an independent observer needs. It is passed as
// flags to a separate process on purpose: a peer that shares the wrapper's
// address space is not an independent execution context, and the verifier
// checks exactly that.
type ObserverConfig struct {
	Producer    Producer
	Level       Level
	Seq         int
	Dir         string
	ControlBase string
	Containment ContainmentIdentity
	Run         RunIdentity
	Key         ed25519.PrivateKey
	// Timeout bounds the wait for verified-empty containment. A timeout is
	// TERMINAL: the observer writes why and exits non-zero, and no duration is
	// inferred for the lifecycle it could not close.
	Timeout time.Duration
}

// RunObserver is the whole life of one containment peer or trace collector: it
// takes its OWN fresh clock reading and its OWN raw containment read at
// admission, again at verified-empty, and writes them to its OWN signed
// stream. It never reads the physical wrapper's records, so there is nothing
// for it to copy.
func RunObserver(cfg ObserverConfig) error {
	role, err := RoleFor(cfg.Producer, cfg.Level)
	if err != nil {
		return err
	}
	cont, err := AttachContainment(cfg.Containment)
	if err != nil {
		return err
	}
	w, err := NewWriter(filepath.Join(cfg.Dir, streamName(cfg.Producer, cfg.Level, cfg.Seq)), cfg.Producer, ProducerID(cfg.Producer), cfg.Key)
	if err != nil {
		return err
	}
	defer w.Close()
	clock := NewSystemClock()
	ctl := control{base: cfg.ControlBase}
	observer := string(cfg.Producer)

	deadline := time.Now().Add(cfg.Timeout)
	if err := ctl.await(phaseAdmit, deadline); err != nil {
		return observerTerminal(w, cfg, role, clock, TerminalWrapperError, err.Error())
	}
	// The admission endpoint: this observer's own raw containment read, and
	// its own clock reading. Both are taken here, in this process.
	ev, _, err := cont.Observe(observer)
	if err != nil {
		return observerTerminal(w, cfg, role, clock, TerminalWrapperError, "admission read: "+err.Error())
	}
	if _, err := w.Append(Record{
		Kind: "boundary", Role: role, Level: cfg.Level, Boundary: "start",
		Source: ev.Source, RawEventID: ev.ID, RawEventDigest: ev.Digest,
		Phase: lifecyclePhase(cfg.Level), Seqno: cfg.Seq,
		Run: cfg.Run, Containment: cont.Identity(), Instant: clock.Now(),
	}); err != nil {
		return err
	}

	if err := ctl.await(phaseClose, deadline); err != nil {
		return observerTerminal(w, cfg, role, clock, TerminalCancelled, err.Error())
	}
	// Verified empty is the observer's OWN conclusion from its OWN reads. It
	// is never the wrapper's "the child returned" boolean: a returned root with
	// a live descendant is exactly the escape this endpoint exists to catch.
	last, err := awaitEmpty(cont, observer, deadline)
	if err != nil {
		return observerTerminal(w, cfg, role, clock, TerminalCrashUnclosed, err.Error())
	}
	if _, err := w.Append(Record{
		Kind: "boundary", Role: role, Level: cfg.Level, Boundary: "end",
		Source: last.Source, RawEventID: last.ID, RawEventDigest: last.Digest,
		Phase: lifecyclePhase(cfg.Level), Seqno: cfg.Seq,
		Run: cfg.Run, Containment: cont.Identity(), Instant: clock.Now(),
		Terminal: TerminalPassed,
	}); err != nil {
		return err
	}
	return nil
}

// awaitEmpty polls the containment with this observer's own reads until the
// kernel reports it unpopulated, returning the raw event that showed it.
func awaitEmpty(cont Containment, observer string, deadline time.Time) (RawEvent, error) {
	for {
		ev, populated, err := cont.Observe(observer)
		if err != nil {
			return RawEvent{}, fmt.Errorf("empty read: %w", err)
		}
		if !populated {
			return ev, nil
		}
		if time.Now().After(deadline) {
			return RawEvent{}, fmt.Errorf("containment %s never reached verified-empty", cont.Identity().ID)
		}
		time.Sleep(pollInterval)
	}
}

// observerTerminal retains an unclosable lifecycle instead of dropping it. The
// record carries the reason and no duration: an interval with one endpoint is
// not a shorter interval, it is a missing one.
func observerTerminal(w *Writer, cfg ObserverConfig, role Role, clock Clock, state, reason string) error {
	if _, err := w.Append(Record{
		Kind: "terminal", Role: role, Level: cfg.Level, Seqno: cfg.Seq,
		Source: SourceWrapper, Run: cfg.Run, Containment: cfg.Containment,
		Instant: clock.Now(), Terminal: state, Reason: reason,
	}); err != nil {
		return err
	}
	return fmt.Errorf("walltime: %s %s: %s", cfg.Producer, state, reason)
}

// lifecyclePhase names the observed lifecycle at each level. The taxonomy is
// deliberately coarse and OS-delimited: it claims only what a containment
// transition proves, and never an in-process causal boundary.
func lifecyclePhase(l Level) string {
	switch l {
	case LevelAction:
		return "action_containment_lifecycle"
	case LevelScript:
		return "script_containment_lifecycle"
	default:
		return "invocation_containment_lifecycle"
	}
}
