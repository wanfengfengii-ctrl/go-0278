package domain

import "sort"

// InfluenceResult is the deterministic, canonically ordered influence domain
// computed around a single failing hole, together with the completeness flag
// used to decide whether expansion is allowed at all.
type InfluenceResult struct {
	// Complete is false when the boundary data is insufficient (missing bar or
	// anchor batch, or an unparsable influence bound). Callers must then route
	// the task to pending review rather than guessing an expansion range.
	Complete bool
	// Holes is the canonical set of candidates sharing the failing point's
	// excavation cycle and either its bar batch or anchor batch, restricted to
	// the adjacent-ring window. It is sorted and de-duplicated by canonical key.
	Holes []CandidateHole
}

// ComputeInfluenceDomain builds the unique influence domain around the failing
// hole. A candidate is included when it belongs to the same excavation cycle as
// the failure, shares either the bar batch or the anchor batch, and its ring
// number lies within [failureRing - bound, failureRing + bound] where bound is
// the locked influence bound parsed by InfluenceAdjacentRings. The failure hole
// itself is included. The result is sorted by canonical hole key and
// de-duplicated. When the failing hole lacks a bar or anchor batch, or the bound
// cannot be parsed, Complete is false and no expansion set is produced.
func ComputeInfluenceDomain(failure CandidateHole, candidates []CandidateHole, boundStr string) InfluenceResult {
	bound, ok := InfluenceAdjacentRings(boundStr)
	if !ok || failure.BarBatch == "" || failure.AnchorBatch == "" {
		return InfluenceResult{Complete: false}
	}

	lo := failure.HoleKey.RingNo - int64(bound)
	hi := failure.HoleKey.RingNo + int64(bound)

	seen := make(map[HoleKey]struct{})
	var out []CandidateHole
	for _, c := range candidates {
		if c.CycleNo != failure.CycleNo {
			continue
		}
		if c.HoleKey.RingNo < lo || c.HoleKey.RingNo > hi {
			continue
		}
		if c.BarBatch != failure.BarBatch && c.AnchorBatch != failure.AnchorBatch {
			continue
		}
		if _, dup := seen[c.HoleKey]; dup {
			continue
		}
		seen[c.HoleKey] = struct{}{}
		out = append(out, c)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].HoleKey.Less(out[j].HoleKey) })
	return InfluenceResult{Complete: true, Holes: out}
}

// InfluenceDigest returns the stable content digest of an influence result. It
// uses the ordered canonical keys of the included holes so two equivalent
// computations produce identical digests, and an incomplete result digests to a
// distinct sentinel value.
func (r InfluenceResult) InfluenceDigest() string {
	if !r.Complete {
		return "incomplete"
	}
	keys := make([]HoleKey, 0, len(r.Holes))
	for _, h := range r.Holes {
		keys = append(keys, h.HoleKey)
	}
	return Digest(keys)
}
