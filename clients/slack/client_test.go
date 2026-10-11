package slackclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/clients/appbridge"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/slack-go/slack"
)

type fakeBridge struct {
	mu          sync.Mutex
	targets     []string
	requests    []appbridge.Request
	bindingIDs  []string
	streams     map[string]chan event.Event
	failRequest bool
}

func (b *fakeBridge) Connect(_ context.Context, target string) (appbridge.Binding, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.targets = append(b.targets, target)
	b.streams[target] = make(chan event.Event, 10)
	return appbridge.Binding{ID: target, Session: target, Target: target}, nil
}
func (b *fakeBridge) Request(_ context.Context, id string, r appbridge.Request) (json.RawMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failRequest {
		return nil, errors.New("temporary")
	}
	b.requests = append(b.requests, r)
	b.bindingIDs = append(b.bindingIDs, id)
	return json.RawMessage(`{}`), nil
}
func (b *fakeBridge) Events(ctx context.Context, id string, _ int64, each func(event.Event) bool) error {
	b.mu.Lock()
	ch := b.streams[id]
	b.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-ch:
			if !each(e) {
				return nil
			}
		}
	}
}

type fakeTransport struct {
	mu       sync.Mutex
	posts    []string
	failures int
	reads    int
	messages []slack.Message
}

func (f *fakeTransport) PostMessageContext(_ context.Context, ch string, _ ...slack.MsgOption) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failures > 0 {
		f.failures--
		return "", "", errors.New("temporary")
	}
	f.posts = append(f.posts, ch)
	return ch, "1", nil
}
func (f *fakeTransport) GetConversationRepliesContext(_ context.Context, p *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.messages != nil {
		return f.messages, false, "", nil
	}
	return []slack.Message{{Msg: slack.Msg{Text: p.ChannelID + "/" + p.Timestamp}}}, false, "", nil
}
func (f *fakeTransport) PublishViewContext(context.Context, slack.PublishViewContextRequest) (*slack.ViewResponse, error) {
	return &slack.ViewResponse{}, nil
}
func fixture() (*Client, *fakeBridge, *fakeTransport) {
	b := &fakeBridge{streams: map[string]chan event.Event{}}
	f := &fakeTransport{}
	return New(b, f, "BOT"), b, f
}
func TestThreadIsolationReplayAndNoUnsolicitedMessages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, b, f := fixture()
	a := Input{EventID: "a", Team: "T", User: "U", Channel: "C", Thread: "1", Text: "hello"}
	other := a
	other.EventID = "b"
	other.Thread = "2"
	for _, i := range []Input{a, a, other} {
		if err := c.Handle(ctx, i); err != nil {
			t.Fatal(err)
		}
	}
	b.mu.Lock()
	if len(b.requests) != 2 || len(b.targets) != 2 {
		t.Fatal("event dedup or thread isolation failed")
	}
	ch := b.streams[key(a)]
	b.mu.Unlock()
	f.mu.Lock()
	if len(f.posts) != 0 {
		t.Fatal("unsolicited external message")
	}
	f.mu.Unlock()
	data, _ := json.Marshal(event.PartAppendedData{Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: "hello"}})
	e := event.Event{Seq: 1, SessionID: session.SessionID(key(a)), Type: event.TypePartAppended, Data: data}
	wrong := e
	wrong.SessionID = "other"
	old := e
	old.Seq = 0
	old.TS = time.Now().Add(-time.Hour)
	ch <- old
	ch <- wrong
	ch <- e
	ch <- e
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n := len(f.posts)
		f.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 1 || f.posts[0] != "C" {
		t.Fatalf("posts=%v", f.posts)
	}
}
func TestApprovalIsBoundAndSingleUse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, b, _ := fixture()
	i := Input{EventID: "a", Team: "T", User: "U", Channel: "C", Thread: "1", Text: "hello"}
	if err := c.Handle(ctx, i); err != nil {
		t.Fatal(err)
	}
	cv := c.conversations[key(i)]
	c.ask(ctx, cv, "call", "approve", []string{"allow", "deny"}, true)
	token := ""
	sibling := ""
	for k, p := range c.actions {
		if p.decision == "deny" {
			sibling = k
		}
		if p.decision == "allow" {
			token = k
		}
	}
	for _, bad := range []Input{{Team: "T", User: "OTHER", Channel: "C", Thread: "1"}, {Team: "OTHER", User: "U", Channel: "C", Thread: "1"}, {Team: "T", User: "U", Channel: "C", Thread: "2"}} {
		if c.Action(ctx, bad, token) == nil {
			t.Fatal("cross-user/team/thread approval accepted")
		}
	}
	if err := c.Action(ctx, i, token); err != nil {
		t.Fatal(err)
	}
	if c.Action(ctx, i, sibling) == nil {
		t.Fatal("sibling approval replayed")
	}
	if c.Action(ctx, i, token) == nil {
		t.Fatal("replayed approval")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.requests) != 2 || b.requests[1].Decision != "allow" {
		t.Fatal("missing bound approval")
	}
}
func TestToolsOnlyReadBoundThreadAndDraft(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _, f := fixture()
	i := Input{EventID: "a", Team: "T", User: "U", Channel: "C", Thread: "1", Text: "hello"}
	if err := c.Handle(ctx, i); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(ctx, "unbound", "slack_read_thread", nil); err == nil {
		t.Fatal("unbound read")
	}
	if _, err := c.Call(ctx, key(i), "slack_read_thread", json.RawMessage(`{"channel":"OTHER"}`)); err != nil {
		t.Fatal(err)
	}
	result, err := c.Call(ctx, key(i), "slack_draft_message", json.RawMessage(`{"text":"draft"}`))
	if err != nil || result.(map[string]any)["sent"] != false {
		t.Fatal("draft contract")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 0 || f.reads != 1 {
		t.Fatal("draft sent or unexpected read")
	}
}

func TestSlackHTTPPayloadPlainBoundedThreadReply(t *testing.T) {
	var text, thread, markdown string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		text = r.Form.Get("text")
		thread = r.Form.Get("thread_ts")
		markdown = r.Form.Get("mrkdwn")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true,"channel":"C","ts":"2"}`)
	}))
	defer server.Close()
	api := slack.New("test-only", slack.OptionAPIURL(server.URL+"/"))
	c := New(nil, api, "BOT")
	cv := &conversation{input: Input{Channel: "C", Thread: "1"}}
	if err := c.post(context.Background(), cv, "<!channel> "+strings.Repeat("가", 4000)); err != nil {
		t.Fatal(err)
	}
	if thread != "1" || markdown != "false" || strings.Contains(text, "<!channel>") || !strings.Contains(text, "&lt;!channel&gt;") || len([]rune(text)) > 2800 {
		t.Fatalf("unsafe/unbounded Slack payload thread=%s markdown=%s length=%d", thread, markdown, len([]rune(text)))
	}
}
func TestFreeAnswerCannotEscapeThread(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, b, _ := fixture()
	i := Input{EventID: "a", Team: "T", User: "U", Channel: "C", Thread: "1", Text: "hello"}
	if err := c.Handle(ctx, i); err != nil {
		t.Fatal(err)
	}
	cv := c.conversations[key(i)]
	c.ask(ctx, cv, "q", "Question", nil, false)
	i.EventID = "answer"
	i.Text = "/answer q yes"
	if err := c.Handle(ctx, i); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requests[len(b.requests)-1].Answer != "yes" {
		t.Fatal("free answer not routed")
	}
}

func TestThreadToolBoundsTextWithoutAttachments(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _, f := fixture()
	i := Input{EventID: "a", Team: "T", User: "U", Channel: "C", Thread: "1", Text: "read"}
	if err := c.Handle(ctx, i); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 30; n++ {
		f.messages = append(f.messages, slack.Message{Msg: slack.Msg{Text: strings.Repeat("가", 2000)}})
	}
	out, err := c.Call(ctx, key(i), "slack_read_thread", nil)
	if err != nil {
		t.Fatal(err)
	}
	result := out.(map[string]any)
	if result["truncated"] != true {
		t.Fatal("missing truncation marker")
	}
	total := 0
	for _, m := range result["messages"].([]map[string]any) {
		total += len([]rune(m["text"].(string)))
	}
	if total > 12000 {
		t.Fatal("unbounded thread output")
	}
}

func TestActionRetainedAfterBridgeFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, b, _ := fixture()
	i := Input{EventID: "a", Team: "T", User: "U", Channel: "C", Thread: "1", Text: "hello"}
	if err := c.Handle(ctx, i); err != nil {
		t.Fatal(err)
	}
	cv := c.conversations[key(i)]
	if err := c.ask(ctx, cv, "call", "approve", []string{"allow", "deny"}, true); err != nil {
		t.Fatal(err)
	}
	token := ""
	for k, p := range c.actions {
		if p.decision == "allow" {
			token = k
		}
	}
	b.mu.Lock()
	b.failRequest = true
	b.mu.Unlock()
	if c.Action(ctx, i, token) == nil {
		t.Fatal("failure missing")
	}
	b.mu.Lock()
	b.failRequest = false
	b.mu.Unlock()
	if err := c.Action(ctx, i, token); err != nil {
		t.Fatal("lost retry", err)
	}
}
func TestFailedDeliveryRetriesSameEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, b, f := fixture()
	i := Input{EventID: "a", Team: "T", User: "U", Channel: "C", Thread: "1", Text: "hello"}
	if err := c.Handle(ctx, i); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.failures = 1
	f.mu.Unlock()
	data, _ := json.Marshal(event.PartAppendedData{Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: "retry"}})
	b.streams[key(i)] <- event.Event{Seq: 1, SessionID: session.SessionID(key(i)), Type: event.TypePartAppended, Data: data}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n := len(f.posts)
		f.mu.Unlock()
		if n == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("failed event lost")
}

func TestFreeAnswerRetainedAfterBridgeFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, b, _ := fixture()
	i := Input{EventID: "a", Team: "T", User: "U", Channel: "C", Thread: "1", Text: "hello"}
	if err := c.Handle(ctx, i); err != nil {
		t.Fatal(err)
	}
	if err := c.ask(ctx, c.conversations[key(i)], "q", "question", nil, false); err != nil {
		t.Fatal(err)
	}
	i.EventID = "reply"
	i.Text = "/answer q yes"
	b.mu.Lock()
	b.failRequest = true
	b.mu.Unlock()
	if c.Handle(ctx, i) == nil {
		t.Fatal("missing bridge failure")
	}
	b.mu.Lock()
	b.failRequest = false
	b.mu.Unlock()
	if err := c.Handle(ctx, i); err != nil {
		t.Fatal("answer/event retry lost", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requests[len(b.requests)-1].Answer != "yes" {
		t.Fatal("dedup silently discarded retry")
	}
}

func TestChannelPostingScheduleHonorsCancellation(t *testing.T) {
	c, _, f := fixture()
	cv := &conversation{input: Input{Channel: "C", Thread: "1"}}
	if err := c.post(context.Background(), cv, "first"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.post(ctx, cv, "second") == nil {
		t.Fatal("channel scheduler ignored cancellation")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 1 {
		t.Fatal("posted inside one-second channel limit")
	}
}
