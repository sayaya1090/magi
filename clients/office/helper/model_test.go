package office

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// 모델·컨텍스트 문은 붙기 전에는 **사유를 실어** 거절한다 — 빈 답이나 500 이면 창은 「고장」과 「아직」을 못 가른다.
func TestModelDoorsSayWhyBeforeTheyAreBound(t *testing.T) {
	api := &API{App: Word, Bridge: NewBridge(), Bridges: NewBridges(), ConfigDir: t.TempDir()}
	for _, tc := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"context", "GET", "/api/context?deck=d1", "", 409},
		{"models", "GET", "/api/models?deck=d1", "", 409},
		{"compact", "POST", "/api/compact?deck=d1", "", 409},
		{"compact-get", "GET", "/api/compact?deck=d1", "", 405},
		{"model-empty", "POST", "/api/model?deck=d1", `{}`, 400},
		{"model-unbound", "POST", "/api/model?deck=d1", `{"model":"sonnet"}`, 409},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		switch {
		case strings.HasPrefix(tc.path, "/api/context"):
			api.contextState(w, r)
		case strings.HasPrefix(tc.path, "/api/models"):
			api.models(w, r)
		case strings.HasPrefix(tc.path, "/api/model"):
			api.setModel(w, r)
		default:
			api.compact(w, r)
		}
		if w.Code != tc.want {
			t.Errorf("%s: %d, want %d — %s", tc.name, w.Code, tc.want, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out["error"] == "" {
			t.Errorf("%s: 사유 없는 답: %s", tc.name, w.Body.String())
		}
	}
}
