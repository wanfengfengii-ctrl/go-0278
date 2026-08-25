package api

import (
	"encoding/json"
	"net/http"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

func (s *Server) handleExpansion(w http.ResponseWriter, r *http.Request) {
	var req expansionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", req.OperationID, 0, nil)
		return
	}
	t, influenceDigest, err := s.svc.Expand(r.Context(), req.OperationID, r.PathValue("id"), req.FailureSampleID)
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task":             t,
		"influence_digest": influenceDigest,
	})
}

func (s *Server) handleReinforce(w http.ResponseWriter, r *http.Request) {
	var req reinforcementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", req.OperationID, 0, nil)
		return
	}
	t, err := s.svc.SealReinforcement(r.Context(), req.OperationID, r.PathValue("id"),
		req.InfluenceDigest, req.ReinforceDigest, req.Reason)
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleRetest(w http.ResponseWriter, r *http.Request) {
	var req operationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "", 0, nil)
		return
	}
	t, err := s.svc.CreateRetest(r.Context(), req.OperationID, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	var req reviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", req.OperationID, 0, nil)
		return
	}
	t, err := s.svc.Review(r.Context(), req.OperationID, r.PathValue("id"),
		req.Reviewer, req.QualSnapshot, req.Signature)
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	var req finalizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", req.OperationID, 0, nil)
		return
	}
	t, err := s.svc.Finalize(r.Context(), req.OperationID, r.PathValue("id"), domain.VerdictType(req.Verdict))
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}
