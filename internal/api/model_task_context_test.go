package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rockbolt-pullout-zonal-closure/internal/service"
	"rockbolt-pullout-zonal-closure/internal/store"
)

func TestModel_CreateTaskHonorsRequestContext(t *testing.T) {
	const body = `{"mileage_start_mm":12340000,"mileage_end_mm":12380000,"rock_grade":"III","cycle_no":1,"sampling_seed":42}`

	tests := []struct {
		name string
		mode string
	}{
		{name: "cancelled before transaction", mode: "pre-cancelled"},
		{name: "cancelled while insert waits for writer", mode: "blocked-insert"},
		{name: "ordinary create is still visible by its established id", mode: "ordinary"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "tasks.db"))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer st.Close()
			handler := NewServer(service.New(st))

			var blockerCleanup func()
			if tc.mode == "blocked-insert" {
				st.DB().SetMaxOpenConns(2)
				conn, err := st.DB().Conn(context.Background())
				if err != nil {
					t.Fatalf("reserve blocker connection: %v", err)
				}
				if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
					conn.Close()
					t.Fatalf("hold sqlite writer lock: %v", err)
				}
				blockerCleanup = func() {
					_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
					_ = conn.Close()
				}
				defer func() {
					if blockerCleanup != nil {
						blockerCleanup()
					}
				}()
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest(http.MethodPost, "/v1/tasks", strings.NewReader(body)).WithContext(ctx)
			rec := httptest.NewRecorder()

			switch tc.mode {
			case "pre-cancelled":
				cancel()
				handler.ServeHTTP(rec, req)
			case "blocked-insert":
				done := make(chan struct{})
				go func() {
					handler.ServeHTTP(rec, req)
					close(done)
				}()

				deadline := time.Now().Add(2 * time.Second)
				for st.DB().Stats().InUse < 2 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if st.DB().Stats().InUse < 2 {
					t.Fatal("create request did not reach the blocked INSERT")
				}
				cancel()

				select {
				case <-done:
				case <-time.After(2 * time.Second):
					// Release the writer so an implementation that discarded the
					// request context can finish and expose its leaked row.
					blockerCleanup()
					blockerCleanup = nil
					select {
					case <-done:
					case <-time.After(2 * time.Second):
						t.Fatal("cancelled create request did not return")
					}
				}
				if blockerCleanup != nil {
					blockerCleanup()
					blockerCleanup = nil
				}
			case "ordinary":
				handler.ServeHTTP(rec, req)
			default:
				t.Fatalf("unknown test mode %q", tc.mode)
			}

			if tc.mode == "ordinary" {
				if rec.Code != http.StatusCreated {
					t.Fatalf("POST /v1/tasks status = %d, want 201; body=%s", rec.Code, rec.Body.String())
				}
				var created struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
					t.Fatalf("decode create response: %v", err)
				}
				if created.ID != "t-1" {
					t.Fatalf("created task id = %q, want established first id t-1", created.ID)
				}
				getRec := httptest.NewRecorder()
				handler.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/v1/tasks/"+created.ID, nil))
				if getRec.Code != http.StatusOK {
					t.Fatalf("GET /v1/tasks/%s status = %d, want 200; body=%s", created.ID, getRec.Code, getRec.Body.String())
				}
				return
			}

			var count int
			if err := st.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM inspection_tasks").Scan(&count); err != nil {
				t.Fatalf("count inspection tasks: %v", err)
			}
			if count != 0 {
				t.Fatalf("cancelled POST persisted %d inspection task(s), want 0", count)
			}
			getRec := httptest.NewRecorder()
			handler.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/v1/tasks/t-1", nil))
			if getRec.Code != http.StatusNotFound {
				t.Fatalf("GET leaked task status = %d, want 404; body=%s", getRec.Code, getRec.Body.String())
			}
			if rec.Code == http.StatusCreated {
				t.Fatalf("cancelled POST unexpectedly returned 201: %s", rec.Body.String())
			}
		})
	}
}
