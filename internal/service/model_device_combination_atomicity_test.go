package service_test

import (
	"context"
	"errors"
	"testing"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/service"
	"rockbolt-pullout-zonal-closure/internal/store"
)

func TestModel_DeviceCombinationAtomicity(t *testing.T) {
	tests := []struct {
		name          string
		puller        string
		pump          string
		displacement  string
		wantErr       error
		wantStatus    domain.TaskStatus
		wantVersion   int64
		wantNewLeases int
	}{
		{
			name:          "one device cannot fill puller and pump roles",
			puller:        "rig-puller",
			pump:          "rig-puller",
			displacement:  "rig-displacement",
			wantErr:       service.ErrLeaseConflict,
			wantStatus:    domain.StatusHoleVerification,
			wantNewLeases: 0,
		},
		{
			name:          "three distinct devices commit at an adjacent half-open boundary",
			puller:        "rig-puller",
			pump:          "rig-pump",
			displacement:  "rig-displacement",
			wantStatus:    domain.StatusLoading,
			wantVersion:   1,
			wantNewLeases: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open("")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer st.Close()

			devices := []domain.Device{
				{Number: "rig-puller", Type: domain.DevicePuller},
				{Number: "rig-pump", Type: domain.DevicePump},
				{Number: "rig-displacement", Type: domain.DeviceDisplacement},
			}
			if err := st.EnsureDevices(ctx, devices); err != nil {
				t.Fatalf("seed devices: %v", err)
			}

			for _, taskID := range []string{"prior-test", "requested-test"} {
				if err := st.CreateTask(ctx, &domain.InspectionTask{
					ID: taskID, MileageStartMM: 1, MileageEndMM: 2, RockGrade: "III",
					CycleNo: 1, SamplingSeed: 1, Generation: 1,
					Status: domain.StatusHoleVerification,
				}); err != nil {
					t.Fatalf("create task %s: %v", taskID, err)
				}
			}

			priorLeases := []domain.DeviceLease{
				{ID: "prior-puller", DeviceNo: "rig-puller", TestID: "prior-test", Generation: 1, Start: 0, End: 10},
				{ID: "prior-pump", DeviceNo: "rig-pump", TestID: "prior-test", Generation: 1, Start: 0, End: 10},
				{ID: "prior-displacement", DeviceNo: "rig-displacement", TestID: "prior-test", Generation: 1, Start: 0, End: 10},
			}
			if _, err := st.AcquireLeases(ctx, "op-prior", "prior-test", domain.Digest(priorLeases), priorLeases, 0); err != nil {
				t.Fatalf("seed adjacent leases: %v", err)
			}

			svc := service.New(st)
			input := service.LeaseInput{
				TestID: "requested-test", TaskID: "requested-test", Generation: 1,
				Puller: tt.puller, Pump: tt.pump, Displacement: tt.displacement,
				Start: 10, End: 20,
			}
			_, acquireErr := svc.AcquireLeases(ctx, "op-requested", input)
			if tt.wantErr == nil && acquireErr != nil {
				t.Fatalf("acquire valid combination: %v", acquireErr)
			}
			if tt.wantErr != nil && !errors.Is(acquireErr, tt.wantErr) {
				t.Fatalf("acquire invalid combination error = %v, want %v", acquireErr, tt.wantErr)
			}

			var newLeases []domain.DeviceLease
			for _, device := range devices {
				leases, err := st.ActiveLeasesForDevice(ctx, device.Number)
				if err != nil {
					t.Fatalf("list leases for %s: %v", device.Number, err)
				}
				for _, lease := range leases {
					if lease.TestID == "requested-test" {
						newLeases = append(newLeases, lease)
					}
				}
			}
			if len(newLeases) != tt.wantNewLeases {
				t.Fatalf("requested leases persisted = %d, want %d", len(newLeases), tt.wantNewLeases)
			}

			task, err := st.GetTask(ctx, "requested-test")
			if err != nil {
				t.Fatalf("get requested task: %v", err)
			}
			if task.Status != tt.wantStatus || task.Version != tt.wantVersion {
				t.Fatalf("requested task = status %s version %d, want status %s version %d", task.Status, task.Version, tt.wantStatus, tt.wantVersion)
			}

			op, opErr := st.GetOperation(ctx, "op-requested")
			if tt.wantErr != nil {
				if !errors.Is(opErr, store.ErrNotFound) {
					t.Fatalf("failed request left operation record %+v: %v", op, opErr)
				}
				return
			}
			if opErr != nil {
				t.Fatalf("get committed operation: %v", opErr)
			}
			if op.TaskID != "requested-test" || op.Action != "acquire_leases" || op.CommitVersion != 1 {
				t.Fatalf("unexpected committed operation: %+v", op)
			}
			if _, err := st.AcquireLeases(ctx, "op-requested", "requested-test", domain.Digest(newLeases), newLeases, 1); !errors.Is(err, store.ErrReplay) {
				t.Fatalf("replay committed combination error = %v, want %v", err, store.ErrReplay)
			}
		})
	}
}
