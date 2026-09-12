package idebridge

import (
	"slices"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// How long the two steps before the read may take. Separate from History's silence bound, which
// cannot apply until the stream exists: these cover connecting and the handshake.
const (
	rowsConnect = 2 * time.Second
	rowsAsk     = 5 * time.Second
)

// rows answers a conversation as the lines a screen shows.
//
// ⚠ **The rule was here and nothing could ask for it.** `Rows` has been in this package since
// 2026-09-10, 700 lines checked row-for-row against the TypeScript original, and `Methods()`
// answered `about`, `activity` and `daemon` — so the one copy of the third of the eight was
// unreachable while two clients went on deriving it separately in 856 lines of TypeScript and 868
// of Kotlin. A rule nobody can call looks exactly like no rule at all; this repository has now paid
// for that shape three times in one week (Response.instance, Published.instance, Move.Asked).
//
// # Why the whole conversation and not a cursor
//
// The fold reaches BACKWARDS. A reply clears the waiting mark on the prompt above it, a tool result
// lands on its call's row, a resurfaced interjection moves its original — see the comment on `Rows`.
// Folding a tail therefore does not produce the tail of folding the whole: the rows that would have
// been reached back into are not in the slice. So this door takes no `since`, and
// `TestTheFoldIsWholeLogAndTheDoorSaysSo` measures the difference rather than trusting this
// paragraph. A screen that wants to append while a turn runs keeps shaping its own live frames; what
// it gets here is the one thing both clients rebuild by hand — replay when a window opens.
//
// # Why its own connection
//
// `Transcript` is given the connection for as long as the caller keeps reading (daemon.Client's own
// note says a reader opens a connection of its own), and the daemon ends the stream by closing it.
// Reusing `b.conn` would hand the NEXT request a dead socket, so this dials its own and closes it.
func (b *bridge) rows(req request) {
	if strings.TrimSpace(req.Session) == "" {
		b.fail(req.ID, "the rows method needs a session: {\"method\":\"rows\",\"session\":\"s_…\"}")
		return
	}
	// ⚠ **The bound has to start at the dial, not after the handshake.** A companion that accepts the
	// connection and then never answers `about` held this door for ever — the silence bound inside
	// History does not exist yet at that point, and the bridge answers requests in order, so every
	// later request waited behind it. Two different failures, two bounds: a socket file whose owner is
	// gone in a way that leaves connect hanging, and a peer that accepted and went quiet.
	//
	// Short, because both steps are local and neither waits on a model: the daemon either has the
	// workspace open or it does not. A person is waiting on this reply.
	c, err := daemon.DialWithin(b.socket, rowsConnect, rowsAsk)
	if err != nil {
		b.fail(req.ID, err.Error())
		return
	}
	defer c.Close()
	// ⚠ **Ask whether this companion can END the replay, not whether it can stream one.**
	// `History` stops at the marker an older daemon never sends, so gating on "transcript" would
	// make this door hang instead of answering — the failure measured on 2026-09-12 before the
	// marker existed (202s, killed). A sentence about an older companion is a thing a person can
	// act on; a call that never returns is not.
	peer, err := c.Hello()
	if err != nil {
		b.fail(req.ID, err.Error())
		return
	}
	if !slices.Contains(peer.Caps, "history") {
		b.fail(req.ID, "this companion streams a transcript but cannot say where the replay ends "+
			"(no \"history\" capability) — it is an older build than this bridge; restart it onto "+
			"this binary and the rows method works")
		return
	}
	events, err := c.History(req.Session)
	if err != nil {
		b.fail(req.ID, err.Error())
		return
	}
	// `events` is sent alongside the rows because they are different numbers and a client that sees
	// only one cannot tell "this conversation is short" from "most of it does not become a row" —
	// and most of it legitimately does not (context.usage and todos.changed are other screens').
	rows := Rows(events)
	if rows == nil {
		// [] and null are different answers to "what does this conversation look like": an empty
		// list is a conversation with nothing to draw, null reads as a field this build does not
		// implement. The same distinction `about` keeps for caps.
		rows = []Row{}
	}
	b.reply(map[string]any{"id": req.ID, "ok": true, "rows": rows, "events": len(events)})
}
