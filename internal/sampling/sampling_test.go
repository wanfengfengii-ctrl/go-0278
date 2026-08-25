package sampling

import (
	"errors"
	"testing"
)

func TestSplitMix64Golden(t *testing.T) {
	rng := NewSplitMix64(0)
	want := []uint64{16294208416658607535, 7960286522194355700, 487617019471545679}
	for i, w := range want {
		if got := rng.Next(); got != w {
			t.Fatalf("value %d = %d, want %d", i, got, w)
		}
	}
}

func TestSplitMix64Deterministic(t *testing.T) {
	a := NewSplitMix64(42)
	b := NewSplitMix64(42)
	for i := 0; i < 100; i++ {
		if a.Next() != b.Next() {
			t.Fatalf("sequence diverged at %d", i)
		}
	}
}

func TestSplitMix64DifferentSeedsDiverge(t *testing.T) {
	a := NewSplitMix64(1)
	b := NewSplitMix64(2)
	same := true
	for i := 0; i < 10; i++ {
		if a.Next() != b.Next() {
			same = false
			break
		}
	}
	if same {
		t.Fatal("different seeds produced identical sequences")
	}
}

func TestSelectPrefixDeterministic(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7, 8}
	first, err := SelectPrefix(NewSplitMix64(7), items, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := SelectPrefix(NewSplitMix64(7), items, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("selection diverged at %d: %v vs %v", i, first, second)
		}
	}
	if len(first) != 3 {
		t.Fatalf("want 3 selected, got %d", len(first))
	}
}

func TestSelectPrefixQuotaExceeded(t *testing.T) {
	items := []int{1, 2, 3}
	if _, err := SelectPrefix(NewSplitMix64(0), items, 4); !errors.Is(err, ErrQuotaInvalid) {
		t.Fatalf("want ErrQuotaInvalid, got %v", err)
	}
	if _, err := SelectPrefix(NewSplitMix64(0), items, -1); !errors.Is(err, ErrQuotaInvalid) {
		t.Fatalf("want ErrQuotaInvalid for negative quota, got %v", err)
	}
}
