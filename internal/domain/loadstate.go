package domain

import (
	"errors"
	"sort"
)

// ErrStageOutOfOrder is returned when a reading does not match the single
// expected next action of a bolt's load prefix.
var ErrStageOutOfOrder = errors.New("domain: stage out of order")

// LoadStep is one action in the strictly ordered pullout procedure. Load levels
// use 0 for preload, unload and rebound, and 1..N for the locked load levels.
type LoadStep struct {
	Stage LoadStage
	Level int
}

// BuildLoadSequence returns the full, strictly ordered action sequence for a
// bolt given its locked load level definitions. The sequence is: preload, then
// for each level in ascending order a load and a hold, then unload and rebound.
// The input levels are sorted by level first so the result never depends on the
// caller's ordering.
func BuildLoadSequence(levels []LoadLevelDef) []LoadStep {
	sorted := make([]LoadLevelDef, len(levels))
	copy(sorted, levels)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Level < sorted[j].Level })

	steps := []LoadStep{{Stage: StagePreload}}
	for _, l := range sorted {
		steps = append(steps,
			LoadStep{Stage: StageLoad, Level: l.Level},
			LoadStep{Stage: StageHold, Level: l.Level},
		)
	}
	steps = append(steps,
		LoadStep{Stage: StageUnload},
		LoadStep{Stage: StageRebound},
	)
	return steps
}

// ExpectedStep returns the next action in the sequence after completed steps
// have already been recorded. It reports ok=false when the sequence is complete,
// meaning no further action may be accepted.
func ExpectedStep(levels []LoadLevelDef, completed int) (LoadStep, bool) {
	seq := BuildLoadSequence(levels)
	if completed < 0 || completed >= len(seq) {
		return LoadStep{}, false
	}
	return seq[completed], true
}

// IsLoadComplete reports whether completed steps exhaust the whole sequence.
func IsLoadComplete(levels []LoadLevelDef, completed int) bool {
	return completed >= len(BuildLoadSequence(levels))
}

// ValidateNextStep reports whether a proposed step (stage, level) is exactly the
// single expected next action. It never permits skipping, repeating or moving
// backwards.
func ValidateNextStep(levels []LoadLevelDef, completed int, stage LoadStage, level int) bool {
	next, ok := ExpectedStep(levels, completed)
	if !ok {
		return false
	}
	return next.Stage == stage && next.Level == level
}

// HoldLevelDef returns the load level definition for the given level, or false
// when the level is not one of the locked load levels. This lets callers obtain
// the target load and required hold duration for a hold confirmation.
func HoldLevelDef(levels []LoadLevelDef, level int) (LoadLevelDef, bool) {
	for _, l := range levels {
		if l.Level == level {
			return l, true
		}
	}
	return LoadLevelDef{}, false
}
