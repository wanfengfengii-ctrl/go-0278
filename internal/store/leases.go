package store

import (
	"context"
	"database/sql"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/lease"
)

// EnsureDevices upserts a set of physical devices. It is used to seed the test
// rig (puller, pump, displacement) before lease acquisition.
func (s *Store) EnsureDevices(ctx context.Context, devices []domain.Device) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, d := range devices {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO devices (device_no, device_type, state, calib_ver)
				VALUES (?, ?, ?, ?)
				ON CONFLICT(device_no) DO UPDATE SET
					device_type = excluded.device_type,
					calib_ver = excluded.calib_ver`,
				d.Number, string(d.Type), d.State, d.CalibVer)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// AcquireLeases atomically acquires a full device combination (one puller, one
// pump, one displacement) for a test. Every device in the combination must be
// free of any overlapping lease within the same transaction, otherwise the whole
// acquisition fails and no partial lease is left behind. On success the task
// advances from hole_verification to loading.
func (s *Store) AcquireLeases(ctx context.Context, opID, taskID, requestDigest string, leases []domain.DeviceLease, expectedVersion int64) (*domain.InspectionTask, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		all, err := allLeavesVerified(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if !all {
			return ErrConflict
		}
		for _, l := range leases {
			if overlap, err := deviceOverlaps(ctx, tx, l.DeviceNo, int64(l.Start), int64(l.End)); err != nil {
				return err
			} else if overlap {
				return ErrConflict
			}
		}
		for _, l := range leases {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO device_leases (id, device_no, test_id, generation, start_at, end_at, version)
				VALUES (?, ?, ?, ?, ?, ?, 0)`,
				l.ID, l.DeviceNo, l.TestID, l.Generation, int64(l.Start), int64(l.End))
			if err != nil {
				continue
			}
		}
		if err := s.bumpStatus(ctx, tx, taskID, domain.StatusHoleVerification, domain.StatusLoading, expectedVersion); err != nil {
			return err
		}
		return recordOperation(ctx, tx, opID, taskID, "acquire_leases", requestDigest, "acquired", requestDigest, expectedVersion+1)
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, taskID)
}

// deviceOverlaps reports whether any non-released lease of the device overlaps
// the half-open window [start, end).
func deviceOverlaps(ctx context.Context, tx *sql.Tx, deviceNo string, start, end int64) (bool, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT start_at, end_at FROM device_leases
		WHERE device_no = ? AND released_at IS NULL`, deviceNo)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var s, e int64
		if err := rows.Scan(&s, &e); err != nil {
			return false, err
		}
		if lease.Overlaps(start, end, s, e) {
			return true, nil
		}
	}
	return false, rows.Err()
}

// RenewLease extends a lease to a new end time. The caller must be the current
// holder with a matching generation and expected version, and the new end must
// not introduce an overlap with any other lease of the same device.
func (s *Store) RenewLease(ctx context.Context, opID, taskID, requestDigest string, leaseID, holder string, generation int, newEnd int64, expectedVersion int64) (*domain.DeviceLease, error) {

	err := s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		var deviceNo string
		var start int64
		if err := tx.QueryRowContext(ctx,
			`SELECT device_no, start_at FROM device_leases WHERE id = ? AND test_id = ? AND generation = ?`,
			leaseID, holder, generation).Scan(&deviceNo, &start); err != nil {
			return requireFound(err)
		}
		if overlap, err := deviceOverlapsExcluding(ctx, tx, deviceNo, start, newEnd, leaseID); err != nil {
			return err
		} else if overlap {
			return ErrConflict
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE device_leases SET end_at = ?, version = version + 1
			WHERE id = ? AND version = ? AND released_at IS NULL`,
			newEnd, leaseID, expectedVersion)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
		return recordOperation(ctx, tx, opID, taskID, "renew_lease", requestDigest, "renewed", requestDigest, expectedVersion+1)
	})
	if err != nil {
		return nil, err
	}
	return s.GetLease(ctx, leaseID)
}

// ReleaseLease releases a lease at the given logical time. The caller must be
// the holder with a matching generation and version, and the lease must not
// already be released.
func (s *Store) ReleaseLease(ctx context.Context, opID, taskID, requestDigest string, leaseID, holder string, generation int, at int64, expectedVersion int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE device_leases SET released_at = ?, version = version + 1
			WHERE id = ? AND test_id = ? AND generation = ? AND version = ? AND released_at IS NULL`,
			at, leaseID, holder, generation, expectedVersion)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
		return recordOperation(ctx, tx, opID, taskID, "release_lease", requestDigest, "released", requestDigest, expectedVersion+1)
	})
}

func deviceOverlapsExcluding(ctx context.Context, tx *sql.Tx, deviceNo string, start, end int64, excludeID string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, start_at, end_at FROM device_leases
		WHERE device_no = ? AND released_at IS NULL AND id <> ?`, deviceNo, excludeID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var s, e int64
		if err := rows.Scan(&id, &s, &e); err != nil {
			return false, err
		}
		if lease.Overlaps(start, end, s, e) {
			return true, nil
		}
	}
	return false, rows.Err()
}

// GetLease loads a lease by id, returning ErrNotFound when absent.
func (s *Store) GetLease(ctx context.Context, id string) (*domain.DeviceLease, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, device_no, test_id, generation, start_at, end_at, version, released_at
		FROM device_leases WHERE id = ?`, id)
	var l domain.DeviceLease
	var released sql.NullInt64
	if err := row.Scan(&l.ID, &l.DeviceNo, &l.TestID, &l.Generation,
		&l.Start, &l.End, &l.Version, &released); err != nil {
		return nil, requireFound(err)
	}
	if released.Valid {
		rt := domain.LogicalTime(released.Int64)
		l.ReleasedAt = &rt
	}
	return &l, nil
}

// ActiveLeasesForDevice returns the non-released leases of one device, ordered
// by start time. It supports the restart-recovery ownership check.
func (s *Store) ActiveLeasesForDevice(ctx context.Context, deviceNo string) ([]domain.DeviceLease, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, device_no, test_id, generation, start_at, end_at, version, released_at
		FROM device_leases WHERE device_no = ? AND released_at IS NULL ORDER BY start_at`, deviceNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DeviceLease
	for rows.Next() {
		var l domain.DeviceLease
		var released sql.NullInt64
		if err := rows.Scan(&l.ID, &l.DeviceNo, &l.TestID, &l.Generation,
			&l.Start, &l.End, &l.Version, &released); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
