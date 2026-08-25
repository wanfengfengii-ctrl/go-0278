package sampling

import "errors"

// ErrQuotaInvalid is returned when a layer quota is negative or exceeds the
// number of available candidates.
var ErrQuotaInvalid = errors.New("sampling: quota exceeds available candidates")

// FisherYatesShuffle reorders s in place using the provided generator. The
// result is deterministic for a fixed seed and input order.
func FisherYatesShuffle[T any](rng *SplitMix64, s []T) {
	for i := len(s) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		s[i], s[j] = s[j], s[i]
	}
}

// SelectPrefix deterministically selects n elements from items: it shuffles a
// copy using Fisher-Yates and returns the first n elements, preserving the
// shuffled (quota-prefix) order. It returns ErrQuotaInvalid when the quota is
// negative or exceeds the available items.
func SelectPrefix[T any](rng *SplitMix64, items []T, n int) ([]T, error) {
	if n < 0 || n > len(items) {
		return nil, ErrQuotaInvalid
	}
	copied := make([]T, len(items))
	copy(copied, items)
	FisherYatesShuffle(rng, copied)
	out := make([]T, n)
	copy(out, copied[:n])
	return out, nil
}
