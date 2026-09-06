package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A reply that is one JSON object with no tool name is not an answer and not a call — it is a
// call the model failed to phrase (gpt-oss via Ollama drops the name; Excel 2021, 2026-09-07:
// {"address":"A1","text":"…"} printed, nothing done). The finish carries MalformedCall so the loop
// asks for a repair instead of showing the JSON to the person. A readable call and ordinary prose
// do not carry it.
func TestFinishFlagsAReplyShapedLikeACallWithNoToolName(t *testing.T) {
	stream := func(t *testing.T, content string) []port.ProviderEvent {
		t.Helper()
		raw, _ := json.Marshal(content)
		body := "data: {\"choices\":[{\"delta\":{\"content\":" + string(raw) + "},\"finish_reason\":null}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"
		srv := sseServer(t, body)
		defer srv.Close()
		ch, err := New(srv.URL, "").StreamChat(context.Background(), port.ChatRequest{Model: "m",
			Tools: []port.ToolSpec{{Name: "add_comment"}, {Name: "skill"}}})
		if err != nil {
			t.Fatalf("StreamChat: %v", err)
		}
		var evs []port.ProviderEvent
		for e := range ch {
			evs = append(evs, e)
		}
		return evs
	}
	finish := func(evs []port.ProviderEvent) (port.ProviderEvent, int) {
		calls := 0
		var fin port.ProviderEvent
		for _, e := range evs {
			switch e.Type {
			case port.ProviderToolCall:
				calls++
			case port.ProviderFinish:
				fin = e
			}
		}
		return fin, calls
	}

	for _, c := range []struct {
		name, content string
		malformed     bool
		calls         int
	}{
		{"arguments only", `{"address":"A1","text":"사용자 메모"}`, true, 0},
		{"a skill's arguments only", `{"name":"sheet-design"}`, true, 0},
		{"fenced arguments only", "```json\n{\"address\":\"A1\"}\n```", true, 0},
		{"a readable fallback call", `{"name":"add_comment","arguments":{"address":"A1","text":"x"}}`, false, 1},
		{"prose", "A1 에 메모를 붙였습니다.", false, 0},
		{"an array", `[1,2,3]`, false, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			fin, calls := finish(stream(t, c.content))
			if fin.Type != port.ProviderFinish {
				t.Fatal("no finish")
			}
			if fin.MalformedCall != c.malformed || calls != c.calls {
				t.Errorf("malformed=%v calls=%d, want %v/%d", fin.MalformedCall, calls, c.malformed, c.calls)
			}
		})
	}
}

// Without any tool offered there is nothing to phrase — a JSON reply is just a JSON reply.
func TestNoToolsMeansNoMalformedCall(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"a\\\":1}\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	srv := sseServer(t, body)
	defer srv.Close()
	for _, e := range collect(t, New(srv.URL, "")) {
		if e.Type == port.ProviderFinish && e.MalformedCall {
			t.Error("a JSON reply with no tools offered was flagged as a malformed call")
		}
	}
}

func TestLooksLikeToolCall(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{`{"address":"A1","text":"x"}`, true},
		{"```json\n{\"name\":\"sheet-design\"}\n```", true},
		{"<function=bash><parameter=command>ls</parameter></function>", true},
		{"[function=read][parameter=path]/x", true},
		{"메모를 붙였습니다.", false},
		{`[1,2]`, false},
		{`42`, false},
		{"{\"a\":1} 그리고 설명", false},
		{"이 형식은 <function=bash> 처럼 씁니다.", false},
	} {
		if got := looksLikeToolCall(c.in); got != c.want {
			t.Errorf("looksLikeToolCall(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
