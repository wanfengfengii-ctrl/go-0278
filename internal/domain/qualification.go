package domain

import "encoding/json"

// Qualification is the parsed personnel qualification snapshot attached to a
// review signature. A reviewer is eligible to sign only while the snapshot is
// both certified and not yet expired at the current logical time.
type Qualification struct {
	Certified  bool        `json:"certified"`
	ValidUntil LogicalTime `json:"valid_until"`
}

// ParseQualification decodes a qualification snapshot string. It reports
// ok=false for malformed input so callers never guess a reviewer's eligibility.
func ParseQualification(s string) (Qualification, bool) {
	var q Qualification
	if err := json.Unmarshal([]byte(s), &q); err != nil {
		return Qualification{}, false
	}
	return q, true
}

// ValidAt reports whether the qualification is certified and unexpired at now.
func (q Qualification) ValidAt(now LogicalTime) bool {
	return q.Certified && q.ValidUntil >= now
}
