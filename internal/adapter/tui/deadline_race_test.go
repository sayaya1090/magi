//go:build race

package tui

// Under the race detector everything here runs an order of magnitude slower: these tests drive a
// real loop against a real store, and the whole package's turns are in flight at once. A fixed
// wall-clock deadline then fails on machine load rather than on anything the code did — which was
// observed as one 20-second timeout in a full `-race` run that passed on its own three times over.
//
// Scaled rather than removed: a deadline is what keeps a genuine deadlock from hanging the suite.
const deadlineScale = 8
