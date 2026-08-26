package api

import "rockbolt-pullout-zonal-closure/internal/domain"

// createTaskRequest is the normalized input for creating a pending-lock task.
type createTaskRequest struct {
	MileageStartMM int64  `json:"mileage_start_mm"`
	MileageEndMM   int64  `json:"mileage_end_mm"`
	RockGrade      string `json:"rock_grade"`
	CycleNo        int64  `json:"cycle_no"`
	SamplingSeed   uint64 `json:"sampling_seed"`
}

// candidateHoleRequest is one candidate hole in a lock request.
type candidateHoleRequest struct {
	MileageMM   int64  `json:"mileage_mm"`
	RingNo      int64  `json:"ring_no"`
	Azimuth     string `json:"azimuth"`
	HoleNo      int64  `json:"hole_no"`
	CoordX      int64  `json:"coord_x"`
	CoordY      int64  `json:"coord_y"`
	CoordZ      int64  `json:"coord_z"`
	BarBatch    string `json:"bar_batch"`
	AnchorBatch string `json:"anchor_batch"`
	CycleNo     int64  `json:"cycle_no"`
}

// lockTaskRequest is the full lock spec submitted to freeze a task snapshot.
type lockTaskRequest struct {
	OperationID       string                 `json:"operation_id"`
	LayoutRevision    string                 `json:"layout_revision"`
	LayoutDigest      string                 `json:"layout_digest"`
	GroutDigest       string                 `json:"grout_digest"`
	RockGrade         string                 `json:"rock_grade"`
	CycleNo           int64                  `json:"cycle_no"`
	LayerQuotas       map[string]int         `json:"layer_quotas"`
	LoadLevels        []domain.LoadLevelDef  `json:"load_levels"`
	StopThreshold     int64                  `json:"stop_threshold"`
	AcceptThreshold   int64                  `json:"accept_threshold"`
	InfluenceBound    string                 `json:"influence_bound"`
	CalibrationDigest string                 `json:"calibration_digest"`
	Candidates        []candidateHoleRequest `json:"candidates"`
}

func (r lockTaskRequest) spec() *domain.LockSpec {
	spec := &domain.LockSpec{
		LayoutRevision:    r.LayoutRevision,
		LayoutDigest:      r.LayoutDigest,
		GroutDigest:       r.GroutDigest,
		RockGrade:         r.RockGrade,
		CycleNo:           r.CycleNo,
		LayerQuotas:       r.LayerQuotas,
		LoadLevels:        r.LoadLevels,
		StopThreshold:     r.StopThreshold,
		AcceptThreshold:   r.AcceptThreshold,
		InfluenceBound:    r.InfluenceBound,
		CalibrationDigest: r.CalibrationDigest,
	}
	if spec.LayerQuotas == nil {
		spec.LayerQuotas = map[string]int{}
	}
	for _, c := range r.Candidates {
		spec.Candidates = append(spec.Candidates, domain.CandidateHole{
			CycleNo: c.CycleNo,
			HoleKey: domain.HoleKey{
				MileageMM: c.MileageMM,
				RingNo:    c.RingNo,
				Azimuth:   domain.Azimuth(c.Azimuth),
				HoleNo:    c.HoleNo,
			},
			CoordX:      c.CoordX,
			CoordY:      c.CoordY,
			CoordZ:      c.CoordZ,
			BarBatch:    c.BarBatch,
			AnchorBatch: c.AnchorBatch,
		})
	}
	return spec
}

// verifyHolesRequest carries a batch of sample ids to verify atomically.
type verifyHolesRequest struct {
	OperationID string   `json:"operation_id"`
	SampleIDs   []string `json:"sample_ids"`
}

// leaseRequest is the input to acquire a three-device lease combination.
type leaseRequest struct {
	OperationID  string `json:"operation_id"`
	TestID       string `json:"test_id"`
	TaskID       string `json:"task_id"`
	Generation   int    `json:"generation"`
	Puller       string `json:"puller"`
	Pump         string `json:"pump"`
	Displacement string `json:"displacement"`
	Start        int64  `json:"start"`
	End          int64  `json:"end"`
}

// renewLeaseRequest extends a lease.
type renewLeaseRequest struct {
	OperationID     string `json:"operation_id"`
	Holder          string `json:"holder"`
	Generation      int    `json:"generation"`
	NewEnd          int64  `json:"new_end"`
	ExpectedVersion int64  `json:"expected_version"`
}

// releaseLeaseRequest releases a lease.
type releaseLeaseRequest struct {
	OperationID     string `json:"operation_id"`
	Holder          string `json:"holder"`
	Generation      int    `json:"generation"`
	ExpectedVersion int64  `json:"expected_version"`
}

// readingRequest is a single bolt reading submission.
type readingRequest struct {
	OperationID        string `json:"operation_id"`
	TaskID             string `json:"task_id"`
	SampleID           string `json:"sample_id"`
	Stage              string `json:"stage"`
	LoadLevel          int    `json:"load_level"`
	Load               int64  `json:"load"`
	Displacement       int64  `json:"displacement"`
	HoldSecs           int64  `json:"hold_secs"`
	Rebound            int64  `json:"rebound"`
	DevicePuller       string `json:"device_puller"`
	DevicePump         string `json:"device_pump"`
	DeviceDisplacement string `json:"device_displacement"`
}

// expansionRequest triggers an influence-domain expansion around a failure.
type expansionRequest struct {
	OperationID     string `json:"operation_id"`
	FailureSampleID string `json:"failure_sample_id"`
}

// reinforcementRequest seals a reinforcement record.
type reinforcementRequest struct {
	OperationID     string `json:"operation_id"`
	InfluenceDigest string `json:"influence_digest"`
	ReinforceDigest string `json:"reinforce_digest"`
	Reason          string `json:"reason"`
}

// reviewRequest submits a qualified review signature.
type reviewRequest struct {
	OperationID  string `json:"operation_id"`
	Reviewer     string `json:"reviewer"`
	QualSnapshot string `json:"qual_snapshot"`
	Signature    string `json:"signature"`
}

// finalizeRequest competes for the single final verdict slot.
type finalizeRequest struct {
	OperationID string `json:"operation_id"`
	Verdict     string `json:"verdict"`
}
