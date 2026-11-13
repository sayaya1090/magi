package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMermaidAssetServedOffline(t *testing.T) {
	s := &server{}
	w := httptest.NewRecorder()
	s.asset(w, httptest.NewRequest("GET", "/vendor/magi-diagram.js", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "window.magiMermaid") {
		t.Fatalf("missing renderer: %d", w.Code)
	}
	if w.Header().Get("ETag") == "" {
		t.Fatal("renderer needs revalidation")
	}
}
