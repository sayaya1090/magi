package headless

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestOriginValidation(t *testing.T) {
	if _, e := New(context.Background(), Config{Channel: "firefox", AllowedOrigins: []string{"https://example.com"}}); e == nil {
		t.Fatal("invalid channel accepted")
	}
	for _, origin := range []string{"file:///tmp", "https://example.com/path", "https://u:p@example.com", ""} {
		if _, e := New(context.Background(), Config{AllowedOrigins: []string{origin}}); e == nil {
			t.Fatalf("accepted %q", origin)
		}
	}
}
func TestLiveHeadless(t *testing.T) {
	if os.Getenv("MAGI_HEADLESS_LIVE") != "1" {
		t.Skip("set MAGI_HEADLESS_LIVE=1 for real Chromium")
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<title>Fixture</title><p>Hello</p><input aria-label="name"><input type="password" value="secret"><button onclick="document.querySelector('p').textContent='Done'">Go</button>`))
	}))
	defer s.Close()
	ctx := context.Background()
	b, e := New(ctx, Config{Channel: os.Getenv("MAGI_HEADLESS_CHANNEL"), AllowedOrigins: []string{s.URL}, InitialURL: s.URL, AllowActions: true})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	call := func(name string, args any) (any, error) {
		raw, _ := json.Marshal(args)
		return b.Call(ctx, "", name, raw)
	}
	tabs, e := call("list_tabs", map[string]any{})
	if e != nil || len(tabs.(map[string]any)["tabs"].([]any)) != 1 {
		t.Fatalf("tab contract %v %v", tabs, e)
	}
	if _, err := call("navigate", map[string]any{"tabId": 1, "url": strings.Replace(s.URL, "http://", "http://user:password@", 1)}); err == nil {
		t.Fatal("credential-bearing URL accepted")
	}
	if _, e = call("navigate", map[string]any{"tabId": 1, "url": "https://example.com"}); e == nil {
		t.Fatal("outside origin accepted")
	}
	v, e := call("read_page", map[string]any{"tabId": 1})
	if e != nil {
		t.Fatal(e)
	}
	snap := v.(map[string]any)
	raw, _ := json.Marshal(snap)
	if strings.Contains(string(raw), "secret") {
		t.Fatal("password leaked")
	}
	if string(raw) == "" {
		t.Fatal("empty snapshot")
	}
	filled := false
	for _, el := range snap["elements"].([]any) {
		item := el.(map[string]any)
		if item["label"] == "name" {
			filled = true
			_, e = call("fill", map[string]any{"tabId": 1, "snapshot": snap["snapshot"], "elementId": item["elementId"], "text": "Alice"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = call("click", map[string]any{"tabId": 1, "snapshot": snap["snapshot"], "elementId": item["elementId"]}); e == nil {
				t.Fatal("stale snapshot accepted")
			}
		}
	}
	if !filled {
		t.Fatal("fixture input not found; fill coverage missing")
	}
	v, e = call("read_page", map[string]any{"tabId": 1})
	if e != nil {
		t.Fatal(e)
	}
	snap = v.(map[string]any)
	clicked := false
	for _, el := range snap["elements"].([]any) {
		item := el.(map[string]any)
		if item["label"] == "Go" {
			clicked = true
			if _, e = call("click", map[string]any{"tabId": 1, "snapshot": snap["snapshot"], "elementId": item["elementId"]}); e != nil {
				t.Fatal(e)
			}
		}
	}
	if !clicked {
		t.Fatal("fixture button not found; click coverage missing")
	}
	v, e = call("read_page", map[string]any{"tabId": 1})
	if e != nil || v.(map[string]any)["text"] != "DoneGo" {
		t.Fatalf("unexpected result %v %v", v, e)
	}
	// Distinct lease targets have independent browser profiles and page state.
	a, _ := json.Marshal(map[string]any{"tabId": 1})
	isolated, e := b.Call(ctx, "other-lease", "read_page", a)
	if e != nil || isolated.(map[string]any)["text"] != "HelloGo" {
		t.Fatalf("lease isolation %v %v", isolated, e)
	}
	if e = b.Release("other-lease"); e != nil {
		t.Fatal(e)
	}
	b2, e := New(ctx, Config{Channel: os.Getenv("MAGI_HEADLESS_CHANNEL"), AllowedOrigins: []string{s.URL}, InitialURL: s.URL})
	if e != nil {
		t.Fatal(e)
	}
	defer b2.Close()
	args, _ := json.Marshal(map[string]any{"tabId": 1, "url": s.URL})
	if _, e = b2.Call(ctx, "", "navigate", args); e == nil {
		t.Fatal("actions allowed by default")
	}
	if e = b2.Close(); e != nil {
		t.Fatal(e)
	}
	if b2.cmd.ProcessState == nil || !b2.cmd.ProcessState.Exited() {
		t.Fatal("runner not reaped")
	}
	if e = b2.Close(); e != nil {
		t.Fatal("Close not idempotent", e)
	}
}
