package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

func TestModel_RetestGenerationLeaseIsolation(t *testing.T) {
	ctx := context.Background()
	svc, st := newTestService(t)
	if err := st.EnsureDevices(ctx, []domain.Device{
		{Number: "puller-2", Type: domain.DevicePuller},
		{Number: "pump-2", Type: domain.DevicePump},
		{Number: "disp-2", Type: domain.DeviceDisplacement},
	}); err != nil {
		t.Fatalf("seed alternate devices: %v", err)
	}

	taskID := lockSampleVerifyLease(t, svc)
	fullReadings(t, svc, taskID, "sample-1")
	_, influence, err := svc.Expand(ctx, "model-expand", taskID, "sample-1")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if _, err := svc.SealReinforcement(ctx, "model-reinforce", taskID, influence, "reinforce-v1", "repair"); err != nil {
		t.Fatalf("seal reinforcement: %v", err)
	}
	retestTask, err := svc.CreateRetest(ctx, "model-retest", taskID)
	if err != nil {
		t.Fatalf("create retest: %v", err)
	}
	if retestTask.Generation != 2 || retestTask.Status != domain.StatusRetesting {
		t.Fatalf("retest task = generation %d, status %s", retestTask.Generation, retestTask.Status)
	}

	baselineNodes, err := st.ListSampleNodes(ctx, taskID)
	if err != nil {
		t.Fatalf("list baseline samples: %v", err)
	}
	baselineEvidence, err := svc.GetEvidence(ctx, taskID)
	if err != nil {
		t.Fatalf("list baseline evidence: %v", err)
	}
	if len(baselineEvidence) != 5 {
		t.Fatalf("generation 1 evidence count = %d, want 5", len(baselineEvidence))
	}

	retestSampleID := "ret-2-sample-1"
	primaryLeaseID := "lease-" + taskID + "-puller-1-100000"
	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "old_generation_leases_cannot_feed_retest",
			run: func(t *testing.T) {
				_, err := svc.SubmitReading(ctx, "model-old-lease-reading", ReadingInput{
					TaskID: taskID, SampleID: retestSampleID, Stage: domain.StagePreload,
					Load: 1000, Displacement: 100,
					DevicePuller: "puller-1", DevicePump: "pump-1", DeviceDisplacement: "disp-1",
				})
				if !errors.Is(err, ErrLeaseConflict) {
					t.Fatalf("reading with only generation 1 leases: got %v, want ErrLeaseConflict", err)
				}
			},
		},
		{
			name: "wrong_generation_acquisition_rolls_back",
			run: func(t *testing.T) {
				_, err := svc.AcquireLeases(ctx, "model-wrong-generation", LeaseInput{
					TestID: taskID, TaskID: taskID, Generation: 1,
					Puller: "puller-2", Pump: "pump-2", Displacement: "disp-2",
					Start: 100000, End: 200000,
				})
				if !errors.Is(err, ErrLeaseConflict) {
					t.Fatalf("wrong generation acquisition: got %v, want ErrLeaseConflict", err)
				}
				for _, device := range []string{"puller-2", "pump-2", "disp-2"} {
					leases, listErr := svc.ActiveLeasesForDevice(ctx, device)
					if listErr != nil || len(leases) != 0 {
						t.Fatalf("%s leases after rollback: leases=%+v err=%v", device, leases, listErr)
					}
				}
			},
		},
		{
			name: "current_generation_acquires_complete_rig",
			run: func(t *testing.T) {
				svc.SetClock(func() domain.LogicalTime { return 100001 })
				got, err := svc.AcquireLeases(ctx, "model-generation-2-leases", LeaseInput{
					TestID: taskID, TaskID: taskID, Generation: 2,
					Puller: "puller-1", Pump: "pump-1", Displacement: "disp-1",
					Start: 100000, End: 200000,
				})
				if err != nil {
					t.Fatalf("acquire generation 2 rig: %v", err)
				}
				if got.Generation != 2 || got.Status != domain.StatusRetesting {
					t.Fatalf("task after acquisition = generation %d, status %s", got.Generation, got.Status)
				}
				for _, device := range []string{"puller-1", "pump-1", "disp-1"} {
					leases, listErr := svc.ActiveLeasesForDevice(ctx, device)
					if listErr != nil || len(leases) != 2 || leases[0].Generation != 1 || leases[1].Generation != 2 {
						t.Fatalf("%s generation-isolated leases: leases=%+v err=%v", device, leases, listErr)
					}
				}
			},
		},
		{
			name: "overlapping_bundle_is_atomic",
			run: func(t *testing.T) {
				_, err := svc.AcquireLeases(ctx, "model-overlap", LeaseInput{
					TestID: taskID, TaskID: taskID, Generation: 2,
					Puller: "puller-1", Pump: "pump-2", Displacement: "disp-2",
					Start: 100000, End: 200000,
				})
				if !IsConflict(err) {
					t.Fatalf("overlapping acquisition: got %v, want conflict", err)
				}
				for _, device := range []string{"pump-2", "disp-2"} {
					leases, listErr := svc.ActiveLeasesForDevice(ctx, device)
					if listErr != nil || len(leases) != 0 {
						t.Fatalf("partial %s lease survived: leases=%+v err=%v", device, leases, listErr)
					}
				}
			},
		},
		{
			name: "lease_version_conflict_rolls_back",
			run: func(t *testing.T) {
				_, err := svc.RenewLease(ctx, "model-stale-renew", taskID, RenewLeaseInput{
					LeaseID: primaryLeaseID, Holder: taskID, Generation: 2,
					NewEnd: 210000, ExpectedVersion: 99,
				})
				if !IsConflict(err) {
					t.Fatalf("stale renewal: got %v, want conflict", err)
				}
				lease, getErr := st.GetLease(ctx, primaryLeaseID)
				if getErr != nil || lease.End != 200000 || lease.Version != 0 {
					t.Fatalf("lease changed after stale renewal: lease=%+v err=%v", lease, getErr)
				}
			},
		},
		{
			name: "old_sample_tree_is_not_addressable",
			run: func(t *testing.T) {
				_, err := svc.SubmitReading(ctx, "model-old-sample", ReadingInput{
					TaskID: taskID, SampleID: "sample-1", Stage: domain.StagePreload,
					Load: 1000, Displacement: 100,
					DevicePuller: "puller-1", DevicePump: "pump-1", DeviceDisplacement: "disp-1",
				})
				if !errors.Is(err, ErrTaskNotFound) {
					t.Fatalf("old generation sample reading: got %v, want ErrTaskNotFound", err)
				}
			},
		},
		{
			name: "retest_reading_uses_generation_2_lease",
			run: func(t *testing.T) {
				res, err := svc.SubmitReading(ctx, "model-retest-reading", ReadingInput{
					TaskID: taskID, SampleID: retestSampleID, Stage: domain.StagePreload,
					Load: 1000, Displacement: 100,
					DevicePuller: "puller-1", DevicePump: "pump-1", DeviceDisplacement: "disp-1",
				})
				if err != nil || !res.Accepted {
					t.Fatalf("generation 2 reading: accepted=%v err=%v", res != nil && res.Accepted, err)
				}
				evidence, listErr := svc.GetEvidence(ctx, taskID)
				if listErr != nil || len(evidence) != len(baselineEvidence)+1 {
					t.Fatalf("evidence after retest reading: count=%d err=%v", len(evidence), listErr)
				}
				last := evidence[len(evidence)-1]
				if last.Generation != 2 || last.SampleID != retestSampleID || last.Kind != domain.EvidenceRetest {
					t.Fatalf("retest evidence not isolated: %+v", last)
				}
				if !reflect.DeepEqual(evidence[:len(baselineEvidence)], baselineEvidence) {
					t.Fatal("generation 1 evidence chain changed")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}

	finalNodes, err := st.ListSampleNodes(ctx, taskID)
	if err != nil {
		t.Fatalf("list final samples: %v", err)
	}
	if !reflect.DeepEqual(finalNodes, baselineNodes) {
		t.Fatal("lease/read conflicts forked or changed the sample tree")
	}
}
