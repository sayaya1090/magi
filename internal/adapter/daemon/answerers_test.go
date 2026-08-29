package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/session"
)

// omniEngine answers every optional door, recording what crossed so a test can check the wire's
// fields landed in the right parameters — the whole job of an answerer.
type omniEngine struct {
	fakeEngine
	calls []string
	ask   bool
	flags [2]bool
}

func (o *omniEngine) note(s string) { o.calls = append(o.calls, s) }

func (o *omniEngine) BackgroundJobs() []app.BackgroundJob {
	return []app.BackgroundJob{{ID: "j1", Command: "sleep 9", Running: true}}
}
func (o *omniEngine) BackgroundTail(id string, max int) string { return "tail:" + id }
func (o *omniEngine) SubagentJobs() []app.SubagentJob {
	return []app.SubagentJob{{ID: "c1", Tool: "loop", Task: "count", Running: true}}
}
func (o *omniEngine) QueuedWork() []QueuedWork { return nil }

func (o *omniEngine) Hand(_ context.Context, label, request string, looking bool) (string, error) {
	o.note("hand:" + label + ":" + request)
	return "receipt-1", nil
}
func (o *omniEngine) Handed(_ context.Context, receipt string) (Handover, error) {
	o.note("handed:" + receipt)
	return Handover{Done: true, Answer: "42"}, nil
}
func (o *omniEngine) Watch(_ context.Context, _ string, _ func(Handover) error) error { return nil }

func (o *omniEngine) ReadOnlyTool(_ context.Context, name string, args json.RawMessage) (string, error) {
	o.note("read:" + name)
	return "read-out", nil
}
func (o *omniEngine) WriteTool(_ context.Context, name string, _ json.RawMessage, ask bool) (string, error) {
	o.note("write:" + name)
	o.ask = ask
	return "write-out", nil
}
func (o *omniEngine) PatchFile(_ context.Context, path, patch string, ask bool) error {
	o.note("patch:" + path + ":" + patch)
	o.ask = ask
	return nil
}
func (o *omniEngine) Git(_ context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"branch":"main"}`), nil
}
func (o *omniEngine) GitDiff(_ context.Context, path string, staged, untracked bool) (string, error) {
	o.note("diff:" + path)
	o.flags = [2]bool{staged, untracked}
	return "diff-out", nil
}
func (o *omniEngine) FileDo(_ context.Context, what, path, to string, ask bool) error {
	o.note("filedo:" + what + ":" + path + ":" + to)
	o.ask = ask
	return nil
}
func (o *omniEngine) GitDo(_ context.Context, what, path, message string, ask bool) (string, error) {
	o.note("gitdo:" + what + ":" + path + ":" + message)
	o.ask = ask
	return "done", nil
}
func (o *omniEngine) MeetingJoin(_ context.Context, meeting, topic string) (string, string, error) {
	o.note("join:" + meeting + ":" + topic)
	return "ready-note", "s_room", nil
}
func (o *omniEngine) MeetingTurn(_ context.Context, meeting, topic, transcript string, closing bool) (Contribution, error) {
	o.note("turn:" + meeting)
	o.flags[0] = closing
	return Contribution{Said: "", Pass: true, Room: "s_room2"}, nil
}
func (o *omniEngine) LookOver(_ context.Context, path, text string) (string, error) {
	return "remark", nil
}
func (o *omniEngine) OpenPR(_ context.Context, title, body string) (string, error) {
	o.note("pr:" + title)
	return "https://pr/1", nil
}
func (o *omniEngine) PRFacts(_ context.Context) (string, error) { return `{"repo":true}`, nil }
func (o *omniEngine) DraftPR(_ context.Context, rules string) (string, error) {
	o.note("draftpr:" + rules)
	return "title\n\nbody", nil
}
func (o *omniEngine) DraftCommit(_ context.Context, rules string) (string, error) {
	return "subject", nil
}
func (o *omniEngine) CompleteCode(_ context.Context, path, prefix, suffix string) (string, app.CompleteReason, error) {
	o.note("complete:" + path + ":" + prefix + "|" + suffix)
	return "", app.CompleteUnrouted, nil
}
func (o *omniEngine) SetOpenFile(_ context.Context, path, text string) error {
	o.note("open:" + path)
	return nil
}
func (o *omniEngine) SuggestPrompt(_ context.Context, prefix string) (string, error) {
	return "…finish it like this", nil
}
func (o *omniEngine) RunShellHere(_ context.Context, cmd string) (string, int, error) {
	o.note("sh:" + cmd)
	return "out", 3, nil
}
func (o *omniEngine) SessionsHere(_ context.Context) ([]session.SessionMeta, error) {
	older := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	return []session.SessionMeta{
		{ID: "s_old", Title: "the first ask", LastActivity: older},
		{ID: "s_new", Title: "the newest ask", Model: "m1", LastActivity: newer},
	}, nil
}
func (o *omniEngine) NewSession(_ context.Context) (session.SessionID, error) {
	o.note("new-session")
	return "s_fresh", nil
}

func (o *omniEngine) About() string   { return "a companion" }
func (o *omniEngine) Version() string { return "v-test" }

// Every payload method refuses an engine without its door — in words, not a shrug. answerJobs is
// the deliberate exception: no door means no jobs, which IS the answer.
func TestAnswerersRefuseInWords(t *testing.T) {
	ctx := context.Background()
	bare := &fakeEngine{}
	for method, fn := range map[string]func(context.Context, Engine, Request) Response{
		"hand": answerHand, "tool": answerTool, "edit-file": answerEditFile,
		"git": answerGit, "file-do": answerFileDo, "git-diff": answerGitDiff,
		"git-do": answerGitDo, "meet-join": answerMeetJoin, "meet": answerMeet,
		"git-pr": answerGitPR, "pr-facts": answerPRFacts, "git-msg": answerGitMsg,
		"look-over": answerLookOver, "complete": answerComplete, "open-file": answerOpenFile,
		"suggest": answerSuggest, "shell": answerShell, "about": answerAbout,
		"sessions": answerSessions, "session-new": answerSessionNew,
	} {
		resp := fn(ctx, bare, Request{Method: method, Name: "x", Text: "y"})
		if resp.OK || resp.Err == "" {
			t.Errorf("%s: an engine without the door must refuse in words, got %+v", method, resp)
		}
	}
	if resp := answerJobs(ctx, bare, Request{Method: "jobs"}); !resp.OK || resp.Jobs == nil {
		t.Errorf("jobs: no door means no jobs, which is an answer: %+v", resp)
	}
}

// Each answerer maps the wire's fields into the door's parameters and the door's answer back —
// the two directions a relay can silently drop something in.
func TestAnswerersMapBothDirections(t *testing.T) {
	ctx := context.Background()
	o := &omniEngine{}

	if r := answerJobs(ctx, o, Request{}); !r.OK ||
		len(r.Jobs.Background) != 1 || r.Jobs.Background[0].Tail != "tail:j1" ||
		len(r.Jobs.Children) != 1 || r.Jobs.Children[0].Tool != "loop" {
		t.Fatalf("jobs must carry both kinds with their tails: %+v", r.Jobs)
	}
	if r := answerHand(ctx, o, Request{Method: "hand", Name: "design", Text: "count", Looking: true}); r.Out != "receipt-1" {
		t.Fatalf("hand answers the receipt, got %+v", r)
	}
	if r := answerHand(ctx, o, Request{Method: "hand-state", Name: "receipt-1"}); r.Handover == nil || !r.Handover.Done || r.Handover.Answer != "42" {
		t.Fatalf("hand-state answers the handover whole, got %+v", r)
	}
	if r := answerTool(ctx, o, Request{Name: "read", Args: json.RawMessage(`{}`)}); r.Out != "read-out" {
		t.Fatalf("tool: %+v", r)
	}
	if r := answerTool(ctx, o, Request{Name: "  "}); r.OK || r.Err != "no tool named" {
		t.Fatalf("a nameless tool is refused before the door: %+v", r)
	}
	if r := answerEditFile(ctx, o, Request{Name: "patch", Text: "/f.go", Answer: "@@", Ask: true}); !r.OK || !o.ask ||
		o.calls[len(o.calls)-1] != "patch:/f.go:@@" {
		t.Fatalf("patch must reach PatchFile — not fall through to a write tool named patch: %+v %v", r, o.calls)
	}
	if r := answerEditFile(ctx, o, Request{Name: "write", Ask: false}); r.Out != "write-out" {
		t.Fatalf("edit-file: %+v", r)
	}
	if r := answerGit(ctx, o, Request{}); !strings.Contains(r.Out, "branch") {
		t.Fatalf("git answers the teller's own bytes: %+v", r)
	}
	if r := answerFileDo(ctx, o, Request{Name: "rename", Text: "a", Answer: "b", Ask: true}); !r.OK ||
		o.calls[len(o.calls)-1] != "filedo:rename:a:b" {
		t.Fatalf("file-do maps what/path/to in order: %v", o.calls)
	}
	answerGitDiff(ctx, o, Request{Text: "f.go", Decision: "staged"})
	if o.flags != [2]bool{true, false} {
		t.Fatalf("git-diff: staged means staged and nothing else, got %v", o.flags)
	}
	answerGitDiff(ctx, o, Request{Text: "f.go", Decision: "untracked"})
	if o.flags != [2]bool{false, true} {
		t.Fatalf("git-diff: untracked likewise, got %v", o.flags)
	}
	if r := answerGitDo(ctx, o, Request{Name: "commit", Text: "f.go", Answer: "msg"}); r.Out != "done" ||
		o.calls[len(o.calls)-1] != "gitdo:commit:f.go:msg" {
		t.Fatalf("git-do: %v", o.calls)
	}
	if r := answerMeetJoin(ctx, o, Request{Meeting: "m1", Name: "topic"}); r.Out != "ready-note" || r.Session != "s_room" {
		t.Fatalf("meet-join answers readiness AND the room: %+v", r)
	}
	if r := answerMeet(ctx, o, Request{Meeting: "m1", Decision: "closing"}); !o.flags[0] ||
		r.Exit == nil || *r.Exit != 1 || r.Session != "s_room2" {
		t.Fatalf("meet: closing crosses, a pass travels as a flag, the room rides every turn: %+v", r)
	}
	if r := answerGitPR(ctx, o, Request{Name: "T", Text: "B"}); r.Out != "https://pr/1" {
		t.Fatalf("git-pr: %+v", r)
	}
	if r := answerPRFacts(ctx, o, Request{Method: "pr-facts"}); !strings.Contains(r.Out, "repo") {
		t.Fatalf("pr-facts: %+v", r)
	}
	if r := answerPRFacts(ctx, o, Request{Method: "pr-msg", Text: "rules"}); r.Out != "title\n\nbody" ||
		o.calls[len(o.calls)-1] != "draftpr:rules" {
		t.Fatalf("pr-msg is the draft, with the rules crossing: %+v", r)
	}
	if r := answerGitMsg(ctx, o, Request{Text: "rules"}); r.Out != "subject" {
		t.Fatalf("git-msg: %+v", r)
	}
	if r := answerLookOver(ctx, o, Request{Name: "f.go", Text: "code"}); r.Out != "remark" {
		t.Fatalf("look-over: %+v", r)
	}
	args, _ := json.Marshal(completeArgs{Prefix: "pre", Suffix: "suf"})
	if r := answerComplete(ctx, o, Request{Name: "f.go", Args: args}); !r.OK || r.Reason != "unrouted" ||
		o.calls[len(o.calls)-1] != "complete:f.go:pre|suf" {
		t.Fatalf("complete: both cursor sides and the WHY must cross: %+v %v", r, o.calls)
	}
	if r := answerOpenFile(ctx, o, Request{Name: "f.go", Text: "buf"}); !r.OK {
		t.Fatalf("open-file: %+v", r)
	}
	if r := answerSuggest(ctx, o, Request{Text: "fix the"}); r.Out == "" {
		t.Fatalf("suggest: %+v", r)
	}
	if r := answerShell(ctx, o, Request{Text: "ls"}); r.Out != "out" || r.Exit == nil || *r.Exit != 3 {
		t.Fatalf("shell answers output AND exit: %+v", r)
	}
	if r := answerShell(ctx, o, Request{Text: "  "}); r.OK || r.Err != "no command" {
		t.Fatalf("an empty command is refused before the door: %+v", r)
	}
	if r := answerAbout(ctx, o, Request{}); r.Out != "a companion" || r.Version != "v-test" ||
		r.Proto != ProtoVersion {
		t.Fatalf("about is the negotiation: text, version, proto, caps — got %+v", r)
	}

	// The session picker's two verbs: newest activity first, and a new conversation answers with
	// the id the caller must use — never invent.
	if r := answerSessions(ctx, o, Request{}); !r.OK || len(r.Sessions) != 2 ||
		r.Sessions[0].ID != "s_new" || r.Sessions[0].Model != "m1" ||
		r.Sessions[1].Title != "the first ask" {
		t.Fatalf("sessions: %+v", r.Sessions)
	}
	if r := answerSessionNew(ctx, o, Request{}); !r.OK || r.Session != "s_fresh" ||
		o.calls[len(o.calls)-1] != "new-session" {
		t.Fatalf("session-new: %+v", r)
	}
}
