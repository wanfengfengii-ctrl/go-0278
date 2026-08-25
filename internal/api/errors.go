// Package api exposes the versioned JSON HTTP surface of the rock-bolt pullout
// inspection service. It owns the stable error contract: every error carries a
// stable code, an operation id, a task version and a deterministically sorted
// reasons list, and never leaks database or device-adapter internals.
package api

import "sort"

// Reason is a single, sortable cause of an error. Sorting follows the
// documented order: mileage, ring, azimuth ordinal, hole number, load level and
// finally the stable error code.
type Reason struct {
	Code      string `json:"code"`
	MileageMM *int64 `json:"mileage_mm,omitempty"`
	RingNo    *int64 `json:"ring_no,omitempty"`
	Azimuth   string `json:"azimuth,omitempty"`
	HoleNo    *int64 `json:"hole_no,omitempty"`
	LoadLevel *int   `json:"load_level,omitempty"`
}

// ErrorResponse is the uniform error envelope returned by every endpoint.
type ErrorResponse struct {
	Code        string   `json:"code"`
	OperationID string   `json:"operation_id"`
	TaskVersion int64    `json:"task_version"`
	Reasons     []Reason `json:"reasons"`
}

// SortReasons orders reasons deterministically, in place. It never depends on
// map iteration order.
func SortReasons(rs []Reason) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if mcmp(a.MileageMM, b.MileageMM) != 0 {
			return mcmp(a.MileageMM, b.MileageMM) < 0
		}
		if mcmp(a.RingNo, b.RingNo) != 0 {
			return mcmp(a.RingNo, b.RingNo) < 0
		}
		if a.Azimuth != b.Azimuth {
			return a.Azimuth < b.Azimuth
		}
		if mcmp(a.HoleNo, b.HoleNo) != 0 {
			return mcmp(a.HoleNo, b.HoleNo) < 0
		}
		if icmp(a.LoadLevel, b.LoadLevel) != 0 {
			return icmp(a.LoadLevel, b.LoadLevel) < 0
		}
		return a.Code < b.Code
	})
}

// mcmp compares two optional int64 pointers, treating nil as less than any
// value so that missing dimensions sort first.
func mcmp(a, b *int64) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	case *a < *b:
		return -1
	case *a > *b:
		return 1
	default:
		return 0
	}
}

// icmp compares two optional int pointers, treating nil as less than any value.
func icmp(a, b *int) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	case *a < *b:
		return -1
	case *a > *b:
		return 1
	default:
		return 0
	}
}
