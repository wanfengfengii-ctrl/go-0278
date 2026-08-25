package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/service"
	"rockbolt-pullout-zonal-closure/internal/store"
)

func TestModel_AcquireLeasesValidatesDeviceCatalogAtomically(t *testing.T) {
	cases := []struct {
		name         string
		puller       string
		pump         string
		displacement string
		wantStatus   int
		wantCode     string
		wantLeases   int
		wantTask     domain.TaskStatus
		wantOp       bool
	}{
		{
			name: "unregistered puller", puller: "puller-missing", pump: "pump-1", displacement: "disp-1",
			wantStatus: http.StatusUnprocessableEntity, wantCode: "device_invalid", wantTask: domain.StatusHoleVerification,
		},
		{
			name: "unregistered pump", puller: "puller-1", pump: "pump-missing", displacement: "disp-1",
			wantStatus: http.StatusUnprocessableEntity, wantCode: "device_invalid", wantTask: domain.StatusHoleVerification,
		},
		{
			name: "unregistered displacement", puller: "puller-1", pump: "pump-1", displacement: "disp-missing",
			wantStatus: http.StatusUnprocessableEntity, wantCode: "device_invalid", wantTask: domain.StatusHoleVerification,
		},
		{
			name: "registered devices in wrong roles", puller: "pump-1", pump: "puller-1", displacement: "disp-1",
			wantStatus: http.StatusUnprocessableEntity, wantCode: "device_invalid", wantTask: domain.StatusHoleVerification,
		},
		{
			name: "registered devices in matching roles", puller: "puller-1", pump: "pump-1", displacement: "disp-1",
			wantStatus: http.StatusOK, wantLeases: 3, wantTask: domain.StatusLoading, wantOp: true,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open("")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			if err := st.EnsureDevices(ctx, []domain.Device{
				{Number: "puller-1", Type: domain.DevicePuller},
				{Number: "pump-1", Type: domain.DevicePump},
				{Number: "disp-1", Type: domain.DeviceDisplacement},
			}); err != nil {
				t.Fatalf("seed devices: %v", err)
			}

			svc := service.New(st)
			task, err := svc.CreateTask(ctx, domain.InspectionTask{
				MileageStartMM: 1000, MileageEndMM: 2000, RockGrade: "III", CycleNo: 1, SamplingSeed: 7,
			})
			if err != nil {
				t.Fatalf("create task: %v", err)
			}
			locked, err := svc.LockTask(ctx, fmt.Sprintf("lock-%d", i), task.ID, &domain.LockSpec{
				LayoutRevision: "r1", LayoutDigest: "layout", GroutDigest: "grout", RockGrade: "III", CycleNo: 1,
				LayerQuotas:   map[string]int{"crown": 1},
				LoadLevels:    []domain.LoadLevelDef{{Level: 1, TargetLoad: 100, HoldSecs: 1}},
				StopThreshold: 200, AcceptThreshold: 100, InfluenceBound: "0", CalibrationDigest: "calibration",
				Candidates: []domain.CandidateHole{{
					CycleNo: 1, HoleKey: domain.HoleKey{MileageMM: 1500, RingNo: 1, Azimuth: domain.AzimuthCrown, HoleNo: 1},
					BarBatch: "bar", AnchorBatch: "anchor",
				}},
			})
			if err != nil || len(locked.Violations) != 0 {
				t.Fatalf("lock task: err=%v violations=%v", err, locked.Violations)
			}
			sampled, err := svc.Sample(ctx, fmt.Sprintf("sample-%d", i), task.ID)
			if err != nil {
				t.Fatalf("sample task: %v", err)
			}
			var leafIDs []string
			for _, node := range sampled.Nodes {
				if node.HoleKey != nil {
					leafIDs = append(leafIDs, node.ID)
				}
			}
			if _, err := svc.VerifyHoles(ctx, fmt.Sprintf("verify-%d", i), task.ID, leafIDs); err != nil {
				t.Fatalf("verify holes: %v", err)
			}
			before, err := svc.GetTask(ctx, task.ID)
			if err != nil {
				t.Fatalf("get task before lease: %v", err)
			}

			opID := fmt.Sprintf("lease-%d", i)
			payload := fmt.Sprintf(
				`{"operation_id":%q,"test_id":%q,"task_id":%q,"generation":1,"puller":%q,"pump":%q,"displacement":%q,"start":10,"end":20}`,
				opID, task.ID, task.ID, tc.puller, tc.pump, tc.displacement)
			req := httptest.NewRequest(http.MethodPost, "/v1/tests/"+task.ID+"/leases", strings.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			NewServer(svc).ServeHTTP(resp, req)
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%v", resp.Code, tc.wantStatus, body)
			}
			if tc.wantCode != "" && body["code"] != tc.wantCode {
				t.Fatalf("error code = %v, want %q", body["code"], tc.wantCode)
			}

			var leaseCount int
			if err := st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM device_leases WHERE test_id = ?`, task.ID).Scan(&leaseCount); err != nil {
				t.Fatalf("count leases: %v", err)
			}
			if leaseCount != tc.wantLeases {
				t.Fatalf("lease count = %d, want %d", leaseCount, tc.wantLeases)
			}
			after, err := svc.GetTask(ctx, task.ID)
			if err != nil {
				t.Fatalf("get task after lease: %v", err)
			}
			if after.Status != tc.wantTask {
				t.Fatalf("task status = %q, want %q", after.Status, tc.wantTask)
			}
			if !tc.wantOp && after.Version != before.Version {
				t.Fatalf("failed acquisition changed version from %d to %d", before.Version, after.Version)
			}
			_, opErr := st.GetOperation(ctx, opID)
			if tc.wantOp && opErr != nil {
				t.Fatalf("successful acquisition operation missing: %v", opErr)
			}
			if !tc.wantOp && !errors.Is(opErr, store.ErrNotFound) {
				t.Fatalf("failed acquisition recorded an operation: %v", opErr)
			}
		})
	}
}
