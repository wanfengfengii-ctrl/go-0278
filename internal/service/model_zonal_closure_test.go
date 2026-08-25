package service

import (
	"context"
	"sort"
	"testing"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/instrument"
)

func TestModel_LastCurrentGenerationSampleAdvancesToPendingReview(t *testing.T) {
	cases := []struct {
		name          string
		failFirstCall bool
	}{
		{name: "accepted final reading"},
		{name: "accepted retry of final reading", failFirstCall: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, st := newTestService(t)
			t.Cleanup(func() { _ = st.Close() })
			taskID := lockSampleVerifyLease(t, svc)

			nodes, err := svc.GetSampleTree(ctx, taskID)
			if err != nil {
				t.Fatalf("get sample tree: %v", err)
			}
			var sampleIDs []string
			for _, node := range nodes {
				if node.HoleKey != nil && node.Generation == 1 {
					sampleIDs = append(sampleIDs, node.ID)
				}
			}
			sort.Strings(sampleIDs)
			if len(sampleIDs) != 3 {
				t.Fatalf("want three current-generation leaf samples, got %v", sampleIDs)
			}

			fullReadings(t, svc, taskID, sampleIDs[0])
			fullReadings(t, svc, taskID, sampleIDs[1])

			lastID := sampleIDs[2]
			prefix := []ReadingInput{
				{Stage: domain.StagePreload, Load: 1000, Displacement: 100},
				{Stage: domain.StageLoad, LoadLevel: 1, Load: 50000, Displacement: 900},
				{Stage: domain.StageHold, LoadLevel: 1, Load: 50000, Displacement: 950, HoldSecs: 2},
				{Stage: domain.StageUnload, Load: 0, Displacement: 400},
			}
			for i, in := range prefix {
				in.TaskID, in.SampleID = taskID, lastID
				in.DevicePuller, in.DevicePump, in.DeviceDisplacement = "puller-1", "pump-1", "disp-1"
				result, err := svc.SubmitReading(ctx, "op-final-prefix-"+string(rune('a'+i)), in)
				if err != nil || !result.Accepted {
					t.Fatalf("submit final-sample prefix step %d: accepted=%v err=%v", i, result != nil && result.Accepted, err)
				}
			}

			before, err := svc.GetTask(ctx, taskID)
			if err != nil {
				t.Fatalf("get task before final reading: %v", err)
			}
			if before.Status != domain.StatusLoading {
				t.Fatalf("want loading before final reading, got %s", before.Status)
			}
			evidenceBefore, err := svc.GetEvidence(ctx, taskID)
			if err != nil {
				t.Fatalf("get evidence before final reading: %v", err)
			}

			final := ReadingInput{
				TaskID: taskID, SampleID: lastID, Stage: domain.StageRebound,
				Displacement: 400, Rebound: 300,
				DevicePuller: "puller-1", DevicePump: "pump-1", DeviceDisplacement: "disp-1",
			}
			failedCallKey := callKey(taskID, lastID, before.Generation, 0, "puller-1", string(domain.StageRebound), 0)
			if tc.failFirstCall {
				svc.SetInstrument(instrument.NewScriptedAdapter(0, map[string]domain.CallResult{
					failedCallKey: domain.CallTimeout,
				}))
			}

			finalOpID := "op-final-accepted"
			result, err := svc.SubmitReading(ctx, finalOpID, final)
			if err != nil {
				t.Fatalf("submit final reading: %v", err)
			}
			if tc.failFirstCall {
				if result.Accepted {
					t.Fatal("timed-out instrument call must not be accepted")
				}
				unchanged, err := svc.GetTask(ctx, taskID)
				if err != nil {
					t.Fatalf("get task after timeout: %v", err)
				}
				if unchanged.Status != domain.StatusLoading || unchanged.Version != before.Version {
					t.Fatalf("timeout changed task: status=%s version=%d, want loading version=%d", unchanged.Status, unchanged.Version, before.Version)
				}
				gotEvidence, err := svc.GetEvidence(ctx, taskID)
				if err != nil {
					t.Fatalf("get evidence after timeout: %v", err)
				}
				if len(gotEvidence) != len(evidenceBefore) {
					t.Fatalf("timeout appended evidence: got %d records, want %d", len(gotEvidence), len(evidenceBefore))
				}
				finalOpID = "op-final-retry-accepted"
				result, err = svc.RetryCall(ctx, finalOpID, failedCallKey)
				if err != nil {
					t.Fatalf("retry final reading: %v", err)
				}
			}

			if !result.Accepted {
				t.Fatal("final legal reading was not accepted")
			}
			if result.Task.Status != domain.StatusPendingReview {
				t.Fatalf("final legal reading left task in %s, want pending_review", result.Task.Status)
			}
			if result.Task.Version != before.Version+1 {
				t.Fatalf("final legal reading version=%d, want %d", result.Task.Version, before.Version+1)
			}

			afterEvidence, err := svc.GetEvidence(ctx, taskID)
			if err != nil {
				t.Fatalf("get evidence after final reading: %v", err)
			}
			if len(afterEvidence) != len(evidenceBefore)+1 {
				t.Fatalf("final reading evidence count=%d, want %d", len(afterEvidence), len(evidenceBefore)+1)
			}
			nodes, err = svc.GetSampleTree(ctx, taskID)
			if err != nil {
				t.Fatalf("get closed sample tree: %v", err)
			}
			for _, node := range nodes {
				if node.HoleKey != nil && node.Generation == before.Generation && !node.Closed {
					t.Errorf("current-generation sample %s remains open", node.ID)
				}
			}

			op, err := st.GetOperation(ctx, finalOpID)
			if err != nil {
				t.Fatalf("get final-reading operation: %v", err)
			}
			if op.Action != "reading" || op.ResponseCode != "accepted" || op.CommitVersion != before.Version+1 {
				t.Fatalf("unexpected final-reading operation: %+v", op)
			}
		})
	}
}
