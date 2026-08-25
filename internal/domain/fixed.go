package domain

import (
	"errors"
	"math"
)

// Fixed-point arithmetic errors. These are stable identifiers surfaced to
// callers rather than raw database or adapter text.
var (
	ErrDivideByZero = errors.New("fixed: division by zero")
	ErrOverflow     = errors.New("fixed: integer overflow")
)

// mulChecked multiplies two signed 64-bit integers, returning ErrOverflow when
// the product cannot be represented.
func mulChecked(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	if a == math.MinInt64 && b == -1 {
		return 0, ErrOverflow
	}
	if b == math.MinInt64 && a == -1 {
		return 0, ErrOverflow
	}
	p := a * b
	if p/b != a {
		return 0, ErrOverflow
	}
	return p, nil
}

// absU64 returns the magnitude of v as an unsigned 64-bit integer without
// overflowing for math.MinInt64.
func absU64(v int64) uint64 {
	if v < 0 {
		return uint64(-(v + 1)) + 1
	}
	return uint64(v)
}

// RoundDiv divides a by b, rounding to the nearest integer. When the remainder
// is exactly half of the divisor the result is rounded away from zero. It
// returns ErrDivideByZero when b is zero and ErrOverflow when the quotient is
// not representable.
func RoundDiv(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrDivideByZero
	}
	if a == math.MinInt64 && b == -1 {
		return 0, ErrOverflow
	}
	q := a / b
	r := a % b

	// Round away from zero when 2*|r| >= |b|. Because |r| < |b| <= 2^63, the
	// doubled magnitude cannot overflow uint64.
	ar := absU64(r)
	ab := absU64(b)
	if 2*ar >= ab {
		// The sign of the result is positive iff a and b share a sign.
		if (a >= 0) == (b >= 0) {
			q++
		} else {
			q--
		}
	}
	return q, nil
}

// MulScale multiplies a and b and divides by scale, rounding the result to the
// nearest integer (ties away from zero). It is the single checked multiply and
// divide primitive used for fixed-point load, displacement and ratio math.
func MulScale(a, b, scale int64) (int64, error) {
	if scale == 0 {
		return 0, ErrDivideByZero
	}
	p, err := mulChecked(a, b)
	if err != nil {
		return 0, err
	}
	return RoundDiv(p, scale)
}

// Scale factors for the documented fixed-point quantities. Values are stored as
// signed 64-bit integers scaled by these factors.
const (
	ScaleLoad         int64 = 1000      // 荷载，单位 10^-3 kN
	ScaleDisplacement int64 = 1000      // 位移，单位 10^-3 mm
	ScaleRebound      int64 = 1_000_000 // 回弹率，单位 10^-6
	ScaleRatio        int64 = 1_000_000 // 判定比值，单位 10^-6
)
