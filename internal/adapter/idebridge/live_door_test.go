package idebridge

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/event"
)

// liveDaemon is a companion that replays and then keeps talking.
//
// The other fake in this package answers one line per request, which is all a one-shot door needs.
// A live door needs the other half: frames that arrive AFTER the reply, on the daemon's schedule.
type liveDaemon struct {
	t      *testing.T
	path   string
	ln     net.Listener
	replay []event.Event
	caps   []string
	// gap 은 재생 프레임 사이의 간격. 0 이면 한 번에 다 보낸다.
	gap time.Duration

	mu        sync.Mutex
	followers []net.Conn
}

func liveListen(t *testing.T, replay []event.Event, caps ...string) *liveDaemon {
	t.Helper()
	if caps == nil {
		caps = []string{"handshake", "transcript", "history"}
	}
	dir, err := os.MkdirTemp("", "idl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	p := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", p)
	if err != nil {
		t.Fatal(err)
	}
	d := &liveDaemon{t: t, path: p, ln: ln, replay: replay, caps: caps}
	go d.accept()
	t.Cleanup(func() { ln.Close() })
	return d
}

func (d *liveDaemon) accept() {
	for {
		c, err := d.ln.Accept()
		if err != nil {
			return
		}
		go d.serve(c)
	}
}

func (d *liveDaemon) serve(c net.Conn) {
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		raw := sc.Text()
		switch {
		case strings.Contains(raw, `"method":"about"`):
			hi, _ := json.Marshal(map[string]any{"ok": true, "version": "test", "proto": 1, "caps": d.caps})
			io.WriteString(c, string(hi)+"\n")
		case strings.Contains(raw, `"method":"transcript"`) && slices.Contains(d.caps, "say-nothing"):
			// 받고 조용해진다 — 프레임을 하나도 안 보낸다.
			d.mu.Lock()
			d.followers = append(d.followers, c)
			d.mu.Unlock()
		case strings.Contains(raw, `"method":"transcript"`):
			for i := range d.replay {
				frame, _ := json.Marshal(map[string]any{"ok": true, "event": d.replay[i]})
				io.WriteString(c, string(frame)+"\n")
				if d.gap > 0 {
					time.Sleep(d.gap)
				}
			}
			if slices.Contains(d.caps, "history") {
				io.WriteString(c, `{"ok":true,"live":true}`+"\n")
			}
			d.mu.Lock()
			d.followers = append(d.followers, c)
			d.mu.Unlock()
		case strings.Contains(raw, `"method":"boom"`):
			// 전송이 깨지는 요청: 답 없이 연결을 닫는다. `ok:false` 는 **정상 답**이라 이 갈래를
			// 못 만든다 — forward 가 연결을 의심하는 그 자리를 재려면 진짜로 끊어야 한다.
			c.Close()
			return
		default:
			io.WriteString(c, `{"ok":false,"error":"this fake serves about and transcript"}`+"\n")
		}
	}
}

// push sends a live event to everyone following.
func (d *liveDaemon) push(e event.Event) {
	frame, _ := json.Marshal(map[string]any{"ok": true, "event": e})
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.followers {
		io.WriteString(c, string(frame)+"\n")
	}
}

// over ends the stream the way a daemon that is stopping on purpose does — it says so first.
func (d *liveDaemon) over() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.followers {
		io.WriteString(c, `{"ok":true,"over":true}`+"\n")
	}
}

// hangUp ends the stream the way a daemon that went down does.
func (d *liveDaemon) hangUp() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.followers {
		c.Close()
	}
	d.followers = nil
}

// driver drives the bridge the way an editor does, over time: requests can be written after replies
// have been read, which is the whole difference between a door and a subscription.
type driver struct {
	t     *testing.T
	in    *io.PipeWriter
	lines chan string
	done  chan int
}

func drive(t *testing.T, sock string) *driver {
	t.Helper()
	pr, pw := io.Pipe()
	outR, outW := io.Pipe()
	d := &driver{t: t, in: pw, lines: make(chan string, 64), done: make(chan int, 1)}
	b := &bridge{workspace: "/ws", socket: sock, out: outW}
	go func() {
		code := b.serve(pr, io.Discard)
		b.shutdown()
		outW.Close()
		d.done <- code
	}()
	go func() {
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for sc.Scan() {
			d.lines <- sc.Text()
		}
		close(d.lines)
	}()
	t.Cleanup(func() { pw.Close(); outR.Close() })
	return d
}

func (d *driver) send(line string) {
	d.t.Helper()
	if _, err := io.WriteString(d.in, line+"\n"); err != nil {
		d.t.Fatalf("요청을 못 보냈다: %v", err)
	}
}

// next is the next line the bridge wrote, or a failure naming what we were waiting for.
func (d *driver) next(what string) map[string]any {
	d.t.Helper()
	select {
	case l, ok := <-d.lines:
		if !ok {
			d.t.Fatalf("%s 를 기다리는데 브리지가 말을 끝냈다", what)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			d.t.Fatalf("%s 자리에 JSON 이 아닌 줄이 왔다: %q", what, l)
		}
		return m
	case <-time.After(5 * time.Second):
		d.t.Fatalf("%s 가 5초 안에 오지 않았다", what)
	}
	return nil
}

func rowsOf(t *testing.T, frame map[string]any, key string) []Row {
	t.Helper()
	raw, err := json.Marshal(frame[key])
	if err != nil {
		t.Fatal(err)
	}
	var rows []Row
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("%s 가 행 목록이 아니다: %v", key, err)
	}
	return rows
}

func opsOf(t *testing.T, frame map[string]any) []Op {
	t.Helper()
	raw, err := json.Marshal(frame["ops"])
	if err != nil {
		t.Fatal(err)
	}
	var ops []Op
	if err := json.Unmarshal(raw, &ops); err != nil {
		t.Fatalf("ops 가 낱말 목록이 아니다: %v", err)
	}
	return ops
}

func shortBatch(t *testing.T) {
	t.Helper()
	was := rowsBatch
	rowsBatch = 5 * time.Millisecond
	t.Cleanup(func() { rowsBatch = was })
}

// **The contract, end to end over the wire.**
//
// A screen that subscribed before a turn started and a screen that opened a window after it ended
// must draw the same conversation. Everything else in this file is about connections and lifetimes;
// this is the one that says the door is worth having.
func TestALiveSubscriptionDrawsWhatAFreshWindowWould(t *testing.T) {
	shortBatch(t)
	replay := []event.Event{
		mk(1, "prompt.submitted", map[string]any{"messageId": "u1",
			"parts": []any{map[string]any{"kind": "text", "text": "what does this do"}}}, nil),
		mk(2, "part.appended", map[string]any{"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "text", "text": "reading it now"}}, nil),
	}
	d := liveListen(t, replay)
	ed := drive(t, d.path)

	ed.send(`{"id":1,"method":"rows","session":"s_1","live":true}`)
	first := ed.next("구독의 첫 답")
	if first["ok"] != true || first["sub"] == nil {
		t.Fatalf("첫 답이 구독이 아니다: %v", first)
	}
	held := rowsOf(t, first, "rows")
	if want := Rows(replay); listJSON(t, held) != listJSON(t, want) {
		t.Fatalf("첫 프레임이 지금 창을 연 답과 다르다\n받은 쪽: %s\n한 번짜리: %s",
			listJSON(t, held), listJSON(t, want))
	}

	// 이제 살아 있는 사건들. 조각 하나, 사실 하나, 그리고 뒤로 손을 뻗는 갈래(도구 결과).
	live := []event.Event{
		mk(0, "part.delta", map[string]any{"messageId": "m2", "kind": "text", "text": "it folds a log\n"}, nil),
		mk(0, "part.delta", map[string]any{"messageId": "m2", "kind": "text", "text": "into rows"}, nil),
		mk(5, "part.appended", map[string]any{"messageId": "m2", "role": "assistant",
			"part": map[string]any{"kind": "text", "text": "it folds a log\ninto rows"}}, nil),
		mk(6, "part.appended", map[string]any{"messageId": "m2", "role": "assistant",
			"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{
				"callId": "c1", "name": "bash", "args": map[string]any{"command": "go test ./..."}}}}, nil),
		mk(7, "part.appended", map[string]any{"messageId": "m2", "role": "tool",
			"part": map[string]any{"kind": "tool-result", "toolResult": map[string]any{
				"callId": "c1", "content": "ok", "isError": false}}}, nil),
		mk(8, "turn.finished", map[string]any{}, nil),
	}
	for i := range live {
		d.push(live[i])
	}

	want := Rows(append(append([]event.Event{}, replay...), live...))
	deadline := time.Now().Add(4 * time.Second)
	frames := 0
	for listJSON(t, held) != listJSON(t, want) && time.Now().Before(deadline) {
		f := ed.next("차이 프레임")
		if f["ops"] == nil {
			t.Fatalf("차이가 아닌 프레임이 왔다: %v", f)
		}
		frames++
		held = Apply(held, opsOf(t, f))
	}
	if listJSON(t, held) != listJSON(t, want) {
		t.Fatalf("차이를 %d 개 받고도 지금 창을 연 답과 다르다\n받은 쪽: %s\n접기: %s",
			frames, listJSON(t, held), listJSON(t, want))
	}
	t.Logf("사건 %d 개가 프레임 %d 개로 왔다 — 묶어 보낸다는 뜻이다", len(live), frames)
	if frames > len(live) {
		t.Errorf("사건 %d 개에 프레임이 %d 개다 — 묶음이 일하지 않는다", len(live), frames)
	}

	// ⚠ **멈춤의 답과 구독의 끝은 순서가 계약이 아니다.** 둘을 쓰는 고루틴이 다르다 — 요청을 처리하는
	// 쪽과 그 구독의 송신기. 그래서 둘이 다 오는 것을 재고, 어느 쪽이 먼저인지는 재지 않는다. 순서를
	// 단언하면 그 시험은 계약에 없는 것을 못박고, 어느 날 부하에서 빨개진다.
	ed.send(`{"id":2,"method":"stop","sub":1}`)
	okSeen, doneSeen := false, false
	for !okSeen || !doneSeen {
		f := ed.next("멈춤의 답과 끝났다는 프레임")
		switch {
		case f["id"] != nil:
			if f["ok"] != true {
				t.Fatalf("멈춤이 거절됐다: %v", f)
			}
			okSeen = true
		case f["done"] == true:
			doneSeen = true
		case f["ops"] != nil:
			// 멈추라는 말과 지나가던 차이 프레임이 엇갈릴 수 있다 — 그것은 정상이다.
		default:
			t.Fatalf("멈춤을 기다리는데 다른 것이 왔다: %v", f)
		}
	}
}

// **A stream that ends says so.**
//
// ⚠ A subscription that simply stops is indistinguishable from a conversation where nothing is
// happening, and a screen would go on claiming to be live while the daemon is gone — the asymmetric
// lie this tree has paid for before: telling somebody when something ENDS but not when it broke
// leaves the last thing said on screen permanently false.
func TestAStreamThatDiesSaysSo(t *testing.T) {
	shortBatch(t)
	d := liveListen(t, []event.Event{
		mk(1, "prompt.submitted", map[string]any{"messageId": "u1",
			"parts": []any{map[string]any{"kind": "text", "text": "hi"}}}, nil),
	})
	ed := drive(t, d.path)
	ed.send(`{"id":1,"method":"rows","session":"s_1","live":true}`)
	if f := ed.next("구독의 첫 답"); f["sub"] == nil {
		t.Fatalf("구독이 안 섰다: %v", f)
	}
	d.hangUp()
	f := ed.next("끝났다는 프레임")
	if f["done"] != true {
		t.Fatalf("스트림이 죽었는데 끝났다고 말하지 않는다: %v", f)
	}
	// ⚠ **사유까지 와야 한다.** 실물에서 데몬을 죽여 보니(2026-09-14) 소켓이 오류 없이 닫혀
	// `{"done":true}` 만 갔다 — 화면은 「구독이 끝났다」는 듣고 「컴패니언이 사라졌다」는 못 듣는다.
	// 끝났다는 말만으로는 조용한 대화와 구별되지 않는다는 것이 이 문의 약속이었고, 그 약속의
	// 나머지 절반이 이 줄이다.
	if why, _ := f["why"].(string); strings.TrimSpace(why) == "" {
		t.Errorf("스트림이 죽었는데 사유가 없다: %v", f)
	}
}

// **An older companion is refused with a sentence, not a subscription that never speaks.**
func TestALiveSubscriptionRefusesACompanionThatCannotEndTheReplay(t *testing.T) {
	d := liveListen(t, nil, "handshake", "transcript")
	ed := drive(t, d.path)
	ed.send(`{"id":1,"method":"rows","session":"s_1","live":true}`)
	f := ed.next("거절")
	if f["ok"] != false || !strings.Contains(str(f, "error"), "history") {
		t.Fatalf("옛 컴패니언을 사유와 함께 거절하지 않는다: %v", f)
	}
}

// **Stopping something that is not there is said out loud.**
func TestStoppingAnUnknownSubscriptionIsAnAnswer(t *testing.T) {
	d := liveListen(t, nil)
	ed := drive(t, d.path)
	ed.send(`{"id":1,"method":"stop","sub":7}`)
	f := ed.next("모르는 구독의 답")
	if f["ok"] != false || !strings.Contains(str(f, "error"), "7") {
		t.Fatalf("없는 구독을 멈추라는 말에 사유가 없다: %v", f)
	}
}

// **A person sees the answer WHILE it is written.**
//
// This is what drafts are for, and the only way to know it works is to stop halfway: push the chunks,
// take the frame, and look for a row that is marked a draft. Without it a long answer on a local model
// is a frozen panel above a status that says "working" — the failure the draft rows were added for.
func TestAStreamedAnswerIsDrawnBeforeItIsFinished(t *testing.T) {
	shortBatch(t)
	replay := []event.Event{
		mk(1, "prompt.submitted", map[string]any{"messageId": "u1",
			"parts": []any{map[string]any{"kind": "text", "text": "explain it"}}}, nil),
	}
	d := liveListen(t, replay)
	ed := drive(t, d.path)
	ed.send(`{"id":1,"method":"rows","session":"s_1","live":true}`)
	held := rowsOf(t, ed.next("구독의 첫 답"), "rows")

	d.push(mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "it folds "}, nil))
	d.push(mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "a log"}, nil))
	held = Apply(held, opsOf(t, ed.next("초안의 차이")))
	draft := -1
	for i, r := range held {
		if r.Draft {
			draft = i
		}
	}
	if draft < 0 {
		t.Fatalf("답이 흐르는 중인데 초안 행이 없다 — 화면이 멈춰 보인다: %s", listJSON(t, held))
	}
	if held[draft].Text != "it folds a log" {
		t.Errorf("초안이 조각을 다 안 들고 있다: %q", held[draft].Text)
	}
	if held[draft].ID == "" {
		t.Errorf("초안에 이름이 없다 — 다음 프레임이 이 줄을 지목할 수 없다: %s", listJSON(t, held))
	}

	// 그리고 사실이 오면 그 자리가 사실로 바뀐다.
	d.push(mk(4, "part.appended", map[string]any{"messageId": "m1", "role": "assistant",
		"part": map[string]any{"kind": "text", "text": "it folds a log"}}, nil))
	want := Rows([]event.Event{
		replay[0],
		mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "it folds "}, nil),
		mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "a log"}, nil),
		mk(4, "part.appended", map[string]any{"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "text", "text": "it folds a log"}}, nil),
	})
	deadline := time.Now().Add(3 * time.Second)
	for listJSON(t, held) != listJSON(t, want) && time.Now().Before(deadline) {
		held = Apply(held, opsOf(t, ed.next("사실의 차이")))
	}
	if listJSON(t, held) != listJSON(t, want) {
		t.Fatalf("사실이 온 뒤 화면이 접기와 다르다\n받은 쪽: %s\n접기: %s",
			listJSON(t, held), listJSON(t, want))
	}
	for _, r := range held {
		if r.Draft {
			t.Errorf("사실이 왔는데 초안 행이 남아 있다 — 답이 두 번 그려진다: %s", listJSON(t, held))
		}
	}
}

// **Changes are sent by the batch, not by the event.**
//
// ⚠ The contract test above cannot hold this: it counts frames, and how many frames a burst becomes
// depends on when the events happen to land — on a busy machine the same burst can be one frame or
// five, so an assertion about the count would be a coin toss. Here the interval is made LONG instead.
// Five events written to a socket take microseconds; the window is 300ms; so if they arrive as more
// than one frame, it is the batching that is broken and not the timing that was unlucky.
//
// Why it matters, measured: folding a conversation is proportional to its length (157ms at 20000
// events) and the difference between two folds costs about the same again. Per-event sending pays that
// per chunk of a streamed answer — the reason this door gathers.
func TestChangesAreSentByTheBatchNotByTheEvent(t *testing.T) {
	was := rowsBatch
	rowsBatch = 300 * time.Millisecond
	t.Cleanup(func() { rowsBatch = was })

	replay := []event.Event{
		mk(1, "prompt.submitted", map[string]any{"messageId": "u1",
			"parts": []any{map[string]any{"kind": "text", "text": "go"}}}, nil),
	}
	d := liveListen(t, replay)
	ed := drive(t, d.path)
	ed.send(`{"id":1,"method":"rows","session":"s_1","live":true}`)
	held := rowsOf(t, ed.next("구독의 첫 답"), "rows")

	live := []event.Event{
		mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "one\n"}, nil),
		mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "two\n"}, nil),
		mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "three\n"}, nil),
		mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "four\n"}, nil),
		mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "five\n"}, nil),
	}
	for i := range live {
		d.push(live[i])
	}
	held = Apply(held, opsOf(t, ed.next("한 묶음의 차이")))
	want := Rows(append(append([]event.Event{}, replay...), live...))
	if listJSON(t, held) != listJSON(t, want) {
		t.Fatalf("사건 다섯이 한 프레임으로 오지 않았다(또는 그 프레임이 다섯을 다 안 담았다)\n"+
			"받은 쪽: %s\n접기: %s", listJSON(t, held), listJSON(t, want))
	}
}

// nopPipe is a connection that reads nothing and writes nowhere — enough to make a daemon.Client
// that can be closed.
type nopPipe struct{}

func (nopPipe) Read([]byte) (int, error)    { return 0, io.EOF }
func (nopPipe) Write(b []byte) (int, error) { return len(b), nil }
func (nopPipe) Close() error                { return nil }

// **A subscription says it is over even when its reader vanishes without a word.**
//
// ⚠ This is the race that a full parallel suite found and an isolated run did not (2026-09-13).
// `stop` closes the stop channel AND the connection, so the reader's own "the stream ended" frame can
// lose its race against the closed channel and never be pushed — and then the only thing the sender
// sees is a closed channel. The branch that noticed that used to return in silence, so the client got
// its `ok` for the stop and waited for ever for a `done` that was never coming.
//
// Forced rather than waited for: the frames channel is closed here WITHOUT a done frame, which is
// exactly what the reader does when it loses that race. No timing, no load, no flake.
func TestASubscriptionSaysDoneEvenIfItsReaderVanishes(t *testing.T) {
	var out strings.Builder
	b := &bridge{workspace: "/ws", out: &out, subs: map[int]*rowsSub{}}
	sub := &rowsSub{id: 4, conn: daemon.Over(nopPipe{}), stop: make(chan struct{})}
	b.subs[sub.id] = sub

	frames := make(chan liveFrame, 4)
	// 재생이 끝난 데까지 간다 — 그 전이면 답해야 할 것은 요청의 실패다.
	frames <- liveFrame{caughtUp: true}
	close(frames)
	b.pump(request{ID: 1, Session: "s_1"}, sub, frames)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("첫 답과 끝났다는 프레임, 둘이어야 하는데 %d 줄이다: %q", len(lines), out.String())
	}
	var done map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &done); err != nil {
		t.Fatalf("끝났다는 자리에 JSON 이 아닌 줄이 왔다: %q", lines[1])
	}
	if done["done"] != true || done["sub"] != float64(4) {
		t.Errorf("읽는 쪽이 말없이 사라졌을 때 구독이 끝났다고 말하지 않는다: %v", done)
	}
	// 그리고 두 번 말하지 않는다 — 나가는 길이 셋인데 말은 하나여야 한다.
	if n := strings.Count(out.String(), `"done":true`); n != 1 {
		t.Errorf("끝났다는 말이 %d 번 나왔다", n)
	}
}

// **What a screen is told when the stream stops, both ways.**
//
// The door has two clean ends and they must say different things: the companion went away, and the
// client asked to stop. Which of them happens first on a real stop is a race, so this asks the rule
// directly rather than trying to win one.
func TestWhatAScreenIsToldWhenTheStreamStops(t *testing.T) {
	open := make(chan struct{})
	vanished := whyEnded(nil, open, false)
	if strings.TrimSpace(vanished) == "" {
		t.Errorf("컴패니언이 조용히 사라졌는데 할 말이 없다 — 화면은 「끝났다」만 듣고 조용한 대화와 못 가른다")
	}
	closed := make(chan struct{})
	close(closed)
	if why := whyEnded(nil, closed, false); why != "" {
		t.Errorf("일부러 멈춘 구독에 %q 라고 말한다 — 죽지 않은 데몬의 부고다", why)
	}
	if why := whyEnded(errors.New("boom"), closed, true); why != "boom" {
		t.Errorf("진짜 오류가 %q 로 바뀐다 — 사유는 있는 그대로 간다", why)
	}
	// ⚠ **끝냈다는 말과 사라진 것은 다른 소식이다.** 코어가 스트림이 끝날 때 그것을 말하기 시작했고
	// (`Response.Over`, #197), 창 둘은 그 프레임을 「데몬이 끝냈다 → 다시 붙는다」로 읽는다. 둘에 같은
	// 문장을 주면 평범한 끝이 사고처럼, 사고가 평범한 끝처럼 보인다.
	ended := whyEnded(nil, open, true)
	if strings.TrimSpace(ended) == "" {
		t.Errorf("컴패니언이 스트림을 끝냈다고 말했는데 화면에는 아무 말도 안 간다")
	}
	if ended == vanished {
		t.Errorf("끝냈다는 말과 말없이 사라진 것을 같은 문장으로 말한다: %q", ended)
	}
}

// **The companion ending the stream reaches the client as its own sentence.**
//
// Over the wire, not only in the rule: the daemon sends `{"ok":true,"over":true}` before it stops, and
// what a screen does next (reattach, or tell a person) depends on hearing THAT rather than a closed
// socket.
func TestAStreamTheCompanionEndsSaysSoInItsOwnWords(t *testing.T) {
	shortBatch(t)
	d := liveListen(t, []event.Event{
		mk(1, "prompt.submitted", map[string]any{"messageId": "u1",
			"parts": []any{map[string]any{"kind": "text", "text": "hi"}}}, nil),
	})
	ed := drive(t, d.path)
	ed.send(`{"id":1,"method":"rows","session":"s_1","live":true}`)
	if f := ed.next("구독의 첫 답"); f["sub"] == nil {
		t.Fatalf("구독이 안 섰다: %v", f)
	}
	d.over()
	f := ed.next("끝났다는 프레임")
	if f["done"] != true {
		t.Fatalf("끝났다고 말하지 않는다: %v", f)
	}
	why, _ := f["why"].(string)
	if !strings.Contains(why, "ended this stream") {
		t.Errorf("컴패니언이 끝냈는데 사라진 것처럼 말한다: %q", why)
	}
}

// **A failed request does not take the live subscriptions with it.**
//
// ⚠ This regressed on 2026-09-13 and the review caught it (2026-09-14). `forward` drops the shared
// request connection when a call breaks — its own comment says the suspect is the connection, not the
// request — and `endSubs` had been put inside that same function, so one editor action that got an
// error blanked every open panel. Subscriptions hold their own connections exactly so they do not
// share this fate, and this is the test that says so.
func TestAFailedRequestDoesNotEndTheLiveSubscriptions(t *testing.T) {
	shortBatch(t)
	replay := []event.Event{
		mk(1, "prompt.submitted", map[string]any{"messageId": "u1",
			"parts": []any{map[string]any{"kind": "text", "text": "hi"}}}, nil),
	}
	d := liveListen(t, replay)
	ed := drive(t, d.path)
	ed.send(`{"id":1,"method":"rows","session":"s_1","live":true}`)
	held := rowsOf(t, ed.next("구독의 첫 답"), "rows")

	// 전송이 깨지는 요청 하나. 이 답은 실패해야 한다 — 그것은 이 시험이 재는 것이 아니다.
	ed.send(`{"id":2,"method":"daemon","req":{"method":"boom"}}`)
	if f := ed.next("깨진 요청의 답"); f["ok"] != false {
		t.Fatalf("연결이 끊긴 요청이 성공했다고 답한다: %v", f)
	}

	// 그리고 구독은 살아 있어야 한다.
	d.push(mk(2, "part.appended", map[string]any{"messageId": "m1", "role": "assistant",
		"part": map[string]any{"kind": "text", "text": "still here"}}, nil))
	f := ed.next("요청이 깨진 뒤의 차이")
	if f["ops"] == nil {
		t.Fatalf("요청 하나가 깨진 뒤 구독이 말을 그쳤다: %v", f)
	}
	held = Apply(held, opsOf(t, f))
	want := Rows(append(append([]event.Event{}, replay...),
		mk(2, "part.appended", map[string]any{"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "text", "text": "still here"}}, nil)))
	if listJSON(t, held) != listJSON(t, want) {
		t.Errorf("구독이 살아는 있는데 접기와 다른 것을 그린다\n받은 쪽: %s\n접기: %s",
			listJSON(t, held), listJSON(t, want))
	}
}

// **The two meanings of hanging up, said apart.**
//
// The wire test above is what a person would notice; this is the rule underneath it, so a future edit
// that puts the teardown back into the shared path fails by name rather than by a blank panel.
func TestHangingUpTheRequestConnectionKeepsTheSubscriptions(t *testing.T) {
	var out strings.Builder
	b := &bridge{workspace: "/ws", out: &out, subs: map[int]*rowsSub{}}
	sub := &rowsSub{id: 1, conn: daemon.Over(nopPipe{}), stop: make(chan struct{})}
	b.subs[sub.id] = sub

	b.hangUp()
	select {
	case <-sub.stop:
		t.Error("요청 연결을 놓는 것이 구독을 끝냈다 — 이 둘은 다른 일이다")
	default:
	}
	b.shutdown()
	select {
	case <-sub.stop:
	default:
		t.Error("브리지가 사라지는데 구독이 남았다 — 연결과 고루틴이 프로세스와 같이 죽지 않는다")
	}
}

// **A long replay that keeps arriving is not a companion that went quiet.**
//
// ⚠ The bound here used to be one timer over the whole replay (20s), which no arriving event reset —
// so a conversation big enough to take longer than that failed while it was streaming perfectly well
// (review, 2026-09-14). It bounds SILENCE now, per frame, exactly as the one-shot read's does.
//
// Measured with the bound made small and the frames made slow: the replay takes four times the bound
// in total, and none of its gaps reaches it.
func TestALongReplayThatKeepsArrivingIsNotSilence(t *testing.T) {
	shortBatch(t)
	was := rowsSilence
	rowsSilence = 120 * time.Millisecond
	t.Cleanup(func() { rowsSilence = was })

	var replay []event.Event
	for i := 0; i < 8; i++ {
		replay = append(replay, mk(int64(i+1), "prompt.submitted", map[string]any{
			"messageId": fmt.Sprintf("u%d", i),
			"parts":     []any{map[string]any{"kind": "text", "text": "a question"}}}, nil))
	}
	d := liveListen(t, replay)
	d.gap = 60 * time.Millisecond // 총 480ms — 한계의 네 배, 그러나 어느 틈도 한계에 못 미친다

	ed := drive(t, d.path)
	ed.send(`{"id":1,"method":"rows","session":"s_1","live":true}`)
	f := ed.next("느린 재생의 첫 답")
	if f["ok"] != true {
		t.Fatalf("도착하고 있는 재생을 침묵으로 읽었다: %v", f)
	}
	if got := rowsOf(t, f, "rows"); listJSON(t, got) != listJSON(t, Rows(replay)) {
		t.Errorf("느리게 온 재생이 접기와 다르다: %s", listJSON(t, got))
	}
}

// **And a companion that really does go quiet still gets a sentence.**
//
// The other half: bounding silence instead of the replay must not mean bounding nothing. A fake that
// accepts, says the handshake, and then says nothing at all.
func TestACompanionThatAcceptsAndSaysNothingIsToldAbout(t *testing.T) {
	was := rowsSilence
	rowsSilence = 150 * time.Millisecond
	t.Cleanup(func() { rowsSilence = was })

	// caps 에 history 는 있지만 transcript 에 아무 프레임도 안 보낸다 — 받고 조용해진 컴패니언.
	d := liveListen(t, nil, "handshake", "transcript", "history", "say-nothing")
	ed := drive(t, d.path)
	ed.send(`{"id":1,"method":"rows","session":"s_1","live":true}`)
	f := ed.next("조용한 컴패니언의 답")
	if f["ok"] != false {
		t.Fatalf("받고 조용해진 컴패니언에 대해 사유가 안 온다: %v", f)
	}
	if why := str(f, "error"); !strings.Contains(why, "went quiet") {
		t.Errorf("사유가 그 사실을 말하지 않는다: %q", why)
	}
}
