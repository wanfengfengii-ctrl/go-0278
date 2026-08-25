package domain

import "sort"

// Azimuth is a normalized arch position. Only documented positions are valid;
// any other spelling must be rejected rather than guessed.
type Azimuth string

// Canonical arch positions, ordered from left foot to right foot. The ordinal
// value is used for deterministic sorting and is independent of map iteration.
const (
	AzimuthLeftFoot      Azimuth = "left_foot"      // 左拱脚
	AzimuthLeftWaist     Azimuth = "left_waist"     // 左拱腰
	AzimuthLeftShoulder  Azimuth = "left_shoulder"  // 左拱肩
	AzimuthCrown         Azimuth = "crown"          // 拱顶
	AzimuthRightShoulder Azimuth = "right_shoulder" // 右拱肩
	AzimuthRightWaist    Azimuth = "right_waist"    // 右拱腰
	AzimuthRightFoot     Azimuth = "right_foot"     // 右拱脚
)

// azimuthOrder assigns a stable, unique ordinal to every documented azimuth.
var azimuthOrder = map[Azimuth]int{
	AzimuthLeftFoot:      0,
	AzimuthLeftWaist:     1,
	AzimuthLeftShoulder:  2,
	AzimuthCrown:         3,
	AzimuthRightShoulder: 4,
	AzimuthRightWaist:    5,
	AzimuthRightFoot:     6,
}

// AzimuthOrdinal returns the sort ordinal of a. A zero boolean is returned when
// the azimuth is not a documented position.
func AzimuthOrdinal(a Azimuth) (int, bool) {
	o, ok := azimuthOrder[a]
	return o, ok
}

// NormalizeAzimuth maps a raw field string to a canonical azimuth.
func NormalizeAzimuth(raw string) (Azimuth, bool) {
	a := Azimuth(raw)
	_, ok := azimuthOrder[a]
	return a, ok
}

// HoleKey is the canonical, normalization-stable identifier of a candidate
// hole: mileage in millimetres, ring number, normalized azimuth and hole number.
type HoleKey struct {
	MileageMM int64   // 规范里程（毫米）
	RingNo    int64   // 环号
	Azimuth   Azimuth // 标准化方位
	HoleNo    int64   // 孔位号
}

// Less reports whether k sorts strictly before o. Ordering is by mileage, ring
// number, azimuth ordinal and hole number, matching the documented sequence.
func (k HoleKey) Less(o HoleKey) bool {
	if k.MileageMM != o.MileageMM {
		return k.MileageMM < o.MileageMM
	}
	if k.RingNo != o.RingNo {
		return k.RingNo < o.RingNo
	}
	ko, kok := AzimuthOrdinal(k.Azimuth)
	oo, ook := AzimuthOrdinal(o.Azimuth)
	if kok != ook || ko != oo {
		return ko < oo
	}
	return k.HoleNo < o.HoleNo
}

// Equal reports whether two keys identify the same canonical hole.
func (k HoleKey) Equal(o HoleKey) bool {
	return k.MileageMM == o.MileageMM &&
		k.RingNo == o.RingNo &&
		k.Azimuth == o.Azimuth &&
		k.HoleNo == o.HoleNo
}

// SortHoleKeys sorts a slice of keys in canonical order, in place.
func SortHoleKeys(keys []HoleKey) {
	sort.Slice(keys, func(i, j int) bool { return keys[i].Less(keys[j]) })
}

// HasDuplicateHoleKey reports whether the (already normalized) slice contains
// any two holes sharing the same canonical key.
func HasDuplicateHoleKey(keys []HoleKey) bool {
	seen := make(map[HoleKey]struct{}, len(keys))
	for _, k := range keys {
		if _, ok := seen[k]; ok {
			return true
		}
		seen[k] = struct{}{}
	}
	return false
}
