package office

import "sync"

// Bridges 는 **덱 하나에 대화 하나**를 지킨다.
//
// 여태 헬퍼는 프로세스당 `Bridge` 를 하나 들었다. 도구 층은 문서별로 갈리는데(`documentOf`)
// 대화 바인딩과 스트림 구독은 안 갈렸다 — 그래서 PowerPoint 창을 둘 띄우면 **양쪽 작업창에
// 같은 말이 흘렀다.** 사람이 그것을 보고 물었다(2026-09-04): "왜 양쪽에 같은 메시지가 와?
// 세션이 달라야하는거 아니냐". 맞는 말이고, 그 자리가 여기다.
//
// 열쇠는 창이 아는 이름(프레젠테이션)이다. 손 스트림은 이미 그 이름으로 갈라져 있었다
// (`HandHTTP.Feed(conn.key)`) — 받고서 버리고 있었을 뿐이다.
type Bridges struct {
	mu   sync.Mutex
	made map[string]*Bridge
	// New 는 하나를 어떻게 만드는가. **시험만 이 자리를 채운다.**
	New func() *Bridge
}

func NewBridges() *Bridges {
	return &Bridges{made: map[string]*Bridge{}, New: NewBridge}
}

// For 는 그 덱의 대화. 없으면 만든다 — **빈 열쇠도 한 덱이다**(창이 이름을 아직 모르는 때).
func (bs *Bridges) For(key string) *Bridge {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if b, ok := bs.made[key]; ok {
		return b
	}
	b := bs.New()
	bs.made[key] = b
	return b
}

// Holder 는 그 세션을 이미 든 덱이 있는가. 같은 세션에 두 덱이 붙으면 갈라 놓은 뜻이 없어진다.
func (bs *Bridges) Holder(session string) (string, bool) {
	if session == "" {
		return "", false
	}
	bs.mu.Lock()
	defer bs.mu.Unlock()
	for key, b := range bs.made {
		if _, sid, _ := b.Bound(); sid == session {
			return key, true
		}
	}
	return "", false
}

// Binding 은 묶음 하나의 (소켓, 대화, 생애). 나갈 때 뗄 것과 「우리 것인가」의 근거가 이것이다.
type Binding struct{ Socket, Session, Life string }

// Bindings 는 지금 묶여 있는 것 전부 — 열쇠 없는 자리까지. 결정의 캐시가 아니라 **살아 있는 묶음의
// 목록**이다: 무엇이 붙어 있는지를 따로 기억하던 `Attachments.held` 를 이것이 대신한다(DESIGN §5.9.2).
func (bs *Bridges) Bindings() []Binding {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	out := []Binding{}
	for _, b := range bs.made {
		if socket, sid, life, _ := b.BoundTo(); socket != "" {
			out = append(out, Binding{socket, sid, life})
		}
	}
	return out
}

// AttachedTo 는 **이 생애의** 그 데몬에 우리 묶음이 하나라도 있는가. 소켓만 같고 생애가 다르면 다시
// 뜬 데몬이고, 우리 등록은 그 데몬과 같이 사라졌다 — 그때는 「아니오」다.
func (bs *Bridges) AttachedTo(socket, life string) bool {
	for _, b := range bs.Bindings() {
		if b.Socket == socket && (life == "" || b.Life == life) {
			return true
		}
	}
	return false
}

// Keys 는 지금 선 덱들. 시험과 상태 보고용이다.
func (bs *Bridges) Keys() []string {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	out := make([]string, 0, len(bs.made))
	for k := range bs.made {
		out = append(out, k)
	}
	return out
}

// StopAll 은 전부 놓는다. 헬퍼가 내려갈 때 한 번.
func (bs *Bridges) StopAll() {
	bs.mu.Lock()
	made := make([]*Bridge, 0, len(bs.made))
	for _, b := range bs.made {
		made = append(made, b)
	}
	bs.mu.Unlock()
	for _, b := range made {
		b.Stop()
	}
}
