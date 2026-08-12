package app

import "strings"

// Where a conversation came from, when it was not a person at this keyboard.
//
// Two kinds so far and they read the same way: the actor id on the session-created event carries a
// prefix, the store lifts it into SessionMeta.Origin, and everything downstream reads one field
// instead of keeping a second ledger that can disagree. See cronActor for the other one.
const handoffOriginPrefix = "handoff:"

// HandoffActorID is the actor a session opened for another companion's request is created under.
// The label is the asker as they declared themselves, which is also what the transcript shows.
func HandoffActorID(label string) string { return handoffOriginPrefix + label }

// HandoffOrigin returns who asked, out of an actor id written by HandoffActorID.
func HandoffOrigin(actorID string) (string, bool) {
	return strings.CutPrefix(actorID, handoffOriginPrefix)
}
