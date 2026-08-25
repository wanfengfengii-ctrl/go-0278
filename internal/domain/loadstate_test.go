package domain

import "testing"

func TestBuildLoadSequence(t *testing.T) {
	levels := []LoadLevelDef{{Level: 2, TargetLoad: 200, HoldSecs: 5}, {Level: 1, TargetLoad: 100, HoldSecs: 3}}
	seq := BuildLoadSequence(levels)
	// Levels must be sorted ascending regardless of input order.
	want := []LoadStep{
		{StagePreload, 0},
		{StageLoad, 1}, {StageHold, 1},
		{StageLoad, 2}, {StageHold, 2},
		{StageUnload, 0},
		{StageRebound, 0},
	}
	if len(seq) != len(want) {
		t.Fatalf("got %d steps, want %d", len(seq), len(want))
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("step %d = %+v, want %+v", i, seq[i], want[i])
		}
	}
}

func TestValidateNextStepRejectsSkips(t *testing.T) {
	levels := []LoadLevelDef{{Level: 1, HoldSecs: 2}, {Level: 2, HoldSecs: 2}}
	// First expected step is preload; anything else is rejected.
	if ValidateNextStep(levels, 0, StageLoad, 1) {
		t.Fatal("load before preload must be rejected")
	}
	if !ValidateNextStep(levels, 0, StagePreload, 0) {
		t.Fatal("preload should be accepted first")
	}
	// After preload (1 step done) load level 1 is expected, not level 2.
	if ValidateNextStep(levels, 1, StageLoad, 2) {
		t.Fatal("load level 2 before level 1 must be rejected")
	}
	if !ValidateNextStep(levels, 1, StageLoad, 1) {
		t.Fatal("load level 1 should be accepted")
	}
}

func TestIsLoadComplete(t *testing.T) {
	levels := []LoadLevelDef{{Level: 1, HoldSecs: 1}}
	if IsLoadComplete(levels, 3) {
		t.Fatal("partial prefix must not be complete")
	}
	if !IsLoadComplete(levels, 5) {
		t.Fatal("full sequence (preload, load, hold, unload, rebound) must be complete")
	}
	if !IsLoadComplete(levels, 99) {
		t.Fatal("past-end must remain complete")
	}
}

func TestHoldLevelDef(t *testing.T) {
	levels := []LoadLevelDef{{Level: 3, TargetLoad: 300, HoldSecs: 9}}
	def, ok := HoldLevelDef(levels, 3)
	if !ok || def.HoldSecs != 9 {
		t.Fatalf("want level 3 hold 9, got %+v ok=%v", def, ok)
	}
	if _, ok := HoldLevelDef(levels, 4); ok {
		t.Fatal("unknown level must not resolve")
	}
}
