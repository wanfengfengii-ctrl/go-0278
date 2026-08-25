package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

// CreateTask inserts a new pending-lock task. It fails with ErrDuplicate when
// the task id already exists.
func (s *Store) CreateTask(ctx context.Context, t *domain.InspectionTask) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO inspection_tasks
				(id, mileage_start_mm, mileage_end_mm, rock_grade, cycle_no,
				 sampling_seed, generation, status, version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			t.ID, t.MileageStartMM, t.MileageEndMM, t.RockGrade, t.CycleNo,
			t.SamplingSeed, t.Generation, string(t.Status))
		return err
	})
}

// GetTask loads a task by id, returning ErrNotFound when absent.
func (s *Store) GetTask(ctx context.Context, id string) (*domain.InspectionTask, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, mileage_start_mm, mileage_end_mm, rock_grade, cycle_no,
		       layout_revision, layout_digest, grout_digest, sampling_seed,
		       generation, status, version, locked_at, verdict,
		       verdict_credential, seal_digest, layer_quotas, load_levels,
		       stop_threshold, accept_threshold, influence_bound,
		       calibration_digest, lock_digest
		FROM inspection_tasks WHERE id = ?`, id)
	return scanTask(row)
}

// scanTask decodes a single inspection_tasks row into a domain.InspectionTask.
func scanTask(row interface{ Scan(...any) error }) (*domain.InspectionTask, error) {
	var (
		t           domain.InspectionTask
		lockedAt    sql.NullInt64
		verdict     sql.NullString
		layerQuotas string
		loadLevels  string
	)
	err := row.Scan(
		&t.ID, &t.MileageStartMM, &t.MileageEndMM, &t.RockGrade, &t.CycleNo,
		&t.LayoutRevision, &t.LayoutDigest, &t.GroutDigest, &t.SamplingSeed,
		&t.Generation, &t.Status, &t.Version, &lockedAt, &verdict,
		&t.VerdictCredential, &t.SealDigest, &layerQuotas, &loadLevels,
		&t.StopThreshold, &t.AcceptThreshold, &t.InfluenceBound,
		&t.CalibrationDigest, &t.LockDigest,
	)
	if err != nil {
		return nil, requireFound(err)
	}
	if lockedAt.Valid {
		lt := domain.LogicalTime(lockedAt.Int64)
		t.LockedAt = &lt
	}
	if verdict.Valid {
		v := domain.VerdictType(verdict.String)
		t.FinalVerdictSlot = &v
	}
	t.LayerQuotas = map[string]int{}
	if layerQuotas != "" {
		_ = json.Unmarshal([]byte(layerQuotas), &t.LayerQuotas)
	}
	t.LoadLevels = []domain.LoadLevelDef{}
	if loadLevels != "" {
		_ = json.Unmarshal([]byte(loadLevels), &t.LoadLevels)
	}
	return &t, nil
}

// LockTask atomically freezes the lock spec: it writes every candidate hole and
// the rule snapshot fields, flips the task to pending_sample, bumps the version
// and records the logical lock time. It fails with ErrConflict if the task is
// not currently pending_lock or the expected version does not match.
func (s *Store) LockTask(ctx context.Context, opID, id, requestDigest string, spec *domain.LockSpec, at domain.LogicalTime, expectedVersion int64) (*domain.InspectionTask, error) {
	quotasJSON, _ := json.Marshal(spec.LayerQuotas)
	levelsJSON, _ := json.Marshal(spec.LoadLevels)
	lockDigest := domain.Digest(spec)

	err := s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		for _, c := range spec.Candidates {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO candidate_holes
					(task_id, mileage_mm, ring_no, azimuth, hole_no, coord_x,
					 coord_y, coord_z, bar_batch, anchor_batch, layer_key)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, c.HoleKey.MileageMM, c.HoleKey.RingNo, string(c.HoleKey.Azimuth),
				c.HoleKey.HoleNo, c.CoordX, c.CoordY, c.CoordZ, c.BarBatch,
				c.AnchorBatch, c.LayerKey)
			if err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE inspection_tasks
			SET layout_revision = ?, layout_digest = ?, grout_digest = ?,
			    layer_quotas = ?, load_levels = ?, stop_threshold = ?,
			    accept_threshold = ?, influence_bound = ?, calibration_digest = ?,
			    lock_digest = ?, status = ?, version = version + 1, locked_at = ?
			WHERE id = ? AND status = ? AND version = ?`,
			spec.LayoutRevision, spec.LayoutDigest, spec.GroutDigest,
			string(quotasJSON), string(levelsJSON), spec.StopThreshold,
			spec.AcceptThreshold, spec.InfluenceBound, spec.CalibrationDigest,
			lockDigest, string(domain.StatusPendingSample), int64(at),
			id, string(domain.StatusPendingLock), expectedVersion)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		return recordOperation(ctx, tx, opID, id, "lock", requestDigest, "locked", lockDigest, expectedVersion+1)
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, id)
}

// ListCandidateHoles returns all locked candidate holes of a task in canonical
// order (mileage, ring, azimuth ordinal, hole number).
func (s *Store) ListCandidateHoles(ctx context.Context, taskID string) ([]domain.CandidateHole, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, mileage_mm, ring_no, azimuth, hole_no, coord_x, coord_y,
		       coord_z, bar_batch, anchor_batch, layer_key
		FROM candidate_holes WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holes []domain.CandidateHole
	for rows.Next() {
		var c domain.CandidateHole
		var azimuth string
		if err := rows.Scan(&c.TaskID, &c.HoleKey.MileageMM, &c.HoleKey.RingNo,
			&azimuth, &c.HoleKey.HoleNo, &c.CoordX, &c.CoordY, &c.CoordZ,
			&c.BarBatch, &c.AnchorBatch, &c.LayerKey); err != nil {
			return nil, err
		}
		c.HoleKey.Azimuth = domain.Azimuth(azimuth)
		holes = append(holes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return holes, nil
}

// bumpStatus updates a task's status and increments its version in one atomic
// step, returning ErrConflict when the current status/version does not match.
func (s *Store) bumpStatus(ctx context.Context, tx *sql.Tx, id string, from domain.TaskStatus, to domain.TaskStatus, expectedVersion int64) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE inspection_tasks SET status = ?, version = version + 1
		WHERE id = ? AND status = ? AND version = ?`,
		string(to), id, string(from), expectedVersion)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

// currentStatus reads the status of a task at a given expected version, returning
// ErrConflict when the version no longer matches. It lets a mutating
// transaction branch on the live status without re-reading through GetTask.
func currentStatus(ctx context.Context, tx *sql.Tx, id string, expectedVersion int64) (string, error) {
	var status string
	err := tx.QueryRowContext(ctx,
		`SELECT status FROM inspection_tasks WHERE id = ? AND version = ?`,
		id, expectedVersion).Scan(&status)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", ErrConflict
		}
		return "", err
	}
	return status, nil
}
