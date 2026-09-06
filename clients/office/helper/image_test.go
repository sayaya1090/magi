package office

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// 작업창은 도구의 그림을 경로로만 받는다. 이 문이 그 파일을 내주되, 데몬의 images 디렉토리 아래의 그림 파일만
// 내준다 — 경로를 받아 파일을 주는 문을 그렇게 닫지 않으면 구멍이다.
func TestImageDoorServesOnlyPicturesUnderTheImagesRoot(t *testing.T) {
	root := t.TempDir()
	sess := filepath.Join(root, "s_1")
	if err := os.MkdirAll(sess, 0o700); err != nil {
		t.Fatal(err)
	}
	png := filepath.Join(sess, "mcp__xl__render_range-abc.png")
	if err := os.WriteFile(png, []byte("\x89PNG fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "config.toml")
	_ = os.WriteFile(secret, []byte("api_key = 'x'"), 0o600)
	api := &API{App: XL, ImageRoot: root}
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		api.image(w, httptest.NewRequest(http.MethodGet, "/api/image?path="+path, nil))
		return w
	}
	if w := get(png); w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || w.Body.Len() == 0 {
		t.Errorf("a picture under the root must be served as an image: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if w := get(secret); w.Code != http.StatusForbidden {
		t.Errorf("a file outside the root must be refused, got %d", w.Code)
	}
	if w := get(filepath.Join(sess, "..", "..", "config.toml")); w.Code != http.StatusForbidden {
		t.Errorf("a path climbing out of the root must be refused, got %d", w.Code)
	}
	txt := filepath.Join(sess, "notes.txt")
	_ = os.WriteFile(txt, []byte("hi"), 0o600)
	if w := get(txt); w.Code != http.StatusForbidden {
		t.Errorf("a non-picture under the root must be refused, got %d", w.Code)
	}
	if w := get(filepath.Join(sess, "missing.png")); w.Code != http.StatusNotFound {
		t.Errorf("a missing picture is 404, got %d", w.Code)
	}
}
