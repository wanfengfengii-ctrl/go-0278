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
func (s *Service) LockTask(ctx context.Context, opID, taskID string, spec *domain.LockSpec) (*LockResult, error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusPendingLock {
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
