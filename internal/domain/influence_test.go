package domain

import "testing"

func hole(mileage, ring int64, az Azimuth, holeNo int64, bar, anchor string) CandidateHole {
	return CandidateHole{
		CycleNo:     1,
		HoleKey:     HoleKey{MileageMM: mileage, RingNo: ring, Azimuth: az, HoleNo: holeNo},
		BarBatch:    bar,
		AnchorBatch: anchor,
	}
}

func TestComputeInfluenceDomainSameBatch(t *testing.T) {
	failure := hole(12340, 5, AzimuthCrown, 1, "bar-A", "anc-X")
	candidates := []CandidateHole{
		failure,
		hole(12340, 5, AzimuthLeftWaist, 2, "bar-A", "anc-Y"), // same bar batch
		hole(12340, 6, AzimuthCrown, 1, "bar-B", "anc-X"),     // same anchor batch, adjacent ring
		hole(12340, 7, AzimuthCrown, 1, "bar-A", "anc-X"),     // ring outside bound
		hole(12340, 5, AzimuthRightFoot, 3, "bar-B", "anc-Y"), // different batches
	}
	res := ComputeInfluenceDomain(failure, candidates, "1")
	if !res.Complete {
		t.Fatal("boundary data should be complete")
	}
	// Expected: failure + same-bar hole + same-anchor adjacent-ring hole.
	if len(res.Holes) != 3 {
		t.Fatalf("want 3 influence holes, got %d: %+v", len(res.Holes), res.Holes)
	}
	// Canonical order: ring 5 left_waist (hole2) before ring 5 crown (hole1)? left_waist ordinal 1 < crown 3.
	if res.Holes[0].HoleKey.Azimuth != AzimuthLeftWaist {
		t.Fatalf("first member should be left_waist, got %+v", res.Holes[0])
	}
}

func TestComputeInfluenceDomainIncomplete(t *testing.T) {
	failure := hole(12340, 5, AzimuthCrown, 1, "", "anc-X")
	res := ComputeInfluenceDomain(failure, []CandidateHole{failure}, "1")
	if res.Complete {
		t.Fatal("missing bar batch must mark result incomplete")
	}
	if res.InfluenceDigest() != "incomplete" {
		t.Fatalf("incomplete digest sentinel mismatch: %q", res.InfluenceDigest())
	}
}

func TestComputeInfluenceDomainBadBound(t *testing.T) {
	failure := hole(12340, 5, AzimuthCrown, 1, "bar-A", "anc-X")
	res := ComputeInfluenceDomain(failure, []CandidateHole{failure}, "nonsense")
	if res.Complete {
		t.Fatal("unparsable bound must mark result incomplete")
	}
}

func TestComputeInfluenceDomainDedup(t *testing.T) {
	failure := hole(12340, 5, AzimuthCrown, 1, "bar-A", "anc-X")
	dup := hole(12340, 5, AzimuthCrown, 1, "bar-A", "anc-X")
	res := ComputeInfluenceDomain(failure, []CandidateHole{failure, dup}, "0")
	if !res.Complete || len(res.Holes) != 1 {
		t.Fatalf("duplicate candidate must be de-duplicated, got %d", len(res.Holes))
	}
}
