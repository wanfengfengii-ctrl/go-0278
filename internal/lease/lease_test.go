package lease

import "testing"

func TestOverlaps(t *testing.T) {
	cases := []struct {
		name           string
		as, ae, bs, be int64
		want           bool
	}{
		{"overlap", 0, 10, 5, 15, true},
		{"contained", 0, 10, 2, 8, true},
		{"adjacent touch", 0, 10, 10, 20, false},
		{"disjoint", 0, 5, 6, 10, false},
		{"equal", 0, 10, 0, 10, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Overlaps(c.as, c.ae, c.bs, c.be); got != c.want {
				t.Fatalf("Overlaps(%d,%d,%d,%d) = %v, want %v", c.as, c.ae, c.bs, c.be, got, c.want)
			}
		})
	}
}

func TestCovers(t *testing.T) {
	if !Covers(0, 10, 0) {
		t.Fatal("start point must be covered")
	}
	if Covers(0, 10, 10) {
		t.Fatal("end point must not be covered (half-open)")
	}
}

func TestValid(t *testing.T) {
	if !Valid(1, 2) {
		t.Fatal("1..2 should be valid")
	}
	if Valid(2, 1) {
		t.Fatal("2..1 should be invalid")
	}
}
