// Package lease implements the ownership rules for half-open device lease
// windows shared by the puller, pump and displacement instruments.
package lease

// Overlaps reports whether two half-open intervals [aStart, aEnd) and
// [bStart, bEnd) overlap. Merely touching intervals (aEnd == bStart) do not
// overlap, which is what allows adjacent windows for the same device.
func Overlaps(aStart, aEnd, bStart, bEnd int64) bool {
	return aStart < bEnd && bStart < aEnd
}

// Covers reports whether the half-open window [start, end) contains point t.
func Covers(start, end, t int64) bool {
	return start <= t && t < end
}

// Valid reports whether a half-open window is non-empty (start < end).
func Valid(start, end int64) bool {
	return start < end
}
