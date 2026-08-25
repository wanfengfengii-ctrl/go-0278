package api

import (
	"encoding/json"
	"net/http"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "", 0, nil)
		return
	}
	if req.MileageStartMM <= 0 || req.MileageEndMM <= req.MileageStartMM {
		writeError(w, http.StatusBadRequest, "invalid_mileage", "", 0, nil)
		return
	}
	t, err := s.svc.CreateTask(r.Context(), domain.InspectionTask{
		MileageStartMM: req.MileageStartMM,
		MileageEndMM:   req.MileageEndMM,
		RockGrade:      req.RockGrade,
		CycleNo:        req.CycleNo,
		SamplingSeed:   req.SamplingSeed,
	})
	if err != nil {
		writeServiceError(w, "", 0, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.svc.GetTask(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, "", 0, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleLockTask(w http.ResponseWriter, r *http.Request) {
	var req lockTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", req.OperationID, 0, nil)
		return
	}
	res, err := s.svc.LockTask(r.Context(), req.OperationID, r.PathValue("id"), req.spec())
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	if len(res.Violations) > 0 {
		reasons := make([]Reason, 0, len(res.Violations))
		for _, v := range res.Violations {
			reasons = append(reasons, Reason{
				Code:      v.Code,
				MileageMM: v.MileageMM,
				RingNo:    v.RingNo,
				Azimuth:   string(v.Azimuth),
				HoleNo:    v.HoleNo,
			})
		}
		writeError(w, http.StatusUnprocessableEntity, "lock_rejected", req.OperationID, 0, reasons)
		return
	}
	writeJSON(w, http.StatusOK, res.Task)
}
