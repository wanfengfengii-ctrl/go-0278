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
// loading. Each device number must be registered in the catalog and its type
// must match the requested role; an unregistered or wrongly typed device can
// never form a lease or advance the task.
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
	if err := s.validateDeviceCombination(ctx, in.Puller, in.Pump, in.Displacement); err != nil {
		return nil, err
	}
	leases := []domain.DeviceLease{
		leaseOf(in.TestID, in.Puller, domain.DevicePuller, in.Generation, in.Start, in.End),
		leaseOf(in.TestID, in.Pump, domain.DevicePump, in.Generation, in.Start, in.End),
		leaseOf(in.TestID, in.Displacement, domain.DeviceDisplacement, in.Generation, in.Start, in.End),
	}
	digest := domain.Digest(leases)
	return s.store.AcquireLeases(ctx, opID, in.TaskID, digest, leases, t.Version)
}

// validateDeviceCombination checks that the three requested device numbers are
// each registered in the catalog and that their catalog type matches the role
// (puller, pump, displacement) they are being leased for. A missing number or a
// type mismatch (e.g. using a pump number where a puller is required) is
// rejected before any lease is created, so the task never advances to loading
// and later readings can never pass lease validation on such a combination.
func (s *Service) validateDeviceCombination(ctx context.Context, puller, pump, displacement string) error {
	return validateDevice(s.store, ctx, puller, domain.DevicePuller, pump, domain.DevicePump, displacement, domain.DeviceDisplacement)
}

// validateDevice resolves each (number, expected type) pair against the device
// catalog. The store is passed so the helper stays testable with any
// deviceCatalog implementation.
func validateDevice(c deviceCatalog, ctx context.Context, puller string, pullerType domain.DeviceType, pump string, pumpType domain.DeviceType, disp string, dispType domain.DeviceType) error {
	for _, p := range []struct {
		number   string
		expected domain.DeviceType
	}{
		{puller, pullerType},
		{pump, pumpType},
		{disp, dispType},
	} {
		if p.number == "" {
			return ErrDeviceInvalid
		}
		d, err := c.GetDevice(ctx, p.number)
		if err != nil || d.Type != p.expected {
			return ErrDeviceInvalid
		}
	}
	return nil
}

// deviceCatalog is the subset of *store.Store used to look up devices. It is
// extracted so device validation can be unit-tested with a fake catalog.
type deviceCatalog interface {
	GetDevice(ctx context.Context, deviceNo string) (*domain.Device, error)
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
