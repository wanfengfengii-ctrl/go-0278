// Package instrument provides a deterministic, in-process implementation of the
// pullout/pump/displacement instrument port. It is scripted so tests can inject
// rejected, disconnected, timeout and success sequences, while the production
// adapter defaults to a successful fixed reading.
package instrument

import (
	"context"
	"sync"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

// Adapter implements domain.InstrumentPort. A nil script means every call
// succeeds and returns the configured reading value.
type Adapter struct {
	mu      sync.Mutex
	script  map[string]domain.CallResult
	reading int64
	seq     int
}

// NewAdapter returns an adapter that always succeeds with the given fixed-point
// reading value.
func NewAdapter(reading int64) *Adapter {
	return &Adapter{reading: reading, script: map[string]domain.CallResult{}}
}

// NewScriptedAdapter returns an adapter whose per-call-key outcomes are driven by
// script. Keys absent from the script succeed.
func NewScriptedAdapter(reading int64, script map[string]domain.CallResult) *Adapter {
	return &Adapter{reading: reading, script: script}
}

// SetScript atomically replaces the script. It is used by tests to install a
// sequence before submitting a reading.
func (a *Adapter) SetScript(script map[string]domain.CallResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.script = script
}

// SetReading sets the fixed-point reading returned on success.
func (a *Adapter) SetReading(reading int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reading = reading
}

// Call resolves the outcome for a call key. The result is deterministic for a
// given key and script and never depends on wall-clock time or shared mutable
// state beyond the script itself.
func (a *Adapter) Call(_ context.Context, req domain.InstrumentRequest) (domain.InstrumentResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	res, ok := a.script[req.CallKey]
	if !ok {
		res = domain.CallAccepted
	}
	if res == domain.CallAccepted {
		reading := a.reading
		return domain.InstrumentResult{Result: res, Reading: &reading}, nil
	}
	return domain.InstrumentResult{Result: res}, nil
}

// CallCount returns how many invocations the adapter has served. It is used by
// tests to assert deterministic retry sequences.
func (a *Adapter) CallCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.seq
}
