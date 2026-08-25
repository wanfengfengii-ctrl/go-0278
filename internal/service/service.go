// Package service is the application layer that coordinates the domain rules,
// the transactional store and the instrument port. It is the only place where
// the business flows (lock, sample, verify, lease, load, expand, reinforce,
// retest, review, finalize) are orchestrated, so every failure boundary is
// enforced in one place and reachable from the HTTP API.
package service

import (
	"errors"
	"sync"
	"sync/atomic"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/instrument"
	"rockbolt-pullout-zonal-closure/internal/store"
)

// Service orchestrates one rock-bolt pullout inspection domain.
type Service struct {
	store *store.Store
	inst  domain.InstrumentPort
	clock *logicalClock
	seq   atomic.Uint64

	// callMu serializes concurrent submissions that resolve to the same
	// content-derived call_key. It guarantees the instrument is invoked at most
	// once per call_key even when two field terminals submit identical readings
	// under distinct operation_ids: the second waits for the first to finish, then
	// observes the committed call and returns a conflict without re-triggering
	// the puller. Arbitration therefore completes before the device is commanded.
	callMu sync.Map // callKey -> *sync.Mutex
}

// New creates a Service over the given store. The instrument port defaults to a
// deterministic success adapter that may be replaced by tests via SetInstrument.
func New(st *store.Store) *Service {
	return &Service{
		store: st,
		inst:  instrument.NewAdapter(0),
		clock: newLogicalClock(),
	}
}

// lockCallKey returns a mutex dedicated to one content-derived call_key. It
// serializes concurrent submissions that resolve to the same call_key so that
// arbitration (the existence check against instrument_calls) completes before
// the device is commanded: a duplicate waits for the first to finish, observes
// the committed call, and returns a conflict instead of re-triggering the
// puller.
func (s *Service) lockCallKey(callKey string) *sync.Mutex {
	actual, _ := s.callMu.LoadOrStore(callKey, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

// SetInstrument replaces the instrument port, used by tests to inject scripted
// failure sequences.
func (s *Service) SetInstrument(p domain.InstrumentPort) { s.inst = p }

// SetClock overrides the logical time source with a fixed function. Tests use it
// to make lease windows and hold durations deterministic.
func (s *Service) SetClock(f func() domain.LogicalTime) { s.clock.setFn(f) }

// Now returns the current logical time.
func (s *Service) Now() domain.LogicalTime { return s.clock.now() }

// Store exposes the underlying store for callers that need direct read access.
func (s *Service) Store() *store.Store { return s.store }

// logicalClock is a monotonic logical clock decoupled from wall time. It
// advances by one unit per read so that lease expiry and hold durations never
// depend on background time races.
type logicalClock struct {
	n      atomic.Int64
	custom atomic.Bool
	fn     atomic.Value // func() domain.LogicalTime
}

func newLogicalClock() *logicalClock { return &logicalClock{} }

func (c *logicalClock) setFn(f func() domain.LogicalTime) {
	c.custom.Store(true)
	c.fn.Store(f)
}

func (c *logicalClock) now() domain.LogicalTime {
	if c.custom.Load() {
		return c.fn.Load().(func() domain.LogicalTime)()
	}
	return domain.LogicalTime(c.n.Add(1))
}

// service errors mapped to stable HTTP codes by the API layer.
var (
	ErrTaskNotFound      = errors.New("service: task not found")
	ErrInvalidTransition = errors.New("service: invalid state transition")
	ErrLeaseConflict     = errors.New("service: lease conflict")
	ErrStageOutOfOrder   = errors.New("service: stage out of order")
	ErrHoldTooShort      = errors.New("service: hold too short")
	ErrArithmetic        = errors.New("service: arithmetic error")
	ErrBoundaryMissing   = errors.New("service: influence boundary incomplete")
	ErrNotQualified      = errors.New("service: reviewer not qualified")
	ErrSamplesOpen       = errors.New("service: samples not closed")
)

// IsNotFound reports whether err indicates a missing entity.
func IsNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound) || errors.Is(err, ErrTaskNotFound)
}

// IsConflict reports whether err indicates a version/duplicate/idempotency
// conflict.
func IsConflict(err error) bool {
	return errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrDuplicate)
}

// IsReplay reports whether err indicates an already-applied operation.
func IsReplay(err error) bool { return errors.Is(err, store.ErrReplay) }
