package domain

// LogicalTime is a monotonically increasing clock value used to order lease
// windows and instrument calls deterministically. It is intentionally decoupled
// from wall-clock time so that lease expiry and hold durations never depend on
// background time races.
type LogicalTime int64

// HoldSeconds returns the non-negative logical time difference between start
// and end. A negative result (end before start) collapses to zero so that hold
// durations are always non-negative integers.
func HoldSeconds(start, end LogicalTime) int64 {
	d := int64(end) - int64(start)
	if d < 0 {
		return 0
	}
	return d
}
