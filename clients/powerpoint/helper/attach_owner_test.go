package main

import "testing"

// **소켓 하나에 등록이 여럿일 수 있다**(창마다 하나). 이름만으로 찾으면 주인을 달고 붙인 등록이
// 「없는 것」으로 읽히고, 그 오답은 조용하지 않다: `/api/own` 이 매 폴마다 「우리 것이 아니다」로
// 읽어 `Forget` → 재시작을 반복하며 **영원히 「준비하는 중」**을 답한다. 판은 5분을 그렇게 돌다
// 포기하고 명단을 그린다 — 실물에서 그 화면을 봤다(2026-09-05).
func TestOurRegistrationIsFoundWhateverTheOwner(t *testing.T) {
	a := NewAttachments()
	a.held[heldKey("/sock", "sess-a")] = attachment{tools: []string{"mcp__ppt__list_slides"}, life: ""}

	if !a.HasLive("/sock", "") {
		t.Error("주인을 달고 붙인 등록을 못 찾는다 — 이 오답이 판을 명단으로 떨어뜨린다")
	}
	if got := a.Tools("/sock"); len(got) != 1 {
		t.Errorf("그 등록의 도구를 못 찾는다: %v", got)
	}
	// 다른 소켓은 여전히 남이다.
	if a.HasLive("/other", "") {
		t.Error("붙은 적 없는 소켓을 우리 것이라고 한다")
	}
	// 주인 없이 붙인 옛 등록도 그대로 찾는다.
	b := NewAttachments()
	b.held["/sock"] = attachment{tools: []string{"x"}}
	if !b.HasLive("/sock", "") || len(b.Tools("/sock")) != 1 {
		t.Error("주인 없는 등록이 안 보인다 — 이 변경 전의 모든 등록이 그 모양이다")
	}
}

// **주소가 바뀌면 다른 등록이다.**
//
// 주소는 덱을 나르고(`MCPURL`), 그 값이 `document` 를 생략한 호출의 목적지를 정한다. 앞 판본은
// (소켓, 주인)만 보고 「이미 붙어 있다」로 재부착을 건너뛰었고, 등록은 **앞 PowerPoint 세션의 덱
// 이름**을 그대로 들고 있었다 — 그 대화의 호출은 사라진 덱으로 갔고, 모델은 사람에게 「어느 덱에
// 만들까요」를 물었다(2026-09-05 실물).
func TestADifferentAddressIsADifferentRegistration(t *testing.T) {
	a := NewAttachments()
	key := heldKey("/sock", "sess-a")
	a.held[key] = attachment{tools: []string{"x"}, url: "https://h/mcp?deck=old"}

	if !a.SameURL(key, "https://h/mcp?deck=old") {
		t.Error("같은 주소를 다르다고 한다 — 매번 다시 붙이면 첫 등록이 떨어진다")
	}
	if a.SameURL(key, "https://h/mcp?deck=new") {
		t.Error("덱이 바뀌었는데 같은 등록으로 친다 — 호출이 옛 덱으로 간다")
	}
	// 주소를 안 들고 있던 옛 등록은 그대로 산다 — 모르는 것을 「다르다」로 읽지 않는다.
	a.held[key] = attachment{tools: []string{"x"}}
	if !a.SameURL(key, "https://h/mcp?deck=new") {
		t.Error("주소를 모르는 옛 등록을 매번 다시 붙인다")
	}
}
