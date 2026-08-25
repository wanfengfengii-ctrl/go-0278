package store

import (
	"context"
	"database/sql"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

// CommitExpansion atomically writes the expanded sample nodes produced by an
// influence-domain computation and advances the task into the expanding state.
// The influence digest and every expanded leaf are written together so a crash
// cannot leave a partial expansion.
func (s *Store) CommitExpansion(ctx context.Context, opID, taskID, requestDigest string, nodes []SampleNode, influenceDigest string, from domain.TaskStatus, expectedVersion int64) (*domain.InspectionTask, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		for i := range nodes {
			nodes[i].InfluenceDigest = influenceDigest
			if err := insertSampleNode(ctx, tx, nodes[i]); err != nil {
				return err
			}
		}
		if err := s.bumpStatus(ctx, tx, taskID, from, domain.StatusExpanding, expectedVersion); err != nil {
			return err
		}
		return recordOperation(ctx, tx, opID, taskID, "expansion", requestDigest, "expanded", requestDigest, expectedVersion+1)
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, taskID)
}

// MarkExpansionIncomplete advances the task to pending_review when boundary data
// is missing, without writing any guessed expansion samples.
func (s *Store) MarkExpansionIncomplete(ctx context.Context, opID, taskID, requestDigest string, from domain.TaskStatus, expectedVersion int64) (*domain.InspectionTask, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		if err := s.bumpStatus(ctx, tx, taskID, from, domain.StatusPendingReview, expectedVersion); err != nil {
			return err
		}
		return recordOperation(ctx, tx, opID, taskID, "expansion", requestDigest, "pending_review", requestDigest, expectedVersion+1)
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, taskID)
}

// SealReinforcement records a reinforcement against an influence domain and
// advances the task from expanding to pending_reinforce. The new generation is
// not created until CreateRetestGeneration runs, keeping reinforcement and
// retest as separate, independently idempotent steps.
func (s *Store) SealReinforcement(ctx context.Context, opID, taskID, requestDigest string, influenceDigest, reinforceDigest, reason string, prevGen int, at domain.LogicalTime, expectedVersion int64) (*domain.InspectionTask, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO reinforcements
				(task_id, influence_digest, reinforce_digest, prev_generation,
				 new_generation, reason, effective_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			taskID, influenceDigest, reinforceDigest, prevGen, prevGen+1, reason, int64(at))
		if err != nil {
			return err
		}
		if err := s.bumpStatus(ctx, tx, taskID, domain.StatusExpanding, domain.StatusPendingReinforce, expectedVersion); err != nil {
			return err
		}
		return recordOperation(ctx, tx, opID, taskID, "reinforce", requestDigest, "sealed", requestDigest, expectedVersion+1)
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, taskID)
}

// CreateRetestGeneration creates a strictly monotonic new inspection generation
// and writes its retest sample nodes. The generation uniqueness constraint in
// the schema makes concurrent retest creation deterministic: exactly one caller
// wins and the other observes ErrDuplicate/ErrConflict without a forked tree.
func (s *Store) CreateRetestGeneration(ctx context.Context, opID, taskID, requestDigest string, nodes []SampleNode, newGen int, expectedVersion int64) (*domain.InspectionTask, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		for _, n := range nodes {
			n.Generation = newGen
			if err := insertSampleNode(ctx, tx, n); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE inspection_tasks SET generation = ?, version = version + 1
			WHERE id = ? AND status = ? AND version = ?`,
			newGen, taskID, domain.StatusPendingReinforce, expectedVersion)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
		if err := s.bumpStatus(ctx, tx, taskID, domain.StatusPendingReinforce, domain.StatusRetesting, expectedVersion+1); err != nil {
			return err
		}
		return recordOperation(ctx, tx, opID, taskID, "retest", requestDigest, "retesting", requestDigest, expectedVersion+2)
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, taskID)
}

// ReviewRow is the persisted form of a qualified review signature.
type ReviewRow struct {
	TaskID          string
	Reviewer        string
	QualSnapshot    string
	SignatureDigest string
	SubmitVersion   int64
}

// SubmitReview records one qualified review signature. Duplicate reviewers are
// rejected by the primary key (task_id, reviewer).
func (s *Store) SubmitReview(ctx context.Context, opID, taskID, requestDigest string, review ReviewRow, expectedVersion int64) (*domain.InspectionTask, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO reviews (task_id, reviewer, qual_snapshot, signature_digest, submit_version)
			VALUES (?, ?, ?, ?, ?)`,
			review.TaskID, review.Reviewer, review.QualSnapshot, review.SignatureDigest, expectedVersion)
		if err != nil {
			return err
		}
		return recordOperation(ctx, tx, opID, taskID, "review", requestDigest, "reviewed", requestDigest, expectedVersion)
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, taskID)
}

// ListReviews returns all review signatures of a task.
func (s *Store) ListReviews(ctx context.Context, taskID string) ([]ReviewRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, reviewer, qual_snapshot, signature_digest, submit_version
		FROM reviews WHERE task_id = ? ORDER BY reviewer`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewRow
	for rows.Next() {
		var r ReviewRow
		if err := rows.Scan(&r.TaskID, &r.Reviewer, &r.QualSnapshot, &r.SignatureDigest, &r.SubmitVersion); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountUnclosedSamples returns the number of leaf sample nodes of the current
// generation that are not yet closed. Finalization requires zero.
func (s *Store) CountUnclosedSamples(ctx context.Context, taskID string, generation int) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sample_nodes
		WHERE task_id = ? AND hole_no IS NOT NULL AND generation = ? AND closed = 0`,
		taskID, generation).Scan(&n)
	return n, err
}

// Finalize competes for the single final slot. The conditional update on a null
// verdict guarantees at most one concurrent caller can win; losers observe
// ErrConflict and may replay the winning verdict.
func (s *Store) Finalize(ctx context.Context, opID, taskID, requestDigest string, verdict domain.VerdictType, credential, sealDigest string, expectedVersion int64) (*domain.InspectionTask, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE inspection_tasks
			SET verdict = ?, verdict_credential = ?, seal_digest = ?, status = ?, version = version + 1
			WHERE id = ? AND verdict IS NULL AND status = ? AND version = ?`,
			string(verdict), credential, sealDigest, taskStatusForVerdict(verdict),
			taskID, domain.StatusPendingReview, expectedVersion)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
		return recordOperation(ctx, tx, opID, taskID, "finalize", requestDigest, "final", requestDigest, expectedVersion+1)
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, taskID)
}

func taskStatusForVerdict(v domain.VerdictType) domain.TaskStatus {
	switch v {
	case domain.VerdictPass:
		return domain.StatusPassed
	case domain.VerdictQuarantined:
		return domain.StatusQuarantined
	default:
		return domain.StatusCancelled
	}
}
