//go:build !race

package engine

// raceEnabled reports whether the test binary was built with the race
// detector, under which timings mean nothing.
const raceEnabled = false
