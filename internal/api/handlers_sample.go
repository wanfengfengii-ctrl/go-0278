package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"rockbolt-pullout-zonal-closure/internal/store"
)

// sampleNodeDTO is the JSON form of a sample-tree node.
type sampleNodeDTO struct {
	ID              string `json:"id"`
	ParentID        string `json:"parent_id,omitempty"`
	LayerKey        string `json:"layer_key"`
	MileageMM       *int64 `json:"mileage_mm,omitempty"`
	RingNo          *int64 `json:"ring_no,omitempty"`
	Azimuth         string `json:"azimuth,omitempty"`
	HoleNo          *int64 `json:"hole_no,omitempty"`
	Category        string `json:"category"`
	PickOrder       int    `json:"pick_order"`
	SourceFailure   string `json:"source_failure,omitempty"`
	InfluenceDigest string `json:"influence_digest,omitempty"`
	Generation      int    `json:"generation"`
	Closed          bool   `json:"closed"`
	Verified        bool   `json:"verified"`
}

func toSampleNodeDTO(n store.SampleNode) sampleNodeDTO {
	d := sampleNodeDTO{
		ID:              n.ID,
		ParentID:        n.ParentID,
		LayerKey:        n.LayerKey,
		Category:        string(n.Category),
		PickOrder:       n.PickOrder,
		InfluenceDigest: n.InfluenceDigest,
		Generation:      n.Generation,
		Closed:          n.Closed,
		Verified:        n.Verified,
	}
	if n.HoleKey != nil {
		m := n.HoleKey.MileageMM
		r := n.HoleKey.RingNo
		h := n.HoleKey.HoleNo
		d.MileageMM = &m
		d.RingNo = &r
		d.HoleNo = &h
		d.Azimuth = string(n.HoleKey.Azimuth)
	}
	if n.SourceFailure != nil {
		d.SourceFailure = fmt.Sprintf("%d:%d:%s:%d", n.SourceFailure.MileageMM,
			n.SourceFailure.RingNo, n.SourceFailure.Azimuth, n.SourceFailure.HoleNo)
	}
	return d
}

func (s *Server) handleSample(w http.ResponseWriter, r *http.Request) {
	var req operationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "", 0, nil)
		return
	}
	res, err := s.svc.Sample(r.Context(), req.OperationID, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	nodes := make([]sampleNodeDTO, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		nodes = append(nodes, toSampleNodeDTO(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task":   res.Task,
		"digest": res.Digest,
		"nodes":  nodes,
	})
}

func (s *Server) handleSampleTree(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.svc.GetSampleTree(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, "", 0, err)
		return
	}
	out := make([]sampleNodeDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, toSampleNodeDTO(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": out})
}

func (s *Server) handleVerifyHoles(w http.ResponseWriter, r *http.Request) {
	var req verifyHolesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", req.OperationID, 0, nil)
		return
	}
	t, err := s.svc.VerifyHoles(r.Context(), req.OperationID, r.PathValue("id"), req.SampleIDs)
	if err != nil {
		writeServiceError(w, req.OperationID, 0, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

type operationRequest struct {
	OperationID string `json:"operation_id"`
}
