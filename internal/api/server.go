// Package api exposes the versioned JSON HTTP surface of the rock-bolt pullout
// inspection service. It owns the stable error contract: every error carries a
// stable code, an operation id, a task version and a deterministically sorted
// reasons list, and never leaks database or device-adapter internals.
package api

import (
	"encoding/json"
	"net/http"

	"rockbolt-pullout-zonal-closure/internal/service"
)

// Server wires the HTTP routes to the application service.
type Server struct {
	svc *service.Service
}

// NewServer returns an http.Handler serving the versioned JSON API.
func NewServer(svc *service.Service) http.Handler {
	s := &Server{svc: svc}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/tasks", s.handleCreateTask)
	mux.HandleFunc("GET /v1/tasks/{id}", s.handleGetTask)
	mux.HandleFunc("POST /v1/tasks/{id}/lock", s.handleLockTask)
	mux.HandleFunc("POST /v1/tasks/{id}/samples", s.handleSample)
	mux.HandleFunc("GET /v1/tasks/{id}/sample-tree", s.handleSampleTree)
	mux.HandleFunc("POST /v1/tasks/{id}/holes/verify", s.handleVerifyHoles)
	mux.HandleFunc("POST /v1/tests/{id}/leases", s.handleAcquireLeases)
	mux.HandleFunc("POST /v1/tests/{id}/leases/{leaseId}/renew", s.handleRenewLease)
	mux.HandleFunc("POST /v1/tests/{id}/leases/{leaseId}/release", s.handleReleaseLease)
	mux.HandleFunc("POST /v1/tests/{id}/readings", s.handleReading)
	mux.HandleFunc("GET /v1/tests/{id}/evidence", s.handleEvidence)
	mux.HandleFunc("POST /v1/instrument-calls/{callId}/retry", s.handleRetry)
	mux.HandleFunc("POST /v1/tasks/{id}/expansion", s.handleExpansion)
	mux.HandleFunc("POST /v1/tasks/{id}/reinforcements", s.handleReinforce)
	mux.HandleFunc("POST /v1/tasks/{id}/retests", s.handleRetest)
	mux.HandleFunc("POST /v1/tasks/{id}/reviews", s.handleReview)
	mux.HandleFunc("POST /v1/tasks/{id}/finalize", s.handleFinalize)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, op string, ver int64, reasons []Reason) {
	if reasons == nil {
		reasons = []Reason{}
	}
	SortReasons(reasons)
	writeJSON(w, status, ErrorResponse{
		Code:        code,
		OperationID: op,
		TaskVersion: ver,
		Reasons:     reasons,
	})
}

// writeServiceError maps a service/store error to a stable HTTP error response.
func writeServiceError(w http.ResponseWriter, op string, ver int64, err error) {
	code, status := mapError(err)
	writeError(w, status, code, op, ver, nil)
}

func mapError(err error) (string, int) {
	switch {
	case service.IsNotFound(err):
		return "task_not_found", http.StatusNotFound
	case service.IsReplay(err):
		return "replayed", http.StatusOK
	case service.IsConflict(err):
		return "conflict", http.StatusConflict
	case err == service.ErrInvalidTransition:
		return "invalid_transition", http.StatusConflict
	case err == service.ErrLeaseConflict:
		return "lease_conflict", http.StatusConflict
	case err == service.ErrStageOutOfOrder:
		return "stage_out_of_order", http.StatusConflict
	case err == service.ErrHoldTooShort:
		return "hold_too_short", http.StatusUnprocessableEntity
	case err == service.ErrArithmetic:
		return "arithmetic_error", http.StatusUnprocessableEntity
	case err == service.ErrBoundaryMissing:
		return "boundary_missing", http.StatusConflict
	case err == service.ErrNotQualified:
		return "not_qualified", http.StatusConflict
	case err == service.ErrSamplesOpen:
		return "samples_open", http.StatusConflict
	case err == service.ErrDeviceInvalid:
		return "device_invalid", http.StatusUnprocessableEntity
	default:
		return "internal_error", http.StatusInternalServerError
	}
}
