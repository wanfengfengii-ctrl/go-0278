package domain

import "testing"

func TestNormalizeAzimuth(t *testing.T) {
	a, ok := NormalizeAzimuth("crown")
	if !ok || a != AzimuthCrown {
		t.Fatalf("want crown, got %q ok=%v", a, ok)
	}
	if _, ok := NormalizeAzimuth("top"); ok {
		t.Fatal("non-documented azimuth must not normalize")
	}
}

func TestHoleKeyLessOrdering(t *testing.T) {
	keys := []HoleKey{
		{MileageMM: 12340, RingNo: 5, Azimuth: AzimuthRightWaist, HoleNo: 2},
		{MileageMM: 12340, RingNo: 4, Azimuth: AzimuthCrown, HoleNo: 9},
		{MileageMM: 12340, RingNo: 5, Azimuth: AzimuthLeftFoot, HoleNo: 1},
		{MileageMM: 12339, RingNo: 9, Azimuth: AzimuthCrown, HoleNo: 1},
	}
	SortHoleKeys(keys)
	want := []HoleKey{
		{MileageMM: 12339, RingNo: 9, Azimuth: AzimuthCrown, HoleNo: 1},
		{MileageMM: 12340, RingNo: 4, Azimuth: AzimuthCrown, HoleNo: 9},
		{MileageMM: 12340, RingNo: 5, Azimuth: AzimuthLeftFoot, HoleNo: 1},
		{MileageMM: 12340, RingNo: 5, Azimuth: AzimuthRightWaist, HoleNo: 2},
	}
	for i := range want {
		if !keys[i].Equal(want[i]) {
			t.Fatalf("index %d: got %+v, want %+v", i, keys[i], want[i])
		}
	}
}

func TestHasDuplicateHoleKey(t *testing.T) {
	dup := []HoleKey{
		{MileageMM: 12340, RingNo: 5, Azimuth: AzimuthCrown, HoleNo: 1},
		{MileageMM: 12340, RingNo: 5, Azimuth: AzimuthCrown, HoleNo: 1},
	}
	if !HasDuplicateHoleKey(dup) {
		t.Fatal("duplicate key not detected")
	}
	uniq := []HoleKey{
		{MileageMM: 12340, RingNo: 5, Azimuth: AzimuthCrown, HoleNo: 1},
		{MileageMM: 12340, RingNo: 5, Azimuth: AzimuthCrown, HoleNo: 2},
	}
	if HasDuplicateHoleKey(uniq) {
		t.Fatal("unique keys flagged as duplicate")
	}
}
