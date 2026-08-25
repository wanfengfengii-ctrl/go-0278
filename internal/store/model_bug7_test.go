package store

import (
	"context"
	"reflect"
	"testing"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

func TestModel_RetryRecoverySuppressesSupersededCalls(t *testing.T) {
	ctx := context.Background()
	st, err := Open("")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	task := &domain.InspectionTask{
		ID: "retry-recovery", MileageStartMM: 1, MileageEndMM: 2,
		RockGrade: "III", CycleNo: 1, SamplingSeed: 7,
		Status: domain.StatusLoading, Generation: 1,
	}
	if err := st.CreateTask(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	devices := [3]string{"puller-1", "pump-1", "disp-1"}
	completedTimeout := domain.InstrumentCall{
		CallKey: "completed-0", TaskID: task.ID, SampleID: "sample-1",
		Generation: 1, LoadLevel: 0, DeviceNo: devices[0], Seq: 0,
		Result: domain.CallTimeout, RetryCount: 0, NextRetryAt: domain.NextRetryAt(0, 0),
		RequestDigest: "completed-reading", Stage: domain.StagePreload,
		Load: 1000, Displacement: 100, DeviceSet: devices,
	}
	completedRetry := completedTimeout
	completedRetry.CallKey = "completed-1"
	completedRetry.Seq = 1
	completedRetry.Result = domain.CallAccepted
	completedRetry.RetryCount = 1
	completedRetry.NextRetryAt = 0

	pendingTimeout := completedTimeout
	pendingTimeout.CallKey = "pending-0"
	pendingTimeout.SampleID = "sample-2"
	pendingTimeout.RequestDigest = "pending-reading"
	pendingRetry := pendingTimeout
	pendingRetry.CallKey = "pending-1"
	pendingRetry.Seq = 1
	pendingRetry.Result = domain.CallDisconnected
	pendingRetry.RetryCount = 1
	pendingRetry.NextRetryAt = domain.NextRetryAt(1, 0)

	commits := []struct {
		opID     string
		call     domain.InstrumentCall
		evidence *domain.LoadEvidence
		version  int64
	}{
		{opID: "op-completed-timeout", call: completedTimeout, version: 0},
		{
			opID: "op-completed-retry", call: completedRetry, version: 0,
			evidence: &domain.LoadEvidence{
				TaskID: task.ID, SampleID: "sample-1", Generation: 1,
				Kind: domain.EvidenceOriginal, Stage: domain.StagePreload,
				Load: 1000, Displacement: 100, DeviceSet: devices,
				Accepted: true, ContentDigest: "completed-reading",
			},
		},
		{opID: "op-pending-timeout", call: pendingTimeout, version: 1},
		{opID: "op-pending-retry", call: pendingRetry, version: 1},
	}
	for _, commit := range commits {
		if _, err := st.CommitReading(ctx, commit.opID, task.ID, commit.call.RequestDigest, commit.call, commit.evidence, "", commit.version); err != nil {
			t.Fatalf("commit %s: %v", commit.opID, err)
		}
	}

	var firstPending []domain.InstrumentCall
	cases := []struct {
		name  string
		check func(*testing.T)
	}{
		{
			name: "successful retry removes covered timeout and appends evidence once",
			check: func(t *testing.T) {
				pending, err := st.ListPendingInstrumentCalls(ctx)
				if err != nil {
					t.Fatalf("list pending: %v", err)
				}
				firstPending = pending
				for _, call := range pending {
					if call.CallKey == completedTimeout.CallKey {
						t.Fatalf("covered timeout %q remained pending", call.CallKey)
					}
				}
				evidence, err := st.ListEvidence(ctx, task.ID)
				if err != nil {
					t.Fatalf("list evidence: %v", err)
				}
				if len(evidence) != 1 || evidence[0].ContentDigest != "completed-reading" {
					t.Fatalf("want one successful retry evidence record, got %+v", evidence)
				}
			},
		},
		{
			name: "latest unsuccessful retry remains at fixed backoff",
			check: func(t *testing.T) {
				pending, err := st.ListPendingInstrumentCalls(ctx)
				if err != nil {
					t.Fatalf("list pending: %v", err)
				}
				if len(pending) != 1 {
					t.Fatalf("want only latest unresolved call, got %+v", pending)
				}
				got := pending[0]
				if got.CallKey != pendingRetry.CallKey || got.RetryCount != 1 || got.NextRetryAt != domain.NextRetryAt(1, 0) {
					t.Fatalf("unexpected unresolved retry: %+v", got)
				}
			},
		},
		{
			name: "repeated recovery is stable and does not reschedule covered calls",
			check: func(t *testing.T) {
				again, err := st.ListPendingInstrumentCalls(ctx)
				if err != nil {
					t.Fatalf("list pending again: %v", err)
				}
				if !reflect.DeepEqual(again, firstPending) {
					t.Fatalf("recovery queue changed: first=%+v again=%+v", firstPending, again)
				}
				evidence, err := st.ListEvidence(ctx, task.ID)
				if err != nil {
					t.Fatalf("list evidence again: %v", err)
				}
				if len(evidence) != 1 {
					t.Fatalf("repeated recovery changed evidence count to %d", len(evidence))
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.check)
	}
}
