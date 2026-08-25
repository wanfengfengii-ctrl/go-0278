package domain

// TaskStatus is the business state of an inspection task. It is deliberately
// finite and transitions are validated centrally by the task aggregate.
type TaskStatus string

const (
	StatusPendingLock      TaskStatus = "pending_lock"      // 待锁定
	StatusPendingSample    TaskStatus = "pending_sample"    // 待抽样
	StatusHoleVerification TaskStatus = "hole_verification" // 孔位核对中
	StatusLoading          TaskStatus = "loading"           // 加载试验中
	StatusExpanding        TaskStatus = "expanding"         // 扩样中
	StatusPendingReinforce TaskStatus = "pending_reinforce" // 待补强
	StatusRetesting        TaskStatus = "retesting"         // 复验中
	StatusPendingReview    TaskStatus = "pending_review"    // 待复核
	StatusPassed           TaskStatus = "passed"            // 已通过
	StatusQuarantined      TaskStatus = "quarantined"       // 质量隔离
	StatusCancelled        TaskStatus = "cancelled"         // 已取消
)

// IsTerminal reports whether the status is a final, irreversible state.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case StatusPassed, StatusQuarantined, StatusCancelled:
		return true
	default:
		return false
	}
}

// IsValid reports whether the status is one of the documented business states.
func (s TaskStatus) IsValid() bool {
	switch s {
	case StatusPendingLock, StatusPendingSample, StatusHoleVerification,
		StatusLoading, StatusExpanding, StatusPendingReinforce, StatusRetesting,
		StatusPendingReview, StatusPassed, StatusQuarantined, StatusCancelled:
		return true
	default:
		return false
	}
}

// allowedTransitions is the centralized state machine. A terminal state has no
// outgoing edge, so any request to move out of it is rejected.
var allowedTransitions = map[TaskStatus][]TaskStatus{
	StatusPendingLock:      {StatusPendingSample},
	StatusPendingSample:    {StatusHoleVerification},
	StatusHoleVerification: {StatusLoading, StatusExpanding, StatusPendingReview},
	StatusLoading:          {StatusExpanding, StatusPendingReview},
	StatusExpanding:        {StatusPendingReinforce, StatusPendingReview, StatusRetesting},
	StatusPendingReinforce: {StatusRetesting},
	StatusRetesting:        {StatusPendingReview, StatusExpanding},
	StatusPendingReview:    {StatusPassed, StatusQuarantined, StatusCancelled},
}

// CanTransition reports whether moving from the current status to next is legal.
// A transition out of an unknown or terminal state is never legal.
func (s TaskStatus) CanTransition(next TaskStatus) bool {
	if !s.IsValid() || s.IsTerminal() {
		return false
	}
	for _, candidate := range allowedTransitions[s] {
		if candidate == next {
			return true
		}
	}
	return false
}
