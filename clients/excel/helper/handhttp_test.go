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

	req, _ := http.NewRequest("GET", srv.URL+handStreamPath+"?workbook=p1&label=q3.pptx", nil)
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
		res, err := hub.Call(context.Background(), hello.Document, "read_range", map[string]any{"slide": 2})
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
	if call.Op != "read_range" || call.ID == "" {
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
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"?workbook=p1&label=q3.pptx", nil)
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
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"?workbook=p1", nil)
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

// 2021 판은 손(COM 프로세스)과 화면(창)이 다른 연결이다. 화면이 role=viewer 로 붙으면 hello 와 전사는 받되
// 호출은 손에게만 간다 — 같은 키의 연결을 허브가 하나로 봐서 호출이 화면으로 새면 손이 영영 못 받는다.
func TestAViewerSeesTheStreamButNeverGetsACall(t *testing.T) {
	hub := NewHandHub()
	hub.Timeout = 3 * time.Second
	hh := &HandHTTP{Hub: hub, PingEvery: time.Hour}
	mux := http.NewServeMux()
	mux.HandleFunc(handStreamPath, hh.Stream)
	mux.HandleFunc(handReplyPath, hh.Reply)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 손이 없는데 보러 오면 404 — 볼 것이 없다.
	if resp, err := http.Get(srv.URL + handStreamPath + "?workbook=p1&role=viewer"); err != nil {
		t.Fatal(err)
	} else if resp.Body.Close(); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("손 없는 문서를 보러 왔는데 %d 다", resp.StatusCode)
	}

	hand, err := http.Get(srv.URL + handStreamPath + "?workbook=p1&label=q3.pptx")
	if err != nil {
		t.Fatal(err)
	}
	defer hand.Body.Close()
	hr := &sseReader{br: bufio.NewReader(hand.Body)}
	_, data := hr.next(t)
	var hello struct {
		Document string `json:"document"`
	}
	if err := json.Unmarshal(data, &hello); err != nil {
		t.Fatal(err)
	}

	viewer, err := http.Get(srv.URL + handStreamPath + "?workbook=p1&role=viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Body.Close()
	if viewer.StatusCode != http.StatusOK {
		t.Fatalf("보는 연결이 %d 다", viewer.StatusCode)
	}
	vr := &sseReader{br: bufio.NewReader(viewer.Body)}
	if event, _ := vr.next(t); event != "hello" {
		t.Fatalf("보는 연결의 첫 프레임이 %q 다", event)
	}

	// 호출 하나 — 손에게만 간다. 화면 쪽 채널에는 아무것도 오지 않는다.
	done := make(chan error, 1)
	go func() {
		_, err := hub.Call(context.Background(), hello.Document, "list_sheets", nil)
		done <- err
	}()
	event, data := hr.next(t)
	if event != "call" {
		t.Fatalf("손에게 %q 가 왔다", event)
	}
	var call struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(data, &call)
	body := `{"id":"` + call.ID + `","document":"` + hello.Document + `","result":{"count":1},"epoch":1}`
	if resp, err := http.Post(srv.URL+handReplyPath, "application/json", strings.NewReader(body)); err != nil {
		t.Fatal(err)
	} else {
		resp.Body.Close()
	}
	if err := <-done; err != nil {
		t.Fatalf("손이 답했는데 호출이 %v 로 끝났다", err)
	}
	got := make(chan string, 1)
	go func() { // vr.next 는 끊기면 t.Fatal 을 부르므로 여기선 날것으로 읽는다 — 아래에서 일부러 닫는다
		for {
			line, err := vr.br.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "event:") {
				got <- strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			}
		}
	}()
	select {
	case ev := <-got:
		t.Fatalf("보는 연결에 %q 가 왔다 — 화면이 호출을 받으면 안 된다", ev)
	case <-time.After(300 * time.Millisecond):
	}

	// 화면이 떠나도 손은 그대로 붙어 있다.
	viewer.Body.Close()
	time.Sleep(50 * time.Millisecond)
	if hub.Peek("p1") == nil {
		t.Fatal("화면이 떠났는데 손의 자리가 비었다")
	}
}

// 2021 판의 창은 자기 덱 이름을 태그(PowerPointApi 1.3)로 짓는데 그 호스트에는 태그 칸이 없어 빈 이름을 들고 오고,
// COM 손은 파일 경로 지문으로 붙는다 — 둘의 키가 같을 길이 없다. 키가 같아야만 보게 하면 2021 의 창은 영영
// 아무것도 못 본다(2026-09-05 실물). 보는 연결은 자기 키의 손이 없으면 **있는 손**을 본다.
func TestAViewerWithAnotherKeyWatchesTheHandThatIsThere(t *testing.T) {
	hub := NewHandHub()
	hh := &HandHTTP{Hub: hub, PingEvery: time.Hour}
	mux := http.NewServeMux()
	mux.HandleFunc(handStreamPath, hh.Stream)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	hand, err := http.Get(srv.URL + handStreamPath + "?workbook=com-3f9a&label=ltsc.pptx")
	if err != nil {
		t.Fatal(err)
	}
	defer hand.Body.Close()
	hr := &sseReader{br: bufio.NewReader(hand.Body)}
	_, data := hr.next(t)
	var hello struct {
		Document string `json:"document"`
	}
	_ = json.Unmarshal(data, &hello)

	for _, q := range []string{"?workbook=doc-from-a-tagless-host&role=viewer", "?role=viewer"} {
		viewer, err := http.Get(srv.URL + handStreamPath + q)
		if err != nil {
			t.Fatal(err)
		}
		if viewer.StatusCode != http.StatusOK {
			t.Fatalf("%s: 보는 연결이 %d 다 — 손이 있는데 못 본다", q, viewer.StatusCode)
		}
		vr := &sseReader{br: bufio.NewReader(viewer.Body)}
		_, vd := vr.next(t)
		var seen struct {
			Document string `json:"document"`
		}
		_ = json.Unmarshal(vd, &seen)
		if seen.Document != hello.Document {
			t.Fatalf("%s: 창이 %q 를 보는데 손은 %q 다", q, seen.Document, hello.Document)
		}
		viewer.Body.Close()
	}
}

// 보는 연결의 첫 규칙은 **같은 키**다. 더 최근에 붙은 다른 손이 있어도 자기 키의 손이 이긴다 — 폴백은
// 자기 키의 손이 없을 때만이다.
func TestAViewerWithItsOwnKeyIgnoresANewerHand(t *testing.T) {
	hub := NewHandHub()
	hh := &HandHTTP{Hub: hub, PingEvery: time.Hour}
	mux := http.NewServeMux()
	mux.HandleFunc(handStreamPath, hh.Stream)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	docOf := func(resp *http.Response) string {
		r := &sseReader{br: bufio.NewReader(resp.Body)}
		_, data := r.next(t)
		var hello struct {
			Document string `json:"document"`
		}
		_ = json.Unmarshal(data, &hello)
		return hello.Document
	}
	mine, err := http.Get(srv.URL + handStreamPath + "?workbook=mine&label=a.pptx")
	if err != nil {
		t.Fatal(err)
	}
	defer mine.Body.Close()
	myDoc := docOf(mine)
	time.Sleep(20 * time.Millisecond) // 다른 손이 더 최근이 되게
	newer, err := http.Get(srv.URL + handStreamPath + "?workbook=newer&label=b.pptx")
	if err != nil {
		t.Fatal(err)
	}
	defer newer.Body.Close()
	_ = docOf(newer)

	viewer, err := http.Get(srv.URL + handStreamPath + "?workbook=mine&role=viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Body.Close()
	if got := docOf(viewer); got != myDoc {
		t.Fatalf("자기 키의 손이 있는데 창이 %q 를 본다(자기 것은 %q)", got, myDoc)
	}
}
