package store

import (
	"context"
	"database/sql"
	"errors"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

// ErrReplay is returned by mutating store methods when the supplied operation id
// has already been committed with an identical request digest. The caller maps
// this to a replay of the original response rather than performing the write
// again.
var ErrReplay = errors.New("store: operation already applied")

// checkOperation returns done=true when an operation record already exists for
// opID with a matching request digest (replay), and ErrConflict when it exists
// with a different digest. When no record exists it returns done=false so the
// caller proceeds and records the operation before commit.
func checkOperation(ctx context.Context, tx *sql.Tx, opID, requestDigest string) (bool, error) {
	if opID == "" {
		return false, nil
	}
	var existing string
	err := tx.QueryRowContext(ctx,
		`SELECT request_digest FROM operation_records WHERE operation_id = ?`, opID).Scan(&existing)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if existing == requestDigest {
		return true, nil
	}
	return false, ErrConflict
}

// recordOperation inserts an operation record inside the current transaction so
// that a crash before commit can never leave a business write without its
// idempotency marker, and vice versa.
func recordOperation(ctx context.Context, tx *sql.Tx, opID, taskID, action, requestDigest, responseCode, responseDigest string, commitVersion int64) error {
	if opID == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO operation_records
			(operation_id, task_id, action, request_digest, response_code, response_digest, commit_version)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		opID, taskID, action, requestDigest, responseCode, responseDigest, commitVersion)
	return err
}

// GetOperation loads an operation record by id, returning ErrNotFound when
// absent. It powers cross-restart idempotent replay.
func (s *Store) GetOperation(ctx context.Context, opID string) (*domain.OperationRecord, error) {
	var r domain.OperationRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT operation_id, task_id, action, request_digest, response_code,
		       response_digest, commit_version
		FROM operation_records WHERE operation_id = ?`, opID).
		Scan(&r.OperationID, &r.TaskID, &r.Action, &r.RequestDigest,
			&r.ResponseCode, &r.ResponseDigest, &r.CommitVersion)
	if err != nil {
		return nil, requireFound(err)
	}
	return &r, nil
}
