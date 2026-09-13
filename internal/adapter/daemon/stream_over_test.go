package daemon

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
)

// **끝난 스트림은 끝났다고 말한다** — 연결이 닫히는 것에 기대지 않는다(#197).
//
// ⚠ 왜 반복이 필요한가. 이 결함은 한 번에 안 나온다: 윈도우 AF_UNIX 에서 **쓰고 곧바로 닫으면 상대가
// EOF 를 잃는** 경합이고, magi 없이 순수 소켓으로 재서 600회 중 12회였다(2026-09-13). 20ms 를 두면
// 0/600, `CloseWrite` 를 앞에 두면 1/600 — 서버 쪽 철자로는 못 고친다. 데이터는 도착하고 **파일 끝만**
// 안 온다. 그래서 끝을 **말한다**(Response.Over).
//
// 이 시험이 없었을 때 그 결함은 `TestTranscriptRefusesAnInventedConversation` 이 이따금 **영원히
// 멈추는** 것으로만 보였고, 전체 수트에서 60분 시간 초과로 처음 발견됐다. 원인을 가리키지 않는 빨간색
// 한 줄이 그것이다 — 그래서 여기서는 반복하고, 멈추면 **그 순간의 고루틴을 함께 찍는다**.
//
// 기한 3초는 감시견이다. 정상 회차는 밀리초 단위로 끝난다(400회가 3초).
func TestAFinishedStreamSaysSoRatherThanRelyingOnTheSocketClosing(t *testing.T) {
	rounds := 400
	if testing.Short() {
		rounds = 25
	}
	for i := 0; i < rounds; i++ {
		if !streamEndsCleanly(t, i) {
			return // 한 번 잡으면 충분하다 — 같은 실패를 400번 찍을 이유가 없다
		}
	}
}

func streamEndsCleanly(t *testing.T, i int) bool {
	t.Helper()
	home, err := os.MkdirTemp(shortRoot(), "mgo")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	sock := filepath.Join(home, "d.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()
	go func() { _ = d.Serve(context.Background(), &omniEngine{}) }()
	c, err := Dial(sock)
	if err != nil {
		t.Fatalf("%d: dial: %v", i, err)
	}
	defer c.Close()

	// 거절이 먼저다 — 그 뒤 **같은 연결로** 다음 대화를 여는 것이 실제 클라이언트가 밟는 순서이고
	// (콘솔은 데몬마다 연결 하나를 캐시한다), 이 결함이 살던 자리다.
	if rerr := c.Transcript("s_invented", 0, nil, func(event.Event) bool { return true }); rerr == nil {
		t.Fatalf("%d: 전제가 안 섰다 — 없는 id 가 거절되지 않았다", i)
	}
	done := make(chan error, 1)
	go func() { done <- c.Transcript("s_new", 0, nil, func(event.Event) bool { return true }) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%d: 깨끗하게 끝나야 하는 스트림이 오류로 끝났다: %v", i, err)
		}
		return true
	case <-time.After(3 * time.Second):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Errorf("%d회: 끝난 스트림이 읽는 쪽을 놓아주지 않는다 — 닫힘만으로는 끝을 알릴 수 없다"+
			"(#197)\n=== 멈춘 순간의 고루틴 ===\n%s", i, buf[:n])
		return false
	}
}
