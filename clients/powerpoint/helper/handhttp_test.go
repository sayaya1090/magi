package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sseReader 는 스트림에서 프레임 하나를 읽는다. 시험이 텍스트 조각을 손으로 파싱하지 않게.
type sseReader struct {
	br *bufio.Reader
}

func (r *sseReader) next(t *testing.T) (string, []byte) {
	t.Helper()
	var event string
	for {
		line, err := r.br.ReadString('\n')
		if err != nil {
			t.Fatalf("스트림이 끊겼다: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			return event, []byte(strings.TrimPrefix(line, "data: "))
		}
	}
}

// 애드인이 붙고, 조작이 스트림으로 내려가고, 답이 POST 로 올라온다.
//
// 이 시험이 재는 것은 **배선 전체**다: 붙는 것이 곧 등록이고(§5.5 — 따로 알리는 프레임이 없다),
// 첫 프레임이 문서 키이며(그 값이 도구의 `document` 인자다), 답이 기다리던 호출로 간다.
func TestTheAddinAttachesAndAnswersOverTheStream(t *testing.T) {
	hub := NewHandHub()
	hub.Timeout = 3 * time.Second
	hh := &HandHTTP{Hub: hub, PingEvery: time.Hour}
	mux := http.NewServeMux()
	mux.HandleFunc(handStreamPath, hh.Stream)
	mux.HandleFunc(handReplyPath, hh.Reply)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+handStreamPath+"?presentation=p1&label=q3.pptx", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type 이 %q 다", ct)
	}
	sr := &sseReader{br: bufio.NewReader(resp.Body)}

	event, data := sr.next(t)
	if event != "hello" {
		t.Fatalf("첫 프레임이 %q 다", event)
	}
	var hello struct {
		Document string `json:"document"`
		Label    string `json:"label"`
	}
	if err := json.Unmarshal(data, &hello); err != nil {
		t.Fatal(err)
	}
	if hello.Document == "" || hello.Label != "q3.pptx" {
		t.Fatalf("첫 프레임이 %s 다", data)
	}

	// 이제 도구 호출을 흉내 낸다.
	done := make(chan error, 1)
	var got HandResult
	go func() {
		res, err := hub.Call(context.Background(), hello.Document, "read_slide", map[string]any{"slide": 2})
		got = res
		done <- err
	}()

	event, data = sr.next(t)
	if event != "call" {
		t.Fatalf("조작 프레임이 %q 다: %s", event, data)
	}
	var call HandRequest
	if err := json.Unmarshal(data, &call); err != nil {
		t.Fatal(err)
	}
	if call.Op != "read_slide" || call.ID == "" {
		t.Fatalf("조작이 %+v 다", call)
	}

	body, _ := json.Marshal(HandReply{
		ID: call.ID, Document: hello.Document, Label: "q3.pptx",
		Result: map[string]any{"placeholders": []any{}}, Epoch: 3, Count: 1,
	})
	rr, err := http.Post(srv.URL+handReplyPath, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	rr.Body.Close()
	if rr.StatusCode != http.StatusNoContent {
		t.Fatalf("답을 %d 로 받았다", rr.StatusCode)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got.Document != hello.Document || got.Revision == nil || !got.Revision.Known {
		t.Fatalf("결과가 %+v 다", got)
	}
}

// 아무도 안 기다리는 답은 **버리고, 버렸다고 말한다**(410). 200 으로 답하면 애드인은 자기
// 답이 갔다고 믿는다 — 늦게 온 답이 다음 호출의 답으로 소비되는 것을 막는 자리라 조용하면 안 된다.
func TestAnAnswerNobodyWaitsForIsRefusedOutLoud(t *testing.T) {
	hub := NewHandHub()
	hh := &HandHTTP{Hub: hub}
	conn := hub.Join("p1", "q3.pptx")
	srv := httptest.NewServer(http.HandlerFunc(hh.Reply))
	defer srv.Close()

	body, _ := json.Marshal(HandReply{ID: "r-nope", Document: conn.key})
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("%d 로 답했다 — 410 이어야 한다", resp.StatusCode)
	}
}

// 스트림이 끊기면 그 손은 떠난 것이다. **따로 작별 프레임을 안 쓴다**(§5.5).
func TestClosingTheStreamIsHowAHandLeaves(t *testing.T) {
	hub := NewHandHub()
	hh := &HandHTTP{Hub: hub, PingEvery: 10 * time.Millisecond}
	srv := httptest.NewServer(http.HandlerFunc(hh.Stream))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"?presentation=p1&label=q3.pptx", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sr := &sseReader{br: bufio.NewReader(resp.Body)}
	sr.next(t) // hello
	if !hub.Attached() {
		t.Fatal("붙었는데 안 붙었다고 한다")
	}
	cancel()
	resp.Body.Close()

	deadline := time.Now().Add(2 * time.Second)
	for hub.Attached() {
		if time.Now().After(deadline) {
			t.Fatal("연결이 끊겼는데 손이 남아 있다")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// 토큰이 걸려 있으면 스트림도 답도 못 연다(§8).
func TestTheHandDoorWantsTheToken(t *testing.T) {
	hub := NewHandHub()
	hh := &HandHTTP{Hub: hub, Token: "s3cret", PingEvery: time.Hour}
	mux := http.NewServeMux()
	mux.HandleFunc(handStreamPath, hh.Stream)
	mux.HandleFunc(handReplyPath, hh.Reply)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + handStreamPath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("토큰 없이 스트림이 %d 로 열렸다", resp.StatusCode)
	}

	// `EventSource` 는 헤더를 못 실어서 스트림만 쿼리로 받는다. 그 자리가 실제로 열리는지 잰다.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+handStreamPath+"?token=s3cret", nil)
	ok, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer ok.Body.Close()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("토큰을 쿼리로 냈는데 %d 다", ok.StatusCode)
	}
}

// 대화 프레임이 **같은 연결**로 내려간다(§5.7 — 새 포트도 새 연결도 없다).
func TestTheTranscriptRidesTheSameStream(t *testing.T) {
	hub := NewHandHub()
	feed := make(chan StreamFrame, 1)
	hh := &HandHTTP{Hub: hub, PingEvery: time.Hour, Feed: func(string) <-chan StreamFrame { return feed }}
	srv := httptest.NewServer(http.HandlerFunc(hh.Stream))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"?presentation=p1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sr := &sseReader{br: bufio.NewReader(resp.Body)}
	sr.next(t) // hello

	feed <- StreamFrame{Kind: "event", Data: json.RawMessage(`{"type":"part.delta","seq":0}`)}
	event, data := sr.next(t)
	if event != "event" || !strings.Contains(string(data), "part.delta") {
		t.Fatalf("대화 프레임이 %q %s 다", event, data)
	}
}
