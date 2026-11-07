package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Peek reports only what is cached and makes no request; Warm fills the rest in the background.
func TestWarmFillsTheCacheThatPeekReads(t *testing.T) {
	b := &batchServer{}
	srv := httptest.NewServer(http.HandlerFunc(b.handler))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", CacheDir: t.TempDir()}
	in := texts(10)
	if _, all := c.Peek(in); all {
		t.Fatal("an empty cache cannot be complete")
	}
	if len(b.sizes) != 0 {
		t.Fatal("Peek must not make a request")
	}
	c.Warm(in)
	waitUntil(t, func() bool { _, all := c.Peek(in); return all })
	vecs, _ := c.Peek(in)
	for i, v := range vecs {
		if v[0] != float32(len(in[i])) {
			t.Fatalf("vector %d is not its text's", i)
		}
	}
}

// While one warm runs, another call adds no work; a client without a cache never warms.
func TestOnlyOneWarmRunsAtATime(t *testing.T) {
	var requests atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		<-release
		http.Error(w, "stop", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", CacheDir: t.TempDir()}
	c.Warm(texts(3))
	waitUntil(t, func() bool { return requests.Load() == 1 })
	c.Warm(texts(3))
	time.Sleep(50 * time.Millisecond)
	if n := requests.Load(); n != 1 {
		t.Fatalf("a second warm ran while the first was in flight: %d requests", n)
	}
	close(release)
	waitUntil(t, func() bool { c.warmMu.Lock(); defer c.warmMu.Unlock(); return !c.warming })

	noCache := &Client{BaseURL: srv.URL, Model: "m"}
	if noCache.Peekable() {
		t.Fatal("a client without a cache directory cannot be peeked")
	}
	noCache.Warm(texts(3))
	time.Sleep(50 * time.Millisecond)
	if n := requests.Load(); n != 1 {
		t.Fatalf("a cacheless client warmed: %d requests", n)
	}
	_ = context.Background
}
