package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/service"
	"rockbolt-pullout-zonal-closure/internal/store"
)

func TestModel_RejectedReinforcementDoesNotBlockRetest(t *testing.T) {
	cases := []struct {
		name             string
		reuseExpansionOp bool
		wrongTaskVersion bool
	}{
		{name: "operation_id conflict", reuseExpansionOp: true},
		{name: "task version conflict", wrongTaskVersion: true},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open("")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer st.Close()

			taskID := fmt.Sprintf("reinforce-task-%d", i)
			initialStatus := domain.StatusExpanding
			if tc.reuseExpansionOp {
				initialStatus = domain.StatusLoading
			}
			if err := st.CreateTask(ctx, &domain.InspectionTask{
				ID: taskID, MileageStartMM: 1000, MileageEndMM: 2000,
				RockGrade: "III", CycleNo: 1, SamplingSeed: 42,
				Status: initialStatus, Generation: 1,
			}); err != nil {
				t.Fatalf("create task: %v", err)
			}

			rejectedOp := fmt.Sprintf("op-rejected-%d", i)
			if tc.reuseExpansionOp {
				if _, err := st.CommitExpansion(ctx, rejectedOp, taskID, "expansion-request",
					nil, "influence-1", domain.StatusLoading, 0); err != nil {
					t.Fatalf("commit expansion: %v", err)
				}
			}
			before, err := st.GetTask(ctx, taskID)
			if err != nil {
				t.Fatalf("get task before rejection: %v", err)
			}

			influenceDigest := "influence-1"
			reinforceDigest := "reinforcement-1"
			reason := "failed pullout point"
			requestDigest := domain.Digest([]string{influenceDigest, reinforceDigest, reason})
			expectedVersion := before.Version
			if tc.wrongTaskVersion {
				expectedVersion++
			}
			if _, err := st.SealReinforcement(ctx, rejectedOp, taskID, requestDigest,
				influenceDigest, reinforceDigest, reason, before.Generation,
				domain.LogicalTime(10), expectedVersion); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("rejected reinforcement: want ErrConflict, got %v", err)
			}

			var rows int
			if err := st.DB().QueryRowContext(ctx,
				"SELECT COUNT(*) FROM reinforcements WHERE task_id = ?", taskID).Scan(&rows); err != nil {
				t.Fatalf("count reinforcements after rejection: %v", err)
			}
			if rows != 0 {
				t.Fatalf("rejected reinforcement left %d row(s)", rows)
			}
			afterRejected, err := st.GetTask(ctx, taskID)
			if err != nil {
				t.Fatalf("get task after rejection: %v", err)
			}
			if afterRejected.Status != domain.StatusExpanding || afterRejected.Version != before.Version || afterRejected.Generation != before.Generation {
				t.Fatalf("rejection changed task: before=%+v after=%+v", before, afterRejected)
			}
			if tc.reuseExpansionOp {
				op, err := st.GetOperation(ctx, rejectedOp)
				if err != nil {
					t.Fatalf("get reused expansion operation: %v", err)
				}
				if op.Action != "expansion" || op.RequestDigest != "expansion-request" {
					t.Fatalf("conflict altered prior operation: %+v", op)
				}
			} else if _, err := st.GetOperation(ctx, rejectedOp); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("rejected operation was persisted: %v", err)
			}

			svc := service.New(st)
			validOp := fmt.Sprintf("op-valid-%d", i)
			sealed, err := svc.SealReinforcement(ctx, validOp, taskID, influenceDigest, reinforceDigest, reason)
			if err != nil {
				t.Fatalf("valid retry with fresh operation_id: %v", err)
			}
			if sealed.Status != domain.StatusPendingReinforce || sealed.Generation != before.Generation || sealed.Version != before.Version+1 {
				t.Fatalf("unexpected sealed task: %+v", sealed)
			}
			if err := st.DB().QueryRowContext(ctx,
				"SELECT COUNT(*) FROM reinforcements WHERE task_id = ?", taskID).Scan(&rows); err != nil {
				t.Fatalf("count reinforcements after retry: %v", err)
			}
			if rows != 1 {
				t.Fatalf("valid retry wrote %d reinforcement rows, want 1", rows)
			}
			validRecord, err := st.GetOperation(ctx, validOp)
			if err != nil {
				t.Fatalf("get valid reinforcement operation: %v", err)
			}
			if validRecord.Action != "reinforce" || validRecord.CommitVersion != sealed.Version {
				t.Fatalf("unexpected reinforcement operation: %+v", validRecord)
			}

			retested, err := svc.CreateRetest(ctx, fmt.Sprintf("op-retest-%d", i), taskID)
			if err != nil {
				t.Fatalf("create retest after valid seal: %v", err)
			}
			if retested.Status != domain.StatusRetesting || retested.Generation != before.Generation+1 {
				t.Fatalf("unexpected retest task: %+v", retested)
			}
		})
	}
}
