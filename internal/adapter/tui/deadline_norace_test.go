//go:build !race

package tui

// deadlineScale is 1 without the race detector: these deadlines are generous already, and a slow
// test is worth noticing.
const deadlineScale = 1
