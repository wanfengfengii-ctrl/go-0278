package service

import (
	"context"
	"fmt"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/store"
)

// CreateTaskResult carries the created task plus its list of lock violations
// (empty on a valid task).
type CreateTaskResult struct {
	Task *domain.InspectionTask
}

// CreateTask creates a pending-lock task from normalized input fields.
func (s *Service) CreateTask(ctx context.Context, req domain.InspectionTask) (*domain.InspectionTask, error) {
	t := req
	if t.ID == "" {
		t.ID = fmt.Sprintf("t-%d", s.seq.Add(1))
	}
	t.Generation = 1
	t.Status = domain.StatusPendingLock
	t.Version = 0
	if err := s.store.CreateTask(ctx, &t); err != nil {
		return nil, err
	}
	return s.store.GetTask(ctx, t.ID)
}

// GetTask loads a task by id.
func (s *Service) GetTask(ctx context.Context, id string) (*domain.InspectionTask, error) {
	t, err := s.store.GetTask(ctx, id)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return t, nil
}

// LockResult is the outcome of a lock attempt: either a locked task or a list of
// deterministic violations.
type LockResult struct {
	Task       *domain.InspectionTask
	Violations []domain.LockViolation
}

// LockTask validates the full lock spec and atomically freezes the task snapshot.
// Any mileage/cycle mismatch, stale digest, duplicate hole or invalid quota is
// reported as a sorted violation list and writes nothing.
//
// A retry that repeats the same operation id (for example after a network blip
// dropped the first response) reuses the already-locked task instead of failing
// the transition, so a client can safely resend POST /v1/tasks/{id}/lock.
func (s *Service) LockTask(ctx context.Context, opID, taskID string, spec *domain.LockSpec) (*LockResult, error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusPendingLock {
		// The task has already been locked (or moved further). If this is a replay
		// of the original lock — same operation id and request digest — return the
		// locked task so a client retrying after a dropped response reuses the first
		// result instead of seeing an invalid_transition.
		locked, rerr := s.replayLock(ctx, opID, taskID, spec)
		if rerr != nil {
			return nil, rerr
		}
		if locked != nil {
			return &LockResult{Task: locked}, nil
		}
		return nil, ErrInvalidTransition
	}
	violations := domain.ValidateLockSpec(t, spec)
	if len(violations) > 0 {
		return &LockResult{Violations: violations}, nil
	}
	digest := domain.Digest(spec)
	locked, err := s.store.LockTask(ctx, opID, taskID, digest, spec, s.Now(), t.Version)
	if err != nil {
		return nil, err
	}
	return &LockResult{Task: locked}, nil
}

// replayLock resolves a retried lock by its operation id. It returns the
// already-locked task with a nil error when opID matches a prior successful lock
// of the same task with the same request digest, so a client retrying after a
// dropped response reuses the first result. It returns ErrConflict when opID was
// already committed with a different request digest, matching the store's
// idempotency contract. A nil task with a nil error means this is not a replay and
// the caller should apply its normal transition guard.
func (s *Service) replayLock(ctx context.Context, opID, taskID string, spec *domain.LockSpec) (*domain.InspectionTask, error) {
	if opID == "" {
		return nil, nil
	}
	rec, err := s.store.GetOperation(ctx, opID)
	if err != nil {
		// No prior operation record for this id: not a replay.
		return nil, nil
	}
	if rec.TaskID != taskID || rec.Action != "lock" {
		return nil, nil
	}
	if rec.RequestDigest != domain.Digest(spec) {
		return nil, store.ErrConflict
	}
	return s.store.GetTask(ctx, taskID)
}
