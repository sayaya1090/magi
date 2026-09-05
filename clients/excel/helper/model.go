package main

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/provider"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

// 모델·백엔드·컨텍스트 — 작업창의 「무엇으로 돌고 있나」 세 문(2026-09-06, 사용자: 「데몬의 프로바이더와 모델
// 선택하는 컨트롤러와 컨텍스트 압축하는 버튼」).
//
// 웹 콘솔이 같은 일을 하는 자리와 같은 재료를 쓴다: 프로바이더 명단은 internal/adapter/provider.Discover
// (플러그인이 기록한 심 주소 중 지금 카탈로그를 답하는 것), 모델 목록은 데몬의 `models` 문, 바꾸는 것은
// `use-backend`·`set-model`, 압축은 `compact`, 창의 구성은 `context` 문이다. 명단 밖의 주소는 안 받는다 —
// 이 프로세스가 데몬으로 하여금 프롬프트(와 키)를 보내게 만드는 주소라, 아무 주소나 받으면 이 헬퍼에
// 닿는 누구나 겨눌 수 있는 리다이렉트가 된다(웹 콘솔 providers.go 와 같은 이유).

// Context 는 붙어 있는 대화의 창이 얼마나·무엇으로 찼는지.
func (b *Bridge) Context() (app.ContextState, error) {
	_, sid, _ := b.Bound()
	var st app.ContextState
	err := b.call(func(cl *daemon.Client) error {
		var err error
		st, err = cl.ContextState(session.SessionID(sid))
		return err
	})
	return st, err
}

// Models 는 데몬의 백엔드가 지금 댈 수 있는 모델 이름들.
func (b *Bridge) Models() ([]string, error) {
	var names []string
	err := b.call(func(cl *daemon.Client) error {
		var err error
		names, err = cl.Models()
		return err
	})
	return names, err
}

// UseBackend 는 데몬의 백엔드를 다른 심으로 돌린다. 명단 검사는 부르는 쪽(handler)이 한다.
func (b *Bridge) UseBackend(base string) error {
	return b.call(func(cl *daemon.Client) error { return cl.UseBackend(base) })
}

// SetModel 은 붙어 있는 대화의 모델을 바꾼다.
func (b *Bridge) SetModel(name string) error {
	_, sid, _ := b.Bound()
	return b.call(func(cl *daemon.Client) error { return cl.SetModel(session.SessionID(sid), name) })
}

// Compact 는 붙어 있는 대화의 컨텍스트를 접는다. 데몬이 하는 일이고 여기서는 문만 두드린다.
func (b *Bridge) Compact() error {
	_, sid, _ := b.Bound()
	return b.call(func(cl *daemon.Client) error {
		return cl.Compact(context.Background(), command.Compact{SessionID: session.SessionID(sid)})
	})
}

// providers 는 지금 카탈로그를 답하는 심 명단. **지금 쓰는 백엔드**가 먼저 후보에 든다 — 컴패니언 설정의
// base_url 로 띄운 심은 어느 플러그인도 기록하지 않아 발견 밖이고, 그러면 명단이 그것을 못 세워 다른 데로 바꾼 뒤
// 되돌아올 길이 없다(실물 2026-09-06: 클로드 심 58412 가 「명단 밖」으로만 섰다). 전역 설정의 base_url 도 든다.
func (a *API) providers(ctx context.Context, current string) []provider.Provider {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	global := ""
	if cfg, err := config.Load(a.ConfigDir); err == nil {
		global = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	}
	current = strings.TrimRight(strings.TrimSpace(current), "/")
	first := current
	if first == "" {
		first = global
	}
	out := provider.Discover(ctx, a.ConfigDir, first)
	taken := map[string]bool{}
	for i := range out {
		taken[out[i].Base] = true
		if out[i].Name == "default" && out[i].Base == current && current != "" {
			out[i].Name = "지금 백엔드"
		}
	}
	if global != "" && global != current && !taken[global] {
		// 발견은 한 후보만 받는다 — 플러그인 기록 없는 빈 뿌리로 한 번 더 물어 전역 것을 붙인다.
		for _, p := range provider.Discover(ctx, filepath.Join(a.ConfigDir, "no-plugin-data-here"), global) {
			out = append(out, p)
		}
	}
	return out
}

// GET /api/context — 창의 구성. 안 붙었으면 사유를 실어 409.
func (a *API) contextState(w http.ResponseWriter, r *http.Request) {
	if socket, _, _ := a.chat(r).Bound(); socket == "" {
		writeStatus(w, http.StatusConflict, map[string]any{"error": "아직 어느 컴패니언에도 안 붙었습니다"})
		return
	}
	st, err := a.chat(r).Context()
	if err != nil {
		writeStatus(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, st)
}

// GET /api/models — 고를 수 있는 것(프로바이더·모델)과 지금 것(백엔드·모델).
func (a *API) models(w http.ResponseWriter, r *http.Request) {
	if socket, _, _ := a.chat(r).Bound(); socket == "" {
		writeStatus(w, http.StatusConflict, map[string]any{"error": "아직 어느 컴패니언에도 안 붙었습니다", "providers": []provider.Provider{}})
		return
	}
	out := map[string]any{}
	current := ""
	if st, err := a.chat(r).Status(); err == nil {
		out["backend"] = st["backend"]
		out["model"] = st["model"]
		current, _ = st["backend"].(string)
	} else {
		out["warning"] = err.Error()
	}
	out["providers"] = a.providers(r.Context(), current)
	names, err := a.chat(r).Models()
	if err != nil {
		out["warning"] = err.Error()
		names = []string{}
	}
	out["models"] = names
	writeJSON(w, out)
}

// POST /api/model {base?, model?} — 백엔드를 먼저, 모델을 그다음. 백엔드가 바뀌면 모델 이름의 어휘가 바뀌므로
// 둘을 같이 보내는 것이 보통이다(데몬의 UseBackend 주석). 명단 밖의 주소는 거절한다.
func (a *API) setModel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Base  string `json:"base"`
		Model string `json:"model"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Base, in.Model = strings.TrimSpace(in.Base), strings.TrimSpace(in.Model)
	if in.Base == "" && in.Model == "" {
		writeStatus(w, http.StatusBadRequest, map[string]any{"error": "base 나 model 이 있어야 합니다"})
		return
	}
	if socket, _, _ := a.chat(r).Bound(); socket == "" {
		writeStatus(w, http.StatusConflict, map[string]any{"error": "아직 어느 컴패니언에도 안 붙었습니다"})
		return
	}
	did := []string{}
	if in.Base != "" {
		known := false
		current := ""
		if st, err := a.chat(r).Status(); err == nil {
			current, _ = st["backend"].(string)
		}
		for _, p := range a.providers(r.Context(), current) {
			if p.Base == in.Base {
				known = true
			}
		}
		if !known {
			writeStatus(w, http.StatusBadRequest, map[string]any{"error": "그 주소에서 답하는 프로바이더가 없습니다 — 명단에 있는 것만 받습니다", "base": in.Base})
			return
		}
		if err := a.chat(r).UseBackend(in.Base); err != nil {
			writeStatus(w, http.StatusBadGateway, map[string]any{"error": "백엔드를 못 바꿨습니다: " + err.Error()})
			return
		}
		did = append(did, "백엔드 → "+in.Base)
	}
	if in.Model != "" {
		if err := a.chat(r).SetModel(in.Model); err != nil {
			writeStatus(w, http.StatusBadGateway, map[string]any{"error": "모델을 못 바꿨습니다: " + err.Error(), "did": did})
			return
		}
		did = append(did, "모델 → "+in.Model)
	}
	writeJSON(w, map[string]any{"did": did, "note": strings.Join(did, " · ") + " — 다음 턴부터입니다"})
}

// POST /api/compact — 압축은 데몬이 한다. 던지고 202; 결과는 전사의 compaction 사건과 /api/context 로 온다.
func (a *API) compact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST 로"})
		return
	}
	if socket, _, _ := a.chat(r).Bound(); socket == "" {
		writeStatus(w, http.StatusConflict, map[string]any{"error": "아직 어느 컴패니언에도 안 붙었습니다"})
		return
	}
	if err := a.chat(r).Compact(); err != nil {
		writeStatus(w, http.StatusBadGateway, map[string]any{"error": "압축을 못 시켰습니다: " + err.Error()})
		return
	}
	writeStatus(w, http.StatusAccepted, map[string]any{"note": "압축을 시켰습니다 — 끝나면 창의 구성이 바뀝니다"})
}
