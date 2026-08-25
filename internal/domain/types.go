package domain

// LoadLevelDef describes one locked load level of the pullout procedure.
type LoadLevelDef struct {
	Level      int   `json:"level"`
	TargetLoad int64 `json:"target_load"`
	HoldSecs   int64 `json:"hold_secs"`
}

// InspectionTask is the aggregate root for a single round of rock-bolt
// pullout inspection.
type InspectionTask struct {
	ID                string         `json:"id"`
	MileageStartMM    int64          `json:"mileage_start_mm"`
	MileageEndMM      int64          `json:"mileage_end_mm"`
	RockGrade         string         `json:"rock_grade"`
	CycleNo           int64          `json:"cycle_no"`
	LayoutRevision    string         `json:"layout_revision"`
	LayoutDigest      string         `json:"layout_digest"`
	GroutDigest       string         `json:"grout_digest"`
	SamplingSeed      uint64         `json:"sampling_seed"`
	Generation        int            `json:"generation"`
	Status            TaskStatus     `json:"status"`
	Version           int64          `json:"version"`
	LockedAt          *LogicalTime   `json:"locked_at,omitempty"`
	FinalVerdictSlot  *VerdictType   `json:"final_verdict_slot,omitempty"`
	LayerQuotas       map[string]int `json:"layer_quotas"`
	LoadLevels        []LoadLevelDef `json:"load_levels"`
	StopThreshold     int64          `json:"stop_threshold"`
	AcceptThreshold   int64          `json:"accept_threshold"`
	InfluenceBound    string         `json:"influence_bound"`
	CalibrationDigest string         `json:"calibration_digest"`
	LockDigest        string         `json:"lock_digest"`
	VerdictCredential string         `json:"verdict_credential"`
	SealDigest        string         `json:"seal_digest"`
}

// CandidateHole is one locked candidate position eligible for sampling.
type CandidateHole struct {
	TaskID      string
	CycleNo     int64
	HoleKey     HoleKey
	CoordX      int64 // 定点坐标
	CoordY      int64
	CoordZ      int64
	BarBatch    string // 杆体批次
	AnchorBatch string // 锚固剂批次
	LayerKey    string // 空间层键
}

// SampleCategory distinguishes original, expanded and retest samples.
type SampleCategory string

const (
	SampleOriginal SampleCategory = "original"
	SampleExpanded SampleCategory = "expanded"
	SampleRetest   SampleCategory = "retest"
)

// DeviceType enumerates the three device roles of a pullout test rig.
type DeviceType string

const (
	DevicePuller       DeviceType = "puller"       // 拉拔仪
	DevicePump         DeviceType = "pump"         // 泵站
	DeviceDisplacement DeviceType = "displacement" // 位移计
)

// Device is a physical test instrument.
type Device struct {
	Number   string     `json:"device_no"`
	Type     DeviceType `json:"device_type"`
	State    string     `json:"state"`
	CalibVer string     `json:"calib_ver"`
}

// DeviceLease is a half-open ownership window [Start, End) of one device.
type DeviceLease struct {
	ID         string       `json:"id"`
	DeviceNo   string       `json:"device_no"`
	TestID     string       `json:"test_id"`
	Generation int          `json:"generation"`
	Start      LogicalTime  `json:"start"`
	End        LogicalTime  `json:"end"`
	Version    int64        `json:"version"`
	ReleasedAt *LogicalTime `json:"released_at,omitempty"`
}

// LoadStage enumerates the strictly ordered actions of one bolt's pullout.
type LoadStage string

const (
	StagePreload LoadStage = "preload" // 预载
	StageLoad    LoadStage = "load"    // 分级加载
	StageHold    LoadStage = "hold"    // 持荷
	StageUnload  LoadStage = "unload"  // 卸载
	StageRebound LoadStage = "rebound" // 回弹
)

// EvidenceKind records whether evidence belongs to the original, expanded or
// retest lineage.
type EvidenceKind string

const (
	EvidenceOriginal EvidenceKind = "original"
	EvidenceExpanded EvidenceKind = "expanded"
	EvidenceRetest   EvidenceKind = "retest"
	EvidenceAudit    EvidenceKind = "audit" // 隔离审计项
)

// LoadEvidence is an append-only, versioned evidence record for one stage.
type LoadEvidence struct {
	TaskID        string       `json:"task_id"`
	SampleID      string       `json:"sample_id"`
	Generation    int          `json:"generation"`
	Kind          EvidenceKind `json:"kind"`
	Stage         LoadStage    `json:"stage"`
	LoadLevel     int          `json:"load_level"`
	Load          int64        `json:"load"`
	Displacement  int64        `json:"displacement"`
	HoldSecs      int64        `json:"hold_secs"`
	Rebound       int64        `json:"rebound"`
	Ratio         int64        `json:"ratio"`
	DeviceSet     [3]string    `json:"device_set"`
	Seq           int64        `json:"seq"`
	Accepted      bool         `json:"accepted"`
	ContentDigest string       `json:"content_digest"`
}

// CallResult classifies an instrument invocation.
type CallResult string

const (
	CallAccepted     CallResult = "accepted"
	CallRejected     CallResult = "rejected"
	CallDisconnected CallResult = "disconnected"
	CallTimeout      CallResult = "timeout"
)

// InstrumentCall is a persistent, idempotent record of one external instrument
// invocation, separated from business evidence.
type InstrumentCall struct {
	CallKey       string
	TaskID        string
	TestID        string
	SampleID      string
	Generation    int
	LoadLevel     int
	DeviceNo      string
	Seq           int
	Result        CallResult
	RetryCount    int
	NextRetryAt   LogicalTime
	EvidenceRef   *int64
	RequestDigest string
	// PendingReading retains the full reading values so a retry can re-attempt
	// the exact same instrument invocation after a restart.
	Stage        LoadStage
	Load         int64
	Displacement int64
	HoldSecs     int64
	Rebound      int64
	DeviceSet    [3]string
}

// VerdictType is the single final slot shared by pass, quarantine and cancel.
type VerdictType string

const (
	VerdictPass        VerdictType = "passed"
	VerdictQuarantined VerdictType = "quarantined"
	VerdictCancelled   VerdictType = "cancelled"
)

// OperationRecord supports idempotent replay across restarts.
type OperationRecord struct {
	OperationID    string
	TaskID         string
	Action         string
	RequestDigest  string
	ResponseCode   string
	ResponseDigest string
	CommitVersion  int64
}
