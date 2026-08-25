package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/service"
	"rockbolt-pullout-zonal-closure/internal/store"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open("")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	svc := service.New(st)
	if err := st.EnsureDevices(context.Background(), []domain.Device{
		{Number: "puller-1", Type: domain.DevicePuller},
		{Number: "pump-1", Type: domain.DevicePump},
		{Number: "disp-1", Type: domain.DeviceDisplacement},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := httptest.NewServer(NewServer(svc))
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, url, body string) (*http.Response, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp, m
}

func TestCreateAndGetTask(t *testing.T) {
	srv := newTestServer(t)
	resp, body := postJSON(t, srv.URL+"/v1/tasks",
		`{"mileage_start_mm":12340000,"mileage_end_mm":12380000,"rock_grade":"III","cycle_no":1,"sampling_seed":42}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want 201, got %d", resp.StatusCode)
	}
	if body["id"] != "t-1" {
		t.Fatalf("want id t-1, got %v", body["id"])
	}
	got, err := http.Get(srv.URL + "/v1/tasks/t-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer got.Body.Close()
	if got.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", got.StatusCode)
	}
}

func TestLockRejectsInvalidMileage(t *testing.T) {
	srv := newTestServer(t)
	if _, body := postJSON(t, srv.URL+"/v1/tasks",
		`{"mileage_start_mm":12340000,"mileage_end_mm":12380000,"rock_grade":"III","cycle_no":1,"sampling_seed":42}`); body["id"] != "t-1" {
		t.Fatalf("create failed: %v", body)
	}
	resp, body := postJSON(t, srv.URL+"/v1/tasks/t-1/lock",
		`{"operation_id":"op-lock","layout_revision":"r","layout_digest":"ld","grout_digest":"gd","rock_grade":"III","cycle_no":1,"layer_quotas":{"crown":1},"load_levels":[{"level":1,"target_load":50000,"hold_secs":2}],"stop_threshold":900000,"accept_threshold":500000,"influence_bound":"1","calibration_digest":"cd","candidates":[{"mileage_mm":99999999,"ring_no":1,"azimuth":"crown","hole_no":1,"bar_batch":"b","anchor_batch":"a","cycle_no":1}]}`)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %v", resp.StatusCode, body)
	}
	if body["code"] != "lock_rejected" {
		t.Fatalf("want code lock_rejected, got %v", body["code"])
	}
}
