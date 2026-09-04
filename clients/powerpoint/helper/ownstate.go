package main

import (
	"sync"
	"time"
)

// 컴패니언을 마련하는 일은 **기다려 주는 일이 아니다.**
//
// 첫 판본은 `/api/own` 안에서 `Ensure` 를 그냥 불렀다. 실물에서 그 요청이 120초에 끊겼다
// (2026-09-02) — 데몬 냉시동은 설정을 읽고 저장소를 열고 백엔드를 재 보고 나서야 소켓에 서므로,
// 그동안 판은 아무 말 없이 멎어 있다. **PC 를 잘 다루지 못하는 사람에게 멎은 화면은 고장이다**:
// 무엇을 기다리는지 모르고, 얼마나 걸릴지 모르고, 다시 눌러야 하는지도 모른다.
//
// 그래서 마련하는 일을 **뒤로 보내고 상태를 답한다.** 판은 즉시 「처음이라 준비하고 있습니다」를
// 그릴 수 있고, 다시 물으면 진행된 만큼을 받는다.
//
// # 두 번 띄우지 않는다
//
// 덱을 둘 열면 판도 둘이고, 둘 다 이 자리를 두드린다. 각자 데몬을 띄우면 **둘이 한 워크스페이스를
// 두고 다툰다.** 그래서 일하는 중이면 새 일을 안 시작하고 지금 상태를 그대로 돌려준다 — 등록의
// 임자가 헬퍼 하나인 것(`attach.go` §5.0.1)과 같은 이유고, 여기서 지키지 않으면 그 규칙이
// 무의미해진다.

// OwnPhase 는 마련하는 일이 어디쯤인가.
type OwnPhase string

const (
	// OwnIdle 은 아직 아무도 안 물었다.
	OwnIdle OwnPhase = "idle"
	// OwnWorking 은 마련하는 중. **판은 이 값을 보고 기다리는 화면을 그린다.**
	OwnWorking OwnPhase = "working"
	// OwnReady 는 섰고 붙었다.
	OwnReady OwnPhase = "ready"
	// OwnFailed 는 못 했다. 사유와 자리가 같이 실린다.
	OwnFailed OwnPhase = "failed"
)

// OwnReport 는 지금 상태 하나. 판이 그대로 그린다.
type OwnReport struct {
	Phase OwnPhase `json:"phase"`
	// Started 는 **이번에 우리가 띄웠는가.** 「이미 있었다」와 다른 소식이라 따로 싣는다 —
	// 처음 쓰는 사람은 몇 초 걸린 이유를 알아야 한다.
	Started bool     `json:"started"`
	Socket  string   `json:"socket,omitempty"`
	Workdir string   `json:"workdir,omitempty"`
	Session string   `json:"session,omitempty"`
	Tools   []string `json:"tools,omitempty"`
	// Log 는 데몬이 자기 말을 적는 자리. **실패에 반드시 실린다** — 사유가 거기에만 있다.
	Log string `json:"log,omitempty"`
	// Why 는 실패 사유. 사람이 읽는 문장이다.
	Why string `json:"why,omitempty"`
	// Chat 은 붙기는 했는데 대화를 못 연 경우의 사유. **Why 와 등급이 다르다**(§5.0.5) —
	// 이쪽은 도구가 다 도는 상태이고, 합치면 멀쩡한 것을 고장으로 그린다.
	Chat string `json:"chat,omitempty"`
}

// stuckAfter 는 **일이 걸린 것으로 치는** 시간이다.
//
// `doing` 은 두 번 띄우는 것을 막는 깃발인데, 그 깃발을 내리는 것은 `Done` 하나뿐이었다. 일이
// 어딘가에서 안 돌아오면 깃발이 영영 참으로 남고, 그러면 **헬퍼가 사는 내내 모든 작업창이**
// 「준비하는 중」을 본다 — 파워포인트를 다시 켜도 헬퍼는 그대로라 안 낫는다. 리뷰가 그 자리를
// 짚었다(2026-09-02).
//
// 왕복은 이제 전부 묶여 있으므로(`aliveTimeout`·`candidateTimeout`) 여기 닿을 일은 없어야 한다.
// 그래도 둔다 — **없어야 하는 일이 일어났을 때 사람이 갇히지 않는 것**이 이 상수가 하는 일이고,
// 여기서 아끼면 대가를 치르는 쪽이 우리가 아니다.
const stuckAfter = 3 * time.Minute

// OwnWork 는 그 일을 들고 있는 자리. **한 번에 하나만 돈다.**
type OwnWork struct {
	mu     sync.Mutex
	report OwnReport
	// doing 은 지금 누가 일하고 있는가. 이 깃발이 두 번 띄우는 것을 막는다.
	doing bool
	// began 은 그 일이 시작한 때. 걸린 일을 알아보는 유일한 근거다.
	began time.Time
	// unread 는 **아직 아무도 안 본 실패**가 들려 있는가. 폴링이 실패를 지우는 것을 막는다.
	unread bool
	// now 는 시계. **시험만 이 자리를 채운다** — 못 재는 갈래는 안 만든 것과 같다.
	now func() time.Time
}

func NewOwnWork() *OwnWork {
	return &OwnWork{report: OwnReport{Phase: OwnIdle}, now: time.Now}
}

func (o *OwnWork) clock() time.Time {
	if o.now != nil {
		return o.now()
	}
	return time.Now()
}

// Now 는 지금 상태를 뜬다.
func (o *OwnWork) Now() OwnReport {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.report
}

// Begin 은 일을 시작한다 — **이미 돌고 있으면 안 시작하고** 지금 상태만 준다.
//
// 돌려주는 `mine` 이 참일 때만 부른 쪽이 실제로 일을 한다. 이 하나가 「덱 둘을 열면 데몬이 둘」을
// 막는 자리다.
func (o *OwnWork) Begin() (now OwnReport, mine bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.doing {
		// **걸린 일은 넘겨받는다.** 앞의 일이 안 돌아온 채로 시간이 지났으면 그 깃발은 더 이상
		// 「누가 하고 있다」가 아니라 「아무도 안 하고 있는데 아무도 못 한다」다. 사람이 거기
		// 갇히게 두지 않는다.
		if !o.began.IsZero() && o.clock().Sub(o.began) < stuckAfter {
			return o.report, false
		}
	} else if o.report.Phase == OwnReady {
		// **이미 다 됐으면 다시 안 한다.** 붙은 것을 다시 붙이면 첫 등록이 떨어진다(§5.0.1).
		return o.report, false
	} else if o.report.Phase == OwnFailed && o.unread {
		// **실패를 폴링이 지우지 않는다.**
		//
		// 판은 `working` 인 동안 이 자리를 1초마다 두드린다. 앞 판본은 「Ready 가 아니면 시작」
		// 이었으므로, 실패로 끝난 바로 다음 폴이 그 일을 **다시 시작하며 `working` 을 답했다** —
		// 판은 5분을 그렇게 돌다 「아직 안 떴습니다」로 포기하고 명단을 그렸고, **실패 사유는
		// 한 번도 화면에 안 닿았다.** 실물에서 그 화면을 봤다(2026-09-05: 사람이 「기본으로
		// 파워포인트 데몬 쓰는 거 아니냐」고 물었다. 맞는 말이었고, 자동 경로가 조용히 죽어 있었다).
		//
		// **한 번만 붙잡는다.** 그 한 번이 사유가 화면에 닿는 자리이고, 그다음 폴은 다시 해 본다 —
		// 영영 붙잡으면 한 번 실패한 판은 헬퍼가 사는 내내 못 낫는다(패닉 뒤 재시도를 못박은
		// 시험이 그 계약이다). 시계를 안 쓰므로 느린 판에서도 같게 군다.
		o.unread = false
		return o.report, false
	}
	o.doing = true
	o.began = o.clock()
	// 앞 실패의 사유를 들고 있으면 안 된다 — 다시 해 보는 중인데 옛 사유가 화면에 남는다.
	o.report = OwnReport{Phase: OwnWorking}
	return o.report, true
}

// Done 은 일을 끝내며 결과를 적는다.
func (o *OwnWork) Done(r OwnReport) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.doing = false
	o.report = r
	// **실패는 한 번 읽힐 때까지 들고 있다.** 안 그러면 다음 폴이 그 자리에서 새 일을 시작하며
	// `working` 을 답하고, 사유는 아무 화면에도 안 닿는다(`Begin` 의 주석).
	o.unread = r.Phase == OwnFailed
}

// Forget 은 결과를 지운다 — 다음 물음이 처음부터 다시 하도록.
//
// 데몬이 죽은 것을 알았을 때 쓴다. **`Ready` 를 든 채로 두면** 다시 물어도 「다 됐습니다」만
// 돌려주고 아무도 다시 띄우지 않는다.
func (o *OwnWork) Forget() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.doing {
		return // 지금 돌고 있는 일의 결과를 미리 지우지 않는다
	}
	o.report = OwnReport{Phase: OwnIdle}
}
