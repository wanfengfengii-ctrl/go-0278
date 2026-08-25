package service

import (
	"context"
	"errors"
	"testing"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/store"
)

func newTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open("")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	svc := New(st)
	if err := st.EnsureDevices(context.Background(), []domain.Device{
		{Number: "puller-1", Type: domain.DevicePuller},
		{Number: "pump-1", Type: domain.DevicePump},
		{Number: "disp-1", Type: domain.DeviceDisplacement},
	}); err != nil {
		t.Fatalf("seed devices: %v", err)
	}
	var t0 int64
	svc.SetClock(func() domain.LogicalTime { t0++; return domain.LogicalTime(t0) })
	return svc, st
}

// lockSampleVerifyLease drives a task through lock, sample, verify and lease so
// a test can focus on readings, expansion or finalization.
func lockSampleVerifyLease(t *testing.T, svc *Service) string {
	t.Helper()
	ctx := context.Background()
	task, err := svc.CreateTask(ctx, domain.InspectionTask{
		MileageStartMM: 12340000, MileageEndMM: 12380000,
		RockGrade: "III", CycleNo: 1, SamplingSeed: 42,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	spec := &domain.LockSpec{
		LayoutRevision: "rev-3", LayoutDigest: "ld", GroutDigest: "gd",
		RockGrade: "III", CycleNo: 1,
		LayerQuotas:   map[string]int{"crown": 2, "left_waist": 1},
		LoadLevels:    []domain.LoadLevelDef{{Level: 1, TargetLoad: 50000, HoldSecs: 2}},
		StopThreshold: 900000, AcceptThreshold: 500000,
		InfluenceBound: "1", CalibrationDigest: "cd",
		Candidates: []domain.CandidateHole{
			{CycleNo: 1, HoleKey: domain.HoleKey{MileageMM: 12350000, RingNo: 1, Azimuth: domain.AzimuthCrown, HoleNo: 1}, BarBatch: "bar-A", AnchorBatch: "anc-X"},
			{CycleNo: 1, HoleKey: domain.HoleKey{MileageMM: 12350000, RingNo: 1, Azimuth: domain.AzimuthCrown, HoleNo: 2}, BarBatch: "bar-A", AnchorBatch: "anc-X"},
			{CycleNo: 1, HoleKey: domain.HoleKey{MileageMM: 12350000, RingNo: 1, Azimuth: domain.AzimuthLeftWaist, HoleNo: 1}, BarBatch: "bar-B", AnchorBatch: "anc-Y"},
			{CycleNo: 1, HoleKey: domain.HoleKey{MileageMM: 12350000, RingNo: 2, Azimuth: domain.AzimuthCrown, HoleNo: 1}, BarBatch: "bar-A", AnchorBatch: "anc-Z"},
		},
	}
	res, err := svc.LockTask(ctx, "op-lock", task.ID, spec)
	if err != nil || len(res.Violations) > 0 {
		t.Fatalf("lock: err=%v violations=%+v", err, res.Violations)
	}
	sampleRes, err := svc.Sample(ctx, "op-sample", task.ID)
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	var leafIDs []string
	for _, n := range sampleRes.Nodes {
		if n.HoleKey != nil {
			leafIDs = append(leafIDs, n.ID)
		}
	}
	if _, err := svc.VerifyHoles(ctx, "op-verify", task.ID, leafIDs); err != nil {
		t.Fatalf("verify: %v", err)
	}
	_, err = svc.AcquireLeases(ctx, "op-lease", LeaseInput{
		TestID: task.ID, TaskID: task.ID, Generation: 1,
		Puller: "puller-1", Pump: "pump-1", Displacement: "disp-1",
		Start: 1, End: 100000,
	})
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	return task.ID
}

func fullReadings(t *testing.T, svc *Service, taskID, sampleID string) {
	t.Helper()
	ctx := context.Background()
	steps := []ReadingInput{
		{SampleID: sampleID, Stage: domain.StagePreload, Load: 1000, Displacement: 100},
		{SampleID: sampleID, Stage: domain.StageLoad, LoadLevel: 1, Load: 50000, Displacement: 900},
		{SampleID: sampleID, Stage: domain.StageHold, LoadLevel: 1, Load: 50000, Displacement: 950, HoldSecs: 2},
		{SampleID: sampleID, Stage: domain.StageUnload, Load: 0, Displacement: 400},
		{SampleID: sampleID, Stage: domain.StageRebound, Displacement: 400, Rebound: 300},
	}
	for i, in := range steps {
		in.TaskID = taskID
		in.DevicePuller, in.DevicePump, in.DeviceDisplacement = "puller-1", "pump-1", "disp-1"
		res, err := svc.SubmitReading(ctx, "op-read-"+sampleID+"-"+string(rune('a'+i)), in)
		if err != nil {
			t.Fatalf("reading %d: err=%v", i, err)
		}
		if !res.Accepted {
			t.Fatalf("reading %d not accepted", i)
		}
	}
}

func TestFullPulloutFlow(t *testing.T) {
	svc, _ := newTestService(t)
	taskID := lockSampleVerifyLease(t, svc)
	sampleID := "sample-1"
	fullReadings(t, svc, taskID, sampleID)

	ev, err := svc.GetEvidence(context.Background(), taskID)
	if err != nil {
		t.Fatalf("evidence: %v", err)
	}
	if len(ev) != 5 {
		t.Fatalf("want 5 evidence records, got %d", len(ev))
	}
	// The final rebound evidence must carry a non-zero judgment ratio.
	if ev[4].Ratio == 0 {
		t.Fatalf("rebound ratio should be non-zero, got %d", ev[4].Ratio)
	}
}

func TestReadingOutOfOrderRejected(t *testing.T) {
	svc, _ := newTestService(t)
	taskID := lockSampleVerifyLease(t, svc)
	ctx := context.Background()
	// Jump straight to load level 1 (skipping preload).
	_, err := svc.SubmitReading(ctx, "op-bad", ReadingInput{
		TaskID: taskID, SampleID: "sample-1", Stage: domain.StageLoad, LoadLevel: 1,
		Load: 50000, Displacement: 100,
		DevicePuller: "puller-1", DevicePump: "pump-1", DeviceDisplacement: "disp-1",
	})
	if !errors.Is(err, ErrStageOutOfOrder) {
		t.Fatalf("want ErrStageOutOfOrder, got %v", err)
	}
	ev, _ := svc.GetEvidence(ctx, taskID)
	if len(ev) != 0 {
		t.Fatalf("no evidence should be written, got %d", len(ev))
	}
}

func TestReadingHoldTooShortRejected(t *testing.T) {
	svc, _ := newTestService(t)
	taskID := lockSampleVerifyLease(t, svc)
	ctx := context.Background()
	// Valid preload then a too-short hold is skipped by ordering; do preload first.
	for i, in := range []ReadingInput{
		{SampleID: "sample-1", Stage: domain.StagePreload, Load: 1000, Displacement: 100},
		{SampleID: "sample-1", Stage: domain.StageLoad, LoadLevel: 1, Load: 50000, Displacement: 900},
		{SampleID: "sample-1", Stage: domain.StageHold, LoadLevel: 1, Load: 50000, Displacement: 950, HoldSecs: 1},
	} {
		in.TaskID = taskID
		in.DevicePuller, in.DevicePump, in.DeviceDisplacement = "puller-1", "pump-1", "disp-1"
		res, err := svc.SubmitReading(ctx, "op-"+string(rune('a'+i)), in)
		if i < 2 {
			if err != nil || !res.Accepted {
				t.Fatalf("step %d should be accepted: err=%v", i, err)
			}
		} else if !errors.Is(err, ErrHoldTooShort) {
			t.Fatalf("want ErrHoldTooShort, got %v", err)
		}
	}
}

func TestExpansionComputesInfluenceDomain(t *testing.T) {
	svc, _ := newTestService(t)
	taskID := lockSampleVerifyLease(t, svc)
	// Fail sample-1 by reading an out-of-range rebound, then expand.
	fullReadings(t, svc, taskID, "sample-1")
	_, influence, err := svc.Expand(context.Background(), "op-expand", taskID, "sample-1")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if influence == "" {
		t.Fatal("influence digest should be non-empty for complete boundary data")
	}
	task, _ := svc.GetTask(context.Background(), taskID)
	if task.Status != domain.StatusExpanding {
		t.Fatalf("want expanding status, got %s", task.Status)
	}
}
