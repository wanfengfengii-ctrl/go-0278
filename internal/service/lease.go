package service

import (
	"context"
	"fmt"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/lease"
)

// LeaseInput describes the full device combination to acquire in one atomic
// transaction.
type LeaseInput struct {
	TestID       string
	TaskID       string
	Generation   int
	Puller       string
	Pump         string
	Displacement string
	Start        domain.LogicalTime
	End          domain.LogicalTime
}

// AcquireLeases atomically acquires the puller, pump and displacement leases for
// a test. All three must be free of overlap within the same logical window, or
// none is acquired. On success the task advances from hole_verification to
// loading.
func (s *Service) AcquireLeases(ctx context.Context, opID string, in LeaseInput) (*domain.InspectionTask, error) {
	t, err := s.GetTask(ctx, in.TaskID)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusHoleVerification {
		return nil, ErrInvalidTransition
	}
	if in.Generation != t.Generation {
		return nil, ErrLeaseConflict
	}
	if !lease.Valid(int64(in.Start), int64(in.End)) {
		return nil, ErrLeaseConflict
	}
	// The three rig roles must be filled by distinct physical devices; reusing
	// one device number for two roles is an invalid combination that must fail
	// as a whole rather than producing a partial lease set.
	if in.Puller == in.Pump || in.Puller == in.Displacement || in.Pump == in.Displacement {
		return nil, ErrLeaseConflict
	}
	leases := []domain.DeviceLease{
		leaseOf(in.TestID, in.Puller, domain.DevicePuller, in.Generation, in.Start, in.End),
		leaseOf(in.TestID, in.Pump, domain.DevicePump, in.Generation, in.Start, in.End),
		leaseOf(in.TestID, in.Displacement, domain.DeviceDisplacement, in.Generation, in.Start, in.End),
	}
	digest := domain.Digest(leases)
	return s.store.AcquireLeases(ctx, opID, in.TaskID, digest, leases, t.Version)
}

func leaseOf(testID, deviceNo string, typ domain.DeviceType, generation int, start, end domain.LogicalTime) domain.DeviceLease {
	return domain.DeviceLease{
		ID:         fmt.Sprintf("lease-%s-%s-%d", testID, deviceNo, int64(start)),
		DeviceNo:   deviceNo,
		TestID:     testID,
		Generation: generation,
		Start:      start,
		End:        end,
	}
}

// RenewLeaseInput is the input to a lease renewal.
type RenewLeaseInput struct {
	LeaseID         string
	Holder          string
	Generation      int
	NewEnd          domain.LogicalTime
	ExpectedVersion int64
}

// RenewLease extends a lease to a new end time, validating the holder,
// generation and version and rejecting any overlap introduced by the extension.
func (s *Service) RenewLease(ctx context.Context, opID, taskID string, in RenewLeaseInput) (*domain.DeviceLease, error) {
	if !lease.Valid(0, int64(in.NewEnd)) {
		return nil, ErrLeaseConflict
	}
	digest := domain.Digest(in)
	return s.store.RenewLease(ctx, opID, taskID, digest, in.LeaseID, in.Holder, in.Generation, int64(in.NewEnd), in.ExpectedVersion)
}

// ReleaseLeaseInput is the input to a lease release.
type ReleaseLeaseInput struct {
	LeaseID         string
	Holder          string
	Generation      int
	ExpectedVersion int64
}

// ReleaseLease releases a lease at the current logical time, validating holder,
// generation and version.
func (s *Service) ReleaseLease(ctx context.Context, opID, taskID string, in ReleaseLeaseInput) error {
	digest := domain.Digest(in)
	return s.store.ReleaseLease(ctx, opID, taskID, digest, in.LeaseID, in.Holder, in.Generation, int64(s.Now()), in.ExpectedVersion)
}

// ActiveLeasesForDevice exposes the current ownership of one device for restart
// recovery checks.
func (s *Service) ActiveLeasesForDevice(ctx context.Context, deviceNo string) ([]domain.DeviceLease, error) {
	return s.store.ActiveLeasesForDevice(ctx, deviceNo)
}
