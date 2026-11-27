//go:build race

package analysis

// raceEnabled reports whether the race detector is on; timing assertions are
// skipped under it because instrumentation slows execution several-fold.
const raceEnabled = true
