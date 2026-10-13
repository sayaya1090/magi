package office

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnidentifiedPaneCannotSubmitToFallbackConversation(t *testing.T) {
	a := &API{Bridges: NewBridges()}
	for _, path := range []string{"/api/submit", "/api/steer"} {
		called := false
		w := httptest.NewRecorder()
		a.say(w, httptest.NewRequest("POST", path, strings.NewReader(`{"text":"edit this deck"}`)), func(string) error { called = true; return nil })
		if w.Code != 409 || called {
			t.Fatalf("unidentified pane reached fallback: %d called=%v", w.Code, called)
		}
	}
	called := false
	w := httptest.NewRecorder()
	a.say(w, httptest.NewRequest("POST", "/api/submit?deck=pid-source", strings.NewReader(`{"text":"edit source"}`)), func(text string) error { called = text == "edit source"; return nil })
	if w.Code != 202 || !called {
		t.Fatalf("identified pane rejected: %d", w.Code)
	}
}
