// Package sampling implements the documented, deterministic SplitMix64
// pseudo-random generator and the Fisher-Yates selection used to build the
// spatial stratified sample tree.
package sampling

// SplitMix64 is a deterministic 64-bit pseudo-random generator. All arithmetic
// is performed modulo 2^64, matching the documented algorithm; results depend
// only on the seed, never on map iteration order.
type SplitMix64 struct {
	state uint64
}

// NewSplitMix64 returns a generator initialized from seed.
func NewSplitMix64(seed uint64) *SplitMix64 {
	return &SplitMix64{state: seed}
}

// Next advances the generator and returns the next 64-bit value.
func (s *SplitMix64) Next() uint64 {
	s.state += 0x9E3779B97F4A7C15
	z := s.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Intn returns a uniformly distributed value in [0, n). It panics if n <= 0.
func (s *SplitMix64) Intn(n int) int {
	if n <= 0 {
		panic("sampling: Intn with non-positive bound")
	}
	return int(s.Next() % uint64(n))
}
