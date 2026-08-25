package domain

import (
	"errors"
	"math"
	"testing"
)

func TestRoundDivHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		name string
		a, b int64
		want int64
	}{
		{"positive exact", 4, 2, 2},
		{"positive round up", 5, 2, 3},
		{"positive half", 3, 2, 2},
		{"negative round down", -5, 2, -3},
		{"negative half", -3, 2, -2},
		{"positive less than half", 2, 3, 1},
		{"negative less than half", -2, 3, -1},
		{"positive near zero", 1, 3, 0},
		{"negative near zero", -1, 3, 0},
		{"negative divisor exact", 4, -2, -2},
		{"negative divisor half", 5, -2, -3},
		{"both negative half", -5, -2, 3},
		{"min by one", math.MinInt64, 1, math.MinInt64},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := RoundDiv(c.a, c.b)
			if err != nil {
				t.Fatalf("RoundDiv(%d,%d) unexpected error: %v", c.a, c.b, err)
			}
			if got != c.want {
				t.Fatalf("RoundDiv(%d,%d) = %d, want %d", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestRoundDivDivideByZero(t *testing.T) {
	if _, err := RoundDiv(10, 0); !errors.Is(err, ErrDivideByZero) {
		t.Fatalf("want ErrDivideByZero, got %v", err)
	}
}

func TestRoundDivOverflow(t *testing.T) {
	if _, err := RoundDiv(math.MinInt64, -1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("want ErrOverflow, got %v", err)
	}
}

func TestMulScale(t *testing.T) {
	cases := []struct {
		name        string
		a, b, scale int64
		want        int64
	}{
		{"basic", 1000, 2000, 1000, 2000},
		{"round half away", 3, 5, 2, 8},
		{"negative", -3, 5, 2, -8},
		{"ratio percent", 25, 1000000, 100, 250000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := MulScale(c.a, c.b, c.scale)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("MulScale(%d,%d,%d) = %d, want %d", c.a, c.b, c.scale, got, c.want)
			}
		})
	}
}

func TestMulScaleOverflow(t *testing.T) {
	if _, err := MulScale(math.MaxInt64, 2, 1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("want ErrOverflow, got %v", err)
	}
}

func TestMulScaleDivideByZero(t *testing.T) {
	if _, err := MulScale(1, 2, 0); !errors.Is(err, ErrDivideByZero) {
		t.Fatalf("want ErrDivideByZero, got %v", err)
	}
}

func TestHoldSecondsNonNegative(t *testing.T) {
	if got := HoldSeconds(LogicalTime(5), LogicalTime(3)); got != 0 {
		t.Fatalf("want 0 for reversed interval, got %d", got)
	}
	if got := HoldSeconds(LogicalTime(3), LogicalTime(8)); got != 5 {
		t.Fatalf("want 5, got %d", got)
	}
}
