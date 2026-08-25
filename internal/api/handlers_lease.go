package api

import (
	"encoding/json"
	"net/http"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/service"
)

func (s *Server) handleAcquireLeases(w http.ResponseWriter, r *http.Request) {
	var req leaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", req.OperationID, 0, nil)
		return
	}
	// The path-identified test is the canonical lease owner; the body test_id is
	// accepted only when the path carries none (kept for backward compatibility).
	testID := r.PathValue("id")
	if testID == "" {
		testID = req.TestID
	}
	t, err := s.svc.AcquireLeases(r.Context(), req.OperationID, service.LeaseInput{
		TestID:       testID,
		TaskID:       req.TaskID,
		Generation:   req.Generation,
		Puller:       req.Puller,
		Pump:         req.Pump,
		Displacement: req.Displacement,
		Start:        domain.LogicalTime(req.Start),
		End:          domain.LogicalTime(req.End),
	})
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleRenewLease(w http.ResponseWriter, r *http.Request) {
	var req renewLeaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", req.OperationID, 0, nil)
		return
	}
	l, err := s.svc.RenewLease(r.Context(), req.OperationID, r.PathValue("id"), service.RenewLeaseInput{
		LeaseID:         r.PathValue("leaseId"),
		Holder:          req.Holder,
		Generation:      req.Generation,
		NewEnd:          domain.LogicalTime(req.NewEnd),
		ExpectedVersion: req.ExpectedVersion,
	})
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleReleaseLease(w http.ResponseWriter, r *http.Request) {
	var req releaseLeaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", req.OperationID, 0, nil)
		return
	}
	err := s.svc.ReleaseLease(r.Context(), req.OperationID, r.PathValue("id"), service.ReleaseLeaseInput{
		LeaseID:         r.PathValue("leaseId"),
		Holder:          req.Holder,
		Generation:      req.Generation,
		ExpectedVersion: req.ExpectedVersion,
	})
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
}
