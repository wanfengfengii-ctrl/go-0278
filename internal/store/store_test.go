package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

func testTask() *domain.InspectionTask {
	return &domain.InspectionTask{
		ID: "t-1", MileageStartMM: 12340000, MileageEndMM: 12380000,
		RockGrade: "III", CycleNo: 1, SamplingSeed: 42,
		Status: domain.StatusPendingLock, Generation: 1,
	}
}

func TestLockAndReopenPreservesDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	if err := st.CreateTask(ctx, testTask()); err != nil {
		t.Fatalf("create: %v", err)
	}
	spec := &domain.LockSpec{
		LayoutRevision: "rev-3", LayoutDigest: "ld", GroutDigest: "gd",
		RockGrade: "III", CycleNo: 1,
		LayerQuotas:   map[string]int{"crown": 1},
		LoadLevels:    []domain.LoadLevelDef{{Level: 1, TargetLoad: 50000, HoldSecs: 2}},
		StopThreshold: 900000, AcceptThreshold: 500000,
		InfluenceBound: "1", CalibrationDigest: "cd",
		Candidates: []domain.CandidateHole{
			{CycleNo: 1, HoleKey: domain.HoleKey{MileageMM: 12350000, RingNo: 1, Azimuth: domain.AzimuthCrown, HoleNo: 1}, BarBatch: "bar-A", AnchorBatch: "anc-X"},
		},
	}
	locked, err := st.LockTask(ctx, "op-1", "t-1", domain.Digest(spec), spec, domain.LogicalTime(5), 0)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	digestBefore := locked.LockDigest
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	got, err := reopened.GetTask(ctx, "t-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LockDigest != digestBefore {
		t.Fatalf("lock digest changed across restart: %q != %q", got.LockDigest, digestBefore)
	}
	if got.Status != domain.StatusPendingSample {
		t.Fatalf("want pending_sample after restart, got %s", got.Status)
	}
}

func TestLockRejectsOutOfRangeHole(t *testing.T) {
	st, err := Open("")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.CreateTask(ctx, testTask()); err != nil {
		t.Fatalf("create: %v", err)
	}
	// The service validates before the store; here we verify the store rejects a
	// candidate with a duplicate canonical key via the unique constraint.
	spec := &domain.LockSpec{
		LayoutRevision: "r", LayoutDigest: "ld", GroutDigest: "gd", RockGrade: "III",
		CycleNo: 1, LayerQuotas: map[string]int{"crown": 1},
		LoadLevels:    []domain.LoadLevelDef{{Level: 1, HoldSecs: 2}},
		StopThreshold: 900000, AcceptThreshold: 500000, InfluenceBound: "1",
		CalibrationDigest: "cd",
		Candidates: []domain.CandidateHole{
			{CycleNo: 1, HoleKey: domain.HoleKey{MileageMM: 12350000, RingNo: 1, Azimuth: domain.AzimuthCrown, HoleNo: 1}, BarBatch: "b", AnchorBatch: "a"},
			{CycleNo: 1, HoleKey: domain.HoleKey{MileageMM: 12350000, RingNo: 1, Azimuth: domain.AzimuthCrown, HoleNo: 1}, BarBatch: "b", AnchorBatch: "a"},
		},
	}
	if _, err := st.LockTask(ctx, "op-dup", "t-1", domain.Digest(spec), spec, domain.LogicalTime(5), 0); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("want ErrDuplicate for duplicate hole, got %v", err)
	}
	// No candidate residue should remain after the rolled-back transaction.
	holes, err := st.ListCandidateHoles(ctx, "t-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(holes) != 0 {
		t.Fatalf("want zero candidate residue, got %d", len(holes))
	}
}

func TestOperationReplayAndConflict(t *testing.T) {
	st, err := Open("")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.CreateTask(ctx, testTask()); err != nil {
		t.Fatalf("create: %v", err)
	}
	spec := &domain.LockSpec{
		LayoutRevision: "r", LayoutDigest: "ld", GroutDigest: "gd", RockGrade: "III",
		CycleNo: 1, LayerQuotas: map[string]int{"crown": 1},
		LoadLevels:    []domain.LoadLevelDef{{Level: 1, HoldSecs: 2}},
		StopThreshold: 900000, AcceptThreshold: 500000, InfluenceBound: "1",
		CalibrationDigest: "cd",
		Candidates: []domain.CandidateHole{
			{CycleNo: 1, HoleKey: domain.HoleKey{MileageMM: 12350000, RingNo: 1, Azimuth: domain.AzimuthCrown, HoleNo: 1}, BarBatch: "b", AnchorBatch: "a"},
		},
	}
	digest := domain.Digest(spec)
	if _, err := st.LockTask(ctx, "op-1", "t-1", digest, spec, domain.LogicalTime(5), 0); err != nil {
		t.Fatalf("lock: %v", err)
	}
	// Replaying the same operation with the same content returns ErrReplay.
	if _, err := st.LockTask(ctx, "op-1", "t-1", digest, spec, domain.LogicalTime(5), 1); !errors.Is(err, ErrReplay) {
		t.Fatalf("want ErrReplay, got %v", err)
	}
	// Same operation with different content returns ErrConflict.
	if _, err := st.LockTask(ctx, "op-1", "t-1", "different", spec, domain.LogicalTime(5), 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}
