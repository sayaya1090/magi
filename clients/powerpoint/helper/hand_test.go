package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// answerWith 는 손이 내려받은 조작에 정해진 답을 준다. 전송 없이 허브만 재는 자리라 채널을
// 직접 읽는다 — 여기서 재려는 것은 배선이지 HTTP 가 아니다.
func answerWith(t *testing.T, c *handConn, fn func(HandRequest) HandReply) {
	t.Helper()
	go func() {
		for req := range c.out {
			rep := fn(req)
			rep.ID = req.ID
			c.deliver(rep)
		}
	}()
}

func TestACallGoesToTheOnlyAttachedDeck(t *testing.T) {
	h := NewHandHub()
	c := h.Join("", "q3.pptx")
	answerWith(t, c, func(req HandRequest) HandReply {
		return HandReply{Result: map[string]any{"op": req.Op}, Label: "q3.pptx", Epoch: 7, Count: 2}
	})

	res, err := h.Call(context.Background(), "", "list_slides", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Document != c.key {
		t.Errorf("손댄 문서가 %q 다 (키는 %q)", res.Document, c.key)
	}
	if res.Label != "q3.pptx" {
		t.Errorf("이름표가 %q 다", res.Label)
	}
	if res.Revision == nil || !res.Revision.Known || res.Revision.Epoch != 7 {
		t.Errorf("개정 쌍이 %+v 다", res.Revision)
	}
}

// 개정 쌍을 **안 실어 보내면 「모른다」**다. 「안 바뀌었다」가 아니다(§5.6) — 헬퍼가 재시작한
// 사이는 아무도 못 봤고, 둘을 같은 값으로 접으면 화면이 없던 보장을 하게 된다.
func TestAMissingRevisionSaysUnknownNotUnchanged(t *testing.T) {
	h := NewHandHub()
	c := h.Join("", "q3.pptx")
	answerWith(t, c, func(HandRequest) HandReply { return HandReply{} })

	res, err := h.Call(context.Background(), "", "read_slide", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Revision == nil || res.Revision.Known {
		t.Fatalf("개정 쌍이 %+v 다 — 안 실려 왔으면 Known 이 거짓이어야 한다", res.Revision)
	}
}

// 문서를 말했는데 그런 덱이 없으면 **비슷한 것으로 갈음하지 않는다**(§5.8 의 규칙).
// 그리고 무엇이 열려 있는지를 같이 적는다 — 모델이 다음 시도에서 또 지어내지 않게.
func TestAnUnknownDocumentIsRefusedNotGuessed(t *testing.T) {
	h := NewHandHub()
	c := h.Join("", "q3.pptx")
	answerWith(t, c, func(HandRequest) HandReply { return HandReply{} })

	_, err := h.Call(context.Background(), "doc-nope", "set_text", map[string]any{})
	if err == nil {
		t.Fatal("없는 문서가 통과했다")
	}
	for _, want := range []string{"doc-nope", "q3.pptx", "Nothing was changed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("거절 문구에 %q 가 없다: %v", want, err)
		}
	}
}

// 생략은 **활성 문서**다 — 가장 최근에 말한 손(§4.4 ④).
func TestOmittingTheDocumentUsesTheDeckThatSpokeLast(t *testing.T) {
	h := NewHandHub()
	base := time.Unix(1000, 0)
	h.Now = func() time.Time { return base }
	first := h.Join("p1", "old.pptx")
	answerWith(t, first, func(HandRequest) HandReply { return HandReply{} })

	base = base.Add(time.Minute)
	second := h.Join("p2", "new.pptx")
	answerWith(t, second, func(HandRequest) HandReply { return HandReply{} })

	res, err := h.Call(context.Background(), "", "read_slide", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Document != second.key {
		t.Fatalf("활성 문서가 %q 다 — 최근에 말한 것은 %q 였다", res.Document, second.key)
	}
}

// 같은 프레젠테이션이 다시 붙으면 **같은 키**다. 작업창은 PowerPoint 를 껐다 켤 때마다 새로
// 붙으므로(§5.7), 그때마다 키가 바뀌면 모델이 들고 있던 `document` 가 죽는다.
func TestRejoiningTheSamePresentationKeepsItsKey(t *testing.T) {
	h := NewHandHub()
	first := h.Join("pid-42", "q3.pptx")
	h.Leave(first)
	again := h.Join("pid-42", "q3.pptx")
	if again.key != first.key {
		t.Fatalf("키가 %q → %q 로 바뀌었다", first.key, again.key)
	}
}

// 저장 안 된 덱 둘은 프레젠테이션 id 가 없어도 **서로 다른 키**를 받아야 한다(§5.6 —
// 경로를 키로 삼으면 아무것도 공유하지 않는 두 덱이 한 손을 다투는 것으로 읽힌다).
func TestTwoUnsavedDecksGetTwoKeys(t *testing.T) {
	h := NewHandHub()
	a := h.Join("", "")
	b := h.Join("", "")
	if a.key == b.key {
		t.Fatalf("저장 안 된 덱 둘이 같은 키 %q 를 받았다", a.key)
	}
}

// 안 답하면 **우리가 끊고, 누가 얼마에서 끊었는지 말한다**(§4.4 ③ 이 코어에 낸 그 요청을
// 우리 쪽에서 되풀이하지 않는다).
func TestATimeoutSaysWhoCutItAndAtWhat(t *testing.T) {
	h := NewHandHub()
	h.Timeout = 30 * time.Millisecond
	c := h.Join("", "q3.pptx")
	// 답을 안 한다. 조작은 큐에서 그대로 기다린다.
	go func() { <-c.out }()

	_, err := h.Call(context.Background(), "", "render_slide", map[string]any{})
	if err == nil {
		t.Fatal("안 끊었다")
	}
	for _, want := range []string{"magi helper", "30ms", "re-read"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("문구에 %q 가 없다: %v", want, err)
		}
	}
}

// 부른 쪽이 그만둔 것은 **애드인에 대한 증거가 아니다.** magi 도 자기 천장을 서버의 잘못으로
// 세지 않는다(§5.4 — `unreachableStreak` 이 안 세는 것 둘 중 하나가 이것이다).
func TestTheCallerGivingUpIsNotTheAddinsFault(t *testing.T) {
	h := NewHandHub()
	c := h.Join("", "q3.pptx")
	go func() { <-c.out }()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := h.Call(ctx, "", "read_slide", map[string]any{})
	if err == nil {
		t.Fatal("안 끊었다")
	}
	if !strings.Contains(err.Error(), "caller stopped waiting") {
		t.Errorf("누가 그만뒀는지가 안 적혔다: %v", err)
	}
	if !strings.Contains(err.Error(), "may still have run") {
		t.Errorf("조작이 여전히 돌 수 있다는 말이 없다: %v", err)
	}
}

// 손이 하나도 없으면 도구는 실패한다. **헬퍼는 그래도 산다**(§5.4) — 작업창을 닫아도
// 프로세스가 죽지 않는다.
func TestTheHelperOutlivesItsLastHand(t *testing.T) {
	h := NewHandHub()
	c := h.Join("", "q3.pptx")
	if !h.Attached() {
		t.Fatal("붙었는데 안 붙었다고 한다")
	}
	h.Leave(c)
	if h.Attached() {
		t.Fatal("떠났는데 붙어 있다고 한다")
	}
	_, err := h.Call(context.Background(), "", "read_slide", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "not attached to PowerPoint") {
		t.Fatalf("사유가 %v 다", err)
	}
}

// 한 문서에 손은 하나 — 그런데 **폭이 호출 하나**다(§5.7). 앞 호출이 답하면 뒤 호출이 곧장
// 돈다. 턴으로 잠그면 채팅 제출과 그 턴의 읽기가 서로를 기다려 교착한다.
func TestOneCallAtATimeButOnlyOneCall(t *testing.T) {
	h := NewHandHub()
	c := h.Join("", "q3.pptx")
	started := make(chan string, 2)
	release := make(chan struct{})
	go func() {
		for req := range c.out {
			started <- req.Op
			if req.Op == "slow" {
				<-release
			}
			c.deliver(HandReply{ID: req.ID})
		}
	}()

	done := make(chan error, 1)
	go func() {
		_, err := h.Call(context.Background(), "", "slow", map[string]any{})
		done <- err
	}()
	if got := <-started; got != "slow" {
		t.Fatalf("첫 조작이 %q 다", got)
	}

	second := make(chan error, 1)
	go func() {
		_, err := h.Call(context.Background(), "", "fast", map[string]any{})
		second <- err
	}()
	select {
	case <-started:
		t.Fatal("앞 호출이 도는 중에 두 번째가 내려갔다")
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-started:
		if got != "fast" {
			t.Fatalf("두 번째 조작이 %q 다", got)
		}
	case <-time.After(time.Second):
		t.Fatal("앞 호출이 끝났는데 두 번째가 안 내려갔다")
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
}
