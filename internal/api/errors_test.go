package api

import "testing"

func ptr64(v int64) *int64 { return &v }
func ptrInt(v int) *int    { return &v }

func TestSortReasonsDeterministic(t *testing.T) {
	rs := []Reason{
		{Code: "hold_too_short", MileageMM: ptr64(200), LoadLevel: ptrInt(2)},
		{Code: "bad_azimuth", MileageMM: ptr64(100)},
		{Code: "hold_too_short", MileageMM: ptr64(100), LoadLevel: ptrInt(1)},
		{Code: "dup_hole", MileageMM: ptr64(100), RingNo: ptr64(3), HoleNo: ptr64(7)},
		{Code: "dup_hole", MileageMM: ptr64(100), RingNo: ptr64(3), HoleNo: ptr64(2)},
	}
	SortReasons(rs)

	// Expected order: mileage asc, then nil ring first, then load level / hole
	// number asc, then code.
	want := []string{"bad_azimuth", "hold_too_short", "dup_hole", "dup_hole", "hold_too_short"}
	for i, c := range want {
		if rs[i].Code != c {
			t.Fatalf("index %d = %s, want %s (order %+v)", i, rs[i].Code, c, rs)
		}
	}
	// Hole numbers ascend within the same ring.
	if rs[2].HoleNo == nil || *rs[2].HoleNo != 2 {
		t.Fatalf("index 2 should be hole 2, got %+v", rs[2])
	}
	if rs[3].HoleNo == nil || *rs[3].HoleNo != 7 {
		t.Fatalf("index 3 should be hole 7, got %+v", rs[3])
	}
}

func TestSortReasonsEmpty(t *testing.T) {
	SortReasons(nil)
	SortReasons([]Reason{})
}
