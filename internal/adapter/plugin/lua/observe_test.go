package lua

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// syncLog is a mutex-guarded log sink: FireEventWith handlers run on the host's
// background event worker, so the test must not race the writer.
type syncLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *syncLog) logf(s string) { l.mu.Lock(); defer l.mu.Unlock(); l.b.WriteString(s + "\n") }
func (l *syncLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func waitLog(t *testing.T, l *syncLog, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(l.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("log never contained %q; got:\n%s", want, l.String())
}

// FireEventWith delivers the payload table to a user_message handler,
// asynchronously (the caller does not block on the handler).
func TestObservationEventPayload(t *testing.T) {
	log := &syncLog{}
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), Logf: log.logf})
	dir := writePlugin(t, `name="obs"`+"\n"+`capabilities=["tool"]`,
		`magi.on("user_message", function(ev)
			magi.log("got " .. ev.session .. ": " .. ev.text)
		end)
		magi.on("turn_finished", function(ev)
			magi.log("done " .. ev.session .. ": " .. ev.text)
		end)`,
	)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	h.FireEventWith("user_message", map[string]string{"session": "s_1", "text": "fix the bug"})
	waitLog(t, log, "got s_1: fix the bug")
	h.FireEventWith("turn_finished", map[string]string{"session": "s_1", "text": "fixed"})
	waitLog(t, log, "done s_1: fixed")
}

// A handler error is logged, not fatal, and later events still deliver.
func TestObservationHandlerErrorIsIsolated(t *testing.T) {
	log := &syncLog{}
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), Logf: log.logf})
	dir := writePlugin(t, `name="obs2"`+"\n"+`capabilities=["tool"]`,
		`magi.on("user_message", function(ev)
			if ev.text == "boom" then error("kaboom") end
			magi.log("ok " .. ev.text)
		end)`,
	)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	h.FireEventWith("user_message", map[string]string{"session": "s", "text": "boom"})
	h.FireEventWith("user_message", map[string]string{"session": "s", "text": "next"})
	waitLog(t, log, "ok next") // the errored handler didn't wedge the worker
	if !strings.Contains(log.String(), "kaboom") {
		t.Errorf("handler error should be logged: %q", log.String())
	}
}

// fakeAnalyzer scripts magi.analyze replies.
type fakeAnalyzer struct {
	reply string
	err   error
	mu    sync.Mutex
	sys   string
	text  string
}

func (f *fakeAnalyzer) Analyze(_ context.Context, system, text, _ string) (string, error) {
	f.mu.Lock()
	f.sys, f.text = system, text
	f.mu.Unlock()
	return f.reply, f.err
}

// magi.analyze forwards system/text to the analyzer and returns its reply;
// magi.json_decode parses the JSON reply into a table.
func TestAnalyzeAndJSONDecode(t *testing.T) {
	log := &syncLog{}
	fa := &fakeAnalyzer{reply: `{"lesson":{"task":"t","outcome":"success"}}`}
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), Analyzer: fa, Logf: log.logf})
	dir := writePlugin(t, `name="ana"`+"\n"+`capabilities=["analyze"]`,
		`magi.on("startup", function()
			local raw, err = magi.analyze{system="be terse", text="window"}
			if not raw then magi.log("analyze err: " .. tostring(err)); return end
			local obj, jerr = magi.json_decode(raw)
			if not obj then magi.log("json err: " .. tostring(jerr)); return end
			magi.log("outcome=" .. obj.lesson.outcome)
		end)`,
	)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	h.FireEvent("startup")
	if !strings.Contains(log.String(), "outcome=success") {
		t.Fatalf("analyze+json_decode pipeline failed: %q", log.String())
	}
	fa.mu.Lock()
	defer fa.mu.Unlock()
	if fa.sys != "be terse" || fa.text != "window" {
		t.Errorf("analyzer got system=%q text=%q", fa.sys, fa.text)
	}
}

// analyze without an analyzer, with empty text, and on analyzer error all fail
// softly (nil, err) — a plugin must be able to ignore failures.
func TestAnalyzeFailurePaths(t *testing.T) {
	log := &syncLog{}
	script := `magi.on("startup", function()
		local raw, err = magi.analyze{system="s", text=%q}
		magi.log("res=" .. tostring(raw) .. " err=" .. tostring(err))
	end)`

	// No analyzer configured.
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), Logf: log.logf})
	dir := writePlugin(t, `name="na"`+"\n"+`capabilities=["analyze"]`,
		strings.ReplaceAll(script, "%q", `"x"`))
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	h.FireEvent("startup")
	if !strings.Contains(log.String(), "no analyzer available") {
		t.Fatalf("missing-analyzer error not surfaced: %q", log.String())
	}

	// Empty text.
	log2 := &syncLog{}
	h2 := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), Analyzer: &fakeAnalyzer{reply: "r"}, Logf: log2.logf})
	dir2 := writePlugin(t, `name="na2"`+"\n"+`capabilities=["analyze"]`,
		strings.ReplaceAll(script, "%q", `"  "`))
	if _, err := h2.Load(context.Background(), dir2); err != nil {
		t.Fatal(err)
	}
	h2.FireEvent("startup")
	if !strings.Contains(log2.String(), "text is required") {
		t.Fatalf("empty-text error not surfaced: %q", log2.String())
	}

	// Analyzer error.
	log3 := &syncLog{}
	h3 := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), Analyzer: &fakeAnalyzer{err: errors.New("backend down")}, Logf: log3.logf})
	dir3 := writePlugin(t, `name="na3"`+"\n"+`capabilities=["analyze"]`,
		strings.ReplaceAll(script, "%q", `"x"`))
	if _, err := h3.Load(context.Background(), dir3); err != nil {
		t.Fatal(err)
	}
	h3.FireEvent("startup")
	if !strings.Contains(log3.String(), "backend down") {
		t.Fatalf("analyzer error not surfaced: %q", log3.String())
	}
}
