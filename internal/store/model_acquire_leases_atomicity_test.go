package store_test

import (
	"context"
	"errors"
	"testing"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/store"
)

func TestModel_AcquireLeasesAtomicCombination(t *testing.T) {
	cases := []struct {
		name        string
		leases      []domain.DeviceLease
		wantSuccess bool
	}{
		{
			name: "duplicate device rolls back the entire combination",
			leases: []domain.DeviceLease{
				{ID: "lease-test-puller-10", DeviceNo: "puller-1", TestID: "test-1", Generation: 1, Start: 10, End: 20},
				{ID: "lease-test-puller-10", DeviceNo: "puller-1", TestID: "test-1", Generation: 1, Start: 10, End: 20},
				{ID: "lease-test-disp-10", DeviceNo: "disp-1", TestID: "test-1", Generation: 1, Start: 10, End: 20},
			},
		},
		{
			name: "three distinct devices commit together",
			leases: []domain.DeviceLease{
				{ID: "lease-test-puller-10", DeviceNo: "puller-1", TestID: "test-1", Generation: 1, Start: 10, End: 20},
				{ID: "lease-test-pump-10", DeviceNo: "pump-1", TestID: "test-1", Generation: 1, Start: 10, End: 20},
				{ID: "lease-test-disp-10", DeviceNo: "disp-1", TestID: "test-1", Generation: 1, Start: 10, End: 20},
			},
			wantSuccess: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open("")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer st.Close()

			if err := st.CreateTask(ctx, &domain.InspectionTask{
				ID: "task-1", MileageStartMM: 1, MileageEndMM: 2, RockGrade: "III",
				CycleNo: 1, SamplingSeed: 1, Generation: 1, Status: domain.StatusHoleVerification,
			}); err != nil {
				t.Fatalf("create task: %v", err)
			}
			if err := st.EnsureDevices(ctx, []domain.Device{
				{Number: "puller-1", Type: domain.DevicePuller},
				{Number: "pump-1", Type: domain.DevicePump},
				{Number: "disp-1", Type: domain.DeviceDisplacement},
			}); err != nil {
				t.Fatalf("seed devices: %v", err)
			}

			opID := "op-acquire"
			digest := domain.Digest(tc.leases)
			_, acquireErr := st.AcquireLeases(ctx, opID, "task-1", digest, tc.leases, 0)
			if tc.wantSuccess && acquireErr != nil {
				t.Fatalf("acquire distinct combination: %v", acquireErr)
			}
			if !tc.wantSuccess && acquireErr == nil {
				t.Fatal("invalid combination unexpectedly succeeded")
			}

			wantLeaseCount := 0
			wantStatus := domain.StatusHoleVerification
			var wantVersion int64
			if tc.wantSuccess {
				wantLeaseCount = 3
				wantStatus = domain.StatusLoading
				wantVersion = 1
			}
			leaseCount := 0
			for _, deviceNo := range []string{"puller-1", "pump-1", "disp-1"} {
				leases, err := st.ActiveLeasesForDevice(ctx, deviceNo)
				if err != nil {
					t.Fatalf("list leases for %s: %v", deviceNo, err)
				}
				leaseCount += len(leases)
			}
			if leaseCount != wantLeaseCount {
				t.Fatalf("active lease count = %d, want %d", leaseCount, wantLeaseCount)
			}

			task, err := st.GetTask(ctx, "task-1")
			if err != nil {
				t.Fatalf("get task: %v", err)
			}
			if task.Status != wantStatus || task.Version != wantVersion {
				t.Fatalf("task after acquisition = status %s version %d, want status %s version %d", task.Status, task.Version, wantStatus, wantVersion)
			}

			op, opErr := st.GetOperation(ctx, opID)
			if tc.wantSuccess {
				if opErr != nil {
					t.Fatalf("get committed operation: %v", opErr)
				}
				if op.TaskID != "task-1" || op.Action != "acquire_leases" || op.CommitVersion != 1 {
					t.Fatalf("unexpected operation record: %+v", op)
				}
			} else if !errors.Is(opErr, store.ErrNotFound) {
				t.Fatalf("failed acquisition left an operation record: op=%+v err=%v", op, opErr)
			}
		})
	}
}
