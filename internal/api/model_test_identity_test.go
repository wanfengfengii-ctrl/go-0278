package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/service"
	"rockbolt-pullout-zonal-closure/internal/store"
)

func TestModel_TestIdentityOwnsLeasesAndReadings(t *testing.T) {
	tests := []struct {
		name              string
		leaseGeneration   int
		readingTestID     string
		readingPuller     string
		readingTime       domain.LogicalTime
		wantLeaseStatus   int
		wantReadingStatus int
		wantCode          string
	}{
		{
			name:              "distinct test id owns lease for reading",
			leaseGeneration:   1,
			readingTestID:     "field-test-77",
			readingPuller:     "puller-1",
			readingTime:       10,
			wantLeaseStatus:   http.StatusOK,
			wantReadingStatus: http.StatusOK,
		},
		{
			name:              "different public test id is not the holder",
			leaseGeneration:   1,
			readingTestID:     "field-test-78",
			readingPuller:     "puller-1",
			readingTime:       10,
			wantLeaseStatus:   http.StatusOK,
			wantReadingStatus: http.StatusConflict,
			wantCode:          "lease_conflict",
		},
		{
			name:              "unleased device is rejected",
			leaseGeneration:   1,
			readingTestID:     "field-test-77",
			readingPuller:     "puller-2",
			readingTime:       10,
			wantLeaseStatus:   http.StatusOK,
			wantReadingStatus: http.StatusConflict,
			wantCode:          "lease_conflict",
		},
		{
			name:              "expired lease is rejected",
			leaseGeneration:   1,
			readingTestID:     "field-test-77",
			readingPuller:     "puller-1",
			readingTime:       100,
			wantLeaseStatus:   http.StatusOK,
			wantReadingStatus: http.StatusConflict,
			wantCode:          "lease_conflict",
		},
		{
			name:            "wrong generation cannot acquire",
			leaseGeneration: 2,
			readingTestID:   "field-test-77",
			readingPuller:   "puller-1",
			readingTime:     10,
			wantLeaseStatus: http.StatusConflict,
			wantCode:        "lease_conflict",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			st, err := store.Open("")
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			if err := st.EnsureDevices(ctx, []domain.Device{
				{Number: "puller-1", Type: domain.DevicePuller},
				{Number: "puller-2", Type: domain.DevicePuller},
				{Number: "pump-1", Type: domain.DevicePump},
				{Number: "disp-1", Type: domain.DeviceDisplacement},
			}); err != nil {
				t.Fatalf("seed devices: %v", err)
			}
			svc := service.New(st)
			now := domain.LogicalTime(10)
			svc.SetClock(func() domain.LogicalTime { return now })

			task, err := svc.CreateTask(ctx, domain.InspectionTask{
				MileageStartMM: 12340000,
				MileageEndMM:   12380000,
				RockGrade:      "III",
				CycleNo:        1,
				SamplingSeed:   42,
			})
			if err != nil {
				t.Fatalf("create task: %v", err)
			}
			spec := &domain.LockSpec{
				LayoutRevision: "rev-identity", LayoutDigest: "ld", GroutDigest: "gd",
				RockGrade: "III", CycleNo: 1,
				LayerQuotas:   map[string]int{"crown": 1},
				LoadLevels:    []domain.LoadLevelDef{{Level: 1, TargetLoad: 50000, HoldSecs: 2}},
				StopThreshold: 900000, AcceptThreshold: 500000,
				InfluenceBound: "1", CalibrationDigest: "cd",
				Candidates: []domain.CandidateHole{{
					CycleNo:  1,
					HoleKey:  domain.HoleKey{MileageMM: 12350000, RingNo: 1, Azimuth: domain.AzimuthCrown, HoleNo: 1},
					BarBatch: "bar-A", AnchorBatch: "anc-A",
				}},
			}
			locked, err := svc.LockTask(ctx, "op-lock", task.ID, spec)
			if err != nil || len(locked.Violations) != 0 {
				t.Fatalf("lock task: err=%v violations=%v", err, locked.Violations)
			}
			sampled, err := svc.Sample(ctx, "op-sample", task.ID)
			if err != nil {
				t.Fatalf("sample: %v", err)
			}
			var sampleIDs []string
			for _, node := range sampled.Nodes {
				if node.HoleKey != nil {
					sampleIDs = append(sampleIDs, node.ID)
				}
			}
			if len(sampleIDs) == 0 {
				t.Fatal("sampling produced no leaf samples")
			}
			if _, err := svc.VerifyHoles(ctx, "op-verify", task.ID, sampleIDs); err != nil {
				t.Fatalf("verify samples: %v", err)
			}

			handler := NewServer(svc)
			post := func(path, body string) (int, map[string]any) {
				t.Helper()
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp := httptest.NewRecorder()
				handler.ServeHTTP(resp, req)
				var decoded map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
					t.Fatalf("decode POST %s response: %v", path, err)
				}
				return resp.Code, decoded
			}

			leaseBody := fmt.Sprintf(`{"operation_id":"op-lease","task_id":%q,"generation":%d,"puller":"puller-1","pump":"pump-1","displacement":"disp-1","start":1,"end":100}`,
				task.ID, tc.leaseGeneration)
			leaseStatus, leaseResponse := post("/v1/tests/field-test-77/leases", leaseBody)
			if leaseStatus != tc.wantLeaseStatus {
				t.Fatalf("lease status = %d, want %d; response=%v", leaseStatus, tc.wantLeaseStatus, leaseResponse)
			}
			if tc.wantLeaseStatus != http.StatusOK {
				if leaseResponse["code"] != tc.wantCode {
					t.Fatalf("lease code = %v, want %q", leaseResponse["code"], tc.wantCode)
				}
				return
			}

			now = tc.readingTime
			readingBody := fmt.Sprintf(`{"operation_id":"op-reading","task_id":%q,"sample_id":%q,"stage":"preload","load":1000,"displacement":100,"device_puller":%q,"device_pump":"pump-1","device_displacement":"disp-1"}`,
				task.ID, sampleIDs[0], tc.readingPuller)
			readingStatus, readingResponse := post("/v1/tests/"+tc.readingTestID+"/readings", readingBody)
			if readingStatus != tc.wantReadingStatus {
				t.Fatalf("reading status = %d, want %d; response=%v", readingStatus, tc.wantReadingStatus, readingResponse)
			}
			if tc.wantCode != "" && readingResponse["code"] != tc.wantCode {
				t.Fatalf("reading code = %v, want %q", readingResponse["code"], tc.wantCode)
			}
			if tc.wantReadingStatus == http.StatusOK && readingResponse["accepted"] != true {
				t.Fatalf("reading was not accepted: %v", readingResponse)
			}
		})
	}
}
