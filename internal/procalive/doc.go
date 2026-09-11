// Package procalive answers one question — is this pid running — for the two callers that must not
// disagree about it.
//
// It lives here rather than inside either caller because both are deciding whether somebody else's
// record is stale: the daemon, reading a published companion's pid, and the updater, reading the pid
// of the generation that was watching a new build. A second spelling would let one of them conclude
// a live process is gone, and the action on that conclusion is destructive in both places — deleting
// a claim, or rolling a good build back.
package procalive
