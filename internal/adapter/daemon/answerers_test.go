package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/event"
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

func (o *omniEngine) KillBackgroundJob(id string) bool {
	o.note("kill:" + id)
	return id == "j1"
}

func (o *omniEngine) ScheduledHere() []app.ScheduledJobInfo {
	return []app.ScheduledJobInfo{
		{Name: "tick", Schedule: "@daily", Enabled: true, Next: time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)},
		{Name: "cursed", Schedule: "not-a-cron", Problem: "unparseable schedule"},
		{Name: "off", Schedule: "@weekly"}, // disabled: no problem, no next — it never runs
	}
}

func (o *omniEngine) Subscribe(ctx context.Context, sid session.SessionID, fromSeq int64) (<-chan event.Event, func(), error) {
	ch := make(chan event.Event)
	close(ch)
	return ch, func() {}, nil
}
func (o *omniEngine) NewSince(context.Context, session.SessionID, int64) (int64, bool, error) {
	return 0, false, nil
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
		"cron": answerCron, "job-kill": answerJobKill,
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

	// The schedule reads out broken-first, and a job that never runs carries its why with an
	// empty next — never a zero time pretending to be an instant.
	// A pending edit prompt carries its diff across the status door — the app computed it once,
	// and a viewer must never recompute it.
	o.waiting = &app.Ask{ID: "c9", Kind: "permission", What: "edit", Diff: "-x\n+y"}
	if r := answerStatus(ctx, o, Request{Session: "s"}); r.Waiting == nil || r.Waiting.Diff != "-x\n+y" {
		t.Fatalf("status must carry the prompt's diff: %+v", r.Waiting)
	}
	o.waiting = nil

	if r := answerJobKill(ctx, o, Request{Name: "j1"}); !r.OK || !r.Removed ||
		o.calls[len(o.calls)-1] != "kill:j1" {
		t.Fatalf("job-kill reaches the registry and answers whether it existed: %+v", r)
	}
	if r := answerJobKill(ctx, o, Request{Name: "gone"}); !r.OK || r.Removed {
		t.Fatalf("a second press reads already-gone, never failure: %+v", r)
	}
	if r := answerJobKill(ctx, o, Request{Name: " "}); r.OK || r.Err != "no job named" {
		t.Fatalf("a nameless kill is refused before the registry: %+v", r)
	}
	if r := answerCron(ctx, o, Request{}); !r.OK || len(r.Cron) != 3 ||
		r.Cron[0].Name != "cursed" || r.Cron[0].Problem == "" || r.Cron[0].Next != "" ||
		r.Cron[1].Name != "tick" || r.Cron[1].Next == "" ||
		r.Cron[2].Name != "off" || r.Cron[2].Next != "" {
		t.Fatalf("cron: broken first, the runnable next, the switched-off last: %+v", r.Cron)
	}
}

// --- the `children` door ---------------------------------------------------

// childEngine answers ChildLister and records the parent it was asked about, so the test can
// check that the wire's `session` reached the engine as the PARENT rather than as anything else.
type childEngine struct {
	fakeEngine
	asked string
	kids  []session.SessionMeta
	err   error
}

func (c *childEngine) ChildrenOf(_ context.Context, parent string) ([]session.SessionMeta, error) {
	c.asked = parent
	return c.kids, c.err
}

// A build without the engine half says so, rather than answering an empty list.
//
// The difference is the whole reason the capability handshake exists: a screen drawing "what did
// this subagent do" must be able to tell "none were spawned" from "this daemon cannot say", and
// an empty list for both makes the second one invisible.
func TestChildrenRefusesWithAReasonWhenTheEngineCannot(t *testing.T) {
	resp := answerChildren(context.Background(), &fakeEngine{}, Request{Session: "s_1"})
	if resp.OK || resp.Err == "" {
		t.Fatalf("a daemon without the door refuses with a reason, got %+v", resp)
	}
	if resp.Children != nil {
		t.Fatalf("a refusal carries no rows, got %+v", resp.Children)
	}
}

// The parent is required. A "current conversation" default would answer a different question
// depending on who asked — and the caller that most wants this door is looking at a session that
// is not the current one.
func TestChildrenRefusesAnEmptyParent(t *testing.T) {
	eng := &childEngine{}
	resp := answerChildren(context.Background(), eng, Request{Session: "  "})
	if resp.OK || !strings.Contains(resp.Err, "session") {
		t.Fatalf("an empty parent is refused with a reason naming what is missing, got %+v", resp)
	}
	if eng.asked != "" {
		t.Fatalf("a refused request never reaches the engine, it asked about %q", eng.asked)
	}
}

// The rows carry what a screen needs to tell one child from another, newest activity first.
func TestChildrenAnswersRowsNewestFirst(t *testing.T) {
	old := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	eng := &childEngine{kids: []session.SessionMeta{
		{ID: "s_old", Agent: "spawn", Origin: "coder", Title: "fix the parser", Created: old, LastActivity: old},
		{ID: "s_new", Agent: "spawn", Origin: "meeting", Title: "which store", Created: old, LastActivity: recent},
	}}
	resp := answerChildren(context.Background(), eng, Request{Session: "s_parent"})
	if !resp.OK || len(resp.Children) != 2 {
		t.Fatalf("two children answered, got %+v", resp)
	}
	if eng.asked != "s_parent" {
		t.Fatalf("the wire's session is the PARENT, engine was asked about %q", eng.asked)
	}
	if resp.Children[0].ID != "s_new" {
		t.Fatalf("newest activity first, got %q", resp.Children[0].ID)
	}
	// **Origin** is what tells a meeting room from a delegate — measured against a live meeting,
	// where the room came back as agent="spawn" like every other child (spawnAgentName is a
	// constant). Agent says only "something else asked for this"; who asked is here.
	if resp.Children[0].Origin != "meeting" || resp.Children[1].Origin != "coder" {
		t.Fatalf("origin travels, got %q and %q", resp.Children[0].Origin, resp.Children[1].Origin)
	}
	if resp.Children[0].Agent != "spawn" {
		t.Fatalf("the child mark travels too, got %q", resp.Children[0].Agent)
	}
	if resp.Children[0].LastActivity != recent.Format(time.RFC3339) {
		t.Fatalf("timestamps are RFC3339, got %q", resp.Children[0].LastActivity)
	}
}

// No children is an ANSWER: OK with an empty list, never an omitted field with OK.
func TestChildrenAnswersNoneAsAnEmptyList(t *testing.T) {
	resp := answerChildren(context.Background(), &childEngine{}, Request{Session: "s_parent"})
	if !resp.OK {
		t.Fatalf("a parent with no children is not a failure, got %+v", resp)
	}
	if resp.Children == nil || len(resp.Children) != 0 {
		t.Fatalf("none is an empty list, not a missing field, got %+v", resp.Children)
	}
}

// Advertised only when the engine can answer — the handshake is how a client knows which screen
// to draw before it calls anything (door principle: advertise, then call).
func TestChildrenIsAdvertisedOnlyWhenTheEngineCanAnswer(t *testing.T) {
	if caps := capsOf(&fakeEngine{}); slices.Contains(caps, "children") {
		t.Fatalf("a build that cannot answer must not advertise the door: %v", caps)
	}
	caps := capsOf(&childEngine{})
	if !slices.Contains(caps, "children") {
		t.Fatalf("an engine that answers ChildLister advertises `children`, got %v", caps)
	}
}

// --- the cron edit doors ---------------------------------------------------

// cronEditEngine holds a schedule in memory: EditCron mutates it, ScheduledHere reads it back. The
// two together are what lets the door tell a refusal from a success by the FACT rather than by
// reading the engine's prose.
type cronEditEngine struct {
	fakeEngine
	jobs   []app.ScheduledJobInfo
	refuse string // when set, EditCron changes nothing and says this
}

func (e *cronEditEngine) ScheduledHere() []app.ScheduledJobInfo { return e.jobs }

func (e *cronEditEngine) EditCron(c CronEdit) (string, error) {
	if e.refuse != "" {
		return e.refuse, nil
	}
	if c.Remove {
		out := e.jobs[:0]
		for _, j := range e.jobs {
			if j.Name != c.Name {
				out = append(out, j)
			}
		}
		e.jobs = out
		return "removed " + c.Name, nil
	}
	e.jobs = append(e.jobs, app.ScheduledJobInfo{Name: c.Name, Schedule: c.Schedule,
		Prompt: c.Prompt, Enabled: true})
	return "set " + c.Name, nil
}

// The listing carries what a job ASKS. Without it a screen can say a job exists and when it runs
// next, and cannot say what it does — so "edit this job" has nowhere to start.
func TestCronCarriesThePromptAJobAsks(t *testing.T) {
	eng := &cronEditEngine{jobs: []app.ScheduledJobInfo{
		{Name: "nightly", Schedule: "0 3 * * *", Prompt: "read yesterday's commits", Enabled: true},
	}}
	resp := answerCron(context.Background(), eng, Request{})
	if !resp.OK || len(resp.Cron) != 1 {
		t.Fatalf("one job listed, got %+v", resp)
	}
	if resp.Cron[0].Prompt != "read yesterday's commits" {
		t.Fatalf("the words a job asks travel, got %q", resp.Cron[0].Prompt)
	}
}

func TestCronEditRefusesWhenTheEngineCannotWrite(t *testing.T) {
	resp := answerCronEdit(context.Background(), &fakeEngine{}, Request{Method: "cron-set", Name: "x"})
	if resp.OK || resp.Err == "" {
		t.Fatalf("a build that cannot change its schedule says so, got %+v", resp)
	}
}

// A job is named, and the name is how it is found again — an unnamed edit is refused before the
// engine is asked.
func TestCronEditRefusesAnUnnamedJob(t *testing.T) {
	eng := &cronEditEngine{}
	resp := answerCronEdit(context.Background(), eng, Request{Method: "cron-set", Name: "  "})
	if resp.OK || resp.Err == "" {
		t.Fatalf("an unnamed job is refused, got %+v", resp)
	}
	if len(eng.jobs) != 0 {
		t.Fatal("a refused request never reached the engine")
	}
}

// Writing answers with the WHOLE new listing: the caller that just edited is about to redraw, and
// a second round trip is a second chance for two answers to disagree about one job.
func TestCronSetAnswersWithTheNewListing(t *testing.T) {
	eng := &cronEditEngine{}
	resp := answerCronEdit(context.Background(), eng,
		Request{Method: "cron-set", Name: "nightly", Schedule: "0 3 * * *", Text: "read the commits"})
	if !resp.OK {
		t.Fatalf("the write succeeded, got %+v", resp)
	}
	if len(resp.Cron) != 1 || resp.Cron[0].Name != "nightly" || resp.Cron[0].Prompt != "read the commits" {
		t.Fatalf("the answer is the new listing, got %+v", resp.Cron)
	}
}

// **A refusal is told from a success by the fact, not by the prose.** The engine reports both as a
// message with a nil error (its words are written for an agent), so the door checks whether the
// world changed — and hands the message back as the reason when it did not.
func TestCronEditReadsARefusalFromTheListingRatherThanTheWords(t *testing.T) {
	eng := &cronEditEngine{refuse: "that schedule will not do: bad field"}
	resp := answerCronEdit(context.Background(), eng,
		Request{Method: "cron-set", Name: "nightly", Schedule: "not a crontab", Text: "x"})
	if resp.OK {
		t.Fatalf("nothing was written, so this is not a success: %+v", resp)
	}
	if resp.Err != "that schedule will not do: bad field" {
		t.Fatalf("the engine's reason is handed back verbatim, got %q", resp.Err)
	}
}

func TestCronRemoveIsRefusedWhenTheJobSurvives(t *testing.T) {
	eng := &cronEditEngine{
		jobs:   []app.ScheduledJobInfo{{Name: "nightly", Schedule: "0 3 * * *"}},
		refuse: "no job called nightly",
	}
	resp := answerCronEdit(context.Background(), eng, Request{Method: "cron-remove", Name: "nightly"})
	if resp.OK {
		t.Fatalf("the job is still listed, so the remove did not happen: %+v", resp)
	}
	// And the happy path: with the engine willing, the row is gone and the answer says so.
	eng.refuse = ""
	resp = answerCronEdit(context.Background(), eng, Request{Method: "cron-remove", Name: "nightly"})
	if !resp.OK || len(resp.Cron) != 0 {
		t.Fatalf("the removed job leaves the listing, got %+v", resp)
	}
}

// Advertised only when the engine can write — a screen deciding whether to draw an editor cannot
// tell an old build from a refusing one by calling and reading prose back.
func TestTheCronWriteDoorsAreAdvertisedSeparately(t *testing.T) {
	readOnly := capsOf(&omniEngine{})
	if slices.Contains(readOnly, "cron-set") {
		t.Fatalf("a build that only reads its schedule must not advertise the write doors: %v", readOnly)
	}
	caps := capsOf(&cronEditEngine{})
	for _, want := range []string{"cron-set", "cron-remove"} {
		if !slices.Contains(caps, want) {
			t.Fatalf("an engine that writes advertises %q, got %v", want, caps)
		}
	}
}

// Both write doors are serialised: read-decide-write against one config file is the shape this
// gate exists for, and two editors landing together would each write the file they read.
func TestTheCronWriteDoorsAreSerialised(t *testing.T) {
	for _, m := range []string{"cron-set", "cron-remove"} {
		if !serialControls[m] {
			t.Fatalf("%q writes a file it just read and is not behind the gate", m)
		}
	}
}

// Every advertised capability must be something a client can actually reach.
//
// A cap is a promise a screen gates on: "draw the editor, the door is there". A typo in one is a
// screen that never appears and never explains why — the call is not made, so there is no refusal
// to read either. Nothing checked that the promise had a door behind it.
//
// Four names are not methods, and each is here with its reason rather than as a blanket exemption:
// two are dispatched before the method table (they take the connection over), and two are group
// names covering several methods. A fifth would have to be added deliberately, which is the point.
func TestEveryAdvertisedCapabilityHasSomethingBehindIt(t *testing.T) {
	notMethods := map[string]string{
		"roster":       "answered before the method table — read from the listener's home directory",
		"transcript":   "turns the connection into a stream, so it is dispatched before the table",
		"settings":     "the group name for config-get / config-set / profiles",
		"tool-servers": "the group name for mcp-attach / mcp-detach",
		// Not a door at all: it marks a build whose `about` carries proto and caps, which is what
		// a peer reads to decide whether the rest of this list means anything.
		"handshake": "the marker that this build answers a handshake in the first place",
	}
	// Every optional door this build knows how to advertise, from an engine that implements all of
	// them — capsOf(nil) would answer only the constants.
	for _, cap := range capsOf(&omniEngine{}) {
		if _, ok := answers[cap]; ok {
			continue
		}
		if why, ok := notMethods[cap]; ok {
			if why == "" {
				t.Errorf("%q is exempt with no reason written down", cap)
			}
			continue
		}
		t.Errorf("capability %q is advertised and nothing answers it — a screen gated on it "+
			"never draws and never says why", cap)
	}
	// And the exemptions do not outlive what they excused: a name here that is now a real method
	// is a comment that has stopped being true.
	for cap := range notMethods {
		if _, ok := answers[cap]; ok {
			t.Errorf("%q is listed as not-a-method but the table answers it now", cap)
		}
	}
}
