package store

import (
	"context"
	"database/sql"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

// EvidenceRow is the persisted form of a load evidence record.
type EvidenceRow struct {
	domain.LoadEvidence
	Seq int64
}

// CommitReading persists one instrument invocation and, when the invocation was
// accepted, appends the corresponding evidence record and advances the task
// version in the same transaction. A failed invocation records only the call, so
// no qualified level, no evidence prefix advancement and no version bump can
// survive a partial write.
func (s *Store) CommitReading(ctx context.Context, opID, taskID, requestDigest string, call domain.InstrumentCall, evidence *domain.LoadEvidence, closeSampleID string, expectedVersion int64) (*domain.InspectionTask, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		if err := insertInstrumentCall(ctx, tx, call); err != nil {
			return err
		}
		if evidence != nil {
			if err := insertEvidence(ctx, tx, *evidence); err != nil {
				return err
			}
			if closeSampleID != "" {
				if _, err := tx.ExecContext(ctx, `
					UPDATE sample_nodes SET closed = 1
					WHERE id = ? AND task_id = ? AND hole_no IS NOT NULL`, closeSampleID, taskID); err != nil {
					return err
				}
			}
			res, err := tx.ExecContext(ctx, `
				UPDATE inspection_tasks SET version = version + 1
				WHERE id = ? AND version = ?`, taskID, expectedVersion)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n != 1 {
				return ErrConflict
			}
			return recordOperation(ctx, tx, opID, taskID, "reading", requestDigest, "accepted", requestDigest, expectedVersion+1)
		}
		return recordOperation(ctx, tx, opID, taskID, "reading", requestDigest, "retry", requestDigest, expectedVersion)
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, taskID)
}

func insertEvidence(ctx context.Context, tx *sql.Tx, e domain.LoadEvidence) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO load_evidence
			(task_id, sample_id, generation, kind, stage, load_level, load,
			 displacement, hold_secs, rebound, ratio, device_puller, device_pump,
			 device_displacement, accepted, content_digest)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.TaskID, e.SampleID, e.Generation, string(e.Kind), string(e.Stage), e.LoadLevel,
		e.Load, e.Displacement, e.HoldSecs, e.Rebound, e.Ratio,
		e.DeviceSet[0], e.DeviceSet[1], e.DeviceSet[2], boolInt(e.Accepted), e.ContentDigest)
	return err
}

func insertInstrumentCall(ctx context.Context, tx *sql.Tx, c domain.InstrumentCall) error {
	var evRef *int64
	if c.EvidenceRef != nil {
		evRef = c.EvidenceRef
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO instrument_calls
			(call_key, task_id, sample_id, generation, load_level, device_no, seq,
			 result, retry_count, next_retry_at, evidence_ref, request_digest,
			 stage, load, displacement, hold_secs, rebound, device_puller,
			 device_pump, device_disp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.CallKey, c.TaskID, c.SampleID, c.Generation, c.LoadLevel, c.DeviceNo,
		c.Seq, string(c.Result), c.RetryCount, int64(c.NextRetryAt), evRef,
		c.RequestDigest, string(c.Stage), c.Load, c.Displacement, c.HoldSecs,
		c.Rebound, c.DeviceSet[0], c.DeviceSet[1], c.DeviceSet[2])
	return err
}

// CountAcceptedEvidence returns the number of accepted evidence records for a
// sample and generation. It is the current length of the bolt's load prefix.
func (s *Store) CountAcceptedEvidence(ctx context.Context, taskID, sampleID string, generation int) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM load_evidence
		WHERE task_id = ? AND sample_id = ? AND generation = ? AND accepted = 1`,
		taskID, sampleID, generation).Scan(&n)
	return n, err
}

// ListEvidence returns all evidence records of a task ordered by sequence, so
// the immutable evidence chain is returned in append order across generations.
func (s *Store) ListEvidence(ctx context.Context, taskID string) ([]EvidenceRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT seq, task_id, sample_id, generation, kind, stage, load_level, load,
		       displacement, hold_secs, rebound, ratio, device_puller, device_pump,
		       device_displacement, accepted, content_digest
		FROM load_evidence WHERE task_id = ? ORDER BY seq`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvidenceRow
	for rows.Next() {
		var r EvidenceRow
		var accepted int
		if err := rows.Scan(&r.Seq, &r.TaskID, &r.SampleID, &r.Generation,
			&r.Kind, &r.Stage, &r.LoadLevel, &r.Load, &r.Displacement, &r.HoldSecs,
			&r.Rebound, &r.Ratio, &r.DeviceSet[0], &r.DeviceSet[1], &r.DeviceSet[2],
			&accepted, &r.ContentDigest); err != nil {
			return nil, err
		}
		r.Accepted = accepted != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListPendingInstrumentCalls returns instrument calls that still require a
// retry (their result was not accepted). This powers restart recovery: the
// service replays the pending queue deterministically after a crash.
func (s *Store) ListPendingInstrumentCalls(ctx context.Context) ([]domain.InstrumentCall, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT call_key, task_id, sample_id, generation, load_level, device_no, seq,
		       result, retry_count, next_retry_at, evidence_ref, request_digest,
		       stage, load, displacement, hold_secs, rebound, device_puller,
		       device_pump, device_disp
		FROM instrument_calls WHERE result <> 'accepted' ORDER BY call_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.InstrumentCall
	for rows.Next() {
		c, err := scanInstrumentCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetInstrumentCall loads a single instrument call by its deterministic key.
func (s *Store) GetInstrumentCall(ctx context.Context, callKey string) (*domain.InstrumentCall, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT call_key, task_id, sample_id, generation, load_level, device_no, seq,
		       result, retry_count, next_retry_at, evidence_ref, request_digest,
		       stage, load, displacement, hold_secs, rebound, device_puller,
		       device_pump, device_disp
		FROM instrument_calls WHERE call_key = ?`, callKey)
	c, err := scanInstrumentCall(row)
	if err != nil {
		return nil, requireFound(err)
	}
	return &c, nil
}

// CallKeyExists reports whether an instrument invocation has already been
// committed for the given content-derived call_key. It powers pre-command
// arbitration: when two field terminals submit identical readings under
// distinct operation_ids, only the call_key is the same, so this check lets the
// second submission discover the first's committed call and return a conflict
// without re-triggering the puller.
func (s *Store) CallKeyExists(ctx context.Context, callKey string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM instrument_calls WHERE call_key = ?`, callKey).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func scanInstrumentCall(scan interface{ Scan(...any) error }) (domain.InstrumentCall, error) {
	var c domain.InstrumentCall
	var evRef sql.NullInt64
	err := scan.Scan(&c.CallKey, &c.TaskID, &c.SampleID, &c.Generation, &c.LoadLevel,
		&c.DeviceNo, &c.Seq, &c.Result, &c.RetryCount, &c.NextRetryAt, &evRef,
		&c.RequestDigest, &c.Stage, &c.Load, &c.Displacement, &c.HoldSecs,
		&c.Rebound, &c.DeviceSet[0], &c.DeviceSet[1], &c.DeviceSet[2])
	if err != nil {
		return c, err
	}
	if evRef.Valid {
		c.EvidenceRef = &evRef.Int64
	}
	return c, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
