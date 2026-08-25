package api

import (
	"encoding/json"
	"net/http"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/service"
)

func (s *Server) handleReading(w http.ResponseWriter, r *http.Request) {
	var req readingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", req.OperationID, 0, nil)
		return
	}
	res, err := s.svc.SubmitReading(r.Context(), req.OperationID, service.ReadingInput{
		TestID:             r.PathValue("id"),
		TaskID:             req.TaskID,
		SampleID:           req.SampleID,
		Stage:              domain.LoadStage(req.Stage),
		LoadLevel:          req.LoadLevel,
		Load:               req.Load,
		Displacement:       req.Displacement,
		HoldSecs:           req.HoldSecs,
		Rebound:            req.Rebound,
		DevicePuller:       req.DevicePuller,
		DevicePump:         req.DevicePump,
		DeviceDisplacement: req.DeviceDisplacement,
	})
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task":        res.Task,
		"accepted":    res.Accepted,
		"retry_count": res.RetryCount,
		"next_retry":  res.NextRetry,
	})
}

func (s *Server) handleEvidence(w http.ResponseWriter, r *http.Request) {
	ev, err := s.svc.GetEvidence(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, "", 0, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"evidence": ev})
}

func (s *Server) handleRetry(w http.ResponseWriter, r *http.Request) {
	var req operationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "", 0, nil)
		return
	}
	res, err := s.svc.RetryCall(r.Context(), req.OperationID, r.PathValue("callId"))
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task":        res.Task,
		"accepted":    res.Accepted,
		"retry_count": res.RetryCount,
		"next_retry":  res.NextRetry,
	})
}
