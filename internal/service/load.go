package service

import (
	"context"
	"fmt"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/store"
)

// ReadingInput is a single reading submission for one bolt.
type ReadingInput struct {
	TaskID             string
	SampleID           string
	Stage              domain.LoadStage
	LoadLevel          int
	Load               int64
	Displacement       int64
	HoldSecs           int64
	Rebound            int64
	DevicePuller       string
	DevicePump         string
	DeviceDisplacement string
}

// ReadingResult reports the outcome of a reading submission.
type ReadingResult struct {
	Task       *domain.InspectionTask
	Accepted   bool
	RetryCount int
	NextRetry  domain.LogicalTime
}

// SubmitReading validates and records one bolt reading. It enforces the strict
// load prefix (no skip, repeat or out-of-order action), the locked hold
// durations, the active three-device lease and checked fixed-point arithmetic.
// A rejected, disconnected or timed-out instrument call produces only a
// persistent retry record and never advances the prefix or releases the lease.
func (s *Service) SubmitReading(ctx context.Context, opID string, in ReadingInput) (*ReadingResult, error) {
	t, sample, err := s.loadTaskAndSample(ctx, in.TaskID, in.SampleID)
	if err != nil {
		return nil, err
	}
	devices := [3]string{in.DevicePuller, in.DevicePump, in.DeviceDisplacement}
	if err := s.validateActiveLease(ctx, t, devices); err != nil {
		return nil, err
	}

	count, err := s.store.CountAcceptedEvidence(ctx, in.TaskID, in.SampleID, t.Generation)
	if err != nil {
		return nil, err
	}
	if !domain.ValidateNextStep(t.LoadLevels, count, in.Stage, in.LoadLevel) {
		return nil, ErrStageOutOfOrder
	}
	if in.Stage == domain.StageHold {
		def, ok := domain.HoldLevelDef(t.LoadLevels, in.LoadLevel)
		if !ok {
			return nil, ErrStageOutOfOrder
		}
		if in.HoldSecs < def.HoldSecs {
			return nil, ErrHoldTooShort
		}
	}
	ratio, err := computeRatio(in)
	if err != nil {
		return nil, ErrArithmetic
	}

	kind := evidenceKind(sample.Category)
	callKey := callKey(in.TaskID, in.SampleID, t.Generation, in.LoadLevel, in.DevicePuller, string(in.Stage), 0)
	req := domain.InstrumentRequest{CallKey: callKey, DeviceNo: in.DevicePuller, Stage: in.Stage, LoadLevel: in.LoadLevel}
	res, _ := s.inst.Call(ctx, req)

	if res.Result == domain.CallAccepted {
		closeID := ""
		if domain.IsLoadComplete(t.LoadLevels, count+1) {
			closeID = in.SampleID
		}
		ev := &domain.LoadEvidence{
			TaskID: in.TaskID, SampleID: in.SampleID, Generation: t.Generation,
			Kind: kind, Stage: in.Stage, LoadLevel: in.LoadLevel, Load: in.Load,
			Displacement: in.Displacement, HoldSecs: in.HoldSecs, Rebound: in.Rebound,
			Ratio: ratio, DeviceSet: devices, Accepted: true,
			ContentDigest: domain.Digest(in),
		}
		call := acceptedCall(callKey, in, t.Generation, ratio, devices)
		digest := domain.Digest(in)
		updated, err := s.store.CommitReading(ctx, opID, in.TaskID, digest, call, ev, closeID, t.Version)
		if err != nil {
			return nil, err
		}
		return &ReadingResult{Task: updated, Accepted: true}, nil
	}

	failed := failedCall(callKey, in, t.Generation, res.Result, 0, devices)
	digest := domain.Digest(in)
	updated, err := s.store.CommitReading(ctx, opID, in.TaskID, digest, failed, nil, "", t.Version)
	if err != nil {
		return nil, err
	}
	return &ReadingResult{Task: updated, Accepted: false, RetryCount: 0, NextRetry: failed.NextRetryAt}, nil
}

// RetryCall re-attempts a previously failed instrument invocation using its
// stored pending reading values. The sequence and retry count advance
// deterministically; on success a single evidence record is written, otherwise
// another failed call is parked at the next fixed backoff time.
func (s *Service) RetryCall(ctx context.Context, opID, callKey string) (*ReadingResult, error) {
	call, err := s.store.GetInstrumentCall(ctx, callKey)
	if err != nil {
		return nil, err
	}
	if call.Result == domain.CallAccepted {
		return nil, ErrStageOutOfOrder
	}
	if domain.RetriesExhausted(call.RetryCount) {
		return nil, ErrStageOutOfOrder
	}
	t, sample, err := s.loadTaskAndSample(ctx, call.TaskID, call.SampleID)
	if err != nil {
		return nil, err
	}
	if err := s.validateActiveLease(ctx, t, call.DeviceSet); err != nil {
		return nil, err
	}

	newSeq := call.RetryCount + 1
	newKey := callKeyFor(call.TaskID, call.SampleID, call.Generation, call.LoadLevel, call.DeviceNo, string(call.Stage), newSeq)
	req := domain.InstrumentRequest{CallKey: newKey, DeviceNo: call.DeviceNo, Stage: call.Stage, LoadLevel: call.LoadLevel}
	res, _ := s.inst.Call(ctx, req)

	in := ReadingInput{
		TaskID: call.TaskID, SampleID: call.SampleID, Stage: call.Stage,
		LoadLevel: call.LoadLevel, Load: call.Load, Displacement: call.Displacement,
		HoldSecs: call.HoldSecs, Rebound: call.Rebound,
		DevicePuller: call.DeviceSet[0], DevicePump: call.DeviceSet[1],
		DeviceDisplacement: call.DeviceSet[2],
	}

	if res.Result == domain.CallAccepted {
		ratio, err := computeRatio(in)
		if err != nil {
			return nil, ErrArithmetic
		}
		count, err := s.store.CountAcceptedEvidence(ctx, call.TaskID, call.SampleID, call.Generation)
		if err != nil {
			return nil, err
		}
		closeID := ""
		if domain.IsLoadComplete(t.LoadLevels, count+1) {
			closeID = call.SampleID
		}
		ev := &domain.LoadEvidence{
			TaskID: in.TaskID, SampleID: in.SampleID, Generation: call.Generation,
			Kind: evidenceKind(sample.Category), Stage: in.Stage, LoadLevel: in.LoadLevel,
			Load: in.Load, Displacement: in.Displacement, HoldSecs: in.HoldSecs,
			Rebound: in.Rebound, Ratio: ratio, DeviceSet: call.DeviceSet,
			Accepted: true, ContentDigest: domain.Digest(in),
		}
		ok := acceptedCall(newKey, in, call.Generation, ratio, call.DeviceSet)
		ok.RetryCount = newSeq
		digest := domain.Digest(in)
		updated, err := s.store.CommitReading(ctx, opID, call.TaskID, digest, ok, ev, closeID, t.Version)
		if err != nil {
			return nil, err
		}
		return &ReadingResult{Task: updated, Accepted: true}, nil
	}

	failed := failedCall(newKey, in, call.Generation, res.Result, newSeq, call.DeviceSet)
	digest := domain.Digest(in)
	updated, err := s.store.CommitReading(ctx, opID, call.TaskID, digest, failed, nil, "", t.Version)
	if err != nil {
		return nil, err
	}
	return &ReadingResult{Task: updated, Accepted: false, RetryCount: newSeq, NextRetry: failed.NextRetryAt}, nil
}

func (s *Service) loadTaskAndSample(ctx context.Context, taskID, sampleID string) (*domain.InspectionTask, store.SampleNode, error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, store.SampleNode{}, err
	}
	switch t.Status {
	case domain.StatusLoading, domain.StatusExpanding, domain.StatusRetesting:
	default:
		return nil, store.SampleNode{}, ErrInvalidTransition
	}
	nodes, err := s.store.ListSampleNodes(ctx, taskID)
	if err != nil {
		return nil, store.SampleNode{}, err
	}
	for _, n := range nodes {
		if n.ID == sampleID && n.HoleKey != nil && n.Generation == t.Generation {
			return t, n, nil
		}
	}
	return nil, store.SampleNode{}, ErrTaskNotFound
}

func (s *Service) validateActiveLease(ctx context.Context, t *domain.InspectionTask, devices [3]string) error {
	now := s.Now()
	for _, d := range devices {
		leases, err := s.store.ActiveLeasesForDevice(ctx, d)
		if err != nil {
			return err
		}
		ok := false
		for _, l := range leases {
			if l.TestID == t.ID && l.Generation == t.Generation &&
				l.Start <= now && now < l.End {
				ok = true
				break
			}
		}
		if !ok {
			return ErrLeaseConflict
		}
	}
	return nil
}

func computeRatio(in ReadingInput) (int64, error) {
	if in.Stage != domain.StageRebound {
		return 0, nil
	}
	return domain.MulScale(in.Rebound, domain.ScaleRatio, in.Displacement)
}

func evidenceKind(c domain.SampleCategory) domain.EvidenceKind {
	switch c {
	case domain.SampleExpanded:
		return domain.EvidenceExpanded
	case domain.SampleRetest:
		return domain.EvidenceRetest
	default:
		return domain.EvidenceOriginal
	}
}

func callKey(taskID, sampleID string, generation, level int, deviceNo, stage string, seq int) string {
	return callKeyFor(taskID, sampleID, generation, level, deviceNo, stage, seq)
}

func callKeyFor(taskID, sampleID string, generation, level int, deviceNo, stage string, seq int) string {
	return fmt.Sprintf("%s:%s:%d:%d:%s:%s:%d", taskID, sampleID, generation, level, deviceNo, stage, seq)
}

func acceptedCall(key string, in ReadingInput, generation int, ratio int64, devices [3]string) domain.InstrumentCall {
	c := baseCall(key, in, generation, devices)
	c.Result = domain.CallAccepted
	return c
}

func failedCall(key string, in ReadingInput, generation int, result domain.CallResult, retryCount int, devices [3]string) domain.InstrumentCall {
	c := baseCall(key, in, generation, devices)
	c.Result = result
	c.RetryCount = retryCount
	c.NextRetryAt = domain.NextRetryAt(retryCount, 0)
	return c
}

func baseCall(key string, in ReadingInput, generation int, devices [3]string) domain.InstrumentCall {
	return domain.InstrumentCall{
		CallKey: key, TaskID: in.TaskID, SampleID: in.SampleID,
		Generation: generation, LoadLevel: in.LoadLevel, DeviceNo: in.DevicePuller, Seq: 0,
		RequestDigest: domain.Digest(in), Stage: in.Stage, Load: in.Load,
		Displacement: in.Displacement, HoldSecs: in.HoldSecs, Rebound: in.Rebound,
		DeviceSet: devices,
	}
}

// GetEvidence returns the immutable evidence chain of a task.
func (s *Service) GetEvidence(ctx context.Context, taskID string) ([]store.EvidenceRow, error) {
	return s.store.ListEvidence(ctx, taskID)
}

// PendingInstrumentCalls returns the retry queue for restart recovery.
func (s *Service) PendingInstrumentCalls(ctx context.Context) ([]domain.InstrumentCall, error) {
	return s.store.ListPendingInstrumentCalls(ctx)
}
