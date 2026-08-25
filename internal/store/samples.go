package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

// SampleNode is the persisted form of a sample-tree node (a spatial layer node
// or a leaf sample referencing one hole).
type SampleNode struct {
	ID              string
	TaskID          string
	ParentID        string
	LayerKey        string
	HoleKey         *domain.HoleKey
	Category        domain.SampleCategory
	PickOrder       int
	SourceFailure   *domain.HoleKey
	InfluenceDigest string
	Generation      int
	Closed          bool
	Verified        bool
}

// CommitSampleTree atomically replaces the sample tree of a task and advances it
// from pending_sample to hole_verification. The whole tree is written inside one
// transaction together with its operation record, so a quota conflict, duplicate
// attribution or crash leaves no partial sample tree.
func (s *Store) CommitSampleTree(ctx context.Context, opID, taskID, requestDigest string, nodes []SampleNode, expectedVersion int64) (*domain.InspectionTask, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		for _, n := range nodes {
			if err := insertSampleNode(ctx, tx, n); err != nil {
				return err
			}
		}
		if err := s.bumpStatus(ctx, tx, taskID, domain.StatusPendingSample, domain.StatusHoleVerification, expectedVersion); err != nil {
			return err
		}
		return recordOperation(ctx, tx, opID, taskID, "samples", requestDigest, "sampled", requestDigest, expectedVersion+1)
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, taskID)
}

func insertSampleNode(ctx context.Context, tx *sql.Tx, n SampleNode) error {
	var mileage, ring, holeNo *int64
	var azimuth *string
	if n.HoleKey != nil {
		mileage = &n.HoleKey.MileageMM
		ring = &n.HoleKey.RingNo
		az := string(n.HoleKey.Azimuth)
		azimuth = &az
		holeNo = &n.HoleKey.HoleNo
	}
	var srcFailure string
	if n.SourceFailure != nil {
		srcFailure = fmt.Sprintf("%d:%d:%s:%d", n.SourceFailure.MileageMM,
			n.SourceFailure.RingNo, n.SourceFailure.Azimuth, n.SourceFailure.HoleNo)
	}
	closed := 0
	if n.Closed {
		closed = 1
	}
	verified := 0
	if n.Verified {
		verified = 1
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO sample_nodes
			(id, task_id, parent_id, layer_key, hole_mileage, hole_ring, hole_azimuth,
			 hole_no, category, pick_order, source_failure, influence_digest,
			 generation, closed, verified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.TaskID, n.ParentID, n.LayerKey, mileage, ring, azimuth, holeNo,
		string(n.Category), n.PickOrder, srcFailure, n.InfluenceDigest,
		n.Generation, closed, verified)
	return err
}

// ListSampleNodes returns all sample nodes of a task in canonical order: layer
// nodes first by id, leaves ordered by mileage, ring, azimuth ordinal, hole
// number and pick order.
func (s *Store) ListSampleNodes(ctx context.Context, taskID string) ([]SampleNode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, parent_id, layer_key, hole_mileage, hole_ring, hole_azimuth,
		       hole_no, category, pick_order, source_failure, influence_digest,
		       generation, closed, verified
		FROM sample_nodes WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SampleNode
	for rows.Next() {
		n, err := scanSampleNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func scanSampleNode(scan interface{ Scan(...any) error }) (SampleNode, error) {
	var (
		n             SampleNode
		mileage, ring sql.NullInt64
		azimuth       sql.NullString
		holeNo        sql.NullInt64
		srcFailure    string
		closed        int
		verified      int
	)
	err := scan.Scan(&n.ID, &n.TaskID, &n.ParentID, &n.LayerKey, &mileage, &ring,
		&azimuth, &holeNo, &n.Category, &n.PickOrder, &srcFailure,
		&n.InfluenceDigest, &n.Generation, &closed, &verified)
	if err != nil {
		return SampleNode{}, err
	}
	if mileage.Valid && ring.Valid && azimuth.Valid && holeNo.Valid {
		n.HoleKey = &domain.HoleKey{
			MileageMM: mileage.Int64,
			RingNo:    ring.Int64,
			Azimuth:   domain.Azimuth(azimuth.String),
			HoleNo:    holeNo.Int64,
		}
	}
	if sf := parseHoleRef(srcFailure); sf != nil {
		n.SourceFailure = sf
	}
	n.Closed = closed != 0
	n.Verified = verified != 0
	return n, nil
}

// VerifyHoles atomically marks a set of sample leaf nodes as verified. Any node
// not found, not a leaf, or already verified aborts the whole batch so no
// partial verification is committed. When all leaves are verified the task
// advances to loading.
func (s *Store) VerifyHoles(ctx context.Context, opID, taskID, requestDigest string, sampleIDs []string, expectedVersion int64) (*domain.InspectionTask, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		done, err := checkOperation(ctx, tx, opID, requestDigest)
		if err != nil {
			return err
		}
		if done {
			return ErrReplay
		}
		for _, id := range sampleIDs {
			res, err := tx.ExecContext(ctx, `
				UPDATE sample_nodes SET verified = 1
				WHERE id = ? AND task_id = ? AND hole_no IS NOT NULL AND verified = 0`,
				id, taskID)
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
		}
		return recordOperation(ctx, tx, opID, taskID, "verify", requestDigest, "verified", requestDigest, expectedVersion)
	})
	if err != nil {
		return nil, err
	}
	return s.GetTask(ctx, taskID)
}

func allLeavesVerified(ctx context.Context, tx *sql.Tx, taskID string) (bool, error) {
	var remaining int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sample_nodes
		WHERE task_id = ? AND hole_no IS NOT NULL AND verified = 0`, taskID).Scan(&remaining)
	if err != nil {
		return false, err
	}
	return remaining == 0, nil
}

func parseHoleRef(s string) *domain.HoleKey {
	if s == "" {
		return nil
	}
	parts := strings.SplitN(s, ":", 4)
	if len(parts) != 4 {
		return nil
	}
	var mk domain.HoleKey
	if _, err := fmt.Sscanf(parts[0], "%d", &mk.MileageMM); err != nil {
		return nil
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &mk.RingNo); err != nil {
		return nil
	}
	mk.Azimuth = domain.Azimuth(parts[2])
	if _, err := fmt.Sscanf(parts[3], "%d", &mk.HoleNo); err != nil {
		return nil
	}
	return &mk
}
