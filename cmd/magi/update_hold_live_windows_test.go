//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/shortdir"
)

// U02·U03 — **턴이 도는 중에 온 후보는 기다린다**, 그리고 안전 시점에 올라선다.
//
// 바쁨의 정의가 `Running()`(턴이 도는 중)이라, 이 둘은 데몬이 **정말로 바빠야** 재인다. 그래서 모델을
// 세운다 — in-process 목이 아니라 **HTTP 목**이다. 데몬은 별도 프로세스이고 `-base-url` 로 OpenAI 호환
// 엔드포인트를 받으므로, 릴리스 서버와 같은 모양이면 된다.
//
// ⚠ **붙드는 것은 내가 보낸 프롬프트에만.** 기동 경로에도 이 엔드포인트를 두드리는 것이 있어(컨텍스트
// 창 탐침 같은) 모든 요청을 붙들면 데몬이 아예 뜨지 못한다 — 그래서 표식이 든 요청만 붙들고 나머지는
// 즉시 답한다.

// heldModel is an OpenAI-compatible endpoint that keeps ONE turn in flight for as long as the test
// wants, and answers everything else at once.
type heldModel struct {
	*httptest.Server
	release chan struct{}
	held    chan struct{} // closed when the marked request has actually arrived
}

const holdMarker = "HOLD-THIS-TURN"

func modelThatHolds(t *testing.T) *heldModel {
	t.Helper()
	m := &heldModel{release: make(chan struct{}), held: make(chan struct{})}
	var once bool
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "mock"}}})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mine := strings.Contains(string(body), holdMarker) && !once
		if mine {
			once = true
		}
		if !strings.Contains(string(body), `"stream":true`) {
			// Whatever else asks — a probe, a title, a summary — gets a whole answer at once.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "mock", "object": "chat.completion", "model": "mock",
				"choices": []map[string]any{{
					"index": 0, "finish_reason": "stop",
					"message": map[string]string{"role": "assistant", "content": "ok"},
				}},
				"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
			})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flush := func() {
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		chunk := func(content string) bool {
			b, _ := json.Marshal(map[string]any{"choices": []map[string]any{
				{"index": 0, "delta": map[string]string{"content": content}},
			}})
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return false
			}
			flush()
			return true
		}
		// ⚠ **붙드는 동안에도 토큰은 흐른다.** 아무것도 안 쓰고 붙들면 제품의 스트림 가드가 「침묵하는
		// 스트림(먹통 백엔드)」으로 턴을 끊는다(무활동 120초, `MAGI_STREAM_STALL`) — 그러면 이 시험은
		// 바쁨이 아니라 **그 가드**를 재게 된다. 실제 모델이 생각하며 쓰는 모양대로, 첫 토큰까지 조금
		// 뜸을 들이고 그 뒤로는 꾸준히 흘린다.
		time.Sleep(300 * time.Millisecond) // TTFT
		if !chunk("생각 중") {
			return
		}
		if mine {
			close(m.held)
			for {
				select {
				case <-m.release:
					goto finish
				case <-r.Context().Done():
					return
				case <-time.After(150 * time.Millisecond):
					if !chunk(".") {
						return
					}
				}
			}
		}
	finish:
		_ = chunk(" 끝")
		b, _ := json.Marshal(map[string]any{"choices": []map[string]any{
			{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"},
		}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
		flush()
	})
	m.Server = httptest.NewServer(mux)
	t.Cleanup(m.Close)
	return m
}

func TestACandidateArrivingWhileATurnRunsWaitsForTheSafePoint(t *testing.T) {
	w := fetchSetup(t)
	rs := serveRelease(t, liveTag, w.newBin, trueDigest)
	llm := modelThatHolds(t)
	ws, err := shortdir.Make("mgh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
	t.Cleanup(func() {
		rows, _ := daemon.List(w.cfg)
		for _, r := range rows {
			if r.PID != 0 {
				if p, ferr := os.FindProcess(r.PID); ferr == nil {
					_ = p.Kill()
				}
			}
		}
	})

	logPath := w.cfg + string(os.PathSeparator) + "hold.log"
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.Command(w.exe, "--daemon", "--no-update-check")
	cmd.Dir = ws
	cmd.Env = append(os.Environ(),
		"MAGI_CONFIG_DIR="+w.cfg,
		"MAGI_SOCKET_DIR="+w.cfg,
		"MAGI_RELEASE_API_BASE="+rs.URL,
		"MAGI_BASE_URL="+llm.URL+"/v1",
		"MAGI_MODEL=mock",
		"MAGI_API_KEY=mock",
		"MAGI_PERMISSION=allow")
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sock := daemon.SocketPath(w.cfg, ws)
	waitServing(t, sock, cmd.Process.Pid, logPath)

	// 턴 하나를 넣고, 그것이 **실제로 모델에 닿을 때까지** 기다린다. 「보냈다」가 아니라 「돌고 있다」가
	// 이 시험의 전제다.
	rec, err := daemon.Published(sock)
	if err != nil {
		t.Fatal(err)
	}
	c, err := daemon.DialWithin(sock, 2*time.Second, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if serr := c.Submit(context.Background(), command.SubmitPrompt{
		SessionID: session.SessionID(rec.Session),
		Parts:     []session.Part{{Kind: session.PartText, Text: holdMarker + " — 이 턴은 시험이 붙들고 있다"}},
	}); serr != nil {
		t.Fatalf("턴을 넣지 못했다: %v\n%s", serr, readLog(logPath))
	}
	select {
	case <-llm.held:
	case <-time.After(90 * time.Second):
		t.Fatalf("턴이 모델에 닿지 않았다 — 이 시험은 바쁨을 못 만들었다:\n%s", readLog(logPath))
	}

	// U02: 이 순간에 후보가 온다. 설치는 지금, 재기동은 **나중**이어야 한다.
	out, uerr := c.UpdateWhen("idle")
	if uerr != nil {
		t.Fatalf("문이 거절했다: %v\n%s", uerr, readLog(logPath))
	}
	c.Close()
	if !strings.Contains(out, "when nothing is running") {
		t.Errorf("바쁜데도 즉시 재기동한다고 답했다: %q", out)
	}
	// 그리고 정말로 안 올라섰다: 아직 이전 판이 서비스하고 있다.
	if now, ok := serving(sock); !ok || now.Version != baseTag || now.PID != cmd.Process.Pid {
		t.Errorf("턴이 도는 중에 세대가 바뀌었다 — 돌던 턴이 버려진다: %+v", now)
	}

	// U03: 턴이 끝나면 그 자리에서 올라선다.
	close(llm.release)
	if got := waitVersion(t, sock, liveTag, readLog(logPath)); got.PID == 0 {
		t.Fatalf("턴이 끝났는데 새 판으로 안 올라섰다:\n%s", readLog(logPath))
	}
}
