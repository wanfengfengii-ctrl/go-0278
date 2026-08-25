package domain

import (
	"sort"
	"strconv"
	"strings"
)

// LockSpec is the complete set of fields frozen atomically when a task is
// locked. It carries the immutable rules snapshot plus every candidate hole.
type LockSpec struct {
	LayoutRevision    string
	LayoutDigest      string
	GroutDigest       string
	RockGrade         string
	CycleNo           int64
	LayerQuotas       map[string]int
	LoadLevels        []LoadLevelDef
	StopThreshold     int64
	AcceptThreshold   int64
	InfluenceBound    string
	CalibrationDigest string
	Candidates        []CandidateHole
}

// LockViolation is one deterministic reason a lock request was rejected.
// It carries the canonical dimensions used to sort reasons in the HTTP error
// response, plus a stable code.
type LockViolation struct {
	Code      string
	MileageMM *int64
	RingNo    *int64
	Azimuth   Azimuth
	HoleNo    *int64
	Message   string
}

// InfluenceAdjacentRings parses the influence bound into a number of adjacent
// rings. A bound of "0" means only the failing ring itself; a bound of "1"
// means the failing ring plus one ring on each side. A non-positive or
// unparsable bound is reported via ok=false so callers never guess.
func InfluenceAdjacentRings(bound string) (int, bool) {
	b := strings.TrimSpace(bound)
	n, err := strconv.Atoi(b)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// ValidateLockSpec validates a lock request in full. Every candidate must fall
// within the locked mileage segment and cycle, must carry a canonical azimuth
// and a valid layer quota, and must not collide with another canonical key.
// The returned violations are sorted by the documented order so callers can
// surface them deterministically.
func ValidateLockSpec(task *InspectionTask, spec *LockSpec) []LockViolation {
	var vs []LockViolation

	if spec.RockGrade == "" {
		vs = append(vs, LockViolation{Code: "missing_rock_grade"})
	}
	if spec.LayoutDigest == "" {
		vs = append(vs, LockViolation{Code: "missing_layout_digest"})
	}
	if spec.CalibrationDigest == "" {
		vs = append(vs, LockViolation{Code: "missing_calibration_digest"})
	}
	if len(spec.LoadLevels) == 0 {
		vs = append(vs, LockViolation{Code: "missing_load_levels"})
	}
	if spec.AcceptThreshold <= 0 || spec.StopThreshold <= 0 {
		vs = append(vs, LockViolation{Code: "invalid_threshold"})
	}
	if _, ok := InfluenceAdjacentRings(spec.InfluenceBound); !ok {
		vs = append(vs, LockViolation{Code: "invalid_influence_bound"})
	}

	// Validate and normalize quotas. Only documented azimuths may carry a
	// quota, and a quota must be non-negative.
	quotas := make(map[Azimuth]int, len(spec.LayerQuotas))
	for raw, n := range spec.LayerQuotas {
		a, ok := NormalizeAzimuth(raw)
		if !ok {
			vs = append(vs, LockViolation{Code: "unknown_azimuth", Azimuth: Azimuth(raw)})
			continue
		}
		if n < 0 {
			vs = append(vs, LockViolation{Code: "negative_quota", Azimuth: a})
			continue
		}
		quotas[a] = n
	}
	spec.LayerQuotas = map[string]int{}
	for a, n := range quotas {
		spec.LayerQuotas[string(a)] = n
	}

	// Validate each candidate hole and detect canonical-key collisions.
	seen := make(map[HoleKey]struct{}, len(spec.Candidates))
	for i := range spec.Candidates {
		c := &spec.Candidates[i]
		m, rn, az, hn := c.HoleKey.MileageMM, c.HoleKey.RingNo, c.HoleKey.Azimuth, c.HoleKey.HoleNo
		if m < task.MileageStartMM || m > task.MileageEndMM {
			vs = append(vs, LockViolation{Code: "mileage_out_of_range", MileageMM: &m, RingNo: &rn, Azimuth: az, HoleNo: &hn})
		}
		if c.CycleNo != task.CycleNo {
			vs = append(vs, LockViolation{Code: "cycle_mismatch", MileageMM: &m, RingNo: &rn, Azimuth: az, HoleNo: &hn})
		}
		if _, ok := AzimuthOrdinal(az); !ok {
			vs = append(vs, LockViolation{Code: "unknown_azimuth", MileageMM: &m, RingNo: &rn, Azimuth: az, HoleNo: &hn})
		}
		if c.BarBatch == "" || c.AnchorBatch == "" {
			vs = append(vs, LockViolation{Code: "missing_material_batch", MileageMM: &m, RingNo: &rn, Azimuth: az, HoleNo: &hn})
		}
		key := c.HoleKey
		if _, ok := seen[key]; ok {
			vs = append(vs, LockViolation{Code: "duplicate_hole", MileageMM: &m, RingNo: &rn, Azimuth: az, HoleNo: &hn})
		}
		seen[key] = struct{}{}
		c.LayerKey = string(az)
	}

	sort.SliceStable(vs, func(i, j int) bool {
		a, b := vs[i], vs[j]
		if mi, mj := a.MileageMM, b.MileageMM; (mi == nil) != (mj == nil) {
			return mi == nil
		} else if mi != nil && mj != nil && *mi != *mj {
			return *mi < *mj
		}
		if ri, rj := a.RingNo, b.RingNo; (ri == nil) != (rj == nil) {
			return ri == nil
		} else if ri != nil && rj != nil && *ri != *rj {
			return *ri < *rj
		}
		if a.Azimuth != b.Azimuth {
			return a.Azimuth < b.Azimuth
		}
		if hi, hj := a.HoleNo, b.HoleNo; (hi == nil) != (hj == nil) {
			return hi == nil
		} else if hi != nil && hj != nil && *hi != *hj {
			return *hi < *hj
		}
		return a.Code < b.Code
	})

	return vs
}
