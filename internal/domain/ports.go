package domain

import "context"

// InstrumentRequest is a normalized request to a physical pullout instrument.
type InstrumentRequest struct {
	CallKey   string
	DeviceNo  string
	Stage     LoadStage
	LoadLevel int
}

// InstrumentResult is the outcome of an instrument request.
type InstrumentResult struct {
	Result  CallResult
	Reading *int64 // 定点读数，仅成功时非空
}

// InstrumentPort abstracts the external pullout/pump/displacement instruments so
// that tests can inject scripted rejected/disconnected/timeout/success
// sequences in-process. Implementations must be deterministic for a given call
// key.
type InstrumentPort interface {
	Call(ctx context.Context, req InstrumentRequest) (InstrumentResult, error)
}
