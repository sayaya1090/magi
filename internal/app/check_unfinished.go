package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// `absent` asks that a pattern NOT be in a file. On the output of a command that never finished,
// that is true for a reason that has nothing to do with the work: the log simply stops before the
// point where the pattern would have appeared.
//
// Observed live. The agent verified its own fix with
//
//	cd /app/ocaml && make -j 4 world opt 2>&1 | tee build.log | tail -50
//
// and magi said exactly what was wrong with it — "this exit 0 is `tail -50`'s, not the command
// before the pipe — that command's own status is not reported here" — while the captured output was
// still mid-compile. The step's check was
//
//	absent (Segmentation fault|Fatal error|crash|Assertion failed)  on  /app/ocaml/build.log
//
// and the fresh `tee` had replaced the previous build's log with one that had not yet reached a
// crash. The absence was about how far the log got, not about the build.
//
// The rule uses only what magi produced itself: it ran the command, it recorded the exit, and it
// already detects when that exit belongs to a pipe tail rather than to the command. Nothing here
// reads the file's contents — an `exit=0` line INSIDE a log is exactly the kind of self-reported
// text this exists to stop crediting.
//
// Deliberately not filename-shaped ("does this look like a log"). That is a whitelist, and the next
// build will write somewhere it does not cover.

// unfinishedRunNote returns why the command that produced p cannot be confirmed to have finished,
// or "" when it can (or when no command in this run produced it).
func (a *App) unfinishedRunNote(ctx context.Context, sid session.SessionID, p string) string {
	cmd, res, ok := a.lastBashProducing(ctx, sid, p)
	if !ok {
		return "" // nothing in this run redirected output here; other gates own that case
	}
	exit, hasExit := exitOfBashResult(res)
	switch {
	case strings.Contains(decodeResultText(res), "[timed out"):
		return fmt.Sprintf("the command that wrote it (`%s`) TIMED OUT — the log stops where the "+
			"deadline cut it, not where the work ended", clipLine(cmd, 90))
	case hasExit && exit != 0:
		return fmt.Sprintf("the command that wrote it (`%s`) exited %d — the log is of a run that "+
			"did not succeed", clipLine(cmd, 90), exit)
	case builtin.ExitStatusMasked(exit, cmd):
		return fmt.Sprintf("the exit reported for the command that wrote it (`%s`) belongs to its "+
			"tail, not to the command — magi never learned whether that command finished", clipLine(cmd, 90))
	}
	return ""
}

// lastBashProducing finds the most recent bash call in this run — the gating session or anything
// beneath it — that redirected output into p, and returns its command and result text.
func (a *App) lastBashProducing(ctx context.Context, sid session.SessionID, p string) (string, string, bool) {
	sessions := append([]session.SessionID{sid}, a.descendantsOf(sid)...)
	var bestTS time.Time
	bestCmd, bestRes, found := "", "", false
	for _, s := range sessions {
		evs := a.readEventsBestEffort(ctx, s)
		if len(evs) > provenanceScanCap {
			evs = evs[len(evs)-provenanceScanCap:]
		}
		results := map[string]string{} // callID → result text
		for _, e := range evs {
			if e.Type != event.TypePartAppended {
				continue
			}
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			if d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil {
				results[d.Part.ToolResult.CallID] = string(d.Part.ToolResult.Content)
			}
		}
		for _, e := range evs {
			if e.Type != event.TypePartAppended {
				continue
			}
			var d event.PartAppendedData
			if json.Unmarshal(e.Data, &d) != nil || d.Part.Kind != session.PartToolCall || d.Part.ToolCall == nil {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(d.Part.ToolCall.Name), "bash") {
				continue
			}
			var args struct{ Command string }
			if json.Unmarshal(d.Part.ToolCall.Args, &args) != nil {
				continue
			}
			hit := false
			for _, w := range bashWritePaths(args.Command) {
				if samePath(w, p) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
			// Newest wins: a later run of the same build replaces the log the check will read.
			if !found || e.TS.After(bestTS) {
				bestTS, bestCmd, bestRes, found = e.TS, args.Command, results[d.Part.ToolCall.CallID], true
			}
		}
	}
	return bestCmd, bestRes, found
}

// exitOfBashResult reads the `exit N` the bash tool puts at the head of its result. Returns
// (0, false) when the result carries none — a background start, or a shape this does not know —
// so an unreadable result is never mistaken for a clean exit.
func exitOfBashResult(res string) (int, bool) {
	s := strings.TrimSpace(decodeResultText(res))
	if !strings.HasPrefix(s, "exit ") {
		return 0, false
	}
	f := strings.Fields(s)
	if len(f) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(f[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// decodeResultText unwraps a tool result's stored form. Content is a json.RawMessage, so a bash
// result arrives as a JSON STRING — quoted, with its newlines escaped — and reading it as raw text
// leaves `exit 0\noutput: …` glued into one token. Anything that is not a JSON string (a structured
// result from another tool) is returned unchanged.
func decodeResultText(res string) string {
	var s string
	if json.Unmarshal([]byte(res), &s) == nil {
		return s
	}
	return res
}
