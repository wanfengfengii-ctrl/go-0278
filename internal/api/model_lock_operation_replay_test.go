package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rockbolt-pullout-zonal-closure/internal/service"
	"rockbolt-pullout-zonal-closure/internal/store"
)

func TestModel_LockOperationReplayAfterRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "replay.db")
	post := func(handler http.Handler, path, body string) (int, map[string]any) {
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

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open initial store: %v", err)
	}
	handler := NewServer(service.New(st))
	status, created := post(handler, "/v1/tasks", `{
		"mileage_start_mm":12340000,
		"mileage_end_mm":12380000,
		"rock_grade":"III",
		"cycle_no":1,
		"sampling_seed":42
	}`)
	if status != http.StatusCreated || created["id"] != "t-1" {
		t.Fatalf("create task: status=%d body=%v", status, created)
	}

	initialRequest := `{
		"operation_id":"op-lock-retry",
		"layout_revision":"revision-7",
		"layout_digest":"layout-a",
		"grout_digest":"grout-a",
		"rock_grade":"III",
		"cycle_no":1,
		"layer_quotas":{"right_waist":0,"crown":0},
		"load_levels":[{"level":1,"target_load":50000,"hold_secs":2}],
		"stop_threshold":900000,
		"accept_threshold":500000,
		"influence_bound":"1",
		"calibration_digest":"calibration-a",
		"candidates":[]
	}`
	status, first := post(handler, "/v1/tasks/t-1/lock", initialRequest)
	if status != http.StatusOK || first["status"] != "pending_sample" {
		t.Fatalf("initial lock: status=%d body=%v", status, first)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close initial store: %v", err)
	}

	restartedStore, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = restartedStore.Close() })
	restarted := NewServer(service.New(restartedStore))

	cases := []struct {
		name       string
		request    string
		wantStatus int
		wantCode   string
		wantFirst  bool
	}{
		{
			name:       "same normalized request replays original result",
			request:    `{"candidates":[],"calibration_digest":"calibration-a","influence_bound":"1","accept_threshold":500000,"stop_threshold":900000,"load_levels":[{"hold_secs":2,"target_load":50000,"level":1}],"layer_quotas":{"crown":0,"right_waist":0},"cycle_no":1,"rock_grade":"III","grout_digest":"grout-a","layout_digest":"layout-a","layout_revision":"revision-7","operation_id":"op-lock-retry"}`,
			wantStatus: http.StatusOK,
			wantFirst:  true,
		},
		{
			name:       "same operation with different content conflicts",
			request:    `{"operation_id":"op-lock-retry","layout_revision":"revision-7","layout_digest":"layout-changed","grout_digest":"grout-a","rock_grade":"III","cycle_no":1,"layer_quotas":{"right_waist":0,"crown":0},"load_levels":[{"level":1,"target_load":50000,"hold_secs":2}],"stop_threshold":900000,"accept_threshold":500000,"influence_bound":"1","calibration_digest":"calibration-a","candidates":[]}`,
			wantStatus: http.StatusConflict,
			wantCode:   "conflict",
		},
		{
			name:       "new operation retains transition validation",
			request:    `{"operation_id":"op-genuinely-new","layout_revision":"revision-7","layout_digest":"layout-a","grout_digest":"grout-a","rock_grade":"III","cycle_no":1,"layer_quotas":{"right_waist":0,"crown":0},"load_levels":[{"level":1,"target_load":50000,"hold_secs":2}],"stop_threshold":900000,"accept_threshold":500000,"influence_bound":"1","calibration_digest":"calibration-a","candidates":[]}`,
			wantStatus: http.StatusConflict,
			wantCode:   "invalid_transition",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, gotBody := post(restarted, "/v1/tasks/t-1/lock", tc.request)
			if gotStatus != tc.wantStatus {
				t.Fatalf("status: want %d, got %d: %v", tc.wantStatus, gotStatus, gotBody)
			}
			if tc.wantFirst {
				if !reflect.DeepEqual(gotBody, first) {
					t.Fatalf("replay response differs from first response:\nfirst=%v\nreplay=%v", first, gotBody)
				}
				return
			}
			if gotBody["code"] != tc.wantCode {
				t.Fatalf("code: want %q, got %v (body=%v)", tc.wantCode, gotBody["code"], gotBody)
			}
		})
	}
}
