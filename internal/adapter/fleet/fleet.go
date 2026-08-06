// Package fleet answers one question for every magi running on this machine: what is it doing?
//
// It exists because two surfaces ask it — the browser dashboard and `magi --agents` — and a state
// derived twice is a state that will eventually be derived differently. The rule this repository
// keeps relearning is that the second copy does not disagree on the day it is written; it disagrees
// six weeks later, when only one of them is updated.
package fleet

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Reader is the part of the engine a fleet view needs: the log, and nothing that runs a turn.
// Declared here rather than taking *app.App so it is visible that this package only READS.
type Reader interface {
	UnfinishedTurnOf(ctx context.Context, sid session.SessionID) (app.UnfinishedTurn, bool)
	SessionState(ctx context.Context, sid session.SessionID) ([]session.Message, int64, error)
	ListSessions(ctx context.Context, workdir string) ([]session.SessionMeta, error)
}

// State is what an agent is doing, as far as anyone outside its process can tell.
type State string

const (
	// Working: the socket answers and a turn is open.
	Working State = "working"
	// Idle: the socket answers and the last turn finished.
	Idle State = "idle"
	// Waiting: the daemon is blocked on a human — a permission prompt or a question. This is the
	// one state that cannot be read from the log (see the note below), and the one that means
	// nothing moves until somebody comes.
	Waiting State = "waiting"
	// Abandoned: nobody is listening and a turn was left open — a crash, a kill, a closed laptop.
	// Every other view renders this identically to a finished session, which is why it is here.
	Abandoned State = "abandoned"
	// Stopped: nobody is listening and the last turn finished.
	Stopped State = "stopped"
)

// Four of the five come from the log and the socket, which outlive the process that made them, so a
// dead daemon is still described. Waiting cannot: a permission prompt is a question about what
// should happen rather than a record of what did, so it is never written down, and the event that
// announced it went to the bus of the process that is blocked. Guessing it from "an open turn that
// has not moved in a while" would flag every slow build. So it is ASKED — the dial that proves a
// daemon alive asks while the connection is open — and it is the only state that needs the daemon
// to be there to be reported, which is right: a daemon that is gone is not waiting for you.

// Agent is one running (or lately running) magi.
type Agent struct {
	Socket  string `json:"socket"`
	Workdir string `json:"workdir"`
	Name    string `json:"name"` // the workspace's base directory — what a person calls it
	Session string `json:"session"`
	PID     int    `json:"pid"`
	Live    bool   `json:"live"`
	State   State  `json:"state"`
	Asking  string `json:"asking"`  // what it is blocked on, when State is waiting
	AskID   string `json:"askId"`   // the call id an answer must carry
	AskKind string `json:"askKind"` // "permission" | "question"
	Task    string `json:"task"`    // what the open turn asked for, or the last thing said
	Steps   int    `json:"steps"`   // tool calls the open turn has made — what a crash would cost
	Idle    int    `json:"idle"`    // seconds since the last event in the log; -1 if unknown
	Here    bool   `json:"here"`    // the daemon in the directory the caller is standing in
}

// List describes every daemon published under configDir, newest first. here is the socket the
// caller considers its own (empty if none); it is only marked, never filtered on.
func List(ctx context.Context, r Reader, configDir, here string) ([]Agent, error) {
	found, err := daemon.List(configDir)
	if err != nil {
		return nil, err
	}
	// One ListSessions per distinct workspace, not per daemon: several daemons in one tree is a
	// normal thing to do, and re-reading the directory for each of them is the same answer again.
	seen := map[string][]session.SessionMeta{}
	out := make([]Agent, 0, len(found))
	for _, in := range found {
		a := Agent{
			Socket: in.Socket, Workdir: in.Workdir, Name: filepath.Base(in.Workdir),
			Session: in.Session, PID: in.PID, Live: in.Live, Here: here != "" && in.Socket == here,
			Idle: -1,
		}
		if a.Name == "" || a.Name == "." || a.Name == string(filepath.Separator) {
			a.Name = in.Workdir
		}
		sid := session.SessionID(in.Session)
		metas, ok := seen[in.Workdir]
		if !ok {
			metas, _ = r.ListSessions(ctx, in.Workdir)
			seen[in.Workdir] = metas
		}
		for _, m := range metas {
			if m.ID == sid && !m.LastActivity.IsZero() {
				a.Idle = int(time.Since(m.LastActivity).Seconds())
			}
		}
		open, isOpen := r.UnfinishedTurnOf(ctx, sid)
		switch {
		case in.Live && in.Asking != nil:
			// Blocked on a person beats anything the log says: the log shows an open turn, which
			// is true and is not the thing that needs doing about it.
			a.State, a.Steps, a.Asking = Waiting, open.Steps, describeAsk(in.Asking)
			a.AskID, a.AskKind = in.Asking.ID, in.Asking.Kind
			a.Task = Clip(open.Text, 160)
		case in.Live && isOpen:
			a.State, a.Steps, a.Task = Working, open.Steps, Clip(open.Text, 160)
		case in.Live:
			a.State = Idle
		case isOpen:
			a.State, a.Steps, a.Task = Abandoned, open.Steps, Clip(open.Text, 160)
		default:
			a.State = Stopped
		}
		if a.Task == "" {
			a.Task = Clip(lastSaid(ctx, r, sid), 160)
		}
		out = append(out, a)
	}
	return out, nil
}

// lastSaid is the final piece of text in a session — what an idle agent left behind.
func lastSaid(ctx context.Context, r Reader, sid session.SessionID) string {
	msgs, _, err := r.SessionState(ctx, sid)
	if err != nil {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		for j := len(msgs[i].Parts) - 1; j >= 0; j-- {
			if p := msgs[i].Parts[j]; p.Kind == session.PartText && p.Text != "" {
				return p.Text
			}
		}
	}
	return ""
}

// Clip shortens s to at most n bytes on a rune boundary, marking that it was cut.
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8ValidCut(s, n) {
		n--
	}
	return s[:n] + "…"
}

// utf8ValidCut reports whether s may be cut at byte i without splitting a rune.
func utf8ValidCut(s string, i int) bool {
	return i <= 0 || i >= len(s) || s[i]&0xC0 != 0x80
}

// commandOf digs the human-meaningful field out of a tool call's arguments. Best effort by design:
// an unrecognised shape falls back to the tool name rather than dumping JSON onto a card.
func commandOf(args []byte) string {
	if len(args) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return ""
	}
	for _, k := range []string{"command", "cmd", "url", "path", "file_path"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// describeAsk turns a pending prompt into the line a person reads on a card.
func describeAsk(w *daemon.Waiting) string {
	if w == nil {
		return ""
	}
	switch w.Kind {
	case "permission":
		// The command, not just the tool. "permission: bash" asks somebody to approve a category;
		// the decision is about what it is going to run.
		if cmd := commandOf(w.Args); cmd != "" {
			if w.Reason != "" {
				return w.What + ": " + Clip(cmd, 100) + "  (" + w.Reason + ")"
			}
			return w.What + ": " + Clip(cmd, 120)
		}
		return "permission: " + w.What
	case "question":
		return Clip(w.What, 120)
	}
	return Clip(w.What, 120)
}
