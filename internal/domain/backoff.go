package domain

// backoffTable is the fixed, documented retry backoff expressed in logical time
// units. Retry index 0 is the delay before the first retry, index 1 before the
// second, and so on. The table is deliberately finite: once exhausted the call
// stays parked at the final delay rather than growing without bound, keeping the
// schedule deterministic.
var backoffTable = []int64{1, 2, 4, 8, 16}

// maxRetries is the maximum number of retry attempts before a call is considered
// exhausted. It matches the length of the backoff table.
const maxRetries = 5

// NextRetryDelay returns the logical delay before the (retryCount+1)-th retry,
// clamped to the fixed table. It is always non-negative.
func NextRetryDelay(retryCount int) int64 {
	if retryCount < 0 {
		retryCount = 0
	}
	if retryCount >= len(backoffTable) {
		return backoffTable[len(backoffTable)-1]
	}
	return backoffTable[retryCount]
}

// NextRetryAt returns the logical time at which the next retry should be
// scheduled, given the current retry count and the base logical time of the
// failing call.
func NextRetryAt(retryCount int, base LogicalTime) LogicalTime {
	return base + LogicalTime(NextRetryDelay(retryCount))
}

// RetriesExhausted reports whether retryCount has reached the fixed maximum, in
// which case no further automatic retry is scheduled.
func RetriesExhausted(retryCount int) bool {
	return retryCount >= maxRetries
}
